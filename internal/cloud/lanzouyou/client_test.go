package lanzouyou

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"filededup/internal/cloud/lanzou"
)

// newTestClient 起一个本地 HTTP 服务，并把客户端的所有远端地址指向它。
func newTestClient(t *testing.T, handler http.HandlerFunc) *Client {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	c := NewClient(Credentials{
		AppToken: "tok-1",
		UID:      "42",
		Account:  "alice@example.com",
		Uuid:     "uuid-1",
		Extra:    "2",
	}, time.Millisecond)
	c.base = srv.URL
	c.uploadRoot = srv.URL + "/upload"
	c.uploadBucket = srv.URL + "/buckets/wpanstore-lanzou/objects/"
	c.pollGap = time.Millisecond
	c.maxPoll = 3
	return c
}

func writeTempFile(t *testing.T, name, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("写临时文件失败：%v", err)
	}
	return path
}

func writeJSON(w http.ResponseWriter, body string) {
	w.Header().Set("Content-Type", "application/json")
	_, _ = io.WriteString(w, body)
}

func TestLoginAndAccount(t *testing.T) {
	var mu sync.Mutex
	var loginBody map[string]string
	var authHeader string

	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/unproved/login":
			if r.Method != http.MethodPost {
				t.Errorf("登录应使用 POST，实际 %s", r.Method)
			}
			mu.Lock()
			_ = json.NewDecoder(r.Body).Decode(&loginBody)
			mu.Unlock()
			if got := r.URL.Query().Get("devType"); got != "6" {
				t.Errorf("devType = %q，期望 6", got)
			}
			writeJSON(w, `{"code":0,"data":{"appToken":"token-abc"}}`)
		case "/proved/user/account/map":
			mu.Lock()
			authHeader = r.Header.Get("Authorization")
			mu.Unlock()
			writeJSON(w, `{"code":0,"data":{"map":{"userId":"42","account":"alice@example.com","totalSize":100,"usedSize":10}}}`)
		default:
			t.Errorf("未预期的请求：%s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	})
	c.creds = Credentials{}

	creds, err := c.Login(context.Background(), "alice@example.com", "secret")
	if err != nil {
		t.Fatalf("登录失败：%v", err)
	}
	if creds.AppToken != "token-abc" {
		t.Errorf("appToken = %q，期望 token-abc", creds.AppToken)
	}
	if creds.UID != "42" || creds.Account != "alice@example.com" {
		t.Errorf("账号信息 = %+v，期望 uid=42 account=alice@example.com", creds)
	}
	if creds.Platform != lanzou.PlatformYouxiang {
		t.Errorf("platform = %q，期望 %q", creds.Platform, lanzou.PlatformYouxiang)
	}

	mu.Lock()
	defer mu.Unlock()
	if loginBody["loginName"] != "alice@example.com" || loginBody["loginPwd"] != "secret" {
		t.Errorf("登录请求体 = %+v", loginBody)
	}
	if authHeader != "Bearer token-abc" {
		t.Errorf("Authorization = %q，期望 Bearer token-abc", authHeader)
	}
}

func TestListing(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/proved/record/folder/list":
			if got := r.URL.Query().Get("offset"); got != "1" {
				t.Errorf("目录分页 offset = %q，期望 1", got)
			}
			writeJSON(w, `{"code":0,"data":{"totalPage":1,"list":[{"folderId":"f1","folderName":"相册 &amp; 视频"}]}}`)
		case "/proved/record/file/list":
			writeJSON(w, `{"code":0,"data":{"totalPage":1,"list":[
				{"fileId":"id1","fileName":"a.txt","fileSize":2,"updTime":"2026-01-01 10:00:00"},
				{"fileId":"x","fileType":2,"folderId":"f2","folderName":"子目录"}
			]}}`)
		default:
			t.Errorf("未预期的请求：%s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	})

	l, err := c.Listing(context.Background(), "0")
	if err != nil {
		t.Fatalf("列目录失败：%v", err)
	}
	if l.FolderID != "" {
		t.Errorf("根目录 FolderID = %q，期望空", l.FolderID)
	}
	if len(l.Folders) != 2 {
		t.Fatalf("目录数 = %d，期望 2：%+v", len(l.Folders), l.Folders)
	}
	if l.Folders[0].Name != "相册 & 视频" {
		t.Errorf("目录名未做 HTML 反转义：%q", l.Folders[0].Name)
	}
	if len(l.Files) != 1 {
		t.Fatalf("文件数 = %d，期望 1", len(l.Files))
	}
	if l.Files[0].Size != 2*1024 {
		t.Errorf("fileSize 应按 KB 换算，实际 %d", l.Files[0].Size)
	}
}

func TestUploadInstant(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/proved/7n/getUpToken" {
			t.Errorf("未预期的请求：%s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
			return
		}
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body["fileName"] != "dup.bin" {
			t.Errorf("fileName = %v", body["fileName"])
		}
		if body["type"] != float64(1) {
			t.Errorf("type = %v，期望 1", body["type"])
		}
		writeJSON(w, `{"code":0,"data":{"upToken":"-1","map":{"fileId":"inst-1"}}}`)
	})

	path := writeTempFile(t, "dup.bin", "hello")
	var sent, total int64
	res, err := c.Upload(context.Background(), path, "0", func(s, tt int64) {
		sent, total = s, tt
	})
	if err != nil {
		t.Fatalf("秒传失败：%v", err)
	}
	if !res.Instant || res.FileID != "inst-1" {
		t.Errorf("结果 = %+v，期望秒传 inst-1", res)
	}
	if sent != 5 || total != 5 {
		t.Errorf("进度 = %d/%d，期望 5/5", sent, total)
	}
}

