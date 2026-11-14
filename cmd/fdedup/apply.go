package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"filededup/internal/engine/apply"
	"filededup/internal/engine/model"
	"filededup/internal/engine/plan"
	"filededup/internal/logx"
	"filededup/internal/platform"
)

func runApply(args []string) int {
	fs := flag.NewFlagSet("apply", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	planPath := fs.String("plan", "", "计划 JSON")
	dryRun := fs.Bool("dry-run", false, "只复核不执行")
	yes := fs.Bool("yes", false, "确认执行")
	continueOnError := fs.Bool("continue-on-error", false, "单条失败后继续")
	journalDir := fs.String("journal-dir", "", "操作清单目录")
	noJournal := fs.Bool("no-journal", false, "不写操作清单")
	out := fs.String("out", "", "报告写入文件")
	jsonOut := fs.Bool("json", false, "报告输出到 stdout")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if fs.NArg() > 0 {
		fmt.Fprintf(os.Stderr, "apply 不接受位置参数：%v\n", fs.Args())
		return 2
	}
	if *planPath == "" {
		fmt.Fprintln(os.Stderr, "缺少 --plan")
		return 2
	}
	if *dryRun == *yes {
		fmt.Fprintln(os.Stderr, "必须且只能指定 --dry-run 或 --yes")
		return 2
	}

	raw, err := os.ReadFile(*planPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "读取计划失败：%v\n", err)
		return 2
	}
	var p model.Plan
	if err := json.Unmarshal(raw, &p); err != nil {
		fmt.Fprintf(os.Stderr, "解析计划失败：%v\n", err)
		return 2
	}
	if err := plan.Validate(&p); err != nil {
		fmt.Fprintf(os.Stderr, "计划校验失败：%v\n", err)
		return 2
	}

	exec := &apply.Executor{Trash: platform.Trash}
	if !*noJournal && !*dryRun {
		w, err := logx.OpenOps(*journalDir, p.RequestID, time.Now())
		if err != nil {
			fmt.Fprintf(os.Stderr, "打开操作清单失败：%v\n", err)
			return 3
		}
		defer w.Close()
		exec.Journal = w
		if !*jsonOut {
			fmt.Fprintf(os.Stderr, "操作清单：%s\n", w.Path())
		}
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	report, err := exec.Execute(ctx, &p, apply.Options{
		DryRun:          *dryRun,
		ContinueOnError: *continueOnError,
	})
	if report != nil {
		if *out != "" {
			if werr := writeJSON(*out, report); werr != nil {
				fmt.Fprintf(os.Stderr, "写入 %s 失败：%v\n", *out, werr)
				return 3
			}
		}
		if *jsonOut {
			if werr := writeJSON("", report); werr != nil {
				fmt.Fprintf(os.Stderr, "输出失败：%v\n", werr)
				return 3
			}
		} else {
			printApplyReport(report)
		}
		if report.JournalError != "" {
			fmt.Fprintf(os.Stderr, "警告：操作清单写入失败：%s\n", report.JournalError)
		}
	}
	if errors.Is(err, context.Canceled) {
		fmt.Fprintln(os.Stderr, "已取消")
		return 4
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "执行失败：%v\n", err)
		return 3
	}
	if report != nil && report.Failed > 0 {
		return 3
	}
	return 0
}

func printApplyReport(r *apply.Report) {
	mode := "执行"
	if r.DryRun {
		mode = "演练"
	}
	fmt.Printf("%s完成：动作 %d，成功 %d，失败 %d，跳过 %d", mode, r.Total, r.Applied, r.Failed, r.Skipped)
	if r.DryRun {
		fmt.Printf("，可执行 %d", r.DryRuns)
	}
	if r.RolledBack > 0 {
		fmt.Printf("，已回滚 %d", r.RolledBack)
	}
	fmt.Println()
	for _, a := range r.Actions {
		if a.Status == model.StatusFailed || a.Status == model.StatusSkipped && a.Err != "" {
			fmt.Printf("  [%s] %s：%s\n", a.Status, a.Src, a.Err)
		}
	}
}
