#!/usr/bin/env python3
"""Package the verified Windows amd64 build; never invokes a compiler."""

import argparse
import hashlib
import importlib.util
import json
from pathlib import Path
import subprocess
import sys
import zipfile


ROOT = Path(__file__).resolve().parents[2]


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.parse_args()
    version = (ROOT / "VERSION").read_text(encoding="utf-8-sig").strip()
    revision_text = (ROOT / "REVISION").read_text(encoding="utf-8-sig").strip()
    if not revision_text.isascii() or not revision_text.isdigit() or len(revision_text) > 1 and revision_text[0] == "0":
        raise ValueError("REVISION 必须是非负整数，且不能包含前导零。")
    revision = int(revision_text)
    if revision > (1 << 64) - 1:
        raise ValueError("REVISION 超出程序支持范围。")
    parts = version.split(".")
    if len(parts) != 3 or any(not part.isascii() or not part.isdigit() or len(part) > 1 and part[0] == "0" for part in parts):
        raise ValueError("VERSION 必须是 major.minor.patch 正式版本号。")
    spec = importlib.util.spec_from_file_location("loghelp_build_info", ROOT / "scripts/build.py")
    build_info = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(build_info)
    stamp = json.loads((ROOT / "artifacts/desktop-windows-amd64.json").read_text(encoding="utf-8-sig"))
    executable = ROOT / "bin/loghelp.exe"
    exe_hash = hashlib.sha256(executable.read_bytes()).hexdigest()
    if stamp.get("Fingerprint") != build_info.fingerprint(True) or stamp.get("OutputHash") != exe_hash:
        raise ValueError("源码/版本/修订号或 EXE 与成功构建记录不一致，请先完成已授权的桌面构建。")
    commit = subprocess.run(["git", "rev-parse", "HEAD"], cwd=ROOT, capture_output=True, encoding="utf-8", check=True).stdout.strip()
    suffix = f"-r{revision}" if revision else ""
    basename = f"loghelp-{version}-windows-amd64{suffix}"
    output = ROOT / "dist" / version
    output.mkdir(parents=True, exist_ok=True)
    archive = output / f"{basename}.zip"
    if archive.exists():
        raise ValueError(f"已存在 {archive.name}；不要覆盖既有修订，请复用已核对的包或递增 REVISION。")
    files = [executable, ROOT / "VERSION", ROOT / "REVISION", ROOT / "README.md", ROOT / "CHANGELOG.md", *sorted((ROOT / "docs").rglob("*.md"))]
    metadata = {"version": version, "revision": revision, "os": "windows", "arch": "amd64", "exe_sha256": exe_hash, "source_commit": commit, "build_completed": stamp["Completed"]}
    archive_created = False
    checksum_created = False
    checksum = output / f"SHA256SUMS{suffix}.txt"
    try:
        package_file = zipfile.ZipFile(archive, "x", compression=zipfile.ZIP_DEFLATED, compresslevel=9)
        archive_created = True
        with package_file as package:
            for path in files:
                relative = "loghelp.exe" if path == executable else path.relative_to(ROOT).as_posix()
                package.write(path, f"{basename}/{relative}")
            package.writestr(f"{basename}/SHA256SUMS.txt", f"{exe_hash}  loghelp.exe\n")
            package.writestr(f"{basename}/release.json", json.dumps(metadata, ensure_ascii=False, indent=2) + "\n")
        with zipfile.ZipFile(archive) as package:
            if package.testzip() is not None or hashlib.sha256(package.read(f"{basename}/loghelp.exe")).hexdigest() != exe_hash:
                raise ValueError("压缩包完整性或 EXE 哈希不一致。")
            for filename, value in (("VERSION", version), ("REVISION", revision_text)):
                if package.read(f"{basename}/{filename}").decode("utf-8").strip() != value:
                    raise ValueError(f"包内 {filename} 不一致。")
        digest = hashlib.sha256(archive.read_bytes()).hexdigest()
        with checksum.open("x", encoding="utf-8", newline="\n") as checksum_file:
            checksum_created = True
            checksum_file.write(f"{digest}  {archive.name}\n")
    except BaseException:
        if archive_created:
            archive.unlink(missing_ok=True)
        if checksum_created:
            checksum.unlink(missing_ok=True)
        raise
    print(json.dumps({**metadata, "archive": str(archive), "bytes": archive.stat().st_size, "sha256": digest, "checksum": str(checksum)}, ensure_ascii=False))


if __name__ == "__main__":
    for stream in (sys.stdout, sys.stderr):
        if hasattr(stream, "reconfigure"):
            stream.reconfigure(encoding="utf-8", errors="replace")
    try:
        main()
    except (OSError, ValueError, subprocess.CalledProcessError) as error:
        print(f"打包失败：{error}", file=sys.stderr)
        sys.exit(1)
