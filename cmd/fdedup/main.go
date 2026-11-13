// Package main 是 fdedup 命令行入口（--nogui 能力的基础）。
//
// M1 提供 scan（精确查重，只读）；输出契约见 internal/engine/model。
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"syscall"

	"filededup/internal/engine/model"
	"filededup/internal/engine/scan"
)

const version = "0.2.0-m1"

func usage() {
	fmt.Fprintf(os.Stderr, `fdedup %s

用法:
  fdedup version
  fdedup scan --root <dir> [--root <dir>...] [选项]

scan 选项:
  --exclude <list>     排除项，逗号/分号/空白分隔；纯名称匹配目录名
  --min-size <n>       最小文件大小（字节），0=不限
  --include-hidden     包含隐藏/系统文件
  --follow-links       跟随符号链接（默认不跟随，与 C# 一致）
  --keep <policy>      newest|oldest|nameShortest|pathShortest（默认 pathShortest）
  --workers <n>        哈希并发数，0=自动
  --no-cache           禁用哈希缓存
  --request <file>     读取 JSON 请求（model.Request），显式参数覆盖
  --json               结果输出到 stdout
  --dump <file>        结果写入文件（2 空格缩进）
`, version)
}

type multiFlag []string

func (m *multiFlag) String() string { return fmt.Sprint([]string(*m)) }
func (m *multiFlag) Set(v string) error {
	*m = append(*m, v)
	return nil
}

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	switch os.Args[1] {
	case "version", "--version", "-v":
		fmt.Printf("fdedup %s %s/%s %s\n", version, runtime.GOOS, runtime.GOARCH, runtime.Version())
	case "scan":
		os.Exit(runScan(os.Args[2:]))
	case "-h", "--help", "help":
		usage()
	default:
		fmt.Fprintf(os.Stderr, "未知子命令：%s\n", os.Args[1])
		usage()
		os.Exit(2)
	}
}

func runScan(args []string) int {
	fs := flag.NewFlagSet("scan", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	var roots multiFlag
	fs.Var(&roots, "root", "扫描根目录（可重复）")
	exclude := fs.String("exclude", "", "排除项")
	minSize := fs.Int64("min-size", 0, "最小文件大小")
	includeHidden := fs.Bool("include-hidden", false, "包含隐藏文件")
	followLinks := fs.Bool("follow-links", false, "跟随符号链接")
	keep := fs.String("keep", "", "保留策略")
	workers := fs.Int("workers", 0, "哈希并发数")
	noCache := fs.Bool("no-cache", false, "禁用缓存")
	requestPath := fs.String("request", "", "请求 JSON")
	jsonOut := fs.Bool("json", false, "输出到 stdout")
	dump := fs.String("dump", "", "输出到文件")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	set := map[string]bool{}
	fs.Visit(func(f *flag.Flag) { set[f.Name] = true })

	req := model.DefaultRequest()
	if *requestPath != "" {
		raw, err := os.ReadFile(*requestPath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "读取请求失败：%v\n", err)
			return 2
		}
		if err := json.Unmarshal(raw, &req); err != nil {
			fmt.Fprintf(os.Stderr, "解析请求失败：%v\n", err)
			return 2
		}
	}
	if len(roots) > 0 {
		req.Roots = roots
	}
	if set["exclude"] {
		req.Excludes = []string{*exclude}
	}
	if set["min-size"] {
		req.MinSize = *minSize
	}
	if *includeHidden {
		req.IncludeHidden = true
	}
	if *followLinks {
		req.FollowLinks = true
	}
	if *keep != "" {
		req.KeepPolicy = model.KeepPolicy(*keep)
	}
	if set["workers"] {
		req.Workers = *workers
	}
	if *noCache {
		req.UseCache = false
	}
	req.Exact = true
	req.SchemaVersion = model.SchemaVersion
	if req.KeepPolicy == "" {
		req.KeepPolicy = model.KeepPathShortest
	}

	absRoots := make([]string, 0, len(req.Roots))
	for _, r := range req.Roots {
		abs, err := filepath.Abs(r)
		if err != nil {
			fmt.Fprintf(os.Stderr, "无效根目录 %q：%v\n", r, err)
			return 2
		}
		info, err := os.Stat(abs)
		if err != nil {
			fmt.Fprintf(os.Stderr, "根目录不可用 %q：%v\n", abs, err)
			return 2
		}
		if !info.IsDir() {
			fmt.Fprintf(os.Stderr, "根目录不是目录：%s\n", abs)
			return 2
		}
		absRoots = append(absRoots, filepath.Clean(abs))
	}
	req.Roots = absRoots
	if req.Excludes == nil {
		req.Excludes = []string{}
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	res, err := scan.RunWithOptions(ctx, req, scan.Options{HiddenFile: hiddenFile})
	if err != nil && !errors.Is(err, context.Canceled) {
		var ve *model.ValidationError
		if errors.As(err, &ve) {
			fmt.Fprintf(os.Stderr, "%v\n", err)
			return 2
		}
		fmt.Fprintf(os.Stderr, "扫描失败：%v\n", err)
		return 3
	}
	if res != nil {
		if *dump != "" {
			if werr := writeJSON(*dump, res); werr != nil {
				fmt.Fprintf(os.Stderr, "写入 %s 失败：%v\n", *dump, werr)
				return 3
			}
		}
		if *jsonOut {
			if werr := writeJSON("", res); werr != nil {
				fmt.Fprintf(os.Stderr, "输出失败：%v\n", werr)
				return 3
			}
		}
		if !*jsonOut && *dump == "" {
			printSummary(res)
		}
	}
	if errors.Is(err, context.Canceled) {
		fmt.Fprintln(os.Stderr, "已取消")
		return 4
	}
	return 0
}

func writeJSON(path string, v any) error {
	raw, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	raw = append(raw, '\n')
	if path == "" {
		_, err = os.Stdout.Write(raw)
		return err
	}
	return os.WriteFile(path, raw, 0o644)
}

func printSummary(r *model.Result) {
	fmt.Printf("扫描完成：文件 %d，组 %d，可回收 %d 字节，跳过 %d，用时 %dms\n",
		r.Stats.FilesEnumerated, r.Stats.Groups, r.Stats.ReclaimableBytes, r.Stats.Skipped,
		func() int64 {
			if r.Runtime == nil {
				return 0
			}
			return r.Runtime.ElapsedMS
		}())
}
