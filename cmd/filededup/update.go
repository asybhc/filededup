//go:build desktop

package main

// 自动更新门面：检查、验签、下载、替换与回滚。
//
// 安全约束（与需求一致）：
//   - 更新源默认走内置仓库的最新 release；手动填写的清单地址仅作为覆盖项；
//   - 清单与安装包都必须通过内置的更新专用 Ed25519 公钥验签；
//   - 长度、SHA-256、清单签名、包签名任一不符都拒绝安装；
//   - 没有“忽略签名继续安装”的选项。

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	wruntime "github.com/wailsapp/wails/v2/pkg/runtime"

	"filededup/internal/brand"
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
	AutoSource     bool   `json:"autoSource,omitempty"`
	InstallerURL   string `json:"installerUrl,omitempty"`
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
	path, err := brand.DataSub("update-state.json")
	if err != nil {
		return "", fmt.Errorf("无法定位用户配置目录：%w", err)
	}
	return path, nil
}

// updateCacheDir 返回安装包缓存目录（与下载时使用的目录一致）。
// 按需求统一落在 %AppData% 的新数据目录下，不再使用 %LocalAppData%。
func updateCacheDir() (string, error) {
	path, err := brand.DataSub("updates")
	if err != nil {
		return "", fmt.Errorf("无法定位数据目录：%w", err)
	}
	return path, nil
}

// pruneUpdateCache 清理旧安装包与残留临时文件：只保留最近 2 个，失败只记日志。
func pruneUpdateCache() {
	dir, err := updateCacheDir()
	if err != nil {
		log.Printf("清理更新缓存失败：%v", err)
		return
	}
	res, err := update.PruneCache(dir, 2)
	if err != nil {
		log.Printf("清理更新缓存部分失败：%v", err)
	}
	if res.Removed > 0 {
		log.Printf("清理更新缓存：删除 %d 个文件，释放 %d 字节", res.Removed, res.Freed)
	}
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
		SkippedVersion: cfg.SkippedVersion,
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
	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Second)
	defer cancel()
	src, err := resolveUpdateSource(ctx, cfg)
	if err != nil {
		return view, fmt.Errorf("检查更新失败：%w", err)
	}
	view.SourceURL = src.manifestURL
	view.AutoSource = src.release != nil
	man, _, err := update.FetchManifest(ctx, &http.Client{Timeout: 15 * time.Second}, src.manifestURL)
	if err != nil {
		return view, fmt.Errorf("检查更新失败：%w", err)
	}
	if err := update.VerifyManifest(man, pub); err != nil {
		return view, fmt.Errorf("检查更新失败：%w", err)
	}
	if err := crossCheckRelease(src.release, man.Version); err != nil {
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
	if asset, err = crossCheckAsset(src.release, asset); err != nil {
		return view, fmt.Errorf("检查更新失败：%w", err)
	}
	view.Status = "available"
	view.AssetName = asset.Name
	view.AssetSize = asset.Size
	view.InstallerURL = asset.URL
	view.Message = fmt.Sprintf("发现新版本 %s。", man.Version)
	return view, nil
}

