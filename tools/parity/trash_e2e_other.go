//go:build !windows

package main

import (
	"fmt"
	"os"
)

// runTrashE2E 仅 Windows 可用（回收站依赖 IFileOperation / Shell）。
func runTrashE2E([]string) int {
	fmt.Fprintln(os.Stderr, "trash-e2e 仅在 Windows 上可用")
	return 2
}
