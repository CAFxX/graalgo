#!/usr/bin/env python3
"""Overlay-assert gate (review 2026-10-31 P1-5).

Fails if any main-style entry point under the backend that can load the
same-FQN shadow overlay lacks the overlay fail-fast.

Background: eight classes exist twice — the truffle module's frozen
Phase-1 stubs and the runtime module's Phase-2 replacements — and the
correct class wins only when runtime/out/classes precedes
truffle/out/classes on the classpath. R-01 added the fail-fast to the
three launchers, but a NEW launcher, daemon, or test harness that builds
its own classpath without the assert reopens silent wrong-class loading
(worst case: the Phase-1 static GoroutineState holder silently shared
across goroutines). P1-5 moved the assert to the top of every launcher
run() and added it to every test runner and to the fast-cycle daemon,
and centralised the implementation in
internal/backend/runtime/src/main/java/graalgo/launcher/OverlayGate.java.
This gate is what keeps the next entry point from dropping it.

Rule: every file under internal/backend/*/src/{main,test}/java or
internal/backend/fast-cycle/ that declares
`public static void main(` must also contain an
`assertSchedulerOverlay(` call or definition. The fast-cycle daemon
qualifies via its own reflective copy (it compiles standalone, so it
cannot call OverlayGate directly — see FastCycleDaemon.java).

Exclusions (deliberate, documented):
- internal/backend/truffle/src/smoke/: Truffle-only smoke fixtures that
  never load graalgo.runtime.* classes.
- docs/arch/spikes/: throwaway spike prototypes, not shipped code.
- internal/frontend/javatest/NodeFactoryTest.java: a manual
  golden-replay tool with no owned classpath construction; no Makefile
  or script launches it. The P1-5 elimination lane removes the hazard
  itself by deleting the stubs; until then this tool stays ungated and
  must not gain a backend-classpath launcher without a gate.

The gate fails loudly (exit 1) if it finds zero mains at all, so a
restructure that moves the sources cannot silently pass vacuously.

Usage: python3 .github/scripts/check_overlay_assert.py [--root DIR]
  (from the repo root; --root exists only so the gate itself can be
  negative-tested against a synthetic tree)
Exit 0 = every in-scope main asserts; exit 1 = gate failed (list printed).
"""

import argparse
import re
import sys
from pathlib import Path

MAIN_RE = re.compile(r"public\s+static\s+void\s+main\s*\(")
ASSERT_RE = re.compile(r"assertSchedulerOverlay\s*\(")

# (module, source root) pairs scanned for mains.
SCAN = (
    ("runtime", "src/main/java"),
    ("runtime", "src/test/java"),
    ("truffle", "src/main/java"),
    ("truffle", "src/test/java"),
    ("interop", "src/main/java"),
    ("interop", "src/test/java"),
)


def scan(root: Path):
    """Return (mains_checked, missing) for the backend tree under root."""
    backend = root / "internal" / "backend"
    if not backend.is_dir():
        print(f"OVERLAY-ASSERT GATE FAILED: missing backend tree: {backend}")
        sys.exit(1)
    mains = []
    for module, srcroot in SCAN:
        d = backend / module / srcroot
        if not d.is_dir():
            print(f"OVERLAY-ASSERT GATE FAILED: missing source tree: {d}")
            sys.exit(1)
        mains.extend(sorted(d.rglob("*.java")))
    fc = backend / "fast-cycle"
    if not fc.is_dir():
        print(f"OVERLAY-ASSERT GATE FAILED: missing fast-cycle dir: {fc}")
        sys.exit(1)
    mains.extend(sorted(fc.glob("*.java")))

    checked = []
    missing = []
    for path in mains:
        text = path.read_text()
        if MAIN_RE.search(text):
            checked.append(path)
            if not ASSERT_RE.search(text):
                missing.append(path)
    return checked, missing


def main():
    parser = argparse.ArgumentParser(description="Overlay-assert gate (P1-5).")
    parser.add_argument("--root", type=Path, default=None,
                        help="repo root (default: derived from this script's path)")
    args = parser.parse_args()
    root = args.root or Path(__file__).resolve().parents[2]
    checked, missing = scan(root)
    if not checked:
        print("OVERLAY-ASSERT GATE FAILED: no main() found in scope "
              "(refusing to pass vacuously — did the backend tree move?)")
        return 1
    if missing:
        print("OVERLAY-ASSERT GATE FAILED: main-style entry points without "
              "an assertSchedulerOverlay gate (a new launcher/daemon/test "
              "harness must fail fast on a wrong shadow-overlay classpath; "
              "see graalgo.launcher.OverlayGate):")
        for path in missing:
            print(f"  {path.relative_to(root)}")
        return 1
    print(f"overlay-assert gate clean: {len(checked)} in-scope main(s) "
          f"all gate the shadow overlay")
    return 0


if __name__ == "__main__":
    sys.exit(main())
