// Package update 实现自动更新的验签、下载与替换回滚。
//
// 信任模型：
//   - 更新清单与安装包都由独立的 Ed25519 更新私钥签名，客户端只内置公钥；
//   - 清单签名覆盖除顶层 signature 外的全部字段；
//   - 包签名覆盖包的 SHA-256 摘要，下载后还要再比对摘要与长度；
//   - 任一环节失败都拒绝安装，不存在“未签名也允许”的开关。
package update

import (
	"bytes"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"runtime"
	"strconv"
	"strings"
	"time"
)

// SchemaVersion 是更新清单的当前版本。
const SchemaVersion = 1

// Kind 用于识别清单类型，避免把别的 JSON 当成更新清单。
const Kind = "filededup.update.manifest"

// Asset 是清单里的一个可下载文件。
type Asset struct {
	Name      string `json:"name"`
	OS        string `json:"os,omitempty"`
	Arch      string `json:"arch,omitempty"`
	URL       string `json:"url"`
	Size      int64  `json:"size"`
	SHA256    string `json:"sha256"`
	Signature string `json:"signature"`
}

// Manifest 是更新清单。Signature 为 Ed25519 签名（base64url，无填充）。
type Manifest struct {
	SchemaVersion int       `json:"schemaVersion"`
	Kind          string    `json:"kind"`
	Version       string    `json:"version"`
	ReleasedAt    string    `json:"releasedAt,omitempty"`
	Notes         string    `json:"notes,omitempty"`
	Assets        []Asset   `json:"assets"`
	Signature     string    `json:"signature"`
	SourceURL     string    `json:"-"`
	FetchedAt     time.Time `json:"-"`
}

// ManifestContext 是清单签名域分隔前缀。
const ManifestContext = "FILEDEDUP-UPDATE-MANIFEST-1:"

// PackageContext 是包签名域分隔前缀。
const PackageContext = "FILEDEDUP-UPDATE-PACKAGE-1:sha256="

// canonicalPayload 返回用于签名的字节：signature 字段置空后的紧凑 JSON。
func (m Manifest) canonicalPayload() ([]byte, error) {
	m.Signature = ""
	// SourceURL / FetchedAt 带 json:"-"，不会进入 payload。
	return json.Marshal(m)
}

// ManifestSigningBytes 返回实际参与签名的消息。
func (m Manifest) ManifestSigningBytes() ([]byte, error) {
	payload, err := m.canonicalPayload()
	if err != nil {
		return nil, err
	}
	prefix := ManifestContext + base64.RawURLEncoding.EncodeToString(payload)
	return []byte(prefix), nil
}

// SignManifest 用私钥写入签名。
func SignManifest(m *Manifest, priv ed25519.PrivateKey) error {
	msg, err := m.ManifestSigningBytes()
	if err != nil {
		return err
	}
	m.Signature = base64.RawURLEncoding.EncodeToString(ed25519.Sign(priv, msg))
	return nil
}

// PackageSigningBytes 返回包摘要对应的签名消息。
func PackageSigningBytes(sha256Hex string) []byte {
	return []byte(PackageContext + strings.ToLower(strings.TrimSpace(sha256Hex)))
}

// SignPackageDigest 用私钥签名包摘要。
func SignPackageDigest(priv ed25519.PrivateKey, sha256Hex string) string {
	return base64.RawURLEncoding.EncodeToString(ed25519.Sign(priv, PackageSigningBytes(sha256Hex)))
}

// ParseManifest 严格解析清单：拒绝未知键、尾随数据与非法结构。
func ParseManifest(data []byte) (Manifest, error) {
	var m Manifest
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&m); err != nil {
		return m, fmt.Errorf("更新清单不是合法 JSON 或含未知字段：%w", err)
	}
	if dec.More() {
		return m, fmt.Errorf("更新清单在第一个 JSON 对象后还有多余内容")
	}
	if m.SchemaVersion != SchemaVersion {
		return m, fmt.Errorf("更新清单版本不支持：schemaVersion=%d，客户端支持 %d", m.SchemaVersion, SchemaVersion)
	}
	if m.Kind != Kind {
		return m, fmt.Errorf("更新清单类型不正确：kind=%q", m.Kind)
	}
	if strings.TrimSpace(m.Version) == "" {
		return m, fmt.Errorf("更新清单缺少 version")
	}
	if _, err := ParseVersion(m.Version); err != nil {
		return m, fmt.Errorf("更新清单版本号非法：%w", err)
	}
	if len(m.Assets) == 0 {
		return m, fmt.Errorf("更新清单没有 assets")
	}
	for i := range m.Assets {
		if err := validateAsset(m.Assets[i]); err != nil {
			return m, fmt.Errorf("更新清单第 %d 个 asset 非法：%w", i+1, err)
		}
	}
	if m.Signature == "" {
		return m, fmt.Errorf("更新清单未签名，拒绝使用")
	}
	return m, nil
}

