#!/usr/bin/env python3
"""Layering gate (make check-repo): the new path must not depend on v1.

Two rules (toolexec-design D11/D12: the v1 pipeline is deleted at M3, so
nothing the new path needs may live in, or reach into, v1 code):

  java  No class reachable from the new node package (graalgo.go.*) may
        reference a v1 package: graalgo.frontend, graalgo.values,
        graalgo.p3*, graalgo.iface, graalgo.rtstack, graalgo.rtmemstats
        (and their subpackages). Reachability is transitive through the
        shared code (graalgo.language, graalgo.runtime, graalgo.sched, ...):
        a graalgo.go class that calls GoLanguage, which calls
        frontend.NodeFactory, depends on v1 just the same. References are
        imports, static imports, fully qualified names in code, and simple
        names of classes in the same package (comments and string literals
        are ignored).
  go    No Go file of internal/toolexec may import internal/frontend,
        internal/driver or internal/vet (by the root module path or the
        frontend modules' own paths), and its go.mod may not require or
        replace them.

A violation is an edge "<class> -> <v1 class>" whose source is reachable
from graalgo.go.* (or "<go file> -> <import path>"). The current
violations are grandfathered in
.github/scripts/layering_allowlist.txt, one edge per line with a comment
naming the owner and the split plan. The list only shrinks:
  - a violation not on the list FAILS;
  - a listed edge that no longer occurs FAILS until it is removed from the
    list (so a paid-down dependency cannot silently come back).

Usage: check_layering.py [repo-root]
"""

import re
import sys
from pathlib import Path

ALLOWLIST = Path(".github/scripts/layering_allowlist.txt")
JAVA_ROOTS = (
    "internal/backend/truffle/src/main/java",
    "internal/backend/interop/src/main/java",
)
NEW_PATH = "graalgo.go"
V1_PACKAGES = re.compile(r"^graalgo\.(frontend|values|p3[A-Za-z0-9_]*|iface|rtstack|rtmemstats)(\.|$)")
TOOLEXEC = "internal/toolexec"
GO_FORBIDDEN = re.compile(
    r"^(github\.com/CAFxX/graalgo/internal/(frontend|driver|vet)|graalgo-frontend(-v2)?)(/|$)")

COMMENT_OR_STRING = re.compile(
    r'//[^\n]*|/\*.*?\*/|"""(?:.|\n)*?"""|"(?:\\.|[^"\\\n])*"|\'(?:\\.|[^\'\\\n])*\'',
    re.S)
PACKAGE = re.compile(r"^\s*package\s+([\w.]+)\s*;", re.M)
IMPORT = re.compile(r"^\s*import\s+(static\s+)?([\w.]+)(\.\*)?\s*;", re.M)
QUALIFIED = re.compile(r"\b(graalgo(?:\.[a-z_][\w]*)*)\.([A-Z]\w*)")
IDENT = re.compile(r"\b[A-Z]\w*\b")


def in_package(pkg, prefix):
    return pkg == prefix or pkg.startswith(prefix + ".")


def strip(text):
    return COMMENT_OR_STRING.sub(lambda m: "\n" * m.group(0).count("\n"), text)


def java_classes(root):
    """FQN -> (package, stripped source) for every top-level Java class."""
    classes = {}
    for base in JAVA_ROOTS:
        d = root / base
        if not d.is_dir():
            continue
        for f in sorted(d.rglob("*.java")):
            text = strip(f.read_text(encoding="utf-8", errors="replace"))
            m = PACKAGE.search(text)
            pkg = m.group(1) if m else ""
            name = f.stem
            if name in ("package-info", "module-info"):
                continue
            classes[f"{pkg}.{name}" if pkg else name] = (pkg, text)
    return classes


def java_edges(fqn, pkg, text, classes, by_package):
    """The graalgo classes (or wildcard packages) fqn's source references."""
    out = set()
    for static, name, star in IMPORT.findall(text):
        if not name.startswith("graalgo."):
            continue
        if star and not static:
            out.add(name + ".*")
            continue
        # static imports name a member: trim to the class.
        parts = name.split(".")
        while parts and ".".join(parts) not in classes:
            parts.pop()
        out.add(".".join(parts) if parts else name)
    for p, cls in QUALIFIED.findall(text):
        cand = f"{p}.{cls}"
        out.add(cand)
    body = IMPORT.sub("", text)
    same = by_package.get(pkg, ())
    if same:
        idents = set(IDENT.findall(body))
        for simple in same:
            if simple in idents:
                out.add(f"{pkg}.{simple}")
    out.discard(fqn)
    return out


