package imageworkflow

import "context"

// DetectCycle reports whether adding candidate to the live edges would close a
// cycle in the pairing graph. Every live workflow is one edge
// input_pair_key -> output_pair_key. A self-reference is a cycle of length one;
// an edge carrying the candidate's own workflow id is ignored, so an update
// re-checks only its new labels.
func DetectCycle(edges []Edge, candidate Edge) error {
	// Layer 0a is structural, but an edge whose ends are equal is a cycle of
	// length one and must be refused before the walk.
	if candidate.InputPairKey == candidate.OutputPairKey {
		return ErrCycle
	}
	forward := make(map[string][]string, len(edges))
	for _, edge := range edges {
		if edge.WorkflowID == candidate.WorkflowID {
			continue
		}
		forward[edge.InputPairKey] = append(forward[edge.InputPairKey], edge.OutputPairKey)
	}
	// Walk forward from the candidate's output label: a cycle exists exactly
	// when the candidate's input label is reachable. Depth is bounded by the
	// number of live edges, so a corrupt graph cannot loop.
	visited := make(map[string]bool, len(forward))
	queue := append([]string(nil), forward[candidate.OutputPairKey]...)
	for steps := 0; len(queue) > 0 && steps <= len(edges)+1; steps++ {
		label := queue[0]
		queue = queue[1:]
		if label == candidate.InputPairKey {
			return ErrCycle
		}
		if visited[label] {
			continue
		}
		visited[label] = true
		queue = append(queue, forward[label]...)
	}
	return nil
}

// CheckCreateCycle is the service entry used by create and update: it reads the
// owner's live edges and applies DetectCycle.
func (s *Service) CheckCreateCycle(ctx context.Context, userID string, candidate Edge) error {
	if s == nil || s.deps.Graph == nil {
		return nil
	}
	edges, err := s.deps.Graph.ListLiveWorkflowEdges(ctx, userID)
	if err != nil {
		return err
	}
	return DetectCycle(edges, candidate)
}
