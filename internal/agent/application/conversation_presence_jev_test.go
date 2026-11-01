package application

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/felinics/memoh/internal/conversationpresence"
	"github.com/felinics/memoh/internal/db/postgres/sqlc"
	"github.com/felinics/memoh/internal/jev"
)

// fakePresencePulls is an in-memory presencePullReader.
type fakePresencePulls struct {
	signals   presenceJevSignals
	turns     []sqlc.ReadPresenceRecentTurnsRow
	lastReply string
	taskState sqlc.ReadPresenceTaskStateRow
	files     []byte
	calls     map[string]int
}

func newFakePresencePulls() *fakePresencePulls {
	return &fakePresencePulls{calls: map[string]int{}}
}

func (f *fakePresencePulls) ReadPresenceDecisionContext(context.Context, sqlc.ReadPresenceDecisionContextParams) (sqlc.ReadPresenceDecisionContextRow, error) {
	f.calls["context"]++
	return sqlc.ReadPresenceDecisionContextRow{
		TurnMessages:        f.signals.TurnMessages,
		AssistantMessages:   f.signals.AssistantMessages,
		HistoryChars:        f.signals.HistoryChars,
		ActiveRunState:      f.signals.ActiveRunState,
		ActiveRunAgeSeconds: f.signals.ActiveRunAge,
		LastReplyTruncated:  f.signals.LastReplyTruncated,
		PriorPresenceRows:   f.signals.PriorPresenceRows,
		RecentAckTexts:      mustJSON(f.signals.RecentACKs),
		FileCount:           f.signals.FileCount,
	}, nil
}

func (f *fakePresencePulls) ReadPresenceRecentTurns(_ context.Context, _ sqlc.ReadPresenceRecentTurnsParams) ([]sqlc.ReadPresenceRecentTurnsRow, error) {
	f.calls["recent_turns"]++
	return f.turns, nil
}

func (f *fakePresencePulls) ReadPresenceLastReply(context.Context, sqlc.ReadPresenceLastReplyParams) (any, error) {
	f.calls["last_reply"]++
	return f.lastReply, nil
}

func (f *fakePresencePulls) ReadPresenceTaskState(context.Context, sqlc.ReadPresenceTaskStateParams) (sqlc.ReadPresenceTaskStateRow, error) {
	f.calls["task_state"]++
	return f.taskState, nil
}

func (f *fakePresencePulls) ReadPresenceFileList(context.Context, pgtype.UUID) (any, error) {
	f.calls["file_list"]++
	return f.files, nil
}

func mustJSON(v any) []byte {
	raw, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	if string(raw) == "null" {
		return []byte("[]")
	}
	return raw
}

// fakeJevGateway scripts answers by which question set the request carries.
type fakeJevGateway struct {
	server      *httptest.Server
	stage1      func(attempt int) map[string]any
	forced      map[string]any
	stage2      map[string]any
	stage1Calls atomic.Int32
	forcedCalls atomic.Int32
	stage2Calls atomic.Int32
}

