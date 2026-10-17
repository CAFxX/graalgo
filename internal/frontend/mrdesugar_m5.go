package main

import (
	"fmt"
	"go/ast"
	"go/token"
	"go/types"
	"strings"
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
	// mrTrivialFwd: a synthetic stub method whose body is a trivial
	// forwarder — a single `return p()` where p is a function-typed
	// parameter. The desugar inlines the p() call directly at the call
	// site (e.g. a no-memoization cache stub whose Get just returns
	// new()). Classified structurally by classifyTrivialForwarder —
	// never by package/type/method name.
	mrTrivialFwd
	// mrRuntimeStack: the synthetic runtime package's multi-result
	// stack-inspection members (runtime.Caller, (*Frames).Next,
	// (*Func).FileLine). Unlike every other kind there is no __-helper:
	// the synthetic runtime overlay has no real-Go source, so the
	// desugar builds the intrinsic wire node directly
	// (desugarRuntimeStack) and the pair-typed temp + field extraction
	// proceed exactly like mrM7. rtStackOp selects the wire node.
	mrRuntimeStack
	// mrSyncMap: the synthetic sync package's multi-result Map methods
	// (Load, LoadOrStore, LoadAndDelete, Swap). Like mrRuntimeStack
	// there is no __-helper: the types-built overlay has no real-Go
	// source, so the desugar builds the "syncop" wire node directly
	// (desugarSyncMap) with the mapAnyBoolPair type, and field
	// extraction proceeds like mrM7. syncMapOp carries the method name
	// for wire-node construction and arity checks.
	mrSyncMap
)

// rtStackOpKind selects the intrinsic wire node for an mrRuntimeStack plan.
type rtStackOpKind int

const (
	rtStackNone         rtStackOpKind = iota
	rtStackCaller                     // runtime.Caller(skip) -> "runtimecaller"
	rtStackFramesNext                 // (*Frames).Next() -> "runtimeframesnext"
	rtStackFuncFileLine               // (*Func).FileLine(pc) -> "runtimefuncfileline"
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
	// ifaceFallback: the method may be selected through an interface
	// (no receiver recorded); classifyM7MultiResultCall retries such
	// receiver-less selections against entries with this flag set
	// (P2-F3, review-n1 — table-driven instead of package-name-keyed).
	ifaceFallback bool
}

// hasStaticMREntry reports whether (pkgPath, name, recvType) has an entry
// in the static Appendix-M desugar tables. The mechanical transform
// (transformMultiResult) skips such functions: they are served by
// hand-written __-helpers whose names need not match the transform's
// __F convention (e.g. io.Pipe → __pipe), and generating a second
// helper would conflict.
// m5StaticMREntry is one M5-era multi-result function with a hand-written
// __-helper (or wire-node desugar) that the mechanical multi-result
// transform must skip.
type m5StaticMREntry struct {
	pkgPath  string // synthetic package path
	name     string // function/method name
	recvType string // receiver named type ("" = package-level function)
	anyRecv  bool   // match any non-empty receiver — for retained WS D
	// method entries like context.Deadline
}

// m5StaticMREntries is the table-driven replacement for the former
// package-path switch in hasStaticMREntry (P2-F2, review-n1): the M5-era
// functions (context.WithCancel etc., runtime.Caller, time.ParseDuration,
// context.Deadline) have hand-written __-helpers and fail-fast bodies; the
// mechanical transform must not rewrite them.
// NOTE: these live in their own table, NOT in m7MultiResultFuncs etc.:
// those tables drive classify*, which runs BEFORE the M5 classifier, so
// adding M5 functions there would misclassify their call sites (mrM7
// instead of the M5 kinds mrWithCancel/mrParseDur/...).
var m5StaticMREntries = []m5StaticMREntry{
	{"context", "WithCancel", "", false},
	{"context", "WithCancelCause", "", false},
	{"context", "WithDeadline", "", false},
	{"context", "WithTimeout", "", false},
	{"runtime", "Caller", "", false},
	{"time", "ParseDuration", "", false},
	// context.Deadline method (mrDeadline) — retained for WS D; the
	// synthetic Context interface omits Deadline so no call site
	// type-checks, but the transform must still skip it.
	{"context", "Deadline", "", true},
}

func hasStaticMREntry(pkgPath, name, recvType string) bool {
	// M5 functions (context.WithCancel etc., runtime.Caller,
	// time.ParseDuration) have hand-written __-helpers and fail-fast
	// bodies; the mechanical transform must not rewrite them.
	for _, me := range m5StaticMREntries {
		if me.pkgPath != pkgPath || me.name != name {
			continue
		}
		if me.anyRecv {
			if recvType != "" {
				return true
			}
			continue
		}
		if me.recvType == recvType {
			return true
		}
	}
	for _, mf := range m7MultiResultFuncs {
		if mf.pkgPath == pkgPath && mf.name == name && mf.recvType == recvType {
			return true
		}
	}
	for _, mf := range ioMultiResultFuncs {
		if mf.pkgPath == pkgPath && mf.name == name && mf.recvType == recvType {
			return true
		}
	}
	for _, mf := range osMultiResultFuncs {
		if (mf.pkgPath == pkgPath || mf.helperPkg == pkgPath) && mf.name == name && mf.recvType == recvType {
			return true
		}
	}
	for _, mf := range m8MultiResultFuncs {
		if (mf.pkgPath == pkgPath || mf.helperPkg == pkgPath) && mf.name == name && mf.recvType == recvType {
			return true
		}
	}
	for _, mf := range m9MultiResultFuncs {
		if (mf.pkgPath == pkgPath || mf.helperPkg == pkgPath) && mf.name == name && mf.recvType == recvType {
			return true
		}
	}
	return false
}

// hasStaticIfaceMREntry reports whether method is in the static
// interface-dispatch table (m8IfaceMultiResultMethods), matched by method
// name as the classifier does.
func hasStaticIfaceMREntry(method string) bool {
	for _, m := range m8IfaceMultiResultMethods {
		if m.method == method {
			return true
		}
	}
	return false
}

// m7MultiResultFuncs is the desugar table for the encoding/* packages
// (extended P3-M8 to reflect, P3-M9 to math/bits — the mechanism is
// package-path-keyed, so entries for any synthetic package work).
// Every entry was verified against the synthetic source it names:
// helper name, pair struct name, and field names/order match the
// source's __-helper and pair struct exactly (e.g. base32's AppendDecode
// desugars through base32DecodeStringPair{b, err} — there is no
// base32AppendPair).
//
// m7HelperPkgOverride maps a package path to the synthetic package
// holding its M7 __-helpers and pair types. Used when a package was
// retired from synthetic (e.g. reflect now uses the disk importer)
// but the M7 desugaring still needs the helpers. The override package
// must be registered in syntheticSources.
var m7HelperPkgOverride = map[string]string{
	"reflect": "reflect_m7",
}

