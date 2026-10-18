#!/usr/bin/env python3
# P3-M9 (tier-5: archive/mime): generator for synthetic archive/tar,
# archive/zip, mime, mime/multipart, mime/quotedprintable packages.
#
# Method: vendor gc 1.27.1 sources and adapt for the graalgo P3 frontend,
# following the gen_synth_compress.py Appendix-M pattern: every multi-result
# func/method keeps its exact gc signature (for type-checking; body is a
# panic stub skipped at synthetic import) and gains a single-result
# __-helper returning a pair struct with the real body. Table entries go
# into m7MultiResultFuncs (mrdesugar_m5.go).
#
# DO NOT EDIT THE GENERATED FILES BY HAND (edit this generator and re-run).
import re
import subprocess
import sys
import os
import tempfile

# --- P1-R1-2 portability prologue: GOROOT and OUTDIR are environment-driven
# so the generator is CI-runnable and never writes outside the repo.
#   GRAALGO_GOROOT   Go installation whose gc 1.27.1 sources are read
#                    (default: `go env GOROOT`).
#   GEN_SYNTH_OUTDIR directory for the generated file(s) (default: this
#                    script's directory, i.e. in-place regeneration).
_HERE = os.path.dirname(os.path.abspath(__file__))

def _graalgo_goroot():
    g = os.environ.get("GRAALGO_GOROOT")
    if g:
        return g
    try:
        return subprocess.run(["go", "env", "GOROOT"], capture_output=True,
                              text=True, check=True).stdout.strip()
    except Exception as e:
        raise SystemExit(
            "gen_synth: cannot determine GOROOT; set GRAALGO_GOROOT (%s)" % e)

OUTDIR = os.environ.get("GEN_SYNTH_OUTDIR", _HERE)

GOROOT = _graalgo_goroot()


# ---------------------------------------------------------------------------
# source surgery helpers (adapted from gen_synth_compress.py)
# ---------------------------------------------------------------------------

def strip_build_tags(src):
    return re.sub(r'^//go:build .*\n', '', src, flags=re.M)

def strip_package(src):
    return re.sub(r'^package \w+\n', '', src, count=1, flags=re.M)

def strip_imports(src):
    src = re.sub(r'^import \(.*?^\)\n', '', src, flags=re.M | re.S)
    src = re.sub(r'^import "\w[^"]*"\n', '', src, flags=re.M)
    src = re.sub(r'^import \(\n\)\n', '', src, flags=re.M)
    return src

def extract_imports(src):
    """Extract import paths from src. Returns set of import paths."""
    imports = set()
    # Match import ("path") or import (\n\t"path"\n)
    for m in re.finditer(r'^import\s+(?:\(\s*(.*?)\s*\)|"([^"]+)")', src, re.M | re.S):
        block, single = m.groups()
        if single:
            imports.add(single)
        elif block:
            for mm in re.finditer(r'"([^"]+)"', block):
                imports.add(mm.group(1))
    return imports

def extract_functions(src, src_path=None):
    """Extract all top-level func declarations. Returns list of dicts.
    
    Uses Go parser for accurate positions via temp file.
    """
    import subprocess
    import tempfile
    
    # Write src to temp file and parse with Go
    with tempfile.NamedTemporaryFile(mode='w', suffix='.go', delete=False) as f:
        # Add package clause for parsing
        f.write("package p\n")
        f.write(src)
        temp_path = f.name
    
    try:
        result = subprocess.run(
            [os.environ.get("GEN_SYNTH_EXTRACT_DECLS",
                      os.path.join(_HERE, "gen_synth_helper", "extract_decls", "extract_decls")),
             temp_path],
            capture_output=True, text=True
        )
        if result.returncode != 0:
            raise Exception(f"extract_decls failed: {result.stderr}")
        
        funcs = []
        # Parse output: FUNC|name|recv|start|end
        # Note: positions are offset by len("package p\n") = 10
        offset = len("package p\n")
        for line in result.stdout.strip().split('\n'):
            if not line.startswith('FUNC|'):
                continue
            parts = line.split('|')
            _, name, recv, start_s, end_s = parts
            start = int(start_s) - offset
            end = int(end_s) - offset
            # Extract the func text
            decl = src[start:end]
            # Parse signature with regex
            sig_pat = re.compile(
                r'^func (\((\w+) (\*?\w+)\) )?(\w+)\((.*?)\)(?: \(((?:[^()]*|\([^()]*\))*)\)| (\S+))? \{\n?',
                re.S
            )
            m = sig_pat.match(decl)
            if not m:
                # Try without trailing newline
                continue
            recv_full, recv_var, recv_type, fname, params, results_paren, results_single = m.groups()
            results = results_paren if results_paren is not None else (results_single or '')
            # Extract body
            body_start = decl.index('{') + 1
            body_end = decl.rindex('}')
            body = decl[body_start:body_end]
            
            funcs.append({
                'decl': decl,
                'recv_var': recv_var,
                'recv_type': recv_type.lstrip('*') if recv_type else None,
                'recv_ptr': recv_type.startswith('*') if recv_type else False,
                'name': fname,
                'params': params,
                'results': results or '',
                'body': body,
                'start': start,
                'end': end,
            })
        # Sort by start position
        funcs.sort(key=lambda f: f['start'])
        return funcs
    finally:
        os.unlink(temp_path)

def extract_type_names(src):
    """Extract defined type names from source (for parse_results heuristic)."""
    # Match: type Name struct, type Name interface, type Name ..., type Name = ...
    names = set()
    for m in re.finditer(r'\btype\s+([a-zA-Z_][a-zA-Z0-9_]*)\b', src):
        names.add(m.group(1))
    return names


