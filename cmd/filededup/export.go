//go:build desktop

package main

import (
	"encoding/csv"
	"errors"
	"fmt"
	"html"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"filededup/internal/brand"
	"filededup/internal/engine/model"
)

// ExportResult 是一次查重结果导出的结果。
type ExportResult struct {
	Path             string `json:"path"`
	Dir              string `json:"dir"`
	Format           string `json:"format"`
	Groups           int    `json:"groups"`
	Members          int    `json:"members"`
	ReclaimableBytes int64  `json:"reclaimableBytes"`
	ScanStartedAt    string `json:"scanStartedAt"`
	Bytes            int64  `json:"bytes"`
}

// exportSummary 汇总报告头部的扫描参数与统计。
type exportSummary struct {
	GeneratedAt    time.Time
	ScanStarted    time.Time
	ScanFinished   string
	Elapsed        string
	Root           string
	KeepPolicy     model.KeepPolicy
	ImageKeep      model.KeepPolicy
	MinSize        int64
	IncludeHidden  bool
	SimilarImage   bool
	ImageThreshold int
	SimilarAudio   bool
	AudioTolerance int
	Excludes       []string
	Groups         int
	Members        int
	Reclaimable    int64
	Files          int64
	Bytes          int64
	DuplicateFiles int64
	Skipped        int64
}

// ExportResults 把当前扫描结果导出为 HTML 或 CSV，默认写到“文档”目录。
func (a *App) ExportResults(id, format string) (ExportResult, error) {
	return a.exportResults(id, format, "")
}

// exportResults 的 dir 仅供测试注入；空字符串表示按默认规则选目录。
func (a *App) exportResults(id, format, dir string) (ExportResult, error) {
	s, err := a.session(id)
	if err != nil {
		return ExportResult{}, err
	}

	ext := strings.ToLower(strings.TrimSpace(format))
	switch ext {
	case "html", "htm":
		ext = "html"
	case "csv":
		ext = "csv"
	default:
		return ExportResult{}, fmt.Errorf("不支持的导出格式：%s（只支持 html 或 csv）", format)
	}

	s.mu.Lock()
	result := s.result
	req := s.req
	start := s.start
	s.mu.Unlock()
	if result == nil {
		return ExportResult{}, errors.New("扫描尚未完成，暂无可导出的结果")
	}

	if strings.TrimSpace(dir) == "" {
		dir, err = defaultExportDir()
		if err != nil {
			return ExportResult{}, err
		}
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return ExportResult{}, fmt.Errorf("无法创建导出目录：%w", err)
	}

	now := time.Now()
	base := "dedup-report-" + now.Format("20060102-150405")
	target := uniqueExportPath(dir, base, ext)

	sum := buildExportSummary(result, req, start, now)
	var writeErr error
	if ext == "csv" {
		writeErr = writeExportCSV(target, result, sum)
	} else {
		writeErr = writeExportHTML(target, result, sum)
	}
	if writeErr != nil {
		return ExportResult{}, writeErr
	}
	info, statErr := os.Stat(target)
	var size int64
	if statErr == nil {
		size = info.Size()
	}

	members := 0
	for _, g := range result.Groups {
		members += len(g.Members)
	}
	return ExportResult{
		Path:             target,
		Dir:              dir,
		Format:           ext,
		Groups:           len(result.Groups),
		Members:          members,
		ReclaimableBytes: result.Stats.ReclaimableBytes,
		ScanStartedAt:    start.Local().Format("2006-01-02 15:04:05"),
		Bytes:            size,
	}, nil
}

// defaultExportDir 优先用系统“文档”目录，不存在时退回用户主目录。
func defaultExportDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("无法定位用户目录：%w", err)
	}
	docs := filepath.Join(home, "Documents")
	if info, err := os.Stat(docs); err == nil && info.IsDir() {
		return docs, nil
	}
	return home, nil
}

// uniqueExportPath 避免同一秒内重复导出覆盖同名文件。
func uniqueExportPath(dir, base, ext string) string {
	p := filepath.Join(dir, base+"."+ext)
	if _, err := os.Stat(p); os.IsNotExist(err) {
		return p
	}
	for i := 2; i < 1000; i++ {
		candidate := filepath.Join(dir, fmt.Sprintf("%s-%d.%s", base, i, ext))
		if _, err := os.Stat(candidate); os.IsNotExist(err) {
			return candidate
		}
	}
	return filepath.Join(dir, fmt.Sprintf("%s-%d.%s", base, time.Now().UnixNano(), ext))
}

