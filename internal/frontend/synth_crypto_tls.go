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
	"context"
	"crypto"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net"
	"os"
	"sync"
	"time"
	"unsafe"
)

// TLS version constants.
const (
	VersionSSL30 = 0x0300
	VersionTLS10 = 0x0301
	VersionTLS11 = 0x0302
	VersionTLS12 = 0x0303
	VersionTLS13 = 0x0304
)

// VersionName returns the name for the passed protocol version,
// or a fallback representation of the version if unknown.
// Mirrors gc go1.27.1.
func VersionName(version uint16) string {
	switch version {
	case VersionSSL30:
		return "SSLv3"
	case VersionTLS10:
		return "TLS 1.0"
	case VersionTLS11:
		return "TLS 1.1"
	case VersionTLS12:
		return "TLS 1.2"
	case VersionTLS13:
		return "TLS 1.3"
	default:
		return fmt.Sprintf("0x%04X", version)
	}
}

// Cipher suite constants (gc go1.27.1 src/crypto/tls/cipher_suites.go).
const (
	TLS_RSA_WITH_RC4_128_SHA                uint16 = 0x0005
	TLS_RSA_WITH_3DES_EDE_CBC_SHA           uint16 = 0x000a
	TLS_RSA_WITH_AES_128_CBC_SHA            uint16 = 0x002f
	TLS_RSA_WITH_AES_256_CBC_SHA            uint16 = 0x0035
	TLS_RSA_WITH_AES_128_CBC_SHA256         uint16 = 0x003c
	TLS_RSA_WITH_AES_128_GCM_SHA256         uint16 = 0x009c
	TLS_RSA_WITH_AES_256_GCM_SHA384         uint16 = 0x009d
	TLS_ECDHE_ECDSA_WITH_RC4_128_SHA        uint16 = 0xc007
	TLS_ECDHE_ECDSA_WITH_AES_128_CBC_SHA    uint16 = 0xc009
	TLS_ECDHE_ECDSA_WITH_AES_256_CBC_SHA    uint16 = 0xc00a
	TLS_ECDHE_RSA_WITH_RC4_128_SHA          uint16 = 0xc011
	TLS_ECDHE_RSA_WITH_3DES_EDE_CBC_SHA     uint16 = 0xc012
	TLS_ECDHE_RSA_WITH_AES_128_CBC_SHA      uint16 = 0xc013
	TLS_ECDHE_RSA_WITH_AES_256_CBC_SHA      uint16 = 0xc014
	TLS_ECDHE_ECDSA_WITH_AES_128_CBC_SHA256 uint16 = 0xc023
	TLS_ECDHE_RSA_WITH_AES_128_CBC_SHA256   uint16 = 0xc027
	TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256   uint16 = 0xc02f
	TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256 uint16 = 0xc02b
	TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384   uint16 = 0xc030
	TLS_ECDHE_ECDSA_WITH_AES_256_GCM_SHA384 uint16 = 0xc02c
	TLS_ECDHE_RSA_WITH_CHACHA20_POLY1305_SHA256   uint16 = 0xcca8
	TLS_ECDHE_ECDSA_WITH_CHACHA20_POLY1305_SHA256 uint16 = 0xcca9
	TLS_AES_128_GCM_SHA256                  uint16 = 0x1301
	TLS_AES_256_GCM_SHA384                  uint16 = 0x1302
	TLS_CHACHA20_POLY1305_SHA256            uint16 = 0x1303
	TLS_FALLBACK_SCSV                       uint16 = 0x5600
)

// CipherSuite describes a TLS cipher suite.
type CipherSuite struct {
	ID   uint16
	Name string

	// SupportedVersions is the list of TLS protocol versions that can
	// negotiate this cipher suite.
	SupportedVersions []uint16

	// Insecure is true if the cipher suite has known security issues
	// due to its primitives, design, or implementation.
	Insecure bool
}

// CipherSuites returns the cipher suites implemented by this package,
// excluding those with security issues (see InsecureCipherSuites).
// Mirrors gc go1.27.1.
func CipherSuites() []*CipherSuite {
	supportedUpToTLS12 := []uint16{VersionTLS10, VersionTLS11, VersionTLS12}
	supportedOnlyTLS12 := []uint16{VersionTLS12}
	supportedOnlyTLS13 := []uint16{VersionTLS13}
	return []*CipherSuite{
		{ID: TLS_AES_128_GCM_SHA256, Name: "TLS_AES_128_GCM_SHA256", SupportedVersions: supportedOnlyTLS13},
		{ID: TLS_AES_256_GCM_SHA384, Name: "TLS_AES_256_GCM_SHA384", SupportedVersions: supportedOnlyTLS13},
		{ID: TLS_CHACHA20_POLY1305_SHA256, Name: "TLS_CHACHA20_POLY1305_SHA256", SupportedVersions: supportedOnlyTLS13},

		{ID: TLS_ECDHE_ECDSA_WITH_AES_128_CBC_SHA, Name: "TLS_ECDHE_ECDSA_WITH_AES_128_CBC_SHA", SupportedVersions: supportedUpToTLS12},
		{ID: TLS_ECDHE_ECDSA_WITH_AES_256_CBC_SHA, Name: "TLS_ECDHE_ECDSA_WITH_AES_256_CBC_SHA", SupportedVersions: supportedUpToTLS12},
		{ID: TLS_ECDHE_RSA_WITH_AES_128_CBC_SHA, Name: "TLS_ECDHE_RSA_WITH_AES_128_CBC_SHA", SupportedVersions: supportedUpToTLS12},
		{ID: TLS_ECDHE_RSA_WITH_AES_256_CBC_SHA, Name: "TLS_ECDHE_RSA_WITH_AES_256_CBC_SHA", SupportedVersions: supportedUpToTLS12},
		{ID: TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256, Name: "TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256", SupportedVersions: supportedOnlyTLS12},
		{ID: TLS_ECDHE_ECDSA_WITH_AES_256_GCM_SHA384, Name: "TLS_ECDHE_ECDSA_WITH_AES_256_GCM_SHA384", SupportedVersions: supportedOnlyTLS12},
		{ID: TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256, Name: "TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256", SupportedVersions: supportedOnlyTLS12},
		{ID: TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384, Name: "TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384", SupportedVersions: supportedOnlyTLS12},
		{ID: TLS_ECDHE_RSA_WITH_CHACHA20_POLY1305_SHA256, Name: "TLS_ECDHE_RSA_WITH_CHACHA20_POLY1305_SHA256", SupportedVersions: supportedOnlyTLS12},
		{ID: TLS_ECDHE_ECDSA_WITH_CHACHA20_POLY1305_SHA256, Name: "TLS_ECDHE_ECDSA_WITH_CHACHA20_POLY1305_SHA256", SupportedVersions: supportedOnlyTLS12},
	}
}

