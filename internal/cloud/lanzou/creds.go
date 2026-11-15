package lanzou

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// CredentialFileName 是凭据文件名。目录与 C# 版一致，都在 %AppData%\FileDedup\lanzou\。
const CredentialFileName = "lanzou.bin"

// CredentialPath 返回本程序的凭据文件路径：
// os.UserConfigDir()/FileDedup/lanzou/lanzou.bin（Windows 即 %AppData%\FileDedup\lanzou\lanzou.bin）。
func CredentialPath() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "FileDedup", "lanzou", CredentialFileName), nil
}

// SaveCredentials 用 DPAPI（CurrentUser）加密后落盘。
// 只保存 Cookie，不保存密码；文件内容不写日志。
func SaveCredentials(creds Credentials) error {
	if !creds.Valid() {
		return errors.New("凭据不完整，拒绝保存")
	}
	if creds.Platform == "" {
		creds.Platform = PlatformLanzou
	}
	creds.SavedAt = time.Now()
	raw, err := json.Marshal(creds)
	if err != nil {
		return err
	}
	enc, err := protect(raw)
	if err != nil {
		return err
	}
	path, err := CredentialPath()
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

// LoadCredentials 读取并解密凭据。文件不存在时返回 (nil, nil)。
func LoadCredentials() (*Credentials, error) {
	path, err := CredentialPath()
	if err != nil {
		return nil, err
	}
	enc, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	raw, err := unprotect(enc)
	if err != nil {
		return nil, err
	}
	var creds Credentials
	if err := json.Unmarshal(raw, &creds); err != nil {
		return nil, err
	}
	if !creds.Valid() {
		return nil, nil
	}
	if creds.Platform == "" {
		creds.Platform = PlatformLanzou
	}
	return &creds, nil
}

// DeleteCredentials 删除本程序保存的凭据；文件不存在不算错误。
func DeleteCredentials() error {
	path, err := CredentialPath()
	if err != nil {
		return err
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	return nil
}

// ImportFromCSharp 尝试从 C# 版（%AppData%\FileDedupTool）导入登录态。
//
// C# 版的文件名是混淆过的，这里不猜文件名：逐个尝试 DPAPI 解密，能解出
// ylogin + phpdisk_info 且 platform 为空或含 lanzou 的就认。
// 返回导入的凭据与来源文件路径；没有可导入的文件时返回 (nil, "", nil)。
func ImportFromCSharp() (*Credentials, string, error) {
	base, err := os.UserConfigDir()
	if err != nil {
		return nil, "", err
	}
	dir := filepath.Join(base, "FileDedupTool")
	entries, err := os.ReadDir(dir)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, "", nil
		}
		return nil, "", err
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		info, err := e.Info()
		if err != nil || info.Size() == 0 || info.Size() > 1<<20 {
			continue
		}
		path := filepath.Join(dir, e.Name())
		enc, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		raw, err := unprotect(enc)
		if err != nil {
			continue
		}
		var cs struct {
			Platform    string `json:"platform"`
			Ylogin      string `json:"ylogin"`
			PhpdiskInfo string `json:"phpdisk_info"`
			AppToken    string `json:"appToken"`
		}
		if json.Unmarshal(raw, &cs) != nil {
			continue
		}
		if strings.TrimSpace(cs.Ylogin) == "" || strings.TrimSpace(cs.PhpdiskInfo) == "" {
			continue
		}
		p := strings.ToLower(strings.TrimSpace(cs.Platform))
		if p != "" && !strings.Contains(p, "lanzou") {
			continue
		}
		return &Credentials{
			Platform:    PlatformLanzou,
			Ylogin:      cs.Ylogin,
			PhpdiskInfo: cs.PhpdiskInfo,
			SavedAt:     time.Now(),
		}, path, nil
	}
	return nil, "", nil
}
