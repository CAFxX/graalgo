#!/usr/bin/env python3
# P3-M9 (tier-5: compress): generator for the synthetic compress/*
# packages (real-Go sources in synth_compress_{flate,gzip,zlib,lzw,bzip2}.go).
#
# Method: vendor gc 1.27.1's compress sources and adapt them for the
# graalgo P3 frontend (WS D: no multi-result emission, no method
# values/expressions), following the gen_synth_bufio.py Appendix-M pattern:
# every multi-result func/method keeps its exact gc signature (for
# type-checking; skipped at synthetic import) and gains a single-result
# __-helper returning a pair struct with the real body. Table entries go
# into m7MultiResultFuncs (mrdesugar_m5.go) — the table consulted by the
# concrete-call, method-call AND interface-dispatch paths.
#
# DO NOT EDIT THE GENERATED FILES BY HAND (edit this generator and re-run).
import os
import re
import subprocess
import sys

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
# source surgery helpers
# ---------------------------------------------------------------------------

def strip_build_tags(src):
    return re.sub(r'^//go:build .*\n', '', src, flags=re.M)

def strip_package(src):
    return re.sub(r'^package \w+\n', '', src, count=1, flags=re.M)

def strip_imports(src):
    # Remove both `import "x"` and `import ( ... )` blocks.
    src = re.sub(r'^import \(.*?^\)\n', '', src, flags=re.M | re.S)
    src = re.sub(r'^import "\w[^"]*"\n', '', src, flags=re.M)
    src = re.sub(r'^import \(\n\)\n', '', src, flags=re.M)
    return src

def extract_func(src, recv, name):
    """Extract (decl, sig, body) of `func (v *Recv) Name(...) (...) {`."""
    if recv:
        pat = re.compile(r'^func \(\w+ \*?' + recv + r'\) ' + name +
                         r'\((.*?)\) \((.*?)\) \{\n', re.M | re.S)
    else:
        pat = re.compile(r'^func ' + name + r'\((.*?)\) \((.*?)\) \{\n',
                         re.M | re.S)
    m = pat.search(src)
    assert m, f"func {recv}.{name} not found"
    params, results = m.group(1), m.group(2)
    # pointer receiver?
    rm = re.match(r'^func \(\w+ (\*?)\w+\)', src[m.start():])
    ptr_recv = rm.group(1) == '*' if rm else False
    i = m.end()
    depth = 1
    while depth:
        c = src[i]
        if c == '{':
            depth += 1
        elif c == '}':
            depth -= 1
        i += 1
    decl = src[m.start():i]
    # recv var name
    rm = re.match(r'^func \((\w+) ', decl)
    recv_var = rm.group(1) if rm else None
    body = decl[decl.index('{') + 1:-1]  # inside braces
    return decl, params, results, recv_var, body, ptr_recv

def split_top_commas(s):
    parts, depth, cur = [], 0, ''
    for c in s:
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
    return parts

def parse_results(results):
    """'n int, err error' -> [('n','int'),('err','error')];
    'length, offset int32, ok bool' -> [('length','int32'),
    ('offset','int32'),('ok','bool')]; 'int, error' ->
    [(None,'int'),(None,'error')]."""
    groups = split_top_commas(results)
    # A name without its own type shares the next group's type
    # (Go result-list grouping).
    types = []
    for g in groups:
        toks = g.split()
        types.append(toks[1] if len(toks) == 2 else None)
    # propagate backwards: fill None from the next non-None
    cur = None
    for i in range(len(groups) - 1, -1, -1):
        if types[i] is not None:
            cur = types[i]
        else:
            types[i] = cur
    out = []
    for g, t in zip(groups, types):
        toks = g.split()
        if len(toks) == 2:
            out.append((toks[0], toks[1]))
        elif len(toks) == 1 and t is not None:
            out.append((toks[0], t))
        else:
            out.append((None, toks[0]))
    return out

