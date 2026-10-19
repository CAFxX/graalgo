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
	"crypto"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rsa"
	"crypto/sha1"
	"crypto/sha256"
	"crypto/sha512"
	"crypto/x509/pkix"
	"encoding/asn1"
	"encoding/pem"
	"errors"
	"fmt"
	"hash"
	"math/big"
	"net"
	"strconv"
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

//go:linkname graalgoX509RsaModulus graalgo.x509.rsaModulus
func graalgoX509RsaModulus(cert any) []byte

//go:linkname graalgoX509RsaExponent graalgo.x509.rsaExponent
func graalgoX509RsaExponent(cert any) int64

//go:linkname graalgoX509EcX graalgo.x509.ecX
func graalgoX509EcX(cert any) []byte

//go:linkname graalgoX509EcY graalgo.x509.ecY
func graalgoX509EcY(cert any) []byte

//go:linkname graalgoX509EcCurveName graalgo.x509.ecCurveName
func graalgoX509EcCurveName(cert any) string

//go:linkname graalgoX509Ed25519Raw graalgo.x509.ed25519Raw
func graalgoX509Ed25519Raw(cert any) []byte

//go:linkname graalgoX509ParseSpki graalgo.x509.parseSpki
func graalgoX509ParseSpki(spki []byte) string

//go:linkname graalgoX509SubjectAltNames graalgo.x509.subjectAltNames
func graalgoX509SubjectAltNames(cert any) string

//go:linkname graalgoX509ParsePrivateKey graalgo.x509.parsePrivateKey
func graalgoX509ParsePrivateKey(der []byte) string

//go:linkname graalgoX509ExtKeyUsage graalgo.x509.extKeyUsage
func graalgoX509ExtKeyUsage(cert any) string

//go:linkname graalgoX509ParsePkcs1 graalgo.x509.parsePkcs1
func graalgoX509ParsePkcs1(der []byte) string

// SignatureAlgorithm identifies the signature algorithm.
type SignatureAlgorithm int

const (
	UnknownSignatureAlgorithm SignatureAlgorithm = iota
	MD2WithRSA
	MD5WithRSA
	SHA1WithRSA
	SHA224WithRSA
	SHA256WithRSA
	SHA384WithRSA
	SHA512WithRSA
	DSAWithSHA1
	DSAWithSHA256
	ECDSAWithSHA1
	ECDSAWithSHA224
	ECDSAWithSHA256
	ECDSAWithSHA384
	ECDSAWithSHA512
	SHA256WithRSAPSS
	SHA384WithRSAPSS
	SHA512WithRSAPSS
	PureEd25519
)

// String mirrors gc's SignatureAlgorithm names; unknown values render numerically.
func (algo SignatureAlgorithm) String() string {
	switch algo {
	case MD2WithRSA:
		return "MD2-RSA"
	case MD5WithRSA:
		return "MD5-RSA"
	case SHA1WithRSA:
		return "SHA1-RSA"
	case SHA224WithRSA:
		return "SHA224-RSA"
	case SHA256WithRSA:
		return "SHA256-RSA"
	case SHA384WithRSA:
		return "SHA384-RSA"
	case SHA512WithRSA:
		return "SHA512-RSA"
	case DSAWithSHA1:
		return "DSA-SHA1"
	case DSAWithSHA256:
		return "DSA-SHA256"
	case ECDSAWithSHA1:
		return "ECDSA-SHA1"
	case ECDSAWithSHA224:
		return "ECDSA-SHA224"
	case ECDSAWithSHA256:
		return "ECDSA-SHA256"
	case ECDSAWithSHA384:
		return "ECDSA-SHA384"
	case ECDSAWithSHA512:
		return "ECDSA-SHA512"
	case SHA256WithRSAPSS:
		return "SHA256-RSAPSS"
	case SHA384WithRSAPSS:
		return "SHA384-RSAPSS"
	case SHA512WithRSAPSS:
		return "SHA512-RSAPSS"
	case PureEd25519:
		return "Ed25519"
	default:
		return strconv.Itoa(int(algo))
	}
}

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

// String mirrors gc's stringer output for KeyUsage.
func (k KeyUsage) String() string {
	switch k {
	case KeyUsageDigitalSignature:
		return "digitalSignature"
	case KeyUsageContentCommitment:
		return "contentCommitment"
	case KeyUsageKeyEncipherment:
		return "keyEncipherment"
	case KeyUsageDataEncipherment:
		return "dataEncipherment"
	case KeyUsageKeyAgreement:
		return "keyAgreement"
	case KeyUsageCertSign:
		return "keyCertSign"
	case KeyUsageCRLSign:
		return "cRLSign"
	case KeyUsageEncipherOnly:
		return "encipherOnly"
	case KeyUsageDecipherOnly:
		return "decipherOnly"
	default:
		return "KeyUsage(" + strconv.Itoa(int(k)) + ")"
	}
}

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
	// NotBefore/NotAfter from the intrinsic millis via time.Unix.
	nbMillis := graalgoX509NotBefore(javaCert)
	naMillis := graalgoX509NotAfter(javaCert)
	c.NotBefore = time.Unix(nbMillis/1000, (nbMillis%1000)*1000000)
	c.NotAfter = time.Unix(naMillis/1000, (naMillis%1000)*1000000)
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
	// Extended key usage OIDs.
	parseExtKeyUsage(c, graalgoX509ExtKeyUsage(javaCert))
	// Subject Alternative Names (DNS/IP/EMAIL/URI) for hostname verification.
	parseSubjectAltNames(c, graalgoX509SubjectAltNames(javaCert))
	pubAlg := graalgoX509PublicKeyAlg(javaCert)
	c.PublicKeyAlgorithm = pubKeyAlgFromName(pubAlg)
	pubDER := graalgoX509PublicKeyEncoded(javaCert)
	c.RawSubjectPublicKeyInfo = pubDER
	__tmpPK := __mrParsePKIXPublicKeyFromCert(javaCert, pubAlg)
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

