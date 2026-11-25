#!/usr/bin/env python3
"""Doc-path existence gate (make check-repo).

Every repo path written in a live (non-archived) markdown doc must exist.
check_markdown_links.py already checks [links](...); this gate checks the
*backticked* paths (`internal/foo/bar.go`) that links do not cover — the
kind of claim that silently rots when code moves ("the IR lives in
`internal/ir/`" after internal/ir was never created).

A backticked token counts as a repo path when it has no spaces, globs,
placeholders or variables and either
  - starts with a top-level repo entry (`cmd/`, `internal/`, `docs/`,
    `test/`, `tools/`, `stdlib/`, `requirements/`, `research/`,
    `.github/`, or a root file such as `Makefile`), resolved from the repo
    root, or
  - starts with `./` or `../`, resolved from the doc's directory.
It passes when it exists either way. Tokens that name a Go standard-library
package or file (`internal/abi`, `cmd/go`, `internal/poll/fd.go`) are not
repo paths: a token whose package part exists under $GOROOT/src is skipped
(GOROOT from $GOROOT, else `$GO env GOROOT`, else ~/tools/go). A line may opt out by saying the path
is "(planned)", or that it was removed/deleted/renamed/moved/retired/
superseded (historical mentions are legitimate; claims of existence are
not).

EXEMPT lists (doc, token) pairs in docs this lane may not edit (owned by
another lane); each names its owner and is removed when fixed there.

Archived trees (history, not claims about today) are skipped: see ARCHIVED.

Requirements too: the statement, rationale and verification_plan of every
live requirement (requirements/*.yaml whose status is not superseded or
retired: those are history) name test files and scripts as bare tokens
("test/m1r/programs/x.go through make test-m1r"), so there every bare token
that starts with a top-level repo directory is a candidate, judged the same
way; the opt-out words apply per sentence. A locked requirement's text can
only change through `req supersede`, so a moved test is fixed by reissuing
the requirement.

Usage: check_doc_paths.py [repo-root]
"""

import os
import re
import subprocess
import sys
from pathlib import Path

ARCHIVED = (
    "docs/archive/", "research/", "internal/frontend/go/",
    "test/corpus/",  # per-wave run logs of the research era
)
EXEMPT = {
    # owner: spec-reconcile lane (SPEC.md); should read docs/archive/research/T1-*.md
    ("docs/arch/SPEC.md", "research/T1"),
}
TOP = ("cmd/", "internal/", "docs/", "test/", "tools/", "stdlib/",
       "requirements/", "research/", ".github/")
ROOT_FILES = ("Makefile", "README.md", "AGENTS.md", "go.mod")
OPT_OUT = re.compile(r"\(planned\)|\b(removed|deleted|renamed|moved|retired|superseded|was|were|formerly|old|landed|gitignored)\b",
                     re.IGNORECASE)
TICK = re.compile(r"`([^`\n]+)`")
BARE = re.compile(r"(?<![\w./`-])((?:%s)[A-Za-z0-9_.\-/]*)" % "|".join(re.escape(t) for t in TOP))
REQ_FIELDS = ("statement", "rationale", "verification_plan")
REQ_DEAD = ("superseded", "retired")
SENTENCE = re.compile(r"(?<=[.;])\s+")
FENCE = re.compile(r"^\s*(```|~~~)")
PATHCHARS = re.compile(r"^[A-Za-z0-9_.\-/]+$")


def find_goroot():
    if os.environ.get("GOROOT"):
        return Path(os.environ["GOROOT"])
    go = os.environ.get("GO") or str(Path.home() / "tools/go/bin/go")
    try:
        out = subprocess.run([go, "env", "GOROOT"], capture_output=True, text=True, timeout=30)
        if out.returncode == 0 and out.stdout.strip():
            return Path(out.stdout.strip())
    except OSError:
        pass
    return None


GOROOT_SRC = None


def is_std(tok):
    """`internal/abi`, `internal/abi.TypeOf`, `internal/poll/fd.go`: a Go std name."""
    if GOROOT_SRC is None:
        return False
    t = tok.rstrip("/")
    return (GOROOT_SRC / t).exists() or (GOROOT_SRC / t.rsplit(".", 1)[0]).exists()


