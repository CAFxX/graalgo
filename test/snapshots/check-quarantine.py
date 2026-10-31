#!/usr/bin/env python3
"""Snapshot quarantine gate (see QUARANTINE.md in this directory).

Checks, read-only by default:
  1. Every exclusion in the live list (test/differential/backends-p3.json)
     and in the recorded snapshot(s) carries a non-empty `owner` and a
     `since` date that is a real calendar date in YYYY-MM-DD form
     (format alone is not enough: 2026-13-99 is rejected).
  2. The exclusion count in the config AND in every snapshot file equals
     the `approved_count` in QUARANTINE.md. Growing the list without
     updating the manifest (the review record) fails the gate.
  3. The config and snapshot exclusion sets agree on program+reason
     (the harness's own snapshot-check enforces this too, but this gate
     fails fast without a JVM build).
  4. Excluded programs plus recorded programs cover the differential
     corpus exactly (no program silently in, out, or double-counted),
     and there are no duplicate exclusions.
  5. The QUARANTINE.md exclusion table agrees with the config on
     program, owner, and since (the table is a checked record, not
     decoration; its reason column is a paraphrase and is not compared).

Which snapshot file is vetted: the SAME derivation as the Makefile
(SNAPSHOT_TOOLCHAIN from `go version`, see the snapshot targets):
  1. $SNAPSHOT_TOOLCHAIN, when set (the Makefile's command-line override
     equivalent for this gate);
  2. otherwise the version reported by $GO, defaulting to
     ~/tools/go/bin/go (the Makefile's GO default) and then `go` on PATH;
  3. only if no Go binary can be run at all, the gate falls back to
     go1.27.1.json with a loud stderr warning. A detected toolchain NEVER
     falls back: if the derived file is missing the gate FAILS and lists
     the snapshot files present, so a toolchain upgrade can never leave
     the gate silently vetting a stale file while `make snapshot-check`
     reads the new one.
In addition, EVERY other test/snapshots/go*.json file present is vetted
for owner/since, count-lock, and config agreement (but not corpus
coverage, which is toolchain-specific): a superseded snapshot left in
the tree cannot silently carry unowned exclusions either.

Usage (from the repo root):
    python3 test/snapshots/check-quarantine.py [--sync]

--sync (the ONLY writing mode): after a `make snapshot` re-record, which
drops owner/since from the snapshot file (the harness Exclusion struct
carries only program+reason), copy owner/since from the config into the
snapshot for matching program+reason pairs, preserving every other byte
of the snapshot file. --sync applies to the derived snapshot AND to any
other go*.json present. Re-run without --sync afterwards; it must pass.
"""
import json
import os
import re
import subprocess
import sys
from datetime import datetime
from pathlib import Path

ROOT = Path(__file__).resolve().parent.parent.parent
CONFIG = ROOT / "test/differential/backends-p3.json"
CORPUS = ROOT / "test/differential/corpus"
MANIFEST = ROOT / "test/snapshots/QUARANTINE.md"
SNAPSHOT_DIR = ROOT / "test/snapshots"
DATE_RE = re.compile(r"^\d{4}-\d{2}-\d{2}$")
TOOLCHAIN_RE = re.compile(r"^go\d+\.\d+(\.\d+)?$")
# Last-resort filename when no Go binary can be run at all (see docstring).
FALLBACK_TOOLCHAIN = "go1.27.1"


def valid_since(since):
    """True only for a real calendar date in strict YYYY-MM-DD form."""
    if not isinstance(since, str) or not DATE_RE.match(since):
        return False
    try:
        datetime.strptime(since, "%Y-%m-%d")
    except ValueError:
        return False
    return True


def resolve_toolchain():
    """Return (toolchain, warning-or-None), mirroring the Makefile.

    $SNAPSHOT_TOOLCHAIN wins; then `go version` from $GO (or the
    Makefile's GO default, ~/tools/go/bin/go, then PATH `go`).
    """
    override = os.environ.get("SNAPSHOT_TOOLCHAIN", "").strip()
    if override:
        if not TOOLCHAIN_RE.match(override):
            raise ValueError(
                f"SNAPSHOT_TOOLCHAIN={override!r} is not a Go version like 'go1.27.1'"
            )
        return override, None
    candidates = []
    if os.environ.get("GO"):
        candidates.append(os.environ["GO"])
    default_go = Path.home() / "tools/go/bin/go"
    if default_go.exists():
        candidates.append(str(default_go))
    candidates.append("go")
    for go_bin in candidates:
        try:
            out = subprocess.run(
                [go_bin, "version"], capture_output=True, text=True, timeout=30
            ).stdout.strip()
        except (OSError, subprocess.SubprocessError):
            continue
        parts = out.split()
        if len(parts) >= 3 and TOOLCHAIN_RE.match(parts[2]):
            return parts[2], None
    return FALLBACK_TOOLCHAIN, (
        "check-quarantine: WARNING: could not run any Go binary to derive "
        f"the snapshot toolchain; falling back to {FALLBACK_TOOLCHAIN}.json. "
        "This fallback never overrides a detected toolchain."
    )


