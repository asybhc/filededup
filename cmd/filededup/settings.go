//go:build desktop

package main

// 设置持久化：把界面选项存到用户配置目录，启动时恢复。
//
// 容错约定（与需求一致）：
//   - 文件带 schemaVersion；缺失时按当前版本写入。
//   - 缺键按默认值补齐（先填默认值，再让 JSON 覆盖存在的键）。
//   - 多出来的键不视为损坏：读取时忽略，写入时原样保留。
//   - 只有整体不是合法 JSON 才回退默认值，并在界面上给出提示。

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const settingsSchemaVersion = 1

// ScanSettings 是查重页的持久化选项。
type ScanSettings struct {
	Root           string   `json:"root"`
	KeepPolicy     string   `json:"keepPolicy"`
	DeleteMode     string   `json:"deleteMode"`
	SimilarImage   bool     `json:"similarImage"`
	ImageThreshold int      `json:"imageThreshold"`
	SimilarAudio   bool     `json:"similarAudio"`
	AudioTolerance int      `json:"audioTolerance"`
	MinSize        int64    `json:"minSize"`
	IncludeHidden  bool     `json:"includeHidden"`
	Excludes       []string `json:"excludes"`
}

// OrganizeSettings 是整理页的持久化选项。
type OrganizeSettings struct {
	Root            string        `json:"root"`
	Target          string        `json:"target"`
	UsePreviewPlan  bool          `json:"usePreviewPlan"`
	Steps           OrganizeSteps `json:"steps"`
	Mode            string        `json:"mode"`
	Keyword         string        `json:"keyword"`
	Regex           string        `json:"regex"`
	FixedDir        string        `json:"fixedDir"`
	Conflict        string        `json:"conflict"`
	DeleteMode      string        `json:"deleteMode"`
	KeepPolicy      string        `json:"keepPolicy"`
	ImageKeepPolicy string        `json:"imageKeepPolicy"`
	ImageThreshold  int           `json:"imageThreshold"`
	AudioTolerance  int           `json:"audioTolerance"`
	Strength        int           `json:"strength"`
	MinSize         int64         `json:"minSize"`
	IncludeHidden   bool          `json:"includeHidden"`
	Excludes        []string      `json:"excludes"`
	ProtectWords    string        `json:"protectWords"`
	Hardlink        bool          `json:"hardlink"`
	ContinueOnError bool          `json:"continueOnError"`
}

// UpdateSettings 是自动更新的持久化选项。
// ManifestURL 默认为空：为空时不检查更新，填了才启用。
type UpdateSettings struct {
	ManifestURL    string `json:"manifestUrl"`
	CheckMode      string `json:"checkMode"`
	IntervalHours  int    `json:"intervalHours"`
	SkippedVersion string `json:"skippedVersion"`
}

// LanzouSettings 是蓝奏云上传页的持久化选项。
// 凭据不在这里：它单独用 DPAPI 加密存到 FileDedup/lanzou/lanzou.bin。
type LanzouSettings struct {
	Workers        int    `json:"workers"`            // 并发 1–5，默认 3
	IntervalMS     int    `json:"intervalMs"`         // 请求间隔 1000–2000ms，默认 1500
	ForceReupload  bool   `json:"forceReupload"`      // 忽略云端去重与本机缓存
	DisclaimerOK   bool   `json:"disclaimerAccepted"` // 免责声明确认
	LastLocalDir   string `json:"lastLocalDir"`
	LastFolderID   string `json:"lastFolderId"`
	LastFolderName string `json:"lastFolderName"`
}

// UISettings 是整份配置。
type UISettings struct {
	SchemaVersion int              `json:"schemaVersion"`
	Scan          ScanSettings     `json:"scan"`
	Organize      OrganizeSettings `json:"organize"`
	Update        UpdateSettings   `json:"update"`
	Lanzou        LanzouSettings   `json:"lanzou"`
}

// SettingsResult 把“加载结果 + 提示”一起返回，界面可如实展示警告。
type SettingsResult struct {
	Settings UISettings `json:"settings"`
	Path     string     `json:"path"`
	Warning  string     `json:"warning,omitempty"`
}

