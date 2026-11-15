//go:build desktop

package main

// 整理规则的导入 / 导出。
//
// 文件是带版本号的 JSON；导入严格校验：
//   - 顶层与 rules 内出现未知键 → 拒绝；
//   - 缺少/不支持 schemaVersion、kind 不匹配 → 拒绝；
//   - 枚举、数值范围、正则非法 → 拒绝，并一次列出全部问题。
//
// 导入只返回解析后的规则，不直接落盘；界面确认后走既有设置保存流程
// （保存失败时旧设置不受影响）。

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	wruntime "github.com/wailsapp/wails/v2/pkg/runtime"

	"filededup/internal/engine/model"
)

const (
	organizeRulesKind          = "filededup.organize.rules"
	organizeRulesSchemaVersion = 1
	organizeRulesMaxBytes      = 1 << 20 // 1 MiB
)

type organizeRulesFile struct {
	SchemaVersion int              `json:"schemaVersion"`
	Kind          string           `json:"kind"`
	Rules         OrganizeSettings `json:"rules"`
}

// OrganizeRulesExport 是导出结果；Canceled 表示用户取消了保存对话框。
type OrganizeRulesExport struct {
	Canceled bool   `json:"canceled"`
	Path     string `json:"path"`
	Bytes    int    `json:"bytes"`
}

// OrganizeRulesImport 是导入结果；Canceled 表示用户取消了打开对话框。
type OrganizeRulesImport struct {
	Canceled bool             `json:"canceled"`
	Path     string           `json:"path"`
	Rules    OrganizeSettings `json:"rules"`
}

var validOrganizeKeepPolicies = map[string]bool{
	"pathshortest": true, "newest": true, "oldest": true, "largest": true, "nameshortest": true,
}

// ExportOrganizeRules 把当前规则写入用户选择的文件。根目录不写入规则文件。
func (a *App) ExportOrganizeRules(rules OrganizeSettings) (OrganizeRulesExport, error) {
	if a.ctx == nil {
		return OrganizeRulesExport{}, errors.New("应用尚未完成初始化")
	}
	rules = normalizeImportedRules(rules)
	if err := validateOrganizeRules(rules); err != nil {
		return OrganizeRulesExport{}, err
	}
	path, err := wruntime.SaveFileDialog(a.ctx, wruntime.SaveDialogOptions{
		Title:           "导出整理规则",
		DefaultFilename: "filededup-organize-rules.json",
		Filters: []wruntime.FileFilter{
			{DisplayName: "整理规则 (*.json)", Pattern: "*.json"},
		},
	})
	if err != nil {
		return OrganizeRulesExport{}, fmt.Errorf("打开保存对话框失败：%w", err)
	}
	if strings.TrimSpace(path) == "" {
		return OrganizeRulesExport{Canceled: true}, nil
	}
	// 根目录是机器相关状态，不属于可复用规则。
	rules.Root = ""
	payload := organizeRulesFile{
		SchemaVersion: organizeRulesSchemaVersion,
		Kind:          organizeRulesKind,
		Rules:         rules,
	}
	raw, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return OrganizeRulesExport{}, err
	}
	raw = append(raw, '\n')
	if err := os.WriteFile(path, raw, 0o644); err != nil {
		return OrganizeRulesExport{}, fmt.Errorf("写入规则文件失败：%w", err)
	}
	return OrganizeRulesExport{Path: path, Bytes: len(raw)}, nil
}

// ImportOrganizeRules 读取并严格校验规则文件；失败时不改任何设置。
func (a *App) ImportOrganizeRules() (OrganizeRulesImport, error) {
	if a.ctx == nil {
		return OrganizeRulesImport{}, errors.New("应用尚未完成初始化")
	}
	path, err := wruntime.OpenFileDialog(a.ctx, wruntime.OpenDialogOptions{
		Title: "导入整理规则",
		Filters: []wruntime.FileFilter{
			{DisplayName: "整理规则 (*.json)", Pattern: "*.json"},
			{DisplayName: "所有文件", Pattern: "*"},
		},
	})
	if err != nil {
		return OrganizeRulesImport{}, fmt.Errorf("打开文件对话框失败：%w", err)
	}
	if strings.TrimSpace(path) == "" {
		return OrganizeRulesImport{Canceled: true}, nil
	}
	f, err := os.Open(path)
	if err != nil {
		return OrganizeRulesImport{}, fmt.Errorf("读取规则文件失败：%w", err)
	}
	defer f.Close()
	raw, err := io.ReadAll(io.LimitReader(f, organizeRulesMaxBytes+1))
	if err != nil {
		return OrganizeRulesImport{}, fmt.Errorf("读取规则文件失败：%w", err)
	}
	if len(raw) > organizeRulesMaxBytes {
		return OrganizeRulesImport{}, fmt.Errorf("规则文件超过 %d 字节上限", organizeRulesMaxBytes)
	}

	var payload organizeRulesFile
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&payload); err != nil {
		return OrganizeRulesImport{}, fmt.Errorf("规则文件格式错误（未知字段或 JSON 不合法）：%w", err)
	}
	var extra interface{}
	if err := dec.Decode(&extra); !errors.Is(err, io.EOF) {
		return OrganizeRulesImport{}, errors.New("规则文件在顶层对象之后还有多余内容")
	}

	switch {
	case payload.SchemaVersion == 0:
		return OrganizeRulesImport{}, errors.New("规则文件缺少 schemaVersion，拒绝导入")
	case payload.SchemaVersion > organizeRulesSchemaVersion:
		return OrganizeRulesImport{}, fmt.Errorf(
			"规则文件版本 %d 高于本程序支持的 %d，拒绝导入",
			payload.SchemaVersion, organizeRulesSchemaVersion)
	case payload.SchemaVersion < organizeRulesSchemaVersion:
		return OrganizeRulesImport{}, fmt.Errorf("规则文件版本 %d 不受支持", payload.SchemaVersion)
	}
	if payload.Kind != organizeRulesKind {
		return OrganizeRulesImport{}, fmt.Errorf("规则文件类型不匹配：期望 %q，实际 %q", organizeRulesKind, payload.Kind)
	}

	rules := normalizeImportedRules(payload.Rules)
	if err := validateOrganizeRules(rules); err != nil {
		return OrganizeRulesImport{}, err
	}
	// 导入不改变当前根目录：即使文件里带了 root，也不返回给界面。
	rules.Root = ""
	return OrganizeRulesImport{Path: path, Rules: rules}, nil
}

