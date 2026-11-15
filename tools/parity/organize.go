// organize 子命令族：整理器（organize）对拍工具链。
//
// 用法：
//
//	parity organize-scenes [--list] [--scene <id>] [--dest <dir>]
//	parity organize-run --root <dir> [--out <json>] [--target <相对子目录>] [整理器参数]
//	parity organize-compare --base <csharp.json> --cand <go.json> [-json] [-max N]
//	parity organize-batch --exe <FileDedupTool.exe> --work <dir> [--scenes a,b] [--orders ...]
//	                     [--conflicts ...] [--dry-run] [--matrix] [--out <summary.json>]
//
// 语料由代码确定性生成（固定字节 + 固定 mtime），不提交任何二进制文件；
// organize-run 复刻 OrganizeDump.cs 的 JSON 结构，organize-compare 逐项比较两份导出。
// 退出码：0 = MATCH，1 = DIFF，2 = 用法/读取错误。
package main

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"flag"
	"fmt"
	stdimage "image"
	stdcolor "image/color"
	stdpng "image/png"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"filededup/internal/engine/organize"
	"filededup/internal/orgscan"
)

// orgCorpusEpoch 是语料的固定基准时间，保证 mtime 可复现。
var orgCorpusEpoch = time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)

// orgDefaultSteps 与 OrganizeDump.cs 的 --steps 缺省值一致。
const orgDefaultSteps = "prefix,suffix,special,readonly,content,image,audio,organize,empty"

// ---------------------------------------------------------------- 参数与场景

// orgOptions 是整理器的一次完整配置，CLI 与批次共用。
type orgOptions struct {
	Root      string
	Target    string
	Out       string
	Steps     string
	Mode      string
	Keyword   string
	Regex     string
	Fixed     string
	Conflict  string
	Delete    string
	Protect   string
	Keep      string
	ImageKeep string

	ImageThreshold int
	AudioTolerance int
	Strength       int
	MinSize        int64

	ConfirmedFile string
	AutoConfirm   bool
	Hardlink      bool
	IncludeHidden bool
	Exclude       string

	DryRun  bool
	Preview bool
}

// defaultOrgOptions 返回与 OrganizeDump.cs 一致的缺省配置。
func defaultOrgOptions() orgOptions {
	return orgOptions{
		Steps:          orgDefaultSteps,
		Mode:           "category",
		Conflict:       "overwrite",
		Delete:         "trash",
		Keep:           "pathshortest",
		ImageKeep:      "largest",
		ImageThreshold: 8,
		AudioTolerance: 2,
		Strength:       1,
	}
}

// orgScene 是一个确定性语料场景；args 中的非零字段覆盖缺省配置。
type orgScene struct {
	id   string
	args orgOptions
	gen  func(root string) ([]string, error)
}

// options 合并场景覆盖项与缺省值。
func (s orgScene) options() orgOptions {
	o := defaultOrgOptions()
	in := s.args
	if in.Steps != "" {
		o.Steps = in.Steps
	}
	if in.Mode != "" {
		o.Mode = in.Mode
	}
	if in.Keyword != "" {
		o.Keyword = in.Keyword
	}
	if in.Regex != "" {
		o.Regex = in.Regex
	}
	if in.Fixed != "" {
		o.Fixed = in.Fixed
	}
	if in.Protect != "" {
		o.Protect = in.Protect
	}
	if in.Keep != "" {
		o.Keep = in.Keep
	}
	if in.ImageKeep != "" {
		o.ImageKeep = in.ImageKeep
	}
	if in.ImageThreshold > 0 {
		o.ImageThreshold = in.ImageThreshold
	}
	if in.AudioTolerance > 0 {
		o.AudioTolerance = in.AudioTolerance
	}
	if in.Strength > 0 {
		o.Strength = in.Strength
	}
	if in.Exclude != "" {
		o.Exclude = in.Exclude
	}
	if in.MinSize > 0 {
		o.MinSize = in.MinSize
	}
	if in.Delete != "" {
		o.Delete = in.Delete
	}
	if in.Hardlink {
		o.Hardlink = true
	}
	if in.Preview {
		o.Preview = true
	}
	return o
}

// orgScenes 是全部场景；顺序即 --list 顺序。
var orgScenes = []orgScene{
	{id: "basic", gen: genOrgBasic},
	{id: "rename", args: orgOptions{Steps: "prefix,suffix"}, gen: genOrgRename},
	{id: "special", args: orgOptions{Steps: "prefix,suffix,special,readonly,organize,empty", Protect: "重要"}, gen: genOrgSpecial},
	{id: "conflict", args: orgOptions{Steps: "prefix,suffix"}, gen: genOrgConflict},
	{id: "organize-category", args: orgOptions{Steps: "organize"}, gen: genOrgOrganizeFiles},
	{id: "organize-ext", args: orgOptions{Steps: "organize", Mode: "extension"}, gen: genOrgOrganizeFiles},
	{id: "organize-fixed", args: orgOptions{Steps: "organize", Mode: "fixedfolder", Fixed: "归档"}, gen: genOrgOrganizeFiles},
	{id: "organize-keyword", args: orgOptions{Steps: "organize", Mode: "keyword", Keyword: "报告,总结"}, gen: genOrgKeyword},
	{id: "organize-regex", args: orgOptions{Steps: "organize", Mode: "namefeature", Regex: `^img_\d+`}, gen: genOrgRegex},
	{id: "dedup-content", args: orgOptions{Steps: "content"}, gen: genOrgDedupContent},
	{id: "hardlink-content", args: orgOptions{Steps: "content", Hardlink: true}, gen: genOrgDedupContent},
	{id: "dedup-image", args: orgOptions{Steps: "image", ImageKeep: "largest", ImageThreshold: 8}, gen: genOrgDedupImage},
	{id: "dedup-audio", args: orgOptions{Steps: "audio", AudioTolerance: 2}, gen: genOrgDedupAudio},
	{id: "emptydirs", args: orgOptions{Steps: "empty"}, gen: genOrgEmptyDirs},
	{id: "readonly", args: orgOptions{Steps: "readonly"}, gen: genOrgReadonly},
	{id: "hidden", gen: genOrgHidden},
	{id: "mixed", gen: genOrgMixed},
}

// selectOrgScenes 按逗号列表过滤场景；空列表返回全部。
func selectOrgScenes(list string) ([]orgScene, error) {
	if strings.TrimSpace(list) == "" {
		return orgScenes, nil
	}
	want := map[string]bool{}
	for _, s := range strings.Split(list, ",") {
		if s = strings.TrimSpace(s); s != "" {
			want[s] = true
		}
	}
	if len(want) == 0 {
		return nil, fmt.Errorf("未指定有效场景 id")
	}
	var out []orgScene
	for _, sc := range orgScenes {
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
		sort.Strings(unknown)
		return nil, fmt.Errorf("未知场景 id：%s", strings.Join(unknown, ","))
	}
	return out, nil
}

// ---------------------------------------------------------------- 语料生成

// orgSceneBuilder 负责落盘并记录文件名，统一固定 mtime。
type orgSceneBuilder struct {
	root  string
	seq   int
	files []string
}

func newOrgSceneBuilder(root string) *orgSceneBuilder {
	return &orgSceneBuilder{root: root}
}

