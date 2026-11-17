// Package archive reads and extends the Unix ar archives that the gc
// toolchain uses for compiled packages (_pkg_.a).
//
// The -toolexec compile wrapper appends one member, [IRMember], carrying
// the package's graalgo IR. Stock consumers of the archive (cmd/go's
// in-process pack and build-ID rewrite, vet and go/importer reading
// __.PKGDEF) are indifferent to additional members; see
// docs/arch/toolexec-design.md §2.2.
package archive

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
)

// IRMember is the archive member name that holds a package's graalgo IR.
// ar member names are at most 16 bytes.
const IRMember = "__.GRAALGOIR"

const (
	magic     = "!<arch>\n"
	headerLen = 60
)

// ErrNoMember reports that the archive has no member of the requested name.
var ErrNoMember = errors.New("archive: member not found")

// Append appends a member with the given name and contents to the archive
// at path. The archive must already exist and be well formed up to its end.
func Append(path, name string, data []byte) error {
	if len(name) > 16 {
		return fmt.Errorf("archive: member name %q longer than 16 bytes", name)
	}
	f, err := os.OpenFile(path, os.O_RDWR, 0)
	if err != nil {
		return err
	}
	defer f.Close()
	end, err := walk(f, nil)
	if err != nil {
		return fmt.Errorf("archive: %s: %v", path, err)
	}
	var buf bytes.Buffer
	if end%2 == 1 {
		buf.WriteByte('\n')
	}
	// name[16] mtime[12] uid[6] gid[6] mode[8] size[10] magic[2]
	fmt.Fprintf(&buf, "%-16s%-12d%-6d%-6d%-8o%-10d`\n", name, 0, 0, 0, 0o644, len(data))
	buf.Write(data)
	if len(data)%2 == 1 {
		buf.WriteByte('\n')
	}
	if _, err := f.WriteAt(buf.Bytes(), end); err != nil {
		return err
	}
	return f.Close()
}

// Read returns the contents of the last member named name in the archive
// at path, or [ErrNoMember].
func Read(path, name string) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var off, size int64 = -1, 0
	if _, err := walk(f, func(n string, o, s int64) {
		if n == name {
			off, size = o, s
		}
	}); err != nil {
		return nil, fmt.Errorf("archive: %s: %v", path, err)
	}
	if off < 0 {
		return nil, ErrNoMember
	}
	data := make([]byte, size)
	if _, err := f.ReadAt(data, off); err != nil {
		return nil, err
	}
	return data, nil
}

// walk visits every member header and returns the offset just past the
// last member's data (before any padding byte).
func walk(f *os.File, visit func(name string, dataOff, size int64)) (int64, error) {
	hdr := make([]byte, headerLen)
	if _, err := io.ReadFull(io.NewSectionReader(f, 0, int64(len(magic))), hdr[:len(magic)]); err != nil || string(hdr[:len(magic)]) != magic {
		return 0, errors.New("not an ar archive")
	}
	st, err := f.Stat()
	if err != nil {
		return 0, err
	}
	off := int64(len(magic))
	end := off
	for off < st.Size() {
		if _, err := f.ReadAt(hdr, off); err != nil {
			return 0, fmt.Errorf("truncated member header at offset %d", off)
		}
		if hdr[58] != '`' || hdr[59] != '\n' {
			return 0, fmt.Errorf("bad member header at offset %d", off)
		}
		size, err := strconv.ParseInt(strings.TrimSpace(string(hdr[48:58])), 10, 64)
		if err != nil || size < 0 || off+headerLen+size > st.Size() {
			return 0, fmt.Errorf("bad member size at offset %d", off)
		}
		if visit != nil {
			visit(strings.TrimRight(string(hdr[:16]), " /"), off+headerLen, size)
		}
		end = off + headerLen + size
		off = end + size%2
	}
	return end, nil
}
