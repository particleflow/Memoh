import { installHarmonyBack } from './harmony-back'
import { SERVICE_CONFIG_KEY, EDIT_SERVICE_KEY, inspectServiceSetup, readDeploymentConfig, applyDeploymentConfig, deploymentNetworkPolicy, saveServiceConfig, finishServiceTransition, commitServiceTransition, editServiceConfig, type ServiceConfig } from './harmony-service-config'
import type { TransferNetworkPolicy } from './harmony-service-url'
import './harmony-service.css'

type Bridge = { request: (raw: string) => Promise<string> }
const native = (window as Window & { begoniaNative?: Bridge }).begoniaNative
async function nativeCommand(command: string, apiOrigin?: string, networkPolicy?: TransferNetworkPolicy) {
  if (!native) {
    if (location.protocol === 'begonia-client:') throw new Error('SERVICE_BRIDGE_UNAVAILABLE')
    return // Explicit browser fixture/preview, never a native fallback.
  }
  const result = JSON.parse(await native.request(JSON.stringify({ version: 1, command, apiOrigin, networkPolicy }))) as { ok?: boolean }
  if (!result.ok) throw new Error('SERVICE_BRIDGE_REJECTED')
}

installHarmonyBack()

let started = false
let packagedConfig: unknown
async function boot(config: ServiceConfig) {
  if (started) return
  started = true
  try {
    if (config.resetPending) {
      // Reuse the official auth cleanup; do not invent another cache/draft list.
      const { clearPersistedUserScopedState } = await import('./auth-session')
      clearPersistedUserScopedState()
      const { ONBOARDING_KEYS } = await import('../pages/onboarding/constants')
      for (const key of Object.values(ONBOARDING_KEYS)) { localStorage.removeItem(key); sessionStorage.removeItem(key) }
      finishServiceTransition(config)
      config = commitServiceTransition(config)
    }
    await nativeCommand('client.configure', config.apiOrigin, deploymentNetworkPolicy(packagedConfig, config.apiOrigin))
    await import('../main')
    const { default: router } = await import('../router')
    installHarmonyBack(router)
    // Login must also offer a way to correct an unavailable endpoint.
    const button = document.createElement('button')
    button.id = 'harmony-service-settings'; button.textContent = '服务地址'; button.type = 'button'
    button.onclick = editServiceConfig
    document.body.append(button)
    const update = () => { button.hidden = router.currentRoute.value.name !== 'Login' }
    router.afterEach(update); update()
  } catch {
    started = false
    form(config, '启动或本地配置读取失败。未连接服务，请检查配置后重试。')
  }
}

function form(current: ServiceConfig | null, message = '') {
  const root = document.getElementById('app')!
  root.innerHTML = '<main class="service-setup"><form><h1>连接服务</h1><p>地址仅保存在这台设备。API 和安装包下载地址分别设置。</p><label for="service-api">API 地址</label><input id="service-api" type="url" required autocomplete="off" spellcheck="false" maxlength="2048" inputmode="url"><p class="service-hint">填写完整 HTTP(S) 地址和 /api 路径。HTTP 传输不加密。</p><label for="service-download">安装包下载地址（可选）</label><input id="service-download" type="url" autocomplete="off" spellcheck="false" maxlength="2048" inputmode="url"><p id="service-migration"></p><p role="alert" id="service-error"></p><button type="submit">保存并继续</button><button type="button" id="service-cancel">返回</button></form></main>'
  const api = document.getElementById('service-api') as HTMLInputElement
  const download = document.getElementById('service-download') as HTMLInputElement
  const error = document.getElementById('service-error')!
  api.value = current?.apiOrigin ?? ''; download.value = current?.hapDownloadUrl ?? ''; error.textContent = message
  let unbound = false
  try { unbound = inspectServiceSetup().kind === 'unbound' } catch { unbound = true }
  document.getElementById('service-migration')!.textContent = current
    ? '保持 API 地址不变会保留登录；切换服务将退出当前账号。'
    : unbound ? '检测到旧登录或本地数据。请使用已在 Windows 配置并签名的安装包完成迁移；原数据未更改，不会发送到其他地址。' : '本安装包尚未提供服务配置；Windows 配置并签名后的版本会自动读取。也可在此设置新服务。'
  const submit = root.querySelector('button[type=submit]') as HTMLButtonElement
  submit.disabled = unbound
  const cancel = document.getElementById('service-cancel') as HTMLButtonElement
  cancel.hidden = !current
  cancel.onclick = () => { sessionStorage.removeItem(EDIT_SERVICE_KEY); location.reload() }
  root.querySelector('form')!.onsubmit = async event => {
    event.preventDefault()
    submit.disabled = true
    try {
      saveServiceConfig(api.value, download.value)
      sessionStorage.removeItem(EDIT_SERVICE_KEY)
      location.hash = '#/login'
      location.reload()
    } catch (failure) {
      error.textContent = failure instanceof Error && failure.message === 'SERVICE_BINDING_REQUIRED'
        ? '地址未能与旧服务核对，原登录和本地数据未更改。请使用原服务的 API 地址。'
        : '无法保存。请使用无账号、查询或片段的服务地址；内网地址须使用 HTTPS，API 路径须以 /api 结尾，并检查本地存储是否可用。'
      submit.disabled = unbound
    }
  }
}

async function start() {
  try {
    await nativeCommand('client.configuration') // Close transfers before any config UI or app imports.
    packagedConfig = await readDeploymentConfig()
    const value = await applyDeploymentConfig(packagedConfig)
    if (!value || sessionStorage.getItem(EDIT_SERVICE_KEY) === '1') form(value)
    else await boot(value)
  } catch { form(null, '安装包配置或本地存储未能通过核对，已停止连接。请检查 Windows 配置与签名步骤；原登录和本地数据未更改。') }
}

// A second document changing shared configuration invalidates this app, too.
window.addEventListener('storage', event => { if (event.key === SERVICE_CONFIG_KEY || event.key === null) location.reload() })
void start()
