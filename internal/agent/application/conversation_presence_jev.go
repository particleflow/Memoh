package application

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/felinics/memoh/internal/conversationpresence"
	"github.com/felinics/memoh/internal/db/postgres/sqlc"
	"github.com/felinics/memoh/internal/jev"
)

// Fast-reply v3 Jev decision engine (design v3 §2-§4): the two-stage call
// loop, the pull mechanism, the per-phase time guards and the structural
// fallback live here. The engine runs inside the presence sidecar after
// mainWritten; Jev network calls never happen inside a DB transaction.

// Phase-entry guard thresholds in seconds (design v3 §4.4: loop 6.9,
// pull 5.9, forced 5.7, stage two 4.5, generation 3.3).
const (
	presenceJevGuardLoopSeconds     = 6.9
	presenceJevGuardPullSeconds     = 5.9
	presenceJevGuardForcedSeconds   = 5.7
	presenceJevGuardStageTwoSeconds = 4.5
	presenceJevGuardGenerateSeconds = 3.3
	// presenceFallbackGrace is the constant-ack publish grace after
	// mainWritten (design v3 §7). Deliberately a separate name from the
	// value-gate margin in the Q1 criteria even though both are 1.5s
	// (review R5 §3).
	presenceFallbackGrace = 1500 * time.Millisecond
	// presenceJevLoopCallLimit is the structural cap on stage-one loop calls
	// (initial + 4 re-asks, design v3 §4.3) inside a total of 7 calls.
	presenceJevLoopCallLimit = 5
	// presenceFallbackMinInputRunes is the §7 negative condition: a settled
	// follow-up at or below 20 runes is fast-main shape, no fallback ack.
	presenceFallbackMinInputRunes = 20
	presenceStateInputRunes       = 400
)

// Fast-reply final modes (accounting final_mode, design v3 §6.3).
const (
	presenceModeNoFastReply  = "no_fast_reply"
	presenceModeFastReply    = "fast_reply"
	presenceModeFallbackACK  = "fallback_ack"
	presenceModeFallbackNoop = "fallback_silent"
)

// Pull source ids (design v3 §4.1).
const (
	presencePullRecentTurns = "recent_turns"
	presencePullLastReply   = "last_reply"
	presencePullTaskState   = "task_state"
	presencePullFileList    = "file_list"
	presencePullSessionMeta = "session_meta"
	presencePullNone        = "none"
)

func jevNextSourceDescriptions() map[string]string {
	return map[string]string{
		presencePullRecentTurns: "最近对话正文",
		presencePullLastReply:   "上一条回复正文",
		presencePullTaskState:   "任务状态",
		presencePullFileList:    "文件清单",
		presencePullSessionMeta: "会话元信息",
		presencePullNone:        "没有再拉的价值，立即终判",
	}
}

func jevDirectionDescriptions() map[string]string {
	return map[string]string{
		"soothe":   "安抚：接住等待情绪，不做时间承诺",
		"progress": "进度：如实转述当前阶段",
		"eta":      "预告时长：只给系统档位",
		"clarify":  "反问澄清：只问最关键的一个问题",
		"brief":    "简答：一句话给出已拉证据内的答案要点",
	}
}

// PresenceJevRuntimeOptions carries the effective v3 configuration into the
// sidecar. It is the exported surface the host wiring fills from config.
type PresenceJevRuntimeOptions struct {
	Endpoint      string
	APIKey        string //nolint:gosec // config credential field, never logged or marshaled
	Model         string
	Timeout       time.Duration
	PullMax       int
	PullRunes     int
	StateMaxRunes int
	Directions    []string
}

// presenceJevInput is the per-message decision input.
type presenceJevInput struct {
	Text         string
	Facts        conversationpresence.Facts
	Reply        bool
	ImagePresent bool
	// Effort is the main model's thinking effort for this run ("" unknown).
	Effort string
	// AdmittedAt anchors wall_since_admission_ms.
	AdmittedAt time.Time
}

// Accounting detail records (design v3 §6.3).
type presenceStage1Attempt struct {
	Attempt     int     `json:"attempt"`
	Choice      string  `json:"choice"`
	Probability float64 `json:"probability"`
	NextSource  string  `json:"next_source,omitempty"`
	LatencyMs   int64   `json:"latency_ms"`
}

type presencePullRecord struct {
	Source    string `json:"source"`
	Chars     int    `json:"chars"`
	LatencyMs int64  `json:"latency_ms"`
	Truncated bool   `json:"truncated,omitempty"`
	Empty     bool   `json:"empty,omitempty"`
	Duplicate bool   `json:"duplicate,omitempty"`
}

