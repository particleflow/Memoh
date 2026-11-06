package deliverymeta

import "mime"

// Input references an existing NAS original. Paths and URLs are never supplied
// by a caller: the destination is derived from the original task attempt/hash.
type Input struct {
	ContentHash string `json:"content_hash"`
	SizeBytes   int64  `json:"size_bytes"`
	Mime        string `json:"mime"`
}

func (f Input) Valid() bool {
	kind, params, err := mime.ParseMediaType(f.Mime)
	return hex64.MatchString(f.ContentHash) && f.SizeBytes >= 0 && f.SizeBytes <= 1<<40 && len(f.Mime) <= 255 && err == nil && len(params) == 0 && kind == f.Mime
}

type InputPlan struct {
	Version int     `json:"version"`
	Task    Task    `json:"task"`
	Files   []Input `json:"files"`
}

func (p InputPlan) Valid() bool {
	if p.Version != 1 || !p.Task.Valid() || len(p.Files) < 1 || len(p.Files) > MaxArtifacts {
		return false
	}
	seen := make(map[string]bool, len(p.Files))
	for _, f := range p.Files {
		if !f.Valid() || seen[f.ContentHash] {
			return false
		}
		seen[f.ContentHash] = true
	}
	return true
}

func (p InputPlan) Directory() string {
	if !p.Valid() {
		return ""
	}
	return ".memoh-computer/inputs/" + p.Task.Attempt
}

func (p InputPlan) Path(f Input) string {
	if !p.Valid() {
		return ""
	}
	for _, declared := range p.Files {
		if declared == f {
			return p.Directory() + "/" + f.ContentHash
		}
	}
	return ""
}
