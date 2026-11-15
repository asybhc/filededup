package update

import (
	"crypto/ed25519"
	"encoding/base64"
	"fmt"
	"sync"
)

// updatePublicKey 是更新签名专用公钥（base64url，无填充），与许可证公钥完全独立。
// 对应私钥只保存在仓库外，由 cmd/fdedup-update-sign 使用。
const updatePublicKey = "Wkm5PGUPW6JYqTWkyq79nbic3DnzuWE0v0XFPc0phaA"

var (
	pubOnce sync.Once
	pubKey  ed25519.PublicKey
	pubErr  error
)

// PublicKey 返回内置的更新验签公钥。
func PublicKey() (ed25519.PublicKey, error) {
	pubOnce.Do(func() {
		b, err := base64.RawURLEncoding.DecodeString(updatePublicKey)
		if err != nil || len(b) != ed25519.PublicKeySize {
			pubErr = fmt.Errorf("内置更新公钥损坏：%v", err)
			return
		}
		pubKey = ed25519.PublicKey(b)
	})
	return pubKey, pubErr
}
