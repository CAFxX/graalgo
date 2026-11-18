// Package ir is graalgo's per-package typed IR container, GIR
// (docs/arch/toolexec-design.md §3.2–§3.4): a resolved typed AST with a
// string table, a type table keyed by canonical type keys, and one lazily
// decodable blob per function.
//
// The node and record types, and their codecs, are generated from
// schema.txt (gir_gen.go); this file holds the wire primitives, the file
// header and section directory, and the validator.
//
// File layout:
//
//	"GIR1" | uvarint SchemaVersion | SchemaHash (8 bytes)
//	       | str goVersion | str GOOS | str GOARCH      (length-prefixed)
//	       | uvarint nsections | (uvarint id, uvarint length)*
//	       | section payloads, in directory order
//
// Every section of the schema is present, in ascending id order. Integers
// are minimal (canonical) LEB128 varints; a decoder rejects non-minimal
// encodings, so that decode followed by encode is the identity on every
// valid file whose string table is in first-use order.
package ir

//go:generate go run ./gen

import (
	"bytes"
	"errors"
	"fmt"
)

// Magic starts every GIR file.
const Magic = "GIR1"

// TypeID is a 1-based index into File.Types; 0 means none.
type TypeID uint32

// PosID is a 1-based index into the enclosing FuncBody's Positions; 0
// means none.
type PosID uint32

type refKind int

const (
	refType refKind = iota
	refSym
	refObj
	refFn
)

var refKindNames = [...]string{"type", "sym", "obj", "fn"}

// FormatError reports malformed GIR input.
type FormatError struct {
	Off int // byte offset in the input where decoding failed
	Msg string
}

func (e *FormatError) Error() string { return fmt.Sprintf("gir: offset %d: %s", e.Off, e.Msg) }

// ---------------------------------------------------------------- Writer

// Writer encodes GIR primitives into a buffer and interns strings.
type Writer struct {
	buf     []byte
	strIdx  map[string]uint32
	strList []string
}

func newWriter() *Writer {
	w := &Writer{strIdx: map[string]uint32{}}
	w.intern("") // string 0 is always ""
	return w
}

func (w *Writer) intern(s string) uint32 {
	if i, ok := w.strIdx[s]; ok {
		return i
	}
	i := uint32(len(w.strList))
	w.strIdx[s] = i
	w.strList = append(w.strList, s)
	return i
}

func (w *Writer) uvarint(v uint64) {
	for v >= 0x80 {
		w.buf = append(w.buf, byte(v)|0x80)
		v >>= 7
	}
	w.buf = append(w.buf, byte(v))
}

func (w *Writer) varint(v int64) { w.uvarint(uint64(v<<1) ^ uint64(v>>63)) }

func (w *Writer) bool(v bool) {
	if v {
		w.buf = append(w.buf, 1)
	} else {
		w.buf = append(w.buf, 0)
	}
}

func (w *Writer) bytes(b []byte) {
	w.uvarint(uint64(len(b)))
	w.buf = append(w.buf, b...)
}

func (w *Writer) rawString(s string) {
	w.uvarint(uint64(len(s)))
	w.buf = append(w.buf, s...)
}

func (w *Writer) str(s string) { w.uvarint(uint64(w.intern(s))) }

func (w *Writer) optRef(v int32) {
	if v < -1 {
		panic(fmt.Sprintf("ir: optional reference %d", v))
	}
	w.uvarint(uint64(int64(v) + 1))
}

// take returns the bytes written so far and resets the buffer.
func (w *Writer) take() []byte {
	b := w.buf
	w.buf = nil
	return b
}

