"""Derive faint background-only masks; never redraw approved artwork.

Pillow is only needed by this offline asset maintenance tool, not by the HAP
builder. Source originals and their Library identities remain unchanged.
"""
import base64
import hashlib
import json
from pathlib import Path

from PIL import Image

BASE = Path(__file__).resolve().parent
SOURCE = BASE / 'approved/koi-lotus.png'
SOURCE_SHA256 = 'e8e00500e14b444789cc27a891206f29eaef61b4f48c3828b12ee669de1b2917'
CSS = BASE.parents[1] / 'memoh/harmony-service.css'
MARKER = '/* BEGIN approved Harmony artwork: background-only, alpha <= 6/255. */'


def main():
    assert hashlib.sha256(SOURCE.read_bytes()).hexdigest() == SOURCE_SHA256
    original = Image.open(SOURCE).convert('RGB')
    # Neutral near-white source background becomes alpha 0. Full foreground is
    # capped at 6/255 (2.35294%). No crop, resize, tracing, new contour or blur.
    pixels = original.tobytes()
    alpha = [round(min(1, max(0, (240 - min(pixels[i:i + 3])) / 24)) * 6)
             for i in range(0, len(pixels), 3)]
    records = []
    urls = []
    for name, value in [('watermark-light.png', 0), ('watermark-dark.png', 255)]:
        derived = Image.new('RGBA', original.size, (value, value, value, 0))
        derived.putalpha(bytes_to_alpha(original.size, alpha))
        path = BASE / name
        derived.save(path, optimize=True)
        data = path.read_bytes()
        records.append({'path': name, 'sha256': hashlib.sha256(data).hexdigest(),
                        'bytes': len(data), 'foregroundRGB': [value] * 3,
                        'alphaRange': [min(alpha), max(alpha)]})
        urls.append('url("data:image/png;base64,' + base64.b64encode(data).decode() + '")')
    css = CSS.read_text().split(MARKER)[0].rstrip() + '\n\n' + MARKER + '\n'
    css += ':root { --harmony-artwork-image: ' + urls[0] + '; }\n'
    css += ':root.dark { --harmony-artwork-image: ' + urls[1] + '; }\n'
    css += '''/* These existing surfaces paint the single motif behind their content.
   No pseudo-element, overlay, pointer target, opacity on content, or layout edit.
   The data URI is intentional: this existing CSS is already copied and hashed
   by the Harmony producer, so the shared build recipe needs no competing edit. */
#app .dv-content-container,
#app [data-slot="sidebar-inset"],
#app > section > main,
#app > .service-setup {
  background-image: var(--harmony-artwork-image);
  background-repeat: no-repeat;
  background-position: center;
  background-size: min(78vw, 26rem) auto;
}
/* END approved Harmony artwork. */
'''
    CSS.write_text(css)
    manifest = {
        'sourceLibraryId': 'libfile_71fe864e294c8191bcb4d9e0ec17f487',
        'sourceSHA256': SOURCE_SHA256,
        'operation': 'Preserve 1254x1254 geometry; replace RGB with neutral theme ink; alpha = round(clamp((240-min(sourceRGB))/24,0,1)*6).',
        'maxEffectiveOpacity': 6 / 255,
        'backgroundWhiteRemoved': True,
        'cssSingleBackgroundLayer': True,
        'derived': records,
    }
    (BASE / 'watermark-derivation.json').write_text(json.dumps(manifest, indent=2) + '\n')


def bytes_to_alpha(size, values):
    return Image.frombytes('L', size, bytes(values))


if __name__ == '__main__':
    main()
