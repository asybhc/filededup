package organize

import (
	"regexp"
	"strings"
	"unicode"
)

// 本文件是 C# B.t 特殊文件/垃圾文件名判定引擎的 Go 纯函数移植。
// 逐段对照 rebuild\outputs\src\FileDedupTool\B\T.cs 与 B\R.cs，行号见各函数注释。
// 正则均按 C# 原文逐字符改写：.NET 的 \d/\w/\s 与 Go 的 ASCII 语义不同，
// 因此 \d 写作 \p{Nd}、\s 写作显式空白类 specialWS、\u4e00-\u9fff 写作 \x{4e00}-\x{9fff}；
// 原正则的 RegexOptions.IgnoreCase 用 (?i) 表达，CultureInvariant 视为不变区域设置。

// SpecialVerdict 对应 C# B.r 枚举（normal / garbage / suspect）。
type SpecialVerdict string

const (
	SpecialNormal  SpecialVerdict = "normal"
	SpecialGarbage SpecialVerdict = "garbage"
	SpecialSuspect SpecialVerdict = "suspect"
)

// SpecialOptions 对应 C# B.T（T.cs:15-24）。
type SpecialOptions struct {
	ProtectWords   string // T.A：用户保护词，按 [，,;；\s]+ 拆分后逐词做子串匹配
	Strength       int    // T.A_1：C# B.S 枚举，0=低 1=中 2=高
	LocalWhitelist bool   // T.A_2：是否启用本地白名单 B.U.A_m_1
}

// SpecialResult 对应 C# B.s（T.cs:608 的返回值）。
type SpecialResult struct {
	Verdict    SpecialVerdict
	Suggestion string // C# s.A_1
	Reason     string // C# s.a
	Score      int    // C# s.A_2
	HardRule   bool   // C# s.A_3
	Normalized string // C# s.B：名称归一后的文本
}

// specialText 是一条判定结论，对应 C# 返回的 (建议 getter 明文, 原因 getter 明文)。
type specialText struct {
	suggestion string
	reason     string
}

// 文案取自 C# 资源字符串表，与 B\T.cs 中各分支引用的 getter 一一对应。
var (
	textProtectEmpty   = specialText{"PROTECT_EMPTY", "剥掉标记后为空"}       // T.cs:251 ael/xN
	textWhitelist      = specialText{"WHITELIST", "本地白名单"}             // T.cs:255 aeM/xO
	textProtectUser    = specialText{"PROTECT_USER", "用户保护词"}          // T.cs:259 aem/xP
	textProtectDevice  = specialText{"PROTECT_DEVICE", "设备/软件编号前缀"}    // T.cs:263 aeN/xR
	textProtectDate    = specialText{"PROTECT_DATE", "日期或年份"}          // T.cs:267 aen/xS
	textProtectChinese = specialText{"PROTECT_CHINESE", "纯中文标题"}       // T.cs:271 aeO/xT
	textProtectUnit    = specialText{"PROTECT_UNIT", "数字+量词/单位"}       // T.cs:275 aeo/xU
	textProtectShort   = specialText{"PROTECT_SHORT", "长度不超过 3 个字符"}   // T.cs:279 aeP/xQ
	textProtectWord    = specialText{"PROTECT_WORD", "常见英文词/缩写"}       // T.cs:286 aeQ/xV
	textAllDigits      = specialText{"ALL_DIGITS", "全数字文件名"}           // T.cs:326 aeR/xW
	textDigitsHanzi2   = specialText{"DIGITS_HANZI2", "数字+2汉字组合"}      // T.cs:334 aer/xX
	textAlnumHanzi2    = specialText{"ALNUM_HANZI2", "字母+数字+2汉字组合"}    // T.cs:342 aeS/xY
	textHanziInside    = specialText{"HANZI_INSIDE", "汉字夹在乱码字母数字中"}    // T.cs:350 aes/xZ
	textOddSymbol      = specialText{"ODD_SYMBOL", "含异常符号的乱码名"}        // T.cs:358 aeT/YA
	textMeaningless    = specialText{"MEANINGLESS_ALNUM", "无意义字母数字组合"} // T.cs:366 aet/YB
	textRandomCase     = specialText{"RANDOM_CASE", "随机大小写字母串"}        // T.cs:374 aeU/YC
	textNoVowel        = specialText{"NO_VOWEL", "无元音乱码字母串"}           // T.cs:382 aeu/YD
	textLowVowelUpper  = specialText{"LOW_VOWEL_UPPER", "低元音大写随机串"}    // T.cs:390 aeV/YE
	textScoreHigh      = specialText{"SCORE_HIGH", "随机字符打分过高"}         // T.cs:638 aeX/YF
	textScoreMid       = specialText{"SCORE_MID", "疑似随机字符（需人工确认）"}     // T.cs:643 aex/YG
	textKeep           = specialText{"KEEP", "未发现明显垃圾特征"}              // T.cs:648 aeY/YH
)

