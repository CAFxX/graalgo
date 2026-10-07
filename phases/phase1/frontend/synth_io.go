// P3-M8 (wave 2): synthetic `io` package.
//
// REAL Go source, embedded as text and type-checked on demand with the
// overlay importer (imports: "errors" → the P3-M7 synthetic package;
// "sync" → the P3-M2 surface, Mutex/Once only — see deviations D1/D2
// below). The resulting *types.Package is what overlayImporter.Import("io")
// returns, so the user's type-check and the synthetic source's own
// type-check share identical *types.Func/*types.TypeName objects; the
// exporter then emits the synthetic declarations' bodies into the same
// wire document (shared type/object tables) via exportSyntheticPackages.
//
// Exact gc 1.27.1 public API signatures (io.go, multi.go, pipe.go —
// vendored at ~/workspace/graalgo-stdlib/go1.27.1/io/). io/fs and
// io/ioutil are separate packages and explicitly OUT of scope.
//
// WHAT IS REAL vs FAIL-FAST — verified against the exporter, not
// assumed (v1 cannot emit or call multi-result functions, WS D; the
// P3-M3 F4 precedent):
//   - REAL: all 22 interfaces (type declarations only), the Seek consts,
//     the error vars, Discard, every single-result constructor
//     (LimitReader, NewSectionReader, NewOffsetWriter, TeeReader,
//     NopCloser, MultiReader, MultiWriter), Pipe (via the __pipe helper
//   - pipePair desugar), PipeReader.Read / PipeWriter.Write (via
//     __-helpers over channels/select), Close and CloseWithError on both
//     pipe halves (single-result, real), SectionReader.Seek/Size/Outer
//     (pure; Outer via the triple desugar), OffsetWriter.Seek (pure).
//   - FAIL-FAST (exact gc signatures with panic bodies; deliberately NOT
//     in the Appendix M table, so every call site fails at EXPORT with
//     the named M8 scope error via isMultiResultSynthetic — never a
//     silent stub, never a runtime surprise): Copy, CopyN, CopyBuffer,
//     WriteString, ReadAtLeast, ReadFull, ReadAll, LimitedReader.Read,
//     SectionReader.Read, SectionReader.ReadAt, OffsetWriter.Write,
//     OffsetWriter.WriteAt, teeReader.Read, discard.ReadFrom,
//     multiReader.Read, multiReader.WriteTo, multiWriter.Write,
//     multiWriter.WriteString, nopCloserWriterTo.WriteTo. Each of these
//     needs a 2-result call on an interface value (src.Read, dst.Write,
//     wt.WriteTo, rf.ReadFrom). v1 interface dispatch (ADR-0003) is
//     single-result only and no 2-result funcdecl can be emitted at all
//     (exportFuncDecl scope-errors), so this is a hard WS D boundary,
//     not a gap in this source.
//
// Re-test trigger (all fail-fast members): when WS D lands multi-result
// calls, give each a real __-helper + Appendix M table entry and re-run
// the io corpus (phases/phase3/corpus/wave2/io/).
//
// DEVIATIONS from gc 1.27.1 (all confined to unexported structure;
// none observable through the public API):
//
//	D1. pipe holds *sync.Mutex / *sync.Once pointer fields. v1 forbids
//	    bare sync primitives in composite positions (M2 scope —
//	    TestSyncCompositeScopeErrors pins it), and sync.Pool is not in
//	    the M2 surface at all.
//	D2. onceError uses a named mu *sync.Mutex field instead of embedding
//	    sync.Mutex (same M2 reason as D1).
//	D3. PipeReader embeds *pipe instead of pipe: v1 cannot take &r.pipe
//	    (WS D), and the pipe state must be pointer-shared — the onceError
//	    store/load protocol breaks if the struct is ever copied.
//	D4. discard.ReadFrom is NOT rewritten with a fresh 8KB buffer per
//	    call (the analysis's option b): the blocker is 2-result
//	    interface dispatch (WS D), not sync.Pool — the rewrite would
//	    still need r.Read(*bufp). It fails fast with the rest.
//	D5. __pipeWrite uses explicit Unlock instead of defer (behavior-
//	    identical; the critical section cannot panic).
//
// Interface-value call sites (n, err := r.Read(p) with r io.Reader):
// checked against ADR-0003 — IfaceDispatchNode is single-result and no
// 2-result method can be emitted, so these fail fast at export with the
// named M8 scope error (isMultiResultSynthetic covers package "io").
// Loud by design; re-test when WS D lands.
package main