def rewrite_returns(body, pair_name, fields, mr_names, helper_of, recv_var):
    """Rewrite return statements in a helper body.

    fields: [(name, type)] — pair field names in result order.
    mr_names: set of multi-result func names in this package (for
      `return f(...)` forwarding detection).
    helper_of: name -> (helper_name, recv_type or None, prepend_recv_var)
    recv_var: receiver variable name for method helpers (for building
      the temp call); None for package-level funcs.
    Skips returns inside nested func literals.
    """
    fnames = [f for f, _ in fields]
    out = []
    i, n = 0, len(body)
    tmpseq = [0]

    def skip_ws_comments(j):
        while j < n:
            if body[j] in ' \t':
                j += 1
            elif body.startswith('//', j):
                k = body.find('\n', j)
                j = n if k < 0 else k + 1
            elif body.startswith('/*', j):
                k = body.find('*/', j)
                j = n if k < 0 else k + 2
            else:
                break
        return j

    def skip_string(j):
        # j at opening quote (", ', or `)
        q = body[j]
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
        assert body[j] == op
        depth = 0
        while j < n:
            if body[j] in '"\'`':
                j = skip_string(j)
                continue
            if body.startswith('//', j) or body.startswith('/*', j):
                j = skip_ws_comments(j)
                continue
            if body[j] == op:
                depth += 1
            elif body[j] == cl:
                depth -= 1
                if depth == 0:
                    return j + 1
            j += 1
        return j

    def skip_func_literal(j):
        # j at 'func' keyword of a literal; skip to after its body
        k = j + 4
        k = skip_ws_comments(k)
        # no receiver for literals; skip params
        assert body[k] == '(', f"expected ( after func at {k}: {body[k:k+20]!r}"
        k = skip_balanced(k, '(', ')')
        k = skip_ws_comments(k)
        # optional result list
        if body[k] == '(':
            k = skip_balanced(k, '(', ')')
            k = skip_ws_comments(k)
        elif re.match(r'[A-Za-z_*\[\]]', body[k]):
            # single result type; skip one type token-ish (may contain [] etc.)
            while k < n and body[k] not in '{;':
                if body[k] in '"\'`':
                    k = skip_string(k)
                else:
                    k += 1
            k = skip_ws_comments(k)
        assert body[k] == '{', f"expected {{ for func literal at {k}: {body[k:k+20]!r}"
        return skip_balanced(k, '{', '}')

    while i < n:
        # comments / strings pass through
        if body.startswith('//', i) or body.startswith('/*', i):
            j = skip_ws_comments(i)
            out.append(body[i:j])
            i = j
            continue
        if body[i] in '"\'`':
            j = skip_string(i)
            out.append(body[i:j])
            i = j
            continue
        # nested func literal: skip whole thing (its returns belong to it)
        if re.match(r'func\b', body[i:]) and (i == 0 or not (body[i-1].isalnum() or body[i-1] == '_')):
            # make sure it's a literal, not something like 'myfunc('
            m = re.match(r'func(\s*\(|\s*$|\s+[A-Za-z])', body[i:])
            if m and (m.group(1).strip().startswith('(') or m.group(1).strip() == ''):
                j = skip_func_literal(i)
                out.append(body[i:j])
                i = j
                continue
        # return statement?
        m = re.match(r'return\b', body[i:])
        if m and (i == 0 or not (body[i-1].isalnum() or body[i-1] in '_$.')):
            j = i + 6
            k = skip_ws_comments(j)
            # find end of statement: newline at depth 0 terminates, unless
            # the line ends with a binary operator (Go line continuation).
            cont_chars = set('+-*/%&|^<>=!,.(:?')
            depth = 0
            e = k
            while e < n:
                if body[e] in '"\'`':
                    e = skip_string(e)
                    continue
                if body[e] in '([{':
                    depth += 1
                elif body[e] in ')]}':
                    depth -= 1
                elif body[e] == '\n' and depth == 0:
                    # check for line continuation: last non-ws char
                    be = e - 1
                    while be > k and body[be] in ' \t':
                        be -= 1
                    if body[be] not in cont_chars:
                        break
                elif body[e] == ';' and depth == 0:
                    break
                e += 1
            retval = body[k:e].strip()
            # collapse internal newlines in multi-line returns
            retval = re.sub(r'\s+', ' ', retval)
            # already a pair literal (e.g. pre-rewritten forwarding): leave
            if retval.startswith(pair_name + "{"):
                out.append(f'return {retval}')
                i = e
                continue
            if not retval:
                # bare return: use named results (declared as vars below)
                assigns = ', '.join(f'{f}: {v}' for f, v in
                                    zip(fnames, [vn for vn, _ in fields]))
                # fields entries are (name, type); named result vars share names
                out.append(f'return {pair_name}{{{assigns}}}')
            else:
                # forwarding: return f(args) where f is multi-result?
                fm = re.match(r'^([\w.]+)\((.*)\)$', retval, re.S)
                if fm and fm.group(1).split('.')[-1] in mr_names:
                    callee = fm.group(1).split('.')[-1]
                    hname = helper_of[callee][0]
                    callee_ptr_recv = helper_of[callee][8]
                    args = fm.group(2)
                    # rebuild call with receiver prepended for methods
                    if '.' in fm.group(1):
                        recv_expr = fm.group(1).rsplit('.', 1)[0]
                        # pointer-receiver helper + value receiver expr:
                        # take the address (the expr is addressable at
                        # every such call site; go vet verifies).
                        if callee_ptr_recv and not recv_expr.lstrip().startswith('&'):
                            recv_expr = f'&({recv_expr})'
                        call = f'{hname}({recv_expr}, {args})' if args else f'{hname}({recv_expr})'
                    else:
                        call = f'{hname}({args})'
                    tmpseq[0] += 1
                    t = f'__mr{tmpseq[0]}'
                    assigns = ', '.join(f'{f}: {t}.{f}' for f in fnames)
                    # keep original indentation: statement may span lines;
                    # emit temp on its own line(s) preserving leading ws
                    lead = body[i:e]
                    indent = lead[:len(lead) - len(lead.lstrip())]
                    out.append(f'{t} := {call}\n{indent}return {pair_name}{{{assigns}}}')
                else:
                    vals = split_top_commas(retval)
                    assert len(vals) == len(fnames), \
                        f"arity mismatch in return {retval!r} vs {fnames}"
                    assigns = ', '.join(f'{f}: {v}' for f, v in zip(fnames, vals))
                    out.append(f'return {pair_name}{{{assigns}}}')
            i = e
            continue
        out.append(body[i])
        i += 1
    return ''.join(out)

# ---------------------------------------------------------------------------
# package specs
# ---------------------------------------------------------------------------

FLATE_FILES = ["deflate.go", "deflatefast.go", "dict_decoder.go",
               "huffman_bit_writer.go", "huffman_code.go", "inflate.go",
               "level1.go", "level2.go", "level3.go", "level4.go",
               "level5.go", "level6.go", "load_store.go", "token.go",
               "regmask_amd64.go"]
FLATE_IMPORTS = ["bufio", "errors", "fmt", "io", "math", "math/bits",
                 "slices", "strconv", "sync"]

# (recv_type, method, [(pair_field, type)])
FLATE_MR = [
    ("compressor", "findMatch",
     [("length", "int32"), ("offset", "int32"), ("ok", "bool")]),
    ("compressor", "tryBetterMatchAtEnd",
     [("newLen", "int32"), ("newOff", "int32")]),
    ("compressor", "write", [("n", "int"), ("err", "error")]),
    ("", "NewWriter", [("w", "*Writer"), ("err", "error")]),
    ("", "NewWriterDict", [("w", "*Writer"), ("err", "error")]),
    ("Writer", "Write", [("n", "int"), ("err", "error")]),
    ("decompressor", "Read", [("n", "int"), ("err", "error")]),
    ("decompressor", "huffSym", [("sym", "int"), ("err", "error")]),
    ("huffmanBitWriter", "headerSize",
     [("size", "int"), ("numCodegens", "int")]),
    ("huffmanBitWriter", "dynamicSize",
     [("size", "int"), ("numCodegens", "int")]),
    ("huffmanBitWriter", "storedSize", [("size", "int"), ("ok", "bool")]),
    ("huffmanBitWriter", "indexTokens",
     [("numLiterals", "int"), ("numOffsets", "int")]),
]