// specialWSChars 是 .NET 正则 \s 的等价字符集合（不含方括号），供字符类内部拼接使用。
const specialWSChars = `\t\n\v\f\r \x{85}\p{Z}`

// specialWS 等价 .NET 正则的 \s：[\f\n\r\t\v\x85] 加 Unicode 空白类 Z。
const specialWS = `[` + specialWSChars + `]`

// specialDigit 等价 .NET 正则的 \d（Unicode Nd）。
const specialDigit = `\p{Nd}`

var (
	// T.cs:63 A_2：去掉【】/〖〗前缀。
	reSpecialPrefix = regexp.MustCompile(`^(?:【[^】]{1,20}】|〖[^〗]{1,20}〗)` + specialWS + `*`)
	// T.cs:65 a_1：去掉尾部 (1)/（1）/（副本）/- copy 等副本标记。
	reSpecialCopySuffix = regexp.MustCompile(`(?i)` + specialWS + `*(?:\(` + specialDigit + `{1,3}\)|（` + specialDigit + `{1,3}）|-` + specialWS + `*副本|_` + specialWS + `*副本|-` + specialWS + `*copy|_` + specialWS + `*copy)$`)
	// T.cs:67 B：设备/软件编号前缀（原文尾部 \b? 可省略，等价删除）。
	reSpecialDevice = regexp.MustCompile(`(?i)^(?:DSC|DSCN|IMG|IMGN|MVIMG|PXL|VID|Screenshot|Screen` + specialWS + `*Shot|WeChat|微信图片|QQ图片|QQ截图|截图)`)
	// T.cs:69 b：年月日日期。
	reSpecialDateYMD = regexp.MustCompile(`(19|20)` + specialDigit + `{2}[-_.]` + specialDigit + `{1,2}[-_.]` + specialDigit + `{1,2}`)
	// T.cs:71 C：8 位数字日期。
	reSpecialDate8 = regexp.MustCompile(`(19|20)` + specialDigit + `{6}`)
	// T.cs:73 c：四位年份。
	reSpecialYear = regexp.MustCompile(`^(19|20)` + specialDigit + `{2}$`)
	// T.cs:75 D：数字+量词/单位。
	reSpecialUnit = regexp.MustCompile(`^(?:第` + specialWS + `*` + specialDigit + `+` + specialWS + `*[章节集部篇页卷期版]|[共总]?` + specialDigit + `+` + specialWS + `*[集个页卷章篇节部期]|` + specialDigit + `{4}` + specialWS + `*年|` + specialDigit + `+` + specialWS + `*(?:月|日|号|分钟|小时|秒|米|公里|千克|公斤|克|元|万|KB|MB|GB|TB))`)
	// T.cs:77 d：纯中文。
	reSpecialChineseOnly = regexp.MustCompile(`^[\x{4e00}-\x{9fff}]+$`)
	// T.cs:281 aep：分词分隔符。
	reSpecialNonWord = regexp.MustCompile(`[^A-Za-z0-9\x{4e00}-\x{9fff}]+`)
	// T.cs:302 aeq：保护词分隔符。
	reSpecialProtectSplit = regexp.MustCompile(`[,，;；` + specialWSChars + `]+`)

	// R.cs:52 d：全数字。
	reSpecialAllDigits = regexp.MustCompile(`^` + specialDigit + `+$`)
	// R.cs:54 E：仅 ASCII 字母数字。
	reSpecialASCIIAlnum = regexp.MustCompile(`^[A-Za-z0-9]+$`)
	// R.cs:56 e：汉字。
	reSpecialHan = regexp.MustCompile(`[\x{4e00}-\x{9fff}]`)
	// R.cs:58 F：日期/时间戳样式。
	reSpecialDateLoose = regexp.MustCompile(`^` + specialDigit + `{4}[-_.]?` + specialDigit + `{1,2}[-_.]?` + specialDigit + `{1,2}$|^` + specialDigit + `{8}$|^` + specialDigit + `{14}$`)
	// R.cs:60 f：连续两个元音。
	reSpecialDoubleVowel = regexp.MustCompile(`(?i)[aeiou]{2}`)
	// R.cs:62 G：形如 20240102 的 8 位日期数字。
	reSpecialDate8Like = regexp.MustCompile(`(19|20)` + specialDigit + `{6}`)
	// R.cs:64 g：十六进制摘要。
	reSpecialHex = regexp.MustCompile(`(?i)^(?:[0-9a-f]{16}|[0-9a-f]{32}|[0-9a-f]{40}|[0-9a-f]{64})$`)
	// R.cs:66 H：字母前缀+长数字。
	reSpecialWordDigits = regexp.MustCompile(`(?i)^[A-Za-z]{2,6}` + specialDigit + `{3,}$`)
	// R.cs:70 I：全无元音字母。
	reSpecialNoVowelLetters = regexp.MustCompile(`(?i)^[b-df-hj-np-tv-xz]+$`)
	// R.cs:466 aEX：纯 ASCII 字母。
	reSpecialASCIILetters = regexp.MustCompile(`^[A-Za-z]+$`)
	// T.cs:524 aeW：非字母（用于打分前的去符号）。
	reSpecialNonLetter = regexp.MustCompile(`[^A-Za-z]`)
)

