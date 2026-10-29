"""Deterministic synthetic images, never user files. Pillow is a test codec only."""
import io
import json
import sys
from pathlib import Path

from PIL import Image, PngImagePlugin


def describe(path):
    with Image.open(path) as image:
        return {"width": image.width, "height": image.height,
                "mime": Image.MIME[image.format], "frames": getattr(image, "n_frames", 1),
                "orientation": image.getexif().get(274, 1),
                "artist": image.getexif().get(315), "text": image.info.get("fixture"),
                "alpha": "A" in image.getbands()}


def create(directory):
    directory.mkdir(parents=True, exist_ok=True)
    exif = Image.Exif()
    exif[274] = 6
    exif[315] = "Synthetic original metadata"
    Image.new("RGB", (4096, 32), (32, 96, 192)).save(directory / "oriented.jpg", exif=exif)
    text = PngImagePlugin.PngInfo()
    text.add_text("fixture", "Synthetic original metadata")
    Image.new("RGBA", (6, 4), (48, 96, 192, 64)).save(directory / "transparent.png", pnginfo=text)
    Image.new("RGB", (6, 4), "red").save(directory / "animated.gif", save_all=True,
        append_images=[Image.new("RGB", (6, 4), "blue")], duration=[80, 120], loop=0)
    Image.new("RGB", (6, 4), "green").save(directory / "lossless.webp", lossless=True, exif=exif)
    Image.new("RGB", (6, 4), "yellow").save(directory / "tagged.tiff", exif=exif)
    return {path.name: describe(path) for path in sorted(directory.iterdir())}


if __name__ == "__main__":
    if sys.argv[1] == "create":
        print(json.dumps(create(Path(sys.argv[2]))))
    elif sys.argv[1] == "describe":
        print(json.dumps(describe(Path(sys.argv[2]))))
    elif sys.argv[1] == "encode":
        # Adapter for the old ImageKit boundary. These bytes prove which file
        # the ArkTS method hands off; they do not emulate the device's codec.
        with Image.open(sys.argv[2]) as image:
            image = image.resize((int(sys.argv[3]), int(sys.argv[4])))
            image = image.rotate(-int(sys.argv[5]), expand=True)
            output = io.BytesIO()
            format = sys.argv[6]
            if format == "JPEG":
                image = image.convert("RGB")
            image.save(output, format=format, quality=85)
            sys.stdout.buffer.write(output.getvalue())
