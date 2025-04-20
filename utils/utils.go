package utils

import (
	"errors"
	"math/rand"
	"time"
)

func init() {
	rand.Seed(time.Now().UnixNano())
}

// GenerateID returns a 16-byte hex string ID (UUID-like)
func GenerateID() string {
	const charset = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	b := make([]byte, 8)
	for i := range b {
		b[i] = charset[rand.Intn(len(charset))]
	}
	return string(b)
}

var (
	ErrInvalidTransition = errors.New("invalid status transition")
)

func ValidTransition(currentStatus, newStatus string) bool {
	switch currentStatus {
	case "Queued":
		return newStatus == "Running" || newStatus == "Cancelled"
	case "Running":
		return newStatus == "Done" || newStatus == "Cancelled" || newStatus == "Failed"
	default:
		return false
	}
}
