package agenvo

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/felinics/memoh/internal/deliverymeta"
)

// AdvanceBatchDelivery only wakes the existing finite producer command. Neither
// pane.run acceptance nor its output is delivery evidence. The dedicated control
// pane is checked again; this never sends input to the Pi pane or resubmits Pi.
func (s *Service) AdvanceBatchDelivery(ctx context.Context, scope Scope, p BatchProgress) error {
	if !p.valid() || p.Producer == nil || p.ControlPane == "" || p.ControlTerminal == "" || p.ControlPane == p.PaneID {
		return ErrUnsupported
	}
	c, target, err := s.Client(ctx, scope)
	if err != nil {
		return err
	}
	if p.Producer.Task.RootHash != deliverymeta.RootHash(target.WorkDir) {
		return ErrTargetChanged
	}
	collect, err := nativeProducerCollectCommand(target.Producer)
	if err != nil {
		return err
	}
	command := launchBashScript(collect+" >/dev/null 2>&1", target.BashPath)
	r, err := c.native(ctx, scope, target, "pane.get", map[string]any{"session": target.Session, "paneId": p.ControlPane})
	if err != nil {
		return err
	}
	var info struct {
		Type string    `json:"type"`
		Pane batchPane `json:"pane"`
	}
	if resultBody(r, &info) != nil || info.Type != "pane_info" || info.Pane.ID != p.ControlPane || info.Pane.Terminal != p.ControlTerminal || info.Pane.Workspace != p.WorkspaceID {
		return ErrTargetChanged
	}
	_, err = c.native(ctx, scope, target, "pane.run", map[string]any{"session": target.Session, "paneId": p.ControlPane, "command": command})
	return err
}

// RecoverBatchDelivery runs the explicit project producer recovery entry in the
// original dedicated control pane. It does not submit Pi or advance an upload.
// Input is prepared by the authorized original operation owner, never a model.
func (s *Service) RecoverBatchDelivery(ctx context.Context, scope Scope, p BatchProgress, request deliverymeta.ResultRecovery) error {
	if _, ok := request.FilePlan(); !ok || !p.valid() || p.Producer == nil || deliverymeta.PlanHash(*p.Producer) != deliverymeta.PlanHash(request.Plan) ||
		p.RequestHash != request.RequestHash || request.Plan.Task.BotID != scope.BotID || request.Plan.Task.UserID != scope.UserID || request.Plan.Task.TargetID != scope.TargetID ||
		p.ControlPane == "" || p.ControlTerminal == "" || p.ControlPane == p.PaneID {
		return ErrUnsupported
	}
	c, target, err := s.Client(ctx, scope)
	if err != nil {
		return err
	}
	if TargetFingerprint(target) != request.TargetFingerprint || deliverymeta.RootHash(target.WorkDir) != request.Plan.Task.RootHash {
		return ErrTargetChanged
	}
	collect, err := nativeProducerCollectCommand(target.Producer)
	if err != nil {
		return err
	}
	raw, _ := json.Marshal(request)
	entry := strings.Replace(collect, "-action collect-result </dev/null", "-action recover-result", 1)
	command := launchBatchScript("printf '%s' '"+base64.StdEncoding.EncodeToString(raw)+"' | base64 --decode | "+entry+" >/dev/null 2>&1", target)
	if len(command) > 30000 {
		return ErrUnsupported
	}
	r, err := c.native(ctx, scope, target, "pane.get", map[string]any{"session": target.Session, "paneId": p.ControlPane})
	if err != nil {
		return err
	}
	var info struct {
		Type string    `json:"type"`
		Pane batchPane `json:"pane"`
	}
	if resultBody(r, &info) != nil || info.Type != "pane_info" || info.Pane.ID != p.ControlPane || info.Pane.Terminal != p.ControlTerminal || info.Pane.Workspace != p.WorkspaceID {
		return ErrTargetChanged
	}
	_, err = c.native(ctx, scope, target, "pane.run", map[string]any{"session": target.Session, "paneId": p.ControlPane, "command": command})
	return err
}

// ReadBatchProducerFeedback reads only the fixed advisory receipt through the
// original registered control pane. A command acceptance is not a receipt.
func (s *Service) ReadBatchProducerFeedback(ctx context.Context, scope Scope, p BatchProgress) (*deliverymeta.Feedback, error) {
	if !p.valid() || p.Producer == nil || !p.Producer.Valid() || p.Producer.Task.BotID != scope.BotID || p.Producer.Task.TargetID != scope.TargetID || p.Producer.Task.UserID != scope.UserID {
		return nil, ErrUnsupported
	}
	raw, err := s.readBatchFile(ctx, scope, p, deliverymeta.ProducerFeedbackName, 0, deliverymeta.FeedbackMaxBytes)
	if err != nil || len(raw) == 0 {
		return nil, err
	}
	feedback, ok := deliverymeta.DecodeFeedback(raw, *p.Producer)
	if !ok {
		return nil, ErrUnknown
	}
	return feedback, nil
}

