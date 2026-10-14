#!/usr/bin/env python3
"""Package verified desktop builds without compiling; archives are immutable."""

import argparse
import hashlib
import importlib.util
import json
from pathlib import Path
import subprocess
import sys
import zipfile

ROOT = Path(__file__).resolve().parents[2]
TARGETS = [(system, arch) for system in ("windows", "darwin", "linux") for arch in ("amd64", "arm64")]


def git(*args):
    return subprocess.check_output(["git", *args], cwd=ROOT, encoding="utf-8").strip()


def sha(data):
    return hashlib.sha256(data).hexdigest()


def verify_archive(archive, basename, files, metadata):
    with zipfile.ZipFile(archive) as package:
        if package.testzip() is not None:
            raise ValueError(f"压缩包损坏：{archive.name}")
        expected = {f"{basename}/{name}" for name in files}
        expected.update({f"{basename}/release.json", f"{basename}/SHA256SUMS.txt"})
        if set(package.namelist()) != expected:
            raise ValueError(f"已有包内容清单不一致：{archive.name}")
        for name, path in files.items():
            if package.read(f"{basename}/{name}") != path.read_bytes():
                raise ValueError(f"已有包内容不一致：{archive.name}/{name}；禁止覆盖，请递增修订号。")
        previous = json.loads(package.read(f"{basename}/release.json"))
        if previous != metadata:
            raise ValueError(f"已有包来源或构建元数据不一致：{archive.name}")
        executable = "loghelp.exe" if metadata["os"] == "windows" else "loghelp"
        checks = f"{metadata['exe_sha256']}  {executable}\n"
        if package.read(f"{basename}/SHA256SUMS.txt").decode("utf-8") != checks:
            raise ValueError("包内可执行文件校验记录不一致。")
        if metadata["os"] != "windows" and not (package.getinfo(f"{basename}/{executable}").external_attr >> 16) & 0o111:
            raise ValueError("Unix 可执行权限缺失。")


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--all", action="store_true", help="Package all six platforms serially.")
    parser.add_argument("--target-os", choices=("windows", "darwin", "linux"), default="windows")
    parser.add_argument("--arch", choices=("amd64", "arm64"), default="amd64")
    args = parser.parse_args()
    version = (ROOT / "VERSION").read_text(encoding="utf-8-sig").strip()
    revision_text = (ROOT / "REVISION").read_text(encoding="utf-8-sig").strip()
    values = [*version.split("."), revision_text]
    if len(version.split(".")) != 3 or any(not value.isascii() or not value.isdigit() or len(value) > 1 and value[0] == "0" for value in values):
        raise ValueError("VERSION 必须为三段正式版本号，REVISION 为无前导零的非负整数。")
    revision = int(revision_text)
    if revision > (1 << 64) - 1:
        raise ValueError("REVISION 超出程序支持范围。")
    if git("status", "--porcelain", "--untracked-files=normal"):
        raise ValueError("发布源码尚未完整提交；请先核对工作区并提交，避免源码来源错位。")
    spec = importlib.util.spec_from_file_location("loghelp_build_info", ROOT / "scripts/build.py")
    build_info = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(build_info)
    fingerprint = build_info.fingerprint(True)
    commit = git("rev-parse", "HEAD")
    suffix = f"-r{revision}" if revision else ""
    output = ROOT / "dist" / version
    output.mkdir(parents=True, exist_ok=True)
    # Validate every build before creating any archive.
    plans = []
    for system, arch in TARGETS if args.all else [(args.target_os, args.arch)]:
        stamp = json.loads((ROOT / f"artifacts/desktop-{system}-{arch}.json").read_text(encoding="utf-8-sig"))
        executable = build_info.output_path(system, arch)
        digest = sha(executable.read_bytes())
        if (stamp.get("Fingerprint") != fingerprint or stamp.get("OutputHash") != digest
                or stamp.get("GOOS") != system or stamp.get("GOARCH") != arch
                or stamp.get("Version") != version or stamp.get("Revision") != revision
                or stamp.get("CGO_ENABLED") != "0"):
            raise ValueError(f"{system}/{arch} 与成功构建记录不一致，请完成已授权构建。")
        info = subprocess.check_output(["go", "version", "-m", str(executable)], cwd=ROOT, encoding="utf-8")
        if any(f"{key}={value}" not in info for key, value in [("GOOS", system), ("GOARCH", arch), ("CGO_ENABLED", "0")]):
            raise ValueError(f"{system}/{arch} 的 Go 二进制构建信息不一致。")
        filename = "loghelp.exe" if system == "windows" else "loghelp"
        docs = [ROOT / "VERSION", ROOT / "REVISION", ROOT / "README.md", ROOT / "CHANGELOG.md", *sorted((ROOT / "docs").rglob("*.md"))]
        files = {filename: executable, **{path.relative_to(ROOT).as_posix(): path for path in docs}}
        metadata = {"version": version, "revision": revision, "os": system, "arch": arch,
                    "exe_sha256": digest, "source_commit": commit, "build_completed": stamp["Completed"],
                    "cgo_enabled": False, "build_host": f"{stamp['HostOS']}/{stamp['HostArch']}"}
        basename = f"loghelp-{version}-{system}-{arch}{suffix}"
        plans.append((output / f"{basename}.zip", basename, files, metadata, filename))
    created = []
    results = []
    try:
        for archive, basename, files, metadata, filename in plans:
            if not archive.exists():
                with zipfile.ZipFile(archive, "x", compression=zipfile.ZIP_DEFLATED, compresslevel=9) as package:
                    created.append(archive)
                    for name, path in files.items():
                        entry = zipfile.ZipInfo(f"{basename}/{name}")
                        entry.create_system = 3
                        entry.external_attr = (0o100755 if name == filename else 0o100644) << 16
                        entry.compress_type = zipfile.ZIP_DEFLATED
                        package.writestr(entry, path.read_bytes(), compresslevel=9)
                    package.writestr(f"{basename}/SHA256SUMS.txt", f"{metadata['exe_sha256']}  {filename}\n")
                    package.writestr(f"{basename}/release.json", json.dumps(metadata, ensure_ascii=False, indent=2) + "\n")
            verify_archive(archive, basename, files, metadata)
            result = {**metadata, "archive": str(archive), "bytes": archive.stat().st_size, "sha256": sha(archive.read_bytes())}
            results.append(result)
        checksum = output / f"SHA256SUMS{suffix}.txt"
        # Merge verified packages from separate platform invocations, never replace their bytes.
        lines = {}
        if checksum.exists():
            for line in checksum.read_text(encoding="utf-8").splitlines():
                digest, filename = line.split("  ", 1)
                if Path(filename).name != filename or sha((output / filename).read_bytes()) != digest:
                    raise ValueError("已有包外校验文件不一致。")
                lines[filename] = digest
        for result in results:
            filename = Path(result["archive"]).name
            if filename in lines and lines[filename] != result["sha256"]:
                raise ValueError("同名发布包哈希发生变化，禁止覆盖。")
            lines[filename] = result["sha256"]
        temporary = checksum.with_suffix(".tmp")
        temporary.write_text("".join(f"{lines[name]}  {name}\n" for name in sorted(lines)), encoding="utf-8")
        temporary.replace(checksum)
    except BaseException:
        for archive in created:
            archive.unlink(missing_ok=True)
        raise
    print(json.dumps({"packages": results, "checksum": str(checksum)}, ensure_ascii=False, indent=2))


if __name__ == "__main__":
    for stream in (sys.stdout, sys.stderr):
        if hasattr(stream, "reconfigure"):
            stream.reconfigure(encoding="utf-8", errors="replace")
    try:
        main()
    except (OSError, ValueError, subprocess.CalledProcessError) as error:
        print(f"打包失败：{error}", file=sys.stderr)
        sys.exit(1)
