// scenes 子命令：对 13 组固定场景逐组比较 C# 基准与 Go 实现，并可落盘 golden。
//
// 用法：
//
//	parity scenes [-repo .] [-corpus testdata/corpus] [-golden testdata/golden/scan]
//	              [-csharp FileDedupTool.exe] [-write] [-only ids] [-ignore cache] [-keep-temp]
//
// -write 时调用 C# 的 --dump-scan-json 生成基准，MATCH 后把基准相对化写入 golden；
// 不带 -write 时直接读 golden 做回归。退出码：0 = 全 MATCH，1 = 有失败/差异，2 = 用法或环境错误。
package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"filededup/internal/engine/model"
)

// defaultCSharpExe 是 C# 参考实现的对拍入口（WinExe 子系统，os/exec 会正常等待退出）。
const defaultCSharpExe = `F:\我的软件项目\文件去重\2026-11-13\rebuild\outputs\src\FileDedupTool\bin\Release\net8.0-windows\FileDedupTool.exe`

// sceneCase 是一个场景：目录同名，args 对 Go/C# 语义一致。
type sceneCase struct {
	id   string
	args []string
}

// sceneCases 固定为 13 组。精确扫描只支持 newest/oldest/nameShortest/pathShortest。
var sceneCases = []sceneCase{
	{"01-exact", []string{"--keep", "newest"}},
	{"02-unicode", nil},
	{"03-rename", []string{"--keep", "nameShortest"}},
	{"04-images", nil},
	{"05-audio", nil},
	{"06-links", nil},
	{"07-hidden", []string{"--include-hidden"}},
	{"08-long-path", nil},
	{"09-readonly", nil},
	{"10-organize", nil},
	{"11-special-delete", []string{"--keep", "oldest"}},
	{"12-filter", []string{"--exclude", "small", "--min-size", "10"}},
	{"13-ext", nil},
}

func runScenes(args []string) int {
	fs := flag.NewFlagSet("scenes", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	repoFlag := fs.String("repo", ".", "模块根目录")
	corpusFlag := fs.String("corpus", "", "语料根目录（默认 <repo>/testdata/corpus）")
	goldenFlag := fs.String("golden", "", "golden 目录（默认 <repo>/testdata/golden/scan）")
	csharpFlag := fs.String("csharp", defaultCSharpExe, "C# 参考可执行文件")
	write := fs.Bool("write", false, "调用 C# dump 对拍，MATCH 后写入 golden")
	onlyFlag := fs.String("only", "", "只跑指定场景 id，逗号分隔")
	ignoreFlag := fs.String("ignore", "cache", "忽略项，逗号分隔")
	keepTemp := fs.Bool("keep-temp", false, "保留临时目录")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if fs.NArg() > 0 {
		fmt.Fprintf(os.Stderr, "多余的参数：%v\n", fs.Args())
		return 2
	}

	repo, err := filepath.Abs(*repoFlag)
	if err != nil {
		fmt.Fprintf(os.Stderr, "无效的 -repo：%v\n", err)
		return 2
	}
	corpus := *corpusFlag
	if corpus == "" {
		corpus = filepath.Join(repo, "testdata", "corpus")
	}
	if corpus, err = filepath.Abs(corpus); err != nil {
		fmt.Fprintf(os.Stderr, "无效的 -corpus：%v\n", err)
		return 2
	}
	golden := *goldenFlag
	if golden == "" {
		golden = filepath.Join(repo, "testdata", "golden", "scan")
	}
	if golden, err = filepath.Abs(golden); err != nil {
		fmt.Fprintf(os.Stderr, "无效的 -golden：%v\n", err)
		return 2
	}
	ignore := map[string]bool{}
	for _, s := range strings.Split(*ignoreFlag, ",") {
		if s = strings.TrimSpace(s); s != "" {
			ignore[s] = true
		}
	}

	selected, err := selectScenes(*onlyFlag)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}

	if missing := missingSceneDirs(corpus, selected); len(missing) > 0 {
		fmt.Fprintf(os.Stderr, "语料根不完整：%s\n缺少场景目录：\n", corpus)
		for _, id := range missing {
			fmt.Fprintf(os.Stderr, "  %s\n", id)
		}
		return 2
	}
	if *write {
		if _, err := os.Stat(*csharpFlag); err != nil {
			fmt.Fprintf(os.Stderr, "C# 可执行文件不可用 %s：%v\n", *csharpFlag, err)
			return 2
		}
	}

	tmp, err := os.MkdirTemp("", "fdedup-scenes-")
	if err != nil {
		fmt.Fprintf(os.Stderr, "创建临时目录失败：%v\n", err)
		return 2
	}
	if *keepTemp {
		fmt.Printf("临时目录：%s\n", tmp)
	} else {
		defer os.RemoveAll(tmp)
	}

	bin := filepath.Join(tmp, "fdedup")
	if runtime.GOOS == "windows" {
		bin += ".exe"
	}
	if out, code, err := runSceneCmd("go", []string{"build", "-o", bin, "./cmd/fdedup"}, repo); err != nil {
		fmt.Fprintf(os.Stderr, "构建 Go 二进制失败（退出码 %d）：%v\n%s\n", code, err, out)
		return 2
	}

	var failed []string
	for _, sc := range selected {
		if note, ok := runScene(sc, bin, corpus, golden, *csharpFlag, *write, ignore, tmp, repo); !ok {
			failed = append(failed, fmt.Sprintf("%s: %s", sc.id, note))
		}
	}

	if len(failed) == 0 {
		fmt.Printf("scenes: %d/%d MATCH\n", len(selected), len(selected))
		return 0
	}
	fmt.Printf("scenes: %d/%d MATCH\n", len(selected)-len(failed), len(selected))
	fmt.Println("失败清单：")
	for _, f := range failed {
		fmt.Printf("  %s\n", f)
	}
	return 1
}

