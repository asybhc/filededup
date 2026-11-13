// Package main 是桌面壳入口。M5 接入 Wails v2（WebView2/WKWebView/WebKitGTK）。
//
// 当前为 M0 占位：只输出说明，不包含 UI 依赖，保证三平台 CI 可编译。
package main

import (
	"fmt"
	"os"
	"runtime"
)

func main() {
	fmt.Fprintf(os.Stderr, "filededup 桌面壳尚未接入（M5 计划：Wails v2），当前平台 %s/%s。\n", runtime.GOOS, runtime.GOARCH)
	os.Exit(1)
}
