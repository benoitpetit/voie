package utils

import (
	"crypto/rand"
	"fmt"
)

// GenerateID returns a random identifier for request and completion IDs.
func GenerateID() string {
	b := make([]byte, 16)
	rand.Read(b)
	return fmt.Sprintf("%x", b)
}
