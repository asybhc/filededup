package organize

// 本文件把命令行/配置里的字符串映射为整理器枚举，供 CLI 与对拍工具共用，
// 避免两侧对同一份字符串产生不同解释。

import "strings"

// ParseSteps 解析步骤列表（逗号/分号/空白分隔）。
// 别名与 C# 诊断入口一致：prefix/suffix/special/readonly/content/image/audio/organize/empty。
func ParseSteps(s string) StepSwitches {
	set := map[string]bool{}
	for _, p := range strings.FieldsFunc(s, func(r rune) bool {
		return r == ',' || r == ';' || r == ' ' || r == '\t'
	}) {
		if p = strings.TrimSpace(p); p != "" {
			set[strings.ToLower(p)] = true
		}
	}
	return StepSwitches{
		RenamePrefix:  set["prefix"],
		RenameSuffix:  set["suffix"],
		SpecialDelete: set["special"],
		Readonly:      set["readonly"],
		ContentDedup:  set["content"],
		ImageDedup:    set["image"],
		AudioDedup:    set["audio"],
		Organize:      set["organize"],
		EmptyDirs:     set["empty"],
	}
}

// ParseConflictPolicy 解析冲突策略；未知值按原版默认 overwrite。
func ParseConflictPolicy(s string) ConflictPolicy {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "skip":
		return ConflictSkip
	case "rename":
		return ConflictRename
	default:
		return ConflictOverwrite
	}
}

// ParseDeleteMode 解析删除方式；只有 trash 走回收站，其余按永久删除。
func ParseDeleteMode(s string) DeleteMode {
	if strings.EqualFold(strings.TrimSpace(s), "trash") {
		return DeleteTrash
	}
	return DeletePermanent
}

// ParseKeepRule 解析去重保留规则；未知值按原版默认“路径最短”。
func ParseKeepRule(s string) KeepRule {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "newest":
		return KeepNewest
	case "oldest":
		return KeepOldest
	case "largest":
		return KeepLargest
	case "nameshortest":
		return KeepShortestName
	default:
		return KeepShortestPath
	}
}

// ParseOrganizeMode 解析第五步整理方式；未知值按“按文件类型分类”。
func ParseOrganizeMode(s string) OrganizeMode {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "extension":
		return ModeExtension
	case "fixedfolder":
		return ModeFixedFolder
	case "keyword":
		return ModeKeyword
	case "namefeature":
		return ModeNameFeature
	default:
		return ModeCategory
	}
}

// ParseStrength 把垃圾识别强度限制在 0..2；越界按默认 1。
func ParseStrength(n int) int {
	if n < 0 || n > 2 {
		return 1
	}
	return n
}
