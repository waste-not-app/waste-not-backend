package security

import (
	"crypto/sha256"
	"encoding/hex"
)

func Digest(raw []byte) string {
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}