type presenceStage2Record struct {
	Direction            string  `json:"direction"`
	DirectionProbability float64 `json:"direction_probability"`
	Urgency              int     `json:"urgency"`
	Value                float64 `json:"value"`
	Downgrade            string  `json:"downgrade,omitempty"`
}

type presenceJevUsage struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
}

type presenceDecisionTimings struct {
	AssembleMs int64 `json:"assemble_ms"`
	Stage1Ms   int64 `json:"stage1_ms"`
	PullMs     int64 `json:"pull_ms"`
	Stage2Ms   int64 `json:"stage2_ms"`
}

type presenceDecisionDetail struct {
	Stage1               []presenceStage1Attempt `json:"stage1,omitempty"`
	Forced               bool                    `json:"forced,omitempty"`
	Q1Attempts           int                     `json:"q1_attempts"`
	Stage2               *presenceStage2Record   `json:"stage2,omitempty"`
	Pulls                []presencePullRecord    `json:"pulls,omitempty"`
	Outcome              string                  `json:"outcome,omitempty"`
	WallSinceAdmissionMs int64                   `json:"wall_since_admission_ms"`
	Timings              presenceDecisionTimings `json:"timings"`
	Usage                *presenceJevUsage       `json:"usage,omitempty"`
	TruncatedState       bool                    `json:"truncated_state,omitempty"`
}

// presenceJevResult is the terminal decision of the Jev window.
type presenceJevResult struct {
	Mode           string
	Temperature    conversationpresence.Temperature
	Reason         string
	Direction      string
	Urgency        int
	FallbackReason string
	// Phase is the mapped run phase for the generation variable slot.
	Phase conversationpresence.Phase
	// ActiveRunState/Age back the eta band computation ("" when unknown).
	ActiveRunState string
	ActiveRunAge   int64
	// ContextPoints are the pulled evidence segments for the slot JSON.
	ContextPoints []string
	// RecentACKs are the last published acknowledgement texts (anti-template).
	RecentACKs []string
	Detail     presenceDecisionDetail
}

// Publishable reports whether this decision proceeds to generation/publish.
func (r *presenceJevResult) Publishable() bool {
	return r.Mode == presenceModeFastReply || r.Mode == presenceModeFallbackACK
}

// ETABand computes the fixed duration band (design v3 §5.2): soon for a young
// thinking run, tens of seconds for queued or 10-60s, a while beyond 60s.
func (r *presenceJevResult) ETABand() string {
	switch r.ActiveRunState {
	case "queued":
		return "几十秒"
	case "accepted", "running", "waiting_decision", "finishing":
	default:
		return ""
	}
	if r.ActiveRunAge < 10 {
		return "很快"
	}
	if r.ActiveRunAge <= 60 {
		return "几十秒"
	}
	return "可能要一会儿"
}

// presenceJevOutcome bundles the terminal decision with window cancellation.
type presenceJevOutcome struct {
	Result     *presenceJevResult
	Superseded bool
}

// presenceJevEngine drives one decision window.
type presenceJevEngine struct {
	client   *jev.Client
	pulls    presencePullReader
	settings PresenceJevRuntimeOptions
	in       presenceJevInput
	scope    presencePullScope
	logger   *slog.Logger
}

// presencePullScope addresses the pull queries.
type presencePullScope struct {
	SessionID pgtype.UUID
	RunID     pgtype.UUID
	TurnID    pgtype.UUID
	ActorID   string
}

// presencePullReader is the DB port for pull sources and assemble signals.
type presencePullReader interface {
	ReadPresenceDecisionContext(context.Context, sqlc.ReadPresenceDecisionContextParams) (sqlc.ReadPresenceDecisionContextRow, error)
	ReadPresenceRecentTurns(context.Context, sqlc.ReadPresenceRecentTurnsParams) ([]sqlc.ReadPresenceRecentTurnsRow, error)
	ReadPresenceLastReply(context.Context, sqlc.ReadPresenceLastReplyParams) (any, error)
	ReadPresenceTaskState(context.Context, sqlc.ReadPresenceTaskStateParams) (sqlc.ReadPresenceTaskStateRow, error)
	ReadPresenceFileList(context.Context, pgtype.UUID) (any, error)
}

// presenceJevSignals carries the assemble-time context read.
type presenceJevSignals struct {
	TurnMessages       int64
	AssistantMessages  int64
	HistoryChars       int64
	ActiveRunState     string
	ActiveRunAge       int64
	LastReplyTruncated bool
	PriorPresenceRows  int64
	RecentACKs         []string
	FileCount          int64
}