func buildExportSummary(result *model.Result, req model.Request, start, now time.Time) exportSummary {
	sum := exportSummary{
		GeneratedAt:    now,
		ScanStarted:    start,
		Root:           strings.Join(req.Roots, "; "),
		KeepPolicy:     req.KeepPolicy,
		ImageKeep:      req.ImageKeepPolicy,
		MinSize:        req.MinSize,
		IncludeHidden:  req.IncludeHidden,
		Excludes:       req.Excludes,
		Groups:         len(result.Groups),
		Reclaimable:    result.Stats.ReclaimableBytes,
		Files:          result.Stats.FilesEnumerated,
		Bytes:          result.Stats.BytesEnumerated,
		DuplicateFiles: result.Stats.DuplicateFiles,
		Skipped:        result.Stats.Skipped,
	}
	if req.SimilarImage != nil {
		sum.SimilarImage = req.SimilarImage.Enabled
		sum.ImageThreshold = req.SimilarImage.Threshold
	}
	if req.SimilarAudio != nil {
		sum.SimilarAudio = req.SimilarAudio.Enabled
		sum.AudioTolerance = req.SimilarAudio.Tolerance
	}
	for _, g := range result.Groups {
		sum.Members += len(g.Members)
	}
	if result.Runtime != nil {
		if t, err := time.Parse(time.RFC3339Nano, result.Runtime.FinishedAt); err == nil {
			sum.ScanFinished = t.Local().Format("2006-01-02 15:04:05")
		}
		if result.Runtime.ElapsedMS > 0 {
			sum.Elapsed = (time.Duration(result.Runtime.ElapsedMS) * time.Millisecond).String()
		}
	}
	if sum.ScanFinished == "" {
		sum.ScanFinished = start.Local().Format("2006-01-02 15:04:05")
	}
	return sum
}

func keepPolicyLabel(p model.KeepPolicy) string {
	switch p {
	case model.KeepPathShortest:
		return "最短路径优先"
	case model.KeepNewest:
		return "最新修改优先"
	case model.KeepOldest:
		return "最早修改优先"
	case model.KeepNameShortest:
		return "最短文件名优先"
	case model.KeepLargest:
		return "最大文件优先"
	default:
		return string(p)
	}
}

func groupKindLabel(k model.GroupKind) string {
	switch k {
	case model.GroupExact:
		return "内容相同"
	case model.GroupImage:
		return "相似图片"
	case model.GroupAudio:
		return "相似音频"
	default:
		return string(k)
	}
}

func yesNo(v bool) string {
	if v {
		return "是"
	}
	return "否"
}

func humanBytes(n int64) string {
	if n <= 0 {
		return "0 B"
	}
	units := []string{"B", "KB", "MB", "GB", "TB"}
	v := float64(n)
	i := 0
	for v >= 1024 && i < len(units)-1 {
		v /= 1024
		i++
	}
	if v >= 100 {
		return fmt.Sprintf("%.0f %s", v, units[i])
	}
	if v >= 10 {
		return fmt.Sprintf("%.1f %s", v, units[i])
	}
	return fmt.Sprintf("%.2f %s", v, units[i])
}

func localModTime(raw string) string {
	t, err := time.Parse(time.RFC3339Nano, raw)
	if err != nil {
		return raw
	}
	return t.Local().Format("2006-01-02 15:04:05")
}

// ---- CSV ----

var exportCSVHeader = []string{
	"记录类型", "编号", "类型", "保留策略", "可回收字节", "扫描开始时间", "扫描结束时间",
	"扫描根目录", "最小文件大小", "包含隐藏文件", "相似图片", "图片阈值", "相似音频", "音频容差",
	"排除项", "文件路径", "文件名", "文件大小(字节)", "修改时间", "是否保留", "是否可删除",
}

