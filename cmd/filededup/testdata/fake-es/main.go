// 命令行假 es.exe：仅用于测试，验证 -n / -path / 关键词参数确实传给了子进程。
// 行为对齐真实 es.exe：路径不存在时静默退出 0，输出带 BOM 的 CRLF 路径行。
package main

import (
	"bufio"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

func main() {
	if log := os.Getenv("FAKE_ES_LOG"); log != "" {
		if f, err := os.OpenFile(log, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644); err == nil {
			fmt.Fprintln(f, strings.Join(os.Args[1:], "\x1f"))
			f.Close()
		}
	}

	limit := 0
	root := ""
	query := ""
	args := os.Args[1:]
	for i := 0; i < len(args); i++ {
		switch strings.ToLower(args[i]) {
		case "-n":
			if i+1 < len(args) {
				if v, err := strconv.Atoi(args[i+1]); err == nil {
					limit = v
				}
				i++
			}
		case "-path":
			if i+1 < len(args) {
				root = args[i+1]
				i++
			}
		default:
			query = args[i]
		}
	}
	if root == "" {
		return
	}

	var paths []string
	_ = filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			return nil
		}
		if query != "" && !strings.Contains(strings.ToLower(p), strings.ToLower(query)) {
			return nil
		}
		paths = append(paths, p)
		return nil
	})

	w := bufio.NewWriter(os.Stdout)
	w.WriteString("\ufeff")
	for i, p := range paths {
		if limit > 0 && i >= limit {
			break
		}
		fmt.Fprintf(w, "%s\r\n", p)
	}
	w.Flush()
}
