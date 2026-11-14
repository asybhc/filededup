package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"filededup/internal/engine/similar/audio"
	"filededup/internal/engine/similar/image"
)

// similarDump 对应 C# --dump-similar-json 的输出。
type similarDump struct {
	Images []struct {
		Path        string `json:"path"`
		Fingerprint string `json:"fingerprint"`
		OK          bool   `json:"ok"`
	} `json:"images"`
	Audio []struct {
		Path       string `json:"path"`
		DurationMS int64  `json:"durationMs"`
		OK         bool   `json:"ok"`
	} `json:"audio"`
}

type similarMismatch struct {
	Kind   string `json:"kind"`
	Path   string `json:"path"`
	Detail string `json:"detail"`
}

func runSimilar(args []string) int {
	fs := flag.NewFlagSet("similar", flag.ContinueOnError)
	csharpPath := fs.String("csharp", "", "C# --dump-similar-json 输出")
	root := fs.String("root", "", "语料根目录（缺省取 C# 导出路径的公共目录）")
	allowRaw := fs.String("allow", "", "已知差异文件（相对路径，逗号分隔），仅列出不计入失败")
	jsonOut := fs.Bool("json", false, "以 JSON 输出")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *csharpPath == "" {
		fmt.Fprintln(os.Stderr, "必须指定 --csharp")
		return 2
	}
	data, err := os.ReadFile(*csharpPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "读取 %s 失败: %v\n", *csharpPath, err)
		return 2
	}
	var dump similarDump
	if err := json.Unmarshal(data, &dump); err != nil {
		fmt.Fprintf(os.Stderr, "解析 %s 失败: %v\n", *csharpPath, err)
		return 2
	}
	allow := map[string]bool{}
	for _, s := range strings.Split(*allowRaw, ",") {
		if s = strings.TrimSpace(s); s != "" {
			allow[filepath.ToSlash(s)] = true
		}
	}

	rootDir := *root
	if rootDir == "" {
		rootDir = commonRootOf(dump)
	}

	var mismatches []similarMismatch
	imgTotal, imgAgree, imgAllowed := 0, 0, 0
	for _, it := range dump.Images {
		imgTotal++
		rel := relToPath(rootDir, it.Path)
		got, ok := image.Hash(it.Path)
		detail := ""
		switch {
		case it.OK && !ok:
			detail = "C# 有指纹，Go 解码失败"
		case !it.OK && ok:
			detail = "C# 解码失败，Go 得到指纹"
		case it.OK && ok:
			if gotHex := image.Hex(got); gotHex != it.Fingerprint {
				detail = "指纹 " + it.Fingerprint + " → " + gotHex
			}
		}
		if detail == "" {
			imgAgree++
		} else if allow[rel] {
			imgAllowed++
			mismatches = append(mismatches, similarMismatch{Kind: "image(known)", Path: rel, Detail: detail})
		} else {
			mismatches = append(mismatches, similarMismatch{Kind: "image", Path: rel, Detail: detail})
		}
	}

	audTotal, audAgree, audAllowed := 0, 0, 0
	for _, it := range dump.Audio {
		audTotal++
		rel := relToPath(rootDir, it.Path)
		sec, ok := audio.Probe(it.Path)
		detail := ""
		switch {
		case it.OK && !ok:
			detail = "C# 有时长，Go 解析失败"
		case !it.OK && ok:
			detail = fmt.Sprintf("C# 解析失败，Go 得到 %dms", audio.DurationMS(sec))
		case it.OK && ok:
			if got := audio.DurationMS(sec); got != it.DurationMS {
				detail = fmt.Sprintf("时长 %dms → %dms", it.DurationMS, got)
			}
		}
		if detail == "" {
			audAgree++
		} else if allow[rel] {
			audAllowed++
			mismatches = append(mismatches, similarMismatch{Kind: "audio(known)", Path: rel, Detail: detail})
		} else {
			mismatches = append(mismatches, similarMismatch{Kind: "audio", Path: rel, Detail: detail})
		}
	}

	sort.Slice(mismatches, func(i, j int) bool {
		if mismatches[i].Kind != mismatches[j].Kind {
			return mismatches[i].Kind < mismatches[j].Kind
		}
		return mismatches[i].Path < mismatches[j].Path
	})
	failed := 0
	for _, m := range mismatches {
		if !strings.HasSuffix(m.Kind, "(known)") {
			failed++
		}
	}
	res := "MATCH"
	if failed > 0 {
		res = "DIFF"
	}
	if *jsonOut {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		_ = enc.Encode(map[string]any{
			"result": res,
			"images": map[string]int{"total": imgTotal, "agree": imgAgree, "known": imgAllowed},
			"audio":  map[string]int{"total": audTotal, "agree": audAgree, "known": audAllowed},
			"diffs":  mismatches,
		})
	} else {
		for _, m := range mismatches {
			fmt.Printf("%-12s %s: %s\n", m.Kind, m.Path, m.Detail)
		}
		fmt.Printf("images: %d/%d 一致", imgAgree, imgTotal)
		if imgAllowed > 0 {
			fmt.Printf("（另有 %d 条已登记差异）", imgAllowed)
		}
		fmt.Printf("；audio: %d/%d 一致", audAgree, audTotal)
		if audAllowed > 0 {
			fmt.Printf("（另有 %d 条已登记差异）", audAllowed)
		}
		fmt.Printf("\nRESULT: %s\n", res)
	}
	if res == "MATCH" {
		return 0
	}
	return 1
}

// commonRootOf 取所有条目所在目录的公共前缀，作为缺省 --root。
func commonRootOf(dump similarDump) string {
	var dirs []string
	for _, it := range dump.Images {
		dirs = append(dirs, filepath.Dir(it.Path))
	}
	for _, it := range dump.Audio {
		dirs = append(dirs, filepath.Dir(it.Path))
	}
	if len(dirs) == 0 {
		return ""
	}
	common := dirs[0]
	for _, d := range dirs[1:] {
		common = commonDir(common, d)
	}
	return common
}

func commonDir(a, b string) string {
	sep := string(filepath.Separator)
	ap := strings.Split(strings.TrimSuffix(a, sep), sep)
	bp := strings.Split(strings.TrimSuffix(b, sep), sep)
	i := 0
	for i < len(ap) && i < len(bp) && strings.EqualFold(ap[i], bp[i]) {
		i++
	}
	return strings.Join(ap[:i], sep)
}

func relToPath(root, path string) string {
	if root == "" {
		return filepath.ToSlash(path)
	}
	if rel, err := filepath.Rel(root, path); err == nil {
		return filepath.ToSlash(rel)
	}
	return filepath.ToSlash(path)
}
