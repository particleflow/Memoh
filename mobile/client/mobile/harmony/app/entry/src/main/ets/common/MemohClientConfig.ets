import { normalizeServiceUrl, normalizeTransferNetworkPolicy, TransferNetworkPolicy } from './ServiceUrl';

// In-memory copy of the current document's local configuration. Never persisted here.
export let MEMOH_ORIGIN: string = '';
export let MEMOH_NETWORK_POLICY: TransferNetworkPolicy | undefined;
export function resetMemohOrigin(): void { MEMOH_ORIGIN = ''; MEMOH_NETWORK_POLICY = undefined; }
export function configureMemohOrigin(value: string, networkPolicy?: TransferNetworkPolicy): void {
  const normalized = normalizeServiceUrl(value);
  const policy = networkPolicy === undefined ? undefined : normalizeTransferNetworkPolicy(networkPolicy);
  if (policy && !normalized.startsWith('https://')) { throw new Error('NETWORK_POLICY_HTTPS'); }
  if (MEMOH_ORIGIN && (MEMOH_ORIGIN !== normalized || JSON.stringify(MEMOH_NETWORK_POLICY) !== JSON.stringify(policy))) { throw new Error('SERVICE_CONFIG_LOCKED'); }
  MEMOH_ORIGIN = normalized; MEMOH_NETWORK_POLICY = policy;
}
