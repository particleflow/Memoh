// Browser-private recovery state. No token or server filesystem path is stored.
// A committed IDB transaction is the checkpoint; OPFS close precedes offset ACK.
export interface MediaJournal {
  key: string
  kind: 'upload' | 'download'
  owner: string
  origin: string
  bot: string
  draft: string
  name: string
  mime: string
  hash: string
  size: number
  offset: number
  phase: 'create-unknown' | 'uploading' | 'patch-unknown' | 'complete-unknown' | 'ready' | 'downloading'
  receipt?: { id: string, expires_at: string }
  target?: string
  source?: boolean
  dismissed?: boolean
}

async function database() {
  return await new Promise<IDBDatabase>((resolve, reject) => {
    const request = indexedDB.open('memoh-media-recovery-v1', 1)
    request.onupgradeneeded = () => request.result.createObjectStore('journals', { keyPath: 'key' })
    request.onerror = () => reject(request.error)
    request.onblocked = () => reject(new Error('Recovery database is blocked'))
    request.onsuccess = () => resolve(request.result)
  })
}

async function transaction<T>(mode: IDBTransactionMode, action: (store: IDBObjectStore) => IDBRequest<T>): Promise<T> {
  const db = await database()
  try {
    return await new Promise<T>((resolve, reject) => {
      const tx = db.transaction('journals', mode, { durability: 'strict' })
      const request = action(tx.objectStore('journals'))
      tx.oncomplete = () => resolve(request.result)
      tx.onerror = tx.onabort = () => reject(tx.error ?? request.error)
    })
  } finally { db.close() }
}

export async function readJournal(key: string): Promise<MediaJournal | undefined> {
  return await transaction('readonly', store => store.get(key))
}
export async function listJournals(): Promise<MediaJournal[]> {
  return await transaction('readonly', store => store.getAll())
}
export async function writeJournal(journal: MediaJournal) {
  await transaction('readwrite', store => store.put(journal))
}

async function directory() {
  return await (await navigator.storage.getDirectory()).getDirectoryHandle('memoh-media-recovery-v1', { create: true })
}
export async function localFile(key: string): Promise<File> {
  return await (await (await directory()).getFileHandle(key)).getFile()
}
export async function writeLocalFile(key: string, bytes: Blob, offset = 0) {
  const handle = await (await directory()).getFileHandle(key, { create: true })
  const writer = await handle.createWritable({ keepExistingData: offset > 0 })
  try {
    await writer.seek(offset)
    await writer.write(bytes)
    await writer.truncate(offset + bytes.size)
    await writer.close()
  } catch (error) {
    await writer.abort().catch(() => {})
    throw error
  }
}
export async function removeJournal(key: string) {
  // Delete metadata first. An interruption may leave unreferenced local bytes,
  // but can never restore an acknowledged message as an unsent attachment.
  await transaction('readwrite', store => store.delete(key))
  await (await directory()).removeEntry(key).catch(() => {})
}

export async function mediaLock<T>(key: string, signal: AbortSignal, action: () => Promise<T>): Promise<T> {
  // Web Locks also serializes other tabs/processes sharing this IDB/OPFS store.
  // Never replace it with a per-tab mutex that can duplicate a lost Create.
  return await navigator.locks.request(`memoh-media:${key}`, { signal }, action)
}
