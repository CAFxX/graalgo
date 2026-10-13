// P3-M9 wave3: archive/tar — write+read round-trip of every Typeflag,
// field preservation, USTAR 1s ModTime truncation, auto format selection,
// Xattrs forcing PAX. Pure Go over bytes.Buffer.
// SCOPE-LIMITED: no synthetic archive/tar source in frontend/v1 yet
// (imports are not supported in frontend/v1 scope — WS G import overlay).
package main

import (
	"archive/tar"
	"bytes"
	"fmt"
	"io"
	"strings"
	"time"
)

func main() {
	modTime := time.Date(2026, 3, 4, 5, 6, 7, 123456789, time.UTC)
	headers := []*tar.Header{
		{Name: "reg.txt", Mode: 0o644, Size: 11, ModTime: modTime,
			Uid: 1000, Gid: 1000, Uname: "user", Gname: "group", Typeflag: tar.TypeReg},
		{Name: "dir/", Mode: 0o755, ModTime: modTime, Typeflag: tar.TypeDir},
		{Name: "link-to-reg", Mode: 0o777, ModTime: modTime,
			Typeflag: tar.TypeSymlink, Linkname: "reg.txt"},
		{Name: "hardlink", Mode: 0o644, ModTime: modTime,
			Typeflag: tar.TypeLink, Linkname: "reg.txt"},
		{Name: "chardev", Mode: 0o666, ModTime: modTime,
			Typeflag: tar.TypeChar, Devmajor: 1, Devminor: 5},
		{Name: "blockdev", Mode: 0o660, ModTime: modTime,
			Typeflag: tar.TypeBlock, Devmajor: 8, Devminor: 0},
		{Name: "afifo", Mode: 0o644, ModTime: modTime, Typeflag: tar.TypeFifo},
	}
	bodies := map[string][]byte{"reg.txt": []byte("hello world")}

	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	for _, h := range headers {
		if err := tw.WriteHeader(h); err != nil {
			fmt.Println("write-header-err", h.Name, err)
			return
		}
		if b, ok := bodies[h.Name]; ok {
			if _, err := tw.Write(b); err != nil {
				fmt.Println("write-body-err", h.Name, err)
				return
			}
		}
	}
	if err := tw.Close(); err != nil {
		fmt.Println("close-err", err)
		return
	}

	tr := tar.NewReader(&buf)
	i := 0
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			fmt.Println("read-err", err)
			return
		}
		want := headers[i]
		ok := hdr.Name == want.Name &&
			hdr.Mode == want.Mode &&
			hdr.Size == want.Size &&
			hdr.Typeflag == want.Typeflag &&
			hdr.Linkname == want.Linkname &&
			hdr.Uid == want.Uid && hdr.Gid == want.Gid &&
			hdr.Uname == want.Uname && hdr.Gname == want.Gname &&
			hdr.Devmajor == want.Devmajor && hdr.Devminor == want.Devminor &&
			// USTAR truncates ModTime to 1s (pins the truncation).
			hdr.ModTime.Equal(want.ModTime.Truncate(time.Second))
		fmt.Println("hdr-"+want.Name, ok, hdr.Format == tar.FormatUSTAR)
		if b, ok2 := bodies[hdr.Name]; ok2 {
			out, _ := io.ReadAll(tr)
			fmt.Println("body-"+hdr.Name, bytes.Equal(out, b))
		}
		i++
	}
	fmt.Println("count", i == len(headers))

	// Auto format selection: a >100-char single path component forces PAX.
	var buf2 bytes.Buffer
	tw2 := tar.NewWriter(&buf2)
	longName := strings.Repeat("n", 150)
	tw2.WriteHeader(&tar.Header{Name: longName, Mode: 0o644, Size: 0, ModTime: modTime})
	tw2.Close()
	tr2 := tar.NewReader(&buf2)
	hdr2, err := tr2.Next()
	fmt.Println("longname", err == nil, hdr2.Name == longName, hdr2.Format == tar.FormatPAX)

	// Xattrs force PAX and round-trip.
	var buf3 bytes.Buffer
	tw3 := tar.NewWriter(&buf3)
	tw3.WriteHeader(&tar.Header{Name: "x", Mode: 0o644, Size: 0, ModTime: modTime,
		Xattrs: map[string]string{"user.comment": "hello"}})
	tw3.Close()
	tr3 := tar.NewReader(&buf3)
	hdr3, err := tr3.Next()
	fmt.Println("xattr", err == nil, hdr3.Format == tar.FormatPAX,
		hdr3.Xattrs["user.comment"] == "hello")

	// PAXRecords: arbitrary keys round-trip verbatim, but the time keys
	// are writer-managed — a user-supplied "atime" value is overridden by
	// the record derived from hdr.AccessTime and consumed back into
	// hdr.AccessTime on read (never visible in PAXRecords).
	var buf4 bytes.Buffer
	tw4 := tar.NewWriter(&buf4)
	atime := time.Date(2026, 5, 6, 7, 8, 9, 0, time.UTC)
	tw4.WriteHeader(&tar.Header{Name: "p", Mode: 0o644, Size: 0, ModTime: modTime,
		AccessTime: atime, Format: tar.FormatPAX,
		PAXRecords: map[string]string{"comment": "pax-comment", "atime": "123.456"}})
	tw4.Close()
	tr4 := tar.NewReader(&buf4)
	hdr4, err := tr4.Next()
	_, hasAtime := hdr4.PAXRecords["atime"]
	fmt.Println("paxrecords", err == nil, hdr4.Format == tar.FormatPAX,
		hdr4.PAXRecords["comment"] == "pax-comment",
		!hasAtime, hdr4.AccessTime.Equal(atime))

	fmt.Println("ok")
}