// InsecureCipherSuites returns the cipher suites implemented by this package
// which have security issues. Mirrors gc go1.27.1.
func InsecureCipherSuites() []*CipherSuite {
	supportedUpToTLS12 := []uint16{VersionTLS10, VersionTLS11, VersionTLS12}
	supportedOnlyTLS12 := []uint16{VersionTLS12}
	mk := func(id uint16, name string, vers []uint16) *CipherSuite {
		return &CipherSuite{ID: id, Name: name, SupportedVersions: vers, Insecure: true}
	}
	return []*CipherSuite{
		mk(TLS_RSA_WITH_RC4_128_SHA, "TLS_RSA_WITH_RC4_128_SHA", supportedUpToTLS12),
		mk(TLS_RSA_WITH_3DES_EDE_CBC_SHA, "TLS_RSA_WITH_3DES_EDE_CBC_SHA", supportedUpToTLS12),
		mk(TLS_RSA_WITH_AES_128_CBC_SHA, "TLS_RSA_WITH_AES_128_CBC_SHA", supportedUpToTLS12),
		mk(TLS_RSA_WITH_AES_256_CBC_SHA, "TLS_RSA_WITH_AES_256_CBC_SHA", supportedUpToTLS12),
		mk(TLS_RSA_WITH_AES_128_CBC_SHA256, "TLS_RSA_WITH_AES_128_CBC_SHA256", supportedOnlyTLS12),
		mk(TLS_RSA_WITH_AES_128_GCM_SHA256, "TLS_RSA_WITH_AES_128_GCM_SHA256", supportedOnlyTLS12),
		mk(TLS_RSA_WITH_AES_256_GCM_SHA384, "TLS_RSA_WITH_AES_256_GCM_SHA384", supportedOnlyTLS12),
		mk(TLS_ECDHE_ECDSA_WITH_RC4_128_SHA, "TLS_ECDHE_ECDSA_WITH_RC4_128_SHA", supportedUpToTLS12),
		mk(TLS_ECDHE_RSA_WITH_RC4_128_SHA, "TLS_ECDHE_RSA_WITH_RC4_128_SHA", supportedUpToTLS12),
		mk(TLS_ECDHE_RSA_WITH_3DES_EDE_CBC_SHA, "TLS_ECDHE_RSA_WITH_3DES_EDE_CBC_SHA", supportedUpToTLS12),
		mk(TLS_ECDHE_ECDSA_WITH_AES_128_CBC_SHA256, "TLS_ECDHE_ECDSA_WITH_AES_128_CBC_SHA256", supportedOnlyTLS12),
		mk(TLS_ECDHE_RSA_WITH_AES_128_CBC_SHA256, "TLS_ECDHE_RSA_WITH_AES_128_CBC_SHA256", supportedOnlyTLS12),
	}
}

// CipherSuiteName returns the standard name for the passed cipher suite ID,
// or a fallback representation of the ID if not implemented by this package.
// Mirrors gc go1.27.1.
func CipherSuiteName(id uint16) string {
	for _, c := range CipherSuites() {
		if c.ID == id {
			return c.Name
		}
	}
	for _, c := range InsecureCipherSuites() {
		if c.ID == id {
			return c.Name
		}
	}
	return fmt.Sprintf("0x%04X", id)
}

// AlertError is a TLS alert value (RFC 8446, Section 6).
// Mirrors gc go1.27.1 (type AlertError uint8).
type AlertError uint8

// Error returns a description of the alert.
func (e AlertError) Error() string {
	if s, ok := alertText[uint8(e)]; ok {
		return "tls: " + s
	}
	return fmt.Sprintf("tls: alert(%d)", uint8(e))
}

