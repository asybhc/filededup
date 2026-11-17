package update

import (
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// ManifestMaxBytes 是清单大小上限，防止误抓大文件。
const ManifestMaxBytes = 1 << 20

// FetchManifest 下载并严格解析更新清单（不验签）。
func FetchManifest(ctx context.Context, client *http.Client, rawURL string) (Manifest, []byte, error) {
	if err := validateHTTPURL(rawURL); err != nil {
		return Manifest{}, nil, err
	}
	if client == nil {
		client = &http.Client{Timeout: 15 * time.Second}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return Manifest{}, nil, fmt.Errorf("构造更新清单请求失败：%w", err)
	}
	req.Header.Set("Cache-Control", "no-cache")
	resp, err := client.Do(req)
	if err != nil {
		return Manifest{}, nil, fmt.Errorf("下载更新清单失败：%w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return Manifest{}, nil, fmt.Errorf("下载更新清单失败：HTTP %d：%s", resp.StatusCode, responseSnippet(body))
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, ManifestMaxBytes+1))
	if err != nil {
		return Manifest{}, nil, fmt.Errorf("读取更新清单失败：%w", err)
	}
	if len(data) > ManifestMaxBytes {
		return Manifest{}, nil, fmt.Errorf("更新清单超过 %d 字节上限", ManifestMaxBytes)
	}
	m, err := ParseManifest(data)
	if err != nil {
		return Manifest{}, data, err
	}
	m.SourceURL = rawURL
	m.FetchedAt = time.Now()
	return m, data, nil
}

func validateHTTPURL(raw string) error {
	if strings.TrimSpace(raw) == "" {
		return fmt.Errorf("更新源为空")
	}
	u, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("更新源不是合法地址：%w", err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("更新源必须是 http/https 地址")
	}
	if u.Host == "" {
		return fmt.Errorf("更新源缺少主机名")
	}
	return nil
}

// VerifyFile 校验文件长度与 SHA-256。
func VerifyFile(path string, size int64, wantSHA string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	sum, n, err := FileSHA256(f)
	if err != nil {
		return fmt.Errorf("读取文件失败：%w", err)
	}
	if size > 0 && n != size {
		return fmt.Errorf("文件长度不符：期望 %d，实际 %d", size, n)
	}
	if !strings.EqualFold(sum, strings.TrimSpace(wantSHA)) {
		return fmt.Errorf("文件 SHA-256 不符：期望 %s，实际 %s", wantSHA, sum)
	}
	return nil
}

// DownloadAsset 下载安装包到 dir，并校验长度、摘要与包签名。
// 已存在且校验通过的文件会直接复用，不重复下载。
func DownloadAsset(ctx context.Context, client *http.Client, a Asset, dir string) (string, error) {
	return downloadAsset(ctx, client, a, dir, mustPublicKey())
}

func downloadAsset(ctx context.Context, client *http.Client, a Asset, dir string, pub ed25519.PublicKey) (string, error) {
	if err := validateAsset(a); err != nil {
		return "", err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("创建更新缓存目录失败：%w", err)
	}
	final := filepath.Join(dir, a.Name)
	if VerifyFile(final, a.Size, a.SHA256) == nil {
		if err := VerifyAsset(a, pub); err != nil {
			_ = os.Remove(final)
			return "", err
		}
		return final, nil
	}
	_ = os.Remove(final)
	if client == nil {
		client = &http.Client{Timeout: 5 * time.Minute}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, a.URL, nil)
	if err != nil {
		return "", fmt.Errorf("构造下载请求失败：%w", err)
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("下载安装包失败：%w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return "", fmt.Errorf("下载安装包失败：HTTP %d：%s", resp.StatusCode, responseSnippet(body))
	}

	tmp := final + ".part"
	_ = os.Remove(tmp)
	out, err := os.OpenFile(tmp, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		return "", fmt.Errorf("创建临时文件失败：%w", err)
	}
	h := sha256.New()
	n, copyErr := io.Copy(io.MultiWriter(out, h), io.LimitReader(resp.Body, a.Size+1))
	closeErr := out.Close()
	if copyErr != nil {
		_ = os.Remove(tmp)
		return "", fmt.Errorf("写入安装包失败：%w", copyErr)
	}
	if closeErr != nil {
		_ = os.Remove(tmp)
		return "", fmt.Errorf("关闭临时文件失败：%w", closeErr)
	}
	if n != a.Size {
		_ = os.Remove(tmp)
		return "", fmt.Errorf("安装包长度不符：期望 %d，实际 %d", a.Size, n)
	}
	sum := hex.EncodeToString(h.Sum(nil))
	if !strings.EqualFold(sum, a.SHA256) {
		_ = os.Remove(tmp)
		return "", fmt.Errorf("安装包 SHA-256 不符：期望 %s，实际 %s", a.SHA256, sum)
	}
	if err := VerifyAsset(a, pub); err != nil {
		_ = os.Remove(tmp)
		return "", err
	}
	if err := os.Rename(tmp, final); err != nil {
		_ = os.Remove(tmp)
		return "", fmt.Errorf("保存安装包失败：%w", err)
	}
	return final, nil
}

// mustPublicKey 返回内置公钥；公钥损坏时返回 nil，让 ed25519.Verify 直接失败。
func mustPublicKey() []byte {
	pub, err := PublicKey()
	if err != nil {
		return nil
	}
	return pub
}