// normalizeImportedRules 统一枚举大小写并清理空白，便于后续校验与界面选中。
func normalizeImportedRules(r OrganizeSettings) OrganizeSettings {
	r.Mode = strings.ToLower(strings.TrimSpace(r.Mode))
	r.Conflict = strings.ToLower(strings.TrimSpace(r.Conflict))
	r.DeleteMode = strings.ToLower(strings.TrimSpace(r.DeleteMode))
	if p, ok := canonicalKeepPolicy(r.KeepPolicy); ok {
		r.KeepPolicy = p
	}
	if p, ok := canonicalKeepPolicy(r.ImageKeepPolicy); ok {
		r.ImageKeepPolicy = p
	}
	r.Excludes = cleanList(r.Excludes)
	if r.Excludes == nil {
		r.Excludes = []string{}
	}
	r.Regex = strings.TrimSpace(r.Regex)
	r.Target = strings.TrimSpace(r.Target)
	return r
}

func canonicalKeepPolicy(s string) (string, bool) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "pathshortest":
		return "pathShortest", true
	case "newest":
		return "newest", true
	case "oldest":
		return "oldest", true
	case "largest":
		return "largest", true
	case "nameshortest":
		return "nameShortest", true
	}
	return s, false
}

// validateOrganizeRules 汇总全部非法值，避免用户逐条试错。
func validateOrganizeRules(r OrganizeSettings) error {
	var errs []string
	add := func(format string, args ...interface{}) {
		errs = append(errs, fmt.Sprintf(format, args...))
	}

	if !validOrganizeModes[r.Mode] {
		add("mode=%q 不是合法的整理方式（category/extension/namefeature/keyword/fixedfolder）", r.Mode)
	}
	if !validConflictPolicies[r.Conflict] {
		add("conflict=%q 不是合法的冲突策略（overwrite/skip/rename）", r.Conflict)
	}
	if r.DeleteMode != "trash" && r.DeleteMode != "permanent" {
		add("deleteMode=%q 不是合法的删除方式（trash/permanent）", r.DeleteMode)
	}
	if !validOrganizeKeepPolicies[strings.ToLower(r.KeepPolicy)] {
		add("keepPolicy=%q 不是合法的保留规则", r.KeepPolicy)
	}
	if !validOrganizeKeepPolicies[strings.ToLower(r.ImageKeepPolicy)] {
		add("imageKeepPolicy=%q 不是合法的保留规则", r.ImageKeepPolicy)
	}
	if r.ImageThreshold < 0 || r.ImageThreshold > model.MaxImageThreshold {
		add("imageThreshold=%d 超出 0-%d", r.ImageThreshold, model.MaxImageThreshold)
	}
	if r.AudioTolerance < 0 || r.AudioTolerance > 60 {
		add("audioTolerance=%d 超出 0-60", r.AudioTolerance)
	}
	if r.Strength < 0 || r.Strength > 2 {
		add("strength=%d 超出 0-2", r.Strength)
	}
	if r.MinSize < 0 {
		add("minSize=%d 不能为负数", r.MinSize)
	}
	if r.Regex != "" {
		if _, err := regexp.Compile(r.Regex); err != nil {
			add("regex 无法编译：%v", err)
		}
	}
	if r.Target != "" {
		if filepath.IsAbs(r.Target) || filepath.VolumeName(r.Target) != "" {
			add("target=%q 必须是相对根目录的子目录", r.Target)
		} else {
			clean := filepath.Clean(r.Target)
			if clean == "." || clean == ".." ||
				strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
				add("target=%q 不能是根目录本身或上级目录", r.Target)
			}
		}
	}
	if len(errs) > 0 {
		return fmt.Errorf("规则包含非法值：%s", strings.Join(errs, "；"))
	}
	return nil
}