def parse_results(results, known_types=None):
    """Parse result clause into list of (name, type). Handles '(a int, b string)', '(int, error)', 'int', '(token, rest string)'.

    known_types: set of package-defined type names (heuristic for `(MyType, error)`).
    """
    if known_types is None:
        known_types = set()
    results = results.strip()
    if not results:
        return []
    # Remove outer parens if present
    if results.startswith('('):
        results = results[1:-1].strip()
    if not results:
        return []
    parts = []
    depth = 0
    cur = ''
    for c in results:
        if c in '([{':
            depth += 1
        elif c in ')]}':
            depth -= 1
        if c == ',' and depth == 0:
            parts.append(cur.strip())
            cur = ''
        else:
            cur += c
    parts.append(cur.strip())
    
    # First pass: parse each part into (names_list, type_or_None)
    # A part like "token, rest string" is split by comma already, so we have ["token", "rest string"]
    # "token" has no type -> shares with next
    # "rest string" -> names=["rest"], type="string"
    parsed_parts = []
    for p in parts:
        p = p.strip()
        if not p:
            continue
        tokens = p.split()
        if len(tokens) >= 2:
            # Check if last token(s) form a type
            # Heuristic: if first token is a known type, then no names
            first = tokens[0]
            if first in ('int', 'int8', 'int16', 'int32', 'int64', 'uint', 'uint8', 'uint16', 'uint32', 'uint64',
                        'float32', 'float64', 'string', 'bool', 'byte', 'rune', 'error', 'any') or \
               first.startswith('*') or first.startswith('[') or first.startswith('map[') or \
               first.startswith('func(') or first.startswith('chan '):
                # No names, just a type (might have spaces like "map[string] int")
                parsed_parts.append(([], p))
            else:
                # Names + type: all but last token are names, last is type
                # But type could have spaces (e.g., "map[string]int" doesn't, but "[]int" doesn't)
                # Simple: last token is type, rest are names
                # Actually for "a, b string" after comma split we get "a" and "b string"
                # For "b string": names=["b"], type="string"
                name = tokens[0]
                typ = ' '.join(tokens[1:])
                parsed_parts.append(([name], typ))
        else:
            # Single token: could be a type, or a name sharing type with next
            tok = tokens[0]
            if tok in ('int', 'int8', 'int16', 'int32', 'int64', 'uint', 'uint8', 'uint16', 'uint32', 'uint64',
                      'float32', 'float64', 'string', 'bool', 'byte', 'rune', 'error', 'any') or \
               tok.startswith('*') or tok.startswith('[') or tok.startswith('map[') or \
               '.' in tok or tok in known_types:
                parsed_parts.append(([], tok))
            else:
                # Name without type - shares with next part
                parsed_parts.append(([tok], None))
    
    # Second pass: resolve None types by looking ahead
    parsed = []
    for i, (names, typ) in enumerate(parsed_parts):
        if typ is None:
            # Find next part with a type
            for j in range(i+1, len(parsed_parts)):
                if parsed_parts[j][1] is not None:
                    typ = parsed_parts[j][1]
                    break
            if typ is None:
                typ = ''  # Should not happen
        if not names:
            # No name, just type
            parsed.append(('', typ))
        else:
            for n in names:
                parsed.append((n, typ))
    return parsed

def is_multi_result(results):
    """Check if results clause has more than one result."""
    parsed = parse_results(results)
    return len(parsed) > 1


def extract_type_names(src):
    """Extract defined type names from source."""
    names = set()
    # Single: type Name ... (at line start, to avoid comments)
    for m in re.finditer(r'^\s*type\s+([a-zA-Z_][a-zA-Z0-9_]*)\b', src, re.MULTILINE):
        if m.group(1) != '(':
            names.add(m.group(1))
    # Block: type ( Name ...; Name ... )
    for m in re.finditer(r'^\s*type\s*\((.*?)\)', src, re.MULTILINE | re.DOTALL):
        block = m.group(1)
        for tm in re.finditer(r'^\s*([a-zA-Z_][a-zA-Z0-9_]*)\b', block, re.MULTILINE):
            names.add(tm.group(1))
    return names


def compute_field_names(results):
    """Compute pair struct field names from parsed results [(name, type)]."""
    field_names = []
    for idx, (rname, rtype) in enumerate(results):
        if not rname:
            rname = f'f{idx}'
        rname = rname.strip()
        if not rname or rname == '_' or not re.match(r'^[a-zA-Z_][a-zA-Z0-9_]*$', rname):
            rname = f'f{idx}'
        field_names.append(rname)
    return field_names


def name_unnamed_params(params):
    """Give names to unnamed params, handling shared types."""
    params = params.strip()
    if not params:
        return params
    parts = []
    depth = 0
    cur = ''
    for ch in params:
        if ch in '([{':
            depth += 1
        elif ch in ')]}':
            depth -= 1
        if ch == ',' and depth == 0:
            parts.append(cur.strip())
            cur = ''
        else:
            cur += ch
    parts.append(cur.strip())
    parsed = []
    for p in parts:
        p = p.strip()
        if not p:
            continue
        tokens = p.split()
        if len(tokens) >= 2:
            first = tokens[0]
            if '.' in first or first.startswith(('*', '[', 'map[', 'func(', 'chan ')) or \
               first in ('int','int8','int16','int32','int64','uint','uint8','uint16','uint32','uint64',
                        'float32','float64','string','bool','byte','rune','error','any'):
                parsed.append(([], p))
            else:
                parsed.append(([tokens[0]], ' '.join(tokens[1:])))
        else:
            tok = tokens[0]
            if '.' in tok or tok.startswith(('*', '[', 'map[')) or \
               tok in ('int','int8','int16','int32','int64','uint','uint8','uint16','uint32','uint64',
                      'float32','float64','string','bool','byte','rune','error','any'):
                parsed.append(([], tok))
            else:
                parsed.append(([tok], None))
    resolved = []
    for i, (names, typ) in enumerate(parsed):
        if typ is None:
            for j in range(i+1, len(parsed)):
                if parsed[j][1] is not None:
                    typ = parsed[j][1]
                    break
        resolved.append((names, typ))
    named = []
    ui = 0
    for names, typ in resolved:
        if not names:
            named.append(f'__p{ui} {typ}')
            ui += 1
        else:
            for nm in names:
                named.append(f'{nm} {typ}')
    return ', '.join(named)


