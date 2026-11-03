// Regression tests for REVIEW-2026-11-03-13 P1-2 (TLS unwrap conflates
// peer-close and protocol error into one null, and discards app bytes
// produced with the close):
//
//   - A corrupt record after the handshake must surface as a non-EOF
//     error, not clean EOF: a program reading to EOF must not silently
//     accept a truncated/corrupted stream.
//   - A truncated record followed by transport EOF must surface as
//     io.ErrUnexpectedEOF (gc's EOF-mid-record outcome), not clean EOF.
//   - App data followed by the peer's close must deliver all bytes
//     first, then EOF.
//
// The corrupt/truncated tests fail on the pre-fix code (unwrap returned
// null for both close and error, and __tlsConnRead mapped null to
// io.EOF). The app-data-then-close test pins the end-to-end contract;
// its single-flight variant (app data + close_notify in ONE unwrap
// call, where the pre-fix code discarded the bytes) is covered by the
// Java unit test graalgo.p3tls.TlsUnwrapTest, because a graalgo peer's
// Close sends a TCP FIN without a close_notify record, so the
// graalgo-to-graalgo leg never drives unwrap's CLOSED path.
//
// Cert fixtures copied verbatim from test/corpus/wave3/crypto/tls/01_handshake.go
// (same precedent as test/regression/tls-wrap-hang).
//
// No in-test timeout is possible (timers do not fire under graalgo,
// PKG-048): a reintroduced hang blocks this test until test-regression's
// per-directory `timeout 600` kills it and fails the gate, the
// established pattern for hang regressions. Note the server side of a
// successful handshake sleeps 10s (synth_crypto_tls.go:750, Oct queue
// RV-P0-01, owned by another lane and deliberately untouched here), so
// each test takes ~10s.
package tlsunwrapeof

import (
	"bytes"
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"io"
	"net"
	"testing"
)

