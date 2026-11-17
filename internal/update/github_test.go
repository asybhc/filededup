package update

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func withGitHubBase(t *testing.T, base string) {
	t.Helper()
	old := githubAPIBase
	githubAPIBase = base
	t.Cleanup(func() { githubAPIBase = old })
}

func TestFetchLatestReleaseSuccess(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/repos/OWNER/REPO/releases/latest" {
			t.Errorf("请求路径 = %q", r.URL.Path)
		}
		if got := r.Header.Get("Accept"); got != "application/vnd.github+json" {
			t.Errorf("Accept = %q", got)
		}
		if got := r.Header.Get("User-Agent"); got == "" {
			t.Error("缺少 User-Agent")
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"tag_name": "v0.10.9",
			"assets": [
				{"name":"update-manifest.json","size":512,"browser_download_url":"https://example.invalid/manifest.json","digest":""},
				{"name":"文件整理.一杯好茶.exe","size":12932608,"browser_download_url":"https://example.invalid/portable.exe","digest":"sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"},
				{"name":"文件整理.一杯好茶-Setup-0.10.9.exe","size":14500000,"browser_download_url":"https://example.invalid/setup.exe","digest":"sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"}
			]
		}`))
	}))
	defer srv.Close()
	withGitHubBase(t, srv.URL)

	rel, err := FetchLatestRelease(context.Background(), srv.Client(), "OWNER", "REPO")
	if err != nil {
		t.Fatalf("FetchLatestRelease 失败：%v", err)
	}
	if rel.Version != "0.10.9" || rel.Tag != "v0.10.9" {
		t.Fatalf("版本解析错误：tag=%q version=%q", rel.Tag, rel.Version)
	}
	if rel.ManifestURL != "https://example.invalid/manifest.json" {
		t.Errorf("ManifestURL = %q", rel.ManifestURL)
	}
	if rel.Installer == nil {
		t.Fatal("没有选中安装包资产")
	}
	if !strings.Contains(rel.Installer.Name, "Setup") {
		t.Errorf("应优先选择 Setup 安装包，实际 %q", rel.Installer.Name)
	}
	if rel.Installer.Digest != strings.Repeat("b", 64) {
		t.Errorf("digest 解析错误：%q", rel.Installer.Digest)
	}
}

func TestFetchLatestReleaseHTTPErrorKeepsStatusAndSnippet(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"message":"Not Found","documentation_url":"https://docs.github.com"}`))
	}))
	defer srv.Close()
	withGitHubBase(t, srv.URL)

	_, err := FetchLatestRelease(context.Background(), srv.Client(), "OWNER", "REPO")
	if err == nil {
		t.Fatal("404 应返回错误")
	}
	msg := err.Error()
	if !strings.Contains(msg, "HTTP 404") {
		t.Errorf("错误缺少 HTTP 状态：%v", err)
	}
	if !strings.Contains(msg, "Not Found") {
		t.Errorf("错误缺少响应片段：%v", err)
	}
}

func TestFetchLatestReleaseMissingManifestListsAssets(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"tag_name":"v0.10.10","assets":[
			{"name":"fdedup-ui.exe","size":100,"browser_download_url":"https://example.invalid/a.exe","digest":""}
		]}`))
	}))
	defer srv.Close()
	withGitHubBase(t, srv.URL)

	_, err := FetchLatestRelease(context.Background(), srv.Client(), "OWNER", "REPO")
	if err == nil {
		t.Fatal("缺少签名清单时应报错")
	}
	if !strings.Contains(err.Error(), "update-manifest.json") || !strings.Contains(err.Error(), "fdedup-ui.exe") {
		t.Errorf("错误应同时指出缺失清单与现有资产：%v", err)
	}
}

func TestFetchLatestReleaseRejectsBadTag(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"tag_name":"nightly","assets":[
			{"name":"update-manifest.json","size":1,"browser_download_url":"https://example.invalid/m.json","digest":""},
			{"name":"a.exe","size":1,"browser_download_url":"https://example.invalid/a.exe","digest":""}
		]}`))
	}))
	defer srv.Close()
	withGitHubBase(t, srv.URL)

	_, err := FetchLatestRelease(context.Background(), srv.Client(), "OWNER", "REPO")
	if err == nil {
		t.Fatal("非法版本标签应报错")
	}
	if !strings.Contains(err.Error(), "nightly") {
		t.Errorf("错误应包含原始标签：%v", err)
	}
}