def snapshot_files(toolchain):
    """Return (primary_path, [other go*.json paths present])."""
    primary = SNAPSHOT_DIR / f"{toolchain}.json"
    others = sorted(p for p in SNAPSHOT_DIR.glob("go*.json") if p.name != primary.name)
    return primary, others


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
        if not valid_since(since):
            if isinstance(since, str) and DATE_RE.match(since):
                errors.append(
                    f"{where}: exclusion {prog} has impossible since date ({since!r})"
                )
            else:
                errors.append(
                    f"{where}: exclusion {prog} has no valid since date ({since!r})"
                )
    return seen


def check_manifest_table(manifest_text, config_entries, errors):
    """The QUARANTINE.md table must agree with the config on program/owner/since."""
    rows = {}
    duplicates = []
    for line in manifest_text.splitlines():
        m = re.match(r"\|\s*`([^`]+)`\s*\|\s*([^|]+?)\s*\|\s*([^|]+?)\s*\|", line.strip())
        if m:
            if m.group(1) in rows:
                duplicates.append(m.group(1))
            rows[m.group(1)] = (m.group(2).strip(), m.group(3).strip())
    for prog in duplicates:
        errors.append(f"QUARANTINE.md table: duplicate row {prog}")
    cfg = {e["program"]: (e.get("owner", ""), e.get("since", "")) for e in config_entries}
    if rows != cfg:
        for prog in sorted(set(rows) - set(cfg)):
            errors.append(f"QUARANTINE.md table: row {prog} not in backends-p3.json")
        for prog in sorted(set(cfg) - set(rows)):
            errors.append(f"QUARANTINE.md table: missing row {prog} from backends-p3.json")
        for prog in sorted(set(rows) & set(cfg)):
            if rows[prog] != cfg[prog]:
                errors.append(
                    f"QUARANTINE.md table: {prog} is {rows[prog]}, "
                    f"backends-p3.json has {cfg[prog]}"
                )


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
    try:
        toolchain, warning = resolve_toolchain()
    except ValueError as exc:
        print(f"check-quarantine: FAIL: {exc}", file=sys.stderr)
        return 1
    if warning:
        print(warning, file=sys.stderr)
    snapshot_path, other_paths = snapshot_files(toolchain)
    if not snapshot_path.exists():
        present = sorted(p.name for p in SNAPSHOT_DIR.glob("go*.json"))
        print(
            f"check-quarantine: FAIL: derived snapshot {snapshot_path.name} "
            f"(toolchain {toolchain}) not found; snapshots present: {present}. "
            "After a toolchain upgrade, re-record with `make snapshot` and "
            "re-attach owner/since with --sync; do not fall back to a stale file.",
            file=sys.stderr,
        )
        return 1

    config = load_json(CONFIG)
    config_entries = config.get("exclusions", [])

    if sync:
        sync_snapshot(config_entries, snapshot_path)
        for other in other_paths:
            if '"excluded": [' in other.read_text():
                sync_snapshot(config_entries, other)
            else:
                print(f"check-quarantine --sync: {other.name} has no excluded list; skipping")

    snapshot = load_json(snapshot_path)
    snap_entries = snapshot.get("excluded", [])

    errors = []
    check_entries(config_entries, "backends-p3.json", errors)
    check_entries(snap_entries, snapshot_path.name, errors)

    manifest_text = MANIFEST.read_text()
    m = re.search(r"approved_count: (\d+)", manifest_text)
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
                f"{snapshot_path.name} has {len(snap_entries)} exclusions, "
                f"QUARANTINE.md approves {approved}"
            )

    check_manifest_table(manifest_text, config_entries, errors)

    cfg_keys = {(e["program"], e["reason"]) for e in config_entries}
    snap_keys = {(e["program"], e["reason"]) for e in snap_entries}
    if cfg_keys != snap_keys:
        errors.append(
            f"config/snapshot exclusion mismatch: config-only={sorted(cfg_keys - snap_keys)} "
            f"snapshot-only={sorted(snap_keys - cfg_keys)}"
        )

    # Every other snapshot file present gets the ownership/count/agreement
    # checks too, so a stale file can never pass silently.
    for other in other_paths:
        other_entries = load_json(other).get("excluded", [])
        check_entries(other_entries, other.name, errors)
        if approved is not None and len(other_entries) != approved:
            errors.append(
                f"{other.name} has {len(other_entries)} exclusions, "
                f"QUARANTINE.md approves {approved}"
            )
        other_keys = {(e["program"], e["reason"]) for e in other_entries}
        if other_keys != cfg_keys:
            errors.append(
                f"config/{other.name} exclusion mismatch: "
                f"config-only={sorted(cfg_keys - other_keys)} "
                f"snapshot-only={sorted(other_keys - cfg_keys)}"
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
    extra = f" (+{len(other_paths)} other snapshot(s) vetted)" if other_paths else ""
    print(
        f"check-quarantine: OK: {len(config_entries)} exclusions owned+dated, "
        f"count matches approved_count={approved}, "
        f"coverage {len(recorded)} recorded + {len(excluded)} excluded = {len(corpus)} corpus "
        f"(snapshot {snapshot_path.name}, toolchain {toolchain}){extra}"
    )
    return 0


if __name__ == "__main__":
    raise SystemExit(main(sys.argv[1:]))
