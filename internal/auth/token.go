package auth

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"strings"
)

func HashToken(raw string) string {
	sum := sha256.Sum256([]byte(raw))
	return "sha256:" + hex.EncodeToString(sum[:])
}

func VerifyToken(raw, encoded string) bool {
	expected := HashToken(raw)
	if len(expected) != len(encoded) {
		placeholder := make([]byte, len(expected))
		subtle.ConstantTimeCompare([]byte(expected), placeholder)
		return false
	}
	return subtle.ConstantTimeCompare([]byte(expected), []byte(encoded)) == 1
}

func Fingerprint(raw string) string {
	hash := HashToken(raw)
	return strings.TrimPrefix(hash, "sha256:")[:16]
}