func writeExportCSV(path string, result *model.Result, sum exportSummary) error {
	var buf strings.Builder
	w := csv.NewWriter(&buf)
	if err := w.Write(exportCSVHeader); err != nil {
		return err
	}
	excludes := strings.Join(sum.Excludes, " ")
	meta := []string{
		"扫描信息", "", "", keepPolicyLabel(sum.KeepPolicy), strconv.FormatInt(sum.Reclaimable, 10),
		sum.ScanStarted.Local().Format("2006-01-02 15:04:05"), sum.ScanFinished,
		sum.Root, strconv.FormatInt(sum.MinSize, 10), yesNo(sum.IncludeHidden),
		yesNo(sum.SimilarImage), strconv.Itoa(sum.ImageThreshold), yesNo(sum.SimilarAudio),
		strconv.Itoa(sum.AudioTolerance), excludes,
		fmt.Sprintf("扫描文件 %d 个，共 %d 字节", sum.Files, sum.Bytes), "", "", "", "", "",
	}
	if err := w.Write(meta); err != nil {
		return err
	}
	// 汇总行把关键统计放进“文件路径”前一列，数值列留空，避免和文件大小混淆。
	totals := []string{
		"汇总", "", "", "", strconv.FormatInt(sum.Reclaimable, 10), "", "", "", "", "", "", "", "", "", "",
		fmt.Sprintf("重复组 %d 个 / 成员 %d 个 / 重复文件 %d 个 / 扫描文件 %d 个（%d 字节）/ 跳过 %d 个",
			sum.Groups, sum.Members, sum.DuplicateFiles, sum.Files, sum.Bytes, sum.Skipped),
		"", "", "", "", "",
	}
	if err := w.Write(totals); err != nil {
		return err
	}
	for i, g := range result.Groups {
		groupNo := strconv.Itoa(i + 1)
		groupRow := []string{
			"分组", groupNo, groupKindLabel(g.Kind), keepPolicyLabel(sum.KeepPolicy),
			strconv.FormatInt(g.Reclaimable, 10), "", "", "", "", "", "", "", "", "", "",
			g.Keep, filepath.Base(g.Keep), "", "", "", "",
		}
		if err := w.Write(groupRow); err != nil {
			return err
		}
		for _, m := range g.Members {
			row := []string{
				"文件", groupNo, groupKindLabel(g.Kind), keepPolicyLabel(sum.KeepPolicy), "",
				"", "", "", "", "", "", "", "", "", "",
				m.Path, filepath.Base(m.Path), strconv.FormatInt(m.Size, 10), localModTime(m.ModTime),
				yesNo(m.Keep), yesNo(m.Deletable),
			}
			if err := w.Write(row); err != nil {
				return err
			}
		}
	}
	w.Flush()
	if err := w.Error(); err != nil {
		return err
	}
	// UTF-8 BOM：Excel 直接双击打开时靠它识别编码，否则中文会乱码。
	return os.WriteFile(path, []byte("\ufeff"+buf.String()), 0o644)
}

// ---- HTML ----

