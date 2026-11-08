#!/usr/bin/env python3
"""Synthesize a single-file test driver for a stdlib package.

Takes the discovery JSON and generates a main-package Go file that:
  1. Inlines the testing shim (as package main).
  2. Includes the test function sources (package clause stripped,
     `*testing.T` rewritten to `*T`).
  3. Has a main() that runs each test via shim.Run and reports.

Only external test packages (pkg_test) are supported; internal test files
are skipped with a note. Files with unsupported imports are skipped.

Usage: synthesize.py <discover.json> --out driver.go [--shim shim.go]
"""
import json
import os
import re
import sys

# Imports that graalgo is known to handle (from wave corpora). Others -> skip file.
# This list grows as the harness is validated; be conservative in the pilot.
SUPPORTED_IMPORTS = {
    "bytes", "errors", "fmt", "math", "math/cmplx", "reflect", "regexp", "sort",
    "strconv", "strings", "sync", "unicode", "unicode/utf8",
    "encoding", "encoding/json", "io", "io/fs", "os", "path", "time",
    # Test-only helpers that are simple
    "flag",
}

# Build-tag environment the gate runs in (linux/amd64, gc toolchain).
# This ports test/differential/internal/srctest/srctest.go's envTags +
# goVersionSatisfied (go1.N satisfied for N <= 27) to Python, so the gate1
# driver only includes test files gc itself would run on this platform.
# A file whose constraints are not satisfied here is skipped, never run:
# e.g. bytes/bytes_js_wasm_test.go (//go:build js && wasm) allocates a
# 2 GiB slice that must never be scheduled on linux/amd64 (R-1108-141).
ENV_TAGS = {"linux", "amd64", "unix", "gc"}
GO_VERSION = 27


def _tokenize_build_expr(expr):
    """Split a //go:build boolean expression into tokens."""
    toks = []
    cur = ""
    i = 0
    while i < len(expr):
        ch = expr[i]
        if ch in " \t":
            if cur:
                toks.append(cur)
                cur = ""
        elif ch in "()":
            if cur:
                toks.append(cur)
                cur = ""
            toks.append(ch)
        elif ch == "!":
            if cur:
                toks.append(cur)
                cur = ""
            toks.append("!")
        elif ch in "&|":
            if cur:
                toks.append(cur)
                cur = ""
            if i + 1 < len(expr) and expr[i + 1] == ch:
                toks.append(ch * 2)
                i += 1
            else:
                # A lone & or | is a parse error; the parser rejects it
                # (pos != len(toks)) and the expression is unsatisfiable.
                toks.append(ch)
        else:
            cur += ch
        i += 1
    if cur:
        toks.append(cur)
    return toks


def _eval_build_term(term):
    """Evaluate one build-tag term against ENV_TAGS (cf. srctest parseUnary)."""
    if term == "ignore":
        return False
    m = re.match(r"^go1\.(\d+)", term)
    if m:
        return int(m.group(1)) <= GO_VERSION
    return term in ENV_TAGS


class _BuildExprParser:
    """Recursive-descent parser for //go:build expressions (cf. srctest)."""

    def __init__(self, toks):
        self.toks = toks
        self.pos = 0

    def parse(self):
        value, ok = self._parse_or()
        return ok and value and self.pos == len(self.toks)

    def _parse_or(self):
        left, ok = self._parse_and()
        if not ok:
            return False, False
        while self.pos < len(self.toks) and self.toks[self.pos] == "||":
            self.pos += 1
            right, ok = self._parse_and()
            if not ok:
                return False, False
            left = left or right
        return left, True

    def _parse_and(self):
        left, ok = self._parse_unary()
        if not ok:
            return False, False
        while self.pos < len(self.toks) and self.toks[self.pos] == "&&":
            self.pos += 1
            right, ok = self._parse_unary()
            if not ok:
                return False, False
            left = left and right
        return left, True

    def _parse_unary(self):
        if self.pos < len(self.toks) and self.toks[self.pos] == "!":
            self.pos += 1
            value, ok = self._parse_unary()
            return (not value), ok
        if self.pos < len(self.toks) and self.toks[self.pos] == "(":
            self.pos += 1
            value, ok = self._parse_or()
            if not ok or self.pos >= len(self.toks) or self.toks[self.pos] != ")":
                return False, False
            self.pos += 1
            return value, True
        if self.pos >= len(self.toks):
            return False, False
        term = self.toks[self.pos]
        self.pos += 1
        return _eval_build_term(term), True


def build_expr_satisfied(expr):
    """True iff a //go:build boolean expression holds on linux/amd64+gc."""
    expr = expr.strip()
    if not expr:
        return False
    return _BuildExprParser(_tokenize_build_expr(expr)).parse()


def plus_build_to_expr(line):
    """Convert a // +build line to an equivalent //go:build expression.

    Per go/build: within one line, space-separated groups are ORed and
    comma-separated tags within a group are ANDed; multiple lines are ANDed
    (tags_satisfied evaluates each line separately).
    """
    rest = line[len("// +build"):].strip()
    groups = []
    for group in rest.split():
        parts = [p for p in group.split(",") if p]
        if not parts:
            return ""
        if len(parts) > 1:
            groups.append("(" + " && ".join(parts) + ")")
        else:
            groups.append(parts[0])
    return " || ".join(groups)


