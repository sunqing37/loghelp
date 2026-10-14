#!/usr/bin/env python3
"""Build LogHelp with Go and the Python standard library (no CGO)."""

import argparse
import contextlib
from datetime import datetime
import hashlib
import json
import os
from pathlib import Path
import re
import shutil
import subprocess
import sys
import time


ROOT = Path(__file__).resolve().parent.parent
BUILD_NAMES = {"compile", "link", "gcc", "clang", "gradle", "ninja", "cmake"}
GRADLE_EVENT = re.compile(r"daemon has (started|finished) executing the build")


def capture(command, env=None):
    result = subprocess.run(
        command, cwd=ROOT, env=env, stdout=subprocess.PIPE,
        stderr=subprocess.PIPE, encoding="utf-8", errors="replace",
        creationflags=subprocess.CREATE_NO_WINDOW if os.name == "nt" else 0,
        check=True,
    )
    return result.stdout.strip()


def processes():
    if os.name == "nt":
        powershell = shutil.which("powershell") or shutil.which("pwsh")
        if not powershell:
            raise RuntimeError("PowerShell is required for Windows process inspection.")
        command = (
            "[Console]::OutputEncoding = [System.Text.UTF8Encoding]::new($false); "
            "$ErrorActionPreference = 'Stop'; "
            "@(Get-CimInstance Win32_Process | "
            "Select-Object ProcessId,Name,CommandLine) | ConvertTo-Json -Compress"
        )
        data = json.loads(capture([powershell, "-NoProfile", "-NonInteractive", "-Command", command]))
        if isinstance(data, dict):
            data = [data]
        return [(p["ProcessId"], p["Name"], p["CommandLine"] or "") for p in data]
    if sys.platform.startswith("linux"):
        result = []
        for entry in Path("/proc").iterdir():
            if not entry.name.isdecimal():
                continue
            try:
                name = (entry / "comm").read_text(encoding="utf-8").strip()
                command = (entry / "cmdline").read_bytes().replace(b"\0", b" ").decode("utf-8", "replace")
                result.append((int(entry.name), name, command))
            except (FileNotFoundError, ProcessLookupError, PermissionError):
                continue
        return result
    result = []
    for line in capture(["ps", "-axo", "pid=,comm=,args="]).splitlines():
        fields = line.strip().split(None, 2)
        if len(fields) == 3:
            result.append((int(fields[0]), Path(fields[1]).name, fields[2]))
    return result


def active_java_build(pid, command):
    if re.search(r"GradleWorkerMain|org\.gradle\.launcher\.GradleMain|org\.jetbrains\.kotlin\.cli", command):
        return True
    if "GradleDaemon" not in command:
        return False
    gradle_root = Path(os.environ.get("GRADLE_USER_HOME", str(Path.home() / ".gradle")))
    logs = list((gradle_root / "daemon").glob(f"**/daemon-{pid}.out.log"))
    if not logs:
        return True
    try:
        log = max(logs, key=lambda item: item.stat().st_mtime)
        event = None
        with log.open(encoding="utf-8", errors="replace") as stream:
            for line in stream:
                match = GRADLE_EVENT.search(line)
                if match:
                    event = match.group(1)
        return event != "finished"
    except OSError:
        return True


def busy_builds():
    busy = []
    for pid, raw_name, command in processes():
        if pid == os.getpid():
            continue
        name = Path(raw_name).name.lower().removesuffix(".exe")
        if (name in BUILD_NAMES
                or name == "go" and re.search(r"\s(build|run|install|test)(\s|$)", command)
                or name == "java" and active_java_build(pid, command)):
            busy.append(f"{raw_name} (PID {pid})")
    return busy


def wait_for_builds(deadline):
    while True:
        busy = busy_builds()
        if not busy:
            return
        if time.monotonic() >= deadline:
            raise RuntimeError("Another build is still running: " + ", ".join(busy))
        print("Waiting for another build: " + ", ".join(busy), flush=True)
        time.sleep(min(5, max(0, deadline - time.monotonic())))


@contextlib.contextmanager
def build_lock(deadline):
    """Serialize Python invocations; the OS releases the lock after a crash."""
    path = ROOT / "artifacts" / "build-python.lock"
    path.parent.mkdir(parents=True, exist_ok=True)
    with path.open("a+b") as stream:
        if path.stat().st_size == 0:
            stream.write(b"\0")
            stream.flush()
        if os.name == "nt":
            import msvcrt
        else:
            import fcntl
        while True:
            try:
                stream.seek(0)
                if os.name == "nt":
                    msvcrt.locking(stream.fileno(), msvcrt.LK_NBLCK, 1)
                else:
                    fcntl.flock(stream.fileno(), fcntl.LOCK_EX | fcntl.LOCK_NB)
                break
            except (BlockingIOError, OSError):
                if time.monotonic() >= deadline:
                    raise RuntimeError("Another Python build script still holds the build lock.")
                print("Waiting for another Python build script...", flush=True)
                time.sleep(min(5, max(0, deadline - time.monotonic())))
        try:
            yield
        finally:
            stream.seek(0)
            if os.name == "nt":
                msvcrt.locking(stream.fileno(), msvcrt.LK_UNLCK, 1)
            else:
                fcntl.flock(stream.fileno(), fcntl.LOCK_UN)