func validateAsset(a Asset) error {
	if strings.TrimSpace(a.Name) == "" {
		return fmt.Errorf("name 为空")
	}
	if strings.ContainsAny(a.Name, `/\`) || a.Name == "." || a.Name == ".." {
		return fmt.Errorf("name 只能是文件名：%q", a.Name)
	}
	u, err := url.Parse(a.URL)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return fmt.Errorf("url 必须是 http/https 绝对地址：%q", a.URL)
	}
	if a.Size <= 0 {
		return fmt.Errorf("size 必须大于 0")
	}
	if len(a.SHA256) != 64 {
		return fmt.Errorf("sha256 必须是 64 位十六进制")
	}
	if _, err := hex.DecodeString(a.SHA256); err != nil {
		return fmt.Errorf("sha256 含非十六进制字符")
	}
	if a.Signature == "" {
		return fmt.Errorf("缺少包签名，拒绝使用")
	}
	return nil
}

// VerifyManifest 校验清单签名。
func VerifyManifest(m Manifest, pub ed25519.PublicKey) error {
	sig, err := base64.RawURLEncoding.DecodeString(m.Signature)
	if err != nil {
		return fmt.Errorf("清单签名不是合法的 base64url：%w", err)
	}
	msg, err := m.ManifestSigningBytes()
	if err != nil {
		return err
	}
	if !ed25519.Verify(pub, msg, sig) {
		return fmt.Errorf("更新清单签名校验失败")
	}
	return nil
}

// VerifyAsset 校验包签名（针对清单里声明的摘要）。
func VerifyAsset(a Asset, pub ed25519.PublicKey) error {
	sig, err := base64.RawURLEncoding.DecodeString(a.Signature)
	if err != nil {
		return fmt.Errorf("包签名不是合法的 base64url：%w", err)
	}
	if !ed25519.Verify(pub, PackageSigningBytes(a.SHA256), sig) {
		return fmt.Errorf("安装包签名校验失败")
	}
	return nil
}

// SelectAsset 按当前系统挑选安装包；优先精确匹配 os/arch，其次回退到未限定平台的 asset。
func SelectAsset(m Manifest, goos, goarch string) (Asset, error) {
	var fallback *Asset
	for i := range m.Assets {
		a := m.Assets[i]
		if a.OS == "" && a.Arch == "" {
			if fallback == nil {
				fallback = &m.Assets[i]
			}
			continue
		}
		if a.OS == goos && a.Arch == goarch {
			return a, nil
		}
	}
	if fallback != nil {
		return *fallback, nil
	}
	return Asset{}, fmt.Errorf("更新清单中没有适用于 %s/%s 的安装包", goos, goarch)
}

// CurrentAsset 是 SelectAsset(runtime.GOOS, runtime.GOARCH) 的便捷包装。
func (m Manifest) CurrentAsset() (Asset, error) {
	return SelectAsset(m, runtime.GOOS, runtime.GOARCH)
}

// Version 是解析后的版本号。
type Version struct {
	core       []int
	prerelease []string
	raw        string
}

// ParseVersion 解析 1–4 段数字版本，允许 v 前缀、预发布后缀与构建元数据。
func ParseVersion(s string) (Version, error) {
	raw := strings.TrimSpace(s)
	v := Version{raw: raw}
	body := strings.TrimPrefix(strings.TrimPrefix(raw, "v"), "V")
	if body == "" {
		return v, fmt.Errorf("版本号为空")
	}
	if i := strings.IndexByte(body, '+'); i >= 0 {
		body = body[:i]
	}
	if i := strings.IndexByte(body, '-'); i >= 0 {
		pre := body[i+1:]
		body = body[:i]
		if pre == "" {
			return v, fmt.Errorf("预发布标识为空")
		}
		v.prerelease = strings.Split(pre, ".")
	}
	parts := strings.Split(body, ".")
	if len(parts) == 0 || len(parts) > 4 {
		return v, fmt.Errorf("版本号应为 1–4 段数字：%q", s)
	}
	for _, p := range parts {
		if p == "" {
			return v, fmt.Errorf("版本号含空段：%q", s)
		}
		n, err := strconv.Atoi(p)
		if err != nil || n < 0 {
			return v, fmt.Errorf("版本号段不是非负整数：%q", p)
		}
		v.core = append(v.core, n)
	}
	for len(v.core) < 4 {
		v.core = append(v.core, 0)
	}
	return v, nil
}

// CompareVersion 比较两个版本：a<b 返回 -1，相等 0，a>b 返回 1。
func CompareVersion(a, b string) (int, error) {
	va, err := ParseVersion(a)
	if err != nil {
		return 0, err
	}
	vb, err := ParseVersion(b)
	if err != nil {
		return 0, err
	}
	for i := 0; i < 4; i++ {
		if va.core[i] < vb.core[i] {
			return -1, nil
		}
		if va.core[i] > vb.core[i] {
			return 1, nil
		}
	}
	return comparePrerelease(va.prerelease, vb.prerelease), nil
}

func comparePrerelease(a, b []string) int {
	// 有预发布标识的版本低于同核心版本。
	if len(a) == 0 && len(b) == 0 {
		return 0
	}
	if len(a) == 0 {
		return 1
	}
	if len(b) == 0 {
		return -1
	}
	for i := 0; i < len(a) && i < len(b); i++ {
		if a[i] == b[i] {
			continue
		}
		na, ea := strconv.Atoi(a[i])
		nb, eb := strconv.Atoi(b[i])
		switch {
		case ea == nil && eb == nil:
			if na < nb {
				return -1
			}
			return 1
		case ea == nil:
			return -1 // 数字标识低于字母标识
		case eb == nil:
			return 1
		default:
			if a[i] < b[i] {
				return -1
			}
			return 1
		}
	}
	if len(a) < len(b) {
		return -1
	}
	if len(a) > len(b) {
		return 1
	}
	return 0
}

// IsNewer 判断 latest 是否比 current 新。
func IsNewer(latest, current string) (bool, error) {
	c, err := CompareVersion(latest, current)
	if err != nil {
		return false, err
	}
	return c > 0, nil
}

// FileSHA256 计算文件摘要并返回字节数。
func FileSHA256(r io.Reader) (string, int64, error) {
	h := sha256.New()
	n, err := io.Copy(h, r)
	if err != nil {
		return "", n, err
	}
	return hex.EncodeToString(h.Sum(nil)), n, nil
}
