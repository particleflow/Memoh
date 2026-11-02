# 模拟器验收（门禁第 ③ 级）

本目录是模拟验收门禁的第 ③ 级执行工具：在 Windows 上由 DevEco
模拟器运行完整 HAP（ArkUI 壳 + ArkWeb + 原生面），自动驱动并断言。第 ② 级是
Chromium 打包产物 harness（PR #123），第 ④ 级是真机。

两级共用同一份脚本化本地服务端 `stub-service.mjs`（#123 交付）与同一套场景，
区别只在被驱动的运行环境：

| 级别 | 运行环境 | 覆盖 |
| --- | --- | --- |
| ② | Chromium + 打包产物 | 页面逻辑、真实 HTTP/WS 时序、媒体事件 |
| ③ | DevEco 模拟器上的 HAP | 以上 + ArkUI 壳、ArkWeb 资源拦截、跨源 fetch、ability 生命周期、真实音频播放 |
| ④ | 真机 | ③ 覆盖不到：蜂窝网络、真实网络保真、音频焦点、ArkWeb 绑网 |

## 组成

- `acceptance-hap.py`：生成**验收口味**构建副本与签名包。分发包必须保留 TLS
  要求并关闭 ArkWeb 调试；模拟器 harness 两者都需要放宽。脚本只改副本，不回流
  分布树；签名用 SDK 自带的 OpenHarmony 测试证书，不是生产华为证书。
- `stub-host.mjs`：包装 #123 的 `stub-service.mjs`，只补模拟器需要的三处适配
  （下节）。**不修改 #123 已评审文件。**
- `emulator.mjs`：驱动与断言。退出码 `0` = 全部场景真实通过，`1` = 有失败或
  未覆盖，`2` = harness 自身错误。

## 为什么必须多一层 stub-host（只有模拟器级能测出）

1. **CORS**：第 ② 级把 bundle 从 stub 自己的 origin 提供，API 调用同源。手机上面板
   origin 是 `begonia-client://app`，所有 API 都跨源，ArkWeb 会强制 CORS。缺
   `access-control-allow-origin` 时语音请求直接 `Failed to fetch`——Chromium 同源
   场景永远测不出。包装层给每个响应补该头。
2. **流 epoch**：第 ② 级每个场景新建浏览器上下文，runtime 从 seq 0 重来不可见。
   手机上客户端跨重启保留 cursor，把重放的 seq 判为过期而**静默丢流**。真实服务端
   开新流换 epoch，包装层按场景边界推进 epoch。
3. **登录**：第 ② 级直接往 localStorage 种 token。手机上 harness 无法在文档启动前
   种存储，所以包装层应答打包登录表单实际发出的 `POST /auth/login`（响应体必须是
   SDK `HandlersLoginResponse` 形状：`access_token` + `user_id`，`{user, token}` 会被
   登录页判为「服务器返回异常」）。

## 一次性准备

```bash
# 1. 模拟器与设备
#    实测：devecocli emulator list 能看到实例，但 start 会报
#    "system image ... cannot be found"，因为它读的是 %LOCALAPPDATA%\Huawei\Sdk
#    （该目录下只有空的 HarmonyOS-7.0.0）。本机可用镜像在项目内保留目录，
#    所以启动必须显式给 -imageRoot（脚本默认值指向该目录，可用环境变量覆盖）：
Emulator.exe -start "Mate 90 Pro" \
  -imageRoot F:/NAS-Assistant/validation/harmony/.build/emulator-images \
  -bootMode coldboot
hdc tconn 127.0.0.1:5555
hdc -t 127.0.0.1:5555 shell bm get --udid     # 记下 UDID

# 2. 验收构建副本 + 签名包
python mobile/client/tools/acceptance/acceptance-hap.py \
  <unsigned.hap> F:/NAS-Assistant/tmp/emu 18081 <UDID>

# 3. 安装
hdc -t 127.0.0.1:5555 install -r F:/NAS-Assistant/tmp/emu/acceptance-signed.hap
```

`-appCertFile` 必须是 leaf → sub CA → root 的完整链（`generate-app-cert
-outForm certChain` 的产物）；单张证书会报 `Profile cert must a cert chain`。

## 每次运行

```bash
export ACCEPTANCE_CLIENT_WEB=F:/NAS-Assistant/tmp/emu/app/entry/src/main/resources/rawfile/client-web
node mobile/client/tools/acceptance/stub-host.mjs      # 终端 1
node mobile/client/tools/acceptance/emulator.mjs       # 终端 2
```

环境变量：`ACCEPTANCE_REPORT_DIR`、`STUB_PORT`、`ACCEPTANCE_SERIAL`、
`ACCEPTANCE_HDC`、`ACCEPTANCE_CDP_PORT`、`ACCEPTANCE_DEVICE_HOST`。

