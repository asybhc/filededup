package lanzouyou

import (
	"bytes"
	"context"
	"crypto/md5"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"mime/multipart"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"filededup/internal/cloud/lanzou"
)

const (
	baseAPI      = "https://api.ilanzou.com"
	uploadRoot   = "https://upload.qiniup.com/"
	uploadBucket = "https://upload.qiniup.com/buckets/wpanstore-lanzou/objects/"
	userAgent    = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/78.0.3904.70 Safari/537.36"
	acceptHeader = "application/json, text/plain, */*"
	acceptLang   = "zh-CN,zh;q=0.9"
	originHeader = "https://www.ilanzou.com"
	refererValue = "https://www.ilanzou.com/"

	pageSize       = 60
	maxPages       = 200
	defaultPart    = 8 << 20
	defaultPollGap = time.Second
	maxPoll        = 600
)

// APIError 是蓝奏优享接口错误，错误文案可直接展示给用户。
type APIError struct {
	Op     string
	Status int
	Detail string
	Hint   string
}

func (e *APIError) Error() string {
	var b strings.Builder
	b.WriteString("蓝奏优享")
	if e.Op != "" {
		b.WriteString(e.Op)
	}
	b.WriteString("失败")
	if e.Status != 0 {
		fmt.Fprintf(&b, "（code=%d）", e.Status)
	}
	if e.Detail != "" {
		b.WriteString("：")
		b.WriteString(e.Detail)
	}
	if e.Hint != "" {
		b.WriteString(" ")
		b.WriteString(e.Hint)
	}
	return b.String()
}

// Client 是蓝奏优享客户端。零值不可用，请用 NewClient。
type Client struct {
	http *http.Client

	creds Credentials

	mu       sync.Mutex
	interval time.Duration
	last     time.Time

	base         string
	uploadRoot   string
	uploadBucket string
	partSize     int64
	pollGap      time.Duration
	maxPoll      int
}

// NewClient 构造客户端。interval 传 0 时用 1000ms。
func NewClient(creds Credentials, interval time.Duration) *Client {
	if interval <= 0 {
		interval = time.Second
	}
	if creds.Platform == "" {
		creds.Platform = lanzou.PlatformYouxiang
	}
	return &Client{
		http:         &http.Client{Timeout: 30 * time.Minute},
		creds:        creds,
		interval:     interval,
		base:         baseAPI,
		uploadRoot:   uploadRoot,
		uploadBucket: uploadBucket,
		partSize:     defaultPart,
		pollGap:      defaultPollGap,
		maxPoll:      maxPoll,
	}
}

// Credentials 返回当前登录态。
func (c *Client) Credentials() Credentials { return c.creds }

// SetCredentials 更新登录态。
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

// MaxUploadSize 返回单文件上限；0 表示不设本地上限（优享按分片上传）。
func (c *Client) MaxUploadSize() int64 { return 0 }

// Login 用账号密码换取 appToken。成功后会顺带读取账号信息；读取失败不影响登录。
func (c *Client) Login(ctx context.Context, username, password string) (Credentials, error) {
	username = strings.TrimSpace(username)
	if username == "" || password == "" {
		return Credentials{}, &APIError{Op: "登录", Detail: "账号和密码不能为空"}
	}
	uuid := newUUID()
	creds := Credentials{Platform: lanzou.PlatformYouxiang, Uuid: uuid, Extra: "2"}
	c.creds = creds

	payload, err := json.Marshal(map[string]string{"loginName": username, "loginPwd": password})
	if err != nil {
		return Credentials{}, err
	}
	ts := strconv.FormatInt(time.Now().UnixMilli(), 10)
	path := "/unproved/login?uuid=" + url.QueryEscape(uuid) +
		"&devType=6&devCode=" + url.QueryEscape(uuid) +
		"&devModel=chrome&devVersion=131&appVersion=&timestamp=" + ts + "&extra=2"
	raw, err := c.doBytes(ctx, "登录", http.MethodPost, c.base+path, payload, "application/json")
	if err != nil {
		return Credentials{}, err
	}
	data, err := checkEnvelope(raw, "登录")
	if err != nil {
		return Credentials{}, err
	}
	token := strField(data, "appToken")
	if token == "" {
		token = strField(mustObject(raw), "appToken")
	}
	if token == "" {
		return Credentials{}, &APIError{Op: "登录", Detail: "登录响应里没有 appToken", Hint: "请确认账号密码，或稍后重试"}
	}
	creds.AppToken = token
	c.creds = creds

	if info, ierr := c.Account(ctx); ierr == nil {
		creds.UID = info.UID
		creds.Account = info.Account
		c.creds = creds
	}
	return creds, nil
}

