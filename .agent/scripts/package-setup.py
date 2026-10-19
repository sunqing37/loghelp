"""Windows Setup packaging support, called by package-release.py."""

import hashlib
import json
import os
from pathlib import Path
import shutil
import subprocess
import tempfile
import time

ROOT = Path(__file__).resolve().parents[2]


def digest(path):
    return hashlib.sha256(path.read_bytes()).hexdigest()


def find_compiler(explicit=None):
    candidates = [explicit, os.environ.get("ISCC"), shutil.which("ISCC.exe"),
                  ROOT / ".tools/inno-setup/ISCC.exe"]
    for base in (os.environ.get("ProgramFiles(x86)"), os.environ.get("ProgramFiles"),
                 os.path.join(os.environ.get("LOCALAPPDATA", ""), "Programs")):
        if base:
            candidates.extend(Path(base) / name / "ISCC.exe" for name in ("Inno Setup 7", "Inno Setup 6"))
    for candidate in candidates:
        if candidate and Path(candidate).is_file():
            return Path(candidate).resolve()
    raise ValueError("未找到 Inno Setup 6.4+ 的 ISCC.exe；请通过 --iscc 或 ISCC 环境变量指定。")


def package_setup(output, basename, files, metadata, compiler, build_info, created):
    output.resolve().relative_to(ROOT.resolve())
    script = ROOT / "scripts/installer/loghelp.iss"
    installer = output / f"{basename}-setup.exe"
    record = installer.with_suffix(".json")
    inputs = {"metadata": metadata, "files": {name: digest(path) for name, path in files.items()},
              "script_sha256": digest(script), "icon_sha256": digest(ROOT / "assets/loghelp.ico"),
              "compiler_sha256": digest(compiler)}
    if inputs["files"].get("loghelp.exe") != metadata["exe_sha256"]:
        raise ValueError("Setup 输入 EXE 已偏离成功构建记录。")
    if installer.exists() or record.exists():
        if not installer.exists() or not record.exists():
            raise ValueError(f"Setup 或其构建记录缺失：{installer.name}；禁止覆盖已有产物。")
        previous = json.loads(record.read_text(encoding="utf-8"))
        if previous.get("inputs") != inputs or previous.get("sha256") != digest(installer):
            raise ValueError(f"已有 Setup 内容或来源不同：{installer.name}；请递增修订号。")
        return {**metadata, "archive": str(installer), "bytes": installer.stat().st_size,
                "sha256": previous["sha256"]}
    # Use the same build lock and process checks as Go compilation. The compiler
    # writes into staging so failure cannot leave a plausible final installer.
    deadline = time.monotonic() + 600
    with build_info.build_lock(deadline):
        build_info.wait_for_builds(deadline)
        with tempfile.TemporaryDirectory(prefix="loghelp-setup-", dir=output) as temporary:
            staging = Path(temporary)
            staging.resolve().relative_to(output.resolve())
            payload = staging / "payload"
            for name, source in files.items():
                target = payload / name
                target.parent.mkdir(parents=True, exist_ok=True)
                shutil.copyfile(source, target)
                if digest(target) != inputs["files"][name]:
                    raise ValueError(f"打包期间输入文件发生变化：{name}")
            (payload / "release.json").write_text(json.dumps(metadata, ensure_ascii=False, indent=2) + "\n", encoding="utf-8")
            (payload / "SHA256SUMS.txt").write_text(f"{metadata['exe_sha256']}  loghelp.exe\n", encoding="utf-8")
            command = [str(compiler), "/Qp", f"/DPayloadDir={payload}", f"/DPackageDir={staging}",
                       f"/DPackageName={installer.stem}", f"/DAppVersion={metadata['version']}",
                       f"/DAppRevision={metadata['revision']}", f"/DAppArch={metadata['arch']}", str(script)]
            subprocess.run(command, cwd=ROOT, check=True,
                           creationflags=subprocess.CREATE_NO_WINDOW if os.name == "nt" else 0)
            if digest(script) != inputs["script_sha256"] or digest(ROOT / "assets/loghelp.ico") != inputs["icon_sha256"]:
                raise ValueError("安装脚本或图标在打包期间发生变化。")
            staged_installer = staging / installer.name
            result_hash = digest(staged_installer)
            # Windows rename refuses an existing destination, preserving immutability.
            staged_installer.rename(installer)
            created.append(installer)
            with record.open("x", encoding="utf-8") as stream:
                created.append(record)
                json.dump({"inputs": inputs, "sha256": result_hash}, stream, ensure_ascii=False, indent=2)
                stream.write("\n")
    return {**metadata, "archive": str(installer), "bytes": installer.stat().st_size, "sha256": result_hash}
