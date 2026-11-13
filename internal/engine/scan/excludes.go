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

// excluded 判断文件是否命中排除项。语义与 C# A/Y.cs#A_m_1 一致：
//   - 含分隔符或盘符的项按绝对/相对路径前缀匹配（忽略大小写）；
//   - 纯名称项只与文件的父目录链比较（忽略大小写），不匹配文件名。
func excluded(path string, patterns []string) bool {
	if len(patterns) == 0 {
		return false
	}
	slashPath := strings.ToLower(strings.ReplaceAll(path, `\`, "/"))
	dir := strings.ToLower(strings.ReplaceAll(filepath.Dir(path), `\`, "/"))
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
