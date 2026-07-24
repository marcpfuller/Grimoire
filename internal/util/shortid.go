package util

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
)

// GenerateShortID returns a cryptographically random 7-character lowercase hex
// string (28 bits of entropy, ~268M unique values). The format is identical to
// a git short commit hash.
func GenerateShortID() (string, error) {
	b := make([]byte, 4)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("shortid: generate: %w", err)
	}
	return hex.EncodeToString(b)[:7], nil
}
