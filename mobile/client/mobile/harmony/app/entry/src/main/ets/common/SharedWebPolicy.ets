import { webview } from '@kit.ArkWeb';
import { url } from '@kit.ArkTS';
import { WEB_RESOURCES } from './WebResourceManifest';
import { MEMOH_ORIGIN } from './MemohClientConfig';

export const WEB_ORIGIN: string = 'begonia-client://app';
export const WEB_START: string = WEB_ORIGIN + '/index.html';
let sharedWebInitialized: boolean = false;
export function sharedWebAvailable(): boolean { return sharedWebInitialized; }

// No native filesystem path or URI supplied by the page is ever used as a resource name.
export function packagedPath(value: string): string {
  if (!value.startsWith(WEB_ORIGIN + '/')) { return ''; }
  const path = value.substring(WEB_ORIGIN.length).split(/[?#]/)[0];
  if (path.includes('\\') || path.includes('%') || path.includes('//')) { return ''; }
  const parts = path.substring(1).split('/');
  if (parts.some((part: string) => !part || part === '.' || part === '..' || !/^[A-Za-z0-9_.-]+$/.test(part))) { return ''; }
  const relative = parts.join('/');
  return Object.keys(WEB_RESOURCES).includes(relative) ? relative : '';
}

export function memohRequest(value: string): boolean {
  if (/[\\\u0000-\u0020]/.test(value) || /%(?:2f|5c|2e|25)/i.test(value.split(/[?#]/)[0])) { return false; }
  try {
    const base = url.URL.parseURL(MEMOH_ORIGIN);
    const request = url.URL.parseURL(value);
    if (!['http:', 'https:'].includes(base.protocol) || base.username || base.password || base.search || base.hash ||
      request.username || request.password || request.hash) { return false; }
    const socketProtocol = base.protocol === 'https:' ? 'wss:' : 'ws:';
    if (![base.protocol, socketProtocol].includes(request.protocol) || request.host !== base.host) { return false; }
    // Reject encoded separators, dots and nested escapes before comparing the API path boundary.
    if (/%(?:2f|5c|2e|25)/i.test(base.pathname) || /%(?:2f|5c|2e|25)/i.test(request.pathname) ||
      base.pathname.includes('//') || request.pathname.includes('//')) { return false; }
    const prefix = base.pathname.replace(/\/+$/, '');
    return request.pathname.startsWith(prefix + '/');
  } catch { return false; }
}

function viewerHostname(hostname: string, baseHostname: string): boolean {
  const session = hostname.substring(0, 32);
  return /^[a-f0-9]{32}$/.test(session) && hostname === session + '.browser.' + baseHostname;
}

// Official workspace viewer sessions use isolated, short-lived capability hosts.
// They may load only as unbridged subframes/resources, never as the app document.
export function viewerRequest(value: string): boolean {
  if (/[\\\u0000-\u0020]/.test(value)) { return false; }
  try {
    const base = url.URL.parseURL(MEMOH_ORIGIN);
    const parsed = url.URL.parseURL(value);
    if (!['http:', 'https:'].includes(base.protocol) || base.username || base.password || base.search || base.hash) { return false; }
    const socketProtocol = base.protocol === 'https:' ? 'wss:' : 'ws:';
    return [base.protocol, socketProtocol].includes(parsed.protocol) && !parsed.username && !parsed.password &&
      parsed.port === base.port && viewerHostname(parsed.hostname, base.hostname);
  } catch { return false; }
}

// Only a native-confirmed user action may hand an ordinary web link to the OS.
// Authenticated application resources stay inside this document.
export function externalBrowserLink(value: string, userGesture: boolean): boolean {
  if (!userGesture || /[\\\u0000-\u0020]/.test(value)) { return false; }
  // Native isUserTrigger or mainFrame+gesture+GET+non-redirect is required by SharedChat.
  // This never reads/adds credentials or headers, fetches automatically, or creates a bridged window.
  // Keep the credential query refusal; standard URL parsing only restores ordinary public/IPv6 links.
  try {
    const parsed = url.URL.parseURL(value);
    if (!['http:', 'https:'].includes(parsed.protocol) || parsed.username || parsed.password) { return false; }
    // The hostname itself carries the viewer session; do not hand it to another app.
    if (viewerHostname(parsed.hostname, url.URL.parseURL(MEMOH_ORIGIN).hostname)) { return false; }
    if (/[?&#](?:access_token|authorization|token)=/i.test(decodeURIComponent(parsed.search))) { return false; }
    if (parsed.origin === url.URL.parseURL(MEMOH_ORIGIN).origin) { return false; }
    return true;
  } catch { return false; }
}

export function initializeSharedWeb(): void {
  if (sharedWebInitialized) { return; }
  // lazyInit=true guarantees registration precedes the engine's first initialization.
  webview.WebviewController.customizeSchemes([{
    schemeName: 'begonia-client', isSupportCORS: true, isSupportFetch: true, isStandard: true,
    isLocal: false, isDisplayIsolated: true, isSecure: true, isCspBypassing: false
  }], true);
  webview.WebviewController.setWebDebuggingAccess(false);
  webview.WebviewController.initializeWebEngine();
  sharedWebInitialized = true;
}