def convert_raw_strings(src):
    """Convert Go raw strings to interpreted strings."""
    out = []
    i, n = 0, len(src)
    while i < n:
        if src.startswith('//', i):
            j = src.find('\n', i)
            if j == -1: j = n
            out.append(src[i:j].replace('`', "'"))
            i = j
            continue
        if src.startswith('/*', i):
            j = src.find('*/', i)
            j = n if j == -1 else j + 2
            out.append(src[i:j].replace('`', "'"))
            i = j
            continue
        if src[i] == '"' or src[i] == "'":
            q = src[i]
            if q == "'" and src.startswith("'`'", i):
                out.append("'\\x60'")
                i += 3
                continue
            j = i + 1
            while j < n:
                if src[j] == '\\':
                    j += 2
                    continue
                if src[j] == q:
                    j += 1
                    break
                j += 1
            out.append(src[i:j])
            i = j
            continue
        if src[i] == '`':
            j = src.find('`', i+1)
            if j == -1:
                out.append("'")
                i += 1
                continue
            content = src[i+1:j]
            esc = []
            for ch in content:
                if ch == '\\': esc.append('\\\\')
                elif ch == '"': esc.append('\\"')
                elif ch == '\n': esc.append('\\n')
                elif ch == '\r': esc.append('\\r')
                elif ch == '\t': esc.append('\\t')
                elif ord(ch) < 0x20 or ord(ch) == 0x7f:
                    esc.append('\\x%02x' % ord(ch))
                else: esc.append(ch)
            out.append('"' + ''.join(esc) + '"')
            i = j + 1
            continue
        out.append(src[i])
        i += 1
    return ''.join(out)


def extract_receiver(body, dot_pos):
    """Extract receiver before '.' at dot_pos."""
    j = dot_pos - 1
    while j >= 0 and body[j] in ' \t':
        j -= 1
    dp = db = dbr = 0
    end = j + 1
    while j >= 0:
        ch = body[j]
        if ch == ')': dp += 1
        elif ch == ']': db += 1
        elif ch == '}': dbr += 1
        elif ch == '(':
            if dp > 0: dp -= 1
            else: break
        elif ch == '[':
            if db > 0: db -= 1
            else: break
        elif ch == '{':
            if dbr > 0: dbr -= 1
            else: break
        elif dp == 0 and db == 0 and dbr == 0:
            if not (ch.isalnum() or ch == '_' or ch == '.'):
                break
        j -= 1
    start = j + 1
    return body[start:end].strip(), start


