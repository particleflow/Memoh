# 定时结果可信归属与持久回流增量

提交：`1e909d33f99a3a793dc4102ad720d4b7abdb230c`；基于 c4，独立增量交统一线组合。不要整分支合入；不挡住 694 的既有发布。

## 精确基线

已通过本轮官方 prepare/materialize 流程取得 Library v13，包 SHA `d9724fc0035937823dc0e55666bf88100a3db2e59394c519252cdf999942e19b`，导入包内原 Git bundle 的精确 959/694 对象。15 个限定路径中 12 个 blob 完全相同；其余为 task_result_delivery 的 lint 调整与测试整理，没有定时结果回流。逐项 blob 见 baseline-comparison.json。以前 download 流程 tools/list 401 不被冒称修好；本轮新 prepare 流程成功，无手拼下载地址。

## 本次实现

- `cmd/internal/core/schedule_result_delivery.go:49` 产品原生定时工具适配：仅 Web/chat 固定 contact 创建时，从受信 SessionContext + 当前权限得到原请求人、Bot、主聊、team/workspace 和回程路由。模型参数与公开 HTTP CreateRequest 无法填来源；执行工具时重验，描述符生成后的 contact 变化会拒绝。
- `internal/schedule/service.go:173` 将来源与计划同次写入；每次 fire 在日志中复制来源，以原请求人身份执行。创建、修改执行目标与实际执行均禁止把已 opt-in 的计划固定到主聊执行；权限失效不触发，留下失败日志。旧计划没有来源保持原路径。
- `cmd/internal/core/schedule_result_delivery.go:160` 仅按 `schedule:<planID>:<fireID>` 查精确 run。终态、结果记录、已向用户汇报分开保存。使用现有队列/内部来源/摘要回执，不调用原任务。迟到日志不能把 lost/unknown 洗成成功或失败，也不另造摘要事件。
- `cmd/internal/core/task_result_delivery.go:107,174` 接现有启动/终态唤醒扫描；`schedule_logs.result_delivery` CAS 持久保存 summary receipt。结果摘要仍由既有主 Bot 提炼，未改官方 Agent 核心、调度服务或任何界面。

## 证据和范围

新增 4 项领域测试（含子项）：公开 JSON 无法冒充来源；原请求人不同于 Bot owner；旧记录不 opt-in；撤权/pinned 执行拒绝且保留失败；修改不能固定回主聊。

真实 PostgreSQL 专属 C1/C2/C3 测试：连接池及 host 重建后，已保存结果继续同 receipt；进度后失败、空最终回复不消费结果；最终摘要由原 fenced message writer 落盘后丢确认，8 路并发恢复无重复发布。另覆盖精确 fire/run、删除计划留原结果、legacy 不回流、unknown/失败不丢、迟到 callback 不改结果、contact 改绑和用户停用拒绝执行授权及回流准备。此处重建 host/连接池，不冒称杀进程验证；summary submit 为捕获器，模型调用为零。

差分红：仅用 Go overlay 将共享 dispatch 换回 c4 原文件，保留新 fixture；同一 PostgreSQL 测试在 schedule_fire 入口被拒绝。绿：恢复候选 dispatch 后通过。不是声称对完整旧版本跑新 API 测试。

当前候选 binary 本地启动成功；真实 Chromium 在隔离克隆库验证主聊刷新仍同一 ID、已有摘要一次、输入框和 20 项目录正常，无页面异常、无任务/模型提交。此为现有展示回归 smoke，不是定时模型到 UI 的端到端验收。截图只留本地内部检查，不在交付包中。

新增差分 lint、生成 sqlc、编译、diff check 均通过。未重跑旧大套测试。

## 迁移与组合

181 仅添加 schedule.result_origin、schedule_logs.result_origin/result_delivery 三个 JSONB 列，默认空对象；同时同步 0001 snapshot。必要性是 target_session_id 只表示执行位置，无法作为原请求人/固定 contact 或交付回执。

仅在隔离 `memoh_schedule_result_20261029` 应用。down 有意保留新增列与历史（SELECT 1），遵守本任务历史保全要求；不是破坏性降级。旧 consumer 忽略新增列；生产升级/迁移未执行。回滚停用新 consumer，保留原记录，不能重投执行。隔离 down/up 检查总消息 335 不变，原数据库 contact 仍 114 条；此次迁移保全计数没有活跃 plan/log 样本，不能冒称有数据回执回滚测试。

统一组合须保留 694 的 task_result_delivery lint 改动，仅应用本提交增量，按统一当前 schema 重新生成 sqlc，并检查 181 编号尚未被其他未交接候选占用。

## 明确剩余边界

1. fire 日志已有但尚无 admitted run 就崩溃：本切片不猜执行结果，仍 pending/目录保留，不自动重执行；需要后续同一恢复闭环。
2. due callback 原子领取、暂停后旧 callback、忙时计次/排队未改，本轮不扩大调度器。
3. REST、其他渠道、旧计划不自动 opt-in；没有来源不能猜原用户。外部 runtime 真执行与生产/HAP 未验。
4. 三次摘要失败沿既有 attention 状态保留，没有新增管理面板；真实主 Bot 模型提炼与定时执行期间继续闲聊的定时专属端到端还未验。
5. 当前不受读取权限阻断；没有部署、实际计划创建、时区/权限现场修改、历史重投或收费调用。

包内 manifest.json 列出除自身外全部文件 SHA256/大小；source/ 是本提交 19 个文件，bundle 与 patch 为可组合增量。脚本依赖既有私密隔离 config/auth，未包含这些文件、DSN 或 token。
