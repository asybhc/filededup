// Package auth 负责机器码、许可证验签、试用期与功能门控。
//
// 新许可证使用 Ed25519 签名（FDLIC1 文本格式），私钥只在签发工具
// （cmd/fdedup-keygen）中使用，客户端只内置公钥。
//
// 旧 C# 版的 ECDSA P-256 许可证只做只读兼容：验签通过即可使用，
// 本包不再签发旧格式。
//
// 机器码与旧版保持一致（MachineGuid + 系统盘卷序列号 → SHA-256 前 10 字节
// → Crockford Base32），因此旧许可证与新许可证绑定同一台机器。
package auth
