// Package main 是 fdedup 命令行入口（--nogui 能力的基础）。
//
// M1 提供 scan（精确查重，只读）；M2 增加 plan / apply（可审查、可回放、可回滚）
// 与 machine-id（授权方案 B 的机器码 v2 采集）。
// 输出契约见 internal/engine/model。
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

const version = "0.3.0-m3"

func usage() {
	fmt.Fprintf(os.Stderr, `fdedup %s

用法:
  fdedup version
  fdedup scan --root <dir> [--root <dir>...] [选项]
  fdedup plan --scan <result.json> [--permanent] [--dump <plan.json>]
  fdedup apply --plan <plan.json> (--dry-run | --yes) [选项]
  fdedup machine-id [--json]

scan 选项:
  --exclude <list>     排除项，逗号/分号/空白分隔；纯名称匹配目录名
  --min-size <n>       最小文件大小（字节），0=不限
  --include-hidden     包含隐藏/系统文件
  --follow-links       跟随符号链接（默认不跟随，与 C# 一致）
  --keep <policy>      newest|oldest|largest|nameShortest|pathShortest（默认 pathShortest）
  --no-exact           关闭精确查重（可与 --image/--audio 组合）
  --image              开启相似图片判定（jpg/jpeg/png/gif/bmp/tif/tiff/webp）
  --image-threshold <n> 图片 dHash 汉明阈值，0-64（默认 8）
  --image-keep <policy> 相似图片保留策略（默认 largest）
  --image-skip-uniform 无行内梯度的退化指纹（纯色图）不参与相似分组（默认关闭，与原版一致）
  --audio              开启相似音频判定（mp3/wav）
  --audio-threshold <n> 音频时长容差（秒，默认 2）
  --workers <n>        哈希并发数，0=自动
  --no-cache           禁用哈希缓存
  --request <file>     读取 JSON 请求（model.Request），显式参数覆盖
  --json               结果输出到 stdout
  --dump <file>        结果写入文件（2 空格缩进）

plan 选项:
  --scan <file>        扫描结果 JSON（必填）
  --permanent          永久删除（默认移入回收站）；会标记 irreversible
  --request-id <id>    计划 ID（默认沿用扫描 requestId）
  --json               计划输出到 stdout（否则只打印摘要）
  --dump <file>        计划写入文件

apply 选项:
  --plan <file>        计划 JSON（必填）
  --dry-run            只做执行前复核，不改动文件系统
  --yes                确认执行（必须与 --dry-run 二选一）
  --continue-on-error  单条失败后继续执行（默认中止后续）
  --journal-dir <dir>  操作清单目录（默认 <配置目录>/filededup/logs/ops）
  --no-journal         不写操作清单（不建议）
  --out <file>         执行报告写入文件
  --json               执行报告输出到 stdout

退出码: 0 成功 / 2 参数或输入错误 / 3 执行失败或内部错误 / 4 用户取消
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
	case "plan":
		os.Exit(runPlan(os.Args[2:]))
	case "apply":
		os.Exit(runApply(os.Args[2:]))
	case "machine-id":
		os.Exit(runMachineID(os.Args[2:]))
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
	noExact := fs.Bool("no-exact", false, "关闭精确查重")
	image := fs.Bool("image", false, "开启相似图片判定")
	imageThreshold := fs.Int("image-threshold", 8, "图片 dHash 汉明阈值（0-64）")
	imageKeep := fs.String("image-keep", "", "相似图片保留策略")
	imageSkipUniform := fs.Bool("image-skip-uniform", false, "退化指纹（纯色图）不参与相似分组")
	audio := fs.Bool("audio", false, "开启相似音频判定")
	audioThreshold := fs.Int("audio-threshold", 2, "音频时长容差（秒）")
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
	if *noExact {
		req.Exact = false
	}
	if *image || set["image-threshold"] || *imageKeep != "" || *imageSkipUniform {
		opts := model.ImageOpts{Enabled: true, Threshold: 8}
		if req.SimilarImage != nil {
			opts = *req.SimilarImage
			opts.Enabled = true
		}
		if set["image-threshold"] {
			opts.Threshold = *imageThreshold
		}
		if *imageSkipUniform {
			opts.SkipUniform = true
		}
		req.SimilarImage = &opts
	}
	if *imageKeep != "" {
		req.ImageKeepPolicy = model.KeepPolicy(*imageKeep)
	}
	if *audio || set["audio-threshold"] {
		opts := model.AudioOpts{Enabled: true, Tolerance: 2}
		if req.SimilarAudio != nil {
			opts = *req.SimilarAudio
			opts.Enabled = true
		}
		if set["audio-threshold"] {
			opts.Tolerance = *audioThreshold
		}
		req.SimilarAudio = &opts
	}
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
	uniform := 0
	for _, s := range r.Skipped {
		if s.Code == model.SkipUniformFingerprint {
			uniform++
		}
	}
	if uniform > 0 {
		fmt.Printf("退化指纹未参与相似分组：%d 个（--image-skip-uniform）\n", uniform)
	}
}