// file 写入一个普通文件，mtime 按序号递增。
func (b *orgSceneBuilder) file(rel string, data []byte) error {
	p := filepath.Join(b.root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(p, data, 0o644); err != nil {
		return err
	}
	ts := orgCorpusEpoch.Add(time.Duration(b.seq) * time.Minute)
	b.seq++
	if err := os.Chtimes(p, ts, ts); err != nil {
		return err
	}
	b.files = append(b.files, rel)
	return nil
}

// dir 创建一个空目录。
func (b *orgSceneBuilder) dir(rel string) error {
	return os.MkdirAll(filepath.Join(b.root, filepath.FromSlash(rel)), 0o755)
}

// readonly 写入文件并置只读位。
func (b *orgSceneBuilder) readonly(rel string, data []byte) error {
	if err := b.file(rel, data); err != nil {
		return err
	}
	return os.Chmod(filepath.Join(b.root, filepath.FromSlash(rel)), 0o444)
}

// hidden 写入文件并置隐藏属性（非 Windows 平台改为 "." 前缀）。
func (b *orgSceneBuilder) hidden(rel string, data []byte) error {
	real := hiddenRel(rel)
	if err := b.file(real, data); err != nil {
		return err
	}
	return setFileHidden(filepath.Join(b.root, filepath.FromSlash(real)))
}

// finish 返回排序后的文件清单。
func (b *orgSceneBuilder) finish() ([]string, error) {
	sort.Strings(b.files)
	return b.files, nil
}

// checkerPNG 生成确定性棋盘格 PNG：同一个 seed 在不同尺寸下 dHash 近似一致。
func checkerPNG(w, h, seed int) ([]byte, error) {
	img := stdimage.NewRGBA(stdimage.Rect(0, 0, w, h))
	cell := w / 8
	if cell < 1 {
		cell = 1
	}
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			v := uint8(32)
			if (x/cell+y/cell+seed)%2 == 0 {
				v = 224
			}
			img.Set(x, y, stdcolor.RGBA{R: v, G: v, B: v, A: 255})
		}
	}
	var buf bytes.Buffer
	if err := stdpng.Encode(&buf, img); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// wavBytes 生成最小合法 WAV（PCM、单声道、8bit），时长 = seconds。
func wavBytes(seconds, sampleRate int, seed byte) []byte {
	dataSize := sampleRate * seconds
	var b bytes.Buffer
	writeU32 := func(v uint32) { _ = binary.Write(&b, binary.LittleEndian, v) }
	writeU16 := func(v uint16) { _ = binary.Write(&b, binary.LittleEndian, v) }
	b.WriteString("RIFF")
	writeU32(uint32(36 + dataSize))
	b.WriteString("WAVE")
	b.WriteString("fmt ")
	writeU32(16)
	writeU16(1)                  // PCM
	writeU16(1)                  // 单声道
	writeU32(uint32(sampleRate)) // 采样率
	writeU32(uint32(sampleRate)) // 字节率
	writeU16(1)                  // 块对齐
	writeU16(8)                  // 位深
	b.WriteString("data")
	writeU32(uint32(dataSize))
	data := make([]byte, dataSize)
	for i := range data {
		data[i] = byte(i*13) + seed
	}
	b.Write(data)
	return b.Bytes()
}

func genOrgBasic(root string) ([]string, error) {
	b := newOrgSceneBuilder(root)
	png, err := checkerPNG(64, 64, 0)
	if err != nil {
		return nil, err
	}
	for _, it := range []struct {
		rel  string
		data []byte
	}{
		{"readme.txt", []byte("basic-readme\n")},
		{"docs/guide.md", []byte("# guide\n\nstep 1\n")},
		{"docs/notes.txt", []byte("notes-0123456789\n")},
		{"archive/data.zip", []byte("PK\x03\x04fake-zip-payload")},
		{"media/logo.png", png},
		{"empty.bin", nil},
	} {
		if err := b.file(it.rel, it.data); err != nil {
			return nil, err
		}
	}
	return b.finish()
}

func genOrgRename(root string) ([]string, error) {
	b := newOrgSceneBuilder(root)
	for _, it := range []struct {
		rel  string
		data []byte
	}{
		{"01 报告.txt", []byte("rename-prefix-digit")},
		{"20240102 简介.txt", []byte("rename-prefix-date")},
		{"副本 文档.txt", []byte("rename-prefix-copy")},
		{"a-3-b.txt", []byte("rename-middle")},
		{"file - 副本.txt", []byte("rename-suffix")},
		{"01-02-03 记录.txt", []byte("rename-multi-digit")},
		{"无扩展名文件", []byte("rename-no-ext")},
		{"trailing.", []byte("rename-dot")},
	} {
		if err := b.file(it.rel, it.data); err != nil {
			return nil, err
		}
	}
	return b.finish()
}

func genOrgSpecial(root string) ([]string, error) {
	b := newOrgSceneBuilder(root)
	for _, it := range []struct {
		rel  string
		data []byte
	}{
		{"新建文本文档.txt", []byte("junk-new-text")},
		{"副本 - 副本.txt", []byte("junk-copy")},
		{"~$临时.doc", []byte("junk-lock")},
		{"1.txt", []byte("junk-digit")},
		{"Thumbs.db", []byte("junk-thumbs")},
		{"重要资料.txt", []byte("protect-hit")},
		{"正常文件.txt", []byte("normal")},
	} {
		if err := b.file(it.rel, it.data); err != nil {
			return nil, err
		}
	}
	return b.finish()
}

func genOrgConflict(root string) ([]string, error) {
	b := newOrgSceneBuilder(root)
	for _, it := range []struct {
		rel  string
		data []byte
	}{
		{"文档.txt", []byte("conflict-target")},
		{"文档 (2).txt", []byte("conflict-suffix")},
		{"01 文档.txt", []byte("conflict-prefix")},
	} {
		if err := b.file(it.rel, it.data); err != nil {
			return nil, err
		}
	}
	return b.finish()
}

func genOrgOrganizeFiles(root string) ([]string, error) {
	b := newOrgSceneBuilder(root)
	for _, it := range []struct {
		rel  string
		data []byte
	}{
		{"a.txt", []byte("org-txt")},
		{"b.pdf", []byte("org-pdf")},
		{"c.jpg", []byte("org-jpg-not-an-image")},
		{"d.mp4", []byte("org-mp4")},
		{"e.zip", []byte("org-zip")},
		{"noext", []byte("org-noext")},
	} {
		if err := b.file(it.rel, it.data); err != nil {
			return nil, err
		}
	}
	return b.finish()
}

func genOrgKeyword(root string) ([]string, error) {
	b := newOrgSceneBuilder(root)
	for _, it := range []struct {
		rel  string
		data []byte
	}{
		{"报告1.txt", []byte("kw-report-1")},
		{"年度总结.txt", []byte("kw-summary")},
		{"其他.txt", []byte("kw-other")},
	} {
		if err := b.file(it.rel, it.data); err != nil {
			return nil, err
		}
	}
	return b.finish()
}

func genOrgRegex(root string) ([]string, error) {
	b := newOrgSceneBuilder(root)
	for _, it := range []struct {
		rel  string
		data []byte
	}{
		{"img_01.png", []byte("regex-img-1")},
		{"img_02.jpg", []byte("regex-img-2")},
		{"doc.txt", []byte("regex-doc")},
	} {
		if err := b.file(it.rel, it.data); err != nil {
			return nil, err
		}
	}
	return b.finish()
}

func genOrgDedupContent(root string) ([]string, error) {
	b := newOrgSceneBuilder(root)
	same := bytes.Repeat([]byte("content-dedup-"), 1024/14+1)[:1024]
	for _, name := range []string{"a.txt", "b.txt", "c.txt"} {
		if err := b.file(name, same); err != nil {
			return nil, err
		}
	}
	// 同大小不同内容：不应被内容去重删除。
	d := bytes.Repeat([]byte{0x11}, 512)
	e := bytes.Repeat([]byte{0x22}, 512)
	for _, it := range []struct {
		rel  string
		data []byte
	}{{"d.bin", d}, {"e.bin", e}, {"unique.txt", []byte("unique-content")}} {
		if err := b.file(it.rel, it.data); err != nil {
			return nil, err
		}
	}
	return b.finish()
}

