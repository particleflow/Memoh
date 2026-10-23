// One authority for transient media retention. Closed downloads have a short reuse grace period.
export const MEDIA_IDLE_CACHE_MS: number = 5 * 60 * 1000;
// Dismissal does not prove that the receiving application read the shared URI.
export const MEDIA_SHARE_LEASE_MS: number = 24 * 60 * 60 * 1000;
// Housekeeping must not leave a media command waiting for an absent SDK callback.
export const MEDIA_CLEANUP_BUDGET_MS: number = 1500;
// One foreground rebind hands existing transfers to their existing reconcilers.
// Remaining work keeps its journal and system schedule after this bounded pass.
export const TRANSFER_REOPEN_BUDGET_MS: number = 90 * 1000;
