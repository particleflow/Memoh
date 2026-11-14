package imageworkflow

// Fingerprint is the idempotency identity of one trigger batch: the SHA-256
// hex of the batch's content hashes sorted ascending and joined by a newline,
// with no trailing newline. Two triggers naming the same bytes in a different
// order therefore share one run.
func Fingerprint(_ []string) (string, error) {
	// TODO(F8): canonicalize and hash.
	return "", ErrNotImplemented
}
