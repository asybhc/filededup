package organize

import (
	"regexp"
	"sort"
	"strings"
	"unicode/utf16"
)

// ---- 路径语义（对齐 .NET Path.GetFileName / GetExtension / GetFileNameWithoutExtension） ----

// WinBase 返回路径最后一段，'/' 与 '\' 均视作分隔符。
func WinBase(p string) string {
	if i := strings.LastIndexAny(p, `/\`); i >= 0 {
		return p[i+1:]
	}
	return p
}

// WinExt 对齐 Path.GetExtension：尾部点是空扩展名，前导点算扩展名。
func WinExt(p string) string {
	name := WinBase(p)
	for i := len(name) - 1; i >= 0; i-- {
		c := name[i]
		if c == '.' {
			if i == len(name)-1 {
				return ""
			}
			return name[i:]
		}
		if c == '/' || c == '\\' {
			break
		}
	}
	return ""
}

// WinStem 对齐 Path.GetFileNameWithoutExtension：从最后一个 '.' 处截断，
// 因此 "trailingdot." → "trailingdot"、"a." → "a"、".." → "."、".gitignore" → ""。
func WinStem(p string) string {
	name := WinBase(p)
	if i := strings.LastIndexByte(name, '.'); i >= 0 {
		return name[:i]
	}
	return name
}

// JoinRel 以 '/' 拼接相对路径。
func JoinRel(dir, name string) string {
	if dir == "" {
		return name
	}
	dir = strings.TrimSuffix(dir, "/")
	return dir + "/" + name
}

// ParentRel 返回相对路径的父目录，顶层返回 ""。
func ParentRel(p string) string {
	if i := strings.LastIndex(p, "/"); i >= 0 {
		return p[:i]
	}
	return ""
}

// Fold 用于大小写不敏感比较的键。
func Fold(s string) string { return strings.ToLower(s) }

// SamePath 对齐 C# 的 OrdinalIgnoreCase 比较。
func SamePath(a, b string) bool { return strings.EqualFold(a, b) }

// ---- 重命名规则（B/R.cs L91-210） ----

var (
	prefixDateSep = regexp.MustCompile(`(?i)^\d{4}[-_.]\d{1,2}[-_.]\d{1,2}[\s_-]*`)
	prefixCopy1   = regexp.MustCompile(`(?i)^(copy|副本|备份|复件|复制|new|old|新建|最新)\s*\d{0,4}[\s._-]+`)
	prefixCopy2   = regexp.MustCompile(`(?i)^(copy|副本|备份|复件|复制|new|old)\s?\d{1,4}`)
	prefixBrack   = regexp.MustCompile(`(?i)^[\(（\[]\d{1,4}[\)）\]]\s*`)
	prefixDigit1  = regexp.MustCompile(`(?i)^\d{1,5}[\s._-]+`)
	prefixDigit2  = regexp.MustCompile(`(?i)^\d{1,5}\s+`)
	midBracket    = regexp.MustCompile(`(?i)\s*[\(（]\d{1,3}[\)）]\s*`)

	suffixRules = []*regexp.Regexp{
		regexp.MustCompile(`(?i)[\s_-]*\d{4}[-_.]\d{1,2}[-_.]\d{1,2}$`),
		regexp.MustCompile(`(?i)[\s_-]*\d{14}$`),
		regexp.MustCompile(`(?i)[\s_-]*\d{8}$`),
		regexp.MustCompile(`(?i)[\s._-]*[\(（\[]\d{1,3}[\)）\]]$`),
		regexp.MustCompile(`(?i)[\s._-]*[\(（](副本|复制|重复|备份|复件|copy|duplicate|backup|new|old|latest|final|version)\d{0,3}[\)）]$`),
		regexp.MustCompile(`(?i)[\s._-]*(副本|复制|重复|备份|复件)\d{0,3}$`),
		regexp.MustCompile(`(?i)[\s._-]*(copy|copy1|copy2|duplicate|backup|new|old|latest|final|version)\d{0,3}$`),
		regexp.MustCompile(`(?i)[\s._-]+\d{1,3}$`),
	}

	excludeSplit = regexp.MustCompile(`[,，;；\s]+`)
	numericOnly  = regexp.MustCompile(`^[0-9]{1,3}$`)
)

// CleanPrefix 对齐 B/R.cs L91 的 a_m_1：去前缀/中间序号。结果为空时回退原串。
func CleanPrefix(in string) string {
	if in == "" {
		return in
	}
	orig := in
	text := orig

	// A 日期前缀：依次尝试 3 条正则（日期分隔式 → 14 位 → 8 位），
	// Replace 一次后 Trim(' ','\t')；命中且非空即停。
	handled := false
	if loc := prefixDateSep.FindStringIndex(text); loc != nil {
		if t := trimSpaceTab(text[loc[1]:]); t != "" && t != text {
			text = t
			handled = true
		}
	}
	if !handled {
		if t, ok := stripDatePrefixDigits(text); ok {
			text = t
		}
	}

	// B 复制前缀（while）：优先 copy1，未命中才试 copy2。
	for {
		end := -1
		if loc := prefixCopy1.FindStringIndex(text); loc != nil {
			end = loc[1]
		} else if loc := prefixCopy2.FindStringIndex(text); loc != nil && (loc[1] >= len(text) || !isASCIIDigit(text[loc[1]])) {
			end = loc[1]
		}
		if end < 0 || end >= len(text) {
			break
		}
		t := trimStartName(text[end:])
		if t == "" {
			break
		}
		text = t
	}

	// C 开头括号数字：Replace 一次 + Trim(' ','\t')。
	if loc := prefixBrack.FindStringIndex(text); loc != nil {
		if t := trimSpaceTab(text[loc[1]:]); t != "" && t != text {
			text = t
		}
	}

	// D 前导数字（while）：优先 digit1（带分隔符），否则 digit2（纯空白）。
	for {
		end := -1
		if loc := prefixDigit1.FindStringIndex(text); loc != nil && loc[1] < len(text) {
			end = loc[1]
		} else if loc := prefixDigit2.FindStringIndex(text); loc != nil && loc[1] < len(text) {
			end = loc[1]
		}
		if end < 0 {
			break
		}
		t := trimStartName(text[end:])
		if t == "" {
			break
		}
		text = t
	}

	// E 中间序号（while）：优先 D 规则，未变化再去括号数字并 Trim(' ','\t')。
	for {
		next := stripMiddleNumber(text)
		if next == text {
			next = trimSpaceTab(midBracket.ReplaceAllString(text, ""))
		}
		if next == "" || next == text {
			break
		}
		text = next
	}

	text = strings.TrimSpace(text)
	if text == "" {
		return orig
	}
	return text
}

// stripDatePrefixDigits 复刻 ^\d{14}(?!\d)[\s_-]* 与 ^\d{8}(?!\d)[\s_-]* 的零宽断言，
// 命中后整体删除并 Trim(' ','\t')。
func stripDatePrefixDigits(text string) (string, bool) {
	for _, n := range []int{14, 8} {
		if len(text) < n {
			continue
		}
		digits := true
		for i := 0; i < n; i++ {
			if !isASCIIDigit(text[i]) {
				digits = false
				break
			}
		}
		if !digits {
			continue
		}
		if len(text) > n && isASCIIDigit(text[n]) {
			continue
		}
		rest := text[n:]
		rest = strings.TrimLeft(rest, " \t\n\v\f\r_-")
		t := trimSpaceTab(rest)
		if t != "" && t != text {
			return t, true
		}
	}
	return text, false
}

func isASCIIDigit(c byte) bool { return c >= '0' && c <= '9' }

// trimSpaceTab 对齐 C# 的 Trim(' ', '\t')。
func trimSpaceTab(s string) string { return strings.Trim(s, " \t") }

// trimStartName 对齐 C# 的 TrimStart(' ', '\t', '.', '_', '-')。
func trimStartName(s string) string { return strings.TrimLeft(s, " \t._-") }

func isWordRune(r rune) bool {
	return r < 128 && (r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9') ||
		(r >= 0x4e00 && r <= 0x9fff)
}

// stripMiddleNumber 对齐 aeC 的替换：x-3-y / x_3_y → x-y / x_y（替换所有出现）。
func stripMiddleNumber(s string) string {
	runes := []rune(s)
	var out []rune
	for i := 0; i < len(runes); {
		if i+4 < len(runes) && isWordRune(runes[i]) && (runes[i+1] == '-' || runes[i+1] == '_') {
			sep := runes[i+1]
			j := i + 2
			digits := 0
			for j < len(runes) && digits < 3 && runes[j] >= '0' && runes[j] <= '9' {
				j++
				digits++
			}
			if digits > 0 && j < len(runes) && runes[j] == sep && j+1 < len(runes) && isWordRune(runes[j+1]) {
				out = append(out, runes[i], sep, runes[j+1])
				i = j + 2
				continue
			}
		}
		out = append(out, runes[i])
		i++
	}
	return string(out)
}

// CleanSuffix 对齐 B/R.cs L177 的 B_m_1：最多 32 轮剥离后缀序号。
func CleanSuffix(in string) string {
	if in == "" {
		return in
	}
	orig := in
	text := in
	for round := 0; round < 32; round++ {
		before := text
		for _, re := range suffixRules {
			t := re.ReplaceAllString(text, "")
			t = strings.TrimRight(t, " \t._-")
			// 原版：命中但结果为空或未变化时不算命中，继续试下一条。
			if t != "" && t != text {
				text = t
				break
			}
		}
		if text == before {
			break
		}
	}
	text = strings.TrimSpace(text)
	if text != "" {
		return text
	}
	return orig
}

// ---- 目标名清洗（B/R.cs L552-629） ----

// SanitizeTargetName 对齐 g_m_1：只有括号对过多时才清洗主干，否则原样返回。
func SanitizeTargetName(name string) string {
	if name == "" {
		return name
	}
	stem := WinStem(name)
	ext := WinExt(name)
	folded := foldParens(stem)
	if folded == "" || folded == stem {
		return name
	}
	var b strings.Builder
	for _, r := range folded {
		if strings.ContainsRune(`\/:*?"<>|`, r) {
			b.WriteByte('_')
		} else {
			b.WriteRune(r)
		}
	}
	out := strings.TrimRight(strings.TrimSpace(b.String()), ". ")
	if out == "" {
		return name
	}
	if utf16Len(out) > 200 {
		out = trimUTF16(out, 200)
		out = strings.TrimRight(out, ". ")
		if out == "" {
			return name
		}
	}
	return out + ext
}

