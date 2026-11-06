package application

import "github.com/felinics/memoh/internal/jev"

// Fast-reply v3 question ids (design v3 §3).
const (
	jevQReplyMode  = "reply_mode"
	jevQNextSource = "next_source"
	jevQUrgency    = "urgency"
)

// jevChoiceOptions carries a choice question's option set with the
// descriptions Jev scores against.
type jevChoiceOptions map[string]string

// Criteria texts are server-owned constants (design v3 §6.5: same policy as
// the generation prompts; no config override surface).

const jevCriteriaReplyMode = `判定核心（静默空窗维度）：判"从受理到主链出现首个实质正文"预计需要多久（排队＋思考＋首 token，即 TTFT）。用户是否空等只取决于这段无输出窗口，不取决于整段回答要写多久——正文开始流式出字后，用户已在阅读，对话没有冷却。
判别：A 一次生成流到底、且预计首个正文 <4s → no_fast_reply；B 前置长静默（深思考，effort 高/极高）或中段多次长静默（需检索、多工具串行、审批等待）→ fast_reply；C 用户着急/已等待 → fast_reply（叠在 A/B 之上）。
快回价值门（机械，判据核心）：若"预估 ack 可达时刻（组装 0.3＋一段＋二段＋生成保底 3.0，观测时序 ≈3.5-5.5s）≥ 预计首个正文时刻 −1.5s"，判 no_fast_reply——ack 注定被正文盖掉，纯成本。TTFT 未知时以 effort 档位＋最近同类样本中位值（账目 timings）为先验。
输出长度不是判据，只是辅助信号：成段长文只要首字快、流式连贯，就不需要快回；只有"长文＋需先检索/多步"因中段静默进入 fast_reply。长文更常伴随起笔前的结构思考，可作为抬高 TTFT 先验的旁证，不能单独成为判 fast_reply 的理由。
思考档位是 TTFT 的主要预测器：effort 高/极高时同题 TTFT 上移，边界题倾向 fast_reply。

判定顺序（多条判据同时命中时按此仲裁）：第一步，本条是否在回答 pending 问题、或属确认/许可/选择的短应答 → 是则 no_fast_reply；第二步，本条是否指向一个仍在进行的任务（追问进度、催促、重复询问、截断后的"继续"）→ 是则 fast_reply；第三步，按 TTFT 预判（判别 A/B/C＋价值门）仲裁。催促一律看被催促对象的复杂度，不看催促强度：催促一个首个正文马上就到的简单问题仍判 no_fast_reply；催促深任务/长静默对象则判 fast_reply。

判 no_fast_reply（首个正文很快，直接等正文）：①事实/确认类提问，主链无前置深思考/检索即起笔、预计首个正文 <4s（答案一两句可说尽是常见伴随特征，不是判据本身）；②确认、许可、选择类的短应答（"好的""用第二个"）；③闲聊、寒暄、致谢、道别；④对上一条回复的简单复述性追问，且答案就在上一条里、正文随即可到；⑤本条是在回答本会话 pending 的问题；⑥续写指令（"继续""然后呢"）且上一轮回复完整未截断——主模型直接接着写，首个正文常在 1-2s。共同点：从受理到首个正文无可感知的静默空窗，或价值门判定 ack 注定被盖，快回反而会打断和重叠。

判 fast_reply（首个正文之前存在可感知静默，先接住用户）：①深加工请求：分析、对比、评审、规划、多步推理、长文翻译；②多步任务：需要多个工具调用或分阶段执行（查多个来源、跑代码、批量处理）；③创作类：写作、代码、方案、报告、长列表或结构化输出——起笔前通常先深思考/列结构；④问题依赖外部信息且需要检索后才能答；⑤答案本身确定，但需先长思考才起笔（effort 高/极高、多步推理）→ TTFT 过线即 fast_reply（概念解释、原理说明类，如"X 是什么"；effort 低且一步起笔的同类题按价值门判 no_fast_reply）；⑥输入存在关键歧义或缺参数（指代不明、范围未定、二选一未标记），主回答大概率需要先澄清才能开工（与 Q3 clarify 同判据，为 clarify 提供 Q1 入口）；⑦本回合新增附件≥1（信号区"新增附件"）——附件内容只能经查阅工具获取，首正文前必有工具往返＋多模态/文档理解，静默空窗可感知；价值门按此 TTFT 先验必过线；⑧粘贴的长材料（输入被截断=true）附带任务——典型深加工；⑨用户正在等待一个仍在进行的上一轮任务：追问进度、催促、重复询问同一事项，含上一轮因长度截断后的"继续"。共同点：首个正文之前存在可感知的静默空窗（前置深思考/检索/多步），且价值门判定 ack 来得及，用户需要先有一句自然回应接住。

判 need_context（先补一段证据再判）：依据当前信息无法可靠区分上述两种——典型：输入引用"上次/那个/前面说的"而上下文未提供；问题看似一两句可答但答案可能藏在历史对话里；等待情境信号（是否有进行中任务）不完整。此时必须在 Q2 选一个最有判别价值的证据源；若认为再拉任何源也无帮助，Q2 直接选 none（立即强制终判，不再重问，见 §4.3）。

信号使用（【信号】区字段是判据输入，不是装饰）：思考档位高/极高时同题 TTFT 上移，一两句可答的边界题也应倾向 fast_reply；上下文 token 估算大（历史长）→ 同题 TTFT 更长、价值门更易过线，倾向 fast_reply；新增附件≥1 → 直接命中⑦，例外：无（pending 答语附带附件同样要读附件，仍慢）；引用回复=是 → 按被引用内容的复杂度处理（同催促规则），不能一眼判定时选 need_context 先拉取；上一轮因长度截断=是 且本条为续写 → ⑨；输入被截断=true → 不得判 no_fast_reply；此前快回频繁（距上次快回很短、近期次数多）→ 同等条件下倾向 no_fast_reply，避免连环打扰。`