def candidate(tok):
    tok = tok.strip()
    if not PATHCHARS.match(tok) or "//" in tok or "..." in tok:
        return None
    if tok.startswith(("./", "../")):
        return tok
    if tok.startswith(TOP) or tok in ROOT_FILES:
        return tok
    return None


def live_docs(root):
    out = subprocess.run(["git", "-C", str(root), "ls-files", "-z", "*.md"],
                         check=True, capture_output=True).stdout.decode()
    for rel in out.split("\0"):
        if rel and not rel.startswith(ARCHIVED):
            yield rel


def check_doc(root, rel, text):
    errors = []
    in_fence = False
    base = (root / rel).parent
    for n, line in enumerate(text.splitlines(), 1):
        if FENCE.match(line):
            in_fence = not in_fence
            continue
        if in_fence or OPT_OUT.search(line):
            continue
        for m in TICK.finditer(line):
            tok = candidate(m.group(1))
            if tok is None:
                continue
            clean = tok.rstrip("/").rstrip(".")
            if (root / clean).exists() or (base / clean).exists():
                continue
            if not tok.startswith(("./", "../")) and is_std(tok):
                continue
            if (rel, tok) in EXEMPT:
                continue
            errors.append(f"{rel}:{n}: `{tok}` does not exist (mark it \"(planned)\" if it is)")
    return errors


def req_fields(text):
    """{field: text} of a requirement YAML's top-level scalars (a minimal
    reader: single-line scalars, optionally quoted, plus indented
    continuation lines; no PyYAML dependency)."""
    out, key = {}, None
    for line in text.splitlines():
        m = re.match(r"^([a-z_]+):\s?(.*)$", line)
        if m:
            key = m.group(1)
            out[key] = m.group(2)
        elif key and line.startswith(" ") and not line.lstrip().startswith("- "):
            out[key] += " " + line.strip()
        else:
            key = None
    for k, v in out.items():
        v = v.strip()
        if len(v) >= 2 and v[0] == v[-1] == "'":
            v = v[1:-1].replace("''", "'")
        elif len(v) >= 2 and v[0] == v[-1] == '"':
            v = v[1:-1]
        out[k] = v
    return out


def check_requirement(root, rel, text):
    f = req_fields(text)
    if f.get("status", "") in REQ_DEAD:
        return []
    errors = []
    for field in REQ_FIELDS:
        for sentence in SENTENCE.split(f.get(field, "")):
            if OPT_OUT.search(sentence):
                continue
            for m in BARE.finditer(sentence):
                tok = m.group(1).rstrip(".,:;)")
                clean = tok.rstrip("/")
                if (root / clean).exists() or is_std(tok):
                    continue
                errors.append(f"{rel}: {field}: {tok} does not exist (reissue the requirement "
                              f"with `req supersede`, or say \"(planned)\")")
    return errors


def requirement_files(root):
    out = subprocess.run(["git", "-C", str(root), "ls-files", "-z", "requirements/*.yaml"],
                         check=True, capture_output=True).stdout.decode()
    return [rel for rel in out.split("\0") if rel]


def scan(root, docs=None):
    root = Path(root)
    errors = []
    for rel in (docs if docs is not None else live_docs(root)):
        p = root / rel
        if p.is_file():
            check = check_requirement if rel.endswith(".yaml") else check_doc
            errors += check(root, rel, p.read_text(errors="replace"))
    return errors


def main(argv):
    global GOROOT_SRC
    gr = find_goroot()
    GOROOT_SRC = gr / "src" if gr and (gr / "src").is_dir() else None
    root = Path(argv[1]) if len(argv) > 1 else Path(__file__).resolve().parents[2]
    errors = scan(root)
    errors += scan(root, requirement_files(root))
    if errors:
        print("DOC-PATH GATE FAILED (backticked repo paths that do not exist):")
        for e in errors:
            print("  " + e)
        return 1
    print("doc-path gate clean")
    return 0


if __name__ == "__main__":
    sys.exit(main(sys.argv))