// foldParens 对齐 G_m_1：'(' 与 ')'（含全角）各出现 >=3 次时删除所有括号并 Trim。
func foldParens(s string) string {
	if s == "" {
		return s
	}
	open, closeN := 0, 0
	for _, r := range s {
		switch r {
		case '(', '（':
			open++
		case ')', '）':
			closeN++
		}
	}
	if open < 3 || closeN < 3 {
		return s
	}
	var b strings.Builder
	for _, r := range s {
		if r != '(' && r != ')' && r != '（' && r != '）' {
			b.WriteRune(r)
		}
	}
	out := strings.TrimSpace(b.String())
	if out == "" {
		return s
	}
	return out
}

func utf16Len(s string) int { return len(utf16.Encode([]rune(s))) }

func trimUTF16(s string, n int) string {
	runes := []rune(s)
	units := 0
	for i, r := range runes {
		w := 1
		if r > 0xffff {
			w = 2
		}
		if units+w > n {
			return string(runes[:i])
		}
		units += w
	}
	return s
}

// ---- 扩展名分类（A/T.cs L723-790） ----

type categoryGroup struct {
	Name string
	Exts []string
}

var compoundSuffixWhitelist = map[string]bool{
	"crdownload": true, "part": true, "partial": true, "download": true,
	"downloading": true, "!ut": true, "td": true, "bc!": true, "xltd": true,
	"baiduyun": true, "aria2": true, "opdownload": true, "tmp": true,
}

