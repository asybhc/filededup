// Package main 是许可证/机器码工具入口。授权方案待用户决策（A/B/C）。
package main

import (
	"fmt"
	"os"
)

func main() {
	fmt.Fprintln(os.Stderr, "fdedup-keygen 将在授权方案确定后实现（M9）。")
	os.Exit(2)
}
