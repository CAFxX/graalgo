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

    files = find_test_files(goroot, pkg)
    result = {"package": pkg, "goroot": goroot, "files": []}
    for path in files:
        tests, imports, tags = extract_tests(path)
        result["files"].append({
            "path": path,
            "tests": tests,
            "imports": imports,
            "build_tags": tags,
        })
    result["total_tests"] = sum(len(f["tests"]) for f in result["files"])
    print(json.dumps(result, indent=2))

if __name__ == "__main__":
    main()
