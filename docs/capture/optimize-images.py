#!/usr/bin/env python3
"""Shrinks the documentation screenshots (docs/images by default).

Interface screenshots have few colours: a 256-colour palette without
dithering is visually identical and makes the PNG files about 2.5 times
smaller. Requires Pillow (pip install pillow).

    python3 docs/capture/optimize-images.py [dossier]
"""
import os
import sys

from PIL import Image

root = sys.argv[1] if len(sys.argv) > 1 else os.path.join(os.path.dirname(__file__), '..', 'images')
before = after = 0
for name in sorted(os.listdir(root)):
    if not name.endswith('.png'):
        continue
    path = os.path.join(root, name)
    before += os.path.getsize(path)
    with Image.open(path) as im:
        if im.mode == 'P':  # already optimized
            after += os.path.getsize(path)
            continue
        q = im.convert('RGB').quantize(colors=256, method=Image.Quantize.MEDIANCUT, dither=Image.Dither.NONE)
    q.save(path, optimize=True)
    after += os.path.getsize(path)
print(f'{before / 1e6:.1f} Mo -> {after / 1e6:.1f} Mo')
