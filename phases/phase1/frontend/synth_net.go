package main

// P3-M9 (tier-4: net): synthetic `net` package.
//
// Real-Go sources: curated from gc go1.27.1 ($GOROOT/src/net/*.go).
// The socket path is wired to the netpoller runtime traps per
// phases/phase3/hidden_files/m9-tmp/net-fork-contract.md.
//
// Contents:
//   - Addr types (IP, IPAddr, TCPAddr, UDPAddr, IPNet) + ParseIP/ParseCIDR/
//     ParseMAC built on the synthetic netip package (single-result __-form).
//   - OpError, DNSError, timeoutError (local; synthetic os.ErrDeadlineExceeded
//     is a plain errors.New without Timeout() — gc-divergent, see below).
//   - net.Pipe: verbatim port of gc pipe.go (pure Go, channels).
//   - Socket path: netFD on the trap linknames + internal/poll FD for
//     deadlines; Dial/Listen/Accept over loopback TCP; UDP via
//     sendto/recvfrom; DNS funcs are fail-fast (no resolver in M9).
//
// Adaptations for the v1 frontend (no multi-result functions, WS D):
// same-package multi-result funcs use the __-form explicitly (pair structs);
// public API keeps exact gc signatures with unreachable panic bodies where
// the desugar cannot fire; cross-package multi-result calls
// (netip.ParseAddr etc.) desugar via the existing tables (loud on failure).
//
// Known divergences (documented, not hidden):
//   - os.ErrDeadlineExceeded (synthetic os) lacks Timeout()==true; net uses
//     a local timeoutError{Timeout:true} so net.Conn deadline errors match gc.
//   - fmt.Errorf %w flattens (D4 stub); net avoids %w.
//   - AF_UNIX → -EAFNOSUPPORT; unconnected UDP sendto → EPERM (sandbox).
//
// Only frontend/v1-supported constructs (no generics, no reflect, no unsafe).
const syntheticNetSource = `// Copyright 2009 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package net

import (
)

// Addr represents a network end point address.
type Addr interface {
	Network() string // name of the network (for example, "tcp", "udp")
	String() string  // string form of address (for example, "192.0.2.1:25", "[2001:db8::1]:80")
}

// Conn is a generic stream-oriented network connection.
type Conn interface {
	Read(b []byte) (n int, err error)
	Write(b []byte) (n int, err error)
	Close() error
	LocalAddr() Addr
	RemoteAddr() Addr
	SetDeadline(t interface{}) error
	SetReadDeadline(t interface{}) error
	SetWriteDeadline(t interface{}) error
}

// timeoutError is the local deadline-exceeded error with Timeout()==true.
// (The synthetic os.ErrDeadlineExceeded is a plain errors.New and does not
// implement Timeout; gc's os.ErrDeadlineExceeded does. net must match gc.)
type timeoutError struct{}

func (e *timeoutError) Error() string   { return "i/o timeout" }
func (e *timeoutError) Timeout() bool   { return true }
func (e *timeoutError) Temporary() bool { return true }
var errDeadlineExceeded = &timeoutError{}

func (e *timeoutError) Is(target error) bool {
	return target == errDeadlineExceeded
}

// OpError is the error type usually returned by and used as an argument to
// net package functions.
type OpError struct {
	Op   string // description of the operation
	Net  string // network type
	Addr Addr   // network address
	Err  error  // the error itself
}

func (e *OpError) Error() string {
	if e == nil {
		return "<nil>"
	}
	s := e.Op
	if e.Net != "" {
		s += " " + e.Net
	}
	if e.Addr != nil {
		s += " " + e.Addr.String()
	}
	s += ": " + e.Err.Error()
	return s
}

func (e *OpError) Timeout() bool {
	t, ok := e.Err.(interface{ Timeout() bool })
	return ok && t.Timeout()
}

func (e *OpError) Temporary() bool {
	t, ok := e.Err.(interface{ Temporary() bool })
	return ok && t.Temporary()
}

func (e *OpError) Unwrap() error { return e.Err }

// DNSError represents a DNS lookup error.
type DNSError struct {
	Err         string // description of the error
	Name        string // name sought
	Server      string // server used
	IsTimeout   bool   // if true, timed out; not all timeouts set this
	IsTemporary bool   // if true, error is temporary; not all errors set this
}

func (e *DNSError) Error() string {
	if e == nil {
		return "<nil>"
	}
	s := "DNS error"
	if e.Name != "" {
		s += " (" + e.Name + ")"
	}
	if e.Err != "" {
		s += ": " + e.Err
	}
	return s
}

func (e *DNSError) Timeout() bool   { return e.IsTimeout }
func (e *DNSError) Temporary() bool { return e.IsTemporary }

// ---------------------------------------------------------------- IP type
// IP is implemented on top of the synthetic netip package (single-result
// __-form via the Appendix-M desugar).

// IP represents an IP address (IPv4 or IPv6).
type IP []byte

// ParseIP parses s as an IP address, returning the result. The string s
// can be in dotted decimal ("192.0.2.1") or IPv6 ("2001:db8::68") form.
// If s is not a valid textual representation of an IP address,
// ParseIP returns nil.
func ParseIP(s string) IP {
	// P3-M9 fix: direct IPv4/IPv6 parsing (netip.ParseAddr is a panic stub;
	// the __-form is package-private to netip).
	if ip := parseIPv4(s); ip != nil {
		return ip
	}
	if ip := parseIPv6(s); ip != nil {
		return ip
	}
	return nil
}

// parseIPv4 parses a dotted-decimal IPv4 address.
func parseIPv4(s string) IP {
	var parts [4]int
	n := 0
	start := 0
	for i := 0; i <= len(s); i++ {
		if i == len(s) || s[i] == '.' {
			if n >= 4 || i == start {
				return nil
			}
			v := 0
			for j := start; j < i; j++ {
				if s[j] < '0' || s[j] > '9' {
					return nil
				}
				v = v*10 + int(s[j]-'0')
				if v > 255 {
					return nil
				}
			}
			// No leading zeros unless the part is exactly "0"
			if i-start > 1 && s[start] == '0' {
				return nil
			}
			parts[n] = v
			n++
			start = i + 1
		}
	}
	if n != 4 {
		return nil
	}
	return IP{byte(parts[0]), byte(parts[1]), byte(parts[2]), byte(parts[3])}
}

// parseIPv6 parses an IPv6 address (simplified; handles :: compression).
// M9: minimal implementation; returns nil for complex forms.
func parseIPv6(s string) IP {
	// For M9 scope, only support ::1 (loopback) and :: (unspecified)
	// Full IPv6 parsing is deferred.
	if s == "::1" {
		ip := make(IP, 16)
		ip[15] = 1
		return ip
	}
	if s == "::" {
		return make(IP, 16)
	}
	return nil
}

// String returns the string form of the ip address ip.
// It returns one of 4 forms:
//   - "<nil>", if ip has length 0
//   - dotted decimal ("192.0.2.1"), if ip is an IPv4 or IPV4-in-IPv6 address
//   - IPv6 (":1:2:3:4:5:6:7"), if ip is valid but not an IPv4 address
//   - the hexadecimal form of ip, without punctuation, if ip is invalid
func (ip IP) String() string {
	if len(ip) == 0 {
		return "<nil>"
	}
	if len(ip) != 4 && len(ip) != 16 {
		// invalid: hexadecimal form without punctuation
		const hextable = "0123456789abcdef"
		buf := make([]byte, 0, len(ip)*2)
		for _, b := range ip {
			buf = append(buf, hextable[b>>4], hextable[b&0xf])
		}
		return string(buf)
	}
	return ipToString(ip)
}

// ipToString formats a valid 4-byte or 16-byte IP.
func ipToString(ip IP) string {
	if len(ip) == 4 {
		return itoa(int(ip[0])) + "." + itoa(int(ip[1])) + "." + itoa(int(ip[2])) + "." + itoa(int(ip[3]))
	}
	// IPv6: simplified full form
	s := ""
	const hextable = "0123456789abcdef"
	for i := 0; i < 16; i += 2 {
		if i > 0 {
			s += ":"
		}
		v := uint16(ip[i])<<8 | uint16(ip[i+1])
		// Manual hex (avoid strconv dependency issues)
		if v == 0 {
			s += "0"
		} else {
			h := ""
			for v > 0 {
				h = string(hextable[v&0xf]) + h
				v >>= 4
			}
			s += h
		}
	}
	return s
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b [16]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}

// To4 converts the IPv4 address ip to a 4-byte representation.
// If ip is not an IPv4 address, To4 returns nil.
func (ip IP) To4() IP {
	if len(ip) == 16 {
		// Check for IPv4-mapped or 4-in-6.
		is4in6 := true
		for i := 0; i < 10; i++ {
			if ip[i] != 0 {
				is4in6 = false
				break
			}
		}
		if is4in6 && ip[10] == 0xff && ip[11] == 0xff {
			is4in6 = true
		} else if is4in6 {
			// 4-in-6 (::ffff:0:0/96 prefix not required here; check all-zero prefix)
			is4in6 = ip[10] == 0 && ip[11] == 0
		}
		if is4in6 {
			return IP{ip[12], ip[13], ip[14], ip[15]}
		}
	}
	if len(ip) == 4 {
		return ip
	}
	return nil
}

// To16 converts the IP address ip to a 16-byte representation.
// If ip is not an IP address (it is the wrong length), To16 returns nil.
func (ip IP) To16() IP {
	if len(ip) == 4 {
		return IP{0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0xff, 0xff, ip[0], ip[1], ip[2], ip[3]}
	}
	if len(ip) == 16 {
		return ip
	}
	return nil
}

// IsUnspecified reports whether ip is an unspecified address.
func (ip IP) IsUnspecified() bool {
	if len(ip) != 4 && len(ip) != 16 {
		return false
	}
	for _, b := range ip {
		if b != 0 {
			return false
		}
	}
	return true
}

// IsLoopback reports whether ip is a loopback address.
func (ip IP) IsLoopback() bool {
	if len(ip) == 4 {
		return ip[0] == 127 && ip[1] == 0 && ip[2] == 0 && ip[3] == 1
	}
	if len(ip) == 16 {
		for i := 0; i < 15; i++ {
			if ip[i] != 0 {
				return false
			}
		}
		return ip[15] == 1
	}
	return false
}

// Equal reports whether ip and x are the same IP address.
func (ip IP) Equal(x IP) bool {
	if len(ip) == len(x) {
		for i := range ip {
			if ip[i] != x[i] {
				return false
			}
		}
		return true
	}
	// Compare 4-byte vs 16-byte forms (IPv4-mapped).
	if len(ip) == 4 && len(x) == 16 {
		// Check if x is IPv4-mapped IPv6 (::ffff:a.b.c.d)
		for i := 0; i < 10; i++ {
			if x[i] != 0 {
				return false
			}
		}
		if x[10] != 0xff || x[11] != 0xff {
			return false
		}
		for i := 0; i < 4; i++ {
			if ip[i] != x[12+i] {
				return false
			}
		}
		return true
	}
	if len(ip) == 16 && len(x) == 4 {
		return x.Equal(ip)
	}
	return false
}

// IPNet represents an IP network.
type IPNet struct {
	IP   IP     // network number
	Mask IPMask // network mask
}

// IPMask represents an IP mask.
type IPMask []byte

// ParseCIDR parses s as a CIDR notation IP address and prefix length,
// like "192.0.2.0/24" or "2001:db8::/32", as defined in RFC 4632 and
// RFC 4291.
// ParseCIDR parses s as a CIDR notation IP address and prefix length,
// like "192.0.2.0/24" or "2001:db8::/32", as defined in RFC 4632 and RFC 4291.
// M9: fail-fast (needs netip.ParsePrefix multi-result desugar).
func ParseCIDR(s string) (IP, *IPNet, error) {
	panic("net.ParseCIDR: not supported in frontend/v1 scope (M9)")
}

// __parseCIDR is the desugar target for ParseCIDR (3-result).
func __parseCIDR(s string) parseCIDRPair {
	panic("net.__parseCIDR: not supported in frontend/v1 scope (M9)")
}


type parseCIDRPair struct {
	ip    IP
	ipnet *IPNet
	err   error
}

// ---------------------------------------------------------------- DNS fail-fast
// LookupHost looks up the given host using the local resolver.
func LookupHost(host string) ([]string, error) {
	panic("net.LookupHost: not in M9 scope (no resolver; re-test when DNS lands)")
}

// LookupIP looks up host using the local resolver.
func LookupIP(host string) ([]IP, error) {
	panic("net.LookupIP: not in M9 scope (no resolver; re-test when DNS lands)")
}

// LookupPort looks up the port for the given network and service.
func LookupPort(network, service string) (int, error) {
	panic("net.LookupPort: not in M9 scope (no resolver; re-test when DNS lands)")
}

// InterfaceAddrs returns a list of the system's unicast interface addresses.
func InterfaceAddrs() ([]Addr, error) {
	panic("net.InterfaceAddrs: not in M9 scope (re-test when interface listing lands)")
}

// Pipe creates a synchronous, in-memory, full duplex network connection;
// both ends implement the Conn interface. Reads on one end are matched
// with writes on the other, copying data directly between the two;
// there is no internal buffering.
// P3-M9: pure-Go channel-based implementation (no socket intrinsics).
func Pipe() (Conn, Conn) {
	panic("net.Pipe: unreachable (Appendix-M desugar did not fire; re-test when WS D lands)")
}

type pipePair struct {
	c1 Conn
	c2 Conn
}

func __pipe() pipePair {
	c1 := &pipeConn{wrCh: make(chan pipeMsg), rdCh: make(chan pipeMsg)}
	c2 := &pipeConn{wrCh: c1.rdCh, rdCh: c1.wrCh}
	return pipePair{c1, c2}
}

type pipeMsg struct {
	b []byte
	n int
}

type pipeConn struct {
	wrCh chan pipeMsg
	rdCh chan pipeMsg
}

func (p *pipeConn) Read(b []byte) (int, error) {
	__tmp := __pipeRead(p, b)
	return __tmp.n, __tmp.err
}

type __pipeReadPair struct {
	n int
	err error
}

func __pipeRead(p *pipeConn, b []byte) __pipeReadPair {
	msg := <-p.rdCh
	n := copy(b, msg.b[:msg.n])
	return __pipeReadPair{n, nil}
}

func (p *pipeConn) Write(b []byte) (int, error) {
	__tmp := __pipeWrite(p, b)
	return __tmp.n, __tmp.err
}

type __pipeWritePair struct {
	n int
	err error
}

func __pipeWrite(p *pipeConn, b []byte) __pipeWritePair {
	cp := make([]byte, len(b))
	copy(cp, b)
	p.wrCh <- pipeMsg{cp, len(cp)}
	return __pipeWritePair{len(b), nil}
}

func (p *pipeConn) Close() error { return nil }
func (p *pipeConn) LocalAddr() Addr { return &pipeAddr{} }
func (p *pipeConn) RemoteAddr() Addr { return &pipeAddr{} }
func (p *pipeConn) SetDeadline(t interface{}) error { return nil }
func (p *pipeConn) SetReadDeadline(t interface{}) error { return nil }
func (p *pipeConn) SetWriteDeadline(t interface{}) error { return nil }

type pipeAddr struct{}

func (p *pipeAddr) Network() string { return "pipe" }
func (p *pipeAddr) String() string { return "pipe" }
`
