package transferclient

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/felinics/memoh/internal/deliverymeta"
)

// Task sources use the invoking OS identity's existing access. Only config,
// credentials, queue and publications require private local durable storage.
// No root spelling/config digest, task identity or stored source ID is rewritten.
func openTaskSourceRoot(path string) (*os.Root, *os.File, error) {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return nil, nil, ErrBinding
	}
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil || resolved != path {
		return nil, nil, ErrBinding
	}
	root, err := os.OpenRoot(path)
	if err != nil {
		return nil, nil, ErrBinding
	}
	guard, err := guardSourceRoot(root)
	if err != nil {
		_ = root.Close()
		return nil, nil, err
	}
	return root, guard, nil
}

func sourceLeaf(name string) bool {
	return name != "" && name != "." && name != ".." && filepath.Base(name) == name && !strings.ContainsAny(name, `\/:`)
}

// Preserve legacy source/manifest references, adding only the explicit bounded
// result filename contract. Queue, receipt and transport IDs still use validID.
func validSourceRef(ref string) bool {
	return validID(ref) || deliverymeta.ValidSourceName(ref)
}

func sourceManifestName(name string) bool {
	return strings.HasPrefix(name, deliverymeta.ManifestPrefix) && validHash(strings.TrimPrefix(name, deliverymeta.ManifestPrefix))
}

// Open only existing, non-link attempt components beneath the pinned source.
// In particular, never resolve an absolute attempt path independently of it.
func (h *HostConfig) sourceAttempt(attempt string) (*os.Root, func(), error) {
	relative, ok := deliverymeta.AttemptDirectory(attempt)
	if !ok || h.openSources() != nil {
		return nil, nil, ErrProducerManifest
	}
	parent := h.sources
	var roots []*os.Root
	var guards []*os.File
	closeAll := func() {
		for i := len(roots) - 1; i >= 0; i-- {
			closeQueueGuard(guards[i])
			_ = roots[i].Close()
		}
	}
	for _, name := range strings.Split(relative, "/") {
		root, guard, err := openSourceChild(parent, name)
		if err != nil {
			closeAll()
			return nil, nil, ErrProducerManifest
		}
		roots, guards = append(roots, root), append(guards, guard)
		parent = root
	}
	return parent, closeAll, nil
}

func sourceBytes(file *os.File, limit int64) ([]byte, error) {
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return nil, ErrProducerManifest
	}
	raw, err := io.ReadAll(io.LimitReader(file, limit+1))
	if err != nil || int64(len(raw)) > limit {
		return nil, ErrProducerManifest
	}
	return raw, nil
}

func (h *HostConfig) sourcePlan(attempt string) (deliverymeta.Plan, *os.Root, *os.File, func(), error) {
	root, closeRoot, err := h.sourceAttempt(attempt)
	if err != nil {
		return deliverymeta.Plan{}, nil, nil, nil, err
	}
	f, err := openSourceFile(root, deliverymeta.ProducerContextName, os.O_RDONLY, false)
	if err != nil {
		closeRoot()
		return deliverymeta.Plan{}, nil, nil, nil, ErrProducerManifest
	}
	closeAll := func() { _ = f.Close(); closeRoot() }
	raw, err := sourceBytes(f, 16384)
	var plan deliverymeta.Plan
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err != nil || dec.Decode(&plan) != nil || dec.Decode(new(any)) != io.EOF || !plan.Valid() || plan.Task.Attempt != attempt || plan.Task.RootHash != deliverymeta.RootHash(h.sourcePath) {
		closeAll()
		return deliverymeta.Plan{}, nil, nil, nil, ErrProducerManifest
	}
	return plan, root, f, closeAll, nil
}

func (h *HostConfig) sourceDeclares(closed deliverymeta.Closed) error {
	plan, _, _, closePlan, err := h.sourcePlan(closed.Task.Attempt)
	if err != nil {
		return err
	}
	defer closePlan()
	if plan.Task == closed.Task {
		for _, file := range plan.FilePlans() {
			if file.Artifact == closed.Artifact {
				return nil
			}
		}
	}
	return ErrProducerManifest
}

// Delete only the exact validated closed manifest, never an artifact. Native
// Windows deletes through its pinned handle; the queue already owns the durable
// original-ID registration if death precedes deletion or directory flush.
func (h *HostConfig) consumeSourceManifest(name string, raw []byte) error {
	_, closed, err := ProducerAdmission(bytes.NewReader(raw), "source-check")
	if err != nil || name != closed.ManifestName() || closed.Task.RootHash != deliverymeta.RootHash(h.sourcePath) || h.sourceDeclares(closed) != nil {
		return ErrProducerManifest
	}
	f, err := openSourceFile(h.sources, name, os.O_RDONLY, true)
	if os.IsNotExist(err) {
		return nil // another collector consumed the same original registration
	}
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	actual, err := sourceBytes(f, 16384)
	if err != nil || !bytes.Equal(raw, actual) {
		return ErrProducerManifest
	}
	if err := removeSourceManifest(h.sources, name, f); err != nil {
		return err
	}
	if err := f.Close(); err != nil {
		return ErrStore
	}
	return syncSourceDir(h.sources)
}