// CompoundExt 对齐 A/T.cs A_m_1：返回复合扩展名（小写），无扩展名返回 ""。
func CompoundExt(p string) string {
	name := strings.TrimSpace(WinBase(p))
	name = strings.TrimRight(name, ". ")
	parts := strings.Split(name, ".")
	if len(parts) < 2 {
		return ""
	}
	num := len(parts) - 1
	for num >= 1 {
		seg := parts[num]
		if compoundSuffixWhitelist[strings.ToLower(seg)] || numericOnly.MatchString(seg) {
			num--
			continue
		}
		break
	}
	if num < 1 {
		last := parts[len(parts)-1]
		if !numericOnly.MatchString(last) {
			return ""
		}
		return strings.ToLower(last)
	}
	ext := strings.ToLower(parts[num])
	if ext == "" {
		return ""
	}
	if num >= 2 && strings.EqualFold(parts[num-1], "tar") {
		return "tar." + ext
	}
	return ext
}

// Category 对齐 a/T.cs a_m_1：按扩展名归类，未知/无扩展名一律“其他”。
func Category(p string) string {
	ext := CompoundExt(p)
	if ext == "" {
		return "其他"
	}
	for _, g := range categoryTable {
		for _, e := range g.Exts {
			if strings.EqualFold(e, ext) {
				return g.Name
			}
		}
	}
	return "其他"
}

