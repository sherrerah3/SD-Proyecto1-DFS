#!/usr/bin/env python3
"""Reset DFSHA MetaNode metadata without manually wiping DataNode disks.

The script is platform-independent and only removes the MetaNode state snapshot
and, when a shared HA directory is configured, its coordination files. DataNode
block directories are deliberately left untouched. Once the MetaNode starts
with an empty catalog, DataNodes report their disk inventory; the MetaNode then
queues deletion of every block that is no longer referenced. DataNodes remove
those block files through the normal authenticated heartbeat workflow, which
frees filesystem space without secure-erasing the underlying sectors.
"""

from __future__ import annotations

import argparse
import os
import shutil
import sys
from datetime import datetime
from pathlib import Path
from typing import Iterable

SCRIPT_PATH = Path(__file__).resolve()
DEFAULT_ROOT = SCRIPT_PATH.parent.parent
COORDINATION_FILES = ("MetaNode.lock", "heartbeat.txt")
COORDINATION_TEMP_PREFIXES = ("MetaNode.lock.tmp-", "heartbeat.txt.tmp-")
STALE_LOCK_PREFIX = "MetaNode.lock.stale-"


def resolve_path(value: str | Path, root: Path) -> Path:
    """Resolve a configured path relative to the repository root."""
    path = Path(value).expanduser()
    if not path.is_absolute():
        path = root / path
    return path.resolve(strict=False)


def choose_state_path(args: argparse.Namespace, root: Path) -> tuple[Path, Path | None]:
    """Use the same precedence as cmd/metanode/main.go."""
    shared_value = args.shared_dir or os.environ.get("METANODE_SHARED_DIR") or None
    shared_dir = resolve_path(shared_value, root) if shared_value else None

    state_value = args.state_path or os.environ.get("METANODE_STATE_PATH")
    if state_value:
        return resolve_path(state_value, root), shared_dir

    if shared_dir is not None:
        return (shared_dir / "state.json").resolve(strict=False), shared_dir

    data_value = args.data_dir or os.environ.get("METANODE_DATA_DIR")
    if data_value:
        data_dir = resolve_path(data_value, root)
    else:
        node_id = args.metanode_id or os.environ.get("METANODE_ID") or "MetaNode-1"
        data_dir = (root / "data" / "MetaNode" / node_id).resolve(strict=False)

    return (data_dir / "state.json").resolve(strict=False), None


def backup_path(path: Path, timestamp: str) -> Path:
    candidate = path.with_name(f"{path.name}.backup-{timestamp}")
    suffix = 1
    while candidate.exists():
        candidate = path.with_name(f"{path.name}.backup-{timestamp}-{suffix}")
        suffix += 1
    return candidate


def coordination_targets(shared_dir: Path) -> list[Path]:
    """Return only known HA coordination files, never arbitrary directory data."""
    targets = [shared_dir / name for name in COORDINATION_FILES]
    if shared_dir.is_dir():
        for entry in shared_dir.iterdir():
            if entry.name.startswith(STALE_LOCK_PREFIX) or any(
                entry.name.startswith(prefix) for prefix in COORDINATION_TEMP_PREFIXES
            ):
                targets.append(entry)
    return sorted(set(targets))


def existing_files(paths: Iterable[Path]) -> list[Path]:
    result = []
    for path in paths:
        if path.is_file():
            result.append(path)
        elif path.exists():
            raise RuntimeError(f"Expected a file but found a non-file path: {path}")
    return result


def print_candidates(root: Path) -> None:
    """Help locate a state file if the configured/default path was incorrect."""
    data_root = root / "data"
    if not data_root.exists():
        return
    found = sorted(
        candidate.resolve(strict=False)
        for candidate in data_root.rglob("state.json")
        if candidate.is_file()
    )
    if found:
        print("Other state.json files found under the repository data directory:")
        for candidate in found:
            print(f"  {candidate}")
        print("Use --state-path with the intended file if one of these is the active database.")


def main() -> int:
    parser = argparse.ArgumentParser(
        description=(
            "Reset DFSHA metadata. DataNode directories are not manually cleared; "
            "the MetaNode will reconcile their inventories after restart."
        )
    )
    parser.add_argument(
        "--repo-root",
        type=Path,
        default=DEFAULT_ROOT,
        help="repository root (default: inferred from this script location)",
    )
    parser.add_argument("--state-path", help="exact path to the MetaNode state.json")
    parser.add_argument("--data-dir", help="MetaNode data directory; state.json is inside it")
    parser.add_argument("--shared-dir", help="shared HA directory containing state.json and coordination files")
    parser.add_argument("--metanode-id", help="MetaNode ID used with the default data directory")
    parser.add_argument(
        "--keep-ha-coordination",
        action="store_true",
        help="keep HA lock and heartbeat files in a configured shared directory",
    )
    parser.add_argument("--dry-run", action="store_true", help="show targets without changing any files")
    parser.add_argument("--yes", action="store_true", help="skip the interactive confirmation")
    args = parser.parse_args()

    root = args.repo_root.expanduser().resolve(strict=False)
    if not root.is_dir():
        print(f"Error: repository root does not exist or is not a directory: {root}", file=sys.stderr)
        return 2

    state_path, shared_dir = choose_state_path(args, root)
    targets: list[Path] = [state_path]
    if shared_dir is not None and not args.keep_ha_coordination:
        targets.extend(coordination_targets(shared_dir))
    try:
        files = existing_files(dict.fromkeys(targets))
    except RuntimeError as exc:
        print(f"Error: {exc}", file=sys.stderr)
        return 2

    print("DFSHA metadata reset plan")
    print(f"Repository root: {root}")
    print(f"MetaNode state file: {state_path}")
    print(f"Shared HA directory: {shared_dir if shared_dir else '(not configured)'}")
    if args.keep_ha_coordination:
        print("HA coordination files: keep")
    elif shared_dir is not None:
        print("HA coordination files: reset")
    print("DataNode block directories: KEEP (they will reconcile with the empty catalog)")
    if files:
        print("Files to remove (a timestamped backup is created first):")
        for path in files:
            print(f"  {path}")
    else:
        print("No existing state/coordination files were found at the configured paths.")
        if not state_path.exists():
            print_candidates(root)

    if args.dry_run:
        print("Dry run complete; no files were changed.")
        return 0

    print()
    print("IMPORTANT: Stop every MetaNode before continuing.")
    print("DataNodes may remain running, but they need to reach the reset MetaNode to reconcile.")
    if not args.yes:
        try:
            answer = input('Type "RESET DFSHA" to confirm: ').strip()
        except (EOFError, KeyboardInterrupt):
            print("Cancelled; no files were changed.")
            return 1
        if answer != "RESET DFSHA":
            print("Cancelled; no files were changed.")
            return 1

    timestamp = datetime.now().strftime("%Y%m%d-%H%M%S")
    removed_count = 0
    try:
        for path in files:
            backup = backup_path(path, timestamp)
            shutil.copy2(path, backup)
            print(f"Backup created: {backup}")
            path.unlink()
            print(f"Removed: {path}")
            removed_count += 1
    except OSError as exc:
        print(f"Error while resetting metadata: {exc}", file=sys.stderr)
        print("The backups created so far were retained.", file=sys.stderr)
        return 1

    print(f"Reset complete. Removed {removed_count} metadata/coordination file(s).")
    print("DataNode block files were not manually erased.")
    print("Start the MetaNode first, then ensure each DataNode reconnects and reports its disk inventory.")
    print("Unreferenced block files should be deleted by the normal reconciliation workflow, freeing their filesystem space.")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