// AccountInfo 是蓝奏优享账号信息。
type AccountInfo struct {
	UID       string `json:"uid"`
	Account   string `json:"account"`
	TotalSize int64  `json:"totalSize"`
	UsedSize  int64  `json:"usedSize"`
	VIPSize   int64  `json:"vipSize"`
	VIPTime   string `json:"vipTime"`
}

// Account 读取账号信息（同时用于校验登录态）。
func (c *Client) Account(ctx context.Context) (AccountInfo, error) {
	raw, err := c.doBytes(ctx, "读取账号信息", http.MethodGet, c.withCommon("/proved/user/account/map"), nil, "")
	if err != nil {
		return AccountInfo{}, err
	}
	data, err := checkEnvelope(raw, "读取账号信息")
	if err != nil {
		return AccountInfo{}, err
	}
	m := data
	if inner, ok := asObject(data["map"]); ok {
		m = inner
	}
	return AccountInfo{
		UID:       strField(m, "userId"),
		Account:   strField(m, "account"),
		TotalSize: intField(m, "totalSize"),
		UsedSize:  intField(m, "usedSize"),
		VIPSize:   intField(m, "vipSize"),
		VIPTime:   strField(m, "vipTime"),
	}, nil
}

// Validate 校验登录态是否仍然可用。
func (c *Client) Validate(ctx context.Context) error {
	if !c.creds.Valid() {
		return &APIError{Op: "校验登录", Detail: "未登录"}
	}
	_, err := c.Account(ctx)
	return err
}

// Listing 列出目录内容。folderID 为空、"0"、"-1" 都表示根目录。
func (c *Client) Listing(ctx context.Context, folderID string) (lanzou.Listing, error) {
	if !c.creds.Valid() {
		return lanzou.Listing{}, &APIError{Op: "列目录", Detail: "未登录"}
	}
	id := normalizeFolderID(folderID)
	out := lanzou.Listing{FolderID: id, Folders: []lanzou.Folder{}, Files: []lanzou.File{}}
	seen := map[string]bool{}

	rows, err := c.listAllPages(ctx, "/proved/record/folder/list?offset=", id)
	if err != nil {
		return lanzou.Listing{}, err
	}
	for _, row := range rows {
		id := strField(row, "folderId")
		name := strField(row, "folderName")
		if name == "" {
			name = strField(row, "fileName")
		}
		if id == "" || name == "" || seen[id] {
			continue
		}
		seen[id] = true
		out.Folders = append(out.Folders, lanzou.Folder{ID: id, Name: htmlName(name)})
	}

	rows, err = c.listAllPages(ctx, "/proved/record/file/list?offset=", id)
	if err != nil {
		return lanzou.Listing{}, err
	}
	for _, row := range rows {
		isFolder := intField(row, "fileType") == 2
		itemID := strField(row, "fileId")
		name := strField(row, "fileName")
		if isFolder {
			itemID = strField(row, "folderId")
			if name == "" {
				name = strField(row, "folderName")
			}
		}
		if name == "" {
			if isFolder {
				name = strField(row, "fileName")
			} else {
				name = strField(row, "folderName")
			}
		}
		if itemID == "" || name == "" || seen[itemID] {
			continue
		}
		seen[itemID] = true
		if isFolder {
			out.Folders = append(out.Folders, lanzou.Folder{ID: itemID, Name: htmlName(name)})
			continue
		}
		size := int64(-1)
		if kb := intField(row, "fileSize"); kb >= 0 && kb <= math.MaxInt64/1024 {
			size = kb * 1024
		}
		out.Files = append(out.Files, lanzou.File{
			ID:   itemID,
			Name: htmlName(name),
			Size: size,
			Time: strField(row, "updTime"),
		})
	}
	return out, nil
}

