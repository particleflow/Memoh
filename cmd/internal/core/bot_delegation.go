package core

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"slices"

	"github.com/jackc/pgx/v5"
	"go.uber.org/fx"

	"github.com/felinics/memoh/internal/accounts"
	"github.com/felinics/memoh/internal/agent/application"
	historyfrag "github.com/felinics/memoh/internal/agent/context/history"
	"github.com/felinics/memoh/internal/agent/turn"
	"github.com/felinics/memoh/internal/agenvo"
	attachmentpkg "github.com/felinics/memoh/internal/attachment"
	"github.com/felinics/memoh/internal/boot"
	"github.com/felinics/memoh/internal/botdelegation"
	"github.com/felinics/memoh/internal/bots"
	"github.com/felinics/memoh/internal/channel"
	"github.com/felinics/memoh/internal/channel/route"
	messagepkg "github.com/felinics/memoh/internal/chat/message"
	"github.com/felinics/memoh/internal/chat/thread"
	"github.com/felinics/memoh/internal/config"
	"github.com/felinics/memoh/internal/db"
	"github.com/felinics/memoh/internal/db/postgres/sqlc"
	postgresstore "github.com/felinics/memoh/internal/db/postgres/store"
	dbstore "github.com/felinics/memoh/internal/db/store"
	"github.com/felinics/memoh/internal/media"
	"github.com/felinics/memoh/internal/storage"
	"github.com/felinics/memoh/internal/toolcontext"
	"github.com/felinics/memoh/internal/workdir"
	"github.com/felinics/memoh/internal/workspace"
)

type delegationHost struct {
	store       *postgresstore.Store
	accounts    *accounts.Service
	permissions *applicationBotPermissionChecker
	sessions    *thread.Service
	agent       *application.Service
	service     *botdelegation.Service
	messages    *messagepkg.DBService
	media       *media.Service
	routes      delegationRouteReader
}

func provideBotDelegation(lc fx.Lifecycle, log *slog.Logger, store *postgresstore.Store, accounts *accounts.Service, bots *bots.Service, sessions *thread.Service, media *media.Service, messages *messagepkg.DBService, runtime *boot.RuntimeConfig, routes *route.DBService, cfg config.Config) (*botdelegation.Service, error) {
	timeout, err := cfg.BotDelegation.ExecutionTimeoutDuration()
	if err != nil {
		return nil, err
	}
	h := &delegationHost{store: store, accounts: accounts, permissions: &applicationBotPermissionChecker{bots: bots, accounts: accounts}, sessions: sessions, media: media, messages: messages, routes: routes}
	a := &botdelegation.Authority{Permissions: h, Sessions: sessions, Workdirs: delegationWorkdirs{store}, Mounts: store, Assets: h}
	s := botdelegation.NewService(a, botdelegation.NewPostgresLedger(store.SQLC()), media, log, timeout)
	s.SetCursorSecret(runtime.JwtSecret)
	h.service = s
	s.Host = h
	lc.Append(fx.Hook{OnStop: func(context.Context) error { s.Close(); return nil }})
	return s, nil
}

// The authority reads persisted selection metadata, not a workspace capability.
// Using workdir.Service.Get here would compare the source Bot's mount with the
// destination ComputerScope and reject two valid mounts of the same computer.
type delegationWorkdirs struct {
	store interface {
		GetWorkdir(context.Context, string, string) (dbstore.BotWorkdirRecord, error)
	}
}

func (w delegationWorkdirs) Get(ctx context.Context, bot, id string) (workdir.Workdir, error) {
	r, err := w.store.GetWorkdir(ctx, bot, id)
	if err != nil {
		return workdir.Workdir{}, err
	}
	target := "native"
	if r.TargetKind == workdir.TargetKindRemote {
		target = r.RemoteBindingID
	}
	return workdir.Workdir{ID: r.ID, BotID: r.BotID, WorkspaceTargetID: target}, nil
}

func injectBotDelegation(s *botdelegation.Service, agent *application.Service, assets *media.Service) {
	h := s.Host.(*delegationHost)
	h.agent = agent
	agent.SetDelegationScope(h.prepare)
	agent.SetGatewayAssetLoader(&delegationAssetLoader{base: &gatewayAssetLoaderAdapter{media: assets}, host: h})
}

func (h *delegationHost) ValidateSession(ctx context.Context, user string) error {
	return h.accounts.ValidateSession(ctx, user)
}

func (h *delegationHost) BotTeam(ctx context.Context, bot string) (string, error) {
	id, err := db.ParseUUID(bot)
	if err != nil {
		return "", err
	}
	team, err := h.store.DelegationBotTeam(ctx, id)
	return team.String(), err
}

