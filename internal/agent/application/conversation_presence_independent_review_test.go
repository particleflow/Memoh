package application

import (
	"github.com/felinics/memoh/internal/conversationpresence"
	"github.com/felinics/memoh/internal/db/postgres/sqlc"
	"github.com/felinics/memoh/internal/models"
	"github.com/felinics/memoh/internal/reasoning"
	githubcopilot "github.com/felinics/twilight/provider/github/copilot"
	"testing"
)

// Construction only: no Generate, HTTP request, credential or model call.
// The pinned SDK explicitly omits the model wire field for AutoModel, so
// repeating this route cannot establish the same effective selected member.
func TestIndependentPresenceRequiresEffectiveModelIdentity(t *testing.T) {
	for _, tc := range []struct {
		name, id string
		eligible bool
	}{
		{"pinned_model_control", "fixture-pinned-model", true},
		{"upstream_managed_model_without_resolved_member", githubcopilot.AutoModel, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			model := models.GetResponse{ID: "fixture-model-uuid", ModelID: tc.id, Model: models.Model{Enable: true, Type: models.ModelTypeChat, ProviderID: "fixture-provider", ModelID: tc.id, Config: models.ModelConfig{ThinkingMode: reasoning.ModeNone}}}
			provider := sqlc.Provider{Enable: true, ClientType: string(models.ClientTypeGitHubCopilot)}
			cfg := models.SDKModelConfig{ModelID: tc.id, ClientType: provider.ClientType}
			identity := conversationpresence.Identity{ModelUUID: model.ID, ModelID: model.ModelID, ProviderID: model.ProviderID, ClientType: cfg.ClientType}
			verified := declaredPresenceCapabilities(model, provider)
			generator, choice, err := prepareConversationPresenceSDK(identity, model, cfg, verified)
			if err != nil {
				t.Fatal(err)
			}
			usable := verified && generator != nil && choice.Usable
			if usable != tc.eligible {
				t.Fatalf("effective-model qualification=%v want=%v; verified=%v choice=%+v (no resolved served member provided)", usable, tc.eligible, verified, choice)
			}
		})
	}
}
