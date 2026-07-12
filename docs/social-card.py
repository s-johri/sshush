#!/usr/bin/env python3
"""Compose the GitHub social preview card (1280x640) from the vhs screenshot.

Run after `sh docs/demo-fixture.sh docs/social.tape`:
    python3 docs/social-card.py    # needs Pillow; writes docs/social-preview.png
"""
from pathlib import Path

from PIL import Image, ImageDraw, ImageFont

docs = Path(__file__).parent
app = Image.open(docs / "social" / "app.png").convert("RGB")

W, H = 1280, 640
bg = app.getpixel((2, 2))  # match the vhs margin so the seam is invisible
card = Image.new("RGB", (W, H), bg)
card.paste(app, (0, H - app.height))

draw = ImageDraw.Draw(card)
mono = "/usr/share/fonts/truetype/dejavu/DejaVuSansMono-Bold.ttf"
sans = "/usr/share/fonts/truetype/dejavu/DejaVuSans.ttf"
title_font = ImageFont.truetype(mono, 96)
tag_font = ImageFont.truetype(sans, 34)

# Tokyo Night palette, matching the screenshot's theme.
draw.text((76, 44), "sshush", font=title_font, fill="#7aa2f7")
draw.text(
    (82, 166),
    "SSH keys, ssh-agent, and config — one terminal UI",
    font=tag_font,
    fill="#a9b1d6",
)

out = docs / "social-preview.png"
card.save(out)
print(f"wrote {out} ({W}x{H})")