// decide runs the whole Jev window. ctx is the sidecar context; its
// cancellation means the main chain superseded this turn and no accounting
// row is written (only the log counters, design v3 §6.3).
func (e *presenceJevEngine) decide(ctx context.Context) presenceJevOutcome {
	windowEnd := time.Now().Add(e.settings.Timeout)
	if deadline, ok := ctx.Deadline(); ok && deadline.Before(windowEnd) {
		windowEnd = deadline
	}
	// The 10s window is a real deadline on every call, not just a guard
	// input (design v3 §4.4: the per-call context deadline is the segment's
	// remaining budget). The sidecar context keeps propagating cancellation;
	// a window that expires mid-call classifies as jev_timeout while a dead
	// sidecar context stays the superseded path.
	jevCtx, cancelWindow := context.WithDeadline(ctx, windowEnd)
	defer cancelWindow()
	detail := presenceDecisionDetail{}
	assembleStart := time.Now()
	signals, err := e.readSignals(jevCtx)
	if err != nil {
		if ctx.Err() != nil {
			return presenceJevOutcome{Superseded: true}
		}
		// Assemble reads are local DB reads; failure routes to the fallback.
		e.logFallback(jev.ReasonInvalidResponse, err)
		return presenceJevOutcome{Result: e.structuralFallback(jev.ReasonInvalidResponse, detail)}
	}
	detail.Timings.AssembleMs = time.Since(assembleStart).Milliseconds()

	loop := &presenceJevLoop{
		eng:       e,
		ctx:       ctx,
		jevCtx:    jevCtx,
		windowEnd: windowEnd,
		signals:   signals,
		detail:    &detail,
	}
	result := loop.run()
	if result == nil {
		return presenceJevOutcome{Superseded: true}
	}
	detail.WallSinceAdmissionMs = wallSince(e.in.AdmittedAt)
	result.Detail = detail
	return presenceJevOutcome{Result: result}
}

// presenceJevLoop is one window's mutable loop state.
type presenceJevLoop struct {
	eng *presenceJevEngine
	// ctx is the sidecar context: only its cancellation is superseded.
	ctx context.Context
	// jevCtx carries the 10s window deadline derived in decide; every Jev
	// call and pull runs inside it (design v3 §4.4).
	jevCtx    context.Context
	windowEnd time.Time
	signals   presenceJevSignals
	detail    *presenceDecisionDetail

	pulled        map[string]bool
	pullCount     int
	taskStatePull bool
	// lastDuplicate remembers the last duplicate marker to detect repeats.
	lastDuplicate string
	segments      []string
	recentCursor  *sqlc.ReadPresenceRecentTurnsParams
}

func (l *presenceJevLoop) remaining() time.Duration {
	left := time.Until(l.windowEnd)
	if deadline, ok := l.ctx.Deadline(); ok {
		if until := time.Until(deadline); until < left {
			left = until
		}
	}
	return left
}

func (l *presenceJevLoop) canAfford(seconds float64) bool {
	return l.remaining().Seconds() >= seconds
}

func (l *presenceJevLoop) run() *presenceJevResult {
	if l.pulled == nil {
		l.pulled = map[string]bool{}
	}
	for attempt := 0; attempt < presenceJevLoopCallLimit; attempt++ {
		if l.ctx.Err() != nil {
			return nil
		}
		if !l.canAfford(presenceJevGuardLoopSeconds) {
			return l.forcedFinal()
		}
		response, err := l.eng.client.Ask(l.jevCtx, l.buildState(false), jevStageOneQuestions(l.offeredSources()))
		if err != nil {
			return l.fallback(err)
		}
		l.detail.Timings.Stage1Ms += response.Latency.Milliseconds()
		l.recordUsage(response)
		choice, answer, ok := response.Choice(jevQReplyMode)
		if !ok {
			return l.fallback(&jev.Error{Kind: jev.ReasonInvalidResponse, Err: errors.New("reply_mode answer missing or invalid")})
		}
		attemptRecord := presenceStage1Attempt{Attempt: attempt, Choice: choice, Probability: confidence(answer), LatencyMs: response.Latency.Milliseconds()}
		l.detail.Stage1 = append(l.detail.Stage1, attemptRecord)
		l.detail.Q1Attempts++
		switch choice {
		case "no_fast_reply":
			return l.terminalNoFast()
		case "fast_reply":
			return l.stageTwo()
		case "need_context":
			next, nextAnswer, ok := response.Choice(jevQNextSource)
			if !ok {
				// Q1 valid + Q2 missing/invalid behaves as none (design §6.5).
				return l.forcedFinal()
			}
			attemptRecord.NextSource = next
			attemptRecord.Probability = confidence(nextAnswer)
			l.detail.Stage1[len(l.detail.Stage1)-1] = attemptRecord
			if next == presencePullNone || !l.sourceOffered(next) {
				return l.forcedFinal()
			}
			if l.pulled[next] && next != presencePullTaskState {
				if repeated := l.duplicateSource(next); repeated != nil {
					return repeated
				}
				continue
			}
			if l.pullCount >= l.eng.settings.PullMax || !l.canAfford(presenceJevGuardPullSeconds) {
				return l.forcedFinal()
			}
			l.pull(l.jevCtx, next)
		default:
			return l.fallback(&jev.Error{Kind: jev.ReasonInvalidResponse, Err: fmt.Errorf("reply_mode choice %q not offered", choice)})
		}
	}
	// Loop structural cap exhausted while still needing context (design v3
	// §4.3 termination ⑦, review R5-D): force the final judgment.
	return l.forcedFinal()
}

