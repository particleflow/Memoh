package core

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/felinics/memoh/internal/agent/application"
	tools "github.com/felinics/memoh/internal/agent/tool"
	"github.com/felinics/memoh/internal/bots"
	thread "github.com/felinics/memoh/internal/chat/thread"
	"github.com/felinics/memoh/internal/db"
	dbsqlc "github.com/felinics/memoh/internal/db/postgres/sqlc"
	dbstore "github.com/felinics/memoh/internal/db/store"
	"github.com/felinics/memoh/internal/inputsource"
	"github.com/felinics/memoh/internal/oauthctx"
	"github.com/felinics/memoh/internal/runtimefence"
	"github.com/felinics/memoh/internal/toolcontext"
)

// nativeTaskQueueHost adapts existing source-run storage and tool queue ports.
// No standalone service, new runtime, or alternative scheduler is introduced.
type nativeTaskQueueHost struct{ host *delegationHost }

func (q *nativeTaskQueueHost) Lookup(ctx context.Context, s tools.SessionContext, call, fingerprint string) (dbstore.NativeTaskReceipt, bool, error) {
	var r dbstore.NativeTaskReceipt
	fence, ok := runtimefence.FromContext(ctx)
	if !ok || fence.BotID != s.BotID || fence.SessionID != s.SessionID || call == "" {
		return r, false, errors.New("native task requires its source run and tool call")
	}
	source, err := q.host.store.GetNativeTaskSource(ctx, dbsqlc.GetNativeTaskSourceParams{BotID: nativeTaskUUID(s.BotID), SessionID: nativeTaskUUID(s.SessionID), UserID: nativeTaskUUID(s.UserID), FencingToken: fence.Token})
	if err != nil {
		return r, false, err
	}
	id := uuid.NewSHA1(uuid.MustParse(source.RunID.String()), []byte("memoh/native-task/v1\x00"+call)).String()
	r, err = q.read(ctx, dbstore.NativeTaskReceipt{BotID: s.BotID, SourceRunID: source.RunID.String(), ID: id})
	if errors.Is(err, pgx.ErrNoRows) {
		return r, false, nil
	}
	if err != nil {
		return r, false, err
	}
	if r.CallFingerprint != fingerprint {
		return r, false, errors.New("native task tool call conflicts with the original request")
	}
	_, _, err = q.RestoreSession(ctx, r)
	return r, err == nil, err
}

// This allowlist deliberately omits runtime/session credentials and callbacks.
type nativeTaskParent struct {
	SessionType, CurrentPlatform, ReplyTarget, ConversationType                    string
	CurrentModelUUID, CurrentModelID, CurrentModelProvider, CurrentModelProviderID string
	ReasoningStoredEffort, ReasoningRequestedEffort                                string
	WorkspaceTargetID, WorkspaceTargetKind, WorkdirPath                            string
	Skills                                                                         map[string]tools.SkillDetail
}

func nativeTaskUUID(value string) pgtype.UUID { id, _ := db.ParseUUID(value); return id }
func decodeNativeTask(data []byte) (dbstore.NativeTaskReceipt, error) {
	var r dbstore.NativeTaskReceipt
	if json.Unmarshal(data, &r) != nil || r.Version != 1 || r.TaskID == "" || r.SourceRunID == "" || r.ID == "" {
		return r, errors.New("invalid native task receipt")
	}
	return r, nil
}

func (q *nativeTaskQueueHost) read(ctx context.Context, r dbstore.NativeTaskReceipt) (dbstore.NativeTaskReceipt, error) {
	raw, err := q.host.store.ReadNativeTask(ctx, dbsqlc.ReadNativeTaskParams{SourceRunID: nativeTaskUUID(r.SourceRunID), BotID: nativeTaskUUID(r.BotID), TaskKey: r.ID})
	if err != nil {
		return r, err
	}
	return decodeNativeTask(raw)
}