func genOrgDedupImage(root string) ([]string, error) {
	b := newOrgSceneBuilder(root)
	for _, it := range []struct {
		rel  string
		w    int
		h    int
		seed int
	}{
		{"img1.png", 64, 64, 0},
		{"img2.png", 128, 128, 0},
		{"img3.png", 64, 64, 1},
	} {
		png, err := checkerPNG(it.w, it.h, it.seed)
		if err != nil {
			return nil, err
		}
		if err := b.file(it.rel, png); err != nil {
			return nil, err
		}
	}
	return b.finish()
}

func genOrgDedupAudio(root string) ([]string, error) {
	b := newOrgSceneBuilder(root)
	for _, it := range []struct {
		rel  string
		sec  int
		seed byte
	}{
		{"a.wav", 3, 0},
		{"b.wav", 3, 7},
		{"c.wav", 8, 0},
	} {
		if err := b.file(it.rel, wavBytes(it.sec, 8000, it.seed)); err != nil {
			return nil, err
		}
	}
	return b.finish()
}

func genOrgEmptyDirs(root string) ([]string, error) {
	b := newOrgSceneBuilder(root)
	for _, d := range []string{"x/y/z", "a/b", "keep/withfile"} {
		if err := b.dir(d); err != nil {
			return nil, err
		}
	}
	for _, it := range []struct {
		rel  string
		data []byte
	}{
		{"keep/withfile/f.txt", []byte("empty-dirs-keep")},
		{"root.txt", []byte("empty-dirs-root")},
	} {
		if err := b.file(it.rel, it.data); err != nil {
			return nil, err
		}
	}
	return b.finish()
}

func genOrgReadonly(root string) ([]string, error) {
	b := newOrgSceneBuilder(root)
	if err := b.readonly("ro1.txt", []byte("readonly-1")); err != nil {
		return nil, err
	}
	if err := b.readonly("ro2.txt", []byte("readonly-2")); err != nil {
		return nil, err
	}
	if err := b.file("rw.txt", []byte("writable")); err != nil {
		return nil, err
	}
	return b.finish()
}

func genOrgHidden(root string) ([]string, error) {
	b := newOrgSceneBuilder(root)
	if err := b.file("visible.txt", []byte("visible")); err != nil {
		return nil, err
	}
	if err := b.hidden("hidden.txt", []byte("hidden")); err != nil {
		return nil, err
	}
	return b.finish()
}

func genOrgMixed(root string) ([]string, error) {
	b := newOrgSceneBuilder(root)
	png, err := checkerPNG(64, 64, 0)
	if err != nil {
		return nil, err
	}
	type item struct {
		rel  string
		data []byte
	}
	items := []item{
		{"01 报告.txt", []byte("mixed-prefix")},
		{"20240101 报告.txt", []byte("mixed-date")},
		{"report - 副本.txt", []byte("mixed-suffix")},
		{"a.txt", bytes.Repeat([]byte("mixed-dedup-"), 100)},
		{"b.txt", bytes.Repeat([]byte("mixed-dedup-"), 100)},
		{"img1.png", png},
		{"img2.png", png},
		{"temp/新建文本文档.txt", []byte("mixed-junk")},
		{"temp/important.txt", []byte("mixed-important")},
	}
	for _, it := range items {
		if err := b.file(it.rel, it.data); err != nil {
			return nil, err
		}
	}
	if err := b.dir("temp/empty-nested/deep"); err != nil {
		return nil, err
	}
	if err := b.readonly("temp/ro.txt", []byte("mixed-readonly")); err != nil {
		return nil, err
	}
	return b.finish()
}

// ---------------------------------------------------------------- organize-scenes

