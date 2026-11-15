// Package lanzou 实现蓝奏云（老版）的登录、目录浏览、上传，以及上传前的
// 「同名 + 大小容差」云端去重。里程碑：M6。
//
// 一期范围（对等 C# 版）：
//   - 账号密码登录（含 acw_sc__v2 JS 挑战）或直接粘贴 Cookie；
//   - 浏览云端文件夹与文件（doupload.php）；
//   - 上传（html5up.php），单文件上限 100 MiB，>100 MiB 本地直接跳过；
//   - 上传前按 max(2048, 本地大小/100) 容差做同名同大小去重，命中则把本地文件
//     移动到「网盘重复文件」目录，不上传；
//   - 本机上传缓存（同一路径/大小/修改时间已传过则跳过）。
//
// 未联调声明：仓库内没有蓝奏账号，登录后的列表与上传只按协议实现并用 httptest
// 覆盖；真实链路（登录成功、真实上传、云端去重命中）都需要账号验证，见
// docs/ui-shell.md「蓝奏云上传」。
//
// 分享创建不在本包范围：C# 版没有这个功能，服务端 /proved/share/url 目前也不可用。
package lanzou

import (
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// PlatformLanzou 是老版蓝奏云的平台标识，与 C# 版凭据文件里的 platform 字段对应。
const PlatformLanzou = "lanzou"

// MaxUploadSize 是老版蓝奏云单文件上限（100 MiB）。超过的本地文件直接跳过，不发起请求。
//
// 这是 C# 版的既有规则（c/G.cs 的 100 MiB 判断），未在服务端重新确认。
const MaxUploadSize int64 = 100 << 20

// 去重容差：|本地 - 云端| <= max(2048 字节, 本地大小/100)。
const (
	MinSizeTolerance     int64 = 2048
	SizeToleranceDivisor int64 = 100
)

// Kind 是一次上传结果的分类，取值与 C# 版七种结果一一对应。
type Kind string

const (
	KindSuccess      Kind = "success"       // 上传成功
	KindInstant      Kind = "instant"       // 秒传（服务端已有相同内容）
	KindCached       Kind = "cached"        // 本机缓存命中，本次跳过
	KindSkippedLarge Kind = "skipped-large" // 超过 100 MiB，未上传
	KindDuplicate    Kind = "duplicate"     // 云端已有同名同大小，本地文件已移动
	KindCanceled     Kind = "canceled"      // 用户取消，未上传
	KindFailed       Kind = "failed"        // 失败（错误原文见 Error）
	KindPending      Kind = "pending"       // 预检通过，等待上传（不是最终结果）
)

// KindLabel 返回中文标签，前端与报告共用，避免两边各写一份。
func KindLabel(k Kind) string {
	switch k {
	case KindSuccess:
		return "上传成功"
	case KindInstant:
		return "秒传"
	case KindCached:
		return "缓存跳过"
	case KindSkippedLarge:
		return "超过 100 MiB 跳过"
	case KindDuplicate:
		return "云端重复（本地已移动）"
	case KindCanceled:
		return "已取消"
	case KindFailed:
		return "失败"
	case KindPending:
		return "待上传"
	default:
		return string(k)
	}
}

// Credentials 是登录态。只保存 Cookie（ylogin / phpdisk_info），不保存密码。
type Credentials struct {
	Platform    string    `json:"platform"`
	Ylogin      string    `json:"ylogin"`
	PhpdiskInfo string    `json:"phpdisk_info"`
	SavedAt     time.Time `json:"savedAt,omitempty"`
}

// CookieHeader 拼出访问 pc.woozooo.com 所需的 Cookie 头。
func (c Credentials) CookieHeader() string {
	parts := make([]string, 0, 2)
	if c.Ylogin != "" {
		parts = append(parts, "ylogin="+c.Ylogin)
	}
	if c.PhpdiskInfo != "" {
		parts = append(parts, "phpdisk_info="+c.PhpdiskInfo)
	}
	return strings.Join(parts, "; ")
}

// Valid 判断凭据是否完整。
func (c Credentials) Valid() bool {
	return strings.TrimSpace(c.Ylogin) != "" && strings.TrimSpace(c.PhpdiskInfo) != ""
}

// MaskedAccount 只暴露 ylogin 的后四位，界面与日志都用它，避免泄露完整登录态。
func (c Credentials) MaskedAccount() string {
	y := strings.TrimSpace(c.Ylogin)
	if y == "" {
		return ""
	}
	if len(y) <= 4 {
		return "****"
	}
	return "****" + y[len(y)-4:]
}

// Folder 是云端文件夹。
type Folder struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// File 是云端文件。Size 为 -1 表示服务端给的大小无法解析（此时不参与去重）。
type File struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Size     int64  `json:"size"`
	SizeText string `json:"sizeText,omitempty"`
	Time     string `json:"time,omitempty"`
}