// duplicateSource handles a repeated non-repeatable source: the first repeat
// returns a marker segment without consuming pull quota (the re-ask itself
// consumes a loop call); a second repeat in a row forces the final judgment.
func (l *presenceJevLoop) duplicateSource(source string) *presenceJevResult {
	if l.lastDuplicate == source {
		return l.forcedFinal()
	}
	l.lastDuplicate = source
	l.detail.Pulls = append(l.detail.Pulls, presencePullRecord{Source: source, Duplicate: true})
	l.segments = append(l.segments, "（"+source+" 已拉取过，不会再提供新内容：请换一个源，或直接终判）")
	return nil
}

// pull fetches one evidence segment and records the trace. The single pull
// budget bounds one DB read (design v3 §4.4: ~50ms expected, 0.2s p95 guard).
func (l *presenceJevLoop) pull(ctx context.Context, source string) {
	l.lastDuplicate = ""
	pullStart := time.Now()
	pullCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	text, truncated := l.fetchSource(pullCtx, source)
	text = presenceStateLine(text)
	record := presencePullRecord{Source: source, Chars: runeLen(text), LatencyMs: time.Since(pullStart).Milliseconds(), Truncated: truncated, Empty: text == ""}
	l.detail.Pulls = append(l.detail.Pulls, record)
	l.detail.Timings.PullMs += record.LatencyMs
	l.pullCount++
	l.pulled[source] = true
	if text != "" {
		l.segments = append(l.segments, "第"+strconv.Itoa(l.pullCount)+"段·"+source+"："+text)
		if source == presencePullTaskState {
			l.taskStatePull = true
		}
	}
}

func (l *presenceJevLoop) fetchSource(ctx context.Context, source string) (string, bool) {
	switch source {
	case presencePullRecentTurns:
		rows, err := l.eng.pulls.ReadPresenceRecentTurns(ctx, sqlc.ReadPresenceRecentTurnsParams{SessionID: l.eng.scope.SessionID, TurnID: l.eng.scope.TurnID, CursorCreatedAt: l.recentCursorValue(), CursorID: l.recentCursorID()})
		if err != nil || len(rows) == 0 {
			return "", false
		}
		var builder []rune
		for i := len(rows) - 1; i >= 0; i-- {
			row := rows[i]
			prefix := "助手:"
			if row.Role == "user" {
				prefix = "用户:"
			}
			for _, char := range prefix + asString(row.Text) {
				if len(builder) >= l.eng.settings.PullRunes {
					l.advanceRecentCursor(rows, i)
					return string(builder), true
				}
				builder = append(builder, char)
			}
			if i == 0 {
				l.advanceRecentCursor(rows, 0)
			}
		}
		return string(builder), false
	case presencePullLastReply:
		text, err := l.eng.pulls.ReadPresenceLastReply(ctx, sqlc.ReadPresenceLastReplyParams{SessionID: l.eng.scope.SessionID, TurnID: l.eng.scope.TurnID})
		if err != nil {
			return "", false
		}
		return tailRunes(asString(text), l.eng.settings.PullRunes), false
	case presencePullTaskState:
		row, err := l.eng.pulls.ReadPresenceTaskState(ctx, sqlc.ReadPresenceTaskStateParams{SessionID: l.eng.scope.SessionID, RunID: l.eng.scope.RunID})
		if err != nil {
			return "", false
		}
		return formatPresenceTaskState(row), false
	case presencePullFileList:
		raw, err := l.eng.pulls.ReadPresenceFileList(ctx, l.eng.scope.SessionID)
		if err != nil {
			return "", false
		}
		return formatPresenceNameList(asBytes(raw)), false
	case presencePullSessionMeta:
		signals, err := l.eng.readSignals(ctx)
		if err != nil {
			return "", false
		}
		l.signals = signals
		return formatPresenceSessionMeta(signals), false
	default:
		return "", false
	}
}

func (l *presenceJevLoop) recentCursorValue() any {
	if l.recentCursor == nil {
		return nil
	}
	return l.recentCursor.CursorCreatedAt
}

func (l *presenceJevLoop) recentCursorID() pgtype.UUID {
	if l.recentCursor == nil {
		return pgtype.UUID{}
	}
	return l.recentCursor.CursorID
}