func __mrParsePKIXPublicKeyFromCert(javaCert any, pubAlg string) __mrParsePKIXPublicKeyPair {
	// Parses the public key from a certificate using JDK delegation.
	// (asn1.Unmarshal cannot be used: pointer-store WS D limitation.)
	switch pubAlg {
	case "RSA":
		n := graalgoX509RsaModulus(javaCert)
		e := graalgoX509RsaExponent(javaCert)
		if n == nil || e < 0 {
			return __mrParsePKIXPublicKeyPair{nil, errors.New("x509: invalid RSA public key")}
		}
		return __mrParsePKIXPublicKeyPair{&rsa.PublicKey{N: new(big.Int).SetBytes(n), E: int(e)}, nil}
	case "EC", "ECDSA":
		x := graalgoX509EcX(javaCert)
		y := graalgoX509EcY(javaCert)
		name := graalgoX509EcCurveName(javaCert)
		if x == nil || y == nil || name == "" {
			return __mrParsePKIXPublicKeyPair{nil, errors.New("x509: invalid ECDSA public key")}
		}
		// The curve is identified by name, never by field size (a
		// 256-bit field does not imply secp256r1; the old field-size
		// mapping silently built P-256 for secp256k1 certificates).
		curve := curveFromName(name)
		if curve == nil {
			return __mrParsePKIXPublicKeyPair{nil, errors.New("x509: unknown curve")}
		}
		return __mrParsePKIXPublicKeyPair{&ecdsa.PublicKey{Curve: curve, X: new(big.Int).SetBytes(x), Y: new(big.Int).SetBytes(y)}, nil}
	case "Ed25519", "EdDSA":
		raw := graalgoX509Ed25519Raw(javaCert)
		if raw == nil || len(raw) != ed25519.PublicKeySize {
			return __mrParsePKIXPublicKeyPair{nil, errors.New("x509: invalid Ed25519 public key")}
		}
		pub := make(ed25519.PublicKey, ed25519.PublicKeySize)
		copy(pub, raw)
		return __mrParsePKIXPublicKeyPair{pub, nil}
	}
	return __mrParsePKIXPublicKeyPair{nil, fmt.Errorf("x509: unknown public key algorithm %s", pubAlg)}
}

// __hexToBytes decodes a hex string (used for parseSpki/parsePkcs1 results).
func __hexToBytes(s string) []byte {
	if len(s)%2 != 0 {
		return nil
	}
	b := make([]byte, len(s)/2)
	for i := 0; i < len(b); i++ {
		hi := __hexVal(s[2*i])
		lo := __hexVal(s[2*i+1])
		if hi < 0 || lo < 0 {
			return nil
		}
		b[i] = byte(hi<<4 | lo)
	}
	return b
}

func __hexVal(c byte) int {
	switch {
	case c >= '0' && c <= '9':
		return int(c - '0')
	case c >= 'a' && c <= 'f':
		return int(c-'a') + 10
	case c >= 'A' && c <= 'F':
		return int(c-'A') + 10
	}
	return -1
}

// __parseSpkiComponents parses the "ALG:..." string from graalgoX509ParseSpki.
func __parseSpkiComponents(s string) __mrParsePKIXPublicKeyPair {
	if s == "" {
		return __mrParsePKIXPublicKeyPair{nil, errors.New("x509: failed to parse SPKI")}
	}
	// Find first colon
	colon1 := -1
	for i := 0; i < len(s); i++ {
		if s[i] == ':' {
			colon1 = i
			break
		}
	}
	if colon1 < 0 {
		return __mrParsePKIXPublicKeyPair{nil, errors.New("x509: failed to parse SPKI")}
	}
	alg := s[:colon1]
	rest := s[colon1+1:]
	switch alg {
	case "RSA":
		// "n_hex:e"
		colon2 := -1
		for i := 0; i < len(rest); i++ {
			if rest[i] == ':' {
				colon2 = i
				break
			}
		}
		if colon2 < 0 {
			return __mrParsePKIXPublicKeyPair{nil, errors.New("x509: failed to parse RSA SPKI")}
		}
		n := __hexToBytes(rest[:colon2])
		eStr := rest[colon2+1:]
		e := 0
		for i := 0; i < len(eStr); i++ {
			if eStr[i] < '0' || eStr[i] > '9' {
				return __mrParsePKIXPublicKeyPair{nil, errors.New("x509: failed to parse RSA exponent")}
			}
			e = e*10 + int(eStr[i]-'0')
		}
		if n == nil || e <= 0 {
			return __mrParsePKIXPublicKeyPair{nil, errors.New("x509: invalid RSA public key")}
		}
		return __mrParsePKIXPublicKeyPair{&rsa.PublicKey{N: new(big.Int).SetBytes(n), E: e}, nil}
	case "EC":
		// "curveName:x_hex:y_hex" — the curve is identified by its JDK
		// name, never by field size (a 256-bit field does not imply
		// secp256r1; the old field-size mapping silently built P-256
		// for secp256k1 keys).
		parts := __splitColon(rest)
		if len(parts) != 3 {
			return __mrParsePKIXPublicKeyPair{nil, errors.New("x509: failed to parse EC SPKI")}
		}
		curve := curveFromName(parts[0])
		if curve == nil {
			return __mrParsePKIXPublicKeyPair{nil, errors.New("x509: unknown curve")}
		}
		x := __hexToBytes(parts[1])
		y := __hexToBytes(parts[2])
		if x == nil || y == nil {
			return __mrParsePKIXPublicKeyPair{nil, errors.New("x509: invalid ECDSA public key")}
		}
		return __mrParsePKIXPublicKeyPair{&ecdsa.PublicKey{Curve: curve, X: new(big.Int).SetBytes(x), Y: new(big.Int).SetBytes(y)}, nil}
	case "Ed25519":
		raw := __hexToBytes(rest)
		if raw == nil || len(raw) != ed25519.PublicKeySize {
			return __mrParsePKIXPublicKeyPair{nil, errors.New("x509: invalid Ed25519 public key")}
		}
		pub := make(ed25519.PublicKey, ed25519.PublicKeySize)
		copy(pub, raw)
		return __mrParsePKIXPublicKeyPair{pub, nil}
	}
	return __mrParsePKIXPublicKeyPair{nil, fmt.Errorf("x509: unknown public key algorithm %s", alg)}
}

