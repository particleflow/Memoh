package agenvo

import "path"

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
	attempts := path.Join(".memoh-computer", "attempts")
	return batchLayout{
		Attempts: attempts,
		Attempt:  path.Join(attempts, p.Attempt),
		Session:  path.Join(".memoh-computer", "sessions", p.PiSessionID),
	}, nil
}

func (l batchLayout) resultFile(name string) (string, error) {
	switch name {
	case "receipt.json", "stdout.jsonl", "stderr.txt":
		return path.Join(l.Attempt, name), nil
	default:
		return "", ErrUnsupported
	}
}