GZIP_FILES = ["gunzip.go", "gzip.go"]
GZIP_IMPORTS = ["bufio", "compress/flate", "encoding/binary", "errors",
                "fmt", "hash/crc32", "io", "time"]
GZIP_MR = [
    ("", "NewReader", [("r", "*Reader"), ("err", "error")]),
    ("Reader", "readString", [("s", "string"), ("err", "error")]),
    ("Reader", "readHeader", [("hdr", "Header"), ("err", "error")]),
    ("Reader", "Read", [("n", "int"), ("err", "error")]),
    ("", "NewWriterLevel", [("w", "*Writer"), ("err", "error")]),
    ("Writer", "Write", [("n", "int"), ("err", "error")]),
]

ZLIB_FILES = ["reader.go", "writer.go"]
ZLIB_IMPORTS = ["bufio", "compress/flate", "encoding/binary", "errors",
                "fmt", "hash", "hash/adler32", "io"]
ZLIB_MR = [
    ("", "NewReader", [("r", "io.ReadCloser"), ("err", "error")]),
    ("", "NewReaderDict", [("r", "io.ReadCloser"), ("err", "error")]),
    ("reader", "Read", [("n", "int"), ("err", "error")]),
    ("", "NewWriterLevel", [("w", "*Writer"), ("err", "error")]),
    ("", "NewWriterLevelDict", [("w", "*Writer"), ("err", "error")]),
    ("Writer", "Write", [("n", "int"), ("err", "error")]),
]

LZW_FILES = ["reader.go", "writer.go"]
LZW_IMPORTS = ["bufio", "errors", "fmt", "io"]
LZW_MR = [
    ("Reader", "readLSB", [("code", "uint16"), ("err", "error")]),
    ("Reader", "readMSB", [("code", "uint16"), ("err", "error")]),
    ("Reader", "Read", [("n", "int"), ("err", "error")]),
    ("Writer", "Write", [("n", "int"), ("err", "error")]),
]

BZIP2_FILES = ["bit_reader.go", "bzip2.go", "huffman.go", "move_to_front.go"]
BZIP2_IMPORTS = ["bufio", "cmp", "io", "slices"]
BZIP2_MR = [
    ("reader", "Read", [("n", "int"), ("err", "error")]),
    ("reader", "read", [("n", "int"), ("err", "error")]),
    ("", "newHuffmanTree", [("t", "huffmanTree"), ("err", "error")]),
    ("", "buildHuffmanNode", [("nodeIndex", "uint16"), ("err", "error")]),
]

HEADER = """\
// SYNTHETIC (Principle 4) — %PKG%: real go1.27.1 source uses constructs the graalgo frontend cannot compile; ported with the documented adaptations below.
// RETIRES WHEN: frontend gains the underlying capability and the real GOROOT source compiles unchanged — then delete this file.
// RE-TEST TRIGGER: the capability named in this file's adaptation notes lands (B-4 phased removal: docs/arch/bridge-inventory.md).
// P3-M9 (tier-5: compress): synthetic `%PKG%` package.
//
// REAL Go source, embedded as text and type-checked on demand with the
// overlay importer (same mechanism as the P3-M7 synthetic math package,
// ADR-0007 D5).
//
// SOURCE: go1.27.1 GOROOT/src/compress/%PKG%, vendored verbatim except
// for the documented adaptations below.
//
// ADAPTATIONS (behavior-preserving; re-test trigger: frontend gains the
// underlying capability):
%ADAPT%
//
// Every multi-result func/method keeps its exact gc signature (for
// type-checking; skipped at synthetic import) and is served via the
// Appendix M pattern: a single-result __%PFX%<Recv><Method> helper
// returning a pair struct with the real body, plus m7MultiResultFuncs
// table entries (mrdesugar_m5.go).
//
// Generated by gen_synth_compress.py from gc 1.27.1; DO NOT EDIT BY
// HAND (edit the generator and re-run).
package main

// synthetic%CONST%Source is the P3-M9 synthetic `compress/%PKG%` package.
const synthetic%CONST%Source = `package %PKG%
%IMPORTS%
%PREAMBLE%
"""

# ---------------------------------------------------------------------------
# per-package adaptations
# ---------------------------------------------------------------------------

FLATE_ADAPT = """\
//  1. sync.OnceValue -> eager init: the M2 synthetic sync surface has
//     Mutex/RWMutex/WaitGroup/Once but not the generic OnceValue (and the
//     frontend cannot monomorphize a synthetic generic helper). All three
//     uses memoize a *huffmanEncoder from pure functions, so eager
//     package-level initialization is behavior-identical.
//  2. compressor.fill / compressor.step and decompressor.step were
//     func-typed fields holding method expressions (unsupported: WS H).
//     They are int enums (__flateFill*, __flateStep*) dispatched by
//     __flateCompressorFill/__flateCompressorStep/__flateDecompressorStep.
//  3. regmask_other.go (//go:build !amd64) excluded; the amd64 const
//     (reg8SizeMask64 = 63) is vendored (graalgo targets amd64)."""

