#!/usr/bin/env python3
"""Generate the Plan 9 assembly inventory for the P3-M6 asm strategy.

Enumerates EVERY .s/.S/.sx file under $GOROOT/src (default:
~/tools/go-1.27.1/src, the pinned toolchain) -- go/build treats the
three suffixes identically as assembly -- and assigns each one a SPEC
section 2.8 (GG-0009) category:

    evaporate              runtime-core asm: no Go stacks on Truffle, the
                           code has no meaning; Go-visible services are
                           provided by graalgo intrinsics / the linkname
                           registry instead.
    intrinsify             atomics/bytealg: replaced by graalgo intrinsics
                           (cross-checked against the P3-M4 intrinsic audit).
    emulate-then-intrinsify crypto: the emulate leg runs the pure-Go generic
                           implementation; the intrinsify leg is a later
                           phase (re-test trigger recorded per file).
    jvm-math               math package: covered by JVM IEEE754.
    unsupported            fail-fast: the importer rejects the file with a
                           named error (file, package, category, reason).

Output (next to this script):
    asm-inventory-go1.27.1.json   machine-readable source of truth, embedded
                                 by phases/phase3/src/p3frontend/asm.go
                                 via //go:embed; the loader resolves every
                                 buildable assembly file against it.
    asm-inventory-go1.27.1.md     human-readable summary.

The generator FAILS if any assembly file matches no rule, so toolchain drift
(new files, moved files) surfaces here, never as a silent gap. Re-run on
every Go toolchain upgrade and re-verify the category counts.

Usage: python3 gen-asm-inventory.py [--goroot DIR] [--check]
    --check: verify the committed JSON is complete and up to date without
             rewriting it (for CI / review gates).
"""

import argparse
import json
import os
import sys

SCRIPT_DIR = os.path.dirname(os.path.abspath(__file__))
VERSION = "go1.27.1"
JSON_NAME = f"asm-inventory-{VERSION}.json"
MD_NAME = f"asm-inventory-{VERSION}.md"
# The p3frontend Go package cannot //go:embed ../../ (embed patterns forbid
# ".."), so the generator also emits a Go source file carrying the JSON as
# a string literal. The JSON next to this script stays the single source of
# truth; `make -C internal/backend/interop check` runs this script with --check to assert
# the generated file is in sync.
# (2026-10-14: GO_DIR updated for the Phase-4 move of p3frontend Go sources
# from internal/backend/interop/ to internal/frontend/.)
GO_NAME = "zz_asm_inventory_gen.go"
GO_DIR = os.path.join(SCRIPT_DIR, "..", "..", "internal", "frontend", "p3frontend")

CATEGORIES = (
    "evaporate",
    "intrinsify",
    "emulate-then-intrinsify",
    "jvm-math",
    "unsupported",
)

# ---------------------------------------------------------------------------
# Category rules: (directory-prefix predicate, category, replacement, reason)
# Evaluated in order; the first match wins. A file matching nothing is a
# generator error (fail fast, never silently uncategorized).
# ---------------------------------------------------------------------------

EVAPORATE_REPLACEMENT = (
    "evaporates into the graalgo runtime (no Go stacks on Truffle); "
    "Go-visible services are provided by graalgo.runtime.Intrinsics / "
    "the graalgo.runtime.Linknames registry (ADR-0004 D1), not by this code"
)

ATOMIC_REPLACEMENT = (
    "graalgo.runtime.Atomics (P3-M4 intrinsic audit: REAL, 16 methods) "
    "via the phase-3 AtomicOpNode (SyncOpNode atomic.* namespace, ADR-0004 D2)"
)

BYTEALG_REPLACEMENT = (
    "graalgo.runtime.Intrinsics.memequal (P3-M4 audit: REAL) + planned "
    "ByteAlg vector nodes (T4: memchr/memcmp via Unsafe/vector APIs); "
    "until then the pure-Go fallback (*_generic.go) is selected by "
    "excluding the arch asm"
)

CRYPTO_REPLACEMENT = (
    "emulate leg: pure-Go generic implementation with the arch asm "
    "excluded; intrinsify leg: planned Phase-5+ per-algorithm intrinsics "
    "(re-test trigger: when the intrinsify leg lands, re-run the "
    "emulate-leg differential corpora and re-verify this entry)"
)

