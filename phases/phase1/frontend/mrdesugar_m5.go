package main

import (
	"fmt"
	"go/ast"
	"go/token"
	"go/types"
)

// P3-M5 (WS E, ADR-0005 Appendix M): multi-result call-site desugar.
//
// v1 cannot emit or call multi-result functions (WS D; the P3-M3 F4
// fail-fast precedent). Five operations desugar at call sites —
// context.WithCancel, context.WithCancelCause, context.WithDeadline,
// context.WithTimeout, time.ParseDuration — keeping exact gc 1.27.1
// signatures (so type-checking works) with fail-fast bodies. When one
// of them is the RHS of a 2-target assignment (`:=` or `=`), the
// exporter desugars to the synthetic single-result helpers plus field
// extraction:
//
//	ctx, cancel := context.WithCancel(parent)
//	// →
//	__p5mr_N := context.__withCancel(parent) // *cancelCtx, real Go
//	ctx := __p5mr_N                         // concrete type, no Context()
//	                                        // boxing (afc0dfb amendment:
//	                                        // v1 cannot emit the
//	                                        // multi-result Deadline
//	                                        // method the interface
//	                                        // would require)
//	cancel := __p5mr_N.cancelFunc()          // CancelFunc
//
//	d, err := time.ParseDuration(s)
//	// →
//	__p5mr_N := time.__parseDuration(s)      // durationPair, real Go
//	d := __p5mr_N.d
//	err := __p5mr_N.err
//
// The sixth Appendix M operation, ctx.Deadline(), is NOT callable in M5:
// the afc0dfb amendment removed Deadline from the synthetic Context
// interface (v1 cannot emit multi-result methods, WS D), so an
// interface-typed call is a go/types type error that never reaches the
// exporter (see TestDeadlineNotCallableM5). The mrDeadline
// classification path below is retained defense-in-depth — it keeps the
// __deadlineOf desugar wired for the day WS D lands and Deadline returns
// to the interface (re-test trigger: add a 2-target ctx.Deadline()
// corpus test then). It is currently unreachable from user code: the
// synthetic interface omits Deadline, the concrete types are unexported,
// and a user interface re-adding Deadline fails isDeadlineMethod.
//
// P3-M7 (wave 1) extends Appendix M to the encoding/* synthetic
// packages (mrM7, m7MultiResultFuncs table below): their multi-result
// members keep exact gc signatures with fail-fast bodies, and the real
// implementations live in single-result __-helpers returning
// pair/triple structs. 2-target (and, for ascii85.Decode, 3-target)
// assignments desugar to a helper call plus field extractions; every
// other position fails fast with the named M7 scope error via
// isMultiResultSynthetic. asn1.Unmarshal is the deliberate exception:
// decoding stores through the recipient pointer, which v1 cannot
// express (WS D), so it is not in the table and fails fast everywhere.
//
// Every other syntactic position keeps the fail-fast scope error. The
// helpers are real Go (no new Java nodes); the unexported names are
// resolved by the exporter directly off the synthetic package scope
// (user code can never name them — go/types rejects unexported
// cross-package references — so there is no collision).

// mrKind classifies the Appendix M operations: the 5 reachable ones
// plus mrDeadline, retained defense-in-depth for WS D (see the package
// header — currently unreachable from user code).
type mrKind int

const (
	mrNone         mrKind = iota
	mrWithCancel          // context.WithCancel / context.WithCancelCause
	mrWithDeadline        // context.WithDeadline / context.WithTimeout
	mrDeadline            // ctx.Deadline() (method) — retained, unreachable in M5
	mrParseDur            // time.ParseDuration
	// P3-M7 (wave 1): xml.Decoder.Token / xml.Decoder.RawToken.
	mrXmlToken // xml.Decoder.Token() / xml.Decoder.RawToken()
	// P3-M7 (wave 1): the encoding/* multi-result members (Decode,
	// DecodeString, AppendDecode, Uvarint, Varint, ascii85.Decode,
	// pem.Decode, asn1.Marshal/Unmarshal). One kind for all of them —
	// the pair/triple distinction is data (len(pairFields)), selected
	// from the m7MultiResultFuncs table below.
	mrM7
	// P3-M8 (WS D): a multi-result call on an interface value
	// (r.Read(buf) with r io.Reader). The "helper call" is the
	// ifacemethod dispatch node itself (built by exportIfaceMethodCall);
	// the itab maps the method to the concrete type's pair-returning
	// __-helper (wired via methodTableEntries), so the node yields the
	// pair and field extraction proceeds exactly like mrM7.
	mrIfaceMethod
	// P3-M9 (WS D): a user-defined multi-result function/method
	// transformed to pair-returning form. Unlike mrM7 (which calls a
	// separate __-helper in a synthetic package), the call is to the
	// transformed function itself, which already returns the pair.
	mrUser
	// P3-M9 (WS D): a multi-result function field call (e.g.,
	// c.generate(r) where generate is a field of function type on
	// nistCurve). The selection is FieldVal (not MethodVal). The
	// desugar calls the function value directly and packs the 2
	// results into the pair struct from the nistCurveFuncFields table.
	mrFuncField
	// P3-M9 (WS D): fips140cache.Cache.Get(k, new, check) — the
	// synthetic cache is a no-memoization stub (Get just returns
	// new()), so the desugar inlines the new function call directly:
	// v, err := cache.Get(k, newFn, checkFn) becomes v, err := newFn().
	// This avoids generic helper instantiation complexity.
	mrCacheGet
)

// m7MultiResultFunc describes one desugarable M7 multi-result function:
// the exact gc 1.27.1 multi-result signature is kept for type-checking
// with a fail-fast body, and the real implementation lives in the
// single-result __-helper returning the pair/triple struct.
type m7MultiResultFunc struct {
	pkgPath    string   // synthetic package path
	name       string   // function/method name
	recvType   string   // receiver named type ("" for package-level funcs)
	helper     string   // __-helper name
	pairType   string   // pair/triple struct type name
	pairFields []string // field names in result order (must match the
	// multi-result signature's result order positionally)
}