const jevCriteriaReplyModeForced = `这是最后一次判定机会：拉取次数或时间预算已用尽，不得再要求补充证据。只回答一个问题——基于 state 中现有全部信息（含已拉取段落与"最后判定机会=是"标记），此刻应不应该发一条快回。判据与 Q1 完全同源（TTFT 预判＋价值门＋判定顺序第一/二步），区别仅在取消 need_context 选项：信息不足以区分时按保守侧仲裁——无明确等待情绪且无进行中任务信号 → no_fast_reply（宁可不接，不发注定被正文盖掉的快回）；用户有明确等待情绪、或存在进行中的具体事项 → fast_reply。信号区"新增附件"≥1 → fast_reply（附件回合首正文前必有查阅工具往返，与 Q1 判据⑦同源）。已拉取段落足以支撑任一方向时，直接按该方向回答。`

const jevCriteriaNextSource = `当需要补充证据时，选择最能帮助判定"主模型能否短时作答"的一段证据源：
- recent_turns 最近对话正文：判断本条输入是否延续最近话题、上一轮是否已给出答案、话题转换的边界。
- last_reply 上一条回复正文：判断追问的答案是否已经写过、上一轮任务的实际复杂度。
- task_state 任务状态：判断上一轮任务是否仍在进行、处于什么阶段、已运行多久——催促、进度、时长类判定的关键源（随时间变化，可重复拉取刷新）。
- file_list 文件清单：判断输入是否指向特定文件、任务是否涉及文件处理。
- session_meta 会话元信息：判断会话密度、此前是否已多次快回（避免连环打扰）。
- none 没有再拉的价值——选它即立即按现有信息强制终判（Q1b），不再重问、不计拉取。
已拉过的不可重复源不会提供新内容，不要重复选；唯一例外是 task_state（状态随时间变化）。选择尚有余量且最可能改变判定的那一个。`

