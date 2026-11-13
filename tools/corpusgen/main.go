package main

import (
	"flag"
	"fmt"
	"os"
)

func main() {
	out := flag.String("out", "", "语料输出目录（必填）")
	seed := flag.Int64("seed", 20260113, "随机种子；同一 seed 输出可复现")
	scale := flag.Int("scale", 0, "生成 N 个文件的规模压测语料（替代内置场景）")
	flag.Parse()

	if *out == "" {
		fmt.Fprintln(os.Stderr, "用法: go run ./tools/corpusgen -out <dir> [-seed 20260113] [-scale N]")
		os.Exit(2)
	}
	if *scale > 0 {
		if err := GenerateScale(*out, *scale, *seed); err != nil {
			fmt.Fprintf(os.Stderr, "生成规模语料失败: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("scale corpus: %s (%d files)\n", *out, *scale)
		return
	}
	m, err := Generate(*out, *seed)
	if err != nil {
		fmt.Fprintf(os.Stderr, "生成语料失败: %v\n", err)
		os.Exit(1)
	}
	manifestPath, err := WriteManifest(*out, m)
	if err != nil {
		fmt.Fprintf(os.Stderr, "写清单失败: %v\n", err)
		os.Exit(1)
	}
	files, dirs := 0, 0
	for _, e := range m.Entries {
		if e.Kind == "dir" {
			dirs++
		} else {
			files++
		}
	}
	fmt.Printf("corpus: %s\n", *out)
	fmt.Printf("entries: %d (files %d, dirs %d)\n", len(m.Entries), files, dirs)
	fmt.Printf("pathListSHA256: %s\n", m.PathListSHA256)
	fmt.Printf("fileSetSHA256:  %s\n", m.FileSetSHA256)
	fmt.Printf("manifest: %s\n", manifestPath)
	for _, n := range m.Notes {
		fmt.Printf("note: %s\n", n)
	}
}
