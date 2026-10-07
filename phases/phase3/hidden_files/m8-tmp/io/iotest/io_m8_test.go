package io

// Behavioral tests for the REAL parts of the P3-M8 synthetic io source,
// exercised through the same __-helpers the Appendix M desugar calls.
// Run: go test ./ (in this dir). Mutation check: break a helper and watch
// it go red.

import (
	"errors"
	"testing"
)

func TestPipeRendezvous(t *testing.T) {
	pp := __pipe()
	r, w := pp.r, pp.w
	done := make(chan pipeWriterWritePair, 1)
	go func() {
		done <- __pipeWriterWrite(w, []byte("hello pipe"))
		w.Close()
	}()
	var out []byte
	buf := make([]byte, 3)
	for {
		rr := __pipeReaderRead(r, buf)
		out = append(out, buf[:rr.n]...)
		if rr.err != nil {
			break
		}
	}
	wr := <-done
	if wr.n != 10 || wr.err != nil {
		t.Fatalf("write = (%d, %v), want (10, nil)", wr.n, wr.err)
	}
	if string(out) != "hello pipe" {
		t.Fatalf("read %q, want %q", out, "hello pipe")
	}
	if err := w.Close(); err != nil {
		t.Fatalf("Close = %v, want nil", err)
	}
	rr := __pipeReaderRead(r, buf)
	if rr.n != 0 || rr.err != EOF {
		t.Fatalf("post-close read = (%d, %v), want (0, EOF)", rr.n, rr.err)
	}
}

func TestPipeCloseWithError(t *testing.T) {
	custom := errors.New("boom")
	pp := __pipe()
	readRes := make(chan pipeReaderReadPair, 1)
	go func() { readRes <- __pipeReaderRead(pp.r, make([]byte, 16)) }()
	if err := pp.w.CloseWithError(custom); err != nil {
		t.Fatalf("CloseWithError = %v, want nil", err)
	}
	rr := <-readRes
	if rr.n != 0 || rr.err != custom {
		t.Fatalf("blocked read = (%d, %v), want (0, boom)", rr.n, rr.err)
	}
	// write after close: writeCloseError sees werr=custom? No — the READER
	// was never closed; werr=custom was stored by closeWrite. A new write:
	wr := __pipeWriterWrite(pp.w, []byte("x"))
	if wr.err != ErrClosedPipe {
		t.Fatalf("write after close = %v, want ErrClosedPipe", wr.err)
	}
}

func TestPipeReadCloseWakesWriter(t *testing.T) {
	custom := errors.New("reader gone")
	pp := __pipe()
	writeRes := make(chan pipeWriterWritePair, 1)
	go func() { writeRes <- __pipeWriterWrite(pp.w, []byte("hello")) }()
	if err := pp.r.CloseWithError(custom); err != nil {
		t.Fatalf("CloseWithError = %v, want nil", err)
	}
	wr := <-writeRes
	if wr.err != custom {
		t.Fatalf("blocked write = (%d, %v), want (0, reader gone)", wr.n, wr.err)
	}
}

func TestSectionReaderSeek(t *testing.T) {
	s := NewSectionReader(nil, 10, 20)
	if got := s.Size(); got != 20 {
		t.Fatalf("Size = %d, want 20", got)
	}
	if p := __sectionReaderSeek(s, 5, SeekStart); p.n != 5 || p.err != nil {
		t.Fatalf("SeekStart 5 = (%d, %v), want (5, nil)", p.n, p.err)
	}
	if p := __sectionReaderSeek(s, 3, SeekCurrent); p.n != 8 || p.err != nil {
		t.Fatalf("SeekCurrent 3 = (%d, %v), want (8, nil)", p.n, p.err)
	}
	if p := __sectionReaderSeek(s, -4, SeekEnd); p.n != 16 || p.err != nil {
		t.Fatalf("SeekEnd -4 = (%d, %v), want (16, nil)", p.n, p.err)
	}
	if p := __sectionReaderSeek(s, 0, 99); p.err == nil || p.err.Error() != "Seek: invalid whence" {
		t.Fatalf("bad whence = (%d, %v), want whence error", p.n, p.err)
	}
	if p := __sectionReaderSeek(s, -100, SeekStart); p.err == nil || p.err.Error() != "Seek: invalid offset" {
		t.Fatalf("negative = (%d, %v), want offset error", p.n, p.err)
	}
	o := __sectionReaderOuter(s)
	if o.off != 10 || o.n != 20 || o.r != nil {
		t.Fatalf("Outer = (%v, %d, %d), want (nil, 10, 20)", o.r, o.off, o.n)
	}
}

func TestNewSectionReaderOverflow(t *testing.T) {
	const maxint64 = int64(1<<63 - 1)
	s := NewSectionReader(nil, maxint64-5, 10)
	if got := s.Size(); got != 5 {
		t.Fatalf("overflow Size = %d, want 5", got)
	}
	s2 := NewSectionReader(nil, 0, maxint64)
	if got := s2.Size(); got != maxint64 {
		t.Fatalf("full Size = %d, want maxint64", got)
	}
}

func TestOffsetWriterSeek(t *testing.T) {
	o := NewOffsetWriter(nil, 100)
	if p := __offsetWriterSeek(o, 10, SeekStart); p.n != 10 || p.err != nil {
		t.Fatalf("SeekStart 10 = (%d, %v), want (10, nil)", p.n, p.err)
	}
	if p := __offsetWriterSeek(o, 5, SeekCurrent); p.n != 15 || p.err != nil {
		t.Fatalf("SeekCurrent 5 = (%d, %v), want (15, nil)", p.n, p.err)
	}
	if p := __offsetWriterSeek(o, 0, SeekEnd); p.err == nil || p.err.Error() != "Seek: invalid whence" {
		t.Fatalf("SeekEnd = (%d, %v), want whence error", p.n, p.err)
	}
	if p := __offsetWriterSeek(o, -200, SeekStart); p.err == nil || p.err.Error() != "Seek: invalid offset" {
		t.Fatalf("negative = (%d, %v), want offset error", p.n, p.err)
	}
}

func TestConstructors(t *testing.T) {
	lr := LimitReader(nil, 42)
	if l, ok := lr.(*LimitedReader); !ok || l.N != 42 {
		t.Fatalf("LimitReader fields wrong: %v %v", ok, l)
	}
	if TeeReader(nil, nil) == nil {
		t.Fatal("TeeReader returned nil")
	}
	if err := NopCloser(nil).Close(); err != nil {
		t.Fatalf("NopCloser.Close = %v, want nil", err)
	}
	if MultiReader(nil, nil) == nil {
		t.Fatal("MultiReader returned nil")
	}
	if MultiWriter(nil, nil) == nil {
		t.Fatal("MultiWriter returned nil")
	}
	if Discard == nil {
		t.Fatal("Discard is nil")
	}
}

func TestErrorVars(t *testing.T) {
	want := map[error]string{
		EOF:             "EOF",
		ErrUnexpectedEOF: "unexpected EOF",
		ErrShortWrite:   "short write",
		ErrShortBuffer:  "short buffer",
		ErrNoProgress:   "multiple Read calls return no data or error",
		ErrClosedPipe:   "io: read/write on closed pipe",
	}
	for err, msg := range want {
		if err.Error() != msg {
			t.Fatalf("error message = %q, want %q", err.Error(), msg)
		}
	}
	if SeekStart != 0 || SeekCurrent != 1 || SeekEnd != 2 {
		t.Fatal("seek consts wrong")
	}
}