func (h *delegationHost) Allowed(ctx context.Context, user, bot, permission string) (bool, error) {
	return h.permissions.HasBotPermission(ctx, bot, user, permission)
}

func (h *delegationHost) SessionHasAsset(ctx context.Context, bot, session, hash string) (bool, error) {
	params := sqlc.SessionHasMediaAssetParams{BotID: db.ParseUUIDOrEmpty(bot), SessionID: db.ParseUUIDOrEmpty(session), ContentHash: hash}
	linked, err := h.store.SessionHasMediaAsset(ctx, params)
	if err != nil || linked {
		return linked, err
	}
	// Native chat commits the user message with the first complete model/tool
	// step. A tool in that step can therefore precede its history asset link.
	// Use only the current admitted, fenced input, not arbitrary Bot-owned files
	// or an old run's input after history was cleared.
	run, err := h.store.GetLatestSessionRun(ctx, params.SessionID)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	s, err := h.store.GetSessionByID(ctx, params.SessionID)
	if err != nil {
		return false, err
	}
	if admittedDelegationInput(run, s, bot, hash) {
		return true, nil
	}
	// The first step may have committed between the history and run reads.
	return h.store.SessionHasMediaAsset(ctx, params)
}

func admittedDelegationInput(run sqlc.SessionRun, s sqlc.BotSession, bot, hash string) bool {
	if run.BotID.String() != bot || s.BotID != run.BotID || s.ID != run.SessionID || s.TeamID != run.TeamID || s.DeletedAt.Valid || !s.CreatedByUserID.Valid || run.FencingToken <= 0 || run.FencingToken != s.RuntimeFencingToken {
		return false
	}
	switch run.State {
	case "running", "waiting_decision", "finishing":
	default:
		return false
	}
	var input struct {
		Resume struct {
			Version          int      `json:"version"`
			UserID           string   `json:"user_id"`
			InputAssetHashes []string `json:"input_asset_hashes"`
		} `json:"resume"`
	}
	return json.Unmarshal(run.InputJson, &input) == nil && input.Resume.Version == 1 && input.Resume.UserID == s.CreatedByUserID.String() && slices.Contains(input.Resume.InputAssetHashes, hash)
}

func (h *delegationHost) ResultHasAsset(ctx context.Context, bot, user, root, hash string) (bool, error) {
	refs, err := h.resultAssets(ctx, bot, user, root)
	return slices.Contains(refs, hash), err
}

func (h *delegationHost) resultAssets(ctx context.Context, bot, user, root string) ([]string, error) {
	return h.store.DelegationResultAssets(ctx, sqlc.DelegationResultAssetsParams{BotID: db.ParseUUIDOrEmpty(bot), UserID: db.ParseUUIDOrEmpty(user), RootID: db.ParseUUIDOrEmpty(root)})
}

func (h *delegationHost) Plan(ctx context.Context, b botdelegation.Identity, selected string) ([]botdelegation.DevicePair, string, error) {
	var candidates []dbstore.BotRemoteRuntimeBindingRecord
	if selected == "native" {
		var err error
		candidates, err = h.store.ListMounts(ctx, b.SourceBotID)
		if err != nil {
			return nil, "", err
		}
	} else {
		m, err := h.store.GetMount(ctx, b.SourceBotID, selected)
		if err != nil {
			return nil, "", botdelegation.ErrDenied
		}
		candidates = []dbstore.BotRemoteRuntimeBindingRecord{m}
	}
	pairs := []botdelegation.DevicePair{}
	target := "native"
	for _, m := range candidates {
		if m.RuntimeRevoked || m.RuntimeUserID != m.BotOwnerUserID || m.RuntimeProvider != agenvo.Provider {
			continue
		}
		id, err := h.store.DelegationMountForRuntime(ctx, sqlc.DelegationMountForRuntimeParams{BotID: db.ParseUUIDOrEmpty(b.TargetBotID), RuntimeID: db.ParseUUIDOrEmpty(m.RuntimeID)})
		if errors.Is(err, pgx.ErrNoRows) {
			continue
		}
		if err != nil {
			return nil, "", err
		}
		tm, err := h.store.GetMount(ctx, b.TargetBotID, id.String())
		if err != nil {
			return nil, "", err
		}
		t, err := agenvo.DecodeTarget(m.RuntimeProviderConfig)
		if err != nil {
			continue
		}
		tt, err := agenvo.DecodeTarget(tm.RuntimeProviderConfig)
		if err != nil || tm.RuntimeRevoked || tm.RuntimeUserID != tm.BotOwnerUserID || tm.RuntimeProvider != agenvo.Provider || agenvo.TargetFingerprint(t) != agenvo.TargetFingerprint(tt) {
			continue
		}
		pairs = append(pairs, botdelegation.DevicePair{SourceMountID: m.ID, TargetMountID: tm.ID, RuntimeID: m.RuntimeID, Fingerprint: agenvo.TargetFingerprint(t)})
		if selected != "native" {
			target = tm.ID
		}
	}
	if selected != "native" && target == "native" {
		return nil, "", botdelegation.ErrDenied
	}
	return pairs, target, nil
}

