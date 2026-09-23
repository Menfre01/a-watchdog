package config

import (
	"crypto/sha256"
	"encoding/hex"
)

// HashClientKey 计算客户端密钥的存储哈希(SHA-256 十六进制)。
// 数据库只存哈希:明文仅在创建时一次性展示(见 design.md D4)。
func HashClientKey(key string) string {
	sum := sha256.Sum256([]byte(key))
	return hex.EncodeToString(sum[:])
}