// runScene 执行单个场景，返回失败说明；ok=false 时由调用方汇总。
func runScene(sc sceneCase, bin, corpus, golden, csharp string, write bool, ignore map[string]bool,
	tmp, repo string) (string, bool) {
	sceneDir := filepath.Join(corpus, sc.id)
	goJSON := filepath.Join(tmp, sc.id+".go.json")

	basePath := filepath.Join(golden, sc.id+".json")
	if write {
		csharpJSON := filepath.Join(tmp, sc.id+".csharp.json")
		argsC := append([]string{"--dump-scan-json", csharpJSON, "--root", sceneDir}, sc.args...)
		if out, code, err := runSceneCmd(csharp, argsC, repo); err != nil {
			fmt.Printf("[%s] C# 失败 退出码=%d\n%s\n", sc.id, code, strings.TrimSpace(out))
			return fmt.Sprintf("C# 退出码 %d", code), false
		}
		basePath = csharpJSON
	}

	base, err := loadScan(basePath)
	if err != nil {
		fmt.Printf("[%s] 读取基准失败：%v\n", sc.id, err)
		return "读取基准失败", false
	}

	argsGo := append([]string{"scan", "--root", sceneDir, "--dump", goJSON}, sc.args...)
	if out, code, err := runSceneCmd(bin, argsGo, repo); err != nil {
		fmt.Printf("[%s] Go 失败 退出码=%d\n%s\n", sc.id, code, strings.TrimSpace(out))
		return fmt.Sprintf("Go 退出码 %d", code), false
	}
	cand, err := loadScan(goJSON)
	if err != nil {
		fmt.Printf("[%s] 读取 Go 结果失败：%v\n", sc.id, err)
		return "读取 Go 结果失败", false
	}

	nb := normalizeScan(base, sceneDir)
	nc := normalizeScan(cand, sceneDir)
	d := newDiffer(0)
	compareScan(d, nb, nc, ignore)
	res := d.finish("scan", len(nb.Groups), len(nc.Groups))

	if res.Result != "MATCH" {
		fmt.Printf("[%s] DIFF diffs=%d\n", sc.id, res.Summary.DiffCount)
		for _, item := range res.Diffs {
			fmt.Println(item.String())
		}
		return fmt.Sprintf("DIFF diffs=%d", res.Summary.DiffCount), false
	}

	fmt.Printf("[%s] MATCH groups=%d compared=%d\n", sc.id, len(nb.Groups), res.Summary.Compared)
	if write {
		relativizeScan(base, sceneDir)
		if err := writeGolden(golden, sc.id, base); err != nil {
			fmt.Printf("[%s] 写入 golden 失败：%v\n", sc.id, err)
			return "写入 golden 失败", false
		}
	}
	return "", true
}

// runSceneCmd 执行外部命令并捕获合并输出，返回退出码。
func runSceneCmd(name string, args []string, dir string) (string, int, error) {
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	var buf bytes.Buffer
	cmd.Stdout = &buf
	cmd.Stderr = &buf
	err := cmd.Run()
	if err == nil {
		return buf.String(), 0, nil
	}
	if ee, ok := err.(*exec.ExitError); ok {
		return buf.String(), ee.ExitCode(), err
	}
	return buf.String(), -1, err
}

// relativizeScan 把结果中的绝对路径改为相对语料根的形式，golden 才能跨机器复现。
func relativizeScan(r *model.Result, sceneDir string) {
	for gi := range r.Groups {
		g := &r.Groups[gi]
		g.Keep = relPath(g.Keep, sceneDir)
		for mi := range g.Members {
			g.Members[mi].Path = relPath(g.Members[mi].Path, sceneDir)
		}
	}
	for si := range r.Skipped {
		r.Skipped[si].Path = relPath(r.Skipped[si].Path, sceneDir)
	}
}

func writeGolden(goldenDir, id string, r *model.Result) error {
	raw, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return err
	}
	raw = append(raw, '\n')
	if err := os.MkdirAll(goldenDir, 0o755); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(goldenDir, id+".json"), raw, 0o644)
}

// selectScenes 按 -only 过滤，未知 id 视为用法错误。
func selectScenes(only string) ([]sceneCase, error) {
	if strings.TrimSpace(only) == "" {
		return sceneCases, nil
	}
	want := map[string]bool{}
	for _, s := range strings.Split(only, ",") {
		if s = strings.TrimSpace(s); s != "" {
			want[s] = true
		}
	}
	if len(want) == 0 {
		return nil, fmt.Errorf("-only 未指定有效场景 id")
	}
	var out []sceneCase
	for _, sc := range sceneCases {
		if want[sc.id] {
			out = append(out, sc)
			delete(want, sc.id)
		}
	}
	if len(want) > 0 {
		var unknown []string
		for id := range want {
			unknown = append(unknown, id)
		}
		return nil, fmt.Errorf("未知场景 id：%s", strings.Join(unknown, ","))
	}
	return out, nil
}

func missingSceneDirs(corpus string, scenes []sceneCase) []string {
	var missing []string
	for _, sc := range scenes {
		info, err := os.Stat(filepath.Join(corpus, sc.id))
		if err != nil || !info.IsDir() {
			missing = append(missing, sc.id)
		}
	}
	return missing
}
