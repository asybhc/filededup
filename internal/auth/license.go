package auth

import (
	"crypto/ed25519"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	// LicenseAppID 是新许可证绑定的应用标识。
	LicenseAppID   = "filededup-go"
	licensePrefix  = "FDLIC1"
	licenseName    = "license.fdlic"
	licenseMaxSize = 64 * 1024
)

// embeddedEd25519PublicKeyB64 是签发公钥（Ed25519 原始 32 字节的 base64）。
// 私钥只在签发工具中使用，绝不进入客户端或仓库。
// 使用 var 是为了让测试可以替换成临时公钥。
var embeddedEd25519PublicKeyB64 = "wW68/Ea4R9SRjGIEv2JqyBYAyn9uvlQaOnfxoUarF14="

// Feature 是功能位名称。与许可证里的 features 字段一致。
const (
	FeatureScan     = "scan"
	FeatureApply    = "apply"
	FeatureOrganize = "organize"
)

var allFeatures = []string{FeatureScan, FeatureApply, FeatureOrganize}

// License 是验签通过后的许可证内容。
type License struct {
	Source   string    // ed25519 | legacy-ecdsa
	Edition  string    // pro | standard
	Expiry   time.Time // Forever 为 true 时无意义
	Forever  bool
	Features []string
	ID       string
	Machine  string // 许可证里的机器码（可能为 "*"）
}

// ExpiredOn 判断指定日期是否已超过到期日（到期日当天仍可用）。
func (l License) ExpiredOn(t time.Time) bool {
	if l.Forever {
		return false
	}
	ly, lm, ld := l.Expiry.Date()
	ty, tm, td := t.Date()
	if ty != ly {
		return ty > ly
	}
	if tm != lm {
		return tm > lm
	}
	return td > ld
}

// Payload 是新许可证的 JSON 载荷。
type Payload struct {
	Version  int      `json:"v"`
	App      string   `json:"app"`
	Machine  string   `json:"machine"` // "*" 表示不绑定机器
	Expiry   string   `json:"exp"`     // YYYY-MM-DD 或 forever
	Issued   string   `json:"issued"`  // YYYY-MM-DD
	Edition  string   `json:"edition"` // pro | standard
	Features []string `json:"features,omitempty"`
	ID       string   `json:"id,omitempty"`
}

// IssueLicense 用私钥签发一份新许可证。仅供签发工具与测试使用。
func IssueLicense(p Payload, priv ed25519.PrivateKey) (string, error) {
	if len(priv) != ed25519.PrivateKeySize {
		return "", errors.New("私钥长度不正确")
	}
	p.Version = 1
	p.App = LicenseAppID
	if p.Edition == "" {
		p.Edition = "standard"
	}
	raw, err := json.Marshal(p)
	if err != nil {
		return "", err
	}
	body := base64.RawURLEncoding.EncodeToString(raw)
	sig := ed25519.Sign(priv, []byte(licensePrefix+":"+body))
	return licensePrefix + ":" + body + ":" + base64.RawURLEncoding.EncodeToString(sig), nil
}

func embeddedPublicKey() (ed25519.PublicKey, error) {
	raw, err := base64.StdEncoding.DecodeString(embeddedEd25519PublicKeyB64)
	if err != nil || len(raw) != ed25519.PublicKeySize {
		return nil, errors.New("内置公钥不可用")
	}
	return ed25519.PublicKey(raw), nil
}

// VerifyLicense 校验许可证文本，自动识别新格式与旧格式。
func VerifyLicense(text string) (License, error) {
	fp, err := machineFingerprint()
	if err != nil {
		return License{}, err
	}
	return verifyLicenseWithMachine(text, fp, time.Now())
}

