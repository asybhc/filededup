// Package main 是 headless 本地服务壳入口（127.0.0.1 + 一次性 Token）。
// M5 接入 internal/api；当前为 M0 占位。
package main

import (
	"fmt"
	"os"
)

func main() {
	fmt.Fprintln(os.Stderr, "filededupd 本地服务壳尚未接入（M5 计划）。")
	os.Exit(1)
}