func (c *Client) listAllPages(ctx context.Context, prefix, folderID string) ([]map[string]json.RawMessage, error) {
	var out []map[string]json.RawMessage
	for page := 1; page <= maxPages; page++ {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		path := prefix + strconv.Itoa(page) + "&limit=" + strconv.Itoa(pageSize) +
			"&folderId=" + url.QueryEscape(folderID) + "&type=0"
		raw, err := c.doBytes(ctx, "列目录", http.MethodGet, c.withCommon(path), nil, "")
		if err != nil {
			return nil, err
		}
		data, err := checkEnvelope(raw, "列目录")
		if err != nil {
			return nil, err
		}
		list, _ := asArray(data["list"])
		for _, item := range list {
			out = append(out, item)
		}
		total := int(intField(data, "totalPage"))
		if total <= page || page >= 200 {
			break
		}
	}
	return out, nil
}

// Upload 上传单个文件。progress 可为 nil；total 为文件字节数。
func (c *Client) Upload(ctx context.Context, localPath, folderID string, progress func(sent, total int64)) (lanzou.UploadResult, error) {
	if !c.creds.Valid() {
		return lanzou.UploadResult{}, &APIError{Op: "上传", Detail: "未登录"}
	}
	info, err := os.Stat(localPath)
	if err != nil {
		return lanzou.UploadResult{}, &APIError{Op: "上传", Detail: err.Error()}
	}
	if info.IsDir() {
		return lanzou.UploadResult{}, &APIError{Op: "上传", Detail: "目标是目录，不是文件"}
	}
	name := filepath.Base(localPath)
	sum, err := fileMD5(localPath)
	if err != nil {
		return lanzou.UploadResult{}, &APIError{Op: "上传", Detail: "计算 MD5 失败：" + err.Error()}
	}
	folderID = normalizeFolderID(folderID)

	upToken, instantID, err := c.requestUpToken(ctx, name, folderID, info.Size(), sum)
	if err != nil {
		return lanzou.UploadResult{}, err
	}
	if upToken == "-1" {
		if instantID == "" {
			return lanzou.UploadResult{}, &APIError{Op: "上传", Detail: "服务端返回秒传，但没有文件 ID"}
		}
		if progress != nil {
			progress(info.Size(), info.Size())
		}
		return lanzou.UploadResult{FileID: instantID, Instant: true}, nil
	}

	key := c.objectKey(time.Now())
	var token string
	if info.Size() > c.partSize {
		token, err = c.uploadChunks(ctx, upToken, key, localPath, info.Size(), progress)
	} else {
		token, err = c.uploadForm(ctx, upToken, key, localPath, info.Size(), progress)
	}
	if err != nil {
		return lanzou.UploadResult{}, err
	}
	fileID, err := c.waitResult(ctx, token, name)
	if err != nil {
		return lanzou.UploadResult{}, err
	}
	return lanzou.UploadResult{FileID: fileID}, nil
}

func (c *Client) requestUpToken(ctx context.Context, name, folderID string, size int64, md5hex string) (string, string, error) {
	body := fmt.Sprintf(`{"fileName":%s,"fileSize":%d,"folderId":%s,"md5":%s,"type":1}`,
		jsonString(name), sizeKB(size), jsonString(folderID), jsonString(md5hex))
	raw, err := c.doBytes(ctx, "获取上传令牌", http.MethodPost, c.withCommon("/proved/7n/getUpToken"), []byte(body), "application/json")
	if err != nil {
		return "", "", err
	}
	data, err := checkEnvelope(raw, "获取上传令牌")
	if err != nil {
		return "", "", err
	}
	upToken := strField(data, "upToken")
	if upToken == "" {
		upToken = strField(mustObject(raw), "upToken")
	}
	if upToken == "" {
		return "", "", &APIError{Op: "获取上传令牌", Detail: "响应里没有 upToken"}
	}
	instantID := ""
	if upToken == "-1" {
		if m, ok := asObject(data["map"]); ok {
			instantID = strField(m, "fileId")
		}
	}
	return upToken, instantID, nil
}