func (l *presenceJevLoop) advanceRecentCursor(rows []sqlc.ReadPresenceRecentTurnsRow, idx int) {
	oldest := rows[idx]
	l.recentCursor = &sqlc.ReadPresenceRecentTurnsParams{CursorCreatedAt: oldest.CreatedAt, CursorID: oldest.ID}
}

// stageTwo runs Q3+Q4 (one call, two questions) and applies the downgrade
// cascade (design v3 §3.3).
func (l *presenceJevLoop) stageTwo() *presenceJevResult {
	if l.ctx.Err() != nil {
		return nil
	}
	if !l.canAfford(presenceJevGuardStageTwoSeconds) {
		return l.fallbackBudget()
	}
	callStart := time.Now()
	response, err := l.eng.client.Ask(l.jevCtx, l.buildState(false), jevStageTwoQuestions(l.eng.settings.Directions))
	if err != nil {
		return l.fallback(err)
	}
	l.detail.Timings.Stage2Ms = time.Since(callStart).Milliseconds()
	l.recordUsage(response)
	direction, answer, ok := response.Choice(jevQDirection)
	if !ok {
		return l.fallback(&jev.Error{Kind: jev.ReasonInvalidResponse, Err: errors.New("direction answer missing or invalid")})
	}
	if !l.directionOffered(direction) {
		// Q3 answer outside the offered option set (design v3 §6.5): the §7
		// structural fallback, never an accounting insert that would trip
		// the migration's direction CHECK and roll back step2.
		return l.fallback(&jev.Error{Kind: jev.ReasonInvalidResponse, Err: fmt.Errorf("direction choice %q not offered", direction)})
	}
	rawUrgency, _, urgencyOK := response.Score(jevQUrgency)
	urgency := clampUrgency(rawUrgency, urgencyOK)
	stage2 := &presenceStage2Record{Direction: direction, DirectionProbability: confidence(answer), Urgency: urgency, Value: rawUrgency}
	l.detail.Stage2 = stage2
	final, downgrade := l.applyDowngradeCascade(direction)
	stage2.Downgrade = downgrade
	return &presenceJevResult{
		Mode:           presenceModeFastReply,
		Temperature:    conversationpresence.Cold,
		Reason:         "jev_fast_reply",
		Direction:      final,
		Urgency:        urgency,
		Phase:          l.mappedPhase(),
		ActiveRunState: l.signals.ActiveRunState,
		ActiveRunAge:   l.signals.ActiveRunAge,
		ContextPoints:  append([]string(nil), l.segments...),
		RecentACKs:     append([]string(nil), l.signals.RecentACKs...),
	}
}

// applyDowngradeCascade evaluates the server-side preconditions once
// (design v3 §3.3): progress needs task_state pulled this round with a
// mappable phase; eta needs a computable band; brief needs complete evidence
// (off by default) and cascades through progress. soothe is the terminus.
func (l *presenceJevLoop) applyDowngradeCascade(direction string) (string, string) {
	progressOK := l.taskStatePull && l.signals.ActiveRunState != ""
	switch direction {
	case "progress":
		if progressOK {
			return "progress", ""
		}
		return "soothe", "progress→soothe"
	case "eta":
		if l.signals.ActiveRunState != "" {
			return "eta", ""
		}
		return "soothe", "eta→soothe"
	case "brief":
		if progressOK {
			return "progress", "brief→progress"
		}
		return "soothe", "brief→soothe"
	default:
		return direction, ""
	}
}

// mappedPhase maps run state onto the generation phase vocabulary
// (design v3 §5.2: accepted, queued, running→thinking,
// waiting_decision→waiting_user, finishing→finishing; "" when unknown).
func (l *presenceJevLoop) mappedPhase() conversationpresence.Phase {
	switch l.signals.ActiveRunState {
	case "accepted":
		return conversationpresence.Accepted
	case "queued":
		return conversationpresence.Queued
	case "running":
		return conversationpresence.Thinking
	case "waiting_decision":
		return conversationpresence.WaitingUser
	case "finishing":
		return conversationpresence.Finishing
	default:
		return ""
	}
}