// R.cs:72 aEw：判定“异常符号”时放行的符号集合。
const specialAllowedSymbols = " \t._-+&=#@!,'’~$%^()（）[]【】《》{}，。、;；:："

var (
	// specialWordSet 对应 T.cs:79 的 HashSet<string>(StringComparer.OrdinalIgnoreCase)。
	specialWordSet = specialBuildSet(specialWordList)
	// specialSyllableSet 对应 T.cs:215 的 HashSet<string>(StringComparer.OrdinalIgnoreCase)。
	specialSyllableSet = specialBuildSet(specialSyllables)
)

// specialBuildSet 构造 OrdinalIgnoreCase 语义的字符串集合（统一按小写存放）。
func specialBuildSet(items []string) map[string]struct{} {
	set := make(map[string]struct{}, len(items))
	for _, item := range items {
		set[strings.ToLower(item)] = struct{}{}
	}
	return set
}

// specialWordsContains 对应 T.cs 中 A_3.Contains(text)（OrdinalIgnoreCase）。
func specialWordsContains(s string) bool {
	_, ok := specialWordSet[strings.ToLower(s)]
	return ok
}

// specialLocalWhitelist 对应 C# B.U 读取的本地“排除列表.json”（B\U.cs:53、110）。
// 该文件位于 %APPDATA%\<应用>\排除列表.json，属于运行期本地数据而非编译期常量，
// 无法随纯函数层移植；这里以可注入列表等价替代，默认空列表与“本机不存在该文件”一致。
var specialLocalWhitelist []string

// SetSpecialLocalWhitelist 注入本地白名单条目；传 nil 表示空列表。
func SetSpecialLocalWhitelist(items []string) {
	specialLocalWhitelist = items
}

