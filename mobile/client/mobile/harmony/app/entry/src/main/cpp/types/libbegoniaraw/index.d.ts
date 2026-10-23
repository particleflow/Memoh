export interface RawPixels { width: number; height: number; pixels: ArrayBuffer; mime?: string; }
export const decode: (fd: number) => Promise<RawPixels>;
// Read-only fstat of a borrowed FD; decimal dev/ino, links/size and regular flag.
export const identity: (fd: number) => string;
// Native-selected documents URI -> official sandbox path conversion; no file I/O.
export const folderUriPath: (uri: string) => string;
// Final same-directory no-overwrite commit only. Borrowed directory/source FDs;
// caller owns authority, persistent grant, staging receipt and post-move identity.
// Unsupported native/filesystem capability throws; no copy/delete fallback.
export const folderPublish: (directoryFd: number, sourceFd: number, stage: string, destination: string) => void;
export const folderPublishFrom: (stagingDirectoryFd: number, directoryFd: number, sourceFd: number, stage: string, destination: string) => void;
// Direct regular single-link child through borrowed root FD. mode0=read,
// mode1=read/write existing, mode2=exclusive create. Caller closes returned FD.
export const folderOpen: (directoryFd: number, name: string, mode: number) => number;
// Borrowed existing media directory: shared reader or exclusive cleanup lease.
// Nonblocking; false means busy. Close of the owned FD also releases the lock.
export const mediaLeaseLock: (fd: number, acquire: boolean, exclusive: boolean) => boolean;
// Borrowed FD of the existing private assets directory. Synchronous local sections only.
export const authorityLock: (fd: number, acquire: boolean) => void;
