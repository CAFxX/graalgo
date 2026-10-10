// P3-M9 (tier-5): synthetic `crypto/tls` package.
//
// Strategy per 2026-10-10 user directive: PREFER Java delegation via
// javax.net.ssl.SSLEngine where semantically compatible, over reimplementing
// the TLS handshake in Go. The Go synthetic package provides the API surface
// (types, Config) for type-checking; actual TLS operations trap to the Java
// runtime via //go:linkname.
//
// Real-Go API: adapted from gc go1.27.1 ($GOROOT/src/crypto/tls/*.go).
// Networking is loopback-only per M9 constraints.
//
// Contents:
//   - Config struct, Certificate struct (API surface for type-checking)
//   - X509KeyPair, LoadX509KeyPair (parse via Go, delegate handshake to Java)
//   - Client, Server, Dial, Listen (trap to Java SSLEngine)
//   - Conn with Handshake, Read, Write, Close (trap to Java)
//
// Java delegation (phases/phase2/src/main/java/graalgo/runtime/TlsRuntime.java):
//   - graalgo.tls.handshake, graalgo.tls.wrap, graalgo.tls.unwrap
//   - graalgo.tls.newEngine (client/server SSLEngine creation)
//   - Certificate parsing uses java.security.cert.CertificateFactory
package main