// m7MultiResultFuncs is the desugar table for the encoding/* packages
// (extended P3-M8 to reflect, P3-M9 to math/bits — the mechanism is
// package-path-keyed, so entries for any synthetic package work).
// Every entry was verified against the synthetic source it names:
// helper name, pair struct name, and field names/order match the
// source's __-helper and pair struct exactly (e.g. base32's AppendDecode
// desugars through base32DecodeStringPair{b, err} — there is no
// base32AppendPair).
var m7MultiResultFuncs = []m7MultiResultFunc{
	// bytes: methods on *Reader (P3-M9 WS D: v1 cannot emit 2-result methods).
	{"bytes", "Read", "Reader", "__bytesReaderRead", "__bytesReadPair", []string{"n", "err"}},
	// bytes: methods on *Buffer (P3-M9 WS D: v1 cannot emit 2-result methods).
	{"bytes", "Write", "Buffer", "__bytesBufferWrite", "__bytesWritePair", []string{"n", "err"}},
	// encoding/base32: methods on *Encoding.
	{"encoding/base32", "Decode", "Encoding", "__base32Decode", "base32DecodePair", []string{"n", "err"}},
	{"encoding/base32", "DecodeString", "Encoding", "__base32DecodeString", "base32DecodeStringPair", []string{"b", "err"}},
	{"encoding/base32", "AppendDecode", "Encoding", "__base32AppendDecode", "base32DecodeStringPair", []string{"b", "err"}},
	// encoding/base64: methods on *Encoding.
	{"encoding/base64", "Decode", "Encoding", "__base64Decode", "base64DecodePair", []string{"n", "err"}},
	{"encoding/base64", "DecodeString", "Encoding", "__base64DecodeString", "base64DecodeStringPair", []string{"b", "err"}},
	{"encoding/base64", "AppendDecode", "Encoding", "__base64AppendDecode", "base64DecodeStringPair", []string{"b", "err"}},
	// encoding/hex: package-level funcs.
	{"encoding/hex", "Decode", "", "__hexDecode", "hexDecodePair", []string{"n", "err"}},
	{"encoding/hex", "DecodeString", "", "__hexDecodeString", "hexDecodeStringPair", []string{"b", "err"}},
	{"encoding/hex", "AppendDecode", "", "__hexAppendDecode", "hexDecodeStringPair", []string{"b", "err"}},
	// encoding/binary: package-level funcs.
	{"encoding/binary", "Uvarint", "", "__binaryUvarint", "binaryUvarintPair", []string{"x", "n"}},
	{"encoding/binary", "Varint", "", "__binaryVarint", "binaryVarintPair", []string{"x", "n"}},
	// encoding/ascii85: package-level func, 3 results.
	{"encoding/ascii85", "Decode", "", "__ascii85Decode", "ascii85DecodeTriple", []string{"ndst", "nsrc", "err"}},
	// encoding/pem: package-level func.
	{"encoding/pem", "Decode", "", "__pemDecode", "pemDecodePair", []string{"p", "rest"}},
	// encoding/asn1: package-level func. The __-helper implements the
	// scalar cases in real Go and fails fast (needs reflect) for composite
	// types — the desugar applies, the reflect limit moves into the helper.
	// (Unmarshal is NOT desugared: decoding stores through the recipient
	// pointer, which v1 cannot express — WS D — so it fails fast at every
	// call site via isMultiResultSynthetic.)
	{"encoding/asn1", "Marshal", "", "__asn1Marshal", "asn1MarshalPair", []string{"b", "err"}},
	// strconv: package-level funcs. The Pub helpers are the public-shape
	// single-result forms (gc-compatible error mapping via toError);
	// ParseFloat's helper is __parseFloat (no Pub variant — its error
	// mapping is internal to __parseFloat).
	{"strconv", "ParseBool", "", "__parseBoolPub", "parseBoolPair", []string{"v", "err"}},
	{"strconv", "ParseInt", "", "__parseIntPub", "parseIntPair", []string{"v", "err"}},
	{"strconv", "ParseUint", "", "__parseUintPub", "parseUintPair", []string{"v", "err"}},
	{"strconv", "Atoi", "", "__atoiPub", "atoiPair", []string{"v", "err"}},
	{"strconv", "ParseFloat", "", "__parseFloat", "parseFloatPair", []string{"v", "err"}},
	{"strconv", "ParseComplex", "", "__parseComplexPub", "parseComplexPair", []string{"v", "err"}},
	{"strconv", "Unquote", "", "__unquotePub", "unquotePair", []string{"v", "err"}},
	{"strconv", "QuotedPrefix", "", "__quotedPrefix", "quotedPrefixPair", []string{"v", "err"}},
	// UnquoteChar is the 4-result entry (value, multibyte, tail, err) —
	// the desugar supports N-ary via pairFields.
	{"strconv", "UnquoteChar", "", "__unquoteChar", "unquoteCharPair", []string{"value", "multibyte", "tail", "err"}},
	// encoding/json: package-level funcs plus methods on *Decoder,
	// Number, and RawMessage (prependRecv passes the receiver as the
	// helper's first argument).
	{"encoding/json", "Marshal", "", "__marshal", "marshalPair", []string{"data", "err"}},
	{"encoding/json", "MarshalIndent", "", "__marshalIndent", "marshalIndentPair", []string{"data", "err"}},
	{"encoding/json", "Token", "Decoder", "__decoderToken", "decoderTokenPair", []string{"tok", "err"}},
	{"encoding/json", "Float64", "Number", "__numberFloat64", "numberFloat64Pair", []string{"f", "err"}},
	{"encoding/json", "Int64", "Number", "__numberInt64", "numberInt64Pair", []string{"i", "err"}},
	{"encoding/json", "MarshalJSON", "RawMessage", "__rawMessageMarshalJSON", "rawMessageMarshalJSONPair", []string{"data", "err"}},
	// P3-M8 (wave 2): synthetic io (synth_io.go). Only the members with
	// real single-result __-helpers are desugared — see the SCOPE NOTE in
	// the patch header; everything else fails at export via
	// isMultiResultSynthetic. prependRecv applies (all have receivers).
	{"io", "Pipe", "", "__pipe", "pipePair", []string{"r", "w"}},
	{"io", "Read", "PipeReader", "__pipeReaderRead", "pipeReaderReadPair", []string{"n", "err"}},
	{"io", "Write", "PipeWriter", "__pipeWriterWrite", "pipeWriterWritePair", []string{"n", "err"}},
	{"io", "Seek", "SectionReader", "__sectionReaderSeek", "sectionReaderSeekPair", []string{"n", "err"}},
	{"io", "Outer", "SectionReader", "__sectionReaderOuter", "sectionReaderOuterTriple", []string{"r", "off", "n"}},
	{"io", "Seek", "OffsetWriter", "__offsetWriterSeek", "offsetWriterSeekPair", []string{"n", "err"}},
	// P3-M8 (wave 2): strings.Reader methods discharged from their M7
	// fail-fast (patch 06, synth_strings.go). They need only io.EOF and
	// the io.Seek* consts — no interface dispatch — so their __-helpers
	// are real. strings.Reader.WriteTo stays fail-fast (needs w.Write).
	{"strings", "Read", "Reader", "__stringsReaderRead", "stringsReaderReadPair", []string{"n", "err"}},
	{"strings", "ReadAt", "Reader", "__stringsReaderReadAt", "stringsReaderReadAtPair", []string{"n", "err"}},
	{"strings", "ReadByte", "Reader", "__stringsReaderReadByte", "stringsReaderReadBytePair", []string{"b", "err"}},
	{"strings", "ReadRune", "Reader", "__stringsReaderReadRune", "stringsReaderReadRuneTriple", []string{"ch", "size", "err"}},
	{"strings", "Seek", "Reader", "__stringsReaderSeek", "stringsReaderSeekPair", []string{"n", "err"}},
	// P3-M8 (wave 2): synthetic syscall package (synth_syscall.go).
	// The multi-result members keep exact gc signatures with fail-fast
	// bodies; the __-helpers return pair structs (real implementations
	// for Getenv/Getgroups/Getwd, exact-message panics for the rest).
	{"syscall", "Open", "", "__open", "intErrorPair", []string{"v", "err"}},
	{"syscall", "Read", "", "__read", "intErrorPair", []string{"v", "err"}},
	{"syscall", "Write", "", "__write", "intErrorPair", []string{"v", "err"}},
	{"syscall", "Pread", "", "__pread", "intErrorPair", []string{"v", "err"}},
	{"syscall", "Pwrite", "", "__pwrite", "intErrorPair", []string{"v", "err"}},
	{"syscall", "Seek", "", "__seek", "int64ErrorPair", []string{"v", "err"}},
	{"syscall", "Readlink", "", "__readlink", "intErrorPair", []string{"v", "err"}},
	{"syscall", "Mmap", "", "__mmap", "bytesErrorPair", []string{"v", "err"}},
	{"syscall", "StartProcess", "", "__startprocess", "startProcessTriple", []string{"pid", "handle", "err"}},
	{"syscall", "ForkExec", "", "__forkexec", "intErrorPair", []string{"v", "err"}},
	{"syscall", "Wait4", "", "__wait4", "intErrorPair", []string{"v", "err"}},
	{"syscall", "Socket", "", "__socket", "intErrorPair", []string{"v", "err"}},
	{"syscall", "Accept", "", "__accept", "acceptTriple", []string{"fd", "sa", "err"}},
	{"syscall", "GetsockoptInt", "", "__getsockoptint", "intErrorPair", []string{"v", "err"}},
	{"syscall", "Recvmsg", "", "__recvmsg", "recvmsgTuple", []string{"n", "oobn", "recvflags", "from", "err"}},
	{"syscall", "EpollCreate", "", "__epollcreate", "intErrorPair", []string{"v", "err"}},
	{"syscall", "EpollCreate1", "", "__epollcreate1", "intErrorPair", []string{"v", "err"}},
	{"syscall", "EpollWait", "", "__epollwait", "intErrorPair", []string{"v", "err"}},
	{"syscall", "InotifyInit", "", "__inotifyinit", "intErrorPair", []string{"v", "err"}},
	{"syscall", "InotifyAddWatch", "", "__inotifyaddwatch", "intErrorPair", []string{"v", "err"}},
	{"syscall", "InotifyRmWatch", "", "__inotifyrmwatch", "intErrorPair", []string{"v", "err"}},
	{"syscall", "Getenv", "", "__getenv", "getenvPair", []string{"value", "found"}},
	{"syscall", "Getgroups", "", "__getgroups", "gidsErrorPair", []string{"gids", "err"}},
	{"syscall", "Getwd", "", "__getwd", "stringErrorPair", []string{"v", "err"}},
	{"syscall", "ByteSliceFromString", "", "__byteSliceFromString", "bytesErrorPair", []string{"v", "err"}},
	{"syscall", "BytePtrFromString", "", "__bytePtrFromString", "bytePtrErrorPair", []string{"p", "err"}},
	{"syscall", "Syscall", "", "__syscall", "syscallTriple", []string{"r1", "r2", "err"}},
	{"syscall", "RawSyscall", "", "__rawsyscall", "syscallTriple", []string{"r1", "r2", "err"}},
	{"syscall", "Syscall6", "", "__syscall6", "syscallTriple", []string{"r1", "r2", "err"}},
	{"syscall", "RawSyscall6", "", "__rawsyscall6", "syscallTriple", []string{"r1", "r2", "err"}},
	// P3-M8 (wave 2, reflect): multi-result members desugared via the
	// single-result __-helpers. Select is package-level; the Value methods
	// are on the Value struct; the Type methods are on rtype but usually
	// called via the Type interface (the classify function retries with
	// recvType="rtype" for reflect interface methods); StructTag.Lookup is
	// on the named string type StructTag.
	{"reflect", "Select", "", "__reflectSelect", "reflectSelectTriple", []string{"chosen", "recv", "recvOK"}},
	{"reflect", "FieldByIndexErr", "Value", "__reflectFieldByIndexErr", "reflectValuePair", []string{"v", "err"}},
	{"reflect", "TryRecv", "Value", "__reflectTryRecv", "reflectValueBoolPair", []string{"x", "ok"}},
	{"reflect", "MethodByName", "rtype", "__reflectTMethodByName", "reflectMethodPair", []string{"m", "ok"}},
	{"reflect", "FieldByName", "rtype", "__reflectTFieldByName", "reflectStructFieldPair", []string{"f", "ok"}},
	{"reflect", "FieldByNameFunc", "rtype", "__reflectTFieldByNameFunc", "reflectStructFieldPair", []string{"f", "ok"}},
	{"reflect", "Lookup", "StructTag", "__reflect_taglookup", "reflectTagPair", []string{"value", "ok"}},
	// NOTE: the top-level "encoding" package has no functions (only the six
	// marshaling interfaces), so it needs no desugar entry. Its interface
	// methods are multi-result and fail fast via isMultiResultSynthetic.
	// P3-M9 (tier1c): math/bits multi-result functions (Add/Sub/Mul/Div
	// + 32/64 variants). Pair field names match the gc result names in
	// order (see synth_mathbits.go).
	{"math/bits", "Add", "", "__bitsAdd", "bitsAddPair", []string{"sum", "carryOut"}},
	{"math/bits", "Add32", "", "__bitsAdd32", "bitsAdd32Pair", []string{"sum", "carryOut"}},
	{"math/bits", "Add64", "", "__bitsAdd64", "bitsAdd64Pair", []string{"sum", "carryOut"}},
	{"math/bits", "Sub", "", "__bitsSub", "bitsSubPair", []string{"diff", "borrowOut"}},
	{"math/bits", "Sub32", "", "__bitsSub32", "bitsSub32Pair", []string{"diff", "borrowOut"}},
	{"math/bits", "Sub64", "", "__bitsSub64", "bitsSub64Pair", []string{"diff", "borrowOut"}},
	{"math/bits", "Mul", "", "__bitsMul", "bitsMulPair", []string{"hi", "lo"}},
	{"math/bits", "Mul32", "", "__bitsMul32", "bitsMul32Pair", []string{"hi", "lo"}},
	{"math/bits", "Mul64", "", "__bitsMul64", "bitsMul64Pair", []string{"hi", "lo"}},
	{"math/bits", "Div", "", "__bitsDiv", "bitsDivPair", []string{"quo", "rem"}},
	{"math/bits", "Div32", "", "__bitsDiv32", "bitsDiv32Pair", []string{"quo", "rem"}},
	{"math/bits", "Div64", "", "__bitsDiv64", "bitsDiv64Pair", []string{"quo", "rem"}},
	// P3-M9 (tier1c): math.Sincos desugars to the existing __sincos
	// helper (used internally by the Bessel functions; Sin(x)/Cos(x)
	// each bit-exact, and Sincos == (Sin, Cos) bit-wise: 0 mismatches
	// over 100010 adversarial+random inputs vs gc 1.27.1). Needed by
	// the synthetic math/cmplx package (Rect/Exp/Pow/Sin/Cos).
	{"math", "Sincos", "", "__sincos", "__sincosPair", []string{"sin", "cos"}},
	{"math", "Frexp", "", "__frexp", "__frexpPair", []string{"frac", "exp"}}, // P3-M9 math/big
	// P3-M9 (tier1c): math/cmplx.Polar desugars to the __cmplxPolar helper
	// (Appendix M; pair fields match the gc result names in order).
	{"math/cmplx", "Polar", "", "__cmplxPolar", "cmplxPolarPair", []string{"r", "theta"}},
	// P3-M9 (wave 3, math/big): 47 multi-result funcs/methods. Each has a
	{"math/big", "AppendText", "Float", "__mrFloat_AppendText", "__mrFloat_AppendTextPair", []string{"v0", "v1"}},
	{"math/big", "AppendText", "Int", "__mrInt_AppendText", "__mrInt_AppendTextPair", []string{"text", "err"}},
	{"math/big", "AppendText", "Rat", "__mrRat_AppendText", "__mrRat_AppendTextPair", []string{"v0", "v1"}},
	{"math/big", "DivMod", "Int", "__mrInt_DivMod", "__mrInt_DivModPair", []string{"v0", "v1"}},
	{"math/big", "Divide", "Int", "__mrInt_Divide", "__mrInt_DividePair", []string{"v0", "v1"}},
	{"math/big", "Float32", "Float", "__mrFloat_Float32", "__mrFloat_Float32Pair", []string{"v0", "v1"}},
	{"math/big", "Float32", "Rat", "__mrRat_Float32", "__mrRat_Float32Pair", []string{"f", "exact"}},
	{"math/big", "Float64", "Float", "__mrFloat_Float64", "__mrFloat_Float64Pair", []string{"v0", "v1"}},
	{"math/big", "Float64", "Int", "__mrInt_Float64", "__mrInt_Float64Pair", []string{"v0", "v1"}},
	{"math/big", "Float64", "Rat", "__mrRat_Float64", "__mrRat_Float64Pair", []string{"f", "exact"}},
	{"math/big", "FloatPrec", "Rat", "__mrRat_FloatPrec", "__mrRat_FloatPrecPair", []string{"n", "exact"}},
	{"math/big", "GobEncode", "Float", "__mrFloat_GobEncode", "__mrFloat_GobEncodePair", []string{"v0", "v1"}},
	{"math/big", "GobEncode", "Int", "__mrInt_GobEncode", "__mrInt_GobEncodePair", []string{"v0", "v1"}},
	{"math/big", "GobEncode", "Rat", "__mrRat_GobEncode", "__mrRat_GobEncodePair", []string{"v0", "v1"}},
	{"math/big", "Int", "Float", "__mrFloat_Int", "__mrFloat_IntPair", []string{"v0", "v1"}},
	{"math/big", "Int64", "Float", "__mrFloat_Int64", "__mrFloat_Int64Pair", []string{"v0", "v1"}},
	{"math/big", "MarshalJSON", "Int", "__mrInt_MarshalJSON", "__mrInt_MarshalJSONPair", []string{"v0", "v1"}},
	{"math/big", "MarshalText", "Float", "__mrFloat_MarshalText", "__mrFloat_MarshalTextPair", []string{"text", "err"}},
	{"math/big", "MarshalText", "Int", "__mrInt_MarshalText", "__mrInt_MarshalTextPair", []string{"text", "err"}},
	{"math/big", "MarshalText", "Rat", "__mrRat_MarshalText", "__mrRat_MarshalTextPair", []string{"text", "err"}},
	{"math/big", "Parse", "Float", "__mrFloat_Parse", "__mrFloat_ParsePair", []string{"f", "b", "err"}},
	{"math/big", "ParseFloat", "", "__mrParseFloat", "__mrParseFloatPair", []string{"f", "b", "err"}},
	{"math/big", "QuoRem", "Int", "__mrInt_QuoRem", "__mrInt_QuoRemPair", []string{"v0", "v1"}},
	{"math/big", "Rat", "Float", "__mrFloat_Rat", "__mrFloat_RatPair", []string{"v0", "v1"}},
	{"math/big", "ReadByte", "byteReader", "__mrbyteReader_ReadByte", "__mrbyteReader_ReadBytePair", []string{"v0", "v1"}},
	{"math/big", "SetString", "Float", "__mrFloat_SetString", "__mrFloat_SetStringPair", []string{"v0", "v1"}},
	{"math/big", "SetString", "Int", "__mrInt_SetString", "__mrInt_SetStringPair", []string{"v0", "v1"}},
	{"math/big", "SetString", "Rat", "__mrRat_SetString", "__mrRat_SetStringPair", []string{"v0", "v1"}},
	{"math/big", "Uint64", "Float", "__mrFloat_Uint64", "__mrFloat_Uint64Pair", []string{"v0", "v1"}},
	{"math/big", "div", "nat", "__mrnat_div", "__mrnat_divPair", []string{"q", "r"}},
	{"math/big", "divLarge", "nat", "__mrnat_divLarge", "__mrnat_divLargePair", []string{"q", "r"}},
	{"math/big", "divW", "nat", "__mrnat_divW", "__mrnat_divWPair", []string{"q", "r"}},
	{"math/big", "divWW", "", "__mrdivWW", "__mrdivWWPair", []string{"q", "r"}},
	{"math/big", "euclidUpdate", "", "__mreuclidUpdate", "__mreuclidUpdatePair", []string{"nA", "nB", "nr", "nUa", "nUb"}},
	{"math/big", "isPow2", "nat", "__mrnat_isPow2", "__mrnat_isPow2Pair", []string{"v0", "v1"}},
	{"math/big", "lehmerSimulate", "", "__mrlehmerSimulate", "__mrlehmerSimulatePair", []string{"u0", "u1", "v0", "v1", "even"}},
	{"math/big", "maxPow", "", "__mrmaxPow", "__mrmaxPowPair", []string{"p", "n"}},
	{"math/big", "mulAddWWW_g", "", "__mrmulAddWWW_g", "__mrmulAddWWW_gPair", []string{"z1", "z0"}},
	{"math/big", "mulWW", "", "__mrmulWW", "__mrmulWWPair", []string{"z1", "z0"}},
	{"math/big", "quotToFloat32", "", "__mrquotToFloat32", "__mrquotToFloat32Pair", []string{"f", "exact"}},
	{"math/big", "quotToFloat64", "", "__mrquotToFloat64", "__mrquotToFloat64Pair", []string{"f", "exact"}},
	{"math/big", "scan", "Float", "__mrFloat_scan", "__mrFloat_scanPair", []string{"f", "b", "err"}},
	{"math/big", "scan", "Int", "__mrInt_scan", "__mrInt_scanPair", []string{"v0", "v1", "v2"}},
	{"math/big", "scan", "nat", "__mrnat_scan", "__mrnat_scanPair", []string{"res", "b", "count", "err"}},
	{"math/big", "scanExponent", "", "__mrscanExponent", "__mrscanExponentPair", []string{"exp", "base", "err"}},
	{"math/big", "scanSign", "", "__mrscanSign", "__mrscanSignPair", []string{"neg", "err"}},
	{"math/big", "setFromScanner", "Int", "__mrInt_setFromScanner", "__mrInt_setFromScannerPair", []string{"v0", "v1"}},
	// P3-M9 (tier1c): bufio multi-result methods (Appendix M; pair fields
	// match the gc result names in order; see synth_bufio.go).
	{"bufio", "Read", "Reader", "__bufioReaderRead", "__bufioReaderReadPair", []string{"n", "err"}},
	{"bufio", "Peek", "Reader", "__bufioReaderPeek", "__bufioReaderPeekPair", []string{"b", "err"}},
	{"bufio", "Discard", "Reader", "__bufioReaderDiscard", "__bufioReaderDiscardPair", []string{"discarded", "err"}},
	{"bufio", "ReadByte", "Reader", "__bufioReaderReadByte", "__bufioReaderReadBytePair", []string{"c", "err"}},
	{"bufio", "ReadSlice", "Reader", "__bufioReaderReadSlice", "__bufioReaderReadSlicePair", []string{"line", "err"}},
	{"bufio", "ReadBytes", "Reader", "__bufioReaderReadBytes", "__bufioReaderReadBytesPair", []string{"b", "err"}},
	{"bufio", "ReadString", "Reader", "__bufioReaderReadString", "__bufioReaderReadStringPair", []string{"s", "err"}},
	{"bufio", "Write", "Writer", "__bufioWriterWrite", "__bufioWriterWritePair", []string{"n", "err"}},
	{"bufio", "WriteString", "Writer", "__bufioWriterWriteString", "__bufioWriterWriteStringPair", []string{"n", "err"}},
	// P3-M9 (tier-5b): bufio.ScanLines (Appendix M; the 3-result split
	// function — package-level, so recvType is ""; see synth_bufio.go).
	{"bufio", "ScanLines", "", "__bufioScanLines", "__bufioScanLinesPair", []string{"advance", "token", "err"}},
	// P3-M9 (tier1c): math/rand.Read (Appendix M).
	{"math/rand", "Read", "Rand", "__randRead", "__randReadPair", []string{"n", "err"}},
	// P3-M9 (tier1c): path.Split (Appendix M).
	{"path", "Split", "", "__pathSplit", "pathSplitPair", []string{"dir", "file"}},
	// P3-M9 (tier-5: compress): synthetic compress/compress/flate (Appendix M).
	{"compress/flate", "findMatch", "compressor", "__flateCompressorfindMatch", "__flateCompressorfindMatchPair", []string{"length", "offset", "ok"}},
	{"compress/flate", "tryBetterMatchAtEnd", "compressor", "__flateCompressortryBetterMatchAtEnd", "__flateCompressortryBetterMatchAtEndPair", []string{"newLen", "newOff"}},
	{"compress/flate", "write", "compressor", "__flateCompressorwrite", "__flateCompressorwritePair", []string{"n", "err"}},
	{"compress/flate", "NewWriter", "", "__flateNewWriter", "__flateNewWriterPair", []string{"w", "err"}},
	{"compress/flate", "NewWriterDict", "", "__flateNewWriterDict", "__flateNewWriterDictPair", []string{"w", "err"}},
	{"compress/flate", "Write", "Writer", "__flateWriterWrite", "__flateWriterWritePair", []string{"n", "err"}},
	{"compress/flate", "Read", "decompressor", "__flateDecompressorRead", "__flateDecompressorReadPair", []string{"n", "err"}},
	{"compress/flate", "huffSym", "decompressor", "__flateDecompressorhuffSym", "__flateDecompressorhuffSymPair", []string{"sym", "err"}},
	{"compress/flate", "headerSize", "huffmanBitWriter", "__flateHuffmanBitWriterheaderSize", "__flateHuffmanBitWriterheaderSizePair", []string{"size", "numCodegens"}},
	{"compress/flate", "dynamicSize", "huffmanBitWriter", "__flateHuffmanBitWriterdynamicSize", "__flateHuffmanBitWriterdynamicSizePair", []string{"size", "numCodegens"}},
	{"compress/flate", "storedSize", "huffmanBitWriter", "__flateHuffmanBitWriterstoredSize", "__flateHuffmanBitWriterstoredSizePair", []string{"size", "ok"}},
	{"compress/flate", "indexTokens", "huffmanBitWriter", "__flateHuffmanBitWriterindexTokens", "__flateHuffmanBitWriterindexTokensPair", []string{"numLiterals", "numOffsets"}},
	// P3-M9 (tier-5: compress): synthetic compress/compress/gzip (Appendix M).
	{"compress/gzip", "NewReader", "", "__gzipNewReader", "__gzipNewReaderPair", []string{"r", "err"}},
	{"compress/gzip", "readString", "Reader", "__gzipReaderreadString", "__gzipReaderreadStringPair", []string{"s", "err"}},
	{"compress/gzip", "readHeader", "Reader", "__gzipReaderreadHeader", "__gzipReaderreadHeaderPair", []string{"hdr", "err"}},
	{"compress/gzip", "Read", "Reader", "__gzipReaderRead", "__gzipReaderReadPair", []string{"n", "err"}},
	{"compress/gzip", "NewWriterLevel", "", "__gzipNewWriterLevel", "__gzipNewWriterLevelPair", []string{"w", "err"}},
	{"compress/gzip", "Write", "Writer", "__gzipWriterWrite", "__gzipWriterWritePair", []string{"n", "err"}},
	// P3-M9 (tier-5: compress): synthetic compress/compress/zlib (Appendix M).
	{"compress/zlib", "NewReader", "", "__zlibNewReader", "__zlibNewReaderPair", []string{"r", "err"}},
	{"compress/zlib", "NewReaderDict", "", "__zlibNewReaderDict", "__zlibNewReaderDictPair", []string{"r", "err"}},
	{"compress/zlib", "Read", "reader", "__zlibReaderRead", "__zlibReaderReadPair", []string{"n", "err"}},
	{"compress/zlib", "NewWriterLevel", "", "__zlibNewWriterLevel", "__zlibNewWriterLevelPair", []string{"w", "err"}},
	{"compress/zlib", "NewWriterLevelDict", "", "__zlibNewWriterLevelDict", "__zlibNewWriterLevelDictPair", []string{"w", "err"}},
	{"compress/zlib", "Write", "Writer", "__zlibWriterWrite", "__zlibWriterWritePair", []string{"n", "err"}},
	// P3-M9 (tier-5: compress): synthetic compress/compress/lzw (Appendix M).
	{"compress/lzw", "readLSB", "Reader", "__lzwReaderreadLSB", "__lzwReaderreadLSBPair", []string{"code", "err"}},
	{"compress/lzw", "readMSB", "Reader", "__lzwReaderreadMSB", "__lzwReaderreadMSBPair", []string{"code", "err"}},
	{"compress/lzw", "Read", "Reader", "__lzwReaderRead", "__lzwReaderReadPair", []string{"n", "err"}},
	{"compress/lzw", "Write", "Writer", "__lzwWriterWrite", "__lzwWriterWritePair", []string{"n", "err"}},
	// P3-M9 (tier-5: compress): synthetic compress/compress/bzip2 (Appendix M).
	{"compress/bzip2", "Read", "reader", "__bzip2ReaderRead", "__bzip2ReaderReadPair", []string{"n", "err"}},
	{"compress/bzip2", "read", "reader", "__bzip2Readerread", "__bzip2ReaderreadPair", []string{"n", "err"}},
	{"compress/bzip2", "newHuffmanTree", "", "__bzip2NewHuffmanTree", "__bzip2NewHuffmanTreePair", []string{"t", "err"}},
	{"compress/bzip2", "buildHuffmanNode", "", "__bzip2BuildHuffmanNode", "__bzip2BuildHuffmanNodePair", []string{"nodeIndex", "err"}},
	// P3-M9 (tier-5: archive/mime): synthetic archive/* and mime/* (Appendix M).
{"archive/tar", "allowedFormats", "Header", "__tarHeaderallowedFormats", "__tarHeaderallowedFormatsPair", []string{"format", "paxHdrs", "err"}},
    {"archive/tar", "FileInfoHeader", "", "__tarFileInfoHeader", "__tarFileInfoHeaderPair", []string{"f0", "f1"}},
    {"archive/tar", "computeChecksum", "block", "__tarblockcomputeChecksum", "__tarblockcomputeChecksumPair", []string{"f0", "signed"}},
    {"archive/tar", "Next", "Reader", "__tarReaderNext", "__tarReaderNextPair", []string{"f0", "f1"}},
    {"archive/tar", "next", "Reader", "__tarReadernext", "__tarReadernextPair", []string{"f0", "f1"}},
    {"archive/tar", "readGNUSparsePAXHeaders", "Reader", "__tarReaderreadGNUSparsePAXHeaders", "__tarReaderreadGNUSparsePAXHeadersPair", []string{"f0", "f1"}},
    {"archive/tar", "parsePAX", "", "__tarparsePAX", "__tarparsePAXPair", []string{"f0", "f1"}},
    {"archive/tar", "readHeader", "Reader", "__tarReaderreadHeader", "__tarReaderreadHeaderPair", []string{"f0", "f1", "f2"}},
    {"archive/tar", "readOldGNUSparseMap", "Reader", "__tarReaderreadOldGNUSparseMap", "__tarReaderreadOldGNUSparseMapPair", []string{"f0", "f1"}},
    {"archive/tar", "readGNUSparseMap1x0", "", "__tarreadGNUSparseMap1x0", "__tarreadGNUSparseMap1x0Pair", []string{"f0", "f1"}},
    {"archive/tar", "readGNUSparseMap0x1", "", "__tarreadGNUSparseMap0x1", "__tarreadGNUSparseMap0x1Pair", []string{"f0", "f1"}},
    {"archive/tar", "appendSparseEntry", "", "__tarappendSparseEntry", "__tarappendSparseEntryPair", []string{"f0", "f1"}},
    {"archive/tar", "Read", "Reader", "__tarReaderRead", "__tarReaderReadPair", []string{"f0", "f1"}},
    {"archive/tar", "writeTo", "Reader", "__tarReaderwriteTo", "__tarReaderwriteToPair", []string{"f0", "f1"}},
    {"archive/tar", "Read", "regFileReader", "__tarregFileReaderRead", "__tarregFileReaderReadPair", []string{"n", "err"}},
    {"archive/tar", "WriteTo", "regFileReader", "__tarregFileReaderWriteTo", "__tarregFileReaderWriteToPair", []string{"f0", "f1"}},
    {"archive/tar", "Read", "sparseFileReader", "__tarsparseFileReaderRead", "__tarsparseFileReaderReadPair", []string{"n", "err"}},
    {"archive/tar", "WriteTo", "sparseFileReader", "__tarsparseFileReaderWriteTo", "__tarsparseFileReaderWriteToPair", []string{"n", "err"}},
    {"archive/tar", "mustReadFull", "", "__tarmustReadFull", "__tarmustReadFullPair", []string{"f0", "f1"}},
    {"archive/tar", "tryReadFull", "", "__tartryReadFull", "__tartryReadFullPair", []string{"n", "err"}},
    {"archive/tar", "readSpecialFile", "", "__tarreadSpecialFile", "__tarreadSpecialFilePair", []string{"f0", "f1"}},
    {"archive/tar", "splitUSTARPath", "", "__tarsplitUSTARPath", "__tarsplitUSTARPathPair", []string{"f0", "suffix", "ok"}},
    {"archive/tar", "Write", "Writer", "__tarWriterWrite", "__tarWriterWritePair", []string{"f0", "f1"}},
    {"archive/tar", "readFrom", "Writer", "__tarWriterreadFrom", "__tarWriterreadFromPair", []string{"f0", "f1"}},
    {"archive/tar", "Write", "regFileWriter", "__tarregFileWriterWrite", "__tarregFileWriterWritePair", []string{"n", "err"}},
    {"archive/tar", "ReadFrom", "regFileWriter", "__tarregFileWriterReadFrom", "__tarregFileWriterReadFromPair", []string{"f0", "f1"}},
    {"archive/tar", "Write", "sparseFileWriter", "__tarsparseFileWriterWrite", "__tarsparseFileWriterWritePair", []string{"n", "err"}},
    {"archive/tar", "ReadFrom", "sparseFileWriter", "__tarsparseFileWriterReadFrom", "__tarsparseFileWriterReadFromPair", []string{"n", "err"}},
    {"archive/tar", "parsePAXTime", "", "__tarparsePAXTime", "__tarparsePAXTimePair", []string{"f0", "f1"}},
    {"archive/tar", "parsePAXRecord", "", "__tarparsePAXRecord", "__tarparsePAXRecordPair", []string{"f0", "f1", "r", "err"}},
    {"archive/tar", "formatPAXRecord", "", "__tarformatPAXRecord", "__tarformatPAXRecordPair", []string{"f0", "f1"}},
    {"archive/zip", "OpenReader", "", "__zipOpenReader", "__zipOpenReaderPair", []string{"f0", "f1"}},
    {"archive/zip", "NewReader", "", "__zipNewReader", "__zipNewReaderPair", []string{"f0", "f1"}},
    {"archive/zip", "DataOffset", "File", "__zipFileDataOffset", "__zipFileDataOffsetPair", []string{"offset", "err"}},
    {"archive/zip", "Open", "File", "__zipFileOpen", "__zipFileOpenPair", []string{"f0", "f1"}},
    {"archive/zip", "OpenRaw", "File", "__zipFileOpenRaw", "__zipFileOpenRawPair", []string{"f0", "f1"}},
    {"archive/zip", "Read", "dirReader", "__zipdirReaderRead", "__zipdirReaderReadPair", []string{"f0", "f1"}},
    {"archive/zip", "Stat", "checksumReader", "__zipchecksumReaderStat", "__zipchecksumReaderStatPair", []string{"f0", "f1"}},
    {"archive/zip", "Read", "checksumReader", "__zipchecksumReaderRead", "__zipchecksumReaderReadPair", []string{"n", "err"}},
    {"archive/zip", "Close", "checksumReader", "__zipchecksumReaderClose", "__zipchecksumReaderClosePair", []string{"f0", "f1"}},
    {"archive/zip", "readDirectoryEnd", "", "__zipreadDirectoryEnd", "__zipreadDirectoryEndPair", []string{"dir", "baseOffset", "err"}},
    {"archive/zip", "findDirectory64End", "", "__zipfindDirectory64End", "__zipfindDirectory64EndPair", []string{"f0", "f1"}},
    {"archive/zip", "stat", "fileListEntry", "__zipfileListEntrystat", "__zipfileListEntrystatPair", []string{"f0", "f1"}},
    {"archive/zip", "Open", "Reader", "__zipReaderOpen", "__zipReaderOpenPair", []string{"f0", "f1"}},
    {"archive/zip", "split", "", "__zipsplit", "__zipsplitPair", []string{"f0", "elem", "isDir"}},
    {"archive/zip", "Close", "openDir", "__zipopenDirClose", "__zipopenDirClosePair", []string{"f0", "f1"}},
    {"archive/zip", "ReadDir", "openDir", "__zipopenDirReadDir", "__zipopenDirReadDirPair", []string{"f0", "f1"}},
    {"archive/zip", "Create", "Writer", "__zipWriterCreate", "__zipWriterCreatePair", []string{"f0", "f1"}},
    {"archive/zip", "detectUTF8", "", "__zipdetectUTF8", "__zipdetectUTF8Pair", []string{"f0", "require"}},
    {"archive/zip", "CreateHeader", "Writer", "__zipWriterCreateHeader", "__zipWriterCreateHeaderPair", []string{"f0", "f1"}},
    {"archive/zip", "CreateRaw", "Writer", "__zipWriterCreateRaw", "__zipWriterCreateRawPair", []string{"f0", "f1"}},
    {"archive/zip", "Write", "fileWriter", "__zipfileWriterWrite", "__zipfileWriterWritePair", []string{"f0", "f1"}},
    {"archive/zip", "Write", "countWriter", "__zipcountWriterWrite", "__zipcountWriterWritePair", []string{"f0", "f1"}},
    {"archive/zip", "FileInfoHeader", "", "__zipFileInfoHeader", "__zipFileInfoHeaderPair", []string{"f0", "f1"}},
    {"archive/zip", "timeToMsDosTime", "", "__ziptimeToMsDosTime", "__ziptimeToMsDosTimePair", []string{"fDate", "fTime"}},
    {"archive/zip", "Write", "pooledFlateWriter", "__zippooledFlateWriterWrite", "__zippooledFlateWriterWritePair", []string{"n", "err"}},
    {"archive/zip", "Read", "pooledFlateReader", "__zippooledFlateReaderRead", "__zippooledFlateReaderReadPair", []string{"n", "err"}},
    {"mime", "ParseMediaType", "", "__mimeParseMediaType", "__mimeParseMediaTypePair", []string{"mediatype", "params", "err"}},
    {"mime", "decode2231Enc", "", "__mimedecode2231Enc", "__mimedecode2231EncPair", []string{"f0", "f1"}},
    {"mime", "consumeToken", "", "__mimeconsumeToken", "__mimeconsumeTokenPair", []string{"f0", "rest"}},
    {"mime", "consumeValue", "", "__mimeconsumeValue", "__mimeconsumeValuePair", []string{"f0", "rest"}},
    {"mime", "consumeMediaParam", "", "__mimeconsumeMediaParam", "__mimeconsumeMediaParamPair", []string{"f0", "f1", "rest"}},
    {"mime", "percentHexUnescape", "", "__mimepercentHexUnescape", "__mimepercentHexUnescapePair", []string{"f0", "f1"}},
    {"mime", "Decode", "WordDecoder", "__mimeWordDecoderDecode", "__mimeWordDecoderDecodePair", []string{"f0", "f1"}},
    {"mime", "DecodeHeader", "WordDecoder", "__mimeWordDecoderDecodeHeader", "__mimeWordDecoderDecodeHeaderPair", []string{"f0", "f1"}},
    {"mime", "decode", "", "__mimedecode", "__mimedecodePair", []string{"f0", "f1"}},
    {"mime", "qDecode", "", "__mimeqDecode", "__mimeqDecodePair", []string{"f0", "f1"}},
    {"mime", "readHexByte", "", "__mimereadHexByte", "__mimereadHexBytePair", []string{"f0", "f1"}},
    {"mime", "fromHex", "", "__mimefromHex", "__mimefromHexPair", []string{"f0", "f1"}},
    {"mime/multipart", "Read", "stickyErrorReader", "__mpstickyErrorReaderRead", "__mpstickyErrorReaderReadPair", []string{"n", "_"}},
    {"mime/multipart", "newPart", "", "__mpnewPart", "__mpnewPartPair", []string{"f0", "f1"}},
    {"mime/multipart", "Read", "Part", "__mpPartRead", "__mpPartReadPair", []string{"n", "err"}},
    {"mime/multipart", "Read", "partReader", "__mppartReaderRead", "__mppartReaderReadPair", []string{"f0", "f1"}},
    {"mime/multipart", "scanUntilBoundary", "", "__mpscanUntilBoundary", "__mpscanUntilBoundaryPair", []string{"f0", "f1"}},
    {"mime/multipart", "NextPart", "Reader", "__mpReaderNextPart", "__mpReaderNextPartPair", []string{"f0", "f1"}},
    {"mime/multipart", "NextRawPart", "Reader", "__mpReaderNextRawPart", "__mpReaderNextRawPartPair", []string{"f0", "f1"}},
    {"mime/multipart", "nextPart", "Reader", "__mpReadernextPart", "__mpReadernextPartPair", []string{"f0", "f1"}},
    {"mime/multipart", "CreatePart", "Writer", "__mpWriterCreatePart", "__mpWriterCreatePartPair", []string{"f0", "f1"}},
    {"mime/multipart", "CreateFormFile", "Writer", "__mpWriterCreateFormFile", "__mpWriterCreateFormFilePair", []string{"f0", "f1"}},
    {"mime/multipart", "CreateFormField", "Writer", "__mpWriterCreateFormField", "__mpWriterCreateFormFieldPair", []string{"f0", "f1"}},
    {"mime/multipart", "Write", "part", "__mppartWrite", "__mppartWritePair", []string{"n", "err"}},
    {"mime/quotedprintable", "fromHex", "", "__qpfromHex", "__qpfromHexPair", []string{"f0", "f1"}},
    {"mime/quotedprintable", "readHexByte", "", "__qpreadHexByte", "__qpreadHexBytePair", []string{"b", "err"}},
    {"mime/quotedprintable", "Read", "Reader", "__qpReaderRead", "__qpReaderReadPair", []string{"n", "err"}},
    {"mime/quotedprintable", "Write", "Writer", "__qpWriterWrite", "__qpWriterWritePair", []string{"n", "err"}},
}

