package main

import (
	"fmt"
	"os"
	"strings"
)

// build 生成全部主语料。所有内容都由 seed 或固定常量决定。
func (g *generator) build() error {
	g.buildExact()
	g.buildUnicode()
	g.buildRenameCases()
	g.buildImages()
	g.buildAudio()
	g.buildLinks()
	g.buildHidden()
	g.buildLongPath()
	g.buildReadonly()
	g.buildOrganize()
	g.buildSpecialDelete()
	g.buildFilter()
	g.buildExtensionCases()

	g.note("本语料不写入 Windows Hidden/System 属性（需要平台 API，M2 平台测试补齐）；dot 前缀文件在各平台都存在")
	return nil
}

func (g *generator) randomData(key string, n int) []byte {
	r := g.rngFor("content/" + key)
	buf := make([]byte, n)
	for i := range buf {
		buf[i] = byte(r.Intn(256))
	}
	return buf
}

func (g *generator) buildExact() {
	a := []byte("exact duplicate A\n")
	b := []byte("exact duplicate B\n") // 与 A 等长但内容不同
	g.file("01-exact/a.txt", a)
	g.file("01-exact/b.txt", a)
	g.file("01-exact/c.txt", b)
	g.file("01-exact/d.txt", []byte("different size content\n"))
	g.file("01-exact/empty-1.bin", nil)
	g.file("01-exact/empty-2.bin", nil)
	g.file("01-exact/one-1.bin", []byte{0x42})
	g.file("01-exact/one-2.bin", []byte{0x42})

	medium := g.randomData("medium-256k", 256*1024)
	g.file("01-exact/medium-1.bin", medium)
	g.file("01-exact/medium-2.bin", medium)

	noext := []byte("no extension duplicate\n")
	g.file("01-exact/no-ext-a", noext)
	g.file("01-exact/no-ext-b", noext)
}

func (g *generator) buildUnicode() {
	g.text("02-unicode/报告 2024 最终版.txt", "unicode-a\n")
	g.text("02-unicode/报告 2024 最终版 (1).txt", "unicode-b\n")
	g.text("02-unicode/日本語のファイル.txt", "japanese\n")
	g.text("02-unicode/emoji_😀_🙂.txt", "emoji\n")
	g.text("02-unicode/ｆｕｌｌｗｉｄｔｈ.txt", "fullwidth\n")
	g.text("02-unicode/space dir/with space.txt", "space\n")
}

func (g *generator) buildRenameCases() {
	names := []string{
		"1 a.txt",
		"01_b.txt",
		"2024-01-02_c.txt",
		"copy_d.txt",
		"副本 2_e.txt",
		"(3) f.txt",
		"g (1).txt",
		"h-2-i.txt",
		"_尾部_20240101.txt",
		"abc_12_def.txt",
		"新建 文本文档.txt",
	}
	for i, name := range names {
		g.text("03-rename/"+name, fmt.Sprintf("rename case %d\n", i))
	}
}

func (g *generator) buildImages() {
	red := rgb(220, 30, 30)
	blue := rgb(20, 40, 220)

	g.file("04-images/solid-red-64.png", encodePNG(solid(64, 64, red)))
	g.file("04-images/solid-red-128.png", encodePNG(solid(128, 128, red)))
	g.file("04-images/solid-blue-64.png", encodePNG(solid(64, 64, blue)))

	grad := gradientH(256, 256)
	g.file("04-images/gradient-h-256.png", encodePNG(grad))
	g.file("04-images/gradient-h-flip-256.png", encodePNG(flipH(grad)))
	g.file("04-images/gradient-v-256.png", encodePNG(gradientV(256, 256)))
	g.file("04-images/panorama-512x64.png", encodePNG(gradientH(512, 64)))
	g.file("04-images/tiny-1x1.png", encodePNG(solid(1, 1, red)))
	g.file("04-images/alpha-64.png", encodePNG(alphaGradient(64, 64)))

	base := gradientH(256, 256)
	g.file("04-images/exif-orient1.jpg", insertExifOrientation(encodeJPEG(base), 1))
	g.file("04-images/exif-orient6.jpg", insertExifOrientation(encodeJPEG(rotate90CCW(base)), 6))

	valid := encodePNG(grad)
	corrupt := append([]byte{}, valid[:len(valid)/3]...)
	g.file("04-images/corrupt.png", corrupt)
}

func (g *generator) buildAudio() {
	g.file("05-audio/tone-440-1s.wav", wavPCM(44100, 1.0, 440))
	g.file("05-audio/tone-440-1s-copy.wav", wavPCM(44100, 1.0, 440))
	g.file("05-audio/tone-880-1s.wav", wavPCM(44100, 1.0, 880))
	g.file("05-audio/silent-2s.wav", wavPCM(44100, 2.0, 0))
	g.file("05-audio/clipped-1s.wav", wavPCM(44100, 1.0, 440, 1.0))

	g.file("05-audio/cbr-10s.mp3", mp3CBR(10))
	g.file("05-audio/vbr-10s.mp3", mp3VBR(10))
	g.file("05-audio/cbr-10s-id3.mp3", append(id3v23("测试音频", encodePNG(solid(8, 8, rgb(10, 200, 10)))), mp3CBR(10)...))
	g.file("05-audio/corrupt.mp3", append([]byte("ID3\x03\x00\x00\x00\x00\x00\x00"), g.randomData("corrupt-mp3", 91)...))
	g.file("05-audio/zero.mp3", nil)
}