// specialUTF16Len 返回 C# string.Length 等价的 UTF-16 码元个数（增补平面字符计 2）。
func specialUTF16Len(s string) int {
	n := 0
	for _, r := range s {
		if r > 0xFFFF {
			n += 2
		} else {
			n++
		}
	}
	return n
}

// specialHanziCount 对应 R.cs:78 A_m_1：统计汉字个数。
func specialHanziCount(s string) int {
	n := 0
	for _, r := range s {
		if r >= 0x4E00 && r <= 0x9FFF {
			n++
		}
	}
	return n
}

// specialContainsFold 近似 C# 的 IndexOf(..., StringComparison.OrdinalIgnoreCase) >= 0。
func specialContainsFold(haystack, needle string) bool {
	if needle == "" {
		return true
	}
	h := []rune(haystack)
	n := []rune(needle)
	if len(n) > len(h) {
		return false
	}
	for i := 0; i+len(n) <= len(h); i++ {
		match := true
		for j := 0; j < len(n); j++ {
			if unicode.ToLower(h[i+j]) != unicode.ToLower(n[j]) {
				match = false
				break
			}
		}
		if match {
			return true
		}
	}
	return false
}

// normalizeSpecialStem 对应 T.cs:217 A_m_1(string)：名称归一。
func normalizeSpecialStem(stem string) string {
	if stem == "" {
		return ""
	}
	text := strings.TrimSpace(stem)
	for i := 0; i < 8; i++ { // T.cs:224-233
		loc := reSpecialPrefix.FindStringIndex(text)
		if loc == nil {
			break
		}
		text = strings.TrimLeftFunc(text[loc[1]:], unicode.IsSpace)
	}
	for i := 0; i < 8; i++ { // T.cs:234-243
		next := strings.TrimRightFunc(reSpecialCopySuffix.ReplaceAllString(text, ""), unicode.IsSpace)
		if next == "" || next == text {
			break
		}
		text = next
	}
	return strings.TrimSpace(text) // T.cs:244
}

// specialProtect 对应 T.cs:247 A_m_1(string, T)：保护层 1-9。
func specialProtect(text string, opt SpecialOptions) *specialText {
	if len(text) == 0 { // T.cs:249
		return &textProtectEmpty
	}
	if opt.LocalWhitelist && specialLocalWhitelisted(text) { // T.cs:253
		return &textWhitelist
	}
	if specialMatchUserWords(text, opt.ProtectWords) { // T.cs:257
		return &textProtectUser
	}
	if reSpecialDevice.MatchString(text) { // T.cs:261
		return &textProtectDevice
	}
	if reSpecialDateYMD.MatchString(text) || reSpecialDate8.MatchString(text) || reSpecialYear.MatchString(text) { // T.cs:265
		return &textProtectDate
	}
	if reSpecialChineseOnly.MatchString(text) && specialUTF16Len(text) >= 3 { // T.cs:269
		return &textProtectChinese
	}
	if reSpecialUnit.MatchString(text) { // T.cs:273
		return &textProtectUnit
	}
	if specialUTF16Len(text) <= 3 { // T.cs:277
		return &textProtectShort
	}
	for _, token := range reSpecialNonWord.Split(text, -1) { // T.cs:281-288
		if specialUTF16Len(token) >= 3 && specialWordsContains(token) {
			return &textProtectWord
		}
	}
	if specialWordsContains(text) { // T.cs:289
		return &textProtectWord
	}
	return nil
}

// specialMatchUserWords 对应 T.cs:296 A_m_1(string, string)：用户保护词子串匹配。
func specialMatchUserWords(text, words string) bool {
	if strings.TrimSpace(words) == "" { // T.cs:298
		return false
	}
	for _, part := range reSpecialProtectSplit.Split(words, -1) { // T.cs:302-305
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		if specialContainsFold(text, part) { // T.cs:308
			return true
		}
	}
	return false
}

// specialLocalWhitelisted 对应 B\U.cs:110 A_m_1：本地白名单命中判定。
func specialLocalWhitelisted(text string) bool {
	if text == "" {
		return false
	}
	norm := normalizeSpecialStem(text)
	for _, item := range specialLocalWhitelist {
		if strings.EqualFold(normalizeSpecialStem(item), norm) {
			return true
		}
	}
	return false
}