// m8MultiResultFuncs is the desugar table for the crypto/* packages (P3-M8).
// Pattern: exact gc signature + fail-fast stub body + real single-result
// helper + pair struct. The helperPkg is the synthetic package path.
// Interface multi-result (cipher AEAD.Open, io.Reader.Read) needs the
// interface receiver form; see handoff.
var m8MultiResultFuncs = []osMultiResultFunc{
	// crypto/md5, sha1, sha256, sha512, sha3: Hash.Write methods
	// (via hash.__hashWrite) + md5 binary marshaling (via
	// hash.__binaryMarshal/__binaryAppend).
	{"crypto/md5", "Write", "digest", "__hashWrite", "hash", "hashWritePair", []string{"n", "err"}},
	{"crypto/md5", "MarshalBinary", "digest", "__binaryMarshal", "hash", "binaryMarshalPair", []string{"b", "err"}},
	{"crypto/md5", "AppendBinary", "digest", "__binaryAppend", "hash", "binaryAppendPair", []string{"b", "err"}},
	// crypto/sha256, sha512: same pattern (digest types).
	{"crypto/sha256", "Write", "digest", "__hashWrite", "hash", "hashWritePair", []string{"n", "err"}},
	{"crypto/sha512", "Write", "digest", "__hashWrite", "hash", "hashWritePair", []string{"n", "err"}},
	{"crypto/sha1", "Write", "digest", "__hashWrite", "hash", "hashWritePair", []string{"n", "err"}},
	{"crypto/sha3", "Write", "SHA3", "__hashWrite", "hash", "hashWritePair", []string{"n", "err"}},
	{"crypto/sha3", "Write", "SHAKE", "__hashWrite", "hash", "hashWritePair", []string{"n", "err"}},
	// crypto/hmac: New returns a single hash.Hash (not multi-result);
	// no desugar entry needed.
	// crypto/aes, des, rc4: NewCipher is multi-result.
	{"crypto/aes", "NewCipher", "", "__aesNewCipher", "", "newCipherPair", []string{"block", "err"}},
	{"crypto/des", "NewCipher", "", "__desNewCipher", "", "newCipherPair", []string{"block", "err"}},
	{"crypto/des", "NewTripleDESCipher", "", "__desNewTripleDESCipher", "", "newCipherPair", []string{"block", "err"}},
	{"crypto/rc4", "NewCipher", "", "__rc4NewCipher", "", "newCipherPair", []string{"cipher", "err"}},
	// crypto/cipher: NewGCM* and AEAD.Open (interface receiver).
	{"crypto/cipher", "NewGCM", "", "__newGCM", "", "newGCMAEADPair", []string{"aead", "err"}},
	{"crypto/cipher", "NewGCMWithNonceSize", "", "__newGCMWithNonceSize", "", "newGCMAEADPair", []string{"aead", "err"}},
	{"crypto/cipher", "NewGCMWithTagSize", "", "__newGCMWithTagSize", "", "newGCMAEADPair", []string{"aead", "err"}},
	{"crypto/cipher", "Open", "AEAD", "__aeadOpen", "", "aeadOpenPair", []string{"out", "err"}},
	// P3-M8b: gcmFallback.Open concrete form (method-table lookup uses the
	// concrete receiver type name; the helper takes AEAD and asserts OpenSingle).
	{"crypto/cipher", "Open", "gcmFallback", "__aeadOpen", "", "aeadOpenPair", []string{"out", "err"}},
	// crypto/cipher: StreamReader.Read, StreamWriter.Write (need io desugar).
	{"crypto/cipher", "Read", "StreamReader", "__streamReaderRead", "", "streamReaderReadPair", []string{"n", "err"}},
	{"crypto/cipher", "Write", "StreamWriter", "__streamWriterWrite", "", "streamWriterWritePair", []string{"n", "err"}},
	// crypto/rand: Read is multi-result (fail-fast).
	{"crypto/rand", "Read", "", "__randRead", "", "readPair", []string{"n", "err"}},
	{"crypto/rand", "Prime", "", "__mrPrime", "", "__mrPrimePair", []string{"v0", "v1"}},
	// P3-M8b: hash.Cloner.Clone for every synthetic digest (itab wiring
	// for the m8IfaceMultiResultMethods "hash"/"Clone" entry above; the
	// concrete Clone stubs are fail-fast, the real logic is each
	// type's CloneSinglePair served via hash.__hashClone).
	{"crypto/md5", "Clone", "digest", "__hashClone", "hash", "ClonePair", []string{"C", "Err"}},
	{"crypto/sha1", "Clone", "digest", "__hashClone", "hash", "ClonePair", []string{"C", "Err"}},
	{"crypto/sha256", "Clone", "digest", "__hashClone", "hash", "ClonePair", []string{"C", "Err"}},
	{"crypto/sha512", "Clone", "digest", "__hashClone", "hash", "ClonePair", []string{"C", "Err"}},
	{"crypto/sha3", "Clone", "SHA3", "__hashClone", "hash", "ClonePair", []string{"C", "Err"}},
	{"crypto/hmac", "Clone", "hmac", "__hashClone", "hash", "ClonePair", []string{"C", "Err"}},
	// P3-M9 (wave 3, tier-1b): hash/adler32, hash/crc32, hash/crc64
	// digest methods via package-local __-helpers. The shared
	// hash.__hashWrite/__hashClone lose the dynamic type's method set
	// when the itab converts *digest across interfaces (the
	// h.(singleWriter) assertion panics at runtime); the package-local
	// helpers take interface{} and assert the concrete type directly.
	// The digests keep the exact gc multi-result signatures with
	// fail-fast bodies; real logic lives in the single-result
	// WriteSingle/CloneSinglePair/MarshalSingle/AppendSingle methods.
	// Pair structs are positionally identical to the shared
	// hash.hashWritePair/ClonePair/binaryMarshalPair/binaryAppendPair
	// (runtime field reads are positional; see SelectorNode).
	{"hash/adler32", "Write", "digest", "__adler32Write", "hash/adler32", "writePair", []string{"n", "err"}},
	{"hash/adler32", "Clone", "digest", "__adler32Clone", "hash/adler32", "clonePair", []string{"c", "err"}},
	{"hash/adler32", "MarshalBinary", "digest", "__adler32Marshal", "hash/adler32", "bytesPair", []string{"b", "err"}},
	{"hash/adler32", "AppendBinary", "digest", "__adler32Append", "hash/adler32", "bytesPair", []string{"b", "err"}},
	{"hash/crc32", "Write", "digest", "__crc32Write", "hash/crc32", "writePair", []string{"n", "err"}},
	{"hash/crc32", "Clone", "digest", "__crc32Clone", "hash/crc32", "clonePair", []string{"c", "err"}},
	{"hash/crc32", "MarshalBinary", "digest", "__crc32Marshal", "hash/crc32", "bytesPair", []string{"b", "err"}},
	{"hash/crc32", "AppendBinary", "digest", "__crc32Append", "hash/crc32", "bytesPair", []string{"b", "err"}},
	{"hash/crc64", "Write", "digest", "__crc64Write", "hash/crc64", "writePair", []string{"n", "err"}},
	{"hash/crc64", "Clone", "digest", "__crc64Clone", "hash/crc64", "clonePair", []string{"c", "err"}},
	{"hash/crc64", "MarshalBinary", "digest", "__crc64Marshal", "hash/crc64", "bytesPair", []string{"b", "err"}},
	{"hash/crc64", "AppendBinary", "digest", "__crc64Append", "hash/crc64", "bytesPair", []string{"b", "err"}},
	// P3-M9 (wave 3, tier-1b): hash/fnv sum methods (no Clone; gc's fnv
	// digests do not implement hash.Cloner). Each sum type has its own
	// concrete-type helper (the itab dispatcher passes the raw dynamic
	// value; interface{} + type assertion fails at runtime).
	{"hash/fnv", "Write", "sum32", "__fnvWrite32", "hash/fnv", "writePair", []string{"n", "err"}},
	{"hash/fnv", "Write", "sum32a", "__fnvWrite32a", "hash/fnv", "writePair", []string{"n", "err"}},
	{"hash/fnv", "Write", "sum64", "__fnvWrite64", "hash/fnv", "writePair", []string{"n", "err"}},
	{"hash/fnv", "Write", "sum64a", "__fnvWrite64a", "hash/fnv", "writePair", []string{"n", "err"}},
	{"hash/fnv", "Write", "sum128", "__fnvWrite128", "hash/fnv", "writePair", []string{"n", "err"}},
	{"hash/fnv", "Write", "sum128a", "__fnvWrite128a", "hash/fnv", "writePair", []string{"n", "err"}},
	{"hash/fnv", "MarshalBinary", "sum32", "__fnvMarshal32", "hash/fnv", "bytesPair", []string{"b", "err"}},
	{"hash/fnv", "MarshalBinary", "sum32a", "__fnvMarshal32a", "hash/fnv", "bytesPair", []string{"b", "err"}},
	{"hash/fnv", "MarshalBinary", "sum64", "__fnvMarshal64", "hash/fnv", "bytesPair", []string{"b", "err"}},
	{"hash/fnv", "MarshalBinary", "sum64a", "__fnvMarshal64a", "hash/fnv", "bytesPair", []string{"b", "err"}},
	{"hash/fnv", "MarshalBinary", "sum128", "__fnvMarshal128", "hash/fnv", "bytesPair", []string{"b", "err"}},
	{"hash/fnv", "MarshalBinary", "sum128a", "__fnvMarshal128a", "hash/fnv", "bytesPair", []string{"b", "err"}},
	{"hash/fnv", "AppendBinary", "sum32", "__fnvAppend32", "hash/fnv", "bytesPair", []string{"b", "err"}},
	{"hash/fnv", "AppendBinary", "sum32a", "__fnvAppend32a", "hash/fnv", "bytesPair", []string{"b", "err"}},
	{"hash/fnv", "AppendBinary", "sum64", "__fnvAppend64", "hash/fnv", "bytesPair", []string{"b", "err"}},
	{"hash/fnv", "AppendBinary", "sum64a", "__fnvAppend64a", "hash/fnv", "bytesPair", []string{"b", "err"}},
	{"hash/fnv", "AppendBinary", "sum128", "__fnvAppend128", "hash/fnv", "bytesPair", []string{"b", "err"}},
	{"hash/fnv", "AppendBinary", "sum128a", "__fnvAppend128a", "hash/fnv", "bytesPair", []string{"b", "err"}},
	// P3-M9 (wave 3, tier-1b): hash/maphash Write/WriteString/Clone via
	// the package-local __maphashWrite/__maphashWriteString/__maphashClone
	// helpers (same rationale as above). The public methods keep the
	// exact gc multi-result signatures with fail-fast bodies; real logic
	// is in WriteSingle/WriteStringSingle/CloneSinglePair.
	{"hash/maphash", "Write", "Hash", "__maphashWrite", "hash/maphash", "maphashWritePair", []string{"n", "err"}},
	{"hash/maphash", "WriteString", "Hash", "__maphashWriteString", "hash/maphash", "maphashWritePair", []string{"n", "err"}},
	{"hash/maphash", "Clone", "Hash", "__maphashClone", "hash/maphash", "maphashClonePair", []string{"c", "err"}},
	// P3-M8b: encoding.BinaryMarshaler/BinaryAppender for the synthetic
	// digests (itab wiring for the "hash"/"MarshalBinary" and
	// "hash"/"AppendBinary" iface entries; md5 entries pre-exist above).
	{"crypto/sha1", "MarshalBinary", "digest", "__binaryMarshal", "hash", "binaryMarshalPair", []string{"b", "err"}},
	{"crypto/sha1", "AppendBinary", "digest", "__binaryAppend", "hash", "binaryAppendPair", []string{"b", "err"}},
	{"crypto/sha256", "MarshalBinary", "digest", "__binaryMarshal", "hash", "binaryMarshalPair", []string{"b", "err"}},
	{"crypto/sha256", "AppendBinary", "digest", "__binaryAppend", "hash", "binaryAppendPair", []string{"b", "err"}},
	{"crypto/sha512", "MarshalBinary", "digest", "__binaryMarshal", "hash", "binaryMarshalPair", []string{"b", "err"}},
	{"crypto/sha512", "AppendBinary", "digest", "__binaryAppend", "hash", "binaryAppendPair", []string{"b", "err"}},
	{"crypto/sha3", "MarshalBinary", "SHA3", "__binaryMarshal", "hash", "binaryMarshalPair", []string{"b", "err"}},
	// P3-M8b: hmac.Write through hash.Hash (itab wiring; the call site
	// desugars via the structural io/Write iface entry).
	{"crypto/hmac", "Write", "hmac", "__hashWrite", "hash", "hashWritePair", []string{"n", "err"}},
	// P3-M8b: SHAKE Write/Read (concrete *SHAKE call sites, e.g.
	// h.Write(in) / h.Read(got) as bare statements).
	{"crypto/sha3", "Write", "SHAKE", "__hashWrite", "hash", "hashWritePair", []string{"n", "err"}},
	{"crypto/sha3", "Read", "SHAKE", "__hashRead", "hash", "hashReadPair", []string{"n", "err"}},
	// P3-M9 (tier-5): asymmetric crypto multi-result functions.
	// Generated by tier5crypto/transform.go from go1.27.1 vendored
	// sources; see phases/phase3/hidden_files/m9-tmp/tier5crypto/.
	{"crypto/dsa", "Sign", "", "__mrSign", "", "__mrSignPair", []string{"r", "s", "err"}},
	{"crypto/ecdh", "ECDH", "PrivateKey", "__mrPrivateKey_ECDH", "", "__mrPrivateKey_ECDHPair", []string{"v0", "v1"}},
	{"crypto/ecdh", "GenerateKey", "nistCurve", "__mrnistCurve_GenerateKey", "", "__mrnistCurve_GenerateKeyPair", []string{"v0", "v1"}},
	{"crypto/ecdh", "GenerateKey", "x25519Curve", "__mrx25519Curve_GenerateKey", "", "__mrx25519Curve_GenerateKeyPair", []string{"v0", "v1"}},
	{"crypto/ecdh", "NewPrivateKey", "nistCurve", "__mrnistCurve_NewPrivateKey", "", "__mrnistCurve_NewPrivateKeyPair", []string{"v0", "v1"}},
	{"crypto/ecdh", "NewPrivateKey", "x25519Curve", "__mrx25519Curve_NewPrivateKey", "", "__mrx25519Curve_NewPrivateKeyPair", []string{"v0", "v1"}},
	{"crypto/ecdh", "NewPublicKey", "nistCurve", "__mrnistCurve_NewPublicKey", "", "__mrnistCurve_NewPublicKeyPair", []string{"v0", "v1"}},
	{"crypto/ecdh", "NewPublicKey", "x25519Curve", "__mrx25519Curve_NewPublicKey", "", "__mrx25519Curve_NewPublicKeyPair", []string{"v0", "v1"}},
	{"crypto/ecdh", "ecdh", "nistCurve", "__mrnistCurve_ecdh", "", "__mrnistCurve_ecdhPair", []string{"v0", "v1"}},
	{"crypto/ecdh", "ecdh", "x25519Curve", "__mrx25519Curve_ecdh", "", "__mrx25519Curve_ecdhPair", []string{"v0", "v1"}},
	{"crypto/ecdsa", "Bytes", "PrivateKey", "__mrPrivateKey_Bytes", "", "__mrPrivateKey_BytesPair", []string{"v0", "v1"}},
	{"crypto/ecdsa", "Bytes", "PublicKey", "__mrPublicKey_Bytes", "", "__mrPublicKey_BytesPair", []string{"v0", "v1"}},
	{"crypto/ecdsa", "ECDH", "PrivateKey", "__mrPrivateKey_ECDH", "", "__mrPrivateKey_ECDHPair", []string{"v0", "v1"}},
	{"crypto/ecdsa", "ECDH", "PublicKey", "__mrPublicKey_ECDH", "", "__mrPublicKey_ECDHPair", []string{"v0", "v1"}},
	{"crypto/ecdsa", "GenerateKey", "", "__mrGenerateKey", "", "__mrGenerateKeyPair", []string{"v0", "v1"}},
	{"crypto/ecdsa", "ParseRawPrivateKey", "", "__mrParseRawPrivateKey", "", "__mrParseRawPrivateKeyPair", []string{"v0", "v1"}},
	{"crypto/ecdsa", "ParseUncompressedPublicKey", "", "__mrParseUncompressedPublicKey", "", "__mrParseUncompressedPublicKeyPair", []string{"v0", "v1"}},
	{"crypto/ecdsa", "Sign", "", "__mrSign", "", "__mrSignPair", []string{"r", "s", "err"}},
	{"crypto/ecdsa", "Sign", "PrivateKey", "__mrPrivateKey_Sign", "", "__mrPrivateKey_SignPair", []string{"v0", "v1"}},
	{"crypto/ecdsa", "SignASN1", "", "__mrSignASN1", "", "__mrSignASN1Pair", []string{"v0", "v1"}},
	{"crypto/ecdsa", "boringPrivateKey", "", "__mrboringPrivateKey", "", "__mrboringPrivateKeyPair", []string{"v0", "v1"}},
	{"crypto/ecdsa", "boringPublicKey", "", "__mrboringPublicKey", "", "__mrboringPublicKeyPair", []string{"v0", "v1"}},
	{"crypto/ecdsa", "encodeSignature", "", "__mrencodeSignature", "", "__mrencodeSignaturePair", []string{"v0", "v1"}},
	{"crypto/ecdsa", "generateFIPS", "", "__mrgenerateFIPS", "", "__mrgenerateFIPSPair", []string{"v0", "v1"}},
	{"crypto/ecdsa", "generateLegacy", "", "__mrgenerateLegacy", "", "__mrgenerateLegacyPair", []string{"v0", "v1"}},
	{"crypto/ecdsa", "parseRawPrivateKey", "", "__mrparseRawPrivateKey", "", "__mrparseRawPrivateKeyPair", []string{"v0", "v1"}},
	{"crypto/ecdsa", "parseSignature", "", "__mrparseSignature", "", "__mrparseSignaturePair", []string{"r", "s", "err"}},
	{"crypto/ecdsa", "parseUncompressedPublicKey", "", "__mrparseUncompressedPublicKey", "", "__mrparseUncompressedPublicKeyPair", []string{"v0", "v1"}},
	{"crypto/ecdsa", "pointFromAffine", "", "__mrpointFromAffine", "", "__mrpointFromAffinePair", []string{"v0", "v1"}},
	{"crypto/ecdsa", "pointToAffine", "", "__mrpointToAffine", "", "__mrpointToAffinePair", []string{"x", "y", "err"}},
	{"crypto/ecdsa", "privateKeyBytes", "", "__mrprivateKeyBytes", "", "__mrprivateKeyBytesPair", []string{"v0", "v1"}},
	{"crypto/ecdsa", "privateKeyFromFIPS", "", "__mrprivateKeyFromFIPS", "", "__mrprivateKeyFromFIPSPair", []string{"v0", "v1"}},
	{"crypto/ecdsa", "privateKeyToFIPS", "", "__mrprivateKeyToFIPS", "", "__mrprivateKeyToFIPSPair", []string{"v0", "v1"}},
	{"crypto/ecdsa", "publicKeyBytes", "", "__mrpublicKeyBytes", "", "__mrpublicKeyBytesPair", []string{"v0", "v1"}},
	{"crypto/ecdsa", "publicKeyFromFIPS", "", "__mrpublicKeyFromFIPS", "", "__mrpublicKeyFromFIPSPair", []string{"v0", "v1"}},
	{"crypto/ecdsa", "publicKeyToFIPS", "", "__mrpublicKeyToFIPS", "", "__mrpublicKeyToFIPSPair", []string{"v0", "v1"}},
	{"crypto/ecdsa", "randFieldElement", "", "__mrrandFieldElement", "", "__mrrandFieldElementPair", []string{"k", "err"}},
	{"crypto/ecdsa", "signFIPS", "", "__mrsignFIPS", "", "__mrsignFIPSPair", []string{"v0", "v1"}},
	{"crypto/ecdsa", "signFIPSDeterministic", "", "__mrsignFIPSDeterministic", "", "__mrsignFIPSDeterministicPair", []string{"v0", "v1"}},
	{"crypto/ecdsa", "signLegacy", "", "__mrsignLegacy", "", "__mrsignLegacyPair", []string{"sig", "err"}},
	{"crypto/ecdsa", "signRFC6979", "", "__mrsignRFC6979", "", "__mrsignRFC6979Pair", []string{"v0", "v1"}},
	{"crypto/ed25519", "GenerateKey", "", "__mrGenerateKey", "", "__mrGenerateKeyPair", []string{"v0", "v1", "v2"}},
	{"crypto/ed25519", "Sign", "PrivateKey", "__mrPrivateKey_Sign", "", "__mrPrivateKey_SignPair", []string{"signature", "err"}},
	{"crypto/elliptic", "Add", "CurveParams", "__mrCurveParams_Add", "", "__mrCurveParams_AddPair", []string{"v0", "v1"}},
	{"crypto/elliptic", "Add", "nistCurve", "__mrnistCurve_Add", "", "__mrnistCurve_AddPair", []string{"v0", "v1"}},
	{"crypto/elliptic", "Double", "CurveParams", "__mrCurveParams_Double", "", "__mrCurveParams_DoublePair", []string{"v0", "v1"}},
	{"crypto/elliptic", "Double", "nistCurve", "__mrnistCurve_Double", "", "__mrnistCurve_DoublePair", []string{"v0", "v1"}},
	{"crypto/elliptic", "GenerateKey", "", "__mrGenerateKey", "", "__mrGenerateKeyPair", []string{"priv", "x", "y", "err"}},
	{"crypto/elliptic", "ScalarBaseMult", "CurveParams", "__mrCurveParams_ScalarBaseMult", "", "__mrCurveParams_ScalarBaseMultPair", []string{"v0", "v1"}},
	{"crypto/elliptic", "ScalarBaseMult", "nistCurve", "__mrnistCurve_ScalarBaseMult", "", "__mrnistCurve_ScalarBaseMultPair", []string{"v0", "v1"}},
	{"crypto/elliptic", "ScalarMult", "CurveParams", "__mrCurveParams_ScalarMult", "", "__mrCurveParams_ScalarMultPair", []string{"v0", "v1"}},
	{"crypto/elliptic", "ScalarMult", "nistCurve", "__mrnistCurve_ScalarMult", "", "__mrnistCurve_ScalarMultPair", []string{"v0", "v1"}},
	{"crypto/elliptic", "Unmarshal", "", "__mrUnmarshal", "", "__mrUnmarshalPair", []string{"x", "y"}},
	{"crypto/elliptic", "Unmarshal", "nistCurve", "__mrnistCurve_Unmarshal", "", "__mrnistCurve_UnmarshalPair", []string{"x", "y"}},
	{"crypto/elliptic", "UnmarshalCompressed", "", "__mrUnmarshalCompressed", "", "__mrUnmarshalCompressedPair", []string{"x", "y"}},
	{"crypto/elliptic", "UnmarshalCompressed", "nistCurve", "__mrnistCurve_UnmarshalCompressed", "", "__mrnistCurve_UnmarshalCompressedPair", []string{"x", "y"}},
	{"crypto/elliptic", "addJacobian", "CurveParams", "__mrCurveParams_addJacobian", "", "__mrCurveParams_addJacobianPair", []string{"v0", "v1", "v2"}},
	{"crypto/elliptic", "affineFromJacobian", "CurveParams", "__mrCurveParams_affineFromJacobian", "", "__mrCurveParams_affineFromJacobianPair", []string{"xOut", "yOut"}},
	{"crypto/elliptic", "doubleJacobian", "CurveParams", "__mrCurveParams_doubleJacobian", "", "__mrCurveParams_doubleJacobianPair", []string{"v0", "v1", "v2"}},
	{"crypto/elliptic", "matchesSpecificCurve", "", "__mrmatchesSpecificCurve", "", "__mrmatchesSpecificCurvePair", []string{"v0", "v1"}},
	{"crypto/elliptic", "pointFromAffine", "nistCurve", "__mrnistCurve_pointFromAffine", "", "__mrnistCurve_pointFromAffinePair", []string{"p", "err"}},
	{"crypto/elliptic", "pointToAffine", "nistCurve", "__mrnistCurve_pointToAffine", "", "__mrnistCurve_pointToAffinePair", []string{"x", "y"}},
	{"crypto/hkdf", "Expand", "", "__mrExpand", "", "__mrExpandPair", []string{"v0", "v1"}},
	{"crypto/hkdf", "Extract", "", "__mrExtract", "", "__mrExtractPair", []string{"v0", "v1"}},
	{"crypto/hkdf", "Key", "", "__mrKey", "", "__mrKeyPair", []string{"v0", "v1"}},
	{"crypto/internal/boring", "DecryptRSANoPadding", "", "__mrDecryptRSANoPadding", "", "__mrDecryptRSANoPaddingPair", []string{"v0", "v1"}},
	{"crypto/internal/boring", "DecryptRSAOAEP", "", "__mrDecryptRSAOAEP", "", "__mrDecryptRSAOAEPPair", []string{"v0", "v1"}},
	{"crypto/internal/boring", "DecryptRSAPKCS1", "", "__mrDecryptRSAPKCS1", "", "__mrDecryptRSAPKCS1Pair", []string{"v0", "v1"}},
	{"crypto/internal/boring", "ECDH", "", "__mrECDH", "", "__mrECDHPair", []string{"v0", "v1"}},
	{"crypto/internal/boring", "EncryptRSANoPadding", "", "__mrEncryptRSANoPadding", "", "__mrEncryptRSANoPaddingPair", []string{"v0", "v1"}},
	{"crypto/internal/boring", "EncryptRSAOAEP", "", "__mrEncryptRSAOAEP", "", "__mrEncryptRSAOAEPPair", []string{"v0", "v1"}},
	{"crypto/internal/boring", "EncryptRSAPKCS1", "", "__mrEncryptRSAPKCS1", "", "__mrEncryptRSAPKCS1Pair", []string{"v0", "v1"}},
	{"crypto/internal/boring", "GenerateKeyECDH", "", "__mrGenerateKeyECDH", "", "__mrGenerateKeyECDHPair", []string{"v0", "v1", "v2"}},
	{"crypto/internal/boring", "GenerateKeyECDSA", "", "__mrGenerateKeyECDSA", "", "__mrGenerateKeyECDSAPair", []string{"X", "Y", "D", "err"}},
	{"crypto/internal/boring", "GenerateKeyRSA", "", "__mrGenerateKeyRSA", "", "__mrGenerateKeyRSAPair", []string{"N", "E", "D", "P", "Q", "Dp", "Dq", "Qinv", "err"}},
	{"crypto/internal/boring", "NewAESCipher", "", "__mrNewAESCipher", "", "__mrNewAESCipherPair", []string{"v0", "v1"}},
	{"crypto/internal/boring", "NewGCMTLS", "", "__mrNewGCMTLS", "", "__mrNewGCMTLSPair", []string{"v0", "v1"}},
	{"crypto/internal/boring", "NewGCMTLS13", "", "__mrNewGCMTLS13", "", "__mrNewGCMTLS13Pair", []string{"v0", "v1"}},
	{"crypto/internal/boring", "NewPrivateKeyECDH", "", "__mrNewPrivateKeyECDH", "", "__mrNewPrivateKeyECDHPair", []string{"v0", "v1"}},
	{"crypto/internal/boring", "NewPrivateKeyECDSA", "", "__mrNewPrivateKeyECDSA", "", "__mrNewPrivateKeyECDSAPair", []string{"v0", "v1"}},
	{"crypto/internal/boring", "NewPrivateKeyRSA", "", "__mrNewPrivateKeyRSA", "", "__mrNewPrivateKeyRSAPair", []string{"v0", "v1"}},
	{"crypto/internal/boring", "NewPublicKeyECDH", "", "__mrNewPublicKeyECDH", "", "__mrNewPublicKeyECDHPair", []string{"v0", "v1"}},
	{"crypto/internal/boring", "NewPublicKeyECDSA", "", "__mrNewPublicKeyECDSA", "", "__mrNewPublicKeyECDSAPair", []string{"v0", "v1"}},
	{"crypto/internal/boring", "NewPublicKeyRSA", "", "__mrNewPublicKeyRSA", "", "__mrNewPublicKeyRSAPair", []string{"v0", "v1"}},
	{"crypto/internal/boring", "PublicKey", "PrivateKeyECDH", "__mrPrivateKeyECDH_PublicKey", "", "__mrPrivateKeyECDH_PublicKeyPair", []string{"v0", "v1"}},
	{"crypto/internal/boring", "Read", "randReader", "__mrrandReader_Read", "", "__mrrandReader_ReadPair", []string{"v0", "v1"}},
	{"crypto/internal/boring", "SignMarshalECDSA", "", "__mrSignMarshalECDSA", "", "__mrSignMarshalECDSAPair", []string{"v0", "v1"}},
	{"crypto/internal/boring", "SignRSAPKCS1v15", "", "__mrSignRSAPKCS1v15", "", "__mrSignRSAPKCS1v15Pair", []string{"v0", "v1"}},
	{"crypto/internal/boring", "SignRSAPSS", "", "__mrSignRSAPSS", "", "__mrSignRSAPSSPair", []string{"v0", "v1"}},
	{"crypto/internal/fips140/bigmod", "GCDVarTime", "Nat", "__mrNat_GCDVarTime", "", "__mrNat_GCDVarTimePair", []string{"v0", "v1"}},
	{"crypto/internal/fips140/bigmod", "InverseVarTime", "Nat", "__mrNat_InverseVarTime", "", "__mrNat_InverseVarTimePair", []string{"v0", "v1"}},
	{"crypto/internal/fips140/bigmod", "NewModulus", "", "__mrNewModulus", "", "__mrNewModulusPair", []string{"v0", "v1"}},
	{"crypto/internal/fips140/bigmod", "NewModulusProduct", "", "__mrNewModulusProduct", "", "__mrNewModulusProductPair", []string{"v0", "v1"}},
	{"crypto/internal/fips140/bigmod", "SetBytes", "Nat", "__mrNat_SetBytes", "", "__mrNat_SetBytesPair", []string{"v0", "v1"}},
	{"crypto/internal/fips140/bigmod", "SetOverflowingBytes", "Nat", "__mrNat_SetOverflowingBytes", "", "__mrNat_SetOverflowingBytesPair", []string{"v0", "v1"}},
	{"crypto/internal/fips140/bigmod", "extendedGCD", "", "__mrextendedGCD", "", "__mrextendedGCDPair", []string{"u", "A", "err"}},
	{"crypto/internal/fips140/bigmod", "newModulus", "", "__mrnewModulus", "", "__mrnewModulusPair", []string{"v0", "v1"}},
	{"crypto/internal/fips140/ecdh", "ECDH", "", "__mrECDH", "", "__mrECDHPair", []string{"v0", "v1"}},
	{"crypto/internal/fips140/ecdh", "GenerateKey", "", "__mrGenerateKey", "", "__mrGenerateKeyPair", []string{"v0", "v1"}},
	{"crypto/internal/fips140/ecdh", "NewPrivateKey", "", "__mrNewPrivateKey", "", "__mrNewPrivateKeyPair", []string{"v0", "v1"}},
	{"crypto/internal/fips140/ecdh", "NewPublicKey", "", "__mrNewPublicKey", "", "__mrNewPublicKeyPair", []string{"v0", "v1"}},
	{"crypto/internal/fips140/ecdh", "ecdh", "", "__mrecdh", "", "__mrecdhPair", []string{"v0", "v1"}},
	{"crypto/internal/fips140/ecdsa", "GenerateKey", "", "__mrGenerateKey", "", "__mrGenerateKeyPair", []string{"v0", "v1"}},
	{"crypto/internal/fips140/ecdsa", "NewPrivateKey", "", "__mrNewPrivateKey", "", "__mrNewPrivateKeyPair", []string{"v0", "v1"}},
	{"crypto/internal/fips140/ecdsa", "NewPublicKey", "", "__mrNewPublicKey", "", "__mrNewPublicKeyPair", []string{"v0", "v1"}},
	{"crypto/internal/fips140/ecdsa", "Sign", "", "__mrSign", "", "__mrSignPair", []string{"v0", "v1"}},
	{"crypto/internal/fips140/ecdsa", "SignDeterministic", "", "__mrSignDeterministic", "", "__mrSignDeterministicPair", []string{"v0", "v1"}},
	{"crypto/internal/fips140/ecdsa", "randomPoint", "", "__mrrandomPoint", "", "__mrrandomPointPair", []string{"k", "p", "err"}},
	{"crypto/internal/fips140/ecdsa", "signGeneric", "", "__mrsignGeneric", "", "__mrsignGenericPair", []string{"v0", "v1"}},
	{"crypto/internal/fips140/ed25519", "GenerateKey", "", "__mrGenerateKey", "", "__mrGenerateKeyPair", []string{"v0", "v1"}},
	{"crypto/internal/fips140/ed25519", "NewPrivateKey", "", "__mrNewPrivateKey", "", "__mrNewPrivateKeyPair", []string{"v0", "v1"}},
	{"crypto/internal/fips140/ed25519", "NewPrivateKeyFromSeed", "", "__mrNewPrivateKeyFromSeed", "", "__mrNewPrivateKeyFromSeedPair", []string{"v0", "v1"}},
	{"crypto/internal/fips140/ed25519", "NewPublicKey", "", "__mrNewPublicKey", "", "__mrNewPublicKeyPair", []string{"v0", "v1"}},
	{"crypto/internal/fips140/ed25519", "SignCtx", "", "__mrSignCtx", "", "__mrSignCtxPair", []string{"v0", "v1"}},
	{"crypto/internal/fips140/ed25519", "SignPH", "", "__mrSignPH", "", "__mrSignPHPair", []string{"v0", "v1"}},
	{"crypto/internal/fips140/ed25519", "generateKey", "", "__mrgenerateKey", "", "__mrgenerateKeyPair", []string{"v0", "v1"}},
	{"crypto/internal/fips140/ed25519", "newPrivateKey", "", "__mrnewPrivateKey", "", "__mrnewPrivateKeyPair", []string{"v0", "v1"}},
	{"crypto/internal/fips140/ed25519", "newPrivateKeyFromSeed", "", "__mrnewPrivateKeyFromSeed", "", "__mrnewPrivateKeyFromSeedPair", []string{"v0", "v1"}},
	{"crypto/internal/fips140/ed25519", "newPublicKey", "", "__mrnewPublicKey", "", "__mrnewPublicKeyPair", []string{"v0", "v1"}},
	{"crypto/internal/fips140/ed25519", "signCtx", "", "__mrsignCtx", "", "__mrsignCtxPair", []string{"v0", "v1"}},
	{"crypto/internal/fips140/ed25519", "signPH", "", "__mrsignPH", "", "__mrsignPHPair", []string{"v0", "v1"}},
	{"crypto/internal/fips140/edwards25519", "SetBytes", "Point", "__mrPoint_SetBytes", "", "__mrPoint_SetBytesPair", []string{"v0", "v1"}},
	{"crypto/internal/fips140/edwards25519", "SetBytesWithClamping", "Scalar", "__mrScalar_SetBytesWithClamping", "", "__mrScalar_SetBytesWithClampingPair", []string{"v0", "v1"}},
	{"crypto/internal/fips140/edwards25519", "SetCanonicalBytes", "Scalar", "__mrScalar_SetCanonicalBytes", "", "__mrScalar_SetCanonicalBytesPair", []string{"v0", "v1"}},
	{"crypto/internal/fips140/edwards25519", "SetUniformBytes", "Scalar", "__mrScalar_SetUniformBytes", "", "__mrScalar_SetUniformBytesPair", []string{"v0", "v1"}},
	{"crypto/internal/fips140/edwards25519/field", "SetBytes", "Element", "__mrElement_SetBytes", "", "__mrElement_SetBytesPair", []string{"v0", "v1"}},
	{"crypto/internal/fips140/edwards25519/field", "SqrtRatio", "Element", "__mrElement_SqrtRatio", "", "__mrElement_SqrtRatioPair", []string{"R", "wasSquare"}},
	{"crypto/internal/fips140/edwards25519/field", "mul51", "", "__mrmul51", "", "__mrmul51Pair", []string{"lo", "hi"}},
	{"crypto/internal/fips140/hmac", "Clone", "HMAC", "__mrHMAC_Clone", "", "__mrHMAC_ClonePair", []string{"v0", "v1"}},
	{"crypto/internal/fips140/hmac", "Write", "HMAC", "__mrHMAC_Write", "", "__mrHMAC_WritePair", []string{"n", "err"}},
	{"crypto/internal/fips140/nistec", "BytesX", "P224Point", "__mrP224Point_BytesX", "", "__mrP224Point_BytesXPair", []string{"v0", "v1"}},
	{"crypto/internal/fips140/nistec", "BytesX", "P256Point", "__mrP256Point_BytesX", "", "__mrP256Point_BytesXPair", []string{"v0", "v1"}},
	{"crypto/internal/fips140/nistec", "BytesX", "P384Point", "__mrP384Point_BytesX", "", "__mrP384Point_BytesXPair", []string{"v0", "v1"}},
	{"crypto/internal/fips140/nistec", "BytesX", "P521Point", "__mrP521Point_BytesX", "", "__mrP521Point_BytesXPair", []string{"v0", "v1"}},
	{"crypto/internal/fips140/nistec", "ScalarBaseMult", "P224Point", "__mrP224Point_ScalarBaseMult", "", "__mrP224Point_ScalarBaseMultPair", []string{"v0", "v1"}},
	{"crypto/internal/fips140/nistec", "ScalarBaseMult", "P256Point", "__mrP256Point_ScalarBaseMult", "", "__mrP256Point_ScalarBaseMultPair", []string{"v0", "v1"}},
	{"crypto/internal/fips140/nistec", "ScalarBaseMult", "P384Point", "__mrP384Point_ScalarBaseMult", "", "__mrP384Point_ScalarBaseMultPair", []string{"v0", "v1"}},
	{"crypto/internal/fips140/nistec", "ScalarBaseMult", "P521Point", "__mrP521Point_ScalarBaseMult", "", "__mrP521Point_ScalarBaseMultPair", []string{"v0", "v1"}},
	{"crypto/internal/fips140/nistec", "ScalarMult", "P224Point", "__mrP224Point_ScalarMult", "", "__mrP224Point_ScalarMultPair", []string{"v0", "v1"}},
	{"crypto/internal/fips140/nistec", "ScalarMult", "P256Point", "__mrP256Point_ScalarMult", "", "__mrP256Point_ScalarMultPair", []string{"v0", "v1"}},
	{"crypto/internal/fips140/nistec", "ScalarMult", "P384Point", "__mrP384Point_ScalarMult", "", "__mrP384Point_ScalarMultPair", []string{"v0", "v1"}},
	{"crypto/internal/fips140/nistec", "ScalarMult", "P521Point", "__mrP521Point_ScalarMult", "", "__mrP521Point_ScalarMultPair", []string{"v0", "v1"}},
	{"crypto/internal/fips140/nistec", "SetBytes", "P224Point", "__mrP224Point_SetBytes", "", "__mrP224Point_SetBytesPair", []string{"v0", "v1"}},
	{"crypto/internal/fips140/nistec", "SetBytes", "P256Point", "__mrP256Point_SetBytes", "", "__mrP256Point_SetBytesPair", []string{"v0", "v1"}},
	{"crypto/internal/fips140/nistec", "SetBytes", "P384Point", "__mrP384Point_SetBytes", "", "__mrP384Point_SetBytesPair", []string{"v0", "v1"}},
	{"crypto/internal/fips140/nistec", "SetBytes", "P521Point", "__mrP521Point_SetBytes", "", "__mrP521Point_SetBytesPair", []string{"v0", "v1"}},
	{"crypto/internal/fips140/nistec", "SetBytes", "p256OrdElement", "__mrp256OrdElement_SetBytes", "", "__mrp256OrdElement_SetBytesPair", []string{"v0", "v1"}},
	{"crypto/internal/fips140/nistec", "boothW5", "", "__mrboothW5", "", "__mrboothW5Pair", []string{"v0", "v1"}},
	{"crypto/internal/fips140/nistec", "boothW6", "", "__mrboothW6", "", "__mrboothW6Pair", []string{"v0", "v1"}},
	{"crypto/internal/fips140/nistec", "bytesX", "P224Point", "__mrP224Point_bytesX", "", "__mrP224Point_bytesXPair", []string{"v0", "v1"}},
	{"crypto/internal/fips140/nistec", "bytesX", "P256Point", "__mrP256Point_bytesX", "", "__mrP256Point_bytesXPair", []string{"v0", "v1"}},
	{"crypto/internal/fips140/nistec", "bytesX", "P384Point", "__mrP384Point_bytesX", "", "__mrP384Point_bytesXPair", []string{"v0", "v1"}},
	{"crypto/internal/fips140/nistec", "bytesX", "P521Point", "__mrP521Point_bytesX", "", "__mrP521Point_bytesXPair", []string{"v0", "v1"}},
	{"crypto/internal/fips140/nistec/fiat", "SetBytes", "P224Element", "__mrP224Element_SetBytes", "", "__mrP224Element_SetBytesPair", []string{"v0", "v1"}},
	{"crypto/internal/fips140/nistec/fiat", "SetBytes", "P256Element", "__mrP256Element_SetBytes", "", "__mrP256Element_SetBytesPair", []string{"v0", "v1"}},
	{"crypto/internal/fips140/nistec/fiat", "SetBytes", "P384Element", "__mrP384Element_SetBytes", "", "__mrP384Element_SetBytesPair", []string{"v0", "v1"}},
	{"crypto/internal/fips140/nistec/fiat", "SetBytes", "P521Element", "__mrP521Element_SetBytes", "", "__mrP521Element_SetBytesPair", []string{"v0", "v1"}},
	{"crypto/internal/fips140/pbkdf2", "Key", "", "__mrKey", "", "__mrKeyPair", []string{"v0", "v1"}},
	{"crypto/internal/fips140/rsa", "DecryptOAEP", "", "__mrDecryptOAEP", "", "__mrDecryptOAEPPair", []string{"v0", "v1"}},
	{"crypto/internal/fips140/rsa", "DecryptWithCheck", "", "__mrDecryptWithCheck", "", "__mrDecryptWithCheckPair", []string{"v0", "v1"}},
	{"crypto/internal/fips140/rsa", "DecryptWithoutCheck", "", "__mrDecryptWithoutCheck", "", "__mrDecryptWithoutCheckPair", []string{"v0", "v1"}},
	{"crypto/internal/fips140/rsa", "Encrypt", "", "__mrEncrypt", "", "__mrEncryptPair", []string{"v0", "v1"}},
	{"crypto/internal/fips140/rsa", "EncryptOAEP", "", "__mrEncryptOAEP", "", "__mrEncryptOAEPPair", []string{"v0", "v1"}},
	{"crypto/internal/fips140/rsa", "Export", "PrivateKey", "__mrPrivateKey_Export", "", "__mrPrivateKey_ExportPair", []string{"N", "e", "d", "P", "Q", "dP", "dQ", "qInv"}},
	{"crypto/internal/fips140/rsa", "GenerateKey", "", "__mrGenerateKey", "", "__mrGenerateKeyPair", []string{"v0", "v1"}},
	{"crypto/internal/fips140/rsa", "NewPrivateKey", "", "__mrNewPrivateKey", "", "__mrNewPrivateKeyPair", []string{"v0", "v1"}},
	{"crypto/internal/fips140/rsa", "NewPrivateKeyWithPrecomputation", "", "__mrNewPrivateKeyWithPrecomputation", "", "__mrNewPrivateKeyWithPrecomputationPair", []string{"v0", "v1"}},
	{"crypto/internal/fips140/rsa", "NewPrivateKeyWithoutCRT", "", "__mrNewPrivateKeyWithoutCRT", "", "__mrNewPrivateKeyWithoutCRTPair", []string{"v0", "v1"}},
	{"crypto/internal/fips140/rsa", "PSSMaxSaltLength", "", "__mrPSSMaxSaltLength", "", "__mrPSSMaxSaltLengthPair", []string{"v0", "v1"}},
	{"crypto/internal/fips140/rsa", "SignPKCS1v15", "", "__mrSignPKCS1v15", "", "__mrSignPKCS1v15Pair", []string{"v0", "v1"}},
	{"crypto/internal/fips140/rsa", "SignPSS", "", "__mrSignPSS", "", "__mrSignPSSPair", []string{"v0", "v1"}},
	{"crypto/internal/fips140/rsa", "TestingOnlyLargeExponentDecryptOAEP", "", "__mrTestingOnlyLargeExponentDecryptOAEP", "", "__mrTestingOnlyLargeExponentDecryptOAEPPair", []string{"v0", "v1"}},
	{"crypto/internal/fips140/rsa", "TestingOnlyLargeExponentEncryptOAEP", "", "__mrTestingOnlyLargeExponentEncryptOAEP", "", "__mrTestingOnlyLargeExponentEncryptOAEPPair", []string{"v0", "v1"}},
	{"crypto/internal/fips140/rsa", "TestingOnlyNewLargeExponentPrivateKeyWithPrecomputation", "", "__mrTestingOnlyNewLargeExponentPrivateKeyWithPrecomputation", "", "__mrTestingOnlyNewLargeExponentPrivateKeyWithPrecomputationPair", []string{"v0", "v1"}},
	{"crypto/internal/fips140/rsa", "checkPublicKey", "", "__mrcheckPublicKey", "", "__mrcheckPublicKeyPair", []string{"fipsApproved", "err"}},
	{"crypto/internal/fips140/rsa", "decrypt", "", "__mrdecrypt", "", "__mrdecryptPair", []string{"v0", "v1"}},
	{"crypto/internal/fips140/rsa", "decryptLargeExponent", "", "__mrdecryptLargeExponent", "", "__mrdecryptLargeExponentPair", []string{"v0", "v1"}},
	{"crypto/internal/fips140/rsa", "emsaPSSEncode", "", "__mremsaPSSEncode", "", "__mremsaPSSEncodePair", []string{"v0", "v1"}},
	{"crypto/internal/fips140/rsa", "encrypt", "", "__mrencrypt", "", "__mrencryptPair", []string{"v0", "v1"}},
	{"crypto/internal/fips140/rsa", "encryptLargeExponent", "", "__mrencryptLargeExponent", "", "__mrencryptLargeExponentPair", []string{"v0", "v1"}},
	{"crypto/internal/fips140/rsa", "millerRabinIteration", "", "__mrmillerRabinIteration", "", "__mrmillerRabinIterationPair", []string{"v0", "v1"}},
	{"crypto/internal/fips140/rsa", "millerRabinSetup", "", "__mrmillerRabinSetup", "", "__mrmillerRabinSetupPair", []string{"v0", "v1"}},
	{"crypto/internal/fips140/rsa", "newPrivateKey", "", "__mrnewPrivateKey", "", "__mrnewPrivateKeyPair", []string{"v0", "v1"}},
	{"crypto/internal/fips140/rsa", "pkcs1v15ConstructEM", "", "__mrpkcs1v15ConstructEM", "", "__mrpkcs1v15ConstructEMPair", []string{"v0", "v1"}},
	{"crypto/internal/fips140/rsa", "randomPrime", "", "__mrrandomPrime", "", "__mrrandomPrimePair", []string{"v0", "v1"}},
	{"crypto/internal/fips140/rsa", "signPKCS1v15", "", "__mrsignPKCS1v15", "", "__mrsignPKCS1v15Pair", []string{"v0", "v1"}},
	{"crypto/internal/fips140/rsa", "totient", "", "__mrtotient", "", "__mrtotientPair", []string{"v0", "v1"}},
	{"crypto/internal/fips140/sha256", "AppendBinary", "Digest", "__mrDigest_AppendBinary", "", "__mrDigest_AppendBinaryPair", []string{"v0", "v1"}},
	{"crypto/internal/fips140/sha256", "Clone", "Digest", "__mrDigest_Clone", "", "__mrDigest_ClonePair", []string{"v0", "v1"}},
	{"crypto/internal/fips140/sha256", "MarshalBinary", "Digest", "__mrDigest_MarshalBinary", "", "__mrDigest_MarshalBinaryPair", []string{"v0", "v1"}},
	{"crypto/internal/fips140/sha256", "Write", "Digest", "__mrDigest_Write", "", "__mrDigest_WritePair", []string{"nn", "err"}},
	{"crypto/internal/fips140/sha256", "consumeUint32", "", "__mrconsumeUint32", "", "__mrconsumeUint32Pair", []string{"v0", "v1"}},
	{"crypto/internal/fips140/sha256", "consumeUint64", "", "__mrconsumeUint64", "", "__mrconsumeUint64Pair", []string{"v0", "v1"}},
	{"crypto/internal/fips140/sha3", "AppendBinary", "Digest", "__mrDigest_AppendBinary", "", "__mrDigest_AppendBinaryPair", []string{"v0", "v1"}},
	{"crypto/internal/fips140/sha3", "AppendBinary", "SHAKE", "__mrSHAKE_AppendBinary", "", "__mrSHAKE_AppendBinaryPair", []string{"v0", "v1"}},
	{"crypto/internal/fips140/sha3", "MarshalBinary", "Digest", "__mrDigest_MarshalBinary", "", "__mrDigest_MarshalBinaryPair", []string{"v0", "v1"}},
	{"crypto/internal/fips140/sha3", "MarshalBinary", "SHAKE", "__mrSHAKE_MarshalBinary", "", "__mrSHAKE_MarshalBinaryPair", []string{"v0", "v1"}},
	{"crypto/internal/fips140/sha3", "Read", "SHAKE", "__mrSHAKE_Read", "", "__mrSHAKE_ReadPair", []string{"n", "err"}},
	{"crypto/internal/fips140/sha3", "Write", "Digest", "__mrDigest_Write", "", "__mrDigest_WritePair", []string{"n", "err"}},
	{"crypto/internal/fips140/sha3", "Write", "SHAKE", "__mrSHAKE_Write", "", "__mrSHAKE_WritePair", []string{"n", "err"}},
	{"crypto/internal/fips140/sha3", "read", "Digest", "__mrDigest_read", "", "__mrDigest_readPair", []string{"n", "err"}},
	{"crypto/internal/fips140/sha3", "readGeneric", "Digest", "__mrDigest_readGeneric", "", "__mrDigest_readGenericPair", []string{"n", "err"}},
	{"crypto/internal/fips140/sha3", "write", "Digest", "__mrDigest_write", "", "__mrDigest_writePair", []string{"n", "err"}},
	{"crypto/internal/fips140/sha3", "writeGeneric", "Digest", "__mrDigest_writeGeneric", "", "__mrDigest_writeGenericPair", []string{"n", "err"}},
	{"crypto/internal/fips140/sha512", "AppendBinary", "Digest", "__mrDigest_AppendBinary", "", "__mrDigest_AppendBinaryPair", []string{"v0", "v1"}},
	{"crypto/internal/fips140/sha512", "Clone", "Digest", "__mrDigest_Clone", "", "__mrDigest_ClonePair", []string{"v0", "v1"}},
	{"crypto/internal/fips140/sha512", "MarshalBinary", "Digest", "__mrDigest_MarshalBinary", "", "__mrDigest_MarshalBinaryPair", []string{"v0", "v1"}},
	{"crypto/internal/fips140/sha512", "Write", "Digest", "__mrDigest_Write", "", "__mrDigest_WritePair", []string{"nn", "err"}},
	{"crypto/internal/fips140/sha512", "consumeUint64", "", "__mrconsumeUint64", "", "__mrconsumeUint64Pair", []string{"v0", "v1"}},
	{"crypto/pbkdf2", "Key", "", "__mrKey", "", "__mrKeyPair", []string{"v0", "v1"}},
	{"crypto/rsa", "Decrypt", "PrivateKey", "__mrPrivateKey_Decrypt", "", "__mrPrivateKey_DecryptPair", []string{"plaintext", "err"}},
	{"crypto/rsa", "DecryptOAEP", "", "__mrDecryptOAEP", "", "__mrDecryptOAEPPair", []string{"v0", "v1"}},
	{"crypto/rsa", "DecryptPKCS1v15", "", "__mrDecryptPKCS1v15", "", "__mrDecryptPKCS1v15Pair", []string{"v0", "v1"}},
	{"crypto/rsa", "EncryptOAEP", "", "__mrEncryptOAEP", "", "__mrEncryptOAEPPair", []string{"v0", "v1"}},
	{"crypto/rsa", "EncryptOAEPWithOptions", "", "__mrEncryptOAEPWithOptions", "", "__mrEncryptOAEPWithOptionsPair", []string{"v0", "v1"}},
	{"crypto/rsa", "EncryptPKCS1v15", "", "__mrEncryptPKCS1v15", "", "__mrEncryptPKCS1v15Pair", []string{"v0", "v1"}},
	{"crypto/rsa", "GenerateKey", "", "__mrGenerateKey", "", "__mrGenerateKeyPair", []string{"v0", "v1"}},
	{"crypto/rsa", "GenerateMultiPrimeKey", "", "__mrGenerateMultiPrimeKey", "", "__mrGenerateMultiPrimeKeyPair", []string{"v0", "v1"}},
	{"crypto/rsa", "Sign", "PrivateKey", "__mrPrivateKey_Sign", "", "__mrPrivateKey_SignPair", []string{"v0", "v1"}},
	{"crypto/rsa", "SignPKCS1v15", "", "__mrSignPKCS1v15", "", "__mrSignPKCS1v15Pair", []string{"v0", "v1"}},
	{"crypto/rsa", "SignPSS", "", "__mrSignPSS", "", "__mrSignPSSPair", []string{"v0", "v1"}},
	{"crypto/rsa", "boringPrivateKey", "", "__mrboringPrivateKey", "", "__mrboringPrivateKeyPair", []string{"v0", "v1"}},
	{"crypto/rsa", "boringPublicKey", "", "__mrboringPublicKey", "", "__mrboringPublicKeyPair", []string{"v0", "v1"}},
	{"crypto/rsa", "decryptOAEP", "", "__mrdecryptOAEP", "", "__mrdecryptOAEPPair", []string{"v0", "v1"}},
	{"crypto/rsa", "decryptPKCS1v15", "", "__mrdecryptPKCS1v15", "", "__mrdecryptPKCS1v15Pair", []string{"valid", "em", "index", "err"}},
	{"crypto/rsa", "encryptOAEP", "", "__mrencryptOAEP", "", "__mrencryptOAEPPair", []string{"v0", "v1"}},
	{"crypto/rsa", "fipsError2", "", "__mrfipsError2", "", "__mrfipsError2Pair", []string{"v0", "v1"}},
	{"crypto/rsa", "fipsPrivateKey", "", "__mrfipsPrivateKey", "", "__mrfipsPrivateKeyPair", []string{"v0", "v1"}},
	{"crypto/rsa", "fipsPublicKey", "", "__mrfipsPublicKey", "", "__mrfipsPublicKeyPair", []string{"v0", "v1"}},
	{"crypto/rsa", "precompute", "PrivateKey", "__mrPrivateKey_precompute", "", "__mrPrivateKey_precomputePair", []string{"v0", "v1"}},
	{"crypto/rsa", "precomputeLegacy", "PrivateKey", "__mrPrivateKey_precomputeLegacy", "", "__mrPrivateKey_precomputeLegacyPair", []string{"v0", "v1"}},
	// P3-M9: crypto/x509 multi-result functions (Java delegation via graalgo.x509.* intrinsics).
	{"crypto/x509", "ParseCertificate", "", "__mrParseCertificate", "", "__mrParseCertificatePair", []string{"v0", "v1"}},
	{"crypto/x509", "ParsePKIXPublicKey", "", "__mrParsePKIXPublicKey", "", "__mrParsePKIXPublicKeyPair", []string{"v0", "v1"}},
	{"crypto/x509", "ParsePKCS1PublicKey", "", "__mrParsePKCS1PublicKey", "", "__mrParsePKCS1PublicKeyPair", []string{"v0", "v1"}},
	{"golang.org/x/crypto/cryptobyte", "Bytes", "Builder", "__mrBuilder_Bytes", "", "__mrBuilder_BytesPair", []string{"v0", "v1"}},
}

