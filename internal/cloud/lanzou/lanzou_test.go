package lanzou

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestSolveACWChallenge(t *testing.T) {
	cases := map[string]string{
		"8B6EE7AA04C9355CFF87BD390449FBF470A90B75": "6af96beb400a34d505697e296b47d2aed9e903ec",
		"D2D3AA54ACB2115C4A588709AE19F76473ADC80E": "6af96bb8cad38176a7be0bd7284d4e536d593f58",
	}
	for arg1, want := range cases {
		got, err := SolveACWChallenge(arg1)
		if err != nil {
			t.Fatalf("SolveACWChallenge(%q): %v", arg1, err)
		}
		if got != want {
			t.Errorf("SolveACWChallenge(%q)=%q，期望 %q", arg1, got, want)
		}
	}
	if _, err := SolveACWChallenge("abc"); err == nil {
		t.Error("非 40 位应报错")
	}
	if _, err := SolveACWChallenge(strings.Repeat("z", 40)); err == nil {
		t.Error("非十六进制应报错")
	}
}

func TestSizeWithinTolerance(t *testing.T) {
	cases := []struct {
		local, remote int64
		want          bool
	}{
		{1000, 3048, true},   // 差 2048，恰好等于下限
		{1000, 3049, false},  // 差 2049，超出
		{1000, -1048, false}, // 另一侧同样按 2048
		{0, 2048, true},      // 本地 0，1% 为 0，取下限 2048
		{0, 2049, false},
		{1_000_000, 1_010_000, true},  // 1% 边界
		{1_000_000, 1_010_001, false}, // 超出 1%
		{1_000_000, 990_000, true},
		{1_000_000, 989_999, false},
		{-1, 100, false}, // 云端大小未知
		{100, -1, false}, // 本地大小未知
	}
	for _, c := range cases {
		if got := SizeWithinTolerance(c.local, c.remote); got != c.want {
			t.Errorf("SizeWithinTolerance(%d,%d)=%v，期望 %v", c.local, c.remote, got, c.want)
		}
	}
}

func TestSameNameAndFindDuplicate(t *testing.T) {
	if !SameName("  Report.PDF ", "report.pdf") {
		t.Error("Trim + 忽略大小写后应相等")
	}
	if SameName("a", "a ") == false {
		t.Error("尾部空白应被忽略")
	}
	if SameName("报告.doc", "报告.doc ") == false {
		t.Error("中文名字 + 空白应相等")
	}
	if SameName("a.txt", "b.txt") {
		t.Error("不同名字不应相等")
	}

	files := []File{
		{ID: "1", Name: "a&amp;b.mp4", Size: 1000},
		{ID: "2", Name: "Report.PDF", Size: 1_000_000},
	}
	f, ok := FindDuplicate("a&b.mp4", 2500, files)
	if !ok || f.ID != "1" {
		t.Errorf("HTML 实体 + 容差内应命中，实际 %+v %v", f, ok)
	}
	if _, ok := FindDuplicate("report.pdf", 1_020_000, files); ok {
		t.Error("超出 1% 不应命中")
	}
	if _, ok := FindDuplicate("missing.pdf", 1, files); ok {
		t.Error("不同名字不应命中")
	}
}

func TestParseSizeText(t *testing.T) {
	cases := map[string]int64{
		"1.2 M": 1258291,
		"345 K": 353280,
		"12":    12,
		"1.5 G": 1610612736,
		"2 MB":  2097152,
		"0":     0,
		"":      -1,
		"abc":   -1,
		"-1 K":  -1,
		"1.2 X": -1,
	}
	for in, want := range cases {
		if got := ParseSizeText(in); got != want {
			t.Errorf("ParseSizeText(%q)=%d，期望 %d", in, got, want)
		}
	}
}

func TestParseCookieHeaderAndMask(t *testing.T) {
	creds, err := ParseCookieHeader("Cookie: acw_tc=1; ylogin=1234567; phpdisk_info=abc%3D; foo=bar")
	if err != nil {
		t.Fatal(err)
	}
	if creds.Ylogin != "1234567" || creds.PhpdiskInfo != "abc%3D" {
		t.Errorf("解析结果不对：%+v", creds)
	}
	if creds.MaskedAccount() != "****4567" {
		t.Errorf("掩码应为后四位，实际 %q", creds.MaskedAccount())
	}
	if _, err := ParseCookieHeader("ylogin=1"); err == nil {
		t.Error("缺少 phpdisk_info 应报错")
	}
	if (Credentials{Ylogin: "12"}).MaskedAccount() != "****" {
		t.Error("长度 <= 4 时应全部打码")
	}
}