func runOrganizeScenes(args []string) int {
	fs := flag.NewFlagSet("organize-scenes", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	listFlag := fs.Bool("list", false, "只列出场景 id")
	sceneFlag := fs.String("scene", "", "只生成指定场景 id")
	destFlag := fs.String("dest", "", "生成根目录；缺省使用临时目录")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *listFlag {
		for _, sc := range orgScenes {
			fmt.Println(sc.id)
		}
		return 0
	}
	selected, err := selectOrgScenes(*sceneFlag)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	dest := *destFlag
	if dest == "" {
		tmp, terr := os.MkdirTemp("", "fdedup-organize-scenes-")
		if terr != nil {
			fmt.Fprintf(os.Stderr, "创建临时目录失败：%v\n", terr)
			return 2
		}
		dest = tmp
		fmt.Printf("临时目录：%s\n", dest)
	} else if dest, err = filepath.Abs(dest); err != nil {
		fmt.Fprintf(os.Stderr, "无效的 --dest：%v\n", err)
		return 2
	}
	if err := os.MkdirAll(dest, 0o755); err != nil {
		fmt.Fprintf(os.Stderr, "创建目录失败：%v\n", err)
		return 2
	}
	for _, sc := range selected {
		root := filepath.Join(dest, sc.id)
		if err := os.RemoveAll(root); err != nil {
			fmt.Fprintf(os.Stderr, "清理 %s 失败：%v\n", root, err)
			return 2
		}
		if err := os.MkdirAll(root, 0o755); err != nil {
			fmt.Fprintf(os.Stderr, "创建 %s 失败：%v\n", root, err)
			return 2
		}
		files, gerr := sc.gen(root)
		if gerr != nil {
			fmt.Fprintf(os.Stderr, "[%s] 生成失败：%v\n", sc.id, gerr)
			return 2
		}
		fmt.Printf("[%s] %d 个文件 → %s\n", sc.id, len(files), root)
		for _, f := range files {
			fmt.Printf("  %s\n", f)
		}
	}
	return 0
}

// ---------------------------------------------------------------- 快照与导出

// orgDumpFile 对齐 OrganizeDump.cs 的 files[] 元素；与 CLI 共用同一枚举结构。
type orgDumpFile = orgscan.File

// orgDump 与 OrganizeDump.cs 的 JSON 完全同构。
type orgDump struct {
	SchemaVersion int              `json:"schemaVersion"`
	Root          string           `json:"root"`
	Target        string           `json:"target"`
	DryRun        bool             `json:"dryRun"`
	Counters      organize.Counter `json:"counters"`
	Dirs          []string         `json:"dirs"`
	Files         []orgDumpFile    `json:"files"`
	// PreviewPlan 仅预览计划模式下输出：原路径（小写）→ 保留路径（小写）。
	// 原版应用没有该导出入口，C# 侧由 OrganizeDump.cs 的诊断重建提供。
	PreviewPlan map[string]string `json:"previewPlan,omitempty"`
}

// orgTree 是一次完整枚举的结果（导出用 + 规划器快照用）。
type orgTree = orgscan.Tree

// collectOrgTree 枚举 root 下全部目录与文件：相对 '/' 路径、大小、只读、SHA-256。
// 实现放在 internal/orgscan，CLI 与对拍工具共用，避免两侧枚举语义漂移。
func collectOrgTree(root string) (orgTree, error) { return orgscan.Collect(root) }

// ---------------------------------------------------------------- 配置映射

func mapOrgConflict(s string) organize.ConflictPolicy { return organize.ParseConflictPolicy(s) }

func mapOrgDelete(s string) organize.DeleteMode { return organize.ParseDeleteMode(s) }

func mapOrgKeep(s string) organize.KeepRule { return organize.ParseKeepRule(s) }

func mapOrgMode(s string) organize.OrganizeMode { return organize.ParseOrganizeMode(s) }

// buildOrgConfig 把 CLI 参数翻译成 organize.Config。
func buildOrgConfig(o orgOptions, root string) (organize.Config, error) {
	cfg := organize.DefaultConfig()
	cfg.Steps = organize.ParseSteps(o.Steps)
	cfg.Order = organize.OrderStandalone
	if o.Preview {
		cfg.Order = organize.OrderPreview
	}
	cfg.DryRun = o.DryRun
	cfg.Conflict = mapOrgConflict(o.Conflict)
	cfg.Delete = mapOrgDelete(o.Delete)
	cfg.HardlinkMerge = o.Hardlink
	cfg.Excludes = organize.SplitKeywords(o.Exclude)
	cfg.MinSize = o.MinSize
	cfg.SkipHiddenSystem = !o.IncludeHidden
	cfg.ProtectWords = o.Protect
	cfg.Strength = o.Strength
	cfg.AutoDeleteConfirmed = o.AutoConfirm
	cfg.KeepRule = mapOrgKeep(o.Keep)
	cfg.ImageKeepRule = mapOrgKeep(o.ImageKeep)
	if cfg.ImageKeepRule == "" {
		cfg.ImageKeepRule = organize.KeepLargest
	}
	cfg.ImageThreshold = o.ImageThreshold
	cfg.AudioToleranceSec = o.AudioTolerance
	cfg.OrganizeMode = mapOrgMode(o.Mode)
	cfg.Keyword = o.Keyword
	cfg.Regex = o.Regex
	cfg.FixedFolder = o.Fixed
	if o.ConfirmedFile != "" {
		raw, err := os.ReadFile(o.ConfirmedFile)
		if err != nil {
			return cfg, fmt.Errorf("读取 --confirmed 失败: %w", err)
		}
		cfg.ConfirmedKeys = map[string]bool{}
		for _, line := range strings.Split(string(raw), "\n") {
			p := strings.TrimSpace(line)
			if p == "" {
				continue
			}
			rel := p
			if filepath.IsAbs(p) {
				if r, rerr := filepath.Rel(root, p); rerr == nil {
					rel = r
				}
			}
			rel = strings.TrimRight(filepath.ToSlash(rel), "/")
			cfg.ConfirmedKeys[organize.Fold(rel)] = true
		}
	}
	if cfg.Strength < 0 || cfg.Strength > 2 {
		cfg.Strength = 1
	}
	return cfg, nil
}

// ---------------------------------------------------------------- organize-run

func bindOrgRunFlags(fs *flag.FlagSet, o *orgOptions) {
	def := defaultOrgOptions()
	fs.StringVar(&o.Root, "root", "", "语料根目录")
	fs.StringVar(&o.Target, "target", "", "整理目标目录（root 下的相对子目录）")
	fs.StringVar(&o.Out, "out", "", "输出 JSON 路径；缺省写 stdout")
	fs.StringVar(&o.Steps, "steps", def.Steps, "启用步骤，逗号分隔")
	fs.StringVar(&o.Mode, "mode", def.Mode, "整理方式：category|extension|fixedfolder|keyword|namefeature")
	fs.StringVar(&o.Keyword, "keyword", "", "关键字模式的词表")
	fs.StringVar(&o.Regex, "regex", "", "名字特征模式的正则")
	fs.StringVar(&o.Fixed, "fixed", "", "指定文件夹模式的子目录名")
	fs.StringVar(&o.Conflict, "conflict", def.Conflict, "冲突策略：overwrite|skip|rename")
	fs.StringVar(&o.Delete, "delete", def.Delete, "删除方式：permanent|trash")
	fs.StringVar(&o.Protect, "protect", "", "保护词，逗号/空格分隔")
	fs.StringVar(&o.Keep, "keep", def.Keep, "保留规则：pathshortest|newest|oldest|largest|nameshortest")
	fs.StringVar(&o.ImageKeep, "image-keep", def.ImageKeep, "图片保留规则，缺省 largest")
	fs.IntVar(&o.ImageThreshold, "image-threshold", def.ImageThreshold, "图片汉明距离阈值")
	fs.IntVar(&o.AudioTolerance, "audio-tolerance", def.AudioTolerance, "音频时长容差（秒）")
	fs.IntVar(&o.Strength, "strength", def.Strength, "特殊文件判定强度 0|1|2")
	fs.Int64Var(&o.MinSize, "min-size", 0, "最小文件字节数")
	fs.StringVar(&o.ConfirmedFile, "confirmed", "", "已确认存疑文件清单（每行一个路径）")
	fs.BoolVar(&o.AutoConfirm, "auto-confirm", false, "自动删除已确认存疑项")
	fs.BoolVar(&o.Hardlink, "hardlink", false, "硬链接合并")
	fs.BoolVar(&o.IncludeHidden, "include-hidden", false, "包含隐藏/系统文件")
	fs.StringVar(&o.Exclude, "exclude", "", "排除目录名，逗号/空格分隔")
	fs.BoolVar(&o.DryRun, "dry-run", false, "只演练不落盘")
	fs.BoolVar(&o.Preview, "preview-plan", false, "使用预览计划（先去重后整理）")
}

func runOrganizeRun(args []string) int {
	var o orgOptions
	fs := flag.NewFlagSet("organize-run", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	bindOrgRunFlags(fs, &o)
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if fs.NArg() > 0 {
		fmt.Fprintf(os.Stderr, "多余的参数：%v\n", fs.Args())
		return 2
	}
	if strings.TrimSpace(o.Root) == "" {
		fmt.Fprintln(os.Stderr, "必须指定 --root")
		return 2
	}
	dump, err := executeOrgRun(o)
	if err != nil {
		fmt.Fprintf(os.Stderr, "organize-run 失败：%v\n", err)
		return 2
	}
	if o.Out != "" {
		if err := writeOrgDump(o.Out, dump); err != nil {
			fmt.Fprintf(os.Stderr, "写入 %s 失败：%v\n", o.Out, err)
			return 2
		}
		fmt.Printf("organize-run: files=%d dirs=%d renamed=%d specialDeleted=%d moved=%d contentDeleted=%d imageDeleted=%d audioDeleted=%d emptyDirs=%d\n",
			len(dump.Files), len(dump.Dirs), dump.Counters.Renamed, dump.Counters.SpecialDeleted,
			dump.Counters.Moved, dump.Counters.ContentDeleted, dump.Counters.ImageDeleted,
			dump.Counters.AudioDeleted, dump.Counters.EmptyDirs)
		return 0
	}
	raw, err := json.MarshalIndent(dump, "", "  ")
	if err != nil {
		fmt.Fprintf(os.Stderr, "序列化失败：%v\n", err)
		return 2
	}
	fmt.Println(string(raw))
	return 0
}

// executeOrgRun 是 organize-run / organize-batch 共用的完整流程。
func executeOrgRun(o orgOptions) (orgDump, error) {
	root, err := filepath.Abs(o.Root)
	if err != nil {
		return orgDump{}, fmt.Errorf("无效的 --root：%w", err)
	}
	info, err := os.Stat(root)
	if err != nil {
		return orgDump{}, fmt.Errorf("root 不可访问：%w", err)
	}
	if !info.IsDir() {
		return orgDump{}, fmt.Errorf("root 不是目录：%s", root)
	}

	targetRel, targetAbs := "", ""
	if strings.TrimSpace(o.Target) != "" {
		abs := o.Target
		if !filepath.IsAbs(abs) {
			abs = filepath.Join(root, filepath.FromSlash(o.Target))
		}
		rel, rerr := filepath.Rel(root, abs)
		if rerr != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return orgDump{}, fmt.Errorf("--target 必须是 root 的子目录：%s", o.Target)
		}
		targetRel = filepath.ToSlash(rel)
		targetAbs = filepath.Join(root, filepath.FromSlash(targetRel))
	}

	cfg, err := buildOrgConfig(o, root)
	if err != nil {
		return orgDump{}, err
	}
	tree, err := collectOrgTree(root)
	if err != nil {
		return orgDump{}, err
	}
	providers := prepareOrgProviders(tree.Entries, root, cfg)
	snap := buildOrgSnapshot(root, targetRel, tree)
	if o.Preview {
		snap.PreviewPlan = buildOrgPreviewPlan(cfg, snap, providers)
	}

	res := organize.Plan(cfg, snap, providers)
	// 即使试运行也调用 Apply：原版试运行会创建目标目录，其余动作不落盘。
	{
		hooks := orgExecHooks()
		if strings.EqualFold(o.Delete, "trash") {
			// 平台回收站实现由 platform 注入，不在整理器内重写；
			// 非 Windows 构建下 orgTrash 为 nil，退化为永久删除。
			hooks.Trash = orgTrash
		}
		rep, aerr := organize.Apply(root, res, organize.ExecOptions{DryRun: o.DryRun}, hooks)
		if aerr != nil {
			return orgDump{}, aerr
		}
		if rep.Failed > 0 {
			fmt.Fprintf(os.Stderr, "organize-run: %d 条动作执行失败\n", rep.Failed)
			for _, line := range rep.Errors {
				fmt.Fprintf(os.Stderr, "  %s\n", line)
			}
		}
	}

	after, err := collectOrgTree(root)
	if err != nil {
		return orgDump{}, err
	}
	dump := orgDump{
		SchemaVersion: 1,
		Root:          root,
		Target:        targetAbs,
		DryRun:        o.DryRun,
		Counters:      res.Counters,
		Dirs:          after.Dirs,
		Files:         after.Files,
	}
	if o.Preview {
		dump.PreviewPlan = snap.PreviewPlan
	}
	return dump, nil
}

// buildOrgSnapshot 把枚举结果装成规划器输入。
func buildOrgSnapshot(root, targetRel string, tree orgTree) organize.Snapshot {
	return organize.Snapshot{Root: root, Target: targetRel, Files: tree.Entries, Dirs: tree.Dirs}
}

// prepareOrgProviders 预计算图片 dHash / 音频时长，并返回规划器所需的 Providers。
// 实现放在 internal/orgscan，CLI 与对拍工具共用。
func prepareOrgProviders(entries []organize.FileEntry, root string, cfg organize.Config) organize.Providers {
	return orgscan.PrepareProviders(entries, root, cfg)
}

// ---------------------------------------------------------------- 预览计划

// buildOrgPreviewPlan 构造“原路径（小写）→ 保留路径（小写）”映射。
// 组顺序与扫描一致：内容 → 图片 → 音频；同文件命中多组时后者覆盖前者。
// 生成逻辑与 CLI 共用，见 organize.BuildPreviewPlan。
func buildOrgPreviewPlan(cfg organize.Config, snap organize.Snapshot, providers organize.Providers) map[string]string {
	return organize.BuildPreviewPlan(cfg, snap, providers).Map()
}

func writeOrgDump(path string, dump orgDump) error {
	raw, err := json.MarshalIndent(dump, "", "  ")
	if err != nil {
		return err
	}
	if dir := filepath.Dir(path); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
	}
	return os.WriteFile(path, append(raw, '\n'), 0o644)
}