// nistCurveFuncField describes one desugarable function field on the
// synthetic crypto/ecdh nistCurve struct (P3-M9 WS D). The fields
// (generate, newPrivateKey, newPublicKey, sharedSecret) are of function
// type with 2 results. Calls like c.generate(r) are FieldVal selections
// (not MethodVal), so they need a separate table and classifier arm.
// The pairType is the struct used to pack the 2 results; pairFields are
// the field names in result order.
type nistCurveFuncField struct {
	pkgPath    string   // synthetic package path ("crypto/ecdh")
	structName string   // struct containing the field ("nistCurve")
	fieldName  string   // function field name ("generate", etc.)
	pairType   string   // pair struct type name
	pairFields []string // field names in result order
}

// nistCurveFuncFields is the desugar table for the nistCurve function
// fields. Each entry was verified against the synthetic source:
// the field exists on nistCurve with a 2-result function type, and the
// pair struct exists with the named fields.
var nistCurveFuncFields = []nistCurveFuncField{
	{"crypto/ecdh", "nistCurve", "generate", "__mrnistCurve_generatePair", []string{"v0", "v1"}},
	{"crypto/ecdh", "nistCurve", "newPrivateKey", "__mrnistCurve_newPrivateKeyPair", []string{"v0", "v1"}},
	{"crypto/ecdh", "nistCurve", "newPublicKey", "__mrnistCurve_newPublicKeyPair", []string{"v0", "v1"}},
	{"crypto/ecdh", "nistCurve", "sharedSecret", "__mrnistCurve_sharedSecretPair", []string{"v0", "v1"}},
}