// InstallUpdate 下载并安装最新版本，成功后重启程序。
func (a *App) InstallUpdate() (UpdateInstallView, error) {
	a.updateMu.Lock()
	defer a.updateMu.Unlock()

	settings, _ := LoadSettings()
	cfg := settings.Update
	pub, err := update.PublicKey()
	if err != nil {
		return UpdateInstallView{}, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	src, err := resolveUpdateSource(ctx, cfg)
	if err != nil {
		return UpdateInstallView{}, fmt.Errorf("安装已取消：%w", err)
	}
	man, _, err := update.FetchManifest(ctx, &http.Client{Timeout: 15 * time.Second}, src.manifestURL)
	if err != nil {
		return UpdateInstallView{}, fmt.Errorf("安装已取消：%w", err)
	}
	if err := update.VerifyManifest(man, pub); err != nil {
		return UpdateInstallView{}, fmt.Errorf("安装已取消：%w", err)
	}
	if err := crossCheckRelease(src.release, man.Version); err != nil {
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
	if asset, err = crossCheckAsset(src.release, asset); err != nil {
		return UpdateInstallView{}, fmt.Errorf("安装已取消：%w", err)
	}
	if err := update.VerifyAsset(asset, pub); err != nil {
		return UpdateInstallView{}, fmt.Errorf("安装已取消：%w", err)
	}

	cacheDir, err := updateCacheDir()
	if err != nil {
		return UpdateInstallView{}, fmt.Errorf("安装已取消：%w", err)
	}
	pkg, err := update.DownloadAsset(ctx, &http.Client{Timeout: 10 * time.Minute}, asset, cacheDir)
	if err != nil {
		return UpdateInstallView{}, fmt.Errorf("安装已取消：%w", err)
	}

	// 安装包资产走静默安装器：Replace 会把包本身当成新 exe 启动，
	// 对 NSIS 安装包等于弹出图形向导，因此必须交给安装器完成替换。
	if isSetupPackage(asset.Name) {
		return a.installViaSetup(pkg, asset, man.Version)
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
	// 安装成功后再清一次缓存：旧安装包与残留临时文件随之删除。
	// 备份的 .old 仍由旧进程映像占用，删不掉，留到下次启动的 update.Cleanup 处理。
	pruneUpdateCache()
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

// installViaSetup 用已验签的安装包执行静默安装：先安排后台进程等本进程退出，
// 再运行 setup /S，由安装器替换 exe、更新卸载键与快捷方式。
// 安装包在启动安装器前会再校验一次长度与 SHA-256，失败不执行。
func (a *App) installViaSetup(pkg string, asset update.Asset, version string) (UpdateInstallView, error) {
	if err := update.VerifyFile(pkg, asset.Size, asset.SHA256); err != nil {
		return UpdateInstallView{}, fmt.Errorf("安装已取消：%w", err)
	}
	exe, err := os.Executable()
	if err != nil {
		return UpdateInstallView{}, fmt.Errorf("安装已取消：无法定位当前程序：%w", err)
	}
	if err := scheduleSilentInstall(pkg); err != nil {
		return UpdateInstallView{}, fmt.Errorf("安装已取消：无法启动静默安装器：%w", err)
	}
	pruneUpdateCache()
	if a.ctx != nil {
		go func() {
			time.Sleep(1200 * time.Millisecond)
			wruntime.Quit(a.ctx)
		}()
	}
	return UpdateInstallView{
		Version:    version,
		ExePath:    exe,
		Size:       asset.Size,
		SHA256:     asset.SHA256,
		Restarting: a.ctx != nil,
	}, nil
}

// isSetupPackage 判断清单资产是否是 NSIS 安装包（与 internal/update/github.go 的
// isSetupAsset 同一判据，避免把便携版 exe 误走安装器分支）。
func isSetupPackage(name string) bool {
	lower := strings.ToLower(strings.TrimSpace(name))
	if !strings.HasSuffix(lower, ".exe") {
		return false
	}
	return strings.Contains(lower, "setup") || strings.Contains(lower, "安装")
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

// resolvedSource 是本次检查/安装实际使用的清单来源。
// release 非空表示走内置默认源（GitHub 最新 release），为空表示用户手动覆盖。
type resolvedSource struct {
	manifestURL string
	release     *update.Release
}

func resolveUpdateSource(ctx context.Context, cfg UpdateSettings) (resolvedSource, error) {
	if url := strings.TrimSpace(cfg.ManifestURL); url != "" {
		return resolvedSource{manifestURL: url}, nil
	}
	rel, err := update.FetchLatestRelease(ctx, &http.Client{Timeout: 20 * time.Second},
		update.DefaultOwner, update.DefaultRepo)
	if err != nil {
		return resolvedSource{}, fmt.Errorf("自动更新源不可用：%w", err)
	}
	r := rel
	return resolvedSource{manifestURL: rel.ManifestURL, release: &r}, nil
}

// crossCheckRelease 校验清单版本与 GitHub release 标签一致，防止清单被错配。
func crossCheckRelease(rel *update.Release, manifestVersion string) error {
	if rel == nil {
		return nil
	}
	n, err := update.CompareVersion(manifestVersion, rel.Version)
	if err != nil {
		return fmt.Errorf("清单版本 %q 无法比较：%w", manifestVersion, err)
	}
	if n != 0 {
		return fmt.Errorf("release 标签 %s 与清单版本 %s 不一致", rel.Tag, manifestVersion)
	}
	return nil
}

// crossCheckAsset 用 release 资产校准安装包：名称、大小、SHA-256 必须与清单一致，
// 通过后把下载地址替换为 release 里的真实资产地址。签名校验仍按清单执行，不降级。
func crossCheckAsset(rel *update.Release, asset update.Asset) (update.Asset, error) {
	if rel == nil {
		return asset, nil
	}
	ra, ok := rel.AssetByName(asset.Name)
	if !ok {
		return asset, fmt.Errorf("release %s 不包含清单声明的安装包 %s（现有资产：%s）",
			rel.Tag, asset.Name, rel.AssetNames())
	}
	if ra.Size != asset.Size {
		return asset, fmt.Errorf("安装包大小不一致：release=%d 清单=%d", ra.Size, asset.Size)
	}
	if ra.Digest != "" && !strings.EqualFold(ra.Digest, asset.SHA256) {
		return asset, fmt.Errorf("安装包 SHA-256 不一致：release=%s 清单=%s", ra.Digest, asset.SHA256)
	}
	asset.URL = ra.URL
	return asset, nil
}

func sameVersion(a, b string) bool {
	if strings.TrimSpace(b) == "" {
		return false
	}
	n, err := update.CompareVersion(a, b)
	return err == nil && n == 0
}