// Encode serializes f. Every function that is not bodyless must have its
// body in f.Bodies; Funcs[i].Body and Size are computed here.
func Encode(f *File) ([]byte, error) {
	if len(f.Bodies) != len(f.Funcs) {
		return nil, fmt.Errorf("ir: %d bodies for %d funcs", len(f.Bodies), len(f.Funcs))
	}
	w := newWriter()
	// Bodies first: their sizes and offsets go into FUNCINDEX.
	var bodies []byte
	for i := range f.Funcs {
		fn := &f.Funcs[i]
		if f.Bodies[i] == nil {
			fn.Body, fn.Size = 0, 0
			continue
		}
		w.writeFuncBody(f.Bodies[i])
		b := w.take()
		fn.Body, fn.Size = uint64(len(bodies)), uint64(len(b))
		bodies = append(bodies, b...)
	}
	payload := map[int][]byte{secBODIES: bodies, secDATA: f.Data}
	for _, id := range sectionIDs {
		switch id {
		case secSTRINGS, secBODIES, secDATA:
			continue
		}
		w.writeTypedSection(f, id)
		payload[id] = w.take()
	}
	w.uvarint(uint64(len(w.strList)))
	for _, s := range w.strList {
		w.rawString(s)
	}
	payload[secSTRINGS] = w.take()

	var out bytes.Buffer
	out.WriteString(Magic)
	w.uvarint(SchemaVersion)
	w.buf = append(w.buf, SchemaHash[:]...)
	w.rawString(f.GoVersion)
	w.rawString(f.GOOS)
	w.rawString(f.GOARCH)
	w.uvarint(uint64(len(sectionIDs)))
	for _, id := range sectionIDs {
		w.uvarint(uint64(id))
		w.uvarint(uint64(len(payload[id])))
	}
	out.Write(w.take())
	for _, id := range sectionIDs {
		out.Write(payload[id])
	}
	return out.Bytes(), nil
}

// ---------------------------------------------------------------- Reader

type scope struct{ npos, nlocals, nlabels int }

// Reader decodes GIR primitives from a byte slice. Malformed input makes
// it panic with a *FormatError, which the entry points recover.
type Reader struct {
	data   []byte
	off    int
	base   int // offset of data within the whole file, for messages
	strs   []string
	counts [4]int // by refKind
	scopes []scope
	depth  int
}

func (r *Reader) failf(format string, a ...any) {
	panic(&FormatError{Off: r.base + r.off, Msg: fmt.Sprintf(format, a...)})
}

func (r *Reader) remaining() int { return len(r.data) - r.off }

func (r *Reader) uvarint() uint64 {
	var v uint64
	for shift, i := uint(0), 0; ; shift, i = shift+7, i+1 {
		if r.off >= len(r.data) {
			r.failf("truncated varint")
		}
		c := r.data[r.off]
		r.off++
		if i == 9 && c > 1 {
			r.failf("varint overflows 64 bits")
		}
		v |= uint64(c&0x7f) << shift
		if c < 0x80 {
			if c == 0 && i > 0 {
				r.failf("non-minimal varint")
			}
			return v
		}
	}
}

func (r *Reader) varint() int64 {
	u := r.uvarint()
	return int64(u>>1) ^ -int64(u&1)
}

func (r *Reader) bool() bool {
	if r.off >= len(r.data) {
		r.failf("truncated bool")
	}
	c := r.data[r.off]
	r.off++
	if c > 1 {
		r.failf("bad bool %d", c)
	}
	return c == 1
}

// count reads a list length. Every list element occupies at least one
// byte, so a count above the remaining input is malformed; this bounds
// every allocation by the input size.
func (r *Reader) count() int {
	n := r.uvarint()
	if n > uint64(r.remaining()) {
		r.failf("list of %d elements exceeds the %d remaining bytes", n, r.remaining())
	}
	return int(n)
}

func (r *Reader) rawBytes() []byte {
	n := r.uvarint()
	if n > uint64(r.remaining()) {
		r.failf("byte string of %d bytes exceeds the %d remaining", n, r.remaining())
	}
	b := r.data[r.off : r.off+int(n)]
	r.off += int(n)
	return b
}

func (r *Reader) bytes() []byte { return bytes.Clone(r.rawBytes()) }

func (r *Reader) str() string {
	i := r.uvarint()
	if i >= uint64(len(r.strs)) {
		r.failf("string index %d out of range [0,%d)", i, len(r.strs))
	}
	return r.strs[i]
}

func (r *Reader) typeRef() TypeID {
	v := r.uvarint()
	if v > uint64(r.counts[refType]) {
		r.failf("type id %d out of range [0,%d]", v, r.counts[refType])
	}
	return TypeID(v)
}

func (r *Reader) ref(k refKind) uint32 {
	v := r.uvarint()
	if v >= uint64(r.counts[k]) {
		r.failf("%s index %d out of range [0,%d)", refKindNames[k], v, r.counts[k])
	}
	return uint32(v)
}

func (r *Reader) optRef(k refKind) int32 {
	v := r.uvarint()
	if v > uint64(r.counts[k]) {
		r.failf("optional %s index %d out of range", refKindNames[k], int64(v)-1)
	}
	return int32(int64(v) - 1)
}

