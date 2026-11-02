package computertask

import (
	"context"
	"strings"

	"github.com/felinics/memoh/internal/computertarget"
	"github.com/felinics/memoh/internal/toolcontext"
)

// ModelSelectionError is returned before any durable claim or dispatch when a
// herdr_pi_task request carries no model id or an id absent from the live
// Magpie catalog. The current catalog travels with the error; the server never
// selects, defaults or substitutes a model on the caller's behalf.
type ModelSelectionError struct {
	Models []PiModel
}

func (e *ModelSelectionError) Error() string {
	ids := make([]string, 0, len(e.Models))
	for _, m := range e.Models {
		ids = append(ids, m.ID)
	}
	return "model selection required: pass model with one id from the current available model list: [" + strings.Join(ids, ", ") + "]"
}

// WithPiModelList injects the live Magpie catalog reader. It reuses the
// server's existing saved-provider wiring; no new provider or key surface.
func (s *Service) WithPiModelList(list PiModelList) *Service {
	s.piModelList = list
	return s
}

// requirePiModel enforces the per-request model choice against the catalog
// current at dispatch time. Empty lister, unreadable catalog, missing id or an
// id no longer present all stop the submission with the live list attached.
// The accepted entry is returned so its stated capability can travel with the
// invocation; a choice absent from the live list never carries metadata.
func (s *Service) requirePiModel(ctx context.Context, in Submit) (PiModel, error) {
	if s.piModelList == nil {
		return PiModel{}, &ModelSelectionError{Models: nil}
	}
	models, err := s.piModelList(ctx)
	if err != nil {
		return PiModel{}, err
	}
	for _, m := range models {
		if m.ID == in.Model {
			return m, nil
		}
	}
	return PiModel{}, &ModelSelectionError{Models: models}
}

// batchModelCapability converts the accepted catalog entry into the capability
// snapshot the endpoint writes into this one task's Pi configuration. Nothing
// is invented: unstated fields stay absent and the endpoint keeps its anchor
// entry value.
func batchModelCapability(m PiModel) *computertarget.BatchModel {
	caps := computertarget.BatchModel{ContextWindow: m.ContextWindow, MaxTokens: m.MaxOutput}
	for _, modality := range m.Input {
		if modality == "text" || modality == "image" {
			caps.Input = append(caps.Input, modality)
		}
	}
	if !caps.Stated() {
		return nil
	}
	return &caps
}

// listedTargets is the computer_targets view: the capability listing plus the
// live model catalog on agent-task-capable targets. A catalog that cannot be
// read is omitted, not guessed; submission itself still requires a readable
// catalog.
func (s *Service) listedTargets(ctx context.Context, b toolcontext.ComputerBinding) ([]Target, error) {
	items, err := s.targets(ctx, b)
	if err != nil || s.piModelList == nil {
		return items, err
	}
	need := false
	for _, item := range items {
		for _, cap := range item.Capabilities {
			if cap == "herdr_pi_task" {
				need = true
			}
		}
	}
	if !need {
		return items, nil
	}
	models, err := s.piModelList(ctx)
	if err != nil {
		return items, nil
	}
	for i := range items {
		for _, cap := range items[i].Capabilities {
			if cap == "herdr_pi_task" {
				items[i].AvailableModels = models
			}
		}
	}
	return items, nil
}
