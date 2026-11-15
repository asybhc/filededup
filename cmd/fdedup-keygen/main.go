// fdedup-keygen 是许可证签发工具。私钥只在运行本工具的机器上保存，
// 不会写入客户端，也不要提交到仓库。
package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"filededup/internal/auth"
)

const envPrivateKey = "FDEDUP_LICENSE_KEY"

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	var err error
	switch os.Args[1] {
	case "gen-keypair":
		err = cmdGenKeypair(os.Args[2:])
	case "issue":
		err = cmdIssue(os.Args[2:])
	case "inspect":
		err = cmdInspect(os.Args[2:])
	case "machine-code":
		err = cmdMachineCode(os.Args[2:])
	case "-h", "--help", "help":
		usage()
		return
	default:
		usage()
		os.Exit(2)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "错误："+err.Error())
		os.Exit(1)
	}
}

func usage() {
	fmt.Fprint(os.Stderr, `fdedup-keygen — 文件去重 Go 版许可证工具

用法：
  fdedup-keygen gen-keypair -out DIR [-force]
      生成 Ed25519 密钥对：DIR/ed25519-private.pem、DIR/ed25519-public.txt

  fdedup-keygen issue -key PRIVATE.pem (-machine CODE | -any) [选项]
      -days N            有效天数，默认 365；与 -exp 互斥
      -exp YYYY-MM-DD    指定到期日
      -forever           永久许可证
      -edition pro|standard
      -features a,b,c    功能位，默认 scan,apply,organize
      -id ID             许可证编号，默认随机
      -out FILE          写入文件；不指定则打印到标准输出

  fdedup-keygen inspect -license FILE
  fdedup-keygen machine-code

私钥也可以用环境变量 `+envPrivateKey+` 指定。
`)
}

func cmdGenKeypair(args []string) error {
	fs := flag.NewFlagSet("gen-keypair", flag.ExitOnError)
	out := fs.String("out", "", "密钥输出目录")
	force := fs.Bool("force", false, "覆盖已有文件")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *out == "" {
		return errors.New("必须指定 -out 目录")
	}
	privPath := filepath.Join(*out, "ed25519-private.pem")
	pubPath := filepath.Join(*out, "ed25519-public.txt")
	if !*force {
		for _, p := range []string{privPath, pubPath} {
			if _, err := os.Stat(p); err == nil {
				return fmt.Errorf("文件已存在：%s（如需覆盖请加 -force）", p)
			}
		}
	}
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return err
	}
	der, err := x509.MarshalPKCS8PrivateKey(priv)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(*out, 0o700); err != nil {
		return err
	}
	if err := os.WriteFile(privPath, pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der}), 0o600); err != nil {
		return err
	}
	pubB64 := base64.StdEncoding.EncodeToString(pub)
	if err := os.WriteFile(pubPath, []byte(pubB64+"\n"), 0o644); err != nil {
		return err
	}
	fmt.Println("已生成密钥对：")
	fmt.Println("  私钥：" + privPath)
	fmt.Println("  公钥：" + pubPath)
	fmt.Println("公钥（嵌入客户端 internal/auth 常量）：")
	fmt.Println("  " + pubB64)
	return nil
}

