package agenvo

import (
	"path"

	"github.com/felinics/memoh/internal/deliverymeta"
)

// BatchLayoutVersion identifies paths within the configured Windows target
// root, independently of the receipt format and Pi execution protocol.
const BatchLayoutVersion = 1

type batchLayout struct {
	Attempts string
	Attempt  string
	Session  string
}

// resolveBatchLayout is shared by the envelope writer and recovery reader.
// Version zero is the pre-versioned durable operation layout, identical to v1.
// These are Bash-relative paths, not paths on the Go server's filesystem;
// conversion of the configured Windows root stays at the cygpath boundary.
func resolveBatchLayout(p BatchProgress) (batchLayout, error) {
	if (p.LayoutVersion != 0 && p.LayoutVersion != BatchLayoutVersion) || !batchID.MatchString(p.Attempt) || !batchHex.MatchString(p.PiSessionID) {
		return batchLayout{}, ErrUnsupported
	}
	attempt, ok := deliverymeta.AttemptDirectory(p.Attempt)
	if !ok {
		return batchLayout{}, ErrUnsupported
	}
	attempts := path.Dir(attempt)
	return batchLayout{
		Attempts: attempts,
		Attempt:  attempt,
		Session:  path.Join(".memoh-computer", "sessions", p.PiSessionID),
	}, nil
}

func (l batchLayout) resultFile(name string) (string, error) {
	switch name {
	case "receipt.json", "stdout.jsonl", "stderr.txt", deliverymeta.ProducerFeedbackName:
		return path.Join(l.Attempt, name), nil
	default:
		return "", ErrUnsupported
	}
}
