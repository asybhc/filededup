//go:build desktop

package main

// 整理配置方案预设：把一套整理规则命名保存，供整理页一键切换。
//
// 存储位置与设置文件同目录（FileDedup/organize-presets.json），带 schemaVersion/kind；
// 保存前复用组织规则的 normalize + validate 口径，根目录不写入预设（预设不绑定机器）。
// 写入用临时文件 + 原子替换，避免断电留下半截文件。

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode"
)

const (
	organizePresetsKind          = "filededup.organize.presets"
	organizePresetsSchemaVersion = 1
	organizePresetsMaxBytes      = 1 << 20 // 1 MiB
	organizePresetNameMaxRunes   = 60
)

// OrganizePreset 是一套命名保存的整理规则。
type OrganizePreset struct {
	ID        string           `json:"id"`
	Name      string           `json:"name"`
	CreatedAt string           `json:"createdAt"`
	UpdatedAt string           `json:"updatedAt"`
	Rules     OrganizeSettings `json:"rules"`
}

// OrganizePresetsResult 是预设列表与文件路径（路径用于界面如实展示出错位置）。
type OrganizePresetsResult struct {
	Path    string           `json:"path"`
	Presets []OrganizePreset `json:"presets"`
}

type organizePresetFile struct {
	SchemaVersion int              `json:"schemaVersion"`
	Kind          string           `json:"kind"`
	Presets       []OrganizePreset `json:"presets"`
}

// organizePresetsPath 返回预设文件路径：用户配置目录下 FileDedup/organize-presets.json。
func organizePresetsPath() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("无法定位用户配置目录：%w", err)
	}
	return filepath.Join(dir, "FileDedup", "organize-presets.json"), nil
}

// loadOrganizePresets 读取预设文件；文件不存在时返回空列表。
// 文件损坏时返回错误而不是静默覆盖，避免一次保存把旧预设清空。
func loadOrganizePresets(path string) ([]OrganizePreset, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("读取预设文件失败：%w", err)
	}
	if len(raw) > organizePresetsMaxBytes {
		return nil, fmt.Errorf("预设文件超过 %d 字节上限", organizePresetsMaxBytes)
	}
	var payload organizePresetFile
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&payload); err != nil {
		return nil, fmt.Errorf("预设文件格式错误（未知字段或 JSON 不合法）：%w", err)
	}
	var extra interface{}
	if err := dec.Decode(&extra); !errors.Is(err, io.EOF) {
		return nil, errors.New("预设文件在顶层对象之后还有多余内容")
	}
	switch {
	case payload.SchemaVersion == 0:
		return nil, errors.New("预设文件缺少 schemaVersion")
	case payload.SchemaVersion > organizePresetsSchemaVersion:
		return nil, fmt.Errorf("预设文件版本 %d 高于本程序支持的 %d", payload.SchemaVersion, organizePresetsSchemaVersion)
	case payload.SchemaVersion < organizePresetsSchemaVersion:
		return nil, fmt.Errorf("预设文件版本 %d 不受支持", payload.SchemaVersion)
	}
	if payload.Kind != organizePresetsKind {
		return nil, fmt.Errorf("预设文件类型不匹配：期望 %q，实际 %q", organizePresetsKind, payload.Kind)
	}
	// 读取时统一枚举大小写并清空根目录；非法数值保持原样，执行前仍由 StartOrganize 收敛。
	out := make([]OrganizePreset, 0, len(payload.Presets))
	for _, p := range payload.Presets {
		p.Rules = normalizeImportedRules(p.Rules)
		p.Rules.Root = ""
		out = append(out, p)
	}
	sortPresets(out)
	return out, nil
}

// sortPresets 按最近更新倒序，名字兜底，保证列表顺序稳定。
func sortPresets(list []OrganizePreset) {
	sort.SliceStable(list, func(i, j int) bool {
		if list[i].UpdatedAt != list[j].UpdatedAt {
			return list[i].UpdatedAt > list[j].UpdatedAt
		}
		return list[i].Name < list[j].Name
	})
}