FLATE_PREAMBLE = """
// ---- Adaptation 1: sync.OnceValue replaced by eager init (see header) ----
// (no preamble code; the rewrite happens in flate_adapt)

// ---- max/min builtins (WS G: frontend has no max/min intrinsic) ----
func __flateMaxInt(a, b int) int { if a > b { return a }; return b }
func __flateMinInt(a, b int) int { if a < b { return a }; return b }
func __flateMaxInt32(a, b int32) int32 { if a > b { return a }; return b }
func __flateMinInt32(a, b int32) int32 { if a < b { return a }; return b }
func __flateMaxFloat32(a, b float32) float32 { if a > b { return a }; return b }
func __flateMinFloat32(a, b float32) float32 { if a < b { return a }; return b }

// ---- *p = Struct{...} helpers (WS D) ----
// (hcode.set inlined at call site; see below)
func __flateZeroHuffmanDecoder(h *huffmanDecoder) {
\th.min = 0
\th.chunks = [huffmanNumChunks]uint32{}
\th.links = nil
\th.linkMask = 0
}
func __flateResetDecompressor(f *decompressor) {
\t// *f = decompressor{rBuf: f.rBuf, bits: f.bits, codebits: f.codebits,
\t//                  dict: f.dict, step: (*decompressor).nextBlock}
\trBuf := f.rBuf
\tbits := f.bits
\tcodebits := f.codebits
\tdict := f.dict
\tf.r = nil
\tf.rBuf = rBuf
\tf.roffset = 0
\tf.b = 0
\tf.nb = 0
\tf.h1 = nil
\tf.h2 = nil
\tf.bits = bits
\tf.codebits = codebits
\tf.dict = dict
\tf.buf = [4]byte{}
\tf.step = __flateDStepNextBlock
\tf.stepState = 0
\tf.final = false
\tf.err = nil
\tf.toRead = nil
\tf.hl = nil
\tf.hd = nil
\tf.copyLen = 0
\tf.copyDist = 0
}

// ---- Adaptations 2: step/fill state machines (see header) ----
const (
\t__flateFillBlock = iota
\t__flateFillDeflate
)

const (
\t__flateStepStore = iota
\t__flateStepDeflateHuff
\t__flateStepDeflateFast
\t__flateStepDeflateLazy
)

const (
\t__flateDStepNextBlock = iota
\t__flateDStepHuffmanBlock
\t__flateDStepCopyData
)

func __flateCompressorFill(d *compressor, b []byte) int {
\tswitch d.fill {
\tcase __flateFillBlock:
\t\treturn d.fillBlock(b)
\tcase __flateFillDeflate:
\t\treturn d.fillDeflate(b)
\t}
\treturn 0
}

func __flateCompressorStep(d *compressor) {
\tswitch d.step {
\tcase __flateStepStore:
\t\td.store()
\tcase __flateStepDeflateHuff:
\t\td.deflateHuff()
\tcase __flateStepDeflateFast:
\t\td.deflateFast()
\tcase __flateStepDeflateLazy:
\t\td.deflateLazy()
\t}
}

func __flateDecompressorStep(f *decompressor) {
\tswitch f.step {
\tcase __flateDStepNextBlock:
\t\tf.nextBlock()
\tcase __flateDStepHuffmanBlock:
\t\tf.huffmanBlock()
\tcase __flateDStepCopyData:
\t\tf.copyData()
\t}
}
"""

LZW_ADAPT = """\
//  1. Reader.read / Writer.write were func-typed fields holding method
//     expressions (unsupported: WS H). Reader keeps the order in the
//     readOrder field and dispatches via __lzwReaderReadCode; Writer
//     already stores order and dispatches via __lzwWriterWrite.
//  2. *p = Struct{...} assignments (unsupported: WS D, no assignment to
//     *ast.StarExpr). Reader.Reset / Writer.Reset zero the struct
//     field-by-field instead: same observable state."""

GZIP_ADAPT = """\
//  1. *p = Struct{...} assignments (unsupported: WS D, no assignment to
//     *ast.StarExpr). Reader.Reset / Writer.init assign the struct
//     field-by-field instead: same observable state."""

ZLIB_ADAPT = """\
//  1. *p = Struct{...} assignments (unsupported: WS D, no assignment to
//     *ast.StarExpr). reader.Reset assigns the struct field-by-field
//     instead: same observable state."""

LZW_PREAMBLE = """
// ---- Adaptation 1: order dispatch (see header) ----
type __lzwReadCodePair struct {
\tcode uint16
\terr  error
}

func __lzwReaderReadCode(r *Reader) __lzwReadCodePair {
\tif r.readOrder == LSB {
\t\t__p := __lzwReaderreadLSB(r)
\t\treturn __lzwReadCodePair{code: __p.code, err: __p.err}
\t}
\t__p := __lzwReaderreadMSB(r)
\treturn __lzwReadCodePair{code: __p.code, err: __p.err}
}

func __lzwWriterWriteCode(w *Writer, code uint32) error {
\tif w.order == LSB {
\t\treturn w.writeLSB(code)
\t}
\treturn w.writeMSB(code)
}
"""

def _fix_decompressor_reset(src):
    import re
    pat = r'\*f = decompressor\{[^}]+\}'
    src = re.sub(pat, '__flateResetDecompressor(f)', src)
    return src