func cmdIssue(args []string) error {
	fs := flag.NewFlagSet("issue", flag.ExitOnError)
	keyPath := fs.String("key", "", "Ed25519 私钥文件（PKCS#8 PEM）")
	machine := fs.String("machine", "", "绑定机器码；与 -any 二选一")
	anyMachine := fs.Bool("any", false, "不绑定机器")
	days := fs.Int("days", 365, "有效天数")
	exp := fs.String("exp", "", "到期日 YYYY-MM-DD")
	forever := fs.Bool("forever", false, "永久有效")
	edition := fs.String("edition", "pro", "pro 或 standard")
	features := fs.String("features", "scan,apply,organize", "功能位，逗号分隔")
	id := fs.String("id", "", "许可证编号")
	out := fs.String("out", "", "输出文件")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *keyPath == "" {
		*keyPath = os.Getenv(envPrivateKey)
	}
	if *keyPath == "" {
		return fmt.Errorf("必须指定 -key 或环境变量 %s", envPrivateKey)
	}
	if *anyMachine == (*machine != "") {
		return errors.New("-machine 与 -any 必须且只能指定一个")
	}
	if *forever && *exp != "" {
		return errors.New("-forever 与 -exp 不能同时使用")
	}
	if *exp != "" && *days != 365 {
		return errors.New("-exp 与 -days 不能同时使用")
	}
	priv, err := readPrivateKey(*keyPath)
	if err != nil {
		return err
	}

	expiry := ""
	switch {
	case *forever:
		expiry = "forever"
	case *exp != "":
		if _, err := time.ParseInLocation("2006-01-02", *exp, time.Local); err != nil {
			return fmt.Errorf("到期日格式不正确：%s", *exp)
		}
		expiry = *exp
	default:
		expiry = time.Now().AddDate(0, 0, *days).Format("2006-01-02")
	}

	bind := *machine
	if *anyMachine {
		bind = "*"
	} else {
		normalized := auth.NormalizeMachineCode(bind)
		if len(normalized) != 16 {
			return fmt.Errorf("机器码应为 16 位（可带连字符），实际为 %d 位：%s", len(normalized), bind)
		}
		bind = normalized
	}

	licenseID := *id
	if licenseID == "" {
		buf := make([]byte, 6)
		if _, err := rand.Read(buf); err != nil {
			return err
		}
		licenseID = strings.ToUpper(fmt.Sprintf("%x", buf))
	}

	text, err := auth.IssueLicense(auth.Payload{
		Machine:  bind,
		Expiry:   expiry,
		Issued:   time.Now().Format("2006-01-02"),
		Edition:  *edition,
		Features: splitFeatures(*features),
		ID:       licenseID,
	}, priv)
	if err != nil {
		return err
	}
	if *out != "" {
		if err := os.MkdirAll(filepath.Dir(*out), 0o700); err != nil && filepath.Dir(*out) != "." {
			return err
		}
		if err := os.WriteFile(*out, []byte(text+"\n"), 0o600); err != nil {
			return err
		}
		fmt.Println("已写入：" + *out)
		return nil
	}
	fmt.Println(text)
	return nil
}

func cmdInspect(args []string) error {
	fs := flag.NewFlagSet("inspect", flag.ExitOnError)
	path := fs.String("license", "", "许可证文件")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *path == "" {
		return errors.New("必须指定 -license 文件")
	}
	raw, err := os.ReadFile(*path)
	if err != nil {
		return err
	}
	lic, err := auth.VerifyLicense(string(raw))
	if err != nil {
		return err
	}
	fmt.Printf("来源：%s\n", lic.Source)
	fmt.Printf("版本：%s\n", lic.Edition)
	if lic.Forever {
		fmt.Println("到期：永久")
	} else {
		fmt.Printf("到期：%s（本地判定：%s）\n", lic.Expiry.Format("2006-01-02"), expiryStatus(lic))
	}
	fmt.Printf("机器码：%s\n", lic.Machine)
	fmt.Printf("功能位：%s\n", strings.Join(lic.Features, ","))
	if lic.ID != "" {
		fmt.Printf("编号：%s\n", lic.ID)
	}
	return nil
}

func cmdMachineCode(args []string) error {
	fs := flag.NewFlagSet("machine-code", flag.ExitOnError)
	if err := fs.Parse(args); err != nil {
		return err
	}
	code, err := auth.MachineCode()
	if err != nil {
		return err
	}
	fmt.Println(code)
	return nil
}

func expiryStatus(lic auth.License) string {
	if lic.ExpiredOn(time.Now()) {
		return "已到期"
	}
	return "有效"
}

func readPrivateKey(path string) (ed25519.PrivateKey, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	block, _ := pem.Decode(raw)
	if block == nil {
		return nil, errors.New("私钥不是 PEM 格式")
	}
	key, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, err
	}
	priv, ok := key.(ed25519.PrivateKey)
	if !ok {
		return nil, errors.New("私钥不是 Ed25519")
	}
	return priv, nil
}

func splitFeatures(s string) []string {
	var out []string
	for _, part := range strings.Split(s, ",") {
		part = strings.TrimSpace(part)
		if part != "" {
			out = append(out, part)
		}
	}
	return out
}
