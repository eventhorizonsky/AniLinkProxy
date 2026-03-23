package security

import (
	"crypto/sha256"
	"encoding/base64"
	"strconv"
)

// GenerateSignature 按平台协议计算 X-Signature。
// 规则：base64(sha256(appId + timestamp + path + secret))
func GenerateSignature(appID string, ts int64, path string, secret string) string {
	raw := appID + strconv.FormatInt(ts, 10) + path + secret
	sum := sha256.Sum256([]byte(raw))
	return base64.StdEncoding.EncodeToString(sum[:])
}