func TestUploadFormSmallFile(t *testing.T) {
	var mu sync.Mutex
	var formToken, formKey, fileBody string

	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/proved/7n/getUpToken":
			writeJSON(w, `{"code":0,"data":{"upToken":"UP-TOKEN"}}`)
		case r.URL.Path == "/upload":
			if got := r.Header.Get("Authorization"); got != "" {
				t.Errorf("七牛表单上传不应带 Authorization，实际 %q", got)
			}
			if err := r.ParseMultipartForm(1 << 20); err != nil {
				t.Errorf("解析 multipart 失败：%v", err)
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			f, _, err := r.FormFile("file")
			if err != nil {
				t.Errorf("缺少 file 字段：%v", err)
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			defer f.Close()
			raw, _ := io.ReadAll(f)
			mu.Lock()
			formToken = r.FormValue("token")
			formKey = r.FormValue("key")
			fileBody = string(raw)
			mu.Unlock()
			writeJSON(w, `{"token":"QI-NIU-TOKEN"}`)
		case r.URL.Path == "/unproved/7n/results":
			if got := r.URL.Query().Get("tokenList"); got != "QI-NIU-TOKEN" {
				t.Errorf("tokenList = %q", got)
			}
			writeJSON(w, `{"code":0,"data":{"list":[{"fileName":"small.bin","fileId":"file-9","status":1}]}}`)
		default:
			t.Errorf("未预期的请求：%s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	})

	path := writeTempFile(t, "small.bin", "hello lanzou")
	res, err := c.Upload(context.Background(), path, "", nil)
	if err != nil {
		t.Fatalf("表单上传失败：%v", err)
	}
	if res.Instant || res.FileID != "file-9" {
		t.Errorf("结果 = %+v，期望普通上传 file-9", res)
	}
	mu.Lock()
	defer mu.Unlock()
	if formToken != "UP-TOKEN" {
		t.Errorf("表单 token = %q", formToken)
	}
	if !strings.Contains(formKey, "storage/files/") || !strings.HasSuffix(formKey, ".gz") {
		t.Errorf("对象键格式 = %q", formKey)
	}
	if fileBody != "hello lanzou" {
		t.Errorf("上传内容 = %q", fileBody)
	}
}

func TestUploadChunks(t *testing.T) {
	var mu sync.Mutex
	partsSeen := map[string]bool{}
	mergedBody := ""

	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/proved/7n/getUpToken":
			writeJSON(w, `{"code":0,"data":{"upToken":"UP-CHUNK"}}`)
		case strings.HasSuffix(r.URL.Path, "/uploads") && r.Method == http.MethodPost:
			if got := r.Header.Get("Authorization"); got != "UpToken UP-CHUNK" {
				t.Errorf("初始化 Authorization = %q", got)
			}
			writeJSON(w, `{"uploadId":"UPID"}`)
		case strings.Contains(r.URL.Path, "/uploads/UPID/") && r.Method == http.MethodPut:
			if got := r.Header.Get("Content-MD5"); got == "" {
				t.Errorf("分片缺少 Content-MD5")
			}
			partsSeen[r.URL.Path[strings.LastIndex(r.URL.Path, "/")+1:]] = true
			writeJSON(w, `{"etag":"etag-`+r.URL.Path[strings.LastIndex(r.URL.Path, "/")+1:]+`"}`)
		case strings.HasSuffix(r.URL.Path, "/uploads/UPID") && r.Method == http.MethodPost:
			raw, _ := io.ReadAll(r.Body)
			mu.Lock()
			mergedBody = string(raw)
			mu.Unlock()
			writeJSON(w, `{"token":"QI-CHUNK-TOKEN"}`)
		case r.URL.Path == "/unproved/7n/results":
			writeJSON(w, `{"code":0,"data":{"list":[{"fileName":"big.bin","fileId":"file-big","status":1}]}}`)
		default:
			t.Errorf("未预期的请求：%s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	})
	c.partSize = 4

	path := writeTempFile(t, "big.bin", "0123456789")
	res, err := c.Upload(context.Background(), path, "f1", nil)
	if err != nil {
		t.Fatalf("分片上传失败：%v", err)
	}
	if res.FileID != "file-big" {
		t.Errorf("结果 = %+v，期望 file-big", res)
	}

	mu.Lock()
	defer mu.Unlock()
	if len(partsSeen) != 3 {
		t.Errorf("分片数 = %d，期望 3：%v", len(partsSeen), partsSeen)
	}
	for _, n := range []string{"1", "2", "3"} {
		if !partsSeen[n] {
			t.Errorf("缺少分片 %s", n)
		}
	}
	for _, want := range []string{`"partNumber":1`, `"partNumber":2`, `"partNumber":3`, `"etag":"etag-1"`} {
		if !strings.Contains(mergedBody, want) {
			t.Errorf("合并请求缺少 %s：%s", want, mergedBody)
		}
	}
}

func TestWaitResultFailure(t *testing.T) {
	calls := 0
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/unproved/7n/results" {
			t.Errorf("未预期的请求：%s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
			return
		}
		calls++
		writeJSON(w, `{"code":0,"data":{"list":[{"fileName":"x.bin","status":0,"msg":"空间不足"}]}}`)
	})
	c.maxPoll = 2

	_, err := c.waitResult(context.Background(), "tok", "x.bin")
	if err == nil {
		t.Fatal("期望返回错误")
	}
	var apiErr *APIError
	if !strings.Contains(err.Error(), "空间不足") {
		t.Errorf("错误未带上服务端原因：%v", err)
	}
	if !strings.Contains(err.Error(), "蓝奏优享") {
		t.Errorf("错误缺少统一前缀：%v", err)
	}
	if !errors.As(err, &apiErr) {
		t.Errorf("错误类型 = %T，期望 *APIError", err)
	}
	if calls != 2 {
		t.Errorf("轮询次数 = %d，期望 2", calls)
	}
}