var alertText = map[uint8]string{
	0:   "close notify",
	10:  "unexpected message",
	20:  "bad record MAC",
	21:  "decryption failed",
	22:  "record overflow",
	30:  "decompression failure",
	40:  "handshake failure",
	42:  "bad certificate",
	43:  "unsupported certificate",
	44:  "revoked certificate",
	45:  "expired certificate",
	46:  "unknown certificate",
	47:  "illegal parameter",
	48:  "unknown certificate authority",
	49:  "access denied",
	50:  "decode error",
	51:  "decrypt error",
	60:  "export restriction",
	70:  "protocol version",
	71:  "insufficient security",
	80:  "internal error",
	86:  "inappropriate fallback",
	90:  "user canceled",
	100: "no renegotiation",
	109: "missing extension",
	110: "unsupported extension",
	111: "certificate unobtainable",
	112: "unrecognized name",
	113: "bad certificate status response",
	114: "bad certificate hash value",
	115: "unknown PSK identity",
	116: "certificate required",
	120: "no application protocol",
	121: "ECH required",
}

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
	SessionTicketKey       [32]byte
	ClientSessionCache     ClientSessionCache
	MinVersion             uint16
	MaxVersion             uint16
	CurvePreferences       []CurveID
	DynamicRecordSizingDisabled bool
	Renegotiation          RenegotiationSupport
	KeyLogWriter           io.Writer

	// sessionTicketKeys holds keys installed via SetSessionTicketKeys.
	// The Java SSLEngine manages its own session-ticket encryption;
	// these are stored for API compatibility only.
	sessionTicketKeys [][32]byte
}

// Clone returns a shallow clone of c. It is safe to clone a Config that is
// being used concurrently by a TLS client or server.
func (c *Config) Clone() *Config {
	if c == nil {
		return nil
	}
	c2 := *c
	return &c2
}

// SetSessionTicketKeys updates the session ticket keys used by c.
// Like gc, it panics if keys is empty.
func (c *Config) SetSessionTicketKeys(keys [][32]byte) {
	if len(keys) == 0 {
		panic("tls: keys must have at least one key")
	}
	c.sessionTicketKeys = keys
}

// BuildNameToCertificate parses Certificates and populates NameToCertificate.
// Mirrors gc go1.27.1.
func (c *Config) BuildNameToCertificate() {
	c.NameToCertificate = make(map[string]*Certificate)
	for i := range c.Certificates {
		cert := &c.Certificates[i]
		leaf := cert.Leaf
		if leaf == nil && len(cert.Certificate) > 0 {
			var err error
			leaf, err = x509.ParseCertificate(cert.Certificate[0])
			if err != nil {
				continue
			}
		}
		if leaf == nil {
			continue
		}
		// If SANs are *not* present, some clients will consider the
		// certificate valid for the name in the Common Name.
		if leaf.Subject.CommonName != "" && len(leaf.DNSNames) == 0 {
			c.NameToCertificate[leaf.Subject.CommonName] = cert
		}
		for _, san := range leaf.DNSNames {
			c.NameToCertificate[san] = cert
		}
	}
}

// SessionState is the state of a TLS session, used for session resumption.
// Only the exported fields are meaningful to callers; the session ticket
// itself is managed by the Java SSLEngine.
type SessionState struct {
	// Extra contains extra data to be stored with the session.
	Extra [][]byte
	// EarlyData indicates whether the session can be used for early data.
	EarlyData bool

	ticket []byte
}

// ClientSessionState is the state of a single TLS session, as stored in a
// ClientSessionCache.
type ClientSessionState struct {
	session *SessionState
}

// ResumptionState returns the session ticket sent by the server and the
// state necessary to resume this session.
func (cs *ClientSessionState) ResumptionState() (ticket []byte, state *SessionState, err error) {
	if cs == nil || cs.session == nil {
		return nil, nil, nil
	}
	return cs.session.ticket, cs.session, nil
}

// NewResumptionState returns a state value that can be returned by
// ClientSessionCache.Get to resume a previous session.
func NewResumptionState(ticket []byte, state *SessionState) (*ClientSessionState, error) {
	if state == nil {
		return nil, errors.New("tls: nil SessionState")
	}
	state.ticket = ticket
	return &ClientSessionState{session: state}, nil
}

// ClientSessionCache is a cache of ClientSessionState entries for TLS
// clients, used for session resumption.
type ClientSessionCache interface {
	// Get searches for a ClientSessionState associated with the given key.
	// On return, ok is true if one was found.
	Get(sessionKey string) (session *ClientSessionState, ok bool)

	// Put adds the ClientSessionState to the cache with the given key.
	// If called with a nil *ClientSessionState, it removes the entry.
	Put(sessionKey string, cs *ClientSessionState)
}

// NewLRUClientSessionCache returns a ClientSessionCache with the given
// capacity. Once the cache reaches capacity, the least-recently-used
// entry is evicted. Pure-Go implementation mirroring gc's semantics.
func NewLRUClientSessionCache(capacity int) ClientSessionCache {
	if capacity < 1 {
		capacity = 1
	}
	return &lruSessionCache{capacity: capacity, entries: make(map[string]*lruSessionCacheEntry)}
}

type lruSessionCacheEntry struct {
	cs   *ClientSessionState
	prev *lruSessionCacheEntry
	next *lruSessionCacheEntry
	key  string
}

type lruSessionCache struct {
	mu       sync.Mutex
	capacity int
	entries  map[string]*lruSessionCacheEntry
	newest   *lruSessionCacheEntry
	oldest   *lruSessionCacheEntry
}

func (c *lruSessionCache) Get(sessionKey string) (*ClientSessionState, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.entries[sessionKey]
	if !ok {
		return nil, false
	}
	c.moveToFront(e)
	return e.cs, true
}

func (c *lruSessionCache) Put(sessionKey string, cs *ClientSessionState) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if e, ok := c.entries[sessionKey]; ok {
		e.cs = cs
		if cs == nil {
			c.remove(e)
		} else {
			c.moveToFront(e)
		}
		return
	}
	if cs == nil {
		return
	}
	e := &lruSessionCacheEntry{cs: cs, key: sessionKey}
	c.entries[sessionKey] = e
	c.pushFront(e)
	for len(c.entries) > c.capacity {
		c.remove(c.oldest)
	}
}

