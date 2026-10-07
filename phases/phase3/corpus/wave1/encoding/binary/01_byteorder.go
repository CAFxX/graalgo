package main

import (
	"encoding/binary"
	"fmt"
)

func main() {
	// String() identifies the order.
	fmt.Println(binary.LittleEndian.String())
	fmt.Println(binary.BigEndian.String())
	fmt.Println(binary.NativeEndian.String())

	for _, order := range []binary.ByteOrder{binary.LittleEndian, binary.BigEndian} {
		buf := make([]byte, 8)
		order.PutUint16(buf, 0x1234)
		fmt.Println(order.Uint16(buf) == 0x1234)
		order.PutUint32(buf, 0x12345678)
		fmt.Println(order.Uint32(buf) == 0x12345678)
		order.PutUint64(buf, 0x123456789abcdef0)
		fmt.Println(order.Uint64(buf) == 0x123456789abcdef0)
	}

	// Endianness actually differs.
	le := make([]byte, 2)
	be := make([]byte, 2)
	binary.LittleEndian.PutUint16(le, 0x1234)
	binary.BigEndian.PutUint16(be, 0x1234)
	fmt.Println(le[0], be[0])

	// AppendByteOrder.
	var abo binary.AppendByteOrder = binary.LittleEndian
	out := abo.AppendUint16(nil, 0x1234)
	out = abo.AppendUint32(out, 0x12345678)
	out = abo.AppendUint64(out, 0x123456789abcdef0)
	fmt.Println(len(out))

	// NativeEndian agrees with one of the two.
	ne := make([]byte, 2)
	binary.NativeEndian.PutUint16(ne, 0x1234)
	fmt.Println(ne[0] == le[0] || ne[0] == be[0])
}
