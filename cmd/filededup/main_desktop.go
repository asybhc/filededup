//go:build desktop

package main

import (
	"log"

	"filededup"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
)

// main 是 Wails 桌面入口。需要先构建前端：
//
//	cd web && npm install && npm run build
//	go build -tags desktop -o bin/fdedup-ui.exe ./cmd/filededup
//
// 不构建桌面版（CI 的三平台 go build ./...）时走 main.go 的无 GUI 分支，
// 避免 Linux 上引入 WebKitGTK 的 cgo 依赖。
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
