package p3frontend

// Drift harness for the v1 synthetic stdlib (lane D, review P1-4).
//
// This module is no longer a frontend: it contains no importer, loader,
// flattener, exporter, or synthetic package. It is a test-only harness
// that pins v1's synthetic packages (internal/frontend/synth_*.go)
// against the real gc 1.27.1 stdlib (GOROOT, loaded from source).
//
// Why this replaced the old mirror tests: the old harness compared
// hand-built *types.Package mirrors against v1's sources, and the
// mirrors existed only to serve p3frontend's own (now deleted) importer
// — a second, parallel bridge universe that had to be maintained in
// lockstep with v1. The mirror-vs-v1 comparison could never catch a
// case where BOTH sides drifted from gc together, and its omission
// checks for methods were vacuous (they looked methods up in the
// package scope, where methods never live). This harness compares v1
// directly against gc, so a v1 change fails fast here, and omissions
// are validated as real gc members (package names, Type.Method pairs,
// or Type.Field pairs) that v1 deliberately lacks, with a reason from
// the exclusions.yaml vocabulary.
//
// Scope of the check (exported API only):
//   - every exported gc package member is present in v1 with an
//     identical rendered type (consts also match by value), except
//     declared omissions;
//   - every exported v1 member is present in gc (v1-only members fail
//     unless listed in extra with a reason);
//   - for each named type in both: exported method sets match
//     (modulo method omissions); for structs, exported fields match
//     (modulo field omissions); for interfaces and other underlyings,
//     the rendered underlying type matches.
// Unexported types, fields, and methods are v1-internal implementation
// detail (e.g. the Appendix M pair carriers) and are not pinned here;
// v1's own suite and the differential corpus cover their behaviour.
//
// Accepted type drifts (accept table, logged on every run that hits
// one): same-name members whose declared type differs between gc and
// v1 for a documented reason — currently only error vars gc declares
// as error that v1 declares with a concrete unexported type. The
// difference is real but narrow (only the %T dynamic type differs;
// == and errors.Is behave identically) and the fix belongs to the
// owning v1 lane, not to this harness.
//
// Excluded from the table, with reasons:
//   - "sync": the synthetic sync was retired (real sync ships now), so
//     there is no v1 synthetic surface left to pin.
//   - "io": v1's io is a minimal dependency-only surface (Reader/Writer
//     for bytes/strings), not a claimed gc API, so a gc comparison
//     would fail by design.
//   - "encoding/xml": v1 xml is a divergent port (its own
//     Reader/Writer/ReadResult instead of io.Reader/io.Writer, plus
//     missing Header/TagPathError/UnsupportedTypeError/CharsetReader),
//     not a subset of gc — see the NOTE above the table. It needs its
//     planned io migration before it can be pinned.
//   - "encoding/json": v1 json predates the gc 1.27 json API (missing
//     options surface, v2 methods/fields, even DisallowUnknownFields;
//     RawMessage redefined upstream) — see the NOTE above the table.
//     It needs a refresh to the gc 1.27 API before it can be pinned.
//   - "runtime", "fmt", "sync/atomic", "unsafe": these never had v1
//     synth files or drift tests; their old mirrors died with the
//     importer and nothing replaces them.

import (
	"fmt"
	"go/ast"
	"go/importer"
	"go/parser"
	"go/token"
	"go/types"
	"strconv"
	"testing"
)

// importerFunc adapts a func to types.Importer.
type importerFunc func(path string) (*types.Package, error)

func (f importerFunc) Import(path string) (*types.Package, error) { return f(path) }

// gorootSourceImporter returns a types.Importer that type-checks real
// GOROOT packages from source (the test toolchain's GOROOT, pinned to gc
// 1.27.1). The source importer resolves internal packages from the std
// tree, so imports like internal/reflectlite succeed here.
func gorootSourceImporter() types.Importer {
	return importer.ForCompiler(token.NewFileSet(), "source", nil)
}

