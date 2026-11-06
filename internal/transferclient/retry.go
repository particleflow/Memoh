package transferclient

import (
	"errors"
	"math/rand/v2"
	"time"
)

// This is only the existing local backoff ceiling, not a server deadline cap
// or a total attempt budget. Admission/retention limits remain independent.
const maxRetryDelay = 5 * time.Minute

// Attempts are diagnostics, never permission or a terminal-state condition.
// Saturate at the representation limit so a long-lived journal cannot wrap.
func incrementAttempts(n int) int {
	if n < int(^uint(0)>>1) {
		return n + 1
	}
	return n
}

func retryDue(now time.Time, attempts int, err error) time.Time {
	ceiling := time.Second
	for n := 0; n < attempts && ceiling < maxRetryDelay; n++ {
		ceiling = min(ceiling*2, maxRetryDelay)
	}
	delay := ceiling/2 + time.Duration(rand.Int64N(int64(ceiling/2))) //nolint:gosec // Scheduling jitter is not an identity or security decision.
	due := now.Add(delay)
	var h *HTTPError
	if errors.As(err, &h) {
		if h.RetryAfter > 0 && now.Add(h.RetryAfter).After(due) {
			due = now.Add(h.RetryAfter)
		}
		if h.RetryAt.After(due) {
			due = h.RetryAt
		}
	}
	return due
}