// osMultiResultFunc describes one desugarable P3-M8 (wave 2) multi-result
// function: the synthetic os package's multi-result API (exact gc 1.27.1
// signatures, fail-fast bodies) plus io/fs's DirEntry.Info (desugared
// through an os helper via the helperPkg override). Same contract as
// m7MultiResultFunc; kept as a separate table so the M7 entries (and
// their positional literals) are untouched.
type osMultiResultFunc struct {
	pkgPath    string   // declaring package ("os", or "io/fs" for DirEntry.Info)
	name       string   // function/method name
	recvType   string   // receiver named type ("" for package-level funcs)
	helper     string   // __-helper name
	helperPkg  string   // package holding the helper+pair ("" = pkgPath)
	pairType   string   // pair/triple struct type name
	pairFields []string // field names in result order (must match the
	// multi-result signature's result order positionally)
}

// osMultiResultFuncs is the desugar table for the synthetic os package
// (P3-M8 wave 2). Every entry was verified against synth_os.go: helper
// name, pair struct name, and field names/order match the source's
// __-helper and pair struct exactly.
var osMultiResultFuncs = []osMultiResultFunc{
	// os: package-level funcs.
	{"os", "Open", "", "__open", "", "openPair", []string{"f", "err"}},
	{"os", "Create", "", "__create", "", "openPair", []string{"f", "err"}},
	{"os", "OpenFile", "", "__openFile", "", "openPair", []string{"f", "err"}},
	{"os", "Stat", "", "__stat", "", "statPair", []string{"fi", "err"}},
	{"os", "Lstat", "", "__lstat", "", "statPair", []string{"fi", "err"}},
	{"os", "ReadFile", "", "__readFile", "", "readFilePair", []string{"b", "err"}},
	{"os", "ReadDir", "", "__readDir", "", "readDirPair", []string{"de", "err"}},
	{"os", "CreateTemp", "", "__createTemp", "", "openPair", []string{"f", "err"}},
	{"os", "MkdirTemp", "", "__mkdirTemp", "", "mkdirTempPair", []string{"s", "err"}},
	{"os", "Readlink", "", "__readlink", "", "readlinkPair", []string{"s", "err"}},
	{"os", "Getwd", "", "__getwd", "", "getwdPair", []string{"s", "err"}},
	{"os", "Pipe", "", "__pipe", "", "pipeTriple", []string{"r", "w", "err"}},
	{"os", "Hostname", "", "__hostname", "", "hostnamePair", []string{"s", "err"}},
	{"os", "LookupEnv", "", "__lookupEnv", "", "lookupEnvPair", []string{"s", "ok"}},
	{"os", "UserHomeDir", "", "__userHomeDir", "", "stringErrPair", []string{"s", "err"}},
	{"os", "UserConfigDir", "", "__userConfigDir", "", "stringErrPair", []string{"s", "err"}},
	{"os", "UserCacheDir", "", "__userCacheDir", "", "stringErrPair", []string{"s", "err"}},
	// os: methods on *File.
	{"os", "Read", "File", "__fileRead", "", "readPair", []string{"n", "err"}},
	{"os", "ReadAt", "File", "__fileReadAt", "", "readPair", []string{"n", "err"}},
	{"os", "Write", "File", "__fileWrite", "", "writePair", []string{"n", "err"}},
	{"os", "WriteAt", "File", "__fileWriteAt", "", "writePair", []string{"n", "err"}},
	{"os", "WriteString", "File", "__fileWriteString", "", "writePair", []string{"n", "err"}},
	{"os", "Seek", "File", "__fileSeek", "", "seekPair", []string{"off", "err"}},
	{"os", "Stat", "File", "__fileStat", "", "statPair", []string{"fi", "err"}},
	{"os", "ReadDir", "File", "__fileReadDir", "", "readDirPair", []string{"de", "err"}},
	{"os", "Readdir", "File", "__fileReaddir", "", "readdirPair", []string{"fi", "err"}},
	{"os", "Readdirnames", "File", "__fileReaddirnames", "", "readdirnamesPair", []string{"names", "err"}},
	{"os", "ReadFrom", "File", "__fileReadFrom", "", "readFromPair", []string{"n", "err"}},
	{"os", "WriteTo", "File", "__fileWriteTo", "", "writeToPair", []string{"n", "err"}},
	// os: multi-result fail-fast stubs (§6). The public stubs keep gc's
	// exact signatures with panic bodies (for the gc reference leg); the
	// single-result __-helpers panic with the identical message, and call
	// sites desugar to them — otherwise the call itself would fail at
	// export with "multi-result calls need WS D" instead of pinning the
	// runtime fail-fast (cf. syscall.Syscall → __syscall).
	{"os", "StartProcess", "", "__startProcess", "", "startProcessPair", []string{"proc", "err"}},
	{"os", "Wait", "Process", "__processWait", "", "processWaitPair", []string{"ps", "err"}},
	{"os", "SyscallConn", "File", "__fileSyscallConn", "", "syscallConnPair", []string{"rc", "err"}},
	{"os", "OpenRoot", "", "__openRoot", "", "openRootPair", []string{"root", "err"}},
	{"os", "Executable", "", "__executable", "", "executablePair", []string{"s", "err"}},
	// io/fs: DirEntry.Info (interface method) desugared through the os
	// helper __dirEntryInfo (helperPkg override — the concrete dirEntry
	// type lives in the synthetic os package).
	{"io/fs", "Info", "DirEntry", "__dirEntryInfo", "os", "infoPair", []string{"fi", "err"}},
	// os: the concrete (*dirEntry).Info method, mapped to the same
	// helper for itab construction (multiResultHelperFunc keys on the
	// concrete method's package/receiver).
	{"os", "Info", "dirEntry", "__dirEntryInfo", "", "infoPair", []string{"fi", "err"}},
}

// m9MultiResultFunc describes one desugarable P3-M9 (tier-4: net)
// multi-result function: the synthetic net/netip/net/url/net/textproto
// packages' multi-result API (exact gc 1.27.1 signatures, fail-fast
// bodies). Same contract as osMultiResultFunc; kept as a separate table
// so the earlier entries (and their positional literals) are untouched.
type m9MultiResultFunc struct {
	pkgPath    string   // declaring package ("net/netip", "net/url", "net", "net/textproto")
	name       string   // function/method name
	recvType   string   // receiver named type ("" for package-level funcs)
	helper     string   // __-helper name
	helperPkg  string   // package holding the helper+pair ("" = pkgPath)
	pairType   string   // pair/triple/quad struct type name
	pairFields []string // field names in result order (must match the
	// multi-result signature's result order positionally)
}

// m9MultiResultFuncs is the desugar table for the synthetic net packages
// (P3-M9 tier-4). Every entry was verified against the synth_*.go sources:
// helper name, pair struct name, and field names/order match exactly.
var m9MultiResultFuncs = []m9MultiResultFunc{
	// net/netip: package-level funcs.
	{"net/netip", "ParseAddr", "", "__parseAddr", "", "parseAddrPair", []string{"addr", "err"}},
	{"net/netip", "ParsePrefix", "", "__parsePrefix", "", "parsePrefixPair", []string{"prefix", "err"}},
	{"net/netip", "ParseAddrPort", "", "__parseAddrPort", "", "parseAddrPortPair", []string{"ap", "err"}},
	{"net/netip", "AddrFromSlice", "", "__addrFromSlice", "", "addrFromSlicePair", []string{"ip", "ok"}},
	// net/netip: methods on Addr (prependRecv).
	{"net/netip", "Prefix", "Addr", "__addrPrefix", "", "prefixPair", []string{"prefix", "err"}},
	{"net/netip", "AppendText", "Addr", "__addrAppendText", "", "bytesErrPair", []string{"b", "err"}},
	{"net/netip", "MarshalText", "Addr", "__addrMarshalText", "", "bytesErrPair", []string{"b", "err"}},
	{"net/netip", "AppendBinary", "Addr", "__addrAppendBinary", "", "bytesErrPair", []string{"b", "err"}},
	{"net/netip", "MarshalBinary", "Addr", "__addrMarshalBinary", "", "bytesErrPair", []string{"b", "err"}},
	// net/netip: methods on AddrPort (prependRecv).
	{"net/netip", "AppendText", "AddrPort", "__addrPortAppendText", "", "bytesErrPair", []string{"b", "err"}},
	{"net/netip", "MarshalText", "AddrPort", "__addrPortMarshalText", "", "bytesErrPair", []string{"b", "err"}},
	{"net/netip", "AppendBinary", "AddrPort", "__addrPortAppendBinary", "", "bytesErrPair", []string{"b", "err"}},
	{"net/netip", "MarshalBinary", "AddrPort", "__addrPortMarshalBinary", "", "bytesErrPair", []string{"b", "err"}},
	// net/netip: methods on Prefix (prependRecv).
	{"net/netip", "AppendText", "Prefix", "__prefixAppendText", "", "bytesErrPair", []string{"b", "err"}},
	{"net/netip", "MarshalText", "Prefix", "__prefixMarshalText", "", "bytesErrPair", []string{"b", "err"}},
	{"net/netip", "AppendBinary", "Prefix", "__prefixAppendBinary", "", "bytesErrPair", []string{"b", "err"}},
	{"net/netip", "MarshalBinary", "Prefix", "__prefixMarshalBinary", "", "bytesErrPair", []string{"b", "err"}},
	// net/url: package-level funcs.
	{"net/url", "QueryUnescape", "", "__queryUnescape", "", "stringErrPair", []string{"s", "err"}},
	{"net/url", "PathUnescape", "", "__pathUnescape", "", "stringErrPair", []string{"s", "err"}},
	{"net/url", "Parse", "", "__parseURLPub", "", "parseURLPair", []string{"u", "err"}},
	{"net/url", "ParseRequestURI", "", "__parseRequestURIPub", "", "parseURLPair", []string{"u", "err"}},
	{"net/url", "ParseQuery", "", "__parseQueryPub", "", "parseQueryPair", []string{"v", "err"}},
	{"net/url", "JoinPath", "", "__joinPathPub", "", "stringErrPair", []string{"s", "err"}},
	// net/url: methods (prependRecv).
	{"net/url", "Password", "Userinfo", "__userinfoPassword", "", "passwordPair", []string{"s", "ok"}},
	{"net/url", "Parse", "URL", "__urlParsePub", "", "parseURLPair", []string{"u", "err"}},
	// net: package-level funcs (socket path; __-helpers in synth_net.go).
	{"net", "Pipe", "", "__pipe", "", "pipePair", []string{"c1", "c2"}},
	{"net", "Dial", "", "__dial", "", "dialPair", []string{"c", "err"}},
	{"net", "DialTimeout", "", "__dialTimeout", "", "dialPair", []string{"c", "err"}},
	{"net", "Listen", "", "__listen", "", "listenPair", []string{"l", "err"}},
	{"net", "ListenPacket", "", "__listenPacket", "", "listenPacketPair", []string{"c", "err"}},
	{"net", "ResolveIPAddr", "", "__resolveIPAddr", "", "resolveIPAddrPair", []string{"ip", "err"}},
	{"net", "ResolveTCPAddr", "", "__resolveTCPAddr", "", "resolveTCPAddrPair", []string{"addr", "err"}},
	{"net", "ResolveUDPAddr", "", "__resolveUDPAddr", "", "resolveUDPAddrPair", []string{"addr", "err"}},
	{"net", "ParseCIDR", "", "__parseCIDR", "", "parseCIDRPair", []string{"ip", "ipnet", "err"}},
	{"net", "ParseMAC", "", "__parseMAC", "", "parseMACPair", []string{"hw", "err"}},
	{"net", "SplitHostPort", "", "__splitHostPort", "", "splitHostPortPair", []string{"host", "port"}},
	// net: methods on TCPListener / Conn (prependRecv).
	{"net", "Accept", "TCPListener", "__tcpListenerAccept", "", "acceptPair", []string{"c", "err"}},
	{"net", "Read", "TCPConn", "__tcpConnRead", "", "readPair", []string{"n", "err"}},
	{"net", "Write", "TCPConn", "__tcpConnWrite", "", "writePair", []string{"n", "err"}},
	{"net", "Read", "UDPConn", "__udpConnRead", "", "readPair", []string{"n", "err"}},
	{"net", "Write", "UDPConn", "__udpConnWrite", "", "writePair", []string{"n", "err"}},
	{"net", "ReadFrom", "UDPConn", "__udpConnReadFrom", "", "readFromPair", []string{"n", "addr", "err"}},
	{"net", "WriteTo", "UDPConn", "__udpConnWriteTo", "", "writePair", []string{"n", "err"}},
}