func (r *Reader) scope() *scope {
	if len(r.scopes) == 0 {
		r.failf("local reference outside a function body")
	}
	return &r.scopes[len(r.scopes)-1]
}

func (r *Reader) pushScope() { r.scopes = append(r.scopes, scope{}) }
func (r *Reader) popScope()  { r.scopes = r.scopes[:len(r.scopes)-1] }

func (r *Reader) posRef() PosID {
	v := r.uvarint()
	if v > uint64(r.scope().npos) {
		r.failf("position %d out of range [0,%d]", v, r.scope().npos)
	}
	return PosID(v)
}

func (r *Reader) localRef() uint32 {
	v := r.uvarint()
	if v >= uint64(r.scope().nlocals) {
		r.failf("local %d out of range [0,%d)", v, r.scope().nlocals)
	}
	return uint32(v)
}

func (r *Reader) optLocalRef() int32 {
	v := r.uvarint()
	if v > uint64(r.scope().nlocals) {
		r.failf("optional local %d out of range", int64(v)-1)
	}
	return int32(int64(v) - 1)
}

func (r *Reader) labelRef() uint32 {
	v := r.uvarint()
	if v >= uint64(r.scope().nlabels) {
		r.failf("label %d out of range [0,%d)", v, r.scope().nlabels)
	}
	return uint32(v)
}

func (r *Reader) optLabelRef() int32 {
	v := r.uvarint()
	if v > uint64(r.scope().nlabels) {
		r.failf("optional label %d out of range", int64(v)-1)
	}
	return int32(int64(v) - 1)
}

func (r *Reader) enter() {
	r.depth++
	if r.depth > MaxDepth {
		r.failf("nesting deeper than %d", MaxDepth)
	}
}

func (r *Reader) leave() { r.depth-- }

func catch(err *error) {
	if p := recover(); p != nil {
		fe, ok := p.(*FormatError)
		if !ok {
			panic(p)
		}
		*err = fe
	}
}

// sub returns a reader over data[off:off+n] sharing r's tables.
func (r *Reader) sub(off, n int) *Reader {
	return &Reader{data: r.data[off : off+n], base: r.base + off, strs: r.strs, counts: r.counts}
}

// Decode parses a GIR file. Function bodies are not decoded: Body decodes
// one on demand, Validate decodes and checks all of them.
func Decode(data []byte) (f *File, err error) {
	defer catch(&err)
	r := &Reader{data: data}
	if len(data) < len(Magic) || string(data[:len(Magic)]) != Magic {
		r.failf("not a GIR file (bad magic)")
	}
	r.off = len(Magic)
	if v := r.uvarint(); v != SchemaVersion {
		r.failf("GIR version %d, this decoder reads version %d", v, SchemaVersion)
	}
	if r.remaining() < len(SchemaHash) || !bytes.Equal(data[r.off:r.off+len(SchemaHash)], SchemaHash[:]) {
		r.failf("GIR schema hash mismatch (encoder and decoder built from different schema.txt)")
	}
	r.off += len(SchemaHash)
	f = &File{}
	f.GoVersion = string(r.rawBytes())
	f.GOOS = string(r.rawBytes())
	f.GOARCH = string(r.rawBytes())
	n := r.count()
	if n != len(sectionIDs) {
		r.failf("%d sections, want %d", n, len(sectionIDs))
	}
	type span struct{ off, n int }
	spans := map[int]span{}
	lengths := make([]uint64, n)
	for i := range n {
		id := r.uvarint()
		if id != uint64(sectionIDs[i]) {
			r.failf("section %d is id %d, want %d (%s)", i, id, sectionIDs[i], sectionNames[sectionIDs[i]])
		}
		lengths[i] = r.uvarint()
	}
	off := r.off
	for i, id := range sectionIDs {
		if lengths[i] > uint64(len(data)-off) {
			r.failf("section %s (%d bytes) exceeds the input", sectionNames[id], lengths[i])
		}
		spans[id] = span{off, int(lengths[i])}
		off += int(lengths[i])
	}
	if off != len(data) {
		r.off = off
		r.failf("%d trailing bytes after the last section", len(data)-off)
	}
	// Table sizes first: references may point forward across sections.
	for k, id := range refSections {
		sp := spans[id]
		pr := r.sub(sp.off, sp.n)
		r.counts[k] = pr.count()
	}
	// Strings.
	sp := spans[secSTRINGS]
	sr := r.sub(sp.off, sp.n)
	ns := sr.count()
	r.strs = make([]string, ns)
	for i := range ns {
		r.strs[i] = string(sr.rawBytes())
	}
	if sr.remaining() != 0 {
		sr.failf("%d trailing bytes in STRINGS", sr.remaining())
	}
	if ns == 0 || r.strs[0] != "" {
		sr.failf("string 0 must be the empty string")
	}
	f.strings = r.strs
	for _, id := range sectionIDs {
		sp := spans[id]
		switch id {
		case secSTRINGS:
			continue
		case secBODIES:
			f.bodies = data[sp.off : sp.off+sp.n]
			continue
		case secDATA:
			f.Data = bytes.Clone(data[sp.off : sp.off+sp.n])
			continue
		}
		sr := r.sub(sp.off, sp.n)
		sr.readTypedSection(f, id)
		if sr.remaining() != 0 {
			sr.failf("%d trailing bytes in section %s", sr.remaining(), sectionNames[id])
		}
	}
	// FUNCINDEX body layout: contiguous in FUNCINDEX order, no gaps, so
	// that bodies never overlap and re-encoding reproduces offsets.
	next := uint64(0)
	for i, fn := range f.Funcs {
		if fn.Size == 0 {
			if fn.Body != 0 {
				r.failf("func %d (%s): empty body at offset %d, want 0", i, fn.Name, fn.Body)
			}
			continue
		}
		if fn.Body != next {
			r.failf("func %d (%s): body at %d, want %d (bodies are contiguous in FUNCINDEX order)", i, fn.Name, fn.Body, next)
		}
		if fn.Size > uint64(len(f.bodies))-next {
			r.failf("func %d (%s): body exceeds BODIES", i, fn.Name)
		}
		next += fn.Size
	}
	if next != uint64(len(f.bodies)) {
		r.failf("BODIES has %d bytes, FUNCINDEX covers %d", len(f.bodies), next)
	}
	f.Bodies = make([]*FuncBody, len(f.Funcs))
	f.counts = r.counts
	return f, nil
}