// writeOrganizePresets 原子写入预设文件。
func writeOrganizePresets(path string, list []OrganizePreset) error {
	payload := organizePresetFile{
		SchemaVersion: organizePresetsSchemaVersion,
		Kind:          organizePresetsKind,
		Presets:       list,
	}
	raw, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return err
	}
	raw = append(raw, '\n')
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("创建配置目录失败：%w", err)
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o644); err != nil {
		return fmt.Errorf("写入预设文件失败：%w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("替换预设文件失败：%w", err)
	}
	return nil
}

// normalizePresetName 校验并清理预设名：去首尾空白、拒绝空名、控制字符与超长。
func normalizePresetName(name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return "", errors.New("预设名称不能为空")
	}
	for _, r := range name {
		if unicode.IsControl(r) {
			return "", errors.New("预设名称不能包含控制字符或换行")
		}
	}
	if len([]rune(name)) > organizePresetNameMaxRunes {
		return "", fmt.Errorf("预设名称不能超过 %d 个字符", organizePresetNameMaxRunes)
	}
	return name, nil
}

// ListOrganizePresets 返回全部预设。文件损坏时返回错误与路径，界面据此提示用户。
func (a *App) ListOrganizePresets() (OrganizePresetsResult, error) {
	path, err := organizePresetsPath()
	if err != nil {
		return OrganizePresetsResult{}, err
	}
	list, err := loadOrganizePresets(path)
	if err != nil {
		return OrganizePresetsResult{Path: path}, err
	}
	if list == nil {
		list = []OrganizePreset{}
	}
	return OrganizePresetsResult{Path: path, Presets: list}, nil
}

// SaveOrganizePreset 保存或按同名覆盖一套预设；返回落库后的条目。
// 同名判断大小写不敏感（与 Windows 文件名习惯一致），覆盖时保留原 ID 与创建时间。
func (a *App) SaveOrganizePreset(name string, rules OrganizeSettings) (OrganizePreset, error) {
	clean, err := normalizePresetName(name)
	if err != nil {
		return OrganizePreset{}, err
	}
	rules = normalizeImportedRules(rules)
	rules.Root = ""
	if err := validateOrganizeRules(rules); err != nil {
		return OrganizePreset{}, err
	}
	path, err := organizePresetsPath()
	if err != nil {
		return OrganizePreset{}, err
	}
	list, err := loadOrganizePresets(path)
	if err != nil {
		return OrganizePreset{}, err
	}
	now := time.Now().UTC().Format(time.RFC3339)
	entry := OrganizePreset{
		ID:        fmt.Sprintf("p%d", time.Now().UnixNano()),
		Name:      clean,
		CreatedAt: now,
		UpdatedAt: now,
		Rules:     rules,
	}
	replaced := false
	for i := range list {
		if strings.EqualFold(list[i].Name, clean) {
			entry.ID = list[i].ID
			entry.CreatedAt = list[i].CreatedAt
			list[i] = entry
			replaced = true
			break
		}
	}
	if !replaced {
		list = append(list, entry)
	}
	sortPresets(list)
	if err := writeOrganizePresets(path, list); err != nil {
		return OrganizePreset{}, err
	}
	return entry, nil
}

// DeleteOrganizePreset 删除指定预设；ID 不存在时返回错误，不改文件。
func (a *App) DeleteOrganizePreset(id string) error {
	id = strings.TrimSpace(id)
	if id == "" {
		return errors.New("预设 ID 不能为空")
	}
	path, err := organizePresetsPath()
	if err != nil {
		return err
	}
	list, err := loadOrganizePresets(path)
	if err != nil {
		return err
	}
	out := make([]OrganizePreset, 0, len(list))
	found := false
	for _, p := range list {
		if p.ID == id {
			found = true
			continue
		}
		out = append(out, p)
	}
	if !found {
		return errors.New("预设不存在（可能已在其他窗口删除）")
	}
	return writeOrganizePresets(path, out)
}
