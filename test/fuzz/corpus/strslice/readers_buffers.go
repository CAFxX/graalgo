// Targets strings.Reader / bytes.Reader (ReadRune over invalid UTF-8,
// UnreadRune/UnreadByte error rules, Seek whence/negative, ReadAt) and
// bytes.Buffer (Grow, Truncate, Next, ReadBytes, UnreadRune, aliasing).
package main

import (
	"bytes"
	"errors"
	"strconv"
	"strings"
)

func errs(err error) string {
	if err == nil {
		return "nil"
	}
	return err.Error()
}

func protect(name string, f func()) {
	defer func() {
		if r := recover(); r != nil {
			if e, ok := r.(interface{ Error() string }); ok {
				println(name, "panic:", e.Error())
			} else if s, ok := r.(string); ok {
				println(name, "panic str:", s)
			}
		}
	}()
	f()
}

func main() {
	src := "a\xffé\xe2\x82日\xf0\x9f\x98\x80"
	r := strings.NewReader(src)
	println("len", r.Len(), r.Size())
	for {
		ch, size, err := r.ReadRune()
		if err != nil {
			println("rr end", errs(err), ch, size)
			break
		}
		println("rr", ch, size, r.Len())
	}
	println("unread-after-eof", errs(r.UnreadRune()))
	r.Reset(src)
	println("unread-fresh", errs(r.UnreadRune()), errs(r.UnreadByte()))
	r.ReadRune()
	r.ReadRune()
	println("unread1", errs(r.UnreadRune()), r.Len())
	println("unread2", errs(r.UnreadRune()), r.Len())
	b, _ := r.ReadByte()
	println("readbyte", b, errs(r.UnreadRune()), errs(r.UnreadByte()), r.Len())
	for _, sk := range [][2]int64{{0, 0}, {3, 0}, {-1, 1}, {2, 1}, {-2, 2}, {100, 0}, {-1, 0}, {0, 3}} {
		pos, err := r.Seek(sk[0], int(sk[1]))
		println("seek", sk[0], sk[1], pos, errs(err), r.Len())
	}
	r.Seek(1, 0)
	ch, sz, _ := r.ReadRune()
	println("afterseek", ch, sz)
	buf := make([]byte, 4)
	for _, off := range []int64{0, 5, 13, 14, 20, -1} {
		n, err := r.ReadAt(buf, off)
		println("readat", off, n, errs(err), strconv.Quote(string(buf[:n])))
	}
	var sb strings.Builder
	r.Seek(0, 0)
	nw, err := r.WriteTo(&sb)
	println("writeto", nw, errs(err), sb.Len())
	// bytes.Reader parallels
	br := bytes.NewReader([]byte(src))
	cnt := 0
	for {
		_, _, err := br.ReadRune()
		if err != nil {
			break
		}
		cnt++
	}
	println("bytesreader", cnt, errs(br.UnreadRune()), br.Len())
	_, err = br.Seek(-1, 0)
	println("bytesseekneg", errs(err))
	// bytes.Buffer
	var bb bytes.Buffer
	println("buf0", bb.Len(), bb.Cap(), bb.String() == "")
	bb.Grow(64)
	println("grow", bb.Len(), bb.Cap() >= 64)
	bb.WriteString("hello\nworld\n\xff")
	bb.WriteRune('日')
	bb.WriteRune(-5)
	bb.WriteByte('!')
	println("buf", bb.Len(), strconv.Quote(bb.String()))
	line, err := bb.ReadString('\n')
	println("line", strconv.Quote(line), errs(err))
	lb, err := bb.ReadBytes('\n')
	println("line2", strconv.Quote(string(lb)), errs(err))
	ch, sz, err = bb.ReadRune()
	println("brr", ch, sz, errs(err))
	println("unread", errs(bb.UnreadRune()), errs(bb.UnreadRune()))
	nx := bb.Next(2)
	println("next", strconv.Quote(string(nx)), bb.Len())
	println("unreadbyte", errs(bb.UnreadByte()), bb.Len())
	rest, err := bb.ReadString('Q')
	println("rest", strconv.Quote(rest), errs(err), errors.Is(err, errors.New("EOF")))
	ch, sz, err = bb.ReadRune()
	println("empty", ch, sz, errs(err))
	// Bytes() aliasing
	bb.Reset()
	bb.WriteString("abcdef")
	al := bb.Bytes()
	al[0] = 'Z'
	println("alias", bb.String())
	bb.Truncate(3)
	println("trunc", bb.String(), bb.Len())
	protect("trunc-bad", func() { bb.Truncate(10) })
	protect("trunc-neg", func() { bb.Truncate(-1) })
	protect("grow-neg", func() { bb.Grow(-1) })
	// large writes and reads
	bb.Reset()
	for i := 0; i < 5000; i++ {
		bb.WriteString(strconv.Itoa(i))
		bb.WriteByte(',')
	}
	println("biglen", bb.Len())
	total := 0
	for {
		tok, err := bb.ReadString(',')
		if err != nil {
			println("bigtail", strconv.Quote(tok), errs(err))
			break
		}
		v, _ := strconv.Atoi(tok[:len(tok)-1])
		total += v
	}
	println("bigsum", total, bb.Len())
	// Buffer from existing slice
	nb := bytes.NewBufferString("seed")
	nb.WriteString("+more")
	p := make([]byte, 3)
	n, err := nb.Read(p)
	println("read", n, errs(err), string(p), nb.String())
	n, err = nb.Read(make([]byte, 0))
	println("read0", n, errs(err))
	nb.Reset()
	n, err = nb.Read(p)
	println("readempty", n, errs(err))
	n, err = nb.Read(make([]byte, 0))
	println("read0empty", n, errs(err))
	// bytes package functions over invalid UTF-8
	data := []byte("x\xffy  \xe2\x82 z")
	println("fields", len(bytes.Fields(data)), bytes.IndexRune(data, 0xFFFD), bytes.Count(data, nil), len(bytes.Runes(data)))
	println("title", strconv.Quote(string(bytes.ToValidUTF8(data, []byte("?")))), strconv.Quote(strings.ToValidUTF8("a\xff\xfeb\xc0", "")))
	println("equal", bytes.Equal(nil, []byte{}), bytes.Compare(nil, []byte{}), bytes.Compare([]byte("a"), nil))
	cl := bytes.Clone(nil)
	println("clone", cl == nil, len(bytes.Clone([]byte{})), bytes.Clone([]byte{}) == nil)
}
