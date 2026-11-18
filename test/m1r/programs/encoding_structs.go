package main

import (
	"encoding"
	"structs"
)

// encoding and structs are declarations only: interfaces to implement and a
// layout marker to embed.
type point struct {
	_    structs.HostLayout
	x, y int
}

func (p point) MarshalText() ([]byte, error) {
	return []byte{byte('0' + p.x), ',', byte('0' + p.y)}, nil
}

func (p *point) UnmarshalText(b []byte) error {
	p.x, p.y = int(b[0]-'0'), int(b[2]-'0')
	return nil
}

func (p point) AppendBinary(b []byte) ([]byte, error) {
	return append(b, byte(p.x), byte(p.y)), nil
}

func main() {
	var m encoding.TextMarshaler = point{x: 3, y: 4}
	text, err := m.MarshalText()
	println(string(text), err == nil)
	var q point
	var u encoding.TextUnmarshaler = &q
	println(u.UnmarshalText([]byte("7,2")) == nil, q.x, q.y)
	var a encoding.BinaryAppender = q
	bin, _ := a.AppendBinary([]byte{9})
	println(len(bin), bin[0], bin[1], bin[2])
	_, isBoth := any(&q).(interface {
		encoding.TextMarshaler
		encoding.TextUnmarshaler
	})
	_, isBinary := any(q).(encoding.BinaryMarshaler)
	println(isBoth, isBinary)
}