// producerEnvelope does not read credentials or change Pi. The declared flat
// result is closed only after Pi exits. Native admission still verifies private
// ownership, links, exact size/hash and an immutable source snapshot.
func producerEnvelope(script string, p BatchProgress, target Target) (string, error) {
	if p.Producer == nil {
		return script, nil
	}
	plan := p.Producer
	if !plan.Valid() || plan.Task.RootHash != deliverymeta.RootHash(target.WorkDir) {
		return "", ErrUnsupported
	}
	raw, _ := json.Marshal(plan)
	setup := fmt.Sprintf(`printf '%%s' '%s' | base64 --decode > "$memoh_attempt/%s"
export MEMOH_DELIVERY_CONTEXT_FILE="$(cygpath -w "$memoh_attempt/%s")"`, base64.StdEncoding.EncodeToString(raw), deliverymeta.ProducerContextName, deliverymeta.ProducerContextName)
	script = strings.Replace(script, `mkdir "$memoh_attempt" || exit 125`, `mkdir "$memoh_attempt" || exit 125`+"\n"+setup, 1)
	// A pre-existing export is not evidence from this invocation. Reject before
	// starting Pi; the ordinary not-started receipt still records that fact.
	var guards, closures strings.Builder
	closures.WriteString(producerClosureFunction(plan.Task))
	for _, file := range plan.FilePlans() {
		fmt.Fprintf(&guards, `[ ! -e "$memoh_root/%s" ] && [ ! -L "$memoh_root/%s" ] && `, file.Artifact.SourceRef, file.Artifact.SourceRef)
		artifact, _ := json.Marshal(file.Artifact)
		fmt.Fprintf(&closures, "memoh_close_result '%s' '%s' '%s'\n", file.Artifact.SourceRef, file.ManifestName(), base64.StdEncoding.EncodeToString(artifact))
	}
	script = strings.Replace(script, `if cd "$memoh_cwd" &&`, `if `+guards.String()+`cd "$memoh_cwd" &&`, 1)
	// One bounded hook per envelope. Remaining manifests stay for later existing
	// task queries/host actions; never rerun Pi or add a background worker.
	collect, err := nativeProducerCollectCommand(target.Producer)
	if err != nil {
		return "", err
	}
	hook := "\nif [ \"$memoh_started\" = true ]; then\n  " + collect + ` > "$memoh_attempt/producer-trigger.json" 2> "$memoh_attempt/producer-trigger-error.txt" || true` + "\nfi\n"

	result := script + closures.String() + hook
	// Leave headroom below Windows CreateProcessW's 32767-character bound.
	// The connector's existing larger command cap alone is insufficient here.
	if len(launchBatchScript(result, target)) > 30000 {
		return "", ErrUnsupported
	}
	return result, nil
}

// The native host's existing collector is a finite CLI, not an enabled Herdr
// plugin hook. Explicit directory references prevent inheritance from a retired
// Agenvo profile. Durable producer admission retains its existing idempotency.
func nativeProducerCollectCommand(p ProducerLocator) (string, error) {
	if p.validate() != nil {
		return "", ErrUnsupported
	}
	quote := func(value string) string { return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'" }
	return fmt.Sprintf(`HERDR_PLUGIN_CONFIG_DIR=%s HERDR_PLUGIN_STATE_DIR=%s "$(cygpath -u %s)" -action collect-result </dev/null`, quote(p.ConfigDir), quote(p.StateDir), quote(p.ProgramPath)), nil
}

func producerClosureFunction(task deliverymeta.Task) string {
	raw, _ := json.Marshal(task)
	prefix := `{"version":1,"task":` + string(raw) + `,"artifact":`
	// Task metadata and the closure body appear once, not once per file. Each
	// call supplies only validated logical names and base64 artifact metadata.
	return fmt.Sprintf(`
memoh_producer_prefix='%s'
memoh_close_result() {
  if [ "$memoh_started" != true ] || [ ! -f "$memoh_root/$1" ] || [ -L "$memoh_root/$1" ]; then return; fi
  memoh_result_hash="$(sha256sum -- "$memoh_root/$1")"; memoh_result_hash="${memoh_result_hash%%%% *}"
  memoh_result_size="$(wc -c < "$memoh_root/$1")"
  if [ "$memoh_result_size" -gt 0 ]; then
    (
      set -C
      {
        printf '%%s' "$memoh_producer_prefix" | base64 --decode
        printf '%%s' "$3" | base64 --decode
        printf ',"closed":true,"sha256":"%%s","size_bytes":%%d}' "$memoh_result_hash" "$memoh_result_size"
      } > "$memoh_root/$2.tmp"
    ) && mv -n -- "$memoh_root/$2.tmp" "$memoh_root/$2"
  fi
}
`, base64.StdEncoding.EncodeToString([]byte(prefix)))
}
