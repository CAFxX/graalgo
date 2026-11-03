// Regression test for REVIEW-2026-11-03-13 P1-3 (tls-connstate):
// ConnectionState() on the error path reported version/cipher 65535:
// TlsRuntime.connectionState returned -1 (bad handle, unknown protocol,
// unknown suite name) and the Go side cast it to uint16 without a check,
// so uint16(-1) == 65535. gc reports zero values there. The fix maps a
// negative pack (and a negative half) to zero fields on the Go side and
// normalizes the -1 sentinel to 0 on the Java side; this test pins the
// observable Go behavior through the real trap.
//
// Unknown protocol/suite strings are not reachable through the public Go
// API (the JDK only ever returns known names), so those two parser cases
// are pinned at the Java layer (TlsRuntimeConnectionStateTest); this file
// pins the reachable error path (bad handle) and the success path.
//
// Cert fixtures copied verbatim from test/corpus/wave3/crypto/tls/01_handshake.go
// (via test/regression/tls-wrap-hang/mismatch_test.go).
package tlsconnstate

import (
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
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

// Bad handle: a Server with no certificates never creates an engine
// (engineHandle 0), so ConnectionState goes through the connectionState
// error path. Pre-fix it reported version/cipher 65535; gc reports zeros.
func TestBadHandleConnectionStateIsZero(t *testing.T) {
	client, server := net.Pipe()
	defer client.Close()
	defer server.Close()
	conn := tls.Server(server, &tls.Config{})
	cs := conn.ConnectionState()
	if cs.Version != 0 || cs.CipherSuite != 0 {
		t.Fatalf("bad-handle ConnectionState = version %d suite %d, want 0/0", cs.Version, cs.CipherSuite)
	}
	if cs.HandshakeComplete {
		t.Fatalf("bad-handle HandshakeComplete = true, want false")
	}
}

type serverResult struct {
	err error
	cs  tls.ConnectionState
}

// Success path: after a real handshake both sides must report the same
// nonzero negotiated pair — the real values, not zeros and not 65535.
func TestHandshakeConnectionStateReportsRealPair(t *testing.T) {
	ln, err := tls.Listen("tcp", "127.0.0.1:0", serverConfig())
	if err != nil {
		t.Fatalf("tls.Listen: %v", err)
	}
	defer ln.Close()
	serverCh := make(chan serverResult, 1)
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			serverCh <- serverResult{err: err}
			return
		}
		defer conn.Close()
		srv := conn.(*tls.Conn)
		// Server Handshake sleeps 10s on success (Oct queue RV-P0-01,
		// deliberately untouched by this lane) before returning.
		err = srv.Handshake()
		serverCh <- serverResult{err: err, cs: srv.ConnectionState()}
	}()
	raw, err := net.Dial("tcp", ln.Addr().String())
	if err != nil {
		t.Fatalf("net.Dial: %v", err)
	}
	defer raw.Close()
	cli := tls.Client(raw, clientConfig())
	if err := cli.Handshake(); err != nil {
		t.Fatalf("client Handshake: %v", err)
	}
	cliCS := cli.ConnectionState()
	if !cliCS.HandshakeComplete {
		t.Fatalf("client HandshakeComplete = false after successful handshake")
	}
	if cliCS.Version == 0 || cliCS.CipherSuite == 0 {
		t.Fatalf("client ConnectionState = version %d suite %d, want nonzero pair", cliCS.Version, cliCS.CipherSuite)
	}
	if cliCS.Version == 0xffff || cliCS.CipherSuite == 0xffff {
		t.Fatalf("client ConnectionState = version %d suite %d, sentinel 65535 leaked", cliCS.Version, cliCS.CipherSuite)
	}
	if len(cliCS.PeerCertificates) == 0 {
		t.Fatalf("client ConnectionState has no peer certificates after successful handshake")
	}
	res := <-serverCh
	if res.err != nil {
		t.Fatalf("server Handshake: %v", res.err)
	}
	if res.cs.Version != cliCS.Version || res.cs.CipherSuite != cliCS.CipherSuite {
		t.Fatalf("client/server pair mismatch: client %d/%d, server %d/%d",
			cliCS.Version, cliCS.CipherSuite, res.cs.Version, res.cs.CipherSuite)
	}
}