func (h *delegationHost) CreateSession(ctx context.Context, r botdelegation.Record) (string, error) {
	s, err := h.sessions.Create(ctx, thread.CreateInput{BotID: r.Identity.TargetBotID, Type: thread.TypeChat, Visibility: thread.VisibilityInternal, CreatedByUserID: r.Identity.UserID, ChannelType: "web", Title: "Delegated task", Metadata: map[string]any{"workspace_target_id": r.Payload.TargetSelection}})
	return s.ID, err
}

func (h *delegationHost) inputAttachments(ctx context.Context, r botdelegation.Record, result bool) ([]turn.Attachment, error) {
	refs := r.Payload.Inputs
	if result {
		refs = r.Result.Outputs
	}
	out := make([]turn.Attachment, 0, len(refs))
	for _, hash := range refs {
		a, err := h.service.Attachments(r).Describe(ctx, hash, result)
		if err != nil {
			return nil, err
		}
		typ := "file"
		if len(a.Mime) > 6 && a.Mime[:6] == "image/" {
			typ = "image"
		}
		out = append(out, turn.Attachment{ContentHash: hash, Type: typ, Mime: a.Mime, Size: a.SizeBytes, URL: botdelegation.AttachmentURL(r, hash), Metadata: map[string]any{"delegation_url": botdelegation.AttachmentURL(r, hash)}})
	}
	return out, nil
}

func (h *delegationHost) Run(ctx context.Context, r botdelegation.Record, started func(string) error) (botdelegation.Result, string, error) {
	attachments, err := h.inputAttachments(ctx, r, false)
	if err != nil {
		return botdelegation.Result{}, "failed", err
	}
	cmd := turn.StartTurnCommand{SchemaVersion: 1, NoDefer: true, TeamID: r.Identity.TeamID, Mode: turn.ModeChat, BotID: r.Identity.TargetBotID, ChatID: r.Identity.TargetBotID, ThreadID: r.Identity.TargetSessionID, UserID: r.Identity.UserID, SourceChannelIdentityID: r.Identity.UserID, IdempotencyKey: "bot-delegation:" + r.ID, Query: r.Payload.Message, UserVisibleText: r.Payload.Message, Attachments: attachments, CurrentChannel: "web", ReplyTarget: r.Identity.TargetBotID}
	handle, err := h.agent.StartTurn(ctx, cmd)
	if err != nil {
		return botdelegation.Result{}, "unknown", err
	}
	if err = started(handle.RunID()); err != nil {
		handle.Cancel()
		return botdelegation.Result{}, "unknown", err
	}
	events, errs := handle.Events(), handle.Errs()
	var runErr error
	var outbound []messagepkg.AssetRef
	for events != nil || errs != nil {
		select {
		case <-ctx.Done():
			handle.Cancel()
			return botdelegation.Result{}, "unknown", ctx.Err()
		case event, ok := <-events:
			if !ok {
				events = nil
			} else if event.Kind == "attachment_delta" {
				refs, err := h.outboundRefs(ctx, r, event.Payload)
				outbound = append(outbound, refs...)
				runErr = errors.Join(runErr, err)
			}
		case e, ok := <-errs:
			if !ok {
				errs = nil
			} else if e != nil {
				runErr = errors.Join(runErr, e)
			}
		}
	}
	run, err := h.store.GetSessionRun(ctx, db.ParseUUIDOrEmpty(handle.RunID()))
	if err != nil || run.BotID.String() != r.Identity.TargetBotID || run.SessionID.String() != r.Identity.TargetSessionID || run.TeamID.String() != r.Identity.TeamID {
		return botdelegation.Result{}, "unknown", errors.Join(runErr, err)
	}
	if run.State != "completed" {
		if run.State == "failed" {
			return botdelegation.Result{}, "failed", runErr
		}
		return botdelegation.Result{}, "unknown", runErr
	}
	result := botdelegation.Result{}
	m, err := h.store.DelegationFinalMessage(ctx, sqlc.DelegationFinalMessageParams{BotID: run.BotID, SessionID: run.SessionID, RunID: run.RunID})
	if err == nil {
		if len(outbound) > 0 {
			if linkErr := h.messages.LinkAssets(ctx, m.ID.String(), outbound); linkErr != nil {
				return botdelegation.Result{}, "unknown", linkErr
			}
		}
		result.Summary = botdelegation.FinalSummary([]turn.ModelMessage{historyfrag.DecodeStoredModelMessage(nil, m.ID.String(), m.Role, m.Content)}, true)
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return botdelegation.Result{}, "unknown", err
	}
	result.Outputs, err = h.resultAssets(ctx, r.Identity.TargetBotID, r.Identity.UserID, r.Identity.TargetSessionID)
	return result, "completed", errors.Join(runErr, err)
}