// ioMultiResultFuncs is the desugar table for the synthetic io package's
// newly-real multi-result API (P3-M8, WS D): the exact gc 1.27.1
// signatures are kept with fail-fast bodies for type-checking, and the
// real implementations live in the single-result __-helpers declared in
// synth_io.go. Same contract as m7MultiResultFunc; kept as a separate
// table so the M7 entries (and their positional literals) are untouched.
// CopyBuffer maps to __copyBufferChecked (which keeps gc's empty-buffer
// panic); Copy maps to __copy and CopyN to __copyN.
var ioMultiResultFuncs = []m7MultiResultFunc{
	{"io", "WriteString", "", "__writeString", "ioWritePair", []string{"n", "err"}},
	{"io", "ReadAtLeast", "", "__readAtLeast", "readAtLeastPair", []string{"n", "err"}},
	{"io", "ReadFull", "", "__readFull", "readFullPair", []string{"n", "err"}},
	{"io", "Copy", "", "__copy", "copyPair", []string{"written", "err"}},
	{"io", "CopyN", "", "__copyN", "copyNPair", []string{"written", "err"}},
	{"io", "CopyBuffer", "", "__copyBufferChecked", "copyBufferPair", []string{"written", "err"}},
	{"io", "ReadAll", "", "__readAll", "readAllPair", []string{"b", "err"}},
	{"io", "Read", "LimitedReader", "__limitedReaderRead", "limitedReaderReadPair", []string{"n", "err"}},
	{"io", "Write", "discard", "__discardWrite", "ioWritePair", []string{"n", "err"}},
	{"io", "WriteString", "discard", "__discardWriteString", "ioWritePair", []string{"n", "err"}},
	// P3-M9: synthetic testing/iotest readers/writers. Each Read/Write
	// desugars to its pair-returning __-helper (see synth_iotest.go);
	// the pair field names/types match the method results positionally.
	{"testing/iotest", "Read", "oneByteReader", "__oneByteReaderRead", "oneByteReaderReadPair", []string{"n", "err"}},
	{"testing/iotest", "Read", "halfReader", "__halfReaderRead", "halfReaderReadPair", []string{"n", "err"}},
	{"testing/iotest", "Read", "dataErrReader", "__dataErrReaderRead", "dataErrReaderReadPair", []string{"n", "err"}},
	{"testing/iotest", "Read", "timeoutReader", "__timeoutReaderRead", "timeoutReaderReadPair", []string{"n", "err"}},
	{"testing/iotest", "Read", "errReader", "__errReaderRead", "errReaderReadPair", []string{"n", "err"}},
	{"testing/iotest", "Write", "truncateWriter", "__truncateWriterWrite", "truncateWriterWritePair", []string{"n", "err"}},
	// P3-M9: synthetic testing internal writers. outputWriter.Write and
	// indenter.Write desugar to their pair-returning __-helpers (see
	// synth_testing.go); the pair field names/types match the method
	// results positionally.
	{"testing", "Write", "outputWriter", "__outputWriterWrite", "outputWriterWritePair", []string{"n", "err"}},
	{"testing", "Write", "indenter", "__indenterWrite", "indenterWritePair", []string{"n", "err"}},
}

// m8IfaceMultiResultMethod describes one P3-M8 (WS D) multi-result
// interface method: an interface-typed call site (r.Read(buf)) dispatches
// through the itab to the concrete type's pair-returning __-helper, and
// the ifacemethod node yields the helper's pair. pairType is the static
// result type of the ifacemethod node (declared in pkgPath); pairFields
// are extracted positionally, exactly like the concrete Appendix M
// desugar. Matching is structural on (method name, result count,
// positional result types) — see classifyIfaceMultiResultCall — so
// user-declared interfaces with the same method shape dispatch too.
type m8IfaceMultiResultMethod struct {
	pkgPath    string   // synthetic package holding the pair type ("io")
	method     string   // method name ("Read")
	pairType   string   // pair struct type name in pkgPath
	pairFields []string // field names in result order
}

// m8IfaceMultiResultMethods is the interface-dispatch table. Every entry
// was verified against synth_io.go: the pair struct exists with exactly
// these fields in this order, and every concrete Read/Write __-helper
// returns a pair with positionally identical field types (checked loudly
// at export by multiResultHelperFunc).
var m8IfaceMultiResultMethods = []m8IfaceMultiResultMethod{
	{"io", "Read", "ioReadPair", []string{"n", "err"}},
	{"io", "Write", "ioWritePair", []string{"n", "err"}},
	{"io", "WriteString", "ioWritePair", []string{"n", "err"}},
	// P3-M9 (tier-5: compress): io.ByteReader.ReadByte (flate.Reader,
	// bzip2's bitReader, lzw's reader all read through ByteReader
	// interfaces). Concrete *bufio.Reader has the m7 entry; the pair
	// lives in synth_io.go.
	{"io", "ReadByte", "ioByteReaderPair", []string{"c", "err"}},
	// P3-M8 (wave 2, reflect): the Type interface's multi-result methods.
	// The itab maps each to the concrete rtype's pair-returning
	// __-helper (wired via multiResultHelperFor/methodTableEntries); the
	// pair field names/types are positionally identical to the method's
	// results (checked by classifyIfaceMultiResultCall).
	{"reflect", "MethodByName", "reflectMethodPair", []string{"m", "ok"}},
	{"reflect", "FieldByName", "reflectStructFieldPair", []string{"f", "ok"}},
	{"reflect", "FieldByNameFunc", "reflectStructFieldPair", []string{"f", "ok"}},
	// P3-M8b (wave 2, crypto): hash.Cloner.Clone and the
	// encoding.BinaryMarshaler/BinaryAppender methods, dispatched on the
	// hash.Cloner / encoding.BinaryMarshaler interfaces. The itab maps
	// each to the concrete digest's pair-returning __-helper (wired via
	// the m8MultiResultFuncs entries below); pair field names/types are
	// positionally identical to the methods' results (checked by
	// classifyIfaceMultiResultCall).
	{"hash", "Clone", "ClonePair", []string{"C", "Err"}},
	{"hash", "MarshalBinary", "binaryMarshalPair", []string{"b", "err"}},
	{"hash", "AppendBinary", "binaryAppendPair", []string{"b", "err"}},
	// P3-M9 (tier-5): elliptic.Curve and ecdh.Curve interface methods.
	// The concrete helpers are wired via m8MultiResultFuncs +
	// methodTableEntries; the pair types are positionally identical
	// across implementations (checked by classifyIfaceMultiResultCall).
	{"crypto/elliptic", "Add", "__mrnistCurve_AddPair", []string{"v0", "v1"}},                       // elliptic.Curve.Add
	{"crypto/elliptic", "Double", "__mrnistCurve_DoublePair", []string{"v0", "v1"}},                 // elliptic.Curve.Double
	{"crypto/elliptic", "ScalarMult", "__mrnistCurve_ScalarMultPair", []string{"v0", "v1"}},         // elliptic.Curve.ScalarMult
	{"crypto/elliptic", "ScalarBaseMult", "__mrnistCurve_ScalarBaseMultPair", []string{"v0", "v1"}}, // elliptic.Curve.ScalarBaseMult
	{"crypto/ecdh", "GenerateKey", "__mrnistCurve_GenerateKeyPair", []string{"v0", "v1"}},           // ecdh.Curve.GenerateKey
	{"crypto/ecdh", "NewPrivateKey", "__mrnistCurve_NewPrivateKeyPair", []string{"v0", "v1"}},       // ecdh.Curve.NewPrivateKey
	{"crypto/ecdh", "NewPublicKey", "__mrnistCurve_NewPublicKeyPair", []string{"v0", "v1"}},         // ecdh.Curve.NewPublicKey
	{"crypto/ecdh", "ecdh", "__mrnistCurve_ecdhPair", []string{"v0", "v1"}},                         // ecdh.Curve.ecdh (interface method)
	// P3-M9 (tier-5): elliptic.Curve and ecdh.Curve interface methods.
	// The concrete helpers are wired via m8MultiResultFuncs +
	// methodTableEntries; the pair types are positionally identical
	// across implementations (checked by classifyIfaceMultiResultCall).
	{"crypto/elliptic", "Add", "__mrnistCurve_AddPair", []string{"v0", "v1"}},                       // elliptic.Curve.Add
	{"crypto/elliptic", "Double", "__mrnistCurve_DoublePair", []string{"v0", "v1"}},                 // elliptic.Curve.Double
	{"crypto/elliptic", "ScalarMult", "__mrnistCurve_ScalarMultPair", []string{"v0", "v1"}},         // elliptic.Curve.ScalarMult
	{"crypto/elliptic", "ScalarBaseMult", "__mrnistCurve_ScalarBaseMultPair", []string{"v0", "v1"}}, // elliptic.Curve.ScalarBaseMult
	{"crypto/ecdh", "GenerateKey", "__mrnistCurve_GenerateKeyPair", []string{"v0", "v1"}},           // ecdh.Curve.GenerateKey
	{"crypto/ecdh", "NewPrivateKey", "__mrnistCurve_NewPrivateKeyPair", []string{"v0", "v1"}},       // ecdh.Curve.NewPrivateKey
	{"crypto/ecdh", "NewPublicKey", "__mrnistCurve_NewPublicKeyPair", []string{"v0", "v1"}},         // ecdh.Curve.NewPublicKey
	// P3-M9 (WS H + WS D, graalgo-wsh-wsd-interaction): fips140/ecdsa
	// Point[P] interface multi-result methods, dispatched via the WS H
	// ifacemethod fallback when called on a type parameter. The pair
	// types are non-generic (v0 any) for wire simplicity; the itab
	// dispatches to the concrete nistec helper at runtime.
	{"crypto/internal/fips140/ecdsa", "SetBytes", "__mrPointIfaceSetBytesPair", []string{"v0", "v1"}},
	{"crypto/internal/fips140/ecdsa", "BytesX", "__mrPointIfaceBytesXPair", []string{"v0", "v1"}},
	{"crypto/internal/fips140/ecdsa", "ScalarMult", "__mrPointIfaceScalarMultPair", []string{"v0", "v1"}},
	{"crypto/internal/fips140/ecdsa", "ScalarBaseMult", "__mrPointIfaceScalarBaseMultPair", []string{"v0", "v1"}},
	// P3-M9 (ecdsa-blockers): fmt.State.Precision/Width interface methods.
	// The synthetic math/big Format methods call s.Precision()/s.Width();
	// the pair types live in math/big (see synth_math_big.go). These are
	// never dispatched at runtime (FmtCallNode formats directly); the
	// entries exist so the frontend can classify the calls.
	{"math/big", "Precision", "fmtPrecisionPair", []string{"prec", "ok"}},
	{"math/big", "Width", "fmtWidthPair", []string{"wid", "ok"}},
	// P3-M9 (WS D): crypto/elliptic.unmarshaler interface (2-result).
	// The Unmarshal/UnmarshalCompressed methods dispatch via itab to the
	// concrete nistCurve helpers. Pair types live in synthetic elliptic.
	{"crypto/elliptic", "Unmarshal", "__mrUnmarshalPair", []string{"x", "y"}},
	{"crypto/elliptic", "UnmarshalCompressed", "__mrUnmarshalCompressedPair", []string{"x", "y"}},
}

// mrPlan is the desugar plan for one classified call.
type mrPlan struct {
	kind       mrKind
	helperPkg  string // "context" or "time"
	helperName string // "__withCancel", "__deadlineOf", ...
	// tmpType is the helper's single result type (the __p5mr_N temp).
	tmpType types.Type
	// For mrWithCancel/mrWithDeadline: the second extraction is a
	// method call on the temp (methodRecv.methodName()).
	methodRecv *types.Named
	methodName string
	// For mrDeadline/mrParseDur: the pair field names ([d, ok]/[d, err]).
	pairFields  []string
	prependRecv bool
}

// classifyMultiResultCall recognizes the Appendix M operations (the 5
// reachable ones; the mrDeadline path is retained defense-in-depth for
// WS D — see the package header). It returns nil when c is not one of
// them.
func (e *exporter) classifyMultiResultCall(c *ast.CallExpr) *mrPlan {
	// P3-M9 (WS D): user-defined package-level multi-result functions
	// called as `foo()` (Ident, not SelectorExpr).
	if id, ok := c.Fun.(*ast.Ident); ok {
		if obj, ok := e.info.Uses[id].(*types.Func); ok {
			if mrInfo, ok := e.userMRFuncs[obj]; ok {
				return &mrPlan{
					kind:       mrUser,
					tmpType:    mrInfo.pairType,
					pairFields: mrInfo.pairFields,
				}
			}
			// P3-M9 (WS D): same-package Ident calls to synthetic
			// multi-result functions (e.g. parseSignature in the
			// synthetic crypto/ecdsa package, 3 results) desugar via
			// the tables, like package-qualified calls. The
			// multi-result stub is skipped at synthetic import; the
			// __-helper carries the real implementation. N-result
			// generally: the tables and desugarExtract are already
			// arity-generic (want := len(plan.pairFields)).
			if obj.Pkg() != nil {
				pkgPath := obj.Pkg().Path()
				if plan := classifyM7MultiResultCall(pkgPath, obj); plan != nil {
					return plan
				}
				if plan := classifyOsMultiResultCall(pkgPath, obj); plan != nil {
					return plan
				}
				if plan := classifyM8MultiResultCall(pkgPath, obj); plan != nil {
					return plan
				}
				if plan := classifyIoMultiResultCall(pkgPath, obj); plan != nil {
					return plan
				}
				if plan := classifyM9MultiResultCall(pkgPath, obj); plan != nil {
					return plan
				}
			}
		}
		return nil
	}
	sel, ok := c.Fun.(*ast.SelectorExpr)
	if !ok {
		return nil
	}
	ctxPkg := getSyntheticPackage("context").pkg
	timePkg := getSyntheticPackage("time").pkg
	named := func(pkg *types.Package, name string) *types.Named {
		obj := pkg.Scope().Lookup(name)
		if obj == nil {
			return nil
		}
		nt, _ := obj.Type().(*types.Named)
		return nt
	}
	// Package-qualified call: context.WithCancel(...) etc.
	if id, ok := sel.X.(*ast.Ident); ok {
		if pn, ok := e.info.Uses[id].(*types.PkgName); ok {
			obj := e.info.Uses[sel.Sel]
			fn, ok := obj.(*types.Func)
			if !ok {
				return nil
			}
			// P3-M7 (wave 1): encoding/* package-level multi-result
			// members (hex.Decode, binary.Uvarint, ascii85.Decode,
			// pem.Decode, asn1.Marshal/Unmarshal).
			if plan := classifyM7MultiResultCall(pn.Imported().Path(), fn); plan != nil {
				return plan
			}
			// P3-M8 (wave 2): the synthetic os package's multi-result
			// API (osMultiResultFuncs table above).
			if plan := classifyOsMultiResultCall(pn.Imported().Path(), fn); plan != nil {
				return plan
			}
			// P3-M8 (wave 2): the crypto/* multi-result API
			// (m8MultiResultFuncs table above).
			if plan := classifyM8MultiResultCall(pn.Imported().Path(), fn); plan != nil {
				return plan
			}
			// P3-M8 (WS D): the synthetic io package's multi-result API
			// (ioMultiResultFuncs table above).
			if plan := classifyIoMultiResultCall(pn.Imported().Path(), fn); plan != nil {
				return plan
			}
			// P3-M9 (tier-4: net): the synthetic net packages' multi-result
			// API (m9MultiResultFuncs table above).
			if plan := classifyM9MultiResultCall(pn.Imported().Path(), fn); plan != nil {
				return plan
			}
			switch pn.Imported().Path() {
			case "context":
				switch fn.Name() {
				case "WithCancel", "WithCancelCause":
					method := "cancelFunc"
					if fn.Name() == "WithCancelCause" {
						method = "cancelCauseFunc"
					}
					nt := named(ctxPkg, "cancelCtx")
					if nt == nil {
						return nil
					}
					return &mrPlan{
						kind: mrWithCancel, helperPkg: "context",
						helperName: "__with" + fn.Name()[4:],
						tmpType:    types.NewPointer(nt),
						methodRecv: nt, methodName: method,
					}
				case "WithDeadline", "WithTimeout":
					nt := named(ctxPkg, "timerCtx")
					if nt == nil {
						return nil
					}
					return &mrPlan{
						kind: mrWithDeadline, helperPkg: "context",
						helperName: "__with" + fn.Name()[4:],
						tmpType:    types.NewPointer(nt),
						methodRecv: nt, methodName: "cancelFunc",
					}
				}
			case "time":
				if fn.Name() == "ParseDuration" {
					nt := named(timePkg, "durationPair")
					if nt == nil {
						return nil
					}
					return &mrPlan{
						kind: mrParseDur, helperPkg: "time",
						helperName: "__parseDuration",
						tmpType:    nt, pairFields: []string{"d", "err"},
					}
				}
			}
			return nil
		}
	}
	// Method call: ctx.Deadline(). Must be the synthetic context method
	// (declared in package "context", 2 results) — a user type's
	// Deadline method is not desugared. NOTE: unreachable in M5 (the
	// synthetic Context interface omits Deadline post-afc0dfb, so no
	// user call site type-checks); retained for WS D — see the package
	// header.
	if selection, ok := e.info.Selections[sel]; ok && selection.Kind() == types.MethodVal {
		if f, ok := selection.Obj().(*types.Func); ok && isDeadlineMethod(f) {
			nt := named(ctxPkg, "deadlinePair")
			if nt == nil {
				return nil
			}
			return &mrPlan{
				kind: mrDeadline, helperPkg: "context",
				helperName: "__deadlineOf",
				tmpType:    nt, pairFields: []string{"d", "ok"},
			}
		}
		if f, ok := selection.Obj().(*types.Func); ok {
			if helper, ok := isXmlTokenMethod(f); ok {
				xmlPkg := getSyntheticPackage("encoding/xml").pkg
				nt := named(xmlPkg, "tokenPair")
				if nt == nil {
					return nil
				}
				return &mrPlan{
					kind: mrXmlToken, helperPkg: "encoding/xml",
					helperName: helper, prependRecv: true,
					tmpType: nt, pairFields: []string{"tok", "err"},
				}
			}
		}
		// P3-M7 (wave 1): encoding/* multi-result methods
		// (base32/base64 (*Encoding).Decode/DecodeString/AppendDecode).
		// The receiver's named type must match the table — a user method
		// with the same name is never desugared.
		if f, ok := selection.Obj().(*types.Func); ok && f.Pkg() != nil {
			// P3-M9 (WS D): user-defined multi-result methods transformed
			// to pair-returning form. Check the userMRFuncs map before
			// the synthetic tables.
			if mrInfo, ok := e.userMRFuncs[f]; ok {
				return &mrPlan{
					kind:        mrUser,
					tmpType:     mrInfo.pairType,
					pairFields:  mrInfo.pairFields,
					prependRecv: false,
				}
			}
			// P3-M9 (WS D): fips140cache.Cache.Get — the synthetic cache
			// is a no-memoization stub, so inline the new() call directly.
			// This avoids generic helper instantiation complexity.
			if f.Pkg().Path() == "crypto/internal/fips140cache" && f.Name() == "Get" {
				sig, _ := f.Type().(*types.Signature)
				if sig != nil && sig.Recv() != nil {
					rt := sig.Recv().Type()
					if pt, ok := rt.(*types.Pointer); ok {
						rt = pt.Elem()
					}
					if nt, ok := rt.(*types.Named); ok && nt.Obj() != nil && nt.Obj().Name() == "Cache" {
						// pairFields is a placeholder for the arity check;
						// the desugar inlines new() directly, no pair used.
						return &mrPlan{kind: mrCacheGet, pairFields: []string{"v", "err"}}
					}
				}
			}
			if plan := classifyM7MultiResultCall(f.Pkg().Path(), f); plan != nil {
				return plan
			}
			// P3-M8 (wave 2): os methods + io/fs DirEntry.Info.
			if plan := classifyOsMultiResultCall(f.Pkg().Path(), f); plan != nil {
				return plan
			}
			// P3-M8 (wave 2): crypto/* multi-result API.
			if plan := classifyM8MultiResultCall(f.Pkg().Path(), f); plan != nil {
				return plan
			}
			// P3-M8 (WS D): the synthetic io package's multi-result
			// API (WriteString, ReadAtLeast, ReadFull, Copy, CopyN,
			// CopyBuffer, ReadAll, LimitedReader.Read,
			// discard.Write/WriteString).
			if plan := classifyIoMultiResultCall(f.Pkg().Path(), f); plan != nil {
				return plan
			}
			// P3-M8 (WS D): multi-result calls on interface values
			// (r.Read(buf) with r of interface type). Concrete
			// receivers are handled by the table classifiers above —
			// this arm is interface-only, so a concrete multi-result
			// method with no helper never reaches the ifacemethod
			// path.
			if recvIface(selection.Recv()) != nil {
				if plan := e.classifyIfaceMultiResultCall(selection); plan != nil {
					return plan
				}
			}
			// P3-M9 (WS H + WS D, graalgo-wsh-wsd-interaction):
			// multi-result calls on type parameters (e.g.,
			// c.newPoint().SetBytes(Q) where the receiver is P
			// constrained by Point[P]). These dispatch via the WS H
			// ifacemethod fallback; classify with the lenient
			// fallback classifier.
			if _, isTP := selection.Recv().(*types.TypeParam); isTP {
				if plan := e.classifyIfaceFallbackMultiResultCall(selection); plan != nil {
					return plan
				}
			}
		}
	}
	// P3-M9 (WS D): function field call (e.g., c.generate(r) where
	// generate is a field of function type on nistCurve). The selection
	// is FieldVal (not MethodVal), and the field type is *types.Signature
	// with 2 results. Look up the field in the nistCurveFuncFields table.
	if selection, ok := e.info.Selections[sel]; ok && selection.Kind() == types.FieldVal {
		if v, ok := selection.Obj().(*types.Var); ok {
			if sig, ok := v.Type().(*types.Signature); ok && sig.Results().Len() == 2 {
				if plan := classifyNistCurveFuncField(selection, v); plan != nil {
					return plan
				}
			}
		}
	}
	return nil
}

