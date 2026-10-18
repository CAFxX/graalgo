// Regression tests for the 2026-10-18 P0 remediation of the synthetic
// crypto/x509 and crypto/tls packages (independent codebase review):
//
//   - P0-2: TLS verifyPeer dropped the peer's extra certificates instead of
//     handing them to x509 as Intermediates, so chains longer than
//     leaf→root failed where gc succeeds.
//   - P0-3: x509.Verify used an empty pool when VerifyOptions.Roots was nil
//     instead of the system roots, so default-config clients always failed;
//     SystemCertPool did not exist at all.
//   - P0-4: x509.Verify omitted name-constraints, BasicConstraints
//     path-length and nested EKU enforcement — fail-open silent acceptance
//     divergences from gc, security-critical.
//
// TestSyntheticTLSVerifyPeerPopulatesIntermediates is a source-level gate
// (same technique as the P0-1 sleep gate): verifyPeer is driven by the JVM
// TLS-engine intrinsics, so a behavioral test needs the full backend; the
// gate pins the exact property the fix establishes (peerDERs[1:] flow into
// VerifyOptions.Intermediates).
//
// TestSyntheticX509VerifyDifferential extracts the synthetic x509 source
// verbatim (syntheticCryptoX509Source), rewrites the graalgo.x509.*
// linkname intrinsics to stub implementations backed by gc's own parser,
// and differentially compares Verify against gc's Verify over a matrix of
// generated chains covering P0-2/3/4. A behavioral divergence fails the
// test. The tests are not tautological: every negative case rejects on the
// pre-fix code (the old code paths are noted in the case comments), and
// every positive control accepts on both.
package main

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"math/big"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// P0-2: source-level gate on the synthetic crypto/tls verifyPeer.
// ---------------------------------------------------------------------------

// TestSyntheticTLSVerifyPeerPopulatesIntermediates pins that the synthetic
// crypto/tls verifyPeer hands the peer's extra certificates (peerDERs[1:])
// to x509 as VerifyOptions.Intermediates, exactly like gc's crypto/tls
// handshake. Before the fix they were silently dropped.
func TestSyntheticTLSVerifyPeerPopulatesIntermediates(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("synth_crypto_tls.go"))
	if err != nil {
		t.Fatalf("ReadFile synth_crypto_tls.go: %v", err)
	}
	body := extractFuncBody(t, string(raw), "func (c *Conn) verifyPeer() error {")
	if !strings.Contains(body, "peerDERs[1:]") {
		t.Errorf("verifyPeer does not consume peerDERs[1:]: the peer's extra certificates are not used as intermediates")
	}
	if !strings.Contains(body, "Intermediates") {
		t.Errorf("verifyPeer does not populate VerifyOptions.Intermediates")
	}
	if !strings.Contains(body, "x509.NewCertPool()") {
		t.Errorf("verifyPeer does not build an intermediates pool")
	}
}

// extractFuncBody returns the body of the function whose declaration line is
// decl (the line including the opening brace), by brace matching.
func extractFuncBody(t *testing.T, src, decl string) string {
	t.Helper()
	start := strings.Index(src, decl)
	if start < 0 {
		t.Fatalf("declaration %q not found", decl)
	}
	pos := start + len(decl) - 1 // at the opening '{'
	depth := 0
	for i := pos; i < len(src); i++ {
		switch src[i] {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return src[pos : i+1]
			}
		}
	}
	t.Fatalf("unbalanced braces in %q", decl)
	return ""
}

// ---------------------------------------------------------------------------
// P0-3/P0-4: differential harness for the synthetic crypto/x509 Verify.
// ---------------------------------------------------------------------------

// x509LinknameRe matches the bodyless linkname declarations in the synthetic
// x509 source:
//
//	//go:linkname graalgoX509Parse graalgo.x509.parse
//	func graalgoX509Parse(der []byte) any
var x509LinknameRe = regexp.MustCompile(`(?m)^//go:linkname (graalgoX509\w+) graalgo\.x509\.\S+\nfunc (\w+)\((\w+) ([^)]*)\)( [^\n]*)?$`)

// extractSyntheticX509 returns the synthetic x509 source with every
// graalgo.x509.* linkname declaration rewritten to a stub call, so the
// package compiles under plain go for differential testing. It also
// replicates the frontend's multi-result desugar for the one desugared
// stub on the Verify path (hashFromSigAlg -> __mrHashFromSigAlg;
// see the crypto/x509 entries in mrdesugar_m5.go): under the frontend the
// call site is rewritten, under plain go the panic stub would fire.
func extractSyntheticX509(t *testing.T) string {
	t.Helper()
	out := x509LinknameRe.ReplaceAllString(syntheticCryptoX509Source,
		`func $2($3 $4)$5 { return stub$1($3) }`)
	if strings.Contains(out, "//go:linkname") {
		t.Fatal("preprocessing left //go:linkname directives unreplaced: extend the stubs")
	}
	out = strings.Replace(out,
		"func hashFromSigAlg(alg SignatureAlgorithm) (func() hash.Hash, error) {\n\tpanic(\"crypto/x509: hashFromSigAlg: multi-result desugar\")\n}",
		"func hashFromSigAlg(alg SignatureAlgorithm) (func() hash.Hash, error) {\n\t__tmp := __mrHashFromSigAlg(alg)\n\treturn __tmp.v0, __tmp.v1\n}",
		1)
	return out
}

func writeHarnessFile(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0644); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
}

func goToolchain(t *testing.T) string {
	t.Helper()
	if p := os.Getenv("GO"); p != "" {
		return p
	}
	if home, err := os.UserHomeDir(); err == nil {
		if p := filepath.Join(home, "tools", "go", "bin", "go"); fileExists(p) {
			return p
		}
	}
	return "go"
}

func fileExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

// makeHarnessSystemRoot generates a throwaway root CA for the stub system
// roots and returns the paths of its PEM-encoded certificate and PKCS#8 key.
func makeHarnessSystemRoot(t *testing.T) (certPEM, keyPEM string) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(424242),
		Subject:               pkix.Name{CommonName: "harness-test-system-root"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(24 * time.Hour),
		IsCA:                  true,
		BasicConstraintsValid: true,
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	keyDER, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	certPEM = filepath.Join(dir, "sysroot-cert.pem")
	keyPEM = filepath.Join(dir, "sysroot-key.pem")
	if err := os.WriteFile(certPEM, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(keyPEM, pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER}), 0644); err != nil {
		t.Fatal(err)
	}
	return certPEM, keyPEM
}