MATH_REPLACEMENT = (
    "JVM IEEE754: java.lang.StrictMath (exact per JLS 15); math/big "
    "arbitrary precision: java.math.BigInteger semantics (Go fallback "
    "initially, per T4)"
)


def _is_testdata(rel):
    return "/testdata/" in rel or rel.startswith("testdata/")


def classify(rel):
    """Return (category, replacement, reason) for a GOROOT/src-relative path."""
    if _is_testdata(rel):
        return (
            "unsupported",
            "",
            "test fixture data (testdata/): never assembled as package input; "
            "no graalgo handling needed or possible",
        )
    # --- intrinsify: atomics (checked before the general --------------------
    # --- internal/runtime/ rule below) --------------------------------------
    if rel.startswith("internal/runtime/atomic"):
        return (
            "intrinsify",
            ATOMIC_REPLACEMENT,
            "runtime-internal atomic primitives (Xchg/Cas/...); same "
            "intrinsic surface as sync/atomic (sync/atomic/asm.s jumps "
            "here)",
        )
    # --- cgo gcc-assembly: must precede the runtime/ evaporate rule ------
    # --- (.S files compiled by the C toolchain, not Plan 9 asm) -----------
    if rel.startswith("runtime/cgo/") and rel.endswith((".S", ".sx")):
        return (
            "unsupported",
            "",
            "cgo gcc-assembly (not Plan 9 asm): compiled by the C toolchain "
            "via cgo; graalgo has no C-compilation path. runtime/cgo is "
            "excluded as CGO in the v1 exclusion list (SPEC §3.8 GG-0023) -- "
            "strategy decided with the Panama FFI bring-up "
            "(re-test trigger: P3-M8)",
        )
    # --- runtime-core: evaporates (no Go stacks on Truffle) -----------------
    if rel.startswith("runtime/"):
        return (
            "evaporate",
            EVAPORATE_REPLACEMENT,
            "runtime-core asm: manipulates Go stacks/registers and gc ABI "
            "internals directly; meaningless on Truffle",
        )
    if rel.startswith("internal/runtime/"):
        return (
            "evaporate",
            EVAPORATE_REPLACEMENT,
            "runtime-support asm (syscall trampolines, map hashing, GC "
            "scan helpers): meaningless on Truffle",
        )
    if rel.startswith("internal/abi") or rel.startswith("internal/reflectlite"):
        return (
            "evaporate",
            EVAPORATE_REPLACEMENT,
            "runtime-core ABI/reflect shims: meaningless on Truffle",
        )
    if rel.startswith("internal/chacha8rand"):
        return (
            "evaporate",
            "graalgo.runtime.Intrinsics.fastrand/fastrandn "
            "(ADR-0004 D1 linkname table: runtime.fastrand)",
            "runtime RNG source asm; the Go-visible symbol is a graalgo "
            "intrinsic, the SIMD asm evaporates",
        )
    if rel.startswith("internal/cpu"):
        return (
            "evaporate",
            "graalgo host CPU-feature model (planned)",
            "CPUID/xgetbv feature detection; on Truffle the host JVM owns "
            "CPU features. Coupling note: crypto's emulate leg and bytealg "
            "read internal/cpu to pick code paths -- the Go-level cpu "
            "package needs a graalgo-provided implementation before the "
            "crypto wave (re-test trigger)",
        )
    if rel.startswith("internal/syscall/unix"):
        return (
            "evaporate",
            EVAPORATE_REPLACEMENT,
            "runtime-support syscall helpers (all non-linux files); the "
            "syscall mechanism itself is reimplemented, not trampolined",
        )
    if rel.startswith("os/signal"):
        return (
            "evaporate",
            EVAPORATE_REPLACEMENT,
            "signal trampoline (sigtramp); the JVM owns signal delivery on "
            "Truffle",
        )
    if rel == "syscall/js/asm_js.s" or rel.startswith("syscall/js/"):
        return (
            "evaporate",
            EVAPORATE_REPLACEMENT,
            "js/wasm-only syscall shim; not a linux/amd64 build input",
        )
    # --- intrinsify: atomics / bytealg --------------------------------------
    if rel.startswith("sync/atomic"):
        return (
            "intrinsify",
            ATOMIC_REPLACEMENT,
            "public atomic ops; replaced by the intrinsic surface, "
            "differential vs gc on identical source (ADR-0004 D2)",
        )
    if rel.startswith("internal/bytealg"):
        return (
            "intrinsify",
            BYTEALG_REPLACEMENT,
            "SIMD memequal/index/strcmp kernels (T4: intrinsify hot paths, "
            "Go fallback for the rest); linkname targets memequal/cmpstring "
            "exist as real intrinsics (P3-M4 audit)",
        )
    # --- emulate-then-intrinsify: crypto ------------------------------------
    if rel.startswith("crypto/"):
        return (
            "emulate-then-intrinsify",
            CRYPTO_REPLACEMENT,
            "crypto SIMD kernels (incl. fips140 and boringssl marker "
            "stubs); emulate leg first, intrinsify leg later",
        )
    # --- jvm-math ------------------------------------------------------------
    if rel.startswith("math/"):
        return (
            "jvm-math",
            MATH_REPLACEMENT,
            "arch float kernels / big-int kernels; JVM covers IEEE754 "
            "exactly, math/big is mostly-Go with asm speedups (T4)",
        )
    # --- unsupported: fail fast with a named reason --------------------------
    if rel.startswith("cmd/"):
        return (
            "unsupported",
            "",
            "Go toolchain source (compiler/assembler/linker/cgo/vet/dist "
            "and their testdata); not part of the stdlib fork -- graalgo "
            "never compiles the toolchain",
        )
    if rel.startswith("vendor/"):
        return (
            "unsupported",
            "",
            "vendored third-party module (x/sys, x/crypto, x/net); "
            "toolchain build input, not part of the stdlib fork",
        )
    if rel.startswith("syscall/"):
        return (
            "unsupported",
            "",
            "syscall trampoline asm; graalgo's syscall package is adapted "
            "to Panama FFI (SPEC section 3.7) in the wave-2 bring-up -- "
            "per-file strategy then (re-test trigger: syscall wave)",
        )
    if rel.startswith("hash/crc32"):
        return (
            "unsupported",
            "",
            "SIMD CRC32 kernels (PCLMULQDQ etc.); no graalgo intrinsic yet "
            "-- the wave-1 bring-up decides intrinsify-vs-Go-fallback "
            "(re-test trigger: hash/crc32 wave)",
        )
    if rel.startswith("reflect/"):
        return (
            "unsupported",
            "",
            "makeFuncStub/callReflect stubs assume the gc stack layout and "
            "register ABI; reflect is reimplemented on Truffle in the "
            "wave-2 bring-up -- per-file strategy then (re-test trigger: "
            "reflect wave)",
        )
    if rel.startswith("simd/"):
        return (
            "unsupported",
            "",
            "experimental SIMD API (go1.27); no graalgo strategy decided "
            "yet (re-test trigger: simd API decision)",
        )
    return None