func (q *nativeTaskQueueHost) Enqueue(ctx context.Context, s tools.SessionContext, call, agent, child string, cfg thread.SubagentConfig, message, description string) (dbstore.NativeTaskReceipt, bool, error) {
	var r dbstore.NativeTaskReceipt
	fence, ok := runtimefence.FromContext(ctx)
	if !ok || fence.BotID != s.BotID || fence.SessionID != s.SessionID || strings.TrimSpace(call) == "" || s.UserID == "" {
		return r, false, errors.New("native task requires an authenticated source turn and tool call")
	}
	source, err := q.host.store.GetNativeTaskSource(ctx, dbsqlc.GetNativeTaskSourceParams{BotID: nativeTaskUUID(s.BotID), SessionID: nativeTaskUUID(s.SessionID), UserID: nativeTaskUUID(s.UserID), FencingToken: fence.Token})
	if err != nil {
		return r, false, err
	}
	id := uuid.NewSHA1(uuid.MustParse(source.RunID.String()), []byte("memoh/native-task/v1\x00"+call)).String()
	parent := nativeTaskParent{SessionType: s.SessionType, CurrentPlatform: s.CurrentPlatform, ReplyTarget: s.ReplyTarget, ConversationType: s.ConversationType, CurrentModelUUID: s.CurrentModelUUID, CurrentModelID: s.CurrentModelID, CurrentModelProvider: s.CurrentModelProvider, CurrentModelProviderID: s.CurrentModelProviderID, ReasoningStoredEffort: s.ReasoningStoredEffort, ReasoningRequestedEffort: s.ReasoningRequestedEffort, WorkspaceTargetID: s.WorkspaceTargetID, WorkspaceTargetKind: s.WorkspaceTargetKind, WorkdirPath: s.WorkdirPath, Skills: s.Skills}
	conf, err := json.Marshal(cfg)
	if err != nil {
		return r, false, err
	}
	snapshot, err := json.Marshal(parent)
	if err != nil {
		return r, false, err
	}
	r = dbstore.NativeTaskReceipt{Version: 1, ID: id, TaskID: "bg_" + strings.ReplaceAll(id, "-", ""), SourceRunID: source.RunID.String(), BotID: s.BotID, ParentSessionID: s.SessionID, UserID: s.UserID, SessionID: child, AgentID: agent, CallID: call, CallFingerprint: tools.NativeTaskCallFingerprint(ctx), State: "queued", Message: message, Description: description, Config: conf, Parent: snapshot}
	b, err := q.host.service.Authority.Source(ctx, r.UserID, r.BotID, r.ParentSessionID)
	if err != nil {
		return r, false, err
	}
	routing, err := q.host.SourceRoute(ctx, b)
	if err != nil {
		return r, false, err
	}
	r.ReturnRoute, _ = json.Marshal(routing)
	sourceSession, err := q.host.sessions.Get(ctx, r.ParentSessionID)
	if err != nil {
		return r, false, err
	}
	if routing.Channel == "web" && (sourceSession.RuntimeType == "" || sourceSession.RuntimeType == thread.RuntimeModel) {
		r.ResultDeliveryVersion = 1
	}
	// The source run/tool call, resolved original child and immutable request form
	// the retry boundary. State/claim/run observations are outside this hash.
	immutable, _ := json.Marshal(r)
	sum := sha256.Sum256(immutable)
	r.Fingerprint = hex.EncodeToString(sum[:])
	if _, _, err = q.RestoreSession(ctx, r); err != nil {
		return r, false, err
	}
	data, _ := json.Marshal(r)
	raw, err := q.host.store.EnqueueNativeTask(ctx, dbsqlc.EnqueueNativeTaskParams{TaskKey: r.ID, TaskData: data, SourceRunID: source.RunID, BotID: source.BotID, ParentSessionID: source.SessionID, FencingToken: fence.Token, UserID: nativeTaskUUID(s.UserID), ChildSessionID: nativeTaskUUID(child)})
	if err == nil {
		out, e := decodeNativeTask(raw)
		return out, true, e
	}
	// A transport error may conceal a committed enqueue. Read its deterministic
	// identity once; never mint a replacement task or dispatch on uncertainty.
	lookup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	old, readErr := q.read(lookup, r)
	if readErr != nil {
		return r, false, errors.Join(err, readErr)
	}
	if old.Fingerprint != r.Fingerprint {
		return old, false, errors.New("native task invocation conflicts with its original request")
	}
	return old, false, nil
}