// ExtensionUpper 对齐 a/T.cs B：大写扩展名，无扩展名返回“无扩展名”。
func ExtensionUpper(p string) string {
	ext := CompoundExt(p)
	if ext == "" {
		return "无扩展名"
	}
	return strings.ToUpper(ext)
}

// ---- 扩展名集合 ----

var imageExts = map[string]bool{"jpg": true, "jpeg": true, "png": true, "gif": true, "bmp": true, "tif": true, "tiff": true, "webp": true}
var audioExts = map[string]bool{"mp3": true, "wav": true}

// IsImage 对齐 a/Q.cs A_m_1。
func IsImage(p string) bool {
	ext := strings.TrimPrefix(WinExt(p), ".")
	return ext != "" && imageExts[strings.ToLower(ext)]
}

// IsAudio 对齐 a/S.cs A_m_1。
func IsAudio(p string) bool {
	ext := strings.ToLower(WinExt(p))
	return ext == ".mp3" || ext == ".wav"
}

// ---- 保留规则（B/E.cs L1186-1230） ----

// KeepWinner 返回组内应保留的文件下标。
func KeepWinner(group []FileEntry, rule KeepRule) int {
	if len(group) == 0 {
		return -1
	}
	best := 0
	less := func(i, j int) bool { return false }
	switch rule {
	case KeepNewest:
		less = func(i, j int) bool {
			if !group[i].ModTime.Equal(group[j].ModTime) {
				return group[i].ModTime.After(group[j].ModTime)
			}
			return len(group[i].Path) < len(group[j].Path)
		}
	case KeepOldest:
		less = func(i, j int) bool {
			if !group[i].ModTime.Equal(group[j].ModTime) {
				return group[i].ModTime.Before(group[j].ModTime)
			}
			return len(group[i].Path) < len(group[j].Path)
		}
	case KeepLargest:
		less = func(i, j int) bool {
			if group[i].Size != group[j].Size {
				return group[i].Size > group[j].Size
			}
			return len(group[i].Path) < len(group[j].Path)
		}
	case KeepShortestName:
		less = func(i, j int) bool {
			li, lj := len(WinBase(group[i].Path)), len(WinBase(group[j].Path))
			if li != lj {
				return li < lj
			}
			return len(group[i].Path) < len(group[j].Path)
		}
	default:
		less = func(i, j int) bool { return len(group[i].Path) < len(group[j].Path) }
	}
	for i := range group {
		if less(i, best) {
			best = i
		}
	}
	return best
}

// SplitKeywords 对齐 B/E.cs L572：按 [,，;；\s]+ 拆分并去空。
func SplitKeywords(s string) []string {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	var out []string
	for _, part := range excludeSplit.Split(s, -1) {
		if part = strings.TrimSpace(part); part != "" {
			out = append(out, part)
		}
	}
	return out
}

// ContainsAnyFold 子串包含（大小写不敏感），用于排除词与关键字。
func ContainsAnyFold(s string, words []string) bool {
	for _, w := range words {
		if w != "" && strings.Contains(strings.ToLower(s), strings.ToLower(w)) {
			return true
		}
	}
	return false
}

// SortFilesNTFS 用“大写后比较”的近似 NTFS 顺序排序，保证规划可复现。
func SortFilesNTFS(files []FileEntry) {
	sort.SliceStable(files, func(i, j int) bool {
		a, b := strings.ToUpper(files[i].Path), strings.ToUpper(files[j].Path)
		if a != b {
			return a < b
		}
		return files[i].Path < files[j].Path
	})
}

// SortDirsNTFS 同 SortFilesNTFS，用于目录。
func SortDirsNTFS(dirs []string) {
	sort.SliceStable(dirs, func(i, j int) bool {
		a, b := strings.ToUpper(dirs[i]), strings.ToUpper(dirs[j])
		if a != b {
			return a < b
		}
		return dirs[i] < dirs[j]
	})
}