const caPEM = `-----BEGIN CERTIFICATE-----
MIIC8TCCAdmgAwIBAgIBATANBgkqhkiG9w0BAQsFADAaMRgwFgYDVQQDEw9ncmFh
bGdvLXRlc3QtY2EwHhcNMjAwMTAxMDAwMDAwWhcNMzAwMTAxMDAwMDAwWjAaMRgw
FgYDVQQDEw9ncmFhbGdvLXRlc3QtY2EwggEiMA0GCSqGSIb3DQEBAQUAA4IBDwAw
ggEKAoIBAQDs90AwLve5K+6JJm09i29+OvTr0RzhcLA3JaZD2+WkkBlRElaeWDpb
mNHa/8eDygRjwtJEPPEDpkkNuEbmXgd8gnrGeMqOhRvyVqI4b0TdvtTn5o5DM6OR
gjDXuXxaUgpE1pJU7/jEWFH/EFftiNmLO6+lt/drJKb5/l5nnHROK+p7g+gXDyf3
L5x8c3RoSSFVaTAVQE1zc8puQcQu4NLBCs6+/6xtnSpNofUmTqUCQEpQx5a8f8hy
tI18tCZVkRfT7RP95DS+Y9M9OW9RblQSiBAW5+DuQD7EexXNTnBoj4RpV/fRHP0x
HRGcTj6wEC9G8LiwDLdMs1gwiLZO5anhAgMBAAGjQjBAMA4GA1UdDwEB/wQEAwIC
hDAPBgNVHRMBAf8EBTADAQH/MB0GA1UdDgQWBBQjW9fRc89jeQWJUkgdXjdiFTrF
NDANBgkqhkiG9w0BAQsFAAOCAQEAO3neSSMLLgrsJp83UERZTlh4DLZl1/JxZMie
2XHmiuqRpw5h1s0cwkFrINGsvi/SxivDGfn0BSEQtFlhsvkLenldphBPbl0YFrJz
7JokPmLnDgAjk4CkAN/e93/TzpMFDN4Cij/QFeBVD2tT0YysP3ayZnEpzddqWtqt
u/HaZgBkuUoHkNl06wuBtRJc12yF1oJLZOyvefjBCgccWk7AJocdMrEb5kmYcIX8
ARGxHcItbk6jvN2cMqZPRaZxNq9P0ybNZVwZxQljw64t9hTjEXLjT3LtEjj1nR0g
/3sutkp8bp+QXRCzuyExiTID2OEogdWT4iATG9dJJAxvbvALRQ==
-----END CERTIFICATE-----`
const srvPEM = `-----BEGIN CERTIFICATE-----
MIIDGjCCAgKgAwIBAgIBAjANBgkqhkiG9w0BAQsFADAaMRgwFgYDVQQDEw9ncmFh
bGdvLXRlc3QtY2EwHhcNMjAwMTAxMDAwMDAwWhcNMzAwMTAxMDAwMDAwWjAUMRIw
EAYDVQQDEwlsb2NhbGhvc3QwggEiMA0GCSqGSIb3DQEBAQUAA4IBDwAwggEKAoIB
AQCt1dUUe3CCvA2lW3k9CTei512AP2p+IUEDuoaxyi+cEwEFQKtqE8/VS59lD3KB
ZBc1Vls20uf3LmsMX/fI062SEZm+D1KQrGj4azDkUFbtGGGRV+ujmBdE6SBomWnb
AJrF9CeAXIJT1QC/JUqydFXiZt/TUN6+h8pG8+Fb2tbDLGRV86ZlemW66Ro1W7jm
uFQOvlbVdoas2DLWHLZU5RM7U0qtjshCEOt5kxXpayxRMm+ax3MGlogUkX4gwczM
jB6msEQwuFs7rvog+shHysioF1Q6U/Q6fejSSm6IKTqrCf2RDUHDCXHyCMQ4DLBe
lqmkI9v/SuW4124vRIqsAD3pAgMBAAGjcTBvMA4GA1UdDwEB/wQEAwIHgDATBgNV
HSUEDDAKBggrBgEFBQcDATAfBgNVHSMEGDAWgBQjW9fRc89jeQWJUkgdXjdiFTrF
NDAnBgNVHREEIDAegglsb2NhbGhvc3SCC2V4YW1wbGUuY29thwR/AAABMA0GCSqG
SIb3DQEBCwUAA4IBAQCv149c0cHrbjaiJMnTrTKzloTMZVaqbcBIdmZDKbz0WFmn
EXW3eQy4KCC5II3rdOf3jDAdE8LWP6yfshDrIoMy/Ze1r896u8yE/LlzDxyxjr+Q
LYv4snQRJk72bAYeFAwpwLVulLYlZh29V+DNWL6a/+4VcStQLJ8adRA4RqtgMesp
3mE+/POgpwCmz5kP1+rA1noaWh2PslS4k6Igvn60WsZ3vVCX37J9js6ML3Zxhadn
nyvcveOSGnYVN3vN/ZqzUUFGRGZDkC0lEcSb6+qXZIQOVGfTT4j5PmQEfImfmpAX
mS98pvkmVmYJJFgkLNz9bVYiKxbYq3leFurZFGUO
-----END CERTIFICATE-----`
const srvKeyPEM = `-----BEGIN RSA PRIVATE KEY-----
MIIEogIBAAKCAQEArdXVFHtwgrwNpVt5PQk3ouddgD9qfiFBA7qGscovnBMBBUCr
ahPP1UufZQ9ygWQXNVZbNtLn9y5rDF/3yNOtkhGZvg9SkKxo+Gsw5FBW7RhhkVfr
o5gXROkgaJlp2wCaxfQngFyCU9UAvyVKsnRV4mbf01DevofKRvPhW9rWwyxkVfOm
ZXpluukaNVu45rhUDr5W1XaGrNgy1hy2VOUTO1NKrY7IQhDreZMV6WssUTJvmsdz
BpaIFJF+IMHMzIweprBEMLhbO676IPrIR8rIqBdUOlP0On3o0kpuiCk6qwn9kQ1B
wwlx8gjEOAywXpappCPb/0rluNduL0SKrAA96QIDAQABAoIBAChrdIi3LRGQ9WFS
FC4vgvrDblfAJxxd0hayW9hiKVDSkGWuxEy9f5FabJ0ciuTwJ39U7Y+hWaN9bRGr
DNvq8ExrAEMToACYvi2pCuzDi3rEtQcNGPLojdyNkmYP6e+sw9AsdbTgjcM9X1eS
rbwx1bh3cCrb/nbx7wQxd+CaUV5/BUyU36l9LShtpigsonjL0P0Ew+fm79bOZpOe
ap1zBWkhnhnOSA4eRynqpXak3KO1HIFgh1T45e9q3yXwUV/PLKLNkEXCRAWaOiHh
xBllZU+ewP/9eSiRvz9e+TVfbk/phbBRm37MwBDdZttSBPrVa99cHxZfj+4Rd1VK
sbPyYeMCgYEA2b9y7gn/q5HDKItIZuNdLx1aj49JHyFWtf0PgupFUhksDPg8nGp9
wSWFGUd5TaRLqod9ducEbOQRdJzUDOo9fkzd8lklqdMtF/0qECJR8F16FLye4ajf
Xc+4vv+Me3Hv+l5+r1af6AcBO8vHDkM7CS1aF3rLPsVPU5IcQEumklcCgYEAzF+M
bwm6Inen4POnEepjFfGWS8LQ3lSb9zt2pdC4liYxbznr/YTddOX2Qm4LpwJH7S0T
ZCVpwVcTPf3voDb3uSOUspoi670OKgGuKbePoUfzT2NBwgXMV3eOtfhvqYmXT22a
NXKqfPJnrEtuUzoem3yTYUY4XdZi3Gro0miMCb8CgYBvVZFJHrmoGtZjU5wIZ3ZM
0cBtKMEldqFkWejlci1RheddFku/Wj/EUw/T1hSy2y+JGM/QzNwZZR97tXhCxj38
5waQlH5Kb9xdqKcqKrIkqVbzZWIDVZon2D7JAdbtg1tFc61or20BcRtzXIUByORV
k+e9m55yEOa3fzyMr2GS7wKBgBhce1zxB+L76GMEKDIWgmEnPwcg3cziwQSjbe0u
CBK90r6zZNH/eHZqwwQAIhtyRiibauhRHV4ShxSopFuwsa678wmnHeqDyqBvOtd2
LgTHktUvu7LpBgaXFcfTKVxILmUlZ3Bfjrr+GiWLiEcbcnKOUvMRb76fw4EKrV4H
YEn5AoGAI5nb8K9GaCSoPKVojgUezQHpmJ55x1S+XGAx2BjZphZj27OvqNE7NB9R
O2twfPCYDMvTQiE8cmquId60jxYnCWblSoW2qEWzVizIKuCbBjuGswdgqAWp8IX0
C5baQ4MR6V6bahfKjdlUCWpAhRpl8DgJ/k/hVMPWltreAOlSN9Y=
-----END RSA PRIVATE KEY-----`