func loadOrgDump(path string) (*orgDump, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var d orgDump
	if err := json.Unmarshal(raw, &d); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return &d, nil
}

// ---------------------------------------------------------------- organize-compare

// orgCompare 是逐项 diff 的结论（-json 时序列化）。
type orgCompare struct {
	Result   string             `json:"result"`
	Counters orgCompareCounters `json:"counters"`
	Dirs     orgCompareSet      `json:"dirs"`
	Files    orgCompareFiles    `json:"files"`
	// PreviewPlan 仅在任一侧输出 previewPlan 时出现；值为“原路径 → 保留路径”的逐条比较。
	PreviewPlan *orgCompareSet  `json:"previewPlan,omitempty"`
	Renamed     []orgRenamePair `json:"renamed"`
	Deleted     []string        `json:"deleted"`
	Added       []string        `json:"added"`
	Unpaired    []string        `json:"unpaired"`
	Diffs       []diffItem      `json:"diffs"`
	Dropped     int             `json:"dropped,omitempty"`
}

type orgCompareCounters struct {
	Matched int  `json:"matched"`
	Total   int  `json:"total"`
	Match   bool `json:"match"`
}

type orgCompareSet struct {
	Matched int     `json:"matched"`
	Union   int     `json:"union"`
	Rate    float64 `json:"rate"`
}

type orgCompareFiles struct {
	BaseCount int     `json:"baseCount"`
	CandCount int     `json:"candCount"`
	Matched   int     `json:"matched"`
	Union     int     `json:"union"`
	Rate      float64 `json:"rate"`
}

type orgRenamePair struct {
	Base   string `json:"base"`
	Cand   string `json:"cand"`
	SHA256 string `json:"sha256"`
}

type orgNormFile struct {
	Path     string
	Size     int64
	SHA256   string
	Readonly bool
}

// orgRelPath 把绝对路径相对化（root 不同也能比），统一 '/'。
func orgRelPath(p, root string) string {
	p = strings.TrimSpace(p)
	if p == "" {
		return ""
	}
	if filepath.IsAbs(p) || filepath.VolumeName(p) != "" {
		if root != "" {
			if rel, err := filepath.Rel(root, p); err == nil && rel != "." && !strings.HasPrefix(rel, "..") {
				p = rel
			}
		}
	}
	p = filepath.ToSlash(p)
	for strings.HasPrefix(p, "./") {
		p = p[2:]
	}
	return p
}

func orgRate(matched, union int) float64 {
	if union == 0 {
		return 1
	}
	return float64(matched) / float64(union)
}

