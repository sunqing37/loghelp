#!/usr/bin/env python3
"""Build or verify the pinned, offline Markdown renderer embedded in LogHelp."""
import argparse
import hashlib
import io
import json
from pathlib import Path
import shutil
import subprocess
import zipfile

ROOT = Path(__file__).resolve().parent.parent
SOURCE = ROOT / "scripts/markdown"
TARGET = ROOT / "internal/markdownassets/runtime.zip"


def source_hashes():
    # Git may check text files out as CRLF on Windows; newline conversion must
    # not make the committed offline runtime appear stale on another platform.
    return {path.name: hashlib.sha256(path.read_bytes().replace(b"\r\n",b"\n")).hexdigest()
            for path in sorted(SOURCE.iterdir()) if path.is_file()}


def verify():
    try:
        with zipfile.ZipFile(TARGET) as package:
            if json.loads(package.read("sources.json")) == source_hashes():
                return
    except (OSError, KeyError, ValueError, zipfile.BadZipFile):
        pass
    raise ValueError("Markdown 内嵌资源缺失或过期，请执行 python -X utf8 scripts/prepare-markdown.py --build")


def build():
    work = ROOT / ".tools/markdown-renderer"
    work.mkdir(parents=True, exist_ok=True)
    for name in ("package.json", "package-lock.json", "entry.mjs"):
        shutil.copyfile(SOURCE / name, work / name)
    npm = shutil.which("npm.cmd") or shutil.which("npm")
    node = shutil.which("node")
    if not npm or not node:
        raise ValueError("重新生成 Markdown 资源需要 Node.js 和 npm；普通 Go 构建无需安装它们。")
    subprocess.run([npm, "ci", "--ignore-scripts", "--no-audit", "--no-fund"], cwd=work, check=True)
    subprocess.run([node, "node_modules/esbuild/bin/esbuild", "entry.mjs", "--bundle", "--minify",
                    "--format=esm", "--target=chrome110", "--outfile=bundle.js"], cwd=work, check=True)
    modules = work / "node_modules"
    files = {"bundle.js": (work / "bundle.js").read_bytes(), "style.css": (SOURCE / "style.css").read_bytes(),
             "katex.css": (modules / "katex/dist/katex.min.css").read_bytes(),
             "sources.json": json.dumps(source_hashes(), sort_keys=True).encode()}
    for path in (modules / "katex/dist/fonts").glob("*.woff2"):
        files[f"fonts/{path.name}"] = path.read_bytes()
    notices = []
    for path in sorted(modules.rglob("*")):
        if path.is_file() and path.name.lower().startswith(("license", "copying", "ofl")) and path.stat().st_size < 100000:
            notices.append(f"\n--- {path.relative_to(modules)} ---\n" + path.read_text(encoding="utf-8", errors="replace"))
    files["THIRD-PARTY-NOTICES.txt"] = "".join(notices).encode("utf-8")
    buffer = io.BytesIO()
    with zipfile.ZipFile(buffer, "w", compression=zipfile.ZIP_DEFLATED, compresslevel=9) as package:
        for name, payload in sorted(files.items()):
            entry = zipfile.ZipInfo(name, date_time=(2025, 1, 1, 0, 0, 0))
            entry.compress_type = zipfile.ZIP_DEFLATED
            package.writestr(entry, payload)
    TARGET.parent.mkdir(parents=True, exist_ok=True)
    TARGET.write_bytes(buffer.getvalue())
    verify()
    print(f"Markdown runtime ready: {TARGET} ({TARGET.stat().st_size} bytes)")


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--build", action="store_true")
    args = parser.parse_args()
    build() if args.build else verify()
