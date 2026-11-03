# 手机端模拟行为验收（打包产物门禁）

本目录是用户 2026-11-02 裁定的「模拟验收门禁」执行工具：

> 手机端及一切真机可感知改动，必须先过模拟行为验收——驱动真实打包产物跑完整事件序列并断言可观测行为——通过后才准出包推送真机。

HAP41 的教训是组件测试全绿而快回、自动朗读、记忆污染三症状全数流到真机。因此本 harness **不读源码树、不跑 vitest**：它加载 `package_memoh_web.py` 写进 HAP 的 `rawfile/client-web`（实际将分发的 bundle），用脚本化本地服务端回放完整事件序列，只断言页面上可观测的行为与真实发出的请求。

## 用法

```bash
# 默认读取本仓打包目录
node mobile/client/tools/acceptance/acceptance.mjs

# 指定任意打包产物（同一份脚本即可验收候选包）
node mobile/client/tools/acceptance/acceptance.mjs /path/to/rawfile/client-web
```

- 退出码 `0` = 全部门禁场景真实通过；`1` = 有场景失败或未覆盖；`2` = harness 自身错误。
- 报告写到 `$ACCEPTANCE_REPORT_DIR`，未设置时写到系统临时目录 `memoh-acceptance-reports/`，文件名 `acceptance-<ms>.json`。报告含每场景状态、`speechTiming`（打包 bundle 自带的 `__memohSpeechTiming` 导出）、服务端 speech 请求序列、回放的流帧、页面 fetch 日志与错误。
- 依赖：Node ≥ 22（内置 `WebSocket`）、Chromium（默认取 ms-playwright 的 chromium，`ACCEPTANCE_CHROME` 可覆盖）、`ffmpeg`（生成本地静音 MP3 作为播放音频，不接触任何语音供应商）。无需额外 npm 安装。
- 前端 bundle 的 URL 校验拒绝 loopback，因此 harness 用 `stub.test`（Chromium 内解析到 127.0.0.1）作为 origin；stub 在 serve 时注入该 origin 到 `memoh_client.json`，磁盘上的打包文件不被修改。

## 模拟验收标准（最小集与断言粒度）

一次「完整事件序列」= 冷启动读历史 → 用户发送 → 服务端回放 ack/思考/正文/终态 → 用户对朗读做操作 → 页面进入后台/返回。断言只认三类证据：**页面可见文本**、**真实发出的 HTTP 请求（含方法/次数/顺序/时刻）**、**真实播放事件**。禁止以内联状态、组件 props 或源码结构作为通过依据。

| 编号 | 场景 | 时机 | 次数 | 顺序 |
| --- | --- | --- | --- | --- |
| E1 | 快回 ack 出现在 UI | 发送后 ≤ 3000ms | ack 行出现于流式期间 | 先于正文 |
| G1 | 首句完成自动开读 | 首句帧后 ≤ 1000ms 出现携带首句的 speech 请求，且早于末句帧 | 至少 1 个 task | 早于全段结束，≪ settle |
| G5 | 全段按序朗读 | — | 帧文本拼接覆盖首句与末句 | 首句索引 < 末句索引，无跳段 |
| G3 | 终态帧不吞剩余文本 | 终态后 ≤ 4s | ≥1 个 task | 末句仍被读出 |
| G4 | 音频提前 ended 不吞队列 | — | ≥1 个 task | 末句仍被读出 |
| G6 | 同文重读命中缓存 | 重读后 ≤ 3s | **新增 POST = 0** | 重读手势落在刚朗读完的那一行（最后一条回复行内的朗读控件） |
| G2 | 单任务失败只结束该任务 | 终态后 ≤ 6s | POST ≥ 2 | 被拒任务的正文在后续请求中整段重现；末句仍被读出且首句索引 ≤ 末句索引 |
| V2 | 思考默认折叠 | 流式期间 | — | — |
| V3a | 折叠/展开点击不停读 | 点击后 1.2s | pause 事件数不变 | — |
| V3b | 打字不停读 | 输入后 1.2s | pause 事件数不变 | — |
| V3c | 点击正在朗读的气泡停读 | 点击后 1.5s | pause 事件数增加 | — |
| V3d | 提交新消息停读 | 提交后 1.5s | pause 事件数增加 | — |
| G7 | 加载历史不朗读 | 历史渲染后 3s | speech 请求 = 0 | — |
| G8 | 后台/前台不乱不断 | 前台后 2s | 播放事件 ≥2 | 先停后恢复 |
| S1 | 追问认领后续段自动开读 | 续段首句帧后 ≤ 1000ms 出现携带续段首句的 speech 请求 | ≥1 个 task | 早于 settle，流式期间而非 settle 时 |
| S2 | 追问后下一轮全新回复仍开读 | 新一轮首句帧后 ≤ 1000ms | ≥1 个 task | — |
| bubble-kind-ack | 快回为轻量气泡 | ack 单独在屏期间 | 1 个 `data-bubble-kind=ack` | 文本恰为 ack 行；虚线描边+浅底（浅/深色主题）；正文到达后消失 |
| bubble-kind-body | 正文保持标准气泡 | settle 后 | 1 个 `data-bubble-kind=body` | 无描边、无底色；行内无 ack/receipt |
| bubble-kind-receipt | 回执轮为另一种气泡 | 流式与 settle 后 | 1 个 `data-bubble-kind=receipt` | 左缘 3px 色条+淡强调底；文本与同一回复的正文气泡完全相同；三种形态两两不同（浅/深色主题） |

