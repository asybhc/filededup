// plans 子命令：在临时目录生成确定性语料，逐项对拍 C#/Go 的计划层，并用 C# 计划驱动 Go 执行器验证终态。
//
// 用法：
//
//	parity plans [-repo .] [-csharp FileDedupTool.exe] [-groups 30] [-keep-temp]
//
// 覆盖：
//   - 回收站/永久删除两种计划模式逐条比较（动作、预检、汇总、顺序）；
//   - C# 计划 → Go apply --dry-run，文件系统不得变化；
//   - C# 计划 → Go apply --yes，剩余文件集合必须等于"语料全集 - 计划删除集"，且每组保留项仍在。
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"
)

func runPlans(args []string) int {
	fs := flag.NewFlagSet("plans", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	repoFlag := fs.String("repo", ".", "模块根目录")
	csharpFlag := fs.String("csharp", defaultCSharpExe, "C# 参考可执行文件")
	groupsFlag := fs.Int("groups", 30, "重复组数量（每组 3 个文件）")
	keepTemp := fs.Bool("keep-temp", false, "保留临时目录")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if fs.NArg() > 0 {
		fmt.Fprintf(os.Stderr, "多余的参数：%v\n", fs.Args())
		return 2
	}
	if *groupsFlag < 1 {
		fmt.Fprintln(os.Stderr, "-groups 必须为正数")
		return 2
	}
	repo, err := filepath.Abs(*repoFlag)
	if err != nil {
		fmt.Fprintf(os.Stderr, "无效的 -repo：%v\n", err)
		return 2
	}
	if _, err := os.Stat(*csharpFlag); err != nil {
		fmt.Fprintf(os.Stderr, "C# 可执行文件不可用 %s：%v\n", *csharpFlag, err)
		return 2
	}

	tmp, err := os.MkdirTemp("", "fdedup-plans-")
	if err != nil {
		fmt.Fprintf(os.Stderr, "创建临时目录失败：%v\n", err)
		return 2
	}
	tmp = longPath(tmp)
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

	corpus := filepath.Join(tmp, "corpus")
	if err := genPlanCorpus(corpus, *groupsFlag); err != nil {
		fmt.Fprintf(os.Stderr, "生成语料失败：%v\n", err)
		return 2
	}
	files := listFileKeys(corpus)
	fmt.Printf("[plans] 语料 %d 个文件，重复组 %d 个\n", len(files), *groupsFlag+2)

	csScan := filepath.Join(tmp, "csharp.scan.json")
	if out, code, err := runSceneCmd(*csharpFlag,
		[]string{"--dump-scan-json", csScan, "--root", corpus}, repo); err != nil {
		fmt.Fprintf(os.Stderr, "C# 扫描失败（退出码 %d）：%v\n%s\n", code, err, out)
		return 2
	}
	goScan := filepath.Join(tmp, "go.scan.json")
	if out, code, err := runSceneCmd(bin, []string{"scan", "--root", corpus, "--dump", goScan}, repo); err != nil {
		fmt.Fprintf(os.Stderr, "Go 扫描失败（退出码 %d）：%v\n%s\n", code, err, out)
		return 2
	}

	for _, mode := range []string{"trash", "permanent"} {
		if code := comparePlanMode(*csharpFlag, bin, corpus, tmp, repo, mode); code != 0 {
			return code
		}
	}

	// 执行层：在副本上重新生成 C# 计划，确保计划路径与执行目标一致。
	work := filepath.Join(tmp, "apply-copy")
	if err := copyTree(corpus, work); err != nil {
		fmt.Fprintf(os.Stderr, "复制语料失败：%v\n", err)
		return 2
	}
	csPlan := filepath.Join(tmp, "apply.csharp.plan.json")
	if out, code, err := runSceneCmd(*csharpFlag,
		[]string{"--dump-plan-json", csPlan, "--root", work, "--request-id", "parity-apply", "--permanent"}, repo); err != nil {
		fmt.Fprintf(os.Stderr, "C# 计划（执行副本）失败（退出码 %d）：%v\n%s\n", code, err, out)
		return 2
	}
	p, err := loadPlan(csPlan)
	if err != nil {
		fmt.Fprintf(os.Stderr, "读取 C# 计划失败：%v\n", err)
		return 2
	}
	if len(p.Actions) == 0 {
		fmt.Fprintln(os.Stderr, "C# 计划为空，无法验证执行层")
		return 1
	}

	before := listFileKeys(work)
	if out, code, err := runSceneCmd(bin, []string{"apply", "--plan", csPlan, "--dry-run", "--json"}, repo); err != nil {
		fmt.Fprintf(os.Stderr, "Go 演练失败（退出码 %d）：%v\n%s\n", code, err, out)
		return 1
	}
	if after := listFileKeys(work); !sameKeys(before, after) {
		fmt.Println("[plans/apply] DIFF 演练改动了文件系统")
		return 1
	}
	fmt.Printf("[plans/apply] 演练未改动文件（%d 个文件）\n", len(before))

	if out, code, err := runSceneCmd(bin, []string{"apply", "--plan", csPlan, "--yes", "--no-journal", "--json"}, repo); err != nil {
		fmt.Fprintf(os.Stderr, "Go 执行失败（退出码 %d）：%v\n%s\n", code, err, out)
		return 1
	}
	after := listFileKeys(work)
	deleted := make(map[string]bool, len(p.Actions))
	for _, a := range p.Actions {
		deleted[relPath(a.Src, work)] = true
	}
	var want []string
	for _, k := range before {
		if !deleted[keyRel(k)] {
			want = append(want, k)
		}
	}
	if !sameKeys(want, after) {
		fmt.Printf("[plans/apply] DIFF 剩余集合不符：期望 %d，实际 %d\n", len(want), len(after))
		printKeyDiff("缺少", want, after)
		printKeyDiff("多余", after, want)
		return 1
	}
	if err := verifyKeepers(csScan, work); err != nil {
		fmt.Printf("[plans/apply] DIFF %v\n", err)
		return 1
	}
	fmt.Printf("[plans/apply] 删除 %d 个文件，剩余 %d 个，终态 MATCH\n", len(p.Actions), len(after))
	fmt.Println("plans: MATCH")
	return 0
}

// comparePlanMode 用同一语料分别生成 C#/Go 计划并逐条比较。
func comparePlanMode(csharp, bin, corpus, tmp, repo, mode string) int {
	reqID := "parity-" + mode
	csPlan := filepath.Join(tmp, "csharp."+mode+".plan.json")
	goPlan := filepath.Join(tmp, "go."+mode+".plan.json")

	argsC := []string{"--dump-plan-json", csPlan, "--root", corpus, "--request-id", reqID}
	argsGo := []string{"plan", "--scan", filepath.Join(tmp, "go.scan.json"), "--request-id", reqID, "--dump", goPlan}
	if mode == "permanent" {
		argsC = append(argsC, "--permanent")
		argsGo = append(argsGo, "--permanent")
	}
	if out, code, err := runSceneCmd(csharp, argsC, repo); err != nil {
		fmt.Fprintf(os.Stderr, "C# 计划失败（退出码 %d）：%v\n%s\n", code, err, out)
		return 2
	}
	if out, code, err := runSceneCmd(bin, argsGo, repo); err != nil {
		fmt.Fprintf(os.Stderr, "Go 计划失败（退出码 %d）：%v\n%s\n", code, err, out)
		return 2
	}
	base, err := loadPlan(csPlan)
	if err != nil {
		fmt.Fprintf(os.Stderr, "读取 C# 计划失败：%v\n", err)
		return 2
	}
	cand, err := loadPlan(goPlan)
	if err != nil {
		fmt.Fprintf(os.Stderr, "读取 Go 计划失败：%v\n", err)
		return 2
	}
	nb, nc := normalizePlan(base, corpus), normalizePlan(cand, corpus)
	d := newDiffer(0)
	comparePlan(d, nb, nc, nil)
	res := d.finish("plan", len(nb.Actions), len(nc.Actions))
	if res.Result != "MATCH" {
		fmt.Printf("[plans/%s] PLAN DIFF diffs=%d\n", mode, res.Summary.DiffCount)
		for _, item := range res.Diffs {
			fmt.Println(item.String())
		}
		return 1
	}
	fmt.Printf("[plans/%s] PLAN MATCH actions=%d compared=%d\n", mode, len(nb.Actions), res.Summary.Compared)
	return 0
}

// genPlanCorpus 生成确定性语料：N 组三胞胎 + 中文组 + 非 BMP 字符组 + 唯一文件。
func genPlanCorpus(root string, groups int) error {
	sizes := []int{0, 1, 17, 1024, 4096, 65536}
	for g := 0; g < groups; g++ {
		data := make([]byte, sizes[g%len(sizes)]+g)
		for i := range data {
			data[i] = byte(g*31 + i*7)
		}
		dir := filepath.Join(root, fmt.Sprintf("g%03d", g))
		for _, name := range []string{"a.txt", "b.txt", "c.txt"} {
			if err := writeCorpusFile(filepath.Join(dir, name), data, g); err != nil {
				return err
			}
		}
	}
	special := []struct {
		dir   string
		names []string
	}{
		{"u-unicode", []string{"报告.txt", "报告-副本.txt"}},
		// U+FF00 与 U+1F600：UTF-8 与 UTF-16 的排序结果相反，用于验证路径排序口径。
		{"u-emoji", []string{"a.txt", "＀.txt", "😀.txt"}},
		{"unique", []string{"only.txt"}},
	}
	for i, sp := range special {
		data := []byte(fmt.Sprintf("special-%d", i))
		for _, name := range sp.names {
			if err := writeCorpusFile(filepath.Join(root, sp.dir, name), data, 100+i); err != nil {
				return err
			}
		}
	}
	return nil
}

func writeCorpusFile(path string, data []byte, seq int) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return err
	}
	ts := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC).Add(time.Duration(seq) * time.Minute)
	return os.Chtimes(path, ts, ts)
}