def enumerate(goroot_src):
    # go/build treats .s, .S and .sx identically as assembly
    # (fileListForExt: case ".s", ".S", ".sx"); enumerate all three.
    found = []
    for dirpath, _dirnames, filenames in os.walk(goroot_src):
        for fn in filenames:
            if fn.endswith((".s", ".S", ".sx")):
                full = os.path.join(dirpath, fn)
                rel = os.path.relpath(full, goroot_src)
                with open(full, "rb") as f:
                    lines = sum(1 for _ in f)
                found.append((rel, lines))
    found.sort()
    return found


def build_inventory(goroot_src):
    entries = []
    for rel, lines in enumerate(goroot_src):
        cls = classify(rel)
        if cls is None:
            raise SystemExit(
                f"gen-asm-inventory: UNCATEGORIZED assembly file: {rel} -- "
                "add a rule (never silently uncategorized)"
            )
        category, replacement, reason = cls
        assert category in CATEGORIES, category
        pkg = os.path.dirname(rel).replace(os.sep, "/")
        entries.append(
            {
                "path": rel.replace(os.sep, "/"),
                "package": pkg,
                "category": category,
                "lines": lines,
                "replacement": replacement,
                "reason": reason,
            }
        )
    return entries


def render_md(entries, total_lines):
    from collections import Counter

    counts = Counter(e["category"] for e in entries)
    line_counts = Counter()
    for e in entries:
        line_counts[e["category"]] += e["lines"]
    order = list(CATEGORIES)
    lines = []
    a = lines.append
    a(f"# Plan 9 assembly inventory -- {VERSION}")
    a("")
    a(f"Generated by `gen-asm-inventory.py` from the pinned toolchain "
      f"(`~/tools/go-1.27.1/src`). **Do not edit by hand** -- re-run the "
      f"generator (it fails on any uncategorized file).")
    a("")
    a("## Headline")
    a("")
    a(f"- **{len(entries)} assembly files (.s/.S/.sx) / {total_lines:,} lines** enumerated "
      f"(SPEC section 2.8 said 485 files / ~127k lines on the older "
      f"toolchain the SPEC was written against -- drift recorded, not "
      f"carried: go1.27.1 grew the tree, notably crypto/internal/fips140).")
    a("- Every file has a category; there are no uncategorized files "
      "(the generator exits non-zero if one appears).")
    a("- `unsupported` is a fail-fast category, not a parking lot: the "
      "importer rejects such a file with an error naming the file, its "
      "package, its category, and the reason below.")
    a("")
    a("## Counts per category")
    a("")
    a("| category | files | lines | disposition |")
    a("|---|---|---|---|")
    disp = {
        "evaporate": "replaced: evaporates into the graalgo runtime",
        "intrinsify": "replaced: graalgo intrinsic (see replacement)",
        "emulate-then-intrinsify": "replaced: emulate leg now, intrinsify leg later",
        "jvm-math": "replaced: JVM IEEE754 / BigInteger",
        "unsupported": "REJECTED: fail-fast with file+package+category+reason",
    }
    for c in order:
        a(f"| {c} | {counts[c]} | {line_counts[c]:,} | {disp[c]} |")
    a("")
    a("## Per-category rationale (summary; full rationale: ADR-0007)")
    a("")
    a("1. **evaporate** (runtime-core): no Go stacks exist on Truffle, so "
      "stack/register/GC-ABI asm has no meaning. Go-visible services "
      "(fastrand, nanotime, memequal, ...) are provided by real graalgo "
      "intrinsics or the Linknames registry (ADR-0004 D1; P3-M4 audit: "
      "259 REAL, 0 true stubs). Includes internal/cpu (host CPUID -- the "
      "host JVM owns CPU features; coupling note for the crypto emulate "
      "leg) and os/signal (the JVM owns signals).")
    a("2. **intrinsify** (atomics/bytealg): `graalgo.runtime.Atomics` is "
      "real (P3-M4 audit, 16 methods) and reachable from Go via the "
      "phase-3 AtomicOpNode; bytealg hot paths map to "
      "`Intrinsics.memequal` (real) + planned ByteAlg vector nodes, with "
      "the pure-Go `*_generic.go` fallback selectable by excluding the "
      "arch asm.")
    a("3. **emulate-then-intrinsify** (crypto): the emulate leg compiles "
      "the pure-Go generic implementation with arch asm excluded; the "
      "intrinsify leg (per-algorithm SIMD intrinsics) is a later phase -- "
      "re-test trigger recorded per file.")
    a("4. **jvm-math** (math): the JVM implements IEEE754 exactly "
      "(java.lang.StrictMath); math/big's asm kernels are speedups over "
      "mostly-Go code (T4), with java.math.BigInteger semantics as the "
      "reference.")
    a("5. **unsupported**: toolchain sources (cmd/), vendored modules "
      "(vendor/), and stdlib asm with no decided strategy yet "
      "(syscall trampolines, hash/crc32, reflect stubs, experimental "
      "simd). Each carries its re-test trigger (the bring-up wave that "
      "decides it). Encountering one is a build error, never silent.")
    a("")
    a("## Unsupported files (fail-fast registry)")
    a("")
    a("These are rejected by the importer with a named error. Each row is "
      "the complete fail-fast payload (file, package, category, reason).")
    a("")
    for e in entries:
        if e["category"] == "unsupported":
            a(f"- `{e['path']}` (package `{e['package']}`): {e['reason']}")
    a("")
    a("## Re-test triggers")
    a("")
    a("- Go toolchain upgrade: re-run the generator; any new/uncategorized "
      "assembly file fails the generator. Re-verify category counts.")
    a("- crypto intrinsify leg lands: re-run emulate-leg differential "
      "corpora; re-verify every emulate-then-intrinsify entry.")
    a("- syscall wave-2 bring-up: decide per-file strategy for the "
      "syscall trampolines.")
    a("- hash/crc32 wave-1 bring-up: decide intrinsify-vs-Go-fallback.")
    a("- reflect wave-2 bring-up: decide per-file strategy for the "
      "makeFuncStub/callReflect stubs.")
    a("- internal/cpu Go-level implementation lands: re-verify the "
      "coupling note (crypto emulate leg path selection).")
    a("")
    return "\n".join(lines) + "\n"