G1 的时机起算点是**首个完整句子的帧**（脚本化模型的 `text_chunk_ready`），不是按键时刻；口径与 PR #121 `speech-field-acceptance.test.ts` 六门一致，差別只在于本 harness 驱动打包 dist 而非组合树。

G1 回放**长回复**（`FAIL_TASK_BODY`，6 句、句间 800ms、末句后 3s 才 settle），断言「携带首句的 speech 请求时刻 − 首句终止符到达时刻 ≤ 1000ms，且早于末句帧、≪ settle 时刻」：只会 settle 才开读的产物在 4 句短回复上可以把 2.8s 的 settle 时刻误算成首句时延（TF-18 坐实过），长回复把「首句即读」与「settle 才读」拉开到秒级可分辨。该口径与 #126 ③级（首句帧→首请求）一致，供 ③级复用。快回 ack 以其自己的短任务先读（stub 对 ack 行回放短音频），不计入首句时延的起算或终点。

S1/S2 复现「流式中途追问后不再自动朗读」（HAP42 现场，2026-11-03）。追问走真实入队协议：客户端在流式期间提交的不是 websocket `message`，而是 `POST /bots/{bot}/sessions/{sid}/follow-up-queue`（stub 按服务端 `sessionQueueResponse` 形状回放队列与 `/queue` 读回）。stub 的脚本在下一句边界执行认领，帧形状对齐 PR #131：`steer_turn_upserts`（认领项命名其新开 turn，锚在已提交文本块之后）与 `user_turn_upserts`（被认领输入以 UITurn 随帧发布）同帧下发；后续正文以新文本块续流（`STEER_BODY`，专用句子以便与被打断的首段区分），settle 落四条持久化 turn。断言：提交追问按 V3d 停掉首段朗读后，**认领续段仍按首句即读**（首句终止符后 ≤1000ms 出现携带续段首句的 speech 请求，早于 settle）；HAP42 真包上该断言红（续段首请求晚 5474ms、落在 settle 前 27ms，即流式期间完全静默、settle 才读）。S2 断言追问 settle 后的**全新一轮**回复仍按 G1 口径开读（HAP42 上本就绿，用于钉住修复不回归该路径）。

未驱动的断言（例如打包版本里找不到思考折叠控件）报告为 `uncovered`，**同样让门禁失败**——没有证据不等于通过。

三种气泡场景只认计算样式与 DOM 文本：每个形态元素不得带 `title`/`aria-label`/tooltip 属性或伪元素文字，自身不得有直接文本节点（ack 除外，恰为 ack 行）。回执轮由 stub 的 `receipt` 模式回放：该轮每一行带 `task_result` 输入源，且不发快回（服务端对 task_result 输入不出 ack）。

## 已知红项与修复线

当前（review/harmony-image-timing-20261026，含 #118 / #121 / #123 / #126 / #128 / #132 与 TF-18 修复）：14 个场景在打包产物上真实通过。TF-18 修复前，打包 bundle 上首个 speech 任务只在回复 settle 时才开（`run_accepted` 给乐观回复行盖 `runtimeRunId` 被误判为换 run，回复被移出 live 集合），G1 的 2.8s 恰等于 stub 的 settle 时刻——G1 绿只因回复短；加强后的长回复场景在旧包上真实变红（首句请求晚 7089ms、晚于 settle），修复后 30ms 内开读。

场景之间必须真正隔离：stub 在 `/__control/reset` 后放弃上一场景仍在回放的脚本（不再发帧、不再写入该会话的持久化 transcript）。否则上一场景的回复会在下一场景的终态刷新里作为第二条同文回复出现，G6 的重读手势会落到这条页面从未合成过的行上，把「页内缓存未命中」误报成产品缺陷。

G2 使用 stub 的长正文（`FAIL_TASK_BODY`，6 句）：第一个**正文**任务被 503 拒绝（ack 的独立短任务不参与），断言被拒任务的正文在后续任务中整段重现，并且正文末句仍被按序读出。

## 边界

- 不连生产 NAS；不读写任何真实会话、凭据或用户数据。
- 不重写、不补丁化被测 bundle；stub 只回放脚本化事件与协议响应。
- 不改 `docs/PLAN.md` 的门禁总则；本文件是执行细则与场景清单。
