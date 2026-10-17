#!/usr/bin/env python3
"""Makes the icons of the packages from build/appicon.png (1024x1024):
the Linux sizes (build/linux/icons/hicolor) and the macOS icon
(build/darwin/skiff.icns). Run it again after changing appicon.png;
needs Pillow (python3-pil)."""
import os
from PIL import Image

root = os.path.dirname(os.path.abspath(__file__))
src = Image.open(os.path.join(root, "appicon.png")).convert("RGBA")

for n in (16, 24, 32, 48, 64, 128, 256, 512):
    out = os.path.join(root, "linux", "icons", "hicolor", f"{n}x{n}", "apps", "skiff.png")
    os.makedirs(os.path.dirname(out), exist_ok=True)
    src.resize((n, n), Image.LANCZOS).save(out, optimize=True)

# every size of an .icns, from 16 to 1024 (512@2x)
src.save(os.path.join(root, "darwin", "skiff.icns"))
print("icons written")
