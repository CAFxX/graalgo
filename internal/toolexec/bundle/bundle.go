// Package bundle defines the program bundle the -toolexec link wrapper
// appends to the launcher stub: the IR of every package of the link plus
// the information the runtime needs to start the program.
//
// Layout of a linked executable (docs/arch/toolexec-design.md §4.2):
//
//	stub ELF | bundle | trailer
//	trailer := bundle offset (8 bytes LE) | bundle length (8 bytes LE) | "GGB1"
//	bundle  := "GGBN" | version | runner | main package | count | x* | modinfo
//	           | resolution | count | package*
//	x       := "importpath.name=value" (a link -X setting)
//	resolution := registry version | registry hash | count | func* | count | var*
//	func    := package | name | intrinsic | target package | target name
//	var     := package | name | target package | target name
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
	version      = 3
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
	Main string
	// X are the linker's -X settings, "importpath.name=value", in command
	// line order (the user's -ldflags and cmd/go's own, such as
	// runtime.godebugDefault); the backend applies them before the
	// package's initializers (design §3.6).
	X []string
	// ModInfo is the main module's build info (importcfg.link "modinfo"),
	// what debug.ReadBuildInfo reports.
	ModInfo string
	// Resolution is the link's symbol resolution (design §5.3).
	Resolution Resolution
	Packages   []Package
}

// Resolution is the resolved symbol table of a link (design §4.2, §5.3):
// what package link computed and the backend applies without resolving
// anything itself. Packages are named by their -p path ("main" for the
// main package), functions by gc's symbol name without the package.
type Resolution struct {
	// RegistryVersion and RegistryHash identify the intrinsic registry
	// the link resolved against; the runtime refuses any other.
	RegistryVersion uint64
	RegistryHash    string
	// Funcs binds functions that are not their own Go body; Vars are the
	// variables that share another variable's storage. Both are sorted by
	// (Pkg, Name).
	Funcs []FuncBinding
	Vars  []VarAlias
}

// FuncBinding binds function Name of package Pkg to an intrinsic
// (Intrinsic is its registry symbol) or, when Intrinsic is empty, to the
// Go body of function TargetName of package TargetPkg.
type FuncBinding struct {
	Pkg, Name             string
	Intrinsic             string
	TargetPkg, TargetName string
}

// VarAlias makes variable Name of package Pkg the same variable as
// TargetName of package TargetPkg (//go:linkname on variables).
type VarAlias struct {
	Pkg, Name             string
	TargetPkg, TargetName string
}

// Encode serializes b.
func Encode(b *Bundle) []byte {
	out := []byte(bundleMagic)
	out = binary.AppendUvarint(out, version)
	out = appendBytes(out, []byte(b.Runner))
	out = appendBytes(out, []byte(b.Main))
	out = binary.AppendUvarint(out, uint64(len(b.X)))
	for _, x := range b.X {
		out = appendBytes(out, []byte(x))
	}
	out = appendBytes(out, []byte(b.ModInfo))
	r := &b.Resolution
	out = binary.AppendUvarint(out, r.RegistryVersion)
	out = appendBytes(out, []byte(r.RegistryHash))
	out = binary.AppendUvarint(out, uint64(len(r.Funcs)))
	for _, f := range r.Funcs {
		for _, s := range []string{f.Pkg, f.Name, f.Intrinsic, f.TargetPkg, f.TargetName} {
			out = appendBytes(out, []byte(s))
		}
	}
	out = binary.AppendUvarint(out, uint64(len(r.Vars)))
	for _, v := range r.Vars {
		for _, s := range []string{v.Pkg, v.Name, v.TargetPkg, v.TargetName} {
			out = appendBytes(out, []byte(s))
		}
	}
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
	nx := d.uvarint()
	for i := uint64(0); i < nx && d.err == nil; i++ {
		b.X = append(b.X, string(d.bytes()))
	}
	b.ModInfo = string(d.bytes())
	b.Resolution.RegistryVersion = d.uvarint()
	b.Resolution.RegistryHash = string(d.bytes())
	nf := d.uvarint()
	for i := uint64(0); i < nf && d.err == nil; i++ {
		b.Resolution.Funcs = append(b.Resolution.Funcs, FuncBinding{
			Pkg: string(d.bytes()), Name: string(d.bytes()), Intrinsic: string(d.bytes()),
			TargetPkg: string(d.bytes()), TargetName: string(d.bytes()),
		})
	}
	nv := d.uvarint()
	for i := uint64(0); i < nv && d.err == nil; i++ {
		b.Resolution.Vars = append(b.Resolution.Vars, VarAlias{
			Pkg: string(d.bytes()), Name: string(d.bytes()),
			TargetPkg: string(d.bytes()), TargetName: string(d.bytes()),
		})
	}
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
