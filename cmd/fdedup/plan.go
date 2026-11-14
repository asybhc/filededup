package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"

	"filededup/internal/engine/model"
	"filededup/internal/engine/plan"
)

func runPlan(args []string) int {
	fs := flag.NewFlagSet("plan", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	scanPath := fs.String("scan", "", "扫描结果 JSON")
	permanent := fs.Bool("permanent", false, "永久删除")
	requestID := fs.String("request-id", "", "计划 ID")
	jsonOut := fs.Bool("json", false, "输出到 stdout")
	dump := fs.String("dump", "", "写入文件")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if fs.NArg() > 0 {
		fmt.Fprintf(os.Stderr, "plan 不接受位置参数：%v\n", fs.Args())
		return 2
	}
	if *scanPath == "" {
		fmt.Fprintln(os.Stderr, "缺少 --scan")
		return 2
	}
	raw, err := os.ReadFile(*scanPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "读取扫描结果失败：%v\n", err)
		return 2
	}
	var res model.Result
	if err := json.Unmarshal(raw, &res); err != nil {
		fmt.Fprintf(os.Stderr, "解析扫描结果失败：%v\n", err)
		return 2
	}

	mode := model.DeleteTrash
	if *permanent {
		mode = model.DeletePermanent
	}
	p, err := plan.FromScan(&res, plan.Options{RequestID: *requestID, DeleteMode: mode})
	if err != nil {
		fmt.Fprintf(os.Stderr, "生成计划失败：%v\n", err)
		return 3
	}
	if err := plan.Validate(p); err != nil {
		fmt.Fprintf(os.Stderr, "计划自检失败：%v\n", err)
		return 3
	}

	if *dump != "" {
		if werr := writeJSON(*dump, p); werr != nil {
			fmt.Fprintf(os.Stderr, "写入 %s 失败：%v\n", *dump, werr)
			return 3
		}
	}
	if *jsonOut {
		if werr := writeJSON("", p); werr != nil {
			fmt.Fprintf(os.Stderr, "输出失败：%v\n", werr)
			return 3
		}
	}
	if !*jsonOut {
		printPlanSummary(p, *dump)
	}
	return 0
}

func printPlanSummary(p *model.Plan, dumpPath string) {
	s := p.Summary
	fmt.Printf("计划 %s：动作 %d，删除 %d（回收站 %d / 永久 %d），不可逆 %d，可释放 %d 字节\n",
		p.RequestID, s.Actions, s.Deletes, s.Trash, s.Permanent, s.Irreversible, s.ReclaimableBytes)
	if dumpPath != "" {
		fmt.Printf("计划已写入：%s\n", dumpPath)
	}
	fmt.Println("下一步：fdedup apply --plan <plan.json> --dry-run 预览，确认后加 --yes 执行")
}