func (h *delegationHost) Deliver(ctx context.Context, r botdelegation.Record) error {
	routing, err := h.deliveryRoute(ctx, r)
	if err != nil {
		return err
	}
	attachments, err := h.inputAttachments(ctx, r, true)
	if err != nil {
		return err
	}
	text := "Delegation " + r.ID + " (Bot " + r.Identity.TargetBotID + ") execution state: " + r.State + "."
	if r.Result.Summary != nil {
		text += "\n\n" + r.Result.Summary.Text
	}
	return h.agent.EnqueueAuthorizedFollowUp(ctx, "bot-delegation-result:"+r.ID, delegationDeliveryCommand(r, routing, text, attachments))
}

type (
	delegationMediaIdentity struct{ bot, user, session string }
	delegationMediaKey      struct{}
)

// Like the Web/channel ingress, the host normalizes send's stream attachments
// through the existing media pipeline before linking the final assistant. Input
// hashes keep their source owner; they are never re-ingested under the specialist.
func (h *delegationHost) outboundRefs(ctx context.Context, r botdelegation.Record, raw json.RawMessage) ([]messagepkg.AssetRef, error) {
	var event struct {
		Type        string           `json:"type"`
		Attachments []map[string]any `json:"attachments"`
	}
	if json.Unmarshal(raw, &event) != nil || event.Type != "attachment_delta" {
		return nil, nil
	}
	if _, err := h.service.Authority.Authorize(ctx, r.Identity); err != nil {
		return nil, err
	}
	ctx, _, err := h.prepare(ctx, application.ChatRequest{BotID: r.Identity.TargetBotID, ThreadID: r.Identity.TargetSessionID, UserID: r.Identity.UserID})
	if err != nil {
		return nil, err
	}
	refs := []messagepkg.AssetRef{}
	for _, item := range event.Attachments {
		b := attachmentpkg.BundleFromMap(item)
		if b.ContentHash == "" {
			if b.Path != "" {
				if err := toolcontext.CheckDelegationTarget(ctx, r.Identity.TargetBotID, r.Payload.TargetSelection, bots.PermissionWorkspaceRead); err != nil {
					return nil, err
				}
			}
			prepared, err := channel.PrepareStreamEvent(ctx, h.media, channel.ChannelConfig{BotID: r.Identity.TargetBotID, ChannelType: "web"}, channel.StreamEvent{Type: channel.StreamEventAttachment, Attachments: []channel.Attachment{channel.AttachmentFromBundle(b)}})
			if err != nil {
				return nil, err
			}
			attachments := prepared.LogicalEvent().Attachments
			if len(attachments) != 1 || attachments[0].ContentHash == "" {
				return nil, storage.ErrAccessPathUnavailable
			}
			b = channel.BundleFromAttachment(attachments[0])
		}
		var asset media.Asset
		if slices.Contains(r.Payload.Inputs, b.ContentHash) {
			if _, err := h.service.Attachments(r).Describe(ctx, b.ContentHash, false); err != nil {
				return nil, err
			}
			a, err := h.media.Stat(ctx, r.Identity.SourceBotID, b.ContentHash)
			if err != nil {
				return nil, err
			}
			asset = a
		} else {
			a, err := h.media.Stat(ctx, r.Identity.TargetBotID, b.ContentHash)
			if err != nil {
				return nil, err
			}
			asset = a
		}
		refs = append(refs, messagepkg.AssetRef{ContentHash: asset.ContentHash, Role: "attachment", Ordinal: len(refs), Mime: asset.Mime, SizeBytes: asset.SizeBytes, Metadata: map[string]any{"delegation_url": botdelegation.AttachmentURL(r, asset.ContentHash)}})
	}
	return refs, nil
}

