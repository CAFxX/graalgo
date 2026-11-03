package main

import (
	"os"
	"path/filepath"
	"testing"
)

// TestSyntheticNetLookup is the REVIEW-2026-11-03-16 S7 regression
// test: net.LookupHost/LookupIP panicked at runtime ("no resolver")
// and hostname resolution in Dial/Resolve* accepted IP literals only,
// so a program resolving "localhost" type-checked and exported but
// died at runtime. The JVM resolver (graalgo.net.lookupHost) now
// backs LookupHost/LookupIP/LookupPort and __resolveHostIP. This
// test pins export of a program exercising all four call shapes;
// execution-level assertions (localhost resolves, empty host is a
// DNSError, LookupPort("tcp","http") == 80) live in the lane's
// sync-run verification.
func TestSyntheticNetLookup(t *testing.T) {
	src := `package main

import (
	"net"
)

func main() {
	addrs, err := net.LookupHost("localhost")
	if err != nil {
		println("lookuphost err")
	}
	println(len(addrs))
	ips, err := net.LookupIP("localhost")
	if err != nil {
		println("lookupip err")
	}
	println(len(ips))
	port, err := net.LookupPort("tcp", "http")
	if err != nil {
		println("lookupport err")
	}
	println(port)
	addr, err := net.ResolveTCPAddr("tcp", "localhost:80")
	if err != nil {
		println("resolve err")
	}
	println(addr.String())
}
`
	dir := t.TempDir()
	p := filepath.Join(dir, "synnetlookup.go")
	if err := os.WriteFile(p, []byte(src), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := ExportFile(p); err != nil {
		t.Fatalf("ExportFile: %v", err)
	}
}
