package core

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"encoding/base64"
	"errors"
	"log/slog"
	"strings"
	"testing"

	"github.com/felinics/memoh/internal/agent/runtime/claudecode"
	"github.com/felinics/memoh/internal/agent/runtime/codex"
	"github.com/felinics/memoh/internal/agent/runtime/external"
	"github.com/felinics/memoh/internal/agent/runtime/toolmount"
	"github.com/felinics/memoh/internal/agentcredential"
	"github.com/felinics/memoh/internal/botagents"
	"github.com/felinics/memoh/internal/config"
	dbsqlc "github.com/felinics/memoh/internal/db/postgres/sqlc"
	dbstore "github.com/felinics/memoh/internal/db/store"
	"github.com/felinics/memoh/internal/toolcontext"
	"github.com/felinics/memoh/internal/workspace/bridge"
)

type originalDriverQueries struct {
	dbstore.Queries
	metadata       []byte
	payload, nonce []byte
}

func (q originalDriverQueries) GetBotAgentByID(_ context.Context, p dbsqlc.GetBotAgentByIDParams) (dbsqlc.BotAgent, error) {
	return dbsqlc.BotAgent{ID: p.ID, BotID: p.BotID, Metadata: q.metadata}, nil
}

func (q originalDriverQueries) GetBotAgentCredential(context.Context, dbsqlc.GetBotAgentCredentialParams) (dbsqlc.GetBotAgentCredentialRow, error) {
	return dbsqlc.GetBotAgentCredentialRow{AgentRuntime: "codex", AuthKind: agentcredential.AuthKindOpenAIAPIKey, EncryptedPayload: q.payload, EncryptionNonce: q.nonce, KeyVersion: 1}, nil
}

type hostLauncherProbe struct {
	t     *testing.T
	calls int
	stop  error
}

func (p *hostLauncherProbe) ResolveLauncher(ctx context.Context, _, _ string) (external.Launcher, error) {
	p.calls++
	b, ok := toolcontext.HostComputerBinding(ctx)
	if !ok || b.TargetID != "pc-a" || toolcontext.ComputerScope(ctx) != "pc-a" {
		p.t.Fatal("original driver lost task scope")
	}
	return external.Launcher{}, p.stop
}

// Invoke the unchanged original drivers, not a synthetic replacement driver.
// Stop at CLI resolution after their real bridge/RequireContainer guards.
// Standard MCP transport and tool binding are exercised separately; this test
// neither launches vendor binaries nor claims their external login is tested.
func TestComputerOriginalDriversReachLauncherWithPCTaskScope(t *testing.T) {
	const bot = "11111111-1111-4111-8111-111111111111"
	const agent = "22222222-2222-4222-8222-222222222222"
	key := make([]byte, 32)
	block, _ := aes.NewCipher(key)
	aead, _ := cipher.NewGCM(block)
	nonce := make([]byte, aead.NonceSize())
	for _, runtime := range []string{"claude-code", "codex"} {
		t.Run(runtime, func(t *testing.T) {
			ctx, err := toolcontext.WithComputerBinding(t.Context(), toolcontext.ComputerBinding{BotID: bot, UserID: "user", SessionID: "thread", TargetID: "pc-a", RunID: "run"})
			if err != nil {
				t.Fatal(err)
			}
			q := originalDriverQueries{metadata: []byte(`{"auth":"workspace"}`), nonce: nonce, payload: aead.Seal(nil, nonce, []byte(`{"api_key":"synthetic-only"}`), nil)}
			if runtime == "codex" {
				q.metadata = []byte(`{"auth":"api_key"}`)
			}
			agents := botagents.NewService(slog.Default(), q)
			credentials := agentcredential.NewService(q, config.Config{Auth: config.AuthConfig{AgentCredentialsEncryptionKey: base64.StdEncoding.EncodeToString(key)}})
			source := &nativeHostFixture{guardedRuntimeFixture: guardedRuntimeFixture{info: bridge.WorkspaceInfo{Backend: bridge.WorkspaceBackendAgenvo}, client: &bridge.Client{}}}
			host := containerRuntimeBridge{source: source, runtime: runtime}
			probe := &hostLauncherProbe{t: t, stop: errors.New("synthetic launcher boundary")}
			var driver external.Driver
			if runtime == "claude-code" {
				d := claudecode.NewDriver(host, agents, credentials, nil, toolmount.Gateway{}, slog.Default())
				d.SetLauncherResolver(probe)
				driver = d
			} else {
				d := codex.NewDriver(host, agents, credentials, nil, nil, toolmount.Gateway{}, slog.Default())
				d.SetLauncherResolver(probe)
				driver = d
			}
			for range 2 {
				_, err = driver.Prompt(ctx, external.PromptInput{BotID: bot, BotAgentID: agent, ThreadID: "thread", RunID: "run"})
				if err == nil || !strings.Contains(err.Error(), probe.stop.Error()) {
					t.Fatalf("original driver stopped before launcher: %v", err)
				}
			}
			if probe.calls != 2 || source.clientCalls != 0 || source.nativeCalls != 2 {
				t.Fatalf("host routing: launches=%d native=%d task-client=%d", probe.calls, source.nativeCalls, source.clientCalls)
			}
		})
	}
}
