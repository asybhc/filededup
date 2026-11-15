//go:build desktop

package main

import (
	"errors"
	"os"
	"strings"

	"filededup/internal/auth"

	wruntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

// enforceLicense 允许桌面冒烟测试关闭授权门控；正式构建保持 true。
var enforceLicense = true

func requireLicense(feature string) error {
	if !enforceLicense {
		return nil
	}
	return auth.RequireFeature(feature)
}

// GetLicenseState 返回当前授权状态（许可证优先，其次试用期）。
func (a *App) GetLicenseState() auth.State {
	return auth.LoadState()
}

// GetMachineCode 返回本机机器码，供用户复制给签发方。
func (a *App) GetMachineCode() (string, error) {
	return auth.MachineCode()
}

// PickLicenseFile 打开文件选择框，返回用户选中的许可证文件。
func (a *App) PickLicenseFile() (string, error) {
	if a.ctx == nil {
		return "", errors.New("应用尚未完成初始化")
	}
	return wruntime.OpenFileDialog(a.ctx, wruntime.OpenDialogOptions{
		Title: "选择许可证文件",
		Filters: []wruntime.FileFilter{
			{DisplayName: "许可证文件 (*.lic;*.fdlic;*.txt)", Pattern: "*.lic;*.fdlic;*.txt"},
		},
	})
}

// ImportLicenseFile 选择并导入许可证；用户取消时返回当前状态与 nil。
func (a *App) ImportLicenseFile() (auth.State, error) {
	path, err := a.PickLicenseFile()
	if err != nil {
		return auth.LoadState(), err
	}
	if strings.TrimSpace(path) == "" {
		return auth.LoadState(), nil
	}
	info, err := os.Stat(path)
	if err != nil {
		return auth.LoadState(), err
	}
	if info.Size() > 64*1024 {
		return auth.LoadState(), errors.New("许可证文件过大（上限 64 KB）")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return auth.LoadState(), err
	}
	return a.ImportLicenseText(string(raw))
}

// ImportLicenseText 校验并保存粘贴进来的许可证文本。
func (a *App) ImportLicenseText(text string) (auth.State, error) {
	if strings.TrimSpace(text) == "" {
		return auth.LoadState(), errors.New("许可证内容为空")
	}
	if err := auth.ImportLicenseText(text); err != nil {
		return auth.LoadState(), err
	}
	return auth.LoadState(), nil
}