// TestSyntheticX509VerifyDifferential builds the synthetic x509 source
// (with stubbed intrinsics) as a plain-go test module and runs the
// differential matrix against gc: once with stub system roots configured
// and once without (the latter exercises the SystemRootsError path).
func TestSyntheticX509VerifyDifferential(t *testing.T) {
	dir := t.TempDir()
	writeHarnessFile(t, dir, "go.mod", "module x509harness\n\ngo 1.24\n")
	writeHarnessFile(t, dir, "x509.go", extractSyntheticX509(t))
	writeHarnessFile(t, dir, "stubs.go", syntheticX509StubsSource)
	writeHarnessFile(t, dir, "verify_test.go", syntheticX509DriverSource)

	goBin := goToolchain(t)
	runGo := func(t *testing.T, extraEnv []string, args ...string) {
		t.Helper()
		if len(args) > 0 && args[0] == "test" {
			if r := os.Getenv("HARNESS_RUN"); r != "" {
				args = append(args, "-run", r)
			}
			if os.Getenv("HARNESS_V") != "" {
				args = append(args, "-v")
			}
		}
		cmd := exec.Command(goBin, args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(),
			"GOTOOLCHAIN=local", "GOFLAGS=-mod=mod", "GOPROXY=off", "GOSUMDB=off")
		cmd.Env = append(cmd.Env, extraEnv...)
		out, err := cmd.CombinedOutput()
		if os.Getenv("HARNESS_V") != "" {
			t.Logf("inner go %v output:\n%s", args, out)
		}
		if err != nil {
			t.Fatalf("go %v failed: %v\n%s", args, err, out)
		}
	}

	// Vet the extracted package first: catches type errors in the
	// preprocessing fast, before the slower test run.
	runGo(t, nil, "vet", "./...")

	certPEM, keyPEM := makeHarnessSystemRoot(t)
	withRoots := []string{
		"HARNESS_SYSTEM_ROOTS_PEM=" + certPEM,
		"HARNESS_SYSTEM_ROOTS_KEY=" + keyPEM,
	}
	// Run 1: stub system roots present (TestSystemRootsDefault active).
	runGo(t, withRoots, "test", "-count=1", "-timeout", "5m", "./...")
	// Run 2: no stub system roots (TestSystemRootsUnavailable active,
	// TestSystemRootsDefault skips).
	runGo(t, nil, "test", "-count=1", "-timeout", "5m", "./...")

	fmt.Printf("synthetic x509 differential harness green (with and without stub system roots)\n")
}

