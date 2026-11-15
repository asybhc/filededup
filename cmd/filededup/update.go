//go:build desktop

package main

// 自动更新门面：检查、验签、下载、替换与回滚。
//
// 安全约束（与需求一致）：
//   - 更新源默认留空，只有配置了 http/https 清单地址才检查；
//   - 清单与安装包都必须通过内置的更新专用 Ed25519 公钥验签；
//   - 长度、SHA-256、清单签名、包签名任一不符都拒绝安装；
//   - 没有“忽略签名继续安装”的选项。

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	wruntime "github.com/wailsapp/wails/v2/pkg/runtime"

	"filededup/internal/update"
)

// UpdateCheckView 是检查更新的结果，交给界面展示。
type UpdateCheckView struct {
	Status         string `json:"status"` // disabled / manual / throttled / uptodate / available / skipped
	Message        string `json:"message,omitempty"`
	CurrentVersion string `json:"currentVersion"`
	LatestVersion  string `json:"latestVersion,omitempty"`
	Notes          string `json:"notes,omitempty"`
	ReleasedAt     string `json:"releasedAt,omitempty"`
	AssetName      string `json:"assetName,omitempty"`
	AssetSize      int64  `json:"assetSize,omitempty"`
	SourceURL      string `json:"sourceUrl,omitempty"`
	CheckedAt      string `json:"checkedAt,omitempty"`
	SkippedVersion string `json:"skippedVersion,omitempty"`
}

// UpdateInstallView 是安装成功后的结果。
type UpdateInstallView struct {
	Version    string `json:"version"`
	ExePath    string `json:"exePath"`
	BackupPath string `json:"backupPath"`
	Size       int64  `json:"size"`
	SHA256     string `json:"sha256"`
	Restarting bool   `json:"restarting"`
}

type updateCheckState struct {
	LastCheck   time.Time `json:"lastCheck"`
	LastVersion string    `json:"lastVersion,omitempty"`
}

func updateStatePath() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("无法定位用户配置目录：%w", err)
	}
	return filepath.Join(dir, "FileDedup", "update-state.json"), nil
}

func loadUpdateState() updateCheckState {
	var st updateCheckState
	path, err := updateStatePath()
	if err != nil {
		return st
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return st
	}
	_ = json.Unmarshal(raw, &st)
	return st
}

func saveUpdateState(st updateCheckState) {
	path, err := updateStatePath()
	if err != nil {
		return
	}
	data, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return
	}
	data = append(data, '\n')
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
	}
}