func (c *Client) uploadForm(ctx context.Context, upToken, key, localPath string, size int64, progress func(int64, int64)) (string, error) {
	pr, pw := io.Pipe()
	mw := multipart.NewWriter(pw)
	go func() {
		err := func() error {
			if err := mw.WriteField("token", upToken); err != nil {
				return err
			}
			if err := mw.WriteField("key", key); err != nil {
				return err
			}
			part, err := mw.CreateFormFile("file", sanitizeFilename(filepath.Base(localPath)))
			if err != nil {
				return err
			}
			f, err := os.Open(localPath)
			if err != nil {
				return err
			}
			defer f.Close()
			var src io.Reader = f
			if progress != nil {
				src = &progressReader{r: f, total: size, cb: progress}
			}
			_, err = io.Copy(part, src)
			return err
		}()
		if err == nil {
			err = mw.Close()
		}
		_ = pw.CloseWithError(err)
	}()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.uploadRoot, pr)
	if err != nil {
		return "", &APIError{Op: "七牛上传", Detail: err.Error()}
	}
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Origin", originHeader)
	req.Header.Set("Referer", refererValue)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	resp, err := c.http.Do(req)
	if err != nil {
		return "", &APIError{Op: "七牛上传", Detail: err.Error()}
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode >= 400 {
		return "", &APIError{Op: "七牛上传", Status: resp.StatusCode, Detail: truncate(string(raw), 300)}
	}
	return jsonToken(raw)
}

func (c *Client) uploadChunks(ctx context.Context, upToken, key, localPath string, size int64, progress func(int64, int64)) (string, error) {
	target := c.uploadBucket + base64.URLEncoding.EncodeToString([]byte(key))
	initURL := target + "/uploads"
	raw, err := c.doUpToken(ctx, "七牛分片初始化", http.MethodPost, initURL, upToken, nil, "", nil)
	if err != nil {
		return "", err
	}
	uploadID := jsonField(raw, "uploadId")
	if uploadID == "" {
		return "", &APIError{Op: "七牛分片初始化", Detail: "响应缺少 uploadId"}
	}

	parts := int((size + c.partSize - 1) / c.partSize)
	if parts < 1 {
		parts = 1
	}
	etags := make([]string, 0, parts)
	f, err := os.Open(localPath)
	if err != nil {
		return "", &APIError{Op: "七牛分片上传", Detail: err.Error()}
	}
	defer f.Close()

	buf := make([]byte, c.partSize)
	var sent int64
	for n := 1; n <= parts; n++ {
		if err := ctx.Err(); err != nil {
			return "", &APIError{Op: "七牛分片上传", Detail: "已取消"}
		}
		want := int64(c.partSize)
		if remain := size - sent; remain < want {
			want = remain
		}
		if _, err := io.ReadFull(f, buf[:want]); err != nil {
			return "", &APIError{Op: "七牛分片上传", Detail: "读取分片失败：" + err.Error()}
		}
		part := buf[:want]
		sum := md5.Sum(part)
		// 七牛公开协议为 /uploads/<uploadId>/<partNumber>；C# 反编译片段里漏了 uploadId。
		// 这里按公开协议实现，是否与优享服务端完全一致尚未真机验证（见 docs 未验证项）。
		partURL := initURL + "/" + url.PathEscape(uploadID) + "/" + strconv.Itoa(n)
		raw, err := c.doUpToken(ctx, "七牛分片上传", http.MethodPut, partURL, upToken, part, "application/octet-stream",
			map[string]string{"Content-MD5": base64.StdEncoding.EncodeToString(sum[:])})
		if err != nil {
			return "", err
		}
		etag := jsonField(raw, "etag")
		if etag == "" {
			return "", &APIError{Op: "七牛分片上传", Detail: "响应缺少 etag"}
		}
		etags = append(etags, etag)
		sent += want
		if progress != nil {
			progress(sent, size)
		}
	}

	var b strings.Builder
	b.WriteString(`{"parts":[`)
	for i, etag := range etags {
		if i > 0 {
			b.WriteString(",")
		}
		fmt.Fprintf(&b, `{"partNumber":%d,"etag":%s}`, i+1, jsonString(etag))
	}
	b.WriteString(`]}`)
	raw, err = c.doUpToken(ctx, "七牛分片合并", http.MethodPost, initURL+"/"+url.PathEscape(uploadID), upToken, []byte(b.String()), "application/json", nil)
	if err != nil {
		return "", err
	}
	return jsonToken(raw)
}

