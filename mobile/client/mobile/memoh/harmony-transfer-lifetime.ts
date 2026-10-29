// Local observation lifetime only; never serialized into a file/task journal.
export const NATIVE_VIEW_DETACHED = 'harmony-view-detached'
export function nativeViewDetached(signal?: AbortSignal): boolean {
  return signal?.aborted === true && signal.reason === NATIVE_VIEW_DETACHED
}
