// Package bundle defines the program bundle the -toolexec link wrapper
// appends to the launcher stub: the IR of every package of the link plus
// the information the runtime needs to start the program.
//
// Layout of a linked executable (docs/arch/toolexec-design.md §4.2):
//
//	stub ELF | bundle | trailer
//	trailer := bundle offset (8 bytes LE) | bundle length (8 bytes LE) | "GGB1"
//	bundle  := "GGBN" | version | runner | main package | count | package*
//	package := import path | IR bytes
//
// Integers are unsigned varints and strings/byte fields are
// length-prefixed, as in the frontend/v2 binary encoding.
package bundle

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
)

const (
	bundleMagic  = "GGBN"
	trailerMagic = "GGB1"
	version      = 1
	trailerLen   = 8 + 8 + len(trailerMagic)
)

// Package is one package's IR inside a bundle.
type Package struct {
	ImportPath string
	IR         []byte
}

// Bundle is the decoded program bundle.
type Bundle struct {
	// Runner is the command that executes the bundle, recorded at link
	// time. GRAALGO_RUNNER overrides it at run time.
	Runner string
	// Main is the import path of the main package ("main").
	Main     string
	Packages []Package
}

// Encode serializes b.
func Encode(b *Bundle) []byte {
	out := []byte(bundleMagic)
	out = binary.AppendUvarint(out, version)
	out = appendBytes(out, []byte(b.Runner))
	out = appendBytes(out, []byte(b.Main))
	out = binary.AppendUvarint(out, uint64(len(b.Packages)))
	for _, p := range b.Packages {
		out = appendBytes(out, []byte(p.ImportPath))
		out = appendBytes(out, p.IR)
	}
	return out
}

// Trailer returns the trailer for a bundle of the given length placed at
// the given file offset.
func Trailer(offset, length int64) []byte {
	t := make([]byte, 0, trailerLen)
	t = binary.LittleEndian.AppendUint64(t, uint64(offset))
	t = binary.LittleEndian.AppendUint64(t, uint64(length))
	return append(t, trailerMagic...)
}

// Locate reads the trailer of the executable at path and returns the
// bundle's offset and length.
func Locate(path string) (offset, length int64, err error) {
	f, err := os.Open(path)
	if err != nil {
		return 0, 0, err
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return 0, 0, err
	}
	if st.Size() < int64(trailerLen) {
		return 0, 0, errors.New("bundle: file too short for a trailer")
	}
	t := make([]byte, trailerLen)
	if _, err := f.ReadAt(t, st.Size()-int64(trailerLen)); err != nil {
		return 0, 0, err
	}
	if string(t[16:]) != trailerMagic {
		return 0, 0, errors.New("bundle: no program bundle trailer (not linked by graalgo-tool?)")
	}
	offset = int64(binary.LittleEndian.Uint64(t[0:8]))
	length = int64(binary.LittleEndian.Uint64(t[8:16]))
	if offset < 0 || length < 0 || offset+length != st.Size()-int64(trailerLen) {
		return 0, 0, errors.New("bundle: trailer does not describe this file")
	}
	return offset, length, nil
}

// ReadFile locates and decodes the bundle of the executable at path.
func ReadFile(path string) (*Bundle, error) {
	off, n, err := Locate(path)
	if err != nil {
		return nil, err
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	data := make([]byte, n)
	if _, err := io.ReadFull(io.NewSectionReader(f, off, n), data); err != nil {
		return nil, err
	}
	return Decode(data)
}

// Decode parses an encoded bundle.
func Decode(data []byte) (*Bundle, error) {
	d := &decoder{data: data}
	if string(d.next(len(bundleMagic))) != bundleMagic {
		return nil, errors.New("bundle: bad magic")
	}
	if v := d.uvarint(); d.err == nil && v != version {
		return nil, fmt.Errorf("bundle: unsupported version %d (this build reads %d)", v, version)
	}
	b := &Bundle{Runner: string(d.bytes()), Main: string(d.bytes())}
	n := d.uvarint()
	for i := uint64(0); i < n && d.err == nil; i++ {
		b.Packages = append(b.Packages, Package{ImportPath: string(d.bytes()), IR: d.bytes()})
	}
	if d.err != nil {
		return nil, d.err
	}
	if len(d.data) != 0 {
		return nil, errors.New("bundle: trailing bytes")
	}
	return b, nil
}

func appendBytes(out, b []byte) []byte {
	out = binary.AppendUvarint(out, uint64(len(b)))
	return append(out, b...)
}

type decoder struct {
	data []byte
	err  error
}

func (d *decoder) next(n int) []byte {
	if d.err != nil {
		return nil
	}
	if n < 0 || n > len(d.data) {
		d.err = errors.New("bundle: truncated")
		return nil
	}
	b := d.data[:n]
	d.data = d.data[n:]
	return b
}

func (d *decoder) uvarint() uint64 {
	if d.err != nil {
		return 0
	}
	v, n := binary.Uvarint(d.data)
	if n <= 0 {
		d.err = errors.New("bundle: bad varint")
		return 0
	}
	d.data = d.data[n:]
	return v
}

func (d *decoder) bytes() []byte {
	n := d.uvarint()
	if d.err == nil && n > uint64(len(d.data)) {
		d.err = errors.New("bundle: truncated")
		return nil
	}
	return d.next(int(n))
}

// BuildIDPlaceholder is the build ID the launcher stub is linked with
// (go build -ldflags=-buildid=...). It has the length and shape of the IDs
// cmd/go passes to the linker (four 20-character fields), so the link
// wrapper can overwrite it in place inside the stub's ELF build-ID note
// and cmd/go can then read and rewrite the ID as for any gc binary.
const BuildIDPlaceholder = "GRAALGO-STUB-BUILDID/GRAALGO-STUB-BUILDID/GRAALGO-STUB-BUILDID/GRAALGO-STUB-BUILDID"
