package application

import (
	"strings"
	"unicode/utf8"

	"github.com/felinics/memoh/internal/agent/sessionmode"
	messagepkg "github.com/felinics/memoh/internal/chat/message"
	sessionpkg "github.com/felinics/memoh/internal/chat/thread"
	"github.com/felinics/memoh/internal/models"
	"github.com/felinics/memoh/internal/reasoning"
)

// Per-turn reasoning effort tiering (main-chain first-text latency). The tier
// a turn runs at is the strongest lever on first_text: the reasoning phase is
// minutes of the visible wait, and a bot configured for a strong tier pays it
// on every confirmation as much as on every hard task. This file picks the
// tier from signals that exist before the provider request — session type and
// the shape of the input — and never adds an LLM call to the critical path.
//
// The decision runs once per run config, before the SDK model is built, so
// every step of the run carries the same effort: switching effort mid-run
// invalidates prompt-cache prefixes, and a per-turn decision made at turn
// start is the granularity the cache tolerates.

const (
	// tierSourceScheduleSubagent marks unattended turns: schedule fires and
	// subagent spawns run with no user watching a first token.
	tierSourceScheduleSubagent = "schedule_subagent"
	// tierSourceComplex marks turns that keep the bot's configured tier.
	tierSourceComplex = "complex"
	// tierSourceShortInput marks short interactive turns without tool intent.
	tierSourceShortInput = "short_input"
	// tierSourceChatDefault is the interactive default.
	tierSourceChatDefault = "chat_default"
)

const (
	// shortInputMaxRunes bounds the short bucket. Research on the latency
	// decomposition (docs/field-issues 2026-11-02) puts short factual turns at
	// ≤200 characters; anything longer is not a short turn.
	shortInputMaxRunes = 200
	// longInputMinRunes marks inputs long enough that the turn is presumed a
	// heavy task regardless of wording.
	longInputMinRunes = 600
)

// complexityMarkers are explicit user asks for depth. A turn carrying one keeps
// the bot's configured tier rather than any auto downgrade.
var complexityMarkers = []string{
	"仔细", "彻底", "认真", "深入", "详尽", "详细", "全面", "严谨", "慢慢来",
	"thorough", "carefully", "in-depth", "detailed", "comprehensive", "rigorously",
	"take your time",
}

// toolIntentMarkers are action phrasings that make a short input a task, not a
// quick answer. The list errs toward matching: a false hit only keeps the
// ordinary tier (latency, not quality), while a miss would downgrade a real
// task.
var toolIntentMarkers = []string{
	"发给", "发送", "传给", "发过来", "打开", "关掉", "删除", "删掉", "移到", "移动",
	"复制", "拷贝", "保存", "上传", "下载", "安装", "卸载", "运行", "执行", "重启",
	"启动", "停止", "找出", "找一下", "搜一下", "搜索", "查找", "整理", "清理", "归类",
	"备份", "压缩", "解压", "转换", "新建", "创建", "写个", "写一", "生成", "截图",
	"录制", "录屏", "播放", "暂停",
	"send it", "send to", "open", "close", "delete", "remove", "move", "copy",
	"save", "upload", "download", "install", "uninstall", "run", "execute",
	"restart", "start", "stop", "find", "search", "screenshot", "record", "play",
}

// effortTierInputs are the synchronous signals a tier decision is made from.
type effortTierInputs struct {
	Enabled bool
	// SessionType is the resolved session type ("chat", "schedule", "subagent").
	SessionType string
	// InputText is the current turn's user text; empty for resumed turns that
	// carry no new input.
	InputText string
	// Attachments is the count of inbound attachments on the turn.
	Attachments int
	// RequestedEffort is the per-message explicit pick; a non-empty value
	// disables tiering for the turn (the user chose).
	RequestedEffort string
	// SessionEffort is the session's remembered pick, already model-matched by
	// the caller; non-empty disables tiering for the turn.
	SessionEffort string
}

// applyEffortTiering mutates cfg.Effort to the tier the turn's shape selects,
// bounded above by the effort the ordinary resolution chain produced (the cap:
// session-remembered or bot-stored tier). It returns the decision for
// accounting, or nil when no tiering applies — switch off, reasoning inactive,
// a user's explicit choice, or a model whose tiers cannot express a downgrade.
// Nil always means cfg is left exactly as resolved.
func applyEffortTiering(cfg *models.ReasoningConfig, advertised []string, in effortTierInputs) *reasoning.TierDecision {
	if cfg == nil || !in.Enabled || !cfg.Active {
		return nil
	}
	if strings.TrimSpace(in.RequestedEffort) != "" || strings.TrimSpace(in.SessionEffort) != "" {
		return nil
	}
	cap := strings.TrimSpace(cfg.Effort)
	if cap == "" {
		return nil
	}

	desired, source := tierForTurn(in, cap)
	sent, ok := reasoning.DownshiftTier(desired, cap, advertised)
	if !ok {
		return nil
	}
	cfg.Effort = sent
	return &reasoning.TierDecision{Applied: sent, Cap: cap, Source: source}
}

// tierForTurn maps the turn's shape to a desired tier. First match wins.
func tierForTurn(in effortTierInputs, cap string) (string, string) {
	sessionType := strings.ToLower(strings.TrimSpace(in.SessionType))
	if sessionType == sessionmode.Schedule || sessionType == sessionpkg.TypeSubagent {
		return reasoning.EffortLow, tierSourceScheduleSubagent
	}
	text := strings.TrimSpace(in.InputText)
	runes := utf8.RuneCountInString(text)
	if complexityMarkerIn(text) || in.Attachments > 0 || runes >= longInputMinRunes {
		return cap, tierSourceComplex
	}
	if runes > 0 && runes <= shortInputMaxRunes && !toolIntentMarkerIn(text) {
		return reasoning.EffortLow, tierSourceShortInput
	}
	return reasoning.EffortHigh, tierSourceChatDefault
}

func complexityMarkerIn(text string) bool {
	return containsAnyFold(text, complexityMarkers)
}

func toolIntentMarkerIn(text string) bool {
	return containsAnyFold(text, toolIntentMarkers)
}

func containsAnyFold(text string, markers []string) bool {
	if text == "" {
		return false
	}
	lower := strings.ToLower(text)
	for _, marker := range markers {
		if marker != "" && strings.Contains(lower, strings.ToLower(marker)) {
			return true
		}
	}
	return false
}

// withReasoningEffortMetadata stamps the tiering decision on the assistant rows
// of the round so the tier a turn actually ran at is queryable from history
// (acceptance for the latency work reads it next to reasoning_timing). A nil
// decision stamps nothing: those turns ran at the ordinary resolved effort and
// say so by the key's absence.
func (opts storeRoundOptions) withReasoningEffortMetadata(messages []ModelMessage) storeRoundOptions {
	if opts.ReasoningEffortTier == nil {
		return opts
	}
	for index, message := range messages {
		if !strings.EqualFold(strings.TrimSpace(message.Role), "assistant") {
			continue
		}
		if opts.MessageMetadataByIndex == nil {
			opts.MessageMetadataByIndex = make(map[int]map[string]any, 1)
		}
		existing := opts.MessageMetadataByIndex[index]
		if existing == nil {
			existing = map[string]any{}
		}
		existing[messagepkg.ReasoningEffortMetadataKey] = *opts.ReasoningEffortTier
		opts.MessageMetadataByIndex[index] = existing
	}
	return opts
}