// compareOrgDumps 逐字段比较 counters / dirs / files，并给出改名/删除/新增分类。
func compareOrgDumps(base, cand *orgDump, max int) orgCompare {
	d := newDiffer(max)
	out := orgCompare{
		Counters: orgCompareCounters{Total: 14},
		Files:    orgCompareFiles{BaseCount: len(base.Files), CandCount: len(cand.Files)},
		Renamed:  []orgRenamePair{},
		Deleted:  []string{},
		Added:    []string{},
		Unpaired: []string{},
	}

	counterPairs := []struct {
		name string
		b, c int
	}{
		{"renamed", base.Counters.Renamed, cand.Counters.Renamed},
		{"specialDeleted", base.Counters.SpecialDeleted, cand.Counters.SpecialDeleted},
		{"moved", base.Counters.Moved, cand.Counters.Moved},
		{"conflictSkip", base.Counters.ConflictSkip, cand.Counters.ConflictSkip},
		{"readonly", base.Counters.Readonly, cand.Counters.Readonly},
		{"organizeSkip", base.Counters.OrganizeSkip, cand.Counters.OrganizeSkip},
		{"contentDeleted", base.Counters.ContentDeleted, cand.Counters.ContentDeleted},
		{"imageDeleted", base.Counters.ImageDeleted, cand.Counters.ImageDeleted},
		{"audioDeleted", base.Counters.AudioDeleted, cand.Counters.AudioDeleted},
		{"emptyDirs", base.Counters.EmptyDirs, cand.Counters.EmptyDirs},
		{"excluded", base.Counters.Excluded, cand.Counters.Excluded},
		{"hiddenSkipped", base.Counters.HiddenSkipped, cand.Counters.HiddenSkipped},
		{"suspectsKept", base.Counters.SuspectsKept, cand.Counters.SuspectsKept},
		{"hardlinkMerged", base.Counters.HardlinkMerged, cand.Counters.HardlinkMerged},
	}
	for _, p := range counterPairs {
		if p.b == p.c {
			out.Counters.Matched++
			continue
		}
		d.add("counters."+p.name, strconv.Itoa(p.b), strconv.Itoa(p.c))
	}
	out.Counters.Match = out.Counters.Matched == out.Counters.Total

	// dirs：按小写折叠做集合比较，报告保留原路径。
	baseDirs := map[string]string{}
	for _, p := range base.Dirs {
		p = orgRelPath(p, base.Root)
		baseDirs[strings.ToLower(p)] = p
	}
	candDirs := map[string]string{}
	for _, p := range cand.Dirs {
		p = orgRelPath(p, cand.Root)
		candDirs[strings.ToLower(p)] = p
	}
	union := map[string]bool{}
	for k := range baseDirs {
		union[k] = true
	}
	for k := range candDirs {
		union[k] = true
	}
	out.Dirs.Union = len(union)
	for _, k := range sortedKeys(baseDirs) {
		if _, ok := candDirs[k]; ok {
			out.Dirs.Matched++
			continue
		}
		d.add("dirs["+baseDirs[k]+"]", "present", "missing_in_cand")
	}
	for _, k := range sortedKeys(candDirs) {
		if _, ok := baseDirs[k]; !ok {
			d.add("dirs["+candDirs[k]+"]", "absent", "extra_in_cand")
		}
	}
	out.Dirs.Rate = orgRate(out.Dirs.Matched, out.Dirs.Union)

	// previewPlan：原版没有导出入口，C# 侧为 OrganizeDump 的诊断重建；
	// 这里只比较两边都声称的“原路径 → 保留路径”映射。
	if base.PreviewPlan != nil || cand.PreviewPlan != nil {
		normPlan := func(m map[string]string, root string) map[string]string {
			out := map[string]string{}
			for k, v := range m {
				out[strings.ToLower(orgRelPath(k, root))] = strings.ToLower(orgRelPath(v, root))
			}
			return out
		}
		bp, cp := normPlan(base.PreviewPlan, base.Root), normPlan(cand.PreviewPlan, cand.Root)
		union := map[string]bool{}
		for k := range bp {
			union[k] = true
		}
		for k := range cp {
			union[k] = true
		}
		set := &orgCompareSet{Union: len(union)}
		for _, k := range sortedBoolKeys(union) {
			bv, bok := bp[k]
			cv, cok := cp[k]
			if bok && cok && bv == cv {
				set.Matched++
				continue
			}
			d.add("previewPlan["+k+"]", bv, cv)
		}
		set.Rate = orgRate(set.Matched, set.Union)
		out.PreviewPlan = set
	}

	// files：按折叠路径配对，逐字段比较。
	baseFiles := map[string]orgNormFile{}
	for _, f := range base.Files {
		p := orgRelPath(f.Path, base.Root)
		k := strings.ToLower(p)
		if _, dup := baseFiles[k]; dup {
			continue
		}
		baseFiles[k] = orgNormFile{Path: p, Size: f.Size, SHA256: f.SHA256, Readonly: f.Readonly}
	}
	candFiles := map[string]orgNormFile{}
	for _, f := range cand.Files {
		p := orgRelPath(f.Path, cand.Root)
		k := strings.ToLower(p)
		if _, dup := candFiles[k]; dup {
			continue
		}
		candFiles[k] = orgNormFile{Path: p, Size: f.Size, SHA256: f.SHA256, Readonly: f.Readonly}
	}

	fileUnion := map[string]bool{}
	for k := range baseFiles {
		fileUnion[k] = true
	}
	for k := range candFiles {
		fileUnion[k] = true
	}
	out.Files.Union = len(fileUnion)

	var baseOnly, candOnly []string
	for _, k := range sortedNormKeys(baseFiles) {
		b := baseFiles[k]
		c, ok := candFiles[k]
		if !ok {
			baseOnly = append(baseOnly, k)
			continue
		}
		out.Files.Matched++
		if b.Size != c.Size {
			d.add("files["+b.Path+"].size", strconv.FormatInt(b.Size, 10), strconv.FormatInt(c.Size, 10))
		}
		if b.SHA256 != c.SHA256 {
			d.add("files["+b.Path+"].sha256", short(b.SHA256), short(c.SHA256))
		}
		if b.Readonly != c.Readonly {
			d.add("files["+b.Path+"].readonly", boolStr(b.Readonly), boolStr(c.Readonly))
		}
	}
	for _, k := range sortedNormKeys(candFiles) {
		if _, ok := baseFiles[k]; !ok {
			candOnly = append(candOnly, k)
		}
	}
	out.Files.Rate = orgRate(out.Files.Matched, out.Files.Union)

	// 改名配对：同 SHA-256 一对一贪心；空哈希无法验证身份，单列“无法配对”。
	candBySHA := map[string][]string{}
	for _, k := range candOnly {
		if c := candFiles[k]; c.SHA256 != "" {
			candBySHA[c.SHA256] = append(candBySHA[c.SHA256], k)
		}
	}
	usedCand := map[string]bool{}
	for _, k := range baseOnly {
		b := baseFiles[k]
		if b.SHA256 == "" {
			out.Unpaired = append(out.Unpaired, b.Path)
			d.add("unpaired["+b.Path+"]", b.Path, "no_pair")
			continue
		}
		pick := ""
		for _, ck := range candBySHA[b.SHA256] {
			if !usedCand[ck] {
				pick = ck
				break
			}
		}
		if pick == "" {
			out.Deleted = append(out.Deleted, b.Path)
			d.add("files["+b.Path+"]", "present", "missing_in_cand")
			continue
		}
		usedCand[pick] = true
		out.Renamed = append(out.Renamed, orgRenamePair{Base: b.Path, Cand: candFiles[pick].Path, SHA256: b.SHA256})
		d.add("rename["+b.Path+"]", b.Path, candFiles[pick].Path)
	}
	for _, k := range candOnly {
		if usedCand[k] {
			continue
		}
		out.Added = append(out.Added, candFiles[k].Path)
		d.add("files["+candFiles[k].Path+"]", "absent", "extra_in_cand")
	}

	out.Result = "MATCH"
	if d.total > 0 {
		out.Result = "DIFF"
	}
	out.Diffs = d.items
	if out.Diffs == nil {
		out.Diffs = []diffItem{}
	}
	out.Dropped = d.total - len(d.items)
	return out
}

func sortedKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func sortedBoolKeys(m map[string]bool) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func sortedNormKeys(m map[string]orgNormFile) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func runOrganizeCompare(args []string) int {
	fs := flag.NewFlagSet("organize-compare", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	basePath := fs.String("base", "", "基准 JSON（C# 导出）")
	candPath := fs.String("cand", "", "候选 JSON（Go 导出）")
	jsonOut := fs.Bool("json", false, "以 JSON 输出")
	maxDiffs := fs.Int("max", 200, "最多列出差异条数，0=不限")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *basePath == "" || *candPath == "" {
		fmt.Fprintln(os.Stderr, "必须同时指定 --base 与 --cand")
		return 2
	}
	base, err := loadOrgDump(*basePath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "解析 base 失败：%v\n", err)
		return 2
	}
	cand, err := loadOrgDump(*candPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "解析 cand 失败：%v\n", err)
		return 2
	}
	out := compareOrgDumps(base, cand, *maxDiffs)
	if *jsonOut {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		_ = enc.Encode(out)
	} else {
		printOrgCompare(out)
	}
	if out.Result == "MATCH" {
		return 0
	}
	return 1
}