func newTestClient(t *testing.T, handler http.HandlerFunc) (*Client, *httptest.Server) {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	c := NewClient(Credentials{Platform: PlatformLanzou, Ylogin: "123456", PhpdiskInfo: "pd"}, time.Millisecond)
	c.loginPage = srv.URL + "/login"
	c.loginPost = srv.URL + "/accounts.php"
	c.apiBase = srv.URL + "/doupload.php"
	c.uploadURL = srv.URL + "/html5up.php"
	return c, srv
}

func TestLoginFlow(t *testing.T) {
	const arg1 = "8B6EE7AA04C9355CFF87BD390449FBF470A90B75"
	solved, _ := SolveACWChallenge(arg1)
	var posted atomic.Bool

	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/login":
			if ck, err := r.Cookie("acw_sc__v2"); err != nil || ck.Value != solved {
				http.SetCookie(w, &http.Cookie{Name: "acw_tc", Value: "t1", Path: "/"})
				_, _ = io.WriteString(w, "var arg1='"+arg1+"';")
				return
			}
			_, _ = io.WriteString(w, "<html>login form</html>")
		case "/accounts.php":
			if err := r.ParseForm(); err != nil {
				t.Errorf("ParseForm: %v", err)
			}
			if r.PostForm.Get("task") != "uselogin" || r.PostForm.Get("username") != "alice" || r.PostForm.Get("password") != "secret" {
				t.Errorf("登录表单不对：%v", r.PostForm)
			}
			posted.Store(true)
			_, _ = io.WriteString(w, `{"zt":1,"msgs":"/jump"}`)
		case "/jump":
			http.SetCookie(w, &http.Cookie{Name: "ylogin", Value: "7654321", Path: "/"})
			http.SetCookie(w, &http.Cookie{Name: "phpdisk_info", Value: "pd-value", Path: "/"})
			_, _ = io.WriteString(w, "ok")
		default:
			http.NotFound(w, r)
		}
	})

	creds, err := c.Login(context.Background(), "alice", "secret")
	if err != nil {
		t.Fatalf("Login: %v", err)
	}
	if !posted.Load() {
		t.Error("没有发出登录 POST")
	}
	if creds.Ylogin != "7654321" || creds.PhpdiskInfo != "pd-value" {
		t.Errorf("登录态不对：%+v", creds)
	}
}

func TestLoginReportsServerMessage(t *testing.T) {
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/login" {
			_, _ = io.WriteString(w, "<html>login form</html>")
			return
		}
		_, _ = io.WriteString(w, `{"zt":0,"msgs":"用户名不正确","usename":""}`)
	})
	_, err := c.Login(context.Background(), "alice", "bad")
	if err == nil {
		t.Fatal("应返回错误")
	}
	if !strings.Contains(err.Error(), "用户名不正确") {
		t.Errorf("错误信息应原样包含服务端文案，实际：%v", err)
	}
}

func TestListing(t *testing.T) {
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			t.Errorf("ParseForm: %v", err)
		}
		if r.Header.Get("Cookie") != "ylogin=123456; phpdisk_info=pd" {
			t.Errorf("Cookie 头不对：%q", r.Header.Get("Cookie"))
		}
		switch r.PostForm.Get("task") {
		case "47":
			_, _ = io.WriteString(w, `{"zt":1,"info":"ok","text":[{"folder_id":"-1","folder_name":"根目录"},{"folder_id":"9","folder_name":"A&amp;B"}]}`)
		case "5":
			if r.PostForm.Get("page") == "1" {
				_, _ = io.WriteString(w, `{"zt":1,"info":"ok","text":[{"id":"f1","name_all":"a&amp;b.mp4","size":"1.2 M","time":"2026-01-01"},{"id":"f2","name_all":"c.txt","size":"10"}]}`)
				return
			}
			_, _ = io.WriteString(w, `{"zt":1,"info":"ok","text":[]}`)
		default:
			t.Errorf("未知 task: %q", r.PostForm.Get("task"))
		}
	})

	list, err := c.Listing(context.Background(), "0")
	if err != nil {
		t.Fatalf("Listing: %v", err)
	}
	if len(list.Folders) != 2 || list.Folders[1].Name != "A&B" {
		t.Errorf("文件夹解析不对：%+v", list.Folders)
	}
	if len(list.Files) != 2 || list.Files[0].Size != 1258291 || list.Files[0].Name != "a&b.mp4" {
		t.Errorf("文件解析不对：%+v", list.Files)
	}
}