func (c *lruSessionCache) pushFront(e *lruSessionCacheEntry) {
	e.prev = nil
	e.next = c.newest
	if c.newest != nil {
		c.newest.prev = e
	}
	c.newest = e
	if c.oldest == nil {
		c.oldest = e
	}
}

func (c *lruSessionCache) moveToFront(e *lruSessionCacheEntry) {
	if e == c.newest {
		return
	}
	if e.prev != nil {
		e.prev.next = e.next
	}
	if e.next != nil {
		e.next.prev = e.prev
	}
	if e == c.oldest {
		c.oldest = e.prev
	}
	c.pushFront(e)
}

func (c *lruSessionCache) remove(e *lruSessionCacheEntry) {
	if e.prev != nil {
		e.prev.next = e.next
	} else {
		c.newest = e.next
	}
	if e.next != nil {
		e.next.prev = e.prev
	} else {
		c.oldest = e.prev
	}
	delete(c.entries, e.key)
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
	CurveID                     CurveID
	NegotiatedProtocol          string
	NegotiatedProtocolIsMutual  bool
	ServerName                  string
	PeerCertificates            []*x509.Certificate
	VerifiedChains              [][]*x509.Certificate
	SignedCertificateTimestamps [][]byte
	OCSPResponse                []byte
	TLSUnique                   []byte
	ECHAccepted                 bool
	HelloRetryRequest           bool
	LocalCertificate            [][]byte

	ekm func(label string, context []byte, length int) ([]byte, error)

	testingOnlyPeerSignatureAlgorithm SignatureScheme
}

// ExportKeyingMaterial returns length bytes of exported key material in a new
// slice as defined in RFC 5705. If context is nil, it is not used as part of
// the seed. If the connection was set to allow renegotiation via
// Config.Renegotiation, or if the connections supports neither TLS 1.3 nor
// Extended Master Secret, this function will return an error.
func (cs *ConnectionState) ExportKeyingMaterial(label string, context []byte, length int) ([]byte, error) {
	return cs.ekm(label, context, length)
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
func __mrX509KeyPair(certPEMBlock, keyPEMBlock []byte) __mrX509KeyPairPair {
	return x509KeyPairImpl(certPEMBlock, keyPEMBlock)
}

type __mrX509KeyPairPair struct {
	v0 Certificate
	v1 error
}

func X509KeyPair(certPEMBlock, keyPEMBlock []byte) (Certificate, error) {
	panic("crypto/tls: X509KeyPair: multi-result desugar")
}

func x509KeyPairImpl(certPEMBlock, keyPEMBlock []byte) __mrX509KeyPairPair {
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
		return __mrX509KeyPairPair{cert, errors.New("tls: failed to find any PEM data in certificate input")}
	}

	// Parse leaf for API compatibility (delegates to x509 synthetic)
	leaf, err := x509.ParseCertificate(cert.Certificate[0])
	if err != nil {
		return __mrX509KeyPairPair{cert, err}
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
		return __mrX509KeyPairPair{cert, errors.New("tls: failed to find any PEM data in key input")}
	}

	// Store raw key bytes; actual crypto.PrivateKey materialization happens
	// lazily via the Java SSLEngine key manager (see TlsRuntime).
	// For API compatibility, we attempt Go-side parsing.
	__tmpPK := parsePrivateKey(keyBlock.Bytes)
	priv, err := __tmpPK.v0, __tmpPK.v1
	if err != nil {
		return __mrX509KeyPairPair{cert, err}
	}
	cert.PrivateKey = priv

	return __mrX509KeyPairPair{cert, nil}
}

type __mrTlsParsePrivateKeyPair struct {
	v0 crypto.PrivateKey
	v1 error
}

func parsePrivateKey(der []byte) __mrTlsParsePrivateKeyPair {
	// Try PKCS#8 first
	if key, err := x509.ParsePKCS8PrivateKey(der); err == nil {
		if pk, ok := key.(crypto.PrivateKey); ok {
			return __mrTlsParsePrivateKeyPair{pk, nil}
		}
	}
	// Try PKCS#1 (RSA)
	if key, err := x509.ParsePKCS1PrivateKey(der); err == nil {
		return __mrTlsParsePrivateKeyPair{key, nil}
	}
	// Try EC
	if key, err := x509.ParseECPrivateKey(der); err == nil {
		return __mrTlsParsePrivateKeyPair{key, nil}
	}
	return __mrTlsParsePrivateKeyPair{nil, errors.New("tls: failed to parse private key")}
}

// LoadX509KeyPair loads cert/key from files (via the synthetic os package).
func __mrLoadX509KeyPair(certFile, keyFile string) __mrLoadX509KeyPairPair {
	return loadX509KeyPairImpl(certFile, keyFile)
}

type __mrLoadX509KeyPairPair struct {
	v0 Certificate
	v1 error
}

func LoadX509KeyPair(certFile, keyFile string) (Certificate, error) {
	panic("crypto/tls: LoadX509KeyPair: multi-result desugar")
}

