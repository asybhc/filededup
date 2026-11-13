package model

import (
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
)

// ValidationError 汇总一次输入校验的全部问题，便于 API 层一次返回给前端。
type ValidationError struct {
	Issues []string
}

func (e *ValidationError) Error() string {
	return "model: invalid input: " + strings.Join(e.Issues, "; ")
}

// Validate 校验扫描请求。空 KeepPolicy 视为默认值，不做报错。
func (r *Request) Validate() error {
	var issues []string
	add := func(format string, args ...any) {
		issues = append(issues, fmt.Sprintf(format, args...))
	}

	if r.SchemaVersion != 0 && r.SchemaVersion != SchemaVersion {
		add("schemaVersion=%d 不受支持（当前 %d）", r.SchemaVersion, SchemaVersion)
	}
	if len(r.Roots) == 0 {
		add("roots 不能为空")
	}
	for i, root := range r.Roots {
		if strings.TrimSpace(root) == "" {
			add("roots[%d] 为空", i)
			continue
		}
		if !filepath.IsAbs(root) {
			add("roots[%d] 必须是绝对路径：%s", i, root)
		}
	}
	if r.MinSize < 0 {
		add("minSize 不能为负：%d", r.MinSize)
	}
	if r.Workers < 0 || r.Workers > 256 {
		add("workers 必须在 0..256：%d", r.Workers)
	}
	if r.KeepPolicy != "" && !scanKeepPolicies[r.KeepPolicy] {
		add("keepPolicy 不受支持：%s", r.KeepPolicy)
	}
	if r.ImageKeepPolicy != "" && !organizeKeepPolicies[r.ImageKeepPolicy] {
		add("imageKeepPolicy 不受支持：%s", r.ImageKeepPolicy)
	}
	if r.SimilarImage != nil {
		if r.SimilarImage.Threshold < 0 || r.SimilarImage.Threshold > 64 {
			add("similarImage.threshold 必须在 0..64：%d", r.SimilarImage.Threshold)
		}
		if r.SimilarImage.Enabled && r.QuickHash {
			add("similarImage 不能与 quickHash 同时启用：相似判定必须先有完整内容指纹")
		}
	}
	if r.SimilarAudio != nil {
		if r.SimilarAudio.Tolerance < 0 || r.SimilarAudio.Tolerance > 60 {
			add("similarAudio.tolerance 必须在 0..60：%d", r.SimilarAudio.Tolerance)
		}
	}
	exactOn := r.Exact
	imageOn := r.SimilarImage != nil && r.SimilarImage.Enabled
	audioOn := r.SimilarAudio != nil && r.SimilarAudio.Enabled
	if !exactOn && !imageOn && !audioOn {
		add("至少启用一种查重方式（exact / similarImage / similarAudio）")
	}

	if len(issues) > 0 {
		return &ValidationError{Issues: issues}
	}
	return nil
}

