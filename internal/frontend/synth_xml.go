// P3-M7 (wave 1): synthetic `encoding/xml` package.
//
// REAL Go source, embedded as text and type-checked on demand with the
// overlay importer (no imports — the package is self-contained, by the
// errors/sort precedent). The resulting *types.Package is what
// overlayImporter.Import("encoding/xml") returns, so the user's
// type-check and the synthetic source's own type-check share identical
// *types.Func/*types.TypeName objects; the exporter then emits the
// synthetic declarations' bodies into the same wire document (shared
// type/object tables) via exportSyntheticPackages.
//
// SCOPE (honest subset — the full rationale is in
// phases/phase3/hidden_files/m7-enc-xml/redgreen.md and the source's own
// M7 notes):
//   - IN: the token stream — Decoder.Token/RawToken (desugared; see
//     mrdesugar_m5.go mrXmlToken), the full XML grammar (byte-exact vs
//     gc, error strings included), Encoder.EncodeToken over a Writer,
//     EscapeText/Escape, token types + Copy/CopyToken, SyntaxError,
//     Skip, InputOffset, HTMLEntity/HTMLAutoClose, the
//     Marshaler/MarshalerAttr/Unmarshaler/UnmarshalerAttr/UnmarshalError
//     declarations.
//   - OUT (fail fast, never silent): Unmarshal/Marshal/MarshalIndent/
//     Decode/DecodeElement/Encode/EncodeElement (need reflect, P3-M8);
//     InputPos (multi-result, WS D); Decoder.CharsetReader (needs io,
//     P3-M8); NewTokenDecoder for non-*Decoder readers (needs WS D).
//   - P3-M8: io landed, but NewDecoder keeps the structural Reader
//     interface (single-result Next) and NewEncoder/EscapeText/Escape
//     keep Writer. Switching to io.Reader/io.Writer needs 2-result
//     interface dispatch (r.Read/w.Write in the decoder/encoder bodies —
//     WS D), not just nameable signatures. Re-test when WS D lands;
//     until then the stand-ins are the honest subset (the corpus
//     dual-mode reader/writer documents the migration).
//   - v1 cannot call multi-result functions (WS D): Token/RawToken keep
//     exact gc signatures with panic bodies; 2-target := and = call
//     sites desugar to the __tokenOf/__rawTokenOf single-result helpers.
//     Every other position fails fast with a named scope error.
//
// The grammar is a faithful port of gc 1.27.1 encoding/xml's rawToken,
// text, and Token (namespace translation + element stack). `io` is
// replaced by the chunkless Reader cursor, bytes/strconv/strings/unicode
// by small inlined helpers, and the unicode name tables by compressed
// [lo,hi,stride] triples (all gc strides preserved).
package main