// Listing 是某个云端目录下的内容。
type Listing struct {
	FolderID string   `json:"folderId"`
	Folders  []Folder `json:"folders"`
	Files    []File   `json:"files"`
}

// Item 是一个本地文件的上传结果。
type Item struct {
	LocalPath   string `json:"localPath"`
	Name        string `json:"name"`
	Size        int64  `json:"size"`
	Kind        Kind   `json:"kind"`
	RemoteID    string `json:"remoteId,omitempty"`
	DuplicateOf string `json:"duplicateOf,omitempty"`
	MovedTo     string `json:"movedTo,omitempty"`
	Error       string `json:"error,omitempty"`
}

// Summary 是各类结果的计数，最后一行汇总用。
type Summary struct {
	Total        int `json:"total"`
	Success      int `json:"success"`
	Instant      int `json:"instant"`
	Cached       int `json:"cached"`
	SkippedLarge int `json:"skippedLarge"`
	Duplicate    int `json:"duplicate"`
	Canceled     int `json:"canceled"`
	Failed       int `json:"failed"`
}

// Add 把一个结果计入汇总。
func (s *Summary) Add(k Kind) {
	s.Total++
	switch k {
	case KindSuccess:
		s.Success++
	case KindInstant:
		s.Instant++
	case KindCached:
		s.Cached++
	case KindSkippedLarge:
		s.SkippedLarge++
	case KindDuplicate:
		s.Duplicate++
	case KindCanceled:
		s.Canceled++
	case KindFailed:
		s.Failed++
	}
}

// Done 返回已经完成的上传数（成功 + 秒传），用于进度显示。
func (s Summary) Done() int { return s.Success + s.Instant }

// APIError 保留服务端原文，不做二次包装。
//
// 设计约束：宁可把 HTTP 状态码和响应片段直接摆给用户，也不要编一句“网络错误”。
type APIError struct {
	Op     string // 操作名，如“登录”“列目录”“上传”
	Status int    // HTTP 状态码，0 表示没拿到响应
	Detail string // 服务端原文（JSON 或截断后的响应体）
	Hint   string // 排查建议，可为空
}

func (e *APIError) Error() string {
	var b strings.Builder
	b.WriteString("蓝奏云 ")
	b.WriteString(e.Op)
	b.WriteString("失败")
	if e.Status > 0 {
		fmt.Fprintf(&b, "（HTTP %d）", e.Status)
	}
	if e.Detail != "" {
		b.WriteString("：")
		b.WriteString(e.Detail)
	}
	if e.Hint != "" {
		b.WriteString("；")
		b.WriteString(e.Hint)
	}
	return b.String()
}

// ErrDPAPIUnsupported 表示当前平台没有 DPAPI，凭据只能留在内存里。
var ErrDPAPIUnsupported = errors.New("当前平台不支持 DPAPI 凭据加密存储")

// SizeWithinTolerance 复刻 C# 版 a.Z.A_m_1(long,long)：
//
//	|local-remote| <= max(2048, local/100)
//
// 任一为负（服务端大小解析失败）一律不匹配。
func SizeWithinTolerance(local, remote int64) bool {
	if local < 0 || remote < 0 {
		return false
	}
	diff := local - remote
	if diff < 0 {
		diff = -diff
	}
	tol := local / SizeToleranceDivisor
	if tol < MinSizeTolerance {
		tol = MinSizeTolerance
	}
	return diff <= tol
}

// SameName 复刻 C# 版的名字比较：先 Trim，再 OrdinalIgnoreCase。
func SameName(a, b string) bool {
	return strings.EqualFold(strings.TrimSpace(a), strings.TrimSpace(b))
}

