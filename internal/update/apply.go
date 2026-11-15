package update

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// ReplaceResult 描述一次成功替换。
type ReplaceResult struct {
	ExePath    string `json:"exePath"`
	BackupPath string `json:"backupPath"`
	Size       int64  `json:"size"`
	SHA256     string `json:"sha256"`
}

// Replace 用已下载并验签的安装包替换当前 exe。
//
// 策略与 C# 版已验证流程一致：
//  1. 再次校验安装包长度与 SHA-256；
//  2. 把当前 exe 改名为 .old（Windows 允许重命名正在运行的程序）；
//  3. 安装包先复制为 .new，校验副本长度与哈希后再原子改名为正式 exe；
//  4. 启动新程序；启动失败则删除新文件并把 .old 改回去；
//  5. 成功后保留 .old，由下次启动的 Cleanup 删除。
func Replace(exePath, pkgPath string, expectSize int64, expectSHA string, launch func(string) error) (ReplaceResult, error) {
	absExe, err := filepath.Abs(exePath)
	if err != nil {
		return ReplaceResult{}, fmt.Errorf("解析当前程序路径失败：%w", err)
	}
	absPkg, err := filepath.Abs(pkgPath)
	if err != nil {
		return ReplaceResult{}, fmt.Errorf("解析安装包路径失败：%w", err)
	}
	if _, err := os.Stat(absExe); err != nil {
		return ReplaceResult{}, fmt.Errorf("当前程序不存在：%w", err)
	}
	if err := VerifyFile(absPkg, expectSize, expectSHA); err != nil {
		return ReplaceResult{}, fmt.Errorf("安装包校验失败，拒绝替换：%w", err)
	}

	backup := absExe + ".old"
	stage := absExe + ".new"
	_ = os.Remove(stage)
	if err := os.Remove(backup); err != nil && !os.IsNotExist(err) {
		return ReplaceResult{}, fmt.Errorf("清理旧备份失败：%w", err)
	}
	if err := os.Rename(absExe, backup); err != nil {
		return ReplaceResult{}, fmt.Errorf("备份当前程序失败：%w", err)
	}

	rollback := func(cause error) (ReplaceResult, error) {
		_ = os.Remove(absExe)
		_ = os.Remove(stage)
		var rbErr error
		if err := os.Rename(backup, absExe); err != nil {
			rbErr = fmt.Errorf("回滚失败，原程序仍在 %s：%v", backup, err)
		}
		if rbErr != nil {
			return ReplaceResult{}, fmt.Errorf("%v；%v", cause, rbErr)
		}
		return ReplaceResult{}, fmt.Errorf("%v；已回滚到原程序", cause)
	}

	if err := copyFile(absPkg, stage); err != nil {
		return rollback(fmt.Errorf("写入新程序失败：%w", err))
	}
	if err := VerifyFile(stage, expectSize, expectSHA); err != nil {
		return rollback(fmt.Errorf("新程序副本校验失败：%w", err))
	}
	if err := os.Rename(stage, absExe); err != nil {
		return rollback(fmt.Errorf("替换程序失败：%w", err))
	}
	if launch != nil {
		if err := launch(absExe); err != nil {
			return rollback(fmt.Errorf("启动新程序失败：%w", err))
		}
	}
	return ReplaceResult{
		ExePath:    absExe,
		BackupPath: backup,
		Size:       expectSize,
		SHA256:     expectSHA,
	}, nil
}

// Cleanup 处理上次替换留下的文件：成功则删 .old，异常则恢复 .old，并清理 .new 残留。
func Cleanup(exePath string) error {
	absExe, err := filepath.Abs(exePath)
	if err != nil {
		return err
	}
	backup := absExe + ".old"
	stage := absExe + ".new"
	_ = os.Remove(stage)
	if _, err := os.Stat(backup); err != nil {
		return nil
	}
	if _, err := os.Stat(absExe); err == nil {
		return os.Remove(backup)
	}
	return os.Rename(backup, absExe)
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o755)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	if err := out.Sync(); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}