func (q *nativeTaskQueueHost) RestoreSession(ctx context.Context, r dbstore.NativeTaskReceipt) (context.Context, tools.SessionContext, error) {
	var s tools.SessionContext
	if err := q.host.accounts.ValidateSession(ctx, r.UserID); err != nil {
		return ctx, s, err
	}
	allowed, err := q.host.permissions.HasBotPermission(ctx, r.BotID, r.UserID, bots.PermissionChat)
	if err != nil {
		return ctx, s, err
	}
	if !allowed {
		return ctx, s, errors.New("native task source access denied")
	}
	parent, err := q.host.sessions.Get(ctx, r.ParentSessionID)
	if err != nil {
		return ctx, s, err
	}
	child, err := q.host.sessions.Get(ctx, r.SessionID)
	if err != nil {
		return ctx, s, err
	}
	if parent.BotID != r.BotID || child.BotID != r.BotID || parent.TeamID != child.TeamID || parent.CreatedByUserID != r.UserID || child.CreatedByUserID != r.UserID || child.ParentThreadID != parent.ID || child.Type != thread.TypeSubagent {
		return ctx, s, errors.New("native task ancestry changed")
	}
	var p nativeTaskParent
	if err = json.Unmarshal(r.Parent, &p); err != nil {
		return ctx, s, err
	}
	scope, err := q.host.service.Authority.Source(ctx, r.UserID, r.BotID, r.ParentSessionID)
	if err != nil {
		return ctx, s, err
	}
	expected := p.WorkspaceTargetID
	if expected == "" {
		expected = "native"
	}
	if scope.SourceSelection != expected {
		return ctx, s, errors.New("native task workspace selection changed")
	}
	if expected != "native" {
		if _, bound := toolcontext.HostComputerBinding(ctx); !bound {
			return ctx, s, errors.New("native task computer binding requires live host authorization; queued receipt retained")
		}
		allowed, e := q.host.permissions.HasBotPermission(ctx, r.BotID, r.UserID, bots.PermissionWorkspaceExec)
		if e != nil {
			return ctx, s, e
		}
		if !allowed {
			return ctx, s, errors.New("native task workspace access denied")
		}
	}
	// Reuse the existing host's delegated-session capability checks. A recovered
	// task cannot inherit another account, Bot or current computer selection.
	ctx = oauthctx.WithUserID(ctx, r.UserID)
	ctx, _, err = q.host.prepare(ctx, application.ChatRequest{BotID: r.BotID, UserID: r.UserID, ThreadID: r.ParentSessionID, WorkspaceTargetID: p.WorkspaceTargetID})
	if err != nil {
		return ctx, s, err
	}
	s = tools.SessionContext{BotID: r.BotID, ChatID: r.BotID, SessionID: r.ParentSessionID, UserID: r.UserID, SessionType: p.SessionType, CurrentPlatform: p.CurrentPlatform, ReplyTarget: p.ReplyTarget, ConversationType: p.ConversationType, CurrentModelUUID: p.CurrentModelUUID, CurrentModelID: p.CurrentModelID, CurrentModelProvider: p.CurrentModelProvider, CurrentModelProviderID: p.CurrentModelProviderID, ReasoningStoredEffort: p.ReasoningStoredEffort, ReasoningRequestedEffort: p.ReasoningRequestedEffort, WorkspaceTargetID: p.WorkspaceTargetID, WorkspaceTargetKind: p.WorkspaceTargetKind, WorkdirPath: p.WorkdirPath, Skills: p.Skills}
	origin := inputsource.Source{Kind: inputsource.NativeTask, BotID: r.BotID, SessionID: r.SessionID, TaskKind: "native_task", TaskID: r.TaskID, OriginBotID: r.BotID, OriginSessionID: r.ParentSessionID, OriginRunID: r.SourceRunID}
	return inputsource.WithContext(ctx, origin), s, nil
}