def java_violations(root):
    classes = java_classes(root)
    by_package = {}
    for fqn, (pkg, _) in classes.items():
        by_package.setdefault(pkg, set()).add(fqn.rsplit(".", 1)[-1])
    edges = {fqn: java_edges(fqn, pkg, text, classes, by_package)
             for fqn, (pkg, text) in classes.items()}

    def pkg_of(ref):
        if ref.endswith(".*"):
            return ref[:-2]
        if ref in classes:
            return classes[ref][0]
        return ref.rsplit(".", 1)[0]

    roots = sorted(f for f, (pkg, _) in classes.items() if in_package(pkg, NEW_PATH))
    seen = set(roots)
    queue = list(roots)
    violations = set()
    while queue:
        cur = queue.pop()
        for ref in sorted(edges.get(cur, ())):
            if V1_PACKAGES.match(pkg_of(ref)):
                violations.add(f"{cur} -> {ref}")
            elif ref in classes and ref not in seen:
                seen.add(ref)
                queue.append(ref)
    return violations


GO_IMPORT_BLOCK = re.compile(r"^import\s*\((.*?)^\)", re.M | re.S)
GO_IMPORT_LINE = re.compile(r'^import\s+(?:[\w.]+\s+)?"([^"]+)"', re.M)
GO_QUOTED = re.compile(r'"([^"]+)"')


def go_violations(root):
    violations = set()
    d = root / TOOLEXEC
    if not d.is_dir():
        return violations
    for f in sorted(d.rglob("*.go")):
        text = f.read_text(encoding="utf-8", errors="replace")
        paths = GO_IMPORT_LINE.findall(text)
        for block in GO_IMPORT_BLOCK.findall(text):
            for line in block.splitlines():
                line = line.split("//", 1)[0]
                paths += GO_QUOTED.findall(line)
        rel = f.relative_to(root).as_posix()
        for p in paths:
            if GO_FORBIDDEN.match(p):
                violations.add(f"{rel} -> {p}")
    for mod in sorted(d.rglob("go.mod")):
        rel = mod.relative_to(root).as_posix()
        for line in mod.read_text(encoding="utf-8", errors="replace").splitlines():
            t = line.split("//", 1)[0].strip()
            for tok in re.split(r"[\s=>()]+", t):
                tok = tok.strip('"')
                if GO_FORBIDDEN.match(tok) or re.search(r"(^|/)\.\./+(frontend|driver|vet)(/|$)", tok):
                    violations.add(f"{rel} -> {tok}")
    return violations


def load_allowlist(root):
    p = root / ALLOWLIST
    allowed = {}
    if not p.is_file():
        return allowed, []
    errors = []
    for n, line in enumerate(p.read_text(encoding="utf-8").splitlines(), 1):
        body, _, comment = line.partition("#")
        body = body.strip()
        if not body:
            continue
        if " -> " not in body:
            errors.append(f"{ALLOWLIST}:{n}: not an edge '<from> -> <to>': {body!r}")
            continue
        if not comment.strip():
            errors.append(f"{ALLOWLIST}:{n}: {body}: needs a '# owner: split plan' comment")
        allowed[body] = n
    return allowed, errors


def main(argv):
    root = Path(argv[1]) if len(argv) > 1 else Path(".")
    allowed, errors = load_allowlist(root)
    found = java_violations(root) | go_violations(root)
    for v in sorted(found - set(allowed)):
        errors.append(f"layering violation (v1 dependency on the new path): {v}")
    for v in sorted(set(allowed) - found):
        errors.append(f"{ALLOWLIST}:{allowed[v]}: stale entry (no longer occurs; remove it): {v}")
    if errors:
        for e in errors:
            print(e)
        print(f"layering gate: {len(errors)} problem(s); see the header of {Path(__file__).name}")
        return 1
    print(f"layering gate clean ({len(found)} grandfathered edge(s) in {ALLOWLIST})")
    return 0


if __name__ == "__main__":
    sys.exit(main(sys.argv))
