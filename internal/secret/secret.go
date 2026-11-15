// Package secret 保存需要加密的小块配置（目前只有 AI 接口密钥）。
//
// Windows 用 DPAPI（CurrentUser）加密，落盘到 %AppData%\FileDedup\ai\key.bin；
// 其他平台没有 DPAPI，SupportsProtection 返回 false，保存会失败并由界面原文提示。
// 明文密钥只存在于内存，界面只显示后四位。
package secret

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// FileName 是密钥文件名。
const FileName = "key.bin"

// Path 返回 AI 密钥文件路径：<用户配置目录>/FileDedup/ai/key.bin。
func Path() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "FileDedup", "ai", FileName), nil
}

// Save 加密并原子写入密钥。空字符串等同于 Clear。
func Save(key string) error {
	key = strings.TrimSpace(key)
	if key == "" {
		return Clear()
	}
	enc, err := protect([]byte(key))
	if err != nil {
		return err
	}
	path, err := Path()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, enc, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// Load 读取并解密密钥；文件不存在时返回空字符串与 nil。
func Load() (string, error) {
	path, err := Path()
	if err != nil {
		return "", err
	}
	enc, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return "", nil
		}
		return "", err
	}
	raw, err := unprotect(enc)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(raw)), nil
}

// Clear 删除密钥文件；不存在不算错误。
func Clear() error {
	path, err := Path()
	if err != nil {
		return err
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	return nil
}

// Hint 返回界面展示用的掩码：只露后四位；空密钥返回空串。
func Hint(key string) string {
	key = strings.TrimSpace(key)
	if key == "" {
		return ""
	}
	if len(key) <= 4 {
		return "****"
	}
	return "****" + key[len(key)-4:]
}