// specialHardRule 对应 T.cs:316 a_m_1(string)：9 条硬规则及其文案。
func specialHardRule(text string) *specialText {
	if text == "" {
		return nil
	}
	if specialAllDigits(text) && !specialDateLoose(text) { // T.cs:322
		return &textAllDigits
	}
	if specialDigitsHanzi2(text) { // T.cs:330
		return &textDigitsHanzi2
	}
	if specialAlnumHanzi2(text) { // T.cs:338
		return &textAlnumHanzi2
	}
	if specialHanziInside(text) { // T.cs:346
		return &textHanziInside
	}
	if specialOddSymbol(text) { // T.cs:354
		return &textOddSymbol
	}
	if specialMeaninglessAlnum(text) { // T.cs:362
		return &textMeaningless
	}
	if specialRandomCase(text) { // T.cs:370
		return &textRandomCase
	}
	if specialNoVowel(text) { // T.cs:378
		return &textNoVowel
	}
	if specialLowVowelUpper(text) { // T.cs:386
		return &textLowVowelUpper
	}
	return nil
}

// specialAllDigits 对应 R.cs:212 b_m_1。
func specialAllDigits(s string) bool {
	if s == "" {
		return false
	}
	return reSpecialAllDigits.MatchString(s)
}

// specialDateLoose 对应 R.cs 中 F 正则（aeI）。
func specialDateLoose(s string) bool {
	return reSpecialDateLoose.MatchString(s)
}

// specialAlnumHanzi2 对应 R.cs:221 C_m_1：字母+数字+2 个汉字。
func specialAlnumHanzi2(s string) bool {
	if s == "" { // R.cs:223
		return false
	}
	if specialHanziCount(s) != 2 { // R.cs:227
		return false
	}
	rest := reSpecialHan.ReplaceAllString(s, "")
	if rest == "" || !reSpecialASCIIAlnum.MatchString(rest) { // R.cs:232
		return false
	}
	hasLetter := false
	hasDigit := false
	for _, r := range rest {
		if unicode.IsLetter(r) {
			hasLetter = true
		} else if unicode.IsDigit(r) {
			hasDigit = true
		}
	}
	return hasLetter && hasDigit // R.cs:250
}

// specialDigitsHanzi2 对应 R.cs:253 c_m_1：数字+2 个汉字。
func specialDigitsHanzi2(s string) bool {
	if s == "" { // R.cs:255
		return false
	}
	if specialHanziCount(s) != 2 { // R.cs:259
		return false
	}
	rest := reSpecialHan.ReplaceAllString(s, "")
	if specialUTF16Len(rest) < 3 || specialUTF16Len(rest) != specialUTF16Len(s)-2 { // R.cs:264
		return false
	}
	for _, r := range rest { // R.cs:269-275
		if !unicode.IsDigit(r) {
			return false
		}
	}
	// R.cs:276 int.TryParse：.NET 只识别 ASCII 数字，全角数字解析失败，故此断言等价实现。
	if specialUTF16Len(rest) == 4 {
		if v, ok := specialParseIntASCII(rest); ok && v >= 1900 && v <= 2099 {
			return false
		}
	}
	return true
}

// specialParseIntASCII 模拟 int.TryParse 对纯数字串的解析（仅 ASCII 数字）。
func specialParseIntASCII(s string) (int, bool) {
	if s == "" {
		return 0, false
	}
	v := 0
	for _, r := range s {
		if r < '0' || r > '9' {
			return 0, false
		}
		v = v*10 + int(r-'0')
		if v > 1<<31-1 {
			return 0, false
		}
	}
	return v, true
}

