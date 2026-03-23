package utils

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"math/big"
	"strings"
)

func RandString(n int) string {
	const letters = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	var b strings.Builder
	for i := 0; i < n; i++ {
		num, _ := rand.Int(rand.Reader, big.NewInt(int64(len(letters))))
		b.WriteByte(letters[num.Int64()])
	}
	return b.String()
}

func RandCode(n int) string {
	const digits = "0123456789"
	var b strings.Builder
	for i := 0; i < n; i++ {
		num, _ := rand.Int(rand.Reader, big.NewInt(int64(len(digits))))
		b.WriteByte(digits[num.Int64()])
	}
	return b.String()
}

func ShaHex(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

func ShortHash(s string) string {
	if strings.TrimSpace(s) == "" {
		return "empty"
	}
	v := ShaHex(s)
	if len(v) > 12 {
		return v[:12]
	}
	return v
}