func (q *nativeTaskQueueHost) Claim(ctx context.Context, r dbstore.NativeTaskReceipt) (dbstore.NativeTaskReceipt, bool, error) {
	if _, _, err := q.RestoreSession(ctx, r); err != nil {
		return r, false, err
	}
	if q.host.store.Pool() == nil {
		return r, false, errors.New("native task claim requires transactional storage")
	}
	tx, err := q.host.store.Pool().Begin(ctx)
	if err != nil {
		return r, false, err
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()
	query := q.host.store.SQLC().WithTx(tx)
	if _, err = query.LockBotForSessionRunClaim(ctx, nativeTaskUUID(r.BotID)); err != nil {
		return r, false, err
	}
	token := uuid.NewString()
	raw, err := query.ClaimNativeTask(ctx, dbsqlc.ClaimNativeTaskParams{SourceRunID: nativeTaskUUID(r.SourceRunID), BotID: nativeTaskUUID(r.BotID), TaskKey: r.ID, ChildSessionID: nativeTaskUUID(r.SessionID), ClaimToken: token})
	if errors.Is(err, pgx.ErrNoRows) {
		raw, e := query.ReadNativeTask(ctx, dbsqlc.ReadNativeTaskParams{SourceRunID: nativeTaskUUID(r.SourceRunID), BotID: nativeTaskUUID(r.BotID), TaskKey: r.ID})
		if e != nil {
			return r, false, e
		}
		old, e := decodeNativeTask(raw)
		return old, false, e
	}
	if err != nil {
		return r, false, err
	}
	claimed, err := decodeNativeTask(raw)
	if err != nil {
		return r, false, err
	}
	if err = tx.Commit(ctx); err != nil {
		return r, false, fmt.Errorf("native task claim outcome unknown: %w", err)
	}
	// Waiting for the Bot lock may outlive the first permission check. Recheck
	// after releasing the transaction, immediately before handing execution to
	// the native provider. A denied/uncertain claim is retained, never rerun.
	if _, _, err = q.RestoreSession(ctx, claimed); err != nil {
		return claimed, false, err
	}
	return claimed, true, nil
}

func (q *nativeTaskQueueHost) Finish(ctx context.Context, r dbstore.NativeTaskReceipt, run, state string) (string, error) {
	defer q.host.wakeResults(ctx)
	if r.ClaimToken == "" {
		return "unknown", errors.New("native task has no claim")
	}
	switch state {
	case "completed", "failed", "aborted", "unknown":
	default:
		return "unknown", errors.New("invalid native task terminal state")
	}
	// A runtime run is authoritative when present; a tool's narration is not.
	actual, err := q.host.store.GetSessionRunByInvocation(ctx, dbsqlc.GetSessionRunByInvocationParams{SessionID: nativeTaskUUID(r.SessionID), InvocationID: "subagent:" + r.TaskID})
	switch {
	case err == nil:
		if actual.BotID.String() != r.BotID || (run != "" && actual.RunID.String() != run) {
			return "unknown", errors.New("native task execution identity changed")
		}
		run = actual.RunID.String()
		state = actual.State
		if state != "completed" && state != "failed" && state != "aborted" {
			state = "unknown"
		}
	case !errors.Is(err, pgx.ErrNoRows):
		return state, err
	case run != "" || state == "completed":
		state = "unknown"
	}
	_, err = q.host.store.FinishNativeTask(ctx, dbsqlc.FinishNativeTaskParams{SourceRunID: nativeTaskUUID(r.SourceRunID), BotID: nativeTaskUUID(r.BotID), TaskKey: r.ID, ClaimToken: r.ClaimToken, TaskState: state, RunID: run})
	if !errors.Is(err, pgx.ErrNoRows) {
		return state, err
	}
	old, e := q.read(ctx, r)
	if e == nil && old.ClaimToken == r.ClaimToken && old.State == state && old.RunID == run {
		return state, nil
	}
	return "unknown", errors.Join(err, e)
}

func (q *nativeTaskQueueHost) Pending(ctx context.Context) ([]dbstore.NativeTaskReceipt, error) {
	// Read existing terminal executions before restoring successors. This only
	// reconciles the original claim; it never creates or reruns an execution.
	observed, err := q.host.store.ListClaimedNativeTasks(ctx)
	if err != nil {
		return nil, err
	}
	for _, data := range observed {
		r, err := decodeNativeTask(data)
		if err != nil {
			return nil, err
		}
		if _, err = q.Finish(ctx, r, "", "unknown"); err != nil {
			return nil, err
		}
	}
	rows, err := q.host.store.ListQueuedNativeTasks(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]dbstore.NativeTaskReceipt, 0, len(rows))
	for _, row := range rows {
		r, e := decodeNativeTask(row.TaskData)
		if e != nil {
			return nil, e
		}
		if r.SourceRunID != row.SourceRunID.String() || r.BotID != row.BotID.String() || r.ID != row.TaskKey {
			return nil, errors.New("native task source binding changed")
		}
		out = append(out, r)
	}
	return out, nil
}

func (q *nativeTaskQueueHost) Cancel(task string) error {
	defer q.host.wakeResults(context.Background())
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	data, err := q.host.store.FindNativeTask(ctx, task)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	r, err := decodeNativeTask(data)
	if err != nil {
		return err
	}
	if r.State != "queued" {
		return nil
	}
	_, err = q.host.store.CancelQueuedNativeTask(ctx, dbsqlc.CancelQueuedNativeTaskParams{SourceRunID: nativeTaskUUID(r.SourceRunID), BotID: nativeTaskUUID(r.BotID), TaskKey: r.ID})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	return err
}

func (q *nativeTaskQueueHost) Reject(ctx context.Context, r dbstore.NativeTaskReceipt) error {
	defer q.host.wakeResults(ctx)
	_, err := q.host.store.RejectQueuedNativeTask(ctx, dbsqlc.RejectQueuedNativeTaskParams{SourceRunID: nativeTaskUUID(r.SourceRunID), BotID: nativeTaskUUID(r.BotID), TaskKey: r.ID})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	return err
}