func serverConfig() *tls.Config {
	cert, err := tls.X509KeyPair([]byte(srvPEM), []byte(srvKeyPEM))
	if err != nil {
		panic("X509KeyPair: " + err.Error())
	}
	return &tls.Config{Certificates: []tls.Certificate{cert}}
}

func clientConfig() *tls.Config {
	caBlock, _ := pem.Decode([]byte(caPEM))
	ca, err := x509.ParseCertificate(caBlock.Bytes)
	if err != nil {
		panic("parse ca: " + err.Error())
	}
	roots := x509.NewCertPool()
	roots.AddCert(ca)
	return &tls.Config{RootCAs: roots, ServerName: "localhost"}
}

// runPair starts a TLS server that runs serverFn after a successful
// handshake, and returns a handshaked client connection, a channel
// receiving the server goroutine's outcome, and a cleanup function the
// caller defers. (t.Cleanup is deliberately NOT used: graalgo's testing
// shim has no Cleanup method, and the established regression-test
// pattern is defer — see test/regression/tls-wrap-hang.)
func runPair(t *testing.T, serverFn func(*tls.Conn)) (*tls.Conn, chan error, func()) {
	t.Helper()
	ln, err := tls.Listen("tcp", "127.0.0.1:0", serverConfig())
	if err != nil {
		t.Fatalf("tls.Listen: %v", err)
	}
	cleanup := func() { ln.Close() }
	done := make(chan error, 1)
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			done <- err
			return
		}
		defer conn.Close()
		tlsConn := conn.(*tls.Conn)
		if err := tlsConn.Handshake(); err != nil {
			done <- err
			return
		}
		serverFn(tlsConn)
		done <- nil
	}()
	raw, err := net.Dial("tcp", ln.Addr().String())
	if err != nil {
		t.Fatalf("net.Dial: %v", err)
	}
	client := tls.Client(raw, clientConfig())
	if err := client.Handshake(); err != nil {
		t.Fatalf("client Handshake: %v", err)
	}
	return client, done, cleanup
}

