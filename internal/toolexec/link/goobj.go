package link

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
)

// The parts of gc's object file format (cmd/internal/goobj, go1.20+: magic
// "\x00go120ld") that StaticData reads. The tracked toolchain is pinned
// (GG-0069), and a format change fails loudly on the magic.
const (
	goobjMagic   = "\x00go120ld"
	goobjNBlk    = 19 // goobj.NBlk
	blkSymdef    = 3  // goobj.BlkSymdef
	blkHashed64  = 4  // goobj.BlkHashed64def, the block after BlkSymdef
	blkDataIdx   = 13 // goobj.BlkDataIdx
	goobjSymSize = 8 + 2 + 1 + 1 + 1 + 4 + 4
)

// StaticData reports whether the gc object member of a package archive
// (the "_go_.o" member: a text header, "\n!\n", then the goobj) defines
// symbol with static data: what `go tool nm` shows as D rather than B. For
// a package-level variable that is gc's verdict on its initializer: data
// when the compiler initialized it statically, none when it is zero or set
// by initializer code.
func StaticData(obj []byte, symbol string) (bool, error) {
	i := bytes.Index(obj, []byte("\n!\n"+goobjMagic))
	if i < 0 {
		return false, errors.New("not a go120ld object file")
	}
	o := obj[i+3:]
	hdr := len(goobjMagic) + 8 + 4
	if len(o) < hdr+4*goobjNBlk {
		return false, errors.New("truncated object header")
	}
	u32 := func(off uint32) (uint32, error) {
		if uint64(off)+4 > uint64(len(o)) {
			return 0, errors.New("truncated object file")
		}
		return binary.LittleEndian.Uint32(o[off:]), nil
	}
	var offs [goobjNBlk]uint32
	for k := range offs {
		offs[k] = binary.LittleEndian.Uint32(o[hdr+4*k:])
	}
	if offs[blkHashed64] < offs[blkSymdef] || (offs[blkHashed64]-offs[blkSymdef])%goobjSymSize != 0 {
		return false, errors.New("malformed symbol block")
	}
	n := (offs[blkHashed64] - offs[blkSymdef]) / goobjSymSize
	for s := range n {
		so := offs[blkSymdef] + s*goobjSymSize
		nameLen, err := u32(so)
		if err != nil {
			return false, err
		}
		nameOff, err := u32(so + 4)
		if err != nil {
			return false, err
		}
		if uint64(nameOff)+uint64(nameLen) > uint64(len(o)) {
			return false, errors.New("symbol name out of range")
		}
		if string(o[nameOff:nameOff+nameLen]) != symbol {
			continue
		}
		start, err := u32(offs[blkDataIdx] + 4*s)
		if err != nil {
			return false, err
		}
		end, err := u32(offs[blkDataIdx] + 4*s + 4)
		if err != nil {
			return false, err
		}
		return end > start, nil
	}
	return false, fmt.Errorf("symbol %s not defined by the object", symbol)
}
