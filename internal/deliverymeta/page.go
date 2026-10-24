package deliverymeta

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
)

// ClosedPage bounds one stdin message, not the task's total file count. Each
// file retains its established v1 closure and derived delivery ID. There is no
// new batch identity or user-operated migration; replay a page after lost output.
type ClosedPage struct {
	Version int      `json:"version"`
	Files   []Closed `json:"files"`
}

const ProducerPageMaxBytes = MaxArtifacts * 16384

const ProducerPagePrefix = ManifestPrefix + "page_"

// ManifestName identifies a closed transport page, not a new task/attempt.
func (p ClosedPage) ManifestName() string {
	raw, _ := json.Marshal(p)
	return ProducerPageName(raw)
}

// ProducerPageName hashes the exact written bytes, independent of the JSON
// encoder used by a Windows or phone producer.
func ProducerPageName(raw []byte) string {
	h := sha256.Sum256(raw)
	return ProducerPagePrefix + hex.EncodeToString(h[:])
}

func DecodeClosedPage(raw []byte) (ClosedPage, bool) {
	var p ClosedPage
	if len(raw) > ProducerPageMaxBytes {
		return p, false
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	ok := d.Decode(&p) == nil && d.Decode(new(any)) == io.EOF && p.Valid()
	return p, ok
}

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