func newFakeJevGateway(t *testing.T, stage1 func(attempt int) map[string]any, forced, stage2 map[string]any) *fakeJevGateway {
	t.Helper()
	gateway := &fakeJevGateway{stage1: stage1, forced: forced, stage2: stage2}
	gateway.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Questions map[string]json.RawMessage `json:"questions"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode: %v", err)
			http.Error(w, "bad", http.StatusBadRequest)
			return
		}
		var answers map[string]any
		switch {
		case body.Questions["direction"] != nil:
			answers = gateway.stage2
			gateway.stage2Calls.Add(1)
		case body.Questions["next_source"] != nil:
			answers = gateway.stage1(int(gateway.stage1Calls.Add(1)) - 1)
		default:
			answers = gateway.forced
			gateway.forcedCalls.Add(1)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"model": "jev-test", "answers": answers, "usage": map[string]int{"input_tokens": 100, "output_tokens": 5}})
	}))
	t.Cleanup(gateway.server.Close)
	return gateway
}

func answerChoice(id, choice string, confidence float64) map[string]any {
	return map[string]any{"id": id, "type": "choice", "choice": choice, "confidence": confidence}
}

func newTestEngine(gatewayURL string, pulls presencePullReader, timeout time.Duration, facts conversationpresence.Facts, text string) *presenceJevEngine {
	return &presenceJevEngine{
		client: &jev.Client{Endpoint: gatewayURL, APIKey: "test", Model: "jev-latest"},
		pulls:  pulls,
		settings: PresenceJevRuntimeOptions{
			Endpoint: gatewayURL, Model: "jev-latest", Timeout: timeout,
			PullMax: 4, PullRunes: 500, StateMaxRunes: 3200,
			Directions: []string{"soothe", "progress", "eta", "clarify"},
		},
		in:     presenceJevInput{Text: text, Facts: facts, AdmittedAt: time.Now().Add(-2 * time.Second)},
		logger: nil,
	}
}

func TestPresenceJevStraightFastReply(t *testing.T) {
	pulls := newFakePresencePulls()
	pulls.signals = presenceJevSignals{TurnMessages: 3, AssistantMessages: 2, HistoryChars: 1200, ActiveRunState: "", PriorPresenceRows: 1, RecentACKs: []string{"收到，在看。"}}
	gateway := newFakeJevGateway(t,
		func(int) map[string]any {
			return map[string]any{"reply_mode": answerChoice("reply_mode", "fast_reply", 0.9)}
		},
		nil,
		map[string]any{"direction": answerChoice("direction", "soothe", 0.8), "urgency": map[string]any{"type": "score", "score": 1.4, "confidence": 0.7}},
	)
	engine := newTestEngine(gateway.server.URL, pulls, 10*time.Second, conversationpresence.Facts{HistoryAvailable: true}, "帮我把这三份对比一下给个推荐")
	outcome := engine.decide(context.Background())
	if outcome.Superseded || outcome.Result == nil {
		t.Fatalf("outcome %+v", outcome)
	}
	result := outcome.Result
	if result.Mode != presenceModeFastReply || result.Temperature != conversationpresence.Cold {
		t.Fatalf("mode=%s temperature=%s", result.Mode, result.Temperature)
	}
	if result.Direction != "soothe" || result.Urgency != 2 {
		t.Fatalf("direction=%s urgency=%d", result.Direction, result.Urgency)
	}
	if result.Detail.Stage2 == nil || result.Detail.Stage2.Value != 1.4 {
		t.Fatalf("stage2 %+v", result.Detail.Stage2)
	}
	if result.Detail.Q1Attempts != 1 || len(result.Detail.Stage1) != 1 {
		t.Fatalf("attempts %+v", result.Detail.Stage1)
	}
	if result.Detail.Usage == nil || result.Detail.Usage.InputTokens != 200 {
		t.Fatalf("usage aggregated across both calls: %+v", result.Detail.Usage)
	}
	if result.ETABand() != "" {
		t.Fatalf("eta band without active run: %q", result.ETABand())
	}
}

func TestPresenceJevNoFastReplyStaysHot(t *testing.T) {
	gateway := newFakeJevGateway(t, func(int) map[string]any {
		return map[string]any{"reply_mode": answerChoice("reply_mode", "no_fast_reply", 0.95)}
	}, nil, nil)
	engine := newTestEngine(gateway.server.URL, newFakePresencePulls(), 10*time.Second, conversationpresence.Facts{}, "好的")
	result := engine.decide(context.Background()).Result
	if result == nil || result.Mode != presenceModeNoFastReply || result.Temperature != conversationpresence.Hot {
		t.Fatalf("result %+v", result)
	}
	if result.Publishable() {
		t.Fatal("no_fast_reply must not publish")
	}
}

func TestPresenceJevPullLoopAndDowngradeCascade(t *testing.T) {
	pulls := newFakePresencePulls()
	pulls.signals = presenceJevSignals{TurnMessages: 5, AssistantMessages: 4, HistoryChars: 800}
	pulls.taskState = sqlc.ReadPresenceTaskStateRow{Runs: []byte(`[{"state":"finishing","age_seconds":95}]`), PendingUserInput: []byte(`[]`), PendingApprovals: []byte(`[]`)}
	gateway := newFakeJevGateway(t,
		func(attempt int) map[string]any {
			if attempt == 0 {
				return map[string]any{"reply_mode": answerChoice("reply_mode", "need_context", 0.6), "next_source": answerChoice("next_source", "recent_turns", 0.7)}
			}
			return map[string]any{"reply_mode": answerChoice("reply_mode", "fast_reply", 0.8)}
		},
		nil,
		map[string]any{"direction": answerChoice("direction", "progress", 0.75), "urgency": map[string]any{"type": "score", "score": 0.2}},
	)
	engine := newTestEngine(gateway.server.URL, pulls, 10*time.Second, conversationpresence.Facts{HistoryAvailable: true}, "上一轮说的那个方案后来怎么样了")
	result := engine.decide(context.Background()).Result
	if result == nil {
		t.Fatal("no result")
	}
	if pulls.calls["recent_turns"] != 1 {
		t.Fatalf("recent_turns pulls=%d", pulls.calls["recent_turns"])
	}
	// progress was requested but task_state was never pulled this round →
	// cascade downgrades to soothe and records the trail (design §3.3).
	if result.Direction != "soothe" || result.Detail.Stage2.Downgrade != "progress→soothe" {
		t.Fatalf("direction=%s downgrade=%q", result.Direction, result.Detail.Stage2.Downgrade)
	}
	if result.Detail.Q1Attempts != 2 || len(result.Detail.Pulls) != 1 {
		t.Fatalf("attempts=%d pulls=%+v", result.Detail.Q1Attempts, result.Detail.Pulls)
	}
}

func TestPresenceJevTaskStatePullKeepsProgress(t *testing.T) {
	pulls := newFakePresencePulls()
	pulls.signals = presenceJevSignals{TurnMessages: 5, AssistantMessages: 4, ActiveRunState: "finishing", ActiveRunAge: 95}
	pulls.taskState = sqlc.ReadPresenceTaskStateRow{Runs: []byte(`[{"state":"finishing","age_seconds":95}]`)}
	gateway := newFakeJevGateway(t,
		func(attempt int) map[string]any {
			if attempt == 0 {
				return map[string]any{"reply_mode": answerChoice("reply_mode", "need_context", 0.6), "next_source": answerChoice("next_source", "task_state", 0.7)}
			}
			return map[string]any{"reply_mode": answerChoice("reply_mode", "fast_reply", 0.8)}
		},
		nil,
		map[string]any{"direction": answerChoice("direction", "progress", 0.75)},
	)
	engine := newTestEngine(gateway.server.URL, pulls, 10*time.Second, conversationpresence.Facts{}, "怎么样了")
	result := engine.decide(context.Background()).Result
	if result.Direction != "progress" || result.Detail.Stage2.Downgrade != "" {
		t.Fatalf("direction=%s downgrade=%q", result.Direction, result.Detail.Stage2.Downgrade)
	}
	if result.Phase != conversationpresence.Finishing {
		t.Fatalf("phase=%s", result.Phase)
	}
	if band := result.ETABand(); band != "可能要一会儿" {
		t.Fatalf("eta band %q", band)
	}
}

func TestPresenceJevForcedFinalOnNone(t *testing.T) {
	gateway := newFakeJevGateway(t,
		func(int) map[string]any {
			return map[string]any{"reply_mode": answerChoice("reply_mode", "need_context", 0.55), "next_source": answerChoice("next_source", "none", 0.9)}
		},
		map[string]any{"reply_mode": answerChoice("reply_mode", "no_fast_reply", 0.8)},
		nil,
	)
	engine := newTestEngine(gateway.server.URL, newFakePresencePulls(), 10*time.Second, conversationpresence.Facts{}, "测试")
	result := engine.decide(context.Background()).Result
	if result.Mode != presenceModeNoFastReply {
		t.Fatalf("mode %s", result.Mode)
	}
	if !result.Detail.Forced || gateway.forcedCalls.Load() != 1 {
		t.Fatalf("forced=%v calls=%d", result.Detail.Forced, gateway.forcedCalls.Load())
	}
	if len(result.Detail.Pulls) != 0 {
		t.Fatalf("none must not count as a pull: %+v", result.Detail.Pulls)
	}
}

func TestPresenceJevDuplicateSourceForcesFinal(t *testing.T) {
	gateway := newFakeJevGateway(t,
		func(int) map[string]any {
			// Keeps asking for the same exhausted non-repeatable source.
			return map[string]any{"reply_mode": answerChoice("reply_mode", "need_context", 0.5), "next_source": answerChoice("next_source", "last_reply", 0.6)}
		},
		map[string]any{"reply_mode": answerChoice("reply_mode", "no_fast_reply", 0.7)},
		nil,
	)
	pulls := newFakePresencePulls()
	pulls.signals = presenceJevSignals{TurnMessages: 2, AssistantMessages: 2}
	pulls.lastReply = "上一条回复"
	engine := newTestEngine(gateway.server.URL, pulls, 10*time.Second, conversationpresence.Facts{}, "测试输入长于二十个字符用于重复源判定")
	result := engine.decide(context.Background()).Result
	if result == nil || !result.Detail.Forced {
		t.Fatalf("duplicate source must force the final judgment: %+v", result)
	}
	duplicates := 0
	for _, pull := range result.Detail.Pulls {
		if pull.Duplicate {
			duplicates++
		}
	}
	if duplicates == 0 || pulls.calls["last_reply"] != 1 {
		t.Fatalf("duplicates=%d pulls=%d", duplicates, pulls.calls["last_reply"])
	}
}

func TestPresenceJevLoopCapExhaustionForcesFinal(t *testing.T) {
	gateway := newFakeJevGateway(t,
		func(int) map[string]any {
			return map[string]any{"reply_mode": answerChoice("reply_mode", "need_context", 0.5), "next_source": answerChoice("next_source", "session_meta", 0.5)}
		},
		map[string]any{"reply_mode": answerChoice("reply_mode", "no_fast_reply", 0.7)},
		nil,
	)
	pulls := newFakePresencePulls()
	pulls.signals = presenceJevSignals{TurnMessages: 2, PriorPresenceRows: 1}
	engine := newTestEngine(gateway.server.URL, pulls, 10*time.Second, conversationpresence.Facts{}, "测试输入长于二十个字符用于循环上限判定")
	result := engine.decide(context.Background()).Result
	// session_meta is repeatable-on-offer? No: it is not repeatable on the
	// pulled map, so after one pull the loop hits the duplicate path or the
	// 5-call structural cap; both end in a forced final judgment.
	if result == nil || !result.Detail.Forced {
		t.Fatalf("loop exhaustion must force the final judgment: %+v", result)
	}
	if total := int(gateway.stage1Calls.Load()) + int(gateway.forcedCalls.Load()); total > 7 {
		t.Fatalf("total calls %d exceed the structural cap", total)
	}
}

func TestPresenceJevBudgetGuardsFallToStructuralFallback(t *testing.T) {
	gateway := newFakeJevGateway(t,
		func(int) map[string]any {
			return map[string]any{"reply_mode": answerChoice("reply_mode", "need_context", 0.5), "next_source": answerChoice("next_source", "session_meta", 0.5)}
		},
		nil, nil,
	)
	pulls := newFakePresencePulls()
	pulls.signals = presenceJevSignals{TurnMessages: 2, PriorPresenceRows: 1}
	facts := conversationpresence.Facts{PreviousSettled: true, HistoryAvailable: true}
	// A window too small to afford the loop guard: everything degrades to the
	// structural fallback with budget_exhausted, and the settled follow-up
	// shape with a long input still gets the constant ack.
	engine := newTestEngine(gateway.server.URL, pulls, 2*time.Second, facts, strings.Repeat("还在吗，帮我看一下上次那个任务的结论", 3))
	result := engine.decide(context.Background()).Result
	if result == nil {
		t.Fatal("no result")
	}
	if result.FallbackReason != "budget_exhausted" {
		t.Fatalf("fallback reason %q", result.FallbackReason)
	}
	if result.Mode != presenceModeFallbackACK || result.Temperature != conversationpresence.Cold {
		t.Fatalf("settled long follow-up must map to fallback_ack/cold: %s/%s", result.Mode, result.Temperature)
	}
	if gateway.stage1Calls.Load() != 0 || gateway.forcedCalls.Load() != 0 {
		t.Fatal("no call may be sent when the guard cannot afford it")
	}
}

func TestPresenceJevFallbackSilentForShortInputAndOtherShapes(t *testing.T) {
	fallbackFor := func(facts conversationpresence.Facts, text string) *presenceJevResult {
		return (&presenceJevEngine{
			settings: PresenceJevRuntimeOptions{Timeout: 10 * time.Second, PullMax: 4, PullRunes: 500, StateMaxRunes: 3200, Directions: []string{"soothe"}},
			in:       presenceJevInput{Text: text, Facts: facts, AdmittedAt: time.Now()},
		}).structuralFallback(jev.ReasonTimeout, presenceDecisionDetail{})
	}
	long := strings.Repeat("追问内容", 10)
	if result := fallbackFor(conversationpresence.Facts{PreviousSettled: true, HistoryAvailable: true}, long); result.Mode != presenceModeFallbackACK || result.Temperature != conversationpresence.Cold {
		t.Fatalf("settled long follow-up: %+v", result)
	}
	if result := fallbackFor(conversationpresence.Facts{PreviousSettled: true, HistoryAvailable: true}, "还在吗"); result.Mode != presenceModeFallbackNoop || result.Temperature != conversationpresence.Hot {
		t.Fatalf("short settled input must stay silent: %+v", result)
	}
	if result := fallbackFor(conversationpresence.Facts{PendingUserInput: true, HistoryAvailable: true}, long); result.Mode != presenceModeFallbackNoop {
		t.Fatalf("pending answer turn must stay silent: %+v", result)
	}
	if result := fallbackFor(conversationpresence.Facts{FirstDirectInput: true, HistoryAvailable: true}, long); result.Mode != presenceModeFallbackNoop {
		t.Fatalf("first direct input must stay silent: %+v", result)
	}
	if result := fallbackFor(conversationpresence.Facts{ReplyContinuation: true, PreviousSettled: true, HistoryAvailable: true}, long); result.Mode != presenceModeFallbackNoop {
		t.Fatalf("reply continuation must stay silent: %+v", result)
	}
}

func TestPresenceJevUrgencyClamp(t *testing.T) {
	cases := []struct {
		raw  float64
		ok   bool
		want int
	}{
		{0.1, true, 1}, {1.6, true, 3}, {2.9, true, 3}, {4.2, true, 3}, {-3, true, 1}, {1, false, 2},
	}
	for _, tc := range cases {
		if got := clampUrgency(tc.raw, tc.ok); got != tc.want {
			t.Fatalf("clampUrgency(%v,%v)=%d want %d", tc.raw, tc.ok, got, tc.want)
		}
	}
}

func TestPresenceJevStateClippingAndSegments(t *testing.T) {
	pulls := newFakePresencePulls()
	pulls.signals = presenceJevSignals{TurnMessages: 1, HistoryChars: 100}
	engine := newTestEngine("", pulls, 10*time.Second, conversationpresence.Facts{}, "输入")
	loop := &presenceJevLoop{eng: engine, ctx: context.Background(), signals: pulls.signals, detail: &presenceDecisionDetail{}, pulled: map[string]bool{}}
	state := loop.buildState(false)
	for _, want := range []string{"【输入】", "【信号】", "【已拉取】（无）", "【约束】剩余拉取次数=4；最后判定机会=否"} {
		if !strings.Contains(state, want) {
			t.Fatalf("state missing %q:\n%s", want, state)
		}
	}
	forced := loop.buildState(true)
	if !strings.Contains(forced, "最后判定机会=是") {
		t.Fatalf("forced state missing last-chance marker:\n%s", forced)
	}
	// Clipping: a tiny state budget forces segment dropping and flags it.
	engine.settings.StateMaxRunes = 120
	loop.segments = []string{strings.Repeat("段", 300)}
	_ = loop.buildState(false)
	if !loop.detail.TruncatedState {
		t.Fatal("clipping must be recorded")
	}
}

func TestPresenceJevSupersededDuringWindow(t *testing.T) {
	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		select {
		case <-release:
		case <-r.Context().Done():
		}
	}))
	defer server.Close()
	defer close(release)
	engine := newTestEngine(server.URL, newFakePresencePulls(), 10*time.Second, conversationpresence.Facts{}, "测试")
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()
	outcome := engine.decide(ctx)
	if !outcome.Superseded || outcome.Result != nil {
		t.Fatalf("outcome %+v", outcome)
	}
}

func TestPresencePromptGates(t *testing.T) {
	if _, matched := presenceLeadMatches("收到，马上看。", []string{"收到，在看。"}); !matched {
		t.Fatal("leading two runes must match")
	}
	if _, matched := presenceLeadMatches("这就去看。", []string{"收到，在看。"}); matched {
		t.Fatal("different leads must not match")
	}
	if !presenceETABandAllows("估计几十秒。", "几十秒") {
		t.Fatal("band-compatible duration rejected")
	}
	if presenceETABandAllows("大约 5 分钟就好", "几十秒") {
		t.Fatal("out-of-band duration accepted")
	}
	if presenceETABandAllows("很快", "很快") {
		// No duration expression at all is fine.
	} else {
		t.Fatal("no-duration text rejected")
	}
}

func TestPresenceSlotsBudgets(t *testing.T) {
	slots := buildPresenceSlots("Bot", "zh", strings.Repeat("长", 1000), []string{strings.Repeat("段", 400), strings.Repeat("节", 300), strings.Repeat("条", 300)}, conversationpresence.Thinking, true, 3, "几十秒", []string{"早", "午", "晚"})
	if got := len([]rune(slots.UserMessage)); got != presenceSlotUserMaxRunes {
		t.Fatalf("user message %d", got)
	}
	total := 0
	for _, point := range slots.ContextPoints {
		if len([]rune(point)) > presenceSlotPointMaxRunes {
			t.Fatalf("point too long")
		}
		total += len([]rune(point))
	}
	if total > presenceSlotPointsTotal {
		t.Fatalf("context total %d", total)
	}
	if len(slots.RecentACKTexts) != 2 {
		t.Fatalf("recent acks %d", len(slots.RecentACKTexts))
	}
	encoded, err := json.Marshal(slots)
	if err != nil {
		t.Fatal(err)
	}
	if len(encoded) > 4096 {
		t.Fatalf("slots payload unexpectedly large: %d", len(encoded))
	}
	if !strings.Contains(presenceSystemPrompt("progress"), "【方向：进度 progress】") {
		t.Fatal("direction body missing from system prompt")
	}
	if !strings.Contains(presenceSystemPrompt("unknown-direction"), "【方向：安抚 soothe】") {
		t.Fatal("unknown direction must fall back to soothe")
	}
}

func TestPresenceFallbackGeneratorGrace(t *testing.T) {
	generator := presenceFallbackGenerator{text: presenceFallbackText, notBefore: time.Now().Add(80 * time.Millisecond)}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	input := conversationpresence.Input{Phase: conversationpresence.Thinking}
	start := time.Now()
	result, err := generator.Generate(ctx, input, conversationpresence.Limits{MaxInputRunes: 100, MaxOutputTokens: 100, MaxTextRunes: 160})
	if err != nil {
		t.Fatal(err)
	}
	if result.Text != presenceFallbackText || result.Phase != conversationpresence.Thinking {
		t.Fatalf("result %+v", result)
	}
	if elapsed := time.Since(start); elapsed < 70*time.Millisecond {
		t.Fatalf("grace not observed: %v", elapsed)
	}
	// Canceled grace is an error, the coordinator records canceled.
	canceledCtx, cancelCanceled := context.WithCancel(context.Background())
	cancelCanceled()
	if _, err := (presenceFallbackGenerator{text: presenceFallbackText, notBefore: time.Now().Add(time.Hour)}).Generate(canceledCtx, input, conversationpresence.Limits{}); err == nil {
		t.Fatal("canceled grace must error")
	}
}

func TestPresenceJevOfferedSources(t *testing.T) {
	pulls := newFakePresencePulls()
	pulls.signals = presenceJevSignals{TurnMessages: 0, AssistantMessages: 0, FileCount: 2, PriorPresenceRows: 0}
	engine := newTestEngine("", pulls, 10*time.Second, conversationpresence.Facts{}, "输入")
	loop := &presenceJevLoop{eng: engine, ctx: context.Background(), signals: pulls.signals, detail: &presenceDecisionDetail{}}
	offered := loop.offeredSources()
	if len(offered) != 2 || offered[0] != presencePullFileList || offered[1] != presencePullNone {
		t.Fatalf("offered %v", offered)
	}
	if loop.sourceOffered(presencePullRecentTurns) {
		t.Fatal("absent source must not be offered")
	}
}