// specialHanziInside 对应 R.cs:283 D_m_1：汉字夹在字母数字中。
func specialHanziInside(s string) bool {
	if s == "" { // R.cs:285
		return false
	}
	n := specialHanziCount(s)
	if n < 1 || n > 2 { // R.cs:290
		return false
	}
	first := -1
	last := -1
	u16 := 0
	for _, r := range s { // R.cs:296-306（按 UTF-16 下标比较）
		if r >= 0x4E00 && r <= 0x9FFF {
			if first < 0 {
				first = u16
			}
			last = u16
		}
		if r > 0xFFFF {
			u16 += 2
		} else {
			u16++
		}
	}
	if first <= 0 || last >= u16-1 { // R.cs:307
		return false
	}
	return specialMeaninglessAlnum(reSpecialHan.ReplaceAllString(s, "")) // R.cs:311
}

// specialOddSymbol 对应 R.cs:314 d_m_1：含异常符号的乱码名。
func specialOddSymbol(s string) bool {
	if s == "" || specialUTF16Len(s) < 4 { // R.cs:316
		return false
	}
	found := false
	for _, r := range s { // R.cs:321-328
		if !unicode.IsLetter(r) && !unicode.IsDigit(r) && !strings.ContainsRune(specialAllowedSymbols, r) {
			found = true
			break
		}
	}
	if !found { // R.cs:329
		return false
	}
	if specialHanziCount(s) > 2 { // R.cs:333
		return false
	}
	return true
}

// specialMeaninglessAlnum 对应 R.cs:340 E_m_1：无意义字母数字组合。
func specialMeaninglessAlnum(s string) bool {
	if s == "" { // R.cs:342
		return false
	}
	n := specialUTF16Len(s)
	if n < 6 || n > 64 { // R.cs:346
		return false
	}
	if !reSpecialASCIIAlnum.MatchString(s) { // R.cs:350
		return false
	}
	if reSpecialAllDigits.MatchString(s) { // R.cs:354
		return false
	}
	hasLetter := false
	hasDigit := false
	for _, r := range s { // R.cs:360-370
		if unicode.IsDigit(r) {
			hasDigit = true
		} else {
			hasLetter = true
		}
	}
	if !hasLetter || !hasDigit { // R.cs:371
		return false
	}
	if reSpecialHex.MatchString(s) { // R.cs:375
		return true
	}
	if reSpecialDateLoose.MatchString(s) { // R.cs:379
		return false
	}
	if reSpecialDate8Like.MatchString(s) { // R.cs:383
		return false
	}
	if reSpecialWordDigits.MatchString(s) { // R.cs:387
		return false
	}
	score := 0
	runs := specialDigitRuns(s) // R.cs:392
	if runs >= 4 {
		score += 2
	} else if runs == 3 {
		score++
	}
	digits := 0
	letters := 0
	vowels := 0
	upperAfterFirst := 0
	for i, r := range s { // R.cs:405-422
		if unicode.IsDigit(r) {
			digits++
			continue
		}
		letters++
		if strings.ContainsRune("aeiou", unicode.ToLower(r)) { // R.cs:414 h 正则
			vowels++
		}
		if i > 0 && unicode.IsUpper(r) {
			upperAfterFirst++
		}
	}
	if letters < 4 || digits < 2 { // R.cs:423
		return false
	}
	digitRatio := float64(digits) / float64(n)       // R.cs:427
	vowelRatio := float64(vowels) / float64(letters) // R.cs:428
	if vowelRatio < 0.2 {
		score += 2
	} else if vowelRatio < 0.3 {
		score++
	}
	if upperAfterFirst >= 2 { // R.cs:437
		score++
	}
	if !reSpecialDoubleVowel.MatchString(s) && n >= 10 { // R.cs:441
		score++
	}
	if digitRatio > 0.3 { // R.cs:445
		score++
	}
	if score >= 4 { // R.cs:449
		return true
	}
	if score >= 3 { // R.cs:453
		return digitRatio >= 0.3
	}
	return false
}