def rewrite_call_sites(body, mr_map):
    """Rewrite calls to multi-result functions in a helper body."""
    if not mr_map:
        return body
    tmp_counter = [0]
    def get_tmp():
        tmp_counter[0] += 1
        return f"__mrtmp{tmp_counter[0]}"
    # Denylist: common interface methods - receiver type uncertain
    iface_methods = {'Write', 'Read', 'Close', 'Flush', 'Seek', 'ReadAt', 'WriteAt'}
    for orig_name in sorted(mr_map.keys(), key=len, reverse=True):
        if orig_name in iface_methods:
            continue
        helper_name, field_names = mr_map[orig_name]
        nfields = len(field_names)
        out = []
        i = 0
        emit_i = 0
        n = len(body)
        while i < n:
            idx = body.find(orig_name, i)
            if idx == -1:
                out.append(body[emit_i:])
                break
            if idx > 0 and (body[idx-1].isalnum() or body[idx-1] == '_'):
                i = idx + 1
                continue
            line_start = body.rfind('\n', 0, idx) + 1
            line_prefix = body[line_start:idx]
            if re.search(r'\bfunc\s+(\([^)]*\)\s+)?$', line_prefix):
                i = idx + len(orig_name) + 1
                continue
            j = idx + len(orig_name)
            while j < n and body[j] in ' \t':
                j += 1
            if j >= n or body[j] != '(':
                i = j
                continue
            depth = 1
            k = j + 1
            in_str = None
            while k < n and depth > 0:
                c = body[k]
                if in_str:
                    if c == '\\':
                        k += 2
                        continue
                    if c == in_str:
                        in_str = None
                else:
                    if c in ('"', "'", '`'):
                        in_str = c
                    elif c == '(':
                        depth += 1
                    elif c == ')':
                        depth -= 1
                k += 1
            call_end = k
            args = body[j+1:k-1]
            # Receiver handling (conservative: simple identifiers only,
            # not packages, not selectors)
            recv = None
            recv_start = None
            skip_rewrite = False
            rd = idx - 1
            while rd >= 0 and body[rd] in ' \t':
                rd -= 1
            if rd >= 0 and body[rd] == '.':
                rc, rs = extract_receiver(body, rd)
                # Skip if not a simple identifier, or if it's a package name
                pkg_names = {'os', 'io', 'bytes', 'strings', 'fmt', 'errors', 'sort',
                           'strconv', 'unicode', 'path', 'filepath', 'math', 'time',
                           'sync', 'bufio', 'hash', 'crypto', 'encoding'}
                if re.match(r'^[a-zA-Z_][a-zA-Z0-9_]*$', rc) and rc not in pkg_names:
                    recv, recv_start = rc, rs
                else:
                    skip_rewrite = True
            if skip_rewrite:
                out.append(body[emit_i:call_end])
                emit_i = call_end
                i = call_end
                continue
            def helper_call():
                if recv:
                    return f"{helper_name}({recv}, {args})" if args.strip() else f"{helper_name}({recv})"
                return f"{helper_name}({args})"
            line_start = body.rfind('\n', 0, idx) + 1
            prefix = body[line_start:idx].strip()
            if prefix.startswith('return'):
                out.append(body[emit_i:recv_start if recv else idx])
                out.append(helper_call())
                emit_i = call_end
                i = call_end
            elif ':=' in prefix or '=' in prefix:
                if ':=' in prefix:
                    lhs, _ = prefix.rsplit(':=', 1)
                    op = ':='
                else:
                    lhs, _ = prefix.rsplit('=', 1)
                    op = '='
                    if lhs.endswith(('=', '!', '<', '>')):
                        out.append(body[emit_i:call_end])
                        emit_i = call_end
                        i = call_end
                        continue
                lhs = lhs.strip()
                lhs_vars = [v.strip() for v in lhs.split(',')]
                if len(lhs_vars) != nfields:
                    out.append(body[emit_i:call_end])
                    emit_i = call_end
                    i = call_end
                    continue
                tmp = get_tmp()
                fields_access = ', '.join(f"{tmp}.{fn}" for fn in field_names)
                line_text = body[line_start:idx]
                if 'func ' in line_text and '{' in line_text:
                    brace_rel = line_text.rfind('{')
                    brace_abs = line_start + brace_rel
                    eff_prefix = body[brace_abs+1:idx].strip()
                    if ':=' in eff_prefix:
                        eff_lhs, _ = eff_prefix.rsplit(':=', 1)
                        eff_op = ':='
                    else:
                        eff_lhs, _ = eff_prefix.rsplit('=', 1)
                        eff_op = '='
                    eff_lhs = eff_lhs.strip()
                    out.append(body[emit_i:brace_abs+1])
                    out.append(f" {tmp} := {helper_call()}; {eff_lhs} {eff_op} {fields_access}")
                    emit_i = call_end
                    i = call_end
                else:
                    out.append(body[emit_i:line_start])
                    out.append(f"{tmp} := {helper_call()}; {lhs} {op} {fields_access}")
                    emit_i = call_end
                    i = call_end
            else:
                line_prefix_stripped = body[line_start:idx].strip()
                if not line_prefix_stripped:
                    out.append(body[emit_i:call_end])
                    emit_i = call_end
                    i = call_end
                else:
                    out.append(body[emit_i:recv_start if recv else idx])
                    out.append(helper_call())
                    emit_i = call_end
                    i = call_end
        body = ''.join(out)
    return body

def make_pair_name(pkg_prefix, recv_type, name):
    """Generate pair struct name: __<prefix><Recv><Name>Pair"""
    recv_part = recv_type if recv_type else ''
    # Capitalize first letter of recv and name for Go export style (but keep __ prefix)
    return f"__{pkg_prefix}{recv_part}{name}Pair"

def make_helper_name(pkg_prefix, recv_type, name):
    """Generate helper func name: __<prefix><Recv><Name>"""
    recv_part = recv_type if recv_type else ''
    return f"__{pkg_prefix}{recv_part}{name}"

