#!/usr/bin/env python3
"""Replay native fuzz corpora differentially (GG-0037(i)).

Reads the Go fuzz corpus for this package from the fuzz cache
($GOCACHE/fuzz/<import-path>/FuzzXxx/, grown by `go test -fuzz`),
materializes each entry as a single-file Go program calling the target
stdlib function, and writes a diffharness corpus directory. The caller
then runs `diffharness run -config backends.json -corpus <dir>` for the
gc-vs-graalgo differential.

The corpus does NOT live in testdata/fuzz/: `go test -fuzz` writes new
interesting inputs to the fuzz cache only (testdata/fuzz/ holds optional
hand-written seed files, and this package has none). Pointing replay at
testdata/fuzz/ materializes zero programs and the differential that
follows is vacuously green — a dead pipeline. So the default fuzz dir
is resolved from `go env GOCACHE` plus this package's import path (read
from the enclosing go.mod); --fuzzdir overrides it explicitly, e.g. to
replay a copied corpus. Exits non-zero when zero programs are
materialized (pass --allow-empty to override).

Usage: replay.py [--fuzzdir DIR] [--allow-empty] --out DIR
"""
import argparse
import ast as pyast
import os
import re
import shutil
import subprocess
import sys

# TEMPLATES[target] = (imports, argtypes, body-template).
# {a0}, {a1}, ... are replaced with Go-quoted input literals.
TEMPLATES = {
    "FuzzStringsToUpper": (
        ["strings"], ["string"],
        'fmt.Printf("%q", strings.ToUpper({a0}))'),
    "FuzzStringsToLower": (
        ["strings"], ["string"],
        'fmt.Printf("%q", strings.ToLower({a0}))'),
    "FuzzStringsTrimSpace": (
        ["strings"], ["string"],
        'fmt.Printf("%q", strings.TrimSpace({a0}))'),
    "FuzzStringsCount": (
        ["strings"], ["string", "string"],
        'fmt.Printf("%d", strings.Count({a0}, {a1}))'),
    "FuzzStringsReplaceAll": (
        ["strings"], ["string", "string", "string"],
        'fmt.Printf("%q", strings.ReplaceAll({a0}, {a1}, {a2}))'),
    "FuzzStrconvAtoi": (
        ["strconv"], ["string"],
        'n, err := strconv.Atoi({a0}); fmt.Printf("%d|%v", n, err)'),
    "FuzzStrconvItoa": (
        ["strconv"], ["int"],
        'fmt.Printf("%q", strconv.Itoa({a0}))'),
    "FuzzBytesToUpper": (
        ["bytes"], ["[]byte"],
        'fmt.Printf("%q", bytes.ToUpper({a0}))'),
}


def go_quote_string(s):
    # Go double-quoted literal with escapes.
    out = ['"']
    for ch in s:
        o = ord(ch)
        if ch == '"':
            out.append('\\"')
        elif ch == '\\':
            out.append('\\\\')
        elif ch == '\n':
            out.append('\\n')
        elif ch == '\t':
            out.append('\\t')
        elif ch == '\r':
            out.append('\\r')
        elif 32 <= o < 127:
            out.append(ch)
        elif o < 256:
            out.append('\\x%02x' % o)
        elif o < 65536:
            out.append('\\u%04x' % o)
        else:
            out.append('\\U%08x' % o)
    out.append('"')
    return ''.join(out)


def go_quote_bytes(b):
    return '[]byte{' + ', '.join(str(x) for x in b) + '}'


def parse_corpus_file(path):
    """Parse a Go fuzz corpus file into a list of (type, value) pairs."""
    with open(path, 'rb') as f:
        data = f.read()
    lines = data.decode('utf-8', errors='replace').splitlines()
    if not lines or lines[0] != 'go test fuzz v1':
        raise ValueError(f'{path}: not a v1 fuzz corpus file')
    vals = []
    for line in lines[1:]:
        line = line.strip()
        if not line:
            continue
        m = re.match(r'^(string|\[\]byte|int|int64)\((.*)\)$', line, re.S)
        if not m:
            raise ValueError(f'{path}: cannot parse corpus line: {line!r}')
        typ, lit = m.group(1), m.group(2)
        vals.append((typ, parse_go_literal(typ, lit)))
    return vals