// DefaultSettings 返回与界面初始值一致的默认设置。
func DefaultSettings() UISettings {
	return UISettings{
		SchemaVersion: settingsSchemaVersion,
		Scan: ScanSettings{
			KeepPolicy:     "pathShortest",
			DeleteMode:     "trash",
			SimilarImage:   true,
			ImageThreshold: 8,
			SimilarAudio:   true,
			AudioTolerance: 2,
			Excludes:       []string{},
		},
		Organize: OrganizeSettings{
			Steps:           DefaultOrganizeSteps(),
			Mode:            "category",
			Conflict:        "rename",
			DeleteMode:      "trash",
			KeepPolicy:      "pathShortest",
			ImageKeepPolicy: "largest",
			ImageThreshold:  8,
			AudioTolerance:  2,
			Strength:        1,
			Excludes:        []string{},
		},
		Update: UpdateSettings{
			CheckMode:     "auto",
			IntervalHours: 18,
		},
		Lanzou: LanzouSettings{
			Workers:    3,
			IntervalMS: 1500,
		},
	}
}

// settingsPath 返回配置文件路径：用户配置目录下 FileDedup/ui-settings.json。
func settingsPath() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("无法定位用户配置目录：%w", err)
	}
	return filepath.Join(dir, "FileDedup", "ui-settings.json"), nil
}

// LoadSettings 读取配置。第二个返回值是给用户看的提示，正常时为空。
func LoadSettings() (UISettings, string) {
	def := DefaultSettings()
	path, err := settingsPath()
	if err != nil {
		return def, ""
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return def, ""
		}
		return def, fmt.Sprintf("读取设置失败，已使用默认值：%v", err)
	}

	var top map[string]json.RawMessage
	if err := json.Unmarshal(raw, &top); err != nil {
		return def, "设置文件不是合法 JSON，已使用默认值；下次保存会覆盖该文件。"
	}

	out := def
	var warns []string
	if v, ok := top["schemaVersion"]; ok {
		var n int
		if json.Unmarshal(v, &n) == nil {
			if n > settingsSchemaVersion {
				warns = append(warns, fmt.Sprintf("设置来自更新版本（schemaVersion=%d），未知字段已忽略", n))
			}
		}
	}
	if v, ok := top["scan"]; ok {
		if err := json.Unmarshal(v, &out.Scan); err != nil {
			out.Scan = def.Scan
			warns = append(warns, "scan 段无法解析，已整体使用默认值")
		}
	}
	if v, ok := top["organize"]; ok {
		if err := json.Unmarshal(v, &out.Organize); err != nil {
			out.Organize = def.Organize
			warns = append(warns, "organize 段无法解析，已整体使用默认值")
		}
	}
	if v, ok := top["update"]; ok {
		if err := json.Unmarshal(v, &out.Update); err != nil {
			out.Update = def.Update
			warns = append(warns, "update 段无法解析，已整体使用默认值")
		}
	}
	if v, ok := top["lanzou"]; ok {
		if err := json.Unmarshal(v, &out.Lanzou); err != nil {
			out.Lanzou = def.Lanzou
			warns = append(warns, "lanzou 段无法解析，已整体使用默认值")
		}
	}

	normalizeSettings(&out, def)
	return out, strings.Join(warns, "；")
}

