package main

// P3-M9: synthetic testing/iotest package.

const syntheticTestingIotestSource = `// Package iotest implements Readers and Writers useful mainly for testing.
//
// Synthetic P3-M9 package: faithful port of go1.27.1 testing/iotest
// (reader.go, writer.go, logger.go) with one documented divergence:
// NewReadLogger/NewWriteLogger log via the log package in gc (which adds a
// date/time prefix); the synthetic log package has not landed, so this port
// logs "prefix hexdata" lines directly to os.Stderr with no timestamp.
// Data passthrough is gc-exact; re-test the stderr format when the log
// package lands.
package iotest

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
)

// OneByteReader returns a Reader that implements
// each non-empty Read by reading one byte from r.
func OneByteReader(r io.Reader) io.Reader { return &oneByteReader{r} }

type oneByteReader struct {
	r io.Reader
}

func (r *oneByteReader) Read(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	return r.r.Read(p[0:1])
}

// HalfReader returns a Reader that implements Read
// by reading half as many requested bytes from r.
func HalfReader(r io.Reader) io.Reader { return &halfReader{r} }

type halfReader struct {
	r io.Reader
}

func (r *halfReader) Read(p []byte) (int, error) {
	return r.r.Read(p[0 : (len(p)+1)/2])
}

// DataErrReader changes the way errors are handled by a Reader.
func DataErrReader(r io.Reader) io.Reader { return &dataErrReader{r, nil, make([]byte, 1024)} }

type dataErrReader struct {
	r      io.Reader
	unread []byte
	data   []byte
}

func (r *dataErrReader) Read(p []byte) (n int, err error) {
	for {
		if len(r.unread) == 0 {
			n1, err1 := r.r.Read(r.data)
			r.unread = r.data[0:n1]
			err = err1
		}
		if n > 0 || err != nil {
			break
		}
		n = copy(p, r.unread)
		r.unread = r.unread[n:]
	}
	return
}

// ErrTimeout is a fake timeout error.
var ErrTimeout = errors.New("timeout")

// TimeoutReader returns [ErrTimeout] on the second read
// with no data. Subsequent calls to read succeed.
func TimeoutReader(r io.Reader) io.Reader { return &timeoutReader{r, 0} }

type timeoutReader struct {
	r     io.Reader
	count int
}

func (r *timeoutReader) Read(p []byte) (int, error) {
	r.count++
	if r.count == 2 {
		return 0, ErrTimeout
	}
	return r.r.Read(p)
}

// ErrReader returns an [io.Reader] that returns 0, err from all Read calls.
func ErrReader(err error) io.Reader {
	return &errReader{err: err}
}

type errReader struct {
	err error
}

func (r *errReader) Read(p []byte) (int, error) {
	return 0, r.err
}

type smallByteReader struct {
	r   io.Reader
	off int
	n   int
}

func (r *smallByteReader) Read(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	r.n = r.n%3 + 1
	n := r.n
	if n > len(p) {
		n = len(p)
	}
	n, err := r.r.Read(p[0:n])
	if err != nil && err != io.EOF {
		err = fmt.Errorf("Read(%d bytes at offset %d): %v", []any{n, r.off, err}...)
	}
	r.off += n
	return n, err
}

// TestReader tests that reading from r returns the expected file content.
func TestReader(r io.Reader, content []byte) error {
	if len(content) > 0 {
		n, err := r.Read(nil)
		if n != 0 || err != nil {
			return fmt.Errorf("Read(0) = %d, %v, want 0, nil", []any{n, err}...)
		}
	}

	data, err := io.ReadAll(&smallByteReader{r: r})
	if err != nil {
		return err
	}
	if !bytes.Equal(data, content) {
		return fmt.Errorf("ReadAll(small amounts) = %q\n\twant %q", []any{data, content}...)
	}
	n, err := r.Read(make([]byte, 10))
	if n != 0 || err != io.EOF {
		return fmt.Errorf("Read(10) at EOF = %v, %v, want 0, EOF", []any{n, err}...)
	}

	if r, ok := r.(io.ReadSeeker); ok {
		if off, err := r.Seek(0, 1); off != int64(len(content)) || err != nil {
			return fmt.Errorf("Seek(0, 1) from EOF = %d, %v, want %d, nil", []any{off, err, len(content)}...)
		}

		middle := len(content) - len(content)/3
		if middle > 0 {
			if off, err := r.Seek(-1, 1); off != int64(len(content)-1) || err != nil {
				return fmt.Errorf("Seek(-1, 1) from EOF = %d, %v, want %d, nil", []any{-off, err, len(content) - 1}...)
			}
			if off, err := r.Seek(int64(-len(content)/3), 1); off != int64(middle-1) || err != nil {
				return fmt.Errorf("Seek(%d, 1) from %d = %d, %v, want %d, nil", []any{-len(content) / 3, len(content) - 1, off, err, middle - 1}...)
			}
			if off, err := r.Seek(+1, 1); off != int64(middle) || err != nil {
				return fmt.Errorf("Seek(+1, 1) from %d = %d, %v, want %d, nil", []any{middle - 1, off, err, middle}...)
			}
		}

		if off, err := r.Seek(0, 1); off != int64(middle) || err != nil {
			return fmt.Errorf("Seek(0, 1) from %d = %d, %v, want %d, nil", []any{middle, off, err, middle}...)
		}

		data, err := io.ReadAll(&smallByteReader{r: r})
		if err != nil {
			return fmt.Errorf("ReadAll from offset %d: %v", []any{middle, err}...)
		}
		if !bytes.Equal(data, content[middle:]) {
			return fmt.Errorf("ReadAll from offset %d = %q\n\twant %q", []any{middle, data, content[middle:]}...)
		}

		if off, err := r.Seek(int64(middle/2), 0); off != int64(middle/2) || err != nil {
			return fmt.Errorf("Seek(%d, 0) from EOF = %d, %v, want %d, nil", []any{middle / 2, off, err, middle / 2}...)
		}
		if off, err := r.Seek(int64(-len(content)/3), 2); off != int64(middle) || err != nil {
			return fmt.Errorf("Seek(%d, 2) from %d = %d, %v, want %d, nil", []any{-len(content) / 3, middle / 2, off, err, middle}...)
		}

		data, err = io.ReadAll(&smallByteReader{r: r})
		if err != nil {
			return fmt.Errorf("ReadAll from offset %d: %v", []any{middle, err}...)
		}
		if !bytes.Equal(data, content[middle:]) {
			return fmt.Errorf("ReadAll from offset %d = %q\n\twant %q", []any{middle, data, content[middle:]}...)
		}

		if off, err := r.Seek(int64(middle/2), 0); off != int64(middle/2) || err != nil {
			return fmt.Errorf("Seek(%d, 0) from EOF = %d, %v, want %d, nil", []any{middle / 2, off, err, middle / 2}...)
		}
		data, err = io.ReadAll(r)
		if err != nil {
			return fmt.Errorf("ReadAll from offset %d: %v", []any{middle / 2, err}...)
		}
		if !bytes.Equal(data, content[middle/2:]) {
			return fmt.Errorf("ReadAll from offset %d = %q\n\twant %q", []any{middle / 2, data, content[middle/2:]}...)
		}
	}

	if r, ok := r.(io.ReaderAt); ok {
		data := make([]byte, len(content), len(content)+1)
		for i := range data {
			data[i] = 0xfe
		}
		n, err := r.ReadAt(data, 0)
		if n != len(data) || err != nil && err != io.EOF {
			return fmt.Errorf("ReadAt(%d, 0) = %v, %v, want %d, nil or EOF", []any{len(data), n, err, len(data)}...)
		}
		if !bytes.Equal(data, content) {
			return fmt.Errorf("ReadAt(%d, 0) = %q\n\twant %q", []any{len(data), data, content}...)
		}

		n, err = r.ReadAt(data[:1], int64(len(data)))
		if n != 0 || err != io.EOF {
			return fmt.Errorf("ReadAt(1, %d) = %v, %v, want 0, EOF", []any{len(data), n, err}...)
		}

		for i := range data {
			data[i] = 0xfe
		}
		n, err = r.ReadAt(data[:cap(data)], 0)
		if n != len(data) || err != io.EOF {
			return fmt.Errorf("ReadAt(%d, 0) = %v, %v, want %d, EOF", []any{cap(data), n, err, len(data)}...)
		}
		if !bytes.Equal(data, content) {
			return fmt.Errorf("ReadAt(%d, 0) = %q\n\twant %q", []any{len(data), data, content}...)
		}

		for i := range data {
			data[i] = 0xfe
		}
		for i := range data {
			n, err = r.ReadAt(data[i:i+1], int64(i))
			if n != 1 || err != nil && (i != len(data)-1 || err != io.EOF) {
				want := "nil"
				if i == len(data)-1 {
					want = "nil or EOF"
				}
				return fmt.Errorf("ReadAt(1, %d) = %v, %v, want 1, %s", []any{i, n, err, want}...)
			}
			if data[i] != content[i] {
				return fmt.Errorf("ReadAt(1, %d) = %q want %q", []any{i, data[i : i+1], content[i : i+1]}...)
			}
		}
	}
	return nil
}

// TruncateWriter returns a Writer that writes to w
// but stops silently after n bytes.
func TruncateWriter(w io.Writer, n int64) io.Writer {
	return &truncateWriter{w, n}
}

type truncateWriter struct {
	w io.Writer
	n int64
}

func (t *truncateWriter) Write(p []byte) (n int, err error) {
	panic("testing/iotest.truncateWriter.Write: not in scope (needs multi-result call support)")
}

// truncateWriterWritePair is the Appendix M carrier for truncateWriter.Write.
type truncateWriterWritePair struct {
	n   int
	err error
}

// __truncateWriterWrite is the real truncateWriter.Write.
func __truncateWriterWrite(t *truncateWriter, p []byte) truncateWriterWritePair {
	if t.n <= 0 {
		return truncateWriterWritePair{len(p), nil}
	}
	n := len(p)
	if int64(n) > t.n {
		n = int(t.n)
	}
	n, err := t.w.Write(p[0:n])
	t.n -= int64(n)
	if err == nil {
		n = len(p)
	}
	return truncateWriterWritePair{n, err}
}

func iotestLogf(format string, args ...any) {
	os.Stderr.Write([]byte(fmt.Sprintf(format+"\n", args...)))
}

type writeLogger struct {
	prefix string
	w      io.Writer
}

func (l *writeLogger) Write(p []byte) (n int, err error) {
	n, err = l.w.Write(p)
	if err != nil {
		iotestLogf("%s %x: %v", l.prefix, p[0:n], err)
	} else {
		iotestLogf("%s %x", l.prefix, p[0:n])
	}
	return
}

// NewWriteLogger returns a writer that behaves like w except
// that it logs each write to standard error.
// DIVERGENCE: gc logs via log.Printf (timestamped); this port writes
// "prefix hexdata" lines directly (see package doc).
func NewWriteLogger(prefix string, w io.Writer) io.Writer {
	return &writeLogger{prefix, w}
}

type readLogger struct {
	prefix string
	r      io.Reader
}

func (l *readLogger) Read(p []byte) (n int, err error) {
	n, err = l.r.Read(p)
	if err != nil {
		iotestLogf("%s %x: %v", l.prefix, p[0:n], err)
	} else {
		iotestLogf("%s %x", l.prefix, p[0:n])
	}
	return
}

// NewReadLogger returns a reader that behaves like r except
// that it logs each read to standard error.
// DIVERGENCE: gc logs via log.Printf (timestamped); this port writes
// "prefix hexdata" lines directly (see package doc).
func NewReadLogger(prefix string, r io.Reader) io.Reader {
	return &readLogger{prefix, r}
}

`