def tags_satisfied(build_tags):
    """True iff every build-constraint line in build_tags holds here.

    build_tags is the list discover.py emits: raw '//go:build ...' and/or
    '// +build ...' lines. An empty list (no constraints) is satisfied;
    multiple lines are ANDed, matching go/build. A line in neither form,
    or an unparsable expression, fails closed (the file is skipped).
    """
    for line in build_tags or []:
        line = line.strip()
        if line.startswith("//go:build"):
            expr = line[len("//go:build"):].strip()
        elif line.startswith("// +build"):
            expr = plus_build_to_expr(line)
        else:
            return False
        if not build_expr_satisfied(expr):
            return False
    return True

def load_shim(path):
    with open(path) as f:
        src = f.read()
    # Strip package clause (may follow leading doc comment); caller provides it.
    src = re.sub(r"^package\s+\w+\n", "", src, count=1, flags=re.MULTILINE)
    # Strip the shim's import block (driver provides fmt/os already).
    src = re.sub(r'^import\s*\([^)]*\)\n', "", src, count=1, flags=re.MULTILINE)
    src = re.sub(r'^import\s+"[^"]+"\n', "", src, flags=re.MULTILINE)
    return src

def process_test_file(path, pkg):
    """Return (test_names, rewritten_source) or (None, skip_reason)."""
    with open(path, encoding="utf-8", errors="replace") as f:
        src = f.read()

    # Check package clause (may follow leading comments)
    m = re.search(r"^package\s+(\w+)", src, re.MULTILINE)
    if not m:
        return None, "no package clause"
    pkgname = m.group(1)
    # Only external test packages (foo_test) — internal ones need unexported access
    if pkgname != f"{pkg}_test":
        return None, f"internal test package {pkgname}"

    # Extract test function names
    tests = re.findall(r"^func\s+(Test[A-Z]\w*)\s*(?:\[[^\]]*\])?\s*\(", src, re.MULTILINE)
    # Files with no Test functions but with top-level decls (e.g. import_test.go
    # aliases) are still useful — include them with empty test list.
    has_decls = bool(re.search(r"^(?:var|const|func)\s", src, re.MULTILINE))
    if not tests and not has_decls:
        return None, "no Test functions"

    # Strip package clause (may follow leading comments)
    src = re.sub(r"^package\s+\w+\n", "", src, count=1, flags=re.MULTILINE)

    # Rewrite imports: remove "testing" and the target package (driver provides it).
    # We handle the import block textually.
    def rewrite_imports(m):
        block = m.group(0)
        # Remove "testing" import lines
        block = re.sub(r'^\s*(?:"testing"|_?\w+\s+"testing")\s*\n', "", block, flags=re.MULTILINE)
        # Remove target package imports (plain, dot, or aliased) — driver dot-imports it
        block = re.sub(r'^\s*(?:"' + pkg + r'"|\.\s+"' + pkg + r'"|\w+\s+"' + pkg + r'")\s*\n', "", block, flags=re.MULTILINE)
        return block
    src = re.sub(r"import\s*\([^)]*\)", rewrite_imports, src)
    src = re.sub(r'^\s*import\s+"testing"\s*\n', "", src, flags=re.MULTILINE)

    # Check remaining imports for support — parse only the import block/line.
    imports = set()
    for im in re.finditer(r'import\s*(?:\((.*?)\)|"([^"]+)")', src, re.DOTALL):
        block = im.group(1) or im.group(2) or ""
        for line in block.split("\n"):
            line = line.strip()
            # Match: "pkg", alias "pkg", . "pkg", _ "pkg"
            m2 = re.match(r'^(?:[\w.]+\s+)?"([\w./]+)"$', line)
            if m2:
                imports.add(m2.group(1))
    # The target package itself is fine (driver dot-imports it)
    imports.discard(pkg)
    unsupported = {i for i in imports if i not in SUPPORTED_IMPORTS}
    if unsupported:
        return None, f"unsupported imports: {sorted(unsupported)}"

    # Rewrite *testing.T -> *T, testing.B -> skip (we don't run benchmarks)
    if "testing.B" in src:
        # Remove benchmark functions
        src = re.sub(r"^func\s+Benchmark\w+\s*\([^)]*\)\s*\{.*?\n\}\n", "", src, flags=re.MULTILINE | re.DOTALL)
    src = src.replace("*testing.T", "*T")

    # Package-level testing.* calls -> shim equivalents
    src = src.replace("testing.Short()", "testingShort()")
    src = src.replace("testing.AllocsPerRun", "testingAllocsPerRun")

    return tests, src