// __splitColon splits s on ':' (simple, no empty handling needed here).
func __splitColon(s string) []string {
	var parts []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == ':' {
			parts = append(parts, s[start:i])
			start = i + 1
		}
	}
	parts = append(parts, s[start:])
	return parts
}

func __mrParsePKIXPublicKey(derBytes []byte) __mrParsePKIXPublicKeyPair {
	// Standalone ParsePKIXPublicKey: delegates SPKI parsing to the JDK.
	// (asn1.Unmarshal cannot be used: pointer-store WS D limitation.)
	s := graalgoX509ParseSpki(derBytes)
	if s == "" {
		return __mrParsePKIXPublicKeyPair{nil, errors.New("x509: failed to parse public key")}
	}
	return __parseSpkiComponents(s)
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

type __mrParsePKCS1PublicKeyPair struct {
	v0 *rsa.PublicKey
	v1 error
}

func __mrParsePKCS1PublicKey(der []byte) __mrParsePKCS1PublicKeyPair {
	// Delegates PKCS1 parsing to the JDK (asn1.Unmarshal cannot be used:
	// pointer-store WS D limitation). Returns "n_hex:e".
	s := graalgoX509ParsePkcs1(der)
	if s == "" {
		return __mrParsePKCS1PublicKeyPair{nil, errors.New("x509: failed to parse PKCS#1 public key")}
	}
	colon := -1
	for i := 0; i < len(s); i++ {
		if s[i] == ':' {
			colon = i
			break
		}
	}
	if colon < 0 {
		return __mrParsePKCS1PublicKeyPair{nil, errors.New("x509: failed to parse PKCS#1 public key")}
	}
	n := __hexToBytes(s[:colon])
	eStr := s[colon+1:]
	e := 0
	for i := 0; i < len(eStr); i++ {
		if eStr[i] < '0' || eStr[i] > '9' {
			return __mrParsePKCS1PublicKeyPair{nil, errors.New("x509: failed to parse PKCS#1 exponent")}
		}
		e = e*10 + int(eStr[i]-'0')
	}
	if n == nil || e <= 0 {
		return __mrParsePKCS1PublicKeyPair{nil, errors.New("x509: invalid PKCS#1 public key")}
	}
	bn := new(big.Int).SetBytes(n)
	if bn.Sign() <= 0 {
		return __mrParsePKCS1PublicKeyPair{nil, errors.New("x509: invalid PKCS#1 public key")}
	}
	return __mrParsePKCS1PublicKeyPair{&rsa.PublicKey{N: bn, E: e}, nil}
}

func ParsePKCS1PublicKey(der []byte) (*rsa.PublicKey, error) {
	panic("crypto/x509: ParsePKCS1PublicKey: multi-result desugar")
}

func ParsePKIXPublicKey(derBytes []byte) (any, error) {
	panic("crypto/x509: ParsePKIXPublicKey: multi-result desugar")
}

// ============================================================================
// P3-M9: Subject Alternative Names, private-key parsing, and Verify.
// (Appended for the x509/TLS un-scope-limiting lane.)
// ============================================================================

// parseExtKeyUsage decodes the comma-separated OID list from the
// graalgo.x509.extKeyUsage intrinsic into ExtKeyUsage/UnknownExtKeyUsage.
func parseExtKeyUsage(c *Certificate, oids string) {
	if oids == "" {
		return
	}
	start := 0
	for i := 0; i <= len(oids); i++ {
		if i == len(oids) || oids[i] == ',' {
			oid := oids[start:i]
			start = i + 1
			switch oid {
			case "1.3.6.1.5.5.7.3.1":
				c.ExtKeyUsage = append(c.ExtKeyUsage, ExtKeyUsageServerAuth)
			case "1.3.6.1.5.5.7.3.2":
				c.ExtKeyUsage = append(c.ExtKeyUsage, ExtKeyUsageClientAuth)
			case "1.3.6.1.5.5.7.3.3":
				c.ExtKeyUsage = append(c.ExtKeyUsage, ExtKeyUsageCodeSigning)
			case "1.3.6.1.5.5.7.3.4":
				c.ExtKeyUsage = append(c.ExtKeyUsage, ExtKeyUsageEmailProtection)
			case "1.3.6.1.5.5.7.3.8":
				c.ExtKeyUsage = append(c.ExtKeyUsage, ExtKeyUsageTimeStamping)
			case "1.3.6.1.5.5.7.3.9":
				c.ExtKeyUsage = append(c.ExtKeyUsage, ExtKeyUsageOCSPSigning)
			default:
				if parsed := parseOID(oid); parsed != nil {
					c.UnknownExtKeyUsage = append(c.UnknownExtKeyUsage, parsed)
				}
			}
		}
	}
}

// parseOID parses a dotted-decimal OID string.
func parseOID(s string) asn1.ObjectIdentifier {
	var oid asn1.ObjectIdentifier
	part := 0
	digits := 0
	for i := 0; i <= len(s); i++ {
		ch := byte('.')
		if i < len(s) {
			ch = s[i]
		}
		if ch == '.' {
			if digits == 0 {
				return nil
			}
			oid = append(oid, part)
			part = 0
			digits = 0
		} else if ch >= '0' && ch <= '9' {
			part = part*10 + int(ch-'0')
			digits++
		} else {
			return nil
		}
	}
	return oid
}

// parseSubjectAltNames decodes the "TYPE:value;..." encoding produced by the
// graalgo.x509.subjectAltNames intrinsic into the Certificate SAN fields.
func parseSubjectAltNames(c *Certificate, san string) {
	if san == "" {
		return
	}
	start := 0
	for i := 0; i <= len(san); i++ {
		if i == len(san) || san[i] == ';' {
			part := san[start:i]
			start = i + 1
			colon := -1
			for j := 0; j < len(part); j++ {
				if part[j] == ':' {
					colon = j
					break
				}
			}
			if colon < 0 {
				continue
			}
			tag, value := part[:colon], part[colon+1:]
			switch tag {
			case "DNS":
				c.DNSNames = append(c.DNSNames, value)
			case "EMAIL":
				c.EmailAddresses = append(c.EmailAddresses, value)
			case "IP":
				if ip := parseIP(value); ip != nil {
					c.IPAddresses = append(c.IPAddresses, ip)
				}
			case "URI":
				c.URIs = append(c.URIs, value)
			}
		}
	}
}

// parseIP parses a dotted-decimal or colon-hex IP string (no zones).
func parseIP(s string) net.IP {
	// IPv4 dotted decimal.
	var v4 [4]byte
	n := 0
	part := 0
	digits := 0
	for i := 0; i <= len(s); i++ {
		ch := byte('.')
		if i < len(s) {
			ch = s[i]
		}
		if ch == '.' {
			if digits == 0 || part > 255 || n >= 4 {
				n = -1
				break
			}
			v4[n] = byte(part)
			n++
			part = 0
			digits = 0
		} else if ch >= '0' && ch <= '9' {
			part = part*10 + int(ch-'0')
			digits++
		} else {
			n = -1
			break
		}
	}
	if n == 4 {
		return net.IP([]byte{v4[0], v4[1], v4[2], v4[3]})
	}
	// IPv6: delegate to net.ParseIP (synthetic net package).
	if ip := net.ParseIP(s); ip != nil {
		return ip
	}
	return nil
}

// splitColon splits a colon-separated descriptor (no empty parts).
func splitColon(s string) []string {
	var parts []string
	start := 0
	for i := 0; i <= len(s); i++ {
		if i == len(s) || s[i] == ':' {
			parts = append(parts, s[start:i])
			start = i + 1
		}
	}
	return parts
}

// parsePrivateKeyDescriptor decodes the graalgo.x509.parsePrivateKey output
// ("FMT:ALG:hex:..." with FMT PKCS8/PKCS1/SEC1, ALG RSA/EC/Ed25519) into a
// crypto.PrivateKey. wantFmt restricts the accepted format ("" = any).
// __mrParsePrivateKeyDescriptorPair is the pair type for parsePrivateKeyDescriptor.
type __mrParsePrivateKeyDescriptorPair struct {
	v0 any
	v1 error
}

func parsePrivateKeyDescriptor(desc, wantFmt string) (any, error) {
	panic("crypto/x509: parsePrivateKeyDescriptor: multi-result desugar")
}

func __mrParsePrivateKeyDescriptor(desc, wantFmt string) __mrParsePrivateKeyDescriptorPair {
	parts := splitColon(desc)
	if len(parts) < 2 {
		return __mrParsePrivateKeyDescriptorPair{nil, errors.New("x509: failed to parse private key")}
	}
	fmtName, alg := parts[0], parts[1]
	if wantFmt != "" && fmtName != wantFmt {
		return __mrParsePrivateKeyDescriptorPair{nil, errors.New("x509: failed to parse private key")}
	}
	rest := parts[1:] // alg + components
	switch alg {
	case "RSA":
		if len(rest) != 6 {
			return __mrParsePrivateKeyDescriptorPair{nil, errors.New("x509: failed to parse private key")}
		}
		nB := __hexToBytes(rest[1])
		if nB == nil {
			return __mrParsePrivateKeyDescriptorPair{nil, errors.New("x509: failed to parse private key")}
		}
		eB := __hexToBytes(rest[2])
		if eB == nil {
			return __mrParsePrivateKeyDescriptorPair{nil, errors.New("x509: failed to parse private key")}
		}
		dB := __hexToBytes(rest[3])
		if dB == nil {
			return __mrParsePrivateKeyDescriptorPair{nil, errors.New("x509: failed to parse private key")}
		}
		pB := __hexToBytes(rest[4])
		if pB == nil {
			return __mrParsePrivateKeyDescriptorPair{nil, errors.New("x509: failed to parse private key")}
		}
		qB := __hexToBytes(rest[5])
		if qB == nil {
			return __mrParsePrivateKeyDescriptorPair{nil, errors.New("x509: failed to parse private key")}
		}
		priv := &rsa.PrivateKey{
			PublicKey: rsa.PublicKey{
				N: new(big.Int).SetBytes(nB),
				E: int(new(big.Int).SetBytes(eB).Int64()),
			},
			D:      new(big.Int).SetBytes(dB),
			Primes: []*big.Int{new(big.Int).SetBytes(pB), new(big.Int).SetBytes(qB)},
		}
		return __mrParsePrivateKeyDescriptorPair{priv, nil}
	case "EC":
		// EC: EC:curveName:d (x,y derived Go-side via ScalarBaseMult);
		// a 5-part EC:curveName:x:y:d form is also accepted.
		if len(rest) != 5 && len(rest) != 3 {
			return __mrParsePrivateKeyDescriptorPair{nil, errors.New("x509: failed to parse private key")}
		}
		curve := curveFromName(rest[1])
		if curve == nil {
			return __mrParsePrivateKeyDescriptorPair{nil, errors.New("x509: failed to parse private key")}
		}
		var dB []byte
		var err error
		if len(rest) == 5 {
			dB = __hexToBytes(rest[4])
			if dB == nil {
				err = errors.New("x509: failed to parse private key")
			} else {
				err = nil
			}
		} else {
			dB = __hexToBytes(rest[2])
			if dB == nil {
				err = errors.New("x509: failed to parse private key")
			} else {
				err = nil
			}
		}
		if err != nil {
			return __mrParsePrivateKeyDescriptorPair{nil, err}
		}
		d := new(big.Int).SetBytes(dB)
		x, y := curve.ScalarBaseMult(dB)
		priv := &ecdsa.PrivateKey{
			PublicKey: ecdsa.PublicKey{Curve: curve, X: x, Y: y},
			D:         d,
		}
		return __mrParsePrivateKeyDescriptorPair{priv, nil}
	case "Ed25519":
		if len(rest) != 2 {
			return __mrParsePrivateKeyDescriptorPair{nil, errors.New("x509: failed to parse private key")}
		}
		seed := __hexToBytes(rest[1])
		if seed == nil {
			return __mrParsePrivateKeyDescriptorPair{nil, errors.New("x509: failed to parse private key")}
		}
		if len(seed) != 32 {
			return __mrParsePrivateKeyDescriptorPair{nil, errors.New("x509: failed to parse private key")}
		}
		return __mrParsePrivateKeyDescriptorPair{ed25519.NewKeyFromSeed(seed), nil}
	}
	return __mrParsePrivateKeyDescriptorPair{nil, errors.New("x509: failed to parse private key")}
}

// curveFromName maps a JDK curve name to the synthetic elliptic.Curve.
// secp256k1 is deliberately absent: gc's x509 rejects it ("unsupported
// elliptic curve"), so an unmapped name fails loudly here too instead of
// being mistaken for secp256r1.
func curveFromName(name string) elliptic.Curve {
	switch name {
	case "secp224r1":
		return elliptic.P224()
	case "secp256r1":
		return elliptic.P256()
	case "secp384r1":
		return elliptic.P384()
	case "secp521r1":
		return elliptic.P521()
	}
	return nil
}

// __mrParsePKCS8PrivateKeyPair is the pair type for ParsePKCS8PrivateKey.
type __mrParsePKCS8PrivateKeyPair struct {
	v0 any
	v1 error
}

func __mrParsePKCS8PrivateKey(der []byte) __mrParsePKCS8PrivateKeyPair {
	desc := graalgoX509ParsePrivateKey(der)
	if desc == "" {
		return __mrParsePKCS8PrivateKeyPair{nil, errors.New("x509: failed to parse private key")}
	}
	key, err := parsePrivateKeyDescriptor(desc, "PKCS8")
	return __mrParsePKCS8PrivateKeyPair{key, err}
}

func ParsePKCS8PrivateKey(der []byte) (any, error) {
	panic("crypto/x509: ParsePKCS8PrivateKey: multi-result desugar")
}

// __mrParsePKCS1PrivateKeyPair is the pair type for ParsePKCS1PrivateKey.
type __mrParsePKCS1PrivateKeyPair struct {
	v0 *rsa.PrivateKey
	v1 error
}

func __mrParsePKCS1PrivateKey(der []byte) __mrParsePKCS1PrivateKeyPair {
	desc := graalgoX509ParsePrivateKey(der)
	if desc == "" {
		return __mrParsePKCS1PrivateKeyPair{nil, errors.New("x509: failed to parse private key")}
	}
	key, err := parsePrivateKeyDescriptor(desc, "PKCS1")
	if err != nil {
		return __mrParsePKCS1PrivateKeyPair{nil, err}
	}
	rsaKey, ok := key.(*rsa.PrivateKey)
	if !ok {
		return __mrParsePKCS1PrivateKeyPair{nil, errors.New("x509: failed to parse private key")}
	}
	return __mrParsePKCS1PrivateKeyPair{rsaKey, nil}
}

func ParsePKCS1PrivateKey(der []byte) (*rsa.PrivateKey, error) {
	panic("crypto/x509: ParsePKCS1PrivateKey: multi-result desugar")
}

// __mrParseECPrivateKeyPair is the pair type for ParseECPrivateKey.
type __mrParseECPrivateKeyPair struct {
	v0 *ecdsa.PrivateKey
	v1 error
}

func __mrParseECPrivateKey(der []byte) __mrParseECPrivateKeyPair {
	desc := graalgoX509ParsePrivateKey(der)
	if desc == "" {
		return __mrParseECPrivateKeyPair{nil, errors.New("x509: failed to parse private key")}
	}
	key, err := parsePrivateKeyDescriptor(desc, "SEC1")
	if err != nil {
		return __mrParseECPrivateKeyPair{nil, err}
	}
	ecKey, ok := key.(*ecdsa.PrivateKey)
	if !ok {
		return __mrParseECPrivateKeyPair{nil, errors.New("x509: failed to parse private key")}
	}
	return __mrParseECPrivateKeyPair{ecKey, nil}
}

func ParseECPrivateKey(der []byte) (*ecdsa.PrivateKey, error) {
	panic("crypto/x509: ParseECPrivateKey: multi-result desugar")
}

// ============================================================================
// P3-M9: Certificate.Verify (chain building + signature checks in Go for
// exact Go semantics; parsing delegates to java.security.cert).
// ============================================================================

// VerifyOptions contains parameters for Certificate.Verify.
type VerifyOptions struct {
	DNSName                  string
	Intermediates            *CertPool
	Roots                    *CertPool
	CurrentTime              time.Time
	KeyUsages                []ExtKeyUsage
	MaxConstraintComparisons int
}

// __mrVerifyPair is the pair type for (*Certificate).Verify.
type __mrVerifyPair struct {
	v0 [][]*Certificate
	v1 error
}

func __mrVerify(c *Certificate, opts VerifyOptions) __mrVerifyPair {
	return c.verify(opts)
}

func (c *Certificate) Verify(opts VerifyOptions) ([][]*Certificate, error) {
	panic("crypto/x509: Verify: multi-result desugar")
}

// VerifyHostname verifies the certificate against host h.
// It performs hostname matching only (no chain building); the previous
// implementation incorrectly routed through the full chain verify() with
// empty roots, which always failed with UnknownAuthorityError.
func (c *Certificate) VerifyHostname(h string) error {
	return c.verifyHostname(h)
}

// verify implements chain building and checking.
func (c *Certificate) verify(opts VerifyOptions) __mrVerifyPair {
	currentTime := opts.CurrentTime
	if currentTime.IsZero() {
		currentTime = time.Now()
	}
	roots := opts.Roots
	if roots == nil {
		roots = NewCertPool()
	}
	intermediates := opts.Intermediates
	if intermediates == nil {
		intermediates = NewCertPool()
	}

	// Collect candidate parents: intermediates + roots.
	var candidates []*Certificate
	for _, cert := range intermediates.certs {
		candidates = append(candidates, cert)
	}
	for _, cert := range roots.certs {
		candidates = append(candidates, cert)
	}

	var chains [][]*Certificate
	// Depth-first search for chains from the leaf to a root.
	var visit func(cert *Certificate, chain []*Certificate)
	visit = func(cert *Certificate, chain []*Certificate) {
		// If cert is in Roots, the chain is complete.
		if roots.containsCert(cert) {
			newChain := make([]*Certificate, len(chain))
			copy(newChain, chain)
			chains = append(chains, newChain)
			return
		}
		if len(chain) > 10 {
			return
		}
		for _, parent := range candidates {
			if !isParentOf(cert, parent) {
				continue
			}
			if err := cert.CheckSignatureFrom(parent); err != nil {
				continue
			}
			if !parent.IsCA && parent != cert {
				continue
			}
			// Validity of the parent at currentTime.
			if currentTime.Before(parent.NotBefore) || currentTime.After(parent.NotAfter) {
				continue
			}
			// Avoid cycles.
			cycle := false
			for _, link := range chain {
				if link == parent {
					cycle = true
					break
				}
			}
			if cycle {
				continue
			}
			visit(parent, append(chain, parent))
		}
	}
	visit(c, []*Certificate{c})

	if len(chains) == 0 {
		return __mrVerifyPair{nil, &UnknownAuthorityError{Cert: c}}
	}

	// Check each chain for validity, hostname, EKU; return the first valid.
	var lastErr error
	for _, chain := range chains {
		if err := c.verifyChain(chain, opts, currentTime); err != nil {
			lastErr = err
			continue
		}
		return __mrVerifyPair{[][]*Certificate{chain}, nil}
	}
	if lastErr != nil {
		return __mrVerifyPair{nil, lastErr}
	}
	return __mrVerifyPair{nil, &UnknownAuthorityError{Cert: c}}
}

// verifyChain checks validity periods, hostname, and key usages for a chain.
func (c *Certificate) verifyChain(chain []*Certificate, opts VerifyOptions, currentTime time.Time) error {
	// Validity of every link.
	for _, cert := range chain {
		if currentTime.Before(cert.NotBefore) || currentTime.After(cert.NotAfter) {
			return &CertificateInvalidError{Cert: cert, Reason: Expired}
		}
	}
	// Hostname.
	if opts.DNSName != "" {
		if err := c.verifyHostname(opts.DNSName); err != nil {
			return err
		}
	}
	// Extended key usage.
	if len(opts.KeyUsages) > 0 {
		for _, ku := range opts.KeyUsages {
			if !c.hasExtKeyUsage(ku) {
				return &CertificateInvalidError{Cert: c, Reason: IncompatibleUsage}
			}
		}
	}
	return nil
}

// isParentOf reports whether parent is a plausible issuer of cert.
func isParentOf(cert, parent *Certificate) bool {
	if len(cert.RawIssuer) > 0 && len(parent.RawSubject) > 0 {
		match := len(cert.RawIssuer) == len(parent.RawSubject)
		if match {
			for i := range cert.RawIssuer {
				if cert.RawIssuer[i] != parent.RawSubject[i] {
					match = false
					break
				}
			}
		}
		if !match {
			return false
		}
	}
	// AuthorityKeyId should match parent's SubjectKeyId (if both present).
	if len(cert.AuthorityKeyId) > 0 && len(parent.SubjectKeyId) > 0 {
		if len(cert.AuthorityKeyId) != len(parent.SubjectKeyId) {
			return false
		}
		for i := range cert.AuthorityKeyId {
			if cert.AuthorityKeyId[i] != parent.SubjectKeyId[i] {
				return false
			}
		}
	}
	return true
}

// containsCert reports whether the pool contains cert (by Raw bytes).
func (p *CertPool) containsCert(cert *Certificate) bool {
	for _, c := range p.certs {
		if len(c.Raw) == len(cert.Raw) {
			match := true
			for i := range c.Raw {
				if c.Raw[i] != cert.Raw[i] {
					match = false
					break
				}
			}
			if match {
				return true
			}
		}
	}
	return false
}

// hasExtKeyUsage reports whether the certificate is valid for the given EKU.
// A certificate with no EKU is valid for all usages (Go semantics).
func (c *Certificate) hasExtKeyUsage(ku ExtKeyUsage) bool {
	if len(c.ExtKeyUsage) == 0 && len(c.UnknownExtKeyUsage) == 0 {
		return true
	}
	for _, eku := range c.ExtKeyUsage {
		if eku == ku {
			return true
		}
	}
	return false
}

// CheckSignatureFrom verifies that parent signed c.
func (c *Certificate) CheckSignatureFrom(parent *Certificate) error {
	if parent == nil {
		return errors.New("x509: nil parent certificate")
	}
	return verifyCertSignature(c, parent)
}

// verifyCertSignature checks c.Signature over c.RawTBSCertificate with
// parent's public key, per c.SignatureAlgorithm.
func verifyCertSignature(c, parent *Certificate) error {
	newHash, err := hashFromSigAlg(c.SignatureAlgorithm)
	if err != nil {
		return err
	}
	var digest []byte
	if newHash != nil {
		h := newHash()
		h.Write(c.RawTBSCertificate)
		digest = h.Sum(nil)
	}
	switch parent.PublicKeyAlgorithm {
	case RSA:
		rsaPub, ok := parent.PublicKey.(*rsa.PublicKey)
		if !ok {
			return errors.New("x509: parent public key is not RSA")
		}
		if newHash == nil {
			return errors.New("x509: unsupported signature algorithm")
		}
		return rsa.VerifyPKCS1v15(rsaPub, hashForAlg(c.SignatureAlgorithm), digest, c.Signature)
	case ECDSA:
		ecdsaPub, ok := parent.PublicKey.(*ecdsa.PublicKey)
		if !ok {
			return errors.New("x509: parent public key is not ECDSA")
		}
		if newHash == nil {
			return errors.New("x509: unsupported signature algorithm")
		}
		if !ecdsa.VerifyASN1(ecdsaPub, digest, c.Signature) {
			return errors.New("x509: ECDSA verification failure")
		}
		return nil
	case Ed25519:
		edPub, ok := parent.PublicKey.(ed25519.PublicKey)
		if !ok {
			return errors.New("x509: parent public key is not Ed25519")
		}
		if !ed25519.Verify(edPub, c.RawTBSCertificate, c.Signature) {
			return errors.New("x509: Ed25519 verification failure")
		}
		return nil
	}
	return errors.New("x509: unsupported public key algorithm")
}

// hashForAlg maps a signature algorithm to the crypto.Hash value the
// synthetic rsa package expects.
func hashForAlg(alg SignatureAlgorithm) crypto.Hash {
	switch alg {
	case SHA256WithRSA, ECDSAWithSHA256, SHA256WithRSAPSS:
		return crypto.SHA256
	case SHA384WithRSA, ECDSAWithSHA384:
		return crypto.SHA384
	case SHA512WithRSA, ECDSAWithSHA512:
		return crypto.SHA512
	case SHA224WithRSA, ECDSAWithSHA224:
		return crypto.SHA224
	case SHA1WithRSA, ECDSAWithSHA1:
		return crypto.SHA1
	}
	return 0
}

// hashFromSigAlg maps a signature algorithm to a new hash (nil = direct sign,
// e.g. Ed25519).
type __mrHashFromSigAlgPair struct {
	v0 func() hash.Hash
	v1 error
}

func hashFromSigAlg(alg SignatureAlgorithm) (func() hash.Hash, error) {
	panic("crypto/x509: hashFromSigAlg: multi-result desugar")
}
func __mrHashFromSigAlg(alg SignatureAlgorithm) __mrHashFromSigAlgPair {
	switch alg {
	case SHA256WithRSA, ECDSAWithSHA256, SHA256WithRSAPSS:
		return __mrHashFromSigAlgPair{sha256.New, nil}
	case SHA384WithRSA, ECDSAWithSHA384:
		return __mrHashFromSigAlgPair{sha512.New384, nil}
	case SHA512WithRSA, ECDSAWithSHA512:
		return __mrHashFromSigAlgPair{sha512.New, nil}
	case SHA224WithRSA, ECDSAWithSHA224:
		return __mrHashFromSigAlgPair{sha256.New224, nil}
	case SHA1WithRSA, ECDSAWithSHA1:
		return __mrHashFromSigAlgPair{sha1.New, nil}
	case MD2WithRSA, MD5WithRSA:
		return __mrHashFromSigAlgPair{nil, errors.New("x509: insecure signature algorithm")}
	case PureEd25519:
		return __mrHashFromSigAlgPair{nil, nil}
	}
	return __mrHashFromSigAlgPair{nil, errors.New("x509: unknown signature algorithm")}
}

// verifyHostname checks opts.DNSName against the leaf's SANs/CN.
func (c *Certificate) verifyHostname(h string) error {
	// Strip trailing dot and port.
	host := h
	for i := len(host) - 1; i >= 0; i-- {
		if host[i] == ':' {
			host = host[:i]
			break
		}
		if host[i] == '.' {
			break
		}
	}
	host = trimSuffix(host, ".")
	if ip := parseIP(host); ip != nil {
		for _, cand := range c.IPAddresses {
			if ipEqual(ip, cand) {
				return nil
			}
		}
		return &HostnameError{Certificate: c, Host: h}
	}
	// DNS: try SANs first, then CN fallback.
	if len(c.DNSNames) > 0 {
		for _, pattern := range c.DNSNames {
			if matchHostname(pattern, host) {
				return nil
			}
		}
		return &HostnameError{Certificate: c, Host: h}
	}
	if cn := c.Subject.CommonName; cn != "" {
		if matchHostname(cn, host) {
			return nil
		}
	}
	return &HostnameError{Certificate: c, Host: h}
}

// matchHostname implements Go's wildcard matching (RFC 6125 §6.4.3).
func matchHostname(pattern, host string) bool {
	pattern = toLower(pattern)
	host = toLower(host)
	if pattern == "" || host == "" {
		return false
	}
	// Wildcard: only a leading "*." is valid, matching exactly one label.
	if len(pattern) > 2 && pattern[0] == '*' && pattern[1] == '.' {
		rest := pattern[2:]
		if !validHostnameLabel(rest) {
			return false
		}
		// Host must have exactly one more label than the pattern suffix.
		if len(host) <= len(rest) {
			return false
		}
		if host[len(host)-len(rest):] != rest {
			return false
		}
		prefix := host[:len(host)-len(rest)-1]
		// Prefix must be a single label (no dots).
		for i := 0; i < len(prefix); i++ {
			if prefix[i] == '.' {
				return false
			}
		}
		return len(prefix) > 0
	}
	return pattern == host
}

func validHostnameLabel(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		ch := s[i]
		if !(ch >= 'a' && ch <= 'z' || ch >= '0' && ch <= '9' || ch == '-' || ch == '.') {
			return false
		}
	}
	return true
}