// syntheticXmlSource is the P3-M7 synthetic `encoding/xml` package.
const syntheticXmlSource = `package xml

// =====================================================================
// M7 structural I/O.
//
// 'io' is a P3-M8 wave-2 package: the synthetic source cannot import it,
// so 'io.Reader'/'io.Writer' cannot appear in signatures. The M7 honest
// subset uses these structural stand-ins instead:
//
//   - Reader: any program-defined type with Next() ReadResult. Under gc,
//     the same program-defined type additionally implements Read (so it
//     satisfies io.Reader and gc's NewDecoder accepts it); under graalgo
//     the synthetic NewDecoder accepts it via Reader. One call site,
//     shared source, both implementations.
//   - Writer: any program-defined type with WriteBytes(p []byte); under gc
//     it additionally implements Write (io.Writer).
//
// v1 cannot emit or call multi-result methods (WS D), so the interfaces
// use single-result methods only. P3-M8 re-test (WS D, not io — io has
// landed): switch NewDecoder, NewEncoder, EscapeText and Escape to
// io.Reader/io.Writer and retire Reader/Writer/ReadResult (call sites
// keep working: every io.Reader with a Next method... — see redgreen.md;
// the corpus dual-mode reader/writer documents the migration).
// =====================================================================

// ReadResult is the single-result carrier for Reader.Next: one input
// byte, or OK == false at end of input (sticky: the decoder stops at
// the first !OK).
type ReadResult struct {
	Byte byte
	OK   bool
}

// Reader is the M7 stand-in for io.Reader (see above).
type Reader interface {
	Next() ReadResult
}

// Writer is the M7 stand-in for io.Writer (see above).
type Writer interface {
	WriteBytes(p []byte)
}

// NewDecoder creates a new XML parser reading from r. Strict defaults
// to true, as in gc.
func NewDecoder(r Reader) *Decoder {
	d := &Decoder{
		r:        r,
		ns:       make(map[string]string),
		nextByte: -1,
		line:     1,
		Strict:   true,
	}
	return d
}

// =====================================================================
// Errors.
// =====================================================================

// xmlError is the internal error type (gc uses fmt.Errorf/errors.New;
// the messages are identical).
type xmlError struct {
	msg string
}

func (e *xmlError) Error() string { return e.msg }

// errXMLEOF is the M7 EOF sentinel (gc: io.EOF). It renders as "EOF",
// so rendered-error comparisons (the corpus) match gc byte-for-byte.
// Identity comparison against io.EOF is impossible without io; re-test
// P3-M8: Token must return io.EOF itself.
var errXMLEOF = &xmlError{msg: "EOF"}

// A SyntaxError represents a syntax error in the XML input stream.
type SyntaxError struct {
	Msg  string
	Line int
}

func (e *SyntaxError) Error() string {
	return "XML syntax error on line " + xmlItoa(e.Line) + ": " + e.Msg
}

// xmlItoa is strconv.Itoa (strconv is a parallel M7 worker; the package
// is self-contained by the errors/sort precedent).
func xmlItoa(v int) string {
	neg := v < 0
	var u uint
	if neg {
		u = uint(-(v + 1)) + 1
	} else {
		u = uint(v)
	}
	var arr [20]byte
	w := len(arr)
	if u == 0 {
		w--
		arr[w] = '0'
	} else {
		for u > 0 {
			w--
			arr[w] = byte(u%10) + '0'
			u /= 10
		}
	}
	if neg {
		w--
		arr[w] = '-'
	}
	return string(arr[w:])
}

// xmlQuote renders s as a Go double-quoted literal (the %q verb), for
// the two fmt.Errorf("...%q...") sites in the decoder prolog.
func xmlQuote(s string) string {
	b := make([]byte, 0, len(s)+2)
	b = append(b, '"')
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch c {
		case '"':
			b = append(b, '\\', '"')
		case '\\':
			b = append(b, '\\', '\\')
		case '\n':
			b = append(b, '\\', 'n')
		case '\r':
			b = append(b, '\\', 'r')
		case '\t':
			b = append(b, '\\', 't')
		default:
			if c < 0x20 || c == 0x7f {
				b = append(b, '\\', 'x')
				b = append(b, "0123456789abcdef"[c>>4], "0123456789abcdef"[c&0xf])
			} else {
				b = append(b, c)
			}
		}
	}
	b = append(b, '"')
	return string(b)
}

// xmlFormatCharCode renders r in %U form ("U+0041", uppercase hex,
// minimum 4 digits), for the "illegal character code %U" error.
func xmlFormatCharCode(r rune) string {
	var hex [6]byte
	w := len(hex)
	u := uint32(r)
	if u == 0 {
		w--
		hex[w] = '0'
	} else {
		for u > 0 {
			w--
			hex[w] = "0123456789ABCDEF"[u&0xf]
			u >>= 4
		}
	}
	for len(hex)-w < 4 {
		w--
		hex[w] = '0'
	}
	return "U+" + string(hex[w:])
}

// =====================================================================
// Token types (exact gc 1.27.1 API).
// =====================================================================

// A Name represents an XML name (Local) annotated
// with a name space identifier (Space).
type Name struct {
	Space, Local string
}

// An Attr represents an attribute in an XML element (Name=Value).
type Attr struct {
	Name  Name
	Value string
}

// A Token is an interface holding one of the token types:
// [StartElement], [EndElement], [CharData], [Comment], [ProcInst], or [Directive].
type Token any

// A StartElement represents an XML start element.
type StartElement struct {
	Name Name
	Attr []Attr
}

// Copy creates a new copy of StartElement.
func (e StartElement) Copy() StartElement {
	attrs := make([]Attr, len(e.Attr))
	copy(attrs, e.Attr)
	e.Attr = attrs
	return e
}

// End returns the corresponding XML end element.
func (e StartElement) End() EndElement {
	return EndElement{e.Name}
}

// An EndElement represents an XML end element.
type EndElement struct {
	Name Name
}

// A CharData represents XML character data (raw text),
// in which XML escape sequences have been replaced by
// the characters they represent.
type CharData []byte

// Copy creates a new copy of CharData.
func (c CharData) Copy() CharData { return CharData(xmlClone(c)) }

// A Comment represents an XML comment of the form <!--comment-->.
// The bytes do not include the <!-- and --> comment markers.
type Comment []byte

// Copy creates a new copy of Comment.
func (c Comment) Copy() Comment { return Comment(xmlClone(c)) }

// A ProcInst represents an XML processing instruction of the form <?target inst?>
type ProcInst struct {
	Target string
	Inst   []byte
}

// Copy creates a new copy of ProcInst.
func (p ProcInst) Copy() ProcInst {
	p.Inst = xmlClone(p.Inst)
	return p
}

// A Directive represents an XML directive of the form <!text>.
// The bytes do not include the <! and > markers.
type Directive []byte

// Copy creates a new copy of Directive.
func (d Directive) Copy() Directive { return Directive(xmlClone(d)) }

// CopyToken returns a copy of a Token.
func CopyToken(t Token) Token {
	switch v := t.(type) {
	case CharData:
		return v.Copy()
	case Comment:
		return v.Copy()
	case Directive:
		return v.Copy()
	case ProcInst:
		return v.Copy()
	case StartElement:
		return v.Copy()
	}
	return t
}

// xmlClone is bytes.Clone (self-contained; see xmlItoa).
func xmlClone(b []byte) []byte {
	if b == nil {
		return nil
	}
	c := make([]byte, len(b))
	copy(c, b)
	return c
}

// A TokenReader is anything that can decode a stream of XML tokens,
// including a [Decoder].
//
// The 2-result Token method is declared for exact gc API fidelity; v1
// cannot emit or dispatch multi-result methods (WS D), so calling Token
// through this interface fails fast. The only v1-constructible
// TokenReader is *Decoder itself (user types cannot declare 2-result
// methods); use NewTokenDecoder for the gc fast path. Re-test WS D.
type TokenReader interface {
	Token() (Token, error)
}

// NewTokenDecoder creates a new XML parser reading from t. The gc fast
// path (t already a *Decoder) is real; any other TokenReader needs WS D
// to call Token and fails fast. Re-test WS D.
func NewTokenDecoder(t TokenReader) *Decoder {
	if d, ok := t.(*Decoder); ok {
		return d
	}
	panic("xml.NewTokenDecoder: not in M7 scope (calling TokenReader.Token needs 2-result interface dispatch)")
}

// =====================================================================
// Decoder.
// =====================================================================

// A Decoder represents an XML parser reading a particular input stream.
// The parser assumes that its input is encoded in UTF-8.
//
// M7 scope notes:
//   - CharsetReader is OMITTED: its type mentions io.Reader (io is
//     P3-M8). A non-UTF-8 encoding declaration therefore always fails
//     exactly as gc fails with a nil CharsetReader:
//     'xml: encoding %q declared but Decoder.CharsetReader is nil'.
//     P3-M8: io landed so the type is nameable, but calling the field
//     needs 2-result dispatch (WS D); declaring it without calling it
//     would silently drop user charsets. Re-test when WS D lands.
//   - The Unmarshal stack (stk/free/saved/inUnmarshalXML) is omitted:
//     Decode/DecodeElement/Unmarshal need reflect (P3-M8) and fail fast.
//     The Token()-level element/namespace stack below is the part of
//     gc's stack machinery the token stream needs.
type Decoder struct {
	// Strict defaults to true, enforcing the requirements
	// of the XML specification. See gc's doc comment (xml.go) for the
	// Strict == false allowances.
	Strict bool

	// AutoClose, Entity, DefaultSpace: exact gc semantics (see gc doc).
	AutoClose []string
	Entity    map[string]string
	DefaultSpace string

	r        Reader   // M7 structural input (see Reader)
	xbuf     []byte   // scratch buffer (gc: d.buf bytes.Buffer)
	nextByte int      // ungetc slot; -1 = empty (gc: d.nextByte)
	eof      bool     // underlying Reader exhausted (sticky)
	stk      *xmlStack
	stkDepth int
	needClose bool   // last element was self-closing; EndElement pending
	toClose   Name
	nextToken Token
	ns        map[string]string // prefix -> URL (Token namespace translation)
	err       error
	line      int
	linestart int64
	offset    int64
}

// tokenPair is the single-result carrier for the __tokenOf/__rawTokenOf
// desugar targets (v1 cannot call multi-result functions, WS D — the
// P3-M5 deadlinePair pattern).
type tokenPair struct {
	tok Token
	err error
}

// The following pair types are the single-result carriers for the
// decoder's internal multi-result helpers. v1 cannot emit multi-result
// calls at all (WS D), so even unexported helpers use them; only the
// public API keeps exact gc 2-result signatures (with panic bodies).
type autoPair struct {
	tok Token
	ok  bool
}

type getcPair struct {
	b  byte
	ok bool
}

type namePair struct {
	name Name
	ok   bool
}

type stringPair struct {
	s  string
	ok bool
}

type runePair struct {
	r rune
	n int
}

type uint64ErrPair struct {
	v   uint64
	err error
}

type cutTriple struct {
	before string
	after  string
	found  bool
}

type endElemPair struct {
	t  EndElement
	ok bool
}

// Token returns the next XML token in the input stream.
// At the end of the input stream, Token returns nil and an error
// rendering as "EOF" (gc: io.EOF — identity needs io; re-test P3-M8).
//
// Exact gc 1.27.1 signature for type-checking. v1 cannot call
// multi-result functions (WS D): 2-target ':=' and '=' call sites are
// desugared to __tokenOf (mrdesugar_m5.go, mrXmlToken); every other
// position fails fast here. The body never runs.
func (d *Decoder) Token() (Token, error) {
	panic("xml.Decoder.Token: only 2-target := and = call sites are desugared in M7")
}

// RawToken is like [Decoder.Token] but does not verify that start and
// end elements match and does not translate namespace prefixes to their
// corresponding URLs. Same M7 desugar story as Token.
func (d *Decoder) RawToken() (Token, error) {
	panic("xml.Decoder.RawToken: only 2-target := and = call sites are desugared in M7")
}

// __tokenOf is the desugar target behind Decoder.Token: the real gc
// Token logic (namespace translation, element-stack matching,
// Strict==false auto-close), returning a single tokenPair. It cannot
// call d.Token: a 2-target call would recurse into the desugar, any
// other position fails fast (the __parseDuration precedent).
func __tokenOf(d *Decoder) tokenPair {
	var t Token
	var err error
	if d.stk != nil && d.stk.kind == stkEOF {
		return tokenPair{err: errXMLEOF}
	}
	if d.nextToken != nil {
		t = d.nextToken
		d.nextToken = nil
	} else {
		tr := __rawTokenOf(d)
		t, err = tr.tok, tr.err
		if t == nil && err != nil {
			if err == errXMLEOF && d.stk != nil && d.stk.kind != stkEOF {
				err = d.syntaxError("unexpected EOF")
			}
			return tokenPair{err: err}
		}
		// We still have a token to process, so clear any
		// errors (e.g. EOF) and proceed.
		err = nil
	}
	if !d.Strict {
		if ap := d.autoClose(t); ap.ok {
			d.nextToken = t
			t = ap.tok
		}
	}
	switch t1 := t.(type) {
	case StartElement:
		// In XML name spaces, the translations listed in the
		// attributes apply to the element name and
		// to the other attribute names, so process
		// the translations first.
		for _, a := range t1.Attr {
			if a.Name.Space == xmlnsPrefix {
				v, ok := d.ns[a.Name.Local]
				d.pushNs(a.Name.Local, v, ok)
				d.ns[a.Name.Local] = a.Value
			}
			if a.Name.Space == "" && a.Name.Local == xmlnsPrefix {
				// Default space for untagged names
				v, ok := d.ns[""]
				d.pushNs("", v, ok)
				d.ns[""] = a.Value
			}
		}

		d.pushElement(t1.Name)
		t1.Name = d.translate(t1.Name, true)
		for i := range t1.Attr {
			t1.Attr[i].Name = d.translate(t1.Attr[i].Name, false)
		}
		t = t1

	case EndElement:
		pp := d.popElement(t1)
		if !pp.ok {
			return tokenPair{err: d.err}
		}
		t = pp.t
	}
	return tokenPair{tok: t}
}

const (
	xmlURL      = "http://www.w3.org/XML/1998/namespace"
	xmlnsPrefix = "xmlns"
	xmlPrefix   = "xml"
)

// Apply name space translation to name n.
// The default name space (for Space=="")
// applies only to element names, not to attribute names.
// M7: takes Name by value and returns the translated Name (v1 cannot
// take the address of a struct field, WS D).
func (d *Decoder) translate(n Name, isElementName bool) Name {
	switch {
	case n.Space == xmlnsPrefix:
		return n
	case n.Space == "" && !isElementName:
		return n
	case n.Space == xmlPrefix:
		n.Space = xmlURL
	case n.Space == "" && n.Local == xmlnsPrefix:
		return n
	}
	if v, ok := d.ns[n.Space]; ok {
		n.Space = v
	} else if n.Space == "" {
		n.Space = d.DefaultSpace
	}
	return n
}

// xmlStack is gc's stack (element records + namespace undo records).
// The free-list is dropped (unobservable allocation optimization);
// pushEOF/popEOF are omitted (only reachable from the reflect-driven
// unmarshal paths, which fail fast in M7 — re-test P3-M8).
type xmlStack struct {
	next *xmlStack
	kind int
	name Name
	ok   bool
}

const (
	stkStart = iota
	stkNs
	stkEOF
)

func (d *Decoder) push(kind int) *xmlStack {
	s := &xmlStack{next: d.stk, kind: kind}
	if kind == stkStart {
		d.stkDepth++
	}
	d.stk = s
	return s
}

func (d *Decoder) pop() *xmlStack {
	s := d.stk
	if s != nil {
		if s.kind == stkStart {
			d.stkDepth--
		}
		d.stk = s.next
	}
	return s
}

// Record that we are starting an element with the given name.
func (d *Decoder) pushElement(name Name) {
	s := d.push(stkStart)
	s.name = name
}

// Record that we are changing the value of ns[local].
// The old value is url, ok.
func (d *Decoder) pushNs(local string, url string, ok bool) {
	s := d.push(stkNs)
	s.name.Local = local
	s.name.Space = url
	s.ok = ok
}

// Creates a SyntaxError with the current line number.
func (d *Decoder) syntaxError(msg string) error {
	return &SyntaxError{Msg: msg, Line: d.line}
}

// Record that we are ending an element with the given name.
// The name must match the record at the top of the stack,
// which must be a pushElement record.
// After popping the element, apply any undo records from
// the stack to restore the name translations that existed
// before we saw this element.
// M7: takes EndElement by value and returns endElemPair (v1 cannot
// take the address of a local variable, WS D).
func (d *Decoder) popElement(t EndElement) endElemPair {
	s := d.pop()
	name := t.Name
	switch {
	case s == nil || s.kind != stkStart:
		d.err = d.syntaxError("unexpected end element </" + name.Local + ">")
		return endElemPair{t, false}
	case s.name.Local != name.Local:
		if !d.Strict {
			d.needClose = true
			d.toClose = t.Name
			t.Name = s.name
			return endElemPair{t, true}
		}
		d.err = d.syntaxError("element <" + s.name.Local + "> closed by </" + name.Local + ">")
		return endElemPair{t, false}
	case s.name.Space != name.Space:
		ns := name.Space
		if name.Space == "" {
			ns = "\"\""
		}
		d.err = d.syntaxError("element <" + s.name.Local + "> in space " + s.name.Space +
			" closed by </" + name.Local + "> in space " + ns)
		return endElemPair{t, false}
	}

	t.Name = d.translate(t.Name, true)

	// Pop stack until a Start or EOF is on the top, undoing the
	// translations that were associated with the element we just closed.
	for d.stk != nil && d.stk.kind != stkStart && d.stk.kind != stkEOF {
		s := d.pop()
		if s.ok {
			d.ns[s.name.Local] = s.name.Space
		} else {
			delete(d.ns, s.name.Local)
		}
	}

	return endElemPair{t, true}
}

// If the top element on the stack is autoclosing and
// t is not the end tag, invent the end tag.
// M7: single autoPair result (v1 cannot emit multi-result calls, WS D).
func (d *Decoder) autoClose(t Token) autoPair {
	if d.stk == nil || d.stk.kind != stkStart {
		return autoPair{nil, false}
	}
	for _, s := range d.AutoClose {
		if xmlEqualFold(s, d.stk.name.Local) {
			// This one should be auto closed if t doesn't close it.
			et, ok := t.(EndElement)
			if !ok || !xmlEqualFold(et.Name.Local, d.stk.name.Local) {
				return autoPair{EndElement{d.stk.name}, true}
			}
			break
		}
	}
	return autoPair{nil, false}
}

// Read a single byte.
// If there is no byte to read, return ok==false
// and leave the error in d.err.
// Maintain line number.
//
// M7: single getcPair result (v1 cannot emit multi-result calls, WS D).
func (d *Decoder) getc() getcPair {
	var b byte
	if d.err != nil {
		return getcPair{0, false}
	}
	if d.nextByte >= 0 {
		b = byte(d.nextByte)
		d.nextByte = -1
	} else {
		rr := d.r.Next()
		if !rr.OK {
			d.eof = true
			d.err = errXMLEOF
			return getcPair{0, false}
		}
		b = rr.Byte
	}
	if b == '\n' {
		d.line++
		d.linestart = d.offset + 1
	}
	d.offset++
	return getcPair{b, true}
}

// InputOffset returns the input stream byte offset of the current decoder position.
// The offset gives the location of the end of the most recently returned token
// and the beginning of the next token.
func (d *Decoder) InputOffset() int64 {
	return d.offset
}

// InputPos returns the line of the current decoder position and the 1 based
// input position of the line.
//
// M7: exact gc signature for type-checking, fail-fast body — 2 results
// need WS D (the mrXmlToken desugar only covers Token/RawToken).
// Re-test when WS D lands.
func (d *Decoder) InputPos() (line, column int) {
	panic("xml.Decoder.InputPos: not in M7 scope (no Appendix-M table entry; needs __-helper + desugar coverage)")
}

// Must read a single byte.
// If there is no byte to read,
// set d.err to SyntaxError("unexpected EOF")
// and return ok==false
func (d *Decoder) mustgetc() getcPair {
	gp := d.getc()
	if !gp.ok {
		if d.err == errXMLEOF {
			d.err = d.syntaxError("unexpected EOF")
		}
	}
	return gp
}

// Unread a single byte.
func (d *Decoder) ungetc(b byte) {
	if b == '\n' {
		d.line--
	}
	d.nextByte = int(b)
	d.offset--
}

// Skip skips the current element's subtree: it consumes tokens until it
// has consumed the end element matching the current start element (or,
// if not inside an element, the next end element). Reflect-free (drives
// __tokenOf directly), so it is real in M7.
func (d *Decoder) Skip() error {
	var depth int64
	for {
		tr := __tokenOf(d)
		if tr.err != nil {
			return tr.err
		}
		switch tr.tok.(type) {
		case StartElement:
			depth++
		case EndElement:
			if depth == 0 {
				return nil
			}
			depth--
		}
	}
}

// __rawTokenOf is the desugar target behind Decoder.RawToken: the real
// gc rawToken logic (XML grammar), returning a single tokenPair.
func __rawTokenOf(d *Decoder) tokenPair {
	fail := func() tokenPair { return tokenPair{err: d.err} }
	if d.err != nil {
		return fail()
	}
	if d.needClose {
		// The last element we read was self-closing and
		// we returned just the StartElement half.
		// Return the EndElement half now.
		d.needClose = false
		return tokenPair{tok: EndElement{d.toClose}}
	}

	__gp1 := d.getc()
	b, ok := __gp1.b, __gp1.ok
	if !ok {
		return fail()
	}

	if b != '<' {
		// Text section.
		d.ungetc(b)
		data := d.text(-1, false)
		if data == nil {
			return fail()
		}
		return tokenPair{tok: CharData(data)}
	}

	__gp2 := d.mustgetc()
	b, ok = __gp2.b, __gp2.ok
	if !ok {
		return fail()
	}
	switch b {
	case '/':
		// </: End element
		var name Name
		__np1 := d.nsname()
		name, ok = __np1.name, __np1.ok
		if !ok {
			if d.err == nil {
				d.err = d.syntaxError("expected element name after </")
			}
			return fail()
		}
		d.space()
		__gp3 := d.mustgetc()
		b, ok = __gp3.b, __gp3.ok
		if !ok {
			return fail()
		}
		if b != '>' {
			d.err = d.syntaxError("invalid characters between </" + name.Local + " and >")
			return fail()
		}
		return tokenPair{tok: EndElement{name}}

	case '?':
		// <?: Processing instruction.
		var target string
		__sp4 := d.name()
		target, ok = __sp4.s, __sp4.ok
		if !ok {
			if d.err == nil {
				d.err = d.syntaxError("expected target name after <?")
			}
			return fail()
		}
		d.space()
		d.xbuf = d.xbuf[:0]
		var b0 byte
		for {
			__gp4 := d.mustgetc()
			b, ok = __gp4.b, __gp4.ok
			if !ok {
				return fail()
			}
			d.xbuf = append(d.xbuf, b)
			if b0 == '?' && b == '>' {
				break
			}
			b0 = b
		}
		data := d.xbuf
		data = data[0 : len(data)-2] // chop ?>

		if target == "xml" {
			content := string(data)
			ver := procInst("version", content)
			if ver != "" && ver != "1.0" {
				d.err = &xmlError{msg: "xml: unsupported version " + xmlQuote(ver) + "; only version 1.0 is supported"}
				return fail()
			}
			enc := procInst("encoding", content)
			if enc != "" && enc != "utf-8" && enc != "UTF-8" && !xmlEqualFold(enc, "utf-8") {
				// M7: CharsetReader is omitted (needs io); gc with a nil
				// CharsetReader fails exactly this way.
				d.err = &xmlError{msg: "xml: encoding " + xmlQuote(enc) + " declared but Decoder.CharsetReader is nil"}
				return fail()
			}
		}
		return tokenPair{tok: ProcInst{target, data}}

	case '!':
		// <!: Maybe comment, maybe CDATA.
		__gp5 := d.mustgetc()
		b, ok = __gp5.b, __gp5.ok
		if !ok {
			return fail()
		}
		switch b {
		case '-': // <!-
			// Probably <!-- for a comment.
			__gp6 := d.mustgetc()
			b, ok = __gp6.b, __gp6.ok
			if !ok {
				return fail()
			}
			if b != '-' {
				d.err = d.syntaxError("invalid sequence <!- not part of <!--")
				return fail()
			}
			// Look for terminator.
			d.xbuf = d.xbuf[:0]
			var b0, b1 byte
			for {
				__gp7 := d.mustgetc()
				b, ok = __gp7.b, __gp7.ok
				if !ok {
					return fail()
				}
				d.xbuf = append(d.xbuf, b)
				if b0 == '-' && b1 == '-' {
					if b != '>' {
						d.err = d.syntaxError(
							"invalid sequence \"--\" not allowed in comments")
						return fail()
					}
					break
				}
				b0, b1 = b1, b
			}
			data := d.xbuf
			data = data[0 : len(data)-3] // chop -->
			return tokenPair{tok: Comment(data)}

		case '[': // <![
			// Probably <![CDATA[.
			for i := 0; i < 6; i++ {
				__gp8 := d.mustgetc()
				b, ok = __gp8.b, __gp8.ok
				if !ok {
					return fail()
				}
				if b != "CDATA["[i] {
					d.err = d.syntaxError("invalid <![ sequence")
					return fail()
				}
			}
			// Have <![CDATA[.  Read text until ]]>.
			data := d.text(-1, true)
			if data == nil {
				return fail()
			}
			return tokenPair{tok: CharData(data)}
		}

		// Probably a directive: <!DOCTYPE ...>, <!ENTITY ...>, etc.
		// We don't care, but accumulate for caller. Quoted angle
		// brackets do not count for nesting.
		d.xbuf = d.xbuf[:0]
		d.xbuf = append(d.xbuf, b)
		inquote := uint8(0)
		depth := 0
		for {
			__gp9 := d.mustgetc()
			b, ok = __gp9.b, __gp9.ok
			if !ok {
				return fail()
			}
			if inquote == 0 && b == '>' && depth == 0 {
				break
			}
		HandleB:
			d.xbuf = append(d.xbuf, b)
			switch {
			case b == inquote:
				inquote = 0

			case inquote != 0:
				// in quotes, no special action

			case b == '\'' || b == '"':
				inquote = b

			case b == '>' && inquote == 0:
				depth--

			case b == '<' && inquote == 0:
				// Look for <!-- to begin comment.
				s := "!--"
				for i := 0; i < len(s); i++ {
					__gp10 := d.mustgetc()
					b, ok = __gp10.b, __gp10.ok
					if !ok {
						return fail()
					}
					if b != s[i] {
						for j := 0; j < i; j++ {
							d.xbuf = append(d.xbuf, s[j])
						}
						depth++
						goto HandleB
					}
				}

				// Remove < that was written above.
				d.xbuf = d.xbuf[:len(d.xbuf)-1]

				// Look for terminator.
				var b0, b1 byte
				for {
					__gp11 := d.mustgetc()
					b, ok = __gp11.b, __gp11.ok
					if !ok {
						return fail()
					}
					if b0 == '-' && b1 == '-' && b == '>' {
						break
					}
					b0, b1 = b1, b
				}

				// Replace the comment with a space in the returned Directive
				// body, so that markup parts that were separated by the comment
				// (like a "<" and a "!") don't get joined when re-encoding the
				// Directive, taking new semantic meaning.
				d.xbuf = append(d.xbuf, ' ')
			}
		}
		return tokenPair{tok: Directive(d.xbuf)}
	}

	// Must be an open element like <a href="foo">
	d.ungetc(b)

	var (
		name  Name
		empty bool
		attr  []Attr
	)
	__np2 := d.nsname()
	name, ok = __np2.name, __np2.ok
	if !ok {
		if d.err == nil {
			d.err = d.syntaxError("expected element name after <")
		}
		return fail()
	}

	attr = []Attr{}
	for {
		d.space()
		__gp12 := d.mustgetc()
		b, ok = __gp12.b, __gp12.ok
		if !ok {
			return fail()
		}
		if b == '/' {
			empty = true
			__gp13 := d.mustgetc()
			b, ok = __gp13.b, __gp13.ok
			if !ok {
				return fail()
			}
			if b != '>' {
				d.err = d.syntaxError("expected /> in element")
				return fail()
			}
			break
		}
		if b == '>' {
			break
		}
		d.ungetc(b)

		a := Attr{}
		__np3 := d.nsname()
		a.Name, ok = __np3.name, __np3.ok
		if !ok {
			if d.err == nil {
				d.err = d.syntaxError("expected attribute name in element")
			}
			return fail()
		}
		d.space()
		__gp14 := d.mustgetc()
		b, ok = __gp14.b, __gp14.ok
		if !ok {
			return fail()
		}
		if b != '=' {
			if d.Strict {
				d.err = d.syntaxError("attribute name without = in element")
				return fail()
			}
			d.ungetc(b)
			a.Value = a.Name.Local
		} else {
			d.space()
			data := d.attrval()
			if data == nil {
				return fail()
			}
			a.Value = string(data)
		}
		attr = append(attr, a)
	}
	if empty {
		d.needClose = true
		d.toClose = name
	}
	return tokenPair{tok: StartElement{name, attr}}
}

func (d *Decoder) attrval() []byte {
	__gp15 := d.mustgetc()
	b, ok := __gp15.b, __gp15.ok
	if !ok {
		return nil
	}
	// Handle quoted attribute values
	if b == '"' || b == '\'' {
		return d.text(int(b), false)
	}
	// Handle unquoted attribute values for strict parsers
	if d.Strict {
		d.err = d.syntaxError("unquoted or missing attribute value in element")
		return nil
	}
	// Handle unquoted attribute values for unstrict parsers
	d.ungetc(b)
	d.xbuf = d.xbuf[:0]
	for {
		__gp16 := d.mustgetc()
		b, ok = __gp16.b, __gp16.ok
		if !ok {
			return nil
		}
		// https://www.w3.org/TR/REC-html40/intro/sgmltut.html#h-3.2.2
		if 'a' <= b && b <= 'z' || 'A' <= b && b <= 'Z' ||
			'0' <= b && b <= '9' || b == '_' || b == ':' || b == '-' {
			d.xbuf = append(d.xbuf, b)
		} else {
			d.ungetc(b)
			break
		}
	}
	return d.xbuf
}

// Skip spaces if any
func (d *Decoder) space() {
	for {
		__gp17 := d.getc()
		b, ok := __gp17.b, __gp17.ok
		if !ok {
			return
		}
		switch b {
		case ' ', '\r', '\n', '\t':
		default:
			d.ungetc(b)
			return
		}
	}
}

var xmlEntity = map[string]rune{
	"lt":   '<',
	"gt":   '>',
	"amp":  '&',
	"apos": '\'',
	"quot": '"',
}

// Read plain text section (XML calls it character data).
// If quote >= 0, we are in a quoted string and need to find the matching quote.
// If cdata == true, we are in a <![CDATA[ section and need to find ]]>.
// On failure return nil and leave the error in d.err.
func (d *Decoder) text(quote int, cdata bool) []byte {
	var b0, b1 byte
	var trunc int
	d.xbuf = d.xbuf[:0]
Input:
	for {
		__gp18 := d.getc()
		b, ok := __gp18.b, __gp18.ok
		if !ok {
			if cdata {
				if d.err == errXMLEOF {
					d.err = d.syntaxError("unexpected EOF in CDATA section")
				}
				return nil
			}
			break Input
		}

		// <![CDATA[ section ends with ]]>.
		// It is an error for ]]> to appear in ordinary text,
		// but it is allowed in quoted strings.
		if quote < 0 && b0 == ']' && b1 == ']' && b == '>' {
			if cdata {
				trunc = 2
				break Input
			}
			d.err = d.syntaxError("unescaped ]]> not in CDATA section")
			return nil
		}

		// Stop reading text if we see a <.
		if b == '<' && !cdata {
			if quote >= 0 {
				d.err = d.syntaxError("unescaped < inside quoted string")
				return nil
			}
			d.ungetc('<')
			break Input
		}
		if quote >= 0 && b == byte(quote) {
			break Input
		}
		if b == '&' && !cdata {
			// Read escaped character expression up to semicolon.
			// XML in all its glory allows a document to define and use
			// its own character names with <!ENTITY ...> directives.
			// Parsers are required to recognize lt, gt, amp, apos, and quot
			// even if they have not been declared.
			before := len(d.xbuf)
			d.xbuf = append(d.xbuf, '&')
			var ok bool
			var textb []byte
			var haveText bool
			__gp19 := d.mustgetc()
			b, ok = __gp19.b, __gp19.ok
			if !ok {
				return nil
			}
			if b == '#' {
				d.xbuf = append(d.xbuf, b)
				__gp20 := d.mustgetc()
				b, ok = __gp20.b, __gp20.ok
				if !ok {
					return nil
				}
				base := 10
				if b == 'x' {
					base = 16
					d.xbuf = append(d.xbuf, b)
					__gp21 := d.mustgetc()
					b, ok = __gp21.b, __gp21.ok
					if !ok {
						return nil
					}
				}
				start := len(d.xbuf)
				for '0' <= b && b <= '9' ||
					base == 16 && 'a' <= b && b <= 'f' ||
					base == 16 && 'A' <= b && b <= 'F' {
					d.xbuf = append(d.xbuf, b)
					__gp22 := d.mustgetc()
					b, ok = __gp22.b, __gp22.ok
					if !ok {
						return nil
					}
				}
				if b != ';' {
					d.ungetc(b)
				} else {
					s := string(d.xbuf[start:])
					d.xbuf = append(d.xbuf, ';')
					__up1 := xmlParseUint(s, base)
					n, perr := __up1.v, __up1.err
					if perr == nil && n <= xmlMaxRune {
						r := rune(n)
						if 0xD800 <= r && r <= 0xDFFF {
							// Go's string(rune) maps surrogates to
							// U+FFFD; do it explicitly (v1's
							// rune->string conversion is not
							// trusted for this edge).
							r = xmlRuneError
						}
						textb = xmlAppendRune(nil, r)
						haveText = true
					}
				}
			} else {
				d.ungetc(b)
				if !d.readName() {
					if d.err != nil {
						return nil
					}
				}
				__gp23 := d.mustgetc()
				b, ok = __gp23.b, __gp23.ok
				if !ok {
					return nil
				}
				if b != ';' {
					d.ungetc(b)
				} else {
					name := d.xbuf[before+1:]
					d.xbuf = append(d.xbuf, ';')
					if isName(name) {
						s := string(name)
						if r, ok := xmlEntity[s]; ok {
							textb = xmlAppendRune(nil, r)
							haveText = true
						} else if d.Entity != nil {
							var tv string
							tv, haveText = d.Entity[s]
							textb = []byte(tv)
						}
					}
				}
			}

			if haveText {
				d.xbuf = append(d.xbuf[:before], textb...)
				b0, b1 = 0, 0
				continue Input
			}
			if !d.Strict {
				b0, b1 = 0, 0
				continue Input
			}
			ent := string(d.xbuf[before:])
			if ent[len(ent)-1] != ';' {
				ent += " (no semicolon)"
			}
			d.err = d.syntaxError("invalid character entity " + ent)
			return nil
		}

		// We must rewrite unescaped \r and \r\n into \n.
		if b == '\r' {
			d.xbuf = append(d.xbuf, '\n')
		} else if b1 == '\r' && b == '\n' {
			// Skip \r\n--we already wrote \n.
		} else {
			d.xbuf = append(d.xbuf, b)
		}

		b0, b1 = b1, b
	}
	data := d.xbuf
	data = data[0 : len(data)-trunc]

	// Inspect each rune for being a disallowed character.
	buf := data
	for len(buf) > 0 {
		__rp1 := xmlDecodeRune(buf)
		r, size := __rp1.r, __rp1.n
		if r == xmlRuneError && size == 1 {
			d.err = d.syntaxError("invalid UTF-8")
			return nil
		}
		buf = buf[size:]
		if !isInCharacterRange(r) {
			d.err = d.syntaxError("illegal character code " + xmlFormatCharCode(r))
			return nil
		}
	}

	return data
}

// Decide whether the given rune is in the XML Character Range, per
// the Char production of https://www.xml.com/axml/testaxml.htm,
// Section 2.2 Characters.
func isInCharacterRange(r rune) (inrange bool) {
	return r == 0x09 ||
		r == 0x0A ||
		r == 0x0D ||
		r >= 0x20 && r <= 0xD7FF ||
		r >= 0xE000 && r <= 0xFFFD ||
		r >= 0x10000 && r <= 0x10FFFF
}

// Get name space name: name with a : stuck in the middle.
// The part before the : is the name space identifier.
//
// M7: single namePair result (v1 cannot emit multi-result calls, WS D).
func (d *Decoder) nsname() namePair {
	sp := d.name()
	if !sp.ok {
		return namePair{Name{}, false}
	}
	s := sp.s
	var name Name
	if xmlCount(s, ":") > 1 {
		return namePair{name, false}
	} else if ct := xmlCut(s, ":"); !ct.found || ct.before == "" || ct.after == "" {
		name.Local = s
	} else {
		name.Space = ct.before
		name.Local = ct.after
	}
	return namePair{name, true}
}

// Get name: /first(first|second)*/
// Do not set d.err if the name is missing (unless unexpected EOF is received):
// let the caller provide better context.
// M7: single stringPair result (v1 cannot emit multi-result calls, WS D).
func (d *Decoder) name() stringPair {
	d.xbuf = d.xbuf[:0]
	if !d.readName() {
		return stringPair{"", false}
	}

	// Now we check the characters.
	b := d.xbuf
	if !isName(b) {
		d.err = d.syntaxError("invalid XML name: " + string(b))
		return stringPair{"", false}
	}
	return stringPair{string(b), true}
}

// Read a name and append its bytes to d.xbuf.
// The name is delimited by any single-byte character not valid in names.
// All multi-byte characters are accepted; the caller must check their validity.
func (d *Decoder) readName() (ok bool) {
	var b byte
	__gp24 := d.mustgetc()
	b, ok = __gp24.b, __gp24.ok
	if !ok {
		return
	}
	if b < xmlRuneSelf && !isNameByte(b) {
		d.ungetc(b)
		return false
	}
	d.xbuf = append(d.xbuf, b)

	for {
		__gp25 := d.mustgetc()
		b, ok = __gp25.b, __gp25.ok
		if !ok {
			return
		}
		if b < xmlRuneSelf && !isNameByte(b) {
			d.ungetc(b)
			break
		}
		d.xbuf = append(d.xbuf, b)
	}
	return true
}

func isNameByte(c byte) bool {
	return 'A' <= c && c <= 'Z' ||
		'a' <= c && c <= 'z' ||
		'0' <= c && c <= '9' ||
		c == '_' || c == ':' || c == '.' || c == '-'
}

func isName(s []byte) bool {
	if len(s) == 0 {
		return false
	}
	__rp2 := xmlDecodeRune(s)
	c, n := __rp2.r, __rp2.n
	if c == xmlRuneError && n == 1 {
		return false
	}
	if !xmlIs(xmlNameFirstRanges, c) {
		return false
	}
	for n < len(s) {
		s = s[n:]
		__rp7 := xmlDecodeRune(s)
		c, n = __rp7.r, __rp7.n
		if c == xmlRuneError && n == 1 {
			return false
		}
		if !xmlIs(xmlNameFirstRanges, c) && !xmlIs(xmlNameSecondRanges, c) {
			return false
		}
	}
	return true
}

func isNameString(s string) bool {
	if len(s) == 0 {
		return false
	}
	__rp3 := xmlDecodeRuneString(s)
	c, n := __rp3.r, __rp3.n
	if c == xmlRuneError && n == 1 {
		return false
	}
	if !xmlIs(xmlNameFirstRanges, c) {
		return false
	}
	for n < len(s) {
		s = s[n:]
		__rp8 := xmlDecodeRuneString(s)
		c, n = __rp8.r, __rp8.n
		if c == xmlRuneError && n == 1 {
			return false
		}
		if !xmlIs(xmlNameFirstRanges, c) && !xmlIs(xmlNameSecondRanges, c) {
			return false
		}
	}
	return true
}

// xmlIs reports whether r is in the [lo,hi,stride] range table (binary
// search). xmlNameFirstRanges/xmlNameSecondRanges are gc's first/second RangeTables
// (XML spec Appendix B), compressed — see ranges_first.txt.
func xmlIs(table [][3]uint32, r rune) bool {
	c := uint32(r)
	lo, hi := 0, len(table)
	for lo < hi {
		mid := (lo + hi) / 2
		e := table[mid]
		if c < e[0] {
			hi = mid
		} else if c > e[1] {
			lo = mid + 1
		} else {
			return (c-e[0])%e[2] == 0
		}
	}
	return false
}

const (
	xmlRuneError = rune(0xFFFD)
	xmlRuneSelf  = 0x80
	xmlMaxRune   = 0x10FFFF
)

// xmlDecodeRune is utf8.DecodeRune (self-contained).
func xmlDecodeRune(s []byte) runePair {
	if len(s) == 0 {
		return runePair{xmlRuneError, 0}
	}
	s0 := s[0]
	if s0 < xmlRuneSelf {
		return runePair{rune(s0), 1}
	}
	var size int
	switch {
	case s0&0xE0 == 0xC0:
		size = 2
	case s0&0xF0 == 0xE0:
		size = 3
	case s0&0xF8 == 0xF0:
		size = 4
	default:
		return runePair{xmlRuneError, 1}
	}
	if len(s) < size {
		return runePair{xmlRuneError, 1}
	}
	r := rune(s0 & (0xFF >> (uint(size) + 1)))
	for i := 1; i < size; i++ {
		c := s[i]
		if c&0xC0 != 0x80 {
			return runePair{xmlRuneError, 1}
		}
		r = r<<6 | rune(c&0x3F)
	}
	switch {
	case size == 2 && r < 0x80:
		return runePair{xmlRuneError, 1}
	case size == 3 && r < 0x800:
		return runePair{xmlRuneError, 1}
	case size == 4 && (r < 0x10000 || r > xmlMaxRune):
		return runePair{xmlRuneError, 1}
	case 0xD800 <= r && r <= 0xDFFF:
		return runePair{xmlRuneError, 1}
	}
	return runePair{r, size}
}

func xmlDecodeRuneString(s string) runePair {
	if len(s) == 0 {
		return runePair{xmlRuneError, 0}
	}
	s0 := s[0]
	if s0 < xmlRuneSelf {
		return runePair{rune(s0), 1}
	}
	var size int
	switch {
	case s0&0xE0 == 0xC0:
		size = 2
	case s0&0xF0 == 0xE0:
		size = 3
	case s0&0xF8 == 0xF0:
		size = 4
	default:
		return runePair{xmlRuneError, 1}
	}
	if len(s) < size {
		return runePair{xmlRuneError, 1}
	}
	r := rune(s0 & (0xFF >> (uint(size) + 1)))
	for i := 1; i < size; i++ {
		c := s[i]
		if c&0xC0 != 0x80 {
			return runePair{xmlRuneError, 1}
		}
		r = r<<6 | rune(c&0x3F)
	}
	switch {
	case size == 2 && r < 0x80:
		return runePair{xmlRuneError, 1}
	case size == 3 && r < 0x800:
		return runePair{xmlRuneError, 1}
	case size == 4 && (r < 0x10000 || r > xmlMaxRune):
		return runePair{xmlRuneError, 1}
	case 0xD800 <= r && r <= 0xDFFF:
		return runePair{xmlRuneError, 1}
	}
	return runePair{r, size}
}

// xmlAppendRune appends the UTF-8 encoding of r (a valid, non-surrogate
// rune) to b.
func xmlAppendRune(b []byte, r rune) []byte {
	switch {
	case r < 0x80:
		return append(b, byte(r))
	case r < 0x800:
		return append(b, byte(0xC0|r>>6), byte(0x80|r&0x3F))
	case r < 0x10000:
		return append(b, byte(0xE0|r>>12), byte(0x80|r>>6&0x3F), byte(0x80|r&0x3F))
	default:
		return append(b, byte(0xF0|r>>18), byte(0x80|r>>12&0x3F), byte(0x80|r>>6&0x3F), byte(0x80|r&0x3F))
	}
}

// xmlParseUint is strconv.ParseUint(s, base, 64) for base 10/16
// (self-contained). It reports an error on empty input, bad digits,
// and overflow.
func xmlParseUint(s string, base int) uint64ErrPair {
	if s == "" {
		return uint64ErrPair{0, &xmlError{msg: "xmlParseUint: empty string"}}
	}
	var n uint64
	for i := 0; i < len(s); i++ {
		c := s[i]
		var d uint64
		switch {
		case '0' <= c && c <= '9':
			d = uint64(c - '0')
		case base == 16 && 'a' <= c && c <= 'f':
			d = uint64(c-'a') + 10
		case base == 16 && 'A' <= c && c <= 'F':
			d = uint64(c-'A') + 10
		default:
			return uint64ErrPair{0, &xmlError{msg: "xmlParseUint: invalid digit"}}
		}
		if uint64(d) >= uint64(base) {
			return uint64ErrPair{0, &xmlError{msg: "xmlParseUint: invalid digit"}}
		}
		if n > (1<<64-1-uint64(d))/uint64(base) {
			return uint64ErrPair{0, &xmlError{msg: "xmlParseUint: overflow"}}
		}
		n = n*uint64(base) + uint64(d)
	}
	return uint64ErrPair{n, nil}
}

// xmlCount is strings.Count(s, sep) for a single-byte sep.
func xmlCount(s, sep string) int {
	n := 0
	for i := 0; i+len(sep) <= len(s); i++ {
		if s[i:i+len(sep)] == sep {
			n++
		}
	}
	return n
}

// xmlCut is strings.Cut(s, ":").
// M7: single cutTriple result (v1 cannot emit multi-result calls, WS D).
func xmlCut(s, sep string) cutTriple {
	for i := 0; i+len(sep) <= len(s); i++ {
		if s[i:i+len(sep)] == sep {
			return cutTriple{s[:i], s[i+len(sep):], true}
		}
	}
	return cutTriple{s, "", false}
}

// xmlEqualFold is strings.EqualFold (ASCII fast path is enough: the only
// use is the "utf-8" encoding name, but implement it generally over
// runes for honesty).
func xmlEqualFold(a, b string) bool {
	// Fast path: byte-wise with ASCII case folding, then rune-wise.
	ia, ib := 0, 0
	for ia < len(a) && ib < len(b) {
		ca, cb := a[ia], b[ib]
		if ca < 0x80 && cb < 0x80 {
			if ca == cb {
				ia++
				ib++
				continue
			}
			// ASCII fold.
			if 'A' <= ca && ca <= 'Z' {
				ca += 'a' - 'A'
			}
			if 'A' <= cb && cb <= 'Z' {
				cb += 'a' - 'A'
			}
			if ca != cb {
				return false
			}
			ia++
			ib++
			continue
		}
		__rp9 := xmlDecodeRuneString(a[ia:])
		ra, sa := __rp9.r, __rp9.n
		__rp10 := xmlDecodeRuneString(b[ib:])
		rb, sb := __rp10.r, __rp10.n
		if ra != rb {
			// Simple case folding for non-ASCII: compare uppercased
			// via the XML name tables? No — fall back to exact match.
			// (Only "utf-8" variants reach here in practice.)
			return false
		}
		ia += sa
		ib += sb
	}
	return ia == len(a) && ib == len(b)
}

// xmlIndex is strings.Index.
func xmlIndex(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}

// xmlIndexByte is strings.IndexByte.
func xmlIndexByte(s string, c byte) int {
	for i := 0; i < len(s); i++ {
		if s[i] == c {
			return i
		}
	}
	return -1
}

// xmlLastIndex is strings.LastIndex.
func xmlLastIndex(s, sub string) int {
	for i := len(s) - len(sub); i >= 0; i-- {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}

// xmlTrimRight is strings.TrimRight(s, "/").
func xmlTrimRight(s, cutset string) string {
	for len(s) > 0 {
		c := s[len(s)-1]
		found := false
		for i := 0; i < len(cutset); i++ {
			if cutset[i] == c {
				found = true
				break
			}
		}
		if !found {
			break
		}
		s = s[:len(s)-1]
	}
	return s
}

// xmlContains is strings.Contains.
func xmlContains(s, sub string) bool {
	return xmlIndex(s, sub) >= 0
}

func procInst(param, s string) string {
	// TODO: this parsing is somewhat lame and not exact.
	// It works for all actual cases, though.
	param = param + "="
	lenp := len(param)
	i := 0
	var sep byte
	for i < len(s) {
		sub := s[i:]
		k := xmlIndex(sub, param)
		if k < 0 || lenp+k >= len(sub) {
			return ""
		}
		i += lenp + k + 1
		if c := sub[lenp+k]; c == '\'' || c == '"' {
			sep = c
			break
		}
	}
	if sep == 0 {
		return ""
	}
	j := xmlIndexByte(s[i:], sep)
	if j < 0 {
		return ""
	}
	return s[i : i+j]
}

// =====================================================================
// Encoder.
// =====================================================================

// An Encoder writes XML tokens to an output stream (a [Writer]).
//
// M7: the gc Encoder writes through a bufio.Writer; the synthetic
// writes directly to the Writer (no buffering — Flush is a no-op
// returning nil, exactly as a buffer-backed gc encoder behaves).
type Encoder struct {
	w      Writer
	prefix string
	indent string

	// pbuf is gc's bufio.Writer (4096-byte buffer) faithfully
	// replicated: writes accumulate here and are flushed to w on
	// Flush/Close or when the buffer fills (with bufio's large-write
	// bypass). This makes partial-output visibility and the
	// xml-declaration-first rule (Buffered() != 0) byte-exact vs gc.
	pbuf []byte

	tags       []Name
	depth      int
	putNewline bool
	indentedIn bool
	attrPrefix map[string]string // map name space -> prefix
	attrNS     map[string]string // map prefix -> name space
	prefixes   []string
	seq        int
	closed     bool
	werr       error // sticky write error (only "use of closed Encoder": writes cannot fail)
}

// NewEncoder creates a new Encoder writing to w.
func NewEncoder(w Writer) *Encoder {
	return &Encoder{w: w}
}

// Indent sets the encoder to generate XML in which each element begins
// on a new indented line that starts with prefix and is followed by one
// or more copies of indent according to the nesting depth.
func (enc *Encoder) Indent(prefix, indent string) {
	enc.prefix = prefix
	enc.indent = indent
}

func (enc *Encoder) writeBytes(p []byte) {
	if enc.closed && enc.werr == nil {
		enc.werr = &xmlError{msg: "use of closed Encoder"}
	}
	if enc.werr != nil {
		return
	}
	// bufio.Writer.Write faithfully (writes cannot fail here): while p
	// overflows the available space, a large write to an empty buffer
	// bypasses it, otherwise the buffer is filled and flushed. A
	// exactly-full buffer is NOT flushed until the next write overflows.
	for len(p) > 4096-len(enc.pbuf) {
		if len(enc.pbuf) == 0 {
			// Large write, empty buffer: write directly, bypassing.
			enc.w.WriteBytes(p)
			p = p[:0]
		} else {
			fill := 4096 - len(enc.pbuf)
			enc.pbuf = append(enc.pbuf, p[:fill]...)
			p = p[fill:]
			enc.flushBuf()
		}
	}
	enc.pbuf = append(enc.pbuf, p...)
}

// flushBuf writes the buffered bytes to the underlying Writer.
func (enc *Encoder) flushBuf() {
	if len(enc.pbuf) > 0 {
		enc.w.WriteBytes(enc.pbuf)
		enc.pbuf = enc.pbuf[:0]
	}
}

func (enc *Encoder) writeString(s string) {
	enc.writeBytes([]byte(s))
}

func (enc *Encoder) writeByte(c byte) {
	enc.writeBytes([]byte{c})
}

// EncodeToken writes the given XML token to the stream.
// It returns nil if the token was written successfully.
//
// EncodeToken allows writing a [ProcInst] with Target set to "xml" only
// as the first token in the stream.
//
// Callers that create an Encoder and then invoke EncodeToken directly,
// without using Encode or EncodeElement, need to call Flush when
// finished to ensure that the XML is written to the underlying writer
// (a no-op in M7: writes are unbuffered).
func (enc *Encoder) EncodeToken(t Token) error {
	switch t := t.(type) {
	case StartElement:
		if err := enc.writeStart(t); err != nil {
			return err
		}
	case EndElement:
		if err := enc.writeEnd(t.Name); err != nil {
			return err
		}
	case CharData:
		xmlEscapeToEncoder(enc, t, false)
	case Comment:
		if xmlContainsBytes(t, xmlEndComment) {
			return &xmlError{msg: "xml: EncodeToken of Comment containing --> marker"}
		}
		enc.writeString("<!--")
		enc.writeBytes(t)
		enc.writeString("-->")
		return enc.werr
	case ProcInst:
		// First token to be encoded which is also a ProcInst with target of xml
		// is the xml declaration. The only ProcInst where target of xml is allowed.
		if t.Target == "xml" && len(enc.pbuf) != 0 {
			return &xmlError{msg: "xml: EncodeToken of ProcInst xml target only valid for xml declaration, first token encoded"}
		}
		if !isNameString(t.Target) {
			return &xmlError{msg: "xml: EncodeToken of ProcInst with invalid Target"}
		}
		if xmlContainsBytes(t.Inst, xmlEndProcInst) {
			return &xmlError{msg: "xml: EncodeToken of ProcInst containing ?> marker"}
		}
		enc.writeString("<?")
		enc.writeString(t.Target)
		if len(t.Inst) > 0 {
			enc.writeByte(' ')
			enc.writeBytes(t.Inst)
		}
		enc.writeString("?>")
	case Directive:
		if !isValidDirective(t) {
			return &xmlError{msg: "xml: EncodeToken of Directive containing wrong < or > markers"}
		}
		enc.writeString("<!")
		enc.writeBytes(t)
		enc.writeString(">")
	default:
		return &xmlError{msg: "xml: EncodeToken of invalid token type"}
	}
	return enc.werr
}

// isValidDirective reports whether dir is a valid directive text,
// meaning angle brackets are matched, ignoring comments and strings.
func isValidDirective(dir Directive) bool {
	var (
		depth     int
		inquote   uint8
		incomment bool
	)
	for i, c := range dir {
		switch {
		case incomment:
			if c == '>' {
				if n := 1 + i - len(xmlEndComment); n >= 0 && xmlEqualBytes(dir[n:i+1], xmlEndComment) {
					incomment = false
				}
			}
			// Just ignore anything in comment
		case inquote != 0:
			if c == inquote {
				inquote = 0
			}
			// Just ignore anything within quotes
		case c == '\'' || c == '"':
			inquote = c
		case c == '<':
			if i+len(xmlBegComment) < len(dir) && xmlEqualBytes(dir[i:i+len(xmlBegComment)], xmlBegComment) {
				incomment = true
			} else {
				depth++
			}
		case c == '>':
			if depth == 0 {
				return false
			}
			depth--
		}
	}
	return depth == 0 && inquote == 0 && !incomment
}

var (
	xmlBegComment = []byte("<!--")
	xmlEndComment = []byte("-->")
	xmlEndProcInst = []byte("?>")
)

func xmlContainsBytes(b, sub []byte) bool {
	for i := 0; i+len(sub) <= len(b); i++ {
		if xmlEqualBytes(b[i:i+len(sub)], sub) {
			return true
		}
	}
	return false
}

func xmlEqualBytes(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// M7: takes StartElement by value (v1 cannot take the address of a
// local variable, WS D).
func (enc *Encoder) writeStart(start StartElement) error {
	if start.Name.Local == "" {
		return &xmlError{msg: "xml: start tag with no name"}
	}

	enc.tags = append(enc.tags, start.Name)
	enc.markPrefix()

	enc.writeIndent(1)
	enc.writeByte('<')
	enc.writeString(start.Name.Local)

	if start.Name.Space != "" {
		enc.writeString(" xmlns=\"")
		enc.escapeString(start.Name.Space)
		enc.writeByte('"')
	}

	// Attributes
	for _, attr := range start.Attr {
		name := attr.Name
		if name.Local == "" {
			continue
		}
		enc.writeByte(' ')
		if name.Space != "" {
			enc.writeString(enc.createAttrPrefix(name.Space))
			enc.writeByte(':')
		}
		enc.writeString(name.Local)
		enc.writeString("=\"")
		enc.escapeString(attr.Value)
		enc.writeByte('"')
	}
	enc.writeByte('>')
	return nil
}

func (enc *Encoder) writeEnd(name Name) error {
	if name.Local == "" {
		return &xmlError{msg: "xml: end tag with no name"}
	}
	if len(enc.tags) == 0 || enc.tags[len(enc.tags)-1].Local == "" {
		return &xmlError{msg: "xml: end tag </" + name.Local + "> without start tag"}
	}
	if top := enc.tags[len(enc.tags)-1]; top != name {
		if top.Local != name.Local {
			return &xmlError{msg: "xml: end tag </" + name.Local + "> does not match start tag <" + top.Local + ">"}
		}
		return &xmlError{msg: "xml: end tag </" + name.Local + "> in namespace " + name.Space + " does not match start tag <" + top.Local + "> in namespace " + top.Space}
	}
	enc.tags = enc.tags[:len(enc.tags)-1]

	enc.writeIndent(-1)
	enc.writeByte('<')
	enc.writeByte('/')
	enc.writeString(name.Local)
	enc.writeByte('>')
	enc.popPrefix()
	return nil
}

func (enc *Encoder) createAttrPrefix(url string) string {
	if prefix := enc.attrPrefix[url]; prefix != "" {
		return prefix
	}

	// The "http://www.w3.org/XML/1998/namespace" name space is predefined as "xml"
	// and must be referred to that way.
	// (The "http://www.w3.org/2000/xmlns/" name space is also predefined as "xmlns",
	// but users should not be trying to use that one directly - that's our job.)
	if url == xmlURL {
		return xmlPrefix
	}

	// Need to define a new name space.
	if enc.attrPrefix == nil {
		enc.attrPrefix = make(map[string]string)
		enc.attrNS = make(map[string]string)
	}

	// Pick a name. We try to use the final element of the path
	// but fall back to _.
	prefix := xmlTrimRight(url, "/")
	if i := xmlLastIndex(prefix, "/"); i >= 0 {
		prefix = prefix[i+1:]
	}
	if prefix == "" || !isNameString(prefix) || xmlContains(prefix, ":") {
		prefix = "_"
	}
	// xmlanything is reserved and any variant of it regardless of
	// case should be matched, so:
	//    (('X'|'x') ('M'|'m') ('L'|'l'))
	// See Section 2.3 of https://www.w3.org/TR/REC-xml/
	if len(prefix) >= 3 && xmlEqualFold(prefix[:3], "xml") {
		prefix = "_" + prefix
	}
	if enc.attrNS[prefix] != "" {
		// Name is taken. Find a better one.
		for enc.seq++; ; enc.seq++ {
			if id := prefix + "_" + xmlItoa(enc.seq); enc.attrNS[id] == "" {
				prefix = id
				break
			}
		}
	}

	enc.attrPrefix[url] = prefix
	enc.attrNS[prefix] = url

	enc.writeString("xmlns:")
	enc.writeString(prefix)
	enc.writeString("=\"")
	xmlEscapeToEncoder(enc, []byte(url), true)
	enc.writeString("\" ")

	enc.prefixes = append(enc.prefixes, prefix)

	return prefix
}

// deleteAttrPrefix removes an attribute name space prefix.
func (enc *Encoder) deleteAttrPrefix(prefix string) {
	delete(enc.attrPrefix, enc.attrNS[prefix])
	delete(enc.attrNS, prefix)
}

func (enc *Encoder) markPrefix() {
	enc.prefixes = append(enc.prefixes, "")
}

func (enc *Encoder) popPrefix() {
	for len(enc.prefixes) > 0 {
		prefix := enc.prefixes[len(enc.prefixes)-1]
		enc.prefixes = enc.prefixes[:len(enc.prefixes)-1]
		if prefix == "" {
			break
		}
		enc.deleteAttrPrefix(prefix)
	}
}

// Flush flushes any buffered XML to the underlying writer.
// M7: writes are unbuffered, so this is a no-op returning nil — exactly
// how a buffer-backed gc Encoder behaves at Flush.
func (enc *Encoder) Flush() error {
	enc.flushBuf()
	return nil
}

// Close the Encoder, indicating that no more data will be written. It
// returns an error if the written XML is invalid (e.g. by containing
// unclosed elements). (M7: no buffered bytes to flush.)
func (enc *Encoder) Close() error {
	if enc.closed {
		return nil
	}
	enc.closed = true
	enc.flushBuf()
	if len(enc.tags) > 0 {
		return &xmlError{msg: "unclosed tag <" + enc.tags[len(enc.tags)-1].Local + ">"}
	}
	return nil
}

func (enc *Encoder) writeIndent(depthDelta int) {
	if len(enc.prefix) == 0 && len(enc.indent) == 0 {
		return
	}
	if depthDelta < 0 {
		enc.depth--
		if enc.indentedIn {
			enc.indentedIn = false
			return
		}
		enc.indentedIn = false
	}
	if enc.putNewline {
		enc.writeByte('\n')
	} else {
		enc.putNewline = true
	}
	if len(enc.prefix) > 0 {
		enc.writeString(enc.prefix)
	}
	if len(enc.indent) > 0 {
		for i := 0; i < enc.depth; i++ {
			enc.writeString(enc.indent)
		}
	}
	if depthDelta > 0 {
		enc.depth++
		enc.indentedIn = true
	}
}

// =====================================================================
// Escaping.
// =====================================================================

var (
	xmlEscQuot = []byte("&#34;") // shorter than "&quot;"
	xmlEscApos = []byte("&#39;") // shorter than "&apos;"
	xmlEscAmp  = []byte("&amp;")
	xmlEscLT   = []byte("&lt;")
	xmlEscGT   = []byte("&gt;")
	xmlEscTab  = []byte("&#x9;")
	xmlEscNL   = []byte("&#xA;")
	xmlEscCR   = []byte("&#xD;")
	xmlEscFFFD = []byte("\uFFFD") // Unicode replacement character
)

// EscapeText writes to w the properly escaped XML equivalent of the
// plain text data s.
func EscapeText(w Writer, s []byte) error {
	xmlEscapeToWriter(w, s, true)
	return nil
}

// Escape is like [EscapeText] but omits the error return value.
// It is provided for backwards compatibility with Go 1.0.
// Code targeting Go 1.1 or later should use [EscapeText].
func Escape(w Writer, s []byte) {
	xmlEscapeToWriter(w, s, true)
}

// xmlEscapeToWriter is gc's escapeText over the M7 Writer (writes cannot
// fail, so no error return).
func xmlEscapeToWriter(w Writer, s []byte, escapeNewline bool) {
	var esc []byte
	last := 0
	for i := 0; i < len(s); {
		__rp4 := xmlDecodeRune(s[i:])
		r, width := __rp4.r, __rp4.n
		i += width
		switch r {
		case '"':
			esc = xmlEscQuot
		case '\'':
			esc = xmlEscApos
		case '&':
			esc = xmlEscAmp
		case '<':
			esc = xmlEscLT
		case '>':
			esc = xmlEscGT
		case '\t':
			esc = xmlEscTab
		case '\n':
			if !escapeNewline {
				continue
			}
			esc = xmlEscNL
		case '\r':
			esc = xmlEscCR
		default:
			if !isInCharacterRange(r) || (r == xmlRuneError && width == 1) {
				esc = xmlEscFFFD
				break
			}
			continue
		}
		w.WriteBytes(s[last : i-width])
		w.WriteBytes(esc)
		last = i
	}
	w.WriteBytes(s[last:])
}

// xmlEscapeToEncoder is escapeText writing to the Encoder's buffer.
func xmlEscapeToEncoder(enc *Encoder, s []byte, escapeNewline bool) {
	var esc []byte
	last := 0
	for i := 0; i < len(s); {
		__rp5 := xmlDecodeRune(s[i:])
		r, width := __rp5.r, __rp5.n
		i += width
		switch r {
		case '"':
			esc = xmlEscQuot
		case '\'':
			esc = xmlEscApos
		case '&':
			esc = xmlEscAmp
		case '<':
			esc = xmlEscLT
		case '>':
			esc = xmlEscGT
		case '\t':
			esc = xmlEscTab
		case '\n':
			if !escapeNewline {
				continue
			}
			esc = xmlEscNL
		case '\r':
			esc = xmlEscCR
		default:
			if !isInCharacterRange(r) || (r == xmlRuneError && width == 1) {
				esc = xmlEscFFFD
				break
			}
			continue
		}
		enc.writeBytes(s[last : i-width])
		enc.writeBytes(esc)
		last = i
	}
	enc.writeBytes(s[last:])
}

// escapeString writes to the Encoder the properly escaped XML
// equivalent of the plain text data s (gc's printer.EscapeString:
// always escapes newlines).
func (enc *Encoder) escapeString(s string) {
	var esc []byte
	last := 0
	for i := 0; i < len(s); {
		__rp6 := xmlDecodeRuneString(s[i:])
		r, width := __rp6.r, __rp6.n
		i += width
		switch r {
		case '"':
			esc = xmlEscQuot
		case '\'':
			esc = xmlEscApos
		case '&':
			esc = xmlEscAmp
		case '<':
			esc = xmlEscLT
		case '>':
			esc = xmlEscGT
		case '\t':
			esc = xmlEscTab
		case '\n':
			esc = xmlEscNL
		case '\r':
			esc = xmlEscCR
		default:
			if !isInCharacterRange(r) || (r == xmlRuneError && width == 1) {
				esc = xmlEscFFFD
				break
			}
			continue
		}
		enc.writeString(s[last : i-width])
		enc.writeBytes(esc)
		last = i
	}
	enc.writeString(s[last:])
}

// =====================================================================
// Fail-fast members (exact gc 1.27.1 signatures; need reflect or WS D).
// Never silent: every one panics with the reason and the re-test trigger.
// =====================================================================

// Unmarshal parses the XML-encoded data and stores the result in the
// value pointed to by v. M7: needs reflect (P3-M8).
func Unmarshal(data []byte, v any) error {
	panic("xml.Unmarshal: not in M7 scope (needs reflect; re-test when reflect lands)")
}

// Decode works like [Unmarshal], except it reads the decoder stream to
// find the start element. M7: needs reflect (P3-M8).
func (d *Decoder) Decode(v any) error {
	panic("xml.Decoder.Decode: not in M7 scope (needs reflect; re-test when reflect lands)")
}

// DecodeElement works like [Unmarshal] except that it takes a pointer to
// the start XML element to decode into v. M7: needs reflect (P3-M8).
func (d *Decoder) DecodeElement(v any, start *StartElement) error {
	panic("xml.Decoder.DecodeElement: not in M7 scope (needs reflect; re-test when reflect lands)")
}

// Marshal returns the XML encoding of v. M7: needs reflect (P3-M8).
// (2 results: skipped at export; call sites fail fast. Re-test WS D.)
func Marshal(v any) ([]byte, error) {
	panic("xml.Marshal: not in M7 scope (needs reflect; re-test when reflect lands)")
}

// MarshalIndent is like Marshal but each XML element begins on a new
// indented line that starts with prefix and is followed by one or more
// copies of indent according to the nesting depth. M7: needs reflect.
func MarshalIndent(v any, prefix, indent string) ([]byte, error) {
	panic("xml.MarshalIndent: not in M7 scope (needs reflect; re-test when reflect lands)")
}

// Encode writes the XML encoding of v to the stream. M7: needs reflect.
func (enc *Encoder) Encode(v any) error {
	panic("xml.Encoder.Encode: not in M7 scope (needs reflect; re-test when reflect lands)")
}

// EncodeElement writes the XML encoding of v to the stream, using start
// as the outermost tag in the encoding. M7: needs reflect.
func (enc *Encoder) EncodeElement(v any, start StartElement) error {
	panic("xml.Encoder.EncodeElement: not in M7 scope (needs reflect; re-test when reflect lands)")
}

// =====================================================================
// Marshal/unmarshal interface types (exact gc 1.27.1; no reflect needed
// to declare — they only become callable via the reflect paths above).
// =====================================================================

// Marshaler is the interface implemented by objects that can marshal
// themselves into valid XML elements.
type Marshaler interface {
	MarshalXML(e *Encoder, start StartElement) error
}

// MarshalerAttr is the interface implemented by objects that can marshal
// themselves into valid XML attributes.
type MarshalerAttr interface {
	MarshalXMLAttr(name Name) (Attr, error)
}

// An UnmarshalError represents an error in the unmarshaling process.
type UnmarshalError string

func (e UnmarshalError) Error() string { return string(e) }

// Unmarshaler is the interface implemented by objects that can unmarshal
// an XML element description of themselves.
type Unmarshaler interface {
	UnmarshalXML(d *Decoder, start StartElement) error
}

// UnmarshalerAttr is the interface implemented by objects that can unmarshal
// an XML attribute description of themselves.
type UnmarshalerAttr interface {
	UnmarshalXMLAttr(attr Attr) error
}

// =====================================================================
// HTML data tables (exact gc 1.27.1 data).
// =====================================================================

var xmlHTMLAutoCloseData = []string{
	"basefont",
	"br",
	"area",
	"link",
	"img",
	"param",
	"hr",
	"input",
	"col",
	"frame",
	"isindex",
	"base",
	"meta",
}

// xmlNameFirstRanges: gc 1.27.1 encoding/xml first RangeTable (XML spec App. B),
// as sorted [lo, hi, stride] triples for binary search.
var xmlNameFirstRanges = [][3]uint32{
	{0x3A, 0x3A, 1},
	{0x41, 0x5A, 1},
	{0x5F, 0x5F, 1},
	{0x61, 0x7A, 1},
	{0xC0, 0xD6, 1},
	{0xD8, 0xF6, 1},
	{0xF8, 0xFF, 1},
	{0x100, 0x131, 1},
	{0x134, 0x13E, 1},
	{0x141, 0x148, 1},
	{0x14A, 0x17E, 1},
	{0x180, 0x1C3, 1},
	{0x1CD, 0x1F0, 1},
	{0x1F4, 0x1F5, 1},
	{0x1FA, 0x217, 1},
	{0x250, 0x2A8, 1},
	{0x2BB, 0x2C1, 1},
	{0x386, 0x386, 1},
	{0x388, 0x38A, 1},
	{0x38C, 0x38C, 1},
	{0x38E, 0x3A1, 1},
	{0x3A3, 0x3CE, 1},
	{0x3D0, 0x3D6, 1},
	{0x3DA, 0x3E0, 2},
	{0x3E2, 0x3F3, 1},
	{0x401, 0x40C, 1},
	{0x40E, 0x44F, 1},
	{0x451, 0x45C, 1},
	{0x45E, 0x481, 1},
	{0x490, 0x4C4, 1},
	{0x4C7, 0x4C8, 1},
	{0x4CB, 0x4CC, 1},
	{0x4D0, 0x4EB, 1},
	{0x4EE, 0x4F5, 1},
	{0x4F8, 0x4F9, 1},
	{0x531, 0x556, 1},
	{0x559, 0x559, 1},
	{0x561, 0x586, 1},
	{0x5D0, 0x5EA, 1},
	{0x5F0, 0x5F2, 1},
	{0x621, 0x63A, 1},
	{0x641, 0x64A, 1},
	{0x671, 0x6B7, 1},
	{0x6BA, 0x6BE, 1},
	{0x6C0, 0x6CE, 1},
	{0x6D0, 0x6D3, 1},
	{0x6D5, 0x6D5, 1},
	{0x6E5, 0x6E6, 1},
	{0x905, 0x939, 1},
	{0x93D, 0x93D, 1},
	{0x958, 0x961, 1},
	{0x985, 0x98C, 1},
	{0x98F, 0x990, 1},
	{0x993, 0x9A8, 1},
	{0x9AA, 0x9B0, 1},
	{0x9B2, 0x9B2, 1},
	{0x9B6, 0x9B9, 1},
	{0x9DC, 0x9DD, 1},
	{0x9DF, 0x9E1, 1},
	{0x9F0, 0x9F1, 1},
	{0xA05, 0xA0A, 1},
	{0xA0F, 0xA10, 1},
	{0xA13, 0xA28, 1},
	{0xA2A, 0xA30, 1},
	{0xA32, 0xA33, 1},
	{0xA35, 0xA36, 1},
	{0xA38, 0xA39, 1},
	{0xA59, 0xA5C, 1},
	{0xA5E, 0xA5E, 1},
	{0xA72, 0xA74, 1},
	{0xA85, 0xA8B, 1},
	{0xA8D, 0xA8D, 1},
	{0xA8F, 0xA91, 1},
	{0xA93, 0xAA8, 1},
	{0xAAA, 0xAB0, 1},
	{0xAB2, 0xAB3, 1},
	{0xAB5, 0xAB9, 1},
	{0xB05, 0xB0C, 1},
	{0xB0F, 0xB10, 1},
	{0xB13, 0xB28, 1},
	{0xB2A, 0xB30, 1},
	{0xB32, 0xB33, 1},
	{0xB36, 0xB39, 1},
	{0xB3D, 0xB3D, 1},
	{0xB5C, 0xB5D, 1},
	{0xB5F, 0xB61, 1},
	{0xB85, 0xB8A, 1},
	{0xB8E, 0xB90, 1},
	{0xB92, 0xB95, 1},
	{0xB99, 0xB9A, 1},
	{0xB9C, 0xB9C, 1},
	{0xB9E, 0xB9F, 1},
	{0xBA3, 0xBA4, 1},
	{0xBA8, 0xBAA, 1},
	{0xBAE, 0xBB5, 1},
	{0xBB7, 0xBB9, 1},
	{0xC05, 0xC0C, 1},
	{0xC0E, 0xC10, 1},
	{0xC12, 0xC28, 1},
	{0xC2A, 0xC33, 1},
	{0xC35, 0xC39, 1},
	{0xC60, 0xC61, 1},
	{0xC85, 0xC8C, 1},
	{0xC8E, 0xC90, 1},
	{0xC92, 0xCA8, 1},
	{0xCAA, 0xCB3, 1},
	{0xCB5, 0xCB9, 1},
	{0xCDE, 0xCDE, 1},
	{0xCE0, 0xCE1, 1},
	{0xD05, 0xD0C, 1},
	{0xD0E, 0xD10, 1},
	{0xD12, 0xD28, 1},
	{0xD2A, 0xD39, 1},
	{0xD60, 0xD61, 1},
	{0xE01, 0xE2E, 1},
	{0xE30, 0xE30, 1},
	{0xE32, 0xE33, 1},
	{0xE40, 0xE45, 1},
	{0xE81, 0xE82, 1},
	{0xE84, 0xE84, 1},
	{0xE87, 0xE88, 1},
	{0xE8A, 0xE8D, 3},
	{0xE94, 0xE97, 1},
	{0xE99, 0xE9F, 1},
	{0xEA1, 0xEA3, 1},
	{0xEA5, 0xEA7, 2},
	{0xEAA, 0xEAB, 1},
	{0xEAD, 0xEAE, 1},
	{0xEB0, 0xEB0, 1},
	{0xEB2, 0xEB3, 1},
	{0xEBD, 0xEBD, 1},
	{0xEC0, 0xEC4, 1},
	{0xF40, 0xF47, 1},
	{0xF49, 0xF69, 1},
	{0x10A0, 0x10C5, 1},
	{0x10D0, 0x10F6, 1},
	{0x1100, 0x1100, 1},
	{0x1102, 0x1103, 1},
	{0x1105, 0x1107, 1},
	{0x1109, 0x1109, 1},
	{0x110B, 0x110C, 1},
	{0x110E, 0x1112, 1},
	{0x113C, 0x1140, 2},
	{0x114C, 0x1150, 2},
	{0x1154, 0x1155, 1},
	{0x1159, 0x1159, 1},
	{0x115F, 0x1161, 1},
	{0x1163, 0x1169, 2},
	{0x116D, 0x116E, 1},
	{0x1172, 0x1173, 1},
	{0x11AE, 0x11AF, 1},
	{0x11B7, 0x11B8, 1},
	{0x11BA, 0x11BA, 1},
	{0x11BC, 0x11C2, 1},
	{0x11F9, 0x11F9, 1},
	{0x1E00, 0x1E9B, 1},
	{0x1EA0, 0x1EF9, 1},
	{0x1F00, 0x1F15, 1},
	{0x1F18, 0x1F1D, 1},
	{0x1F20, 0x1F45, 1},
	{0x1F48, 0x1F4D, 1},
	{0x1F50, 0x1F57, 1},
	{0x1F5D, 0x1F5D, 1},
	{0x1F5F, 0x1F7D, 1},
	{0x1F80, 0x1FB4, 1},
	{0x1FB6, 0x1FBC, 1},
	{0x1FBE, 0x1FBE, 1},
	{0x1FC2, 0x1FC4, 1},
	{0x1FC6, 0x1FCC, 1},
	{0x1FD0, 0x1FD3, 1},
	{0x1FD6, 0x1FDB, 1},
	{0x1FE0, 0x1FEC, 1},
	{0x1FF2, 0x1FF4, 1},
	{0x1FF6, 0x1FFC, 1},
	{0x2126, 0x2126, 1},
	{0x212A, 0x212B, 1},
	{0x212E, 0x212E, 1},
	{0x2180, 0x2182, 1},
	{0x3007, 0x3007, 1},
	{0x3021, 0x3029, 1},
	{0x3041, 0x3094, 1},
	{0x30A1, 0x30FA, 1},
	{0x3105, 0x312C, 1},
	{0x4E00, 0x9FA5, 1},
	{0xAC00, 0xD7A3, 1},
}

// xmlNameSecondRanges: gc 1.27.1 encoding/xml second RangeTable (XML spec App. B),
// as sorted [lo, hi, stride] triples for binary search.
var xmlNameSecondRanges = [][3]uint32{
	{0x2D, 0x2E, 1},
	{0x30, 0x39, 1},
	{0xB7, 0xB7, 1},
	{0x2D0, 0x2D1, 1},
	{0x300, 0x345, 1},
	{0x360, 0x361, 1},
	{0x387, 0x387, 1},
	{0x483, 0x486, 1},
	{0x591, 0x5A1, 1},
	{0x5A3, 0x5B9, 1},
	{0x5BB, 0x5BD, 1},
	{0x5BF, 0x5BF, 1},
	{0x5C1, 0x5C2, 1},
	{0x64B, 0x652, 1},
	{0x660, 0x669, 1},
	{0x670, 0x670, 1},
	{0x6D6, 0x6DC, 1},
	{0x6DD, 0x6DF, 1},
	{0x6E0, 0x6E4, 1},
	{0x6E7, 0x6E8, 1},
	{0x6EA, 0x6ED, 1},
	{0x6F0, 0x6F9, 1},
	{0x901, 0x903, 1},
	{0x93C, 0x93C, 1},
	{0x93E, 0x94C, 1},
	{0x94D, 0x94D, 1},
	{0x951, 0x954, 1},
	{0x962, 0x963, 1},
	{0x966, 0x96F, 1},
	{0x981, 0x983, 1},
	{0x9BC, 0x9BC, 1},
	{0x9BE, 0x9BF, 1},
	{0x9C0, 0x9C4, 1},
	{0x9C7, 0x9C8, 1},
	{0x9CB, 0x9CD, 1},
	{0x9D7, 0x9D7, 1},
	{0x9E2, 0x9E3, 1},
	{0x9E6, 0x9EF, 1},
	{0xA3E, 0xA3F, 1},
	{0xA40, 0xA42, 1},
	{0xA47, 0xA48, 1},
	{0xA4B, 0xA4D, 1},
	{0xA66, 0xA6F, 1},
	{0xA70, 0xA71, 1},
	{0xA81, 0xA83, 1},
	{0xABC, 0xABC, 1},
	{0xABE, 0xAC5, 1},
	{0xAC7, 0xAC9, 1},
	{0xACB, 0xACD, 1},
	{0xAE6, 0xAEF, 1},
	{0xB01, 0xB03, 1},
	{0xB3C, 0xB3C, 1},
	{0xB3E, 0xB43, 1},
	{0xB47, 0xB48, 1},
	{0xB4B, 0xB4D, 1},
	{0xB56, 0xB57, 1},
	{0xB66, 0xB6F, 1},
	{0xB82, 0xB83, 1},
	{0xBBE, 0xBC2, 1},
	{0xBC6, 0xBC8, 1},
	{0xBCA, 0xBCD, 1},
	{0xBD7, 0xBD7, 1},
	{0xBE7, 0xBEF, 1},
	{0xC01, 0xC03, 1},
	{0xC3E, 0xC44, 1},
	{0xC46, 0xC48, 1},
	{0xC4A, 0xC4D, 1},
	{0xC55, 0xC56, 1},
	{0xC66, 0xC6F, 1},
	{0xC82, 0xC83, 1},
	{0xCBE, 0xCC4, 1},
	{0xCC6, 0xCC8, 1},
	{0xCCA, 0xCCD, 1},
	{0xCD5, 0xCD6, 1},
	{0xCE6, 0xCEF, 1},
	{0xD02, 0xD03, 1},
	{0xD3E, 0xD43, 1},
	{0xD46, 0xD48, 1},
	{0xD4A, 0xD4D, 1},
	{0xD57, 0xD57, 1},
	{0xD66, 0xD6F, 1},
	{0xE31, 0xE31, 1},
	{0xE34, 0xE3A, 1},
	{0xE46, 0xE46, 1},
	{0xE47, 0xE4E, 1},
	{0xE50, 0xE59, 1},
	{0xEB1, 0xEB1, 1},
	{0xEB4, 0xEB9, 1},
	{0xEBB, 0xEBC, 1},
	{0xEC6, 0xEC6, 1},
	{0xEC8, 0xECD, 1},
	{0xED0, 0xED9, 1},
	{0xF18, 0xF19, 1},
	{0xF20, 0xF29, 1},
	{0xF35, 0xF39, 2},
	{0xF3E, 0xF3F, 1},
	{0xF71, 0xF84, 1},
	{0xF86, 0xF8B, 1},
	{0xF90, 0xF95, 1},
	{0xF97, 0xF97, 1},
	{0xF99, 0xFAD, 1},
	{0xFB1, 0xFB7, 1},
	{0xFB9, 0xFB9, 1},
	{0x20D0, 0x20DC, 1},
	{0x302A, 0x302F, 1},
	{0x3031, 0x3035, 1},
	{0x3099, 0x309A, 1},
	{0x309D, 0x309E, 1},
	{0x30FC, 0x30FE, 1},
}

// xmlHTMLEntityData is gc 1.27.1 encoding/xml's htmlEntity table
// (standard HTML entity characters), as data for HTMLEntity.
var xmlHTMLEntityData = map[string]string{
	"nbsp": "\u00A0",
	"iexcl": "\u00A1",
	"cent": "\u00A2",
	"pound": "\u00A3",
	"curren": "\u00A4",
	"yen": "\u00A5",
	"brvbar": "\u00A6",
	"sect": "\u00A7",
	"uml": "\u00A8",
	"copy": "\u00A9",
	"ordf": "\u00AA",
	"laquo": "\u00AB",
	"not": "\u00AC",
	"shy": "\u00AD",
	"reg": "\u00AE",
	"macr": "\u00AF",
	"deg": "\u00B0",
	"plusmn": "\u00B1",
	"sup2": "\u00B2",
	"sup3": "\u00B3",
	"acute": "\u00B4",
	"micro": "\u00B5",
	"para": "\u00B6",
	"middot": "\u00B7",
	"cedil": "\u00B8",
	"sup1": "\u00B9",
	"ordm": "\u00BA",
	"raquo": "\u00BB",
	"frac14": "\u00BC",
	"frac12": "\u00BD",
	"frac34": "\u00BE",
	"iquest": "\u00BF",
	"Agrave": "\u00C0",
	"Aacute": "\u00C1",
	"Acirc": "\u00C2",
	"Atilde": "\u00C3",
	"Auml": "\u00C4",
	"Aring": "\u00C5",
	"AElig": "\u00C6",
	"Ccedil": "\u00C7",
	"Egrave": "\u00C8",
	"Eacute": "\u00C9",
	"Ecirc": "\u00CA",
	"Euml": "\u00CB",
	"Igrave": "\u00CC",
	"Iacute": "\u00CD",
	"Icirc": "\u00CE",
	"Iuml": "\u00CF",
	"ETH": "\u00D0",
	"Ntilde": "\u00D1",
	"Ograve": "\u00D2",
	"Oacute": "\u00D3",
	"Ocirc": "\u00D4",
	"Otilde": "\u00D5",
	"Ouml": "\u00D6",
	"times": "\u00D7",
	"Oslash": "\u00D8",
	"Ugrave": "\u00D9",
	"Uacute": "\u00DA",
	"Ucirc": "\u00DB",
	"Uuml": "\u00DC",
	"Yacute": "\u00DD",
	"THORN": "\u00DE",
	"szlig": "\u00DF",
	"agrave": "\u00E0",
	"aacute": "\u00E1",
	"acirc": "\u00E2",
	"atilde": "\u00E3",
	"auml": "\u00E4",
	"aring": "\u00E5",
	"aelig": "\u00E6",
	"ccedil": "\u00E7",
	"egrave": "\u00E8",
	"eacute": "\u00E9",
	"ecirc": "\u00EA",
	"euml": "\u00EB",
	"igrave": "\u00EC",
	"iacute": "\u00ED",
	"icirc": "\u00EE",
	"iuml": "\u00EF",
	"eth": "\u00F0",
	"ntilde": "\u00F1",
	"ograve": "\u00F2",
	"oacute": "\u00F3",
	"ocirc": "\u00F4",
	"otilde": "\u00F5",
	"ouml": "\u00F6",
	"divide": "\u00F7",
	"oslash": "\u00F8",
	"ugrave": "\u00F9",
	"uacute": "\u00FA",
	"ucirc": "\u00FB",
	"uuml": "\u00FC",
	"yacute": "\u00FD",
	"thorn": "\u00FE",
	"yuml": "\u00FF",
	"fnof": "\u0192",
	"Alpha": "\u0391",
	"Beta": "\u0392",
	"Gamma": "\u0393",
	"Delta": "\u0394",
	"Epsilon": "\u0395",
	"Zeta": "\u0396",
	"Eta": "\u0397",
	"Theta": "\u0398",
	"Iota": "\u0399",
	"Kappa": "\u039A",
	"Lambda": "\u039B",
	"Mu": "\u039C",
	"Nu": "\u039D",
	"Xi": "\u039E",
	"Omicron": "\u039F",
	"Pi": "\u03A0",
	"Rho": "\u03A1",
	"Sigma": "\u03A3",
	"Tau": "\u03A4",
	"Upsilon": "\u03A5",
	"Phi": "\u03A6",
	"Chi": "\u03A7",
	"Psi": "\u03A8",
	"Omega": "\u03A9",
	"alpha": "\u03B1",
	"beta": "\u03B2",
	"gamma": "\u03B3",
	"delta": "\u03B4",
	"epsilon": "\u03B5",
	"zeta": "\u03B6",
	"eta": "\u03B7",
	"theta": "\u03B8",
	"iota": "\u03B9",
	"kappa": "\u03BA",
	"lambda": "\u03BB",
	"mu": "\u03BC",
	"nu": "\u03BD",
	"xi": "\u03BE",
	"omicron": "\u03BF",
	"pi": "\u03C0",
	"rho": "\u03C1",
	"sigmaf": "\u03C2",
	"sigma": "\u03C3",
	"tau": "\u03C4",
	"upsilon": "\u03C5",
	"phi": "\u03C6",
	"chi": "\u03C7",
	"psi": "\u03C8",
	"omega": "\u03C9",
	"thetasym": "\u03D1",
	"upsih": "\u03D2",
	"piv": "\u03D6",
	"bull": "\u2022",
	"hellip": "\u2026",
	"prime": "\u2032",
	"Prime": "\u2033",
	"oline": "\u203E",
	"frasl": "\u2044",
	"weierp": "\u2118",
	"image": "\u2111",
	"real": "\u211C",
	"trade": "\u2122",
	"alefsym": "\u2135",
	"larr": "\u2190",
	"uarr": "\u2191",
	"rarr": "\u2192",
	"darr": "\u2193",
	"harr": "\u2194",
	"crarr": "\u21B5",
	"lArr": "\u21D0",
	"uArr": "\u21D1",
	"rArr": "\u21D2",
	"dArr": "\u21D3",
	"hArr": "\u21D4",
	"forall": "\u2200",
	"part": "\u2202",
	"exist": "\u2203",
	"empty": "\u2205",
	"nabla": "\u2207",
	"isin": "\u2208",
	"notin": "\u2209",
	"ni": "\u220B",
	"prod": "\u220F",
	"sum": "\u2211",
	"minus": "\u2212",
	"lowast": "\u2217",
	"radic": "\u221A",
	"prop": "\u221D",
	"infin": "\u221E",
	"ang": "\u2220",
	"and": "\u2227",
	"or": "\u2228",
	"cap": "\u2229",
	"cup": "\u222A",
	"int": "\u222B",
	"there4": "\u2234",
	"sim": "\u223C",
	"cong": "\u2245",
	"asymp": "\u2248",
	"ne": "\u2260",
	"equiv": "\u2261",
	"le": "\u2264",
	"ge": "\u2265",
	"sub": "\u2282",
	"sup": "\u2283",
	"nsub": "\u2284",
	"sube": "\u2286",
	"supe": "\u2287",
	"oplus": "\u2295",
	"otimes": "\u2297",
	"perp": "\u22A5",
	"sdot": "\u22C5",
	"lceil": "\u2308",
	"rceil": "\u2309",
	"lfloor": "\u230A",
	"rfloor": "\u230B",
	"lang": "\u2329",
	"rang": "\u232A",
	"loz": "\u25CA",
	"spades": "\u2660",
	"clubs": "\u2663",
	"hearts": "\u2665",
	"diams": "\u2666",
	"quot": "\u0022",
	"amp": "\u0026",
	"lt": "\u003C",
	"gt": "\u003E",
	"OElig": "\u0152",
	"oelig": "\u0153",
	"Scaron": "\u0160",
	"scaron": "\u0161",
	"Yuml": "\u0178",
	"circ": "\u02C6",
	"tilde": "\u02DC",
	"ensp": "\u2002",
	"emsp": "\u2003",
	"thinsp": "\u2009",
	"zwnj": "\u200C",
	"zwj": "\u200D",
	"lrm": "\u200E",
	"rlm": "\u200F",
	"ndash": "\u2013",
	"mdash": "\u2014",
	"lsquo": "\u2018",
	"rsquo": "\u2019",
	"sbquo": "\u201A",
	"ldquo": "\u201C",
	"rdquo": "\u201D",
	"bdquo": "\u201E",
	"dagger": "\u2020",
	"Dagger": "\u2021",
	"permil": "\u2030",
	"lsaquo": "\u2039",
	"rsaquo": "\u203A",
	"euro": "\u20AC",
}

// HTMLEntity is an entity map containing translations for the
// standard HTML entity characters.
//
// See the [Decoder.Strict] and [Decoder.Entity] fields' documentation.
// (Declared after the data tables: package-var initializers cannot
// reference later-declared vars.)
var HTMLEntity map[string]string = xmlHTMLEntityData

// HTMLAutoClose is the set of HTML elements that
// should be considered to close automatically.
//
// See the [Decoder.Strict] and [Decoder.Entity] fields' documentation.
var HTMLAutoClose []string = xmlHTMLAutoCloseData
`