const jevCriteriaDirection = `选择快回的方向。快回是一句话的即时回应，主回答仍在生成、随后覆盖它。按以下特征选唯一方向：
- soothe 安抚：用户有等待情绪（催促、着急、不满、连续追问），但当前信号不足以如实给出具体进度或时长。目标：接住情绪、确认收到并仍在跟进；不做时间承诺。
- progress 进度：state 信号能如实反映当前阶段（上一轮任务仍在运行+已耗时、或本条已受理在处理），用户在等一个具体的进行中事项。目标：如实转述阶段，绝不虚构工具、步骤或完成度。本回合有新增附件（信号区"新增附件"≥1）→ progress：附件本回合刚到达、处理尚未落定，适合如实接住。
- eta 预告时长：用户明确问了时长（"要多久""什么时候好"），或等待已显著超常（进行中任务已耗时很高）。目标：给出粗档时长（系统会提供按阶段与耗时计算的档位），不编造精确数字。
- clarify 反问澄清：输入存在关键歧义或缺参数（指代不明、范围未定、二选一未标记），主回答大概率需要先澄清才能开工（Q1 侧同判据入口见 §3.1 fast ⑥）。目标：替主回答先问出最关键的那一个问题，只问一个。
- brief 简答：问题本身是事实/确认类，且答案依据已经出现在已拉取的证据段落里，主回答只是还没轮到。目标：一句话给出答案要点并示意完整版随后就到。证据不足时禁止选 brief。
边界：情绪明显但能如实说进度 → progress（顺带安抚语气）；既能简答又在催促 → soothe（先接情绪，答案留给正文）；歧义大到主回答可能整轮白跑 → clarify 优先。`

const jevCriteriaUrgency = `用户此刻的着急程度，影响快回的语气与长短：
1 平和：常规请求语气，无时间压力表达，未催促。
2 关注：出现追问、"尽快""快一点"类时间表达，或距上一条消息间隔很短且指向同一任务。
3 着急：连续催促、"马上""急""怎么还没好"、明显不满或抱怨等待。`

// jevReplyModeOptions returns the Q1 option set.
func jevReplyModeOptions() jevChoiceOptions {
	return jevChoiceOptions{
		"no_fast_reply": "不发快回，直接等主链正文",
		"fast_reply":    "发一条快回先接住用户",
		"need_context":  "先补一段证据再判",
	}
}

func jevReplyModeForcedOptions() jevChoiceOptions {
	return jevChoiceOptions{
		"no_fast_reply": "不发快回，直接等主链正文",
		"fast_reply":    "发一条快回先接住用户",
	}
}

// jevUrgencyLevels returns Q4 score levels (zero-based on the wire; urgency
// 1-3 on the accounting surface maps level index + 1).
func jevUrgencyLevels() []string {
	return []string{"平和：常规请求语气，未催促", "关注：追问/时间表达/短间隔同任务", "着急：连续催促、明显不满"}
}

func jevStageOneQuestions(nextSources []string) map[string]jev.Question {
	options := jevChoiceOptions{}
	descriptions := jevNextSourceDescriptions()
	for _, source := range nextSources {
		if _, ok := descriptions[source]; ok {
			options[source] = descriptions[source]
		}
	}
	return map[string]jev.Question{
		jevQReplyMode:  jev.NewChoice(jevCriteriaReplyMode, jevReplyModeOptions()),
		jevQNextSource: jev.NewChoice(jevCriteriaNextSource, options),
	}
}

func jevForcedQuestion() jev.Question {
	return jev.NewChoice(jevCriteriaReplyModeForced, jevReplyModeForcedOptions())
}

func jevStageTwoQuestions(directions []string) map[string]jev.Question {
	options := jevChoiceOptions{}
	descriptions := jevDirectionDescriptions()
	for _, direction := range directions {
		if _, ok := descriptions[direction]; ok {
			options[direction] = descriptions[direction]
		}
	}
	return map[string]jev.Question{
		jevQDirection: jev.NewChoice(jevCriteriaDirection, options),
		jevQUrgency:   jev.NewScore(jevCriteriaUrgency, jevUrgencyLevels()),
	}
}
