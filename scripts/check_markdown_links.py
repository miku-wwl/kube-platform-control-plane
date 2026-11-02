#!/usr/bin/env python3
"""Check that inline local Markdown links and images resolve within the repo."""

from __future__ import annotations

import argparse
import re
import sys
from pathlib import Path
from urllib.parse import unquote, urlsplit


LINK = re.compile(
    r"!?\[[^\]]*\]\(\s*(<[^>]+>|(?:\\.|[^\s)])+)"
    r"(?:\s+(?:\"[^\"]*\"|'[^']*'|\([^)]*\)))?\s*\)"
)
FENCE = re.compile(r"^\s*(```+|~~~+)")
INLINE_CODE = re.compile(r"(`+).*?\1", re.DOTALL)
IGNORED_DIRS = {".git", "node_modules", "vendor", "dist", "build"}


def markdown_files(root: Path) -> list[Path]:
    return sorted(
        path
        for path in root.rglob("*")
        if path.is_file()
        and path.suffix.lower() in {".md", ".markdown"}
        and not any(part in IGNORED_DIRS for part in path.relative_to(root).parts)
    )


def without_fenced_code(content: str) -> str:
    result: list[str] = []
    fence: str | None = None
    for line in content.splitlines():
        match = FENCE.match(line)
        if match:
            marker = match.group(1)
            if fence is None:
                fence = marker[0]
            elif marker[0] == fence:
                fence = None
            continue
        if fence is None:
            result.append(line)
    return "\n".join(result)


def link_target(raw: str) -> str | None:
    target = raw[1:-1] if raw.startswith("<") and raw.endswith(">") else raw
    target = unquote(target.replace("\\ ", " "))
    parsed = urlsplit(target)
    if parsed.scheme or parsed.netloc or not parsed.path:
        return None
    return parsed.path


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("--root", type=Path, default=Path(__file__).resolve().parents[1])
    parser.add_argument("files", nargs="*", type=Path)
    args = parser.parse_args()
    root = args.root.resolve()
    files = [((root / path).resolve() if not path.is_absolute() else path.resolve()) for path in args.files]
    if not files:
        files = markdown_files(root)

    checked = 0
    missing: list[str] = []
    for source in files:
        if not source.is_file() or source.suffix.lower() not in {".md", ".markdown"}:
            continue
        text = INLINE_CODE.sub("", without_fenced_code(source.read_text(encoding="utf-8")))
        for match in LINK.finditer(text):
            target = link_target(match.group(1))
            if target is None:
                continue
            checked += 1
            candidate = (root / target.lstrip("/")) if target.startswith("/") else (source.parent / target)
            if not candidate.exists():
                rel_source = source.relative_to(root).as_posix()
                missing.append(f"{rel_source}: {target}")

    print(f"Checked {checked} local Markdown links in {len(files)} files; missing: {len(missing)}")
    for item in missing:
        print(item)
    return 1 if missing else 0


if __name__ == "__main__":
    sys.exit(main())