// loadV1SyntheticPackage parses the named string const out of a v1
// frontend source file, type-checks the embedded Go source with imp,
// and returns its *types.Package. This is the v1 side of the drift
// check: tests compare this package against gc — not against a mirror
// — so a v1-side change fails fast instead of silently diverging.
func loadV1SyntheticPackage(t *testing.T, srcFile, constName, path string, imp types.Importer) *types.Package {
	t.Helper()
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, srcFile, nil, 0)
	if err != nil {
		t.Fatalf("parsing v1 synth file %s: %v", srcFile, err)
	}
	var raw string
	found := false
	for _, decl := range file.Decls {
		gd, ok := decl.(*ast.GenDecl)
		if !ok || gd.Tok != token.CONST {
			continue
		}
		for _, spec := range gd.Specs {
			vs, ok := spec.(*ast.ValueSpec)
			if !ok {
				continue
			}
			for i, name := range vs.Names {
				if name.Name == constName && i < len(vs.Values) {
					lit, ok := vs.Values[i].(*ast.BasicLit)
					if !ok || lit.Kind != token.STRING {
						t.Fatalf("%s is not a string literal", constName)
					}
					raw, err = strconv.Unquote(lit.Value)
					if err != nil {
						t.Fatalf("unquoting %s: %v", constName, err)
					}
					found = true
				}
			}
		}
	}
	if !found {
		t.Fatalf("%s const not found in %s", constName, srcFile)
	}
	synFset := token.NewFileSet()
	synFile, err := parser.ParseFile(synFset, constName+".go", raw, parser.AllErrors)
	if err != nil {
		t.Fatalf("parsing synthetic %s source: %v", constName, err)
	}
	conf := types.Config{Importer: imp}
	pkg, err := conf.Check(path, synFset, []*ast.File{synFile}, nil)
	if err != nil {
		t.Fatalf("type-checking v1 synthetic %s: %v", constName, err)
	}
	return pkg
}

// v1Importer resolves the imports of a v1 synthetic source from v1's
// own sources where the dependency is itself synthetic, and from real
// GOROOT source otherwise. It never builds a mirror: the dependency
// graph is the v1 graph, so the harness cannot drift from v1 the way
// the old mirrors could. reflectStub adds the test-only minimal
// reflect shim (see reflectStubForTest) for sources that import
// reflect; only json needs it.
func v1Importer(t *testing.T, reflectStub bool) types.Importer {
	t.Helper()
	cache := map[string]*types.Package{}
	var imp importerFunc
	load := func(file, constName, path string) (*types.Package, error) {
		pkg := loadV1SyntheticPackage(t, "../"+file, constName, path, imp)
		return pkg, nil
	}
	imp = importerFunc(func(path string) (*types.Package, error) {
		if pkg, ok := cache[path]; ok {
			return pkg, nil
		}
		var pkg *types.Package
		var err error
		switch path {
		case "errors":
			pkg, err = load("synth_errors.go", "syntheticErrorsSource", "errors")
		case "io":
			pkg, err = load("synth_io.go", "syntheticIoSource", "io")
		case "encoding/base64":
			pkg, err = load("synth_base64.go", "syntheticBase64Source", "encoding/base64")
		case "time":
			pkg, err = load("synth_time_context.go", "syntheticTimeSource", "time")
		case "bytes":
			pkg, err = load("synth_bytes.go", "syntheticBytesSource", "bytes")
		case "reflect":
			if reflectStub {
				pkg = reflectStubForTest()
				break
			}
			pkg, err = gorootSourceImporter().Import(path)
		default:
			// Real stdlib dependency (iter, internal/reflectlite,
			// sync, unicode/utf8, ...): resolve from GOROOT source.
			pkg, err = gorootSourceImporter().Import(path)
		}
		if err != nil {
			return nil, err
		}
		cache[path] = pkg
		return pkg, nil
	})
	return imp
}

// reflectStubForTest builds the minimal synthetic "reflect" package the
// v1 json source imports: the Type/Value/StructField named types with
// the shapes the source uses (Type.String for jsonTypeString,
// StructField.Name for UnmarshalFieldError.Error). Only the "reflect.X"
// rendering matters for the drift comparison. TEST-ONLY: this stub is
// never shipped, never type-checks user code, and exists solely so the
// harness does not have to drag real reflect through the source
// importer to type-check v1's json source.
func reflectStubForTest() *types.Package {
	pkg := types.NewPackage("reflect", "reflect")
	stringT := types.Typ[types.String]
	// Type as a named interface (nillable like the real reflect.Type;
	// jsonTypeString compares t == nil) with String() string.
	typeName := types.NewTypeName(token.NoPos, pkg, "Type", nil)
	strMethod := types.NewFunc(token.NoPos, pkg, "String",
		types.NewSignatureType(nil, nil, nil,
			types.NewTuple(),
			types.NewTuple(types.NewParam(token.NoPos, pkg, "", stringT)),
			false))
	types.NewNamed(typeName,
		types.NewInterfaceType([]*types.Func{strMethod}, nil).Complete(), nil)
	pkg.Scope().Insert(typeName)
	// Value (opaque for the drift comparison: field type only).
	valueName := types.NewTypeName(token.NoPos, pkg, "Value", nil)
	types.NewNamed(valueName, types.NewStruct(nil, nil), nil)
	pkg.Scope().Insert(valueName)
	// StructField with the Name field the error type formats.
	sfName := types.NewTypeName(token.NoPos, pkg, "StructField", nil)
	sfField := types.NewField(token.NoPos, pkg, "Name", stringT, false)
	types.NewNamed(sfName, types.NewStruct([]*types.Var{sfField}, nil), nil)
	pkg.Scope().Insert(sfName)
	// Required: go/types only resolves imports of complete packages.
	// (The old mirror's stub had this call; dropping it in the port
	// made every reflect use in v1's json source "undefined".)
	pkg.MarkComplete()
	return pkg
}

