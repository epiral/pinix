// Role:    ID generation utilities
// Depends: crypto/rand, encoding/hex, fmt
// Exports: NewID

package agent

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
)

// NewID generates a random ID with the given prefix.
// Example: NewID("agt") → "agt_a1b2c3d4e5f67890"
func NewID(prefix string) string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return fmt.Sprintf("%s_%s", prefix, hex.EncodeToString(b))
}
