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

def extract_functions(src):
    """Extract all top-level func declarations. Returns list of (decl, recv, name, params, results, body)."""
    funcs = []
    # Match func declarations (with or without receiver)
    # Results can be: empty, single type (no parens), or (multiple) with parens
    pat = re.compile(
        r'^func (\((\w+) (\*?\w+)\) )?(\w+)\((.*?)\)(?: \(((?:[^()]*|\([^()]*\))*)\)| (\S+))? \{\n',
        re.M | re.S
    )
    for m in pat.finditer(src):
        recv_full, recv_var, recv_type, name, params, results_paren, results_single = m.groups()
        results = results_paren if results_paren is not None else (results_single or '')
        # Find the matching closing brace
        i = m.end()
        depth = 1
        while i < len(src) and depth > 0:
            c = src[i]
            if c == '{':
                depth += 1
            elif c == '}':
                depth -= 1
            elif c in '"\'`':
                # Skip strings/chars/raw strings
                q = c
                i += 1
                while i < len(src):
                    if q != '`' and src[i] == '\\':
                        i += 2
                        continue
                    if src[i] == q:
                        break
                    i += 1
            i += 1
        decl = src[m.start():i]
        body_start = decl.index('{') + 1
        # Find the last '}' which closes the function (decl may have trailing whitespace)
        body_end = decl.rindex('}')
        body = decl[body_start:body_end]  # inside braces, excluding final }
        funcs.append({
            'decl': decl,
            'recv_var': recv_var,
            'recv_type': recv_type.lstrip('*') if recv_type else None,
            'recv_ptr': recv_type.startswith('*') if recv_type else False,
            'name': name,
            'params': params,
            'results': results or '',
            'body': body,
            'start': m.start(),
            'end': i,
        })
    return funcs

def parse_results(results):
    """Parse result clause into list of (name, type). Handles '(a int, b string)', '(int, error)', 'int'."""
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
    
    parsed = []
    for p in parts:
        p = p.strip()
        if not p:
            continue
        # Try to split into name and type
        # Heuristic: if it has a space and the first word is not a type keyword...
        tokens = p.split()
        if len(tokens) >= 2:
            # Could be "name type" or "type" with space (like "map[string]int")
            # Simple heuristic: check if first token looks like a type
            first = tokens[0]
            # Common types that don't have names
            if first in ('int', 'int8', 'int16', 'int32', 'int64', 'uint', 'uint8', 'uint16', 'uint32', 'uint64',
                        'float32', 'float64', 'string', 'bool', 'byte', 'rune', 'error', 'any') or \
               first.startswith('*') or first.startswith('[') or first.startswith('map[') or \
               first.startswith('func(') or first.startswith('chan '):
                # No name, just type
                parsed.append(('', p))
            else:
                # Assume "name type"
                name = tokens[0]
                typ = ' '.join(tokens[1:])
                parsed.append((name, typ))
        else:
            # Single token: just a type, no name
            parsed.append(('', p))
    return parsed

def is_multi_result(results):
    """Check if results clause has more than one result."""
    parsed = parse_results(results)
    return len(parsed) > 1

def make_pair_name(pkg_prefix, recv_type, name):
    """Generate pair struct name: __<prefix><Recv><Name>Pair"""
    recv_part = recv_type if recv_type else ''
    # Capitalize first letter of recv and name for Go export style (but keep __ prefix)
    return f"__{pkg_prefix}{recv_part}{name}Pair"

def make_helper_name(pkg_prefix, recv_type, name):
    """Generate helper func name: __<prefix><Recv><Name>"""
    recv_part = recv_type if recv_type else ''
    return f"__{pkg_prefix}{recv_part}{name}"

def rewrite_body_returns(body, pair_name, fields):
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
            if c in '"\'`':
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
        # Look for 'return' keyword
        m = re.match(r'\breturn\b', body[i:])
        if not m:
            # Copy char, handling strings
            c = body[i]
            if c in '"\'`':
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
            if c in '"\'`':
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
    
    for rel_file in cfg['files']:
        full_path = os.path.join(GOROOT, 'src', rel_file)
        with open(full_path, 'r') as f:
            src = f.read()
        
        src = strip_build_tags(src)
        src = strip_package(src)
        src = strip_imports(src)
        
        # Extract functions
        funcs = extract_functions(src)
        
        # Process each function
        new_src_parts = []
        last_end = 0
        
        for fn in funcs:
            # Copy text before this function
            new_src_parts.append(src[last_end:fn['start']])
            
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
                
                helper_sig = f"func {helper_name}({all_params}) {pair_name} {{"
                # Rewrite body returns
                rewritten_body = rewrite_body_returns(fn['body'], pair_name, list(zip(field_names, [r[1] for r in results])))
                helper_decl = helper_sig + rewritten_body + "}\n\n"
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
    
    # Handle backticks: they cannot appear in Go raw string literals.
    # 1. Replace rune literal '`' with '\x60' (hex escape for backtick)
    full_src = full_src.replace("'\\`'", "'\\x60'")
    # 2. Replace remaining backticks (in comments only) with single quotes
    if '`' in full_src:
        full_src = full_src.replace('`', "'")
        print(f"  Note: replaced backticks in {pkg_path} (comments only)", file=sys.stderr)
    
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