def flate_adapt(src):
    # sync.OnceValue -> eager init (behavior-identical; all three uses
    # memoize pure-function *huffmanEncoder values). The func-literal
    # case spans lines, so match the closing "})" explicitly.
    src = src.replace("sync.OnceValue(func() *huffmanEncoder {",
                      "(func() *huffmanEncoder {")
    # close the paren opened by sync.OnceValue( : the literal ends with
    # "return h\n})" -> "return h\n})()"
    src = src.replace("return h\n})", "return h\n})()")
    src = src.replace("sync.OnceValue(generateFixedLiteralEncoding)",
                      "generateFixedLiteralEncoding()")
    src = src.replace("sync.OnceValue(generateFixedOffsetEncoding)",
                      "generateFixedOffsetEncoding()")
    # call sites: fixedLiteralEncoding() -> fixedLiteralEncoding (now a value)
    src = src.replace("fixedLiteralEncoding()", "fixedLiteralEncoding")
    src = src.replace("fixedOffsetEncoding()", "fixedOffsetEncoding")
    src = src.replace("huffOffset()", "huffOffset")
    assert "sync.OnceValue" not in src, "unreplaced sync.OnceValue remains"
    # WS D: & on *ast.Ident unsupported. NewWriter/NewWriterDict (and their
    # __-helpers) do "var dw Writer; ...; return &dw". Rewrite to allocate
    # the pointer directly.
    src = src.replace("var dw Writer\n\tif err := dw.d.init(w, level); err != nil {",
                      "dw := new(Writer)\n\tif err := dw.d.init(w, level); err != nil {")
    src = src.replace("return &dw, nil", "return dw, nil")
    src = src.replace("__flateNewWriterPair{w: &dw, err: nil}",
                      "__flateNewWriterPair{w: dw, err: nil}")
    src = src.replace("__flateNewWriterDictPair{w: &dw, err: nil}",
                      "__flateNewWriterDictPair{w: dw, err: nil}")
    # WS D: & on *ast.Ident unsupported. fixedHuffmanDecoder is a package
    # var whose address is taken once; make it a pointer directly.
    src = src.replace("var fixedHuffmanDecoder huffmanDecoder",
                      "var fixedHuffmanDecoder = &huffmanDecoder{}")
    src = src.replace("f.hl = &fixedHuffmanDecoder",
                      "f.hl = fixedHuffmanDecoder")
    # WS D: & on index expr unsupported. Three cases:
    # 1. counts := &leafCounts[maxBits] (read-only) -> copy the array value.
    src = src.replace("counts := &leafCounts[maxBits]",
                      "counts := leafCounts[maxBits]")
    # 2. v0 := &h[t]; *v0++ -> h[t]++ (4x, different indices).
    src = src.replace("v0 := &h[t]\n\t\tv1 := &h[y[i]]\n\t\tv2 := &h[z[i]]\n\t\tv3 := &h[w[i]]\n\t\t*v0++\n\t\t*v1++\n\t\t*v2++\n\t\t*v3++",
                      "h[t]++\n\t\th[y[i]]++\n\t\th[z[i]]++\n\t\th[w[i]]++")
    # 3. l := &levels[level] -> direct levels[level].field accesses.
    #    The l variable is only used in this one loop; drop the declaration
    #    and rewrite l. -> levels[level]. throughout the file. (Verified: no
    #    other \bl\. exists in the flate sources.)
    src = src.replace("\t\tl := &levels[level]\n", "")
    src = re.sub(r"\bl\.", "levels[level].", src)
    # WS D: & on Ident (var f decompressor; return &f) in NewReader/
    # NewReaderDict. Allocate the pointer directly.
    src = src.replace("var f decompressor\n\tf.makeReader(r)",
                      "f := new(decompressor)\n\tf.makeReader(r)")
    src = src.replace("\treturn &f\n}", "\treturn f\n}")
    # WS D: & on SelectorExpr/IndexExpr unsupported. Refactor to avoid &:
    # 1. compressor.tokens: tokens -> *tokens (field); &d.tokens -> d.tokens.
    #    Initialize in init().
    src = src.replace("\ttokens tokens         // tokens store for each block",
                      "\ttokens *tokens        // tokens store (pointer: WS D)")
    src = src.replace("func (d *compressor) init(w io.Writer, level int) (err error) {\n\td.w = newHuffmanBitWriter(w)",
                      "func (d *compressor) init(w io.Writer, level int) (err error) {\n\td.w = newHuffmanBitWriter(w)\n\td.tokens = new(tokens)")
    src = src.replace("&d.tokens", "d.tokens")
    # 2. range &s.hashPrev / &s.hashHead: drop & (read v, write via index).
    src = src.replace("for i, v := range &s.hashPrev {", "for i, v := range s.hashPrev {")
    src = src.replace("for i, v := range &s.hashHead {", "for i, v := range s.hashHead {")
    # 3. decompressor h1/h2: huffmanDecoder -> *huffmanDecoder; &f.h1 -> f.h1.
    #    Initialized in makeReader.
    src = src.replace("\th1, h2 huffmanDecoder", "\th1, h2 *huffmanDecoder")
    src = src.replace("f.hl = &f.h1", "f.hl = f.h1")
    src = src.replace("f.hd = &f.h2", "f.hd = f.h2")
    src = src.replace("x, err := f.huffSym(&f.h1)", "x, err := f.huffSym(f.h1)")
    # Initialize h1/h2 in makeReader (they are now *huffmanDecoder).
    src = src.replace("func (f *decompressor) makeReader(r io.Reader) {",
                      "func (f *decompressor) makeReader(r io.Reader) {\n\tf.h1 = &huffmanDecoder{}\n\tf.h2 = &huffmanDecoder{}")
    # 4. &e.bTable[idx]: eLong := &e.bTable[IDX]; eLong.f = ... ->
    #    e.bTable[IDX].f = ... (RHS evaluated before assignment, so
    #    eLong.cur on RHS -> e.bTable[IDX].cur is safe).
    #    Process line-by-line: track the current eLong index.
    _lines = src.split("\n")
    _out = []
    _cur_idx = None
    for _ln in _lines:
        _m = re.match(r"^(\s*)eLong(\d*) := &e\.bTable\[(.+)\]$", _ln)
        if _m:
            _cur_idx = (_m.group(3), _m.group(2))  # (index expr, suffix)
            continue  # drop the declaration line
        if _cur_idx and re.search(r"\beLong" + _cur_idx[1] + r"\.", _ln):
            _idx, _suf = _cur_idx
            _ln = re.sub(r"\beLong" + _suf + r"\.", f"e.bTable[{_idx}].", _ln)
        _out.append(_ln)
    src = "\n".join(_out)
    # 5. eLong = &e.bTable[idx] (assignment, not declaration): same as #4.
    lines = src.split("\n")
    out = []
    cur_idx = None
    for ln in lines:
        m = re.match(r"^(\s*)eLong(\d*) = &e\.bTable\[(.+)\]$", ln)
        if m:
            cur_idx = (m.group(3), m.group(2))
            out.append(m.group(1) + f"// eLong{m.group(2)} -> e.bTable[{m.group(3)}]")
            continue
        if cur_idx and re.search(r"\beLong" + cur_idx[1] + r"\.", ln):
            idx, suf = cur_idx
            ln = re.sub(r"\beLong" + suf + r"\.", f"e.bTable[{idx}].", ln)
        out.append(ln)
    src = "\n".join(out)
    # 5b. max/min builtins -> typed helpers (WS G: frontend has no max/min).
    # Types determined from gc 1.27.1 source inspection:
    # - int32: v-delta, pos-windowSize, s.index-windowSize, lookahead,
    #   d.nice, startindex+..., newIndex/s.maxInsertIndex (int32 fields)
    # - int: the rest (1, count, len(...), etc.)
    def _maxmin(src):
        # First, replace all with int versions, then fix int32 cases.
        import re as _re2
        # Use paren-matching for nested calls
        def _repl(go, name, helper):
            out2 = []
            i = 0
            while i < len(go):
                j = go.find(name + '(', i)
                if j == -1:
                    out2.append(go[i:])
                    break
                if j > 0 and (go[j-1].isalnum() or go[j-1] == '_'):
                    out2.append(go[i:j+1])
                    i = j + 1
                    continue
                out2.append(go[i:j])
                depth = 0
                k = j + len(name)
                start = k
                while k < len(go):
                    if go[k] == '(':
                        depth += 1
                    elif go[k] == ')':
                        depth -= 1
                        if depth == 0:
                            break
                    k += 1
                args = go[start+1:k]
                out2.append(f'{helper}({args})')
                i = k + 1
            return ''.join(out2)
        src = _repl(src, 'max', '__flateMaxInt')
        src = _repl(src, 'min', '__flateMinInt')
        # int32 cases (from type errors / source inspection)
        for pat in ['v-delta', 'pos-windowSize', 's.index-windowSize',
                    'lookahead', 'd.nice', 'startindex+256+minMatchLength-1',
                    'newIndex', 's.maxInsertIndex', 's.index+1']:
            src = src.replace(f'__flateMaxInt({pat},', f'__flateMaxInt32({pat},')
            src = src.replace(f'__flateMinInt({pat},', f'__flateMinInt32({pat},')
        for pat in ['-mFastLog2(n*invTotal)']:
            # This pattern doesn't match (args are reversed); handled below.
            pass
        # float32: max(1, -mFastLog2(...)) and min(15, max(...))
        src = src.replace('__flateMaxInt(1, -mFastLog2(n*invTotal))',
                          '__flateMaxFloat32(1, -mFastLog2(n*invTotal))')
        src = src.replace('__flateMinInt(15, __flateMaxFloat32(1, -mFastLog2(n*invTotal)))',
                          '__flateMinFloat32(15, __flateMaxFloat32(1, -mFastLog2(n*invTotal)))')
        return src
    src = _maxmin(src)
    # 5c. *p = Struct{...} assignments (WS D: no assignment to StarExpr).
    # Replace with field-by-field resets.
    # 1. *dd = dictDecoder{hist: dd.hist} -> zero wrPos, rdPos, full
    src = src.replace('*dd = dictDecoder{hist: dd.hist}',
                      'dd.wrPos = 0\ndd.rdPos = 0\ndd.full = false')
    # 2. hcode.set method: *h = newhcode(code, length) (WS D)
    #    Only caller: h.codes[node.literal].set(uint16(i), 1)
    #    Inline: h.codes[node.literal] = hcode(1) | (hcode(uint16(i)) << 8)
    #    (index assignment, not StarExpr)
    src = src.replace('h.codes[node.literal].set(uint16(i), 1)',
                      'h.codes[node.literal] = hcode(1) | (hcode(uint16(i)) << 8)')
    #    Remove the set method itself (contains *h = ...)
    src = src.replace('func (h *hcode) set(code uint16, length uint8) {\n\t*h = newhcode(code, length)\n}',
                      'func (h *hcode) set(code uint16, length uint8) {\n\t// WS D: inlined at call site\n}')
    # 3. *h = huffmanDecoder{} -> zero all fields
    #    (huffmanDecoder fields: min, limit, codes; from source)
    src = src.replace('*h = huffmanDecoder{}',
                      '__flateZeroHuffmanDecoder(h)')
    # 4. *f = decompressor{...} in Reset -> field assignments
    #    (handled via specific pattern below)
    src = _fix_decompressor_reset(src)
    # 6. Writer.d: compressor -> *compressor (avoids &(w.d) in helper calls).
    src = src.replace("type Writer struct {\n\td    compressor\n\tdict []byte\n}",
                      "type Writer struct {\n\td    *compressor\n\tdict []byte\n}")
    src = src.replace("dw := new(Writer)\n\tif err := dw.d.init(w, level); err != nil {",
                      "dw := new(Writer)\n\tdw.d = new(compressor)\n\tif err := dw.d.init(w, level); err != nil {")
    # The __-helper call: __flateCompressorwrite(&(w.d), data) -> __flateCompressorwrite(w.d, data)
    src = src.replace("__flateCompressorwrite(&(w.d), data)", "__flateCompressorwrite(w.d, data)")
    # Also for NewWriterDict (same pattern)
    src = src.replace("dw := new(Writer)\n\tif err := dw.d.init(w, level); err != nil {\n\t\treturn nil, err\n\t}\n\tdw.dict = dict",
                      "dw := new(Writer)\n\tdw.d = new(compressor)\n\tif err := dw.d.init(w, level); err != nil {\n\t\treturn nil, err\n\t}\n\tdw.dict = dict")
    # decompressor.step field + uses
    src = src.replace("step      func(*decompressor)",
                      "step      int")
    src = src.replace("f.step(f)", "__flateDecompressorStep(f)")
    src = src.replace("f.step = (*decompressor).nextBlock",
                      "f.step = __flateDStepNextBlock")
    src = src.replace("step:     (*decompressor).nextBlock,",
                      "step:     __flateDStepNextBlock,")
    src = src.replace("f.step = (*decompressor).huffmanBlock",
                      "f.step = __flateDStepHuffmanBlock")
    src = src.replace("f.step = (*decompressor).copyData",
                      "f.step = __flateDStepCopyData")
    # compressor.fill / compressor.step fields + uses
    src = src.replace("fill func(*compressor, []byte) int // copy data to window",
                      "fill int // __flateFill* (adapted: was func field)")
    src = src.replace("step func(*compressor)             // process window",
                      "step int // __flateStep* (adapted: was func field)")
    src = src.replace("d.fill = (*compressor).fillBlock", "d.fill = __flateFillBlock")
    src = src.replace("d.fill = (*compressor).fillDeflate", "d.fill = __flateFillDeflate")
    src = src.replace("d.step = (*compressor).store", "d.step = __flateStepStore")
    src = src.replace("d.step = (*compressor).deflateHuff", "d.step = __flateStepDeflateHuff")
    src = src.replace("d.step = (*compressor).deflateFast", "d.step = __flateStepDeflateFast")
    src = src.replace("d.step = (*compressor).deflateLazy", "d.step = __flateStepDeflateLazy")
    src = src.replace("d.step(d)", "__flateCompressorStep(d)")
    src = src.replace("d.fill(d, b)", "__flateCompressorFill(d, b)")
    return src