func writeExportHTML(path string, result *model.Result, sum exportSummary) error {
	var b strings.Builder
	b.WriteString(`<!DOCTYPE html>
<html lang="zh-CN">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>查重结果报告</title>
<style>
:root{color-scheme:light}
body{font-family:"Microsoft YaHei","PingFang SC",system-ui,sans-serif;margin:0;padding:24px;background:#f6f7f9;color:#1f2328;line-height:1.6}
h1{font-size:22px;margin:0 0 4px}
h2{font-size:17px;margin:24px 0 8px;padding-bottom:6px;border-bottom:1px solid #e3e6ea}
.meta{color:#5b6573;font-size:13px;margin-bottom:16px}
section{background:#fff;border:1px solid #e3e6ea;border-radius:10px;padding:16px;margin-bottom:16px}
table{border-collapse:collapse;width:100%;font-size:13px}
th,td{border:1px solid #e3e6ea;padding:6px 8px;text-align:left;vertical-align:top}
th{background:#f1f3f6;font-weight:600;white-space:nowrap}
td.num{text-align:right;white-space:nowrap}
tr.keep td{background:#eef8ee}
tr.del td{background:#fdf1f1}
.tag{display:inline-block;padding:1px 6px;border-radius:4px;font-size:12px}
.tag.keep{background:#d7f0d7;color:#1d6b1d}
.tag.del{background:#f8d7d7;color:#8a1f1f}
.kv{display:grid;grid-template-columns:140px 1fr;gap:2px 12px;font-size:13px}
.kv dt{color:#5b6573}
.kv dd{margin:0;word-break:break-all}
footer{color:#8a93a0;font-size:12px;margin-top:24px}
</style>
</head>
<body>
`)
	fmt.Fprintf(&b, "<h1>查重结果报告</h1>\n")
	fmt.Fprintf(&b, `<p class="meta">导出时间 %s · 工具版本 v%s · 扫描开始 %s · 扫描结束 %s · 耗时 %s</p>`+"\n",
		esc(sum.GeneratedAt.Local().Format("2006-01-02 15:04:05")), esc(appVersion),
		esc(sum.ScanStarted.Local().Format("2006-01-02 15:04:05")), esc(sum.ScanFinished), esc(sum.Elapsed))

	b.WriteString(`<section><h2>扫描参数</h2><dl class="kv">`)
	kv := func(k, v string) {
		fmt.Fprintf(&b, "<dt>%s</dt><dd>%s</dd>", esc(k), esc(v))
	}
	kv("扫描根目录", sum.Root)
	kv("保留策略", keepPolicyLabel(sum.KeepPolicy))
	if sum.SimilarImage {
		kv("图片保留策略", keepPolicyLabel(sum.ImageKeep))
	}
	kv("最小文件大小", fmt.Sprintf("%d 字节（%s）", sum.MinSize, humanBytes(sum.MinSize)))
	kv("包含隐藏/系统文件", yesNo(sum.IncludeHidden))
	kv("相似图片", fmt.Sprintf("%s（阈值 %d）", yesNo(sum.SimilarImage), sum.ImageThreshold))
	kv("相似音频", fmt.Sprintf("%s（容差 %d 秒）", yesNo(sum.SimilarAudio), sum.AudioTolerance))
	excludes := strings.Join(sum.Excludes, " ")
	if excludes == "" {
		excludes = "（无）"
	}
	kv("排除项", excludes)
	b.WriteString("</dl></section>\n")

	b.WriteString(`<section><h2>汇总</h2><dl class="kv">`)
	kv("重复组", strconv.Itoa(sum.Groups))
	kv("组内成员", strconv.Itoa(sum.Members))
	kv("重复文件（非保留成员）", strconv.FormatInt(sum.DuplicateFiles, 10))
	kv("可回收空间合计", fmt.Sprintf("%s（%d 字节）", humanBytes(sum.Reclaimable), sum.Reclaimable))
	kv("扫描文件数", strconv.FormatInt(sum.Files, 10))
	kv("扫描字节数", fmt.Sprintf("%s（%d 字节）", humanBytes(sum.Bytes), sum.Bytes))
	kv("跳过项", strconv.FormatInt(sum.Skipped, 10))
	b.WriteString("</dl></section>\n")

	for i, g := range result.Groups {
		fmt.Fprintf(&b, "<section><h2>第 %d 组 · %s</h2>\n", i+1, esc(groupKindLabel(g.Kind)))
		fmt.Fprintf(&b, `<p class="meta">保留策略 %s · 保留项 %s · 本组可回收 %s（%d 字节）</p>`+"\n",
			esc(keepPolicyLabel(sum.KeepPolicy)), esc(g.Keep), esc(humanBytes(g.Reclaimable)), g.Reclaimable)
		b.WriteString(`<table><thead><tr><th>状态</th><th>文件名</th><th>大小</th><th>修改时间</th><th>完整路径</th></tr></thead><tbody>` + "\n")
		for _, m := range g.Members {
			cls, tag := "del", "待删除"
			if m.Keep {
				cls, tag = "keep", "保留"
			}
			fmt.Fprintf(&b, `<tr class="%s"><td><span class="tag %s">%s</span></td><td>%s</td><td class="num">%s</td><td>%s</td><td>%s</td></tr>`+"\n",
				cls, cls, tag, esc(filepath.Base(m.Path)), esc(humanBytes(m.Size)),
				esc(localModTime(m.ModTime)), esc(m.Path))
		}
		b.WriteString("</tbody></table></section>\n")
	}

	fmt.Fprintf(&b, `<footer>本报告由%s v%s 生成；以 dedup-report- 开头的 .html/.csv 会被本工具后续扫描自动忽略，不会出现在查重结果中。</footer>`+"\n", esc(brand.DisplayName), esc(appVersion))
	b.WriteString("</body>\n</html>\n")
	return os.WriteFile(path, []byte(b.String()), 0o644)
}

func esc(s string) string { return html.EscapeString(s) }
