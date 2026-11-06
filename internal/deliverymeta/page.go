package deliverymeta

// ClosedPage bounds one stdin message, not the task's total file count. Each
// file retains its established v1 closure and derived delivery ID. There is no
// new batch identity or user-operated migration; replay a page after lost output.
type ClosedPage struct {
	Version int      `json:"version"`
	Files   []Closed `json:"files"`
}

const ProducerPageMaxBytes = MaxArtifacts * 16384

func (p ClosedPage) Valid() bool {
	if p.Version != 1 || len(p.Files) < 1 || len(p.Files) > MaxArtifacts {
		return false
	}
	artifacts := make([]Artifact, 0, len(p.Files))
	for _, file := range p.Files {
		if !file.Valid() || file.Task != p.Files[0].Task {
			return false
		}
		artifacts = append(artifacts, file.Artifact)
	}
	return ValidArtifacts(artifacts)
}
