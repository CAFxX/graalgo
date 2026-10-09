#!/usr/bin/env python3
# P3-M9 tier-5 asymmetric crypto: merges go1.27.1 stdlib sources into single
# synthetic package sources (package clause dedup, import-block merge with
# alias canonicalization, build-tag stripping).
#
# Usage: merge.py <spec.json> <outdir>
# spec.json: {"packages": [{"path": "...", "name": "ConstSuffix",
#   "files": ["rel/path1.go", ...], "adapt": {"old_import": "new_import", ...},
#   "header": "...", "extra_src": "..."}]}
# All file paths relative to GOROOT/src. "adapt" rewrites import paths
# textually (for adapted packages). "extra_src" is appended verbatim
# (for stubs like fipsSelfTest).
import json, re, sys, os

GOROOT = "/home/hatch/tools/go-1.27.1"

def strip_build_tags(src):
    lines = src.split("\n")
    out = []
    for ln in lines:
        if ln.startswith("//go:build") or re.match(r"^// \+build", ln):
            continue
        out.append(ln)
    return "\n".join(out)

def split_file(src):
    """Return (package_clause, [(name, path)], body)."""
    src = strip_build_tags(src)
    # package clause: first line starting with 'package '
    m = re.search(r"^package\s+(\w+)\s*$", src, re.M)
    assert m, "no package clause"
    pkgname = m.group(1)
    rest = src[m.end():]
    imports = []
    # import blocks and single imports
    def import_repl(mb):
        block = mb.group(0)
        for im in re.finditer(r'(?:(\w+|_|\.)\s+)?["`]([^"`]+)["`]', block):
            name, path = im.group(1), im.group(2)
            imports.append((name, path))
        return ""
    # remove import blocks
    rest = re.sub(r"import\s*\([^)]*\)", import_repl, rest)
    # remove single-line imports: import "x" / import _ "x" / import name "x"
    def single_repl(ms):
        name, path = ms.group(1), ms.group(2)
        imports.append((name, path))
        return ""
    rest = re.sub(r'(?m)^import\s+(?:(\w+|_|\.)\s+)?["`]([^"`]+)["`]\s*$', single_repl, rest)
    return pkgname, imports, rest

def merge_package(spec):
    pkgname0 = None
    all_imports = []  # (name, path) in first-seen order
    seen_path = {}
    bodies = []
    drop_imports = set(spec.get("drop_imports", []))
    for f in spec["files"]:
        with open(os.path.join(GOROOT, "src", f)) as fh:
            src = fh.read()
        pkgname, imports, body = split_file(src)
        if pkgname0 is None:
            pkgname0 = pkgname
        else:
            assert pkgname == pkgname0, f"package name mismatch in {f}: {pkgname} vs {pkgname0}"
        # apply import-path adaptations
        adapt = spec.get("adapt", {})
        new_imports = []
        for (name, path) in imports:
            path = adapt.get(path, path)
            new_imports.append((name, path))
        # merge, detecting alias conflicts
        for (name, path) in new_imports:
            if path in drop_imports:
                continue
            if path in seen_path:
                if seen_path[path] != name:
                    print(f"ALIAS CONFLICT in {spec['path']}: {path} as {seen_path[path]} and {name}",
                          file=sys.stderr)
                    sys.exit(1)
            else:
                seen_path[path] = name
                all_imports.append((name, path))
        bodies.append((f, body))
    # canonicalize: drop exact duplicates already handled; build import block
    import_lines = []
    for (name, path) in all_imports:
        if name:
            import_lines.append(f'\t{name} "{path}"')
        else:
            import_lines.append(f'\t"{path}"')
    header = spec.get("header", "")
    extra = spec.get("extra_src", "")
    parts = [header, f"package {pkgname0}\n"]
    if import_lines:
        parts.append("import (\n" + "\n".join(import_lines) + "\n)\n")
    for (f, body) in bodies:
        parts.append(f"\n// ---- vendored from {f} ----\n")
        parts.append(body)
    if extra:
        parts.append("\n// ---- synthetic additions ----\n")
        parts.append(extra)
    merged = "\n".join(parts)
    for old, new in spec.get("replacements", []):
        if old not in merged:
            print(f"REPLACEMENT NOT FOUND in {spec['path']}: {old[:80]!r}", file=sys.stderr)
            sys.exit(1)
        merged = merged.replace(old, new)
    return merged

def main():
    spec_path, outdir = sys.argv[1], sys.argv[2]
    os.makedirs(outdir, exist_ok=True)
    with open(spec_path) as fh:
        spec = json.load(fh)
    for pkg in spec["packages"]:
        if pkg.get("handwritten"):
            continue
        merged = merge_package(pkg)
        out = os.path.join(outdir, pkg["name"] + ".merged.go")
        with open(out, "w") as fh:
            fh.write(merged)
        print(f"wrote {out} ({len(merged)} bytes)")

if __name__ == "__main__":
    main()
