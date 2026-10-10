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
import sys
import os

GOROOT = "/home/hatch/tools/go-1.27.1"
OUTDIR = "/home/hatch/workspace/graalgo-m9/phases/phase1/frontend"

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
            ['/tmp/extract_decls', temp_path],
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
    for m in re.finditer(r'\btype\s+([a-zA-Z_][a-zA-Z0-9_]*)\b', src):
        names.add(m.group(1))
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
    """
    Rewrite calls to multi-result functions in a helper body.
    mr_map: dict {orig_name: (helper_name, [field_names])}
    
    Handles:
    - `a, b := f(args)` -> `__tmpN := __f(args); a, b := __tmpN.f0, __tmpN.f1`
    - `a, b = f(args)` -> `__tmpN := __f(args); a, b = __tmpN.f0, __tmpN.f1`
    - `return f(args)` -> `return __f(args)` (handled by rewrite_body_returns, but also here for safety)
    """
    if not mr_map:
        return body
    
    tmp_counter = [0]
    
    def get_tmp():
        tmp_counter[0] += 1
        return f"__mrtmp{tmp_counter[0]}"
    
    # For each multi-result function, rewrite its call sites
    # We need to be careful about order: longer names first to avoid partial matches
    for orig_name in sorted(mr_map.keys(), key=len, reverse=True):
        helper_name, field_names = mr_map[orig_name]
        nfields = len(field_names)
        
        # Pattern 1: `a, b, ... := f(args)` or `a, b, ... = f(args)`
        # Match: (lhs) (:=|=) f( (args) )
        # LHS can be: `a, b`, `a, b, c`, `_`, etc.
        # This is tricky with regex because args can contain parens.
        # Simplified: match `f(` then find matching `)` via counting.
        
        # We'll do a manual scan
        out = []
        i = 0
        n = len(body)
        while i < n:
            # Look for orig_name followed by '('
            # Ensure it's not part of a longer identifier
            idx = body.find(orig_name, i)
            if idx == -1:
                out.append(body[i:])
                break
            
            # Check boundaries: skip if part of longer identifier or function definition
            if idx > 0 and (body[idx-1].isalnum() or body[idx-1] == '_'):
                out.append(body[i:idx+1])
                i = idx + 1
                continue
            # Skip function definitions: "func Name(" or "func (recv) Name("
            # Look back for "func " on the same line
            line_start = body.rfind('\n', 0, idx) + 1
            line_prefix = body[line_start:idx]
            if re.search(r'\bfunc\s+(\([^)]*\)\s+)?$', line_prefix):
                out.append(body[i:idx+len(orig_name)+1])
                i = idx + len(orig_name) + 1
                continue
            
            j = idx + len(orig_name)
            # Skip whitespace
            while j < n and body[j] in ' \t':
                j += 1
            if j >= n or body[j] != '(':
                out.append(body[i:j])
                i = j
                continue
            
            # Found a call: orig_name(
            # Find the matching )
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
            # k is after the ')'
            call_end = k
            args = body[j+1:k-1]
            
            # Look backwards to see if this is `lhs :=` or `lhs =` or `return`
            # Find the start of the line
            line_start = body.rfind('\n', 0, idx) + 1
            prefix = body[line_start:idx].strip()
            
            if prefix.startswith('return'):
                # `return f(args)` -> `return __f(args)`
                out.append(body[i:idx])
                out.append(helper_name)
                out.append(body[j:call_end])
                i = call_end
            elif ':=' in prefix or '=' in prefix:
                # `a, b := f(args)` -> `__tmp := __f(args); a, b := __tmp.f0, __tmp.f1`
                # Extract LHS
                if ':=' in prefix:
                    lhs, _ = prefix.rsplit(':=', 1)
                    op = ':='
                else:
                    lhs, _ = prefix.rsplit('=', 1)
                    op = '='
                    # Avoid `==`, `!=`, `<=`, `>=`
                    if lhs.endswith(('=', '!', '<', '>')):
                        out.append(body[i:call_end])
                        i = call_end
                        continue
                lhs = lhs.strip()
                # Count LHS vars (split by comma, but careful with blank `_`)
                lhs_vars = [v.strip() for v in lhs.split(',')]
                if len(lhs_vars) != nfields:
                    # Mismatch, don't rewrite
                    out.append(body[i:call_end])
                    i = call_end
                    continue
                
                tmp = get_tmp()
                # Replace the whole `lhs := f(args)` with `tmp := __f(args); lhs := tmp.f0, tmp.f1`
                # We need to replace from line_start to call_end
                out.append(body[i:line_start])
                fields_access = ', '.join(f"{tmp}.{fn}" for fn in field_names)
                out.append(f"{tmp} := {helper_name}({args}); {lhs} {op} {fields_access}")
                i = call_end
            else:
                # Standalone call (should not happen for multi-result, but just in case)
                out.append(body[i:idx])
                out.append(helper_name)
                out.append(body[j:call_end])
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
    """
    Rewrite return statements in a function body to return pair struct.
    fields: list of (name, type) for the pair struct.
    
    Handles:
    - `return a, b` -> `return Pair{a, b}` (positional)
    - `return` (bare, with named results) -> `return Pair{n1, n2}`
    
    This is a simplified version - handles common cases.
    """
    fnames = [f[0] for f in fields]  # field names
    # For bare returns, we need the result variable names
    # For now, assume fields have names (from named results or generated)
    
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
        # Skip // comments
        if body.startswith('//', i):
            k = body.find('\n', i)
            if k == -1:
                out.append(body[i:])
                break
            out.append(body[i:k+1])
            i = k + 1
            continue
        # Skip /* */ comments
        if body.startswith('/*', i):
            k = body.find('*/', i)
            if k == -1:
                out.append(body[i:])
                break
            out.append(body[i:k+2])
            i = k + 2
            continue
        # Look for 'return' keyword
        m = re.match(r'\breturn\b', body[i:])
        if not m:
            # Copy char, handling strings
            c = body[i]
            if c in ('"', "'", '`'):
                j = skip_string(i, c)
                out.append(body[i:j])
                i = j
            else:
                out.append(c)
                i += 1
            continue
        
        # Found 'return'
        j = i + 6  # len('return')
        # Skip whitespace
        while j < n and body[j] in ' \t':
            j += 1
        
        # Check if it's a bare return (followed by newline, semicolon, or })
        if j >= n or body[j] in '\n;}':
            # Bare return: use field names
            out.append(f'return {pair_name}{{{", ".join(fnames)}}}')
            i = j
            continue
        
        # Has return values: parse until semicolon or newline (at depth 0)
        k = j
        depth = 0
        while k < n:
            c = body[k]
            if c in ('"', "'", '`'):
                k = skip_string(k, c)
                continue
            if body.startswith('//', k):
                # Line comment: return ends at newline
                break
            if c in '([{':
                depth += 1
            elif c in ')]}':
                depth -= 1
            elif c in '\n;' and depth == 0:
                break
            k += 1
        
        ret_vals = body[j:k].strip()
        # Split by top-level commas
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
        
        # Generate pair literal
        # If we have field names, use them; otherwise positional
        if len(vals) == len(fnames):
            assigns = [f"{fn}: {v}" for fn, v in zip(fnames, vals)]
            out.append(f'return {pair_name}{{{", ".join(assigns)}}}')
        else:
            # Fallback: positional
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
            'mime/multipart/writer.go',
            # Skip formdata.go (needs os file handling) and readmimeheader.go (check)
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
    """Adapt zip source: no special handling needed (deps available)."""
    return src