def rewrite_body_returns(body, pair_name, fields, mr_map=None, pkg_prefix=''):
    """Rewrite return statements to return pair struct.

    - Skips function literals (their returns belong to the literal).
    - Decomposes `return f(args)` for multi-result f.
    """
    fnames = [f[0] for f in fields]
    tmp_counter = [0]
    def get_tmp():
        tmp_counter[0] += 1
        return f"__mrtmp{tmp_counter[0]}"

    out = []
    i, n = 0, len(body)

    def skip_string(j, q):
        j += 1
        while j < n:
            if q != '`' and body[j] == '\\':
                j += 2
                continue
            if body[j] == q:
                return j + 1
            j += 1
        return j

    def skip_balanced(j, op, cl):
        depth = 0
        while j < n:
            c = body[j]
            if c in ('"', "'", '`'):
                j = skip_string(j, c)
                continue
            if body.startswith('//', j):
                k = body.find('\n', j)
                j = n if k < 0 else k + 1
                continue
            if body.startswith('/*', j):
                k = body.find('*/', j)
                j = n if k < 0 else k + 2
                continue
            if c == op:
                depth += 1
            elif c == cl:
                depth -= 1
                if depth == 0:
                    return j + 1
            j += 1
        return j

    while i < n:
        if body.startswith('//', i):
            k = body.find('\n', i)
            if k == -1:
                out.append(body[i:])
                break
            out.append(body[i:k+1])
            i = k + 1
            continue
        if body.startswith('/*', i):
            k = body.find('*/', i)
            if k == -1:
                out.append(body[i:])
                break
            out.append(body[i:k+2])
            i = k + 2
            continue
        # Skip function literals
        fm = re.match(r'\bfunc\b', body[i:])
        if fm:
            j = i + 4
            while j < n:
                c = body[j]
                if c in ('"', "'", '`'):
                    j = skip_string(j, c)
                    continue
                if body.startswith('//', j):
                    k = body.find('\n', j)
                    j = n if k < 0 else k + 1
                    continue
                if body.startswith('/*', j):
                    k = body.find('*/', j)
                    j = n if k < 0 else k + 2
                    continue
                if c == '(' or c == '[':
                    j = skip_balanced(j, c, ')' if c == '(' else ']')
                    continue
                if c == '{':
                    break
                j += 1
            if j < n and body[j] == '{':
                j = skip_balanced(j, '{', '}')
                out.append(body[i:j])
                i = j
                continue
        m = re.match(r'\breturn\b', body[i:])
        if not m:
            c = body[i]
            if c in ('"', "'", '`'):
                j = skip_string(i, c)
                out.append(body[i:j])
                i = j
            else:
                out.append(c)
                i += 1
            continue
        j = i + 6
        while j < n and body[j] in ' \t':
            j += 1
        if j >= n or body[j] in '\n;}':
            out.append(f'return {pair_name}{{{", ".join(fnames)}}}')
            i = j
            continue
        k = j
        depth = 0
        while k < n:
            c = body[k]
            if c in ('"', "'", '`'):
                k = skip_string(k, c)
                continue
            if body.startswith('//', k):
                break
            if c in '([{':
                depth += 1
            elif c in ')]}':
                if depth == 0:
                    break
                depth -= 1
            elif c in '\n;' and depth == 0:
                break
            k += 1
        ret_vals = body[j:k].strip()
        vals = []
        d = 0
        cur = ''
        for c in ret_vals:
            if c in '([{':
                d += 1
            elif c in ')]}':
                d -= 1
            if c == ',' and d == 0:
                vals.append(cur.strip())
                cur = ''
            else:
                cur += c
        vals.append(cur.strip())
        # Decompose multi-result calls
        decomposed = False
        if len(vals) == 1:
            v = vals[0]
            cm = re.match(r'^([\w.]+)\s*\(', v)
            if cm:
                dotted = cm.group(1)
                fname = dotted.split('.')[-1]
                pstart = v.find('(')
                paren_depth = 0
                pend = -1
                qpos = pstart
                in_q = None
                while qpos < len(v):
                    qc = v[qpos]
                    if in_q:
                        if qc == '\\':
                            qpos += 2
                            continue
                        if qc == in_q:
                            in_q = None
                    elif qc in ('"', "'"):
                        in_q = qc
                    elif qc == '(':
                        paren_depth += 1
                    elif qc == ')':
                        paren_depth -= 1
                        if paren_depth == 0:
                            pend = qpos
                            break
                    qpos += 1
                if pend != -1 and v[pend+1:].strip() == '':
                    args = v[pstart+1:pend]
                    call_text = v[:pend+1]
                    if mr_map and fname in mr_map:
                        helper_name, callee_fields = mr_map[fname]
                        recv_prefix = ''
                        if '.' in dotted:
                            recv_expr = dotted[:dotted.rfind('.')]
                            recv_prefix = recv_expr + ', ' if args.strip() else recv_expr
                        tmp = get_tmp()
                        fields_access = ', '.join(f"{tmp}.{fn}" for fn in callee_fields)
                        out.append(f'{tmp} := {helper_name}({recv_prefix}{args}); return {pair_name}{{{fields_access}}}')
                        decomposed = True
                    elif len(fnames) > 1:
                        tmp = get_tmp()
                        var_names = [f"{tmp}_{fn}" for fn in fnames]
                        assigns = ', '.join(f"{fn}: {vn}" for fn, vn in zip(fnames, var_names))
                        out.append(f'{", ".join(var_names)} := {call_text}; return {pair_name}{{{assigns}}}')
                        decomposed = True
        if decomposed:
            i = k
            continue
        if len(vals) == len(fnames):
            assigns = [f"{fn}: {v}" for fn, v in zip(fnames, vals)]
            out.append(f'return {pair_name}{{{", ".join(assigns)}}}')
        else:
            out.append(f'return {pair_name}{{{ret_vals}}}')
        i = k
    return ''.join(out)

# ---------------------------------------------------------------------------
# Package definitions
# ---------------------------------------------------------------------------

PACKAGES = {
    'archive/tar': {
        'files': [
            'archive/tar/common.go',
            'archive/tar/format.go',
            'archive/tar/reader.go',
            'archive/tar/writer.go',
            'archive/tar/strconv.go',
            # Skip stat_unix.go, stat_actime*.go (OS-specific, not needed for corpus)
        ],
        'const_name': 'syntheticArchiveTarSource',
        'pkg_prefix': 'tar',
        'adapt': 'tar_adapt',
    },
    'archive/zip': {
        'files': [
            'archive/zip/reader.go',
            'archive/zip/writer.go',
            'archive/zip/struct.go',
            'archive/zip/register.go',
        ],
        'const_name': 'syntheticArchiveZipSource',
        'pkg_prefix': 'zip',
        'adapt': 'zip_adapt',
    },
    'mime': {
        'files': [
            'mime/mediatype.go',
            'mime/encodedword.go',
            'mime/grammar.go',
            # Skip type.go and type_*.go (OS-specific MIME type database, not needed for corpus)
        ],
        'const_name': 'syntheticMimeSource',
        'pkg_prefix': 'mime',
        'adapt': 'mime_adapt',
    },
    'mime/multipart': {
        'files': [
            'mime/multipart/multipart.go',
            'mime/multipart/formdata.go',
            'mime/multipart/writer.go',
            # Skip readmimeheader.go (check)
        ],
        'const_name': 'syntheticMimeMultipartSource',
        'pkg_prefix': 'mp',
        'adapt': 'multipart_adapt',
    },
    'mime/quotedprintable': {
        'files': [
            'mime/quotedprintable/reader.go',
            'mime/quotedprintable/writer.go',
        ],
        'const_name': 'syntheticMimeQuotedprintableSource',
        'pkg_prefix': 'qp',
        'adapt': 'qp_adapt',
    },
}

