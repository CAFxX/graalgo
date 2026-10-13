#!/usr/bin/env python3
"""Replay native fuzz corpora differentially (GG-0037(i)).

Reads testdata/fuzz/FuzzXxx/ corpus files (grown by `go test -fuzz`),
materializes each entry as a single-file Go program calling the target
stdlib function, and writes a diffharness corpus directory. The caller
then runs `diffharness run -config backends.json -corpus <dir>` for the
gc-vs-graalgo differential.

Usage: replay.py --fuzzdir DIR --out DIR
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
    # Use Python's literal parsing after translating Go escapes.
    if len(lit) >= 2 and lit[0] == '"' and lit[-1] == '"':
        inner = lit[1:-1]
        # Translate Go-specific escapes Python doesn't know.
        inner = inner.replace('\\x', '\\x')  # same syntax
        return bytes(inner, 'utf-8').decode('unicode_escape').encode('latin1').decode('latin1')
    if len(lit) >= 2 and lit[0] == '`' and lit[-1] == '`':
        return lit[1:-1]
    raise ValueError(f'cannot unquote {lit!r}')


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
    ap.add_argument('--fuzzdir', required=True, help='dir with testdata/fuzz/FuzzXxx/')
    ap.add_argument('--out', required=True, help='diffharness corpus output dir')
    args = ap.parse_args()

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


if __name__ == '__main__':
    main()
