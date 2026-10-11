package main

import (
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"time"
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

const expPEM = `-----BEGIN CERTIFICATE-----
MIIC+jCCAeKgAwIBAgIBAzANBgkqhkiG9w0BAQsFADAaMRgwFgYDVQQDEw9ncmFh
bGdvLXRlc3QtY2EwHhcNMjAwMTAxMDAwMDAwWhcNMjEwMTAxMDAwMDAwWjASMRAw
DgYDVQQDEwdleHBpcmVkMIIBIjANBgkqhkiG9w0BAQEFAAOCAQ8AMIIBCgKCAQEA
rdXVFHtwgrwNpVt5PQk3ouddgD9qfiFBA7qGscovnBMBBUCrahPP1UufZQ9ygWQX
NVZbNtLn9y5rDF/3yNOtkhGZvg9SkKxo+Gsw5FBW7RhhkVfro5gXROkgaJlp2wCa
xfQngFyCU9UAvyVKsnRV4mbf01DevofKRvPhW9rWwyxkVfOmZXpluukaNVu45rhU
Dr5W1XaGrNgy1hy2VOUTO1NKrY7IQhDreZMV6WssUTJvmsdzBpaIFJF+IMHMzIwe
prBEMLhbO676IPrIR8rIqBdUOlP0On3o0kpuiCk6qwn9kQ1Bwwlx8gjEOAywXpap
pCPb/0rluNduL0SKrAA96QIDAQABo1MwUTAOBgNVHQ8BAf8EBAMCB4AwHwYDVR0j
BBgwFoAUI1vX0XPPY3kFiVJIHV43YhU6xTQwHgYDVR0RBBcwFYITZXhwaXJlZC5l
eGFtcGxlLmNvbTANBgkqhkiG9w0BAQsFAAOCAQEAsL2xIJ405N0LZaEIqR4bvGjm
+/zGkHhuAoH3AHJx6ZTi2ikO6Kkcyrbn5kpLvkbsUfBNVbs4+nO4Up0Nk4ycJy/K
/k3tMqqcXa4VBs9EF8ACZNkfLwaHylVXtc+q7ti1rcChshxm8O+L393wMwJbWr2S
H7KDskPlE6oSK+iv9iNWgij43ptpPWKqMHt03yUfZQi4NU9KmzmwHcJ/DGQkIoej
utg+tk7ElICSFFbVG5U4QkzSAsEE+BF0dEjjkgBhmbl3+kIrq4FCSt4knzktcXRG
ZafZTqEzQcyXFLCTLTkMHaKSsN4HDI9lsLZsHZ51xUA9hb9lB8KOIWXrZ5bnow==
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

var testTime = time.Date(2025, 6, 1, 0, 0, 0, 0, time.UTC)

func mustParse(nm, pemStr string) *x509.Certificate {
	block, _ := pem.Decode([]byte(pemStr))
	if block == nil {
		panic("no PEM block: " + nm)
	}
	c, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		panic(nm + ": " + err.Error())
	}
	return c
}

func main() {
	ca := mustParse("ca", caPEM)
	leaf := mustParse("leaf", srvPEM)
	expired := mustParse("expired", expPEM)

	// Validity period (exercises time.Unix path).
	fmt.Println("nb:", leaf.NotBefore.UTC().Format("2006-01-02"))
	fmt.Println("na:", leaf.NotAfter.UTC().Format("2006-01-02"))

	// SANs.
	fmt.Println("dns:", leaf.DNSNames)
	fmt.Println("ips:", leaf.IPAddresses)

	roots := x509.NewCertPool()
	roots.AddCert(ca)

	// Good chain with DNSName + EKU.
	chains, err := leaf.Verify(x509.VerifyOptions{
		Roots:       roots,
		DNSName:     "localhost",
		CurrentTime: testTime,
		KeyUsages:   []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	})
	fmt.Println("verify-ok:", err == nil, "chains:", len(chains))
	if err == nil && len(chains) > 0 {
		fmt.Println("chain-len:", len(chains[0]))
		fmt.Println("chain0-is-ca:", chains[0][len(chains[0])-1].IsCA)
	}

	// Wrong DNSName.
	_, err = leaf.Verify(x509.VerifyOptions{Roots: roots, DNSName: "wrong.example.com", CurrentTime: testTime})
	fmt.Println("verify-bad-dns:", err != nil)

	// Expired certificate.
	_, err = expired.Verify(x509.VerifyOptions{Roots: roots, DNSName: "expired.example.com", CurrentTime: testTime})
	fmt.Println("verify-expired:", err != nil)

	// No roots.
	_, err = leaf.Verify(x509.VerifyOptions{DNSName: "localhost", CurrentTime: testTime})
	fmt.Println("verify-no-roots:", err != nil)

	// VerifyHostname.
	fmt.Println("hostname-ok:", leaf.VerifyHostname("example.com") == nil)
	fmt.Println("hostname-wild-not:", leaf.VerifyHostname("evil.com") != nil)
	fmt.Println("hostname-ip:", leaf.VerifyHostname("127.0.0.1") == nil)

	// CheckSignatureFrom.
	fmt.Println("sig-ok:", leaf.CheckSignatureFrom(ca) == nil)
	fmt.Println("sig-bad:", leaf.CheckSignatureFrom(leaf) != nil)

	// Private key parsing (PKCS#1).
	keyBlock, _ := pem.Decode([]byte(srvKeyPEM))
	pkcs1key, err := x509.ParsePKCS1PrivateKey(keyBlock.Bytes)
	fmt.Println("pkcs1-ok:", err == nil && pkcs1key != nil)
}