var m7MultiResultFuncs = []m7MultiResultFunc{
	// errors: AsType[E error] (P4 — was M7 fail-fast). The pair struct
	// and helper are generic; the desugar instantiates both with the
	// call's type argument (instantiatePairType + helperCallTargs).
	{"errors", "AsType", "", "__asTypePub", "asTypePair", []string{"v", "ok"}, false},
	// bytes: methods on *Reader (P3-M9 WS D: v1 cannot emit 2-result methods).
	{"bytes", "Read", "Reader", "__bytesReaderRead", "__bytesReadPair", []string{"n", "err"}, false},
	// bytes: methods on *Buffer (P3-M9 WS D: v1 cannot emit 2-result
	// methods). Retired the M7 fail-fast stub: WS D landed, so the stub's
	// "re-test when it lands" condition is met; the helper is the real gc
	// 1.27.1 Buffer.Read. Shares __bytesReadPair with the Reader row.
	{"bytes", "Read", "Buffer", "__bytesBufferRead", "__bytesReadPair", []string{"n", "err"}, false},
	// bytes: methods on *Buffer (P3-M9 WS D: v1 cannot emit 2-result methods).
	{"bytes", "Write", "Buffer", "__bytesBufferWrite", "__bytesWritePair", []string{"n", "err"}, false},
	// bytes: Buffer.WriteString (P3-M10 WS D discharge; needed by
	// net/url.URL.String and the net/http response writer, hence by any
	// program importing net/http).
	{"bytes", "WriteString", "Buffer", "__bytesBufferWriteString", "__bytesWriteStringPair", []string{"n", "err"}, false},
	// encoding/base32: methods on *Encoding.
	{"encoding/base32", "Decode", "Encoding", "__base32Decode", "base32DecodePair", []string{"n", "err"}, false},
	{"encoding/base32", "DecodeString", "Encoding", "__base32DecodeString", "base32DecodeStringPair", []string{"b", "err"}, false},
	{"encoding/base32", "AppendDecode", "Encoding", "__base32AppendDecode", "base32DecodeStringPair", []string{"b", "err"}, false},
	// encoding/base64: methods on *Encoding.
	{"encoding/base64", "Decode", "Encoding", "__base64Decode", "base64DecodePair", []string{"n", "err"}, false},
	{"encoding/base64", "DecodeString", "Encoding", "__base64DecodeString", "base64DecodeStringPair", []string{"b", "err"}, false},
	{"encoding/base64", "AppendDecode", "Encoding", "__base64AppendDecode", "base64DecodeStringPair", []string{"b", "err"}, false},
	// encoding/base64: streaming encoder/decoder methods (Gate-1 #4).
	{"encoding/base64", "Write", "encoder", "__encoderWrite", "base64EncoderWritePair", []string{"n", "err"}, false},
	{"encoding/base64", "Read", "decoder", "__decoderRead", "base64DecoderReadPair", []string{"n", "err"}, false},
	{"encoding/base64", "Read", "newlineFilteringReader", "__newlineFilteringReaderRead", "base64NewlineFilteringReaderReadPair", []string{"n", "err"}, false},
	// io/fs: ReadLink/Lstat (Gate-1 #4).
	{"io/fs", "ReadLink", "", "__fsReadLink", "fsReadLinkPair", []string{"s", "err"}, false},
	{"io/fs", "Lstat", "", "__fsLstat", "fsLstatPair", []string{"fi", "err"}, false},
	// encoding/hex: package-level funcs.
	{"encoding/hex", "Decode", "", "__hexDecode", "hexDecodePair", []string{"n", "err"}, false},
	{"encoding/hex", "DecodeString", "", "__hexDecodeString", "hexDecodeStringPair", []string{"b", "err"}, false},
	{"encoding/hex", "AppendDecode", "", "__hexAppendDecode", "hexDecodeStringPair", []string{"b", "err"}, false},
	// encoding/binary: package-level funcs.
	{"encoding/binary", "Uvarint", "", "__binaryUvarint", "binaryUvarintPair", []string{"x", "n"}, false},
	{"encoding/binary", "Varint", "", "__binaryVarint", "binaryVarintPair", []string{"x", "n"}, false},
	// encoding/ascii85: package-level func, 3 results.
	{"encoding/ascii85", "Decode", "", "__ascii85Decode", "ascii85DecodeTriple", []string{"ndst", "nsrc", "err"}, false},
	// encoding/pem: package-level func.
	{"encoding/pem", "Decode", "", "__pemDecode", "pemDecodePair", []string{"p", "rest"}, false},
	// encoding/asn1: package-level func. The __-helper implements the
	// scalar cases in real Go and fails fast (needs reflect) for composite
	// types — the desugar applies, the reflect limit moves into the helper.
	// (Unmarshal is NOT desugared: decoding stores through the recipient
	// pointer, which v1 cannot express — WS D — so it fails fast at every
	// call site via isMultiResultSynthetic.)
	{"encoding/asn1", "Marshal", "", "__asn1Marshal", "asn1MarshalPair", []string{"b", "err"}, false},
	// strconv: package-level funcs. The Pub helpers are the public-shape
	// single-result forms (gc-compatible error mapping via toError);
	// ParseFloat's helper is __parseFloat (no Pub variant — its error
	// mapping is internal to __parseFloat).
	{"strconv", "ParseBool", "", "__parseBoolPub", "parseBoolPair", []string{"v", "err"}, false},
	{"strconv", "ParseInt", "", "__parseIntPub", "parseIntPair", []string{"v", "err"}, false},
	{"strconv", "ParseUint", "", "__parseUintPub", "parseUintPair", []string{"v", "err"}, false},
	{"strconv", "Atoi", "", "__atoiPub", "atoiPair", []string{"v", "err"}, false},
	{"strconv", "ParseFloat", "", "__parseFloat", "parseFloatPair", []string{"v", "err"}, false},
	{"strconv", "ParseComplex", "", "__parseComplexPub", "parseComplexPair", []string{"v", "err"}, false},
	{"strconv", "Unquote", "", "__unquotePub", "unquotePair", []string{"v", "err"}, false},
	{"strconv", "QuotedPrefix", "", "__quotedPrefix", "quotedPrefixPair", []string{"v", "err"}, false},
	// UnquoteChar is the 4-result entry (value, multibyte, tail, err) —
	// the desugar supports N-ary via pairFields.
	{"strconv", "UnquoteChar", "", "__unquoteChar", "unquoteCharPair", []string{"value", "multibyte", "tail", "err"}, false},
	// encoding/json: package-level funcs plus methods on *Decoder,
	// Number, and RawMessage (prependRecv passes the receiver as the
	// helper's first argument).
	{"encoding/json", "Marshal", "", "__marshal", "marshalPair", []string{"data", "err"}, false},
	{"encoding/json", "MarshalIndent", "", "__marshalIndent", "marshalIndentPair", []string{"data", "err"}, false},
	{"encoding/json", "Token", "Decoder", "__decoderToken", "decoderTokenPair", []string{"tok", "err"}, false},
	{"encoding/json", "Float64", "Number", "__numberFloat64", "numberFloat64Pair", []string{"f", "err"}, false},
	{"encoding/json", "Int64", "Number", "__numberInt64", "numberInt64Pair", []string{"i", "err"}, false},
	{"encoding/json", "MarshalJSON", "RawMessage", "__rawMessageMarshalJSON", "rawMessageMarshalJSONPair", []string{"data", "err"}, false},
	// P3-M8 (wave 2): synthetic io (synth_io.go). Only the members with
	// real single-result __-helpers are desugared — see the SCOPE NOTE in
	// the patch header; everything else fails at export via
	// isMultiResultSynthetic. prependRecv applies (all have receivers).
	{"io", "Pipe", "", "__pipe", "pipePair", []string{"r", "w"}, false},
	{"io", "Read", "PipeReader", "__pipeReaderRead", "pipeReaderReadPair", []string{"n", "err"}, false},
	{"io", "Write", "PipeWriter", "__pipeWriterWrite", "pipeWriterWritePair", []string{"n", "err"}, false},
	{"io", "Seek", "SectionReader", "__sectionReaderSeek", "sectionReaderSeekPair", []string{"n", "err"}, false},
	{"io", "Outer", "SectionReader", "__sectionReaderOuter", "sectionReaderOuterTriple", []string{"r", "off", "n"}, false},
	{"io", "Seek", "OffsetWriter", "__offsetWriterSeek", "offsetWriterSeekPair", []string{"n", "err"}, false},
	// P3-M8 (wave 2): strings.Reader methods discharged from their M7
	// fail-fast (patch 06, synth_strings.go). They need only io.EOF and
	// the io.Seek* consts — no interface dispatch — so their __-helpers
	// are real. strings.Reader.WriteTo stays fail-fast (needs w.Write).
	{"strings", "Read", "Reader", "__stringsReaderRead", "stringsReaderReadPair", []string{"n", "err"}, false},
	{"strings", "ReadAt", "Reader", "__stringsReaderReadAt", "stringsReaderReadAtPair", []string{"n", "err"}, false},
	{"strings", "ReadByte", "Reader", "__stringsReaderReadByte", "stringsReaderReadBytePair", []string{"b", "err"}, false},
	{"strings", "ReadRune", "Reader", "__stringsReaderReadRune", "stringsReaderReadRuneTriple", []string{"ch", "size", "err"}, false},
	{"strings", "Seek", "Reader", "__stringsReaderSeek", "stringsReaderSeekPair", []string{"n", "err"}, false},
	// strings: Builder.Write/WriteString/WriteRune (P3-M10 WS D
	// discharge of the M7 fail-fasts; needed by url.Values.Encode,
	// hence by any program importing net/http).
	{"strings", "Write", "Builder", "__builderWrite", "__builderWritePair", []string{"n", "err"}, false},
	{"strings", "WriteString", "Builder", "__builderWriteString", "__builderWriteStringPair", []string{"n", "err"}, false},
	{"strings", "WriteRune", "Builder", "__builderWriteRune", "__builderWriteRunePair", []string{"n", "err"}, false},
	// P3-M8 (wave 2): synthetic syscall package (synth_syscall.go).
	// The multi-result members keep exact gc signatures with fail-fast
	// bodies; the __-helpers return pair structs (real implementations
	// for Getenv/Getgroups/Getwd, exact-message panics for the rest).
	{"syscall", "Open", "", "__open", "intErrorPair", []string{"v", "err"}, false},
	{"syscall", "Read", "", "__read", "intErrorPair", []string{"v", "err"}, false},
	{"syscall", "Write", "", "__write", "intErrorPair", []string{"v", "err"}, false},
	{"syscall", "Pread", "", "__pread", "intErrorPair", []string{"v", "err"}, false},
	{"syscall", "Pwrite", "", "__pwrite", "intErrorPair", []string{"v", "err"}, false},
	{"syscall", "Seek", "", "__seek", "int64ErrorPair", []string{"v", "err"}, false},
	{"syscall", "Readlink", "", "__readlink", "intErrorPair", []string{"v", "err"}, false},
	{"syscall", "Mmap", "", "__mmap", "bytesErrorPair", []string{"v", "err"}, false},
	{"syscall", "StartProcess", "", "__startprocess", "startProcessTriple", []string{"pid", "handle", "err"}, false},
	{"syscall", "ForkExec", "", "__forkexec", "intErrorPair", []string{"v", "err"}, false},
	{"syscall", "Wait4", "", "__wait4", "intErrorPair", []string{"v", "err"}, false},
	{"syscall", "Socket", "", "__socket", "intErrorPair", []string{"v", "err"}, false},
	{"syscall", "Accept", "", "__accept", "acceptTriple", []string{"fd", "sa", "err"}, false},
	{"syscall", "GetsockoptInt", "", "__getsockoptint", "intErrorPair", []string{"v", "err"}, false},
	{"syscall", "Recvmsg", "", "__recvmsg", "recvmsgTuple", []string{"n", "oobn", "recvflags", "from", "err"}, false},
	{"syscall", "EpollCreate", "", "__epollcreate", "intErrorPair", []string{"v", "err"}, false},
	{"syscall", "EpollCreate1", "", "__epollcreate1", "intErrorPair", []string{"v", "err"}, false},
	{"syscall", "EpollWait", "", "__epollwait", "intErrorPair", []string{"v", "err"}, false},
	{"syscall", "InotifyInit", "", "__inotifyinit", "intErrorPair", []string{"v", "err"}, false},
	{"syscall", "InotifyAddWatch", "", "__inotifyaddwatch", "intErrorPair", []string{"v", "err"}, false},
	{"syscall", "InotifyRmWatch", "", "__inotifyrmwatch", "intErrorPair", []string{"v", "err"}, false},
	{"syscall", "Getenv", "", "__getenv", "getenvPair", []string{"value", "found"}, false},
	{"syscall", "Getgroups", "", "__getgroups", "gidsErrorPair", []string{"gids", "err"}, false},
	{"syscall", "Getwd", "", "__getwd", "stringErrorPair", []string{"v", "err"}, false},
	{"syscall", "ByteSliceFromString", "", "__byteSliceFromString", "bytesErrorPair", []string{"v", "err"}, false},
	{"syscall", "BytePtrFromString", "", "__bytePtrFromString", "bytePtrErrorPair", []string{"p", "err"}, false},
	{"syscall", "Syscall", "", "__syscall", "syscallTriple", []string{"r1", "r2", "err"}, false},
	{"syscall", "RawSyscall", "", "__rawsyscall", "syscallTriple", []string{"r1", "r2", "err"}, false},
	{"syscall", "Syscall6", "", "__syscall6", "syscallTriple", []string{"r1", "r2", "err"}, false},
	{"syscall", "RawSyscall6", "", "__rawsyscall6", "syscallTriple", []string{"r1", "r2", "err"}, false},
	// P3-M8 (wave 2, reflect): multi-result members desugared via the
	// single-result __-helpers. Select is package-level; the Value methods
	// are on the Value struct; the Type methods are on rtype but usually
	// called via the Type interface (the classify function retries with
	// recvType="rtype" for reflect interface methods); StructTag.Lookup is
	// on the named string type StructTag.
	{"reflect", "Select", "", "__reflectSelect", "reflectSelectTriple", []string{"chosen", "recv", "recvOK"}, false},
	{"reflect", "FieldByIndexErr", "Value", "__reflectFieldByIndexErr", "reflectValuePair", []string{"v", "err"}, false},
	{"reflect", "TryRecv", "Value", "__reflectTryRecv", "reflectValueBoolPair", []string{"x", "ok"}, false},
	{"reflect", "MethodByName", "rtype", "__reflectTMethodByName", "reflectMethodPair", []string{"m", "ok"}, true},
	{"reflect", "FieldByName", "rtype", "__reflectTFieldByName", "reflectStructFieldPair", []string{"f", "ok"}, true},
	{"reflect", "FieldByNameFunc", "rtype", "__reflectTFieldByNameFunc", "reflectStructFieldPair", []string{"f", "ok"}, true},
	{"reflect", "Lookup", "StructTag", "__reflect_taglookup", "reflectTagPair", []string{"value", "ok"}, false},
	// NOTE: the top-level "encoding" package has no functions (only the six
	// marshaling interfaces), so it needs no desugar entry. Its interface
	// methods are multi-result and fail fast via isMultiResultSynthetic.
	// P3-M9 (tier1c): math/bits multi-result functions (Add/Sub/Mul/Div
	// + 32/64 variants). Pair field names match the gc result names in
	// order (see synth_mathbits.go).
	{"math/bits", "Add", "", "__bitsAdd", "bitsAddPair", []string{"sum", "carryOut"}, false},
	{"math/bits", "Add32", "", "__bitsAdd32", "bitsAdd32Pair", []string{"sum", "carryOut"}, false},
	{"math/bits", "Add64", "", "__bitsAdd64", "bitsAdd64Pair", []string{"sum", "carryOut"}, false},
	{"math/bits", "Sub", "", "__bitsSub", "bitsSubPair", []string{"diff", "borrowOut"}, false},
	{"math/bits", "Sub32", "", "__bitsSub32", "bitsSub32Pair", []string{"diff", "borrowOut"}, false},
	{"math/bits", "Sub64", "", "__bitsSub64", "bitsSub64Pair", []string{"diff", "borrowOut"}, false},
	{"math/bits", "Mul", "", "__bitsMul", "bitsMulPair", []string{"hi", "lo"}, false},
	{"math/bits", "Mul32", "", "__bitsMul32", "bitsMul32Pair", []string{"hi", "lo"}, false},
	{"math/bits", "Mul64", "", "__bitsMul64", "bitsMul64Pair", []string{"hi", "lo"}, false},
	{"math/bits", "Div", "", "__bitsDiv", "bitsDivPair", []string{"quo", "rem"}, false},
	{"math/bits", "Div32", "", "__bitsDiv32", "bitsDiv32Pair", []string{"quo", "rem"}, false},
	{"math/bits", "Div64", "", "__bitsDiv64", "bitsDiv64Pair", []string{"quo", "rem"}, false},
	// P3-M9 (tier1c): math.Sincos desugars to the existing __sincos
	// helper (used internally by the Bessel functions; Sin(x)/Cos(x)
	// each bit-exact, and Sincos == (Sin, Cos) bit-wise: 0 mismatches
	// over 100010 adversarial+random inputs vs gc 1.27.1). Needed by
	// the synthetic math/cmplx package (Rect/Exp/Pow/Sin/Cos).
	{"math", "Sincos", "", "__sincos", "__sincosPair", []string{"sin", "cos"}, false},
	{"math", "Frexp", "", "__frexp", "__frexpPair", []string{"frac", "exp"}, false}, // P3-M9 math/big
	// P3-M9 (tier1c): math/cmplx.Polar desugars to the __cmplxPolar helper
	// (Appendix M; pair fields match the gc result names in order).
	{"math/cmplx", "Polar", "", "__cmplxPolar", "cmplxPolarPair", []string{"r", "theta"}, false},
	// P3-M9 (wave 3, math/big): 47 multi-result funcs/methods. Each has a
	{"math/big", "AppendText", "Float", "__mrFloat_AppendText", "__mrFloat_AppendTextPair", []string{"v0", "v1"}, false},
	{"math/big", "AppendText", "Int", "__mrInt_AppendText", "__mrInt_AppendTextPair", []string{"text", "err"}, false},
	{"math/big", "AppendText", "Rat", "__mrRat_AppendText", "__mrRat_AppendTextPair", []string{"v0", "v1"}, false},
	{"math/big", "DivMod", "Int", "__mrInt_DivMod", "__mrInt_DivModPair", []string{"v0", "v1"}, false},
	{"math/big", "Divide", "Int", "__mrInt_Divide", "__mrInt_DividePair", []string{"v0", "v1"}, false},
	{"math/big", "Float32", "Float", "__mrFloat_Float32", "__mrFloat_Float32Pair", []string{"v0", "v1"}, false},
	{"math/big", "Float32", "Rat", "__mrRat_Float32", "__mrRat_Float32Pair", []string{"f", "exact"}, false},
	{"math/big", "Float64", "Float", "__mrFloat_Float64", "__mrFloat_Float64Pair", []string{"v0", "v1"}, false},
	{"math/big", "Float64", "Int", "__mrInt_Float64", "__mrInt_Float64Pair", []string{"v0", "v1"}, false},
	{"math/big", "Float64", "Rat", "__mrRat_Float64", "__mrRat_Float64Pair", []string{"f", "exact"}, false},
	{"math/big", "FloatPrec", "Rat", "__mrRat_FloatPrec", "__mrRat_FloatPrecPair", []string{"n", "exact"}, false},
	{"math/big", "GobEncode", "Float", "__mrFloat_GobEncode", "__mrFloat_GobEncodePair", []string{"v0", "v1"}, false},
	{"math/big", "GobEncode", "Int", "__mrInt_GobEncode", "__mrInt_GobEncodePair", []string{"v0", "v1"}, false},
	{"math/big", "GobEncode", "Rat", "__mrRat_GobEncode", "__mrRat_GobEncodePair", []string{"v0", "v1"}, false},
	{"math/big", "Int", "Float", "__mrFloat_Int", "__mrFloat_IntPair", []string{"v0", "v1"}, false},
	{"math/big", "Int64", "Float", "__mrFloat_Int64", "__mrFloat_Int64Pair", []string{"v0", "v1"}, false},
	{"math/big", "MarshalJSON", "Int", "__mrInt_MarshalJSON", "__mrInt_MarshalJSONPair", []string{"v0", "v1"}, false},
	{"math/big", "MarshalText", "Float", "__mrFloat_MarshalText", "__mrFloat_MarshalTextPair", []string{"text", "err"}, false},
	{"math/big", "MarshalText", "Int", "__mrInt_MarshalText", "__mrInt_MarshalTextPair", []string{"text", "err"}, false},
	{"math/big", "MarshalText", "Rat", "__mrRat_MarshalText", "__mrRat_MarshalTextPair", []string{"text", "err"}, false},
	{"math/big", "Parse", "Float", "__mrFloat_Parse", "__mrFloat_ParsePair", []string{"f", "b", "err"}, false},
	{"math/big", "ParseFloat", "", "__mrParseFloat", "__mrParseFloatPair", []string{"f", "b", "err"}, false},
	{"math/big", "QuoRem", "Int", "__mrInt_QuoRem", "__mrInt_QuoRemPair", []string{"v0", "v1"}, false},
	{"math/big", "Rat", "Float", "__mrFloat_Rat", "__mrFloat_RatPair", []string{"v0", "v1"}, false},
	{"math/big", "ReadByte", "byteReader", "__mrbyteReader_ReadByte", "__mrbyteReader_ReadBytePair", []string{"v0", "v1"}, false},
	{"math/big", "SetString", "Float", "__mrFloat_SetString", "__mrFloat_SetStringPair", []string{"v0", "v1"}, false},
	{"math/big", "SetString", "Int", "__mrInt_SetString", "__mrInt_SetStringPair", []string{"v0", "v1"}, false},
	{"math/big", "SetString", "Rat", "__mrRat_SetString", "__mrRat_SetStringPair", []string{"v0", "v1"}, false},
	{"math/big", "Uint64", "Float", "__mrFloat_Uint64", "__mrFloat_Uint64Pair", []string{"v0", "v1"}, false},
	{"math/big", "div", "nat", "__mrnat_div", "__mrnat_divPair", []string{"q", "r"}, false},
	{"math/big", "divLarge", "nat", "__mrnat_divLarge", "__mrnat_divLargePair", []string{"q", "r"}, false},
	{"math/big", "divW", "nat", "__mrnat_divW", "__mrnat_divWPair", []string{"q", "r"}, false},
	{"math/big", "divWW", "", "__mrdivWW", "__mrdivWWPair", []string{"q", "r"}, false},
	{"math/big", "euclidUpdate", "", "__mreuclidUpdate", "__mreuclidUpdatePair", []string{"nA", "nB", "nr", "nUa", "nUb"}, false},
	{"math/big", "isPow2", "nat", "__mrnat_isPow2", "__mrnat_isPow2Pair", []string{"v0", "v1"}, false},
	{"math/big", "lehmerSimulate", "", "__mrlehmerSimulate", "__mrlehmerSimulatePair", []string{"u0", "u1", "v0", "v1", "even"}, false},
	{"math/big", "maxPow", "", "__mrmaxPow", "__mrmaxPowPair", []string{"p", "n"}, false},
	{"math/big", "mulAddWWW_g", "", "__mrmulAddWWW_g", "__mrmulAddWWW_gPair", []string{"z1", "z0"}, false},
	{"math/big", "mulWW", "", "__mrmulWW", "__mrmulWWPair", []string{"z1", "z0"}, false},
	{"math/big", "quotToFloat32", "", "__mrquotToFloat32", "__mrquotToFloat32Pair", []string{"f", "exact"}, false},
	{"math/big", "quotToFloat64", "", "__mrquotToFloat64", "__mrquotToFloat64Pair", []string{"f", "exact"}, false},
	{"math/big", "scan", "Float", "__mrFloat_scan", "__mrFloat_scanPair", []string{"f", "b", "err"}, false},
	{"math/big", "scan", "Int", "__mrInt_scan", "__mrInt_scanPair", []string{"v0", "v1", "v2"}, false},
	{"math/big", "scan", "nat", "__mrnat_scan", "__mrnat_scanPair", []string{"res", "b", "count", "err"}, false},
	{"math/big", "scanExponent", "", "__mrscanExponent", "__mrscanExponentPair", []string{"exp", "base", "err"}, false},
	{"math/big", "scanSign", "", "__mrscanSign", "__mrscanSignPair", []string{"neg", "err"}, false},
	{"math/big", "setFromScanner", "Int", "__mrInt_setFromScanner", "__mrInt_setFromScannerPair", []string{"v0", "v1"}, false},
	// P3-M9 (tier1c): bufio multi-result methods (Appendix M; pair fields
	// match the gc result names in order; see synth_bufio.go).
	{"bufio", "Read", "Reader", "__bufioReaderRead", "__bufioReaderReadPair", []string{"n", "err"}, false},
	{"bufio", "Peek", "Reader", "__bufioReaderPeek", "__bufioReaderPeekPair", []string{"b", "err"}, false},
	{"bufio", "Discard", "Reader", "__bufioReaderDiscard", "__bufioReaderDiscardPair", []string{"discarded", "err"}, false},
	{"bufio", "ReadByte", "Reader", "__bufioReaderReadByte", "__bufioReaderReadBytePair", []string{"c", "err"}, false},
	{"bufio", "ReadSlice", "Reader", "__bufioReaderReadSlice", "__bufioReaderReadSlicePair", []string{"line", "err"}, false},
	{"bufio", "ReadBytes", "Reader", "__bufioReaderReadBytes", "__bufioReaderReadBytesPair", []string{"b", "err"}, false},
	{"bufio", "ReadString", "Reader", "__bufioReaderReadString", "__bufioReaderReadStringPair", []string{"s", "err"}, false},
	{"bufio", "Write", "Writer", "__bufioWriterWrite", "__bufioWriterWritePair", []string{"n", "err"}, false},
	{"bufio", "WriteString", "Writer", "__bufioWriterWriteString", "__bufioWriterWriteStringPair", []string{"n", "err"}, false},
	// P3-M9 (tier-5b): bufio.ScanLines (Appendix M; the 3-result split
	// function — package-level, so recvType is ""; see synth_bufio.go).
	{"bufio", "ScanLines", "", "__bufioScanLines", "__bufioScanLinesPair", []string{"advance", "token", "err"}, false},
	// P3-M9 (tier1c): math/rand.Read (Appendix M).
	{"math/rand", "Read", "Rand", "__randRead", "__randReadPair", []string{"n", "err"}, false},
	// P3-M9 (tier1c): path.Split (Appendix M).
	{"path", "Split", "", "__pathSplit", "pathSplitPair", []string{"dir", "file"}, false},
	// P3-M9 (tier-5: compress): synthetic compress/compress/flate (Appendix M).
	{"compress/flate", "findMatch", "compressor", "__flateCompressorfindMatch", "__flateCompressorfindMatchPair", []string{"length", "offset", "ok"}, false},
	{"compress/flate", "tryBetterMatchAtEnd", "compressor", "__flateCompressortryBetterMatchAtEnd", "__flateCompressortryBetterMatchAtEndPair", []string{"newLen", "newOff"}, false},
	{"compress/flate", "write", "compressor", "__flateCompressorwrite", "__flateCompressorwritePair", []string{"n", "err"}, false},
	{"compress/flate", "NewWriter", "", "__flateNewWriter", "__flateNewWriterPair", []string{"w", "err"}, false},
	{"compress/flate", "NewWriterDict", "", "__flateNewWriterDict", "__flateNewWriterDictPair", []string{"w", "err"}, false},
	{"compress/flate", "Write", "Writer", "__flateWriterWrite", "__flateWriterWritePair", []string{"n", "err"}, false},
	{"compress/flate", "Read", "decompressor", "__flateDecompressorRead", "__flateDecompressorReadPair", []string{"n", "err"}, false},
	{"compress/flate", "huffSym", "decompressor", "__flateDecompressorhuffSym", "__flateDecompressorhuffSymPair", []string{"sym", "err"}, false},
	{"compress/flate", "headerSize", "huffmanBitWriter", "__flateHuffmanBitWriterheaderSize", "__flateHuffmanBitWriterheaderSizePair", []string{"size", "numCodegens"}, false},
	{"compress/flate", "dynamicSize", "huffmanBitWriter", "__flateHuffmanBitWriterdynamicSize", "__flateHuffmanBitWriterdynamicSizePair", []string{"size", "numCodegens"}, false},
	{"compress/flate", "storedSize", "huffmanBitWriter", "__flateHuffmanBitWriterstoredSize", "__flateHuffmanBitWriterstoredSizePair", []string{"size", "ok"}, false},
	{"compress/flate", "indexTokens", "huffmanBitWriter", "__flateHuffmanBitWriterindexTokens", "__flateHuffmanBitWriterindexTokensPair", []string{"numLiterals", "numOffsets"}, false},
	// P3-M9 (tier-5: compress): synthetic compress/compress/gzip (Appendix M).
	{"compress/gzip", "NewReader", "", "__gzipNewReader", "__gzipNewReaderPair", []string{"r", "err"}, false},
	{"compress/gzip", "readString", "Reader", "__gzipReaderreadString", "__gzipReaderreadStringPair", []string{"s", "err"}, false},
	{"compress/gzip", "readHeader", "Reader", "__gzipReaderreadHeader", "__gzipReaderreadHeaderPair", []string{"hdr", "err"}, false},
	{"compress/gzip", "Read", "Reader", "__gzipReaderRead", "__gzipReaderReadPair", []string{"n", "err"}, false},
	{"compress/gzip", "NewWriterLevel", "", "__gzipNewWriterLevel", "__gzipNewWriterLevelPair", []string{"w", "err"}, false},
	{"compress/gzip", "Write", "Writer", "__gzipWriterWrite", "__gzipWriterWritePair", []string{"n", "err"}, false},
	// P3-M9 (tier-5: compress): synthetic compress/compress/zlib (Appendix M).
	{"compress/zlib", "NewReader", "", "__zlibNewReader", "__zlibNewReaderPair", []string{"r", "err"}, false},
	{"compress/zlib", "NewReaderDict", "", "__zlibNewReaderDict", "__zlibNewReaderDictPair", []string{"r", "err"}, false},
	{"compress/zlib", "Read", "reader", "__zlibReaderRead", "__zlibReaderReadPair", []string{"n", "err"}, false},
	{"compress/zlib", "NewWriterLevel", "", "__zlibNewWriterLevel", "__zlibNewWriterLevelPair", []string{"w", "err"}, false},
	{"compress/zlib", "NewWriterLevelDict", "", "__zlibNewWriterLevelDict", "__zlibNewWriterLevelDictPair", []string{"w", "err"}, false},
	{"compress/zlib", "Write", "Writer", "__zlibWriterWrite", "__zlibWriterWritePair", []string{"n", "err"}, false},
	// P3-M9 (tier-5: compress): synthetic compress/compress/lzw (Appendix M).
	{"compress/lzw", "readLSB", "Reader", "__lzwReaderreadLSB", "__lzwReaderreadLSBPair", []string{"code", "err"}, false},
	{"compress/lzw", "readMSB", "Reader", "__lzwReaderreadMSB", "__lzwReaderreadMSBPair", []string{"code", "err"}, false},
	{"compress/lzw", "Read", "Reader", "__lzwReaderRead", "__lzwReaderReadPair", []string{"n", "err"}, false},
	{"compress/lzw", "Write", "Writer", "__lzwWriterWrite", "__lzwWriterWritePair", []string{"n", "err"}, false},
	// P3-M9 (tier-5: compress): synthetic compress/compress/bzip2 (Appendix M).
	{"compress/bzip2", "Read", "reader", "__bzip2ReaderRead", "__bzip2ReaderReadPair", []string{"n", "err"}, false},
	{"compress/bzip2", "read", "reader", "__bzip2Readerread", "__bzip2ReaderreadPair", []string{"n", "err"}, false},
	{"compress/bzip2", "newHuffmanTree", "", "__bzip2NewHuffmanTree", "__bzip2NewHuffmanTreePair", []string{"t", "err"}, false},
	{"compress/bzip2", "buildHuffmanNode", "", "__bzip2BuildHuffmanNode", "__bzip2BuildHuffmanNodePair", []string{"nodeIndex", "err"}, false},
	// P3-M9 (tier-5: archive/mime): synthetic archive/* and mime/* (Appendix M).
	{"archive/tar", "allowedFormats", "Header", "__tarHeaderallowedFormats", "__tarHeaderallowedFormatsPair", []string{"format", "paxHdrs", "err"}, false},
	{"archive/tar", "FileInfoHeader", "", "__tarFileInfoHeader", "__tarFileInfoHeaderPair", []string{"f0", "f1"}, false},
	{"archive/tar", "computeChecksum", "block", "__tarblockcomputeChecksum", "__tarblockcomputeChecksumPair", []string{"f0", "signed"}, false},
	{"archive/tar", "Next", "Reader", "__tarReaderNext", "__tarReaderNextPair", []string{"f0", "f1"}, false},
	{"archive/tar", "next", "Reader", "__tarReadernext", "__tarReadernextPair", []string{"f0", "f1"}, false},
	{"archive/tar", "readGNUSparsePAXHeaders", "Reader", "__tarReaderreadGNUSparsePAXHeaders", "__tarReaderreadGNUSparsePAXHeadersPair", []string{"f0", "f1"}, false},
	{"archive/tar", "parsePAX", "", "__tarparsePAX", "__tarparsePAXPair", []string{"f0", "f1"}, false},
	{"archive/tar", "readHeader", "Reader", "__tarReaderreadHeader", "__tarReaderreadHeaderPair", []string{"f0", "f1", "f2"}, false},
	{"archive/tar", "readOldGNUSparseMap", "Reader", "__tarReaderreadOldGNUSparseMap", "__tarReaderreadOldGNUSparseMapPair", []string{"f0", "f1"}, false},
	{"archive/tar", "readGNUSparseMap1x0", "", "__tarreadGNUSparseMap1x0", "__tarreadGNUSparseMap1x0Pair", []string{"f0", "f1"}, false},
	{"archive/tar", "readGNUSparseMap0x1", "", "__tarreadGNUSparseMap0x1", "__tarreadGNUSparseMap0x1Pair", []string{"f0", "f1"}, false},
	{"archive/tar", "appendSparseEntry", "", "__tarappendSparseEntry", "__tarappendSparseEntryPair", []string{"f0", "f1"}, false},
	{"archive/tar", "Read", "Reader", "__tarReaderRead", "__tarReaderReadPair", []string{"f0", "f1"}, false},
	{"archive/tar", "writeTo", "Reader", "__tarReaderwriteTo", "__tarReaderwriteToPair", []string{"f0", "f1"}, false},
	{"archive/tar", "Read", "regFileReader", "__tarregFileReaderRead", "__tarregFileReaderReadPair", []string{"n", "err"}, false},
	{"archive/tar", "WriteTo", "regFileReader", "__tarregFileReaderWriteTo", "__tarregFileReaderWriteToPair", []string{"f0", "f1"}, false},
	{"archive/tar", "Read", "sparseFileReader", "__tarsparseFileReaderRead", "__tarsparseFileReaderReadPair", []string{"n", "err"}, false},
	{"archive/tar", "WriteTo", "sparseFileReader", "__tarsparseFileReaderWriteTo", "__tarsparseFileReaderWriteToPair", []string{"n", "err"}, false},
	{"archive/tar", "mustReadFull", "", "__tarmustReadFull", "__tarmustReadFullPair", []string{"f0", "f1"}, false},
	{"archive/tar", "tryReadFull", "", "__tartryReadFull", "__tartryReadFullPair", []string{"n", "err"}, false},
	{"archive/tar", "readSpecialFile", "", "__tarreadSpecialFile", "__tarreadSpecialFilePair", []string{"f0", "f1"}, false},
	{"archive/tar", "splitUSTARPath", "", "__tarsplitUSTARPath", "__tarsplitUSTARPathPair", []string{"f0", "suffix", "ok"}, false},
	{"archive/tar", "Write", "Writer", "__tarWriterWrite", "__tarWriterWritePair", []string{"f0", "f1"}, false},
	{"archive/tar", "readFrom", "Writer", "__tarWriterreadFrom", "__tarWriterreadFromPair", []string{"f0", "f1"}, false},
	{"archive/tar", "Write", "regFileWriter", "__tarregFileWriterWrite", "__tarregFileWriterWritePair", []string{"n", "err"}, false},
	{"archive/tar", "ReadFrom", "regFileWriter", "__tarregFileWriterReadFrom", "__tarregFileWriterReadFromPair", []string{"f0", "f1"}, false},
	{"archive/tar", "Write", "sparseFileWriter", "__tarsparseFileWriterWrite", "__tarsparseFileWriterWritePair", []string{"n", "err"}, false},
	{"archive/tar", "ReadFrom", "sparseFileWriter", "__tarsparseFileWriterReadFrom", "__tarsparseFileWriterReadFromPair", []string{"n", "err"}, false},
	{"archive/tar", "parsePAXTime", "", "__tarparsePAXTime", "__tarparsePAXTimePair", []string{"f0", "f1"}, false},
	{"archive/tar", "parsePAXRecord", "", "__tarparsePAXRecord", "__tarparsePAXRecordPair", []string{"f0", "f1", "r", "err"}, false},
	{"archive/tar", "formatPAXRecord", "", "__tarformatPAXRecord", "__tarformatPAXRecordPair", []string{"f0", "f1"}, false},
	{"archive/zip", "OpenReader", "", "__zipOpenReader", "__zipOpenReaderPair", []string{"f0", "f1"}, false},
	{"archive/zip", "NewReader", "", "__zipNewReader", "__zipNewReaderPair", []string{"f0", "f1"}, false},
	{"archive/zip", "DataOffset", "File", "__zipFileDataOffset", "__zipFileDataOffsetPair", []string{"offset", "err"}, false},
	{"archive/zip", "Open", "File", "__zipFileOpen", "__zipFileOpenPair", []string{"f0", "f1"}, false},
	{"archive/zip", "OpenRaw", "File", "__zipFileOpenRaw", "__zipFileOpenRawPair", []string{"f0", "f1"}, false},
	{"archive/zip", "Read", "dirReader", "__zipdirReaderRead", "__zipdirReaderReadPair", []string{"f0", "f1"}, false},
	{"archive/zip", "Stat", "checksumReader", "__zipchecksumReaderStat", "__zipchecksumReaderStatPair", []string{"f0", "f1"}, false},
	{"archive/zip", "Read", "checksumReader", "__zipchecksumReaderRead", "__zipchecksumReaderReadPair", []string{"n", "err"}, false},
	{"archive/zip", "Close", "checksumReader", "__zipchecksumReaderClose", "__zipchecksumReaderClosePair", []string{"f0", "f1"}, false},
	{"archive/zip", "readDirectoryEnd", "", "__zipreadDirectoryEnd", "__zipreadDirectoryEndPair", []string{"dir", "baseOffset", "err"}, false},
	{"archive/zip", "findDirectory64End", "", "__zipfindDirectory64End", "__zipfindDirectory64EndPair", []string{"f0", "f1"}, false},
	{"archive/zip", "stat", "fileListEntry", "__zipfileListEntrystat", "__zipfileListEntrystatPair", []string{"f0", "f1"}, false},
	{"archive/zip", "Open", "Reader", "__zipReaderOpen", "__zipReaderOpenPair", []string{"f0", "f1"}, false},
	{"archive/zip", "split", "", "__zipsplit", "__zipsplitPair", []string{"f0", "elem", "isDir"}, false},
	{"archive/zip", "Close", "openDir", "__zipopenDirClose", "__zipopenDirClosePair", []string{"f0", "f1"}, false},
	{"archive/zip", "ReadDir", "openDir", "__zipopenDirReadDir", "__zipopenDirReadDirPair", []string{"f0", "f1"}, false},
	{"archive/zip", "Create", "Writer", "__zipWriterCreate", "__zipWriterCreatePair", []string{"f0", "f1"}, false},
	{"archive/zip", "detectUTF8", "", "__zipdetectUTF8", "__zipdetectUTF8Pair", []string{"f0", "require"}, false},
	{"archive/zip", "CreateHeader", "Writer", "__zipWriterCreateHeader", "__zipWriterCreateHeaderPair", []string{"f0", "f1"}, false},
	{"archive/zip", "CreateRaw", "Writer", "__zipWriterCreateRaw", "__zipWriterCreateRawPair", []string{"f0", "f1"}, false},
	{"archive/zip", "Write", "fileWriter", "__zipfileWriterWrite", "__zipfileWriterWritePair", []string{"f0", "f1"}, false},
	{"archive/zip", "Write", "countWriter", "__zipcountWriterWrite", "__zipcountWriterWritePair", []string{"f0", "f1"}, false},
	{"archive/zip", "FileInfoHeader", "", "__zipFileInfoHeader", "__zipFileInfoHeaderPair", []string{"f0", "f1"}, false},
	{"archive/zip", "timeToMsDosTime", "", "__ziptimeToMsDosTime", "__ziptimeToMsDosTimePair", []string{"fDate", "fTime"}, false},
	{"archive/zip", "Write", "pooledFlateWriter", "__zippooledFlateWriterWrite", "__zippooledFlateWriterWritePair", []string{"n", "err"}, false},
	{"archive/zip", "Read", "pooledFlateReader", "__zippooledFlateReaderRead", "__zippooledFlateReaderReadPair", []string{"n", "err"}, false},
	{"mime", "ParseMediaType", "", "__mimeParseMediaType", "__mimeParseMediaTypePair", []string{"mediatype", "params", "err"}, false},
	{"mime", "decode2231Enc", "", "__mimedecode2231Enc", "__mimedecode2231EncPair", []string{"f0", "f1"}, false},
	{"mime", "consumeToken", "", "__mimeconsumeToken", "__mimeconsumeTokenPair", []string{"f0", "rest"}, false},
	{"mime", "consumeValue", "", "__mimeconsumeValue", "__mimeconsumeValuePair", []string{"f0", "rest"}, false},
	{"mime", "consumeMediaParam", "", "__mimeconsumeMediaParam", "__mimeconsumeMediaParamPair", []string{"f0", "f1", "rest"}, false},
	{"mime", "percentHexUnescape", "", "__mimepercentHexUnescape", "__mimepercentHexUnescapePair", []string{"f0", "f1"}, false},
	{"mime", "Decode", "WordDecoder", "__mimeWordDecoderDecode", "__mimeWordDecoderDecodePair", []string{"f0", "f1"}, false},
	{"mime", "DecodeHeader", "WordDecoder", "__mimeWordDecoderDecodeHeader", "__mimeWordDecoderDecodeHeaderPair", []string{"f0", "f1"}, false},
	{"mime", "decode", "", "__mimedecode", "__mimedecodePair", []string{"f0", "f1"}, false},
	{"mime", "qDecode", "", "__mimeqDecode", "__mimeqDecodePair", []string{"f0", "f1"}, false},
	{"mime", "readHexByte", "", "__mimereadHexByte", "__mimereadHexBytePair", []string{"f0", "f1"}, false},
	{"mime", "fromHex", "", "__mimefromHex", "__mimefromHexPair", []string{"f0", "f1"}, false},
	{"mime/multipart", "Read", "stickyErrorReader", "__mpstickyErrorReaderRead", "__mpstickyErrorReaderReadPair", []string{"n", "_"}, false},
	{"mime/multipart", "newPart", "", "__mpnewPart", "__mpnewPartPair", []string{"f0", "f1"}, false},
	{"mime/multipart", "Read", "Part", "__mpPartRead", "__mpPartReadPair", []string{"n", "err"}, false},
	{"mime/multipart", "Read", "partReader", "__mppartReaderRead", "__mppartReaderReadPair", []string{"f0", "f1"}, false},
	{"mime/multipart", "scanUntilBoundary", "", "__mpscanUntilBoundary", "__mpscanUntilBoundaryPair", []string{"f0", "f1"}, false},
	{"mime/multipart", "NextPart", "Reader", "__mpReaderNextPart", "__mpReaderNextPartPair", []string{"f0", "f1"}, false},
	{"mime/multipart", "NextRawPart", "Reader", "__mpReaderNextRawPart", "__mpReaderNextRawPartPair", []string{"f0", "f1"}, false},
	{"mime/multipart", "nextPart", "Reader", "__mpReadernextPart", "__mpReadernextPartPair", []string{"f0", "f1"}, false},
	{"mime/multipart", "CreatePart", "Writer", "__mpWriterCreatePart", "__mpWriterCreatePartPair", []string{"f0", "f1"}, false},
	{"mime/multipart", "CreateFormFile", "Writer", "__mpWriterCreateFormFile", "__mpWriterCreateFormFilePair", []string{"f0", "f1"}, false},
	{"mime/multipart", "CreateFormField", "Writer", "__mpWriterCreateFormField", "__mpWriterCreateFormFieldPair", []string{"f0", "f1"}, false},
	{"mime/multipart", "Write", "part", "__mppartWrite", "__mppartWritePair", []string{"n", "err"}, false},
	{"mime/quotedprintable", "fromHex", "", "__qpfromHex", "__qpfromHexPair", []string{"f0", "f1"}, false},
	{"mime/quotedprintable", "readHexByte", "", "__qpreadHexByte", "__qpreadHexBytePair", []string{"b", "err"}, false},
	{"mime/quotedprintable", "Read", "Reader", "__qpReaderRead", "__qpReaderReadPair", []string{"n", "err"}, false},
	{"mime/quotedprintable", "Write", "Writer", "__qpWriterWrite", "__qpWriterWritePair", []string{"n", "err"}, false},
	// net/http: package-level funcs (P3-M10 — the M9 net/http drop
	// omitted these; without them any program importing net/http fails
	// with the WS D multi-result scope error at the first client call).
	{"net/http", "NewRequest", "", "__newRequest", "__newRequestPair", []string{"req", "err"}, false},
	{"net/http", "ReadRequest", "", "__readRequest", "__readRequestPair", []string{"req", "err"}, false},
	{"net/http", "readLine", "", "__readLine", "__readLinePair", []string{"line", "err"}, false},
	{"net/http", "readMIMEHeader", "", "__readMIMEHeader", "__readMIMEHeaderPair", []string{"hdr", "err"}, false},
	{"net/http", "ReadResponse", "", "__readResponse", "__readResponsePair", []string{"resp", "err"}, false},
	{"net/http", "Get", "", "__httpGet", "__clientDoPair", []string{"resp", "err"}, false},
	{"net/http", "Head", "", "__httpHead", "__clientDoPair", []string{"resp", "err"}, false},
	{"net/http", "Post", "", "__httpPost", "__clientDoPair", []string{"resp", "err"}, false},
	{"net/http", "PostForm", "", "__httpPostForm", "__clientDoPair", []string{"resp", "err"}, false},
	// net/http: methods.
	{"net/http", "Do", "Client", "__clientDo", "__clientDoPair", []string{"resp", "err"}, false},
	{"net/http", "Handler", "ServeMux", "__muxHandler", "__muxHandlerPair", []string{"h", "pattern"}, false},
	{"net/http", "RoundTrip", "Transport", "__transportRoundTrip", "__clientDoPair", []string{"resp", "err"}, false},
	{"net/http", "Read", "chunkedReader", "__chunkedRead", "__chunkedReadPair", []string{"n", "err"}, false},
	{"net/http", "Read", "limitedReadCloser", "__limitedReadCloserRead", "__limitedReadCloserReadPair", []string{"n", "err"}, false},
	{"net/http", "Write", "responseWriter", "__responseWriterWrite", "__responseWriterWritePair", []string{"n", "err"}, false},
	{"net/http", "Hijack", "responseWriter", "__rwHijack", "__rwHijackPair", []string{"c", "rw", "err"}, false},
	// net/http/httptest: methods.
	{"net/http/httptest", "Write", "ResponseRecorder", "__recorderWrite", "__recorderWritePair", []string{"n", "err"}, false},
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
	// P3-M9: (*reader).Read (the method behind the Reader global) needs its
	// own helper for interface dispatch: without it the *reader method table
	// is empty and rand.Reader.Read fails with "no method table installed".
	{"crypto/rand", "Read", "reader", "__randReaderRead", "", "readPair", []string{"n", "err"}},
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
	// P3-M9: pXXXCurve wrapper helpers for interface dispatch (Curve.Add etc.).
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
	{"crypto/internal/fips140/ecdsa", "sign", "", "__mrsign", "", "__mrsignPair", []string{"v0", "v1"}},
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
	// P3-M9 (x509/TLS lane): private-key parsing, Verify.
	{"crypto/x509", "ParsePKCS8PrivateKey", "", "__mrParsePKCS8PrivateKey", "", "__mrParsePKCS8PrivateKeyPair", []string{"v0", "v1"}},
	{"crypto/x509", "ParsePKCS1PrivateKey", "", "__mrParsePKCS1PrivateKey", "", "__mrParsePKCS1PrivateKeyPair", []string{"v0", "v1"}},
	{"crypto/x509", "ParseECPrivateKey", "", "__mrParseECPrivateKey", "", "__mrParseECPrivateKeyPair", []string{"v0", "v1"}},
	{"crypto/x509", "Verify", "Certificate", "__mrVerify", "", "__mrVerifyPair", []string{"v0", "v1"}},
	{"crypto/x509", "parsePrivateKeyDescriptor", "", "__mrParsePrivateKeyDescriptor", "", "__mrParsePrivateKeyDescriptorPair", []string{"v0", "v1"}},
	{"crypto/x509", "hashFromSigAlg", "", "__mrHashFromSigAlg", "", "__mrHashFromSigAlgPair", []string{"v0", "v1"}},
	// P3-M9 (x509/TLS lane): crypto/tls multi-result functions.
	{"crypto/tls", "X509KeyPair", "", "__mrX509KeyPair", "", "__mrX509KeyPairPair", []string{"v0", "v1"}},
	{"crypto/tls", "LoadX509KeyPair", "", "__mrLoadX509KeyPair", "", "__mrLoadX509KeyPairPair", []string{"v0", "v1"}},
	{"crypto/tls", "Dial", "", "__mrDial", "", "__mrDialPair", []string{"v0", "v1"}},
	{"crypto/tls", "DialWithDialer", "", "__mrDialWithDialer", "", "__mrDialWithDialerPair", []string{"v0", "v1"}},
	{"crypto/tls", "Listen", "", "__mrListen", "", "__mrListenPair", []string{"v0", "v1"}},
	// P3-M9 (x509/TLS lane): (*listener).Accept concrete method (the tls
	// listener type implements net.Listener; the stub panics and calls
	// rewrite to __listenerAccept).
	{"crypto/tls", "Accept", "listener", "__listenerAccept", "", "__listenerAcceptPair", []string{"c", "err"}},
	// P3-M9 (x509/TLS lane, legs-verify-fix): (*net.TCPListener).Accept
	// itab wiring. __listenerAccept calls l.Listener.Accept() — an
	// interface dispatch whose dynamic type is *net.TCPListener; without
	// this entry multiResultHelperFor misses it and the dispatch fails
	// with 'missing method "Accept" for dynamic type "*net.TCPListener"'
	// (01_handshake legs red in legs-nilmap-fix and legs-verify-fix).
	{"net", "Accept", "TCPListener", "__tcpListenerAccept", "", "acceptPair", []string{"c", "err"}},
	// P3-M9 (x509/TLS lane): (*Conn).Read/Write concrete methods.
	{"crypto/tls", "Read", "Conn", "__tlsConnRead", "", "__tlsConnReadPair", []string{"n", "err"}},
	{"crypto/tls", "Write", "Conn", "__tlsConnWrite", "", "__tlsConnWritePair", []string{"n", "err"}},
	{"golang.org/x/crypto/cryptobyte", "Bytes", "Builder", "__mrBuilder_Bytes", "", "__mrBuilder_BytesPair", []string{"v0", "v1"}},
}

