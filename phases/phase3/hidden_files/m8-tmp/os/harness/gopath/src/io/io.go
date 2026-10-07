package io

import "errors"
var EOF = errors.New("EOF")
var ErrShortWrite = errors.New("short write")
type Reader interface{ Read(p []byte) (n int, err error) }
type Writer interface{ Write(p []byte) (n int, err error) }
type ReaderFrom interface{ ReadFrom(r Reader) (n int64, err error) }
type WriterTo interface{ WriteTo(w Writer) (n int64, err error) }
func ReadAll(r Reader) ([]byte, error) { return nil, nil }
func WriteString(w Writer, s string) (int, error) { return 0, nil }