def tar_adapt(src, pkg_prefix):
    """Adapt tar source: stub godebug, inline IsLocal."""
    # Stub internal/godebug
    src = src.replace(
        'var tarinsecurepath = godebug.New("tarinsecurepath")',
        '''type __tarGodebugSetting struct{ name string }
func (s *__tarGodebugSetting) Value() string { return "" }
func (s *__tarGodebugSetting) IncNonDefault() {}
var tarinsecurepath = &__tarGodebugSetting{name: "tarinsecurepath"}'''
    )
    # Inline filepath.IsLocal (from internal/filepathlite, unix)
    # Replace filepath.IsLocal with __tarIsLocal, and add the function
    src = src.replace('filepath.IsLocal(', '__tarIsLocal(')
    # Add __tarIsLocal function (simplified unix version)
    # Actually, let's use a simple implementation based on path/filepath docs:
    # IsLocal reports whether path is local: not empty, not absolute, no .. elements, etc.
    islocal_fn = '''
// __tarIsLocal is a vendored copy of path/filepath.IsLocal (unix).
// Reports whether path is local: empty is false, absolute is false,
// any ".." element is false, etc. Purely lexical.
func __tarIsLocal(path string) bool {
\tif path == "" {
\t\treturn false
\t}
\t// Unix: absolute paths start with /
\tif path[0] == '/' {
\t\treturn false
\t}
\t// Check for .. elements
\tfor _, elem := range __tarSplitPath(path) {
\t\tif elem == ".." {
\t\t\treturn false
\t\t}
\t}
\treturn true
}

func __tarSplitPath(path string) []string {
\tvar elems []string
\tstart := 0
\tfor i := 0; i <= len(path); i++ {
\t\tif i == len(path) || path[i] == '/' {
\t\t\tif i > start {
\t\t\t\telems = append(elems, path[start:i])
\t\t\t}
\t\t\tstart = i + 1
\t\t}
\t}
\treturn elems
}
'''
    # Insert after imports (find first func/type/var declaration)
    # Simpler: prepend to src
    src = islocal_fn + src
    return src

def zip_adapt(src, pkg_prefix):
    """Adapt zip source: stub godebug."""
    src = src.replace(
        'var zipinsecurepath = godebug.New("zipinsecurepath")',
        '''type __zipGodebugSetting struct{ name string }
func (s *__zipGodebugSetting) Value() string { return "" }
func (s *__zipGodebugSetting) IncNonDefault() {}
var zipinsecurepath = &__zipGodebugSetting{name: "zipinsecurepath"}'''
    )
    # Remove unused var err from __zipreadDirectoryEnd (shadowed by :=)
    src = re.sub(
        r'(func __zipreadDirectoryEnd\([^)]*\) __zipreadDirectoryEndPair \{\n\tvar baseOffset int64\n)\tvar err error\n',
        r'\1',
        src
    )
    return src

def mime_adapt(src, pkg_prefix):
    """Adapt mime source: no special handling needed."""
    return src

def multipart_adapt(src, pkg_prefix):
    """Adapt multipart source: stub godebug and textproto."""
    src = src.replace(
        'var multipartmaxheaders = godebug.New("multipartmaxheaders")',
        '''type __mpGodebugSetting struct{ name string }
func (s *__mpGodebugSetting) Value() string { return "" }
func (s *__mpGodebugSetting) IncNonDefault() {}
var multipartmaxheaders = &__mpGodebugSetting{name: "multipartmaxheaders"}'''
    )
    # Stub godebug vars from formdata.go (PKG-076: generator now includes formdata.go)
    src = src.replace(
        '\tmultipartfiles    = godebug.New("#multipartfiles") // TODO: document and remove #\n\tmultipartmaxparts = godebug.New("multipartmaxparts")',
        '\tmultipartfiles    = &__mpGodebugSetting{name: "#multipartfiles"}\n\tmultipartmaxparts = &__mpGodebugSetting{name: "multipartmaxparts"}'
    )
    src = src.replace('textproto.MIMEHeader', '__mpMIMEHeader')
    src += '''
type __mpMIMEHeader map[string][]string
func (__mpMIMEHeader) Get(key string) string { return "" }
func (__mpMIMEHeader) Del(key string) {}
func (h __mpMIMEHeader) Set(key, value string) { h[key] = []string{value} }
type __mpTextprotoReader struct{}
func __mpNewTextprotoReader(r any) *__mpTextprotoReader { return &__mpTextprotoReader{} }
func readMIMEHeader(r *__mpTextprotoReader, maxMemory, maxHeaders int64) (__mpMIMEHeader, error) {
    panic("multipart.readMIMEHeader: not in M9 scope")
}
'''
    src = src.replace('textproto.NewReader(', '__mpNewTextprotoReader(')
    # Add ErrMessageTooLarge (from skipped formdata.go)
    if 'ErrMessageTooLarge' in src and 'var ErrMessageTooLarge' not in src:
        src += '''
// ErrMessageTooLarge is returned by ReadForm if the message is too large.
var ErrMessageTooLarge = errors.New("multipart: message too large")
'''
    return src

def qp_adapt(src, pkg_prefix):
    """Adapt quotedprintable source."""
    return src

