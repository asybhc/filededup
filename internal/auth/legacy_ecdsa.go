package auth

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"errors"
	"math/big"
)

func sha256Sum(b []byte) []byte {
	sum := sha256.Sum256(b)
	return sum[:]
}

func legacyPublicKey() (*ecdsa.PublicKey, error) {
	der, err := base64.StdEncoding.DecodeString(legacyPublicKeySPKI)
	if err != nil {
		return nil, errors.New("内置旧版公钥损坏")
	}
	key, err := x509.ParsePKIXPublicKey(der)
	if err != nil {
		return nil, errors.New("内置旧版公钥无法解析")
	}
	pub, ok := key.(*ecdsa.PublicKey)
	if !ok || pub.Curve != elliptic.P256() {
		return nil, errors.New("内置旧版公钥类型不正确")
	}
	return pub, nil
}

// verifyP1363 校验固定 64 字节（r||s）的 ECDSA 签名。
func verifyP1363(pub *ecdsa.PublicKey, digest, sig []byte) bool {
	if len(sig) != 64 {
		return false
	}
	r := new(big.Int).SetBytes(sig[:32])
	s := new(big.Int).SetBytes(sig[32:])
	return ecdsa.Verify(pub, digest, r, s)
}
