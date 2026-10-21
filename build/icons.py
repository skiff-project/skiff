#!/usr/bin/env python3
"""Makes the icons of the packages from build/appicon.png (1024x1024):
the Linux sizes (build/linux/icons/hicolor), the macOS icon
(build/darwin/skiff.icns) and the images of the MSIX package of the
Microsoft Store (build/windows/msix/Assets). Run it again after changing appicon.png;
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

# the MSIX package: square logos and the wide tile (the icon in the middle)
assets = os.path.join(root, "windows", "msix", "Assets")
os.makedirs(assets, exist_ok=True)
for name, n in (("StoreLogo", 50), ("Square44x44Logo", 44), ("Square150x150Logo", 150)):
    src.resize((n, n), Image.LANCZOS).save(os.path.join(assets, name + ".png"), optimize=True)
wide = Image.new("RGBA", (310, 150), (0, 0, 0, 0))
wide.paste(src.resize((150, 150), Image.LANCZOS), (80, 0))
wide.save(os.path.join(assets, "Wide310x150Logo.png"), optimize=True)
print("icons written")