GO_TEMPLATE = """\
// Code generated by tools/stdlib-fork/gen-asm-inventory.py; DO NOT EDIT.
// Regenerate with: python3 tools/stdlib-fork/gen-asm-inventory.py
package p3frontend

// asmInventoryJSON is the machine-readable Plan 9 assembly inventory
// (go1.27.1), generated from asm-inventory-go1.27.1.json. asm.go parses it.
var asmInventoryJSON = []byte(`%s`)
"""


def render_go(new_json):
    if "`" in new_json:
        raise SystemExit("gen-asm-inventory: JSON contains a backtick; "
                         "cannot embed as a raw string literal")
    return GO_TEMPLATE % new_json


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--goroot", default=os.path.expanduser("~/tools/go-1.27.1"),
                    help="Go toolchain root (default: %(default)s)")
    ap.add_argument("--check", action="store_true",
                    help="verify the committed JSON is complete and current")
    args = ap.parse_args()

    src = os.path.join(args.goroot, "src")
    if not os.path.isdir(src):
        raise SystemExit(f"gen-asm-inventory: no src dir at {src}")

    entries = build_inventory(src)
    total_lines = sum(e["lines"] for e in entries)
    payload = {
        "generated_by": "tools/stdlib-fork/gen-asm-inventory.py",
        "go_version": VERSION,
        "categories": list(CATEGORIES),
        "files": entries,
    }
    new_json = json.dumps(payload, indent=2) + "\n"
    new_md = render_md(entries, total_lines)

    json_path = os.path.join(SCRIPT_DIR, JSON_NAME)
    md_path = os.path.join(SCRIPT_DIR, MD_NAME)
    go_path = os.path.join(GO_DIR, GO_NAME)
    new_go = render_go(new_json)
    if args.check:
        problems = []
        if not os.path.exists(json_path):
            problems.append(f"missing {JSON_NAME}")
        elif open(json_path).read() != new_json:
            problems.append(f"{JSON_NAME} is stale -- re-run the generator")
        if not os.path.exists(md_path):
            problems.append(f"missing {MD_NAME}")
        elif open(md_path).read() != new_md:
            problems.append(f"{MD_NAME} is stale -- re-run the generator")
        if not os.path.exists(go_path):
            problems.append(f"missing {GO_NAME}")
        elif open(go_path).read() != new_go:
            problems.append(f"{GO_NAME} is stale -- re-run the generator")
        if problems:
            raise SystemExit("gen-asm-inventory --check FAILED:\n  "
                             + "\n  ".join(problems))
        print(f"gen-asm-inventory --check OK: {len(entries)} files, "
              f"{total_lines:,} lines, all categorized")
        return

    with open(json_path, "w") as f:
        f.write(new_json)
    with open(md_path, "w") as f:
        f.write(new_md)
    os.makedirs(GO_DIR, exist_ok=True)
    with open(go_path, "w") as f:
        f.write(new_go)
    from collections import Counter
    counts = Counter(e["category"] for e in entries)
    print(f"wrote {JSON_NAME} + {MD_NAME}: {len(entries)} files, "
          f"{total_lines:,} lines")
    for c in CATEGORIES:
        print(f"  {c:24s} {counts[c]:4d}")


if __name__ == "__main__":
    main()
