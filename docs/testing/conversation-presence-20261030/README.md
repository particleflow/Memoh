# 冷／热沟通与快速回应：只读调研及实施建议

日期：2026-10-30 UTC。状态：研究完成，未实施、未发布、未验收。

研究范围：通过会话连续性判断何时提供同模型低思考快速反馈。已参考项目验收要求和兼容故障登记；私有验收页面与对话定位不公开。以下区分来源事实、历史阶段与项目建议。

## 1. 可以直接交给实施负责人的结论

推荐在现有消息链上增加**同一回合的受理／进度表现**，并以确定性会话状态决定是否启动**同模型辅助快回请求**；正式请求立即独立推进。辅助请求不能排进同会话的正式 turn 队列，不能改正式模型／思考偏好，不能执行工具。先有可信受理反馈，再尽可能出现一句自然快回，最后由正式正文接替。

这与用户的同模型想法匹配，但有两个必要前提：实际模型及实际路由支持请求级低／无思考，而且双请求不会因账户并发限制明显拖慢主链。若不满足，快回退为本地、事实准确的受理文字，不能悄悄换模型、谎称已关闭思考或先等待快回再开始正式工作。

**关键兼容发现：**Z.AI 官方当前说明 GLM-5.3 只接受 `low/high/max`，默认 `max`，不接受关闭思考；它还是纯文本输入模型。历史验收记录出现 GLM-5.3，但不是当前主聊模型配置的证明。[官方 GLM-5.3 能力及参数](https://docs.z.ai/guides/llm/glm-5.3)。所以“同模型低思考”可能可行，“同模型无思考”不能普遍承诺。

没有找到一手证据证明成熟产品普遍采用“每条用户消息先追加一个 LLM 冷热分类，再追加一个同模型 ack”。本报告不把该结构包装成行业惯例。成熟机制可复用的是：会话状态、短反馈、并行执行、结果抢先时取消反馈、可中断、按身份归属和显式管理历史。

## 2. 成熟做法比较与来源边界

| 来源与已核事实 | 本项目可复用 | 不能据此推断 |
| --- | --- | --- |
| Slack 当前 Agent Sessions 有 `processing/active/suspended/closed`；处理状态和消息分别管理；流式 API 能进入处理状态 | 受理后及时显示正在处理；等待用户输入是显式状态；完结清除处理态 | Slack 内部是否双模型、如何思考、如何判断话题；新 API 的 processing 不支持自定义 loading 文案 |
| Slack 旧 `assistant.threads.setStatus` 支持短状态，回复后自动清除，且已指向迁移文档 | 短反馈与正式回复的生命周期分离 | 不应照搬已在迁移中的接口，也不能说新接口支持旧自定义文案 |
| LiveKit 官方 external-data 示例并行启动短状态生成，查询先结束则取消状态任务 | 辅助反馈不阻塞正式任务；迟到反馈丢弃 | 示例针对已开始的工具调用，不证明能在工具未开始时说“正在查询” |
| LiveKit 官方 cached-TTS 示例让等待语音与查询并行，结果先到可中断，支持不加入模型上下文 | 反馈音频与正文共用取消权；历史与语音可分别控制 | 不意味着应新增一套播放器或固定等待秒数；本轮不合成缓存音频 |
| Rasa 用显式会话开始、可配置空闲策略和上下文携带控制管理会话边界 | 把沟通重新开始作为产品状态；时长可配置 | Rasa 的默认分钟数不是用户对冷／热的定义 |
| LangGraph 中断状态持久保存，同一 thread 恢复原 checkpoint | 待用户回答／授权的会话，隔很久也仍可能是延续 | 不为本项目引入 LangGraph，也不把等待时间当新任务证据 |

一手链接：

- [Slack Agent Sessions](https://docs.slack.dev/ai/agent-sessions/)；[当前 setStatus](https://docs.slack.dev/reference/methods/agents.sessions.setStatus)；[旧 setStatus 与迁移提醒](https://docs.slack.dev/reference/methods/assistant.threads.setStatus/)。
- [LiveKit：长操作的用户反馈](https://docs.livekit.io/agents/logic/external-data/#user-feedback)；[并行缓存等待语音](https://docs.livekit.io/agents/multimodality/audio/customization/#using-cached-tts-in-a-tool-call)。
- [Rasa Domain / Session configuration](https://rasa.com/docs/reference/config/domain/)；[LangGraph Interrupts](https://docs.langchain.com/oss/python/langgraph/interrupts)。

开源社区经验也提示不能只把两个输出接到一起：[Pipecat #4041](https://github.com/pipecat-ai/pipecat/issues/4041)原作者报告快速工具回合导致 TTS context 重叠、触及供应商并发上限及文本聚合串接问题；这是具版本范围的原始故障报告，不是本项目已复现的根因。它支持“统一音频生命周期、验证真实供应商限制”的验收方向，不能证明 NAS 朗读断续就是同一问题。

LiveKit 的 speculative/preemptive generation 是用户说完前投机生成，可能增加弃用计算；本项目是用户消息受理后的沟通反馈，两者不同。不能用音频端点检测、模型预热、缓存命中或 speculative decoding 代替冷／热沟通判断。[LiveKit 音频及投机生成说明](https://docs.livekit.io/agents/multimodality/audio/)。

## 3. 冷／热沟通的可解释条件（项目设计建议）

这里的“冷”只表示用户在一次新的沟通接触中需要被及时接住，不表示模型进程冷启动、首次连接、缓存失效或 TTFT 较长。既有 thread 可以冷沟通；新打开的页面也可能在恢复热沟通。分类只影响反馈，不新建／清空会话，不重置任务，不改变权限、队列或历史。

先读可信元数据和现有状态；不为每条消息增加 LLM 分类请求。建议返回 `cold / hot / uncertain / ineligible` 和原因码，保留规则版本、证据来源及配置版本，便于解释和调优。规则优先次序：

1. **先做资格判断。**只对经鉴权、成功受理的直接用户沟通启动自然 ack。定时、专家委派、后台结果、工具回流不冒充一次新的用户发言；取消、重试重放、队列重复或已有 ack 的 invocation 不再触发。上传未就绪、网络发送中和真正受理要区分。
2. **明确延续优先。**回答现有 `ask_user`／审批、明确 reply-to、steer／follow-up、对当前任务的补充或纠错、有正在处理的同一沟通回合，都归延续。不能因跨夜、刷新、进程重启或网络恢复就自动冷启动。正在执行的后台专家属于其任务；用户另开新话题仍可冷，不能只看“Bot 有任意后台任务”就永久判热。
3. **明确新开始。**没有既有用户沟通的首次直接输入；或用户明确发起新会话／新话题且没有指向待答交互。沿实际会话身份和状态判断，不以 Bot 名称、模型 slug、样本 ID 特判。明确换话题只重开反馈 episode，保留原 thread 与未完任务。
4. **其余组合证据。**同一 Bot/contact/thread 的最近直接用户交流、上一正式回复是否结束、是否已有未结束反馈 episode、是否有可靠的新话题信号。可配置空闲策略只能作为辅助；间隔短也可能新话题，间隔长也可能回答旧问题。后台心跳、工具事件和自动结果不刷新“最近用户沟通”时间。首次实现不引入额外 embedding／LLM 做主题相似度；主链已有可信主题结果可以后续复用。
5. **模糊时保守。**`uncertain` 仍显示可信受理／运行状态，先不产生额外自然 ack；记录匿名原因与用户反馈，避免热聊每句“收到”。若用户特别偏好模糊时也回应，可配置为候选冷，但保留 episode 去重和正式输出优先。

建议原因码如 `first_direct_input`、`explicit_restart`、`pending_user_input`、`reply_continuation`、`inflight_followup`、`recent_exchange`、`idle_without_continuation`、`history_unavailable`。这些是拟议语义，不是当前已存在字段。`idle_without_continuation` 应先作为弱证据／候选冷，而非硬阈值越界即新沟通。

空闲窗口、连发合并窗口、可见反馈等待窗口、ack 有效期、请求期限、输出预算和辅助并发／费用预算全部是可检查的策略配置；不能写死成用户定义。不给本轮未经测量的默认秒数，也不将这些预算用作终止主任务或删除持久历史的条件。

## 4. 同模型低／无思考：能力与路由核验

### 4.1 已有证据

- [Z.AI Thinking mode](https://docs.z.ai/guides/capabilities/thinking-mode)按模型区分能否关闭；[GLM-5.3 页面](https://docs.z.ai/guides/llm/glm-5.3)给出 `low/high/max` 及禁用无思考的限制。只对实际对应模型及端点适用，不能据历史 GLM 记录猜今天的模型。
- [OpenAI Reasoning](https://developers.openai.com/api/docs/guides/reasoning)明确 effort 值依模型而定，Responses 用 `reasoning.effort`、Chat Completions 用 `reasoning_effort`；不是任意模型均接受 `none`。同一 OpenAI 形状的网关也不能证明后端模型接受全部档位。
- [Anthropic Thinking](https://docs.anthropic.com/en/docs/about-claude/models/extended-thinking-models)区分模型允许的 thinking 类型和 effort 约束；支持关闭与否也不能只由协议名称推出。项目若实际经 Anthropic 协议接入，要核目录及真正出站形状。

Magpie 固定源码核对点是历史项目文档引用的 `d783df46cbac9021e26a6bd71ca0b55544552eb2`。这是可读源码基线，不是今天生产 Magpie 版本的独立读回。

1. [remote_magpie.go](https://github.com/yetone/magpie/blob/d783df46cbac9021e26a6bd71ca0b55544552eb2/internal/provider/remote_magpie.go)区分各协议入口。
2. [glm_responses_test.go](https://github.com/yetone/magpie/blob/d783df46cbac9021e26a6bd71ca0b55544552eb2/internal/gateway/glm_responses_test.go)有按协议路由到不同 GLM 路径的源码级 fixture。未在本轮运行该测试，不能据此说真实端点已通过。
3. [groupeffort_test.go](https://github.com/yetone/magpie/blob/d783df46cbac9021e26a6bd71ca0b55544552eb2/internal/gateway/groupeffort_test.go)明确覆盖成员固定 effort 改写请求值及回落到其他成员。因此“两个请求传相同 group alias”不足以证明同模型，也不足以证明 `low` 生效。
4. [session_route.go](https://github.com/yetone/magpie/blob/d783df46cbac9021e26a6bd71ca0b55544552eb2/internal/gateway/session_route.go)的 route trace 区分 asked、asked_effort、model、effort、served、tries、kind；可用来设计验证字段。但其语义是**会话最新请求**，辅助请求与主请求共用 session 时可能互相覆盖；不可只读一次 latest 就给两者归因。必须有准确请求关联，否则记无法判定。

### 4.2 当前可判定及不能判定的事

固定 NAS 源码提供模型能力与 SDK 请求映射，本轮未接触生产模型表／私有端点。因此当前有效主聊 model UUID、模型 slug、Provider client type、接入方式、目录 revision、实际 Magpie 版本、路由组固定档位及并发／配额都未核实。不读凭据或全配置，后续可由唯一现场 owner 只回报这些非秘密白名单字段；没有必要让用户重新输入已有配置。

**拟议选择规则：**冻结本次正式请求已解析的有效 model+provider+runtime 及能力版本；辅助请求复用这个身份，依据确证能力选择 `none` 或最低支持 effort。两种都不支持／未知时本地受理文案即可，不向上游发送猜测参数。主请求继续用户原设定，不做全局开关、不写回偏好、不借标题模型、不换小模型或另一凭据。

若解析结果仍只是自动路由组／alias，又没有在原授权和原路由策略内准确调用同一成员的契约，就不能保证“同模型”：保留本地反馈并记录该原因，不新建路由组、不改主模型或绕网关来凑同模型。即使请求前两者配置一致，也要分别核真实 served model 与 effort，无法关联则记未证实。

当前 `internal/reasoning/resolve.go` 会对不支持的请求值回落到存储／默认值；空目录还可能有通用 effort 候选。这种主聊天兼容行为不能作为“快回已低思考”的证明。辅助路径必须在调用前校验有效结果是否就是获支持的目标档位；不匹配就跳过，而非用默认高档继续烧费用。

并发调用只意味着应用层并行，供应商或账户仍可能串行。同一 provider 的辅助请求若占掉最后一个并发名额会拖慢主链；需要主请求优先、辅助可丢弃、有限输出、期限和总预算，按现有限流机制执行。429／Retry-After／能力错误只降级辅助反馈，不能启动额外自动重试链。取消并不保证供应商不计费，记录实际 usage 或 `unknown`，不能填零。

## 5. 推荐最小执行流程（尚未开发）

```text
用户发送
  → 原有权限／附件／幂等／受理
  → 已受理状态（失败则保持原失败语义）
  → 正式请求独立启动，保持原队列和模型偏好
  → 并行判定反馈资格及冷／热
      cold + 能力已核 + 辅助预算允许 → 同有效模型低／无思考短回复
      hot / uncertain / 不支持 / 预算不足 → 保留受理或真实进度状态
  → 发布自然 ack 前再次比对 invocation／episode／模型版本／正式输出状态
  → 正式正文到达：取消未发布 ack；清掉临时状态；正文持续流式
```

本地“正在发送”可立即出现；收到原 `run_accepted` 后才能说“收到消息”。正在排队只能说“已收到，等待处理”；模型请求确已开始才说“我在梳理”；只收到图片可说“图片收到了，我会结合问题查看”，不能说“已看过”或描述未经读取的图像；工具 started／查询事件实际发生后才允许“正在查”。“安排操作”不能代表已建立任务或授权，未执行不能给完成承诺，不给未经测量的 ETA。

辅助模型输入采用单独、短且受限的生成任务：当前 Bot 的可信名称／语言／沟通风格、当前用户短文本、已鉴权附件**存在及类型**、当前真实阶段和允许的措辞；不读取其他 Bot／任务上下文，不传隐藏推理或整份历史／图像二进制。辅助请求无工具、无外部动作，不生成答案、澄清问题、事实结论、时间承诺或“已完成”。用户内容只作待回应数据，不能把正文中的“以另一个 Bot 身份回复”等当系统指令。可要求结构化单句并在发布端校验长度、有效非空、允许状态；不能保证靠 prompt 就完全防止越界。校验失败直接采用事实文案，不追加修复 LLM 调用。

若采用“同模型生成但最终只选用允许文案”的严格方案，个性化收益有限，应在小范围实测其相对纯本地反馈的价值；没有收益就保留已学到的生命周期机制，并明确告知该模式不再产生模型快回，而不是表面宣称仍在调用。

### 5.1 去重、竞态和打断

- 服务器以现有可信作用域（team/actor/Bot/contact/session）和 invocation 归属；反馈另挂本次 conversation episode。episode 在首次用户受理时即认领，连发的后续输入视为补充，不能在模型返回前各启动一份 ack。没有固定次数业务特例。
- 最多一份自然 ack 是反馈语义不变量，不是重试预算。相同 invocation 重放／重连沿原记录，不能生成第二份；不同 payload 冲突沿现有拒绝。多标签、多副本必须服务器原子认领／fence，不能只靠浏览器 Map。
- 明确新话题可创建新的反馈 episode，但不改业务 thread、未完任务和排队顺序。连续补充应使旧文案失效或用新 generation 校验，不能沿旧问题继续承诺；本轮推荐不为每次补充另发一个模型请求，只保留通用受理文字。
- 正式正文、可靠进度消息或错误已展示时，未发布 ack 不再展示；主链终态、撤权、取消、lease 丢失、视图切换和消息编辑都要取消相应反馈。网络断开只取消不再可投递的辅助输出，不擅自取消／重放正式任务。
- 发布前与消费前都核“这个反馈还属于当前 invocation 和 generation 吗”。仅 AbortController 不足以防已完成 HTTP 回调或已经排入 TTS 的迟到输出；正式正文先到时，无论辅助生成成功与否都丢弃。
- 用户新发言打断反馈朗读，不恢复旧音频队列；新发言是补充、steer 还是排队沿原产品规则，不把“取消 ack”误当“取消原业务”。
- 跨 Bot 切换后旧反馈可以在其原会话收口，但不得显示／发声到新 Bot。后台结果应保持原任务及 input_source，不成为用户冷启动，也不抑制当前不同用户请求的正确反馈。

### 5.2 显示、历史、上下文和 TTS

建议 ack 作为**原回合内有明确类型的短进度片段**，并由正式内容替换临时表现，不增加一个普通 assistant 回答／独立 turn。已显示的短反馈若需重连去重，保留最小状态 receipt（归属、id、generation、状态和时间）；具体文案是否持久保留可与现有 event/progress 存储契约统一，不能把永久历史塞进临时 WS 专属帧后宣称全端一致。

默认不进入正式模型上下文、memory、检索、标题、总结、任务完成判定；这不是删用户历史。若产品要求回看已说过的话，保留为类型化进度，组装模型上下文时明确排除该类型。不得用截取最后一条 assistant 的办法判断“正式答案已经到了”，要按 invocation 的正文类型。

TTS 必须先确定这类片段是否应读：用户开启自动语音回复时，若决定朗读 ack，只读实际显示且仍有效的短片段，复用同一 Bot 的当前音色及现有朗读协调器，不建第二个播放器。正式正文先到时阻止尚未开始的 ack 发声；若短 ack 已先发声，正文显示仍立即推进，音频由共享协调器在自然短句边界衔接，不能因正式结果到达而任意截断半个词。为等待短句导致的正文可听延迟必须计入指标，不能隐去。用户打断、撤权和取消始终立即停止，旧队列不恢复；被隐藏或切换会话的 ack 不读，重连／历史加载不重读。自动语音关闭时不因 ack 开启语音。

本轮推荐先把类型、generation、取消契约交给现有朗读 owner 对接，再启用语音 ack；在连续朗读与取消契约未被验证前不能承诺“无感语音反馈”。这不取代用户已提出的自动朗读及断续修复，也不修改其首个可听声音要求。固定短句音频缓存是成熟的可选后续优化，须按 provider/model/voice/locale/格式及版本作 key；本轮不预先合成收费缓存、不增加本地 TTS 服务。

## 6. 现有代码接入点（固定 cef 源码）

源码基线：[cefdf0cbb3207d2a07db17ef365e1b8d2d43ca3f](https://github.com/particleflow/Memoh/commit/cefdf0cbb3207d2a07db17ef365e1b8d2d43ca3f)。开始时准备的检出是旧 `851e8da4`；已单独 fetch 并建立本研究工作树，未改原工作树、未把旧版本当生产现状。

| 文件／符号 | 已核行为／接入建议 |
| --- | --- |
| [`local_channel.go`](../../../internal/handlers/local_channel.go)，`startWSStream`、`admitWSTurn`、`sendWSRunAccepted` | Web 主发送是 WS。成功 admission、fence 后先发 `run_accepted` 再执行 runner；duplicate 有独立返回。可信反馈起点可复用受理事件，不能在鉴权前生成收费 ack |
| [`send.ts`](../../../apps/web/src/store/chat/send.ts)，`createChatSend.sendMessage` | 创建 invocation 和 optimistic turn，发 WS；`await completion` 等整个回复。若在它结束后才发 ack 已太晚；不把辅助调用加到该 Promise 前面阻塞主链 |
| [`runtime-integration.ts`](../../../apps/web/src/store/chat/runtime-integration.ts)，`run_accepted` 分支 | 将 invocation 绑定 run/turn，处理首发显示；可消费类型化反馈并核归属，不用“当前页面 Bot”替代事件 Bot |
| [`runtime-projection.ts`](../../../apps/web/src/store/chat/runtime-projection.ts) | 最后活动助手片段即使空也保留运行状态；支持 snapshot/delta 投影。已有进度接缝可复用，但新类型须同时验证 live、snapshot 和历史，不只造 UI 假消息 |
| [`session_queue.go`](../../../internal/agent/application/session_queue.go)、[`memory_live_queue.go`](../../../internal/agent/runtime/session/memory_live_queue.go) | follow-up/steer 归属与 invocation 重放已有语义；它们是业务输入，不应把 ack 当新用户／助手任务入队。必须检查 Redis／持久恢复消费者同样语义 |
| [`service.go`](../../../internal/agent/application/service.go)，`resolveWithHTTPClient`、`buildBaseRunConfig`、`resolveRunReasoningConfig` | 解析有效主模型、思考和上下文；辅助应取冻结后的有效模型快照，用应用层短请求路径，不进入第二次正式 resolve/Chat。不得改 `runtime/native` 或官方 Agent 内核来实现等待文案 |
| [`service_model_selection.go`](../../../internal/agent/application/service_model_selection.go)，`selectChatModel`、`writeBackSessionModelPreference` | request > session preference > Bot default >历史；显式模型／effort 可写回会话偏好。辅助必须不触发写回，不能直接复用一条 `reasoning_effort=low` 的普通聊天调用 |
| [`resolve.go`](../../../internal/reasoning/resolve.go)、[`sdk.go`](../../../internal/models/sdk.go) | 现成能力 resolver 和 `ApplyReasoningToRequest` 映射可以复用；要额外验证实际辅助档位，不接受静默回落到高档作为成功 |
| [`service_title.go`](../../../internal/agent/application/service_title.go)，`generateWithTitleModel` | 已有短辅助 SDK 调用、凭据作用域和非流式 Generate 的模式供参考；当前使用标题模型及标题 timeout/retry，不可直接拿来冒充同主模型快回或继承重试策略 |
| [`input_source.go`](../../../internal/agent/application/input_source.go)、[`useChatReadAloud.ts`](../../../apps/web/src/pages/home/composables/useChatReadAloud.ts) | 可信 input_source 及可见正文朗读均已有边界。辅助输出需要同 Bot/session/source；新类型必须让主链 memory、正文和 TTS 过滤一致 |

外部 direct Codex／Claude 等 runtime 也有独立模型偏好与目录路径；不能拿 native SDK 的兼容结果代表 direct runtime 能进行同模型低思考辅助请求。不具备可授权调用契约时只显示受理状态，后续按实际 driver 能力适配，不偷偷改 runtime 或新开官方 Agent 会话。

**最小实施边界：**应用层反馈协调＋现有 event/projection 的类型化反馈＋现有模型 adapter 的短请求；按需扩共享 SDK 的类型契约并由原流程生成，不手写生成文件。不新增服务／自动化／管理面板，不改变业务队列锁、任务结果 receipt、权限、取消与历史兼容。需要持久 receipt 时优先审现有 invocation/event 存储能否承载；现有证据不足以宣称“无需迁移”。新字段须老消费者安全忽略，旧状态 unknown 不猜补；不能为回退删除字段、改 checkpoint 版本或恢复旧库。

## 7. 验收设计和指标定义

附属 [acceptance-cases.json](acceptance-cases.json) 是**设计输入**，不是已经通过的产品测试。它给出确定性事实、事件顺序和预期；实施后接入真实 admission→模型 adapter→WS→projection→DOM→TTS 链验证，不能只 mock 一个“快回函数”就算通过。

| 指标 | 起止／分母与要求 |
| --- | --- |
| TTFA-accepted | 用户发送事件到当前页面实际展示经服务端受理的反馈；本地 sending 另计，不能冒充受理 |
| TTFA-natural | 用户发送到符合事实、可见的自然 ack；仅冷候选统计，并同时报告未产生／正式答复先到／失败／超时分布，不能只挑成功样本 |
| 主模型 TTFT | 正式请求实际出站到第一个正式可见正文 token；另记首 SSE、首 reasoning token，不混算 |
| 端到端首正文 | 用户发送到 DOM 实际出现正式正文；包含排队、附件就绪、模型解析及网络；ack 不算正文 |
| 首个可听反馈／正文 | 发送或明确约定的语音触发到设备实际可听，分别归 ack／正文；网络首字节、PCM 入队或 AudioContext 创建不是可听证据 |
| 主链影响 | 相同场景、同模型／配置及并发分层下，有／无 ack 的 TTFT、端到端正文和完成时间分布；报告样本量、p50/p95、失败与删失，不用两次不同任务作因果证明 |
| 增量消耗 | 已发辅助请求数、成功／取消／失败、输入／输出／reasoning／cache token 和已核账户计价；未知 usage 单列。总增量含重试、网关内部回落、TTS 和取消后上游仍完成的调用 |
| 重复率 | 同一可信 invocation/episode 被实际展示超过一次自然 ack 的 episode 数／已展示 episode 数；连发新 invocation 不应伪装重试，需另报连发冗余率 |
| 热聊误触发 | 人工标注延续样本中出现多余自然 ack 的比例；冷沟通漏触发、uncertain 比例和标注分歧另报。不要把规则输出当真值 |
| 迟到／串身份／虚假动作 | 正式正文或取消后新展示 ack、跨 Bot/contact/session 输出、不真实“正在执行”及隐藏推理泄露均按独立事件计数；验收中不允许这些错误 |
| 主链隔离 | 辅助 400/429/timeout/异常输出、断网／取消不能阻止原正式 run 与队列终态；正式失败及时清理反馈，不滞留“正在处理” |

目标值不能从供应商营销或示例 sleep 复制。用户此前朗读首个可听声音≤500ms要求属于原朗读验收，仍按原触发定义实测；本轮新增文本自然 ack 没有获知独立数值 SLO，不擅自承诺同模型调用也能 500ms。先采无费用合成链的正确性，再在原授权范围内另行安排最少真实模型样本与对照，记录 TTFA 改善是否以主链退化为代价。

## 8. 分阶段安排、待确定项及阻塞

1. **当前完成：**一手资料／固定源码调查；风险和验收场景形成文档；未把任何产品项勾通过。
2. **可独立先做的后续开发：**确定性 eligibility/continuity 分类、类型化反馈与取消/去重、无推理网络的 fixtures、现有模型 adapter 的出站形状捕获、原 UI 真链核验。该阶段仍应由本新增目标 owner 单独承接，不争朗读／身份／后台 Bot 文件。
3. **启用同模型快回前：**唯一现场 owner 提供非秘密能力快照及实际 runtime；核查确切 Magpie 模型路由/effort 改写和限流机制。有资料仍未知时保留纯受理反馈，不能靠收费探测猜兼容。
4. **真实小样本验证后：**依据 TTFA、主链时延、成本和误触发选择策略数值，再由统一线组合，现场唯一 writer 发布与验收。不是本轮部署授权或当前已在运行。

真正需要产品取舍的是：自然 ack 在最终历史中是否可回看、自动语音开启时是否读 ack、模糊冷热时偏向少打扰还是更多接话，以及可接受的额外消耗／主链退化上限。推荐默认：类型化进度可追溯但不喂模型／memory；模糊时保留受理状态、不多说一句；语音先等共享取消契约通过再随原语音开关接入。可由用户现有偏好确定的部分不重复询问。

**准确阻塞／未验边界：**本工作树不含运行中的 NAS 模型配置；没有生产白名单快照，未能核实“当前实际同模型低／无思考”及 provider 并发上限。这是证据缺失，不是审批拒绝，也不阻塞本报告或合成验收设计。没有发送真实请求，故真实 TTFA／TTFT／费用和 TTS 连续效果均未知。另一次现有 reasoning 定向测试未运行：PATH 的 `go` 实为围棋游戏，返回 `Go: Unknown option: test`，详见[兼容登记 C13](../compatibility-incident-register.md#c13--path-go-names-a-board-game-not-the-go-toolchain)。没有安装替换工具链。没有启动 dev 或截图；本轮按上级明确只读调研、不开发部署的范围执行，文档不是产品修复验收。未申请额外权限、没有被拒后换路或不确定的业务写入。

## 9. 本轮验证范围与复用入口

静态核验：固定 cef 来源、本地所有代码相对链接存在、37 组验收素材结构／编号、文档差分及不含业务改动。结果见 [STATIC-CHECK.json](STATIC-CHECK.json)；可在仓库根目录离线执行 `python3 docs/testing/conversation-presence-20261030/check-design.py`，源码见 [check-design.py](check-design.py)。仅研究文档及既有兼容登记发生变化；现有 Go 测试未运行，不运行模型或真实 UI，静态检查不证明方案实现正确。以后发现能力／协议变化，在本报告修订日期及证据，不覆写旧兼容失败；工具／环境故障继续用既有兼容登记，不新建平行故障手册。
