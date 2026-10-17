package main

import (
	"os"
	"path/filepath"
	"testing"
)

// Temporary verification for the review-s2 lane: a program exercising the
// changed API surface must export cleanly through gofrontend.
//   - *tls.Conn must satisfy net.Conn (P0-3: t time.Time on both sides)
//   - poll.ErrDeadlineExceeded identity (P0-5) type-checks via net
//   - new tls API surface (P1-R2-2) resolves
func TestTmpReviewS2Export(t *testing.T) {
	src := `package main

import (
	"context"
	"crypto/tls"
	"net"
	"time"
)

func useConn(c net.Conn) {
	_ = c.SetDeadline(time.Now())
	_ = c.SetReadDeadline(time.Time{})
	_ = c.SetWriteDeadline(time.Now().Add(time.Second))
}

func main() {
	var c *tls.Conn
	useConn(c)
	_ = tls.CipherSuites()
	_ = tls.InsecureCipherSuites()
	_ = tls.CipherSuiteName(tls.TLS_AES_128_GCM_SHA256)
	_ = tls.CipherSuiteName(tls.TLS_ECDHE_RSA_WITH_CHACHA20_POLY1305_SHA256)
	_ = tls.VersionName(tls.VersionTLS13)
	_ = tls.NewLRUClientSessionCache(4)
	var e tls.AlertError
	_ = e.Error()
	var ss tls.SessionState
	_ = ss.Extra
	css, _ := tls.NewResumptionState([]byte{1}, &ss)
	tick, st, _ := css.ResumptionState()
	_, _, _ = tick, st, css
	var cfg tls.Config
	_ = cfg.Clone()
	cfg.BuildNameToCertificate()
	cfg.SetSessionTicketKeys([][32]byte{{}})
	var _ tls.ClientSessionCache = tls.NewLRUClientSessionCache(2)
	_ = c.NetConn()
	_ = c.OCSPResponse()
	_ = c.HandshakeContext(context.Background())
	_ = c.CloseWrite
}
`
	dir := t.TempDir()
	p := filepath.Join(dir, "main.go")
	if err := os.WriteFile(p, []byte(src), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := ExportFile(p); err != nil {
		t.Fatalf("ExportFile: %v", err)
	}
}