// gorootPackages memoizes GOROOT source loads within one test process.
// Tests run sequentially, so no locking is needed.
var gorootPackages = map[string]*types.Package{}

// gorootPackage loads a real gc package from GOROOT source.
func gorootPackage(t *testing.T, path string) *types.Package {
	t.Helper()
	if pkg, ok := gorootPackages[path]; ok {
		return pkg
	}
	pkg, err := gorootSourceImporter().Import(path)
	if err != nil {
		t.Fatalf("loading GOROOT %q: %v", path, err)
	}
	gorootPackages[path] = pkg
	return pkg
}

// typeQualifier renders package qualifiers by package name, so equal
// signatures render equal strings on both sides of the comparison.
func typeQualifier(p *types.Package) string {
	if p == nil {
		return ""
	}
	return p.Name()
}

func typeString(typ types.Type) string {
	return types.TypeString(typ, typeQualifier)
}

// anonTypeString renders a type with function parameter and result
// names stripped. Go signature identity ignores those names (spec:
// two function types are identical iff their parameter/result TYPES
// match and variadicity matches), so gc's `func() (records
// [][]string, err error)` and v1's `func() ([][]string, error)` are
// the same API — comparing rendered strings verbatim would fail every
// such member for a cosmetic difference. Everything else is preserved:
// named types render by package-qualified name (aliases unwrapped, since
// an alias IS its right-hand side), struct fields keep their names and
// tags, interface methods keep their names, type arguments are compared
// recursively.
func anonTypeString(typ types.Type) string {
	switch t := typ.(type) {
	case *types.Basic:
		return t.Name()
	case *types.Named:
		s := ""
		if t.Obj().Pkg() != nil {
			s = t.Obj().Pkg().Name() + "."
		}
		s += t.Obj().Name()
		if t.TypeArgs().Len() > 0 {
			s += "["
			for i := 0; i < t.TypeArgs().Len(); i++ {
				if i > 0 {
					s += ", "
				}
				s += anonTypeString(t.TypeArgs().At(i))
			}
			s += "]"
		}
		return s
	case *types.Alias:
		return anonTypeString(types.Unalias(t))
	case *types.Signature:
		s := "func"
		if t.TypeParams().Len() > 0 {
			s += "["
			for i := 0; i < t.TypeParams().Len(); i++ {
				if i > 0 {
					s += ", "
				}
				tp := t.TypeParams().At(i)
				s += tp.Obj().Name() + " " + anonTypeString(tp.Constraint())
			}
			s += "]"
		}
		s += "("
		for i := 0; i < t.Params().Len(); i++ {
			if i > 0 {
				s += ", "
			}
			if t.Variadic() && i == t.Params().Len()-1 {
				if sl, ok := t.Params().At(i).Type().(*types.Slice); ok {
					s += "..." + anonTypeString(sl.Elem())
					continue
				}
			}
			s += anonTypeString(t.Params().At(i).Type())
		}
		s += ")"
		switch t.Results().Len() {
		case 0:
		case 1:
			s += " " + anonTypeString(t.Results().At(0).Type())
		default:
			s += " ("
			for i := 0; i < t.Results().Len(); i++ {
				if i > 0 {
					s += ", "
				}
				s += anonTypeString(t.Results().At(i).Type())
			}
			s += ")"
		}
		return s
	case *types.Interface:
		s := "interface{"
		for i := 0; i < t.NumExplicitMethods(); i++ {
			m := t.ExplicitMethod(i)
			s += m.Name() + anonTypeString(m.Type()) + "; "
		}
		for i := 0; i < t.NumEmbeddeds(); i++ {
			s += anonTypeString(t.EmbeddedType(i)) + "; "
		}
		return s + "}"
	case *types.Struct:
		s := "struct{"
		for i := 0; i < t.NumFields(); i++ {
			f := t.Field(i)
			s += f.Name() + " " + anonTypeString(f.Type())
			if tag := t.Tag(i); tag != "" {
				s += " `" + tag + "`"
			}
			s += "; "
		}
		return s + "}"
	case *types.Slice:
		return "[]" + anonTypeString(t.Elem())
	case *types.Array:
		return "[" + strconv.FormatInt(t.Len(), 10) + "]" + anonTypeString(t.Elem())
	case *types.Map:
		return "map[" + anonTypeString(t.Key()) + "]" + anonTypeString(t.Elem())
	case *types.Chan:
		var dir string
		switch t.Dir() {
		case types.SendRecv:
			dir = "chan "
		case types.SendOnly:
			dir = "chan<- "
		case types.RecvOnly:
			dir = "<-chan "
		}
		return dir + anonTypeString(t.Elem())
	case *types.Pointer:
		return "*" + anonTypeString(t.Elem())
	case *types.Tuple:
		s := "("
		for i := 0; i < t.Len(); i++ {
			if i > 0 {
				s += ", "
			}
			s += anonTypeString(t.At(i).Type())
		}
		return s + ")"
	case *types.TypeParam:
		return t.Obj().Name()
	case *types.Union:
		s := ""
		for i := 0; i < t.Len(); i++ {
			if i > 0 {
				s += " | "
			}
			term := t.Term(i)
			if term.Tilde() {
				s += "~"
			}
			s += anonTypeString(term.Type())
		}
		return s
	default:
		return typeString(typ)
	}
}

