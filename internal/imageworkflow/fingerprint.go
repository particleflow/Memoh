package imageworkflow

import (
	"crypto/sha256"
	"encoding/hex"
	"sort"
	"strings"
)

// Fingerprint is the idempotency identity of one trigger batch: the SHA-256
// hex of the batch's content hashes sorted ascending and joined by a newline,
// with no trailing newline. Two triggers naming the same bytes in a different
// order therefore share one run.
func Fingerprint(hashes []string) (string, error) {
	cleaned := make([]string, 0, len(hashes))
	for _, hash := range hashes {
		hash = strings.TrimSpace(hash)
		if hash == "" {
			return "", ErrInvalid
		}
		cleaned = append(cleaned, hash)
	}
	if len(cleaned) == 0 {
		return "", ErrInvalid
	}
	sort.Strings(cleaned)
	sum := sha256.Sum256([]byte(strings.Join(cleaned, "\n")))
	return hex.EncodeToString(sum[:]), nil
}
