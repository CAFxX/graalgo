#!/usr/bin/env python3
"""Regression test for check_no_bridges.py (the no-bridges gate).

Every adversarial bypass from the 2026-10-16 independent review (R2-P1-4,
R2-P1-6/F6, R2-P2-21) must be CAUGHT by the gate; every legitimate /
grandfathered / adjudicated case must still PASS (no false-positive
regressions).

Run: python3 .github/scripts/check_no_bridges_test.py   (also in CI,
right after the no-bridges check itself).
Exit 0: all cases behave as expected. Exit 1: any mismatch, with details.
"""

import importlib.util
import sys
from pathlib import Path

_HERE = Path(__file__).resolve().parent
_spec = importlib.util.spec_from_file_location(
    "check_no_bridges", _HERE / "check_no_bridges.py")
_cnb = importlib.util.module_from_spec(_spec)
_spec.loader.exec_module(_cnb)


def mkdiff(fname, added, newfile=True):
    hdr = ("--- /dev/null\n+++ b/%s\n" % fname if newfile
           else "--- a/%s\n+++ b/%s\n" % (fname, fname))
    return hdr + "".join("+%s\n" % line for line in added)


# (name, diff, expect_caught)
CASES = [
    # --- F2: package-name conditionals on full stdlib paths (were MISSED) ---
    ("F2: pkg == \"net/http\"",
     mkdiff("internal/frontend/loaderx.go", ['if pkg == "net/http" {']), True),
    ("F2: strings.HasPrefix(pkg, \"net\")",
     mkdiff("internal/frontend/loaderx.go", ['if strings.HasPrefix(pkg, "net") {']), True),
    ("F2: strings.Contains on stdlib segment",
     mkdiff("internal/frontend/loaderx.go", ['if strings.Contains(pkgPath, "sync/") {']), True),
    ("F2: map lookup special[pkgPath]",
     mkdiff("internal/frontend/loaderx.go", ["x := special[pkgPath]"]), True),
    ("F2: switch path.Base(pkg)",
     mkdiff("internal/frontend/loaderx.go", ["switch path.Base(pkg) {"]), True),
    ("F2: leaf name via path.Base (\"http\")",
     mkdiff("internal/frontend/loaderx.go", ['if base == "http" {']), True),
    ("F2: Java pkg.equals(\"net/http\")",
     mkdiff("internal/backend/truffle/src/main/java/graalgo/X.java",
            ['if (pkg.equals("net/http")) {']), True),
    ("F2: Java name.startsWith(\"sync\")",
     mkdiff("internal/backend/runtime/src/main/java/graalgo/Y.java",
            ['if (name.startsWith("sync")) {']), True),
    # --- F7/R2-P2-21: narrowed test exemption (was "/test" substring) ---
    ("F7: conditional in testdata/ is caught",
     mkdiff("internal/frontend/testdata/x.go", ['if pkg == "fmt" {']), True),
    # --- F6: fail-fast stubs beyond keyword panics (were MISSED) ---
    ("F6: panic(errors.New(...)) non-keyword message",
     mkdiff("internal/frontend/x.go", ['panic(errors.New("operation failed"))']), True),
    ("F6: panic(fmt.Errorf(...)) non-keyword message",
     mkdiff("internal/frontend/x.go", ['panic(fmt.Errorf("bad state %d", n))']), True),
    ("F6: log.Fatal with stub keyword",
     mkdiff("internal/frontend/x.go", ['log.Fatal("not implemented: x")']), True),
    ("F6: log.Fatalf non-keyword in library code",
     mkdiff("internal/frontend/x.go", ['log.Fatalf("cannot happen: %v", err)']), True),
    ("F6: os.Exit in library code",
     mkdiff("internal/frontend/x.go", ["os.Exit(1)"]), True),
    # --- F3: wire-node gate holes (were MISSED) ---
    ("F3: e.node(\"jsonmarshal\") beyond the old 5 prefixes",
     mkdiff("internal/frontend/export.go", ['e.node("jsonmarshal")']), True),
    ("F3: dynamically composed e.node(\"sync\" + name)",
     mkdiff("internal/frontend/export.go", ['e.node("sync" + name)']), True),
    ("F3: wire node from a non-export.go frontend file",
     mkdiff("internal/frontend/p3frontend/loader.go", ['e.node("cryptosha")']), True),
    # --- F4: synthetic-registration holes (were MISSED) ---
    ("F4: map-literal entry with synthetic value",
     mkdiff("internal/frontend/export.go", ['"NewReader": syntheticNewReader,']), True),
    ("F4: defineFunc registration helper",
     mkdiff("internal/frontend/export.go", ['defineFunc("Foo", ...)']), True),
    ("F4: registerBuiltin registration helper",
     mkdiff("internal/frontend/export.go", ['registerBuiltin("Bar", ...)']), True),
    # --- pre-existing controls: these were always caught, must stay caught ---
    ("control: pkg == \"fmt\" still caught",
     mkdiff("internal/frontend/loaderx.go", ['if pkg == "fmt" {']), True),
    ("control: e.node(\"syncmutex\") still caught",
     mkdiff("internal/frontend/export.go", ['e.node("syncmutex")']), True),
    ("control: bare panic() still caught",
     mkdiff("internal/frontend/x.go", ["panic()"]), True),

    # --- legitimate / grandfathered / adjudicated cases: must PASS ---
    ("pass: package conditional in _test.go",
     mkdiff("internal/frontend/loader_test.go", ['if pkg == "fmt" {']), False),
    ("pass: testsynth.go allowlist (test harness)",
     mkdiff("cmd/graalgo/testsynth.go", ['if path == "testing" {']), False),
    ("pass: adjudicated wire node runtimenumgoroutine",
     mkdiff("internal/frontend/export.go", ['e.node("runtimenumgoroutine")']), False),
    ("pass: adjudicated registration addFunc(\"NumGoroutine\")",
     mkdiff("internal/frontend/export.go", ['addFunc("NumGoroutine", ...)']), False),
    ("pass: addFunc in grandfathered synth_ file (R2-P1-5 ledger owns this)",
     mkdiff("internal/frontend/synth_machinery.go", ['addFunc("Foo", ...)'],
            newfile=False), False),
    ("pass: legitimate invariant panic (no stub shape)",
     mkdiff("internal/frontend/x.go", ['panic("invariant violated: bad state")']), False),
    ("pass: generic (non-stdlib) wire node kind",
     mkdiff("internal/frontend/export.go", ['e.node("ifstmt")']), False),
    ("pass: log.Fatal in cmd/ CLI",
     mkdiff("cmd/graalgo/main.go", ['log.Fatal("bad flag")']), False),
    ("pass: os.Exit in tools/ CLI",
     mkdiff("tools/fuzz/main.go", ["os.Exit(2)"]), False),
    ("pass: non-stdlib string comparison",
     mkdiff("internal/frontend/x.go", ['if name == "network" {']), False),
]


def main():
    failures = []
    for name, diff, expect_caught in CASES:
        try:
            errs = _cnb.check_no_bridges(diff)
        except Exception as exc:  # noqa: BLE001 — a crash is a failure
            failures.append((name, "checker crashed: %r" % exc))
            print("ERROR  ", name, "-> checker crashed: %r" % exc)
            continue
        caught = bool(errs)
        ok = caught == expect_caught
        print(("CAUGHT " if caught else "PASSED "),
              "OK " if ok else "FAIL", name,
              ("-> " + errs[0][:100] if caught else ""))
        if not ok:
            failures.append((name,
                             "expected %s, got %s%s"
                             % ("CAUGHT" if expect_caught else "PASS",
                                "CAUGHT" if caught else "PASS",
                                (": " + errs[0][:100]) if errs else "")))
    print()
    if failures:
        print("REGRESSION TEST FAILED: %d/%d cases misbehaved:"
              % (len(failures), len(CASES)))
        for name, why in failures:
            print("  x %s: %s" % (name, why))
        return 1
    print("All %d gate regression cases behave as expected." % len(CASES))
    return 0


if __name__ == "__main__":
    sys.exit(main())