func verifyLicenseWithMachine(text string, fp [10]byte, now time.Time) (License, error) {
	clean := strings.TrimSpace(stripBOM(text))
	if clean == "" {
		return License{}, errors.New("许可证内容为空")
	}
	if strings.HasPrefix(clean, licensePrefix+":") {
		pub, err := embeddedPublicKey()
		if err != nil {
			return License{}, err
		}
		return verifyEd25519License(clean, pub, fp, now)
	}
	return verifyLegacyLicense(clean, fp, now)
}

func verifyEd25519License(text string, pub ed25519.PublicKey, fp [10]byte, now time.Time) (License, error) {
	// 许可证可能被换行折叠，先去掉所有空白。
	compact := strings.Map(func(r rune) rune {
		if r == ' ' || r == '\r' || r == '\n' || r == '\t' {
			return -1
		}
		return r
	}, text)
	parts := strings.Split(compact, ":")
	if len(parts) != 3 || parts[0] != licensePrefix {
		return License{}, errors.New("许可证格式不正确")
	}
	sig, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil || len(sig) != ed25519.SignatureSize {
		return License{}, errors.New("许可证签名段损坏")
	}
	if !ed25519.Verify(pub, []byte(licensePrefix+":"+parts[1]), sig) {
		return License{}, errors.New("许可证签名无效")
	}
	raw, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return License{}, errors.New("许可证载荷损坏")
	}
	var p Payload
	if err := json.Unmarshal(raw, &p); err != nil {
		return License{}, errors.New("许可证载荷不是合法 JSON")
	}
	if p.Version != 1 {
		return License{}, fmt.Errorf("不支持的许可证版本：%d", p.Version)
	}
	if p.App != LicenseAppID {
		return License{}, fmt.Errorf("许可证应用标识不匹配：%s", p.App)
	}

	machine := strings.TrimSpace(p.Machine)
	if machine == "" {
		return License{}, errors.New("许可证缺少机器码绑定")
	}
	if machine != "*" {
		want := normalizeMachineText(machine)
		got := crockfordEncode(fp[:])
		if want != got {
			return License{}, fmt.Errorf("许可证绑定的机器码不匹配（本机 %s）", groupBy4(got))
		}
	}

	lic := License{
		Source:  "ed25519",
		Edition: strings.ToLower(strings.TrimSpace(p.Edition)),
		ID:      p.ID,
		Machine: machine,
	}
	if lic.Edition == "" {
		lic.Edition = "standard"
	}
	if lic.Edition != "pro" && lic.Edition != "standard" {
		return License{}, fmt.Errorf("未知的许可证版本类型：%s", p.Edition)
	}

	exp := strings.TrimSpace(strings.ToLower(p.Expiry))
	switch exp {
	case "", "forever", "never":
		lic.Forever = true
	default:
		d, err := time.ParseInLocation("2006-01-02", exp, time.Local)
		if err != nil {
			return License{}, fmt.Errorf("许可证到期日格式不正确：%s", p.Expiry)
		}
		lic.Expiry = d
	}

	features := normalizeFeatures(p.Features)
	if len(features) == 0 {
		features = append([]string{}, allFeatures...)
	}
	lic.Features = features
	return lic, nil
}

func normalizeFeatures(in []string) []string {
	seen := make(map[string]bool, len(in))
	out := make([]string, 0, len(in))
	for _, f := range in {
		f = strings.ToLower(strings.TrimSpace(f))
		switch f {
		case FeatureScan, FeatureApply, FeatureOrganize:
			if !seen[f] {
				seen[f] = true
				out = append(out, f)
			}
		}
	}
	return out
}

func hasFeature(features []string, want string) bool {
	for _, f := range features {
		if f == want {
			return true
		}
	}
	return false
}

// legacyAppID 是旧版许可证里的应用编号 20261101。
const legacyAppID uint32 = 20261101

// legacyPublicKeySPKI 是旧版 ECDSA P-256 公钥（Base64 SPKI）。
// 使用 var 是为了让测试可以替换成临时公钥。
var legacyPublicKeySPKI = "MFkwEwYHKoZIzj0CAQYIKoZIzj0DAQcDQgAEnoZ/chqgxBdKJIPvLE68X8fDdre1Y1t+PCnyCbVF3BF7x3o0FH5/V37jZWwB1DYd9tBpyCiF3cjnH+CqSvBPWQ=="

