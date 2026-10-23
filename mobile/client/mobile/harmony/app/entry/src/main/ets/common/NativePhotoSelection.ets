import { common } from '@kit.AbilityKit';
import { fileIo as fs, fileUri, picker } from '@kit.CoreFileKit';
import { photoAccessHelper } from '@kit.MediaLibraryKit';
import { image } from '@kit.ImageKit';
import { util } from '@kit.ArkTS';
import { uniformTypeDescriptor } from '@kit.ArkData';
import { rawPixelMap } from './RawImage';
import { ClientDiagnostics } from './ClientDiagnostics';

const DISPLAY_EDGE = 2048;
const DISPLAY_JPEG_QUALITY = 85;

// System grants and local image encoding only. No Bot, account, transfer or chat state.
export class NativePhotoSelection {
  private context: common.UIAbilityContext;
  private copies: Set<string> = new Set();
  constructor(context: common.UIAbilityContext) { this.context = context; }

  async select(maximum: number, photosOnly: boolean): Promise<string[]> {
    let uris: string[], original = false;
    if (photosOnly) {
      const selected = await new photoAccessHelper.PhotoViewPicker().select({
        MIMEType: photoAccessHelper.PhotoViewMIMETypes.IMAGE_TYPE,
        maxSelectNumber: maximum, isOriginalSupported: true
      });
      uris = selected.photoUris; original = selected.isOriginalPhoto === true;
    } else {
      uris = await new picker.DocumentViewPicker(this.context).select({ maxSelectNumber: maximum });
    }
    const selected: string[] = [];
    try {
      for (const uri of uris) {
        selected.push(original ? uri : await this.displayCopy(uri, photosOnly));
      }
      ClientDiagnostics.event('SYSTEM', 'photo-selection', 'complete',
        'count=' + selected.length + '/original=' + original);
      return selected;
    } catch (error) {
      selected.filter((uri: string) => uri.includes('/memoh-photo-')).forEach((uri: string) => {
        this.remove(new fileUri.FileUri(uri).path);
      });
      throw error as Error;
    }
  }

  private async displayCopy(uri: string, photosOnly: boolean): Promise<string> {
    let name = decodeURIComponent(uri.split('/').pop() ?? 'image').split('?')[0];
    const extension = name.split('.').pop()?.toLowerCase() ?? '';
    if (!photosOnly && extension !== 'dng') {
      const type = extension ? uniformTypeDescriptor.getUniformDataTypeByFilenameExtension('.' + extension) : '';
      if (!type || !uniformTypeDescriptor.getTypeDescriptor(type)?.belongsTo(uniformTypeDescriptor.UniformDataType.IMAGE)) {
        return uri;
      }
    }
    let source: image.ImageSource | undefined, pixels: image.PixelMap | undefined;
    let png = extension === 'png';
    let packer: image.ImagePacker | undefined;
    const file = await fs.open(uri, fs.OpenMode.READ_ONLY);
    try {
      if (extension === 'dng') {
        pixels = await rawPixelMap(file.fd, DISPLAY_EDGE);
      } else {
        source = image.createImageSource(file.fd);
        const info = await source.getImageInfo();
        png = info.mimeType === 'image/png' || png;
        if (info.size.width < 1 || info.size.height < 1) { throw new Error('IMAGE_DIMENSIONS_INVALID'); }
        const scale = Math.min(1, DISPLAY_EDGE / Math.max(info.size.width, info.size.height));
        pixels = await source.createPixelMap({ editable: true, desiredSize: {
          width: Math.max(1, Math.round(info.size.width * scale)),
          height: Math.max(1, Math.round(info.size.height * scale))
        } });
        // ImageKit reports EXIF orientation separately; bake it into the display copy.
        let orientation = 'Top-left';
        try { orientation = await source.getImageProperty(image.PropertyKey.ORIENTATION); }
        catch { /* Formats without EXIF keep their decoded orientation. */ }
        if (orientation === 'Top-right' || orientation === '2') { await pixels.flip(true, false); }
        else if (orientation === 'Bottom-right' || orientation === '3') { await pixels.rotate(180); }
        else if (orientation === 'Bottom-left' || orientation === '4') { await pixels.flip(false, true); }
        else if (orientation === 'Left-top' || orientation === '5') {
          await pixels.flip(true, false); await pixels.rotate(270);
        } else if (orientation === 'Right-top' || orientation === '6') { await pixels.rotate(90); }
        else if (orientation === 'Right-bottom' || orientation === '7') {
          await pixels.flip(true, false); await pixels.rotate(90);
        } else if (orientation === 'Left-bottom' || orientation === '8') { await pixels.rotate(270); }
      }
      packer = image.createImagePacker();
      const bytes = await packer.packing(pixels, { format: png ? 'image/png' : 'image/jpeg', quality: DISPLAY_JPEG_QUALITY });
      if (!bytes.byteLength) { throw new Error('IMAGE_ENCODING_EMPTY'); }
      name = name.replace(/\.[^.]+$/, '').replace(/[\\/\x00-\x1f]/g, '_').slice(0, 180) + (png ? '.png' : '.jpg');
      const directory = this.context.cacheDir + '/memoh-photo-' + util.generateRandomUUID(true);
      fs.mkdirSync(directory);
      const path = directory + '/' + name;
      this.copies.add(path);
      const output = await fs.open(path, fs.OpenMode.CREATE | fs.OpenMode.WRITE_ONLY);
      try {
        let offset = 0;
        while (offset < bytes.byteLength) {
          const written = await fs.write(output.fd, bytes.slice(offset));
          if (written < 1) { throw new Error('IMAGE_WRITE_INCOMPLETE'); }
          offset += written;
        }
        fs.fsyncSync(output.fd);
        if (fs.statSync(output.fd).size !== bytes.byteLength) { throw new Error('IMAGE_WRITE_INCOMPLETE'); }
      } catch (error) { this.remove(path); throw error as Error; }
      finally { await fs.close(output); }
      ClientDiagnostics.event('SYSTEM', 'photo-selection', 'converted', 'bytes=' + bytes.byteLength);
      return fileUri.getUriFromPath(path);
    } finally {
      if (pixels) { try { await pixels.release(); } catch { /* Release the remaining SDK resources too. */ } }
      if (source) { try { await source.release(); } catch { /* Continue cleanup. */ } }
      if (packer) { try { await packer.release(); } catch { /* Continue cleanup. */ } }
      await fs.close(file);
    }
  }

  private remove(path: string): void {
    if (!this.copies.has(path)) { return; }
    try { if (fs.accessSync(path)) { fs.unlinkSync(path); } fs.rmdirSync(path.slice(0, path.lastIndexOf('/'))); }
    finally { this.copies.delete(path); }
  }
  close(): void {
    this.copies.forEach((path: string) => {
      try { this.remove(path); }
      catch (error) { ClientDiagnostics.failure('SYSTEM', 'photo-selection', 'cleanup-failed', error as Error); }
    });
  }
}
