package agenvo

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/felinics/memoh/internal/deliverymeta"
)

// AdvanceBatchDelivery only wakes the existing finite plugin command. Neither
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
	_, err = c.native(ctx, scope, target, "pane.run", map[string]any{"session": target.Session, "paneId": p.ControlPane, "command": `& { herdr plugin action invoke collect-result --plugin memoh.delivery *> $null }`})
	return err
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
	setup := fmt.Sprintf(`printf '%%s' '%s' | base64 --decode > "$memoh_attempt/producer-context.json"
export MEMOH_DELIVERY_CONTEXT_FILE="$(cygpath -w "$memoh_attempt/producer-context.json")"`, base64.StdEncoding.EncodeToString(raw))
	script = strings.Replace(script, `mkdir "$memoh_attempt" || exit 125`, `mkdir "$memoh_attempt" || exit 125`+"\n"+setup, 1)
	// A pre-existing export is not evidence from this invocation. Reject before
	// starting Pi; the ordinary not-started receipt still records that fact.
	script = strings.Replace(script, `if cd "$memoh_cwd" &&`, fmt.Sprintf(`if [ ! -e "$memoh_root/%s" ] && [ ! -L "$memoh_root/%s" ] && cd "$memoh_cwd" &&`, plan.Artifact.SourceRef, plan.Artifact.SourceRef), 1)
	prefix := strings.TrimSuffix(string(raw), "}")
	// The task-root and producer-root configurations must name the same location;
	// the closed manifest carries only a consistency digest, never a lookup path.
	closure := fmt.Sprintf(`
if [ "$memoh_started" = true ] && [ -f "$memoh_root/%s" ] && [ ! -L "$memoh_root/%s" ]; then
  memoh_result_hash="$(sha256sum -- "$memoh_root/%s")"; memoh_result_hash="${memoh_result_hash%%%% *}"
  memoh_result_size="$(wc -c < "$memoh_root/%s")"
  if [ "$memoh_result_size" -gt 0 ]; then
    (
      set -C
      {
        printf '%%s' '%s' | base64 --decode
        printf ',"closed":true,"sha256":"%%s","size_bytes":%%d}' "$memoh_result_hash" "$memoh_result_size"
      } > "$memoh_root/%s.tmp"
    ) && mv -n -- "$memoh_root/%s.tmp" "$memoh_root/%s"
    if command -v herdr >/dev/null; then
      herdr plugin action invoke collect-result --plugin memoh.delivery > "$memoh_attempt/producer-trigger.json" 2> "$memoh_attempt/producer-trigger-error.txt" || true
    fi
  fi
fi
`, plan.Artifact.SourceRef, plan.Artifact.SourceRef, plan.Artifact.SourceRef, plan.Artifact.SourceRef, base64.StdEncoding.EncodeToString([]byte(prefix)), plan.ManifestName(), plan.ManifestName(), plan.ManifestName())
	return script + closure, nil
}