def process_package(pkg_path, cfg):
    """Process a package and return (go_const_code, table_entries)."""
    pkg_prefix = cfg['pkg_prefix']
    const_name = cfg['const_name']
    adapt_fn = globals()[cfg['adapt']]
    
    combined_src = []
    table_entries = []
    seen_funcs = set()  # Deduplicate function names across files
    all_imports = set()

    # Pre-pass: collect type names and build mr_map
    known_types = set()
    for rel_file in cfg['files']:
        full_path = os.path.join(GOROOT, 'src', rel_file)
        with open(full_path, 'r') as f:
            tsrc = f.read()
        known_types.update(extract_type_names(tsrc))
    mr_map = {}
    pre_seen = set()
    name_counts = {}
    # First count method names to detect ambiguity
    for rel_file in cfg['files']:
        full_path = os.path.join(GOROOT, 'src', rel_file)
        with open(full_path, 'r') as f:
            tsrc = f.read()
        tsrc = strip_build_tags(tsrc)
        tsrc = strip_package(tsrc)
        tsrc = strip_imports(tsrc)
        for fn in extract_functions(tsrc):
            if is_multi_result(fn['results']):
                name_counts[fn['name']] = name_counts.get(fn['name'], 0) + 1
    for rel_file in cfg['files']:
        full_path = os.path.join(GOROOT, 'src', rel_file)
        with open(full_path, 'r') as f:
            tsrc = f.read()
        tsrc = strip_build_tags(tsrc)
        tsrc = strip_package(tsrc)
        tsrc = strip_imports(tsrc)
        for fn in extract_functions(tsrc):
            fk = (fn['recv_type'], fn['name'])
            if fk in pre_seen:
                continue
            pre_seen.add(fk)
            if is_multi_result(fn['results']):
                # Skip ambiguous names (multiple types define same method)
                if name_counts.get(fn['name'], 0) > 1:
                    continue
                pr = parse_results(fn['results'], known_types)
                hn = make_helper_name(pkg_prefix, fn['recv_type'], fn['name'])
                mr_map[fn['name']] = (hn, compute_field_names(pr))

    for rel_file in cfg['files']:
        full_path = os.path.join(GOROOT, 'src', rel_file)
        with open(full_path, 'r') as f:
            src = f.read()
        
        src = strip_build_tags(src)
        src = strip_package(src)
        # Collect imports before stripping
        all_imports.update(extract_imports(src))
        src = strip_imports(src)
        
        # Extract functions
        funcs = extract_functions(src)
        
        # Process each function
        new_src_parts = []
        last_end = 0
        
        for fn in funcs:
            # Copy text before this function
            new_src_parts.append(src[last_end:fn['start']])
            
            # Deduplicate: skip if we've already emitted a function with this (recv, name)
            func_key = (fn['recv_type'], fn['name'])
            if func_key in seen_funcs:
                last_end = fn['end']
                continue
            seen_funcs.add(func_key)
            
            if is_multi_result(fn['results']):
                # Generate Appendix M pattern
                results = parse_results(fn['results'], known_types)
                pair_name = make_pair_name(pkg_prefix, fn['recv_type'], fn['name'])
                helper_name = make_helper_name(pkg_prefix, fn['recv_type'], fn['name'])

                # Generate pair struct
                field_names = compute_field_names(results)
                field_defs = [f'\t{rname} {rtype}' for rname, (_, rtype) in zip(field_names, results)]
                
                pair_struct = f"type {pair_name} struct {{\n" + "\n".join(field_defs) + "\n}\n\n"
                new_src_parts.append(pair_struct)
                
                # Generate helper with real body (rewritten returns)
                # Build helper signature: receiver becomes first param (not a method)
                helper_params = name_unnamed_params(fn['params'])
                if fn['recv_type']:
                    ptr = "*" if fn['recv_ptr'] else ""
                    recv_param = f"{fn['recv_var']} {ptr}{fn['recv_type']}"
                    if helper_params.strip():
                        all_params = f"{recv_param}, {helper_params}"
                    else:
                        all_params = recv_param
                else:
                    all_params = helper_params
                
                helper_sig = f"func {helper_name}({all_params}) {pair_name} {{\n"
                # Rewrite body returns
                rewritten_body = rewrite_body_returns(fn['body'], pair_name, list(zip(field_names, [r[1] for r in results])), mr_map=mr_map, pkg_prefix=pkg_prefix)
                # For named results, declare local variables if needed.
                # Heuristic: bare return, or assigned (with =), compound assign,
                # inc/dec, or multi-assign. Over-declaring causes "declared and
                # not used" which is easier to spot than "undefined".
                decls = ""
                has_bare = bool(re.search(r'\breturn\b\s*(\n|;|})', fn['body']))
                for (rname, rtype), fname in zip(results, field_names):
                    if rname and rname.strip():
                        rn = re.escape(rname.strip())
                        assigned = bool(re.search(rf'\b{rn}\b\s*=\s*[^=]', fn['body']))
                        compound = bool(re.search(rf'\b{rn}\b\s*[+\-*/%&|^]=', fn['body']))
                        incdec = bool(re.search(rf'\b{rn}\b\s*(\+\+|--)', fn['body']))
                        multi_assign = bool(re.search(rf'\b{rn}\b\s*,', fn['body']))
                        if has_bare or assigned or compound or incdec or multi_assign:
                            decls += f"\tvar {rname} {rtype}\n"
                helper_decl = helper_sig + decls + rewritten_body + "}\n\n"
                new_src_parts.append(helper_decl)
                
                # Original function with panic body (for type-checking)
                orig_sig = f"func "
                if fn['recv_type']:
                    ptr = "*" if fn['recv_ptr'] else ""
                    orig_sig += f"({fn['recv_var']} {ptr}{fn['recv_type']}) "
                orig_sig += f"{fn['name']}({fn['params']})"
                if fn['results']:
                    # Preserve original result clause
                    if '(' in fn['results'] or ' ' in fn['results'].strip():
                        orig_sig += f" ({fn['results']})"
                    else:
                        orig_sig += f" {fn['results']}"
                orig_sig += " {\n"
                orig_sig += f'\tpanic("{pkg_path}.{fn["name"]}: not in M7 scope (multi-result calls need WS D; re-test when it lands)")\n'
                orig_sig += "}\n\n"
                new_src_parts.append(orig_sig)
                
                # Table entry
                # Format: {"pkg", "Recv.Method" or "Func", "Helper", "Pair", []string{"f0", "f1"}}
                if fn['recv_type']:
                    method_path = f"{fn['recv_type']}.{fn['name']}"
                else:
                    method_path = fn['name']
                table_entries.append((pkg_path, method_path, helper_name, pair_name, field_names))
            else:
                # Single-result: keep as-is
                new_src_parts.append(fn['decl'])
                new_src_parts.append("\n\n")
            
            last_end = fn['end']
        
        new_src_parts.append(src[last_end:])
        file_src = ''.join(new_src_parts)
        combined_src.append(file_src)
    
    full_src = '\n'.join(combined_src)
    full_src = adapt_fn(full_src, pkg_prefix)
    
    # Rewrite call sites using pre-pass mr_map
    if mr_map:
        full_src = rewrite_call_sites(full_src, mr_map)
    
    # Prepend collected imports (exclude internal/ which we stub).
    # Only include imports actually referenced in code (not comments).
    # Strip comments first to avoid false positives.
    src_no_comments = re.sub(r'//.*', '', full_src)
    src_no_comments = re.sub(r'/\*.*?\*/', '', src_no_comments, flags=re.DOTALL)
    filtered_imports = []
    for imp in sorted(all_imports):
        if imp.startswith('internal/'):
            continue
        pkg = imp.split('/')[-1]
        if re.search(rf'\b{re.escape(pkg)}\.', src_no_comments):
            filtered_imports.append(imp)
    if filtered_imports:
        import_block = 'import (\n' + '\n'.join(f'\t"{imp}"' for imp in filtered_imports) + '\n)\n\n'
        full_src = import_block + full_src
    
    # Handle backticks: convert raw strings to interpreted strings
    full_src = convert_raw_strings(full_src)
    if '`' in full_src:
        print(f"  WARNING: leftover backticks in {pkg_path}", file=sys.stderr)
    
    go_code = f'''// SYNTHETIC (Principle 4) — {pkg_path}: vendored verbatim; the real GOROOT tree does not compile under graalgo (go:linkname / asm / intrinsics / unsafe in the source or its closure — B-4).
// RETIRES WHEN: graalgo compiles the real go1.27.1 GOROOT sources directly — then delete this file.
// RE-TEST TRIGGER: real-source compilation lands (B-4 phased removal: docs/arch/bridge-inventory.md).
// P3-M9 (tier-5): synthetic `{pkg_path}` package.
//
// REAL Go source, embedded as text and type-checked on demand with the
// overlay importer (same mechanism as the P3-M7 synthetic math package,
// ADR-0007 D5).
//
// Generated by gen_synth_archive_mime.py from gc 1.27.1; DO NOT EDIT BY
// HAND (edit the generator and re-run).
package main

// {const_name} is the P3-M9 synthetic `{pkg_path}` package.
const {const_name} = `package {pkg_path.split('/')[0] if '/' not in pkg_path else pkg_path}

{full_src}
`
'''
    # Fix package name (should be the last component)
    pkg_name = pkg_path.split('/')[-1]
    go_code = go_code.replace(f'`package {pkg_path.split("/")[0] if "/" not in pkg_path else pkg_path}\n',
                              f'`package {pkg_name}\n')
    
    return go_code, table_entries