def gzip_adapt(src):
    # 1. *p = Struct{...} assignments (WS D: no assignment to StarExpr).
    # Replace with field-by-field assignments (same observable state).
    # 1a. Reader.Reset: *z = Reader{decompressor: z.decompressor, multistream: true}
    src = src.replace(
        "\t*z = Reader{\n\t\tdecompressor: z.decompressor,\n\t\tmultistream:  true,\n\t}",
        "\t// WS D: no StarExpr assignment; fields assigned individually.\n"
        "\tdecompressor := z.decompressor\n"
        "\tz.Header = Header{}\n"
        "\tz.r = nil\n"
        "\tz.decompressor = decompressor\n"
        "\tz.digest = 0\n"
        "\tz.size = 0\n"
        "\tz.buf = [512]byte{}\n"
        "\tz.err = nil\n"
        "\tz.multistream = true")
    # 1b. Writer.init: *z = Writer{Header: {OS: 255}, w: w, level: level, compressor: compressor}
    src = src.replace(
        "\t*z = Writer{\n\t\tHeader: Header{\n\t\t\tOS: 255, // unknown\n\t\t},\n\t\tw:          w,\n\t\tlevel:      level,\n\t\tcompressor: compressor,\n\t}",
        "\t// WS D: no StarExpr assignment; fields assigned individually.\n"
        "\tz.Header = Header{}\n"
        "\tz.Header.OS = 255 // unknown\n"
        "\tz.w = w\n"
        "\tz.level = level\n"
        "\tz.wroteHeader = false\n"
        "\tz.closed = false\n"
        "\tz.buf = [10]byte{}\n"
        "\tz.compressor = compressor\n"
        "\tz.digest = 0\n"
        "\tz.size = 0\n"
        "\tz.err = nil")
    return src


