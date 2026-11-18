#!/usr/bin/env python3
"""Off-heap / NIO / FFM confinement gate (make check, via the truffle
module's `check`).

Go values live on the JVM heap behind GC barriers (ADR-0002 D3/F3), and
goroutine I/O must go through the slot-releasing scheduler path
(GG-SCHED-0006). Direct use of `java.nio.channels`, the FFM API
(`java.lang.foreign`, `MemorySegment`, `Arena`) or raw off-heap memory
(`allocateDirect`, `allocateMemory`) bypasses both.

The recovery's syscall boundary (abi-spike P2, ADR-0009: Panama syscall
table, the GG-0063 epoll poller and the GG-0064 spawn helper) needs exactly
this surface, so it is confined to ONE designated package:
`graalgo.syscall` (`internal/backend/*/src/main/java/graalgo/syscall/`,
planned — toolexec W8/M4). Everywhere else under internal/backend it is
forbidden.

GRANDFATHERED lists the frozen v1 Java std reimplementations that already
use NIO channels (deleted at M3 with the bridge inventory, B-8). The list
may only shrink: an entry that no longer matches fails as stale.

Usage: check_offheap.py [repo-root]
"""

import re
import sys
from pathlib import Path

PATTERN = re.compile(r"java\.nio\.channels|java\.lang\.foreign|\bMemorySegment\b|\bArena\b|"
                     r"\ballocateDirect\b|\ballocateMemory\b")
ALLOWED_PKG = re.compile(r"^internal/backend/[^/]+/src/main/java/graalgo/syscall/")
GRANDFATHERED = {
    # v1 os/net reimplementations (bridge-inventory B-8), deleted at M3.
    "internal/backend/interop/src/main/java/graalgo/p3os/GoOs.java",
    "internal/backend/interop/src/main/java/graalgo/p3net/GoNet.java",
    # the v1 NIO-Selector netpoller, superseded by GG-0063's epoll poller
    "internal/backend/interop/src/main/java/graalgo/p3net/GoNetpoll.java",
    "internal/backend/interop/src/test/java/graalgo/p3os/GoOsErrnoTest.java",
}


def scan(root):
    root = Path(root)
    errors, used = [], set()
    for p in sorted((root / "internal/backend").rglob("*.java")):
        rel = p.relative_to(root).as_posix()
        if "/src/" not in rel or ALLOWED_PKG.match(rel):
            continue
        hits = [(n, l.strip()) for n, l in enumerate(p.read_text(errors="replace").splitlines(), 1)
                if PATTERN.search(l)]
        if not hits:
            continue
        if rel in GRANDFATHERED:
            used.add(rel)
            continue
        for n, l in hits:
            errors.append(f"{rel}:{n}: {l}")
    for g in sorted(GRANDFATHERED - used):
        errors.append(f"stale GRANDFATHERED entry (no longer matches; delete it): {g}")
    return errors


def main(argv):
    root = Path(argv[1]) if len(argv) > 1 else Path(__file__).resolve().parents[2]
    errors = scan(root)
    if errors:
        print("OFF-HEAP/NIO GATE FAILED (only graalgo.syscall may use java.nio.channels, "
              "FFM or off-heap memory):")
        for e in errors:
            print("  " + e)
        return 1
    print("off-heap/NIO confinement gate clean")
    return 0


if __name__ == "__main__":
    sys.exit(main(sys.argv))