def main():
    all_entries = []
    for pkg_path, cfg in PACKAGES.items():
        print(f"Processing {pkg_path}...", file=sys.stderr)
        go_code, table_entries = process_package(pkg_path, cfg)
        
        # Write the Go file
        # Filename: synth_<pkg with _>.go
        fname = 'synth_' + pkg_path.replace('/', '_') + '.go'
        out_path = os.path.join(OUTDIR, fname)
        with open(out_path, 'w') as f:
            f.write(go_code)
        print(f"  Wrote {out_path}", file=sys.stderr)
        
        # Collect table entries
        for pkg, method, helper, pair, fields in table_entries:
            fields_str = ', '.join(f'"{f}"' for f in fields)
            # Split method into recv and name if it contains a dot
            if '.' in method:
                recv, mname = method.split('.', 1)
                all_entries.append(f'    {{"{pkg}", "{mname}", "{recv}", "{helper}", "{pair}", []string{{{fields_str}}}}},')
            else:
                all_entries.append(f'    {{"{pkg}", "{method}", "", "{helper}", "{pair}", []string{{{fields_str}}}}},')
    
    # Write table entries to a file for manual insertion
    entries_path = os.path.join(os.environ.get("GEN_SYNTH_TMPDIR", tempfile.gettempdir()),
                                     'archive_mime_table_entries.txt')
    with open(entries_path, 'w') as f:
        f.write('\n'.join(all_entries) + '\n')
    print(f"Wrote {len(all_entries)} table entries to {entries_path}", file=sys.stderr)

if __name__ == '__main__':
    main()