// Validate 校验整理请求。
func (r *OrganizeRequest) Validate() error {
	var issues []string
	add := func(format string, args ...any) {
		issues = append(issues, fmt.Sprintf(format, args...))
	}

	if r.SchemaVersion != 0 && r.SchemaVersion != SchemaVersion {
		add("schemaVersion=%d 不受支持（当前 %d）", r.SchemaVersion, SchemaVersion)
	}
	if strings.TrimSpace(r.Root) == "" {
		add("root 不能为空")
	} else if !filepath.IsAbs(r.Root) {
		add("root 必须是绝对路径：%s", r.Root)
	}
	if strings.TrimSpace(r.TargetDir) == "" {
		if r.Steps.Organize {
			add("已开启文件整理（steps.organize=true）时必须提供 targetDir")
		}
	} else if !filepath.IsAbs(r.TargetDir) {
		add("targetDir 必须是绝对路径：%s", r.TargetDir)
	}
	if r.MinSize < 0 {
		add("minSize 不能为负：%d", r.MinSize)
	}
	if r.Workers < 0 || r.Workers > 256 {
		add("workers 必须在 0..256：%d", r.Workers)
	}
	if r.ConflictPolicy != "" {
		switch r.ConflictPolicy {
		case ConflictOverwrite, ConflictSkip, ConflictRename:
		default:
			add("conflictPolicy 不受支持：%s", r.ConflictPolicy)
		}
	}
	if r.DeleteMode != "" {
		switch r.DeleteMode {
		case DeleteTrash, DeletePermanent:
		default:
			add("deleteMode 不受支持：%s", r.DeleteMode)
		}
	}
	if r.RecognizeStrength < 0 || r.RecognizeStrength > 2 {
		add("recognizeStrength 必须在 0..2：%d", r.RecognizeStrength)
	}
	if r.KeepPolicy != "" && !organizeKeepPolicies[r.KeepPolicy] {
		add("keepPolicy 不受支持：%s", r.KeepPolicy)
	}
	if r.ImageKeepPolicy != "" && !organizeKeepPolicies[r.ImageKeepPolicy] {
		add("imageKeepPolicy 不受支持：%s", r.ImageKeepPolicy)
	}
	if r.ImageThreshold < 0 || r.ImageThreshold > 64 {
		add("imageThreshold 必须在 0..64：%d", r.ImageThreshold)
	}
	if r.AudioToleranceSec < 0 || r.AudioToleranceSec > 60 {
		add("audioToleranceSec 必须在 0..60：%d", r.AudioToleranceSec)
	}

	switch r.Organize.Mode {
	case "", OrganizeByCategory, OrganizeByExtension, OrganizeToFolder:
	case OrganizeByNameKeyword:
		if strings.TrimSpace(r.Organize.Keyword) == "" {
			add("organize.mode=nameKeyword 时 keyword 不能为空")
		}
	case OrganizeByNameRegex:
		if strings.TrimSpace(r.Organize.Regex) == "" {
			add("organize.mode=nameRegex 时 regex 不能为空")
		} else if _, err := regexp.Compile(r.Organize.Regex); err != nil {
			add("organize.regex 非法：%v", err)
		}
	default:
		add("organize.mode 不受支持：%s", r.Organize.Mode)
	}

	enabled := 0
	for _, id := range StandaloneOrder {
		if r.Steps.Enabled(id) {
			enabled++
		}
	}
	if enabled == 0 {
		add("至少开启一个整理步骤")
	}

	if len(issues) > 0 {
		return &ValidationError{Issues: issues}
	}
	return nil
}

// Validate 校验计划的自洽性：序号递增、动作类型与状态合法。
func (p *Plan) Validate() error {
	var issues []string
	add := func(format string, args ...any) {
		issues = append(issues, fmt.Sprintf(format, args...))
	}
	validKind := func(k ActionKind) bool {
		switch k {
		case ActionRename, ActionMove, ActionDeleteTrash, ActionDeletePermanent,
			ActionHardlink, ActionClearReadonly, ActionMkdir, ActionRemoveDir:
			return true
		}
		return false
	}
	validStatus := func(s ActionStatus) bool {
		switch s {
		case StatusPlanned, StatusApplied, StatusSkipped, StatusFailed, StatusDryRun:
			return true
		}
		return false
	}
	prev := 0
	for i, a := range p.Actions {
		if a.Seq <= prev {
			add("actions[%d].seq=%d 未严格递增（上一个 %d）", i, a.Seq, prev)
		}
		prev = a.Seq
		if !validKind(a.Kind) {
			add("actions[%d].kind 不受支持：%s", i, a.Kind)
		}
		if !validStatus(a.Status) {
			add("actions[%d].status 不受支持：%s", i, a.Status)
		}
		if a.Src == "" {
			add("actions[%d].src 为空", i)
		}
		if (a.Kind == ActionRename || a.Kind == ActionMove || a.Kind == ActionHardlink) && a.Dst == "" {
			add("actions[%d].%s 缺少 dst", i, a.Kind)
		}
	}
	if len(issues) > 0 {
		return &ValidationError{Issues: issues}
	}
	return nil
}