func loadX509KeyPairImpl(certFile, keyFile string) __mrLoadX509KeyPairPair {
	certPEMBlock, err := os.ReadFile(certFile)
	if err != nil {
		return __mrLoadX509KeyPairPair{Certificate{}, err}
	}
	keyPEMBlock, err := os.ReadFile(keyFile)
	if err != nil {
		return __mrLoadX509KeyPairPair{Certificate{}, err}
	}
	pair := x509KeyPairImpl(certPEMBlock, keyPEMBlock)
	return __mrLoadX509KeyPairPair{pair.v0, pair.v1}
}

// Conn is a TLS connection backed by Java SSLEngine.
type Conn struct {
	// engineHandle is an opaque handle to the Java SSLEngine state
	// (managed by TlsRuntime; not directly accessible from Go).
	engineHandle int64
	conn         net.Conn
	config       *Config
	isClient     bool
	// handshakeComplete tracks whether doHandshake finished.
	handshakeComplete bool
}

// Client creates a TLS client over conn. The handshake runs on Handshake().
func Client(conn net.Conn, config *Config) *Conn {
	if config == nil {
		config = &Config{}
	}
	c := &Conn{conn: conn, config: config, isClient: true}
	c.engineHandle = newClientEngineTrap(
		config.ServerName,
		config.InsecureSkipVerify,
		int64(config.MinVersion),
		int64(config.MaxVersion),
		encodeCipherSuites(config.CipherSuites),
	)
	return c
}

// Server creates a TLS server over conn. The handshake runs on Handshake().
func Server(conn net.Conn, config *Config) *Conn {
	if config == nil {
		config = &Config{}
	}
	c := &Conn{conn: conn, config: config, isClient: false}
	if len(config.Certificates) == 0 {
		return c
	}
	cert := &config.Certificates[0]
	__tmpKC := keyComponents(cert.PrivateKey)
	keyAlg, keyParams, err := __tmpKC.v0, __tmpKC.v1, __tmpKC.v2
	if err != nil {
		return c
	}
	c.engineHandle = newServerEngineTrap(
		encodeLengthPrefixed(cert.Certificate),
		keyAlg,
		keyParams,
		int64(config.MinVersion),
		int64(config.MaxVersion),
		encodeCipherSuites(config.CipherSuites),
	)
	return c
}

//go:linkname newClientEngineTrap graalgo.tls.newClientEngine
func newClientEngineTrap(serverName string, insecureSkipVerify bool, minVersion, maxVersion int64, cipherSuites []byte) int64

//go:linkname newServerEngineTrap graalgo.tls.newServerEngine
func newServerEngineTrap(chainDER []byte, keyAlg string, keyParams []byte, minVersion, maxVersion int64, cipherSuites []byte) int64

// encodeCipherSuites encodes []uint16 as big-endian bytes.
func encodeCipherSuites(suites []uint16) []byte {
	out := make([]byte, 2*len(suites))
	for i, s := range suites {
		out[2*i] = byte(s >> 8)
		out[2*i+1] = byte(s)
	}
	return out
}

// encodeLengthPrefixed encodes [][]byte as uint32-BE length + data items.
func encodeLengthPrefixed(items [][]byte) []byte {
	var out []byte
	for _, item := range items {
		n := len(item)
		out = append(out, byte(n>>24), byte(n>>16), byte(n>>8), byte(n))
		out = append(out, item...)
	}
	return out
}

// decodeLengthPrefixed decodes uint32-BE length + data items.
func decodeLengthPrefixed(buf []byte) [][]byte {
	var out [][]byte
	pos := 0
	for pos+4 <= len(buf) {
		n := int(buf[pos])<<24 | int(buf[pos+1])<<16 | int(buf[pos+2])<<8 | int(buf[pos+3])
		pos += 4
		if n < 0 || pos+n > len(buf) {
			break
		}
		out = append(out, buf[pos:pos+n])
		pos += n
	}
	return out
}

// keyComponents extracts the private-key material for the Java KeyManager.
// Returns (keyAlg, length-prefixed components, err).
// RSA: n, e, d, p, q | ECDSA: curveName, x, y, d | Ed25519: seed.
type __mrKeyComponentsPair struct {
	v0 string
	v1 []byte
	v2 error
}

func keyComponents(priv crypto.PrivateKey) __mrKeyComponentsPair {
	switch k := priv.(type) {
	case *rsa.PrivateKey:
		if len(k.Primes) < 2 {
			return __mrKeyComponentsPair{"", nil, errors.New("tls: RSA private key missing primes")}
		}
		eBytes := big.NewInt(int64(k.E)).Bytes()
		params := encodeLengthPrefixed([][]byte{
			k.N.Bytes(), eBytes, k.D.Bytes(),
			k.Primes[0].Bytes(), k.Primes[1].Bytes(),
		})
		return __mrKeyComponentsPair{"RSA", params, nil}
	case *ecdsa.PrivateKey:
		curveName := ""
		if k.Curve != nil {
			switch k.Curve.Params().Name {
			case "P-256":
				curveName = "secp256r1"
			case "P-384":
				curveName = "secp384r1"
			case "P-521":
				curveName = "secp521r1"
			}
		}
		if curveName == "" {
			return __mrKeyComponentsPair{"", nil, errors.New("tls: unsupported ECDSA curve")}
		}
		params := encodeLengthPrefixed([][]byte{
			[]byte(curveName), k.X.Bytes(), k.Y.Bytes(), k.D.Bytes(),
		})
		return __mrKeyComponentsPair{"ECDSA", params, nil}
	case ed25519.PrivateKey:
		if len(k) != 64 {
			return __mrKeyComponentsPair{"", nil, errors.New("tls: invalid Ed25519 private key")}
		}
		return __mrKeyComponentsPair{"Ed25519", encodeLengthPrefixed([][]byte{k[:32]}), nil}
	}
	return __mrKeyComponentsPair{"", nil, errors.New("tls: unsupported private key type")}
}