def mime_adapt(src, pkg_prefix):
    """Adapt mime source: no special handling needed."""
    return src

def multipart_adapt(src, pkg_prefix):
    """Adapt multipart source."""
    # Check readmimeheader.go - might need it
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
                results = parse_results(fn['results'])
                pair_name = make_pair_name(pkg_prefix, fn['recv_type'], fn['name'])
                helper_name = make_helper_name(pkg_prefix, fn['recv_type'], fn['name'])
                
                # Generate pair struct
                field_defs = []
                field_names = []
                for idx, (rname, rtype) in enumerate(results):
                    # Generate field name if not present
                    if not rname:
                        rname = f'f{idx}'
                    # Ensure valid Go identifier (handle cases like "b byte")
                    rname = rname.strip()
                    if not rname or not re.match(r'^[a-zA-Z_][a-zA-Z0-9_]*$', rname):
                        rname = f'f{idx}'
                    field_names.append(rname)
                    field_defs.append(f'\t{rname} {rtype}')
                
                pair_struct = f"type {pair_name} struct {{\n" + "\n".join(field_defs) + "\n}\n\n"
                new_src_parts.append(pair_struct)
                
                # Generate helper with real body (rewritten returns)
                # Build helper signature: receiver becomes first param (not a method)
                if fn['recv_type']:
                    ptr = "*" if fn['recv_ptr'] else ""
                    recv_param = f"{fn['recv_var']} {ptr}{fn['recv_type']}"
                    if fn['params'].strip():
                        all_params = f"{recv_param}, {fn['params']}"
                    else:
                        all_params = recv_param
                else:
                    all_params = fn['params']
                
                helper_sig = f"func {helper_name}({all_params}) {pair_name} {{\n"
                # Rewrite body returns
                rewritten_body = rewrite_body_returns(fn['body'], pair_name, list(zip(field_names, [r[1] for r in results])), pkg_prefix=pkg_prefix)
                # For named results, declare local variables IF they're needed.
                # Needed if: referenced in body, OR body has a bare return (which uses them).
                decls = ""
                has_bare = bool(re.search(r'\breturn\b\s*(\n|;|})', fn['body']))
                for (rname, rtype), fname in zip(results, field_names):
                    if rname and rname.strip():
                        referenced = bool(re.search(rf'\b{re.escape(rname)}\b', fn['body']))
                        if referenced or has_bare:
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
    
    # Build mr_map from table_entries and rewrite call sites
    # table_entries: (pkg_path, method_path, helper_name, pair_name, field_names)
    # method_path is like "mime.ParseMediaType" or "(*tar.Reader).Read"
    mr_map = {}
    for pkg_p, method_p, helper_n, pair_n, field_ns in table_entries:
        # method_p is the function name (e.g., "consumeToken") or method path
        # For functions, use it directly; for methods, extract the name
        if '.' in method_p:
            orig_full = method_p.split('.')[-1].strip('()*')
        else:
            orig_full = method_p
        mr_map[orig_full] = (helper_n, field_ns)
    
    if mr_map:
        # DEBUG
        import sys
        if 'consumeToken' in mr_map:
            print(f"DEBUG: consumeToken fields = {mr_map['consumeToken'][1]}", file=sys.stderr)
        full_src = rewrite_call_sites(full_src, mr_map)
    
    # Prepend collected imports (exclude internal/ which we stub)
    filtered_imports = [imp for imp in sorted(all_imports) if not imp.startswith('internal/')]
    if filtered_imports:
        import_block = 'import (\n' + '\n'.join(f'\t"{imp}"' for imp in filtered_imports) + '\n)\n\n'
        full_src = import_block + full_src
    
    # Handle backticks: convert raw strings to interpreted strings
    full_src = convert_raw_strings(full_src)
    if '`' in full_src:
        print(f"  WARNING: leftover backticks in {pkg_path}", file=sys.stderr)
    
    go_code = f'''// P3-M9 (tier-5): synthetic `{pkg_path}` package.
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
    entries_path = os.path.join(OUTDIR, 'archive_mime_table_entries.txt')
    with open(entries_path, 'w') as f:
        f.write('\n'.join(all_entries) + '\n')
    print(f"Wrote {len(all_entries)} table entries to {entries_path}", file=sys.stderr)

if __name__ == '__main__':
    main()
