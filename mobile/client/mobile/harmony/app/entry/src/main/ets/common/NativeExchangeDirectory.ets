import { common } from '@kit.AbilityKit';
import { fileIo as fs, fileUri } from '@kit.CoreFileKit';

// Only this application-owned subtree is donated. Journal/cache/credentials and
// staging remain outside it. Each account/device has its own opaque child.
export const EXCHANGE_DIRECTORY: string = 'work-exchange';
export const DOWNLOAD_EXCHANGE_ROOT: string = 'file://docs/storage/Users/currentUser/Download/cloud.begonia.workassistant';
export interface ExchangeChoice { uri: string; name: string; kind?: string; scope?: string; }
export function isDownloadExchangeUri(uri: string): boolean {
  return typeof uri === 'string' && uri.startsWith(DOWNLOAD_EXCHANGE_ROOT + '/') &&
    /^[a-f0-9]{32}$/.test(uri.substring(DOWNLOAD_EXCHANGE_ROOT.length + 1));
}
export function isDownloadExchange(folder: ExchangeChoice): boolean {
  return folder.kind === 'download-exchange' && typeof folder.scope === 'string' && /^[a-f0-9]{32}$/.test(folder.scope) &&
    folder.uri === DOWNLOAD_EXCHANGE_ROOT + '/' + folder.scope;
}
export function isExchangeUri(uri: string): boolean {
  return typeof uri === 'string' && uri.startsWith('file://cloud.begonia.workassistant/') &&
    /\/work-exchange\/[a-f0-9]{32}$/.test(uri) && !/[?#\u0000-\u0020]/.test(uri) && !uri.split('/').some((part: string) => part === '.' || part === '..');
}
export function exchangePath(context: common.Context, folder: ExchangeChoice): string {
  if (folder.kind !== 'app-exchange' || typeof folder.scope !== 'string' || !/^[a-f0-9]{32}$/.test(folder.scope)) {
    throw new Error('ASSET_FOLDER_SELECTION_INVALID');
  }
  const root = context.getApplicationContext().filesDir + '/' + EXCHANGE_DIRECTORY;
  const parent = fs.lstatSync(root);
  if (!parent.isDirectory() || parent.isSymbolicLink()) { throw new Error('ASSET_FOLDER_PATH_UNAVAILABLE'); }
  const path = root + '/' + folder.scope;
  if (!isExchangeUri(folder.uri) || folder.uri !== fileUri.getUriFromPath(path)) { throw new Error('ASSET_FOLDER_SELECTION_INVALID'); }
  return path;
}
export function createExchangeChoice(context: common.Context, scope: string): ExchangeChoice {
  if (!/^[a-f0-9]{32}$/.test(scope)) { throw new Error('ASSET_FOLDER_SELECTION_INVALID'); }
  const base = context.getApplicationContext().filesDir;
  for (const path of [base, base + '/' + EXCHANGE_DIRECTORY, base + '/' + EXCHANGE_DIRECTORY + '/' + scope]) {
    if (path !== base && !fs.accessSync(path)) { fs.mkdirSync(path); }
    const stat = fs.lstatSync(path);
    if (!stat.isDirectory() || stat.isSymbolicLink()) { throw new Error('ASSET_FOLDER_PATH_UNAVAILABLE'); }
  }
  const path = base + '/' + EXCHANGE_DIRECTORY + '/' + scope;
  return { uri: fileUri.getUriFromPath(path), name: EXCHANGE_DIRECTORY + '-' + scope.substring(0, 8), kind: 'app-exchange', scope };
}
