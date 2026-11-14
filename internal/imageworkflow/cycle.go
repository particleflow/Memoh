package imageworkflow

import "context"

// DetectCycle reports whether adding candidate to the live edges would close a
// cycle in the pairing graph. Every live workflow is one edge
// input_pair_key -> output_pair_key. A self-reference is a cycle of length one;
// an edge carrying the candidate's own workflow id is ignored, so an update
// re-checks only its new labels.
func DetectCycle(_ []Edge, _ Edge) error {
	// TODO(F8): walk forward from the candidate's output label and report
	// ErrCycle when the candidate's input label is reachable.
	return ErrNotImplemented
}

// CheckCreateCycle is the service entry used by create and update: it reads the
// owner's live edges and applies DetectCycle.
func (s *Service) CheckCreateCycle(_ context.Context, _ string, _ Edge) error {
	// TODO(F8): read edges through s.deps.Graph, then call DetectCycle.
	return ErrNotImplemented
}