// syntheticX509StubsSource implements the graalgo.x509.* linkname
// intrinsics for the differential harness. Parsing semantics come from gc's
// crypto/x509; the wire encodings mirror the Java backend (GoX509.java) so
// the synthetic Go code under test cannot tell the difference.
const syntheticX509StubsSource = `package x509

import (
	realx509 "crypto/x509"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/rsa"
	"encoding/hex"
	"encoding/pem"
	"net"
	"os"
	"strings"
)

func jcert(cert any) *realx509.Certificate {
	return cert.(*realx509.Certificate)
}

func stubgraalgoX509Parse(der []byte) any {
	c, err := realx509.ParseCertificate(der)
	if err != nil {
		return nil
	}
	return c
}

func stubgraalgoX509SerialNumber(cert any) []byte { return jcert(cert).SerialNumber.Bytes() }
func stubgraalgoX509Subject(cert any) string      { return jcert(cert).Subject.String() }
func stubgraalgoX509Issuer(cert any) string       { return jcert(cert).Issuer.String() }
func stubgraalgoX509NotBefore(cert any) int64     { return jcert(cert).NotBefore.UnixMilli() }
func stubgraalgoX509NotAfter(cert any) int64      { return jcert(cert).NotAfter.UnixMilli() }
func stubgraalgoX509Version(cert any) int64       { return int64(jcert(cert).Version) }
func stubgraalgoX509SigAlgOid(cert any) string    { return stubSigAlgOID(jcert(cert).SignatureAlgorithm) }
func stubgraalgoX509Signature(cert any) []byte    { return jcert(cert).Signature }
func stubgraalgoX509TBSCertificate(cert any) []byte {
	return jcert(cert).RawTBSCertificate
}
func stubgraalgoX509KeyUsage(cert any) int64 { return int64(jcert(cert).KeyUsage) }

// Mirrors Java X509Certificate.getBasicConstraints(): -1 when not a CA,
// Integer.MAX_VALUE when a CA without a path-length constraint.
func stubgraalgoX509BasicConstraints(cert any) int64 {
	c := jcert(cert)
	if !c.IsCA {
		return -1
	}
	// CA with no path-length limit: gc parses an absent pathLenConstraint
	// as MaxPathLen=-1 (MaxPathLenZero=false); mirror the Java intrinsic's
	// Integer.MAX_VALUE sentinel rather than the -1 "not a CA" sentinel.
	if c.MaxPathLen < 0 || (c.MaxPathLen == 0 && !c.MaxPathLenZero) {
		return 2147483647
	}
	return int64(c.MaxPathLen)
}

func stubgraalgoX509SubjectKeyId(cert any) []byte   { return jcert(cert).SubjectKeyId }
func stubgraalgoX509AuthorityKeyId(cert any) []byte { return jcert(cert).AuthorityKeyId }

func stubgraalgoX509PublicKeyAlg(cert any) string {
	switch jcert(cert).PublicKey.(type) {
	case *rsa.PublicKey:
		return "RSA"
	case *ecdsa.PublicKey:
		return "EC"
	case ed25519.PublicKey:
		return "Ed25519"
	}
	return "UNKNOWN"
}

func stubgraalgoX509PublicKeyEncoded(cert any) []byte {
	der, err := realx509.MarshalPKIXPublicKey(jcert(cert).PublicKey)
	if err != nil {
		return nil
	}
	return der
}

func stubgraalgoX509RawIssuer(cert any) []byte  { return jcert(cert).RawIssuer }
func stubgraalgoX509RawSubject(cert any) []byte { return jcert(cert).RawSubject }

func stubgraalgoX509RsaModulus(cert any) []byte {
	if k, ok := jcert(cert).PublicKey.(*rsa.PublicKey); ok {
		return k.N.Bytes()
	}
	return nil
}

func stubgraalgoX509RsaExponent(cert any) int64 {
	if k, ok := jcert(cert).PublicKey.(*rsa.PublicKey); ok {
		return int64(k.E)
	}
	return -1
}

func stubgraalgoX509EcX(cert any) []byte {
	if k, ok := jcert(cert).PublicKey.(*ecdsa.PublicKey); ok {
		return k.X.Bytes()
	}
	return nil
}

func stubgraalgoX509EcY(cert any) []byte {
	if k, ok := jcert(cert).PublicKey.(*ecdsa.PublicKey); ok {
		return k.Y.Bytes()
	}
	return nil
}

func stubgraalgoX509EcFieldSize(cert any) int64 {
	if k, ok := jcert(cert).PublicKey.(*ecdsa.PublicKey); ok {
		return int64(k.Curve.Params().BitSize)
	}
	return -1
}

func stubgraalgoX509Ed25519Raw(cert any) []byte {
	if k, ok := jcert(cert).PublicKey.(ed25519.PublicKey); ok {
		return []byte(k)
	}
	return nil
}

// Not exercised by the Verify matrix; present so the package links.
func stubgraalgoX509ParseSpki(spki []byte) string { return "" }

// Mirrors the Java subjectAltNames encoding: "TYPE:value;..." with TYPE in
// DNS, IP, EMAIL, URI.
func stubgraalgoX509SubjectAltNames(cert any) string {
	c := jcert(cert)
	var parts []string
	for _, d := range c.DNSNames {
		parts = append(parts, "DNS:"+d)
	}
	for _, ip := range c.IPAddresses {
		parts = append(parts, "IP:"+ip.String())
	}
	for _, e := range c.EmailAddresses {
		parts = append(parts, "EMAIL:"+e)
	}
	for _, u := range c.URIs {
		parts = append(parts, "URI:"+u.String())
	}
	return strings.Join(parts, ";")
}

// Not exercised by the Verify matrix; present so the package links.
func stubgraalgoX509ParsePrivateKey(der []byte) string { return "" }

var stubEKUOIDs = map[realx509.ExtKeyUsage]string{
	realx509.ExtKeyUsageAny:                            "2.5.29.37.0",
	realx509.ExtKeyUsageServerAuth:                     "1.3.6.1.5.5.7.3.1",
	realx509.ExtKeyUsageClientAuth:                     "1.3.6.1.5.5.7.3.2",
	realx509.ExtKeyUsageCodeSigning:                    "1.3.6.1.5.5.7.3.3",
	realx509.ExtKeyUsageEmailProtection:                "1.3.6.1.5.5.7.3.4",
	realx509.ExtKeyUsageIPSECEndSystem:                 "1.3.6.1.5.5.7.3.5",
	realx509.ExtKeyUsageIPSECTunnel:                    "1.3.6.1.5.5.7.3.6",
	realx509.ExtKeyUsageIPSECUser:                      "1.3.6.1.5.5.7.3.7",
	realx509.ExtKeyUsageTimeStamping:                   "1.3.6.1.5.5.7.3.8",
	realx509.ExtKeyUsageOCSPSigning:                    "1.3.6.1.5.5.7.3.9",
	realx509.ExtKeyUsageMicrosoftServerGatedCrypto:     "1.3.6.1.4.1.311.10.3.3",
	realx509.ExtKeyUsageNetscapeServerGatedCrypto:      "2.16.840.1.113730.4.1",
	realx509.ExtKeyUsageMicrosoftCommercialCodeSigning: "1.3.6.1.4.1.311.2.1.22",
	realx509.ExtKeyUsageMicrosoftKernelCodeSigning:     "1.3.6.1.4.1.311.61.1.1",
}

// Mirrors the Java extKeyUsage encoding: comma-separated OIDs.
func stubgraalgoX509ExtKeyUsage(cert any) string {
	c := jcert(cert)
	var oids []string
	for _, e := range c.ExtKeyUsage {
		oids = append(oids, stubEKUOIDs[e])
	}
	for _, o := range c.UnknownExtKeyUsage {
		oids = append(oids, o.String())
	}
	return strings.Join(oids, ",")
}

// Not exercised by the Verify matrix; present so the package links.
func stubgraalgoX509ParsePkcs1(der []byte) string { return "" }

// stubSystemRootsDER holds the test system roots (DER), injected via the
// HARNESS_SYSTEM_ROOTS_PEM environment variable by the outer test.
var stubSystemRootsDER [][]byte

func init() {
	if p := os.Getenv("HARNESS_SYSTEM_ROOTS_PEM"); p != "" {
		data, err := os.ReadFile(p)
		if err == nil {
			for len(data) > 0 {
				var block *pem.Block
				block, data = pem.Decode(data)
				if block == nil {
					break
				}
				if block.Type == "CERTIFICATE" {
					stubSystemRootsDER = append(stubSystemRootsDER, block.Bytes)
				}
			}
		}
	}
}

// Mirrors the Java systemRoots encoding: 4-byte big-endian length prefixes.
func stubgraalgoX509SystemRoots(ignored any) []byte {
	var out []byte
	for _, der := range stubSystemRootsDER {
		n := len(der)
		out = append(out, byte(n>>24), byte(n>>16), byte(n>>8), byte(n))
		out = append(out, der...)
	}
	return out
}

// Mirrors the Java nameConstraints encoding: "P:TYPE:value" / "X:TYPE:value"
// entries joined by ';', TYPE in DNS, IPH (hexaddr:hexmask), EMAIL, URI,
// with a leading "CRIT:1" when the extension is critical.
func stubgraalgoX509NameConstraints(cert any) string {
	c := jcert(cert)
	var parts []string
	if c.PermittedDNSDomainsCritical {
		parts = append(parts, "CRIT:1")
	}
	add := func(permitted bool, typ, v string) {
		px := "P:"
		if !permitted {
			px = "X:"
		}
		parts = append(parts, px+typ+":"+v)
	}
	for _, d := range c.PermittedDNSDomains {
		add(true, "DNS", d)
	}
	for _, d := range c.ExcludedDNSDomains {
		add(false, "DNS", d)
	}
	for _, n := range c.PermittedIPRanges {
		add(true, "IPH", stubHexIPMask(n))
	}
	for _, n := range c.ExcludedIPRanges {
		add(false, "IPH", stubHexIPMask(n))
	}
	for _, e := range c.PermittedEmailAddresses {
		add(true, "EMAIL", e)
	}
	for _, e := range c.ExcludedEmailAddresses {
		add(false, "EMAIL", e)
	}
	for _, u := range c.PermittedURIDomains {
		add(true, "URI", u)
	}
	for _, u := range c.ExcludedURIDomains {
		add(false, "URI", u)
	}
	return strings.Join(parts, ";")
}

func stubHexIPMask(n *net.IPNet) string {
	addr := n.IP
	if ip4 := addr.To4(); ip4 != nil {
		addr = ip4
	} else {
		addr = addr.To16()
	}
	mask := n.Mask
	if len(mask) > len(addr) {
		mask = mask[:len(addr)]
	}
	return hex.EncodeToString(addr) + ":" + hex.EncodeToString(mask)
}

func stubSigAlgOID(a realx509.SignatureAlgorithm) string {
	switch a {
	case realx509.SHA256WithRSA:
		return "1.2.840.113549.1.1.11"
	case realx509.SHA384WithRSA:
		return "1.2.840.113549.1.1.12"
	case realx509.SHA512WithRSA:
		return "1.2.840.113549.1.1.13"
	case realx509.ECDSAWithSHA256:
		return "1.2.840.10045.4.3.2"
	case realx509.ECDSAWithSHA384:
		return "1.2.840.10045.4.3.3"
	case realx509.ECDSAWithSHA512:
		return "1.2.840.10045.4.3.4"
	}
	return ""
}
`