// forcedFinal runs Q1b (two options, last-chance state). Guards below 5.7s
// or an unusable answer fall to the structural fallback (design v3 §4.3/§4.4).
func (l *presenceJevLoop) forcedFinal() *presenceJevResult {
	if l.ctx.Err() != nil {
		return nil
	}
	l.detail.Forced = true
	if !l.canAfford(presenceJevGuardForcedSeconds) {
		return l.fallbackBudget()
	}
	response, err := l.eng.client.Ask(l.jevCtx, l.buildState(true), map[string]jev.Question{jevQReplyMode: jevForcedQuestion()})
	if err != nil {
		return l.fallback(err)
	}
	l.detail.Timings.Stage1Ms += response.Latency.Milliseconds()
	l.recordUsage(response)
	choice, answer, ok := response.Choice(jevQReplyMode)
	if !ok {
		return l.fallback(&jev.Error{Kind: jev.ReasonInvalidResponse, Err: errors.New("forced reply_mode answer missing or invalid")})
	}
	l.detail.Stage1 = append(l.detail.Stage1, presenceStage1Attempt{Attempt: l.detail.Q1Attempts, Choice: choice, Probability: confidence(answer), LatencyMs: response.Latency.Milliseconds()})
	switch choice {
	case "fast_reply":
		return l.stageTwo()
	case "no_fast_reply":
		return l.terminalNoFast()
	default:
		// Q1b is a two-option question (design v3 §3.1): an answer outside
		// {no_fast_reply, fast_reply} is invalid and takes the conservative
		// §7 fallback, not a silent no_fast_reply.
		return l.fallback(&jev.Error{Kind: jev.ReasonInvalidResponse, Err: fmt.Errorf("forced reply_mode choice %q not offered", choice)})
	}
}

// directionOffered reports whether the direction is one of the options this
// run actually offered in Q3 (the configured directions that have a body).
func (l *presenceJevLoop) directionOffered(direction string) bool {
	if !slices.Contains(l.eng.settings.Directions, direction) {
		return false
	}
	_, described := jevDirectionDescriptions()[direction]
	return described
}

func (*presenceJevLoop) terminalNoFast() *presenceJevResult {
	return &presenceJevResult{
		Mode:        presenceModeNoFastReply,
		Temperature: conversationpresence.Hot,
		Reason:      "jev_no_fast_reply",
	}
}

func (l *presenceJevLoop) fallback(err error) *presenceJevResult {
	if l.ctx.Err() != nil {
		// The sidecar context died mid-call: the main chain superseded this
		// turn. No fallback decision and no accounting row (design §4.3).
		return nil
	}
	kind := jev.ReasonInvalidResponse
	var jevErr *jev.Error
	if errors.As(err, &jevErr) {
		kind = jevErr.Kind
	}
	l.eng.logFallback(kind, err)
	detail := *l.detail
	return l.eng.structuralFallback(kind, detail)
}

func (l *presenceJevLoop) fallbackBudget() *presenceJevResult {
	detail := *l.detail
	return l.eng.structuralFallback("budget_exhausted", detail)
}

// structuralFallback is the §7 constant path: a settled follow-up shape sends
// the fixed acknowledgement, everything else stays silent.
func (e *presenceJevEngine) structuralFallback(reason string, detail presenceDecisionDetail) *presenceJevResult {
	facts := e.in.Facts
	ack := facts.PreviousSettled && !facts.FirstDirectInput && !facts.PendingUserInput && !facts.ReplyContinuation && facts.HistoryAvailable && runeLen(e.in.Text) > presenceFallbackMinInputRunes
	mode := presenceModeFallbackNoop
	temperature := conversationpresence.Hot
	if ack {
		mode = presenceModeFallbackACK
		temperature = conversationpresence.Cold
	}
	detail.WallSinceAdmissionMs = wallSince(e.in.AdmittedAt)
	return &presenceJevResult{
		Mode:           mode,
		Temperature:    temperature,
		Reason:         "fallback_" + reason,
		FallbackReason: reason,
		Phase:          conversationpresence.Thinking,
		Detail:         detail,
	}
}

// buildState renders the decision state text (design v3 §4.3) with the
// state_max_runes clipping order: oldest pulled segment first, then the input
// down to 300 runes, then the token estimate to "长".
func (l *presenceJevLoop) buildState(lastChance bool) string {
	fullInput := l.eng.in.Text
	truncatedInput := runeLen(fullInput) > presenceStateInputRunes
	input := truncateRunes(fullInput, presenceStateInputRunes)
	tokenEstimate := l.signals.HistoryChars / 4
	state := l.renderState(input, truncatedInput, tokenEstimate, lastChance)
	if runeLen(state) <= l.eng.settings.StateMaxRunes {
		return state
	}
	l.detail.TruncatedState = true
	for len(l.segments) > 0 && runeLen(state) > l.eng.settings.StateMaxRunes {
		l.segments = l.segments[1:]
		state = l.renderState(input, truncatedInput, tokenEstimate, lastChance)
	}
	if runeLen(state) > l.eng.settings.StateMaxRunes {
		input = truncateRunes(fullInput, 300)
		state = l.renderState(input, truncatedInput, tokenEstimate, lastChance)
	}
	if runeLen(state) > l.eng.settings.StateMaxRunes {
		state = l.renderState(input, truncatedInput, -1, lastChance)
	}
	return state
}

