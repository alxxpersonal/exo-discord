package access

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
)

// --- Code Generation ---

// GenerateCode returns a new pairing code.
func GenerateCode() (string, error) {
	buf := make([]byte, 4)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("failed to read random bytes: %w", err)
	}
	return stringsToUpper(hex.EncodeToString(buf)), nil
}

// --- Helpers ---

func stringsToUpper(value string) string {
	return string(bytesToUpper([]byte(value)))
}

func bytesToUpper(value []byte) []byte {
	result := make([]byte, len(value))
	for idx, b := range value {
		if b >= 'a' && b <= 'f' {
			result[idx] = b - 32
			continue
		}
		result[idx] = b
	}
	return result
}