## 场景与断言（最小集）

证据只认三类：页面可见文本、服务端实际收到的请求、打包 bundle 自己暴露的朗读
状态（`data-speech-state`）。不注入时间线插桩——app 在本 harness attach 之前就
启动，注入的钩子会漏掉启动期调用而报假阴性。

| 编号 | 场景 | 断言 |
| --- | --- | --- |
| E1 | 发送后快回 ack | ≤3000ms 出现在真实 App UI |
| G1 | 首句自动朗读 | 首句完成后 ≤4000ms 出现首个 speech POST |
| G5 | 全段顺序朗读 | 首句索引 < 末句索引，无跳段 |
| V2 | 思考块默认折叠 | `aria-expanded=false` |
| V3b | 打字不打断 | 朗读状态仍为 playing |
| V3c | 点击气泡停止 | 状态由 playing 变非 playing |
| V3d | 提交新消息停止 | 同上 |
| G7 | 加载历史不朗读 | 历史渲染后 speech 请求 = 0 |
| G8 | 后台/前台不误断 | 回前台后恢复 playing |

未驱动到的场景报 `uncovered` 并使门禁失败——没有证据不等于通过。与 #123 同口径：
`#118`/`#121` 未合并导致的红是**预期红**，不是本 harness 的问题。

## 每场景隔离（设备级冷态）

`aa force-stop` → 等进程真正退出 → `bm clean -n <bundle> -d -c` → `aa start` →
**显式等该场景自己的 `runtime_subscribe`**（不靠固定 sleep）→ 等服务 quiet →
经 CDP 种状态 → 注入事件 → 复检页面可见且本应用持有窗口焦点。

这些步骤全部在 `coldStart()` 里；未订阅直接抛错（fail-fast，不空等）。
可见性判据见下文「稳定性」。

## 已解卡点（2026-11-02）：「配置 + token 同时存在」停在配置页

原卡点收敛错误。真实原因链（三层，都已修复在本目录工具内）：

1. **bundle 等长补丁语法错**（主因）。旧补丁把
   `if(o=t===10||...,` 改写为 `if(o=(t===10||...)&&a!==\`10.0.2.2\`,`——按
   Esprima 语义，函数表达式开头的 `(` 让后面的 `a!==...` 被解析为**函数调用实参**
   （cover grammar），该函数返回模板串（callable 对象），运行时抛
   `TypeError: not callable`。该 TypeError 正是 `boot()` 捕获后展示
   「启动或本地配置读取失败」的来源：`readServiceConfig` 与规范化器从未拒绝
   `http://10.0.2.2:18081`（`10.0.2.2` 不是 RFC1918 私网，无需豁免）。
   现补丁把 IPv4 LAN 判定的首段常量 `t===10` 改为 `t===-1`（不可达常量，等长），
   语义上更贴近「10/8 不再算 LAN」。
2. **原生门与调试开关在 ABC 里**。分发包 `setWebDebuggingAccess(false)` 且
   `ServiceUrl.ets` 的原生规范化器在 CDP attach 前就跑（写配置时）。现在的
   `acceptance-hap.py` 用 `ark_disasm` 定位两处指令操作数做等长改写：
   `ldfalse`→`ldtrue`（调试开关）与 `ldai 0xa`→`ldai 0xffffffff`（原生侧同一
   `first===10` LAN 项），并重算 panda 头 adler32。不再要求重新编译工程，也
   不再碰组合树。
3. **登录两坑**。冷启动后 `boot()` 完成前登录表单是旧文档的（提交无效果）；
   harness 现在等 `#harmony-service-settings`（boot() 成功后才追加）出现再填表。
   另外旧 stub 应答 `{user, token}` 不是 `HandlersLoginResponse` 形状，登录页判
   「服务器返回异常」。

### 复现（当前版）

```bash
Emulator.exe -start "Mate 90 Pro" -imageRoot F:/NAS-Assistant/validation/harmony/.build/emulator-images -bootMode coldboot
hdc tconn 127.0.0.1:5555 && hdc -t 127.0.0.1:5555 shell bm get --udid
python mobile/client/tools/acceptance/acceptance-hap.py <分发包原始 unsigned HAP> F:/NAS-Assistant/tmp/emu 18081 <UDID>
hdc -t 127.0.0.1:5555 install -r F:/NAS-Assistant/tmp/emu/out/acceptance-signed.hap
# 终端 1：ACCEPTANCE_CLIENT_WEB=<打包 client-web 目录> node stub-host.mjs
# 终端 2：node emulator.mjs
```

输入必须是分发包自己的原始 unsigned HAP（pristine anchor 会拒绝已打过补丁的
副本）。

