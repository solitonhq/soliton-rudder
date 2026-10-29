#!/usr/bin/env python3
"""Regenerates every logo asset from the one source image, assets/branding/logo-source.png.

    pip install pillow numpy
    python3 scripts/gen_branding.py

Writes:
  assets/branding/logo-1024.png        cleaned master (flat background, no stray artefacts)
  assets/branding/logo-256.png         README / docs
  assets/branding/social-preview.png   1280x640 GitHub social preview (skipped if no font is found)
  build/appicon.png                    app icon (macOS grid: 824 px tile in a 1024 canvas, with shadow)
  build/windows/icon.ico               Windows icon
  frontend/src/assets/logo.png         in-app mark (rounded tile, 256 px)

The source has a near-black background with faint JPEG-like noise around the artwork; it is flattened to one colour so
that nothing shows as a ghost rectangle at small sizes or on the rounded icon tile.
"""
import os
import sys

import numpy as np
from PIL import Image, ImageDraw, ImageFilter, ImageFont

ROOT = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
SRC = os.path.join(ROOT, "assets/branding/logo-source.png")
CROP = (64, 60, 1024, 1020)  # a 960 px square around the artwork (it spans about x 199-911, y 215-866)
RADIUS = 0.2237  # macOS icon corner radius relative to the tile


def p(*parts):
    return os.path.join(ROOT, *parts)


def flatten(img):
    """Replaces the noisy near-black background by its exact edge colour, keeping the artwork and its anti-aliasing."""
    a = np.asarray(img.convert("RGB")).astype(np.float32)
    bg = np.median(np.concatenate([a[:8].reshape(-1, 3), a[-8:].reshape(-1, 3), a[:, :8].reshape(-1, 3), a[:, -8:].reshape(-1, 3)]), axis=0)
    dist = np.abs(a - bg).max(axis=2)
    t = np.clip((dist - 8.0) / (28.0 - 8.0), 0, 1)
    w = (t * t * (3 - 2 * t))[..., None]  # smoothstep: noise below ~8 vanishes, real pixels (> 28) are untouched
    out = bg + (a - bg) * w
    return Image.fromarray(np.clip(out + 0.5, 0, 255).astype(np.uint8)), tuple(int(round(x)) for x in bg)


def tile(master, size, radius=RADIUS):
    """The cropped artwork as a rounded square of the given size, transparent outside the corners."""
    art = master.crop(CROP).resize((size * 4, size * 4), Image.LANCZOS)  # drawn 4x for smooth corners
    mask = Image.new("L", art.size, 0)
    ImageDraw.Draw(mask).rounded_rectangle([0, 0, art.size[0] - 1, art.size[1] - 1], radius=int(art.size[0] * radius), fill=255)
    art = art.convert("RGBA")
    art.putalpha(mask)
    return art.resize((size, size), Image.LANCZOS)


def app_icon(master):
    canvas = Image.new("RGBA", (1024, 1024), (0, 0, 0, 0))
    t = tile(master, 824)
    shadow = Image.new("RGBA", (1024, 1024), (0, 0, 0, 0))
    shadow.paste((0, 0, 0, 90), (100, 112), t.getchannel("A"))
    canvas.alpha_composite(shadow.filter(ImageFilter.GaussianBlur(14)))
    canvas.alpha_composite(t, (100, 100))
    return canvas


def font(size, bold=True):
    for path in (
        "/System/Library/Fonts/Supplemental/Arial Bold.ttf" if bold else "/System/Library/Fonts/Supplemental/Arial.ttf",
        "/System/Library/Fonts/HelveticaNeue.ttc",
        "/usr/share/fonts/truetype/dejavu/DejaVuSans-Bold.ttf" if bold else "/usr/share/fonts/truetype/dejavu/DejaVuSans.ttf",
    ):
        if os.path.exists(path):
            return ImageFont.truetype(path, size)
    return None


def social_preview(master, bg):
    f1, f2 = font(92), font(36, bold=False)
    if f1 is None or f2 is None:
        print("no font found: skipping social-preview.png")
        return None
    img = Image.new("RGBA", (1280, 640), bg + (255,))
    img.alpha_composite(tile(master, 360), (110, 140))  # the rounded corners blend into the same background colour
    d = ImageDraw.Draw(img)
    d.text((520, 215), "Soliton Rudder", font=f1, fill=(255, 255, 255))
    d.text((524, 335), "A fast, private Kubernetes desktop IDE", font=f2, fill=(150, 175, 215))
    d.text((524, 385), "Open source \u00b7 Local-first \u00b7 No telemetry", font=f2, fill=(110, 130, 165))
    d.rectangle([524, 470, 700, 476], fill=(26, 115, 232))
    return img.convert("RGB")


def main():
    if not os.path.exists(SRC):
        sys.exit(f"missing {SRC}")
    master, bg = flatten(Image.open(SRC))
    master.save(p("assets/branding/logo-1024.png"), optimize=True)
    tile(master, 256).save(p("assets/branding/logo-256.png"), optimize=True)
    app_icon(master).save(p("build/appicon.png"), optimize=True)
    tile(master, 256).save(p("frontend/src/assets/logo.png"), optimize=True)
    sizes = [16, 24, 32, 48, 64, 128, 256]
    tile(master, 256, radius=0.18).save(p("build/windows/icon.ico"), sizes=[(s, s) for s in sizes])
    sp = social_preview(master, bg)
    if sp is not None:
        sp.save(p("assets/branding/social-preview.png"), optimize=True)
    print("background colour:", "#%02x%02x%02x" % bg)


if __name__ == "__main__":
    main()
