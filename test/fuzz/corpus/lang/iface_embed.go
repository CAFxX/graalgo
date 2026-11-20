// Targets interface embedding and method sets: overlapping embedded
// interface methods, value vs pointer method sets (T vs *T satisfying
// interfaces), interfaces embedded in structs and promoted through pointer
// embeds, interface-to-interface conversion, and dynamic dispatch through
// embedded interface fields.
package main

import "strconv"

type Reader interface{ Read() string }
type Writer interface{ Write(s string) int }
type Closer interface{ Close() error }

type ReadCloser interface {
	Reader
	Closer
}
type WriteCloser interface {
	Writer
	Closer
}
type RWC interface {
	ReadCloser
	WriteCloser // Close overlaps
	Reader      // duplicate embedding is fine
}

type File struct {
	name   string
	data   []string
	closed bool
}

func (f *File) Read() string {
	if len(f.data) == 0 {
		return "<eof>"
	}
	s := f.data[0]
	f.data = f.data[1:]
	return s
}
func (f *File) Write(s string) int { f.data = append(f.data, s); return len(s) }
func (f *File) Close() error {
	if f.closed {
		return errStr("already closed: " + f.name)
	}
	f.closed = true
	return nil
}

type errStr string

func (e errStr) Error() string { return string(e) }

type ROFile struct{ content string }

func (r ROFile) Read() string    { return r.content }
func (r ROFile) Close() error    { return nil }
func (r *ROFile) Reset(s string) { r.content = s }

// struct embedding an interface, overriding one method
type Upper struct {
	ReadCloser
	n int
}

func (u *Upper) Read() string {
	u.n++
	s := u.ReadCloser.Read()
	out := []byte(s)
	for i, c := range out {
		if c >= 'a' && c <= 'z' {
			out[i] = c - 32
		}
	}
	return string(out) + "#" + strconv.Itoa(u.n)
}

type Wrapper struct {
	*Upper
	WriteCloser
}

func useRWC(x RWC) string {
	x.Write("w1")
	s := x.Read()
	err := x.Close()
	err2 := x.Close()
	es := "nil"
	if err != nil {
		es = err.Error()
	}
	return s + "|" + es + "|" + err2.Error()
}

func asReader(x any) string {
	if r, ok := x.(Reader); ok {
		return "reader:" + r.Read()
	}
	return "not-reader"
}

func asCloser(x any) string {
	if _, ok := x.(Closer); ok {
		return "closer"
	}
	return "not-closer"
}

func main() {
	f := &File{name: "f", data: []string{"hello"}}
	println(useRWC(f))
	println(f.Read(), f.Read(), len(f.data))

	var rc ReadCloser = f
	var wc WriteCloser = f
	rw := rc.(RWC)
	println(rw.Write("abc"), rc.Read(), wc.Write("x"))
	var c1 Closer = rc
	var c2 Closer = wc
	println(c1 == c2, rc == rw, any(rc) == any(f))

	ro := ROFile{"ro"}
	println(asReader(ro), asReader(&ro), asCloser(ro), asCloser(&ro))
	_, okv := any(ro).(interface{ Reset(string) })
	_, okp := any(&ro).(interface{ Reset(string) })
	println(okv, okp)
	var r1 Reader = ro
	ro.Reset("changed")
	var r2 Reader = &ro
	println(r1.Read(), r2.Read(), asReader(*f), asReader(f))

	u := &Upper{ReadCloser: &File{name: "u", data: []string{"abc", "xyz"}}}
	println(u.Read(), u.Read(), u.Read())
	var rcu ReadCloser = u
	println(rcu.Read(), rcu.Close() == nil, rcu.Close().Error())

	w := Wrapper{Upper: &Upper{ReadCloser: ROFile{"inner"}}, WriteCloser: &File{name: "wc"}}
	println(w.Read(), w.Write("hey"), w.Upper.Close() == nil, w.WriteCloser.Close() == nil)
	var rd Reader = w
	println(rd.Read(), w.n)
	ww := w // copies pointer and interface
	ww.Read()
	println(w.n, ww.n)

	// interface holding a struct value with embedded interface: copy semantic
	var anyU any = *u
	u.n = 100
	uu := anyU.(Upper)
	println(uu.n, u.n)

	// conversion between interfaces with same method set
	type R2 interface{ Read() string }
	var r3 R2 = rd
	var r4 Reader = r3
	println(r4.Read(), r3 == r4)

	// sort-of method set check via assertion on embedded nil
	var emptyUp Upper
	println(asCloser(emptyUp), asCloser(&emptyUp), asReader(42))
	println(callNil(&emptyUp))
}

func callNil(u *Upper) (s string) {
	defer func() {
		if r := recover(); r != nil {
			s = "recovered:" + r.(interface{ Error() string }).Error()
		}
	}()
	return u.Read()
}