func TestListingReportsExpiredLogin(t *testing.T) {
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"zt":9,"info":"login not"}`)
	})
	_, err := c.Listing(context.Background(), "0")
	if err == nil || !strings.Contains(err.Error(), "login not") {
		t.Fatalf("应原样返回服务端错误，实际：%v", err)
	}
}

func TestUpload(t *testing.T) {
	var gotTask, gotFolder, gotName, gotContent atomic.Value
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseMultipartForm(8 << 20); err != nil {
			t.Errorf("ParseMultipartForm: %v", err)
			http.Error(w, "bad multipart", 400)
			return
		}
		gotTask.Store(r.PostFormValue("task"))
		gotFolder.Store(r.PostFormValue("folder_id"))
		gotName.Store(r.PostFormValue("name"))
		f, _, err := r.FormFile("file")
		if err != nil {
			t.Errorf("FormFile: %v", err)
			return
		}
		defer f.Close()
		data, _ := io.ReadAll(f)
		gotContent.Store(string(data))
		_, _ = io.WriteString(w, `{"zt":1,"info":"上传成功","text":[{"id":"remote-1"}]}`)
	})

	dir := t.TempDir()
	path := filepath.Join(dir, "报告.txt")
	if err := os.WriteFile(path, []byte("hello lanzou"), 0o644); err != nil {
		t.Fatal(err)
	}
	var lastSent int64
	res, err := c.Upload(context.Background(), path, "9", func(sent, total int64) {
		lastSent = sent
		if sent > total {
			t.Errorf("进度超过总量：%d/%d", sent, total)
		}
	})
	if err != nil {
		t.Fatalf("Upload: %v", err)
	}
	if gotTask.Load() != "1" || gotFolder.Load() != "9" || gotName.Load() != "报告.txt" {
		t.Errorf("表单字段不对：task=%v folder=%v name=%v", gotTask.Load(), gotFolder.Load(), gotName.Load())
	}
	if gotContent.Load() != "hello lanzou" {
		t.Errorf("文件内容不对：%q", gotContent.Load())
	}
	if lastSent != int64(len("hello lanzou")) || res.FileID != "remote-1" {
		t.Errorf("进度或返回 ID 不对：sent=%d res=%+v", lastSent, res)
	}
}

func TestUploadTooLarge(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "big.bin")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.Truncate(MaxUploadSize + 1); err != nil {
		f.Close()
		t.Skipf("无法创建大文件，跳过：%v", err)
	}
	f.Close()

	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		t.Error("超过 100 MiB 不应发起请求")
	})
	if _, err := c.Upload(context.Background(), path, "0", nil); err != ErrTooLarge {
		t.Fatalf("应返回 ErrTooLarge，实际 %v", err)
	}
}

func TestWriteReport(t *testing.T) {
	dir := t.TempDir()
	items := []Item{
		{LocalPath: `C:\a\b.txt`, Name: "b.txt", Size: 10, Kind: KindSuccess, RemoteID: "r1"},
		{LocalPath: `C:\a\c,d.txt`, Name: "c,d.txt", Size: 20, Kind: KindDuplicate, DuplicateOf: "c,d.txt"},
	}
	var sum Summary
	sum.Add(KindSuccess)
	sum.Add(KindDuplicate)
	r := BuildReport("9", "备份", dir, items, sum)
	jsonPath, csvPath, err := WriteReport(dir, r, time.Date(2026, 11, 15, 10, 0, 0, 0, time.Local))
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(jsonPath)
	if err != nil {
		t.Fatal(err)
	}
	var back Report
	if err := json.Unmarshal(raw, &back); err != nil {
		t.Fatalf("报告不是合法 JSON：%v", err)
	}
	if back.Summary.Total != 2 || len(back.Items) != 2 || back.Kind != "filededup.lanzou.upload-report" {
		t.Errorf("报告内容不对：%+v", back)
	}
	csvRaw, err := os.ReadFile(csvPath)
	if err != nil {
		t.Fatal(err)
	}
	text := string(csvRaw)
	if !strings.HasPrefix(text, "\uFEFF") {
		t.Error("CSV 应带 BOM")
	}
	if !strings.Contains(text, `"C:\a\c,d.txt"`) {
		t.Errorf("含逗号的字段应加引号：%s", text)
	}
}

func TestUploadCacheKey(t *testing.T) {
	c := &UploadCache{Entries: map[string]CacheEntry{}}
	a := c.Key("9", `C:\data\x.txt`, 10, 100)
	b := c.Key("9", `C:\data\.\x.txt`, 10, 100)
	if a != b {
		t.Errorf("同一文件不同写法应同键：%q vs %q", a, b)
	}
	if a == c.Key("10", `C:\data\x.txt`, 10, 100) {
		t.Error("不同目录应不同键")
	}
	if a == c.Key("9", `C:\data\x.txt`, 10, 101) {
		t.Error("修改时间不同应不同键")
	}
}
