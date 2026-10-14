#!/usr/bin/env python3
"""Commit staged changes with an explicit UTF-8 message and no shell interpolation."""

from __future__ import annotations

import argparse
from pathlib import Path
import re
import subprocess
import sys


ROOT = Path(__file__).resolve().parents[2]
TITLE = re.compile(r"\[(feat|fix|modify|add|docs|chore)\]（[^（）\r\n]+）[^\r\n]+「[^「」\r\n]+」")
SECTIONS = ("[问题原因]", "[解决方式]", "[影响范围]", "[验证情况]")
AI_TRAILER = re.compile(
    r"^Co-authored-by:\s*(?:Cursor|Claude|Codex|GitHub Copilot|Copilot|AI)\b",
    re.IGNORECASE | re.MULTILINE,
)


def git(*arguments: str, input_text: str | None = None) -> str:
    result = subprocess.run(
        ["git", *arguments], cwd=ROOT, input=input_text, capture_output=True,
        text=True, encoding="utf-8", errors="replace", check=True,
    )
    return result.stdout


def validate(message: str) -> None:
    lines = message.strip().splitlines()
    if not lines or not TITLE.fullmatch(lines[0]):
        raise ValueError("标题须为 [type]（模块）描述「单号或无」，type 为 feat/fix/modify/add/docs/chore。")
    if len(lines) < 3 or lines[1].strip():
        raise ValueError("标题后须空一行，再填写四段提交正文。")
    if AI_TRAILER.search(message):
        raise ValueError("提交正文不能包含自动 AI Co-authored-by trailer。")
    body = "\n".join(lines[2:])
    positions = []
    for section in SECTIONS:
        matches = list(re.finditer(r"^" + re.escape(section) + r"$", body, re.MULTILINE))
        if len(matches) != 1:
            raise ValueError(f"正文须且仅须包含一个 {section}。")
        positions.append(matches[0])
    if [item.start() for item in positions] != sorted(item.start() for item in positions):
        raise ValueError("正文四段顺序须为问题原因、解决方式、影响范围、验证情况。")
    for index, section in enumerate(positions):
        end = positions[index + 1].start() if index + 1 < len(positions) else len(body)
        if not body[section.end():end].strip():
            raise ValueError(f"{SECTIONS[index]} 需要填写实际内容。")


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    group = parser.add_mutually_exclusive_group(required=True)
    group.add_argument("--file", type=Path, help="UTF-8 message file (BOM accepted)")
    group.add_argument("--check", action="store_true", help="Read-only HEAD message validation")
    parser.add_argument("--amend-unpushed", action="store_true", help="Amend task-owned HEAD only when no remote branch or tag contains it")
    arguments = parser.parse_args()
    if arguments.check:
        if arguments.amend_unpushed:
            raise ValueError("--amend-unpushed 仅可与 --file 一起使用。")
        validate(git("show", "-s", "--format=%B", "HEAD"))
        print("PASS: HEAD 提交正文符合规范。")
        return 0
    message = arguments.file.read_text(encoding="utf-8-sig").strip() + "\n"
    validate(message)
    # Detect unresolved merge entries and whitespace errors before Git or hooks mutate HEAD.
    if git("ls-files", "-u").strip():
        raise ValueError("存在未解决的冲突，请先处理并暂存。")
    git("diff", "--cached", "--check")
    result = subprocess.run(
        ["git", "diff", "--cached", "--quiet"], cwd=ROOT, capture_output=True,
        text=True, encoding="utf-8", errors="replace",
    )
    if result.returncode == 0:
        raise ValueError("暂存区没有改动；脚本不自动暂存或创建空提交。")
    if result.returncode != 1:
        raise RuntimeError(result.stderr or "无法读取暂存区。")
    amend = []
    if arguments.amend_unpushed:
        if git("for-each-ref", "--contains=HEAD", "--format=%(refname)", "refs/remotes/", "refs/tags/").strip():
            raise ValueError("HEAD 已被远端分支或标签引用，禁止改写；请创建新的提交。")
        amend = ["--amend"]
    # Use standard Git, retaining hooks and signing; argv and stdin prevent shell expansion.
    print(git("-c", "i18n.commitEncoding=UTF-8", "commit", *amend, "--file=-", input_text=message), end="")
    validate(git("show", "-s", "--format=%B", "HEAD"))
    print("PASS: 提交后正文校验通过。")
    return 0


if __name__ == "__main__":
    for stream in (sys.stdout, sys.stderr):
        if hasattr(stream, "reconfigure"):
            stream.reconfigure(encoding="utf-8", errors="replace")
    try:
        sys.exit(main())
    except (OSError, ValueError, RuntimeError, subprocess.CalledProcessError) as error:
        if isinstance(error, subprocess.CalledProcessError):
            detail = error.stderr or error.stdout or str(error)
        else:
            detail = str(error)
        print(f"FAIL: {detail}", file=sys.stderr)
        sys.exit(1)
