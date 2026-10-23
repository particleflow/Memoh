import { fileIo as fs } from '@kit.CoreFileKit';
import { cryptoFramework } from '@kit.CryptoArchitectureKit';

// Shared bounded file handoff, not another downloader or transfer inventory.
// Both descriptors are borrowed; the caller owns their independent release.
export async function copyMediaBytes(source: number, target: number, size: number, authorize: () => void, start: number = 0,
  checkpoint?: (written: number) => void): Promise<number> {
  authorize();
  if (!Number.isSafeInteger(start) || start < 0 || start > size || fs.statSync(source).size !== size) { throw new Error('MEDIA_SIZE_MISMATCH'); }
  let written = start;
  while (written < size) {
    authorize();
    const buffer = new ArrayBuffer(Math.min(262144, size - written));
    const count = await fs.read(source, buffer, { offset: written }); authorize();
    if (!Number.isSafeInteger(count) || count <= 0 || count > buffer.byteLength) { throw new Error('MEDIA_SHORT_READ'); }
    let at = 0;
    while (at < count) {
      authorize();
      const n = await fs.write(target, buffer.slice(at, count), { offset: written + at });
      if (checkpoint && Number.isSafeInteger(n) && n > 0 && n <= count - at) { checkpoint(written + at + n); }
      authorize();
      if (!Number.isSafeInteger(n) || n <= 0 || n > count - at) { throw new Error('MEDIA_SHORT_WRITE'); }
      at += n;
    }
    written += count;
  }
  authorize();
  if (fs.statSync(source).size !== size) { throw new Error('MEDIA_SIZE_MISMATCH'); }
  fs.fsyncSync(target); return written;
}

// An already claimed public target may be resumed only if its recorded prefix
// still equals the retained, verified cache. No write/truncate/adoption occurs here.
export async function verifyMediaPrefix(source: number, target: number, size: number, authorize: () => void): Promise<void> {
  if (!Number.isSafeInteger(size) || size < 0 || fs.statSync(source).size < size || fs.statSync(target).size !== size) {
    throw new Error('MEDIA_SIZE_MISMATCH');
  }
  const hashes: string[] = [];
  for (const fd of [source, target]) {
    const md = cryptoFramework.createMd('SHA256'); let at = 0;
    while (at < size) {
      authorize(); const buffer = new ArrayBuffer(Math.min(262144, size - at));
      const count = await fs.read(fd, buffer, { offset: at }); authorize();
      if (!Number.isSafeInteger(count) || count <= 0 || count > buffer.byteLength) { throw new Error('MEDIA_SHORT_READ'); }
      await md.update({ data: new Uint8Array(buffer, 0, count) }); authorize(); at += count;
    }
    hashes.push(Array.from((await md.digest()).data).map((value: number) => value.toString(16).padStart(2, '0')).join(''));
  }
  authorize();
  if (hashes[0] !== hashes[1]) { throw new Error('MEDIA_FOLDER_TARGET_CHANGED'); }
}

export async function verifyMediaFd(fd: number, size: number, expected: string, authorize: () => void): Promise<void> {
  const md = cryptoFramework.createMd('SHA256');
  if (fs.statSync(fd).size !== size) { throw new Error('MEDIA_SIZE_MISMATCH'); }
  let at = 0;
  while (at < size) {
    authorize(); const buffer = new ArrayBuffer(Math.min(262144, size - at));
    const count = await fs.read(fd, buffer, { offset: at }); authorize();
    if (!Number.isSafeInteger(count) || count <= 0 || count > buffer.byteLength) { throw new Error('MEDIA_SHORT_READ'); }
    await md.update({ data: new Uint8Array(buffer, 0, count) }); at += count;
  }
  const sha = Array.from((await md.digest()).data).map((value: number) => value.toString(16).padStart(2, '0')).join('');
  authorize();
  if (sha !== expected || fs.statSync(fd).size !== size) { throw new Error('MEDIA_SHA_MISMATCH'); }
}
