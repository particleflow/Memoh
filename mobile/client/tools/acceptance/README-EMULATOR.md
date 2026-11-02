# 模拟器验收（门禁第 ③ 级）— WIP

**状态：harness 自身尚未跑完，本目录不构成可用门禁。** 见文末「确切卡点」。

本目录是 `docs/PLAN.md` 模拟验收门禁的第 ③ 级执行工具：在 Windows 上由 DevEco
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
   种存储，所以包装层应答打包登录表单实际发出的 `POST /auth/login`。

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
经 CDP 种状态 → 注入事件。

这些步骤全部在 `coldStart()` 里；未订阅直接抛错（fail-fast，不空等）。

## 确切卡点（WIP 原因）

**阻塞点：冷启动后「配置 + token 同时存在」时，页面报“启动或本地配置读取失败”，
停在配置页，后续所有场景无法开始。**

已确认的事实（可复现）：

1. 打包 seed（`memoh_client.json` 的 `apiOrigin`）能自动写入配置——清空 storage
   后 reload，`localStorage['memoh:harmony:service-config:v1']` 变成
   `{"version":1,"apiOrigin":"http://10.0.2.2:18081",...}`，页面进入 `#/login`。
2. 此时再写 `token` 并 reload，页面从 `#/login` 转到
   “启动或本地配置读取失败”并停在配置页；`GET /users/me` 曾成功一次（200），
   说明网络已通、token 被接受，失败点在 `readServiceConfig` 的本地校验。
3. 同时写入配置与 token（不经 seed）同样失败。把 origin 换成公网
   `http://example.test:81` 也失败——**与地址是否私网无关**，是读取路径本身。
4. 原生侧已放开：`client.configure` 经 CDP 调用返回 `{"ok":true}`（ABC 内的
   `ServiceUrl.ets` 补丁生效），说明卡在 bundle 侧的校验器。
5. 第 ② 级之所以没有此问题：它直接种 `localStorage` 且**从未加载 seed 与配置同时
   存在**的状态。

未完成的定位：需要确认 `readServiceConfig` 抛出的具体错误码
（`SERVICE_CONFIG_INVALID` / `DEPLOYMENT_CONFIG_INVALID` /
`DEPLOYMENT_LEGACY_INVALID`），以及打包 bundle 的 URL 规范化器对我打入的等长补丁
（IPv4 分支排除网关）返回的 `origin` 是否与写入值逐字节相等。

### 复现步骤

```bash
hdc -t 127.0.0.1:5555 shell "aa force-stop cloud.begonia.workassistant; sleep 2; \
  bm clean -n cloud.begonia.workassistant -d; aa start -a EntryAbility -b cloud.begonia.workassistant"
# 取 webview_devtools socket → hdc fport → CDP attach
# 1) localStorage.clear(); location.reload()  → 进入 #/login（seed 已生效）
# 2) localStorage.setItem('token','acceptance-token'); location.reload()
#    → 停在配置页，提示“启动或本地配置读取失败”
```

## 验收构建的三处偏差（只在副本，生产零改动）

1. `SharedWebPolicy.ets`：`setWebDebuggingAccess(false)` → `true`（CDP 驱动需要）。
2. `ServiceUrl.ets`（原生）：TLS 门放行 `10.0.2.2`。
3. 打包 bundle：IPv4 LAN 判定排除 `10.0.2.2`；`WebResourceManifest.ets` 必须与
   实际字节**逐一同步**——差 1 字节原生资源门即拒载、页面全白。

分发包不含以上任何一条。

## 模拟器覆盖不到、必须真机首验

- 蜂窝网络下的连接建立与切换、真实网络保真（丢包/时延/分片）
- 音频焦点与系统级中断（来电、其它 App 抢占、蓝牙切换）
- ArkWeb 绑网（`NativeWebNetwork` 选网）在真实双栈环境的行为
- 真实设备的屏幕密度/安全区差异、系统输入法与键盘实际行为
- 省电策略与后台被杀的实际回收时机
- 推送通知、真实 NAS 公网入口（IPv6/DDNS/TLS）可达性
