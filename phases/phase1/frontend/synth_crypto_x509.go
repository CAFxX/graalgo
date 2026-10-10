package main

// P3-M9: synthetic 'crypto/x509' package.
//
// Strategy (per MEMORY.md 2026-10-10 stdlib strategy): delegate to Java's
// certificate handling (java.security.cert) where semantics match, rather
// than reimplementing in Go. Certificate parsing delegates to
// CertificateFactory via graalgo.x509.* intrinsics (see
// phases/phase3/src/main/java/graalgo/p3x509/GoX509.java).
// Certificate creation (CreateCertificate) stays in Go (no standard JDK API).
//
// The Certificate struct carries an unexported javaCert field holding the
// java.security.cert.X509Certificate for field extraction.
const syntheticCryptoX509Source = `package x509

import (
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rsa"
	"crypto/x509/pkix"
	"encoding/asn1"
	"encoding/pem"
	"errors"
	"fmt"
	"math/big"
	"net"
	"time"
)

//go:linkname graalgoX509Parse graalgo.x509.parse
func graalgoX509Parse(der []byte) any

//go:linkname graalgoX509SerialNumber graalgo.x509.serialNumber
func graalgoX509SerialNumber(cert any) []byte

//go:linkname graalgoX509Subject graalgo.x509.subject
func graalgoX509Subject(cert any) string

//go:linkname graalgoX509Issuer graalgo.x509.issuer
func graalgoX509Issuer(cert any) string

//go:linkname graalgoX509NotBefore graalgo.x509.notBefore
func graalgoX509NotBefore(cert any) int64

//go:linkname graalgoX509NotAfter graalgo.x509.notAfter
func graalgoX509NotAfter(cert any) int64

//go:linkname graalgoX509Version graalgo.x509.version
func graalgoX509Version(cert any) int64

//go:linkname graalgoX509SigAlgOid graalgo.x509.sigAlgOid
func graalgoX509SigAlgOid(cert any) string

//go:linkname graalgoX509Signature graalgo.x509.signature
func graalgoX509Signature(cert any) []byte

//go:linkname graalgoX509TBSCertificate graalgo.x509.tbsCertificate
func graalgoX509TBSCertificate(cert any) []byte

//go:linkname graalgoX509KeyUsage graalgo.x509.keyUsage
func graalgoX509KeyUsage(cert any) int64

//go:linkname graalgoX509BasicConstraints graalgo.x509.basicConstraints
func graalgoX509BasicConstraints(cert any) int64

//go:linkname graalgoX509SubjectKeyId graalgo.x509.subjectKeyId
func graalgoX509SubjectKeyId(cert any) []byte

//go:linkname graalgoX509AuthorityKeyId graalgo.x509.authorityKeyId
func graalgoX509AuthorityKeyId(cert any) []byte

//go:linkname graalgoX509PublicKeyAlg graalgo.x509.publicKeyAlg
func graalgoX509PublicKeyAlg(cert any) string

//go:linkname graalgoX509PublicKeyEncoded graalgo.x509.publicKeyEncoded
func graalgoX509PublicKeyEncoded(cert any) []byte

//go:linkname graalgoX509RawIssuer graalgo.x509.rawIssuer
func graalgoX509RawIssuer(cert any) []byte

//go:linkname graalgoX509RawSubject graalgo.x509.rawSubject
func graalgoX509RawSubject(cert any) []byte

// SignatureAlgorithm identifies the signature algorithm.
type SignatureAlgorithm int

const (
	UnknownSignatureAlgorithm SignatureAlgorithm = iota
	MD2WithRSA
	MD5WithRSA
	SHA1WithRSA
	SHA256WithRSA
	SHA384WithRSA
	SHA512WithRSA
	DSAWithSHA1
	DSAWithSHA256
	ECDSAWithSHA1
	ECDSAWithSHA256
	ECDSAWithSHA384
	ECDSAWithSHA512
	SHA256WithRSAPSS
	SHA384WithRSAPSS
	SHA512WithRSAPSS
	PureEd25519
)

// PublicKeyAlgorithm identifies the public key algorithm.
type PublicKeyAlgorithm int

const (
	UnknownPublicKeyAlgorithm PublicKeyAlgorithm = iota
	RSA
	DSA
	ECDSA
	Ed25519
	X25519
)

// KeyUsage bitmask.
type KeyUsage int

const (
	KeyUsageDigitalSignature KeyUsage = 1 << iota
	KeyUsageContentCommitment
	KeyUsageKeyEncipherment
	KeyUsageDataEncipherment
	KeyUsageKeyAgreement
	KeyUsageCertSign
	KeyUsageCRLSign
	KeyUsageEncipherOnly
	KeyUsageDecipherOnly
)

// ExtKeyUsage identifies extended key usage.
type ExtKeyUsage int

const (
	ExtKeyUsageAny ExtKeyUsage = iota
	ExtKeyUsageServerAuth
	ExtKeyUsageClientAuth
	ExtKeyUsageCodeSigning
	ExtKeyUsageEmailProtection
	ExtKeyUsageIPSECEndSystem
	ExtKeyUsageIPSECTunnel
	ExtKeyUsageIPSECUser
	ExtKeyUsageTimeStamping
	ExtKeyUsageOCSPSigning
	ExtKeyUsageMicrosoftServerGatedCrypto
	ExtKeyUsageNetscapeServerGatedCrypto
	ExtKeyUsageMicrosoftCommercialCodeSigning
	ExtKeyUsageMicrosoftKernelCodeSigning
)

// Certificate represents an X.509 certificate.
type Certificate struct {
	Raw                     []byte
	RawTBSCertificate       []byte
	RawSubjectPublicKeyInfo []byte
	RawSubject              []byte
	RawIssuer               []byte

	Signature          []byte
	SignatureAlgorithm SignatureAlgorithm

	PublicKeyAlgorithm PublicKeyAlgorithm
	PublicKey          any

	Version             int
	SerialNumber        *big.Int
	Issuer              pkix.Name
	Subject             pkix.Name
	NotBefore, NotAfter time.Time
	KeyUsage            KeyUsage

	Extensions                  []pkix.Extension
	ExtraExtensions             []pkix.Extension
	UnhandledCriticalExtensions []asn1.ObjectIdentifier
	ExtKeyUsage                 []ExtKeyUsage
	UnknownExtKeyUsage          []asn1.ObjectIdentifier
	BasicConstraintsValid       bool
	IsCA                        bool
	MaxPathLen                  int
	MaxPathLenZero              bool
	SubjectKeyId                []byte
	AuthorityKeyId              []byte
	OCSPServer                  []string
	IssuingCertificateURL       []string
	DNSNames                    []string
	EmailAddresses              []string
	IPAddresses                 []net.IP
	URIs                        []string

	// javaCert holds the java.security.cert.X509Certificate (unexported).
	javaCert any
}

// sigAlgFromOID maps signature algorithm OIDs to SignatureAlgorithm.
func sigAlgFromOID(oid string) SignatureAlgorithm {
	switch oid {
	case "1.2.840.113549.1.1.2":
		return MD2WithRSA
	case "1.2.840.113549.1.1.4":
		return MD5WithRSA
	case "1.2.840.113549.1.1.5":
		return SHA1WithRSA
	case "1.2.840.113549.1.1.11":
		return SHA256WithRSA
	case "1.2.840.113549.1.1.12":
		return SHA384WithRSA
	case "1.2.840.113549.1.1.13":
		return SHA512WithRSA
	case "1.2.840.10045.4.1":
		return ECDSAWithSHA1
	case "1.2.840.10045.4.3.2":
		return ECDSAWithSHA256
	case "1.2.840.10045.4.3.3":
		return ECDSAWithSHA384
	case "1.2.840.10045.4.3.4":
		return ECDSAWithSHA512
	case "1.2.840.113549.1.1.10":
		return SHA256WithRSAPSS
	case "1.3.101.112":
		return PureEd25519
	}
	return UnknownSignatureAlgorithm
}

// pubKeyAlgFromName maps Java algorithm names to PublicKeyAlgorithm.
func pubKeyAlgFromName(name string) PublicKeyAlgorithm {
	switch name {
	case "RSA":
		return RSA
	case "EC", "ECDSA":
		return ECDSA
	case "Ed25519", "EdDSA":
		return Ed25519
	case "X25519", "XDH":
		return X25519
	case "DSA":
		return DSA
	}
	return UnknownPublicKeyAlgorithm
}

// parseName parses RFC 2253 DN into pkix.Name (simplified).
func parseName(dn string) pkix.Name {
	var name pkix.Name
	start := 0
	for i := 0; i <= len(dn); i++ {
		if i == len(dn) || (dn[i] == ',' && (i == 0 || dn[i-1] != '\\')) {
			part := dn[start:i]
			eq := -1
			for j := 0; j < len(part); j++ {
				if part[j] == '=' {
					eq = j
					break
				}
			}
			if eq > 0 {
				key := part[:eq]
				val := unescapeDN(part[eq+1:])
				switch key {
				case "CN":
					name.CommonName = val
				case "O":
					name.Organization = append(name.Organization, val)
				case "OU":
					name.OrganizationalUnit = append(name.OrganizationalUnit, val)
				case "C":
					name.Country = append(name.Country, val)
				case "L":
					name.Locality = append(name.Locality, val)
				case "ST":
					name.Province = append(name.Province, val)
				}
			}
			start = i + 1
		}
	}
	return name
}

func unescapeDN(s string) string {
	var out []byte
	for i := 0; i < len(s); i++ {
		if s[i] == '\\' && i+1 < len(s) {
			out = append(out, s[i+1])
			i++
		} else {
			out = append(out, s[i])
		}
	}
	return string(out)
}

// __mrParseCertificatePair is the pair type for ParseCertificate.
type __mrParseCertificatePair struct {
	v0 *Certificate
	v1 error
}

func __mrParseCertificate(der []byte) __mrParseCertificatePair {
	javaCert := graalgoX509Parse(der)
	if javaCert == nil {
		return __mrParseCertificatePair{nil, errors.New("x509: failed to parse certificate")}
	}
	c := &Certificate{Raw: der, javaCert: javaCert}
	c.RawTBSCertificate = graalgoX509TBSCertificate(javaCert)
	c.RawSubject = graalgoX509RawSubject(javaCert)
	c.RawIssuer = graalgoX509RawIssuer(javaCert)
	c.Signature = graalgoX509Signature(javaCert)
	c.SignatureAlgorithm = sigAlgFromOID(graalgoX509SigAlgOid(javaCert))
	c.Version = int(graalgoX509Version(javaCert))
	c.SerialNumber = new(big.Int).SetBytes(graalgoX509SerialNumber(javaCert))
	c.Subject = parseName(graalgoX509Subject(javaCert))
	c.Issuer = parseName(graalgoX509Issuer(javaCert))
	_ = graalgoX509NotBefore(javaCert)
	_ = graalgoX509NotAfter(javaCert)
	c.KeyUsage = KeyUsage(graalgoX509KeyUsage(javaCert))
	bc := graalgoX509BasicConstraints(javaCert)
	if bc >= 0 {
		c.BasicConstraintsValid = true
		c.IsCA = true
		if bc == 2147483647 {
			c.MaxPathLen = -1
		} else {
			c.MaxPathLen = int(bc)
		}
	}
	c.SubjectKeyId = graalgoX509SubjectKeyId(javaCert)
	c.AuthorityKeyId = graalgoX509AuthorityKeyId(javaCert)
	pubAlg := graalgoX509PublicKeyAlg(javaCert)
	c.PublicKeyAlgorithm = pubKeyAlgFromName(pubAlg)
	pubDER := graalgoX509PublicKeyEncoded(javaCert)
	c.RawSubjectPublicKeyInfo = pubDER
	__tmpPK := __mrParsePKIXPublicKey(pubDER)
	if __tmpPK.v1 == nil {
		c.PublicKey = __tmpPK.v0
	}
	return __mrParseCertificatePair{c, nil}
}

func ParseCertificate(der []byte) (*Certificate, error) {
	panic("crypto/x509: ParseCertificate: multi-result desugar")
}

// CertPool is a set of certificates.
type CertPool struct {
	certs []*Certificate
}

// NewCertPool returns a new, empty CertPool.
func NewCertPool() *CertPool {
	return &CertPool{}
}

// AddCert adds a certificate to the pool.
func (s *CertPool) AddCert(cert *Certificate) {
	if cert == nil {
		return
	}
	s.certs = append(s.certs, cert)
}

// AppendCertsFromPEM appends certificates from PEM data.
func (s *CertPool) AppendCertsFromPEM(pemCerts []byte) (ok bool) {
	for len(pemCerts) > 0 {
		var block *pem.Block
		block, pemCerts = pem.Decode(pemCerts)
		if block == nil {
			break
		}
		if block.Type != "CERTIFICATE" || len(block.Headers) != 0 {
			continue
		}
		__tmpC := __mrParseCertificate(block.Bytes)
		if __tmpC.v1 != nil {
			continue
		}
		s.AddCert(__tmpC.v0)
		ok = true
	}
	return ok
}

// Subjects returns the subjects of the certificates in the pool.
func (s *CertPool) Subjects() [][]byte {
	res := make([][]byte, len(s.certs))
	for i, cert := range s.certs {
		res[i] = cert.RawSubject
	}
	return res
}

// Clone returns a copy of the CertPool.
func (s *CertPool) Clone() *CertPool {
	p := &CertPool{certs: make([]*Certificate, len(s.certs))}
	copy(p.certs, s.certs)
	return p
}

// Equal reports whether two CertPools are equal.
func (s *CertPool) Equal(other *CertPool) bool {
	if len(s.certs) != len(other.certs) {
		return false
	}
	for i, c := range s.certs {
		if string(c.RawSubject) != string(other.certs[i].RawSubject) {
			return false
		}
	}
	return true
}

// __mrParsePKIXPublicKeyPair is the pair type for ParsePKIXPublicKey.
type __mrParsePKIXPublicKeyPair struct {
	v0 any
	v1 error
}

func __mrParsePKIXPublicKey(derBytes []byte) __mrParsePKIXPublicKeyPair {
	// TODO: asn1.Unmarshal not in desugar table; stub for now
	return __mrParsePKIXPublicKeyPair{nil, errors.New("x509: ParsePKIXPublicKey stub")}
	oid := pki.Algorithm.Algorithm
	switch {
	case oidEqual(oid, asn1.ObjectIdentifier{1, 2, 840, 113549, 1, 1, 1}):
		__tmpK := __mrParsePKCS1PublicKey(pki.PublicKey.Bytes)
		if __tmpK.v1 != nil {
			return __mrParsePKIXPublicKeyPair{nil, __tmpK.v1}
		}
		return __mrParsePKIXPublicKeyPair{__tmpK.v0, nil}
	case oidEqual(oid, asn1.ObjectIdentifier{1, 2, 840, 10045, 2, 1}):
		keyBytes := pki.PublicKey.Bytes
		if len(keyBytes) == 0 || keyBytes[0] != 4 {
			return __mrParsePKIXPublicKeyPair{nil, errors.New("x509: invalid ECDSA public key")}
		}
		var params asn1.ObjectIdentifier
		if _, err := asn1.Unmarshal(pki.Algorithm.Parameters.FullBytes, &params); err != nil {
			return __mrParsePKIXPublicKeyPair{nil, err}
		}
		curve := namedCurveFromOID(params)
		if curve == nil {
			return __mrParsePKIXPublicKeyPair{nil, errors.New("x509: unknown curve")}
		}
		byteLen := (curve.Params().BitSize + 7) / 8
		if len(keyBytes) != 1+2*byteLen {
			return __mrParsePKIXPublicKeyPair{nil, errors.New("x509: invalid ECDSA public key length")}
		}
		x := new(big.Int).SetBytes(keyBytes[1 : 1+byteLen])
		y := new(big.Int).SetBytes(keyBytes[1+byteLen:])
		return __mrParsePKIXPublicKeyPair{&ecdsa.PublicKey{Curve: curve, X: x, Y: y}, nil}
	case oidEqual(oid, asn1.ObjectIdentifier{1, 3, 101, 112}):
		keyBytes := pki.PublicKey.Bytes
		if len(keyBytes) != ed25519.PublicKeySize {
			return __mrParsePKIXPublicKeyPair{nil, errors.New("x509: invalid Ed25519 public key")}
		}
		pub := make(ed25519.PublicKey, ed25519.PublicKeySize)
		copy(pub, keyBytes)
		return __mrParsePKIXPublicKeyPair{pub, nil}
	}
	return __mrParsePKIXPublicKeyPair{nil, fmt.Errorf("x509: unknown public key algorithm OID %s", oid)}
}

func ParsePKIXPublicKey(derBytes []byte) (pub any, err error) {
	panic("crypto/x509: ParsePKIXPublicKey: multi-result desugar")
}

func oidEqual(a, b asn1.ObjectIdentifier) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func namedCurveFromOID(oid asn1.ObjectIdentifier) elliptic.Curve {
	if oidEqual(oid, asn1.ObjectIdentifier{1, 2, 840, 10045, 3, 1, 1}) {
		return elliptic.P224()
	}
	if oidEqual(oid, asn1.ObjectIdentifier{1, 2, 840, 10045, 3, 1, 7}) {
		return elliptic.P256()
	}
	if oidEqual(oid, asn1.ObjectIdentifier{1, 3, 132, 0, 34}) {
		return elliptic.P384()
	}
	if oidEqual(oid, asn1.ObjectIdentifier{1, 3, 132, 0, 35}) {
		return elliptic.P521()
	}
	return nil
}

type pkcs1PublicKey struct {
	N *big.Int
	E int
}

type __mrParsePKCS1PublicKeyPair struct {
	v0 *rsa.PublicKey
	v1 error
}

func __mrParsePKCS1PublicKey(der []byte) __mrParsePKCS1PublicKeyPair {
	var pub pkcs1PublicKey
	if _, err := asn1.Unmarshal(der, &pub); err != nil {
		return __mrParsePKCS1PublicKeyPair{nil, err}
	}
	if pub.N.Sign() <= 0 || pub.E <= 0 {
		return __mrParsePKCS1PublicKeyPair{nil, errors.New("x509: invalid PKCS#1 public key")}
	}
	return __mrParsePKCS1PublicKeyPair{&rsa.PublicKey{N: pub.N, E: pub.E}, nil}
}

func ParsePKCS1PublicKey(der []byte) (*rsa.PublicKey, error) {
	panic("crypto/x509: ParsePKCS1PublicKey: multi-result desugar")
}
`
