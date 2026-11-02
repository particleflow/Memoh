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
| G1 | 首句完成自动开读 | 首句帧后 ≤ 4000ms 出现 speech POST | 至少 1 个 task | 早于全段结束 |
| G5 | 全段按序朗读 | — | 帧文本拼接覆盖首句与末句 | 首句索引 < 末句索引，无跳段 |
| G3 | 终态帧不吞剩余文本 | 终态后 ≤ 4s | ≥1 个 task | 末句仍被读出 |
| G4 | 音频提前 ended 不吞队列 | — | ≥1 个 task | 末句仍被读出 |
| G6 | 同文重读命中缓存 | 重读后 ≤ 3s | **新增 POST = 0** | — |
| V2 | 思考默认折叠 | 流式期间 | — | — |
| V3a | 折叠/展开点击不停读 | 点击后 1.2s | pause 事件数不变 | — |
| V3b | 打字不停读 | 输入后 1.2s | pause 事件数不变 | — |
| V3c | 点击正在朗读的气泡停读 | 点击后 1.5s | pause 事件数增加 | — |
| V3d | 提交新消息停读 | 提交后 1.5s | pause 事件数增加 | — |
| G7 | 加载历史不朗读 | 历史渲染后 3s | speech 请求 = 0 | — |
| G8 | 后台/前台不乱不断 | 前台后 2s | 播放事件 ≥2 | 先停后恢复 |

G1 的时机起算点是**首个完整句子的帧**（脚本化模型的 `text_chunk_ready`），不是按键时刻；口径与 PR #121 `speech-field-acceptance.test.ts` 六门一致，差別只在于本 harness 驱动打包 dist 而非组合树。

未驱动的断言（例如打包版本里找不到思考折叠控件、或整段正文被客户端合并进同一个 task 导致「失败后剩余文本」不可观测）报告为 `uncovered`，**同样让门禁失败**——没有证据不等于通过。

## 已知红项与修复线

当前（review/harmony-image-timing-20261026，未含 #118 / #121）：

| 场景 | 现状 | 修复线 |
| --- | --- | --- |
| V2 思考默认折叠 | 红：打包 bundle 的思考头无 `aria-expanded`，流式期间展开 | PR #118 |
| V3a 折叠点击不停读 | 红：点击思考头触发 pause | PR #118 |
| G6 同文重读零新 POST | 红：重读重新申请合成（2→4 次请求） | PR #121 |
| G2 单任务失败后继续读 | uncovered：打包客户端的合句节奏使整段进入首个 task，失败后无后续可观测 | PR #121 组合树门 |

其余场景在打包产物上真实通过。

## 边界

- 不连生产 NAS；不读写任何真实会话、凭据或用户数据。
- 不重写、不补丁化被测 bundle；stub 只回放脚本化事件与协议响应。
- 不改 `docs/PLAN.md` 的门禁总则；本文件是执行细则与场景清单。