// syntheticIoSource is the P3-M8 synthetic `io` package: exact gc 1.27.1
// signatures for the full public API (io.go + multi.go + pipe.go); real
// implementations where v1 can express them (see the header inventory);
// fail-fast bodies everywhere else. Multi-result members with real
// __-helpers desugar at 2-target call sites (Appendix M, mrdesugar_m5.go);
// every other multi-result position fails fast with the named M8 error.
const syntheticIoSource = `package io

import (
	"errors"
	"sync"
)

// Seek whence values.
const (
	SeekStart   = 0 // seek relative to the origin of the file
	SeekCurrent = 1 // seek relative to the current offset
	SeekEnd     = 2 // seek relative to the end
)

// ErrShortWrite means that a write accepted fewer bytes than requested
// but failed to return an explicit error.
var ErrShortWrite = errors.New("short write")

// errInvalidWrite means that a write returned an impossible count.
var errInvalidWrite = errors.New("invalid write result")

// ErrShortBuffer means that a read required a longer buffer than was provided.
var ErrShortBuffer = errors.New("short buffer")

// EOF is the error returned by Read when no more input is available.
var EOF = errors.New("EOF")

// ErrUnexpectedEOF means that EOF was encountered in the
// middle of reading a fixed-size block or data structure.
var ErrUnexpectedEOF = errors.New("unexpected EOF")

// ErrNoProgress is returned by some clients of a Reader when
// many calls to Read have failed to return any data or error,
// usually the sign of a broken Reader implementation.
var ErrNoProgress = errors.New("multiple Read calls return no data or error")

// Reader is the interface that wraps the basic Read method.
type Reader interface {
	Read(p []byte) (n int, err error)
}

// Writer is the interface that wraps the basic Write method.
type Writer interface {
	Write(p []byte) (n int, err error)
}

// Closer is the interface that wraps the basic Close method.
type Closer interface {
	Close() error
}

// Seeker is the interface that wraps the basic Seek method.
type Seeker interface {
	Seek(offset int64, whence int) (int64, error)
}

// ReadWriter is the interface that groups the basic Read and Write methods.
type ReadWriter interface {
	Reader
	Writer
}

// ReadCloser is the interface that groups the basic Read and Close methods.
type ReadCloser interface {
	Reader
	Closer
}

// WriteCloser is the interface that groups the basic Write and Close methods.
type WriteCloser interface {
	Writer
	Closer
}

// ReadWriteCloser is the interface that groups the basic Read, Write and Close methods.
type ReadWriteCloser interface {
	Reader
	Writer
	Closer
}

// ReadSeeker is the interface that groups the basic Read and Seek methods.
type ReadSeeker interface {
	Reader
	Seeker
}

// ReadSeekCloser is the interface that groups the basic Read, Seek and Close
// methods.
type ReadSeekCloser interface {
	Reader
	Seeker
	Closer
}

// WriteSeeker is the interface that groups the basic Write and Seek methods.
type WriteSeeker interface {
	Writer
	Seeker
}

// ReadWriteSeeker is the interface that groups the basic Read, Write and Seek methods.
type ReadWriteSeeker interface {
	Reader
	Writer
	Seeker
}

// ReaderFrom is the interface that wraps the ReadFrom method.
type ReaderFrom interface {
	ReadFrom(r Reader) (n int64, err error)
}

// WriterTo is the interface that wraps the WriteTo method.
type WriterTo interface {
	WriteTo(w Writer) (n int64, err error)
}

// ReaderAt is the interface that wraps the basic ReadAt method.
type ReaderAt interface {
	ReadAt(p []byte, off int64) (n int, err error)
}

// WriterAt is the interface that wraps the basic WriteAt method.
type WriterAt interface {
	WriteAt(p []byte, off int64) (n int, err error)
}

// ByteReader is the interface that wraps the ReadByte method.
type ByteReader interface {
	ReadByte() (byte, error)
}

// ByteScanner is the interface that adds the UnreadByte method to the
// basic ReadByte method.
type ByteScanner interface {
	ByteReader
	UnreadByte() error
}

// ByteWriter is the interface that wraps the WriteByte method.
type ByteWriter interface {
	WriteByte(c byte) error
}

// RuneReader is the interface that wraps the ReadRune method.
type RuneReader interface {
	ReadRune() (r rune, size int, err error)
}

// RuneScanner is the interface that adds the UnreadRune method to the
// basic ReadRune method.
type RuneScanner interface {
	RuneReader
	UnreadRune() error
}

// StringWriter is the interface that wraps the WriteString method.
type StringWriter interface {
	WriteString(s string) (n int, err error)
}

// WriteString writes the contents of the string s to w, which accepts a slice of bytes.
// If w implements StringWriter, StringWriter.WriteString is invoked directly.
// Otherwise, Writer.Write is called exactly once.
//
// M8 scope: fail-fast — both dispatch arms need a 2-result call on an
// interface value (WS D). Re-test when WS D lands.
func WriteString(w Writer, s string) (n int, err error) {
	panic("io.WriteString: not in M8 scope (needs 2-result interface dispatch, WS D; re-test when WS D lands)")
}

// ReadAtLeast reads from r into buf until it has read at least min bytes.
//
// M8 scope: fail-fast — the loop needs r.Read (2-result interface call,
// WS D). Re-test when WS D lands.
func ReadAtLeast(r Reader, buf []byte, min int) (n int, err error) {
	panic("io.ReadAtLeast: not in M8 scope (needs 2-result interface dispatch, WS D; re-test when WS D lands)")
}

// ReadFull reads exactly len(buf) bytes from r into buf.
//
// M8 scope: fail-fast (see ReadAtLeast). Re-test when WS D lands.
func ReadFull(r Reader, buf []byte) (n int, err error) {
	panic("io.ReadFull: not in M8 scope (needs 2-result interface dispatch, WS D; re-test when WS D lands)")
}

// CopyN copies n bytes (or until an error) from src to dst.
//
// M8 scope: fail-fast (see Copy). Re-test when WS D lands.
func CopyN(dst Writer, src Reader, n int64) (written int64, err error) {
	panic("io.CopyN: not in M8 scope (needs 2-result interface dispatch, WS D; re-test when WS D lands)")
}

// Copy copies from src to dst until either EOF is reached
// on src or an error occurs.
//
// M8 scope: fail-fast — the WriterTo/ReaderFrom dispatch and the manual
// loop all need 2-result interface calls (WS D). Re-test when WS D lands.
func Copy(dst Writer, src Reader) (written int64, err error) {
	panic("io.Copy: not in M8 scope (needs 2-result interface dispatch, WS D; re-test when WS D lands)")
}

// CopyBuffer is identical to Copy except that it stages through the
// provided buffer (if one is required) rather than allocating a
// temporary one.
//
// M8 scope: fail-fast (see Copy). Re-test when WS D lands.
func CopyBuffer(dst Writer, src Reader, buf []byte) (written int64, err error) {
	panic("io.CopyBuffer: not in M8 scope (needs 2-result interface dispatch, WS D; re-test when WS D lands)")
}

// copyBuffer is the actual implementation of Copy and CopyBuffer.
//
// M8 scope: fail-fast (see Copy). Re-test when WS D lands.
func copyBuffer(dst Writer, src Reader, buf []byte) (written int64, err error) {
	panic("io.copyBuffer: not in M8 scope (needs 2-result interface dispatch, WS D; re-test when WS D lands)")
}

// LimitReader returns a Reader that reads from r
// but stops with EOF after n bytes.
// The underlying implementation is a *LimitedReader.
func LimitReader(r Reader, n int64) Reader { return &LimitedReader{r, n} }

// A LimitedReader reads from R but limits the amount of
// data returned to just N bytes. Each call to Read
// updates N to reflect the new amount remaining.
// Read returns EOF when N <= 0 or when the underlying R returns EOF.
type LimitedReader struct {
	R Reader // underlying reader
	N int64  // max bytes remaining
}

// Read implements Reader.
//
// M8 scope: fail-fast — needs l.R.Read (2-result interface call, WS D).
// Re-test when WS D lands.
func (l *LimitedReader) Read(p []byte) (n int, err error) {
	panic("io.LimitedReader.Read: not in M8 scope (needs 2-result interface dispatch, WS D; re-test when WS D lands)")
}

// NewSectionReader returns a SectionReader that reads from r
// starting at offset off and stops with EOF after n bytes.
func NewSectionReader(r ReaderAt, off int64, n int64) *SectionReader {
	var remaining int64
	const maxint64 = 1<<63 - 1
	if off <= maxint64-n {
		remaining = n + off
	} else {
		// Overflow, with no way to return error.
		// Assume we can read up to an offset of 1<<63 - 1.
		remaining = maxint64
	}
	return &SectionReader{r, off, off, remaining, n}
}

// SectionReader implements Read, Seek, and ReadAt on a section
// of an underlying ReaderAt.
type SectionReader struct {
	r     ReaderAt // constant after creation
	base  int64    // constant after creation
	off   int64
	limit int64 // constant after creation
	n     int64 // constant after creation
}

// Read implements Reader.
//
// M8 scope: fail-fast — needs s.r.ReadAt (2-result interface call,
// WS D). Re-test when WS D lands.
func (s *SectionReader) Read(p []byte) (n int, err error) {
	panic("io.SectionReader.Read: not in M8 scope (needs 2-result interface dispatch, WS D; re-test when WS D lands)")
}

var errWhence = errors.New("Seek: invalid whence")
var errOffset = errors.New("Seek: invalid offset")

// Seek implements Seeker. Multi-result: the real implementation lives in
// __sectionReaderSeek; 2-target call sites desugar to it (Appendix M).
func (s *SectionReader) Seek(offset int64, whence int) (int64, error) {
	panic("io.SectionReader.Seek: not in M8 scope (multi-result calls need WS D; re-test when it lands)")
}

// sectionReaderSeekPair is the Appendix M carrier for SectionReader.Seek.
type sectionReaderSeekPair struct {
	n   int64
	err error
}

// __sectionReaderSeek is the real SectionReader.Seek: pure offset
// arithmetic, no interface dispatch — v1-expressible.
func __sectionReaderSeek(s *SectionReader, offset int64, whence int) sectionReaderSeekPair {
	switch whence {
	default:
		return sectionReaderSeekPair{0, errWhence}
	case SeekStart:
		offset += s.base
	case SeekCurrent:
		offset += s.off
	case SeekEnd:
		offset += s.limit
	}
	if offset < s.base {
		return sectionReaderSeekPair{0, errOffset}
	}
	s.off = offset
	return sectionReaderSeekPair{offset - s.base, nil}
}

// ReadAt implements ReaderAt.
//
// M8 scope: fail-fast — needs s.r.ReadAt (2-result interface call,
// WS D). Re-test when WS D lands.
func (s *SectionReader) ReadAt(p []byte, off int64) (n int, err error) {
	panic("io.SectionReader.ReadAt: not in M8 scope (needs 2-result interface dispatch, WS D; re-test when WS D lands)")
}

// Size returns the size of the section in bytes.
func (s *SectionReader) Size() int64 { return s.limit - s.base }

// Outer returns the underlying ReaderAt and offsets for the section.
//
// The returned values are the same that were passed to NewSectionReader
// when the SectionReader was created.
//
// Multi-result triple: the real implementation lives in
// __sectionReaderOuter; 3-target call sites desugar to it (Appendix M).
func (s *SectionReader) Outer() (r ReaderAt, off int64, n int64) {
	panic("io.SectionReader.Outer: not in M8 scope (multi-result calls need WS D; re-test when it lands)")
}

// sectionReaderOuterTriple is the Appendix M carrier for SectionReader.Outer.
type sectionReaderOuterTriple struct {
	r   ReaderAt
	off int64
	n   int64
}

// __sectionReaderOuter is the real SectionReader.Outer: pure field
// projection, no interface dispatch — v1-expressible.
func __sectionReaderOuter(s *SectionReader) sectionReaderOuterTriple {
	return sectionReaderOuterTriple{s.r, s.base, s.n}
}

// An OffsetWriter maps writes at offset base to offset base+off in the underlying writer.
type OffsetWriter struct {
	w    WriterAt
	base int64 // the original offset
	off  int64 // the current offset
}

// NewOffsetWriter returns an OffsetWriter that writes to w
// starting at offset off.
func NewOffsetWriter(w WriterAt, off int64) *OffsetWriter {
	return &OffsetWriter{w, off, off}
}

// Write implements Writer.
//
// M8 scope: fail-fast — needs o.w.WriteAt (2-result interface call,
// WS D). Re-test when WS D lands.
func (o *OffsetWriter) Write(p []byte) (n int, err error) {
	panic("io.OffsetWriter.Write: not in M8 scope (needs 2-result interface dispatch, WS D; re-test when WS D lands)")
}

// WriteAt implements WriterAt.
//
// M8 scope: fail-fast — needs o.w.WriteAt (2-result interface call,
// WS D). Re-test when WS D lands.
func (o *OffsetWriter) WriteAt(p []byte, off int64) (n int, err error) {
	panic("io.OffsetWriter.WriteAt: not in M8 scope (needs 2-result interface dispatch, WS D; re-test when WS D lands)")
}

// Seek implements Seeker. Multi-result: the real implementation lives in
// __offsetWriterSeek; 2-target call sites desugar to it (Appendix M).
func (o *OffsetWriter) Seek(offset int64, whence int) (int64, error) {
	panic("io.OffsetWriter.Seek: not in M8 scope (multi-result calls need WS D; re-test when it lands)")
}

// offsetWriterSeekPair is the Appendix M carrier for OffsetWriter.Seek.
type offsetWriterSeekPair struct {
	n   int64
	err error
}

// __offsetWriterSeek is the real OffsetWriter.Seek: pure offset
// arithmetic, no interface dispatch — v1-expressible.
func __offsetWriterSeek(o *OffsetWriter, offset int64, whence int) offsetWriterSeekPair {
	switch whence {
	default:
		return offsetWriterSeekPair{0, errWhence}
	case SeekStart:
		offset += o.base
	case SeekCurrent:
		offset += o.off
	}
	if offset < o.base {
		return offsetWriterSeekPair{0, errOffset}
	}
	o.off = offset
	return offsetWriterSeekPair{offset - o.base, nil}
}

// TeeReader returns a Reader that writes to w what it reads from r.
// All reads from r performed through it are matched with
// corresponding writes to w. There is no internal buffering -
// the write must complete before the read completes.
// Any error encountered while writing is reported as a read error.
func TeeReader(r Reader, w Writer) Reader {
	return &teeReader{r, w}
}

type teeReader struct {
	r Reader
	w Writer
}

// Read implements Reader.
//
// M8 scope: fail-fast — needs t.r.Read/t.w.Write (2-result interface
// calls, WS D). Re-test when WS D lands.
func (t *teeReader) Read(p []byte) (n int, err error) {
	panic("io.teeReader.Read: not in M8 scope (needs 2-result interface dispatch, WS D; re-test when WS D lands)")
}

// Discard is a Writer on which all Write calls succeed
// without doing anything.
var Discard Writer = discard{}

type discard struct{}

// discard implements ReaderFrom as an optimization so Copy to
// io.Discard can avoid doing unnecessary work.
var _ ReaderFrom = discard{}

// Write implements Writer. Multi-result: fail-fast — discard is
// unexported, so no user call site can name its static type and no
// Appendix M entry is reachable; the method exists for signature
// fidelity only. (io.Discard.Write through the Writer interface fails
// at export with the named M8 error.)
func (discard) Write(p []byte) (int, error) {
	panic("io.discard.Write: not in M8 scope (multi-result calls need WS D; re-test when it lands)")
}

// WriteString implements StringWriter. Multi-result: fail-fast (see
// Write).
func (discard) WriteString(s string) (int, error) {
	panic("io.discard.WriteString: not in M8 scope (multi-result calls need WS D; re-test when it lands)")
}

// ReadFrom implements ReaderFrom.
//
// M8 scope: fail-fast — needs r.Read (2-result interface call, WS D);
// sync.Pool is not the blocker (see the header, deviation D4).
// Re-test when WS D lands.
func (discard) ReadFrom(r Reader) (n int64, err error) {
	panic("io.discard.ReadFrom: not in M8 scope (needs 2-result interface dispatch, WS D; re-test when WS D lands)")
}

// NopCloser returns a ReadCloser with a no-op Close method wrapping
// the provided Reader r.
// If r implements WriterTo, the returned ReadCloser will implement WriterTo
// by forwarding calls to r.
func NopCloser(r Reader) ReadCloser {
	if _, ok := r.(WriterTo); ok {
		return nopCloserWriterTo{r}
	}
	return nopCloser{r}
}

type nopCloser struct {
	Reader
}

func (nopCloser) Close() error { return nil }

type nopCloserWriterTo struct {
	Reader
}

func (nopCloserWriterTo) Close() error { return nil }

// WriteTo implements WriterTo.
//
// M8 scope: fail-fast — needs the embedded Reader's WriteTo (2-result
// interface call, WS D). Re-test when WS D lands.
func (c nopCloserWriterTo) WriteTo(w Writer) (n int64, err error) {
	panic("io.nopCloserWriterTo.WriteTo: not in M8 scope (needs 2-result interface dispatch, WS D; re-test when WS D lands)")
}

// ReadAll reads from r until an error or EOF and returns the data it read.
// A successful call returns err == nil, not err == EOF. Because ReadAll is
// defined to read from src until EOF, it does not treat an EOF from Read
// as an error to be reported.
//
// M8 scope: fail-fast — the growth loop needs r.Read (2-result interface
// call, WS D). Re-test when WS D lands.
func ReadAll(r Reader) ([]byte, error) {
	panic("io.ReadAll: not in M8 scope (needs 2-result interface dispatch, WS D; re-test when WS D lands)")
}

// ---------------------------------------------------------------------------
// multi.go (gc 1.27.1).
// ---------------------------------------------------------------------------

type eofReader struct{}

// Read implements Reader. Multi-result: fail-fast (see below).
func (eofReader) Read([]byte) (int, error) {
	panic("io.eofReader.Read: not in M8 scope (multi-result calls need WS D; re-test when it lands)")
}

type multiReader struct {
	readers []Reader
}

// Read implements Reader.
//
// M8 scope: fail-fast — needs mr.readers[0].Read (2-result interface
// call, WS D). Re-test when WS D lands.
func (mr *multiReader) Read(p []byte) (n int, err error) {
	panic("io.multiReader.Read: not in M8 scope (needs 2-result interface dispatch, WS D; re-test when WS D lands)")
}

// WriteTo implements WriterTo.
//
// M8 scope: fail-fast — needs copyBuffer (see Copy). Re-test when WS D
// lands.
func (mr *multiReader) WriteTo(w Writer) (sum int64, err error) {
	panic("io.multiReader.WriteTo: not in M8 scope (needs 2-result interface dispatch, WS D; re-test when WS D lands)")
}

var _ WriterTo = (*multiReader)(nil)

// MultiReader returns a Reader that's the logical concatenation of
// the provided input readers. They're read sequentially. Once all
// inputs have returned EOF, Read will return EOF.  If any of the readers
// return a non-nil, non-EOF error, Read will return that error.
func MultiReader(readers ...Reader) Reader {
	r := make([]Reader, len(readers))
	copy(r, readers)
	return &multiReader{r}
}

type multiWriter struct {
	writers []Writer
}

// Write implements Writer.
//
// M8 scope: fail-fast — needs w.Write per writer (2-result interface
// calls, WS D). Re-test when WS D lands.
func (t *multiWriter) Write(p []byte) (n int, err error) {
	panic("io.multiWriter.Write: not in M8 scope (needs 2-result interface dispatch, WS D; re-test when WS D lands)")
}

var _ StringWriter = (*multiWriter)(nil)

// WriteString implements StringWriter.
//
// M8 scope: fail-fast — needs w.(StringWriter)/w.Write (2-result
// interface calls, WS D). Re-test when WS D lands.
func (t *multiWriter) WriteString(s string) (n int, err error) {
	panic("io.multiWriter.WriteString: not in M8 scope (needs 2-result interface dispatch, WS D; re-test when WS D lands)")
}

// MultiWriter creates a writer that duplicates its writes to all the
// provided writers, similar to the Unix tee(1) command.
//
// Each write is written to each listed writer, one at a time.
// If a listed writer returns an error, that overall write operation
// stops and returns the error; it does not continue down the list.
func MultiWriter(writers ...Writer) Writer {
	allWriters := make([]Writer, 0, len(writers))
	for _, w := range writers {
		if mw, ok := w.(*multiWriter); ok {
			allWriters = append(allWriters, mw.writers...)
		} else {
			allWriters = append(allWriters, w)
		}
	}
	return &multiWriter{allWriters}
}

// ---------------------------------------------------------------------------
// pipe.go (gc 1.27.1).
//
// Structural deviations D1/D2/D3 (see the header): *sync.Mutex /
// *sync.Once pointer fields, onceError's named mu field, PipeReader
// embedding *pipe. The rendezvous protocol is otherwise a faithful port.
// ---------------------------------------------------------------------------

// onceError is an object that will only store an error once.
type onceError struct {
	mu  *sync.Mutex // deviation D2: gc embeds sync.Mutex (M2 forbids it)
	err error
}

// Store records err, keeping the first error stored.
func (a *onceError) Store(err error) {
	a.mu.Lock()
	if a.err == nil {
		a.err = err
	}
	a.mu.Unlock()
}

// Load returns the stored error.
func (a *onceError) Load() error {
	a.mu.Lock()
	err := a.err
	a.mu.Unlock()
	return err
}

// ErrClosedPipe is the error used for read or write operations on a closed pipe.
var ErrClosedPipe = errors.New("io: read/write on closed pipe")

// A pipe is the shared pipe structure underlying PipeReader and PipeWriter.
type pipe struct {
	wrMu *sync.Mutex // deviation D1: gc uses a value field (M2 forbids it)
	wrCh chan []byte
	rdCh chan int

	once *sync.Once // deviation D1: gc uses a value field (M2 forbids it)
	done chan struct{}
	rerr *onceError
	werr *onceError
}

// read implements the PipeReader.Read rendezvous. Multi-result: the real
// implementation lives in __pipeRead; 2-target call sites desugar to it
// (Appendix M).
func (p *pipe) read(b []byte) (n int, err error) {
	panic("io.pipe.read: not in M8 scope (multi-result calls need WS D; re-test when it lands)")
}

// pipeReadPair is the Appendix M carrier for pipe.read.
type pipeReadPair struct {
	n   int
	err error
}

// __pipeRead is the real pipe.read: unbuffered rendezvous over wrCh/rdCh
// plus the Once-guarded done channel. Faithful to gc; single-result.
func __pipeRead(p *pipe, b []byte) pipeReadPair {
	select {
	case <-p.done:
		return pipeReadPair{0, p.readCloseError()}
	default:
	}

	select {
	case bw := <-p.wrCh:
		nr := copy(b, bw)
		p.rdCh <- nr
		return pipeReadPair{nr, nil}
	case <-p.done:
		return pipeReadPair{0, p.readCloseError()}
	}
}

// closeRead closes the read half of the pipe.
func (p *pipe) closeRead(err error) error {
	if err == nil {
		err = ErrClosedPipe
	}
	p.rerr.Store(err)
	p.once.Do(func() { close(p.done) })
	return nil
}

// write implements the PipeWriter.Write rendezvous. Multi-result: the real
// implementation lives in __pipeWrite; 2-target call sites desugar to it
// (Appendix M).
func (p *pipe) write(b []byte) (n int, err error) {
	panic("io.pipe.write: not in M8 scope (multi-result calls need WS D; re-test when it lands)")
}

// pipeWritePair is the Appendix M carrier for pipe.write.
type pipeWritePair struct {
	n   int
	err error
}

// __pipeWrite is the real pipe.write: wrMu-serialized multi-rendezvous
// delivery. Deviation D5: explicit Unlock instead of defer
// (behavior-identical; the critical section cannot panic).
func __pipeWrite(p *pipe, b []byte) pipeWritePair {
	select {
	case <-p.done:
		return pipeWritePair{0, p.writeCloseError()}
	default:
	}

	p.wrMu.Lock()
	n := 0
	for once := true; once || len(b) > 0; once = false {
		select {
		case p.wrCh <- b:
			nw := <-p.rdCh
			b = b[nw:]
			n += nw
		case <-p.done:
			p.wrMu.Unlock()
			return pipeWritePair{n, p.writeCloseError()}
		}
	}
	p.wrMu.Unlock()
	return pipeWritePair{n, nil}
}

// closeWrite closes the write half of the pipe.
func (p *pipe) closeWrite(err error) error {
	if err == nil {
		err = EOF
	}
	p.werr.Store(err)
	p.once.Do(func() { close(p.done) })
	return nil
}

// readCloseError is considered internal to the pipe type.
func (p *pipe) readCloseError() error {
	rerr := p.rerr.Load()
	if werr := p.werr.Load(); rerr == nil && werr != nil {
		return werr
	}
	return ErrClosedPipe
}

// writeCloseError is considered internal to the pipe type.
func (p *pipe) writeCloseError() error {
	werr := p.werr.Load()
	if rerr := p.rerr.Load(); werr == nil && rerr != nil {
		return rerr
	}
	return ErrClosedPipe
}

// A PipeReader is the read half of a pipe.
type PipeReader struct{ *pipe } // deviation D3: gc embeds pipe by value

// Read implements the standard Read interface:
// it reads data from the pipe, blocking until a writer
// arrives or the write end is closed.
// If the write end is closed with an error, that error is
// returned as err; otherwise err is EOF.
//
// Multi-result: the real implementation lives in __pipeReaderRead;
// 2-target call sites desugar to it (Appendix M).
func (r *PipeReader) Read(data []byte) (n int, err error) {
	panic("io.PipeReader.Read: not in M8 scope (multi-result calls need WS D; re-test when it lands)")
}

// pipeReaderReadPair is the Appendix M carrier for PipeReader.Read.
type pipeReaderReadPair struct {
	n   int
	err error
}

// __pipeReaderRead is the real PipeReader.Read.
func __pipeReaderRead(r *PipeReader, data []byte) pipeReaderReadPair {
	res := __pipeRead(r.pipe, data)
	return pipeReaderReadPair{res.n, res.err}
}

// Close closes the reader; subsequent writes to the
// write half of the pipe will return the error ErrClosedPipe.
func (r *PipeReader) Close() error {
	return r.CloseWithError(nil)
}

// CloseWithError closes the reader; subsequent writes
// to the write half of the pipe will return the error err.
//
// CloseWithError never overwrites the previous error if it exists
// and always returns nil.
func (r *PipeReader) CloseWithError(err error) error {
	return r.pipe.closeRead(err)
}

// A PipeWriter is the write half of a pipe.
type PipeWriter struct{ r PipeReader }

// Write implements the standard Write interface:
// it writes data to the pipe, blocking until one or more readers
// have consumed all the data or the read end is closed.
// If the read end is closed with an error, that err is
// returned as err; otherwise err is ErrClosedPipe.
//
// Multi-result: the real implementation lives in __pipeWriterWrite;
// 2-target call sites desugar to it (Appendix M).
func (w *PipeWriter) Write(data []byte) (n int, err error) {
	panic("io.PipeWriter.Write: not in M8 scope (multi-result calls need WS D; re-test when it lands)")
}

// pipeWriterWritePair is the Appendix M carrier for PipeWriter.Write.
type pipeWriterWritePair struct {
	n   int
	err error
}

// __pipeWriterWrite is the real PipeWriter.Write.
func __pipeWriterWrite(w *PipeWriter, data []byte) pipeWriterWritePair {
	res := __pipeWrite(w.r.pipe, data)
	return pipeWriterWritePair{res.n, res.err}
}

// Close closes the writer; subsequent reads from the
// read half of the pipe will return no bytes and EOF.
func (w *PipeWriter) Close() error {
	return w.CloseWithError(nil)
}

// CloseWithError closes the writer; subsequent reads from the
// read half of the pipe will return no bytes and the error err,
// or EOF if err is nil.
//
// CloseWithError never overwrites the previous error if it exists
// and always returns nil.
func (w *PipeWriter) CloseWithError(err error) error {
	return w.r.pipe.closeWrite(err)
}

// Pipe creates a synchronous in-memory pipe.
// It can be used to connect code expecting an io.Reader
// with code expecting an io.Writer.
//
// Reads and Writes on the pipe are matched one to one
// except when multiple Reads are needed to consume a single Write.
// That is, each Write to the PipeWriter blocks until it has satisfied
// one or more Reads from the PipeReader that fully consume
// the written data.
// The data is copied directly from the Write to the corresponding
// Read (or Reads); there is no internal buffering.
//
// It is safe to call Read and Write in parallel with each other or with Close.
// Parallel calls to Read and parallel calls to Write are also safe:
// the individual calls will be gated sequentially.
//
// Multi-result: the real implementation lives in __pipe; 2-target call
// sites desugar to it (Appendix M).
func Pipe() (*PipeReader, *PipeWriter) {
	panic("io.Pipe: not in M8 scope (multi-result calls need WS D; re-test when it lands)")
}

// pipePair is the Appendix M carrier for Pipe.
type pipePair struct {
	r *PipeReader
	w *PipeWriter
}

// __pipe is the real Pipe.
//
// NB: the two halves share one *pipe (built once, referenced from both
// literals): v1 has no &x on identifiers/selectors (WS D), so &pw.r is
// inexpressible — sharing the pointer up front is the faithful shape.
func __pipe() pipePair {
	p := &pipe{
		wrMu: &sync.Mutex{},
		wrCh: make(chan []byte),
		rdCh: make(chan int),
		once: &sync.Once{},
		done: make(chan struct{}),
		rerr: &onceError{mu: &sync.Mutex{}},
		werr: &onceError{mu: &sync.Mutex{}},
	}
	return pipePair{&PipeReader{pipe: p}, &PipeWriter{r: PipeReader{pipe: p}}}
}
`
