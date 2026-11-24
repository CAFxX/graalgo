#!/usr/bin/env python3
"""Replay native fuzz corpora differentially (GG-0037(i)).

Reads FuzzXxx/ corpus files (grown by `go test -fuzz` in
$GOCACHE/fuzz/<import path>/; failing inputs in testdata/fuzz/),
materializes each entry as a single-file Go program calling the target
stdlib function, and writes a diffharness corpus directory. The caller
then runs `diffharness run -config backends.json -corpus <dir>` for the
gc-vs-graalgo differential.

Usage: replay.py --fuzzdir DIR [--fuzzdir DIR ...] --out DIR [--max-per-target N]
"""
import argparse
import ast as pyast
import os
import re
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


def go_quote_string(b):
    """Go double-quoted literal holding exactly the bytes b (a Go string is
    a byte sequence: invalid UTF-8 must survive the round trip)."""
    out = ['"']
    for o in b:
        ch = chr(o)
        if ch == '"':
            out.append('\\"')
        elif ch == '\\':
            out.append('\\\\')
        elif 32 <= o < 127:
            out.append(ch)
        else:
            out.append('\\x%02x' % o)
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
            return unquote_go_string(lit)
        inner = lit.strip('{}')
        return bytes(int(x) for x in inner.split(',') if x.strip())
    if typ in ('int', 'int64'):
        return int(lit, 0)
    raise ValueError(f'unsupported corpus type {typ}')


_SIMPLE_ESC = {'a': 7, 'b': 8, 'f': 12, 'n': 10, 'r': 13, 't': 9, 'v': 11,
               '\\': 92, "'": 39, '"': 34}


def unquote_go_string(lit):
    """Bytes of a Go string literal as strconv.Quote writes it (the fuzz
    corpus format): raw UTF-8 runes, \\xNN bytes, \\uNNNN / \\UNNNNNNNN
    runes, octal and the single-letter escapes. (Until 2026-11-23 this went
    through Python's unicode_escape + latin-1, which failed on every entry
    holding a non-Latin-1 rune.)"""
    if len(lit) >= 2 and lit[0] == '`' and lit[-1] == '`':
        return lit[1:-1].encode('utf-8')
    if not (len(lit) >= 2 and lit[0] == '"' and lit[-1] == '"'):
        raise ValueError(f'cannot unquote {lit!r}')
    s, out, i = lit[1:-1], bytearray(), 0
    while i < len(s):
        ch = s[i]
        if ch != '\\':
            out += ch.encode('utf-8')
            i += 1
            continue
        e = s[i + 1]
        if e == 'x':
            out.append(int(s[i + 2:i + 4], 16)); i += 4
        elif e in 'uU':
            n = 4 if e == 'u' else 8
            out += chr(int(s[i + 2:i + 2 + n], 16)).encode('utf-8'); i += 2 + n
        elif e in '01234567':
            out.append(int(s[i + 1:i + 4], 8)); i += 4
        elif e in _SIMPLE_ESC:
            out.append(_SIMPLE_ESC[e]); i += 2
        else:
            raise ValueError(f'bad escape \\{e} in {lit!r}')
    return bytes(out)


def render_arg(typ, val):
    if typ == 'string':
        return go_quote_string(val)
    if typ == '[]byte':
        return go_quote_bytes(val)
    if typ == 'int':
        return str(val)
    raise ValueError(f'unsupported arg type {typ}')


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument('--fuzzdir', required=True, action='append',
                    help='dir holding FuzzXxx/ corpus dirs; repeatable. `go test -fuzz`'
                         ' writes the inputs it grows to $GOCACHE/fuzz/<import path>/'
                         ' (only failing inputs land in testdata/fuzz/), so the driver'
                         ' passes both')
    ap.add_argument('--out', required=True, help='diffharness corpus output dir')
    ap.add_argument('--max-per-target', type=int, default=0,
                    help='bound the replay per target (0: no bound); entries are'
                         ' content-hash named, so the sorted prefix is a stable sample')
    args = ap.parse_args()

    n = 0
    for target, (imports, argtypes, body) in sorted(TEMPLATES.items()):
        paths = []
        for fuzzdir in args.fuzzdir:
            cdir = os.path.join(fuzzdir, target)
            if os.path.isdir(cdir):
                paths += [os.path.join(cdir, fn) for fn in sorted(os.listdir(cdir))
                          if not fn.startswith('.')]
        paths = [p for p in paths if os.path.isfile(p)]
        if not paths:
            print(f'skip {target}: no corpus entries')
            continue
        if args.max_per_target > 0:
            paths = paths[:args.max_per_target]
        for path in paths:
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


if __name__ == '__main__':
    main()
