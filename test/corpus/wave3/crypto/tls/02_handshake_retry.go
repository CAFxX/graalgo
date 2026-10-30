package main

// P1-18 regression program: a handshake against a forged chain (valid
// CA, wrong ServerName) must fail, and a second Handshake() call on the
// same Conn must fail again — never return nil without re-verifying.
// Output is booleans only, so gc and graalgo agree byte-for-byte even
// though their error strings differ.
//
// NOTE on the server side: the server deliberately prints nothing about
// its own handshake outcome. gc aborts the server handshake with a
// fatal alert when client verification fails, while graalgo's
// post-handshake Go verification sends no alert, so the server may
// report success; that gc-parity gap is a separate open item (the
// pinned property here is the CLIENT retry behavior). The server just
// runs its handshake, ignores the result, and exits.

import (
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"net"
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

// forgedClientConfig trusts the test CA but asks for a name the server
// certificate was not issued for (it is valid for localhost and
// example.com only): chain building succeeds, hostname verification
// must fail.
func forgedClientConfig() *tls.Config {
	caBlock, _ := pem.Decode([]byte(caPEM))
	ca, err := x509.ParseCertificate(caBlock.Bytes)
	if err != nil {
		panic("parse ca: " + err.Error())
	}
	roots := x509.NewCertPool()
	roots.AddCert(ca)
	return &tls.Config{RootCAs: roots, ServerName: "forged.example.invalid"}
}

func runServer(port string, ready chan<- string, done chan<- struct{}) {
	defer close(done)
	ln, err := tls.Listen("tcp", "127.0.0.1:"+port, serverConfig())
	if err != nil {
		panic("listen: " + err.Error())
	}
	defer ln.Close()
	ready <- ln.Addr().String()
	conn, err := ln.Accept()
	if err != nil {
		panic("accept: " + err.Error())
	}
	defer conn.Close()
	tlsConn := conn.(*tls.Conn)
	// Result intentionally ignored (see the header note): only the
	// client's retry behavior is pinned by this program.
	_ = tlsConn.Handshake()
}

func main() {
	// In-process client+server (graalgo-to-graalgo); loopback-only.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		panic(err)
	}
	tcpAddr := ln.Addr().(*net.TCPAddr)
	ln.Close()
	ephemPort := fmt.Sprint(tcpAddr.Port)
	ready := make(chan string, 1)
	done := make(chan struct{})
	go runServer(ephemPort, ready, done)
	<-ready
	rawConn, err := net.Dial("tcp", "127.0.0.1:"+ephemPort)
	if err != nil {
		panic("dial: " + err.Error())
	}
	// NOTE: tls.Dial cannot be used here — it closes the connection on
	// handshake failure and returns no Conn to retry on. An explicit
	// Client + Handshake pair is what exercises the retry path.
	conn := tls.Client(rawConn, forgedClientConfig())
	err1 := conn.Handshake()
	err2 := conn.Handshake()
	fmt.Println("client: first-err:", err1 != nil)
	fmt.Println("client: retry-err:", err2 != nil)
	fmt.Println("client: retry-still-fails:", err2 != nil)
	conn.Close()
	<-done
}
