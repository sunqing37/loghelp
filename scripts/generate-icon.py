#!/usr/bin/env python3
"""Generate Windows icons matching internal/ui/style.go's brandIcon (stdlib only)."""

import math
from pathlib import Path
import struct
import zlib


ROOT = Path(__file__).resolve().parent.parent


def png(size):
    rows = bytearray()
    scale = size / 32
    for y in range(size):
        rows.append(0)
        for x in range(size):
            px, py = x / scale, y / scale
            cx, cy = max(6 - px, px - 25, 0), max(6 - py, py - 25, 0)
            pixel = (50, 91, 185, 255)
            if math.hypot(cx, cy) > 6:
                pixel = (0, 0, 0, 0)
            elif 9 <= px < 13 and 7 <= py < 25 or 9 <= px < 24 and 21 <= py < 25:
                pixel = (242, 247, 255, 255)
            rows.extend(pixel)

    def chunk(kind, data):
        return struct.pack(">I", len(data)) + kind + data + struct.pack(">I", zlib.crc32(kind + data))

    return b"\x89PNG\r\n\x1a\n" + chunk(b"IHDR", struct.pack(">IIBBBBB", size, size, 8, 6, 0, 0, 0)) + chunk(b"IDAT", zlib.compress(rows, 9)) + chunk(b"IEND", b"")


if __name__ == "__main__":
    sizes = (16, 24, 32, 48, 64, 128, 256)
    images = [png(size) for size in sizes]
    offset = 6 + 16 * len(sizes)
    directory = bytearray(struct.pack("<HHH", 0, 1, len(sizes)))
    for size, image in zip(sizes, images):
        directory.extend(struct.pack("<BBBBHHII", size % 256, size % 256, 0, 0, 1, 32, len(image), offset))
        offset += len(image)
    assets = ROOT / "assets"
    assets.mkdir(exist_ok=True)
    (assets / "loghelp.ico").write_bytes(directory + b"".join(images))
    (assets / "loghelp.png").write_bytes(images[-1])
    print("Generated assets/loghelp.ico and assets/loghelp.png")