func (l *presenceJevLoop) renderState(input string, truncatedInput bool, tokenEstimate int64, lastChance bool) string {
	facts := l.eng.in.Facts
	activeTask := "无"
	if l.signals.ActiveRunState != "" {
		activeTask = l.signals.ActiveRunState + "+" + strconv.FormatInt(max64(l.signals.ActiveRunAge, 0), 10) + "s"
	}
	token := strconv.FormatInt(tokenEstimate, 10)
	if tokenEstimate < 0 {
		token = "长"
	}
	pulled := "（无）"
	if len(l.segments) > 0 {
		pulled = strings.Join(l.segments, " ")
	}
	return "【输入】" + presenceStateLine(input) +
		"\n【信号】上一轮已结算=" + yesNo(facts.PreviousSettled) +
		"；进行中任务=" + activeTask +
		"；pending提问=" + hasNo(facts.PendingUserInput) +
		"；首条输入=" + yesNo(facts.FirstDirectInput) +
		"；引用回复=" + yesNo(l.eng.in.Reply) +
		"；主链思考档位=" + presenceEffortLabel(l.eng.in.Effort) +
		"；带图片=" + yesNo(l.eng.in.ImagePresent) +
		"；历史可用=" + yesNo(facts.HistoryAvailable) +
		"；上一轮因长度截断=" + yesNo(l.signals.LastReplyTruncated) +
		"；上下文token估算=" + token +
		"；输入总长=" + strconv.Itoa(runeLen(l.eng.in.Text)) + " 字" +
		"；输入被截断=" + yesNo(truncatedInput) +
		"；此前快回=" + strconv.FormatInt(l.signals.PriorPresenceRows, 10) + " 次" +
		"\n【已拉取】" + pulled +
		"\n【约束】剩余拉取次数=" + strconv.Itoa(maxInt(l.eng.settings.PullMax-l.pullCount, 0)) +
		"；最后判定机会=" + yesNo(lastChance)
}

// offeredSources lists the Q2 options actually available in this session
// (design v3 §4.1: absent sources are not offered; none is always offered).
func (l *presenceJevLoop) offeredSources() []string {
	sources := []string{}
	if l.signals.TurnMessages > 0 {
		sources = append(sources, presencePullRecentTurns)
	}
	if l.signals.AssistantMessages > 0 {
		sources = append(sources, presencePullLastReply)
	}
	if l.signals.ActiveRunState != "" {
		sources = append(sources, presencePullTaskState)
	}
	if l.signals.FileCount > 0 {
		sources = append(sources, presencePullFileList)
	}
	if l.signals.TurnMessages > 0 || l.signals.PriorPresenceRows > 0 {
		sources = append(sources, presencePullSessionMeta)
	}
	return append(sources, presencePullNone)
}

func (l *presenceJevLoop) sourceOffered(source string) bool {
	for _, offered := range l.offeredSources() {
		if offered == source {
			return true
		}
	}
	return false
}

// readSignals performs the assemble-time context read (one query).
func (e *presenceJevEngine) readSignals(ctx context.Context) (presenceJevSignals, error) {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	row, err := e.pulls.ReadPresenceDecisionContext(ctx, sqlc.ReadPresenceDecisionContextParams{SessionID: e.scope.SessionID, TurnID: e.scope.TurnID, RunID: e.scope.RunID})
	if err != nil {
		return presenceJevSignals{}, err
	}
	signals := presenceJevSignals{
		TurnMessages:       row.TurnMessages,
		AssistantMessages:  row.AssistantMessages,
		HistoryChars:       asInt64(row.HistoryChars),
		ActiveRunState:     asString(row.ActiveRunState),
		ActiveRunAge:       asInt64(row.ActiveRunAgeSeconds),
		LastReplyTruncated: asBool(row.LastReplyTruncated),
		PriorPresenceRows:  row.PriorPresenceRows,
		RecentACKs:         parseAckTexts(row.RecentAckTexts),
		FileCount:          asInt64(row.FileCount),
	}
	if signals.ActiveRunAge < 0 {
		signals.ActiveRunAge = 0
	}
	return signals, nil
}

func (e *presenceJevEngine) logFallback(kind string, err error) {
	if e.logger != nil {
		e.logger.Warn("conversation_presence_jev_fallback", slog.String("reason", kind), slog.String("error", errString(err)))
	}
}

func (l *presenceJevLoop) recordUsage(response jev.Response) {
	if response.Usage.InputTokens == 0 && response.Usage.OutputTokens == 0 {
		return
	}
	if l.detail.Usage == nil {
		l.detail.Usage = &presenceJevUsage{}
	}
	l.detail.Usage.InputTokens += response.Usage.InputTokens
	l.detail.Usage.OutputTokens += response.Usage.OutputTokens
}

