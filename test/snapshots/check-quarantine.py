#!/usr/bin/env python3
"""Snapshot quarantine gate (see QUARANTINE.md in this directory).

Checks, read-only by default:
  1. Every exclusion in the live list (test/differential/backends-p3.json)
     and in the recorded snapshot (test/snapshots/go1.27.1.json) carries a
     non-empty `owner` and a `since` date (YYYY-MM-DD).
  2. The exclusion count in BOTH files equals the `approved_count` in
     QUARANTINE.md. Growing the list without updating the manifest (the
     review record) fails the gate.
  3. The config and snapshot exclusion sets agree on program+reason
     (the harness's own snapshot-check enforces this too, but this gate
     fails fast without a JVM build).
  4. Excluded programs plus recorded programs cover the differential
     corpus exactly (no program silently in, out, or double-counted),
     and there are no duplicate exclusions.

Usage (from the repo root):
    python3 test/snapshots/check-quarantine.py [--sync]

--sync (the ONLY writing mode): after a `make snapshot` re-record, which
drops owner/since from the snapshot file (the harness Exclusion struct
carries only program+reason), copy owner/since from the config into the
snapshot for matching program+reason pairs, preserving every other byte
of the snapshot file. Re-run without --sync afterwards; it must pass.
"""
import json
import re
import sys
from pathlib import Path

ROOT = Path(__file__).resolve().parent.parent.parent
CONFIG = ROOT / "test/differential/backends-p3.json"
CORPUS = ROOT / "test/differential/corpus"
MANIFEST = ROOT / "test/snapshots/QUARANTINE.md"
DATE_RE = re.compile(r"^\d{4}-\d{2}-\d{2}$")


def load_json(path):
    with open(path) as f:
        return json.load(f)


def check_entries(entries, where, errors):
    seen = set()
    for e in entries:
        prog = e.get("program", "")
        if prog in seen:
            errors.append(f"{where}: duplicate exclusion {prog}")
        seen.add(prog)
        if not e.get("owner"):
            errors.append(f"{where}: exclusion {prog} has no owner")
        since = e.get("since", "")
        if not DATE_RE.match(since):
            errors.append(f"{where}: exclusion {prog} has no valid since date ({since!r})")
    return seen


def sync_snapshot(config_entries, snapshot_path):
    """Copy owner/since from config into the snapshot, byte-preserving."""
    by_key = {(e["program"], e["reason"]): e for e in config_entries}
    lines = snapshot_path.read_text().splitlines(keepends=True)
    start = next(i for i, ln in enumerate(lines) if '"excluded": [' in ln)
    depth = 0
    end = None
    for i in range(start, len(lines)):
        depth += lines[i].count("[") - lines[i].count("]")
        if i > start and depth == 0:
            end = i
            break
    prog_re = re.compile(r'^(\s*)"program": "([^"]+)",?\s*$')
    reason_re = re.compile(r'^(\s*)"reason": "(.*)",?\s*$')
    out, current = [], None
    for i, ln in enumerate(lines):
        if start <= i <= end:
            pm = prog_re.match(ln)
            if pm:
                # Program precedes reason in the file, so the owner line
                # (which sorts before program) is inserted retroactively
                # once the reason line below identifies the entry.
                current = pm.group(2)
                out.append(ln)
                continue
            rm = reason_re.match(ln)
            if rm and current is not None:
                key = (current, rm.group(2))
                src = by_key.get(key)
                if src is None:
                    print(f"check-quarantine --sync: no config entry for {key}; leaving as-is")
                else:
                    indent = rm.group(1)
                    if not ln.rstrip().endswith(","):
                        ln = ln.rstrip("\n") + ",\n"
                    out.append(ln)
                    out.append(f'{indent}"since": "{src["since"]}"\n')
                    # owner goes before program: patch the already-emitted
                    # program line (two lines back: program, reason->now).
                    for j in range(len(out) - 1, -1, -1):
                        if prog_re.match(out[j]):
                            out.insert(j, f'{indent}"owner": "{src["owner"]}",\n')
                            break
                    current = None
                    continue
            # Drop any stale owner/since lines so --sync is idempotent.
            if re.match(r'^\s*"(owner|since)": ', ln):
                continue
            out.append(ln)
        else:
            out.append(ln)
    snapshot_path.write_text("".join(out))
    print(f"check-quarantine --sync: wrote {snapshot_path}")


def main(argv):
    sync = "--sync" in argv
    config = load_json(CONFIG)
    config_entries = config.get("exclusions", [])
    snapshot_path = ROOT / "test/snapshots" / ("go1.27.1.json")
    snapshot = load_json(snapshot_path)
    snap_entries = snapshot.get("excluded", [])

    if sync:
        sync_snapshot(config_entries, snapshot_path)
        snapshot = load_json(snapshot_path)
        snap_entries = snapshot.get("excluded", [])

    errors = []
    check_entries(config_entries, "backends-p3.json", errors)
    check_entries(snap_entries, "go1.27.1.json", errors)

    m = re.search(r"approved_count: (\d+)", MANIFEST.read_text())
    if not m:
        errors.append("QUARANTINE.md: no approved_count line found")
        approved = None
    else:
        approved = int(m.group(1))
        if len(config_entries) != approved:
            errors.append(
                f"backends-p3.json has {len(config_entries)} exclusions, "
                f"QUARANTINE.md approves {approved}: growing the list "
                "requires updating the manifest in the same change"
            )
        if len(snap_entries) != approved:
            errors.append(
                f"go1.27.1.json has {len(snap_entries)} exclusions, "
                f"QUARANTINE.md approves {approved}"
            )

    cfg_keys = {(e["program"], e["reason"]) for e in config_entries}
    snap_keys = {(e["program"], e["reason"]) for e in snap_entries}
    if cfg_keys != snap_keys:
        errors.append(
            f"config/snapshot exclusion mismatch: config-only={sorted(cfg_keys - snap_keys)} "
            f"snapshot-only={sorted(snap_keys - cfg_keys)}"
        )

    corpus = {d.name for d in CORPUS.iterdir() if d.is_dir()}
    recorded = {p["name"] for p in snapshot.get("programs", [])}
    excluded = {e["program"] for e in snap_entries}
    if excluded & recorded:
        errors.append(f"programs both excluded and recorded: {sorted(excluded & recorded)}")
    if excluded | recorded != corpus:
        errors.append(
            f"excluded+recorded != corpus: missing={sorted(corpus - excluded - recorded)} "
            f"unknown={sorted((excluded | recorded) - corpus)}"
        )

    if errors:
        for e in errors:
            print(f"check-quarantine: FAIL: {e}", file=sys.stderr)
        return 1
    print(
        f"check-quarantine: OK: {len(config_entries)} exclusions owned+dated, "
        f"count matches approved_count={approved}, "
        f"coverage {len(recorded)} recorded + {len(excluded)} excluded = {len(corpus)} corpus"
    )
    return 0


if __name__ == "__main__":
    raise SystemExit(main(sys.argv[1:]))