const legacyEpochYear = 2024

func legacyEpoch() time.Time {
	return time.Date(legacyEpochYear, 1, 1, 0, 0, 0, 0, time.Local)
}

// verifyLegacyLicense 只读兼容旧版 ECDSA 许可证：验签通过即可使用。
// 许可证文本是 Crockford Base32（含连字符），解码后为 载荷 || 64 字节 P1363 签名。
func verifyLegacyLicense(text string, fp [10]byte, now time.Time) (License, error) {
	normalized := normalizeMachineText(text)
	data, ok := crockfordDecode(normalized)
	if !ok || len(data) < 81 {
		return License{}, errors.New("旧版许可证格式不正确")
	}
	payloadLen := len(data) - 64
	if payloadLen != 17 && payloadLen != 18 {
		return License{}, errors.New("旧版许可证长度不正确")
	}
	payload := data[:payloadLen]
	sig := data[payloadLen:]

	pub, err := legacyPublicKey()
	if err != nil {
		return License{}, err
	}
	digest := sha256Sum(payload)
	if !verifyP1363(pub, digest, sig) {
		return License{}, errors.New("旧版许可证签名无效")
	}
	if payload[0] != 2 {
		return License{}, fmt.Errorf("旧版许可证类型不匹配：%d", payload[0])
	}
	if appID := binary.LittleEndian.Uint32(payload[1:5]); appID != legacyAppID {
		return License{}, fmt.Errorf("旧版许可证应用编号不匹配：%d", appID)
	}
	got := crockfordEncode(fp[:])
	want := crockfordEncode(payload[5:15])
	if got != want {
		return License{}, fmt.Errorf("旧版许可证绑定的机器码不匹配（本机 %s）", groupBy4(got))
	}

	days := binary.LittleEndian.Uint16(payload[15:17])
	lic := License{
		Source:   "legacy-ecdsa",
		Edition:  "standard",
		Machine:  groupBy4(want),
		Features: append([]string{}, allFeatures...),
	}
	if payloadLen == 18 && payload[17] == 1 {
		lic.Edition = "pro"
	}
	if days == 0xFFFF {
		lic.Forever = true
		return lic, nil
	}
	lic.Expiry = legacyEpoch().AddDate(0, 0, int(days))
	return lic, nil
}

func licenseDir() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("无法定位用户配置目录：%w", err)
	}
	return filepath.Join(dir, "FileDedup"), nil
}

// LicensePath 返回本机许可证文件的存放路径。
func LicensePath() string {
	dir, err := licenseDir()
	if err != nil {
		return ""
	}
	return filepath.Join(dir, licenseName)
}

func readStoredLicense() (string, error) {
	path := LicensePath()
	if path == "" {
		return "", errors.New("无法定位用户配置目录")
	}
	info, err := os.Stat(path)
	if err != nil {
		return "", err
	}
	if info.Size() > licenseMaxSize {
		return "", errors.New("许可证文件过大")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return string(raw), nil
}

// SaveLicense 先验签再写入用户配置目录；无效许可证不会落盘。
func SaveLicense(text string) error {
	if _, err := VerifyLicense(text); err != nil {
		return err
	}
	path := LicensePath()
	if path == "" {
		return errors.New("无法定位用户配置目录")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("创建配置目录失败：%w", err)
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, []byte(strings.TrimSpace(text)+"\n"), 0o600); err != nil {
		return fmt.Errorf("写入许可证失败：%w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("替换许可证失败：%w", err)
	}
	return nil
}

// ImportLicenseText 校验并保存许可证文本。
func ImportLicenseText(text string) error {
	return SaveLicense(text)
}

func stripBOM(s string) string {
	return strings.TrimPrefix(s, "\uFEFF")
}