// Dial connects to a TLS server (loopback-only in M9).
func __mrDial(network, addr string, config *Config) __mrDialPair {
	return dialImpl(network, addr, config)
}

type __mrDialPair struct {
	v0 *Conn
	v1 error
}

func Dial(network, addr string, config *Config) (*Conn, error) {
	panic("crypto/tls: Dial: multi-result desugar")
}

func dialImpl(network, addr string, config *Config) __mrDialPair {
	if config == nil {
		return __mrDialPair{nil, errors.New("tls: nil Config")}
	}
	// Go semantics: default ServerName from the dial address hostname.
	if config.ServerName == "" {
		config.ServerName = hostnameFromAddr(addr)
	}
	rawConn, err := net.Dial(network, addr)
	if err != nil {
		return __mrDialPair{nil, err}
	}
	conn := Client(rawConn, config)
	if err := conn.Handshake(); err != nil {
		rawConn.Close()
		return __mrDialPair{nil, err}
	}
	return __mrDialPair{conn, nil}
}

// hostnameFromAddr strips the port from a host:port address.
func hostnameFromAddr(addr string) string {
	// IPv6 [host]:port or host:port.
	if len(addr) > 0 && addr[0] == '[' {
		for i := 1; i < len(addr); i++ {
			if addr[i] == ']' {
				return addr[1:i]
			}
		}
		return addr
	}
	for i := len(addr) - 1; i >= 0; i-- {
		if addr[i] == ':' {
			return addr[:i]
		}
	}
	return addr
}

// Dialer dials TLS connections given a configuration and a Dialer for the
// underlying connection.
type Dialer struct {
	// NetDialer is the optional dialer to use for the TLS connections'
	// underlying TCP connections.
	// A nil NetDialer is equivalent to the net.Dialer zero value.
	NetDialer *net.Dialer

	// Config is the TLS configuration to use for new connections.
	// A nil configuration is equivalent to the zero
	// configuration; see the documentation of Config for the
	// defaults.
	Config *Config
}

// Dial connects to the given network address and initiates a TLS
// handshake, returning the resulting TLS connection.
//
// The returned [Conn], if any, will always be of type *[Conn].
//
// Dial uses context.Background internally; to specify the context,
// use [Dialer.DialContext].
func (d *Dialer) Dial(network, addr string) (net.Conn, error) {
	panic("crypto/tls: Dialer.Dial: multi-result desugar")
}

func __mrDialerDial(d *Dialer, network, addr string) __mrDialerDialPair {
	return d.dialImpl(network, addr)
}

type __mrDialerDialPair struct {
	v0 net.Conn
	v1 error
}

func (d *Dialer) dialImpl(network, addr string) __mrDialerDialPair {
	p := d.dialContextImpl(context.Background(), network, addr)
	return __mrDialerDialPair{p.v0, p.v1}
}

func (d *Dialer) netDialer() *net.Dialer {
	if d.NetDialer != nil {
		return d.NetDialer
	}
	return new(net.Dialer)
}

// DialContext connects to the given network address and initiates a TLS
// handshake, returning the resulting TLS connection.
//
// The provided Context must be non-nil. If the context expires before
// the connection is complete, an error is returned. Once successfully
// connected, any expiration of the context will not affect the
// connection.
//
// The returned [Conn], if any, will always be of type *[Conn].
func (d *Dialer) DialContext(ctx context.Context, network, addr string) (net.Conn, error) {
	panic("crypto/tls: Dialer.DialContext: multi-result desugar")
}

func __mrDialerDialContext(d *Dialer, ctx context.Context, network, addr string) __mrDialerDialContextPair {
	return d.dialContextImpl(ctx, network, addr)
}

type __mrDialerDialContextPair struct {
	v0 net.Conn
	v1 error
}

func (d *Dialer) dialContextImpl(ctx context.Context, network, addr string) __mrDialerDialContextPair {
	if d.Config == nil {
		// Mirror DialWithDialer: nil config is an error in this synthetic impl.
		return __mrDialerDialContextPair{nil, errors.New("tls: nil Config")}
	}
	rawConn, err := d.netDialer().DialContext(ctx, network, addr)
	if err != nil {
		return __mrDialerDialContextPair{nil, err}
	}
	conn := Client(rawConn, d.Config)
	if err := conn.Handshake(); err != nil {
		rawConn.Close()
		return __mrDialerDialContextPair{nil, err}
	}
	return __mrDialerDialContextPair{conn, nil}
}

// DialWithDialer connects using a Dialer.
func __mrDialWithDialer(dialer *net.Dialer, network, addr string, config *Config) __mrDialWithDialerPair {
	return dialWithDialerImpl(dialer, network, addr, config)
}

type __mrDialWithDialerPair struct {
	v0 *Conn
	v1 error
}

func DialWithDialer(dialer *net.Dialer, network, addr string, config *Config) (*Conn, error) {
	panic("crypto/tls: DialWithDialer: multi-result desugar")
}

func dialWithDialerImpl(dialer *net.Dialer, network, addr string, config *Config) __mrDialWithDialerPair {
	if config == nil {
		return __mrDialWithDialerPair{nil, errors.New("tls: nil Config")}
	}
	if config.ServerName == "" {
		config.ServerName = hostnameFromAddr(addr)
	}
	rawConn, err := dialer.Dial(network, addr)
	if err != nil {
		return __mrDialWithDialerPair{nil, err}
	}
	conn := Client(rawConn, config)
	if err := conn.Handshake(); err != nil {
		rawConn.Close()
		return __mrDialWithDialerPair{nil, err}
	}
	return __mrDialWithDialerPair{conn, nil}
}