// specialRandomCase 对应 R.cs:509 f_m_1：随机大小写字母串。
func specialRandomCase(s string) bool {
	n := specialUTF16Len(s)
	if s == "" || n < 6 || n > 40 { // R.cs:511
		return false
	}
	if !reSpecialASCIILetters.MatchString(s) { // R.cs:515
		return false
	}
	transitions := 0
	run := 1
	maxRun := 1
	upper := 0
	runes := []rune(s)
	for i, r := range runes { // R.cs:523-544
		isUpper := unicode.IsUpper(r)
		if isUpper {
			upper++
		}
		if i == 0 {
			continue
		}
		if isUpper != unicode.IsUpper(runes[i-1]) {
			transitions++
			run = 1
			continue
		}
		run++
		if run > maxRun {
			maxRun = run
		}
	}
	if upper >= 2 && transitions >= 4 { // R.cs:545
		return maxRun <= 2
	}
	return false
}

// specialNoVowel 对应 R.cs:500 F_m_1：无元音乱码字母串。
func specialNoVowel(s string) bool {
	n := specialUTF16Len(s)
	if s == "" || n < 8 || n > 40 { // R.cs:502
		return false
	}
	return reSpecialNoVowelLetters.MatchString(strings.ToLower(s)) // R.cs:506
}

// specialLowVowelUpper 对应 R.cs:460 e_m_1：低元音大写随机串。
func specialLowVowelUpper(s string) bool {
	n := specialUTF16Len(s)
	if s == "" || n < 6 || n > 20 { // R.cs:462
		return false
	}
	if !reSpecialASCIILetters.MatchString(s) { // R.cs:466
		return false
	}
	upper := 0
	others := 0
	vowels := 0
	for _, r := range s { // R.cs:473-488
		if unicode.IsUpper(r) {
			upper++
		} else {
			others++
		}
		if strings.ContainsRune("aeiou", unicode.ToLower(r)) {
			vowels++
		}
	}
	if others == 0 { // R.cs:489
		return false
	}
	if upper*100 < n*60 { // R.cs:493
		return false
	}
	return vowels*4 <= n // R.cs:497
}

// specialDigitRuns 对应 R.cs:662 i(string)：数字段个数。
func specialDigitRuns(s string) int {
	n := 0
	prevDigit := false
	for i, r := range s {
		digit := unicode.IsDigit(r)
		if i == 0 || digit != prevDigit {
			n++
		}
		prevDigit = digit
	}
	return n
}

// scoreSpecialRandom 对应 T.cs:397 B_m_1：随机字符打分（0-10）。
func scoreSpecialRandom(s string) int {
	s = strings.TrimSpace(s)    // T.cs:403
	if specialUTF16Len(s) < 4 { // T.cs:404
		return 0
	}
	if !reSpecialASCIIAlnum.MatchString(s) { // T.cs:408
		return 0
	}
	num := 0  // 非数字字符数
	num2 := 0 // 数字字符数
	num3 := 0 // 元音字符数
	num5 := 0 // 大小写切换次数
	num6 := 1 // 最长同大小写连续段
	num7 := 1 // 当前同大小写连续段
	num8 := 0 // 最长非元音连续段
	num9 := 0 // 当前非元音连续段
	distinct := make(map[rune]struct{})
	prevUpper := false
	prevDigit := false
	digitRuns := 0
	runes := []rune(s)
	for i, r := range runes { // T.cs:425-477
		distinct[r] = struct{}{}
		isDigit := unicode.IsDigit(r)
		if i == 0 || isDigit != prevDigit {
			digitRuns++
			prevDigit = isDigit
		}
		if isDigit {
			num2++
			num9 = 0
			continue
		}
		num++
		if strings.ContainsRune("aeiouAEIOU", r) { // T.cs:442 aev
			num3++
			num9 = 0
		} else {
			num9++
			if num9 > num8 {
				num8 = num9
			}
		}
		isUpper := unicode.IsUpper(r)
		if i > 0 && unicode.IsLetter(runes[i-1]) { // T.cs:460
			if isUpper != prevUpper {
				num5++
				num7 = 1
			} else {
				num7++
				if num7 > num6 {
					num6 = num7
				}
			}
		}
		prevUpper = isUpper
	}
	if num < 3 { // T.cs:478
		return 0
	}
	score := 0
	if digitRuns >= 4 { // T.cs:483
		score += 2
	} else if digitRuns == 3 {
		score++
	}
	vowelRatio := float64(num3) / float64(maxInt(1, num))
	if vowelRatio < 0.1 { // T.cs:492
		score += 2
	} else if vowelRatio < 0.2 {
		score++
	}
	total := float64(specialUTF16Len(s))
	if num2 > 0 && float64(num2)/total > 0.35 { // T.cs:500
		score += 2
	} else if num2 > 0 && float64(num2)/total >= 0.25 {
		score++
	}
	if num5 >= 4 { // T.cs:508
		score += 2
	} else if num5 >= 2 {
		score++
	}
	if num8 >= 4 { // T.cs:516
		score++
	}
	if float64(len(distinct))/total >= 0.7 { // T.cs:520
		score++
	}
	if !specialLooksLikeWords(reSpecialNonLetter.ReplaceAllString(s, "")) { // T.cs:524
		score += 2
	}
	if score > 10 { // T.cs:528
		score = 10
	}
	return score
}