func (c *Client) waitResult(ctx context.Context, token, wantName string) (string, error) {
	if strings.TrimSpace(token) == "" {
		return "", &APIError{Op: "上传结果", Detail: "七牛返回的 token 为空"}
	}
	tokenTime := time.Now().Format("2006-01-02 15:04:05")
	path := "/unproved/7n/results?tokenList=" + url.QueryEscape(token) + "&tokenTime=" + url.QueryEscape(tokenTime)
	var lastMsg string
	for i := 0; i < c.maxPoll; i++ {
		if err := ctx.Err(); err != nil {
			return "", &APIError{Op: "上传结果", Detail: "已取消"}
		}
		raw, err := c.doBytes(ctx, "查询上传结果", http.MethodGet, c.withCommon(path), nil, "")
		if err != nil {
			return "", err
		}
		data, err := checkEnvelope(raw, "查询上传结果")
		if err != nil {
			return "", err
		}
		list, _ := asArray(data["list"])
		var fallback *resultItem
		for _, row := range list {
			it := resultItem{
				FileID: strField(row, "fileId"),
				Name:   strField(row, "fileName"),
				Status: intField(row, "status"),
				Msg:    strField(row, "msg"),
			}
			if it.Status == 1 && (wantName == "" || it.Name == "" || strings.EqualFold(it.Name, wantName)) {
				return it.FileID, nil
			}
			if fallback == nil {
				cp := it
				fallback = &cp
			}
			if it.Msg != "" {
				lastMsg = it.Msg
			}
		}
		if fallback != nil && fallback.Status == 1 {
			return fallback.FileID, nil
		}
		if i < c.maxPoll-1 {
			select {
			case <-ctx.Done():
				return "", &APIError{Op: "上传结果", Detail: "已取消"}
			case <-time.After(c.pollGap):
			}
		}
	}
	detail := "上传已提交，但未在等待时间内确认结果"
	if lastMsg != "" {
		detail += "：" + lastMsg
	}
	return "", &APIError{Op: "上传结果", Detail: detail}
}

type resultItem struct {
	FileID string
	Name   string
	Status int64
	Msg    string
}

