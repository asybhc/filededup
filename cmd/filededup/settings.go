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
	"strconv"
	"strings"

	"filededup/internal/engine/model"
	"filededup/internal/secret"
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

// AISettings 是 AI 分析配置。密钥不在这里：单独用 DPAPI 存到 FileDedup/ai/key.bin。
type AISettings struct {
	Enabled        bool   `json:"enabled"`
	Endpoint       string `json:"endpoint"`
	Model          string `json:"model"`
	TimeoutSeconds int    `json:"timeoutSeconds"`
	KeyHint        string `json:"keyHint"` // 只读：由后端从密钥文件算出，界面只显后四位
}

// WindowSettings 是窗口关闭行为、托盘与主题。
type WindowSettings struct {
	CloseAction string `json:"closeAction"` // exit | tray
	TrayEnabled bool   `json:"trayEnabled"`
	Theme       string `json:"theme"` // system | light | dark
}

// ScheduleSettings 是定时任务配置。动作目前只支持只读扫描（不做自动删除/整理）。
type ScheduleSettings struct {
	Enabled         bool   `json:"enabled"`
	Mode            string `json:"mode"`            // interval | daily
	IntervalMinutes int    `json:"intervalMinutes"` // 10–10080
	DailyTime       string `json:"dailyTime"`       // HH:MM（24 小时制）
	Action          string `json:"action"`          // scan
	Root            string `json:"root"`            // 留空时用查重页保存的根目录
}

// LogSettings 是运行日志配置。
type LogSettings struct {
	RetentionDays int `json:"retentionDays"`
}

// UISettings 是整份配置。
type UISettings struct {
	SchemaVersion int              `json:"schemaVersion"`
	Scan          ScanSettings     `json:"scan"`
	Organize      OrganizeSettings `json:"organize"`
	Update        UpdateSettings   `json:"update"`
	Lanzou        LanzouSettings   `json:"lanzou"`
	AI            AISettings       `json:"ai"`
	Window        WindowSettings   `json:"window"`
	Schedule      ScheduleSettings `json:"schedule"`
	Logs          LogSettings      `json:"logs"`
}

// SettingsResult 把“加载结果 + 提示”一起返回，界面可如实展示警告。
type SettingsResult struct {
	Settings        UISettings `json:"settings"`
	Path            string     `json:"path"`
	Warning         string     `json:"warning,omitempty"`
	KeyStoragePath  string     `json:"keyStoragePath"`
	KeyProtectionOK bool       `json:"keyProtectionOk"`
}