// classifyNistCurveFuncField recognizes function field calls on the
// synthetic crypto/ecdh nistCurve struct (P3-M9 WS D). The selection must
// be a FieldVal on nistCurve, and the field name must be in the
// nistCurveFuncFields table. Returns nil when the call is not one of
// the table's fields.
func classifyNistCurveFuncField(selection *types.Selection, v *types.Var) *mrPlan {
	// Get the struct type from the receiver.
	recv := selection.Recv()
	if recv == nil {
		return nil
	}
	// Unwrap pointer if needed.
	rt := recv
	if pt, ok := rt.(*types.Pointer); ok {
		rt = pt.Elem()
	}
	nt, ok := rt.(*types.Named)
	if !ok || nt.Obj() == nil {
		return nil
	}
	// Check package path and struct name.
	if nt.Obj().Pkg() == nil || nt.Obj().Pkg().Path() != "crypto/ecdh" {
		return nil
	}
	if nt.Obj().Name() != "nistCurve" {
		return nil
	}
	// Look up the field in the table.
	fieldName := v.Name()
	for _, ff := range nistCurveFuncFields {
		if ff.pkgPath != "crypto/ecdh" || ff.structName != "nistCurve" || ff.fieldName != fieldName {
			continue
		}
		// Verify the signature has 2 results (already checked by caller,
		// but verify arity matches pairFields).
		sig, ok := v.Type().(*types.Signature)
		if !ok || sig.Results().Len() != len(ff.pairFields) {
			return nil
		}
		// Look up the pair type in the synthetic package.
		sp := getSyntheticPackage(ff.pkgPath)
		obj := sp.pkg.Scope().Lookup(ff.pairType)
		if obj == nil {
			return nil
		}
		pairNt, _ := obj.Type().(*types.Named)
		if pairNt == nil {
			return nil
		}
		return &mrPlan{
			kind:       mrFuncField,
			tmpType:    pairNt,
			pairFields: ff.pairFields,
		}
	}
	return nil
}

func isXmlTokenMethod(f *types.Func) (string, bool) {
	if f == nil || f.Pkg() == nil || f.Pkg().Path() != "encoding/xml" {
		return "", false
	}
	if f.Name() != "Token" && f.Name() != "RawToken" {
		return "", false
	}
	sig, ok := f.Type().(*types.Signature)
	if !ok || sig.Results().Len() != 2 {
		return "", false
	}
	recv := sig.Recv()
	if recv == nil {
		return "", false
	}
	pt, ok := recv.Type().(*types.Pointer)
	if !ok {
		return "", false
	}
	nt, ok := pt.Elem().(*types.Named)
	if !ok || nt.Obj() == nil || nt.Obj().Name() != "Decoder" {
		return "", false
	}
	if nt.Obj().Pkg() == nil || nt.Obj().Pkg().Path() != "encoding/xml" {
		return "", false
	}
	if f.Name() == "Token" {
		return "__tokenOf", true
	}
	return "__rawTokenOf", true
}

// classifyM7MultiResultCall recognizes the P3-M7 (wave 1) Appendix M
// operations (the m7MultiResultFuncs table). pkgPath is the called
// function's declaring package; for methods the receiver's named type
// must match the table entry — a user type with the same method name
// (e.g. a local Encoding.Decode) is never desugared. Returns nil when
// the call is not one of the table's operations.
func classifyM7MultiResultCall(pkgPath string, fn *types.Func) *mrPlan {
	if fn == nil || fn.Pkg() == nil || fn.Pkg().Path() != pkgPath {
		return nil
	}
	sig, ok := fn.Type().(*types.Signature)
	if !ok {
		return nil
	}
	recvName := ""
	if recv := sig.Recv(); recv != nil {
		rt := recv.Type()
		if pt, ok := rt.(*types.Pointer); ok {
			rt = pt.Elem()
		}
		if nt, ok := rt.(*types.Named); ok && nt.Obj() != nil {
			recvName = nt.Obj().Name()
		}
	}
	// P3-M8 (wave 2, reflect): the Type interface's multi-result methods
	// (MethodByName, FieldByName, FieldByNameFunc) are usually called via
	// the Type interface, where the selected method has no receiver. Retry
	// such reflect interface methods against the rtype entries.
	if recvName == "" && pkgPath == "reflect" {
		if plan := classifyM7MultiResultCallRtype(pkgPath, fn); plan != nil {
			return plan
		}
	}
	for _, mf := range m7MultiResultFuncs {
		if mf.pkgPath != pkgPath || mf.name != fn.Name() || mf.recvType != recvName {
			continue
		}
		// The result count must match the pair/triple arity: a signature
		// change in the synthetic source fails the desugar loudly instead
		// of extracting the wrong fields.
		if sig.Results().Len() != len(mf.pairFields) {
			return nil
		}
		sp := getSyntheticPackage(mf.pkgPath)
		obj := sp.pkg.Scope().Lookup(mf.pairType)
		if obj == nil {
			return nil
		}
		nt, _ := obj.Type().(*types.Named)
		if nt == nil {
			return nil
		}
		return &mrPlan{
			kind: mrM7, helperPkg: mf.pkgPath,
			helperName: mf.helper,
			tmpType:    nt, pairFields: mf.pairFields,
			prependRecv: mf.recvType != "",
		}
	}
	return nil
}

// classifyOsMultiResultCall recognizes the P3-M8 (wave 2) Appendix M
// operations (the osMultiResultFuncs table). pkgPath is the called
// function's declaring package; for methods the receiver's named type
// must match the table entry — a user type with the same method name
// is never desugared. helperPkg overrides the package the __-helper and
// pair struct are looked up in (used for io/fs DirEntry.Info, whose
// helper lives in the synthetic os package). Returns nil when the call
// is not one of the table's operations.
// classifyM9MultiResultCall desugars the P3-M9 (tier-4: net)
// multi-result API (m9MultiResultFuncs table above). Same contract as
// classifyOsMultiResultCall.
func classifyM9MultiResultCall(pkgPath string, fn *types.Func) *mrPlan {
	if fn == nil || fn.Pkg() == nil || fn.Pkg().Path() != pkgPath {
		return nil
	}
	sig, ok := fn.Type().(*types.Signature)
	if !ok {
		return nil
	}
	recvName := ""
	if recv := sig.Recv(); recv != nil {
		rt := recv.Type()
		if pt, ok := rt.(*types.Pointer); ok {
			rt = pt.Elem()
		}
		if nt, ok := rt.(*types.Named); ok && nt.Obj() != nil {
			recvName = nt.Obj().Name()
		}
	}
	for _, mf := range m9MultiResultFuncs {
		if mf.pkgPath != pkgPath || mf.name != fn.Name() || mf.recvType != recvName {
			continue
		}
		if sig.Results().Len() != len(mf.pairFields) {
			return nil
		}
		helperPkg := mf.helperPkg
		if helperPkg == "" {
			helperPkg = mf.pkgPath
		}
		sp := getSyntheticPackage(helperPkg)
		obj := sp.pkg.Scope().Lookup(mf.pairType)
		if obj == nil {
			return nil
		}
		nt, _ := obj.Type().(*types.Named)
		if nt == nil {
			return nil
		}
		return &mrPlan{
			kind: mrM7, helperPkg: helperPkg,
			helperName: mf.helper,
			tmpType:    nt, pairFields: mf.pairFields,
			prependRecv: mf.recvType != "",
		}
	}
	return nil
}

func classifyOsMultiResultCall(pkgPath string, fn *types.Func) *mrPlan {
	if fn == nil || fn.Pkg() == nil || fn.Pkg().Path() != pkgPath {
		return nil
	}
	sig, ok := fn.Type().(*types.Signature)
	if !ok {
		return nil
	}
	recvName := ""
	if recv := sig.Recv(); recv != nil {
		rt := recv.Type()
		if pt, ok := rt.(*types.Pointer); ok {
			rt = pt.Elem()
		}
		if nt, ok := rt.(*types.Named); ok && nt.Obj() != nil {
			recvName = nt.Obj().Name()
		}
	}
	for _, mf := range osMultiResultFuncs {
		if mf.pkgPath != pkgPath || mf.name != fn.Name() || mf.recvType != recvName {
			continue
		}
		// The result count must match the pair/triple arity: a signature
		// change in the synthetic source fails the desugar loudly instead
		// of extracting the wrong fields.
		if sig.Results().Len() != len(mf.pairFields) {
			return nil
		}
		helperPkg := mf.helperPkg
		if helperPkg == "" {
			helperPkg = mf.pkgPath
		}
		sp := getSyntheticPackage(helperPkg)
		obj := sp.pkg.Scope().Lookup(mf.pairType)
		if obj == nil {
			return nil
		}
		nt, _ := obj.Type().(*types.Named)
		if nt == nil {
			return nil
		}
		return &mrPlan{
			kind: mrM7, helperPkg: helperPkg,
			helperName: mf.helper,
			tmpType:    nt, pairFields: mf.pairFields,
			prependRecv: mf.recvType != "",
		}
	}
	return nil
}

// classifyM8MultiResultCall recognizes the P3-M8 (wave 2) crypto/*
// multi-result API (m8MultiResultFuncs table above). Same contract as
// classifyOsMultiResultCall: helperPkg overrides the package the __-helper
// and pair struct are looked up in.
func classifyM8MultiResultCall(pkgPath string, fn *types.Func) *mrPlan {
	if fn == nil || fn.Pkg() == nil || fn.Pkg().Path() != pkgPath {
		return nil
	}
	sig, ok := fn.Type().(*types.Signature)
	if !ok {
		return nil
	}
	recvName := ""
	if recv := sig.Recv(); recv != nil {
		rt := recv.Type()
		if pt, ok := rt.(*types.Pointer); ok {
			rt = pt.Elem()
		}
		if nt, ok := rt.(*types.Named); ok && nt.Obj() != nil {
			recvName = nt.Obj().Name()
		}
	}
	for _, mf := range m8MultiResultFuncs {
		if mf.pkgPath != pkgPath || mf.name != fn.Name() || mf.recvType != recvName {
			continue
		}
		if sig.Results().Len() != len(mf.pairFields) {
			return nil
		}
		helperPkg := mf.helperPkg
		if helperPkg == "" {
			helperPkg = mf.pkgPath
		}
		sp := getSyntheticPackage(helperPkg)
		obj := sp.pkg.Scope().Lookup(mf.pairType)
		if obj == nil {
			return nil
		}
		nt, _ := obj.Type().(*types.Named)
		if nt == nil {
			return nil
		}
		return &mrPlan{
			kind: mrM7, helperPkg: helperPkg,
			helperName: mf.helper,
			tmpType:    nt, pairFields: mf.pairFields,
			prependRecv: mf.recvType != "",
		}
	}
	return nil
}

// classifyIoMultiResultCall recognizes the P3-M8 (WS D) synthetic io
// multi-result API (the ioMultiResultFuncs table above): WriteString,
// ReadAtLeast, ReadFull, Copy, CopyN, CopyBuffer, ReadAll, plus the
// LimitedReader.Read and discard.Write/WriteString methods (prependRecv
// applies — all have receivers). Same contract as
// classifyM7MultiResultCall.
func classifyIoMultiResultCall(pkgPath string, fn *types.Func) *mrPlan {
	if fn == nil || fn.Pkg() == nil || fn.Pkg().Path() != pkgPath {
		return nil
	}
	sig, ok := fn.Type().(*types.Signature)
	if !ok {
		return nil
	}
	recvName := ""
	if recv := sig.Recv(); recv != nil {
		rt := recv.Type()
		if pt, ok := rt.(*types.Pointer); ok {
			rt = pt.Elem()
		}
		if nt, ok := rt.(*types.Named); ok && nt.Obj() != nil {
			recvName = nt.Obj().Name()
		}
	}
	for _, mf := range ioMultiResultFuncs {
		if mf.pkgPath != pkgPath || mf.name != fn.Name() || mf.recvType != recvName {
			continue
		}
		// The result count must match the pair arity: a signature change
		// in the synthetic source fails the desugar loudly instead of
		// extracting the wrong fields.
		if sig.Results().Len() != len(mf.pairFields) {
			return nil
		}
		sp := getSyntheticPackage(mf.pkgPath)
		obj := sp.pkg.Scope().Lookup(mf.pairType)
		if obj == nil {
			return nil
		}
		nt, _ := obj.Type().(*types.Named)
		if nt == nil {
			return nil
		}
		return &mrPlan{
			kind: mrM7, helperPkg: mf.pkgPath,
			helperName: mf.helper,
			tmpType:    nt, pairFields: mf.pairFields,
			prependRecv: mf.recvType != "",
		}
	}
	return nil
}

// classifyIfaceMultiResultCall recognizes P3-M8 (WS D) multi-result calls
// on interface values (r.Read(buf) with r of interface type). Matching is
// structural on (method name, result count, positional result types) so
// both synthetic interfaces (io.Reader) and user-declared interfaces with
// the same method shape dispatch; the pair type comes from the
// m8IfaceMultiResultMethods table. Returns nil when the method is not a
// dispatched multi-result interface method.
func (e *exporter) classifyIfaceMultiResultCall(selection *types.Selection) *mrPlan {
	if selection.Kind() != types.MethodVal {
		return nil
	}
	f, ok := selection.Obj().(*types.Func)
	if !ok {
		return nil
	}
	sig, ok := f.Type().(*types.Signature)
	if !ok || sig.Results().Len() < 2 {
		return nil
	}
	for _, m := range m8IfaceMultiResultMethods {
		if m.method != f.Name() || len(m.pairFields) != sig.Results().Len() {
			continue
		}
		sp := getSyntheticPackage(m.pkgPath)
		if sp == nil {
			continue
		}
		obj := sp.pkg.Scope().Lookup(m.pairType)
		if obj == nil {
			continue
		}
		nt, _ := obj.Type().(*types.Named)
		if nt == nil {
			continue
		}
		st, ok := nt.Underlying().(*types.Struct)
		if !ok || st.NumFields() != sig.Results().Len() {
			continue
		}
		// The pair's field names and types must match the method's
		// results positionally — otherwise field extraction would read
		// the wrong values. A synthetic-source change that breaks this
		// skips the entry (loud: the call site keeps the fail-fast).
		match := true
		for i := 0; i < st.NumFields(); i++ {
			if st.Field(i).Name() != m.pairFields[i] ||
				!types.Identical(st.Field(i).Type(), sig.Results().At(i).Type()) {
				match = false
				break
			}
		}
		if !match {
			continue
		}
		return &mrPlan{
			kind: mrIfaceMethod, helperPkg: m.pkgPath,
			tmpType: nt, pairFields: m.pairFields,
		}
	}
	return nil
}

// classifyIfaceFallbackMultiResultCall is the WS H fallback variant of
// classifyIfaceMultiResultCall (P3-M9, graalgo-wsh-wsd-interaction). It
// matches a multi-result method called on a type parameter (dispatched
// via the constraint interface) against the m8IfaceMultiResultMethods
// table.
//
// Unlike classifyIfaceMultiResultCall, it is lenient on field types: when
// the method's result is a type parameter (e.g., P in Point[P].SetBytes),
// strict types.Identical against the pair struct's field fails (different
// TypeParam objects). The runtime itab dispatch guarantees the concrete
// helper's pair is returned, and field extraction is by name — so only
// the field count and names must match. Non-type-param results still get
// the strict check.
func (e *exporter) classifyIfaceFallbackMultiResultCall(selection *types.Selection) *mrPlan {
	if selection.Kind() != types.MethodVal {
		return nil
	}
	f, ok := selection.Obj().(*types.Func)
	if !ok {
		return nil
	}
	sig, ok := f.Type().(*types.Signature)
	if !ok || sig.Results().Len() < 2 {
		return nil
	}
	for _, m := range m8IfaceMultiResultMethods {
		if m.method != f.Name() || len(m.pairFields) != sig.Results().Len() {
			continue
		}
		sp := getSyntheticPackage(m.pkgPath)
		if sp == nil {
			continue
		}
		obj := sp.pkg.Scope().Lookup(m.pairType)
		if obj == nil {
			continue
		}
		nt, _ := obj.Type().(*types.Named)
		if nt == nil {
			continue
		}
		st, ok := nt.Underlying().(*types.Struct)
		if !ok || st.NumFields() != sig.Results().Len() {
			continue
		}
		match := true
		for i := 0; i < st.NumFields(); i++ {
			if st.Field(i).Name() != m.pairFields[i] {
				match = false
				break
			}
			// Lenient for type parameters: the constraint's method
			// returns P, the pair struct field may be P (different
			// object) or a concrete instantiation. Accept if either
			// side is a type parameter; otherwise require Identical.
			if _, isTP := sig.Results().At(i).Type().(*types.TypeParam); isTP {
				continue
			}
			if _, isTP := st.Field(i).Type().(*types.TypeParam); isTP {
				continue
			}
			if !types.Identical(st.Field(i).Type(), sig.Results().At(i).Type()) {
				match = false
				break
			}
		}
		if !match {
			continue
		}
		return &mrPlan{
			kind: mrIfaceMethod, helperPkg: m.pkgPath,
			tmpType: nt, pairFields: m.pairFields,
		}
	}
	return nil
}

// multiResultHelperFor maps a multi-result (package path, method name,
// receiver type name) to its pair-returning __-helper's package and name,
// unioned over the m7/os/m8/io desugar tables. It is the exporter's view
// of "the concrete implementation behind this multi-result method", used
// by methodTableEntries to wire interface dispatch at the helper.
func multiResultHelperFor(pkgPath, name, recvType string) (helperPkg, helperName string, ok bool) {
	for _, mf := range m7MultiResultFuncs {
		if mf.pkgPath == pkgPath && mf.name == name && mf.recvType == recvType {
			return mf.pkgPath, mf.helper, true
		}
	}
	for _, mf := range ioMultiResultFuncs {
		if mf.pkgPath == pkgPath && mf.name == name && mf.recvType == recvType {
			return mf.pkgPath, mf.helper, true
		}
	}
	for _, mf := range osMultiResultFuncs {
		if mf.pkgPath == pkgPath && mf.name == name && mf.recvType == recvType {
			hp := mf.helperPkg
			if hp == "" {
				hp = mf.pkgPath
			}
			return hp, mf.helper, true
		}
	}
	for _, mf := range m8MultiResultFuncs {
		if mf.pkgPath == pkgPath && mf.name == name && mf.recvType == recvType {
			hp := mf.helperPkg
			if hp == "" {
				hp = mf.pkgPath
			}
			return hp, mf.helper, true
		}
	}
	return "", "", false
}

// multiResultHelperFunc maps a multi-result synthetic method to its
// pair-returning __-helper's funcDeclInfo (P3-M8, WS D) — the itab target
// for interface dispatch of that method. It returns ok=false when the
// method is not a table-known multi-result method. The helper's pair
// struct field types must be positionally identical to the method's
// result types, or the dispatch would extract wrong fields: a mismatch
// fails the export loudly (ok=false → the method stays out of the table
// → dispatch fails fast with "missing method").
func (e *exporter) multiResultHelperFunc(mobj *types.Func) (*funcDeclInfo, bool) {
	if mobj == nil || mobj.Pkg() == nil {
		return nil, false
	}
	sig, ok := mobj.Type().(*types.Signature)
	if !ok || sig.Results().Len() < 2 {
		return nil, false
	}
	recv := sig.Recv()
	if recv == nil {
		return nil, false
	}
	rt := recv.Type()
	if pt, ok := rt.(*types.Pointer); ok {
		rt = pt.Elem()
	}
	nt, ok := rt.(*types.Named)
	if !ok || nt.Obj() == nil {
		return nil, false
	}
	helperPkg, helperName, ok := multiResultHelperFor(mobj.Pkg().Path(), mobj.Name(), nt.Obj().Name())
	if !ok {
		return nil, false
	}
	sp := getSyntheticPackage(helperPkg)
	if sp == nil {
		return nil, false
	}
	helperObj := sp.pkg.Scope().Lookup(helperName)
	helperFn, ok := helperObj.(*types.Func)
	if !ok {
		return nil, false
	}
	fi, ok := e.funcByObj[helperFn]
	if !ok || fi == nil {
		return nil, false
	}
	// Positional shape check: the helper's single result is the pair
	// struct; its field types must equal the method's result types.
	hsig, ok := helperFn.Type().(*types.Signature)
	if !ok || hsig.Results().Len() != 1 {
		return nil, false
	}
	prt := hsig.Results().At(0).Type()
	if pt, ok := prt.(*types.Pointer); ok {
		prt = pt.Elem()
	}
	pnt, ok := prt.(*types.Named)
	if !ok {
		return nil, false
	}
	pst, ok := pnt.Underlying().(*types.Struct)
	if !ok || pst.NumFields() != sig.Results().Len() {
		return nil, false
	}
	for i := 0; i < pst.NumFields(); i++ {
		if !types.Identical(pst.Field(i).Type(), sig.Results().At(i).Type()) {
			return nil, false
		}
	}
	return fi, true
}