// normalizeSettings 把越界或未知的枚举值拉回默认，避免坏值流进引擎。
func normalizeSettings(s *UISettings, def UISettings) {
	if s.Scan.Excludes == nil {
		s.Scan.Excludes = []string{}
	}
	if s.Organize.Excludes == nil {
		s.Organize.Excludes = []string{}
	}
	if !validKeepPolicy(s.Scan.KeepPolicy) {
		s.Scan.KeepPolicy = def.Scan.KeepPolicy
	}
	if s.Scan.DeleteMode != "permanent" {
		s.Scan.DeleteMode = "trash"
	}
	if s.Scan.ImageThreshold < 0 || s.Scan.ImageThreshold > 64 {
		s.Scan.ImageThreshold = def.Scan.ImageThreshold
	}
	if s.Scan.AudioTolerance < 0 || s.Scan.AudioTolerance > 60 {
		s.Scan.AudioTolerance = def.Scan.AudioTolerance
	}
	if s.Scan.MinSize < 0 {
		s.Scan.MinSize = 0
	}

	if !validKeepPolicy(s.Organize.KeepPolicy) {
		s.Organize.KeepPolicy = def.Organize.KeepPolicy
	}
	if !validKeepPolicy(s.Organize.ImageKeepPolicy) {
		s.Organize.ImageKeepPolicy = def.Organize.ImageKeepPolicy
	}
	if s.Organize.DeleteMode != "permanent" {
		s.Organize.DeleteMode = "trash"
	}
	if !validOrganizeModes[s.Organize.Mode] {
		s.Organize.Mode = def.Organize.Mode
	}
	if !validConflictPolicies[s.Organize.Conflict] {
		s.Organize.Conflict = def.Organize.Conflict
	}
	if s.Organize.ImageThreshold < 0 || s.Organize.ImageThreshold > 64 {
		s.Organize.ImageThreshold = def.Organize.ImageThreshold
	}
	if s.Organize.AudioTolerance < 0 || s.Organize.AudioTolerance > 60 {
		s.Organize.AudioTolerance = def.Organize.AudioTolerance
	}
	if s.Organize.Strength < 0 || s.Organize.Strength > 2 {
		s.Organize.Strength = def.Organize.Strength
	}
	if s.Organize.MinSize < 0 {
		s.Organize.MinSize = 0
	}

	s.Update.ManifestURL = strings.TrimSpace(s.Update.ManifestURL)
	if s.Update.CheckMode != "manual" {
		s.Update.CheckMode = "auto"
	}
	if s.Update.IntervalHours <= 0 || s.Update.IntervalHours > 720 {
		s.Update.IntervalHours = def.Update.IntervalHours
	}
	s.Update.SkippedVersion = strings.TrimSpace(s.Update.SkippedVersion)

	if s.Lanzou.Workers <= 0 {
		s.Lanzou.Workers = def.Lanzou.Workers
	}
	if s.Lanzou.Workers > 5 {
		s.Lanzou.Workers = 5
	}
	if s.Lanzou.IntervalMS <= 0 {
		s.Lanzou.IntervalMS = def.Lanzou.IntervalMS
	}
	if s.Lanzou.IntervalMS < 1000 {
		s.Lanzou.IntervalMS = 1000
	}
	if s.Lanzou.IntervalMS > 2000 {
		s.Lanzou.IntervalMS = 2000
	}
	s.Lanzou.LastLocalDir = strings.TrimSpace(s.Lanzou.LastLocalDir)
	s.Lanzou.LastFolderID = strings.TrimSpace(s.Lanzou.LastFolderID)
	s.Lanzou.LastFolderName = strings.TrimSpace(s.Lanzou.LastFolderName)
}

func validKeepPolicy(s string) bool {
	switch s {
	case "newest", "oldest", "nameShortest", "pathShortest":
		return true
	}
	return false
}

// SaveSettings 写入配置：先读旧文件保留未知键，再原子替换。
func SaveSettings(s UISettings) error {
	path, err := settingsPath()
	if err != nil {
		return err
	}
	s.SchemaVersion = settingsSchemaVersion

	top := map[string]json.RawMessage{}
	if raw, rerr := os.ReadFile(path); rerr == nil {
		// 旧文件损坏时直接忽略，按新内容重写。
		_ = json.Unmarshal(raw, &top)
	}
	if top == nil {
		top = map[string]json.RawMessage{}
	}
	put := func(key string, v interface{}) error {
		b, err := json.Marshal(v)
		if err != nil {
			return err
		}
		top[key] = b
		return nil
	}
	for _, err := range []error{
		put("schemaVersion", s.SchemaVersion),
		put("scan", s.Scan),
		put("organize", s.Organize),
		put("update", s.Update),
		put("lanzou", s.Lanzou),
	} {
		if err != nil {
			return err
		}
	}
	data, err := json.MarshalIndent(top, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("创建配置目录失败：%w", err)
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return fmt.Errorf("写入设置失败：%w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("替换设置文件失败：%w", err)
	}
	return nil
}

// GetSettings 返回当前设置与文件路径。加载失败不视为错误，改用 warning 告知界面。
func (a *App) GetSettings() SettingsResult {
	settings, warning := LoadSettings()
	path, _ := settingsPath()
	return SettingsResult{Settings: settings, Path: path, Warning: warning}
}

// SaveSettings 保存设置。
func (a *App) SaveSettings(s UISettings) error {
	return SaveSettings(s)
}
