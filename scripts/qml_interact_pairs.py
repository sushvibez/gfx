#!/usr/bin/env python3
"""Compose per-step ref|ours pairs for one interaction run and report RMS."""
import os, sys
from PIL import Image, ImageChops, ImageDraw

out, name, script, w, h = sys.argv[1], sys.argv[2], sys.argv[3], int(sys.argv[4]), int(sys.argv[5])
labels = ["(as it opens)"] + [l.split("#")[0].strip() for l in open(script) if l.split("#")[0].strip()]

def load(p):
    if os.path.exists(p) and os.path.getsize(p) > 0:
        return Image.open(p).convert("RGB").resize((w, h))
    return None

def rms(a, b):
    a, b = a.resize((45, 35)), b.resize((45, 35))
    pa, pb = a.load(), b.load()
    t = 0
    for y in range(35):
        for x in range(45):
            for i in range(3):
                d = pa[x, y][i] - pb[x, y][i]
                t += d * d
    return (t / (45 * 35 * 3)) ** 0.5

rows = [(i, label, load(f"{out}/ref/{name}/{i:02d}.png")) for i, label in enumerate(labels)]

def changed(a, b):
    """Did the reference move at all between these two steps?"""
    if a is None or b is None:
        return True
    return ImageChops.difference(a, b).getbbox() is not None

# ours writes <base> then <base>-NN-<step>.png; index them by step number.
ours = {}
d = f"{out}/ours/{name}"
for f in sorted(os.listdir(d)) if os.path.isdir(d) else []:
    if not f.endswith(".png"):
        continue
    if f == "00.png":
        ours[0] = f
    else:
        part = f.split("-")[1] if "-" in f else None
        if part and part.isdigit():
            ours[int(part)] = f

print(f"{name}: {len(labels)} steps")
for i, label, r in rows:
    o = load(f"{d}/{ours[i]}") if i in ours else None
    tag = "ok"
    if r is None or o is None:
        tag = f"MISSING{' ref' if r is None else ''}{' ours' if o is None else ''}"
        print(f"  {i:02d} {label:<28} {tag}")
        continue
    pair = Image.new("RGB", (w * 2 + 12, h + 26), (24, 24, 27))
    pair.paste(r, (0, 26)); pair.paste(o, (w + 12, 26))
    dr = ImageDraw.Draw(pair)
    dr.text((6, 7), f"Qt: {label}", fill=(160, 200, 255))
    dr.text((w + 18, 7), f"ours: {label}", fill=(255, 200, 160))
    pair.save(f"{out}/pairs/{name}/{i:02d}.png")
    # A step the REFERENCE did not react to is a hint that the gesture
    # missed whatever it was aimed at — a flat Δ down the column then reads
    # as agreement when it is really two engines both doing nothing. (Some
    # steps legitimately restore the previous frame: a release, a hover, a
    # drag back where it came from.)
    flag = ""
    if i > 0 and not changed(rows[i - 1][2], r):
        flag = "   (reference unchanged from the previous step)"
    print(f"  {i:02d} {label:<28} d={rms(r, o):5.1f}{flag}")