// classifyM7MultiResultCallRtype retries a reflect interface method
// against the rtype entries (P3-M8 wave 2).
func classifyM7MultiResultCallRtype(pkgPath string, fn *types.Func) *mrPlan {
	sig, ok := fn.Type().(*types.Signature)
	if !ok {
		return nil
	}
	for _, mf := range m7MultiResultFuncs {
		if mf.pkgPath != pkgPath || mf.name != fn.Name() || mf.recvType != "rtype" {
			continue
		}
		if sig.Results().Len() != len(mf.pairFields) {
			return nil
		}
		sp := getSyntheticPackage(mf.pkgPath)
		obj := sp.pkg.Scope().Lookup(mf.pairType)
		if obj == nil {
			return nil
		}
		nt, _ := obj.Type().(*types.Named)
		if nt == nil {
			return nil
		}
		return &mrPlan{
			kind: mrM7, helperPkg: mf.pkgPath,
			helperName: mf.helper,
			tmpType:    nt, pairFields: mf.pairFields,
			prependRecv: true,
		}
	}
	return nil
}

// isDeadlineMethod reports whether f is the synthetic context package's
// Deadline method (2 results). The mrDeadline desugar path is retained
// defense-in-depth for WS D (see the package header); in M5 it is
// unreachable from user code, so the concrete types' fail-fast Deadline
// bodies never execute via this path either.
func isDeadlineMethod(f *types.Func) bool {
	if f == nil || f.Pkg() == nil || f.Pkg().Path() != "context" || f.Name() != "Deadline" {
		return false
	}
	sig, ok := f.Type().(*types.Signature)
	return ok && sig.Results().Len() == 2
}

// exportMultiResultDesugar implements the Appendix M desugar for
// N-target assignments. It returns handled=false when s is not an
// N-target assignment of one of the Appendix M operations.
func (e *exporter) exportMultiResultDesugar(s *ast.AssignStmt, define bool) (*Node, bool, error) {
	if len(s.Rhs) != 1 {
		return nil, false, nil
	}
	c, ok := unparen(s.Rhs[0]).(*ast.CallExpr)
	if !ok {
		return nil, false, nil
	}
	plan := e.classifyMultiResultCall(c)
	if plan == nil {
		return nil, false, nil
	}
	// The target count must match the plan's extraction count: 2 for the
	// M5 kinds and the 2-result M7 ops; 3 for the 3-result M7 op
	// (ascii85.Decode). Any other arity is not a desugar position.
	want := len(plan.pairFields)
	if plan.kind == mrWithCancel || plan.kind == mrWithDeadline {
		want = 2
	}
	if len(s.Lhs) != want {
		return nil, false, nil
	}
	n, err := e.desugarMultiResult(s, c, plan, define)
	return n, true, err
}

// desugarMultiResult emits the desugared block for a classified call.
func (e *exporter) desugarMultiResult(s *ast.AssignStmt, c *ast.CallExpr, plan *mrPlan, define bool) (*Node, error) {
	// P3-M9 (WS D): fips140cache.Cache.Get(k, new, check) inlines the
	// new() call directly: v, err := cache.Get(k, newFn, checkFn)
	// becomes v, err := newFn(). The synthetic cache is a
	// no-memoization stub, so this is semantically equivalent.
	if plan.kind == mrCacheGet {
		if len(c.Args) < 2 {
			return nil, fmt.Errorf("gofrontend: Cache.Get requires at least 2 args (k, new)")
		}
		// Build newFn() call: Fun is Args[1], no args.
		newCall := &ast.CallExpr{
			Fun:    c.Args[1],
			Lparen: c.Lparen,
			Args:   []ast.Expr{},
			Rparen: c.Rparen,
		}
		// Build the assignment: v, err := newFn()
		// Use the standard assignment export with the call as RHS.
		newAssign := &ast.AssignStmt{
			Lhs:    s.Lhs,
			TokPos: s.TokPos,
			Tok:    s.Tok,
			Rhs:    []ast.Expr{newCall},
		}
		// Export as a normal multi-value assignment (bypasses desugar
		// recursion since newCall is not a classified multi-result).
		if newAssign.Tok == token.DEFINE {
			return e.exportDefine(newAssign)
		}
		return e.exportAssign(newAssign)
	}
	// P3-M8 (WS D): an interface multi-result call (r.Read(buf)) desugars
	// to the ifacemethod dispatch node itself — the itab maps the method
	// to the concrete type's pair-returning __-helper, so the node yields
	// the pair. The temp declaration and field extraction below are
	// shared with the helper-call path.
	if plan.kind == mrIfaceMethod {
		return e.desugarIfaceMultiResult(s, c, plan, define)
	}
	// P3-M9 (WS D): user-defined multi-result functions are called
	// directly (they were transformed to pair-returning at definition).
	// Export the call normally, then extract fields from the pair.
	if plan.kind == mrUser {
		call, err := e.exportCall(c)
		if err != nil {
			return nil, err
		}
		resTid, err := e.typeID(plan.tmpType)
		if err != nil {
			return nil, err
		}
		e.chanSeq++
		tmpName := fmt.Sprintf("__p5mr_%d", e.chanSeq)
		v := types.NewVar(token.NoPos, nil, tmpName, plan.tmpType)
		tmpID, err := e.defVar(v, false)
		if err != nil {
			return nil, err
		}
		return e.desugarExtract(s, c, plan, define, tmpName, tmpID, resTid, call, nil)
	}
	// P3-M9 (WS D): function field call (e.g., c.generate(r)). The
	// function value is a field access; call it and pack the 2 results
	// into the pair struct. Generate a funcfieldcall node.
	if plan.kind == mrFuncField {
		return e.desugarFuncFieldCall(s, c, plan, define)
	}
	helperFn, err := e.multiResultHelper(plan)
	if err != nil {
		return nil, err
	}
	args, preStmts, err := e.multiResultHelperArgs(c, plan)
	if err != nil {
		return nil, err
	}
	helperCall, tmpName, tmpID, tmpTid, err := e.synthHelperCall(c, helperFn, plan.tmpType, args)
	if err != nil {
		return nil, err
	}
	return e.desugarExtract(s, c, plan, define, tmpName, tmpID, tmpTid, helperCall, preStmts)
}

// desugarFuncFieldCall desugars a function field call (e.g.,
// c.generate(r)) where the field is of function type with 2 results.
// Generates a funcfieldcall node that calls the field's function value
// and packs the 2 results into the pair struct.
func (e *exporter) desugarFuncFieldCall(s *ast.AssignStmt, c *ast.CallExpr, plan *mrPlan, define bool) (*Node, error) {
	sel, ok := c.Fun.(*ast.SelectorExpr)
	if !ok {
		return nil, fmt.Errorf("gofrontend: funcfieldcall expects SelectorExpr, got %T", c.Fun)
	}
	// Export the receiver (e.g., c).
	recvNode, err := e.exportExpr(sel.X)
	if err != nil {
		return nil, err
	}
	// Export the call args.
	args, err := e.exportCallArgs(c)
	if err != nil {
		return nil, err
	}
	// Get the pair type ID.
	pairTid, err := e.typeID(plan.tmpType)
	if err != nil {
		return nil, err
	}
	// Create the funcfieldcall node.
	// Fields: recv (receiver node), field (field name), args (arg nodes),
	// pairType (pair struct type ID), pairFields (field names).
	callNode := e.node("funcfieldcall", c, map[string]any{
		"recv":       recvNode,
		"field":      sel.Sel.Name,
		"args":       args,
		"pairType":   pairTid,
		"pairFields": plan.pairFields,
	})
	// Create temp var for the pair.
	e.chanSeq++
	tmpName := fmt.Sprintf("__p5mr_%d", e.chanSeq)
	v := types.NewVar(token.NoPos, nil, tmpName, plan.tmpType)
	tmpID, err := e.defVar(v, false)
	if err != nil {
		return nil, err
	}
	return e.desugarExtract(s, c, plan, define, tmpName, tmpID, pairTid, callNode, nil)
}

// multiResultHelper resolves a desugar plan's __-helper to its
// *types.Func in the synthetic package scope.
func (e *exporter) multiResultHelper(plan *mrPlan) (*types.Func, error) {
	sp := getSyntheticPackage(plan.helperPkg)
	// P3-M8b: import the synthetic package so its helpers are registered
	// in funcByObj (idempotent). Without this, synthHelperCallNode fails
	// with "synthetic helper not in funcByObj".
	if err := e.importSyntheticPackage(sp); err != nil {
		return nil, err
	}
	helperObj := sp.pkg.Scope().Lookup(plan.helperName)
	helperFn, ok := helperObj.(*types.Func)
	if !ok {
		return nil, fmt.Errorf("gofrontend: synthetic helper %s.%s missing", plan.helperPkg, plan.helperName)
	}
	return helperFn, nil
}

// multiResultHelperArgs exports a desugar plan's helper-call argument
// expressions: the original call's args, except for the Deadline method
// where the receiver becomes the argument; the receiver is prepended
// when the plan sets prependRecv.
func (e *exporter) multiResultHelperArgs(c *ast.CallExpr, plan *mrPlan) ([]*Node, []*Node, error) {
	// Helper call args: the original call's args, except for the
	// Deadline method where the receiver becomes the argument.
	var argExprs []ast.Expr
	if plan.kind == mrDeadline {
		sel, ok := c.Fun.(*ast.SelectorExpr)
		if !ok {
			return nil, nil, fmt.Errorf("gofrontend: Deadline call has non-selector fun %T", c.Fun)
		}
		argExprs = []ast.Expr{sel.X}
	} else {
		argExprs = c.Args
	}
	if plan.prependRecv {
		sel, ok := c.Fun.(*ast.SelectorExpr)
		if !ok {
			return nil, nil, fmt.Errorf("gofrontend: prependRecv call has non-selector fun %T", c.Fun)
		}
		argExprs = append([]ast.Expr{sel.X}, argExprs...)
	}
	args := make([]*Node, 0, len(argExprs))
	var preStmts []*Node
	for _, a := range argExprs {
		// P3-M9 (WS D): a multi-valued call in argument position, e.g.
		// curve.affineFromJacobian(curve.addJacobian(...)). Per Go spec,
		// f(g()) spreads g's results as f's arguments. Desugar the inner
		// call to its __-helper, emit the temp vardecl as a pre-statement,
		// and spread the pair fields as separate arguments.
		if inner, ok := unparen(a).(*ast.CallExpr); ok {
			if innerPlan := e.classifyMultiResultCall(inner); innerPlan != nil {
				// Per Go spec, a multi-valued call as an argument must be
				// the sole argument (besides the prepended receiver).
				// The type checker guarantees this for valid Go.
				innerArgs, innerPre, err := e.multiResultHelperArgs(inner, innerPlan)
				if err != nil {
					return nil, nil, err
				}
				preStmts = append(preStmts, innerPre...)
				var innerCallNode *Node
				var innerTmpName string
				var innerTmpID, innerTmpTid int
				if innerPlan.kind == mrUser {
					innerCallNode, err = e.exportCall(inner)
					if err != nil {
						return nil, nil, err
					}
					resTid, err := e.typeID(innerPlan.tmpType)
					if err != nil {
						return nil, nil, err
					}
					e.chanSeq++
					innerTmpName = fmt.Sprintf("__p5mr_%d", e.chanSeq)
					v := types.NewVar(token.NoPos, nil, innerTmpName, innerPlan.tmpType)
					innerTmpID, err = e.defVar(v, false)
					if err != nil {
						return nil, nil, err
					}
					innerTmpTid = resTid
				} else if innerPlan.kind == mrIfaceMethod {
					// Interface method: use the ifacemethod dispatch.
					// For now, fail with a clear error — nested interface
					// multi-result calls are not yet supported.
					return nil, nil, scopeErrorf("nested interface multi-result call in argument position is not supported in frontend/v1 scope (WS D)")
				} else {
					helperFn, err := e.multiResultHelper(innerPlan)
					if err != nil {
						return nil, nil, err
					}
					innerCallNode, innerTmpName, innerTmpID, innerTmpTid, err = e.synthHelperCall(inner, helperFn, innerPlan.tmpType, innerArgs)
					if err != nil {
						return nil, nil, err
					}
				}
				// Emit the inner temp vardecl as a pre-statement.
				preStmts = append(preStmts, e.node("vardecl", inner, map[string]any{
					"names": []string{innerTmpName}, "objs": []int{innerTmpID},
					"define": true, "type": nil, "values": []*Node{innerCallNode},
				}))
				// Spread the pair fields as separate arguments.
				for i, field := range innerPlan.pairFields {
					sel, err := e.synthPairField(inner, innerTmpName, innerTmpID, innerTmpTid, innerPlan.tmpType, field, i)
					if err != nil {
						return nil, nil, err
					}
					args = append(args, sel)
				}
				continue
			}
		}
		x, err := e.exportExpr(a)
		if err != nil {
			return nil, nil, err
		}
		args = append(args, x)
	}
	return args, preStmts, nil
}

// exportBareMultiResultCall desugars a concrete multi-result call in
// bare-statement position (w.Write(data) — legal Go, results discarded):
// the __-helper call runs for its effects and the pair is dropped,
// exactly gc's statement-position semantics. Only the concrete
// helper-call plans (mrM7, mrXmlToken) reach here — the M5 kinds keep
// their pinned statement-position fail-fast (TestMultiResultFailFastKept,
// GG-0017), mrDeadline is unreachable defense-in-depth, and mrIfaceMethod
// is handled by the D1 path (exportIfaceMethodCall) before this
// interception.
func (e *exporter) exportBareMultiResultCall(s *ast.ExprStmt, c *ast.CallExpr, plan *mrPlan) (*Node, error) {
	helperFn, err := e.multiResultHelper(plan)
	if err != nil {
		return nil, err
	}
	args, preStmts, err := e.multiResultHelperArgs(c, plan)
	if err != nil {
		return nil, err
	}
	call, err := e.synthHelperCallNode(c, helperFn, plan.tmpType, args)
	if err != nil {
		return nil, err
	}
	exprStmt := e.node("exprstmt", s, map[string]any{"x": call})
	if len(preStmts) > 0 {
		// Nested desugars need their temps declared before the call.
		stmts := append(append([]*Node{}, preStmts...), exprStmt)
		return e.node("block", s, map[string]any{"stmts": stmts}), nil
	}
	return exprStmt, nil
}

// desugarIfaceMultiResult emits the desugared block for an mrIfaceMethod
// plan (P3-M8, WS D): the "helper call" is the ifacemethod dispatch node
// built by exportIfaceMethodCall. The temp declaration mirrors
// synthHelperCall's tail; field extraction and target assignment are
// shared via desugarExtract.
func (e *exporter) desugarIfaceMultiResult(s *ast.AssignStmt, c *ast.CallExpr, plan *mrPlan, define bool) (*Node, error) {
	sel, ok := c.Fun.(*ast.SelectorExpr)
	if !ok {
		return nil, fmt.Errorf("gofrontend: iface-multi-result call has non-selector fun %T", c.Fun)
	}
	selection, ok := e.info.Selections[sel]
	if !ok {
		return nil, fmt.Errorf("gofrontend: no selection for iface-multi-result call")
	}
	callNode, err := e.exportIfaceMethodCall(c, sel, selection)
	if err != nil {
		return nil, err
	}
	resTid, err := e.typeID(plan.tmpType)
	if err != nil {
		return nil, err
	}
	e.chanSeq++
	tmpName := fmt.Sprintf("__p5mr_%d", e.chanSeq)
	v := types.NewVar(token.NoPos, nil, tmpName, plan.tmpType)
	tmpID, err := e.defVar(v, false)
	if err != nil {
		return nil, err
	}
	return e.desugarExtract(s, c, plan, define, tmpName, tmpID, resTid, callNode, nil)
}

// desugarExtract emits the shared desugar tail: the __p5mr_N temp holding
// callNode's pair, the positional field extractions, and the assignment
// of the extractions to the N targets. preStmts (from nested multi-valued
// calls in argument position) are emitted first.
func (e *exporter) desugarExtract(s *ast.AssignStmt, c *ast.CallExpr, plan *mrPlan, define bool,
	tmpName string, tmpID, tmpTid int, callNode *Node, preStmts []*Node) (*Node, error) {
	// Pre-statements from nested desugars (inner temps) come first.
	stmts := append([]*Node{}, preStmts...)
	// __p5mr_N := <helper>(args)
	stmts = append(stmts, e.node("vardecl", s, map[string]any{
		"names": []string{tmpName}, "objs": []int{tmpID},
		"define": true, "type": nil, "values": []*Node{callNode},
	}))
	// The two extractions.
	var extracts []*Node
	switch plan.kind {
	case mrWithCancel, mrWithDeadline:
		// P3-M5: use the concrete *cancelCtx/*timerCtx directly (no
		// Context() interface conversion). The interface conversion would
		// require the multi-result Deadline method in the P3-M3 method
		// table (v1 cannot emit it); the concrete type supports all
		// method calls directly.
		conv := e.node("ident", c, map[string]any{"name": tmpName, "obj": tmpID, "type": tmpTid})
		mcall, err := e.synthMethodCall(c, tmpName, tmpID, tmpTid, plan.methodRecv, plan.methodName)
		if err != nil {
			return nil, err
		}
		extracts = []*Node{conv, mcall}
	case mrDeadline, mrParseDur, mrXmlToken, mrM7, mrIfaceMethod, mrUser, mrFuncField:
		for i, field := range plan.pairFields {
			sel, err := e.synthPairField(c, tmpName, tmpID, tmpTid, plan.tmpType, field, i)
			if err != nil {
				return nil, err
			}
			extracts = append(extracts, sel)
		}
	default:
		return nil, fmt.Errorf("gofrontend: unknown desugar kind %d", plan.kind)
	}
	// Assign the extractions to the two targets (blank skips).
	for i, lhs := range s.Lhs {
		id, ok := lhs.(*ast.Ident)
		if !ok {
			// P3-M9: allow selector LHS (e.g., w.prevHeader, _ = w.headerSize())
			// and index LHS (e.g., a[i], err = strconv.Atoi(s)) in the
			// assignment case via exportAssignable. Define (:=) with a
			// non-identifier lhs is invalid Go, so it still fails below.
			switch lhs.(type) {
			case *ast.SelectorExpr, *ast.IndexExpr:
				// OK: handled by exportAssignable in the assignment branch.
			default:
				return nil, scopeErrorf("multi-result desugar with non-identifier lhs %T is not supported in frontend/v1 scope (WS D)", lhs)
			}
			// Blank selector/index doesn't make sense; treat as error.
		}
		if ok && id.Name == "_" {
			continue
		}
		if define {
			if !ok {
				return nil, scopeErrorf("multi-result desugar := with selector lhs is not valid Go (WS D)")
			}
			v, ok := e.info.ObjectOf(id).(*types.Var)
			if !ok {
				return nil, fmt.Errorf("gofrontend: := name %q does not denote a variable", id.Name)
			}
			oid, err := e.defVar(v, false)
			if err != nil {
				return nil, err
			}
			stmts = append(stmts, e.node("vardecl", s, map[string]any{
				"names": []string{id.Name}, "objs": []int{oid},
				"define": true, "type": nil, "values": []*Node{extracts[i]},
			}))
		} else {
			lhsNode, err := e.exportAssignable(lhs)
			if err != nil {
				return nil, err
			}
			stmts = append(stmts, e.node("assign", s, map[string]any{
				"tok": "=", "lhs": []*Node{lhsNode}, "rhs": []*Node{extracts[i]},
			}))
		}
	}
	return e.node("block", s, map[string]any{"stmts": stmts}), nil
}

// synthHelperCallNode builds a "call" node to a synthetic package-level
// function (mirroring exportFuncCall's non-generic path).
func (e *exporter) synthHelperCallNode(c *ast.CallExpr, fn *types.Func, resultType types.Type, args []*Node) (*Node, error) {
	fi, ok := e.funcByObj[fn]
	if !ok {
		return nil, fmt.Errorf("gofrontend: synthetic helper %s not in funcByObj", fn.Name())
	}
	sigID, err := e.typeID(fn.Type())
	if err != nil {
		return nil, err
	}
	fun := e.node("ident", c, map[string]any{
		"name": fn.Name(), "obj": fi.funcID, "type": sigID,
	})
	resTid, err := e.typeID(resultType)
	if err != nil {
		return nil, err
	}
	return e.node("call", c, map[string]any{
		"fun": fun, "args": args, "type": resTid,
	}), nil
}

// synthHelperCall builds a "call" node to a synthetic package-level
// function (mirroring exportFuncCall's non-generic path) and declares
// the __p5mr_N temp holding its result, returning the call node plus
// the temp's name/obj/type ids.
func (e *exporter) synthHelperCall(c *ast.CallExpr, fn *types.Func, resultType types.Type, args []*Node) (*Node, string, int, int, error) {
	call, err := e.synthHelperCallNode(c, fn, resultType, args)
	if err != nil {
		return nil, "", 0, 0, err
	}
	resTid, err := e.typeID(resultType)
	if err != nil {
		return nil, "", 0, 0, err
	}
	e.chanSeq++
	tmpName := fmt.Sprintf("__p5mr_%d", e.chanSeq)
	v := types.NewVar(token.NoPos, nil, tmpName, resultType)
	tmpID, err := e.defVar(v, false)
	if err != nil {
		return nil, "", 0, 0, err
	}
	return call, tmpName, tmpID, resTid, nil
}

// synthMethodCall builds the __p5mr_N.method() call node (mirroring
// exportMethodCall's methodref construction).
func (e *exporter) synthMethodCall(c *ast.CallExpr, tmpName string, tmpID, tmpTid int, recv *types.Named, method string) (*Node, error) {
	fi, ok := e.methodIndex[methodKey{recv.Obj(), method}]
	if !ok || fi == nil {
		return nil, fmt.Errorf("gofrontend: synthetic method %s.%s not in methodIndex", recv.Obj().Name(), method)
	}
	sigID, err := e.freshSigID(fi.recv, fi.sig.Params(), fi.sig.Results(), fi.sig.Variadic())
	if err != nil {
		return nil, err
	}
	recvNode := e.node("ident", c, map[string]any{"name": tmpName, "obj": tmpID, "type": tmpTid})
	fun := e.node("methodref", c, map[string]any{"obj": fi.funcID, "recv": recvNode, "type": sigID})
	resTid, err := e.typeID(fi.sig.Results().At(0).Type())
	if err != nil {
		return nil, err
	}
	return e.node("call", c, map[string]any{
		"fun": fun, "args": []*Node{}, "type": resTid,
	}), nil
}

// synthPairField builds the __p5mr_N.<field> selector node for a pair
// struct (field index i).
func (e *exporter) synthPairField(c *ast.CallExpr, tmpName string, tmpID, tmpTid int, pairType types.Type, field string, i int) (*Node, error) {
	nt, ok := pairType.(*types.Named)
	if !ok {
		return nil, fmt.Errorf("gofrontend: pair type %v is not named", pairType)
	}
	st, ok := nt.Underlying().(*types.Struct)
	if !ok || i < 0 || i >= st.NumFields() {
		return nil, fmt.Errorf("gofrontend: pair type %v has no field %d", pairType, i)
	}
	if st.Field(i).Name() != field {
		return nil, fmt.Errorf("gofrontend: pair field %d is %q, want %q", i, st.Field(i).Name(), field)
	}
	ftid, err := e.typeID(st.Field(i).Type())
	if err != nil {
		return nil, err
	}
	x := e.node("ident", c, map[string]any{"name": tmpName, "obj": tmpID, "type": tmpTid})
	return e.node("selector", c, map[string]any{
		"x": x, "field": field, "selpath": []any{i}, "type": ftid,
	}), nil
}