func TestCorruptRecordReadIsErrorNotEOF(t *testing.T) {
	client, done, cleanup := runPair(t, func(server *tls.Conn) {
		// A handshake-type record after the handshake, with a payload
		// of 0xFF bytes: not a valid handshake message, so the
		// engine must fail the unwrap. Written on the raw connection
		// to bypass the server's own record layer.
		garbage := append([]byte{0x16, 0x03, 0x01, 0x00, 0x20},
			bytes.Repeat([]byte{0xFF}, 32)...)
		if _, err := server.NetConn().Write(garbage); err != nil {
			t.Errorf("server write garbage: %v", err)
		}
		server.NetConn().Close()
	})
	defer client.Close()
	defer cleanup()
	buf := make([]byte, 64)
	n, err := client.Read(buf)
	if err == nil {
		t.Fatalf("Read = (%d, nil), want a non-EOF error for a corrupt record", n)
	}
	if err == io.EOF {
		t.Fatalf("Read = (%d, EOF), want a non-EOF error for a corrupt record (pre-fix: protocol error reported as clean EOF)", n)
	}
	if n != 0 {
		t.Fatalf("Read = (%d, %v), want (0, error)", n, err)
	}
	if err := <-done; err != nil {
		t.Fatalf("server: %v", err)
	}
}

func TestTruncatedRecordReadIsUnexpectedEOF(t *testing.T) {
	client, done, cleanup := runPair(t, func(server *tls.Conn) {
		// An application-data header claiming 32 payload bytes, with
		// only 3 payload bytes sent before the transport closes: the
		// record is truncated, so the client's read must fail with
		// io.ErrUnexpectedEOF, not clean EOF.
		partial := []byte{0x17, 0x03, 0x03, 0x00, 0x20, 0x01, 0x02, 0x03}
		if _, err := server.NetConn().Write(partial); err != nil {
			t.Errorf("server write partial: %v", err)
		}
		server.NetConn().Close()
	})
	defer client.Close()
	defer cleanup()
	buf := make([]byte, 64)
	n, err := client.Read(buf)
	if !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Fatalf("Read = (%d, %v), want io.ErrUnexpectedEOF for a truncated record", n, err)
	}
	if n != 0 {
		t.Fatalf("Read = (%d, %v), want (0, error)", n, err)
	}
	if err := <-done; err != nil {
		t.Fatalf("server: %v", err)
	}
}

func TestAppDataThenCloseDeliversAllThenEOF(t *testing.T) {
	client, done, cleanup := runPair(t, func(server *tls.Conn) {
		if _, err := server.Write([]byte("final-bytes")); err != nil {
			t.Errorf("server write: %v", err)
		}
		server.Close()
	})
	defer client.Close()
	defer cleanup()
	buf := make([]byte, 64)
	n, err := client.Read(buf)
	if err != nil {
		t.Fatalf("first Read = (%d, %v), want (11, nil)", n, err)
	}
	if string(buf[:n]) != "final-bytes" {
		t.Fatalf("first Read = %q, want %q", buf[:n], "final-bytes")
	}
	n, err = client.Read(buf)
	if err != io.EOF {
		t.Fatalf("second Read = (%d, %v), want (0, io.EOF)", n, err)
	}
	if err := <-done; err != nil {
		t.Fatalf("server: %v", err)
	}
}