// CheckForUpdate 检查更新。manual=true 时忽略自动检查开关与检查间隔。
func (a *App) CheckForUpdate(manual bool) (UpdateCheckView, error) {
	settings, _ := LoadSettings()
	cfg := settings.Update
	view := UpdateCheckView{
		CurrentVersion: appVersion,
		SourceURL:      cfg.ManifestURL,
		SkippedVersion: cfg.SkippedVersion,
	}
	if strings.TrimSpace(cfg.ManifestURL) == "" {
		view.Status = "disabled"
		view.Message = "未配置更新源。填写清单地址后才会检查更新。"
		return view, nil
	}
	state := loadUpdateState()
	if !manual {
		if cfg.CheckMode != "auto" {
			view.Status = "manual"
			view.Message = "当前为手动检查模式。"
			return view, nil
		}
		if !state.LastCheck.IsZero() {
			next := state.LastCheck.Add(time.Duration(cfg.IntervalHours) * time.Hour)
			if time.Now().Before(next) {
				view.Status = "throttled"
				view.Message = fmt.Sprintf("距上次检查不足 %d 小时，下次约在 %s。", cfg.IntervalHours, next.Format("2006-01-02 15:04"))
				view.CheckedAt = state.LastCheck.Format(time.RFC3339)
				return view, nil
			}
		}
	}

	pub, err := update.PublicKey()
	if err != nil {
		return view, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	man, _, err := update.FetchManifest(ctx, &http.Client{Timeout: 15 * time.Second}, cfg.ManifestURL)
	if err != nil {
		return view, fmt.Errorf("检查更新失败：%w", err)
	}
	if err := update.VerifyManifest(man, pub); err != nil {
		return view, fmt.Errorf("检查更新失败：%w", err)
	}

	now := time.Now()
	saveUpdateState(updateCheckState{LastCheck: now, LastVersion: man.Version})
	view.CheckedAt = now.Format(time.RFC3339)
	view.LatestVersion = man.Version
	view.Notes = man.Notes
	view.ReleasedAt = man.ReleasedAt

	newer, err := update.IsNewer(man.Version, appVersion)
	if err != nil {
		return view, fmt.Errorf("比较版本失败：%w", err)
	}
	if !newer {
		view.Status = "uptodate"
		view.Message = "当前已是最新版本。"
		return view, nil
	}
	if sameVersion(man.Version, cfg.SkippedVersion) {
		view.Status = "skipped"
		view.Message = fmt.Sprintf("已跳过版本 %s。", man.Version)
		return view, nil
	}
	asset, err := man.CurrentAsset()
	if err != nil {
		return view, fmt.Errorf("检查更新失败：%w", err)
	}
	view.Status = "available"
	view.AssetName = asset.Name
	view.AssetSize = asset.Size
	view.Message = fmt.Sprintf("发现新版本 %s。", man.Version)
	return view, nil
}

// InstallUpdate 下载并安装最新版本，成功后重启程序。
func (a *App) InstallUpdate() (UpdateInstallView, error) {
	a.updateMu.Lock()
	defer a.updateMu.Unlock()

	settings, _ := LoadSettings()
	cfg := settings.Update
	if strings.TrimSpace(cfg.ManifestURL) == "" {
		return UpdateInstallView{}, fmt.Errorf("未配置更新源，无法安装更新")
	}
	pub, err := update.PublicKey()
	if err != nil {
		return UpdateInstallView{}, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	man, _, err := update.FetchManifest(ctx, &http.Client{Timeout: 15 * time.Second}, cfg.ManifestURL)
	if err != nil {
		return UpdateInstallView{}, fmt.Errorf("安装已取消：%w", err)
	}
	if err := update.VerifyManifest(man, pub); err != nil {
		return UpdateInstallView{}, fmt.Errorf("安装已取消：%w", err)
	}
	newer, err := update.IsNewer(man.Version, appVersion)
	if err != nil {
		return UpdateInstallView{}, fmt.Errorf("安装已取消：%w", err)
	}
	if !newer {
		return UpdateInstallView{}, fmt.Errorf("安装已取消：清单版本 %s 不高于当前版本 %s", man.Version, appVersion)
	}
	asset, err := man.CurrentAsset()
	if err != nil {
		return UpdateInstallView{}, fmt.Errorf("安装已取消：%w", err)
	}
	if err := update.VerifyAsset(asset, pub); err != nil {
		return UpdateInstallView{}, fmt.Errorf("安装已取消：%w", err)
	}

	cacheDir, err := os.UserCacheDir()
	if err != nil {
		return UpdateInstallView{}, fmt.Errorf("安装已取消：无法定位缓存目录：%w", err)
	}
	pkg, err := update.DownloadAsset(ctx, &http.Client{Timeout: 10 * time.Minute}, asset, filepath.Join(cacheDir, "FileDedup", "updates"))
	if err != nil {
		return UpdateInstallView{}, fmt.Errorf("安装已取消：%w", err)
	}

	exe, err := os.Executable()
	if err != nil {
		return UpdateInstallView{}, fmt.Errorf("安装已取消：无法定位当前程序：%w", err)
	}
	res, err := update.Replace(exe, pkg, asset.Size, asset.SHA256, func(target string) error {
		cmd := exec.Command(target)
		cmd.Dir = filepath.Dir(target)
		return cmd.Start()
	})
	if err != nil {
		return UpdateInstallView{}, fmt.Errorf("安装已取消：%w", err)
	}
	if a.ctx != nil {
		go func() {
			time.Sleep(1200 * time.Millisecond)
			wruntime.Quit(a.ctx)
		}()
	}
	return UpdateInstallView{
		Version:    man.Version,
		ExePath:    res.ExePath,
		BackupPath: res.BackupPath,
		Size:       res.Size,
		SHA256:     res.SHA256,
		Restarting: a.ctx != nil,
	}, nil
}

// SkipUpdateVersion 记住要跳过的版本，后续检查不再提示。
func (a *App) SkipUpdateVersion(version string) error {
	version = strings.TrimSpace(version)
	if _, err := update.ParseVersion(version); err != nil {
		return fmt.Errorf("版本号非法：%w", err)
	}
	settings, _ := LoadSettings()
	settings.Update.SkippedVersion = version
	return SaveSettings(settings)
}

// ClearSkippedUpdateVersion 取消“跳过此版本”。
func (a *App) ClearSkippedUpdateVersion() error {
	settings, _ := LoadSettings()
	settings.Update.SkippedVersion = ""
	return SaveSettings(settings)
}

func sameVersion(a, b string) bool {
	if strings.TrimSpace(b) == "" {
		return false
	}
	n, err := update.CompareVersion(a, b)
	return err == nil && n == 0
}
