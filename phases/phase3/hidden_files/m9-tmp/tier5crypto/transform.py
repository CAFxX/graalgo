#!/usr/bin/env python3
"""Appendix-M transform for P3-M9 tier-5 crypto synthetic sources.

For each multi-result func/method in the input (from mrscan3 JSON):
  type __mr<Key>Pair struct { <f0> <T0>; ... }
  func __mr<Key>[<typeparams>](<recv,> <params>) __mr<Key>Pair { <body, returns rewritten> }
  <original signature> { panic("<pkgpath>: <name>: multi-result desugar") }

Body rewrites (line-based, like math/big's mr_apply.py):
  - `return a, b`        -> `return __mr<Key>Pair{a, b}`
  - bare `return`        -> `return __mr<Key>Pair{<result names>}`
  - forwarding `return f(args)` (single call expr) -> REPORTED for manual fix
Named result variables used in the body are declared as locals (math/big
precedent).

Emits <out.go> and <out.table> (m8MultiResultFuncs entries).

Usage: transform.py <merged.go> <pkgpath> <members.json> <out.go> <out.table>
"""
import json, re, sys

def split_top(s):
    depth = 0; parts = []; cur = ''
    for ch in s:
        if ch in '([': depth += 1
        elif ch in ')]': depth -= 1
        if ch == ',' and depth == 0:
            parts.append(cur); cur = ''
        else: cur += ch
    parts.append(cur)
    return parts

def strip_comment(line):
    in_str = in_ch = esc = False
    for i, ch in enumerate(line):
        if esc: esc = False; continue
        if ch == '\\': esc = True; continue
        if ch == '"' and not in_ch: in_str = not in_str; continue
        if ch == "'" and not in_str: in_ch = not in_ch; continue
        if ch == '/' and not in_str and not in_ch and i+1 < len(line) and line[i+1] == '/':
            return line[:i].rstrip(), ' ' + line[i:]
    return line, ''

def sanitize(s):
    return re.sub(r'[^A-Za-z0-9_]', '_', s)

def main():
    merged_path, pkgpath, members_path, out_path, table_path = sys.argv[1:6]
    src = open(merged_path).read()
    members = json.load(open(members_path))
    # process in reverse order of position
    members.sort(key=lambda m: m['start'], reverse=True)

    table = []
    forwards_needed = []
    # track generated pair names for collision detection
    seen_keys = set()

    for m in members:
        recv, name = m['recv'], m['name']
        key = (sanitize(recv) + '_' if recv else '') + sanitize(name)
        if key in seen_keys:
            print(f"KEY COLLISION: {key}", file=sys.stderr); sys.exit(1)
        seen_keys.add(key)
        helper = '__mr' + key
        pair = helper + 'Pair'
        fields = [r['name'] for r in m['results']]
        rtypes = [r['type'] for r in m['results']]

        start, end, header_end = m['start'], m['end'], m['headerEnd']
        header = src[start:header_end].strip()
        body = src[header_end+1:end-1]

        # --- build helper signature ---
        # header is like: func (c *nistCurve[Point]) ScalarMult(x1, ...) (*big.Int, *big.Int)
        # or: func GenerateKey[P Point[P]](c Curve, r io.Reader) (*PrivateKey, error)
        # or: func Key[H hash.Hash](h func() H, ...) ([]byte, error)
        hm = re.match(r'func\s*(\(.*?\))?\s*([A-Za-z0-9_]+)(\[[^\]]*\])?\s*(\(.*\))\s*', header, re.S)
        assert hm, f"cannot parse header: {header[:80]}"
        recv_part, fname, tparams, params = hm.group(1), hm.group(2), hm.group(3) or '', hm.group(4)
        assert fname == name, f"name mismatch {fname} vs {name}"
        if recv:
            # recv_part like "(c *nistCurve[Point])" -> keep as first param
            rvar_rtyp = recv_part.strip()[1:-1].strip()
            helper_sig = f'func {helper}{tparams}({rvar_rtyp}, {params[1:-1]}) {pair}'
        else:
            helper_sig = f'func {helper}{tparams}{params} {pair}'
        stub_sig = header

        # --- rewrite returns in body ---
        lines = []
        for line in body.split('\n'):
            code, comment = strip_comment(line)
            s = code.strip()
            if s.startswith('return') and (len(s) == 6 or not s[6].isalnum() and s[6] != '_'):
                rest = s[len('return'):].strip()
                indent = code[:len(code)-len(code.lstrip())]
                if rest.startswith(pair + '{'):
                    lines.append(line); continue
                if rest == '':
                    lines.append(f'{indent}return {pair}{{{", ".join(fields)}}}{comment}')
                else:
                    exprs = split_top(rest)
                    if len(exprs) >= 2:
                        # check for forwarding: single call expr with >=2 results?
                        # `return a, b` has >=2 exprs; `return f(x)` has 1.
                        lines.append(f'{indent}return {pair}{{{", ".join(e.strip() for e in exprs)}}}{comment}')
                    elif len(exprs) == 1 and '(' in exprs[0]:
                        # forwarding return f(args) — needs manual fix
                        forwards_needed.append((pkgpath, recv, name, exprs[0].strip()))
                        lines.append(line)
                    else:
                        lines.append(line)
            else:
                lines.append(line)
        new_body = '\n'.join(lines)

        # --- declare named-result locals used in body ---
        body_nc = re.sub(r'//[^\n]*', '', new_body)
        body_nc = re.sub(r'/\*.*?\*/', '', body_nc, flags=re.S)
        var_decls = []
        for r in m['results']:
            nm = r['name']
            if re.search(r'(?<!\.)\b%s\b' % re.escape(nm), body_nc):
                var_decls.append(f'\tvar {nm} {r["type"]}')
        if var_decls:
            new_body = '\n' + '\n'.join(var_decls) + '\n' + new_body

        pair_decl = 'type %s struct {\n%s\n}' % (pair, '\n'.join('\t%s %s' % (r['name'], r['type']) for r in m['results']))
        helper_decl = '%s {%s}' % (helper_sig, new_body)
        stub_decl = '%s {\n\tpanic("%s: %s: multi-result desugar")\n}' % (stub_sig, pkgpath, name)
        replacement = pair_decl + '\n\n' + helper_decl + '\n\n' + stub_decl
        src = src[:start] + replacement + src[end:]

        table.append('{"%s", "%s", "%s", "%s", "", "%s", []string{%s}},' % (
            pkgpath, name, recv, helper, pair,
            ', '.join('"%s"' % f for f in fields)))

    open(out_path, 'w').write(src)
    open(table_path, 'w').write('\n'.join(sorted(table)) + '\n')
    print(f'wrote {out_path} ({len(members)} members), {table_path} ({len(table)} entries)')
    if forwards_needed:
        print(f'FORWARDING RETURNS needing manual fix ({len(forwards_needed)}):')
        for pkg, recv, name, expr in forwards_needed:
            print(f'  {pkg} {recv}.{name}: return {expr[:70]}')

if __name__ == '__main__':
    main()