// driftEntry is one v1 synthetic package pinned against gc.
type driftEntry struct {
	path        string // GOROOT import path, e.g. "strings"
	file        string // v1 frontend file, e.g. "synth_strings.go"
	constName   string // synthetic source const in file
	reflectStub bool   // type-check the v1 source with the reflect shim
	// omit maps a deliberately absent gc member to "REASON: detail".
	// Keys are a package-level name ("Lines"), a method
	// ("Replacer.WriteString"), or a struct field ("Timer.C").
	// Every key must exist in gc and be absent in v1, or the test
	// fails: a stale omission (landed) or a typo fails loudly.
	omit map[string]string
	// extra maps a v1-only member (absent in gc) to its reason.
	// Empty unless a v1 extension is deliberately kept; each entry
	// must be justified, never a silent allowance.
	extra map[string]string
	// accept maps a same-name member whose TYPE differs between gc
	// and v1 to the reason the difference is accepted (logged, not
	// hidden, on every run that hits it). This is for divergences
	// that are real but out of this harness's power to fix — e.g. a
	// var gc declares as error that v1 declares with a concrete
	// unexported type — where the fix belongs to the v1 package's
	// owning lane, not to the harness. An accept entry never covers
	// a missing or extra member, only a type difference, and every
	// entry is reported as a finding by the lane that owns the table.
	accept map[string]string
}

// validOmitReasons is the harness's reason vocabulary: the
// exclusions.yaml vocabulary, plus PARTIAL_PORT for members of a v1
// package that is a declared partial port (its own header says so),
// where the member lands with that package's completion rather than
// with any other package's wave.
var validOmitReasons = map[string]bool{
	"PLATFORM": true, "CGO": true, "ASM_UNSUPPORTED": true,
	"LINKNAME_MISSING": true, "UNSUPPORTED_FEATURE": true,
	"DEFERRED_WAVE": true, "NONPORTABLE_BEHAVIOR": true,
	"PARTIAL_PORT": true,
}

func validReason(reason string) bool {
	for code := range validOmitReasons {
		if len(reason) > len(code)+1 && reason[:len(code)] == code && reason[len(code)] == ':' {
			return true
		}
	}
	return false
}