func (h *delegationHost) prepare(ctx context.Context, req application.ChatRequest) (context.Context, application.ChatRequest, error) {
	ctx = context.WithValue(ctx, delegationMediaKey{}, delegationMediaIdentity{req.BotID, req.UserID, req.ThreadID})
	if req.DelegationResultID != "" {
		r, err := h.service.ReadRecord(ctx, req.BotID, req.UserID, req.ThreadID, req.DelegationResultID)
		if err != nil || (r.State != "completed" && r.State != "failed") {
			return ctx, req, botdelegation.ErrDenied
		}
		// Revalidate again at queued-turn admission, including after restart.
		routing, err := h.deliveryRoute(ctx, r)
		if err != nil || !delegationRequestMatchesRoute(req, routing) {
			return ctx, req, botdelegation.ErrDenied
		}
		attachments, err := h.inputAttachments(ctx, r, true)
		if err != nil {
			return ctx, req, err
		}
		refs := make([]turn.OutboundAssetRef, 0, len(attachments))
		for _, a := range attachments {
			refs = append(refs, turn.OutboundAssetRef{ContentHash: a.ContentHash, Mime: a.Mime, SizeBytes: a.Size, Role: "attachment", Ordinal: len(refs), Metadata: a.Metadata})
		}
		ctx = application.WithDelegationResultAssets(ctx, req.BotID, req.ThreadID, r.ID, refs)
	}
	if req.ThreadID == "" || req.UserID == "" {
		return ctx, req, nil
	}
	r, err := h.service.ForSession(ctx, req.BotID, req.UserID, req.ThreadID)
	if errors.Is(err, pgx.ErrNoRows) {
		return ctx, req, nil
	}
	if err != nil {
		return ctx, req, err
	}
	if req.WorkspaceTargetID != "" && req.WorkspaceTargetID != r.Payload.TargetSelection {
		return ctx, req, botdelegation.ErrDenied
	}
	ctx = h.service.BindScope(ctx, r)
	ctx = workspace.WithWorkspaceTarget(ctx, r.Payload.TargetSelection)
	if r.Payload.TargetSelection != "native" {
		req.WorkspaceTargetID = r.Payload.TargetSelection
	}
	return ctx, req, nil
}

type delegationAssetLoader struct {
	base *gatewayAssetLoaderAdapter
	host *delegationHost
}

func (a *delegationAssetLoader) shared(ctx context.Context, bot, hash string) (*botdelegation.AttachmentSet, bool, error) {
	i, ok := ctx.Value(delegationMediaKey{}).(delegationMediaIdentity)
	if !ok || i.bot != bot || i.user == "" || i.session == "" {
		return nil, false, nil
	}
	r, err := a.host.service.ForSession(ctx, bot, i.user, i.session)
	if err == nil {
		if slices.Contains(r.Payload.Inputs, hash) {
			return a.host.service.Attachments(r), false, nil
		}
		return nil, false, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return nil, false, err
	}
	rows, err := a.host.store.DelegationOutputReferences(ctx, sqlc.DelegationOutputReferencesParams{SourceBotID: db.ParseUUIDOrEmpty(bot), SourceSessionID: db.ParseUUIDOrEmpty(i.session), UserID: db.ParseUUIDOrEmpty(i.user)})
	if err != nil {
		return nil, false, err
	}
	for _, row := range rows {
		r, err := a.host.service.Ledger.Get(ctx, bot, i.user, i.session, row.ID.String())
		if err != nil {
			return nil, false, err
		}
		if slices.Contains(r.Result.Outputs, hash) {
			return a.host.service.Attachments(r), true, nil
		}
	}
	return nil, false, nil
}

func (a *delegationAssetLoader) OpenForGateway(ctx context.Context, bot, hash string) (io.ReadCloser, string, error) {
	set, result, err := a.shared(ctx, bot, hash)
	if err != nil {
		return nil, "", err
	}
	if set == nil {
		return a.base.OpenForGateway(ctx, bot, hash)
	}
	desc, err := set.Describe(ctx, hash, result)
	if err != nil {
		return nil, "", err
	}
	r, err := set.Open(ctx, hash, result)
	return r, desc.Mime, err
}

func (a *delegationAssetLoader) AccessPathForGateway(ctx context.Context, bot, hash string) (string, error) {
	set, _, err := a.shared(ctx, bot, hash)
	if err != nil {
		return "", err
	}
	if set != nil {
		return "", storage.ErrAccessPathUnavailable
	}
	return a.base.AccessPathForGateway(ctx, bot, hash)
}
