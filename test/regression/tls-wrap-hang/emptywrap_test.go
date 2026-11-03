// Regression test for REVIEW-2026-11-03-11 P1-2: (*Conn).Handshake's
// engineNeedWrap case bounded consecutive empty (non-nil) wraps at
// maxConsecutiveEmptyWraps instead of sleeping 1ms and retrying forever.
//
// No forcing mechanism for a stuck NEED_WRAP+empty engine exists: a
// stock-JDK probe replicating TlsRuntime.wrap's drain (up to 20 wraps,
// break at 3 consecutive empty singles) across TLS 1.3 / TLS 1.2 /
// client-auth / version-mismatch / garbage-input handshakes observed
// ZERO genuine empty drains — every successful drain produced bytes.
// Persistent empty wraps occurred only when wrapping in a state this
// loop never wraps in (NEED_UNWRAP on a fresh server engine, NEED_TASK
// mid-handshake), which the status switch gates away. Bad handles and
// throwing wraps surface as nil, not empty, and are covered by
// TestVersionMismatchHandshakeFails. So this test cannot force the stuck
// case; instead it pins the other side of the bound — successful
// handshakes on both protocol versions must still complete, proving a
// too-tight cap (or a counter that counts non-empty wraps) does not
// break the documented transitional path. A reintroduced unbounded spin
// would surface, as before, via test-regression's per-directory
// `timeout 600` (no in-test watchdog is possible: timers do not fire
// under graalgo, PKG-048).
//
// Cert fixtures (caPEM, srvPEM, srvKeyPEM) are shared from
// mismatch_test.go in this package.
package tlswraphang

import (
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"net"
	"testing"
)

func serverConfig13() *tls.Config {
	cert, err := tls.X509KeyPair([]byte(srvPEM), []byte(srvKeyPEM))
	if err != nil {
		panic("X509KeyPair: " + err.Error())
	}
	return &tls.Config{
		Certificates: []tls.Certificate{cert},
		MinVersion:   tls.VersionTLS13,
		MaxVersion:   tls.VersionTLS13,
	}
}

func clientConfig12() *tls.Config {
	caBlock, _ := pem.Decode([]byte(caPEM))
	ca, err := x509.ParseCertificate(caBlock.Bytes)
	if err != nil {
		panic("parse ca: " + err.Error())
	}
	roots := x509.NewCertPool()
	roots.AddCert(ca)
	return &tls.Config{
		RootCAs:    roots,
		ServerName: "localhost",
		MinVersion: tls.VersionTLS12,
		MaxVersion: tls.VersionTLS12,
	}
}

func clientConfig13() *tls.Config {
	cfg := clientConfig12()
	cfg.MinVersion = tls.VersionTLS13
	cfg.MaxVersion = tls.VersionTLS13
	return cfg
}

func handshakeOK(t *testing.T, serverCfg, clientCfg *tls.Config) {
	t.Helper()
	ln, err := tls.Listen("tcp", "127.0.0.1:0", serverCfg)
	if err != nil {
		t.Fatalf("tls.Listen: %v", err)
	}
	defer ln.Close()
	serverErr := make(chan error, 1)
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			serverErr <- err
			return
		}
		defer conn.Close()
		serverErr <- conn.(*tls.Conn).Handshake()
	}()
	conn, err := net.Dial("tcp", ln.Addr().String())
	if err != nil {
		t.Fatalf("net.Dial: %v", err)
	}
	defer conn.Close()
	tlsConn := tls.Client(conn, clientCfg)
	if err := tlsConn.Handshake(); err != nil {
		t.Fatalf("client Handshake() = %v, want nil", err)
	}
	if err := <-serverErr; err != nil {
		t.Fatalf("server Handshake() = %v, want nil", err)
	}
}

func TestEmptyWrapBoundDoesNotBreakHandshakes(t *testing.T) {
	t.Run("tls13", func(t *testing.T) {
		handshakeOK(t, serverConfig13(), clientConfig13())
	})
	t.Run("tls12", func(t *testing.T) {
		handshakeOK(t, serverConfig(), clientConfig12())
	})
}
