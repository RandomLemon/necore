package util

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"
)

func GenerateSecureToken(prefix string, byteLength int) (string, error) {
	randomBytes := make([]byte, byteLength)

	if _, err := rand.Read(randomBytes); err != nil {
		return "", fmt.Errorf("failed to generate secure token: %w", err)
	}

	encoded := base64.RawURLEncoding.EncodeToString(randomBytes)

	return fmt.Sprintf("%s_%s", prefix, encoded), nil
}