// Body decodes (once) and returns the body of Funcs[i]; nil for a
// function without a body.
func (f *File) Body(i int) (body *FuncBody, err error) {
	if i < 0 || i >= len(f.Funcs) {
		return nil, fmt.Errorf("gir: no func %d", i)
	}
	if f.Bodies[i] != nil || f.Funcs[i].Size == 0 {
		return f.Bodies[i], nil
	}
	defer catch(&err)
	fn := f.Funcs[i]
	r := &Reader{data: f.bodies[fn.Body : fn.Body+fn.Size], strs: f.strings, counts: f.counts}
	b := &FuncBody{}
	r.readFuncBody(b)
	if r.remaining() != 0 {
		r.failf("func %d (%s): %d trailing bytes in body", i, fn.Name, r.remaining())
	}
	f.Bodies[i] = b
	return b, nil
}

// Validate decodes every body of f and checks the file-level invariants
// the decoder does not already enforce.
func (f *File) Validate() error {
	if f.Pkg.Path == "" {
		return errors.New("gir: empty package path")
	}
	for i := range f.Funcs {
		if _, err := f.Body(i); err != nil {
			return err
		}
		const bodyless = 1
		if (f.Funcs[i].Flags&bodyless != 0) != (f.Funcs[i].Size == 0) {
			return fmt.Errorf("gir: func %d (%s): bodyless flag disagrees with body size %d", i, f.Funcs[i].Name, f.Funcs[i].Size)
		}
	}
	return nil
}

// DecodeValidate is Decode followed by Validate.
func DecodeValidate(data []byte) (*File, error) {
	f, err := Decode(data)
	if err != nil {
		return nil, err
	}
	if err := f.Validate(); err != nil {
		return nil, err
	}
	return f, nil
}

// SectionSizes returns the payload size of each section of a GIR file, by
// section name (nil if the header does not parse).
func SectionSizes(data []byte) (sizes map[string]int) {
	defer func() {
		if recover() != nil {
			sizes = nil
		}
	}()
	r := &Reader{data: data, off: len(Magic)}
	r.uvarint()
	r.off += len(SchemaHash)
	for range 3 {
		r.rawBytes()
	}
	n := r.count()
	sizes = map[string]int{}
	for range n {
		id := r.uvarint()
		sizes[sectionNames[int(id)]] = int(r.uvarint())
	}
	return sizes
}
