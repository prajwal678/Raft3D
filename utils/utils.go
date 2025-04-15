package utils

import (
	"errors"

	"crypto/rand"
	"encoding/hex"
)

// GenerateID returns a 16-byte hex string ID (UUID-like)
func GenerateID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "" // or panic/log
	}
	return hex.EncodeToString(b)
}

var (
	ErrInvalidTransition = errors.New("invalid status transition")
)

// ValidTransition checks if the status transition is valid
func ValidTransition(current, next string) bool {
	switch current {
	case "Queued":
		return next == "Running" || next == "Cancelled"
	case "Running":
		return next == "Done" || next == "Cancelled"
	default:
		return false
	}
}