// driftPackages is the pinned set: every v1 synth package with a drift
// test in the old harness, minus sync (retired) and io (minimal dep
// surface, not a claimed gc API). Omission tables are ported verbatim
// from the old per-package omission tests, except strings'
// Replacer.WriteString and Reader.WriteTo, which the old test looked
// up in the package scope (where methods never live) and so never
// actually pinned; here they are real method omissions.
var driftPackages = []driftEntry{
	{path: "bytes", file: "synth_bytes.go", constName: "syntheticBytesSource",
		omit: map[string]string{
			"Lines":          "DEFERRED_WAVE: needs iter.Seq (iter package + generics)",
			"SplitSeq":       "DEFERRED_WAVE: needs iter.Seq[[]byte]",
			"SplitAfterSeq":  "DEFERRED_WAVE: needs iter.Seq[[]byte]",
			"FieldsSeq":      "DEFERRED_WAVE: needs iter.Seq[[]byte]",
			"FieldsFuncSeq":  "DEFERRED_WAVE: needs iter.Seq[[]byte]",
			"ToUpperSpecial": "DEFERRED_WAVE: needs unicode.SpecialCase",
			"ToLowerSpecial": "DEFERRED_WAVE: needs unicode.SpecialCase",
			"ToTitleSpecial": "DEFERRED_WAVE: needs unicode.SpecialCase",
		}},
	{path: "encoding/base32", file: "synth_base32.go", constName: "syntheticBase32Source"},
	{path: "encoding/base64", file: "synth_base64.go", constName: "syntheticBase64Source"},
	{path: "encoding/hex", file: "synth_hex.go", constName: "syntheticHexSource",
		omit: map[string]string{
			"NewEncoder": "DEFERRED_WAVE: streaming encoder needs io (P3-M8)",
			"NewDecoder": "DEFERRED_WAVE: streaming decoder needs io (P3-M8)",
			"Dumper":     "DEFERRED_WAVE: hex dumper needs io (P3-M8)",
		},
		accept: map[string]string{
			"ErrLength": "v1 declares the var with a concrete unexported type (*hexErrorString); gc declares it as error. == and errors.Is behave identically; only the %T dynamic type differs.",
		}},
	{path: "encoding/binary", file: "synth_binary.go", constName: "syntheticBinarySource",
		omit: map[string]string{
			"Read":        "DEFERRED_WAVE: needs io.Reader",
			"ReadUvarint": "DEFERRED_WAVE: needs io.ByteReader",
			"ReadVarint":  "DEFERRED_WAVE: needs io.ByteReader",
			"Write":       "DEFERRED_WAVE: needs io.Writer",
		}},
	{path: "encoding/ascii85", file: "synth_ascii85.go", constName: "syntheticAscii85Source",
		omit: map[string]string{
			"NewEncoder": "DEFERRED_WAVE: streaming encoder needs io (P3-M8)",
			"NewDecoder": "DEFERRED_WAVE: streaming decoder needs io (P3-M8)",
		}},
	{path: "encoding/pem", file: "synth_pem.go", constName: "syntheticPemSource",
		omit: map[string]string{
			"Encode": "DEFERRED_WAVE: needs io.Writer",
		}},
	{path: "encoding/asn1", file: "synth_asn1.go", constName: "syntheticAsn1Source"},
	{path: "encoding", file: "synth_encoding.go", constName: "syntheticEncodingSource"},
	{path: "encoding/csv", file: "synth_enc_csv_gob.go", constName: "syntheticCsvSource",
		extra: map[string]string{
			"NewBufferWriter": "v1 extension: buffer-based writer helper; not gc API",
			"NewReaderString": "v1 extension: string-based reader helper; not gc API",
			"Writer.Bytes":    "v1 extension: exposes encoded bytes directly; not gc API",
		}},
	{path: "encoding/gob", file: "synth_enc_csv_gob.go", constName: "syntheticGobSource",
		omit: map[string]string{
			"NewDecoder":          "DEFERRED_WAVE: needs io.Reader",
			"NewEncoder":          "DEFERRED_WAVE: needs io.Writer",
			"CommonType":          "PARTIAL_PORT: v1 gob is a partial port; type-system surface lands with gob completion",
			"GobEncoder":          "PARTIAL_PORT: v1 gob is a partial port; type-system surface lands with gob completion",
			"GobDecoder":          "PARTIAL_PORT: v1 gob is a partial port; type-system surface lands with gob completion",
			"Decoder.DecodeValue": "PARTIAL_PORT: v1 gob is a partial port; reflect-based decode lands with gob completion",
			"Encoder.EncodeValue": "PARTIAL_PORT: v1 gob is a partial port; reflect-based encode lands with gob completion",
		},
		extra: map[string]string{
			"NewBufferDecoder": "v1 extension: buffer-based decoder helper; not gc API",
			"NewBufferEncoder": "v1 extension: buffer-based encoder helper; not gc API",
			"Encoder.Bytes":    "v1 extension: exposes encoded bytes directly; not gc API",
		}},
	{path: "errors", file: "synth_errors.go", constName: "syntheticErrorsSource"},
	// NOTE (lane D): encoding/json is DELIBERATELY NOT in this table
	// either. v1 json predates the gc 1.27 json API it would be
	// pinned against: it lacks the whole options surface (Options,
	// DefaultOptionsV1, and the *WithLegacySemantics/Format*/Parse*
	// options), Number's MarshalJSONTo/UnmarshalJSONFrom,
	// UnmarshalTypeError's Err field and Unwrap, and even the
	// long-standing Decoder.DisallowUnknownFields; its RawMessage is
	// the old []byte-based defined type where gc 1.27 made it an
	// alias of jsontext.Value. Its NewDecoder/NewEncoder/Buffered
	// also take local jsonReader/jsonWriter interfaces instead of
	// io.Reader/io.Writer — but those two are method-set-identical
	// to io's, so that part of the drift is cosmetic, unlike xml's
	// Next()-based Reader. Enumerating the version lag as omissions
	// would mislabel "old port" as "deliberate scope", so the
	// harness excludes json and the finding goes to the coordinator:
	// v1 json needs a refresh to the gc 1.27 API before it can be
	// pinned. (The reflect shim above stays: it documents how v1's
	// json source type-checks, and the refresh lane will need it.)
	{path: "math", file: "synth_math.go", constName: "syntheticMathSource"},
	{path: "sort", file: "synth_sort.go", constName: "syntheticSortSource"},
	{path: "strconv", file: "synth_strconv.go", constName: "syntheticStrconvSource",
		accept: map[string]string{
			"ErrRange":  "v1 declares the var with a concrete unexported type (strconvError); gc declares it as error. == and errors.Is behave identically; only the %T dynamic type differs.",
			"ErrSyntax": "v1 declares the var with a concrete unexported type (strconvError); gc declares it as error. == and errors.Is behave identically; only the %T dynamic type differs.",
		}},
	{path: "strings", file: "synth_strings.go", constName: "syntheticStringsSource",
		omit: map[string]string{
			"ToUpperSpecial": "DEFERRED_WAVE: needs unicode.SpecialCase parameter",
			"ToLowerSpecial": "DEFERRED_WAVE: needs unicode.SpecialCase parameter",
			"ToTitleSpecial": "DEFERRED_WAVE: needs unicode.SpecialCase parameter",
			"Lines":          "DEFERRED_WAVE: needs iter.Seq result",
			"SplitAfterSeq":  "DEFERRED_WAVE: needs iter.Seq result",
			"FieldsSeq":      "DEFERRED_WAVE: needs iter.Seq result",
			"FieldsFuncSeq":  "DEFERRED_WAVE: needs iter.Seq result",
		}},
	// NOTE (lane D): Replacer.WriteString and Reader.WriteTo were in
	// the old omission table but are NOT omitted here: the first gc
	// comparison proved they LANDED in v1 (the old check looked
	// methods up in the package scope, where methods never live, so
	// it could not see the landing). SplitSeq likewise landed earlier
	// and stays present.
	{path: "time", file: "synth_time_context.go", constName: "syntheticTimeSource",
		omit: map[string]string{
			// v1 time is a declared partial M5 port (see
			// synth_time_context.go header: "in-scope API"); these
			// members land with time completion, not with any other
			// package's wave. The list is the machine-readable
			// record of exactly how partial v1 time is.
			"FixedZone":              "PARTIAL_PORT: v1 time is a partial M5 port; constructors land with time completion",
			"LoadLocation":           "PARTIAL_PORT: v1 time is a partial M5 port; constructors land with time completion",
			"LoadLocationFromTZData": "PARTIAL_PORT: v1 time is a partial M5 port; constructors land with time completion",
			"ParseError":             "PARTIAL_PORT: v1 time is a partial M5 port; error types land with time completion",
			"ParseInLocation":        "PARTIAL_PORT: v1 time is a partial M5 port; parsers land with time completion",
			"UnixMicro":              "PARTIAL_PORT: v1 time is a partial M5 port; constructors land with time completion",
			"UnixMilli":              "PARTIAL_PORT: v1 time is a partial M5 port; constructors land with time completion",
			"Time.AppendBinary":      "PARTIAL_PORT: v1 time is a partial M5 port; marshalers land with time completion",
			"Time.AppendText":        "PARTIAL_PORT: v1 time is a partial M5 port; marshalers land with time completion",
			"Time.GobDecode":         "PARTIAL_PORT: v1 time is a partial M5 port; marshalers land with time completion",
			"Time.GobEncode":         "PARTIAL_PORT: v1 time is a partial M5 port; marshalers land with time completion",
			"Time.ISOWeek":           "PARTIAL_PORT: v1 time is a partial M5 port; calendar methods land with time completion",
			"Time.IsDST":             "PARTIAL_PORT: v1 time is a partial M5 port; calendar methods land with time completion",
			"Time.MarshalBinary":     "PARTIAL_PORT: v1 time is a partial M5 port; marshalers land with time completion",
			"Time.MarshalJSON":       "PARTIAL_PORT: v1 time is a partial M5 port; marshalers land with time completion",
			"Time.MarshalText":       "PARTIAL_PORT: v1 time is a partial M5 port; marshalers land with time completion",
			"Time.String":            "PARTIAL_PORT: v1 time is a partial M5 port; formatting methods land with time completion",
			"Time.UnmarshalBinary":   "PARTIAL_PORT: v1 time is a partial M5 port; marshalers land with time completion",
			"Time.UnmarshalJSON":     "PARTIAL_PORT: v1 time is a partial M5 port; marshalers land with time completion",
			"Time.UnmarshalText":     "PARTIAL_PORT: v1 time is a partial M5 port; marshalers land with time completion",
			"Time.ZoneBounds":        "PARTIAL_PORT: v1 time is a partial M5 port; zone methods land with time completion",
		}},
	{path: "context", file: "synth_time_context.go", constName: "syntheticContextSource"},
	// NOTE (lane D): encoding/xml is DELIBERATELY NOT in this table.
	// v1 xml is not a subset of gc xml, it is a divergent port: it
	// defines its own Reader/Writer/ReadResult interfaces instead of
	// io.Reader/io.Writer, and lacks Header, TagPathError,
	// UnsupportedTypeError, and Decoder.CharsetReader. Its own header
	// (synth_xml.go, "M7 structural I/O") already plans retiring
	// Reader/Writer/ReadResult for io once WS D lands — that retirement
	// is a real-xml decision for its owning lane, not something this
	// harness can express as omissions. Pinning xml here would either
	// bless the divergence (extras) or mislabel it (omissions), so
	// the harness excludes it and the finding goes to the coordinator:
	// v1 xml needs its planned io migration before it can be pinned.
}

