// tools/parity 对拍器：比较两份确定性 JSON（C# 基准 vs Go 候选）。
//
// 用法：
//
//	parity scan --base csharp.json --cand go.json [--root 语料根] [-json] [-ignore ...]
//	parity plan --base csharp-plan.json --cand go-plan.json [--root 语料根] [-json]
//
// 退出码：0 = MATCH，1 = DIFF（字段级差异已列出），2 = 用法/读取错误。
// 归一化规则：绝对路径 → 相对 --root（缺省取 base.request.roots[0]）；路径统一 "/" 分隔并做 NFC；
// 时间统一为 UnixNano；组与成员排序固定，保证比较结果与输入顺序无关。
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"
)

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	switch os.Args[1] {
	case "scan":
		os.Exit(runScan(os.Args[2:]))
	case "plan":
		os.Exit(runPlan(os.Args[2:]))
	case "-h", "--help", "help":
		usage()
		os.Exit(0)
	default:
		fmt.Fprintf(os.Stderr, "未知子命令 %q\n", os.Args[1])
		usage()
		os.Exit(2)
	}
}

func usage() {
	fmt.Fprintf(os.Stderr, `用法:
  parity scan --base <csharp.json> --cand <go.json> [选项]
  parity plan --base <csharp-plan.json> --cand <go-plan.json> [选项]

选项:
  --root <dir>   语料根目录；缺省取 base.request.roots[0]
  -json          以 JSON 输出结果
  -max <n>       最多列出 n 条差异（默认 200，0=不限）
  -ignore <list> 忽略项，逗号分隔：
                 totalBytes(C# 计入 -1 的已知差异) skipped cache imageKey fingerprint modTime
`)
}

// common 是两个子命令共用的参数。
type common struct {
	base      string
	cand      string
	root      string
	jsonOut   bool
	maxDiffs  int
	ignoreRaw string
	ignore    map[string]bool
}

func (c *common) bindFlags(fs *flag.FlagSet) {
	fs.StringVar(&c.base, "base", "", "基准 JSON（C# 导出）")
	fs.StringVar(&c.cand, "cand", "", "候选 JSON（Go 导出）")
	fs.StringVar(&c.root, "root", "", "语料根目录；缺省取 base.request.roots[0]")
	fs.BoolVar(&c.jsonOut, "json", false, "以 JSON 输出")
	fs.IntVar(&c.maxDiffs, "max", 200, "最多列出差异条数，0=不限")
	fs.StringVar(&c.ignoreRaw, "ignore", "", "忽略项，逗号分隔")
}

func (c *common) validate() error {
	if c.base == "" || c.cand == "" {
		return fmt.Errorf("必须同时指定 --base 与 --cand")
	}
	for _, p := range []string{c.base, c.cand} {
		if _, err := os.Stat(p); err != nil {
			return fmt.Errorf("读取 %s 失败: %w", p, err)
		}
	}
	c.ignore = map[string]bool{}
	for _, s := range strings.Split(c.ignoreRaw, ",") {
		if s = strings.TrimSpace(s); s != "" {
			c.ignore[s] = true
		}
	}
	return nil
}

// result 是对拍结论，-json 时序列化输出。
type result struct {
	Result  string     `json:"result"` // MATCH | DIFF
	Diffs   []diffItem `json:"diffs"`
	Dropped int        `json:"dropped,omitempty"` // 超出 -max 未列出的差异数
	Summary summary    `json:"summary"`
}

func emit(res result, jsonOut bool) int {
	if res.Result == "MATCH" {
		res.Diffs = []diffItem{}
	}
	if jsonOut {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		_ = enc.Encode(res)
	} else {
		for _, d := range res.Diffs {
			fmt.Println(d.String())
		}
		if res.Dropped > 0 {
			fmt.Printf("... 另有 %d 条差异未列出（-max 限制）\n", res.Dropped)
		}
		fmt.Printf("summary: %s\n", res.Summary.String())
		fmt.Printf("RESULT: %s\n", res.Result)
	}
	if res.Result == "MATCH" {
		return 0
	}
	return 1
}

func runScan(args []string) int {
	var c common
	fs := flag.NewFlagSet("scan", flag.ContinueOnError)
	c.bindFlags(fs)
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if err := c.validate(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	base, err := loadScan(c.base)
	if err != nil {
		fmt.Fprintf(os.Stderr, "解析 base 失败: %v\n", err)
		return 2
	}
	cand, err := loadScan(c.cand)
	if err != nil {
		fmt.Fprintf(os.Stderr, "解析 cand 失败: %v\n", err)
		return 2
	}
	root := c.root
	if root == "" && len(base.Request.Roots) > 0 {
		root = base.Request.Roots[0]
	}
	nb, nc := normalizeScan(base, root), normalizeScan(cand, root)
	d := newDiffer(c.maxDiffs)
	compareScan(d, nb, nc, c.ignore)
	return emit(d.finish("scan", len(nb.Groups), len(nc.Groups)), c.jsonOut)
}

func runPlan(args []string) int {
	var c common
	fs := flag.NewFlagSet("plan", flag.ContinueOnError)
	c.bindFlags(fs)
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if err := c.validate(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	base, err := loadPlan(c.base)
	if err != nil {
		fmt.Fprintf(os.Stderr, "解析 base 失败: %v\n", err)
		return 2
	}
	cand, err := loadPlan(c.cand)
	if err != nil {
		fmt.Fprintf(os.Stderr, "解析 cand 失败: %v\n", err)
		return 2
	}
	root := c.root
	if root == "" && len(base.Actions) > 0 {
		root = commonDirOf(base.Actions)
	}
	nb, nc := normalizePlan(base, root), normalizePlan(cand, root)
	d := newDiffer(c.maxDiffs)
	comparePlan(d, nb, nc, c.ignore)
	return emit(d.finish("plan", len(nb.Actions), len(nc.Actions)), c.jsonOut)
}
