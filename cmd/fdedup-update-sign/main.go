// fdedup-update-sign 是更新签名工具：只处理更新清单与安装包的 Ed25519 签名。
//
// 私钥文件只放在仓库外，客户端只内置公钥。常用流程：
//
//	fdedup-update-sign genkey -out "%USERPROFILE%\.tools\filededup-update-key\update.key"
//	fdedup-update-sign make-manifest -key <key> -version 0.8.1 -file bin\fdedup-ui.exe ^
//	    -url https://example.com/fdedup-ui-0.8.1.exe -notes "修复若干问题" -out bin\update.json
//	fdedup-update-sign sign-manifest -key <key> -in bin\update.json
package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"filededup/internal/update"
)

func main() {
	if len(os.Args) < 2 {
		usage()
	}
	switch os.Args[1] {
	case "genkey":
		genkey(os.Args[2:])
	case "sign-manifest":
		signManifest(os.Args[2:])
	case "sign-package":
		signPackage(os.Args[2:])
	case "make-manifest":
		makeManifest(os.Args[2:])
	default:
		usage()
	}
}

func usage() {
	fmt.Fprint(os.Stderr, `用法：
  fdedup-update-sign genkey -out <私钥文件>
  fdedup-update-sign sign-manifest -key <私钥文件> -in <清单> [-out <输出>]
  fdedup-update-sign sign-package -key <私钥文件> -file <安装包>
  fdedup-update-sign make-manifest -key <私钥文件> -version <版本> -file <安装包> -url <下载地址>
      [-name <文件名>] [-notes <说明>] [-os windows] [-arch amd64] [-released-at RFC3339] [-out <输出>]
`)
	os.Exit(2)
}

func genkey(args []string) {
	fs := flag.NewFlagSet("genkey", flag.ExitOnError)
	out := fs.String("out", "", "私钥输出路径（必填，建议放在仓库外）")
	_ = fs.Parse(args)
	if *out == "" {
		fatal("必须指定 -out")
	}
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		fatal("生成密钥失败：%v", err)
	}
	if err := os.MkdirAll(dirOf(*out), 0o700); err != nil {
		fatal("创建目录失败：%v", err)
	}
	seed := hex.EncodeToString(priv.Seed())
	if err := os.WriteFile(*out, []byte(seed+"\n"), 0o600); err != nil {
		fatal("写入私钥失败：%v", err)
	}
	fmt.Printf("私钥：%s\n", *out)
	fmt.Printf("公钥（base64url）：%s\n", base64.RawURLEncoding.EncodeToString(pub))
	fmt.Printf("公钥（hex）：%s\n", hex.EncodeToString(pub))
}

func signManifest(args []string) {
	fs := flag.NewFlagSet("sign-manifest", flag.ExitOnError)
	keyPath := fs.String("key", "", "私钥文件（必填）")
	in := fs.String("in", "", "清单文件（必填）")
	out := fs.String("out", "", "输出路径，默认覆盖 -in")
	_ = fs.Parse(args)
	if *keyPath == "" || *in == "" {
		fatal("必须指定 -key 与 -in")
	}
	priv := loadKey(*keyPath)
	raw, err := os.ReadFile(*in)
	if err != nil {
		fatal("读取清单失败：%v", err)
	}
	m, err := update.ParseManifest(raw)
	if err != nil {
		// 未签名的清单无法通过 ParseManifest，先按宽松结构解析再签名。
		if err := json.Unmarshal(raw, &m); err != nil {
			fatal("解析清单失败：%v", err)
		}
	}
	if err := update.SignManifest(&m, priv); err != nil {
		fatal("签名失败：%v", err)
	}
	m.SourceURL = ""
	m.FetchedAt = time.Time{}
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		fatal("序列化清单失败：%v", err)
	}
	data = append(data, '\n')
	dst := *out
	if dst == "" {
		dst = *in
	}
	if err := os.WriteFile(dst, data, 0o644); err != nil {
		fatal("写入清单失败：%v", err)
	}
	fmt.Printf("已签名：%s\n", dst)
}