// (nistCurve func-field table removed: hunt-special-cases. The synthetic
// crypto/ecdh nistCurve fields are pair-returning (1 result), so the
// N>1 check never triggers for them. Non-synthetic nistCurve doesn't exist.
// classifyGeneralFuncField handles all func-typed struct fields generally.)

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
	{"net", "SplitHostPort", "", "__splitHostPort", "", "splitHostPortPair", []string{"host", "port", "err"}},
	{"net", "Size", "IPMask", "__ipMaskSize", "", "maskSizePair", []string{"ones", "bits"}},
	// net: interface enumeration (P3-M9 module-probe lane).
	{"net", "Interfaces", "", "__interfaces", "", "interfacesPair", []string{"ifs", "err"}},
	// net: methods on TCPListener / Conn (prependRecv).
	{"net", "Accept", "TCPListener", "__tcpListenerAccept", "", "acceptPair", []string{"c", "err"}},
	// P3-M9 (x509/TLS lane): (*Dialer).Dial (prependRecv). The method body
	// calls the 2-result package-level Dial/DialTimeout, which cannot
	// appear in return position under WS D, so it desugars to __dialerDial.
	{"net", "Dial", "Dialer", "__dialerDial", "", "dialPair", []string{"c", "err"}},
	// real-nethttp: (*Dialer).DialContext (needed by crypto/tls.Dialer).
	{"net", "DialContext", "Dialer", "__dialerDialContext", "", "dialPair", []string{"c", "err"}},
	{"net", "Read", "TCPConn", "__tcpConnRead", "", "readPair", []string{"n", "err"}},
	{"net", "Write", "TCPConn", "__tcpConnWrite", "", "writePair", []string{"n", "err"}},
	{"net", "Read", "UDPConn", "__udpConnRead", "", "readPair", []string{"n", "err"}},
	{"net", "Write", "UDPConn", "__udpConnWrite", "", "writePair", []string{"n", "err"}},
	{"net", "ReadFrom", "UDPConn", "__udpConnReadFrom", "", "readFromPair", []string{"n", "addr", "err"}},
	{"net", "WriteTo", "UDPConn", "__udpConnWriteTo", "", "writePair", []string{"n", "err"}},
	// net: methods on pipeConn (net.Pipe; prependRecv). The itab maps
	// these to the pair-returning __-helpers so interface dispatch
	// (net.Conn) works — without entries the methods stay out of the
	// table and dispatch fails fast with "missing method" (wave3
	// net/00_pipe red, 2026-10-12).
	{"net", "Read", "pipeConn", "__pipeRead", "", "__pipeReadPair", []string{"n", "err"}},
	{"net", "Write", "pipeConn", "__pipeWrite", "", "__pipeWritePair", []string{"n", "err"}},
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
	{"io", "WriteString", "", "__writeString", "ioWritePair", []string{"n", "err"}, false},
	{"io", "ReadAtLeast", "", "__readAtLeast", "readAtLeastPair", []string{"n", "err"}, false},
	{"io", "ReadFull", "", "__readFull", "readFullPair", []string{"n", "err"}, false},
	{"io", "Copy", "", "__copy", "copyPair", []string{"written", "err"}, false},
	{"io", "CopyN", "", "__copyN", "copyNPair", []string{"written", "err"}, false},
	{"io", "CopyBuffer", "", "__copyBufferChecked", "copyBufferPair", []string{"written", "err"}, false},
	{"io", "ReadAll", "", "__readAll", "readAllPair", []string{"b", "err"}, false},
	{"io", "Read", "LimitedReader", "__limitedReaderRead", "limitedReaderReadPair", []string{"n", "err"}, false},
	{"io", "Write", "discard", "__discardWrite", "ioWritePair", []string{"n", "err"}, false},
	{"io", "WriteString", "discard", "__discardWriteString", "ioWritePair", []string{"n", "err"}, false},
	// NOTE (P3-M9): the testing/iotest and testing entries added in 139a681
	// are intentionally absent. They referenced __-helpers and pair types
	// that were never defined in any synthetic source, so
	// classifyIoMultiResultCall could never match them (pair-type lookup
	// returns nil). Dead table entries referencing non-existent code are
	// removed, not left dangling; re-add them if/when the helpers are
	// implemented, with the d1IoTable pin extended in lockstep.
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
	// P3 (WS D, Gate-1): io.WriterTo.WriteTo and io.ReaderFrom.ReadFrom.
	// bufio (a dependency of image/png and many others) calls both on
	// interface values: r.WriteTo(w) on io.WriterTo in (*Reader).WriteTo,
	// readerFrom.ReadFrom(r) on io.ReaderFrom in (*Writer).ReadFrom.
	// The result shape (int64, error) differs from Write's (int, error),
	// so they need their own pair types (ioWriteToPair, ioReadFromPair
	// in synth_io.go); the net writePair/readFromPair entries do not
	// match structurally (int vs int64, and 3-result for PacketConn).
	// Matching stays structural on (method name, result count, positional
	// result types) per classifyIfaceMultiResultCall, so user-declared
	// interfaces with the same shape dispatch too.
	{"io", "WriteTo", "ioWriteToPair", []string{"n", "err"}},
	{"io", "ReadFrom", "ioReadFromPair", []string{"n", "err"}},
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
	// P3-M9 (tier-4: net): net.Listener.Accept, net.PacketConn.WriteTo,
	// net.PacketConn.ReadFrom. The corpus calls these on interface values
	// (ln.Accept(), pc.WriteTo(...)); without entries the export fails
	// fast with "interface method Accept/WriteTo has 2 results" (wave3
	// net/02 + net/04 red, 2026-10-12). Pair types live in synthetic
	// net; field names/types are positionally identical to the methods'
	// results (checked by classifyIfaceMultiResultCall).
	{"net", "Accept", "acceptPair", []string{"c", "err"}},
	{"net", "WriteTo", "writePair", []string{"n", "err"}},
	{"net", "ReadFrom", "readFromPair", []string{"n", "addr", "err"}},
	// P3-M10 (tier-5: net/http): RoundTripper.RoundTrip, dispatched via
	// the itab to the concrete transport's pair-returning __-helper
	// (wired via the m7MultiResultFuncs entry + methodTableEntries).
	// Pair fields are positionally identical to the method's results
	// (checked by classifyIfaceMultiResultCall).
	{"net/http", "RoundTrip", "__clientDoPair", []string{"resp", "err"}},
}