// specialLooksLikeWords 对应 T.cs:535 b_m_1：常见词/拼音音节判定。
func specialLooksLikeWords(s string) bool {
	if s == "" { // T.cs:537
		return true
	}
	lower := strings.ToLower(s)
	if _, ok := specialWordSet[lower]; ok { // T.cs:541
		return true
	}
	for _, word := range specialWordList { // T.cs:545-551
		if specialUTF16Len(word) >= 4 && specialContainsFold(s, word) {
			return true
		}
	}
	runes := []rune(lower)
	ok := make([]bool, len(runes)+1) // T.cs:553
	ok[0] = true
	for i := 1; i <= len(runes); i++ { // T.cs:555-574
		for k := 1; k <= 6 && k <= i; k++ {
			if !ok[i-k] {
				continue
			}
			if k == 1 && strings.ContainsRune(specialSingleVowels, runes[i-1]) {
				ok[i] = true
				break
			}
			if _, found := specialSyllableSet[string(runes[i-k:i])]; found {
				ok[i] = true
				break
			}
		}
	}
	return ok[len(runes)]
}

// specialThreshold 对应 T.cs:578 A_m_1(int, S)：阈值判定。
func specialThreshold(score, strength int) SpecialVerdict {
	low, high := 3, 7 // S.a（中）
	switch strength {
	case 0: // S.A（低）
		low, high = 2, 9
	case 2: // S.B（高）
		low, high = 2, 5
	}
	if score >= high { // T.cs:597
		return SpecialGarbage
	}
	if score <= low { // T.cs:601
		return SpecialNormal
	}
	return SpecialSuspect
}

// ClassifySpecial 对应 T.cs:608 a_m_1(string, T)：特殊文件/垃圾文件名判定入口。
func ClassifySpecial(stem string, opt SpecialOptions) SpecialResult {
	text := normalizeSpecialStem(stem) // T.cs:611
	res := SpecialResult{Normalized: text}
	if hit := specialProtect(text, opt); hit != nil { // T.cs:616-624
		res.Verdict = SpecialNormal
		res.Suggestion = hit.suggestion
		res.Reason = hit.reason
		res.Score = 0
		return res
	}
	if hit := specialHardRule(text); hit != nil { // T.cs:625-634
		res.Verdict = SpecialGarbage
		res.Suggestion = hit.suggestion
		res.Reason = hit.reason
		res.Score = 10
		res.HardRule = true
		return res
	}
	res.Score = scoreSpecialRandom(text) // T.cs:635
	res.Verdict = specialThreshold(res.Score, opt.Strength)
	switch res.Verdict {
	case SpecialGarbage:
		res.Suggestion = textScoreHigh.suggestion
		res.Reason = textScoreHigh.reason
	case SpecialSuspect:
		res.Suggestion = textScoreMid.suggestion
		res.Reason = textScoreMid.reason
	default:
		res.Suggestion = textKeep.suggestion
		res.Reason = textKeep.reason
	}
	return res
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
