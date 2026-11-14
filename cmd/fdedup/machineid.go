package main

import (
	"flag"
	"fmt"
	"os"

	"filededup/internal/platform"
)

// machineKeyReport 是 machine-id --json 的输出契约。
type machineKeyReport struct {
	MachineID string                `json:"machineId"`
	Parts     platform.MachineParts `json:"parts"`
}

func runMachineID(args []string) int {
	fs := flag.NewFlagSet("machine-id", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	jsonOut := fs.Bool("json", false, "输出采集明细")
	verify := fs.String("verify", "", "仅校验收到的机器码是否与当前机器一致")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if fs.NArg() > 0 {
		fmt.Fprintf(os.Stderr, "machine-id 不接受位置参数：%v\n", fs.Args())
		return 2
	}

	parts, err := platform.MachineIDParts()
	if err != nil {
		fmt.Fprintf(os.Stderr, "采集机器码失败：%v\n", err)
		return 3
	}
	id, err := platform.MachineID()
	if err != nil {
		fmt.Fprintf(os.Stderr, "生成机器码失败：%v\n", err)
		return 3
	}

	if *verify != "" {
		want, err := platform.ParseMachineID(*verify)
		if err != nil {
			fmt.Fprintf(os.Stderr, "机器码格式非法：%v\n", err)
			return 2
		}
		got, err := platform.ParseMachineID(id)
		if err != nil {
			fmt.Fprintf(os.Stderr, "本机机器码异常：%v\n", err)
			return 3
		}
		if string(want) != string(got) {
			fmt.Fprintln(os.Stderr, "不匹配")
			return 3
		}
		fmt.Println("匹配")
		return 0
	}

	if *jsonOut {
		if err := writeJSON("", machineKeyReport{MachineID: id, Parts: parts}); err != nil {
			fmt.Fprintf(os.Stderr, "输出失败：%v\n", err)
			return 3
		}
		return 0
	}
	fmt.Println(id)
	return 0
}