func (c *Client) doUpToken(ctx context.Context, op, method, rawURL, upToken string, payload []byte, contentType string, extra map[string]string) ([]byte, error) {
	if err := c.wait(ctx); err != nil {
		return nil, err
	}
	var body io.Reader
	if payload != nil {
		body = bytes.NewReader(payload)
	}
	req, err := http.NewRequestWithContext(ctx, method, rawURL, body)
	if err != nil {
		return nil, &APIError{Op: op, Detail: err.Error()}
	}
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Origin", originHeader)
	req.Header.Set("Referer", refererValue)
	req.Header.Set("Authorization", "UpToken "+upToken)
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	for k, v := range extra {
		req.Header.Set(k, v)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, &APIError{Op: op, Detail: err.Error()}
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode >= 400 {
		return nil, &APIError{Op: op, Status: resp.StatusCode, Detail: truncate(string(raw), 300)}
	}
	return raw, nil
}

func (c *Client) doBytes(ctx context.Context, op, method, rawURL string, payload []byte, contentType string) ([]byte, error) {
	var lastErr error
	for attempt := 0; attempt < 3; attempt++ {
		if err := ctx.Err(); err != nil {
			return nil, &APIError{Op: op, Detail: "已取消"}
		}
		raw, status, err := c.doOnce(ctx, method, rawURL, payload, contentType)
		if err == nil {
			return raw, nil
		}
		lastErr = err
		if status >= 400 && status < 500 && status != http.StatusTooManyRequests {
			break
		}
		if attempt < 2 {
			select {
			case <-ctx.Done():
				return nil, &APIError{Op: op, Detail: "已取消"}
			case <-time.After(time.Duration(300*(attempt+1)) * time.Millisecond):
			}
		}
	}
	var apiErr *APIError
	if errors.As(lastErr, &apiErr) {
		if apiErr.Op == "" {
			apiErr.Op = op
		}
		return nil, apiErr
	}
	return nil, &APIError{Op: op, Detail: lastErr.Error()}
}

func (c *Client) doOnce(ctx context.Context, method, rawURL string, payload []byte, contentType string) ([]byte, int, error) {
	if err := c.wait(ctx); err != nil {
		return nil, 0, err
	}
	var body io.Reader
	if payload != nil {
		body = bytes.NewReader(payload)
	}
	req, err := http.NewRequestWithContext(ctx, method, rawURL, body)
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("Accept", acceptHeader)
	req.Header.Set("Accept-Language", acceptLang)
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Origin", originHeader)
	req.Header.Set("Referer", refererValue)
	req.Header.Set("X-WK-Device-Flag", "1")
	if c.creds.AppToken != "" {
		req.Header.Set("Authorization", "Bearer "+c.creds.AppToken)
	}
	if c.creds.UID != "" {
		req.Header.Set("X-WK-UID", c.creds.UID)
	}
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()
	raw, readErr := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if readErr != nil {
		return nil, resp.StatusCode, readErr
	}
	if resp.StatusCode >= 400 {
		return nil, resp.StatusCode, &APIError{Op: "", Status: resp.StatusCode, Detail: truncate(string(raw), 300)}
	}
	return raw, resp.StatusCode, nil
}

func (c *Client) withCommon(path string) string {
	sep := "?"
	if strings.Contains(path, "?") {
		sep = "&"
	}
	uuid := c.creds.Uuid
	extra := c.creds.Extra
	if extra == "" {
		extra = "2"
	}
	q := "uuid=" + url.QueryEscape(uuid) +
		"&devType=6&devCode=" + url.QueryEscape(uuid) +
		"&devModel=chrome&devVersion=131&appVersion=&timestamp=" + strconv.FormatInt(time.Now().UnixMilli(), 10) +
		"&extra=" + url.QueryEscape(extra)
	if c.creds.AppToken != "" {
		q += "&appToken=" + url.QueryEscape(c.creds.AppToken)
	}
	return c.base + path + sep + q
}

func (c *Client) wait(ctx context.Context) error {
	c.mu.Lock()
	interval := c.interval
	last := c.last
	c.mu.Unlock()
	if interval > 0 && !last.IsZero() {
		if gap := interval - time.Since(last); gap > 0 {
			select {
			case <-ctx.Done():
				return &APIError{Op: "请求", Detail: "已取消"}
			case <-time.After(gap):
			}
		}
	}
	c.mu.Lock()
	c.last = time.Now()
	c.mu.Unlock()
	return nil
}

func (c *Client) objectKey(now time.Time) string {
	account := strings.TrimSpace(c.creds.Account)
	if account == "" {
		account = "0"
	}
	guid := strings.ToLower(strings.ReplaceAll(newUUID(), "-", ""))
	return fmt.Sprintf("storage/files/%d/%d/%d/%s/%d_%s.gz",
		now.Year(), int(now.Month()), now.Day(), account, now.UnixMilli(), guid)
}

func newUUID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		now := time.Now().UnixNano()
		return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x", uint32(now), uint16(now>>32), uint16(now>>16), uint16(now>>48), now&0xffffffffffff)
	}
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

