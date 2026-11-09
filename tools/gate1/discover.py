#!/usr/bin/env python3
"""Discover test functions in a stdlib package.

For a given package (e.g. 'strings'), finds all *_test.go files under
$GOROOT/src/<pkg>, extracts `func TestXxx(t *testing.T)` names, and reports
the imports each test file needs. Output is JSON for the synthesizer.

Usage: discover.py <package> [--goroot PATH]
"""
import ast
import json
import os
import re
import sys

import buildtags

def find_test_files(goroot, pkg):
    pkgdir = os.path.join(goroot, "src", pkg)
    if not os.path.isdir(pkgdir):
        sys.exit(f"no such package dir: {pkgdir}")
    return sorted(
        os.path.join(pkgdir, f)
        for f in os.listdir(pkgdir)
        if f.endswith("_test.go")
    )

# Matches: func TestXxx(t *testing.T) {
# Also: func TestXxx(t *testing.T) { with possible type params (rare in tests)
TEST_FN_RE = re.compile(
    r"^func\s+(Test[A-Z]\w*)\s*(?:\[[^\]]*\])?\s*\(\s*\w+\s+\*testing\.T\s*\)",
    re.MULTILINE,
)

# Matches import blocks to detect unsupported imports
IMPORT_RE = re.compile(r'^\s*(?:"([\w./]+)"|([\w.]+)\s+"([\w./]+)")', re.MULTILINE)

def extract_tests(path):
    with open(path, encoding="utf-8", errors="replace") as f:
        src = f.read()
    tests = TEST_FN_RE.findall(src)
    # Collect imports (rough; for skip-classification)
    imports = set()
    for m in IMPORT_RE.finditer(src):
        imp = m.group(1) or m.group(3)
        if imp:
            imports.add(imp)
    # Detect build tags (//go:build lines at top)
    build_tags = []
    for line in src.split("\n")[:10]:
        if line.startswith("//go:build"):
            build_tags.append(line.strip())
        elif line and not line.startswith("//") and not line.startswith("package"):
            break
    return tests, sorted(imports), build_tags

def main():
    if len(sys.argv) < 2:
        sys.exit("usage: discover.py <package> [--goroot PATH]")
    pkg = sys.argv[1]
    goroot = os.path.expanduser("~/tools/go")
    if "--goroot" in sys.argv:
        goroot = sys.argv[sys.argv.index("--goroot") + 1]
    goos = None
    goarch = None
    if "--goos" in sys.argv:
        goos = sys.argv[sys.argv.index("--goos") + 1]
    if "--goarch" in sys.argv:
        goarch = sys.argv[sys.argv.index("--goarch") + 1]
    # Target defaults to the host toolchain (go env); explicit
    # --goos/--goarch override it (e.g. the js/wasm positive control).
    try:
        if goos is None or goarch is None:
            default_goos, default_goarch = buildtags.default_target(
                buildtags.go_binary(goroot))
            goos = goos or default_goos
            goarch = goarch or default_goarch
    except buildtags.BuildTagError as e:
        sys.exit(f"build tag evaluation failed: {e}")

    files = find_test_files(goroot, pkg)
    # R20B-05: exclude test files whose build constraints do not
    # match the target (//go:build, filename suffixes, ...). The
    # decision comes from `go list` via buildtags.py; excluded
    # files are reported under "skipped_files", never synthesized.
    try:
        kept, skipped = buildtags.classify_files(
            buildtags.go_binary(goroot), goroot, pkg, files, goos, goarch)
    except buildtags.BuildTagError as e:
        sys.exit(f"build tag evaluation failed: {e}")
    result = {"package": pkg, "goroot": goroot,
              "target_goos": goos, "target_goarch": goarch,
              "files": [], "skipped_files": []}
    for path in kept:
        tests, imports, tags = extract_tests(path)
        result["files"].append({
            "path": path,
            "tests": tests,
            "imports": imports,
            "build_tags": tags,
        })
    for path in sorted(skipped):
        tests, imports, tags = extract_tests(path)
        result["skipped_files"].append({
            "path": path,
            "tests": tests,
            "imports": imports,
            "build_tags": tags,
            "skip_reason": skipped[path],
        })
    result["total_tests"] = sum(len(f["tests"]) for f in result["files"])
    print(json.dumps(result, indent=2))

if __name__ == "__main__":
    main()
