// Isolated capability probe only. Not imported by the phone or a transport loop.
import rcp from '@hms.collaboration.rcp'
import webview from '@ohos.web.webview'
import connection from '@ohos.net.connection'

export function pinnedRequest(canonical: string, ip: string, authorization: string, body: ArrayBuffer): rcp.Request {
  const url = new URL(canonical)
  if (url.protocol !== 'https:' || url.username || url.password || url.hostname.startsWith('[') || /^[0-9.]+$/.test(url.hostname)) throw new Error('UNVERIFIED_AUTHORITY')
  const rule: rcp.StaticDnsRule = { host: url.hostname, port: Number(url.port || 443), ipAddresses: [ip] }
  const request = new rcp.Request(canonical, 'POST', { authorization }, body)
  request.configuration = { transfer: { autoRedirect: false }, dns: { dnsRules: [rule], happyEyeballOnDnsRule: false }, security: { remoteValidation: 'system' } }
  return request
}

// A candidate eligibility check, not permission to resend any failed request.
// Native-engine connect classification and direct/proxy state still need device proof.
export function connectFailureCandidate(code: number, verifiedDirect: boolean): boolean {
  return verifiedDirect && code === 1007900007
}

export async function originalV6(host: string): Promise<string[]> {
  const net = await connection.getDefaultNet()
  return (await net.getAddressesByName(host)).filter(address => address.family === 2).map(address => address.address)
}

export function hostCacheProbe(host: string, ip: string): void {
  webview.WebviewController.setHostIP(host, ip, 1)
  webview.WebviewController.clearHostIP(host)
}