func printOrgCompare(out orgCompare) {
	fmt.Printf("counters: %d/%d 字段一致\n", out.Counters.Matched, out.Counters.Total)
	fmt.Printf("dirs: %d/%d 集合一致（一致率 %.2f%%）\n", out.Dirs.Matched, out.Dirs.Union, out.Dirs.Rate*100)
	fmt.Printf("files: %d/%d 路径匹配（一致率 %.2f%%），base=%d cand=%d\n",
		out.Files.Matched, out.Files.Union, out.Files.Rate*100, out.Files.BaseCount, out.Files.CandCount)
	fmt.Printf("改名前路径: %d\n", len(out.Renamed))
	for _, r := range out.Renamed {
		fmt.Printf("  %s → %s\n", r.Base, r.Cand)
	}
	fmt.Printf("删除: %d\n", len(out.Deleted))
	for _, p := range out.Deleted {
		fmt.Printf("  %s\n", p)
	}
	fmt.Printf("新增: %d\n", len(out.Added))
	for _, p := range out.Added {
		fmt.Printf("  %s\n", p)
	}
	fmt.Printf("无法配对: %d\n", len(out.Unpaired))
	for _, p := range out.Unpaired {
		fmt.Printf("  %s\n", p)
	}
	for _, item := range out.Diffs {
		fmt.Println(item.String())
	}
	if out.Dropped > 0 {
		fmt.Printf("... 另有 %d 条差异未列出（-max 限制）\n", out.Dropped)
	}
	fmt.Printf("RESULT: %s\n", out.Result)
}

// ---------------------------------------------------------------- organize-batch

// orgBatchCase 是单个组合的对拍结论。
type orgBatchCase struct {
	Scene          string  `json:"scene"`
	Order          string  `json:"order"`
	Conflict       string  `json:"conflict"`
	DryRun         bool    `json:"dryRun"`
	Result         string  `json:"result"`
	Matched        int     `json:"matched"`
	Union          int     `json:"union"`
	FileRate       float64 `json:"fileRate"`
	CountersMatch  bool    `json:"countersMatch"`
	DirsMatch      bool    `json:"dirsMatch"`
	PreviewMatched int     `json:"previewMatched"`
	PreviewUnion   int     `json:"previewUnion"`
	PreviewRate    float64 `json:"previewRate"`
	FirstDiff      string  `json:"firstDiff,omitempty"`
	Deviation      string  `json:"deviation,omitempty"`
	Error          string  `json:"error,omitempty"`
}

// orgBatchSummary 是整批对拍的汇总。
type orgBatchSummary struct {
	Exe   string         `json:"exe"`
	Work  string         `json:"work"`
	Total int            `json:"total"`
	Match int            `json:"match"`
	Known int            `json:"known"`
	Cases []orgBatchCase `json:"cases"`
}

func runOrganizeBatch(args []string) int {
	fs := flag.NewFlagSet("organize-batch", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	exeFlag := fs.String("exe", defaultCSharpExe, "C# 参考可执行文件")
	workFlag := fs.String("work", "", "工作目录（必须显式指定）")
	scenesFlag := fs.String("scenes", "", "场景列表，逗号分隔；缺省全部")
	ordersFlag := fs.String("orders", "standalone", "顺序：standalone,preview")
	conflictsFlag := fs.String("conflicts", "overwrite", "冲突策略：overwrite,skip,rename")
	dryFlag := fs.Bool("dry-run", false, "只跑试运行")
	matrixFlag := fs.Bool("matrix", false, "展开全部（顺序 × 冲突 × 试运行）组合")
	goldenFlag := fs.String("golden", "", "基准目录：读取已归档的 C# 结果（CI 用，不需要 --exe）")
	writeGoldenFlag := fs.String("write-golden", "", "把本次 C# 结果归档到目录（需要 --exe）")
	outFlag := fs.String("out", "", "summary JSON 路径；缺省 <work>/summary.json")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if fs.NArg() > 0 {
		fmt.Fprintf(os.Stderr, "多余的参数：%v\n", fs.Args())
		return 2
	}
	if strings.TrimSpace(*workFlag) == "" {
		fmt.Fprintln(os.Stderr, "必须指定 --work")
		return 2
	}
	if *goldenFlag != "" && *writeGoldenFlag != "" {
		fmt.Fprintln(os.Stderr, "--golden 与 --write-golden 不能同时使用")
		return 2
	}
	work, err := filepath.Abs(*workFlag)
	if err != nil {
		fmt.Fprintf(os.Stderr, "无效的 --work：%v\n", err)
		return 2
	}
	if *goldenFlag == "" {
		if _, err := os.Stat(*exeFlag); err != nil {
			fmt.Fprintf(os.Stderr, "C# 可执行文件不可用 %s：%v\n", *exeFlag, err)
			return 2
		}
	}
	goldenDir := *goldenFlag
	if goldenDir != "" {
		if goldenDir, err = filepath.Abs(goldenDir); err != nil {
			fmt.Fprintf(os.Stderr, "无效的 --golden：%v\n", err)
			return 2
		}
	}
	writeGoldenDir := *writeGoldenFlag
	if writeGoldenDir != "" {
		if writeGoldenDir, err = filepath.Abs(writeGoldenDir); err != nil {
			fmt.Fprintf(os.Stderr, "无效的 --write-golden：%v\n", err)
			return 2
		}
	}
	selected, err := selectOrgScenes(*scenesFlag)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	orders, err := parseOrgList(*ordersFlag, []string{"standalone", "preview"})
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	conflicts, err := parseOrgList(*conflictsFlag, []string{"overwrite", "skip", "rename"})
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	dryRuns := []bool{*dryFlag}
	if *matrixFlag {
		orders = []string{"standalone", "preview"}
		conflicts = []string{"overwrite", "skip", "rename"}
		dryRuns = []bool{false, true}
	}
	if err := os.MkdirAll(work, 0o755); err != nil {
		fmt.Fprintf(os.Stderr, "创建 --work 失败：%v\n", err)
		return 2
	}

	sum := orgBatchSummary{Exe: *exeFlag, Work: work, Cases: []orgBatchCase{}}
	for _, sc := range selected {
		for _, order := range orders {
			for _, conflict := range conflicts {
				for _, dry := range dryRuns {
					c := runOrgBatchCase(*exeFlag, work, goldenDir, writeGoldenDir, sc, order, conflict, dry)
					sum.Cases = append(sum.Cases, c)
					sum.Total++
					switch c.Result {
					case "MATCH":
						sum.Match++
					case "KNOWN":
						sum.Known++
					}
					note := c.FirstDiff
					if c.Deviation != "" {
						note = "已登记偏差：" + c.Deviation
					}
					fmt.Printf("[%s/%s/%s/dry=%v] %s matched=%d/%d rate=%.2f%% counters=%v preview=%d/%d %.2f%% %s\n",
						sc.id, order, conflict, dry, c.Result, c.Matched, c.Union, c.FileRate*100,
						c.CountersMatch, c.PreviewMatched, c.PreviewUnion, c.PreviewRate*100, note)
				}
			}
		}
	}

	outPath := *outFlag
	if outPath == "" {
		outPath = filepath.Join(work, "summary.json")
	}
	if err := writeOrgBatchSummary(sum, outPath); err != nil {
		fmt.Fprintf(os.Stderr, "写入 summary 失败：%v\n", err)
		return 2
	}
	fmt.Printf("organize-batch: %d/%d MATCH，%d 条已登记偏差，summary=%s\n", sum.Match, sum.Total, sum.Known, outPath)
	if sum.Total > 0 && sum.Match+sum.Known == sum.Total {
		return 0
	}
	return 1
}

func runOrgBatchCase(exe, work, goldenDir, writeGoldenDir string, sc orgScene, order, conflict string, dry bool) orgBatchCase {
	c := orgBatchCase{Scene: sc.id, Order: order, Conflict: conflict, DryRun: dry}
	tag := fmt.Sprintf("%s-%s-%s", order, conflict, boolTag(dry))
	caseDir := filepath.Join(work, sc.id, tag)
	if err := os.RemoveAll(caseDir); err != nil {
		c.Result, c.Error = "ERROR", err.Error()
		return c
	}
	csDir := filepath.Join(caseDir, "cs")
	goDir := filepath.Join(caseDir, "go")
	for _, dir := range []string{csDir, goDir} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			c.Result, c.Error = "ERROR", err.Error()
			return c
		}
	}
	csFiles, err := sc.gen(csDir)
	if err != nil {
		c.Result, c.Error = "ERROR", "生成 C# 语料失败："+err.Error()
		return c
	}
	goFiles, err := sc.gen(goDir)
	if err != nil {
		c.Result, c.Error = "ERROR", "生成 Go 语料失败："+err.Error()
		return c
	}
	if !sameOrgStrings(csFiles, goFiles) {
		c.Result, c.Error = "ERROR", "两侧语料清单不一致"
		return c
	}

	// 两侧使用同名相对子目录作为整理目标。
	const targetRel = "整理结果"
	csTarget := filepath.Join(csDir, targetRel)
	goTarget := filepath.Join(goDir, targetRel)

	o := sc.options()
	o.Conflict = conflict
	o.DryRun = dry
	o.Preview = order == "preview"

	var base *orgDump
	if goldenDir != "" {
		// 基准模式：不运行 C#，直接读归档结果（CI 无 C# 参考实现时使用）。
		base, err = loadOrgGolden(goldenDir, sc.id, tag)
		if err != nil {
			c.Result, c.Error = "ERROR", "读取基准失败："+err.Error()
			return c
		}
	} else {
		csOut := filepath.Join(caseDir, "cs.json")
		argsC := []string{"--dump-organize-json", csOut, "--root", csDir, "--target", csTarget}
		argsC = append(argsC, orgCLIArgs(o)...)
		// C# 侧工作目录设为语料根：原版“指定文件夹”是相对当前目录解析的
		// （B/E.cs L1322-1327 直接用 A_1.B），这样两侧相对路径语义一致。
		if out, code, rerr := runSceneCmd(exe, argsC, csDir); rerr != nil {
			c.Result = "ERROR"
			c.Error = fmt.Sprintf("C# 退出码 %d：%s", code, strings.TrimSpace(out))
			return c
		}
		base, err = loadOrgDump(csOut)
		if err != nil {
			c.Result, c.Error = "ERROR", "读取 C# 结果失败："+err.Error()
			return c
		}
		if writeGoldenDir != "" {
			if werr := saveOrgGolden(writeGoldenDir, sc.id, tag, base); werr != nil {
				c.Result, c.Error = "ERROR", "写入基准失败："+werr.Error()
				return c
			}
		}
	}

	goOpts := o
	goOpts.Root = goDir
	goOpts.Target = goTarget
	goDump, err := executeOrgRun(goOpts)
	if err != nil {
		c.Result, c.Error = "ERROR", "Go 执行失败："+err.Error()
		return c
	}
	goOut := filepath.Join(caseDir, "go.json")
	if err := writeOrgDump(goOut, goDump); err != nil {
		c.Result, c.Error = "ERROR", "写入 Go 结果失败："+err.Error()
		return c
	}

	cmp := compareOrgDumps(base, &goDump, 0)
	c.Result = cmp.Result
	c.Matched = cmp.Files.Matched
	c.Union = cmp.Files.Union
	c.FileRate = cmp.Files.Rate
	c.CountersMatch = cmp.Counters.Match
	c.DirsMatch = cmp.Dirs.Matched == cmp.Dirs.Union
	if cmp.PreviewPlan != nil {
		c.PreviewMatched = cmp.PreviewPlan.Matched
		c.PreviewUnion = cmp.PreviewPlan.Union
		c.PreviewRate = cmp.PreviewPlan.Rate
	}
	if len(cmp.Diffs) > 0 {
		c.FirstDiff = cmp.Diffs[0].String()
	}
	if goldenDir != "" && cmp.Result != "MATCH" {
		if reason, ok := orgGoldenDeviation(sc.id, cmp.Diffs); ok {
			c.Result = "KNOWN"
			c.Deviation = reason
		}
	}
	return c
}

