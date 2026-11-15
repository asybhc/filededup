package lanzou

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"html"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

// ErrTooLarge 表示文件超过老版蓝奏云 100 MiB 上限，调用方应记为 skipped-large。
var ErrTooLarge = errors.New("文件超过 100 MiB")

// 老版蓝奏云的固定地址。测试里可以覆盖 Client 的字段，不需要真实网络。
const (
	loginPageURL = "https://accounts.woozooo.com/accounts.php?action=login&ref=up.woozooo.com"
	loginPostURL = "https://accounts.woozooo.com/accounts.php"
	apiBaseURL   = "https://pc.woozooo.com/doupload.php"
	uploadURL    = "https://pc.woozooo.com/html5up.php"
	userAgent    = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/147.0.0.0 Safari/537.36"
)

// Client 是蓝奏云老版客户端。零值不可用，必须用 NewClient 构造。
type Client struct {
	http *http.Client

	creds Credentials

	// 请求间隔（服务端限流），C# 版默认 1500ms，界面可在 1000–2000ms 调。
	interval time.Duration
	mu       sync.Mutex
	last     time.Time

	// 测试注入点，默认值见上方常量。
	loginPage string
	loginPost string
	apiBase   string
	uploadURL string
}

// NewClient 构造客户端。interval 传 0 时用 1500ms。
func NewClient(creds Credentials, interval time.Duration) *Client {
	if interval <= 0 {
		interval = 1500 * time.Millisecond
	}
	return &Client{
		http: &http.Client{
			// 重定向由调用方逐跳处理：登录成功后的中转页需要在每一跳收集 Set-Cookie。
			CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
		},
		creds:     creds,
		interval:  interval,
		loginPage: loginPageURL,
		loginPost: loginPostURL,
		apiBase:   apiBaseURL,
		uploadURL: uploadURL,
	}
}

// Credentials 返回当前登录态。
func (c *Client) Credentials() Credentials { return c.creds }

// SetCredentials 更新登录态（粘贴 Cookie 后复用同一个客户端）。
func (c *Client) SetCredentials(creds Credentials) { c.creds = creds }

// SetInterval 调整请求间隔。
func (c *Client) SetInterval(d time.Duration) {
	if d <= 0 {
		return
	}
	c.mu.Lock()
	c.interval = d
	c.mu.Unlock()
}

type httpResult struct {
	status   int
	header   http.Header
	body     []byte
	finalURL string
}

