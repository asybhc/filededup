//go:build desktop

package main

import (
	"log"

	"filededup"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
)

// main 是 Wails 桌面入口。需要先构建前端，然后走固定脚本：
//
//	cd web && npm install && npm run build
//	powershell -File scripts/build-desktop.ps1
//
// 脚本固定 `desktop,production` 两个 tag 并加上 `-H windowsgui`：
// 只带 desktop 会编入 Wails 的 missing-tags stub（运行只弹框、不开窗），
// 不加 windowsgui 则会先出现控制台黑窗。
func main() {
	app := NewApp()
	err := wails.Run(&options.App{
		Title:     "文件去重 FileDedup",
		Width:     1360,
		Height:    900,
		MinWidth:  1024,
		MinHeight: 680,
		AssetServer: &assetserver.Options{
			Assets: filededup.Assets,
		},
		BackgroundColour: &options.RGBA{R: 245, G: 246, B: 248, A: 255},
		OnStartup:        app.startup,
		OnShutdown:       app.shutdown,
		Bind:             []interface{}{app},
	})
	if err != nil {
		log.Fatal(err)
	}
}