def parse_go_literal(typ, lit):
    # Corpus literals are written by encoding/gob-ish quoting; handle the
    # common shapes: "quoted", []byte("...") / []byte{...}, decimal ints.
    lit = lit.strip()
    if typ == 'string':
        return unquote_go_string(lit)
    if typ == '[]byte':
        if lit.startswith('"'):
            return unquote_go_string(lit).encode('latin1')
        inner = lit.strip('{}')
        return bytes(int(x) for x in inner.split(',') if x.strip())
    if typ in ('int', 'int64'):
        return int(lit, 0)
    raise ValueError(f'unsupported corpus type {typ}')


def unquote_go_string(lit):
    # Unquote a Go string literal. The result is a str holding one char
    # per BYTE of the Go string value (latin1-mapped), which is the
    # representation go_quote_string re-encodes. The old
    # unicode_escape+latin1 round-trip crashed on \\u/\\U escapes above
    # U+00FF and silently corrupted \\u escapes in U+0080..U+00FF (it
    # emitted one byte 0xE9 for "é", whose true value is C3 A9).
    if len(lit) >= 2 and lit[0] == '"' and lit[-1] == '"':
        return parse_go_quoted(lit[1:-1])
    if len(lit) >= 2 and lit[0] == '`' and lit[-1] == '`':
        return lit[1:-1]
    raise ValueError(f'cannot unquote {lit!r}')


_SIMPLE_ESCAPES = {
    'a': '\a', 'b': '\b', 'f': '\f', 'n': '\n', 'r': '\r',
    't': '\t', 'v': '\v', '\\': '\\', '"': '"', "'": "'",
}


def parse_go_quoted(inner):
    """Parse the inside of a Go double-quoted literal to latin1 bytes-as-str."""
    out = []
    i = 0
    n = len(inner)
    while i < n:
        ch = inner[i]
        if ch != '\\':
            # Literal char: append its UTF-8 encoding, byte by byte.
            for b in ch.encode('utf-8'):
                out.append(chr(b))
            i += 1
            continue
        i += 1
        if i >= n:
            raise ValueError('trailing backslash in Go string literal')
        e = inner[i]
        if e in _SIMPLE_ESCAPES:
            out.append(_SIMPLE_ESCAPES[e])
            i += 1
        elif e == 'x':
            out.append(chr(int(inner[i + 1:i + 3], 16)))
            i += 3
        elif e == 'u':
            out.extend(chr(b) for b in chr(int(inner[i + 1:i + 5], 16)).encode('utf-8'))
            i += 5
        elif e == 'U':
            out.extend(chr(b) for b in chr(int(inner[i + 1:i + 9], 16)).encode('utf-8'))
            i += 9
        elif e in '01234567':
            out.append(chr(int(inner[i:i + 3], 8)))
            i += 3
        else:
            raise ValueError(f'unknown Go escape \\{e}')
    return ''.join(out)


def render_arg(typ, val):
    if typ == 'string':
        return go_quote_string(val)
    if typ == '[]byte':
        return go_quote_bytes(val)
    if typ == 'int':
        return str(val)
    raise ValueError(f'unsupported arg type {typ}')


def go_cache_dir():
    """Return the Go build cache dir (`go env GOCACHE`)."""
    env = os.environ.get('GOCACHE')
    if env:
        return env
    go = shutil.which('go')
    if go:
        try:
            out = subprocess.run([go, 'env', 'GOCACHE'],
                                 capture_output=True, text=True, timeout=30)
            if out.returncode == 0 and out.stdout.strip():
                return out.stdout.strip()
        except (OSError, subprocess.SubprocessError):
            pass
    return os.path.expanduser('~/.cache/go-build')