// FindDuplicate 在云端文件里找同名且大小在容差内的文件。
// 云端名字可能带 HTML 实体，这里统一反转义后再比。
func FindDuplicate(name string, size int64, files []File) (File, bool) {
	want := strings.TrimSpace(html.UnescapeString(name))
	for _, f := range files {
		if !SameName(want, html.UnescapeString(f.Name)) {
			continue
		}
		if !SizeWithinTolerance(size, f.Size) {
			continue
		}
		return f, true
	}
	return File{}, false
}

// ParseSizeText 解析蓝奏返回的 "1.2 M" / "345 K" / "12" 这类大小文本。
// 无法解析时返回 -1，保证它不会匹配任何本地文件。
func ParseSizeText(s string) int64 {
	t := strings.TrimSpace(strings.ToUpper(s))
	if t == "" {
		return -1
	}
	mult := int64(1)
	switch {
	case strings.HasSuffix(t, "GB"):
		mult, t = 1<<30, strings.TrimSpace(strings.TrimSuffix(t, "GB"))
	case strings.HasSuffix(t, "G"):
		mult, t = 1<<30, strings.TrimSpace(strings.TrimSuffix(t, "G"))
	case strings.HasSuffix(t, "MB"):
		mult, t = 1<<20, strings.TrimSpace(strings.TrimSuffix(t, "MB"))
	case strings.HasSuffix(t, "M"):
		mult, t = 1<<20, strings.TrimSpace(strings.TrimSuffix(t, "M"))
	case strings.HasSuffix(t, "KB"):
		mult, t = 1<<10, strings.TrimSpace(strings.TrimSuffix(t, "KB"))
	case strings.HasSuffix(t, "K"):
		mult, t = 1<<10, strings.TrimSpace(strings.TrimSuffix(t, "K"))
	case strings.HasSuffix(t, "B"):
		mult, t = 1, strings.TrimSpace(strings.TrimSuffix(t, "B"))
	}
	if t == "" {
		return -1
	}
	v, err := strconv.ParseFloat(t, 64)
	if err != nil || v < 0 {
		return -1
	}
	return int64(v * float64(mult))
}

// ParseCookieHeader 从用户粘贴的 Cookie 文本里取 ylogin 与 phpdisk_info。
// 允许整段 "a=1; b=2"、带换行、带 "Cookie:" 前缀，其他键忽略。
func ParseCookieHeader(raw string) (Credentials, error) {
	raw = strings.TrimSpace(raw)
	raw = strings.TrimPrefix(raw, "Cookie:")
	raw = strings.TrimPrefix(raw, "cookie:")
	creds := Credentials{Platform: PlatformLanzou}
	for _, part := range strings.FieldsFunc(raw, func(r rune) bool {
		return r == ';' || r == '\n' || r == '\r'
	}) {
		kv := strings.SplitN(strings.TrimSpace(part), "=", 2)
		if len(kv) != 2 {
			continue
		}
		key := strings.TrimSpace(kv[0])
		val := strings.TrimSpace(kv[1])
		switch strings.ToLower(key) {
		case "ylogin":
			creds.Ylogin = val
		case "phpdisk_info":
			creds.PhpdiskInfo = val
		}
	}
	if !creds.Valid() {
		return Credentials{}, &APIError{
			Op:     "解析 Cookie",
			Detail: "缺少 ylogin 或 phpdisk_info",
			Hint:   "请在浏览器登录蓝奏云后，从开发者工具复制完整 Cookie（至少包含 ylogin 与 phpdisk_info）",
		}
	}
	return creds, nil
}

// Report 是上传结束后落盘的清单，JSON 与 CSV 内容一致。
type Report struct {
	SchemaVersion int    `json:"schemaVersion"`
	Kind          string `json:"kind"`
	GeneratedAt   string `json:"generatedAt"`
	Platform      string `json:"platform"`
	FolderID      string `json:"folderId"`
	FolderName    string `json:"folderName"`
	LocalDir      string `json:"localDir"`
	Summary       Summary `json:"summary"`
	Items         []Item  `json:"items"`
	Note          string `json:"note"`
}

// BuildReport 组装报告。
func BuildReport(folderID, folderName, localDir string, items []Item, sum Summary) Report {
	return Report{
		SchemaVersion: 1,
		Kind:          "filededup.lanzou.upload-report",
		GeneratedAt:   time.Now().Format(time.RFC3339),
		Platform:      PlatformLanzou,
		FolderID:      folderID,
		FolderName:    folderName,
		LocalDir:      localDir,
		Summary:       sum,
		Items:         items,
		Note: "结果以服务端返回原文为准；服务端拒绝的文件格式会按失败原样记录。" +
			"分享创建不在本功能范围（服务端接口不可用），需要分享请在网页端手动操作。",
	}
}

