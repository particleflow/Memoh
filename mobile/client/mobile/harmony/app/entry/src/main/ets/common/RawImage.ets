import { image } from '@kit.ImageKit';
import raw from 'libbegoniaraw.so';
import { ClientDiagnostics } from './ClientDiagnostics';

// RAW interpretation belongs to the mature decoder. No original is rewritten.
export async function rawPixelMap(fd: number, maximum: number): Promise<image.PixelMap> {
  const decoded = await raw.decode(fd);
  if (decoded.mime === 'image/jpeg') {
    const source = image.createImageSource(decoded.pixels);
    try {
      const info = await source.getImageInfo();
      const scale = Math.min(1, maximum / Math.max(info.size.width, info.size.height));
      return await source.createPixelMap({ desiredSize: {
        width: Math.max(1, Math.round(info.size.width * scale)),
        height: Math.max(1, Math.round(info.size.height * scale)) } });
    } finally {
      try { await source.release(); }
      catch (cleanup) { ClientDiagnostics.failure('media', 'raw', 'cleanup-failed', cleanup as Error, 'phase=source-release'); }
    }
  }
  const pixels = await image.createPixelMap(decoded.pixels, { size: { width: decoded.width, height: decoded.height },
    pixelFormat: image.PixelMapFormat.RGBA_8888, editable: true });
  try {
    const scale = Math.min(1, maximum / Math.max(decoded.width, decoded.height));
    if (scale < 1) { await pixels.scale(scale, scale); }
    return pixels;
  } catch (error) {
    try { await pixels.release(); }
    catch (cleanup) { ClientDiagnostics.failure('media', 'raw', 'cleanup-failed', cleanup as Error, 'phase=pixels-release'); }
    throw error as Error;
  }
}

// Uses the existing native library/runtime; never opens or closes the borrowed descriptor.
export function nativeFileIdentity(fd: number): string { return raw.identity(fd); }