// mrPlan is the desugar plan for one classified call.
type mrPlan struct {
	kind       mrKind
	helperPkg  string // "context" or "time"
	helperName string // "__withCancel", "__deadlineOf", ...
	// WS G (P3-M9): disk-package multi-result (transformDiskMultiResult).
	// The helper lives in a disk-built package, resolved via
	// getDiskPackage, not getSyntheticPackage.
	isDisk bool
	// tmpType is the helper's single result type (the __p5mr_N temp).
	tmpType types.Type
	// For mrWithCancel/mrWithDeadline: the second extraction is a
	// method call on the temp (methodRecv.methodName()).
	methodRecv *types.Named
	methodName string
	// For mrDeadline/mrParseDur: the pair field names ([d, ok]/[d, err]).
	pairFields  []string
	prependRecv bool
	// For mrTrivialFwd: index of the forwarded function-typed parameter
	// in the method's explicit parameter list (receiver excluded).
	fwdParamIdx int
	// For mrFmtF: the S-family variant ("Sprint", "Sprintf", "Sprintln")
	// used to format before the io.Writer.Write.
	fmtVariant string
	// For mrRuntimeStack: which intrinsic wire node desugarRuntimeStack
	// builds (rtStackCaller/rtStackFramesNext/rtStackFuncFileLine).
	rtStackOp rtStackOpKind
	// For mrSyncMap: the sync.Map multi-result method name
	// (Load/LoadOrStore/LoadAndDelete/Swap), selecting the syncop wire
	// node op and its arity check in desugarSyncMap.
	syncMapOp string
}

// classifyMultiResultCall recognizes the Appendix M operations (the 5
// reachable ones; the mrDeadline path is retained defense-in-depth for
// WS D — see the package header). It returns nil when c is not one of
// them.
// instantiatePairType returns the concrete pair type for a desugar-table
// entry. Synthetic pair structs may be generic (e.g.
// __mrnistCurve_pointFromAffinePair[Point nistPoint[Point]]); the table
// lookup yields the template, whose type params would leak "typeparam"
// nodes into the export. When call-site type args are available, instantiate
// the template so tmpType is fully concrete.
func instantiatePairType(nt *types.Named, targs []types.Type) *types.Named {
	if nt == nil || len(targs) == 0 {
		return nt
	}
	tp := nt.TypeParams()
	if tp == nil || tp.Len() == 0 {
		return nt
	}
	if tp.Len() != len(targs) {
		return nt
	}
	inst, err := types.Instantiate(nil, nt, targs, true)
	if err != nil {
		return nt
	}
	if nit, ok := inst.(*types.Named); ok {
		return nit
	}
	return nt
}

// targsFromInstances extracts call-site type arguments for a generic function
// call from go/types instance info.
func targsFromInstances(e *exporter, id *ast.Ident) []types.Type {
	if ii, ok := e.info.Instances[id]; ok {
		return typeList(ii.TypeArgs)
	}
	return nil
}

