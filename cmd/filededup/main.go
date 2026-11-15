//go:build !desktop

// Package main 是桌面壳入口。桌面版需要 -tags desktop 构建（Wails v2）。
//
// 默认（无 tag）分支只保留命令行提示，保证三平台 CI 的 go build ./... 不引入
// WebKitGTK/WebView2 依赖。
package main

import (
	"fmt"
	"os"
	"runtime"
)

func main() {
	fmt.Fprintf(os.Stderr, "这是无 GUI 构建。桌面版请先在 web/ 执行 npm run build，然后用 -tags desktop 构建（当前平台 %s/%s）。\n", runtime.GOOS, runtime.GOARCH)
	os.Exit(1)
}
