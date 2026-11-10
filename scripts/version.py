#!/usr/bin/env python3
# /// script
# requires-python = ">=3.12"
# ///

import argparse
import json
import re
import subprocess
import sys
from datetime import date
from pathlib import Path


def find_project_root(start: Path) -> Path:
    current = start.resolve()
    while current != current.parent:
        if (current / ".git").exists():
            return current
        current = current.parent
    print("Error: could not find .git directory in any parent", file=sys.stderr)
    sys.exit(1)


def git_commit_count(root: Path) -> int:
    try:
        result = subprocess.run(
            ["git", "rev-list", "--count", "HEAD"],
            capture_output=True,
            text=True,
            check=True,
            cwd=root,
        )
        return int(result.stdout.strip())
    except subprocess.CalledProcessError as e:
        print(f"Error: git rev-list failed: {e.stderr.strip()}", file=sys.stderr)
        sys.exit(1)


def compute_calver(root: Path) -> str:
    today = date.today()
    commits = git_commit_count(root)
    return f"{today.year}.{today.month}.{commits}"


def discover_config_files(root: Path) -> list[Path]:
    candidates = [
        root / "package.json",
        root / "Cargo.toml",
        root / "pyproject.toml",
        root / "tauri.conf.json",
        root / "src-tauri" / "tauri.conf.json",
        root / "VERSION",
    ]
    return [p for p in candidates if p.is_file()]


def _detect_json_indent(text: str) -> int:
    for line in text.splitlines()[1:]:
        stripped = line.lstrip()
        if stripped:
            return len(line) - len(stripped)
    return 2


def stamp_json_file(path: Path, version: str, dry_run: bool) -> str | None:
    with open(path, encoding="utf-8") as f:
        raw = f.read()

    data = json.loads(raw)

    if "version" not in data:
        return None

    old_version = data["version"]
    if old_version == version:
        return None

    if not dry_run:
        indent = _detect_json_indent(raw)
        data["version"] = version
        with open(path, "w", encoding="utf-8") as f:
            json.dump(data, f, indent=indent, ensure_ascii=False)
            f.write("\n")

    return old_version


def stamp_toml_file(path: Path, version: str, dry_run: bool) -> str | None:
    with open(path, encoding="utf-8") as f:
        content = f.read()

    pattern = re.compile(r'^(version\s*=\s*)"([^"]*)"', re.MULTILINE)
    match = pattern.search(content)
    if not match:
        return None

    old_version = match.group(2)
    if old_version == version:
        return None

    if not dry_run:
        new_content = pattern.sub(rf'\g<1>"{version}"', content, count=1)
        with open(path, "w", encoding="utf-8") as f:
            f.write(new_content)

    return old_version


def stamp_version_file(path: Path, version: str, dry_run: bool) -> str | None:
    with open(path, encoding="utf-8") as f:
        content = f.read().strip()

    # Only stamp if existing content looks like a version. This avoids
    # rewriting files named VERSION that hold something else (build numbers,
    # tag-prefixed strings, multi-line metadata).
    if not re.match(r"^\d+\.\d+\.\d+", content):
        return None

    if content == version:
        return None

    if not dry_run:
        with open(path, "w", encoding="utf-8") as f:
            f.write(version + "\n")

    return content


def stamp_config_file(path: Path, version: str, dry_run: bool) -> str | None:
    if path.suffix == ".json":
        return stamp_json_file(path, version, dry_run)
    elif path.suffix == ".toml":
        return stamp_toml_file(path, version, dry_run)
    elif path.name == "VERSION":
        return stamp_version_file(path, version, dry_run)
    return None


def unreleased_bounds(lines: list[str]) -> tuple[int, int] | None:
    """Return (heading index, end index) of the [Unreleased] section."""
    for i, line in enumerate(lines):
        if re.match(r"^## \[Unreleased\]", line):
            for j in range(i + 1, len(lines)):
                if re.match(r"^## ", lines[j]):
                    return i, j
            return i, len(lines)
    return None


def released_entries(root: Path, ref: str) -> set[str] | None:
    """Entry lines under [Unreleased] in CHANGELOG.md at a git ref.

    CI tags releases without stamping the changelog, so whatever sat under
    [Unreleased] at the tagged commit is exactly what that release shipped.
    """
    result = subprocess.run(
        ["git", "show", f"{ref}:CHANGELOG.md"],
        capture_output=True,
        text=True,
        cwd=root,
    )
    if result.returncode != 0:
        return None
    lines = result.stdout.splitlines(keepends=True)
    bounds = unreleased_bounds(lines)
    if bounds is None:
        return set()
    start, end = bounds
    return {
        line.strip()
        for line in lines[start + 1 : end]
        if line.strip() and not line.startswith("#")
    }


def group_entries(section: list[str]) -> list[tuple[str | None, list[str]]]:
    """Split a section into (### heading, entry lines) groups, dropping blanks."""
    groups: list[tuple[str | None, list[str]]] = [(None, [])]
    for line in section:
        if line.startswith("### "):
            groups.append((line, []))
        elif line.strip():
            groups[-1][1].append(line)
    return [g for g in groups if g[1]]