// Listen creates a TLS listener (loopback-only in M9).
func __mrListen(network, laddr string, config *Config) __mrListenPair {
	return listenImpl(network, laddr, config)
}

type __mrListenPair struct {
	v0 net.Listener
	v1 error
}

func Listen(network, laddr string, config *Config) (net.Listener, error) {
	panic("crypto/tls: Listen: multi-result desugar")
}

func listenImpl(network, laddr string, config *Config) __mrListenPair {
	if config == nil || len(config.Certificates) == 0 {
		return __mrListenPair{nil, errors.New("tls: neither Certificates, GetCertificate, nor GetConfigForClient set in Config")}
	}
	l, err := net.Listen(network, laddr)
	if err != nil {
		return __mrListenPair{nil, err}
	}
	return __mrListenPair{NewListener(l, config), nil}
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
	panic("crypto/tls.listener.Accept: unreachable (Appendix-M desugar did not fire)")
}

type __listenerAcceptPair struct {
	c   net.Conn
	err error
}

func __listenerAccept(l *listener) __listenerAcceptPair {
	c, err := l.Listener.Accept()
	if err != nil {
		return __listenerAcceptPair{nil, err}
	}
	return __listenerAcceptPair{Server(c, l.config), nil}
}

// Engine handshake status codes (from graalgo.tls.engineStatus).
const (
	engineFinished   = 0
	engineNeedWrap   = 1
	engineNeedUnwrap = 2
	engineNeedTask   = 3
)

//go:linkname beginHandshakeTrap graalgo.tls.beginHandshake
func beginHandshakeTrap(engineHandle int64) int64

//go:linkname engineStatusTrap graalgo.tls.engineStatus
func engineStatusTrap(engineHandle int64) int64

//go:linkname wrapTrap graalgo.tls.wrap
func wrapTrap(engineHandle int64, plaintext []byte) []byte

//go:linkname unwrapTrap graalgo.tls.unwrap
func unwrapTrap(engineHandle int64, netData []byte) []byte

//go:linkname hasPendingTrap graalgo.tls.hasPending
func hasPendingTrap(engineHandle int64) int64

//go:linkname runTasksTrap graalgo.tls.runTasks
func runTasksTrap(engineHandle int64) int64

//go:linkname peerCertificatesTrap graalgo.tls.peerCertificates
func peerCertificatesTrap(engineHandle int64) []byte

//go:linkname connectionStateTrap graalgo.tls.connectionState
func connectionStateTrap(engineHandle int64) int64

// Handshake performs the TLS handshake, driving the Java SSLEngine state
// machine: shuttling handshake records between the engine and the raw
// net.Conn until the engine reports FINISHED. Afterwards (client side,
// unless InsecureSkipVerify) the peer chain is verified in Go for exact
// Go verification semantics.
// NetConn returns the underlying network connection.
func (c *Conn) NetConn() net.Conn {
	return c.conn
}

func (c *Conn) Handshake() error {
	if c.engineHandle == 0 {
		return errors.New("tls: nil engine")
	}
	if c.handshakeComplete {
		return nil
	}
	if rc := beginHandshakeTrap(c.engineHandle); rc != 0 {
		return fmt.Errorf("tls: handshake failed (beginHandshake code %d)", rc)
	}
	tmp := make([]byte, 1<<14)
	for {
		st := engineStatusTrap(c.engineHandle)
		switch st {
		case engineFinished:
			c.handshakeComplete = true
			if err := c.verifyPeer(); err != nil {
				return err
			}
			return nil
		case engineNeedWrap:
			// The graalgo.tls.wrap trap drains the engine's pending
			// handshake flight internally (Java side, driven by output
			// production — e.g. ChangeCipherSpec followed by Finished in
			// TLS 1.3 — not by the reported status, which SSLEngine may
			// report prematurely). This loop therefore drives strictly
			// on engine status: one wrap per NEED_WRAP observation, and
			// the outer switch re-queries the status afterwards.
			out := wrapTrap(c.engineHandle, nil)
			if len(out) > 0 {
				if err := writeAll(c.conn, out); err != nil {
					return err
				}
			} else {
				// P3-M9 (x509): the engine can report NEED_WRAP while a
				// wrap produces no bytes (transitional state). Spinning
				// here starves the peer goroutine on a single worker
				// (--gmp=1): yield so it can make progress.
				time.Sleep(time.Millisecond)
			}
		case engineNeedUnwrap:
			// P3-M9 (x509): the engine may hold unconsumed input bytes
			// from a previous unwrap (buffered Java-side). Drain those
			// first — do NOT block on a network read when there is
			// already input waiting, or the handshake deadlocks.
			if hasPendingTrap(c.engineHandle) != 0 {
				_ = unwrapTrap(c.engineHandle, nil)
				continue
			}
			n, err := c.conn.Read(tmp)
			if err != nil {
				return err
			}
			if n == 0 {
				continue
			}
			_ = unwrapTrap(c.engineHandle, tmp[:n])
		case engineNeedTask:
			runTasksTrap(c.engineHandle)
		default:
			return fmt.Errorf("tls: handshake failed (status %d)", st)
		}
	}
}