// orgCLIArgs 把业务参数翻译成 OrganizeDump.cs 的命令行（不含 --root/--target/输出）。
func orgCLIArgs(o orgOptions) []string {
	args := []string{
		"--steps", o.Steps,
		"--mode", o.Mode,
		"--keep", o.Keep,
		"--image-keep", o.ImageKeep,
		"--image-threshold", strconv.Itoa(o.ImageThreshold),
		"--audio-tolerance", strconv.Itoa(o.AudioTolerance),
		"--strength", strconv.Itoa(o.Strength),
		"--conflict", o.Conflict,
		"--delete", o.Delete,
		// 对拍机试用期可能已结束：用应用自身的试用记录接口重置为“今天起 3 天”，只写试用数据。
		"--fresh-trial",
	}
	if o.Keyword != "" {
		args = append(args, "--keyword", o.Keyword)
	}
	if o.Regex != "" {
		args = append(args, "--regex", o.Regex)
	}
	if o.Fixed != "" {
		args = append(args, "--fixed", o.Fixed)
	}
	if o.Protect != "" {
		args = append(args, "--protect", o.Protect)
	}
	if o.Exclude != "" {
		args = append(args, "--exclude", o.Exclude)
	}
	if o.MinSize > 0 {
		args = append(args, "--min-size", strconv.FormatInt(o.MinSize, 10))
	}
	if o.ConfirmedFile != "" {
		args = append(args, "--confirmed", o.ConfirmedFile)
	}
	if o.AutoConfirm {
		args = append(args, "--auto-confirm")
	}
	if o.Hardlink {
		args = append(args, "--hardlink")
	}
	if o.IncludeHidden {
		args = append(args, "--include-hidden")
	}
	if o.DryRun {
		args = append(args, "--dry-run")
	}
	if o.Preview {
		args = append(args, "--preview-plan")
	}
	return args
}

func parseOrgList(s string, allowed []string) ([]string, error) {
	ok := map[string]bool{}
	for _, a := range allowed {
		ok[a] = true
	}
	var out []string
	for _, p := range strings.Split(s, ",") {
		if p = strings.TrimSpace(p); p == "" {
			continue
		}
		if !ok[p] {
			return nil, fmt.Errorf("不支持的值 %q（可选：%s）", p, strings.Join(allowed, ","))
		}
		out = append(out, p)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("列表为空（可选：%s）", strings.Join(allowed, ","))
	}
	return out, nil
}

func sameOrgStrings(a, b []string) bool {
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

func boolTag(v bool) string {
	if v {
		return "dry"
	}
	return "real"
}

func writeOrgBatchSummary(sum orgBatchSummary, outPath string) error {
	raw, err := json.MarshalIndent(sum, "", "  ")
	if err != nil {
		return err
	}
	if dir := filepath.Dir(outPath); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
	}
	if err := os.WriteFile(outPath, append(raw, '\n'), 0o644); err != nil {
		return err
	}

	var md strings.Builder
	md.WriteString("| 场景 | 顺序 | 冲突 | 试运行 | 文件匹配数 | 一致率 | counters | 首条差异 |\n")
	md.WriteString("| --- | --- | --- | --- | --- | --- | --- | --- |\n")
	for _, c := range sum.Cases {
		note := c.FirstDiff
		if c.Error != "" {
			note = c.Error
		}
		note = strings.ReplaceAll(note, "|", "\\|")
		fmt.Fprintf(&md, "| %s | %s | %s | %v | %d/%d | %.2f%% | %v | %s |\n",
			c.Scene, c.Order, c.Conflict, c.DryRun, c.Matched, c.Union, c.FileRate*100, c.CountersMatch, note)
	}
	fmt.Fprintf(&md, "\n合计：%d/%d MATCH\n", sum.Match, sum.Total)

	mdPath := strings.TrimSuffix(outPath, ".json") + ".md"
	if mdPath == outPath {
		mdPath = outPath + ".md"
	}
	return os.WriteFile(mdPath, []byte(md.String()), 0o644)
}