def render_groups(groups: list[tuple[str | None, list[str]]]) -> list[str]:
    out: list[str] = []
    for heading, entries in groups:
        if heading:
            out += [heading, "\n"]
        out += entries + ["\n"]
    return out


def stamp_changelog(root: Path, version: str, dry_run: bool, released_ref: str | None = None) -> bool:
    """Move released [Unreleased] entries under a [version] heading.

    With released_ref, only entries present under [Unreleased] at that git ref
    move; anything added since stays unreleased. Without it, all entries move.
    """
    changelog = root / "CHANGELOG.md"
    if not changelog.is_file():
        print("Warning: CHANGELOG.md not found, skipping", file=sys.stderr)
        return False

    with open(changelog, encoding="utf-8") as f:
        lines = f.readlines()

    if any(re.match(rf"^## \[{re.escape(version)}\]", line) for line in lines):
        print(f"[{version}] is already in CHANGELOG.md, skipping changelog stamp", file=sys.stderr)
        return False

    bounds = unreleased_bounds(lines)
    if bounds is None:
        print(
            "Warning: no '## [Unreleased]' heading found in CHANGELOG.md, skipping",
            file=sys.stderr,
        )
        return False
    start, end = bounds

    released = None
    if released_ref:
        released = released_entries(root, released_ref)
        if released is None:
            print(f"Error: could not read CHANGELOG.md at {released_ref}", file=sys.stderr)
            sys.exit(1)

    kept: list[tuple[str | None, list[str]]] = []
    shipped: list[tuple[str | None, list[str]]] = []
    for heading, entries in group_entries(lines[start + 1 : end]):
        if released is None:
            shipped.append((heading, entries))
            continue
        old = [e for e in entries if e.strip() in released]
        new = [e for e in entries if e.strip() not in released]
        if old:
            shipped.append((heading, old))
        if new:
            kept.append((heading, new))

    if not shipped:
        print("No released entries under [Unreleased], skipping changelog stamp", file=sys.stderr)
        return False

    today_str = date.today().isoformat()
    section = ["## [Unreleased]\n", "\n"] + render_groups(kept)
    section += [f"## [{version}] - {today_str}\n", "\n"] + render_groups(shipped)
    lines[start:end] = section

    if not dry_run:
        with open(changelog, "w", encoding="utf-8") as f:
            f.writelines(lines)

    return True


def cmd_version(_args: argparse.Namespace) -> None:
    root = find_project_root(Path(__file__).parent)
    version = compute_calver(root)
    print(version)


def cmd_stamp(args: argparse.Namespace) -> None:
    root = find_project_root(Path(__file__).parent)
    version = args.version if args.version else compute_calver(root)
    dry_run = args.dry_run

    prefix = "Dry run for" if dry_run else "Stamping"
    print(f"{prefix} version {version}...", file=sys.stderr)

    changes_made = False

    if not args.no_changelog:
        changed = stamp_changelog(root, version, dry_run, args.released_at)
        if changed:
            today_str = date.today().isoformat()
            suffix = " (would change)" if dry_run else ""
            print(
                f"  CHANGELOG.md: [Unreleased] -> [{version}] - {today_str}{suffix}",
                file=sys.stderr,
            )
            changes_made = True

    if not args.changelog_only:
        for config_path in discover_config_files(root):
            old_version = stamp_config_file(config_path, version, dry_run)
            if old_version is not None:
                rel = config_path.relative_to(root)
                suffix = " (would change)" if dry_run else ""
                print(f"  {rel}: {old_version} -> {version}{suffix}", file=sys.stderr)
                changes_made = True

    if dry_run:
        print("No files modified (dry run).", file=sys.stderr)
    elif changes_made:
        print("Done.", file=sys.stderr)
    else:
        print("No files needed updating.", file=sys.stderr)


def main() -> None:
    parser = argparse.ArgumentParser(description="CalVer versioning and changelog stamping")
    subparsers = parser.add_subparsers(dest="command", required=True)

    subparsers.add_parser("version", help="Compute and print the CalVer version")

    stamp_parser = subparsers.add_parser("stamp", help="Stamp version into changelog and config files")
    stamp_parser.add_argument("--version", help="Manual version override")
    stamp_parser.add_argument("--dry-run", action="store_true", help="Preview changes without modifying files")
    stamp_parser.add_argument("--changelog-only", action="store_true", help="Only stamp CHANGELOG.md")
    stamp_parser.add_argument("--no-changelog", action="store_true", help="Skip CHANGELOG.md stamping")
    stamp_parser.add_argument(
        "--released-at",
        metavar="REF",
        help="Only stamp entries already under [Unreleased] at this git ref (e.g. the release tag)",
    )

    args = parser.parse_args()

    if args.command == "version":
        cmd_version(args)
    elif args.command == "stamp":
        cmd_stamp(args)


if __name__ == "__main__":
    main()
