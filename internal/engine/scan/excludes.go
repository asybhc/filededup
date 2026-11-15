package scan

import (
	"path/filepath"
	"regexp"
	"strings"
)

// excludeSplit 对应 C# A_1.d_f_2 的拆分：[,，;；\s]+。
var excludeSplit = regexp.MustCompile(`[,，;；\s]+`)

// splitExcludes 拆分并清洗排除项，复刻 C# y.A_m_1 的前置处理。
func splitExcludes(raw []string) []string {
	var out []string
	for _, chunk := range raw {
		for _, part := range excludeSplit.Split(chunk, -1) {
			part = strings.TrimSpace(part)
			part = strings.TrimRight(part, `\/`)
			if part != "" {
				out = append(out, part)
			}
		}
	}
	return out
}

// isToolReportName 判断文件是否为本工具导出的查重报告。
// 这类文件由本工具写出，不应在后续扫描中被当成待处理文件（否则用户扫描
// 导出目录时，报告本身也会参与查重与删除）。
func isToolReportName(path string) bool {
	base := strings.ToLower(filepath.Base(path))
	if !strings.HasPrefix(base, "dedup-report-") {
		return false
	}
	return strings.HasSuffix(base, ".html") || strings.HasSuffix(base, ".csv")
}

// excluded 判断文件是否命中排除项。语义与 C# A/Y.cs#A_m_1 一致：
//   - 含分隔符或盘符的项按绝对/相对路径前缀匹配（忽略大小写）；
//   - 纯名称项只与文件的父目录链比较（忽略大小写），不匹配文件名。
func excluded(path string, patterns []string) bool {
	if len(patterns) == 0 {
		return false
	}
	slashPath := strings.ToLower(strings.ReplaceAll(path, `\`, "/"))
	dir := slashPath
	if i := strings.LastIndex(dir, "/"); i >= 0 {
		dir = dir[:i]
	} else {
		dir = ""
	}
	var comps []string
	if dir != "" && dir != "." {
		comps = strings.Split(strings.Trim(dir, "/"), "/")
	}
	for _, p := range patterns {
		if strings.ContainsAny(p, `\/`) || strings.Contains(p, ":") {
			want := strings.ToLower(strings.ReplaceAll(p, `\`, "/"))
			want = strings.TrimRight(want, "/")
			if slashPath == want || strings.HasPrefix(slashPath, want+"/") {
				return true
			}
			continue
		}
		want := strings.ToLower(p)
		for _, c := range comps {
			if c == want {
				return true
			}
		}
	}
	return false
}