// targsFromRecv extracts type arguments from a generic receiver type
// (e.g. *nistCurve[MyPoint] yields [MyPoint]).
func targsFromRecv(recv types.Type) []types.Type {
	rt := recv
	if pt, ok := rt.(*types.Pointer); ok {
		rt = pt.Elem()
	}
	nt, ok := rt.(*types.Named)
	if !ok {
		return nil
	}
	ta := nt.TypeArgs()
	if ta == nil || ta.Len() == 0 {
		return nil
	}
	out := make([]types.Type, ta.Len())
	for i := range out {
		out[i] = ta.At(i)
	}
	return out
}

// userMRInfoForVar returns the MR pair info for a variable holding a
// multi-result closure. The link is established at the variable's
// definition (exportPackageVars, exportVarSpec, importSyntheticPackage).
// For disk-package vars with eta-expanded initializers (e.g.
// path/filepath's lstat), the FuncLit is found via e.etaVarLits and its
// pair info via e.funcLitMRInfo. For vars aliasing a Func (e.g. lstat ->
// os.Lstat), the table info is used. General (real-x509 lane).
func (e *exporter) userMRInfoForVar(v *types.Var) *userMRInfo {
	if mrInfo, ok := e.userMRVars[v]; ok {
		return mrInfo
	}
	// Fallback: eta-expanded var (disk package). Find the FuncLit and
	// its pair info.
	if lit, ok := e.etaVarLits[v]; ok {
		if mrInfo, ok := e.funcLitMRInfo[lit]; ok {
			e.userMRVars[v] = mrInfo
			return mrInfo
		}
	}
	// Fallback: var aliases a Func (e.g. lstat -> os.Lstat). Get the
	// table MR info for the aliased Func.
	if aliasedFn, ok := e.varFuncAliases[v]; ok {
		pkgPath := ""
		if aliasedFn.Pkg() != nil {
			pkgPath = aliasedFn.Pkg().Path()
		}
		// Use the table classifiers to get the pair info.
		// We need to construct a plan to get tmpType/pairFields.
		var plan *mrPlan
		if plan = classifyOsMultiResultCall(pkgPath, aliasedFn, nil); plan == nil {
			if plan = classifyM7MultiResultCall(pkgPath, aliasedFn, nil); plan == nil {
				plan = classifyM8MultiResultCall(pkgPath, aliasedFn, nil)
			}
		}
		if plan != nil {
			if nt, ok := plan.tmpType.(*types.Named); ok {
				mrInfo := &userMRInfo{
					pairType:   nt,
					pairFields: plan.pairFields,
				}
				e.userMRVars[v] = mrInfo
				return mrInfo
			}
		}
	}
	return nil
}

