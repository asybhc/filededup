package update

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// 内置默认更新源：仓库的最新 release。
const (
	DefaultOwner = "asybhc"
	DefaultRepo  = "filededup"
	// DefaultManifestAsset 是每个 release 必须附带的签名清单文件名。
	DefaultManifestAsset = "update-manifest.json"
)

// githubAPIBase 允许测试替换 GitHub API 根地址。
var githubAPIBase = "https://api.github.com"

// ReleaseAsset 是 GitHub release 里的一个资产。
type ReleaseAsset struct {
	Name   string
	URL    string
	Size   int64
	Digest string // 小写十六进制 SHA-256；GitHub 未提供时为空
}

// Release 是最新 release 的解析结果。
type Release struct {
	Tag         string
	Version     string
	Assets      []ReleaseAsset
	ManifestURL string
	Installer   *ReleaseAsset
}

// AssetByName 按文件名（不区分大小写）查找资产。
func (r Release) AssetByName(name string) (ReleaseAsset, bool) {
	for _, a := range r.Assets {
		if strings.EqualFold(a.Name, name) {
			return a, true
		}
	}
	return ReleaseAsset{}, false
}

// AssetNames 返回资产名列表，用于错误信息。
func (r Release) AssetNames() string {
	if len(r.Assets) == 0 {
		return "（无资产）"
	}
	names := make([]string, 0, len(r.Assets))
	for _, a := range r.Assets {
		names = append(names, a.Name)
	}
	return strings.Join(names, "、")
}

// FetchLatestRelease 查询仓库最新 release，解析标签、签名清单与安装包资产。
// 失败时保留真实的 HTTP 状态码与响应片段，便于界面直接展示。
func FetchLatestRelease(ctx context.Context, client *http.Client, owner, repo string) (Release, error) {
	owner = strings.TrimSpace(owner)
	repo = strings.TrimSpace(repo)
	if owner == "" || repo == "" {
		return Release{}, fmt.Errorf("内置更新源未配置仓库")
	}
	if client == nil {
		client = &http.Client{Timeout: 20 * time.Second}
	}
	api := strings.TrimRight(githubAPIBase, "/") + "/repos/" + owner + "/" + repo + "/releases/latest"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, api, nil)
	if err != nil {
		return Release{}, fmt.Errorf("构造 GitHub 请求失败：%w", err)
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	req.Header.Set("User-Agent", "filededup-updater")
	resp, err := client.Do(req)
	if err != nil {
		return Release{}, fmt.Errorf("请求 GitHub 最新发布失败：%w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return Release{}, fmt.Errorf("请求 GitHub 最新发布失败：HTTP %d：%s", resp.StatusCode, responseSnippet(body))
	}
	var payload struct {
		TagName string `json:"tag_name"`
		Assets  []struct {
			Name               string `json:"name"`
			Size               int64  `json:"size"`
			BrowserDownloadURL string `json:"browser_download_url"`
			Digest             string `json:"digest"`
		} `json:"assets"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 4<<20)).Decode(&payload); err != nil {
		return Release{}, fmt.Errorf("GitHub 最新发布响应不是合法 JSON：%w", err)
	}
	tag := strings.TrimSpace(payload.TagName)
	if tag == "" {
		return Release{}, fmt.Errorf("GitHub 最新发布缺少 tag_name")
	}
	ver := strings.TrimPrefix(strings.TrimPrefix(tag, "v"), "V")
	if _, err := ParseVersion(ver); err != nil {
		return Release{}, fmt.Errorf("GitHub 最新发布的标签不是合法版本号：%q", tag)
	}

	rel := Release{Tag: tag, Version: ver}
	for _, a := range payload.Assets {
		name := strings.TrimSpace(a.Name)
		url := strings.TrimSpace(a.BrowserDownloadURL)
		if name == "" || url == "" {
			continue
		}
		digest := parseGitHubDigest(a.Digest)
		rel.Assets = append(rel.Assets, ReleaseAsset{Name: name, URL: url, Size: a.Size, Digest: digest})
	}
	if len(rel.Assets) == 0 {
		return Release{}, fmt.Errorf("GitHub 最新发布 %s 没有可下载资产", tag)
	}

	// 签名清单是自动更新的信任入口，必须由 release 附带。
	for _, a := range rel.Assets {
		if isManifestAsset(a.Name) {
			rel.ManifestURL = a.URL
			break
		}
	}
	if rel.ManifestURL == "" {
		return Release{}, fmt.Errorf("GitHub 最新发布 %s 未附带签名清单 %s（现有资产：%s）",
			tag, DefaultManifestAsset, rel.AssetNames())
	}

	for i := range rel.Assets {
		a := &rel.Assets[i]
		if isSetupAsset(a.Name) {
			rel.Installer = a
			break
		}
	}
	if rel.Installer == nil {
		for i := range rel.Assets {
			a := &rel.Assets[i]
			if isInstallerAsset(a.Name) {
				rel.Installer = a
				break
			}
		}
	}
	if rel.Installer == nil {
		return Release{}, fmt.Errorf("GitHub 最新发布 %s 没有安装包资产（现有资产：%s）", tag, rel.AssetNames())
	}
	return rel, nil
}

func isManifestAsset(name string) bool {
	lower := strings.ToLower(name)
	if lower == DefaultManifestAsset || lower == "update.json" {
		return true
	}
	return strings.HasSuffix(lower, ".manifest.json")
}

func isInstallerAsset(name string) bool {
	return strings.HasSuffix(strings.ToLower(name), ".exe")
}

func isSetupAsset(name string) bool {
	lower := strings.ToLower(name)
	if !isInstallerAsset(name) {
		return false
	}
	return strings.Contains(lower, "setup") || strings.Contains(lower, "安装")
}

func parseGitHubDigest(raw string) string {
	s := strings.ToLower(strings.TrimSpace(raw))
	s = strings.TrimPrefix(s, "sha256:")
	if len(s) != 64 {
		return ""
	}
	for _, r := range s {
		if !strings.ContainsRune("0123456789abcdef", r) {
			return ""
		}
	}
	return s
}

func responseSnippet(body []byte) string {
	s := strings.Join(strings.Fields(string(body)), " ")
	if s == "" {
		return "（响应体为空）"
	}
	if len([]rune(s)) <= 300 {
		return s
	}
	return string([]rune(s)[:300]) + "…"
}