def zlib_adapt(src):
    # 1. *p = Struct{...} assignments (WS D: no assignment to StarExpr).
    # reader.Reset: *z = reader{decompressor: z.decompressor}
    src = src.replace(
        "\t*z = reader{decompressor: z.decompressor}",
        "\t// WS D: no StarExpr assignment; fields assigned individually.\n"
        "\tdecompressor := z.decompressor\n"
        "\tz.r = nil\n"
        "\tz.decompressor = decompressor\n"
        "\tz.digest = nil\n"
        "\tz.err = nil\n"
        "\tz.scratch = [4]byte{}")
    return src


def lzw_adapt(src):
    src = src.replace(
        "read     func(*Reader) (uint16, error) // readLSB or readMSB",
        "readOrder Order // adapted: was func field holding a method expression")
    src = src.replace("r.read = (*Reader).readLSB", "r.readOrder = LSB")
    src = src.replace("r.read = (*Reader).readMSB", "r.readOrder = MSB")
    src = src.replace("code, err := r.read(r)",
                      "__lzwrc := __lzwReaderReadCode(r)\n\t\tcode, err := __lzwrc.code, __lzwrc.err")
    src = src.replace("write func(*Writer, uint32) error\n",
                      "")
    src = src.replace("\tw.write = (*Writer).writeLSB\n", "")
    src = src.replace("\tw.write = (*Writer).writeMSB\n", "")
    src = re.sub(r'w\.write\(w, ', '__lzwWriterWriteCode(w, ', src)
    # 2. *p = Struct{} assignments (WS D: no assignment to StarExpr).
    # Replace with field-by-field zeroing (same observable state).
    # 2a. Reader.Reset: *r = Reader{}
    src = src.replace(
        "\t*r = Reader{}\n\tr.init(src, order, litWidth)",
        "\t// WS D: no StarExpr assignment; fields zeroed individually.\n"
        "\tr.r = nil\n"
        "\tr.bits = 0\n"
        "\tr.nBits = 0\n"
        "\tr.width = 0\n"
        "\tr.readOrder = LSB\n"
        "\tr.litWidth = 0\n"
        "\tr.err = nil\n"
        "\tr.clear = 0\n"
        "\tr.eof = 0\n"
        "\tr.hi = 0\n"
        "\tr.overflow = 0\n"
        "\tr.last = 0\n"
        "\tr.suffix = [1 << maxWidth]uint8{}\n"
        "\tr.prefix = [1 << maxWidth]uint16{}\n"
        "\tr.output = [2 * 1 << maxWidth]byte{}\n"
        "\tr.o = 0\n"
        "\tr.toRead = nil\n"
        "\tr.init(src, order, litWidth)")
    # 2b. Writer.Reset: *w = Writer{}
    src = src.replace(
        "\t*w = Writer{}\n\tw.init(dst, order, litWidth)",
        "\t// WS D: no StarExpr assignment; fields zeroed individually.\n"
        "\tw.w = nil\n"
        "\tw.litWidth = 0\n"
        "\tw.order = LSB\n"
        "\tw.nBits = 0\n"
        "\tw.width = 0\n"
        "\tw.bits = 0\n"
        "\tw.hi = 0\n"
        "\tw.overflow = 0\n"
        "\tw.savedCode = 0\n"
        "\tw.err = nil\n"
        "\tw.table = [tableSize]uint32{}\n"
        "\tw.init(dst, order, litWidth)")
    return src

