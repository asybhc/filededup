// Package lanzouyou 实现蓝奏优享（ilanzou）的最小可用客户端：
// 登录、列目录、上传（含秒传与分片）、判重所需的数据结构与报告由 lanzou 包复用。
//
// 协议字段来自 C# 版反编译（c/m.cs、B/F.cs）与字符串表解密，未做真机全流程验证的部分
// 统一写在 docs/ui-shell.md 的「未验证项」里，代码里不把猜测写成确定结论。
package lanzouyou

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"filededup/internal/cloud/lanzou"
)

// CredentialFileName 是优享凭据文件名，与老版 lanzou.bin 分开放。
const CredentialFileName = "youxiang.bin"

// Credentials 是蓝奏优享登录态。优享用 appToken 而不是 Cookie，因此不保存密码。
type Credentials struct {
	Platform string    `json:"platform"`
	AppToken string    `json:"appToken"`
	UID      string    `json:"uid,omitempty"`
	Account  string    `json:"account,omitempty"`
	Uuid     string    `json:"uuid,omitempty"`
	Extra    string    `json:"extra,omitempty"`
	SavedAt  time.Time `json:"savedAt,omitempty"`
}

// Valid 判断登录态是否可用。
func (c Credentials) Valid() bool { return strings.TrimSpace(c.AppToken) != "" }

// MaskedAccount 返回打码后的账号，界面只显示后四位。
func (c Credentials) MaskedAccount() string {
	a := strings.TrimSpace(c.Account)
	if a == "" {
		return ""
	}
	if len(a) <= 4 {
		return "****"
	}
	return "****" + a[len(a)-4:]
}

// CredentialPath 返回 %AppData%\FileDedup\lanzou\youxiang.bin。
func CredentialPath() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "FileDedup", "lanzou", CredentialFileName), nil
}

// SaveCredentials 用 DPAPI 加密后落盘。
func SaveCredentials(creds Credentials) error {
	if !creds.Valid() {
		return errors.New("优享凭据不完整，拒绝保存")
	}
	if creds.Platform == "" {
		creds.Platform = lanzou.PlatformYouxiang
	}
	creds.SavedAt = time.Now()
	raw, err := json.Marshal(creds)
	if err != nil {
		return err
	}
	enc, err := lanzou.ProtectData(raw)
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

// LoadCredentials 读取并解密优享凭据。文件不存在时返回 (nil, nil)。
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
	raw, err := lanzou.UnprotectData(enc)
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
		creds.Platform = lanzou.PlatformYouxiang
	}
	return &creds, nil
}

// DeleteCredentials 删除优享凭据；文件不存在不算错误。
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