// WriteReport 把报告写成 <dir>/upload-report-<时间戳>.json 与 .csv，返回两个路径。
func WriteReport(dir string, r Report, now time.Time) (string, string, error) {
	stamp := now.Format("20060102-150405")
	base := filepath.Join(dir, "upload-report-"+stamp)
	jsonPath := base + ".json"
	csvPath := base + ".csv"

	raw, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return "", "", err
	}
	if err := os.WriteFile(jsonPath, raw, 0o644); err != nil {
		return "", "", err
	}
	if err := os.WriteFile(csvPath, []byte(reportCSV(r)), 0o644); err != nil {
		return jsonPath, "", err
	}
	return jsonPath, csvPath, nil
}

func reportCSV(r Report) string {
	var b strings.Builder
	b.WriteString("\uFEFF") // BOM，Excel 打开中文不乱码
	b.WriteString("本地路径,文件名,大小(字节),结果,云端ID,重复对象,移动到,错误\r\n")
	for _, it := range r.Items {
		fields := []string{
			it.LocalPath, it.Name, strconv.FormatInt(it.Size, 10),
			KindLabel(it.Kind), it.RemoteID, it.DuplicateOf, it.MovedTo, it.Error,
		}
		for i, f := range fields {
			if i > 0 {
				b.WriteString(",")
			}
			b.WriteString(csvCell(f))
		}
		b.WriteString("\r\n")
	}
	return b.String()
}

func csvCell(s string) string {
	if !strings.ContainsAny(s, ",\"\r\n") {
		return s
	}
	return `"` + strings.ReplaceAll(s, `"`, `""`) + `"`
}

// SolveACWChallenge 复刻 acw_sc__v2 的计算：先按固定表换位，再与固定密钥逐字节异或。
// 换位表与密钥来自 C# 版 B.cs（A_5 与 c_1），与服务端 JS 挑战一致。
//
// 这是公开的反爬协议常量，不是账号凭据。
func SolveACWChallenge(arg1 string) (string, error) {
	arg1 = strings.TrimSpace(arg1)
	if len(arg1) != 40 {
		return "", fmt.Errorf("arg1 长度应为 40，实际 %d", len(arg1))
	}
	if _, err := hex.DecodeString(arg1); err != nil {
		return "", fmt.Errorf("arg1 不是十六进制：%w", err)
	}
	perm := [...]int{
		15, 35, 29, 24, 33, 16, 1, 38, 10, 9,
		19, 31, 40, 27, 22, 23, 25, 13, 6, 11,
		39, 18, 20, 8, 14, 21, 32, 26, 2, 30,
		7, 4, 17, 5, 3, 28, 34, 37, 12, 36,
	}
	const key = "3000176000856006061501533003690027800375"

	var boxed [40]byte
	for i, p := range perm {
		boxed[i] = arg1[p-1]
	}
	out := make([]byte, 20)
	for i := 0; i < 20; i++ {
		a, err := strconv.ParseUint(key[i*2:i*2+2], 16, 8)
		if err != nil {
			return "", err
		}
		b, err := strconv.ParseUint(string(boxed[i*2:i*2+2]), 16, 8)
		if err != nil {
			return "", err
		}
		out[i] = byte(a) ^ byte(b)
	}
	return hex.EncodeToString(out), nil
}

// ResolveURL 以 base 为基准解析 ref，ref 非法时原样返回。
func ResolveURL(base, ref string) string {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return ""
	}
	b, err := url.Parse(base)
	if err != nil {
		return ref
	}
	r, err := url.Parse(ref)
	if err != nil {
		return ref
	}
	return b.ResolveReference(r).String()
}

// truncate 把服务端响应体截断成可读的一行，避免把整页 HTML 塞进错误信息。
func truncate(s string, n int) string {
	s = strings.Join(strings.Fields(s), " ")
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

// readAll 限制读取上限，防止异常响应把内存吃光。
func readAll(r io.Reader, limit int64) ([]byte, error) {
	return io.ReadAll(io.LimitReader(r, limit))
}