// syntheticX509DriverSource is the differential test driver compiled into
// the harness module alongside the extracted synthetic x509 source and the
// stubs. It builds certificate chains with gc's crypto/x509 and compares
// the synthetic __mrVerify against gc's Verify case by case.
const syntheticX509DriverSource = `package x509

import (
	realx509 "crypto/x509"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"math/big"
	"net"
	"net/url"
	"os"
	"testing"
	"time"
)

var driverSerial int64 = 1000

func driverNextSerial() *big.Int {
	driverSerial++
	return big.NewInt(driverSerial)
}

func driverKey(t *testing.T) *ecdsa.PrivateKey {
	t.Helper()
	k, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	return k
}

type driverCA struct {
	der  []byte
	cert *realx509.Certificate
	key  *ecdsa.PrivateKey
}

func driverBaseTemplate(cn string) *realx509.Certificate {
	return &realx509.Certificate{
		SerialNumber:          driverNextSerial(),
		Subject:               pkix.Name{CommonName: cn},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(24 * time.Hour),
		BasicConstraintsValid: true,
	}
}

// newDriverCA creates a self-signed CA. pathLen < 0 means the path-length
// constraint is absent.
func newDriverCA(t *testing.T, cn string, pathLen int, extra func(*realx509.Certificate)) *driverCA {
	t.Helper()
	key := driverKey(t)
	tmpl := driverBaseTemplate(cn)
	tmpl.IsCA = true
	tmpl.KeyUsage = realx509.KeyUsageCertSign | realx509.KeyUsageDigitalSignature
	if pathLen >= 0 {
		tmpl.MaxPathLen = pathLen
		tmpl.MaxPathLenZero = pathLen == 0
	}
	if extra != nil {
		extra(tmpl)
	}
	der, err := realx509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	cert, err := realx509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	return &driverCA{der, cert, key}
}

// signCA issues a subordinate CA certificate signed by ca.
func (ca *driverCA) signCA(t *testing.T, cn string, pathLen int, extra func(*realx509.Certificate)) *driverCA {
	t.Helper()
	key := driverKey(t)
	tmpl := driverBaseTemplate(cn)
	tmpl.IsCA = true
	tmpl.KeyUsage = realx509.KeyUsageCertSign | realx509.KeyUsageDigitalSignature
	if pathLen >= 0 {
		tmpl.MaxPathLen = pathLen
		tmpl.MaxPathLenZero = pathLen == 0
	}
	if extra != nil {
		extra(tmpl)
	}
	der, err := realx509.CreateCertificate(rand.Reader, tmpl, ca.cert, &key.PublicKey, ca.key)
	if err != nil {
		t.Fatal(err)
	}
	cert, err := realx509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	return &driverCA{der, cert, key}
}

// signLeaf issues an end-entity certificate signed by ca.
func (ca *driverCA) signLeaf(t *testing.T, cn string, extra func(*realx509.Certificate)) []byte {
	t.Helper()
	key := driverKey(t)
	tmpl := driverBaseTemplate(cn)
	tmpl.KeyUsage = realx509.KeyUsageDigitalSignature
	if extra != nil {
		extra(tmpl)
	}
	der, err := realx509.CreateCertificate(rand.Reader, tmpl, ca.cert, &key.PublicKey, ca.key)
	if err != nil {
		t.Fatal(err)
	}
	return der
}

// signNonCA issues a non-CA certificate that can still sign (for the
// not-authorized-to-sign case).
func (ca *driverCA) signNonCA(t *testing.T, cn string) *driverCA {
	t.Helper()
	key := driverKey(t)
	tmpl := driverBaseTemplate(cn)
	tmpl.KeyUsage = realx509.KeyUsageDigitalSignature
	der, err := realx509.CreateCertificate(rand.Reader, tmpl, ca.cert, &key.PublicKey, ca.key)
	if err != nil {
		t.Fatal(err)
	}
	cert, err := realx509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	return &driverCA{der, cert, key}
}

func toRealKeyUsages(kus []ExtKeyUsage) []realx509.ExtKeyUsage {
	var out []realx509.ExtKeyUsage
	for _, ku := range kus {
		out = append(out, realx509.ExtKeyUsage(ku))
	}
	return out
}

// invalidReasonName maps InvalidReason values to names. gc and the synthetic
// package use the same numeric order.
func invalidReasonName(r int) string {
	names := []string{"not-authorized-to-sign", "expired", "not-authorized-for-name",
		"too-many-intermediates", "incompatible-usage", "name-mismatch",
		"name-constraints-without-san", "unconstrained-name", "too-many-constraints",
		"not-authorized-for-eku"}
	if r >= 0 && r < len(names) {
		return names[r]
	}
	return fmt.Sprintf("reason-%d", r)
}

// errKind classifies a Verify error for differential comparison.
func errKind(err error, fromGC bool) string {
	if fromGC {
		// gc returns these error types as VALUES (e.g.
		// "return nil, CertificateInvalidError{...}"); accept pointers too.
		switch e := err.(type) {
		case realx509.UnknownAuthorityError, *realx509.UnknownAuthorityError:
			return "unknown-authority"
		case realx509.CertificateInvalidError:
			return "invalid:" + invalidReasonName(int(e.Reason))
		case *realx509.CertificateInvalidError:
			return "invalid:" + invalidReasonName(int(e.Reason))
		case realx509.HostnameError, *realx509.HostnameError:
			return "hostname"
		case realx509.SystemRootsError, *realx509.SystemRootsError:
			return "system-roots"
		}
	} else {
		switch e := err.(type) {
		case *UnknownAuthorityError:
			return "unknown-authority"
		case *CertificateInvalidError:
			return "invalid:" + invalidReasonName(int(e.Reason))
		case *HostnameError:
			return "hostname"
		case *SystemRootsError:
			return "system-roots"
		}
	}
	return fmt.Sprintf("other:%T", err)
}

type verifyCase struct {
	name          string
	leafDER       []byte
	roots         [][]byte
	intermediates [][]byte
	dnsName       string
	keyUsages     []ExtKeyUsage
	currentTime   time.Time
}

func runVerifyCase(t *testing.T, vc verifyCase) {
	t.Helper()

	rleaf, err := realx509.ParseCertificate(vc.leafDER)
	if err != nil {
		t.Fatalf("%s: gc parse leaf: %v", vc.name, err)
	}
	rroots := realx509.NewCertPool()
	for _, d := range vc.roots {
		c, err := realx509.ParseCertificate(d)
		if err != nil {
			t.Fatalf("%s: gc parse root: %v", vc.name, err)
		}
		rroots.AddCert(c)
	}
	rinters := realx509.NewCertPool()
	for _, d := range vc.intermediates {
		c, err := realx509.ParseCertificate(d)
		if err != nil {
			t.Fatalf("%s: gc parse intermediate: %v", vc.name, err)
		}
		rinters.AddCert(c)
	}
	_, rerr := rleaf.Verify(realx509.VerifyOptions{
		Roots:         rroots,
		Intermediates: rinters,
		DNSName:       vc.dnsName,
		KeyUsages:     toRealKeyUsages(vc.keyUsages),
		CurrentTime:   vc.currentTime,
	})

	sp := __mrParseCertificate(vc.leafDER)
	if sp.v1 != nil {
		t.Fatalf("%s: synth parse leaf: %v", vc.name, sp.v1)
	}
	var sroots *CertPool
	sroots = NewCertPool()
	for _, d := range vc.roots {
		c := __mrParseCertificate(d)
		if c.v1 != nil {
			t.Fatalf("%s: synth parse root: %v", vc.name, c.v1)
		}
		sroots.AddCert(c.v0)
	}
	sinters := NewCertPool()
	for _, d := range vc.intermediates {
		c := __mrParseCertificate(d)
		if c.v1 != nil {
			t.Fatalf("%s: synth parse intermediate: %v", vc.name, c.v1)
		}
		sinters.AddCert(c.v0)
	}
	got := __mrVerify(sp.v0, VerifyOptions{
		Roots:         sroots,
		Intermediates: sinters,
		DNSName:       vc.dnsName,
		KeyUsages:     vc.keyUsages,
		CurrentTime:   vc.currentTime,
	})

	wantOK := rerr == nil
	gotOK := got.v1 == nil
	if wantOK != gotOK {
		t.Errorf("%s: MISMATCH gc ok=%v err=%v | synth ok=%v err=%v",
			vc.name, wantOK, rerr, gotOK, got.v1)
		return
	}
	if !wantOK && errKind(rerr, true) != errKind(got.v1, false) {
		t.Errorf("%s: error-kind MISMATCH gc=%q (%v) synth=%q (%v)",
			vc.name, errKind(rerr, true), rerr, errKind(got.v1, false), got.v1)
	}
}

func mustURL(t *testing.T, s string) *url.URL {
	t.Helper()
	u, err := url.Parse(s)
	if err != nil {
		t.Fatal(err)
	}
	return u
}

func mustCIDR(t *testing.T, s string) *net.IPNet {
	t.Helper()
	_, n, err := net.ParseCIDR(s)
	if err != nil {
		t.Fatal(err)
	}
	return n
}

func TestVerifyDifferential(t *testing.T) {
	root := newDriverCA(t, "test-root", -1, nil)
	interCA := root.signCA(t, "test-inter", -1, nil)

	dnsLeaf := func(ca *driverCA, dns string) []byte {
		return ca.signLeaf(t, "leaf", func(c *realx509.Certificate) {
			c.DNSNames = []string{dns}
		})
	}

	// P0-2: chain with intermediates supplied verifies.
	runVerifyCase(t, verifyCase{name: "p0-2/intermediates-ok",
		leafDER: dnsLeaf(interCA, "www.example.com"),
		roots: [][]byte{root.der}, intermediates: [][]byte{interCA.der},
		dnsName: "www.example.com"})
	// P0-2 negative: without intermediates the chain cannot be built.
	// (Pre-fix the intermediates were dropped even when supplied, so the
	// positive case above failed.)
	runVerifyCase(t, verifyCase{name: "p0-2/intermediates-missing",
		leafDER: dnsLeaf(interCA, "www.example.com"),
		roots:   [][]byte{root.der}, dnsName: "www.example.com"})

	// P0-4 path length: interA(pl=1) -> interB -> leaf verifies ...
	iA1 := root.signCA(t, "interA-pl1", 1, nil)
	iB := iA1.signCA(t, "interB", -1, nil)
	runVerifyCase(t, verifyCase{name: "p0-4/pathlen-ok-1",
		leafDER: iB.signLeaf(t, "leaf", nil),
		roots:   [][]byte{root.der}, intermediates: [][]byte{iA1.der, iB.der}})
	// ... while interA(pl=0) -> interB -> leaf is rejected (one
	// intermediate too many below interA). Pre-fix: silently accepted.
	iA0 := root.signCA(t, "interA-pl0", 0, nil)
	iB0 := iA0.signCA(t, "interB0", -1, nil)
	runVerifyCase(t, verifyCase{name: "p0-4/pathlen-violated-0",
		leafDER: iB0.signLeaf(t, "leaf", nil),
		roots:   [][]byte{root.der}, intermediates: [][]byte{iA0.der, iB0.der}})
	// Absent pathLen means no limit.
	iAn := root.signCA(t, "interA-nolimit", -1, nil)
	iBn := iAn.signCA(t, "interBn", -1, nil)
	runVerifyCase(t, verifyCase{name: "p0-4/pathlen-absent-ok",
		leafDER: iBn.signLeaf(t, "leaf", nil),
		roots:   [][]byte{root.der}, intermediates: [][]byte{iAn.der, iBn.der}})
	// A root with pathLen 0 can sign a leaf directly (nothing below it) ...
	rootPL0 := newDriverCA(t, "root-pl0", 0, nil)
	runVerifyCase(t, verifyCase{name: "p0-4/pathlen-root0-direct-ok",
		leafDER: rootPL0.signLeaf(t, "leaf", nil),
		roots:   [][]byte{rootPL0.der}})
	// ... but not via an intermediate (gc enforces pathLen on roots too).
	iUnder := rootPL0.signCA(t, "inter-under", -1, nil)
	runVerifyCase(t, verifyCase{name: "p0-4/pathlen-root0-violation",
		leafDER: iUnder.signLeaf(t, "leaf", nil),
		roots:   [][]byte{rootPL0.der}, intermediates: [][]byte{iUnder.der}})

	// P0-4: a non-CA intermediate is not authorized to sign. Pre-fix: any
	// signature-valid parent was accepted.
	fakeInter := root.signNonCA(t, "fake-inter")
	runVerifyCase(t, verifyCase{name: "p0-4/non-ca-intermediate",
		leafDER: fakeInter.signLeaf(t, "leaf", nil),
		roots:   [][]byte{root.der}, intermediates: [][]byte{fakeInter.der}})

	// P0-4 name constraints: permitted DNS.
	ncDNS := root.signCA(t, "nc-dns", -1, func(c *realx509.Certificate) {
		c.PermittedDNSDomains = []string{"example.com"}
	})
	runVerifyCase(t, verifyCase{name: "p0-4/nc-dns-permitted-ok",
		leafDER: ncDNS.signLeaf(t, "leaf", func(c *realx509.Certificate) {
			c.DNSNames = []string{"www.example.com"}
		}),
		roots: [][]byte{root.der}, intermediates: [][]byte{ncDNS.der}})
	runVerifyCase(t, verifyCase{name: "p0-4/nc-dns-permitted-violation",
		leafDER: ncDNS.signLeaf(t, "leaf", func(c *realx509.Certificate) {
			c.DNSNames = []string{"evil.com"}
		}),
		roots: [][]byte{root.der}, intermediates: [][]byte{ncDNS.der}})
	// Leading-dot permitted constraint: subdomains only.
	ncDot := root.signCA(t, "nc-dot", -1, func(c *realx509.Certificate) {
		c.PermittedDNSDomains = []string{".example.com"}
	})
	runVerifyCase(t, verifyCase{name: "p0-4/nc-dns-leading-dot-sub-ok",
		leafDER: ncDot.signLeaf(t, "leaf", func(c *realx509.Certificate) {
			c.DNSNames = []string{"www.example.com"}
		}),
		roots: [][]byte{root.der}, intermediates: [][]byte{ncDot.der}})
	runVerifyCase(t, verifyCase{name: "p0-4/nc-dns-leading-dot-apex-reject",
		leafDER: ncDot.signLeaf(t, "leaf", func(c *realx509.Certificate) {
			c.DNSNames = []string{"example.com"}
		}),
		roots: [][]byte{root.der}, intermediates: [][]byte{ncDot.der}})
	// Excluded DNS.
	ncExcl := root.signCA(t, "nc-excl", -1, func(c *realx509.Certificate) {
		c.ExcludedDNSDomains = []string{"bad.example.com"}
	})
	runVerifyCase(t, verifyCase{name: "p0-4/nc-dns-excluded-violation",
		leafDER: ncExcl.signLeaf(t, "leaf", func(c *realx509.Certificate) {
			c.DNSNames = []string{"bad.example.com"}
		}),
		roots: [][]byte{root.der}, intermediates: [][]byte{ncExcl.der}})
	runVerifyCase(t, verifyCase{name: "p0-4/nc-dns-excluded-ok",
		leafDER: ncExcl.signLeaf(t, "leaf", func(c *realx509.Certificate) {
			c.DNSNames = []string{"good.example.com"}
		}),
		roots: [][]byte{root.der}, intermediates: [][]byte{ncExcl.der}})
	// Excluded wildcard rule: excluded "sub.example.com" also excludes a
	// "*.example.com" wildcard SAN (gc dnsConstraints wildcard rule).
	ncExclW := root.signCA(t, "nc-exclw", -1, func(c *realx509.Certificate) {
		c.ExcludedDNSDomains = []string{"sub.example.com"}
	})
	runVerifyCase(t, verifyCase{name: "p0-4/nc-dns-wildcard-excluded",
		leafDER: ncExclW.signLeaf(t, "leaf", func(c *realx509.Certificate) {
			c.DNSNames = []string{"*.example.com"}
		}),
		roots: [][]byte{root.der}, intermediates: [][]byte{ncExclW.der}})
	runVerifyCase(t, verifyCase{name: "p0-4/nc-dns-wildcard-ok",
		leafDER: ncExclW.signLeaf(t, "leaf", func(c *realx509.Certificate) {
			c.DNSNames = []string{"*.other.com"}
		}),
		roots: [][]byte{root.der}, intermediates: [][]byte{ncExclW.der}})
	// IP constraints.
	ncIP := root.signCA(t, "nc-ip", -1, func(c *realx509.Certificate) {
		c.PermittedIPRanges = []*net.IPNet{mustCIDR(t, "10.0.0.0/8")}
	})
	runVerifyCase(t, verifyCase{name: "p0-4/nc-ip-permitted-ok",
		leafDER: ncIP.signLeaf(t, "leaf", func(c *realx509.Certificate) {
			c.IPAddresses = []net.IP{net.ParseIP("10.1.2.3")}
		}),
		roots: [][]byte{root.der}, intermediates: [][]byte{ncIP.der}})
	runVerifyCase(t, verifyCase{name: "p0-4/nc-ip-permitted-violation",
		leafDER: ncIP.signLeaf(t, "leaf", func(c *realx509.Certificate) {
			c.IPAddresses = []net.IP{net.ParseIP("192.168.1.1")}
		}),
		roots: [][]byte{root.der}, intermediates: [][]byte{ncIP.der}})
	ncIPExcl := root.signCA(t, "nc-ipexcl", -1, func(c *realx509.Certificate) {
		c.ExcludedIPRanges = []*net.IPNet{mustCIDR(t, "192.168.0.0/16")}
	})
	runVerifyCase(t, verifyCase{name: "p0-4/nc-ip-excluded-violation",
		leafDER: ncIPExcl.signLeaf(t, "leaf", func(c *realx509.Certificate) {
			c.IPAddresses = []net.IP{net.ParseIP("192.168.1.1")}
		}),
		roots: [][]byte{root.der}, intermediates: [][]byte{ncIPExcl.der}})
	runVerifyCase(t, verifyCase{name: "p0-4/nc-ip-excluded-ok",
		leafDER: ncIPExcl.signLeaf(t, "leaf", func(c *realx509.Certificate) {
			c.IPAddresses = []net.IP{net.ParseIP("10.0.0.1")}
		}),
		roots: [][]byte{root.der}, intermediates: [][]byte{ncIPExcl.der}})
	// Email constraints: bare domain and full mailbox forms.
	ncEmail := root.signCA(t, "nc-email", -1, func(c *realx509.Certificate) {
		c.PermittedEmailAddresses = []string{"example.com"}
	})
	runVerifyCase(t, verifyCase{name: "p0-4/nc-email-permitted-ok",
		leafDER: ncEmail.signLeaf(t, "leaf", func(c *realx509.Certificate) {
			c.EmailAddresses = []string{"a@example.com"}
		}),
		roots: [][]byte{root.der}, intermediates: [][]byte{ncEmail.der}})
	runVerifyCase(t, verifyCase{name: "p0-4/nc-email-permitted-violation",
		leafDER: ncEmail.signLeaf(t, "leaf", func(c *realx509.Certificate) {
			c.EmailAddresses = []string{"a@evil.com"}
		}),
		roots: [][]byte{root.der}, intermediates: [][]byte{ncEmail.der}})
	ncEmailExcl := root.signCA(t, "nc-emailexcl", -1, func(c *realx509.Certificate) {
		c.ExcludedEmailAddresses = []string{"bad@example.com"}
	})
	runVerifyCase(t, verifyCase{name: "p0-4/nc-email-excluded-violation",
		leafDER: ncEmailExcl.signLeaf(t, "leaf", func(c *realx509.Certificate) {
			c.EmailAddresses = []string{"bad@example.com"}
		}),
		roots: [][]byte{root.der}, intermediates: [][]byte{ncEmailExcl.der}})
	runVerifyCase(t, verifyCase{name: "p0-4/nc-email-excluded-ok",
		leafDER: ncEmailExcl.signLeaf(t, "leaf", func(c *realx509.Certificate) {
			c.EmailAddresses = []string{"good@example.com"}
		}),
		roots: [][]byte{root.der}, intermediates: [][]byte{ncEmailExcl.der}})
	// URI constraints.
	ncURI := root.signCA(t, "nc-uri", -1, func(c *realx509.Certificate) {
		c.PermittedURIDomains = []string{"example.com"}
	})
	runVerifyCase(t, verifyCase{name: "p0-4/nc-uri-permitted-ok",
		leafDER: ncURI.signLeaf(t, "leaf", func(c *realx509.Certificate) {
			c.URIs = []*url.URL{mustURL(t, "https://www.example.com/x")}
		}),
		roots: [][]byte{root.der}, intermediates: [][]byte{ncURI.der}})
	runVerifyCase(t, verifyCase{name: "p0-4/nc-uri-permitted-violation",
		leafDER: ncURI.signLeaf(t, "leaf", func(c *realx509.Certificate) {
			c.URIs = []*url.URL{mustURL(t, "https://evil.com/")}
		}),
		roots: [][]byte{root.der}, intermediates: [][]byte{ncURI.der}})

	// P0-4 EKU: default request is ServerAuth; a clientAuth-only leaf is
	// rejected. Pre-fix: accepted (leaf-only check, no default).
	runVerifyCase(t, verifyCase{name: "p0-4/eku-leaf-wrong-default",
		leafDER: interCA.signLeaf(t, "leaf", func(c *realx509.Certificate) {
			c.DNSNames = []string{"www.example.com"}
			c.ExtKeyUsage = []realx509.ExtKeyUsage{realx509.ExtKeyUsageClientAuth}
		}),
		roots: [][]byte{root.der}, intermediates: [][]byte{interCA.der},
		dnsName: "www.example.com"})
	// anyExtendedKeyUsage satisfies every request. Pre-fix: it fell into
	// UnknownExtKeyUsage and the leaf was rejected.
	runVerifyCase(t, verifyCase{name: "p0-4/eku-leaf-any",
		leafDER: interCA.signLeaf(t, "leaf", func(c *realx509.Certificate) {
			c.DNSNames = []string{"www.example.com"}
			c.ExtKeyUsage = []realx509.ExtKeyUsage{realx509.ExtKeyUsageAny}
		}),
		roots: [][]byte{root.der}, intermediates: [][]byte{interCA.der},
		dnsName: "www.example.com"})
	// Nested EKU: an intermediate enumerating EKUs constrains the leaf.
	// Pre-fix: only the leaf was checked, so this was accepted.
	ekuInter := root.signCA(t, "eku-inter", -1, func(c *realx509.Certificate) {
		c.ExtKeyUsage = []realx509.ExtKeyUsage{realx509.ExtKeyUsageClientAuth}
	})
	runVerifyCase(t, verifyCase{name: "p0-4/eku-nested-violation",
		leafDER: ekuInter.signLeaf(t, "leaf", nil),
		roots:   [][]byte{root.der}, intermediates: [][]byte{ekuInter.der}})
	ekuInterOK := root.signCA(t, "eku-inter-ok", -1, func(c *realx509.Certificate) {
		c.ExtKeyUsage = []realx509.ExtKeyUsage{realx509.ExtKeyUsageServerAuth}
	})
	runVerifyCase(t, verifyCase{name: "p0-4/eku-nested-ok",
		leafDER: ekuInterOK.signLeaf(t, "leaf", nil),
		roots:   [][]byte{root.der}, intermediates: [][]byte{ekuInterOK.der}})
	// Explicit usage request against a mismatching leaf EKU.
	runVerifyCase(t, verifyCase{name: "p0-4/eku-explicit-mismatch",
		leafDER: interCA.signLeaf(t, "leaf", func(c *realx509.Certificate) {
			c.ExtKeyUsage = []realx509.ExtKeyUsage{realx509.ExtKeyUsageServerAuth}
		}),
		roots: [][]byte{root.der}, intermediates: [][]byte{interCA.der},
		keyUsages: []ExtKeyUsage{ExtKeyUsageClientAuth}})
	// A leaf with no EKU at all is valid for every usage.
	runVerifyCase(t, verifyCase{name: "p0-4/eku-absent-ok",
		leafDER: interCA.signLeaf(t, "leaf", nil),
		roots: [][]byte{root.der}, intermediates: [][]byte{interCA.der},
		keyUsages: []ExtKeyUsage{ExtKeyUsageClientAuth}})

	// Hostname mismatch still surfaces as a HostnameError on both.
	runVerifyCase(t, verifyCase{name: "hostname-mismatch",
		leafDER: dnsLeaf(interCA, "www.example.com"),
		roots: [][]byte{root.der}, intermediates: [][]byte{interCA.der},
		dnsName: "other.com"})
	// Expiry.
	runVerifyCase(t, verifyCase{name: "expired",
		leafDER: interCA.signLeaf(t, "leaf", func(c *realx509.Certificate) {
			c.NotAfter = time.Now().Add(-time.Hour)
		}),
		roots: [][]byte{root.der}, intermediates: [][]byte{interCA.der}})
}

// TestParseNameConstraintsRejectsInvalid pins the fail-closed parsing:
// a structurally invalid extension encoding (the "INVALID" sentinel the
// intrinsic emits, or any malformed entry) fails certificate parsing,
// matching gc's "x509: invalid NameConstraints extension".
func TestParseNameConstraintsRejectsInvalid(t *testing.T) {
	for _, enc := range []string{"INVALID", "P:BOGUS:x", "P:IPH:zzzz", "Q:DNS:example.com", "P:DNS", ""} {
		c := &Certificate{}
		err := parseNameConstraints(c, enc)
		if enc == "" {
			if err != nil {
				t.Fatalf("empty encoding (absent extension) rejected: %v", err)
			}
			continue
		}
		if err == nil {
			t.Fatalf("encoding %q accepted, want parse error", enc)
		}
	}
	c := &Certificate{}
	if err := parseNameConstraints(c, "CRIT:1;P:DNS:example.com;X:IPH:0a000000:ff000000;P:EMAIL:a@example.com"); err != nil {
		t.Fatalf("valid encoding rejected: %v", err)
	}
	if !c.PermittedDNSDomainsCritical {
		t.Fatal("CRIT:1 did not set PermittedDNSDomainsCritical")
	}
	if len(c.PermittedDNSDomains) != 1 || c.PermittedDNSDomains[0] != "example.com" {
		t.Fatalf("bad DNS parse: %v", c.PermittedDNSDomains)
	}
	if len(c.ExcludedIPRanges) != 1 || c.ExcludedIPRanges[0].String() != "10.0.0.0/8" {
		t.Fatalf("bad IP parse: %v", c.ExcludedIPRanges)
	}
	if len(c.PermittedEmailAddresses) != 1 {
		t.Fatalf("bad EMAIL parse: %v", c.PermittedEmailAddresses)
	}
}

// TestSystemRootsDefault verifies that a nil Roots VerifyOptions resolves
// the stub system roots. Skipped when the outer test did not configure stub
// system roots.
func TestSystemRootsDefault(t *testing.T) {
	certPath := os.Getenv("HARNESS_SYSTEM_ROOTS_PEM")
	keyPath := os.Getenv("HARNESS_SYSTEM_ROOTS_KEY")
	if certPath == "" || keyPath == "" {
		t.Skip("no stub system roots configured")
	}
	certPEM, err := os.ReadFile(certPath)
	if err != nil {
		t.Fatal(err)
	}
	block, _ := pem.Decode(certPEM)
	if block == nil {
		t.Fatal("no PEM block in system root cert")
	}
	rootCert, err := realx509.ParseCertificate(block.Bytes)
	if err != nil {
		t.Fatal(err)
	}
	keyPEM, err := os.ReadFile(keyPath)
	if err != nil {
		t.Fatal(err)
	}
	kblock, _ := pem.Decode(keyPEM)
	if kblock == nil {
		t.Fatal("no PEM block in system root key")
	}
	keyAny, err := realx509.ParsePKCS8PrivateKey(kblock.Bytes)
	if err != nil {
		t.Fatal(err)
	}
	key, ok := keyAny.(*ecdsa.PrivateKey)
	if !ok {
		t.Fatal("system root key is not ECDSA")
	}
	ca := &driverCA{der: block.Bytes, cert: rootCert, key: key}
	leafDER := ca.signLeaf(t, "leaf", func(c *realx509.Certificate) {
		c.DNSNames = []string{"www.example.com"}
	})

	rleaf, err := realx509.ParseCertificate(leafDER)
	if err != nil {
		t.Fatal(err)
	}
	rroots := realx509.NewCertPool()
	rroots.AddCert(rootCert)
	if _, err := rleaf.Verify(realx509.VerifyOptions{Roots: rroots, DNSName: "www.example.com"}); err != nil {
		t.Fatalf("gc verify against stub root: %v", err)
	}

	sp := __mrParseCertificate(leafDER)
	if sp.v1 != nil {
		t.Fatalf("synth parse: %v", sp.v1)
	}
	got := __mrVerify(sp.v0, VerifyOptions{DNSName: "www.example.com"})
	if got.v1 != nil {
		t.Fatalf("synth verify with nil Roots (system roots): %v", got.v1)
	}
	if len(got.v0) != 1 || len(got.v0[0]) != 2 {
		t.Fatalf("expected one 2-cert chain, got %v", got.v0)
	}
}

// TestSystemRootsUnavailable verifies the fail-closed path: with no system
// roots available and no explicit Roots, Verify returns SystemRootsError
// (gc behavior). Skipped when stub system roots are configured.
func TestSystemRootsUnavailable(t *testing.T) {
	if os.Getenv("HARNESS_SYSTEM_ROOTS_PEM") != "" {
		t.Skip("stub system roots configured")
	}
	root := newDriverCA(t, "lonely-root", -1, nil)
	leafDER := root.signLeaf(t, "leaf", nil)
	sp := __mrParseCertificate(leafDER)
	if sp.v1 != nil {
		t.Fatalf("synth parse: %v", sp.v1)
	}
	got := __mrVerify(sp.v0, VerifyOptions{})
	if got.v1 == nil {
		t.Fatal("expected SystemRootsError, verification succeeded")
	}
	if _, ok := got.v1.(*SystemRootsError); !ok {
		t.Fatalf("expected *SystemRootsError, got %T (%v)", got.v1, got.v1)
	}
}
`