func toLower(s string) string {
	out := make([]byte, len(s))
	for i := 0; i < len(s); i++ {
		ch := s[i]
		if ch >= 'A' && ch <= 'Z' {
			ch += 'a' - 'A'
		}
		out[i] = ch
	}
	return string(out)
}

func trimSuffix(s, suffix string) string {
	if len(s) >= len(suffix) && s[len(s)-len(suffix):] == suffix {
		return s[:len(s)-len(suffix)]
	}
	return s
}

func ipEqual(a, b net.IP) bool {
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

// UnknownAuthorityError is returned when the chain does not anchor in Roots.
type UnknownAuthorityError struct {
	Cert *Certificate
}

func (e *UnknownAuthorityError) Error() string {
	return "x509: certificate signed by unknown authority"
}

// InvalidReason describes a CertificateInvalidError.
type InvalidReason int

const (
	NotAuthorizedToSign InvalidReason = iota
	Expired
	CANotAuthorizedForThisName
	TooManyIntermediates
	IncompatibleUsage
	NameMismatch
	NameConstraintsWithoutSANs
	UnconstrainedName
	TooManyConstraints
	CANotAuthorizedForExtKeyUsage
)

// CertificateInvalidError is returned for invalid chains.
type CertificateInvalidError struct {
	Cert   *Certificate
	Reason InvalidReason
}

func (e *CertificateInvalidError) Error() string {
	var reason string
	switch e.Reason {
	case NotAuthorizedToSign:
		reason = "certificate is not standards compliant"
	case Expired:
		reason = "certificate has expired or is not yet valid"
	case CANotAuthorizedForThisName:
		reason = "certificate is not authorized for this name"
	case TooManyIntermediates:
		reason = "too many intermediates"
	case IncompatibleUsage:
		reason = "certificate is not valid for the requested usage"
	default:
		reason = "certificate is invalid"
	}
	name := ""
	if e.Cert != nil {
		name = e.Cert.Subject.CommonName
		if name == "" && len(e.Cert.DNSNames) > 0 {
			name = e.Cert.DNSNames[0]
		}
	}
	if name != "" {
		return "x509: " + reason + ": \"" + name + "\""
	}
	return "x509: " + reason
}

// HostnameError is returned on hostname mismatch.
type HostnameError struct {
	Certificate *Certificate
	Host        string
}

func (e *HostnameError) Error() string {
	c := e.Certificate
	var valid string
	if len(c.DNSNames) > 0 {
		valid = c.DNSNames[0]
		for _, n := range c.DNSNames[1:] {
			valid += ", " + n
		}
	} else if c.Subject.CommonName != "" {
		valid = c.Subject.CommonName
	}
	if valid != "" {
		return "x509: certificate is valid for " + valid + ", not " + e.Host
	}
	return "x509: certificate is not valid for " + e.Host
}
`