def main():
    if len(sys.argv) < 2:
        sys.exit("usage: synthesize.py <discover.json> --out driver.go [--shim shim.go]")
    with open(sys.argv[1]) as f:
        disc = json.load(f)
    out = sys.argv[sys.argv.index("--out") + 1]
    shim_path = None
    if "--shim" in sys.argv:
        shim_path = sys.argv[sys.argv.index("--shim") + 1]

    pkg = disc["package"]
    goroot = disc["goroot"]

    # (parts built after collecting imports below)

    all_tests = []
    skipped = []
    # Collect imports from all test files for the driver header
    driver_imports = set()
    file_sources = []
    for f in disc["files"]:
        # Honor the build constraints discover.py collected: a file gc would
        # not run on linux/amd64 (e.g. //go:build js && wasm) must never be
        # scheduled here. Skip loudly; the reason is printed with the rest.
        if not tags_satisfied(f.get("build_tags") or []):
            skipped.append((os.path.basename(f["path"]),
                            f"unsatisfiable build tags: {f.get('build_tags')}"))
            continue
        result, data = process_test_file(f["path"], pkg)
        if result is None:
            skipped.append((os.path.basename(f["path"]), data))
            continue
        all_tests.extend(result)
        # Extract imports from this file's source for hoisting
        # Handles: import "x", import . "x", import a "x", import ( ... )
        for im in re.finditer(r'import\s*(?:\((.*?)\)|(?:[\w.]+\s+)?"([^"]+)")', data, re.DOTALL):
            block = im.group(1)
            if block is not None:
                for line in block.split("\n"):
                    line = line.strip()
                    if not line:
                        continue
                    driver_imports.add(line)
            else:
                # Single import: reconstruct with any prefix
                full = im.group(0)
                # Extract the prefix (. or alias) if present
                pm = re.match(r'import\s+([\w.]+)\s+"', full)
                prefix = pm.group(1) + " " if pm else ""
                driver_imports.add(f'{prefix}"{im.group(2)}"')
        # Remove the import block from the file source (hoisted to driver)
        data = re.sub(r'import\s*(?:\([^)]*\)|(?:[\w.]+\s+)?"[^"]+")\n?', "", data)
        # For strconv: drop import_test.go aliases (we provide replicas directly)
        if pkg == "strconv" and f["path"].endswith("import_test.go"):
            data = re.sub(r"var\s*\(\s*baseError\s*=\s*BaseError[^)]*\)", "", data, flags=re.DOTALL)
        file_sources.append((os.path.basename(f["path"]), data))

    # Build the driver
    parts = []
    parts.append(f"// Code generated by gate1 synthesize.py — DO NOT EDIT.\n// Package {pkg} test driver.\n")
    parts.append("package main\n")
    parts.append("import (")
    parts.append('\t"fmt"')
    parts.append('\t"os"')
    # Determine import style: if test code uses qualified refs (pkg.X),
    # use regular import; else dot-import for unqualified refs.
    # Check the combined file sources for qualified usage, ignoring string
    # literals (test expectations often mention "pkg.Func" in strings).
    combined = "\n".join(data for _, data in file_sources)
    no_strings = re.sub(r"`[^`]*`", "", combined)  # raw strings
    no_strings = re.sub(r'"(?:[^"\\]|\\.)*"', "", no_strings)  # interpreted strings
    use_qualified = bool(re.search(rf"\b{pkg}\.\w+", no_strings))
    if use_qualified:
        parts.append(f'\t"{pkg}"')
    else:
        parts.append(f'\t. "{pkg}"')
    for imp in sorted(driver_imports):
        # Skip fmt/os/pkg (already imported); normalize for comparison
        norm = re.sub(r'^[\w.]+\s+', '', imp).strip('"')
        if norm in ("fmt", "os", pkg):
            continue
        parts.append(f"\t{imp}")
    parts.append(")\n")

    if shim_path:
        parts.append(load_shim(shim_path))

    # Shim helpers for package-level testing calls
    parts.append("""
func testingShort() bool { return false }
func testingAllocsPerRun(runs int, f func()) float64 { return 0 }
""")

    # Replicas of unexported test helpers (from export_test.go/import_test.go).
    # These are test-only; they use only exported APIs.
    if pkg == "strconv":
        parts.append("""
// Replicas for gate1 harness (originals unexported in strconv):
func baseError(fn, str string, base int) *NumError {
	return &NumError{fn, str, errors.New("invalid base " + Itoa(base))}
}
func bitSizeError(fn, str string, bitSize int) *NumError {
	return &NumError{fn, str, errors.New("invalid bit size " + Itoa(bitSize))}
}
""")

    for name, data in file_sources:
        parts.append(f"\n// --- from {name} ---\n")
        parts.append(data)

    # main() that runs all tests
    parts.append("\nfunc main() {\n")
    parts.append("\tresults := []Result{}\n")
    for t in all_tests:
        parts.append(f'\tresults = append(results, Run("{t}", {t}))\n')
    parts.append("\tReport(results)\n}\n")

    # Rewrite bare Test function references that might collide with helpers:
    # (no-op for now; collisions reported at build time)

    with open(out, "w") as f:
        f.write("\n".join(parts))

    print(f"synthesized {len(all_tests)} tests from {pkg}", file=sys.stderr)
    for name, reason in skipped:
        print(f"  SKIP file {name}: {reason}", file=sys.stderr)

if __name__ == "__main__":
    main()