// send 发一次请求，自动带 UA / Cookie，并把 Set-Cookie 收进 jar。
func (c *Client) send(ctx context.Context, method, rawURL string, body io.Reader, contentType string, jar map[string]string, extraHeaders map[string]string) (*httpResult, error) {
	req, err := http.NewRequestWithContext(ctx, method, rawURL, body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Accept", "application/json, text/javascript, */*; q=0.01")
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	if cookie := c.creds.CookieHeader(); cookie != "" && jar == nil {
		// 业务接口直接用凭据里的 Cookie；登录流程用 jar 逐跳累积。
		req.Header.Set("Cookie", cookie)
	}
	if len(jar) > 0 {
		req.Header.Set("Cookie", jarHeader(jar))
	}
	for k, v := range extraHeaders {
		req.Header.Set(k, v)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if jar != nil {
		for _, ck := range resp.Header.Values("Set-Cookie") {
			name, value := parseSetCookie(ck)
			if name != "" {
				jar[name] = value
			}
		}
	}
	raw, err := readAll(resp.Body, 4<<20)
	if err != nil {
		return nil, err
	}
	return &httpResult{status: resp.StatusCode, header: resp.Header, body: raw, finalURL: rawURL}, nil
}

// throttle 保证两次业务请求之间至少间隔 interval，支持取消。
func (c *Client) throttle(ctx context.Context) error {
	c.mu.Lock()
	now := time.Now()
	wait := c.interval - now.Sub(c.last)
	if wait < 0 {
		wait = 0
	}
	c.last = now.Add(wait)
	c.mu.Unlock()
	if wait == 0 {
		return nil
	}
	t := time.NewTimer(wait)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

func jarHeader(jar map[string]string) string {
	parts := make([]string, 0, len(jar))
	for k, v := range jar {
		parts = append(parts, k+"="+v)
	}
	return strings.Join(parts, "; ")
}

func parseSetCookie(raw string) (string, string) {
	first := strings.SplitN(raw, ";", 2)[0]
	kv := strings.SplitN(strings.TrimSpace(first), "=", 2)
	if len(kv) != 2 {
		return "", ""
	}
	return strings.TrimSpace(kv[0]), strings.TrimSpace(kv[1])
}

var (
	arg1Pattern = regexp.MustCompile(`arg1\s*=\s*'([0-9A-Fa-f]{40})'`)
	metaPattern = regexp.MustCompile(`(?is)<meta[^>]+http-equiv=["']?refresh["']?[^>]+content=["'][^"']*url=([^"';]+)`)
	jsonZT      = regexp.MustCompile(`"zt"\s*:\s*"?(-?\d+)"?`)
)

// Login 用账号密码登录，返回可持久化的 Cookie 凭据。
//
// 流程与 C# 版一致：
//  1. GET 登录页；若命中 acw_sc__v2 JS 挑战，本地算出 Cookie 后重试；
//  2. POST /accounts.php（task=uselogin），成功时服务端返回 msgs=中转页地址；
//  3. 逐跳跟随中转页（Location 或 meta refresh），收集 ylogin / phpdisk_info。
func (c *Client) Login(ctx context.Context, username, password string) (Credentials, error) {
	username = strings.TrimSpace(username)
	if username == "" || password == "" {
		return Credentials{}, &APIError{Op: "登录", Detail: "账号或密码为空"}
	}

	jar := map[string]string{}
	for i := 0; i < 3; i++ {
		res, err := c.send(ctx, http.MethodGet, c.loginPage, nil, "", jar, nil)
		if err != nil {
			return Credentials{}, &APIError{Op: "登录", Detail: err.Error(), Hint: "检查网络或代理设置"}
		}
		if res.status != http.StatusOK {
			return Credentials{}, &APIError{Op: "登录", Status: res.status, Detail: truncate(string(res.body), 200)}
		}
		arg1 := ""
		if m := arg1Pattern.FindSubmatch(res.body); m != nil {
			arg1 = string(m[1])
		}
		if arg1 == "" {
			break
		}
		solved, err := SolveACWChallenge(arg1)
		if err != nil {
			return Credentials{}, &APIError{Op: "登录", Detail: "反爬挑战解析失败：" + err.Error()}
		}
		jar["acw_sc__v2"] = solved
	}

	form := url.Values{
		"task":     {"uselogin"},
		"username": {username},
		"password": {password},
		"ref":      {""},
	}
	res, err := c.send(ctx, http.MethodPost, c.loginPost, strings.NewReader(form.Encode()),
		"application/x-www-form-urlencoded; charset=UTF-8", jar, map[string]string{
			"X-Requested-With": "XMLHttpRequest",
			"Referer":          c.loginPage,
		})
	if err != nil {
		return Credentials{}, &APIError{Op: "登录", Detail: err.Error()}
	}
	var lr struct {
		ZT      json.RawMessage `json:"zt"`
		Msgs    string          `json:"msgs"`
		Message string          `json:"info"`
	}
	_ = json.Unmarshal(res.body, &lr)
	if ztValue(lr.ZT) != 1 {
		detail := truncate(string(res.body), 300)
		hint := ""
		if s := strings.TrimSpace(lr.Msgs); s != "" {
			detail = s
		}
		if strings.Contains(detail, "密码") || strings.Contains(detail, "用户名") {
			hint = "请确认账号密码；也可以改用粘贴 Cookie"
		}
		return Credentials{}, &APIError{Op: "登录", Status: res.status, Detail: detail, Hint: hint}
	}

	next := ResolveURL(c.loginPost, lr.Msgs)
	for hop := 0; hop < 10 && next != ""; hop++ {
		r, err := c.send(ctx, http.MethodGet, next, nil, "", jar, nil)
		if err != nil {
			return Credentials{}, &APIError{Op: "登录", Detail: err.Error()}
		}
		if loc := r.header.Get("Location"); loc != "" {
			next = ResolveURL(next, loc)
			continue
		}
		if m := metaPattern.FindSubmatch(r.body); m != nil {
			next = ResolveURL(next, string(m[1]))
			continue
		}
		break
	}

	creds := Credentials{
		Platform:    PlatformLanzou,
		Ylogin:      jar["ylogin"],
		PhpdiskInfo: jar["phpdisk_info"],
		SavedAt:     time.Now(),
	}
	if !creds.Valid() {
		return Credentials{}, &APIError{
			Op:     "登录",
			Status: res.status,
			Detail: "服务端未下发 ylogin / phpdisk_info",
			Hint:   "账号密码登录未联调，服务端可能增加了额外校验；请在浏览器登录后复制 Cookie 粘贴进来",
		}
	}
	return creds, nil
}

// Validate 用列目录验证 Cookie 是否仍有效。
func (c *Client) Validate(ctx context.Context) error {
	if !c.creds.Valid() {
		return &APIError{Op: "校验 Cookie", Detail: "缺少 ylogin 或 phpdisk_info"}
	}
	_, err := c.listFolders(ctx, "0")
	return err
}

// Listing 拉取目录下的文件夹与文件。
func (c *Client) Listing(ctx context.Context, folderID string) (Listing, error) {
	if !c.creds.Valid() {
		return Listing{}, &APIError{Op: "列目录", Detail: "未登录"}
	}
	folderID = normalizeFolderID(folderID)
	folders, err := c.listFolders(ctx, folderID)
	if err != nil {
		return Listing{}, err
	}
	files, err := c.listFiles(ctx, folderID)
	if err != nil {
		return Listing{}, err
	}
	return Listing{FolderID: folderID, Folders: folders, Files: files}, nil
}

// rootFolderID 是老版蓝奏云的根目录标识。实测 folder_id=0 或缺失时
// task=47 返回 {"zt":2}，只有 -1 能列出根目录。
const rootFolderID = "-1"

// normalizeFolderID 把界面上的 "0"（根目录）与空值统一成服务端认识的 -1。
func normalizeFolderID(id string) string {
	id = strings.TrimSpace(id)
	if id == "" || id == "0" {
		return rootFolderID
	}
	return id
}

func (c *Client) listFolders(ctx context.Context, folderID string) ([]Folder, error) {
	body, err := c.postTaskAllowEmpty(ctx, "47", url.Values{"folder_id": {normalizeFolderID(folderID)}}, true)
	if err != nil {
		return nil, err
	}
	items, err := decodeTextArray(body)
	if err != nil {
		return nil, err
	}
	out := make([]Folder, 0, len(items))
	for _, raw := range items {
		var w struct {
			FolderID   string `json:"folder_id"`
			FolID      string `json:"fol_id"`
			FolderName string `json:"folder_name"`
			Name       string `json:"name"`
		}
		if err := json.Unmarshal(raw, &w); err != nil {
			continue
		}
		id := w.FolderID
		if id == "" {
			// 实测返回字段是 fol_id，folder_id 只在部分接口里出现。
			id = w.FolID
		}
		name := w.FolderName
		if name == "" {
			name = w.Name
		}
		out = append(out, Folder{ID: id, Name: htmlUnescape(name)})
	}
	return out, nil
}

func (c *Client) listFiles(ctx context.Context, folderID string) ([]File, error) {
	folderID = normalizeFolderID(folderID)
	var out []File
	seen := make(map[string]bool)
	prevFirst := ""
	for page := 1; page <= 200; page++ {
		body, err := c.postTask(ctx, "5", url.Values{
			"folder_id": {folderID},
			"page":      {strconv.Itoa(page)},
		})
		if err != nil {
			return nil, err
		}
		items, err := decodeTextArray(body)
		if err != nil {
			return nil, err
		}
		firstID := ""
		pageFiles := make([]File, 0, len(items))
		for _, raw := range items {
			var w struct {
				ID      string `json:"id"`
				NameAll string `json:"name_all"`
				Name    string `json:"name"`
				Size    string `json:"size"`
				Time    string `json:"time"`
			}
			if err := json.Unmarshal(raw, &w); err != nil {
				continue
			}
			if firstID == "" {
				firstID = w.ID
			}
			if w.ID != "" && seen[w.ID] {
				continue
			}
			name := w.NameAll
			if name == "" {
				name = w.Name
			}
			pageFiles = append(pageFiles, File{
				ID:       w.ID,
				Name:     htmlUnescape(name),
				Size:     ParseSizeText(w.Size),
				SizeText: w.Size,
				Time:     w.Time,
			})
		}
		// 实测根目录 page=1/2/3 返回同一页：服务端忽略 page 时要停下，
		// 否则会重复 200 次，光限流就要 5 分钟。
		if page > 1 && firstID != "" && firstID == prevFirst {
			break
		}
		prevFirst = firstID
		for _, f := range pageFiles {
			if f.ID != "" {
				seen[f.ID] = true
			}
			out = append(out, f)
		}
		// 实测 info 是总页数（只有一页时为 1）。
		if n := totalPages(body); n > 0 && page >= n {
			break
		}
		if len(items) < filePageSize {
			break
		}
	}
	return out, nil
}

// filePageSize 是老版蓝奏云单页文件数（实测 18）。
const filePageSize = 18

// totalPages 解析响应里的 info 字段；实测文件列表接口的 info 是总页数。
func totalPages(body []byte) int {
	var env struct {
		Info json.RawMessage `json:"info"`
	}
	if json.Unmarshal(body, &env) != nil || len(env.Info) == 0 {
		return 0
	}
	var n int
	if json.Unmarshal(env.Info, &n) == nil {
		return n
	}
	var s string
	if json.Unmarshal(env.Info, &s) == nil {
		if v, err := strconv.Atoi(strings.TrimSpace(s)); err == nil {
			return v
		}
	}
	return 0
}

// isEmptyFolderResponse 判断 task=47 的响应是不是「空子目录」。
// 实测空子目录返回 {"zt":2,"info":[{...目录自身信息...}],"text":[]}，
// 与登录失效（zt=9）和参数错误（zt=2 且 info 为空）不同。
func isEmptyFolderResponse(body []byte) bool {
	var env struct {
		Info json.RawMessage `json:"info"`
		Text json.RawMessage `json:"text"`
	}
	if json.Unmarshal(body, &env) != nil {
		return false
	}
	text := strings.TrimSpace(string(env.Text))
	if text != "[]" && text != "null" && text != "" {
		return false
	}
	var infos []json.RawMessage
	if json.Unmarshal(env.Info, &infos) != nil {
		return false
	}
	return len(infos) > 0
}

// postTask 调用 doupload.php，task 与参数都放在表单里；uid 放查询串（与 C# 版一致）。
func (c *Client) postTask(ctx context.Context, task string, values url.Values) ([]byte, error) {
	return c.postTaskAllowEmpty(ctx, task, values, false)
}

// postTaskAllowEmpty 在 allowEmpty 为真时把「空子目录」的 zt=2 当作成功返回。
func (c *Client) postTaskAllowEmpty(ctx context.Context, task string, values url.Values, allowEmpty bool) ([]byte, error) {
	if err := c.throttle(ctx); err != nil {
		return nil, &APIError{Op: "列目录", Detail: err.Error()}
	}
	values.Set("task", task)
	u := c.apiBase + "?uid=" + url.QueryEscape(c.creds.Ylogin)
	res, err := c.send(ctx, http.MethodPost, u, strings.NewReader(values.Encode()),
		"application/x-www-form-urlencoded; charset=UTF-8", nil, map[string]string{
			"X-Requested-With": "XMLHttpRequest",
			"Referer":          "https://pc.woozooo.com/mydisk.php",
		})
	if err != nil {
		return nil, &APIError{Op: "列目录", Detail: err.Error()}
	}
	if res.status != http.StatusOK {
		return nil, &APIError{Op: "列目录", Status: res.status, Detail: truncate(string(res.body), 200)}
	}
	if zt := ztOf(res.body); zt != 1 {
		if allowEmpty && zt == 2 && isEmptyFolderResponse(res.body) {
			return res.body, nil
		}
		e := &APIError{Op: "列目录", Status: res.status, Detail: truncate(string(res.body), 300)}
		if zt == 9 {
			e.Hint = "登录态已失效，请重新登录或粘贴新的 Cookie"
		}
		return nil, e
	}
	return res.body, nil
}

// UploadResult 是一次上传的服务端返回。
type UploadResult struct {
	FileID  string
	Instant bool
}

// Upload 上传单个文件。progress 可为 nil。
func (c *Client) Upload(ctx context.Context, localPath, folderID string, progress func(sent, total int64)) (UploadResult, error) {
	if !c.creds.Valid() {
		return UploadResult{}, &APIError{Op: "上传", Detail: "未登录"}
	}
	info, err := os.Stat(localPath)
	if err != nil {
		return UploadResult{}, &APIError{Op: "上传", Detail: err.Error()}
	}
	if info.IsDir() {
		return UploadResult{}, &APIError{Op: "上传", Detail: "目标是目录"}
	}
	if info.Size() > MaxUploadSize {
		return UploadResult{}, ErrTooLarge
	}
	if strings.TrimSpace(folderID) == "" {
		folderID = rootFolderID
	}
	name := filepath.Base(localPath)

	var lastErr error
	for attempt := 1; attempt <= 3; attempt++ {
		if err := c.throttle(ctx); err != nil {
			return UploadResult{}, &APIError{Op: "上传", Detail: err.Error()}
		}
		res, err := c.uploadOnce(ctx, localPath, name, folderID, info, progress)
		if err == nil {
			return res, nil
		}
		if ctx.Err() != nil {
			return UploadResult{}, &APIError{Op: "上传", Detail: "已取消"}
		}
		var apiErr *APIError
		if errors.As(err, &apiErr) {
			if apiErr.Status == http.StatusTooManyRequests {
				return UploadResult{}, apiErr
			}
			if apiErr.Status > 0 && apiErr.Status < 500 {
				return UploadResult{}, apiErr
			}
		}
		lastErr = err
		select {
		case <-ctx.Done():
			return UploadResult{}, &APIError{Op: "上传", Detail: "已取消"}
		case <-time.After(2 * time.Second):
		}
	}
	if lastErr == nil {
		lastErr = errors.New("未知错误")
	}
	return UploadResult{}, lastErr
}

func (c *Client) uploadOnce(ctx context.Context, localPath, name, folderID string, info os.FileInfo, progress func(sent, total int64)) (UploadResult, error) {
	pr, pw := io.Pipe()
	mw := multipart.NewWriter(pw)
	go func() {
		var werr error
		defer func() {
			mw.Close()
			pw.CloseWithError(werr)
		}()
		// 实测：html5up.php 的目标目录字段是 folder_id，文件部件必须叫 upload_file。
		// 用 folder_id_bb_n 不会报错，但文件会落到根目录；文件部件叫 file 则返回「请选择需要上传的文件」。
		fields := [][2]string{
			{"task", "1"},
			{"id", "WU_FILE_0"},
			{"folder_id", folderID},
			{"name", name},
			{"type", "application/octet-stream"},
		}
		for _, f := range fields {
			if werr = mw.WriteField(f[0], f[1]); werr != nil {
				return
			}
		}
		var part io.Writer
		part, werr = mw.CreateFormFile("upload_file", name)
		if werr != nil {
			return
		}
		f, err := os.Open(localPath)
		if err != nil {
			werr = err
			return
		}
		defer f.Close()
		_, werr = io.Copy(part, &progressReader{r: f, ctx: ctx, total: info.Size(), progress: progress})
	}()

	res, err := c.send(ctx, http.MethodPost, c.uploadURL, pr, mw.FormDataContentType(), nil, map[string]string{
		"Referer": "https://pc.woozooo.com/mydisk.php",
	})
	if err != nil {
		return UploadResult{}, &APIError{Op: "上传", Detail: err.Error()}
	}
	var ur struct {
		ZT   json.RawMessage `json:"zt"`
		Info string          `json:"info"`
		Text json.RawMessage `json:"text"`
	}
	_ = json.Unmarshal(res.body, &ur)
	if ztValue(ur.ZT) != 1 {
		e := &APIError{Op: "上传", Status: res.status, Detail: truncate(string(res.body), 300)}
		if res.status == http.StatusTooManyRequests || strings.Contains(e.Detail, "频繁") {
			e.Status = http.StatusTooManyRequests
			e.Hint = "请求过快，调大请求间隔后重试"
		}
		return UploadResult{}, e
	}
	out := UploadResult{Instant: strings.Contains(ur.Info, "秒传")}
	for _, raw := range decodeTextItems(ur.Text) {
		var w struct {
			ID       string `json:"id"`
			FolderID string `json:"folder_id"`
		}
		if json.Unmarshal(raw, &w) != nil {
			continue
		}
		if w.ID != "" {
			out.FileID = w.ID
		} else if w.FolderID != "" {
			out.FileID = w.FolderID
		}
		if out.FileID != "" {
			break
		}
	}
	return out, nil
}

type progressReader struct {
	r        io.Reader
	ctx      context.Context
	sent     int64
	total    int64
	progress func(sent, total int64)
}

func (p *progressReader) Read(b []byte) (int, error) {
	if err := p.ctx.Err(); err != nil {
		return 0, err
	}
	n, err := p.r.Read(b)
	p.sent += int64(n)
	if p.progress != nil {
		p.progress(p.sent, p.total)
	}
	return n, err
}

func ztOf(body []byte) int {
	m := jsonZT.FindSubmatch(body)
	if m == nil {
		return -999
	}
	v, _ := strconv.Atoi(string(m[1]))
	return v
}

func ztValue(raw json.RawMessage) int {
	s := strings.TrimSpace(strings.Trim(string(raw), `"`))
	if s == "" || s == "null" {
		return -999
	}
	v, err := strconv.Atoi(s)
	if err != nil {
		return -999
	}
	return v
}

// decodeTextArray 把服务端返回的 text（数组或单个对象）解成原始 JSON 列表。
func decodeTextArray(body []byte) ([]json.RawMessage, error) {
	var wrap struct {
		Text json.RawMessage `json:"text"`
	}
	if err := json.Unmarshal(body, &wrap); err != nil {
		return nil, &APIError{Op: "列目录", Detail: "响应不是 JSON：" + truncate(string(body), 200)}
	}
	return decodeTextItems(wrap.Text), nil
}

func decodeTextItems(raw json.RawMessage) []json.RawMessage {
	s := bytes.TrimSpace(raw)
	if len(s) == 0 || string(s) == "null" {
		return nil
	}
	if s[0] == '[' {
		var arr []json.RawMessage
		if json.Unmarshal(s, &arr) != nil {
			return nil
		}
		return arr
	}
	if s[0] == '{' {
		return []json.RawMessage{json.RawMessage(s)}
	}
	return nil
}

func htmlUnescape(s string) string {
	return strings.TrimSpace(html.UnescapeString(s))
}