func (g *generator) buildLinks() {
	g.text("06-links/hardlink-target.txt", "hardlink target content\n")
	g.hardlink("06-links/hardlink-alias.txt", "06-links/hardlink-target.txt")
	g.symlink("06-links/symlink-file.txt", "hardlink-target.txt")
	g.symlink("06-links/symlink-dir", "../01-exact")
	g.symlink("06-links/broken-symlink.txt", "missing-target.txt")
}

func (g *generator) buildHidden() {
	g.text("07-hidden/.hidden-dot.txt", "dot hidden\n")
	g.text("07-hidden/hidden-attr.txt", "hidden attribute intended (platform dependent)\n")
	g.text("07-hidden/system-attr.txt", "system attribute intended (platform dependent)\n")
}

func (g *generator) buildLongPath() {
	seg := strings.Repeat("d", 48)
	rel := "08-long-path"
	for i := 0; i < 6; i++ {
		rel += "/" + seg
	}
	g.text(rel+"/long.txt", "long path file\n")
	g.text("08-long-path/short.txt", "short\n")

	longName := strings.Repeat("n", 220) + ".txt"
	g.text("08-long-path/"+longName, "long filename\n")
}

func (g *generator) buildReadonly() {
	const rel = "09-readonly/readonly.txt"
	g.text(rel, "readonly duplicate content\n")
	if err := os.Chmod(g.path(rel), 0o444); err != nil {
		g.note("设置只读失败 %s: %v", rel, err)
	}
	g.text("09-readonly/readonly-copy.txt", "readonly duplicate content\n")
	if err := os.Chmod(g.path("09-readonly/readonly-copy.txt"), 0o444); err != nil {
		g.note("设置只读失败 %s: %v", "09-readonly/readonly-copy.txt", err)
	}
	g.text("09-readonly/writable.txt", "writable\n")
}

func (g *generator) buildOrganize() {
	g.text("10-organize/src/report.txt", "organize report\n")
	g.text("10-organize/src/report2.txt", "organize report two\n")
	g.file("10-organize/src/photo.png", encodePNG(solid(32, 32, rgb(90, 90, 200))))
	g.file("10-organize/src/song.wav", wavPCM(8000, 0.5, 220))
	g.text("10-organize/src/archive.zip", "not a real zip\n")
	g.text("10-organize/src/noext", "no extension\n")
	g.text("10-organize/src/dup-name.txt", "source dup name\n")
	g.text("10-organize/src/sub/deep.txt", "deep file\n")
	g.text("10-organize/target/dup-name.txt", "existing target dup name\n")

	g.ensureDir("10-organize/empty-dir")
	g.ensureDir("10-organize/nested/empty-a")
	g.ensureDir("10-organize/nested/empty-b")
	g.ensureDir("10-organize/nested/leaf-only/deeper")
}

func (g *generator) buildSpecialDelete() {
	garbage := []string{
		"123456.txt",
		"123文件.txt",
		"abc123文件.txt",
		"abc中def.txt",
		"ab#cd.txt",
		"x7k2m9.txt",
		"aBcDeFgH.txt",
		"bcdfghjk.txt",
		"BCDXYz.txt",
		"新建文本文档.txt",
	}
	for i, name := range garbage {
		g.text("11-special-delete/garbage/"+name, fmt.Sprintf("garbage %d\n", i))
	}
	protected := []string{
		"IMG_20240101_120000.jpg",
		"Screenshot 2024-01-02.png",
		"微信图片_20240101.jpg",
		"2024年总结.txt",
		"第3章 计划.txt",
		"劳动合同.pdf",
		"照片.jpg",
		"设计稿.png",
	}
	for i, name := range protected {
		g.text("11-special-delete/protected/"+name, fmt.Sprintf("protected %d\n", i))
	}
}

func (g *generator) buildFilter() {
	g.text("12-filter/skip-by-exclude/keep.txt", "should be excluded by substring\n")
	g.text("12-filter/small.txt", "abc")
	g.text("12-filter/big.txt", string(g.randomData("filter-big", 1024)))
}

func (g *generator) buildExtensionCases() {
	g.text("13-ext/UPPER.TXT", "upper extension\n")
	g.text("13-ext/lower.txt", "lower extension\n")
	g.text("13-ext/archive.tar.gz", "compound extension\n")
	g.text("13-ext/dot.name.txt", "dot name\n")
}