func fileMD5(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := md5.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func normalizeFolderID(id string) string {
	id = strings.TrimSpace(id)
	if id == "" || id == "0" || id == "-1" || strings.EqualFold(id, "root") {
		return ""
	}
	return id
}

func sizeKB(size int64) int64 {
	if size <= 0 {
		return 1
	}
	n := int64(math.RoundToEven(float64(size) / 1024.0))
	if n < 1 {
		return 1
	}
	return n
}

func sanitizeFilename(name string) string {
	if name == "" {
		return "upload.bin"
	}
	var b strings.Builder
	for _, r := range name {
		if r < ' ' || r > '~' || r == '"' || r == '\\' {
			b.WriteRune('_')
		} else {
			b.WriteRune(r)
		}
	}
	if b.Len() == 0 {
		return "upload.bin"
	}
	return b.String()
}

func htmlName(s string) string {
	s = strings.TrimSpace(s)
	s = strings.ReplaceAll(s, "&amp;", "&")
	s = strings.ReplaceAll(s, "&quot;", `"`)
	s = strings.ReplaceAll(s, "&#39;", "'")
	s = strings.ReplaceAll(s, "&lt;", "<")
	s = strings.ReplaceAll(s, "&gt;", ">")
	return s
}

func truncate(s string, n int) string {
	s = strings.TrimSpace(s)
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

// ---- JSON 小工具 ----

func checkEnvelope(raw []byte, op string) (map[string]json.RawMessage, error) {
	root, err := decodeObject(raw)
	if err != nil {
		return nil, &APIError{Op: op, Detail: "响应不是 JSON：" + truncate(string(raw), 200)}
	}
	code := intField(root, "code")
	if code != 0 && code != 200 {
		msg := strField(root, "msg")
		if msg == "" {
			msg = "服务端未返回 msg"
		}
		apiErr := &APIError{Op: op, Status: int(code), Detail: msg}
		lower := strings.ToLower(msg)
		if strings.Contains(msg, "容量") || strings.Contains(msg, "空间不足") ||
			strings.Contains(msg, "配额") || strings.Contains(msg, "已满") {
			apiErr.Hint = "请清理网盘空间后重试。"
		} else if strings.Contains(msg, "登录") || strings.Contains(msg, "未授权") ||
			strings.Contains(lower, "token") {
			apiErr.Hint = "登录态可能已失效，请重新登录。"
		}
		return nil, apiErr
	}
	if inner, ok := asObject(root["data"]); ok {
		return inner, nil
	}
	return root, nil
}

func decodeObject(raw []byte) (map[string]json.RawMessage, error) {
	var m map[string]json.RawMessage
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil, err
	}
	return m, nil
}

func mustObject(raw []byte) map[string]json.RawMessage {
	m, _ := decodeObject(raw)
	return m
}

func asObject(raw json.RawMessage) (map[string]json.RawMessage, bool) {
	if len(raw) == 0 {
		return nil, false
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil, false
	}
	return m, true
}

func asArray(raw json.RawMessage) ([]map[string]json.RawMessage, bool) {
	if len(raw) == 0 {
		return nil, false
	}
	var list []map[string]json.RawMessage
	if err := json.Unmarshal(raw, &list); err != nil {
		return nil, false
	}
	return list, true
}

func strField(m map[string]json.RawMessage, key string) string {
	raw, ok := m[key]
	if !ok || len(raw) == 0 {
		return ""
	}
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return strings.TrimSpace(s)
	}
	var n json.Number
	if err := json.Unmarshal(raw, &n); err == nil {
		return n.String()
	}
	return ""
}

func intField(m map[string]json.RawMessage, key string) int64 {
	raw, ok := m[key]
	if !ok || len(raw) == 0 {
		return 0
	}
	var n json.Number
	if err := json.Unmarshal(raw, &n); err == nil {
		if v, err := n.Int64(); err == nil {
			return v
		}
		if f, err := n.Float64(); err == nil {
			return int64(f)
		}
	}
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		if v, err := strconv.ParseInt(strings.TrimSpace(s), 10, 64); err == nil {
			return v
		}
	}
	return 0
}

func jsonString(s string) string {
	raw, _ := json.Marshal(s)
	return string(raw)
}

func jsonField(raw []byte, key string) string {
	m, err := decodeObject(raw)
	if err != nil {
		return ""
	}
	if v := strField(m, key); v != "" {
		return v
	}
	if inner, ok := asObject(m["data"]); ok {
		return strField(inner, key)
	}
	return ""
}

func jsonToken(raw []byte) (string, error) {
	token := jsonField(raw, "token")
	if token == "" {
		return "", &APIError{Op: "七牛上传", Detail: "响应缺少 token：" + truncate(string(raw), 200)}
	}
	return token, nil
}

type progressReader struct {
	r     io.Reader
	total int64
	sent  int64
	cb    func(int64, int64)
}

func (p *progressReader) Read(b []byte) (int, error) {
	n, err := p.r.Read(b)
	if n > 0 {
		p.sent += int64(n)
		p.cb(p.sent, p.total)
	}
	return n, err
}