// copyTree 复制目录树，保留权限位与修改时间。
func copyTree(src, dst string) error {
	return filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if info.IsDir() {
			return os.MkdirAll(target, info.Mode().Perm()|0o200)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if err := os.WriteFile(target, data, info.Mode().Perm()|0o200); err != nil {
			return err
		}
		return os.Chtimes(target, info.ModTime(), info.ModTime())
	})
}

// listFileKeys 返回排序后的 "相对路径|大小|修改时间" 列表。
func listFileKeys(root string) []string {
	var keys []string
	_ = filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}
		rel, relErr := filepath.Rel(root, path)
		if relErr != nil {
			return nil
		}
		keys = append(keys, fmt.Sprintf("%s|%d|%d", filepath.ToSlash(rel), info.Size(), info.ModTime().UnixNano()))
		return nil
	})
	sort.Strings(keys)
	return keys
}

// keyRel 从 "相对路径|大小|时间" 中取出相对路径。
func keyRel(k string) string {
	if i := strings.IndexByte(k, '|'); i >= 0 {
		return k[:i]
	}
	return k
}

func sameKeys(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func printKeyDiff(label string, want, got []string) {
	have := make(map[string]bool, len(got))
	for _, k := range got {
		have[k] = true
	}
	n := 0
	for _, k := range want {
		if !have[k] {
			fmt.Printf("  %s：%s\n", label, k)
			if n++; n >= 5 {
				return
			}
		}
	}
}

// verifyKeepers 确认 C# 扫描结果中每个组的保留项都还在。
func verifyKeepers(scanPath, root string) error {
	r, err := loadScan(scanPath)
	if err != nil {
		return fmt.Errorf("读取 C# 扫描结果失败：%w", err)
	}
	for _, g := range r.Groups {
		if g.Keep == "" {
			continue
		}
		p := g.Keep
		if !filepath.IsAbs(p) {
			p = filepath.Join(root, filepath.FromSlash(p))
		}
		if _, err := os.Stat(p); err != nil {
			return fmt.Errorf("组 %s 的保留项缺失：%s", g.Key, p)
		}
	}
	return nil
}