def fingerprint(desktop):
    files = list((ROOT / "internal").rglob("*.go"))
    files.extend(ROOT.glob("*.go"))
    files.append(ROOT / "VERSION")
    if desktop:
        files.extend((ROOT / "cmd").rglob("*.go"))
    files.extend([ROOT / "go.mod", ROOT / "go.sum"])
    # Match build.ps1 stamps on Windows to avoid recompiling an unchanged app.
    return "\n".join(
        f"{path}:{hashlib.sha256(path.read_bytes()).hexdigest().upper()}"
        for path in sorted(files, key=lambda item: str(item).casefold())
    )


def up_to_date(stamp, source_hash, desktop, output):
    try:
        previous = json.loads(stamp.read_text(encoding="utf-8-sig"))
        if previous.get("Fingerprint") != source_hash:
            return False
        if desktop:
            if not output.is_file():
                return False
            if previous.get("OutputHash"):
                return previous["OutputHash"] == hashlib.sha256(output.read_bytes()).hexdigest()
        return True
    except (OSError, ValueError, AttributeError):
        return False


def compile_go(command, env):
    print("Running: " + subprocess.list2cmdline(command), flush=True)
    with subprocess.Popen(
        command, cwd=ROOT, env=env, stdout=subprocess.PIPE, stderr=subprocess.STDOUT,
        encoding="utf-8", errors="replace",
        creationflags=subprocess.CREATE_NO_WINDOW if os.name == "nt" else 0,
    ) as process:
        try:
            for line in process.stdout:
                print(line, end="", flush=True)
            if process.wait() != 0:
                raise RuntimeError("Compilation failed; the successful-build stamp was not updated.")
        except BaseException:
            if process.poll() is None:
                process.terminate()
                process.wait()
            raise


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--desktop", action="store_true", help="Build the full desktop app after manual approval.")
    parser.add_argument("--run", action="store_true", help="Open the desktop app after --desktop.")
    parser.add_argument("--arch", choices=("amd64", "arm64"), default="amd64")
    parser.add_argument("--target-os", choices=("windows", "linux", "darwin"), help="Defaults to the Go host OS.")
    parser.add_argument("--wait-timeout", type=int, default=600, help="Seconds to wait for other builds (default: 600).")
    parser.add_argument("--dry-run", action="store_true", help="Inspect and print the plan without compiling or opening the app.")
    args = parser.parse_args()
    if args.run and not args.desktop:
        parser.error("--run requires --desktop")
    if args.wait_timeout < 0:
        parser.error("--wait-timeout must be nonnegative")
    go = shutil.which("go")
    if not go:
        raise RuntimeError("Go is not available in PATH.")
    env = os.environ.copy()
    env["CGO_ENABLED"] = "0"
    env["GOARCH"] = args.arch
    host_os = capture([go, "env", "GOHOSTOS"], env)
    target_os = args.target_os or host_os
    if args.desktop and target_os != host_os:
        parser.error("Full desktop builds must run on their target platform; use module checks for other platforms.")
    env["GOOS"] = target_os
    mode = "desktop" if args.desktop else "modules"
    output = ROOT / "bin" / ("loghelp.exe" if target_os == "windows" else "loghelp")
    stamp = ROOT / "artifacts" / f"{mode}-{target_os}-{args.arch}.json"
    command = [go, "build"]
    if args.desktop:
        if target_os == "windows":
            command.extend(["-ldflags", "-H=windowsgui"])
        command.extend(["-o", str(output), "./cmd/loghelp"])
    else:
        command.extend(["./internal/core", "./internal/ui"])
    print(f"Mode: {mode}; target: {target_os}/{args.arch}; CGO_ENABLED=0", flush=True)
    if args.dry_run:
        same = up_to_date(stamp, fingerprint(args.desktop), args.desktop, output)
        print("Source unchanged; compilation would be skipped." if same else "Would run: " + subprocess.list2cmdline(command))
        busy = busy_builds()
        if busy:
            print("Build would wait for: " + ", ".join(busy))
        if args.run:
            print(f"Would open: {output}")
        return
    deadline = time.monotonic() + args.wait_timeout
    with build_lock(deadline):
        wait_for_builds(deadline)
        source_hash = fingerprint(args.desktop)
        if up_to_date(stamp, source_hash, args.desktop, output):
            print("Source unchanged since the successful check; skipping duplicate build.")
        else:
            if args.desktop:
                output.parent.mkdir(parents=True, exist_ok=True)
            compile_go(command, env)
            if fingerprint(args.desktop) != source_hash:
                raise RuntimeError("Sources changed during compilation; no successful-build stamp was saved.")
            record = {"Fingerprint": source_hash, "Completed": datetime.now().astimezone().isoformat(), "Mode": mode}
            if args.desktop:
                record["OutputHash"] = hashlib.sha256(output.read_bytes()).hexdigest()
            temporary = stamp.with_suffix(".tmp")
            temporary.write_text(json.dumps(record, ensure_ascii=False, indent=2), encoding="utf-8")
            temporary.replace(stamp)
            print(f"Build complete: {output}" if args.desktop else "Module compilation passed.")
    if args.run:
        subprocess.Popen([str(output)], cwd=ROOT, env=env)


if __name__ == "__main__":
    for stream in (sys.stdout, sys.stderr):
        if hasattr(stream, "reconfigure"):
            stream.reconfigure(encoding="utf-8", errors="replace")
    try:
        main()
    except subprocess.CalledProcessError as error:
        print(f"Command failed (exit {error.returncode}): {error.stderr or error.stdout or error}", file=sys.stderr)
        sys.exit(1)
    except (OSError, RuntimeError, ValueError) as error:
        print(f"Error: {error}", file=sys.stderr)
        sys.exit(1)
    except KeyboardInterrupt:
        print("Build interrupted.", file=sys.stderr)
        sys.exit(130)
