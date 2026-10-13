// P3-M9 wave3: archive/tar — forced FormatUSTAR / FormatPAX / FormatGNU
// round-trips: GNU long names, USTAR >8GB rejection, PAX nanosecond
// ModTime and >100-char names. Pure Go over bytes.Buffer.
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

	// Forced USTAR: fields round-trip, ModTime truncated to 1s.
	var b1 bytes.Buffer
	tw := tar.NewWriter(&b1)
	tw.WriteHeader(&tar.Header{Name: "u.txt", Mode: 0o640, Size: 4,
		ModTime: modTime, Uid: 42, Gid: 43, Uname: "u", Gname: "g",
		Format: tar.FormatUSTAR})
	tw.Write([]byte("data"))
	tw.Close()
	tr := tar.NewReader(&b1)
	hdr, err := tr.Next()
	out, _ := io.ReadAll(tr)
	fmt.Println("ustar", err == nil, hdr.Format == tar.FormatUSTAR,
		hdr.Name == "u.txt", hdr.Mode == 0o640, hdr.Uid == 42, hdr.Gid == 43,
		hdr.Uname == "u", hdr.Gname == "g",
		hdr.ModTime.Equal(modTime.Truncate(time.Second)),
		string(out) == "data")

	// Forced PAX: nanosecond ModTime preserved, >100-char name preserved.
	var b2 bytes.Buffer
	tw2 := tar.NewWriter(&b2)
	paxName := strings.Repeat("p", 150)
	tw2.WriteHeader(&tar.Header{Name: paxName, Mode: 0o644, Size: 0,
		ModTime: modTime, Format: tar.FormatPAX})
	tw2.Close()
	tr2 := tar.NewReader(&b2)
	hdr2, err := tr2.Next()
	fmt.Println("pax", err == nil, hdr2.Format == tar.FormatPAX,
		hdr2.Name == paxName, hdr2.ModTime.Equal(modTime))

	// Forced GNU: 200-char name via ././@LongLink record round-trips.
	var b3 bytes.Buffer
	tw3 := tar.NewWriter(&b3)
	gnuName := strings.Repeat("g", 200)
	tw3.WriteHeader(&tar.Header{Name: gnuName, Mode: 0o644, Size: 5,
		ModTime: modTime, Format: tar.FormatGNU})
	tw3.Write([]byte("12345"))
	tw3.Close()
	raw3 := b3.String()
	tr3 := tar.NewReader(&b3)
	hdr3, err := tr3.Next()
	out3, _ := io.ReadAll(tr3)
	fmt.Println("gnu-longname", err == nil, hdr3.Format == tar.FormatGNU,
		hdr3.Name == gnuName, string(out3) == "12345",
		strings.Contains(raw3, "././@LongLink"))

	// USTAR rejects >8GB size at WriteHeader time (pins the error class).
	var b4 bytes.Buffer
	tw4 := tar.NewWriter(&b4)
	err = tw4.WriteHeader(&tar.Header{Name: "big", Mode: 0o644,
		Size: 9 << 30, Format: tar.FormatUSTAR})
	fmt.Println("ustar-9gb", err != nil,
		err != nil && strings.Contains(err.Error(), "cannot encode"))
	tw4.Close()

	// PAX accepts >8GB size (no USTAR field limit).
	var b5 bytes.Buffer
	tw5 := tar.NewWriter(&b5)
	err = tw5.WriteHeader(&tar.Header{Name: "big", Mode: 0o644,
		Size: 9 << 30, Format: tar.FormatPAX})
	fmt.Println("pax-9gb", err == nil)
	tw5.Close()

	fmt.Println("ok")
}