// DefaultSettings 返回与界面初始值一致的默认设置。
func DefaultSettings() UISettings {
	return UISettings{
		SchemaVersion: settingsSchemaVersion,
		Scan: ScanSettings{
			KeepPolicy:     "pathShortest",
			DeleteMode:     "trash",
			SimilarImage:   false,
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
		AI: AISettings{
			Enabled:        false,
			TimeoutSeconds: 45,
		},
		Window: WindowSettings{
			CloseAction: "exit",
			TrayEnabled: true,
			Theme:       "system",
		},
		Schedule: ScheduleSettings{
			Enabled:         false,
			Mode:            "interval",
			IntervalMinutes: 60,
			DailyTime:       "09:00",
			Action:          "scan",
		},
		Logs: LogSettings{RetentionDays: 14},
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
	if v, ok := top["ai"]; ok {
		if err := json.Unmarshal(v, &out.AI); err != nil {
			out.AI = def.AI
			warns = append(warns, "ai 段无法解析，已整体使用默认值")
		}
	}
	if v, ok := top["window"]; ok {
		if err := json.Unmarshal(v, &out.Window); err != nil {
			out.Window = def.Window
			warns = append(warns, "window 段无法解析，已整体使用默认值")
		}
	}
	if v, ok := top["schedule"]; ok {
		if err := json.Unmarshal(v, &out.Schedule); err != nil {
			out.Schedule = def.Schedule
			warns = append(warns, "schedule 段无法解析，已整体使用默认值")
		}
	}
	if v, ok := top["logs"]; ok {
		if err := json.Unmarshal(v, &out.Logs); err != nil {
			out.Logs = def.Logs
			warns = append(warns, "logs 段无法解析，已整体使用默认值")
		}
	}

	warns = append(warns, normalizeSettings(&out, def)...)
	return out, strings.Join(warns, "；")
}

// normalizeSettings 把越界或未知的枚举值拉回默认，避免坏值流进引擎。
// 返回值是需要提示用户的收敛说明；旧配置里合法的值原样保留。
func normalizeSettings(s *UISettings, def UISettings) []string {
	var warns []string
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
	// 旧版本允许 0..64，上限收紧后按可预期规则收敛到新上限并提示，不静默截断。
	if s.Scan.ImageThreshold > model.MaxImageThreshold {
		warns = append(warns, fmt.Sprintf("相似图片阈值 %d 超出新上限 %d，已收敛为 %d",
			s.Scan.ImageThreshold, model.MaxImageThreshold, model.MaxImageThreshold))
		s.Scan.ImageThreshold = model.MaxImageThreshold
	} else if s.Scan.ImageThreshold < 0 {
		warns = append(warns, fmt.Sprintf("相似图片阈值 %d 无效，已回退默认值 %d",
			s.Scan.ImageThreshold, def.Scan.ImageThreshold))
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
	if s.Organize.ImageThreshold > model.MaxImageThreshold {
		warns = append(warns, fmt.Sprintf("整理页相似图片阈值 %d 超出新上限 %d，已收敛为 %d",
			s.Organize.ImageThreshold, model.MaxImageThreshold, model.MaxImageThreshold))
		s.Organize.ImageThreshold = model.MaxImageThreshold
	} else if s.Organize.ImageThreshold < 0 {
		warns = append(warns, fmt.Sprintf("整理页相似图片阈值 %d 无效，已回退默认值 %d",
			s.Organize.ImageThreshold, def.Organize.ImageThreshold))
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

	s.AI.Endpoint = strings.TrimSpace(s.AI.Endpoint)
	s.AI.Model = strings.TrimSpace(s.AI.Model)
	if s.AI.TimeoutSeconds < 5 || s.AI.TimeoutSeconds > 300 {
		s.AI.TimeoutSeconds = def.AI.TimeoutSeconds
	}

	if s.Window.CloseAction != "tray" {
		s.Window.CloseAction = "exit"
	}
	switch s.Window.Theme {
	case "light", "dark", "system":
	default:
		s.Window.Theme = def.Window.Theme
	}

	if s.Schedule.Mode != "daily" {
		s.Schedule.Mode = "interval"
	}
	if s.Schedule.IntervalMinutes < 10 || s.Schedule.IntervalMinutes > 10080 {
		s.Schedule.IntervalMinutes = def.Schedule.IntervalMinutes
	}
	if !validDailyTime(s.Schedule.DailyTime) {
		s.Schedule.DailyTime = def.Schedule.DailyTime
	}
	// 只实现只读扫描：不接受其他动作，避免把未验证的自动删除带进定时任务。
	s.Schedule.Action = "scan"
	s.Schedule.Root = strings.TrimSpace(s.Schedule.Root)
	if s.Schedule.Root != "" && !filepath.IsAbs(s.Schedule.Root) {
		s.Schedule.Root = ""
	}

	if s.Logs.RetentionDays < 1 || s.Logs.RetentionDays > 365 {
		s.Logs.RetentionDays = def.Logs.RetentionDays
	}
	return warns
}

// validDailyTime 校验 HH:MM（24 小时制）。
func validDailyTime(v string) bool {
	parts := strings.Split(strings.TrimSpace(v), ":")
	if len(parts) != 2 || len(parts[0]) != 2 || len(parts[1]) != 2 {
		return false
	}
	h, err := strconv.Atoi(parts[0])
	if err != nil || h < 0 || h > 23 {
		return false
	}
	m, err := strconv.Atoi(parts[1])
	if err != nil || m < 0 || m > 59 {
		return false
	}
	return true
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
	// 掩码只用于界面展示，不落盘；每次读取时按密钥文件重新计算。
	s.AI.KeyHint = ""
	normalizeSettings(&s, DefaultSettings())

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
		put("ai", s.AI),
		put("window", s.Window),
		put("schedule", s.Schedule),
		put("logs", s.Logs),
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
	keyPath, _ := secret.Path()
	res := SettingsResult{
		Settings:        settings,
		Path:            path,
		Warning:         warning,
		KeyStoragePath:  keyPath,
		KeyProtectionOK: secret.SupportsProtection(),
	}
	if key, err := secret.Load(); err == nil {
		res.Settings.AI.KeyHint = secret.Hint(key)
	}
	return res
}

// SaveAIKey 保存 AI 密钥（DPAPI）。传空字符串表示清除。
func (a *App) SaveAIKey(key string) error {
	return secret.Save(key)
}

// SaveSettings 保存设置并同步运行时行为（托盘、定时任务、日志保留）。
func (a *App) SaveSettings(s UISettings) error {
	if err := SaveSettings(s); err != nil {
		return err
	}
	a.applyRuntimeSettings(s)
	return nil
}
