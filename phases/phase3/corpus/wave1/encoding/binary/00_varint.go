package main

import (
	"encoding/binary"
	"fmt"
)

func main() {
	// Consts.
	fmt.Println(binary.MaxVarintLen16, binary.MaxVarintLen32, binary.MaxVarintLen64)

	// Uvarint round-trip (multi-result package func desugar).
	for _, v := range []uint64{0, 1, 127, 128, 300, 1<<64 - 1} {
		buf := make([]byte, binary.MaxVarintLen64)
		n := binary.PutUvarint(buf, v)
		x, m := binary.Uvarint(buf[:n])
		fmt.Println(v == x, n == m)
	}

	// Varint round-trip.
	for _, v := range []int64{0, 1, -1, 63, -64, 1<<63 - 1, -(1 << 63)} {
		buf := make([]byte, binary.MaxVarintLen64)
		n := binary.PutVarint(buf, v)
		x, m := binary.Varint(buf[:n])
		fmt.Println(v == x, n == m)
	}

	// Append variants.
	a := binary.AppendUvarint(nil, 300)
	fmt.Println(len(a))
	av := binary.AppendVarint(nil, -300)
	fmt.Println(len(av))

	// Truncated input: n <= 0.
	x, n := binary.Uvarint([]byte{0x80})
	fmt.Println(x, n)
	y, m := binary.Varint([]byte{0x80, 0x80})
	fmt.Println(y, m)

	// Overflow: n < 0.
	x2, n2 := binary.Uvarint([]byte{0x80, 0x80, 0x80, 0x80, 0x80, 0x80, 0x80, 0x80, 0x80, 0x02})
	fmt.Println(x2, n2)
}