## 当前红绿（2026-11-02，Mate 90 Pro 模拟器实测）

报告：`acceptance-emulator-*.json`（每次全量 9 场景落一份）。

| 场景 | 结果 | 证据与说明 |
| --- | --- | --- |
| E1 快回 ack | 绿 | ack 在发送后 ~0.9s 可见，距 ack 帧 ~100ms |
| G1 首句开读 | 绿 | 首句完成后 ~2.9s 出现首个 speech POST |
| G5 全段按序 | 绿 | 单个 POST 覆盖全段四句、顺序正确 |
| V2 思考默认折叠 | 红（**预期红**，#118 未合并） | 打包 bundle 的「已思考」按钮无 `aria-expanded`，无折叠控件标记 |
| V3b 打字不停读 | 绿 | playing=true → 打字后仍 playing |
| V3c 气泡点击停读 | 绿 | playing=true → 点击后 idle |
| V3d 提交停读 | 绿 | playing=true → 提交后 idle |
| G7 历史不朗读 | 绿 | 历史渲染且 speech=0 |
| G8 后台/前台 | 绿 | playing → 后台 paused → 回前台 playing |

**稳定性（已修，2026-11-02）**：此前活动回复类场景（E1/V3b/V3c/V3d/G8）在连续
全量运行间结果翻转，而逐场景单流程探针稳定绿。**根因不是会话衔接，是页面可见性**：

- ArkWeb 在 `document.visibilityState !== 'visible'` 时停止页面的渲染与观察者回调。
  该状态下 stub 的帧照常到达（服务端日志齐全），但 `innerText` 不回流、自动朗读
  不发起——正是「帧到了、页面不动」的外观。
- 该镜像上小艺输入法**首次运行**弹出全屏向导（隐私声明 + 「请选择中文键盘布局」），
  是独立窗口且抢走窗口焦点（`hidumper -s WindowManagerService -a '-a'` 的
  `Focus window` 指向 `inputmethod0`）。登录页 `INPUT#username` 一填表就拉起输入法，
  向导随后盖住应用，页面转入 hidden。向导数据属于输入法自身，`bm clean -d/-c`
  清不到，`aa force-stop` 重启也带着，所以每一场景都会重现，且取决于触发时机
  而时有时无——这就是翻转。
- 判据：`ensureVisible()` 同时要求 `document.visibilityState === 'visible'` 与
  `Focus window` 属于本应用的窗口（`workassistant0`）。未满足时发
  `uitest uiInput keyEvent Home` + `aa start`（不重启进程，文档与流 cursor 保活），
  最多等 40s，超时即 fail-fast 抛错，不空跑。`coldStart()` 在填表后、提交前、
  quiet 后各校验一次；`typeAndSend()` 在 Enter 之后复检一次。任何一次不满足都抛
  harness 错误，不会落成产品红。
- 附带修掉一处计时错误：E1 原先取「最新 `seq===1` 帧」做基准，而帧日志跨场景累积
  （stub 是同一进程、`seq` 每场景从 0 重来），于是量到上一场景的帧（实测
  `ackAfterFrameMs=274368`）。现在只取 `frame.at >= sentAt` 的最早一帧。

复跑判据：**同一提交连跑 5 次全量，非预期红场景必须 5/5 一致绿**；V2 为 #118
未合并的预期红，不计入翻转。

## 验收构建的偏差（只在副本，生产零改动）

1. 打包 bundle：IPv4 LAN 判定首段 `t===10` → `t===-1`（等长，10/8 不再算 LAN）。
2. `ets/modules.abc`（`ark_disasm` 定位、等长操作数 + adler32 重算）：
   `setWebDebuggingAccess(false)` 的 `ldfalse`→`ldtrue`；原生 `ServiceUrl.ets`
   同一 `first===10` 项的 `ldai 0xa`→`ldai 0xffffffff`。
3. rawfile seed：`apiOrigin` 指向模拟器网关（槽位等长填充）。

`WebResourceManifest.ets` 只登记 `{mime, bytes}`（长度，无哈希），三处补丁全部
等长，原生资源门原样通过；分发包不含以上任何一条。

## 模拟器覆盖不到、必须真机首验

- 蜂窝网络下的连接建立与切换、真实网络保真（丢包/时延/分片）
- 音频焦点与系统级中断（来电、其它 App 抢占、蓝牙切换）
- ArkWeb 绑网（`NativeWebNetwork` 选网）在真实双栈环境的行为
- 真实设备的屏幕密度/安全区差异、系统输入法与键盘实际行为
- 省电策略与后台被杀的实际回收时机
- 推送通知、真实 NAS 公网入口（IPv6/DDNS/TLS）可达性