# ---------------------------------------------------------------------------
# generation
# ---------------------------------------------------------------------------

def const_name(pkg):
    return "Compress" + pkg.capitalize()

def build_package(pkg, files, imports, mr_specs, adapt_fn, adapt_doc, preamble):
    parts = []
    for fn in files:
        with open(f"{GOROOT}/src/compress/{pkg}/{fn}") as f:
            src = f.read()
        src = strip_build_tags(src)
        src = strip_package(src)
        src = strip_imports(src)
        src = src.replace("`", "'")
        parts.append(src.rstrip() + "\n")
    src = "\n".join(parts)
    if adapt_fn:
        src = adapt_fn(src)

    # multi-result transformation
    mr_names = {name for _, name, _ in mr_specs}
    helper_of = {}
    prefix = "__" + pkg
    # pre-populate helper names (bodies may forward to later-listed funcs)
    for recv, name, fields in mr_specs:
        # recv capitalized, method name EXACT (Read vs read must stay
        # distinct, e.g. bzip2's (reader,Read) vs (reader,read)).
        capname = (recv[0].upper() + recv[1:] + name) if recv else (name[0].upper() + name[1:])
        helper = f"{prefix}{capname}"
        decl, params, results, recv_var, body, ptr_recv = extract_func(src, recv, name)
        helper_of[name] = (helper, recv, recv_var, decl, params, results, body,
                           fields, ptr_recv)
    helpers = []
    for _recv, name, _fields in mr_specs:
        helper, recv, recv_var, decl, params, results, body, fields, ptr_recv = \
            helper_of[name]
        pair = helper + "Pair"
        # pair struct
        pair_def = f"type {pair} struct {{\n"
        for fn2, ft in fields:
            pair_def += f"\t{fn2} {ft}\n"
        pair_def += "}\n\n"
        # rewrite returns in a copy of the body
        new_body = rewrite_returns(body, pair, fields, mr_names, helper_of, recv_var)
        # Named results are in scope throughout the original body; the
        # helper must declare the ones the body actually uses (standalone,
        # not via := which would conflict, not as .field selectors).
        decls = []
        for vn, vt in parse_results(results):
            if not vn:
                continue
            used = re.search(rf'(?<![.\w]){vn}\b', body)
            shadowed = re.search(rf'(?<![.\w]){vn}\s*:=', body)
            if used and not shadowed:
                decls.append(f"\tvar {vn} {vt}\n")
        if decls:
            new_body = "".join(decls) + new_body
        # helper decl
        if recv:
            hsig = f"func {helper}({recv_var} *{recv}"
            if params.strip():
                hsig += f", {params}"
            hsig += f") {pair} {{\n"
        else:
            hsig = f"func {helper}({params}) {pair} {{\n"
        helpers.append(pair_def + hsig + new_body + "}\n")
    helpers_src = "\n".join(helpers)
    if not helpers_src.endswith("\n"):
        helpers_src += "\n"

    imports_src = "import (\n" + "".join(f'\t"{i}"\n' for i in imports) + ")\n"
    header = (HEADER.replace("%PKG%", pkg)
                   .replace("%ADAPT%", adapt_doc if adapt_doc else "//  (none)")
                   .replace("%PFX%", pkg)
                   .replace("%CONST%", const_name(pkg))
                   .replace("%IMPORTS%", imports_src)
                   .replace("%PREAMBLE%", preamble if preamble else ""))
    out = header + "\n" + src.rstrip() + "\n\n" + helpers_src + "`\n"
    # sanity: no backticks inside the embedded source (would terminate
    # the raw literal early); the final closing backtick is intentional.
    embedded = src.rstrip() + "\n\n" + helpers_src
    assert "`" not in embedded, "backtick survived in embedded source"
    return out, helper_of

def main():
    pkgs = [
        ("flate", FLATE_FILES, FLATE_IMPORTS, FLATE_MR, flate_adapt, FLATE_ADAPT, FLATE_PREAMBLE),
        ("gzip", GZIP_FILES, GZIP_IMPORTS, GZIP_MR, gzip_adapt, GZIP_ADAPT, None),
        ("zlib", ZLIB_FILES, ZLIB_IMPORTS, ZLIB_MR, zlib_adapt, ZLIB_ADAPT, None),
        ("lzw", LZW_FILES, LZW_IMPORTS, LZW_MR, lzw_adapt, LZW_ADAPT, LZW_PREAMBLE),
        ("bzip2", BZIP2_FILES, BZIP2_IMPORTS, BZIP2_MR, None, None, None),
    ]
    table_lines = []
    for pkg, files, imports, mr, adapt_fn, adapt_doc, preamble in pkgs:
        out, helper_of = build_package(pkg, files, imports, mr, adapt_fn, adapt_doc, preamble)
        # Post-build fixups (patterns generated by rewrite_returns that
        # flate_adapt cannot see because it runs before).
        if pkg == "flate":
            # Writer.d is now *compressor; &(w.d) -> w.d.
            out = out.replace("__flateCompressorwrite(&(w.d), data)",
                              "__flateCompressorwrite(w.d, data)")
        fn = f"{OUTDIR}/synth_compress_{pkg}.go"
        with open(fn, "w") as f:
            f.write(out)
        print(f"wrote {fn} ({len(out)} bytes)")
        # emit m7MultiResultFuncs entries for the operator to paste
        for recv, name, fields in mr:
            helper = helper_of[name][0]
            pair = helper + "Pair"
            flds = "[" + ", ".join(f'"{f}"' for f, _ in fields) + "]"
            table_lines.append(
                f'\t{{"compress/{pkg}", "{name}", "{recv}", "{helper}", "{pair}", {flds}}},')
    print("\n// m7MultiResultFuncs entries (paste into mrdesugar_m5.go):")
    print("\n".join(table_lines))

main()
