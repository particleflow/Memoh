package tools

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/felinics/memoh/internal/agent/background"
	sessionpkg "github.com/felinics/memoh/internal/chat/thread"
	dbstore "github.com/felinics/memoh/internal/db/store"
)

// NativeTaskQueue is a host-owned persistence port at tool submission. The
// native Agent and its runtime admission/ownership algorithms are unchanged.
type NativeTaskQueue interface {
	Lookup(context.Context, SessionContext, string, string) (dbstore.NativeTaskReceipt, bool, error)
	Enqueue(context.Context, SessionContext, string, string, string, sessionpkg.SubagentConfig, string, string) (dbstore.NativeTaskReceipt, bool, error)
	Claim(context.Context, dbstore.NativeTaskReceipt) (dbstore.NativeTaskReceipt, bool, error)
	Finish(context.Context, dbstore.NativeTaskReceipt, string, string) (string, error)
	Reject(context.Context, dbstore.NativeTaskReceipt) error
	Pending(context.Context) ([]dbstore.NativeTaskReceipt, error)
	RestoreSession(context.Context, dbstore.NativeTaskReceipt) (context.Context, SessionContext, error)
}

type (
	nativeTaskCallKey struct{}
	nativeTaskCall    struct{ ID, Fingerprint string }
)

func withNativeTaskCall(ctx context.Context, id, name string, args any) context.Context {
	data, _ := json.Marshal(args)
	sum := sha256.Sum256(append([]byte(name+"\x00"), data...))
	return context.WithValue(ctx, nativeTaskCallKey{}, nativeTaskCall{id, hex.EncodeToString(sum[:])})
}

func NativeTaskCallFingerprint(ctx context.Context) string {
	v, _ := ctx.Value(nativeTaskCallKey{}).(nativeTaskCall)
	return v.Fingerprint
}

func (p *SpawnProvider) replayNativeTask(ctx context.Context, s SessionContext) (any, bool, error) {
	if p.taskQueue == nil {
		return nil, false, nil
	}
	call, _ := ctx.Value(nativeTaskCallKey{}).(nativeTaskCall)
	r, found, err := p.taskQueue.Lookup(ctx, s, call.ID, call.Fingerprint)
	if err != nil || !found {
		return nil, found, err
	}
	return nativeTaskResponse(r), true, nil
}

func (p *SpawnProvider) SetNativeTaskQueue(queue NativeTaskQueue) { p.taskQueue = queue }

func nativeTaskResponse(r dbstore.NativeTaskReceipt) map[string]any {
	return map[string]any{"task_id": r.TaskID, "agent_id": r.AgentID, "session_id": r.SessionID, "status": r.State, "description": r.Description}
}

// RecoverNativeTasks restores only requests that never obtained a durable claim.
// Claimed/unknown work is never automatically executed again after restart.
func (p *SpawnProvider) RecoverNativeTasks(ctx context.Context) error {
	if p.taskQueue == nil {
		return nil
	}
	rows, err := p.taskQueue.Pending(ctx)
	if err != nil {
		return err
	}
	var failures []error
	for _, r := range rows {
		runCtx, s, err := p.taskQueue.RestoreSession(ctx, r)
		if err != nil {
			failures = append(failures, err)
			continue
		}
		var config sessionpkg.SubagentConfig
		if err = json.Unmarshal(r.Config, &config); err != nil {
			failures = append(failures, err)
			continue
		}
		_, err = p.submitAgentTaskWithReceipt(runCtx, s, agentRecord{AgentID: r.AgentID, SessionID: r.SessionID}, config, r.Message, true, &r)
		if err != nil {
			failures = append(failures, err)
		}
	}
	return errors.Join(failures...)
}

func (p *SpawnProvider) startRecordedAgentTask(ctx context.Context, req *agentRequest, description string, queued bool) (string, context.Context, error) {
	if req.receipt == nil {
		return p.bgManager.StartAgentTask(ctx, req.parentSession.BotID, req.parentSession.SessionID, req.agentID, req.agentSessionID, req.message, description, queued)
	}
	return p.bgManager.RestoreAgentTask(ctx, req.receipt.TaskID, req.parentSession.BotID, req.parentSession.SessionID, req.agentID, req.agentSessionID, req.message, description, queued)
}

func (p *SpawnProvider) claimRecordedAgentTask(ctx context.Context, req *agentRequest) error {
	if req.receipt == nil {
		return nil
	}
	r, won, err := p.taskQueue.Claim(ctx, *req.receipt)
	if err != nil {
		return err
	}
	if !won {
		return fmt.Errorf("native task %s is not available for a new execution", req.taskID)
	}
	req.receipt = &r
	return nil
}

func (p *SpawnProvider) finishRecordedAgentTask(ctx context.Context, req *agentRequest, result *agentRunResult) {
	if req.receipt == nil || req.receipt.ClaimToken == "" {
		return
	}
	state := "completed"
	if result.Status == string(background.TaskKilled) {
		state = "aborted"
	} else if result.Error != "" {
		state = "failed"
	}
	state, err := p.taskQueue.Finish(context.WithoutCancel(ctx), *req.receipt, req.admission.RunID, state)
	switch {
	case err != nil || state == "unknown":
		result.Status = string(background.TaskUnknown)
		result.Error = "Task execution outcome could not be recorded; inspect the original execution before retrying."
		result.Text = ""
	case state == "aborted":
		result.Status = string(background.TaskKilled)
	case state == "failed":
		result.Status = string(background.TaskFailed)
		if result.Error == "" {
			result.Error = "Task execution failed"
		}
	}
}