func signPackage(args []string) {
	fs := flag.NewFlagSet("sign-package", flag.ExitOnError)
	keyPath := fs.String("key", "", "私钥文件（必填）")
	file := fs.String("file", "", "安装包（必填）")
	_ = fs.Parse(args)
	if *keyPath == "" || *file == "" {
		fatal("必须指定 -key 与 -file")
	}
	priv := loadKey(*keyPath)
	f, err := os.Open(*file)
	if err != nil {
		fatal("打开安装包失败：%v", err)
	}
	defer f.Close()
	sum, size, err := update.FileSHA256(f)
	if err != nil {
		fatal("计算摘要失败：%v", err)
	}
	out, _ := json.MarshalIndent(map[string]interface{}{
		"name":      baseName(*file),
		"size":      size,
		"sha256":    sum,
		"signature": update.SignPackageDigest(priv, sum),
	}, "", "  ")
	fmt.Println(string(out))
}

func makeManifest(args []string) {
	fs := flag.NewFlagSet("make-manifest", flag.ExitOnError)
	keyPath := fs.String("key", "", "私钥文件（必填）")
	version := fs.String("version", "", "版本号（必填）")
	file := fs.String("file", "", "安装包（必填）")
	url := fs.String("url", "", "安装包下载地址（必填）")
	name := fs.String("name", "", "清单里的文件名，默认取 -file 的基本名")
	notes := fs.String("notes", "", "更新说明")
	goos := fs.String("os", "windows", "目标系统")
	goarch := fs.String("arch", "amd64", "目标架构")
	released := fs.String("released-at", "", "发布时间 RFC3339，默认当前时间")
	out := fs.String("out", "", "清单输出路径（必填）")
	_ = fs.Parse(args)
	if *keyPath == "" || *version == "" || *file == "" || *url == "" || *out == "" {
		fatal("必须指定 -key、-version、-file、-url 与 -out")
	}
	if _, err := update.ParseVersion(*version); err != nil {
		fatal("版本号非法：%v", err)
	}
	priv := loadKey(*keyPath)
	f, err := os.Open(*file)
	if err != nil {
		fatal("打开安装包失败：%v", err)
	}
	sum, size, err := update.FileSHA256(f)
	f.Close()
	if err != nil {
		fatal("计算摘要失败：%v", err)
	}
	assetName := *name
	if assetName == "" {
		assetName = baseName(*file)
	}
	at := *released
	if at == "" {
		at = time.Now().Format(time.RFC3339)
	}
	m := update.Manifest{
		SchemaVersion: update.SchemaVersion,
		Kind:          update.Kind,
		Version:       *version,
		ReleasedAt:    at,
		Notes:         *notes,
		Assets: []update.Asset{{
			Name:      assetName,
			OS:        *goos,
			Arch:      *goarch,
			URL:       *url,
			Size:      size,
			SHA256:    sum,
			Signature: update.SignPackageDigest(priv, sum),
		}},
	}
	if err := update.SignManifest(&m, priv); err != nil {
		fatal("签名失败：%v", err)
	}
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		fatal("序列化清单失败：%v", err)
	}
	data = append(data, '\n')
	if err := os.WriteFile(*out, data, 0o644); err != nil {
		fatal("写入清单失败：%v", err)
	}
	fmt.Printf("清单：%s\n版本：%s\n包大小：%d\n包 SHA-256：%s\n", *out, *version, size, sum)
}

func loadKey(path string) ed25519.PrivateKey {
	raw, err := os.ReadFile(path)
	if err != nil {
		fatal("读取私钥失败：%v", err)
	}
	s := strings.TrimSpace(string(raw))
	if b, err := hex.DecodeString(s); err == nil && len(b) == ed25519.SeedSize {
		return ed25519.NewKeyFromSeed(b)
	}
	if b, err := base64.RawURLEncoding.DecodeString(s); err == nil && len(b) == ed25519.SeedSize {
		return ed25519.NewKeyFromSeed(b)
	}
	fatal("私钥格式不正确：需要 32 字节 seed 的 hex 或 base64url")
	return nil
}

func baseName(p string) string {
	p = strings.ReplaceAll(p, "\\", "/")
	if i := strings.LastIndexByte(p, '/'); i >= 0 {
		return p[i+1:]
	}
	return p
}

func dirOf(p string) string {
	p = strings.ReplaceAll(p, "\\", "/")
	if i := strings.LastIndexByte(p, '/'); i >= 0 {
		return p[:i]
	}
	return "."
}

func fatal(format string, args ...interface{}) {
	fmt.Fprintf(os.Stderr, format+"\n", args...)
	os.Exit(1)
}