def import_path_of(pkgdir):
    """Import path of pkgdir: module (from the enclosing go.mod) + rel dir."""
    cur = os.path.abspath(pkgdir)
    while True:
        gomod = os.path.join(cur, 'go.mod')
        if os.path.isfile(gomod):
            module = None
            with open(gomod) as f:
                for line in f:
                    line = line.strip()
                    if line.startswith('module '):
                        module = line.split(None, 1)[1].strip()
                        break
            if not module:
                raise ValueError(f'{gomod}: no module line')
            rel = os.path.relpath(os.path.abspath(pkgdir), cur)
            return module if rel == '.' else module + '/' + rel.replace(os.sep, '/')
        parent = os.path.dirname(cur)
        if parent == cur:
            raise ValueError(f'no go.mod found above {pkgdir}')
        cur = parent


def default_fuzzdir():
    """Fuzz cache dir for this package: $GOCACHE/fuzz/<import-path>/."""
    here = os.path.dirname(os.path.abspath(__file__))
    return os.path.join(go_cache_dir(), 'fuzz', import_path_of(here))


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument('--fuzzdir', default=None,
                    help='dir with FuzzXxx/ corpus subdirs '
                         '(default: $GOCACHE/fuzz/<import-path>/, resolved '
                         'via `go env GOCACHE` + the enclosing go.mod)')
    ap.add_argument('--out', required=True, help='diffharness corpus output dir')
    ap.add_argument('--allow-empty', action='store_true',
                    help='exit 0 even if no replay programs are materialized')
    args = ap.parse_args()

    if args.fuzzdir is None:
        try:
            args.fuzzdir = default_fuzzdir()
        except ValueError as e:
            print(f'replay.py: cannot resolve fuzz cache dir: {e}; '
                  f'pass --fuzzdir explicitly', file=sys.stderr)
            return 2
        print(f'using fuzz cache dir {args.fuzzdir}')
    if not os.path.isdir(args.fuzzdir):
        print(f'replay.py: fuzz corpus dir {args.fuzzdir} does not exist',
              file=sys.stderr)
        print('run `go test -fuzz` in the fuzz package first; the corpus '
              'lives in $GOCACHE/fuzz/, not testdata/fuzz/',
              file=sys.stderr)
        return 2

    n = 0
    for target, (imports, argtypes, body) in sorted(TEMPLATES.items()):
        cdir = os.path.join(args.fuzzdir, target)
        if not os.path.isdir(cdir):
            print(f'skip {target}: no corpus dir')
            continue
        for fn in sorted(os.listdir(cdir)):
            if fn.startswith('.'):
                continue
            path = os.path.join(cdir, fn)
            if not os.path.isfile(path):
                continue
            try:
                vals = parse_corpus_file(path)
            except ValueError as e:
                print(f'skip {path}: {e}')
                continue
            if len(vals) != len(argtypes):
                print(f'skip {path}: want {len(argtypes)} args, have {len(vals)}')
                continue
            qargs = {}
            ok = True
            for i, ((typ, val), want) in enumerate(zip(vals, argtypes)):
                if typ != want and not (want == 'int' and typ == 'int64'):
                    print(f'skip {path}: arg {i} type {typ} != {want}')
                    ok = False
                    break
                qargs[f'a{i}'] = render_arg(want, val)
            if not ok:
                continue
            progdir = os.path.join(args.out, f'{target}_{n:04d}')
            os.makedirs(progdir, exist_ok=True)
            implines = '\n'.join(f'\t"{i}"' for i in imports)
            main_go = ('package main\n\nimport (\n\t"fmt"\n%s\n)\n\nfunc main() {\n\t%s\n}\n'
                       % (implines, body.format(**qargs)))
            with open(os.path.join(progdir, 'main.go'), 'w') as f:
                f.write(main_go)
            n += 1
    print(f'materialized {n} replay programs in {args.out}')
    if n == 0 and not args.allow_empty:
        print('replay.py: no replay programs materialized; refusing to run '
              'an empty differential (pass --allow-empty to override)',
              file=sys.stderr)
        return 3
    return 0


if __name__ == '__main__':
    sys.exit(main())