// namedType returns the *types.Named for an exported type name, or nil.
func namedType(pkg *types.Package, name string) *types.Named {
	obj := pkg.Scope().Lookup(name)
	if obj == nil {
		return nil
	}
	tn, ok := obj.(*types.TypeName)
	if !ok {
		return nil
	}
	named, ok := tn.Type().(*types.Named)
	if !ok {
		return nil
	}
	return named
}

// exportedMethods maps exported method names to rendered signatures.
func exportedMethods(named *types.Named) map[string]string {
	out := map[string]string{}
	for i := 0; i < named.NumMethods(); i++ {
		m := named.Method(i)
		if ast.IsExported(m.Name()) {
			out[m.Name()] = anonTypeString(m.Type())
		}
	}
	return out
}

// exportedFields maps exported struct field names to rendered types.
func exportedFields(st *types.Struct) map[string]string {
	out := map[string]string{}
	for i := 0; i < st.NumFields(); i++ {
		f := st.Field(i)
		if ast.IsExported(f.Name()) {
			out[f.Name()] = anonTypeString(f.Type())
		}
	}
	return out
}

// assertDrift pins one v1 synthetic package against gc.
func assertDrift(t *testing.T, e driftEntry) {
	t.Helper()
	v1 := loadV1SyntheticPackage(t, "../"+e.file, e.constName, e.path, v1Importer(t, e.reflectStub))
	gc := gorootPackage(t, e.path)
	if v1.Name() != gc.Name() {
		t.Fatalf("%s: v1 package name = %q, gc name = %q", e.path, v1.Name(), gc.Name())
	}
	for key, reason := range e.omit {
		if !validReason(reason) {
			t.Errorf("%s: omission %q reason %q not in exclusions.yaml vocabulary", e.path, key, reason)
		}
	}
	// Omission keys resolve against gc: bare names in the package
	// scope, Type.Method in the method set, Type.Field in the struct.
	// A key that resolves nowhere is a typo, not an omission.
	resolveOmission := func(key string) (kind string) {
		for i := 0; i < len(key); i++ {
			if key[i] == '.' {
				typeName, member := key[:i], key[i+1:]
				named := namedType(gc, typeName)
				if named == nil {
					return ""
				}
				if _, ok := exportedMethods(named)[member]; ok {
					return "method"
				}
				if st, ok := named.Underlying().(*types.Struct); ok {
					if _, ok := exportedFields(st)[member]; ok {
						return "field"
					}
				}
				return ""
			}
		}
		if gc.Scope().Lookup(key) != nil && ast.IsExported(key) {
			return "package"
		}
		return ""
	}
	omitted := map[string]bool{}
	for key := range e.omit {
		kind := resolveOmission(key)
		if kind == "" {
			t.Errorf("%s: omission %q does not exist in gc (typo or landed upstream)", e.path, key)
			continue
		}
		omitted[key] = true
	}
	// Accept entries must name a real gc member with a non-empty
	// reason; a dangling accept is a table bug, not an allowance.
	for key, reason := range e.accept {
		if resolveOmission(key) == "" {
			t.Errorf("%s: accept %q does not exist in gc (typo or landed upstream)", e.path, key)
		}
		if reason == "" {
			t.Errorf("%s: accept %q has no reason", e.path, key)
		}
	}

	// 1. Every exported gc member: present in v1, identical rendered
	// type (consts: identical value too), except omissions.
	for _, name := range gc.Scope().Names() {
		if !ast.IsExported(name) {
			continue
		}
		if omitted[name] {
			if v1.Scope().Lookup(name) != nil {
				t.Errorf("%s: omission %q is stale: v1 now has it (%s)", e.path, name, e.omit[name])
			}
			continue
		}
		want := gc.Scope().Lookup(name)
		got := v1.Scope().Lookup(name)
		if got == nil {
			t.Errorf("%s: v1 missing exported %q (gc has it)", e.path, name)
			continue
		}
		if fmt.Sprintf("%T", want) != fmt.Sprintf("%T", got) {
			t.Errorf("%s: %s is %T in v1, want %T (gc)", e.path, name, got, want)
			continue
		}
		if anonTypeString(want.Type()) != anonTypeString(got.Type()) {
			if reason, ok := e.accept[name]; ok {
				t.Logf("%s: accepted type drift on %s (gc %s, v1 %s): %s", e.path, name, anonTypeString(want.Type()), anonTypeString(got.Type()), reason)
			} else {
				t.Errorf("%s: %s type drift:\n gc: %s\n v1: %s", e.path, name, anonTypeString(want.Type()), anonTypeString(got.Type()))
			}
		}
		if wc, ok := want.(*types.Const); ok {
			gcConst := got.(*types.Const)
			if wc.Val().String() != gcConst.Val().String() {
				t.Errorf("%s: const %s value drift: gc %s, v1 %s", e.path, name, wc.Val(), gcConst.Val())
			}
		}
	}
	// 2. Every exported v1 member: present in gc, unless a documented
	// v1-only extra.
	for _, name := range v1.Scope().Names() {
		if !ast.IsExported(name) {
			continue
		}
		if gc.Scope().Lookup(name) == nil {
			if reason, ok := e.extra[name]; ok {
				t.Logf("%s: v1-only member %q kept (%s)", e.path, name, reason)
			} else {
				t.Errorf("%s: v1-only member %q (gc lacks it): document in extra or remove", e.path, name)
			}
		}
	}
	// 3. Named types: method sets, struct exported fields, and other
	// underlyings must match (modulo method/field omissions).
	for _, name := range gc.Scope().Names() {
		if !ast.IsExported(name) {
			continue
		}
		gcNamed := namedType(gc, name)
		if gcNamed == nil {
			continue
		}
		v1Named := namedType(v1, name)
		if v1Named == nil {
			continue // already reported in step 1
		}
		gcMeths, v1Meths := exportedMethods(gcNamed), exportedMethods(v1Named)
		for mName, wantSig := range gcMeths {
			key := name + "." + mName
			if omitted[key] {
				if _, ok := v1Meths[mName]; ok {
					t.Errorf("%s: omission %q is stale: v1 now has it (%s)", e.path, key, e.omit[key])
				}
				continue
			}
			gotSig, ok := v1Meths[mName]
			if !ok {
				t.Errorf("%s: v1 %s missing method %s (gc has it)", e.path, name, mName)
				continue
			}
			if gotSig != wantSig {
				if reason, ok := e.accept[key]; ok {
					t.Logf("%s: accepted signature drift on %s.%s (gc %s, v1 %s): %s", e.path, name, mName, wantSig, gotSig, reason)
				} else {
					t.Errorf("%s: %s.%s signature drift:\n gc: %s\n v1: %s", e.path, name, mName, wantSig, gotSig)
				}
			}
		}
		for mName := range v1Meths {
			if _, ok := gcMeths[mName]; !ok {
				if reason, ok := e.extra[name+"."+mName]; ok {
					t.Logf("%s: v1-only method %s.%s kept (%s)", e.path, name, mName, reason)
				} else {
					t.Errorf("%s: v1-only method %s.%s (gc lacks it): document in extra or remove", e.path, name, mName)
				}
			}
		}
		gcSt, gcIsStruct := gcNamed.Underlying().(*types.Struct)
		v1St, v1IsStruct := v1Named.Underlying().(*types.Struct)
		if gcIsStruct && v1IsStruct {
			gcFields, v1Fields := exportedFields(gcSt), exportedFields(v1St)
			for fName, wantT := range gcFields {
				key := name + "." + fName
				if omitted[key] {
					if _, ok := v1Fields[fName]; ok {
						t.Errorf("%s: omission %q is stale: v1 now has it (%s)", e.path, key, e.omit[key])
					}
					continue
				}
				gotT, ok := v1Fields[fName]
				if !ok {
					t.Errorf("%s: v1 %s missing field %s (gc has it)", e.path, name, fName)
					continue
				}
				if gotT != wantT {
					if reason, ok := e.accept[key]; ok {
						t.Logf("%s: accepted field type drift on %s.%s (gc %s, v1 %s): %s", e.path, name, fName, wantT, gotT, reason)
					} else {
						t.Errorf("%s: %s.%s field type drift: gc %s, v1 %s", e.path, name, fName, wantT, gotT)
					}
				}
			}
			for fName := range v1Fields {
				if _, ok := gcFields[fName]; !ok {
					if reason, ok := e.extra[name+"."+fName]; ok {
						t.Logf("%s: v1-only field %s.%s kept (%s)", e.path, name, fName, reason)
					} else {
						t.Errorf("%s: v1-only field %s.%s (gc lacks it): document in extra or remove", e.path, name, fName)
					}
				}
			}
		} else if anonTypeString(gcNamed.Underlying()) != anonTypeString(v1Named.Underlying()) {
			// Interfaces and all other underlyings render completely:
			// a difference here is a real API difference (for structs
			// the field-by-field check above applies instead, since
			// unexported fields are implementation detail).
			t.Errorf("%s: %s underlying drift:\n gc: %s\n v1: %s", e.path, name, anonTypeString(gcNamed.Underlying()), anonTypeString(v1Named.Underlying()))
		}
	}
	// 4. Every omission key must have been consumed by steps 1-3.
	// (Package omissions are consumed in step 1; method/field
	// omissions in step 3. A key consumed nowhere is a dangling entry:
	// resolveOmission already proved it exists in gc, so a key that
	// reaches here was never checked because its type is missing
	// from v1 — which step 1 already reported.)
}

func TestDriftV1AgainstGoroot(t *testing.T) {
	for _, e := range driftPackages {
		e := e
		t.Run(e.path, func(t *testing.T) {
			assertDrift(t, e)
		})
	}
}
