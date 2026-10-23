import { normalizeServiceUrl } from './ServiceUrl';

// In-memory copy of the current document's local configuration. Never persisted here.
export let MEMOH_ORIGIN: string = '';
export function resetMemohOrigin(): void { MEMOH_ORIGIN = ''; }
export function configureMemohOrigin(value: string): void {
  const normalized = normalizeServiceUrl(value);
  if (MEMOH_ORIGIN && MEMOH_ORIGIN !== normalized) { throw new Error('SERVICE_CONFIG_LOCKED'); }
  MEMOH_ORIGIN = normalized;
}