// syntheticCryptoTlsSource is the P3-M9 synthetic `crypto/tls` package.
const syntheticCryptoTlsSource = `package tls

import (
	"crypto"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"net"
	"time"
	"unsafe"
)

// TLS version constants.
const (
	VersionTLS10 = 0x0301
	VersionTLS11 = 0x0302
	VersionTLS12 = 0x0303
	VersionTLS13 = 0x0304
)

// Cipher suite constants (subset for M9; full list in cipher_suites.go).
const (
	TLS_RSA_WITH_AES_128_GCM_SHA256         uint16 = 0x009c
	TLS_RSA_WITH_AES_256_GCM_SHA384         uint16 = 0x009d
	TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256   uint16 = 0xc02f
	TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256 uint16 = 0xc02b
	TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384   uint16 = 0xc030
	TLS_ECDHE_ECDSA_WITH_AES_256_GCM_SHA384 uint16 = 0xc02c
	TLS_ECDHE_RSA_WITH_CHACHA20_POLY1305    uint16 = 0xcca8
	TLS_ECDHE_ECDSA_WITH_CHACHA20_POLY1305  uint16 = 0xcca9
	TLS_AES_128_GCM_SHA256                  uint16 = 0x1301
	TLS_AES_256_GCM_SHA384                  uint16 = 0x1302
	TLS_CHACHA20_POLY1305_SHA256            uint16 = 0x1303
)

// CurveID identifies an elliptic curve.
type CurveID uint16

const (
	CurveP256 CurveID = 23
	CurveP384 CurveID = 24
	CurveP521 CurveID = 25
	X25519    CurveID = 29
)

// Config is a TLS configuration.
type Config struct {
	Rand                   io.Reader
	Time                   func() time.Time
	Certificates           []Certificate
	NameToCertificate      map[string]*Certificate
	GetCertificate         func(*ClientHelloInfo) (*Certificate, error)
	GetClientCertificate   func(*CertificateRequestInfo) (*Certificate, error)
	GetConfigForClient     func(*ClientHelloInfo) (*Config, error)
	VerifyPeerCertificate  func(rawCerts [][]byte, verifiedChains [][]*x509.Certificate) error
	VerifyConnection       func(ConnectionState) error
	RootCAs                *x509.CertPool
	NextProtos             []string
	ServerName             string
	ClientAuth             ClientAuthType
	ClientCAs              *x509.CertPool
	InsecureSkipVerify     bool
	CipherSuites           []uint16
	PreferServerCipherSuites bool
	SessionTicketsDisabled bool
	MinVersion             uint16
	MaxVersion             uint16
	CurvePreferences       []CurveID
	Renegotiation          RenegotiationSupport
	KeyLogWriter           io.Writer
}

// ClientAuthType determines client authentication.
type ClientAuthType int

const (
	NoClientCert ClientAuthType = iota
	RequestClientCert
	RequireAnyClientCert
	VerifyClientCertIfGiven
	RequireAndVerifyClientCert
)

// RenegotiationSupport controls renegotiation.
type RenegotiationSupport int

const (
	RenegotiateNever RenegotiationSupport = iota
	RenegotiateOnceAsClient
	RenegotiateFreelyAsClient
)

// ClientHelloInfo contains ClientHello information.
type ClientHelloInfo struct {
	CipherSuites      []uint16
	ServerName        string
	SupportedCurves   []CurveID
	SupportedPoints   []uint8
	SignatureSchemes  []SignatureScheme
	SupportedProtos   []string
	SupportedVersions []uint16
	Conn              net.Conn
}

// CertificateRequestInfo contains certificate request info.
type CertificateRequestInfo struct {
	AcceptableCAs    [][]byte
	SignatureSchemes []SignatureScheme
	Version          uint16
}

// ConnectionState records TLS connection details.
type ConnectionState struct {
	Version                     uint16
	HandshakeComplete           bool
	DidResume                   bool
	CipherSuite                 uint16
	NegotiatedProtocol          string
	ServerName                  string
	PeerCertificates            []*x509.Certificate
	VerifiedChains              [][]*x509.Certificate
	SignedCertificateTimestamps [][]byte
	OCSPResponse                []byte
	TLSUnique                   []byte
}

// SignatureScheme identifies a signature scheme.
type SignatureScheme uint16

// Certificate is a TLS certificate chain with private key.
type Certificate struct {
	Certificate [][]byte
	PrivateKey  crypto.PrivateKey
	Leaf        *x509.Certificate
}

// X509KeyPair parses PEM-encoded cert and key.
// Certificate parsing delegates to Java CertificateFactory where possible;
// key parsing uses Go (PKCS#1/PKCS#8/EC).
func __tlsX509KeyPair(certPEMBlock, keyPEMBlock []byte) *__tlsX509KeyPairPair {
	cert, err := x509KeyPairImpl(certPEMBlock, keyPEMBlock)
	return &__tlsX509KeyPairPair{v0: cert, v1: err}
}

type __tlsX509KeyPairPair struct {
	v0 Certificate
	v1 error
}

func X509KeyPair(certPEMBlock, keyPEMBlock []byte) (Certificate, error) {
	p := __tlsX509KeyPair(certPEMBlock, keyPEMBlock)
	return p.v0, p.v1
}

func x509KeyPairImpl(certPEMBlock, keyPEMBlock []byte) (Certificate, error) {
	var cert Certificate
	var skippedBlockTypes []string

	for {
		var certBlock *pem.Block
		certBlock, certPEMBlock = pem.Decode(certPEMBlock)
		if certBlock == nil {
			break
		}
		if certBlock.Type == "CERTIFICATE" {
			cert.Certificate = append(cert.Certificate, certBlock.Bytes)
		} else {
			skippedBlockTypes = append(skippedBlockTypes, certBlock.Type)
		}
	}

	if len(cert.Certificate) == 0 {
		return cert, errors.New("tls: failed to find any PEM data in certificate input")
	}

	// Parse leaf for API compatibility (delegates to x509 synthetic)
	leaf, err := x509.ParseCertificate(cert.Certificate[0])
	if err != nil {
		return cert, err
	}
	cert.Leaf = leaf

	// Key parsing: try PKCS#8, PKCS#1, EC, PKCS#1 via x509 helpers
	// (implemented in Go; Java delegation for key ops happens at handshake)
	var keyBlock *pem.Block
	for {
		keyBlock, keyPEMBlock = pem.Decode(keyPEMBlock)
		if keyBlock == nil {
			break
		}
		if keyBlock.Type == "PRIVATE KEY" || keyBlock.Type == "RSA PRIVATE KEY" ||
			keyBlock.Type == "EC PRIVATE KEY" || keyBlock.Type == "OPENSSH PRIVATE KEY" {
			break
		}
	}
	if keyBlock == nil {
		return cert, errors.New("tls: failed to find any PEM data in key input")
	}

	// Store raw key bytes; actual crypto.PrivateKey materialization happens
	// lazily via the Java SSLEngine key manager (see TlsRuntime).
	// For API compatibility, we attempt Go-side parsing.
	priv, err := parsePrivateKey(keyBlock.Bytes)
	if err != nil {
		return cert, err
	}
	cert.PrivateKey = priv

	return cert, nil
}

func parsePrivateKey(der []byte) (crypto.PrivateKey, error) {
	// Try PKCS#8 first
	if key, err := x509.ParsePKCS8PrivateKey(der); err == nil {
		return key, nil
	}
	// Try PKCS#1 (RSA)
	if key, err := x509.ParsePKCS1PrivateKey(der); err == nil {
		return key, nil
	}
	// Try EC
	if key, err := x509.ParseECPrivateKey(der); err == nil {
		return key, nil
	}
	return nil, errors.New("tls: failed to parse private key")
}

// LoadX509KeyPair loads cert/key from files.
func __tlsLoadX509KeyPair(certFile, keyFile string) *__tlsLoadX509KeyPairPair {
	cert, err := loadX509KeyPairImpl(certFile, keyFile)
	return &__tlsLoadX509KeyPairPair{v0: cert, v1: err}
}

type __tlsLoadX509KeyPairPair struct {
	v0 Certificate
	v1 error
}

func LoadX509KeyPair(certFile, keyFile string) (Certificate, error) {
	p := __tlsLoadX509KeyPair(certFile, keyFile)
	return p.v0, p.v1
}

//go:linkname loadX509KeyPairTrap graalgo.tls.loadX509KeyPair
func loadX509KeyPairTrap(certFile, keyFile string) (Certificate, error)

func loadX509KeyPairImpl(certFile, keyFile string) (Certificate, error) {
	// Delegate file I/O to Java (avoids os package dependency in synthetic)
	return loadX509KeyPairTrap(certFile, keyFile)
}

// Conn is a TLS connection backed by Java SSLEngine.
type Conn struct {
	// engineHandle is an opaque handle to the Java SSLEngine state
	// (managed by TlsRuntime; not directly accessible from Go).
	engineHandle int64
	conn         net.Conn
	config       *Config
	isClient     bool
}

// Client creates a TLS client over conn.
// The handshake is performed lazily via Java SSLEngine.
func Client(conn net.Conn, config *Config) *Conn {
	c := &Conn{conn: conn, config: config, isClient: true}
	c.engineHandle = newClientEngineTrap(config)
	return c
}

// Server creates a TLS server over conn.
func Server(conn net.Conn, config *Config) *Conn {
	c := &Conn{conn: conn, config: config, isClient: false}
	c.engineHandle = newServerEngineTrap(config)
	return c
}

//go:linkname newClientEngineTrap graalgo.tls.newClientEngine
func newClientEngineTrap(config *Config) int64

//go:linkname newServerEngineTrap graalgo.tls.newServerEngine
func newServerEngineTrap(config *Config) int64

// Dial connects to a TLS server (loopback-only in M9).
func __tlsDial(network, addr string, config *Config) *__tlsDialPair {
	conn, err := dialImpl(network, addr, config)
	return &__tlsDialPair{v0: conn, v1: err}
}

type __tlsDialPair struct {
	v0 *Conn
	v1 error
}

func Dial(network, addr string, config *Config) (*Conn, error) {
	p := __tlsDial(network, addr, config)
	return p.v0, p.v1
}

func dialImpl(network, addr string, config *Config) (*Conn, error) {
	if config == nil {
		return nil, errors.New("tls: nil Config")
	}
	rawConn, err := net.Dial(network, addr)
	if err != nil {
		return nil, err
	}
	conn := Client(rawConn, config)
	if err := conn.Handshake(); err != nil {
		rawConn.Close()
		return nil, err
	}
	return conn, nil
}

// DialWithDialer connects using a Dialer.
func __tlsDialWithDialer(dialer *net.Dialer, network, addr string, config *Config) *__tlsDialWithDialerPair {
	conn, err := dialWithDialerImpl(dialer, network, addr, config)
	return &__tlsDialWithDialerPair{v0: conn, v1: err}
}

type __tlsDialWithDialerPair struct {
	v0 *Conn
	v1 error
}

func DialWithDialer(dialer *net.Dialer, network, addr string, config *Config) (*Conn, error) {
	p := __tlsDialWithDialer(dialer, network, addr, config)
	return p.v0, p.v1
}

func dialWithDialerImpl(dialer *net.Dialer, network, addr string, config *Config) (*Conn, error) {
	if config == nil {
		return nil, errors.New("tls: nil Config")
	}
	rawConn, err := dialer.Dial(network, addr)
	if err != nil {
		return nil, err
	}
	conn := Client(rawConn, config)
	if err := conn.Handshake(); err != nil {
		rawConn.Close()
		return nil, err
	}
	return conn, nil
}

// Listen creates a TLS listener (loopback-only in M9).
func __tlsListen(network, laddr string, config *Config) *__tlsListenPair {
	l, err := listenImpl(network, laddr, config)
	return &__tlsListenPair{v0: l, v1: err}
}

type __tlsListenPair struct {
	v0 net.Listener
	v1 error
}

func Listen(network, laddr string, config *Config) (net.Listener, error) {
	p := __tlsListen(network, laddr, config)
	return p.v0, p.v1
}

func listenImpl(network, laddr string, config *Config) (net.Listener, error) {
	if config == nil || len(config.Certificates) == 0 {
		return nil, errors.New("tls: neither Certificates, GetCertificate, nor GetConfigForClient set in Config")
	}
	l, err := net.Listen(network, laddr)
	if err != nil {
		return nil, err
	}
	return NewListener(l, config), nil
}

// NewListener creates a TLS listener.
func NewListener(inner net.Listener, config *Config) net.Listener {
	return &listener{Listener: inner, config: config}
}

type listener struct {
	net.Listener
	config *Config
}

func (l *listener) Accept() (net.Conn, error) {
	c, err := l.Listener.Accept()
	if err != nil {
		return nil, err
	}
	return Server(c, l.config), nil
}

// Handshake performs the TLS handshake via Java SSLEngine.
//go:linkname handshakeTrap graalgo.tls.handshake
func handshakeTrap(engineHandle int64, isClient bool) int64

func (c *Conn) Handshake() error {
	if c.engineHandle == 0 {
		return errors.New("tls: nil engine")
	}
	// Delegate to Java SSLEngine; returns 0 on success, error code otherwise
	if rc := handshakeTrap(c.engineHandle, c.isClient); rc != 0 {
		return tlsHandshakeError(rc)
	}
	return nil
}

func tlsHandshakeError(code int64) error {
	return fmt.Errorf("tls: handshake failed (code %d)", code)
}

// ConnectionState returns the connection state via Java.
func (c *Conn) ConnectionState() ConnectionState {
	// Query Java for the negotiated parameters
	vers, suite := connectionStateTrap(c.engineHandle)
	return ConnectionState{
		Version:           uint16(vers),
		HandshakeComplete: true,
		CipherSuite:       uint16(suite),
		ServerName:        c.config.ServerName,
	}
}

//go:linkname connectionStateTrap graalgo.tls.connectionState
func connectionStateTrap(engineHandle int64) (int64, int64)

// Read reads decrypted data via Java SSLEngine.unwrap.
//go:linkname tlsReadTrap graalgo.tls.read
func tlsReadTrap(engineHandle int64, b []byte) int64

func (c *Conn) Read(b []byte) (int, error) {
	n := tlsReadTrap(c.engineHandle, b)
	if n < 0 {
		return 0, tlsIOError(n)
	}
	return int(n), nil
}

// Write encrypts and writes via Java SSLEngine.wrap.
//go:linkname tlsWriteTrap graalgo.tls.write
func tlsWriteTrap(engineHandle int64, b []byte) int64

func (c *Conn) Write(b []byte) (int, error) {
	n := tlsWriteTrap(c.engineHandle, b)
	if n < 0 {
		return 0, tlsIOError(n)
	}
	return int(n), nil
}

func tlsIOError(code int64) error {
	return fmt.Errorf("tls: I/O error (code %d)", code)
}

// Close closes the TLS connection.
//go:linkname tlsCloseTrap graalgo.tls.close
func tlsCloseTrap(engineHandle int64)

func (c *Conn) Close() error {
	if c.engineHandle != 0 {
		tlsCloseTrap(c.engineHandle)
		c.engineHandle = 0
	}
	return c.conn.Close()
}

// LocalAddr returns the local address.
func (c *Conn) LocalAddr() net.Addr { return c.conn.LocalAddr() }

// RemoteAddr returns the remote address.
func (c *Conn) RemoteAddr() net.Addr { return c.conn.RemoteAddr() }

// SetDeadline sets the deadline.
func (c *Conn) SetDeadline(t time.Time) error { return c.conn.SetDeadline(t) }

// SetReadDeadline sets the read deadline.
func (c *Conn) SetReadDeadline(t time.Time) error { return c.conn.SetReadDeadline(t) }

// SetWriteDeadline sets the write deadline.
func (c *Conn) SetWriteDeadline(t time.Time) error { return c.conn.SetWriteDeadline(t) }

// VerifyHostname verifies the hostname against the peer certificate.
func (c *Conn) VerifyHostname(host string) error {
	if c.config != nil && c.config.InsecureSkipVerify {
		return nil
	}
	// Delegate to Java X509TrustManager hostname verification
	if rc := verifyHostnameTrap(c.engineHandle, host); rc != 0 {
		return fmt.Errorf("tls: hostname verification failed for %s", host)
	}
	return nil
}

//go:linkname verifyHostnameTrap graalgo.tls.verifyHostname
func verifyHostnameTrap(engineHandle int64, host string) int64

// Ensure unsafe is imported for linkname (required by Go toolchain).
var _ = unsafe.Sizeof(0)
`