func (e *exporter) classifyMultiResultCall(c *ast.CallExpr) *mrPlan {
	// P4 (generic MR): generic instantiation call, e.g. hashStr[string](...).
	// The Fun is an IndexExpr/IndexListExpr. Check for instantiation MR
	// info before the Ident/SelectorExpr cases (which don't match).
	if mrInfo := e.instMRInfoForCall(c); mrInfo != nil {
		return &mrPlan{
			kind:       mrUser,
			tmpType:    mrInfo.pairType,
			pairFields: mrInfo.pairFields,
		}
	}
	// P4 (driver-harden): dot-imported fmt F-family (Fprint/Fprintf/
	// Fprintln called as bare idents). Detected structurally via the
	// func's package path.
	// P3-M9 (WS D): user-defined package-level multi-result functions
	// called as `foo()` (Ident, not SelectorExpr).
	if id, ok := c.Fun.(*ast.Ident); ok {
		// MR-closure-var: call through a variable holding a multi-result
		// closure (e.g. readModuleLine(elem) where readModuleLine :=
		// func(...) (T, error) {...}). The variable was linked to the
		// func lit's pair info at its definition; desugar via the pair
		// exactly like a named user MR function.
		if v, ok := e.info.Uses[id].(*types.Var); ok {
			if mrInfo := e.userMRInfoForVar(v); mrInfo != nil {
				return &mrPlan{
					kind:       mrUser,
					tmpType:    mrInfo.pairType,
					pairFields: mrInfo.pairFields,
				}
			}
			// General (real-x509 lane): Var aliases a multi-result Func
			// (e.g. path/filepath's lstat -> os.Lstat). Desugar as the
			// aliased Func via the table.
			if aliasedFn, ok := e.varFuncAliases[v]; ok {
				// Build a synthetic SelectorExpr call for classification.
				// The table classifiers take (pkgPath, fn, targs).
				pkgPath := ""
				if aliasedFn.Pkg() != nil {
					pkgPath = aliasedFn.Pkg().Path()
				}
				// Try the general classifiers with the aliased Func.
				// For os.Lstat, the os table handles it.
				if plan := classifyOsMultiResultCall(pkgPath, aliasedFn, nil); plan != nil {
					return plan
				}
				// Fallback: try other tables.
				if plan := classifyM7MultiResultCall(pkgPath, aliasedFn, nil); plan != nil {
					return plan
				}
				if plan := classifyM8MultiResultCall(pkgPath, aliasedFn, nil); plan != nil {
					return plan
				}
			}
			return nil
		}
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
				targs := targsFromInstances(e, id)
				if plan := classifyM7MultiResultCall(pkgPath, obj, targs); plan != nil {
					return plan
				}
				if plan := classifyOsMultiResultCall(pkgPath, obj, targs); plan != nil {
					return plan
				}
				if plan := classifyM8MultiResultCall(pkgPath, obj, targs); plan != nil {
					return plan
				}
				if plan := classifyIoMultiResultCall(pkgPath, obj, targs); plan != nil {
					return plan
				}
				if plan := classifyM9MultiResultCall(pkgPath, obj, targs); plan != nil {
					return plan
				}
				// WS G (P3-M9): disk-package multi-result (dynamic table).
				if plan := classifyDiskMultiResultCall(pkgPath, obj, targs); plan != nil {
					return plan
				}
			}
		}
		return nil
	}
	sel, ok := unwrapSelector(c.Fun)
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
			// Gate-1 #2: userMRFuncs check for package-qualified calls.
			// Synthetic packages with real Go source (e.g., strings.Cut)
			// get pair-transformed when not table-driven; their call
			// sites must desugar via userMRFuncs, not fail fast.
			if mrInfo, ok := e.userMRFuncs[fn]; ok {
				return &mrPlan{
					kind:       mrUser,
					tmpType:    mrInfo.pairType,
					pairFields: mrInfo.pairFields,
				}
			}
			// P3-M7 (wave 1): encoding/* package-level multi-result
			// members (hex.Decode, binary.Uvarint, ascii85.Decode,
			// pem.Decode, asn1.Marshal/Unmarshal).
			targs := targsFromInstances(e, sel.Sel)
			if plan := classifyM7MultiResultCall(pn.Imported().Path(), fn, targs); plan != nil {
				return plan
			}
			// P3-M8 (wave 2): the synthetic os package's multi-result
			// API (osMultiResultFuncs table above).
			if plan := classifyOsMultiResultCall(pn.Imported().Path(), fn, targs); plan != nil {
				return plan
			}
			// P3-M8 (wave 2): the crypto/* multi-result API
			// (m8MultiResultFuncs table above).
			if plan := classifyM8MultiResultCall(pn.Imported().Path(), fn, targs); plan != nil {
				return plan
			}
			// P3-M8 (WS D): the synthetic io package's multi-result API
			// (ioMultiResultFuncs table above).
			if plan := classifyIoMultiResultCall(pn.Imported().Path(), fn, targs); plan != nil {
				return plan
			}
			// P3-M9 (tier-4: net): the synthetic net packages' multi-result
			// API (m9MultiResultFuncs table above).
			if plan := classifyM9MultiResultCall(pn.Imported().Path(), fn, targs); plan != nil {
				return plan
			}
			// WS G (P3-M9): disk-package multi-result (dynamic table).
			if plan := classifyDiskMultiResultCall(pn.Imported().Path(), fn, targs); plan != nil {
				return plan
			}
			// P4 (driver-harden): the fmt F-family (Fprint/Fprintf/
			// Fprintln). Detected structurally: package "fmt", one of
			// the three names, resolving to the overlay stub with a
			// 2-result signature.
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
			case "runtime":
				// runtime.Caller(skip): the 4-result stack-inspection
				// intrinsic. Desugars (mrRuntimeStack) to the "runtimecaller"
				// wire node returning callerPair; field extraction proceeds
				// like mrM7.
				if fn.Name() == "Caller" {
					nt := named(pn.Imported(), "callerPair")
					if nt == nil {
						return nil
					}
					return &mrPlan{
						kind: mrRuntimeStack, rtStackOp: rtStackCaller,
						tmpType: nt, pairFields: []string{"pc", "file", "line", "ok"},
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
			// mrRuntimeStack: the synthetic runtime package's
			// multi-result stack-inspection methods ((*Frames).Next,
			// (*Func).FileLine). The receiver's named type must be the
			// runtime overlay's Frames/Func — a user method with the same
			// name is never desugared.
			if plan := classifyRuntimeStackMethod(f, selection); plan != nil {
				return plan
			}
			// mrSyncMap: the synthetic sync package's multi-result Map
			// methods (Load/LoadOrStore/LoadAndDelete/Swap). The
			// receiver's named type must be the sync overlay's Map.
			if plan := classifySyncMapMethod(f, selection); plan != nil {
				return plan
			}
			// mrTrivialFwd: a synthetic stub method whose body trivially
			// forwards to a function-typed parameter (single
			// `return p()`). Classified structurally — any synthetic
			// stub with that shape inlines, not just one named method.
			if plan := classifyTrivialForwarder(f); plan != nil {
				return plan
			}
			mtargs := targsFromRecv(selection.Recv())
			if plan := classifyM7MultiResultCall(f.Pkg().Path(), f, mtargs); plan != nil {
				return plan
			}
			// P3-M8 (wave 2): os methods + io/fs DirEntry.Info.
			if plan := classifyOsMultiResultCall(f.Pkg().Path(), f, mtargs); plan != nil {
				return plan
			}
			// P3-M8 (wave 2): crypto/* multi-result API.
			if plan := classifyM8MultiResultCall(f.Pkg().Path(), f, mtargs); plan != nil {
				return plan
			}
			// P3-M8 (WS D): the synthetic io package's multi-result
			// API (WriteString, ReadAtLeast, ReadFull, Copy, CopyN,
			// CopyBuffer, ReadAll, LimitedReader.Read,
			// discard.Write/WriteString).
			if plan := classifyIoMultiResultCall(f.Pkg().Path(), f, mtargs); plan != nil {
				return plan
			}
			// P3-M9 (tier-4: net): the synthetic net packages'
			// multi-result API (m9MultiResultFuncs table above) —
			// methods too (e.g. (*net.Dialer).Dial), mirroring the
			// package-qualified arm.
			if plan := classifyM9MultiResultCall(f.Pkg().Path(), f, mtargs); plan != nil {
				return plan
			}
			// WS G (P3-M9): disk-package multi-result methods (dynamic table).
			if plan := classifyDiskMultiResultCall(f.Pkg().Path(), f, mtargs); plan != nil {
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
				// WS G (P3-M9, BUG-MODPROBE-005): disk-package
				// interface methods with multi-result.
				if plan := e.classifyDiskIfaceMultiResultCall(selection); plan != nil {
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
	// P3-M9 (WS D): function field call (e.g., f.decode(rr) where decode
	// is a field of function type). The selection is FieldVal (not
	// MethodVal), and the field type is *types.Signature with N>1 results.
	// Handled generally by classifyGeneralFuncField — no per-package
	// tables (hunt-special-cases).
	if selection, ok := e.info.Selections[sel]; ok && selection.Kind() == types.FieldVal {
		if v, ok := selection.Obj().(*types.Var); ok {
			if sig, ok := v.Type().(*types.Signature); ok && sig.Results().Len() > 1 {
				if plan := e.classifyGeneralFuncField(selection, v); plan != nil {
					return plan
				}
			}
		}
	}
	return nil
}

// mrPlanForFuncValue resolves the pair-desugar plan for a multi-result
// named function used in value position (PKG-035, P3-M10): e.g.
// var F func() (int, error) = two, or
// var AcceptFunc func(int) (int, syscall.Sockaddr, error) = syscall.Accept.
//
// It mirrors the Ident branch of classifyMultiResultCall: user
// pair-transformed functions first (userMRFuncs), then the table
// classifiers (m7/os/m8/io/m9/disk). The returned plan's tmpType is the
// pair struct and pairFields the extraction order, so the value can be
// formed as the pair-returning implementation and call sites through a
// variable linked to this plan desugar exactly like direct calls.
//
// Returns nil when fn is not a multi-result function with a pair form:
// single-result functions need no plan; generic templates are excluded
// (a value needs an instantiation, WS H); fail-fast stubs have no
// __-helper and keep their honest compile-time error at the use site.
func (e *exporter) mrPlanForFuncValue(fn *types.Func) *mrPlan {
	if fn == nil {
		return nil
	}
	sig, ok := fn.Type().(*types.Signature)
	if !ok || sig.Results().Len() <= 1 {
		return nil
	}
	if sig.TypeParams() != nil && sig.TypeParams().Len() > 0 {
		return nil
	}
	if mrInfo, ok := e.userMRFuncs[fn]; ok {
		return &mrPlan{
			kind:       mrUser,
			tmpType:    mrInfo.pairType,
			pairFields: mrInfo.pairFields,
		}
	}
	if fn.Pkg() == nil {
		return nil
	}
	pkgPath := fn.Pkg().Path()
	// No call site, so no type arguments: targs is nil (generic
	// templates were excluded above).
	if plan := classifyM7MultiResultCall(pkgPath, fn, nil); plan != nil {
		return plan
	}
	if plan := classifyOsMultiResultCall(pkgPath, fn, nil); plan != nil {
		return plan
	}
	if plan := classifyM8MultiResultCall(pkgPath, fn, nil); plan != nil {
		return plan
	}
	if plan := classifyIoMultiResultCall(pkgPath, fn, nil); plan != nil {
		return plan
	}
	if plan := classifyM9MultiResultCall(pkgPath, fn, nil); plan != nil {
		return plan
	}
	if plan := classifyDiskMultiResultCall(pkgPath, fn, nil); plan != nil {
		return plan
	}
	return nil
}

// classifyTrivialForwarder recognizes a synthetic stub method whose body
// is a trivial forwarder: a single `return p()` where p names a
// function-typed parameter. Such stubs (e.g. a no-memoization cache whose
// Get just returns new()) are semantically equivalent to inlining the p()
// call at the call site. The check is structural — it inspects the
// synthetic package's AST for the shape, never the package/type/method
// name. Returns nil when f is not such a forwarder.
func classifyTrivialForwarder(f *types.Func) *mrPlan {
	if f == nil || f.Pkg() == nil {
		return nil
	}
	pkgPath := f.Pkg().Path()
	if _, ok := syntheticSources[pkgPath]; !ok {
		return nil // not a synthetic package; no AST to inspect
	}
	sig, ok := f.Type().(*types.Signature)
	if !ok || sig.Results().Len() < 2 {
		return nil // multi-result desugar only; single results need no plan
	}
	sp := getSyntheticPackage(pkgPath)
	// Match the FuncDecl by name; for methods also match the receiver's
	// named type (handles *T receivers and generic instantiations).
	var wantRecv string
	if sig.Recv() != nil {
		rt := sig.Recv().Type()
		if pt, ok := rt.(*types.Pointer); ok {
			rt = pt.Elem()
		}
		if nt, ok := rt.(*types.Named); ok && nt.Obj() != nil {
			wantRecv = nt.Obj().Name()
		}
	}
	for _, file := range sp.files {
		for _, decl := range file.Decls {
			fd, ok := decl.(*ast.FuncDecl)
			if !ok || fd.Name.Name != f.Name() {
				continue
			}
			if wantRecv != "" {
				if fd.Recv == nil || len(fd.Recv.List) == 0 {
					continue
				}
				recvExpr := fd.Recv.List[0].Type
				if se, ok := recvExpr.(*ast.StarExpr); ok {
					recvExpr = se.X
				}
				// Generic receiver Cache[K, V] is an IndexExpr/IndexListExpr.
				for {
					if ie, ok := recvExpr.(*ast.IndexExpr); ok {
						recvExpr = ie.X
						continue
					}
					if ie, ok := recvExpr.(*ast.IndexListExpr); ok {
						recvExpr = ie.X
						continue
					}
					break
				}
				id, ok := recvExpr.(*ast.Ident)
				if !ok || id.Name != wantRecv {
					continue
				}
			} else if fd.Recv != nil {
				continue
			}
			// Body must be exactly one statement: return p() — a return
			// of a zero-arg call to a bare identifier.
			if fd.Body == nil || len(fd.Body.List) != 1 {
				continue
			}
			ret, ok := fd.Body.List[0].(*ast.ReturnStmt)
			if !ok || len(ret.Results) != 1 {
				continue
			}
			call, ok := ret.Results[0].(*ast.CallExpr)
			if !ok || len(call.Args) != 0 {
				continue
			}
			funIdent, ok := call.Fun.(*ast.Ident)
			if !ok {
				continue
			}
			// funIdent must name a function-typed parameter; its index
			// tells the desugar which call-site arg to inline.
			for i := 0; i < sig.Params().Len(); i++ {
				p := sig.Params().At(i)
				if p.Name() == funIdent.Name {
					if _, ok := p.Type().Underlying().(*types.Signature); !ok {
						return nil
					}
					return &mrPlan{
						kind:        mrTrivialFwd,
						pairFields:  []string{"v", "err"}, // arity placeholder
						fwdParamIdx: i,
					}
				}
			}
			return nil
		}
	}
	return nil
}

// classifyGeneralFuncField handles func-typed struct field calls for ANY
// struct, not just nistCurve (fix-funcfield-mr, Gate-1). The field must
// have a *types.Signature type with N>1 results. A pair type is
// synthesized on demand with fields f0, f1, ... in result order.
//
// Design: the field holds a function value. For the call to work, the
// stored value must be pair-returning. This holds because:
// - Synthetic packages declare pair-returning field types explicitly
//   (e.g., nistCurve.generate)
// - Disk/user packages: the field is assigned a function that has been
//   pair-transformed (via userMRFuncs or the disk MR table), so the
//   value is pair-returning even though the declared field type is
//   multi-result.
//
// The desugar emits a regular "call" node (not "funcfieldcall") with the
// pair type — the backend already handles selector-fun calls via
// FuncValueCallNode.
func (e *exporter) classifyGeneralFuncField(selection *types.Selection, v *types.Var) *mrPlan {
	sig, ok := v.Type().(*types.Signature)
	if !ok {
		return nil
	}
	n := sig.Results().Len()
	if n <= 1 {
		return nil
	}
	// Get struct and field names for the pair type name.
	recv := selection.Recv()
	if recv == nil {
		return nil
	}
	rt := recv
	if pt, ok := rt.(*types.Pointer); ok {
		rt = pt.Elem()
	}
	nt, ok := rt.(*types.Named)
	if !ok || nt.Obj() == nil {
		return nil
	}
	structName := nt.Obj().Name()
	fieldName := v.Name()

	// Synthesize the pair type.
	pairNt, fieldNames := e.synthesizeFuncFieldPairType(sig, structName, fieldName)
	if pairNt == nil {
		return nil
	}
	return &mrPlan{
		kind:       mrFuncField,
		tmpType:    pairNt,
		pairFields: fieldNames,
	}
}

// synthesizeFuncFieldPairType creates a named struct type for a
// func-typed field's multi-result signature (fix-funcfield-mr). Fields
// are f0, f1, ... with the result types in order. The name is unique per
// (struct, field): __wsd_pair_<struct>_<field>.
func (e *exporter) synthesizeFuncFieldPairType(sig *types.Signature, structName, fieldName string) (*types.Named, []string) {
	n := sig.Results().Len()
	fields := make([]*types.Var, n)
	fieldNames := make([]string, n)
	for i := 0; i < n; i++ {
		fname := fmt.Sprintf("f%d", i)
		fieldNames[i] = fname
		rt := sig.Results().At(i).Type()
		fields[i] = types.NewField(token.NoPos, e.pkg, fname, rt, false)
	}
	st := types.NewStruct(fields, nil)
	// Unique name per (struct, field).
	base := fmt.Sprintf("__wsd_pair_%s_%s", structName, fieldName)
	name := base
	for i := 1; ; i++ {
		if e.pkg.Scope().Lookup(name) == nil {
			break
		}
		name = fmt.Sprintf("%s_%d", base, i)
	}
	tname := types.NewTypeName(token.NoPos, e.pkg, name, nil)
	named := types.NewNamed(tname, st, nil)
	e.pkg.Scope().Insert(tname)
	return named, fieldNames
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
func classifyM7MultiResultCall(pkgPath string, fn *types.Func, targs []types.Type) *mrPlan {
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
	// such receiver-less selections against the table entries marked
	// ifaceFallback (their recvType names the concrete type). Table-driven,
	// not package-name-keyed (P2-F3, review-n1).
	if recvName == "" {
		if plan := classifyM7MultiResultCallIfaceFallback(pkgPath, fn, targs); plan != nil {
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
		helperPkg := mf.pkgPath
		if override, ok := m7HelperPkgOverride[mf.pkgPath]; ok {
			helperPkg = override
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
		nt = instantiatePairType(nt, targs)
		return &mrPlan{
			kind: mrM7, helperPkg: helperPkg,
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
// classifyRuntimeStackMethod classifies the synthetic runtime package's
// multi-result stack-inspection methods: (*Frames).Next desugars to the
// "runtimeframesnext" wire node returning framesNextPair, and
// (*Func).FileLine(pc) to the "runtimefuncfileline" wire node returning
// funcFileLinePair. The pair types are looked up in the method's own
// package (the runtime overlay), so the plan never depends on overlay
// package identity.
func classifyRuntimeStackMethod(f *types.Func, selection *types.Selection) *mrPlan {
	if f.Pkg() == nil || f.Pkg().Path() != "runtime" {
		return nil
	}
	recv := selection.Recv()
	if pt, ok := recv.(*types.Pointer); ok {
		recv = pt.Elem()
	}
	nt, ok := recv.(*types.Named)
	if !ok || nt.Obj() == nil || nt.Obj().Pkg() == nil || nt.Obj().Pkg().Path() != "runtime" {
		return nil
	}
	named := func(name string) *types.Named {
		obj := f.Pkg().Scope().Lookup(name)
		if obj == nil {
			return nil
		}
		tn, ok := obj.(*types.TypeName)
		if !ok {
			return nil
		}
		n, _ := tn.Type().(*types.Named)
		return n
	}
	switch {
	case nt.Obj().Name() == "Frames" && f.Name() == "Next":
		pair := named("framesNextPair")
		if pair == nil {
			return nil
		}
		return &mrPlan{
			kind: mrRuntimeStack, rtStackOp: rtStackFramesNext,
			tmpType: pair, pairFields: []string{"frame", "more"},
			prependRecv: true,
		}
	case nt.Obj().Name() == "Func" && f.Name() == "FileLine":
		pair := named("funcFileLinePair")
		if pair == nil {
			return nil
		}
		return &mrPlan{
			kind: mrRuntimeStack, rtStackOp: rtStackFuncFileLine,
			tmpType: pair, pairFields: []string{"file", "line"},
			prependRecv: true,
		}
	}
	return nil
}

// classifySyncMapMethod recognizes the synthetic sync package's
// multi-result Map methods (Load, LoadOrStore, LoadAndDelete, Swap —
// the (any, bool) results). The receiver's named type must be the sync
// overlay's Map — a user method with the same name is never desugared
// (the mrRuntimeStack precedent). Returns an mrSyncMap plan carrying
// the mapAnyBoolPair type; desugarSyncMap builds the "syncop" wire
// node directly (no __-helper: the types-built overlay has no real-Go
// source).
func classifySyncMapMethod(f *types.Func, selection *types.Selection) *mrPlan {
	if f.Pkg() == nil || f.Pkg().Path() != "sync" {
		return nil
	}
	recv := selection.Recv()
	if pt, ok := recv.(*types.Pointer); ok {
		recv = pt.Elem()
	}
	nt, ok := recv.(*types.Named)
	if !ok || nt.Obj() == nil || nt.Obj().Pkg() == nil || nt.Obj().Pkg().Path() != "sync" {
		return nil
	}
	if nt.Obj().Name() != "Map" {
		return nil
	}
	var pair *types.Named
	if obj := f.Pkg().Scope().Lookup("mapAnyBoolPair"); obj != nil {
		if tn, ok := obj.(*types.TypeName); ok {
			pair, _ = tn.Type().(*types.Named)
		}
	}
	if pair == nil {
		return nil
	}
	switch f.Name() {
	case "Load", "LoadOrStore", "LoadAndDelete", "Swap":
		return &mrPlan{
			kind: mrSyncMap, syncMapOp: f.Name(),
			tmpType: pair, pairFields: []string{"v", "ok"},
			prependRecv: true,
		}
	}
	return nil
}

func classifyM9MultiResultCall(pkgPath string, fn *types.Func, targs []types.Type) *mrPlan {
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
		nt = instantiatePairType(nt, targs)
		return &mrPlan{
			kind: mrM7, helperPkg: helperPkg,
			helperName: mf.helper,
			tmpType:    nt, pairFields: mf.pairFields,
			prependRecv: mf.recvType != "",
		}
	}
	return nil
}

func classifyOsMultiResultCall(pkgPath string, fn *types.Func, targs []types.Type) *mrPlan {
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
		nt = instantiatePairType(nt, targs)
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
func classifyM8MultiResultCall(pkgPath string, fn *types.Func, targs []types.Type) *mrPlan {
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
		nt = instantiatePairType(nt, targs)
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
func classifyIoMultiResultCall(pkgPath string, fn *types.Func, targs []types.Type) *mrPlan {
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
		helperPkg := mf.pkgPath
		if override, ok := m7HelperPkgOverride[mf.pkgPath]; ok {
			helperPkg = override
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
		nt = instantiatePairType(nt, targs)
		return &mrPlan{
			kind: mrM7, helperPkg: helperPkg,
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
		// The pair type may live in an override package (m7HelperPkgOverride):
		// reflect retired from synthetic to the disk importer, but its M7
		// __-helpers and pair types still live in the synthetic reflect_m7
		// package. Same pattern as classifyM7MultiResultCall.
		helperPkg := m.pkgPath
		if override, ok := m7HelperPkgOverride[m.pkgPath]; ok {
			helperPkg = override
		}
		sp := getSyntheticPackage(helperPkg)
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
			kind: mrIfaceMethod, helperPkg: helperPkg,
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
		// The pair type may live in an override package (m7HelperPkgOverride):
		// reflect retired from synthetic to the disk importer, but its M7
		// __-helpers and pair types still live in the synthetic reflect_m7
		// package. Same pattern as classifyM7MultiResultCall.
		helperPkg := m.pkgPath
		if override, ok := m7HelperPkgOverride[m.pkgPath]; ok {
			helperPkg = override
		}
		sp := getSyntheticPackage(helperPkg)
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
			kind: mrIfaceMethod, helperPkg: helperPkg,
			tmpType: nt, pairFields: m.pairFields,
		}
	}
	return nil
}

// classifyUserIfaceMultiResultCall is the general (non-whitelist,
// non-disk) classifier for multi-result interface method calls (P4 WS D).
// It handles user-defined interfaces (and any other interface not covered
// by m8IfaceMultiResultMethods or the disk MR table) by synthesizing a
// pair type on first use, cached in userIfaceMRPairs.
//
// The pair uses f0, f1, ... field names (via synthesizePairTypeForSig),
// matching the concrete helper's pair synthesized by registerUserMRFunc.
// The itab dispatches the interface method to the concrete type's
// pair-returning __-helper (wired via methodTableEntries when the
// concrete method is in userMRFuncs); field extraction proceeds by name,
// exactly like the concrete Appendix M desugar.
//
// Returns nil when the selection is not a multi-result interface method.
func (e *exporter) classifyUserIfaceMultiResultCall(selection *types.Selection) *mrPlan {
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
	// General (real-x509 lane): substitute type parameters for generic
	// contexts (e.g. x509's interface{ query(V) (T, bool) } inside
	// constraints[T, V]). Unbound type params keep the scope error.
	csig, ok := e.substType(sig).(*types.Signature)
	if !ok || mentionsTypeParam(csig) {
		return nil
	}
	sig = csig
	pkgPath := ""
	if f.Pkg() != nil {
		pkgPath = f.Pkg().Path()
	}
	// The pair is fully determined by the (substituted) signature; key
	// by it so different instantiations get different pairs. The
	// interface may be anonymous (no named receiver), so the key does
	// not use an interface name.
	key := pkgPath + "." + f.Name() + "." + types.TypeString(sig, nil)
	if mrInfo, ok := e.userIfaceMRPairs[key]; ok {
		return &mrPlan{
			kind: mrIfaceMethod, helperPkg: pkgPath,
			tmpType: mrInfo.pairType, pairFields: mrInfo.pairFields,
		}
	}
	// Field naming must match the concrete helper's pair. Disk pairs
	// use f1..fN (uniform across all disk pair types); user/synthetic
	// pairs use f0..fN-1. The method's package proxies the implementers'
	// world (the interface and its implementers are co-located in
	// practice).
	oneBased := false
	if f.Pkg() != nil {
		if _, err := getDiskPackage(f.Pkg().Path()); err == nil {
			oneBased = true
		}
	}
	nameHint := fmt.Sprintf("__wsd_ifacepair_%s", f.Name())
	pairType, fieldNames, err := e.synthesizeIfacePairType(sig, nameHint, oneBased)
	if err != nil {
		return nil
	}
	mrInfo := &userMRInfo{
		pairType:   pairType,
		pairFields: fieldNames,
		origSig:    sig,
	}
	e.userIfaceMRPairs[key] = mrInfo
	return &mrPlan{
		kind: mrIfaceMethod, helperPkg: pkgPath,
		tmpType: pairType, pairFields: fieldNames,
	}
}

// synthesizeIfacePairType creates a named struct pair type for an
// interface method's (substituted) signature, with field naming chosen
// by oneBased (f1..fN for disk-world implementers, f0..fN-1 otherwise).
// The name is unique per (method, signature); collisions get a numeric
// suffix.
func (e *exporter) synthesizeIfacePairType(sig *types.Signature, nameHint string, oneBased bool) (*types.Named, []string, error) {
	if sig == nil || sig.Results().Len() <= 1 {
		return nil, nil, fmt.Errorf("gofrontend: synthesizeIfacePairType needs a multi-result signature")
	}
	n := sig.Results().Len()
	fields := make([]*types.Var, n)
	fieldNames := make([]string, n)
	for i := 0; i < n; i++ {
		fname := fmt.Sprintf("f%d", i)
		if oneBased {
			fname = fmt.Sprintf("f%d", i+1)
		}
		fieldNames[i] = fname
		rt := sig.Results().At(i).Type()
		fields[i] = types.NewField(token.NoPos, e.pkg, fname, rt, false)
	}
	st := types.NewStruct(fields, nil)
	base := nameHint
	name := base
	for i := 1; ; i++ {
		if e.pkg.Scope().Lookup(name) == nil {
			break
		}
		name = fmt.Sprintf("%s_%d", base, i)
	}
	tname := types.NewTypeName(token.NoPos, e.pkg, name, nil)
	named := types.NewNamed(tname, st, nil)
	e.pkg.Scope().Insert(tname)
	return named, fieldNames, nil
}

// multiResultHelperFor maps a multi-result (package path, method name,
// receiver type name) to its pair-returning __-helper's package and name,
// unioned over the m7/os/m8/io/m9 desugar tables. It is the exporter's view
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
	// P3-M9 (tier-4: net): the net table. Without this, net multi-result
	// methods (pipeConn/TCPConn/UDPConn Read/Write, TCPListener.Accept)
	// never wire into the itab — interface dispatch fails fast with
	// "method missing" (wave3 net/00_pipe red, 2026-10-12).
	for _, mf := range m9MultiResultFuncs {
		if mf.pkgPath == pkgPath && mf.name == name && mf.recvType == recvType {
			hp := mf.helperPkg
			if hp == "" {
				hp = mf.pkgPath
			}
			return hp, mf.helper, true
		}
	}
	// WS G (P3-M9, BUG-MODPROBE-005): disk-package multi-result methods
	// (dynamic diskMRSpecs table). This wires the itab for interface
	// dispatch of disk interface methods: the concrete type's
	// pair-returning __-helper becomes the itab target.
	if spec := lookupDiskMR(pkgPath, name, recvType); spec != nil && spec.helper != "" {
		return spec.pkgPath, spec.helper, true
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
	var fi *funcDeclInfo
	// P3-M9: try synthFuncIDs first (string-keyed). The *types.Func from
	// Scope().Lookup may differ from the one in funcByObj when the
	// synthetic package is type-checked in isolation.
	if sfi, sok := e.synthFuncIDs[helperPkg+"."+helperName]; sok && sfi != nil {
		fi = sfi
	} else {
		var helperFn *types.Func
		if dsp, derr := getDiskPackage(helperPkg); derr == nil {
			// WS G (P3-M9, BUG-MODPROBE-005): disk-package
			// pair-returning __-helper.
			helperObj := dsp.pkg.Scope().Lookup(helperName)
			var ok bool
			helperFn, ok = helperObj.(*types.Func)
			if !ok {
				return nil, false
			}
		} else {
			sp := getSyntheticPackage(helperPkg)
			if sp == nil {
				return nil, false
			}
			helperObj := sp.pkg.Scope().Lookup(helperName)
			var ok bool
			helperFn, ok = helperObj.(*types.Func)
			if !ok {
				return nil, false
			}
		}
		var ok bool
		fi, ok = e.funcByObj[helperFn]
		if !ok || fi == nil {
			return nil, false
		}
	}
	// Positional shape check: the helper's single result is the pair
	// struct; its field types must equal the method's result types.
	// P3-M9 (instantiation method tables): the helper may be generic
	// with a generic pair type (e.g.
	// __mrnistCurve_pointFromAffinePair[Point]). Bind its type
	// parameters from the method receiver's type arguments before
	// comparing, so instantiated methods resolve to their generic
	// helper. The binding is positional, mirroring the table's contract
	// (each helper is written for its receiver); a genuine mismatch
	// still fails closed below. Non-generic helpers are unaffected
	// (no type params to bind).
	hsig := fi.sig
	if hsig == nil || hsig.Results().Len() != 1 {
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
	savedSubst := e.subst
	if hps := fi.sig.TypeParams(); hps.Len() > 0 {
		if rtargs := nt.TypeArgs(); rtargs != nil && rtargs.Len() == hps.Len() {
			bind := make(map[*types.TypeParam]types.Type, hps.Len())
			for i := 0; i < hps.Len(); i++ {
				bind[hps.At(i)] = rtargs.At(i)
			}
			e.subst = &typeSubst{parent: savedSubst, bind: bind}
		}
	}
	match := true
	for i := 0; i < pst.NumFields(); i++ {
		if !types.Identical(e.substType(pst.Field(i).Type()), sig.Results().At(i).Type()) {
			match = false
			break
		}
	}
	e.subst = savedSubst
	if !match {
		return nil, false
	}
	return fi, true
}

// classifyM7MultiResultCallIfaceFallback retries a receiver-less
// (interface-selected) method against the m7 table entries marked
// ifaceFallback, whose recvType names the concrete type (P3-M8 wave 2:
// reflect.Type's MethodByName/FieldByName/FieldByNameFunc selected via
// the Type interface desugar through the rtype entries). Table-driven via
// the entry flag, not package-name-keyed (P2-F3, review-n1).
func classifyM7MultiResultCallIfaceFallback(pkgPath string, fn *types.Func, targs []types.Type) *mrPlan {
	sig, ok := fn.Type().(*types.Signature)
	if !ok {
		return nil
	}
	for _, mf := range m7MultiResultFuncs {
		if mf.pkgPath != pkgPath || mf.name != fn.Name() || !mf.ifaceFallback {
			continue
		}
		if sig.Results().Len() != len(mf.pairFields) {
			return nil
		}
		helperPkg := mf.pkgPath
		if override, ok := m7HelperPkgOverride[mf.pkgPath]; ok {
			helperPkg = override
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
		nt = instantiatePairType(nt, targs)
		return &mrPlan{
			kind: mrM7, helperPkg: helperPkg,
			helperName: mf.helper,
			tmpType:    nt, pairFields: mf.pairFields,
			prependRecv: mf.recvType != "",
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
	if plan.kind == mrTrivialFwd {
		// A trivial-forwarder stub inlines its forwarded parameter call:
		// v, err := x.M(a0, a1, ...) becomes v, err := aN() where N is
		// the forwarded parameter's index (from classifyTrivialForwarder).
		if plan.fwdParamIdx < 0 || plan.fwdParamIdx >= len(c.Args) {
			return nil, fmt.Errorf("gofrontend: trivial-forwarder arg index %d out of range (%d args)", plan.fwdParamIdx, len(c.Args))
		}
		// Build fwdFn() call: Fun is Args[fwdParamIdx], no args.
		newCall := &ast.CallExpr{
			Fun:    c.Args[plan.fwdParamIdx],
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
	// mrRuntimeStack: the synthetic runtime stack-inspection intrinsics
	// build their wire node directly (no __-helper exists).
	if plan.kind == mrRuntimeStack {
		return e.desugarRuntimeStack(s, c, plan, define)
	}
	// mrSyncMap: the synthetic sync.Map multi-result methods build the
	// "syncop" wire node directly (no __-helper exists).
	if plan.kind == mrSyncMap {
		return e.desugarSyncMap(s, c, plan, define)
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

// desugarRuntimeStack desugars the synthetic runtime package's
// multi-result stack-inspection calls: runtime.Caller(skip),
// (*Frames).Next(), (*Func).FileLine(pc). Unlike every other Appendix M
// kind there is no __-helper — the synthetic runtime overlay has no
// real-Go source — so the intrinsic wire node ("runtimecaller",
// "runtimeframesnext", "runtimefuncfileline") is built directly with the
// pair-typed result; the __p5mr_N temp declaration and field extraction
// then proceed exactly like mrM7 via desugarExtract.
func (e *exporter) desugarRuntimeStack(s *ast.AssignStmt, c *ast.CallExpr, plan *mrPlan, define bool) (*Node, error) {
	var kind string
	var args []*Node
	switch plan.rtStackOp {
	case rtStackCaller:
		// runtime.Caller(skip): package-level call, one argument.
		if len(c.Args) != 1 {
			return nil, fmt.Errorf("gofrontend: runtime.Caller takes 1 argument")
		}
		kind = "runtimecaller"
		a, err := e.exportExpr(c.Args[0])
		if err != nil {
			return nil, err
		}
		args = []*Node{a}
	case rtStackFramesNext, rtStackFuncFileLine:
		// Method calls: the receiver is the first wire-node argument
		// (prependRecv, mirroring multiResultHelperArgs).
		sel, ok := unwrapSelector(c.Fun)
		if !ok {
			return nil, fmt.Errorf("gofrontend: runtime stack method call has non-selector fun %T", c.Fun)
		}
		recv, err := e.exportExpr(sel.X)
		if err != nil {
			return nil, err
		}
		args = []*Node{recv}
		if plan.rtStackOp == rtStackFuncFileLine {
			if len(c.Args) != 1 {
				return nil, fmt.Errorf("gofrontend: runtime.Func.FileLine takes 1 argument")
			}
			kind = "runtimefuncfileline"
			a, err := e.exportExpr(c.Args[0])
			if err != nil {
				return nil, err
			}
			args = append(args, a)
		} else {
			kind = "runtimeframesnext"
			if len(c.Args) != 0 {
				return nil, fmt.Errorf("gofrontend: runtime.Frames.Next takes no arguments")
			}
		}
	default:
		return nil, fmt.Errorf("gofrontend: unknown runtime stack op %d", plan.rtStackOp)
	}
	tmpTid, err := e.typeID(plan.tmpType)
	if err != nil {
		return nil, err
	}
	fields := map[string]any{"type": tmpTid}
	switch plan.rtStackOp {
	case rtStackCaller:
		fields["skip"] = args[0]
	case rtStackFramesNext:
		fields["f"] = args[0]
	case rtStackFuncFileLine:
		fields["f"] = args[0]
		fields["pc"] = args[1]
	}
	call := e.node(kind, c, fields)
	e.chanSeq++
	tmpName := fmt.Sprintf("__p5mr_%d", e.chanSeq)
	v := types.NewVar(token.NoPos, nil, tmpName, plan.tmpType)
	tmpID, err := e.defVar(v, false)
	if err != nil {
		return nil, err
	}
	return e.desugarExtract(s, c, plan, define, tmpName, tmpID, tmpTid, call, nil)
}

// desugarSyncMap desugars an N-target assignment of a sync.Map
// multi-result method (v, ok := m.Load(k)) — the mrSyncMap plan. Like
// desugarRuntimeStack there is no __-helper: the "syncop" wire node is
// built directly with the mapAnyBoolPair result type, then the shared
// pair-temp + field extraction (desugarExtract) splits it across the
// targets. Arity is checked per op (fail-fast, never silent).
func (e *exporter) desugarSyncMap(s *ast.AssignStmt, c *ast.CallExpr, plan *mrPlan, define bool) (*Node, error) {
	sel, ok := unwrapSelector(c.Fun)
	if !ok {
		return nil, fmt.Errorf("gofrontend: sync.Map method call has non-selector fun %T", c.Fun)
	}
	recv, err := e.exportExpr(sel.X)
	if err != nil {
		return nil, err
	}
	args, err := e.exportCallArgs(c)
	if err != nil {
		return nil, err
	}
	wantArgs := map[string]int{
		"Load": 1, "LoadOrStore": 2, "LoadAndDelete": 1, "Swap": 2,
	}[plan.syncMapOp]
	if wantArgs == 0 && plan.syncMapOp != "" {
		return nil, fmt.Errorf("gofrontend: unknown sync.Map op %q", plan.syncMapOp)
	}
	if len(args) != wantArgs {
		return nil, fmt.Errorf("gofrontend: sync.Map.%s takes %d arguments, got %d",
			plan.syncMapOp, wantArgs, len(args))
	}
	pairTid, err := e.typeID(plan.tmpType)
	if err != nil {
		return nil, err
	}
	call := e.node("syncop", c, map[string]any{
		"op": plan.syncMapOp, "synctyp": "Map", "recv": recv, "args": args, "type": pairTid,
	})
	e.chanSeq++
	tmpName := fmt.Sprintf("__p5mr_%d", e.chanSeq)
	v := types.NewVar(token.NoPos, nil, tmpName, plan.tmpType)
	tmpID, err := e.defVar(v, false)
	if err != nil {
		return nil, err
	}
	return e.desugarExtract(s, c, plan, define, tmpName, tmpID, pairTid, call, nil)
}

// desugarFuncFieldCall desugars a function field call (e.g.,
// c.generate(r)) where the field is of function type with 2 results.
// Generates a funcfieldcall node that calls the field's function value
// and packs the 2 results into the pair struct.
func (e *exporter) desugarFuncFieldCall(s *ast.AssignStmt, c *ast.CallExpr, plan *mrPlan, define bool) (*Node, error) {
	if _, ok := c.Fun.(*ast.SelectorExpr); !ok {
		return nil, fmt.Errorf("gofrontend: funcfieldcall expects SelectorExpr, got %T", c.Fun)
	}
	// Export the selector as the fun expression (e.g., f.decode).
	// The backend handles call-with-selector-fun via FuncValueCallNode
	// (see NodeFactory.java: the "selector" branch builds the field
	// access and calls the function value).
	funNode, err := e.exportExpr(c.Fun)
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
	// Emit a regular "call" node with the pair type. The backend's
	// FuncValueCallNode will call the function value (which must be
	// pair-returning — see classifyGeneralFuncField) and the result
	// is the pair struct.
	//
	// fix-funcfield-mr: previously this emitted a "funcfieldcall" node,
	// but the backend never implemented it (dead code). The regular
	// "call" path works because the backend already handles selector
	// fun expressions.
	callNode := e.node("call", c, map[string]any{
		"fun":  funNode,
		"args": args,
		"type": pairTid,
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
	var sp *syntheticPackage
	if plan.isDisk {
		// WS G (P3-M9): disk-built package (transformDiskMultiResult).
		var err error
		sp, err = getDiskPackage(plan.helperPkg)
		if err != nil {
			return nil, err
		}
	} else {
		sp = getSyntheticPackage(plan.helperPkg)
	}
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
				if innerPlan.kind == mrUser || innerPlan.kind == mrIfaceMethod {
					if innerPlan.kind == mrUser {
						innerCallNode, err = e.exportCall(inner)
					} else {
						// General interface MR: build the ifacemethod
						// dispatch node (yields the pair, like the
						// assignment path in desugarIfaceMultiResult).
						var sel *ast.SelectorExpr
						var ok bool
						if sel, ok = inner.Fun.(*ast.SelectorExpr); !ok {
							return nil, nil, fmt.Errorf("gofrontend: iface-multi-result call has non-selector fun %T", inner.Fun)
						}
						selection, ok := e.info.Selections[sel]
						if !ok {
							return nil, nil, fmt.Errorf("gofrontend: no selection for iface-multi-result call")
						}
						innerCallNode, err = e.exportIfaceMethodCall(inner, sel, selection)
					}
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

// hoistMRCallArg hoists a multi-result call in argument position (f(g()))
// to a temp var. Per Go spec, g's results spread as f's arguments (g() must
// be the sole argument; the type checker guarantees this for valid Go).
// Returns the unpacked pair-field selector args and the pre-statements
// (temp vardecl) that must execute before the outer call.
// BUG-MODPROBE-010: used by exportCallArgs for the general call path;
// mirrors the nested handling in multiResultHelperArgs.
func (e *exporter) hoistMRCallArg(inner *ast.CallExpr, plan *mrPlan) ([]*Node, []*Node, error) {
	var preStmts []*Node
	var callNode *Node
	var tmpName string
	var tmpID, tmpTid int
	if plan.kind == mrUser || plan.kind == mrIfaceMethod {
		// User-defined MR: export the call normally (pair-valued).
		// Its args are hoisted recursively via exportCallArgs.
		// Interface MR: build the ifacemethod dispatch node (yields the
		// pair, like the assignment path in desugarIfaceMultiResult).
		var err error
		if plan.kind == mrUser {
			callNode, err = e.exportCall(inner)
		} else {
			var sel *ast.SelectorExpr
			var ok bool
			if sel, ok = inner.Fun.(*ast.SelectorExpr); !ok {
				return nil, nil, fmt.Errorf("gofrontend: iface-multi-result call has non-selector fun %T", inner.Fun)
			}
			selection, ok := e.info.Selections[sel]
			if !ok {
				return nil, nil, fmt.Errorf("gofrontend: no selection for iface-multi-result call")
			}
			callNode, err = e.exportIfaceMethodCall(inner, sel, selection)
		}
		if err != nil {
			return nil, nil, err
		}
		resTid, err := e.typeID(plan.tmpType)
		if err != nil {
			return nil, nil, err
		}
		e.chanSeq++
		tmpName = fmt.Sprintf("__p5mr_%d", e.chanSeq)
		v := types.NewVar(token.NoPos, nil, tmpName, plan.tmpType)
		tmpID, err = e.defVar(v, false)
		if err != nil {
			return nil, nil, err
		}
		tmpTid = resTid
	} else if plan.kind == mrFuncField {
		// Function field call (e.g., c.generate(r)): build the
		// funcfieldcall node directly (mirrors desugarFuncFieldCall).
		sel, ok := inner.Fun.(*ast.SelectorExpr)
		if !ok {
			return nil, nil, fmt.Errorf("gofrontend: funcfieldcall expects SelectorExpr, got %T", inner.Fun)
		}
		recvNode, err := e.exportExpr(sel.X)
		if err != nil {
			return nil, nil, err
		}
		args, err := e.exportCallArgs(inner)
		if err != nil {
			return nil, nil, err
		}
		pairTid, err := e.typeID(plan.tmpType)
		if err != nil {
			return nil, nil, err
		}
		callNode = e.node("funcfieldcall", inner, map[string]any{
			"recv":       recvNode,
			"field":      sel.Sel.Name,
			"args":       args,
			"pairType":   pairTid,
			"pairFields": plan.pairFields,
		})
		e.chanSeq++
		tmpName = fmt.Sprintf("__p5mr_%d", e.chanSeq)
		v := types.NewVar(token.NoPos, nil, tmpName, plan.tmpType)
		tmpID, err = e.defVar(v, false)
		if err != nil {
			return nil, nil, err
		}
		tmpTid = pairTid
	} else if plan.kind == mrTrivialFwd {
		// Trivial-forwarder stub in argument position: f(x.M(...))
		// becomes f(fwdFn()) where fwdFn is Args[fwdParamIdx].
		// fwdFn() is single-valued, so no hoisting needed.
		if plan.fwdParamIdx < 0 || plan.fwdParamIdx >= len(inner.Args) {
			return nil, nil, fmt.Errorf("gofrontend: trivial-forwarder arg index %d out of range (%d args)", plan.fwdParamIdx, len(inner.Args))
		}
		newCall := &ast.CallExpr{
			Fun:    inner.Args[plan.fwdParamIdx],
			Lparen: inner.Lparen,
			Args:   []ast.Expr{},
			Rparen: inner.Rparen,
		}
		x, err := e.exportExpr(newCall)
		if err != nil {
			return nil, nil, err
		}
		return []*Node{x}, nil, nil
	} else {
		// Synthetic/disk MR: export args (with nested hoisting), then
		// build the __-helper call.
		innerArgs, innerPre, err := e.multiResultHelperArgs(inner, plan)
		if err != nil {
			return nil, nil, err
		}
		preStmts = append(preStmts, innerPre...)
		helperFn, err := e.multiResultHelper(plan)
		if err != nil {
			return nil, nil, err
		}
		callNode, tmpName, tmpID, tmpTid, err = e.synthHelperCall(inner, helperFn, plan.tmpType, innerArgs)
		if err != nil {
			return nil, nil, err
		}
	}
	// Emit the temp vardecl as a pre-statement.
	preStmts = append(preStmts, e.node("vardecl", inner, map[string]any{
		"names": []string{tmpName}, "objs": []int{tmpID},
		"define": true, "type": nil, "values": []*Node{callNode},
	}))
	// Spread the pair fields as separate arguments.
	var args []*Node
	for i, field := range plan.pairFields {
		sel, err := e.synthPairField(inner, tmpName, tmpID, tmpTid, plan.tmpType, field, i)
		if err != nil {
			return nil, nil, err
		}
		args = append(args, sel)
	}
	return args, preStmts, nil
}

// isTableDrivenMR reports whether fi is a multi-result function served by
// a static table (Appendix M) or the disk MR table, rather than the user
// pair-transform. Synthetic packages with real Go source (e.g.,
// regexp/syntax) and no table coverage need the user mechanism for their
// multi-result functions; blanket-excluding all synthetic packages breaks
// them (Gate-1 #2: image/*, regexp, index/suffixarray).
func isTableDrivenMR(fi *funcDeclInfo) bool {
	if fi == nil || fi.obj == nil || fi.obj.Pkg() == nil {
		return true // conservative: skip if we can't determine
	}
	pkgPath := fi.obj.Pkg().Path()
	name := fi.obj.Name()
	recvType := ""
	if fi.recv != nil {
		rt := fi.recv.Type()
		if pt, ok := rt.(*types.Pointer); ok {
			rt = pt.Elem()
		}
		if nt, ok := rt.(*types.Named); ok && nt.Obj() != nil {
			recvType = nt.Obj().Name()
		}
	}
	if hasStaticMREntry(pkgPath, name, recvType) {
		return true
	}
	if lookupDiskMR(pkgPath, name, recvType) != nil {
		return true
	}
	return false
}

// registerUserMRPairs pre-registers pair types for every user-defined
// multi-result function in funcs (BUG-MODPROBE-010). It runs before
// package-level variable initializers are exported: initializers like
// `var x = Must(Parse(...))` hoist the inner multi-result call via
// classifyMultiResultCall, which consults userMRFuncs — previously
// populated only inside exportFuncDecl, which runs after exportPackageVars,
// so the call failed fast with "multi-valued call result is not supported".
// Registration is idempotent: exportFuncDecl reuses the pre-registered
// entry for the same *types.Func.
func (e *exporter) registerUserMRPairs(funcs []*funcDeclInfo) error {
	for _, fi := range funcs {
		if _, err := e.registerUserMRFunc(fi); err != nil {
			return err
		}
	}
	return nil
}

// registerUserMRFunc synthesizes the pair type for one user-defined
// multi-result function and records it in e.userMRFuncs. It is the single
// decision point for "does this func get a userMRFuncs entry", shared by
// registerUserMRPairs (pre-pass) and exportFuncDecl, so the two can never
// disagree. Returns (nil, nil) when fi must not be registered (synthetic
// package func, generic template, at most one result, bodyless linkname
// stub, or WS G disk-package multi-result stub — all cases exportFuncDecl
// skips or rejects before reaching registration).
func (e *exporter) registerUserMRFunc(fi *funcDeclInfo) (*userMRInfo, error) {
	if fi == nil || fi.isTemplate {
		return nil, nil
	}
	// Synthetic packages: skip only if table-driven (static Appendix-M
	// tables or disk MR table). Synthetic packages with real Go source
	// and no table coverage (e.g., regexp/syntax) need the user
	// pair-transform for their multi-result functions (Gate-1 #2).
	if fi.synthPkg != nil && isTableDrivenMR(fi) {
		return nil, nil
	}
	if fi.sig == nil || fi.sig.Results().Len() <= 1 {
		return nil, nil
	}
	if fi.fd != nil && fi.fd.Body == nil {
		// Bodyless (//go:linkname): exportFuncDecl routes these to
		// exportLinknameDecl before reaching registration.
		return nil, nil
	}
	if fi.obj != nil && fi.obj.Pkg() != nil {
		// WS G: disk-package multi-result stubs are skipped by
		// exportFuncDecl (the desugar rewrites all calls to the
		// __-helper); keep the pre-pass consistent so call sites still
		// classify against the disk table, not a user pair.
		name := fi.obj.Name()
		if !strings.HasPrefix(name, "__") {
			recvType := ""
			if fi.recv != nil {
				rt := fi.recv.Type()
				if pt, ok := rt.(*types.Pointer); ok {
					rt = pt.Elem()
				}
				if nt, ok := rt.(*types.Named); ok && nt.Obj() != nil {
					recvType = nt.Obj().Name()
				}
			}
			if lookupDiskMR(fi.obj.Pkg().Path(), name, recvType) != nil {
				return nil, nil
			}
		}
	}
	if mrInfo, ok := e.userMRFuncs[fi.obj]; ok {
		return mrInfo, nil
	}
	pairType, fieldNames, err := e.synthesizeUserPairType(fi)
	if err != nil {
		return nil, err
	}
	mrInfo := &userMRInfo{
		pairType:   pairType,
		pairFields: fieldNames,
		origSig:    fi.sig,
	}
	e.userMRFuncs[fi.obj] = mrInfo
	return mrInfo, nil
}

// packageHoistTemp converts one mrHoistPre vardecl node (emitted by
// hoistMRCallArg and the other hoisting intrinsics) into a package-level
// temp global initializer (BUG-MODPROBE-010). The temp's object id was
// registered by defVar at hoist time, so emitting it as a GlobalEnt reuses
// the exact backend path of a user-declared package var.
func packageHoistTemp(pn *Node) (ginit, error) {
	if pn.Kind != "vardecl" {
		return ginit{}, fmt.Errorf("gofrontend: mrHoistPre node kind %q is not vardecl (exporter bug)", pn.Kind)
	}
	objs, _ := pn.Fields["objs"].([]int)
	vals, _ := pn.Fields["values"].([]*Node)
	if len(objs) != 1 || len(vals) != 1 || vals[0] == nil {
		return ginit{}, fmt.Errorf("gofrontend: mrHoistPre vardecl has %d objs/%d values, want exactly 1 each (exporter bug)", len(objs), len(vals))
	}
	return ginit{oid: objs[0], init: vals[0], pos: pn.Pos}, nil
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
	case mrDeadline, mrParseDur, mrXmlToken, mrM7, mrIfaceMethod, mrUser, mrFuncField, mrRuntimeStack, mrSyncMap:
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
			// MR-closure-var: also allow dereference LHS (e.g.,
			// *last, err = readModuleLine(elem)); exportAssignable
			// handles *ast.StarExpr via exportDerefAssignable.
			switch lhs.(type) {
			case *ast.SelectorExpr, *ast.IndexExpr, *ast.StarExpr:
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
	// P3-M9 (instantiation method tables): a generic helper (e.g.
	// __mrgenerateFIPS[P]) must be instantiated with the call-site type
	// arguments — referencing the template's funcID would leave an
	// unresolvable function object. The instantiation is created on
	// demand (prepared immediately, body emitted by
	// drainMethodTablePending).
	obj := fi.funcID
	sig := fn.Type()
	if fi.isTemplate {
		targs, err := e.helperCallTargs(c)
		if err != nil {
			return nil, err
		}
		obj, err = e.ensureFuncInstantiation(fi, targs)
		if err != nil {
			return nil, err
		}
		// P4: the wire signature must be the instantiated one. The
		// template's generic signature serializes with empty
		// params/results, so the backend's adaptArgsForInterfaceParams
		// would skip arg boxing (observed: errors.AsType[*myErr] never
		// matches because *myErr is never boxed to error).
		if s, ok := sig.(*types.Signature); ok {
			instSig, err := types.Instantiate(nil, s, targs, true)
			if err != nil {
				return nil, err
			}
			sig = instSig
		}
	}
	sigID, err := e.typeID(sig)
	if err != nil {
		return nil, err
	}
	fun := e.node("ident", c, map[string]any{
		"name": fn.Name(), "obj": obj, "type": sigID,
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
	// P3-M9 (generics, funcs): substitute the field type before describing.
	// If pairType is a generic instantiation (e.g., __mrnistCurve_pair[Point]
	// with Point bound via e.subst), the field type may contain typeparams
	// that need resolution. substType applies the active substitution.
	ft := e.substType(st.Field(i).Type())
	ftid, err := e.typeID(ft)
	if err != nil {
		return nil, err
	}
	x := e.node("ident", c, map[string]any{"name": tmpName, "obj": tmpID, "type": tmpTid})
	return e.node("selector", c, map[string]any{
		"x": x, "field": field, "selpath": []any{i}, "type": ftid,
	}), nil
}

// etaExpandMRFuncValues rewrites variable initializers that capture a
// multi-result function as a value:
//
//	x := pkg.F  →  x := func(p0 T0, ...) (R0, ...) { return pkg.F(p0, ...) }
//
// (and likewise for struct/map literal fields holding such a value).
//
// Rationale (general): frontend/v1 cannot represent a multi-result function
// as a value (M5 scope error "X in value position ... multi-result function
// values require v2"). Eta-expansion is the semantics-preserving general
// fix: the wrapper is an ordinary multi-result closure, which the existing
// machinery already handles — exportFuncLit pair-transforms it, the
// MR-closure-var link (linkFuncLitMRVars and the exportPackageVars ValueSpec
// case) connects call sites through the variable to the pair info, and the
// inner call desugars through the standard multi-result call path
// (classifyMultiResultCall + hoistMRCallArg). This covers any
// package-qualified multi-result function (synthetic Appendix-M tables,
// the disk MR table, user pair-transforms).
//
// Package-level `var x = pkg.F` specs are handled according to
// removeAliasedSpecs:
//
//   - Disk packages (removeAliasedSpecs=true, buildDiskPackage): the spec
//     is removed and the alias (x -> F) recorded in the returned map. The
//     disk exporter never sees the declaration; call sites through x
//     desugar via the alias map and the table classifiers
//     (classifyMultiResultCall / userMRInfoForVar fallbacks). This is the
//     real-x509 path that compiles path/filepath's `var lstat = os.Lstat`.
//   - User and synthetic packages (removeAliasedSpecs=false, ExportFiles
//     and buildSyntheticPackage): the spec is left untouched. PKG-035's
//     export-time machinery lowers the initializer to the pair-returning
//     value form (the __-helper ident for table-driven functions, the
//     transformed funcdecl for user functions) and links the variable to
//     its pair info (userMRVars), so call sites through the variable
//     desugar exactly like direct calls. Removing the declaration here
//     would delete a legal global while leaving every use dangling (and
//     an AST-level ident→pkg.F rewrite would be unsound for reassignable
//     vars), so the removal is deliberately disk-only.
//
// Runs post-typecheck (it needs info for the signature); called from
// buildDiskPackage, buildSyntheticPackage, and ExportFiles. Idempotent:
// after expansion the RHS is a FuncLit, which never re-matches. Method
// values keep their existing path (Selections non-nil skips them).
func etaExpandMRFuncValues(files []*ast.File, info *types.Info, pkg *types.Package, removeAliasedSpecs bool) (map[*types.Var]*ast.FuncLit, map[*types.Var]*types.Func) {
	// Returns (var->FuncLit map, var->Func alias map). The FuncLit map is
	// currently always empty (kept for the disk-package merge shape); the
	// alias map is populated only when removeAliasedSpecs is true.
	varLits := make(map[*types.Var]*ast.FuncLit)
	aliases := make(map[*types.Var]*types.Func)
	for _, f := range files {
		// Names to drop per ValueSpec (can't modify during Inspect).
		type specEdit struct {
			vs   *ast.ValueSpec
			drop map[int]bool // name indices aliased to a multi-result Func
		}
		var edits []specEdit
		ast.Inspect(f, func(n ast.Node) bool {
			switch n := n.(type) {
			case *ast.ValueSpec:
				if !removeAliasedSpecs {
					return true
				}
				drop := make(map[int]bool)
				for i, v := range n.Values {
					sel, ok := unparen(v).(*ast.SelectorExpr)
					if !ok {
						continue
					}
					if _, isSel := info.Selections[sel]; isSel {
						continue
					}
					obj, ok := info.Uses[sel.Sel].(*types.Func)
					if !ok {
						continue
					}
					sig, ok := obj.Type().(*types.Signature)
					if !ok || sig.Results().Len() <= 1 {
						continue
					}
					if tp := sig.TypeParams(); tp != nil && tp.Len() > 0 {
						continue
					}
					// Record the alias and mark the name for removal. The
					// value initializes name i: a selector initializer
					// always pairs positionally (len(Values)==len(Names)
					// post-typecheck; a lone selector initializes the
					// sole name).
					if i < len(n.Names) {
						if vr, ok := info.Defs[n.Names[i]].(*types.Var); ok {
							aliases[vr] = obj
						}
						drop[i] = true
					}
				}
				if len(drop) > 0 {
					edits = append(edits, specEdit{vs: n, drop: drop})
				}
			case *ast.AssignStmt:
				if n.Tok != token.DEFINE {
					return true
				}
				for i, rhs := range n.Rhs {
					if lit := etaExpandFuncValue(rhs, info, pkg); lit != nil {
						n.Rhs[i] = lit
					}
				}
			case *ast.KeyValueExpr:
				// Struct/map literal field holding a multi-result function
				// value, e.g. cipherFunc: des.NewCipher in x509's
				// rfc1423Algos table. Call sites through the field desugar
				// via the mrFuncField plan (classifyGeneralFuncField).
				if lit := etaExpandFuncValue(n.Value, info, pkg); lit != nil {
					n.Value = lit
				}
			}
			return true
		})
		// Apply the edits: drop only the aliased names (and their values),
		// keeping the rest of a multi-name spec intact. When every name is
		// aliased, remove the whole spec from its GenDecl.
		for _, ed := range edits {
			vs := ed.vs
			if len(ed.drop) == len(vs.Names) {
				for _, decl := range f.Decls {
					if gd, ok := decl.(*ast.GenDecl); ok {
						for i, spec := range gd.Specs {
							if spec == vs {
								gd.Specs = append(gd.Specs[:i], gd.Specs[i+1:]...)
								break
							}
						}
					}
				}
				continue
			}
			keepNames := make([]*ast.Ident, 0, len(vs.Names)-len(ed.drop))
			keepValues := make([]ast.Expr, 0, len(vs.Values)-len(ed.drop))
			for ni, name := range vs.Names {
				if ed.drop[ni] {
					continue
				}
				keepNames = append(keepNames, name)
				// Value i initializes name i (positional pairing).
				if ni < len(vs.Values) {
					keepValues = append(keepValues, vs.Values[ni])
				}
			}
			vs.Names = keepNames
			if len(keepValues) == len(keepNames) {
				vs.Values = keepValues
			}
		}
		// NOTE: no AST-level ident→pkg.F rewrite here. Rewriting use-site
		// idents to selector exprs would be unsound for reassignable vars
		// (a later assignment to x must be seen through x), and building
		// correct info.Uses/Types entries for synthesized selectors is
		// fragile. Call sites through an aliased var desugar in the
		// exporter via the returned alias map instead
		// (classifyMultiResultCall / userMRInfoForVar).
	}
	return varLits, aliases
}

// etaExpandFuncValue returns the eta-expanded wrapper closure for v when v
// is a bare package-qualified reference to a multi-result function, or nil
// when v is not such a reference.
func etaExpandFuncValue(v ast.Expr, info *types.Info, pkg *types.Package) *ast.FuncLit {
	sel, ok := unparen(v).(*ast.SelectorExpr)
	if !ok {
		return nil
	}
	if _, isSel := info.Selections[sel]; isSel {
		return nil
	}
	obj, ok := info.Uses[sel.Sel].(*types.Func)
	if !ok {
		return nil
	}
	sig, ok := obj.Type().(*types.Signature)
	if !ok || sig.Results().Len() <= 1 {
		return nil
	}
	if tp := sig.TypeParams(); tp != nil && tp.Len() > 0 {
		return nil
	}
	return etaExpandSelector(sel, sig, info, pkg)
}

// etaExpandSelector builds `func(p0 T0, ...) (R0, ...) { return pkg.F(p0, ...) }`.
// The FuncType's field lists are intentionally empty: the exporter reads the
// signature from info.Types[funcType] (populated here), never from the
// syntax. Fresh param vars are registered in info.Uses/Types so the
// argument idents export; the original selector is reused as the call's
// Fun so its existing info entries (PkgName, Func) classify the inner
// multi-result call.
func etaExpandSelector(sel *ast.SelectorExpr, sig *types.Signature, info *types.Info, pkg *types.Package) *ast.FuncLit {
	params := sig.Params()
	used := make(map[string]bool, params.Len())
	for i := 0; i < params.Len(); i++ {
		if name := params.At(i).Name(); name != "" {
			used[name] = true
		}
	}
	freshVars := make([]*types.Var, params.Len())
	args := make([]ast.Expr, params.Len())
	for i := 0; i < params.Len(); i++ {
		p := params.At(i)
		name := p.Name()
		if name == "" || name == "_" {
			name = fmt.Sprintf("__eta_p%d", i)
			for used[name] {
				name = "_" + name
			}
		}
		used[name] = true
		fv := types.NewParam(sel.Pos(), pkg, name, p.Type())
		freshVars[i] = fv
		id := ast.NewIdent(name)
		id.NamePos = sel.Pos()
		info.Uses[id] = fv
		info.Types[id] = types.TypeAndValue{Type: p.Type()}
		args[i] = id
	}
	call := &ast.CallExpr{Fun: sel, Lparen: sel.Pos(), Args: args, Rparen: sel.End()}
	if sig.Variadic() {
		call.Ellipsis = sel.End()
	}
	freshSig := types.NewSignatureType(nil, nil, nil, types.NewTuple(freshVars...), sig.Results(), sig.Variadic())
	funcType := &ast.FuncType{Func: sel.Pos(), Params: &ast.FieldList{}, Results: &ast.FieldList{}}
	info.Types[funcType] = types.TypeAndValue{Type: freshSig}
	body := &ast.BlockStmt{
		Lbrace: sel.Pos(),
		List: []ast.Stmt{
			&ast.ReturnStmt{Return: sel.Pos(), Results: []ast.Expr{call}},
		},
		Rbrace: sel.End(),
	}
	return &ast.FuncLit{Type: funcType, Body: body}
}