// verifyPeer verifies the peer certificate chain after the handshake
// (client side, unless InsecureSkipVerify), using x509.Verify.
func (c *Conn) verifyPeer() error {
	if !c.isClient {
		return nil
	}
	if c.config != nil && c.config.InsecureSkipVerify {
		return nil
	}
	peerDERs := decodeLengthPrefixed(peerCertificatesTrap(c.engineHandle))
	if len(peerDERs) == 0 {
		return errors.New("tls: no peer certificates")
	}
	leaf, err := x509.ParseCertificate(peerDERs[0])
	if err != nil {
		return err
	}
	opts := x509.VerifyOptions{
		DNSName: c.config.ServerName,
	}
	if c.config != nil && c.config.RootCAs != nil {
		opts.Roots = c.config.RootCAs
	}
	if _, err := leaf.Verify(opts); err != nil {
		return fmt.Errorf("tls: failed to verify certificate: %w", err)
	}
	return nil
}

func writeAll(conn net.Conn, b []byte) error {
	for len(b) > 0 {
		n, err := conn.Write(b)
		if err != nil {
			return err
		}
		b = b[n:]
	}
	return nil
}

// ConnectionState returns the negotiated connection parameters.
func (c *Conn) ConnectionState() ConnectionState {
	packed := connectionStateTrap(c.engineHandle)
	cs := ConnectionState{
		Version:           uint16(packed >> 32),
		HandshakeComplete: c.handshakeComplete,
		CipherSuite:       uint16(packed),
	}
	if c.config != nil {
		cs.ServerName = c.config.ServerName
	}
	// Peer certificates.
	for _, der := range decodeLengthPrefixed(peerCertificatesTrap(c.engineHandle)) {
		if cert, err := x509.ParseCertificate(der); err == nil {
			cs.PeerCertificates = append(cs.PeerCertificates, cert)
		}
	}
	return cs
}

// Read reads decrypted application data via SSLEngine.unwrap.
func (c *Conn) Read(b []byte) (int, error) {
	panic("crypto/tls.Conn.Read: unreachable (Appendix-M desugar did not fire)")
}

type __tlsConnReadPair struct {
	n int
	err error
}

func __tlsConnRead(c *Conn, b []byte) __tlsConnReadPair {
	if c.engineHandle == 0 {
		return __tlsConnReadPair{0, errors.New("tls: nil engine")}
	}
	tmp := make([]byte, 1<<14)
	for {
		n, err := c.conn.Read(tmp)
		if err != nil {
			return __tlsConnReadPair{0, err}
		}
		if n == 0 {
			continue
		}
		app := unwrapTrap(c.engineHandle, tmp[:n])
		if app == nil {
			return __tlsConnReadPair{0, io.EOF}
		}
		if len(app) == 0 {
			// Handshake/control records only; read more.
			continue
		}
		m := copy(b, app)
		return __tlsConnReadPair{m, nil}
	}
}

// Write encrypts via SSLEngine.wrap and writes the records.
func (c *Conn) Write(b []byte) (int, error) {
	panic("crypto/tls.Conn.Write: unreachable (Appendix-M desugar did not fire)")
}

type __tlsConnWritePair struct {
	n int
	err error
}

func __tlsConnWrite(c *Conn, b []byte) __tlsConnWritePair {
	if c.engineHandle == 0 {
		return __tlsConnWritePair{0, errors.New("tls: nil engine")}
	}
	out := wrapTrap(c.engineHandle, b)
	if out == nil {
		return __tlsConnWritePair{0, errors.New("tls: wrap failed")}
	}
	if err := writeAll(c.conn, out); err != nil {
		return __tlsConnWritePair{0, err}
	}
	return __tlsConnWritePair{len(b), nil}
}

// Close closes the TLS connection.
//go:linkname tlsCloseTrap graalgo.tls.close
func tlsCloseTrap(engineHandle int64)

//go:linkname closeWriteTrap graalgo.tls.closeWrite
func closeWriteTrap(engineHandle int64) []byte

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
// CloseWrite shuts down the write side of the connection: it sends a
// close_notify alert. The connection can still be read from afterwards.
func (c *Conn) CloseWrite() error {
	if !c.handshakeComplete {
		return errors.New("tls: CloseWrite before handshake")
	}
	notify := closeWriteTrap(c.engineHandle)
	if notify == nil {
		return errors.New("tls: CloseWrite failed")
	}
	return writeAll(c.conn, notify)
}

// HandshakeContext runs the TLS handshake, or returns nil if the handshake
// is already complete. An already-canceled context is reported immediately;
// a context canceled while the handshake is in flight is observed once the
// handshake returns (the synthetic handshake loop performs blocking I/O
// with no cancellation points).
func (c *Conn) HandshakeContext(ctx context.Context) error {
	if c.handshakeComplete {
		return nil
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	return c.Handshake()
}

// OCSPResponse returns the OCSP status response stapled by the peer.
// The synthetic engine never requests OCSP stapling, so this is always nil.
func (c *Conn) OCSPResponse() []byte { return nil }

// VerifyHostname verifies the hostname against the peer certificate chain
// using x509 hostname semantics.
func (c *Conn) VerifyHostname(host string) error {
	if c.config != nil && c.config.InsecureSkipVerify {
		return nil
	}
	cs := c.ConnectionState()
	if len(cs.PeerCertificates) == 0 {
		return fmt.Errorf("tls: no peer certificates for hostname verification")
	}
	return cs.PeerCertificates[0].VerifyHostname(host)
}

// Ensure unsafe is imported for linkname (required by Go toolchain).
var _ = unsafe.Sizeof(0)
`
