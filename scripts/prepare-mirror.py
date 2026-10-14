#!/usr/bin/env python3
"""Prepare pinned, verified scrcpy/FFmpeg files embedded in the Windows EXE."""
import hashlib
import io
from pathlib import Path
import urllib.request
import zipfile

ROOT = Path(__file__).resolve().parent.parent
VERSION = "3.3.4"
DIGEST = "d8a155b7c180b7ca4cdadd40712b8750b63f3aab48cb5b8a2a39ac2d0d4c5d38"
FILES = ("scrcpy-server", "avcodec-61.dll", "avutil-59.dll", "swresample-5.dll")
TARGET = ROOT / "internal/mirrorassets/runtime_windows_amd64.zip"


def prepare():
    archive = ROOT / f".tools/scrcpy-win64-v{VERSION}.zip"
    archive.parent.mkdir(exist_ok=True)
    data = archive.read_bytes() if archive.exists() else b""
    if hashlib.sha256(data).hexdigest() != DIGEST:
        url = f"https://github.com/Genymobile/scrcpy/releases/download/v{VERSION}/scrcpy-win64-v{VERSION}.zip"
        with urllib.request.urlopen(url, timeout=60) as response:
            data = response.read(16 << 20)
        if hashlib.sha256(data).hexdigest() != DIGEST:
            raise ValueError("scrcpy archive SHA-256 mismatch")
        archive.write_bytes(data)
    contents = {}
    with zipfile.ZipFile(io.BytesIO(data)) as source:
        for name in FILES:
            contents[name] = source.read(f"scrcpy-win64-v{VERSION}/{name}")
    licenses = {
        "SCRCPY-LICENSE": f"https://raw.githubusercontent.com/Genymobile/scrcpy/v{VERSION}/LICENSE",
        "FFMPEG-LICENSE.md": "https://raw.githubusercontent.com/FFmpeg/FFmpeg/n7.1.1/LICENSE.md",
        "COPYING.LGPLv2.1": "https://raw.githubusercontent.com/FFmpeg/FFmpeg/n7.1.1/COPYING.LGPLv2.1",
        "COPYING.LGPLv3": "https://raw.githubusercontent.com/FFmpeg/FFmpeg/n7.1.1/COPYING.LGPLv3",
        "DAV1D-COPYING": "https://code.videolan.org/videolan/dav1d/-/raw/1.5.1/COPYING",
        "ZLIB-LICENSE": "https://raw.githubusercontent.com/madler/zlib/v1.3.1/LICENSE",
    }
    # Reuse downloaded notices; the final archive is deterministic.
    for name, url in licenses.items():
        cached = archive.parent / name
        if not cached.exists():
            with urllib.request.urlopen(url, timeout=30) as response:
                cached.write_bytes(response.read(256 << 10))
        contents[name] = cached.read_bytes()
    contents["SOURCE.txt"] = (
        "scrcpy 3.3.4: https://github.com/Genymobile/scrcpy/tree/v3.3.4\n"
        "FFmpeg: https://github.com/FFmpeg/FFmpeg/tree/n7.1.1\n"
        f"Official Windows archive SHA-256: {DIGEST}\n"
    ).encode()
    for target, entries in (
        (TARGET, {name: data for name, data in contents.items() if name.endswith('.dll')}),
        (TARGET.with_name('runtime_common.zip'), {name: data for name, data in contents.items() if not name.endswith('.dll')}),
    ):
        output = io.BytesIO()
        with zipfile.ZipFile(output, "w", compression=zipfile.ZIP_DEFLATED, compresslevel=9) as package:
            for name, payload in sorted(entries.items()):
                entry = zipfile.ZipInfo(name, date_time=(2025, 1, 1, 0, 0, 0))
                entry.compress_type = zipfile.ZIP_DEFLATED
                package.writestr(entry, payload)
        payload = output.getvalue()
        if not target.exists() or target.read_bytes() != payload:
            target.parent.mkdir(parents=True, exist_ok=True)
            temporary = target.with_suffix(".tmp")
            temporary.write_bytes(payload)
            temporary.replace(target)
        print(f"Mirror runtime ready: {target} ({len(payload)} bytes)")


if __name__ == "__main__":
    prepare()