func confidence(answer jev.Answer) float64 {
	if answer.Confidence != nil {
		return *answer.Confidence
	}
	return 0
}

// clampUrgency maps the zero-based wire score to the 1-3 accounting surface,
// clamping out-of-range and NaN values (design v3 §3.4).
func clampUrgency(raw float64, ok bool) int {
	if !ok || raw != raw {
		return 2
	}
	level := int(raw+0.5) + 1
	if level < 1 {
		return 1
	}
	if level > 3 {
		return 3
	}
	return level
}

func presenceEffortLabel(effort string) string {
	if effort == "" {
		return "未知"
	}
	return effort
}

func wallSince(admitted time.Time) int64 {
	if admitted.IsZero() {
		return 0
	}
	elapsed := time.Since(admitted).Milliseconds()
	if elapsed < 0 {
		return 0
	}
	return elapsed
}

func errString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

func yesNo(b bool) string {
	if b {
		return "是"
	}
	return "否"
}

// presenceStateLine flattens text that goes into the decision state: every
// line-break rune collapses to a space, one for one. User-controlled content
// (the 【输入】 segment and pulled turns) then cannot start a forged
// 【信号】/【约束】 line — the state format is line-oriented and those headers
// only matter at line starts (review P2-13).
func presenceStateLine(text string) string {
	return strings.Map(func(r rune) rune {
		if strings.ContainsRune("\n\r\v\f\u0085\u2028\u2029", r) {
			return ' '
		}
		return r
	}, text)
}

func hasNo(b bool) string {
	if b {
		return "有"
	}
	return "无"
}

func runeLen(text string) int {
	count := 0
	for range text {
		count++
	}
	return count
}

func tailRunes(text string, limit int) string {
	runes := []rune(text)
	if len(runes) <= limit {
		return text
	}
	return string(runes[len(runes)-limit:])
}

func max64(a, b int64) int64 {
	if a > b {
		return a
	}
	return b
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func asString(v any) string {
	switch value := v.(type) {
	case string:
		return value
	case []byte:
		return string(value)
	case nil:
		return ""
	default:
		return fmt.Sprint(value)
	}
}

func asBytes(v any) []byte {
	switch value := v.(type) {
	case []byte:
		return value
	case string:
		return []byte(value)
	default:
		return nil
	}
}

func asInt64(v any) int64 {
	switch value := v.(type) {
	case int64:
		return value
	case int32:
		return int64(value)
	case int:
		return int64(value)
	case float64:
		return int64(value)
	default:
		return 0
	}
}

func asBool(v any) bool {
	value, _ := v.(bool)
	return value
}

func parseAckTexts(v any) []string {
	var texts []string
	if err := json.Unmarshal(asBytes(v), &texts); err != nil {
		return nil
	}
	return texts
}

func parseStringList(v any) []string {
	var list []string
	if err := json.Unmarshal(asBytes(v), &list); err != nil {
		return nil
	}
	return list
}

func formatPresenceTaskState(row sqlc.ReadPresenceTaskStateRow) string {
	var runs []struct {
		State      string `json:"state"`
		AgeSeconds int64  `json:"age_seconds"`
	}
	_ = json.Unmarshal(asBytes(row.Runs), &runs)
	var builder strings.Builder
	builder.WriteString("runs:")
	if len(runs) == 0 {
		builder.WriteString("无进行中run")
	}
	for _, run := range runs {
		builder.WriteString(run.State + "+" + strconv.FormatInt(run.AgeSeconds, 10) + "s;")
	}
	if pending := parseStringList(row.PendingUserInput); len(pending) > 0 {
		builder.WriteString("pending提问=" + strconv.Itoa(len(pending)) + "项;")
	}
	if approvals := parseStringList(row.PendingApprovals); len(approvals) > 0 {
		builder.WriteString("pending审批=" + strings.Join(approvals, ",") + ";")
	}
	return builder.String()
}

func formatPresenceNameList(raw []byte) string {
	var entries []struct {
		Name string `json:"name"`
		Type string `json:"type"`
	}
	if err := json.Unmarshal(raw, &entries); err != nil || len(entries) == 0 {
		return "无附件"
	}
	parts := make([]string, 0, len(entries))
	for _, item := range entries {
		parts = append(parts, item.Name+"("+item.Type+")")
	}
	return strings.Join(parts, ", ")
}

func formatPresenceSessionMeta(signals presenceJevSignals) string {
	return "轮次数=" + strconv.FormatInt(signals.TurnMessages, 10) +
		"；历史字符数=" + strconv.FormatInt(signals.HistoryChars, 10) +
		"；此前presence行=" + strconv.FormatInt(signals.PriorPresenceRows, 10) +
		"；此前已发布ack=" + strconv.Itoa(len(signals.RecentACKs))
}
