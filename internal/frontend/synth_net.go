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
//     sendto/recvfrom; DNS via the graalgo.net.lookupHost trap (JDK
//     java.net.InetAddress delegation; see the DNS section below).
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

import _ "unsafe" // for go:linkname
import "context"
import "errors"
import "internal/poll"
import "io"
import "strconv"
import "strings"
import "time"

// Linux errno numbers (net-fork-contract.md). Defined locally: the
// synthetic syscall package has a pre-existing io/fs export issue
// (P3-M9) that blocks its use from net.
const (
	errnoEAGAIN      = 11
	errnoEINPROGRESS = 115
	errnoECONNREFUSED = 111
	errnoEADDRINUSE  = 98
	errnoEINVAL      = 22
	errnoEBADF       = 9
)

// errno is a Linux error number.
type errno int64

func (e errno) Error() string { return "errno " + itoa(int(e)) }

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
	SetDeadline(t time.Time) error
	SetReadDeadline(t time.Time) error
	SetWriteDeadline(t time.Time) error
}

// timeoutError is the local deadline-exceeded error with Timeout()==true.
// (The synthetic os.ErrDeadlineExceeded is a plain errors.New and does not
// implement Timeout; gc's os.ErrDeadlineExceeded does. net must match gc.)
type timeoutError struct{}

func (e *timeoutError) Error() string   { return "i/o timeout" }
func (e *timeoutError) Timeout() bool   { return true }
func (e *timeoutError) Temporary() bool { return true }
var errDeadlineExceeded = &timeoutError{}

// errClosed exists just so that the docs for ErrClosed don't mention
// the internal package poll (gc 1.27.1, src/net/net.go).
var errClosed = poll.ErrNetClosing

// ErrClosed is the error returned by an I/O call on a network
// connection that has already been closed, or that is closed by
// another goroutine before the I/O is completed. This may be wrapped
// in another error, and should normally be tested using
// errors.Is(err, net.ErrClosed).
var ErrClosed error = errClosed

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

// parseIPv6 parses an IPv6 address, handling :: compression (gc parity for
// the forms the corpus exercises: full, compressed, IPv4-mapped).
func parseIPv6(s string) IP {
	// Strip a zone (%eth0); M9 keeps the zone out of the IP itself.
	if i := strings.LastIndexByte(s, '%'); i != -1 {
		s = s[:i]
	}
	// Split around :: (at most one).
	var head, tail string
	hasDouble := false
	if i := strings.Index(s, "::"); i != -1 {
		if strings.Index(s[i+2:], "::") != -1 {
			return nil // multiple ::
		}
		head, tail = s[:i], s[i+2:]
		hasDouble = true
	} else {
		head = s
	}
	__tmpHG := __parseIPv6Groups(head)
	if !__tmpHG.ok {
		return nil
	}
	hg := __tmpHG.groups
	__tmpTG := __parseIPv6Groups(tail)
	if !__tmpTG.ok {
		return nil
	}
	tg := __tmpTG.groups
	if hasDouble {
		// Total groups must be <= 8; pad zeros in the middle.
		if len(hg)+len(tg) > 8 {
			return nil
		}
		full := make([]uint16, 0, 8)
		full = append(full, hg...)
		for len(full) < 8-len(tg) {
			full = append(full, 0)
		}
		full = append(full, tg...)
		hg = full
	} else if len(hg) != 8 {
		return nil
	}
	ip := make(IP, 16)
	for i, g := range hg {
		ip[2*i] = byte(g >> 8)
		ip[2*i+1] = byte(g)
	}
	return ip
}

type parseIPv6GroupsPair struct {
	groups []uint16
	ok     bool
}

func __parseIPv6Groups(part string) parseIPv6GroupsPair {
	if part == "" {
		return parseIPv6GroupsPair{nil, true}
	}
	segs := strings.Split(part, ":")
	out := make([]uint16, 0, len(segs))
	for _, g := range segs {
		if g == "" {
			return parseIPv6GroupsPair{nil, false}
		}
		// Embedded IPv4 (e.g. ::ffff:192.0.2.1).
		if strings.Contains(g, ".") {
			ip4 := parseIPv4(g)
			if ip4 == nil {
				return parseIPv6GroupsPair{nil, false}
			}
			out = append(out, uint16(ip4[0])<<8|uint16(ip4[1]), uint16(ip4[2])<<8|uint16(ip4[3]))
			continue
		}
		if len(g) > 4 {
			return parseIPv6GroupsPair{nil, false}
		}
		var v uint16
		for i := 0; i < len(g); i++ {
			c := g[i]
			var d uint16
			switch {
			case c >= '0' && c <= '9':
				d = uint16(c - '0')
			case c >= 'a' && c <= 'f':
				d = uint16(c-'a') + 10
			case c >= 'A' && c <= 'F':
				d = uint16(c-'A') + 10
			default:
				return parseIPv6GroupsPair{nil, false}
			}
			v = v*16 + d
		}
		out = append(out, v)
	}
	return parseIPv6GroupsPair{out, true}
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
	// IPv6: gc-parity form with "::" zero compression (the longest run of
	// zero 16-bit groups, first run wins ties; a single zero group is not
	// compressed).
	var groups [8]uint16
	for i := 0; i < 8; i++ {
		groups[i] = uint16(ip[2*i])<<8 | uint16(ip[2*i+1])
	}
	// Find the longest run of zeros (length > 1 to compress).
	bestStart, bestLen := -1, 0
	for i := 0; i < 8; {
		if groups[i] != 0 {
			i++
			continue
		}
		j := i
		for j < 8 && groups[j] == 0 {
			j++
		}
		if j-i > bestLen {
			bestStart, bestLen = i, j-i
		}
		i = j
	}
	s := ""
	const hextable = "0123456789abcdef"
	appendGroup := func(v uint16) {
		if v == 0 {
			s += "0"
			return
		}
		h := ""
		for v > 0 {
			h = string(hextable[v&0xf]) + h
			v >>= 4
		}
		s += h
	}
	if bestLen > 1 {
		for i := 0; i < bestStart; i++ {
			if i > 0 {
				s += ":"
			}
			appendGroup(groups[i])
		}
		s += "::"
		for i := bestStart + bestLen; i < 8; i++ {
			if i > bestStart+bestLen {
				s += ":"
			}
			appendGroup(groups[i])
		}
		return s
	}
	for i := 0; i < 8; i++ {
		if i > 0 {
			s += ":"
		}
		appendGroup(groups[i])
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

// Mask returns the result of masking the IP address ip with mask.
// Ported from GOROOT net/ip.go (required by real crypto/x509 from disk;
// see networkNumberAndMask above).
func (ip IP) Mask(mask IPMask) IP {
	if len(mask) == netIPv6len && len(ip) == netIPv4len && __allFF(mask[:12]) {
		mask = mask[12:]
	}
	if len(mask) == netIPv4len && len(ip) == netIPv6len && __isV4InV6Prefix(ip[:12]) {
		ip = ip[12:]
	}
	n := len(ip)
	if n != len(mask) {
		return nil
	}
	out := make(IP, n)
	for i := 0; i < n; i++ {
		out[i] = ip[i] & mask[i]
	}
	return out
}

// __allFF reports whether b consists entirely of 0xff bytes.
func __allFF(b []byte) bool {
	for _, c := range b {
		if c != 0xff {
			return false
		}
	}
	return true
}

// __isV4InV6Prefix reports whether b is the 12-byte IPv4-in-IPv6 prefix
// (::ffff:0:0/96).
func __isV4InV6Prefix(b []byte) bool {
	if len(b) != 12 {
		return false
	}
	for i := 0; i < 10; i++ {
		if b[i] != 0 {
			return false
		}
	}
	return b[10] == 0xff && b[11] == 0xff
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

// networkNumberAndMask returns the network number and mask of n.
func networkNumberAndMask(n *IPNet) (ip IP, m IPMask) {
	if ip = n.IP.To4(); ip == nil {
		ip = n.IP
		if len(ip) != IPv6len {
			return nil, nil
		}
	}
	m = n.Mask
	switch len(m) {
	case IPv4len:
		if len(ip) != IPv4len {
			return nil, nil
		}
	case IPv6len:
		if len(ip) == IPv4len {
			m = m[12:]
		}
	default:
		return nil, nil
	}
	return
}

// Contains reports whether the network includes ip.
func (n *IPNet) Contains(ip IP) bool {
	nn, m := networkNumberAndMask(n)
	if x := ip.To4(); x != nil {
		ip = x
	}
	l := len(ip)
	if l != len(nn) {
		return false
	}
	for i := 0; i < l; i++ {
		if nn[i]&m[i] != ip[i]&m[i] {
			return false
		}
	}
	return true
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

type parseCIDRPair struct {
	ip    IP
	ipnet *IPNet
	err   error
}

// ---------------------------------------------------------------------------
// DNS resolution (P1-net remediation, 2026-10-18).
//
// DNS resolves through the JDK (java.net.InetAddress) via the
// graalgo.net.lookupHost linkname trap — the same delegation pattern as
// the socket traps below. Rationale: the JVM resolver consults the
// platform resolver (hosts file, DNS, caches) with correct IP-literal
// handling; reimplementing the DNS wire protocol in synthetic Go would
// duplicate the JDK and violate the prefer-JDK-delegation directive (see
// synth_crypto_tls.go). Wire format: the trap returns "" on failure
// (unknown host) or the "\n"-joined getHostAddress() literals on success;
// the Go side splits and re-parses them with ParseIP.
//
// gc parity notes:
//   - LookupHost("") / LookupIP("") reject before the trap: gc returns
//     *DNSError "no such host" without consulting the resolver (and the
//     JDK maps "" to localhost, which would be wrong here).
//   - IP literals never reach the trap (gc's lookupIPAddr fast path).
//   - Unknown host maps to *DNSError{Err: "no such host", Name: host}.
//   - __resolve*Addr filters by network family like gc's internetAddrList
//     (network ending in '4'/'6'); no suitable address is *AddrError
//     "no suitable address found", mirroring gc's filterAddrList.
//
// LookupPort (service name -> port via the service database) is not a DNS
// operation and stays out of scope.
// ---------------------------------------------------------------------------

//go:linkname netLookupHost graalgo.net.lookupHost
func netLookupHost(host string) string // "" on failure; "\n"-joined literal addresses on success

// LookupHost looks up the given host using the local resolver.
func LookupHost(host string) ([]string, error) {
	panic("net.LookupHost: unreachable (Appendix-M desugar did not fire)")
}

// LookupIP looks up host using the local resolver.
func LookupIP(host string) ([]IP, error) {
	panic("net.LookupIP: unreachable (Appendix-M desugar did not fire)")
}

// LookupPort looks up the port for the given network and service.
func LookupPort(network, service string) (int, error) {
	panic("net.LookupPort: not in M9 scope (no service database; re-test when it lands)")
}

type lookupHostPair struct {
	addrs []string
	err   error
}

type lookupIPPair struct {
	ips []IP
	err error
}

// __lookupHost implements LookupHost (desugar target).
func __lookupHost(host string) lookupHostPair {
	if host == "" {
		return lookupHostPair{nil, &DNSError{Err: "no such host", Name: host}}
	}
	if ip := ParseIP(host); ip != nil {
		return lookupHostPair{[]string{host}, nil}
	}
	addrs := __splitLookupAddrs(netLookupHost(host))
	if len(addrs) == 0 {
		return lookupHostPair{nil, &DNSError{Err: "no such host", Name: host}}
	}
	return lookupHostPair{addrs, nil}
}

// __lookupIP implements LookupIP (desugar target).
func __lookupIP(host string) lookupIPPair {
	if host == "" {
		return lookupIPPair{nil, &DNSError{Err: "no such host", Name: host}}
	}
	if ip := ParseIP(host); ip != nil {
		return lookupIPPair{[]IP{ip}, nil}
	}
	__tmp := __lookupHost(host)
	if __tmp.err != nil {
		return lookupIPPair{nil, __tmp.err}
	}
	ips := make([]IP, 0, len(__tmp.addrs))
	for _, a := range __tmp.addrs {
		if ip := ParseIP(a); ip != nil {
			ips = append(ips, ip)
		}
	}
	if len(ips) == 0 {
		// Defensive: the trap only emits getHostAddress() literals, so
		// every entry parses; a total failure is still "no such host".
		return lookupIPPair{nil, &DNSError{Err: "no such host", Name: host}}
	}
	return lookupIPPair{ips, nil}
}

// __splitLookupAddrs splits the trap's "\n"-joined address list, dropping
// empty entries. Pure Go (unit-testable under gc).
func __splitLookupAddrs(s string) []string {
	if s == "" {
		return nil
	}
	parts := strings.Split(s, "\n")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

// __filterResolveIP returns the first address matching the network's IP
// family (gc parity: internetAddrList's ipv4only/ipv6only filters), or nil
// when no address is suitable. Pure Go (unit-testable under gc).
func __filterResolveIP(network string, ips []IP) IP {
	var v4only, v6only bool
	if network != "" {
		switch network[len(network)-1] {
		case '4':
			v4only = true
		case '6':
			v6only = true
		}
	}
	for _, ip := range ips {
		switch {
		case v4only && ip.To4() == nil:
			continue
		case v6only && !(len(ip) == IPv6len && ip.To4() == nil):
			continue
		}
		return ip
	}
	return nil
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
	st := &pipeState{done: make(chan struct{}), mu: make(chan struct{}, 1)}
	st.mu <- struct{}{} // unlocked
	c1 := &pipeConn{wrCh: make(chan pipeMsg), rdCh: make(chan pipeMsg), st: st}
	c2 := &pipeConn{wrCh: c1.rdCh, rdCh: c1.wrCh, st: st}
	return pipePair{c1, c2}
}

// pipeState is shared between the two ends of a net.Pipe. Closing either
// end closes done (once); readers unblock with io.EOF and writers fail
// with io.ErrClosedPipe (gc parity). The data channels are never closed:
// close-then-send would panic, so the done channel carries the signal.
// mu is a channel mutex (token present = unlocked): sync.Mutex as a struct
// field is rejected by the frontend M2 scope.
type pipeState struct {
	done chan struct{}
	mu   chan struct{}
}

func (p *pipeConn) pipeClosed() bool {
	select {
	case <-p.st.done:
		return true
	default:
		return false
	}
}

type pipeMsg struct {
	b []byte
	n int
}

type pipeConn struct {
	wrCh chan pipeMsg
	rdCh chan pipeMsg
	st   *pipeState
	// Per-end deadlines (gc parity), guarded by st.mu.
	rdDeadline time.Time
	wrDeadline time.Time
}

func (p *pipeConn) Read(b []byte) (int, error) {
	__tmp := __pipeRead(p, b)
	return __tmp.n, __tmp.err
}

type __pipeReadPair struct {
	n   int
	err error
}

func __pipeRead(p *pipeConn, b []byte) __pipeReadPair {
	<-p.st.mu
	rdDeadline := p.rdDeadline
	p.st.mu <- struct{}{}
	// A nil timerCh blocks forever in the select below (no deadline).
	var timerCh <-chan time.Time
	if !rdDeadline.IsZero() {
		if d := time.Until(rdDeadline); d <= 0 {
			return __pipeReadPair{0, errDeadlineExceeded}
		} else {
			timerCh = time.After(d)
		}
	}
	select {
	case msg := <-p.rdCh:
		n := copy(b, msg.b[:msg.n])
		return __pipeReadPair{n, nil}
	case <-p.st.done:
		// Peer closed: deliver any message already queued, else EOF
		// (gc parity: read after close drains, then EOF).
		select {
		case msg := <-p.rdCh:
			n := copy(b, msg.b[:msg.n])
			return __pipeReadPair{n, nil}
		default:
			return __pipeReadPair{0, io.EOF}
		}
	case <-timerCh:
		return __pipeReadPair{0, errDeadlineExceeded}
	}
}

func (p *pipeConn) Write(b []byte) (int, error) {
	__tmp := __pipeWrite(p, b)
	return __tmp.n, __tmp.err
}

type __pipeWritePair struct {
	n   int
	err error
}

func __pipeWrite(p *pipeConn, b []byte) __pipeWritePair {
	// A write racing with Close may still land (the data channel is never
	// closed); only a write that starts after done is closed fails. This
	// is panic-free and hang-free; the imprecision (racing write succeeds
	// instead of ErrClosedPipe) is benign.
	if p.pipeClosed() {
		return __pipeWritePair{0, io.ErrClosedPipe}
	}
	<-p.st.mu
	wrDeadline := p.wrDeadline
	p.st.mu <- struct{}{}
	var timerCh <-chan time.Time
	if !wrDeadline.IsZero() {
		if d := time.Until(wrDeadline); d <= 0 {
			return __pipeWritePair{0, errDeadlineExceeded}
		} else {
			timerCh = time.After(d)
		}
	}
	cp := make([]byte, len(b))
	copy(cp, b)
	select {
	case p.wrCh <- pipeMsg{cp, len(cp)}:
		return __pipeWritePair{len(b), nil}
	case <-timerCh:
		return __pipeWritePair{0, errDeadlineExceeded}
	}
}

func (p *pipeConn) Close() error {
	<-p.st.mu
	defer func() { p.st.mu <- struct{}{} }()
	select {
	case <-p.st.done:
		return nil // idempotent: already closed by either end
	default:
		close(p.st.done)
		return nil
	}
}
func (p *pipeConn) LocalAddr() Addr { return &pipeAddr{} }
func (p *pipeConn) RemoteAddr() Addr { return &pipeAddr{} }

func (p *pipeConn) SetDeadline(t time.Time) error {
	if err := p.SetReadDeadline(t); err != nil {
		return err
	}
	return p.SetWriteDeadline(t)
}

func (p *pipeConn) SetReadDeadline(t time.Time) error {
	tt := t
	<-p.st.mu
	p.rdDeadline = tt
	p.st.mu <- struct{}{}
	return nil
}

func (p *pipeConn) SetWriteDeadline(t time.Time) error {
	tt := t
	<-p.st.mu
	p.wrDeadline = tt
	p.st.mu <- struct{}{}
	return nil
}

type pipeAddr struct{}

func (p *pipeAddr) Network() string { return "pipe" }
func (p *pipeAddr) String() string { return "pipe" }


// ---------------------------------------------------------------------------
// Socket address types + Dial/Listen (P3-M9 tier-4: loopback TCP/UDP).
// Wired to the graalgo.net.* traps + internal/poll per
// phases/phase3/hidden_files/m9-tmp/net-fork-contract.md.
// ---------------------------------------------------------------------------

// Listener is a generic network listener for stream-oriented protocols.
type Listener interface {
	Accept() (Conn, error)
	Close() error
	Addr() Addr
}

// PacketConn is a generic packet-oriented network connection.
type PacketConn interface {
	ReadFrom(b []byte) (n int, addr Addr, err error)
	WriteTo(b []byte, addr Addr) (n int, err error)
	Close() error
	LocalAddr() Addr
	SetDeadline(t time.Time) error
	SetReadDeadline(t time.Time) error
	SetWriteDeadline(t time.Time) error
}

// IPAddr represents the address of an IP end point.
type IPAddr struct {
	IP   IP
	Zone string // IPv6 scoped addressing zone
}

func (a *IPAddr) Network() string { return "ip" }

func (a *IPAddr) String() string {
	if a == nil {
		return "<nil>"
	}
	ip := ipEmptyString(a.IP)
	if ip == "" {
		return ""
	}
	return ip
}

// TCPAddr represents the address of a TCP end point.
type TCPAddr struct {
	IP   IP
	Port int
	Zone string // IPv6 scoped addressing zone
}

func (a *TCPAddr) Network() string { return "tcp" }

func (a *TCPAddr) String() string {
	if a == nil {
		return "<nil>"
	}
	ip := ipEmptyString(a.IP)
	if a.Zone != "" {
		return JoinHostPort(ip+"%"+a.Zone, itoa(a.Port))
	}
	return JoinHostPort(ip, itoa(a.Port))
}

// UDPAddr represents the address of a UDP end point.
type UDPAddr struct {
	IP   IP
	Port int
	Zone string // IPv6 scoped addressing zone
}

func (a *UDPAddr) Network() string { return "udp" }

func (a *UDPAddr) String() string {
	if a == nil {
		return "<nil>"
	}
	ip := ipEmptyString(a.IP)
	if a.Zone != "" {
		return JoinHostPort(ip+"%"+a.Zone, itoa(a.Port))
	}
	return JoinHostPort(ip, itoa(a.Port))
}

func ipEmptyString(ip IP) string {
	if len(ip) == 0 {
		return ""
	}
	return ip.String()
}

// JoinHostPort combines host and port into a network address string.
func JoinHostPort(host, port string) string {
	// If host contains a colon, it is an IPv6 literal and needs brackets
	// (unless already bracketed).
	if strings.Contains(host, ":") {
		if strings.HasPrefix(host, "[") {
			return host + ":" + port
		}
		return "[" + host + "]:" + port
	}
	return host + ":" + port
}

// SplitHostPort splits a network address of the form "host:port",
// "host%zone:port", "[host]:port" or "[host%zone]:port" into host and port.
func SplitHostPort(hostport string) (host, port string, err error) {
	panic("net.SplitHostPort: unreachable (Appendix-M desugar did not fire)")
}

type splitHostPortPair struct {
	host string
	port string
	err  error
}

func __splitHostPort(hostport string) splitHostPortPair {
	// gc's SplitHostPort (net/ipsock.go), adapted: no multi-result.
	const errMissingPort = "missing port in address"
	const errTooManyColons = "too many colons in address"
	addrErr := func(addr string, why string) splitHostPortPair {
		return splitHostPortPair{"", "", &AddrError{Err: why, Addr: addr}}
	}
	host := hostport
	var port string

	colon := strings.LastIndexByte(host, ':')
	if colon == -1 {
		return addrErr(hostport, errMissingPort)
	}
	if i := strings.LastIndexByte(host, ']'); i != -1 {
		// Bracketed IPv6: [host]:port or [host%zone]:port.
		if i != colon-1 {
			// Colon after ']' but not immediately: malformed.
			if strings.Contains(host[i+1:], ":") {
				return addrErr(hostport, errTooManyColons)
			}
			return addrErr(hostport, errMissingPort)
		}
		host = host[1:i]
		port := hostport[colon+1:]
		if port == "" {
			return addrErr(hostport, errMissingPort)
		}
		return splitHostPortPair{host, port, nil}
	}
	// Unbracketed: host:port. If host contains a colon, it is an
	// unbracketed IPv6 literal without port (or too many colons).
	if strings.Contains(host[:colon], ":") {
		return addrErr(hostport, errTooManyColons)
	}
	port = host[colon+1:]
	if port == "" {
		return addrErr(hostport, errMissingPort)
	}
	host = host[:colon]
	return splitHostPortPair{host, port, nil}
}

// AddrError records an address parse error.
type AddrError struct {
	Err  string
	Addr string
}

func (e *AddrError) Error() string {
	if e == nil {
		return "<nil>"
	}
	s := e.Err
	if e.Addr != "" {
		s += " (" + e.Addr + ")"
	}
	return s
}

func (e *AddrError) Timeout() bool   { return false }
func (e *AddrError) Temporary() bool { return false }

// ---------------------------------------------------------------------------
// Socket trap bindings.
// ---------------------------------------------------------------------------

//go:linkname netSocket graalgo.net.socket
func netSocket(domain, typ, proto int64) int64 // cookie or -errno

//go:linkname netBind graalgo.net.bind
func netBind(cookie int64, addr []byte) int64 // 0 or -errno

//go:linkname netListenSock graalgo.net.listen
func netListenSock(cookie int64, backlog int64) int64 // 0 or -errno

//go:linkname netAccept graalgo.net.accept
func netAccept(cookie int64) int64 // new cookie or -errno (incl. -EAGAIN)

//go:linkname netConnect graalgo.net.connect
func netConnect(cookie int64, addr []byte) int64 // 0, -EINPROGRESS, or -errno

//go:linkname netSetsockopt graalgo.net.setsockopt
func netSetsockopt(cookie, level, opt, val int64) int64

//go:linkname netGetsockopt graalgo.net.getsockopt
func netGetsockopt(cookie, level, opt int64) int64 // value or -errno

//go:linkname netGetsockname graalgo.net.getsockname
func netGetsockname(cookie int64, buf []byte) int64 // n+1 or -errno

//go:linkname netGetpeername graalgo.net.getpeername
func netGetpeername(cookie int64, buf []byte) int64 // n+1 or -errno

//go:linkname netSendto graalgo.net.sendto
func netSendto(cookie int64, buf []byte, addr []byte) int64 // n+1 or -errno

//go:linkname netRecvfrom graalgo.net.recvfrom
func netRecvfrom(cookie int64, buf []byte, addr []byte) int64 // n+1 or -errno

//go:linkname netSockRead graalgo.os.read
func netSockRead(h int64, b []byte) int64 // n+1 (0=EOF) or -errno

//go:linkname netSockWrite graalgo.os.write
func netSockWrite(h int64, b []byte) int64 // n+1 or -errno

//go:linkname netSockClose graalgo.os.close
func netSockClose(h int64) int64 // 0 or -errno

// ---------------------------------------------------------------------------
// Netpoller linknames (internal/poll.*). The net package drives the
// poller directly by cookie via linknames, avoiding a synthetic->synthetic
// import of internal/poll (whose FD struct contains sync.Mutex, rejected
// by the frontend M2 scope as a struct field).
// ---------------------------------------------------------------------------

//go:linkname netPollOpen internal/poll.runtime_pollOpen
func netPollOpen(fd uintptr) uintptr // pollDesc cookie; 0 = failure

//go:linkname netPollClose internal/poll.runtime_pollClose
func netPollClose(ctx uintptr)

//go:linkname netPollWait internal/poll.runtime_pollWait
func netPollWait(ctx uintptr, mode int) int // 0=no error, 1=closing, 2=timeout, 3=not pollable

//go:linkname netPollUnblock internal/poll.runtime_pollUnblock
func netPollUnblock(ctx uintptr)

//go:linkname netPollSetDeadline internal/poll.runtime_pollSetDeadline
func netPollSetDeadline(ctx uintptr, d int64, mode int)

// netPollCtxs maps socket cookies to pollDesc cookies.
var netPollCtxs = make(map[int64]uintptr)

func netPollInit(cookie int64) error {
	ctx := netPollOpen(uintptr(cookie))
	if ctx == 0 {
		return errors.New("net: poll open failed")
	}
	netPollCtxs[cookie] = ctx
	return nil
}

func netPollCloseCookie(cookie int64) {
	if ctx, ok := netPollCtxs[cookie]; ok {
		netPollUnblock(ctx)
		delete(netPollCtxs, cookie)
		netPollClose(ctx)
	}
}

func netPollWaitRead(cookie int64) error {
	ctx, ok := netPollCtxs[cookie]
	if !ok {
		return errors.New("net: invalid poll cookie")
	}
	switch netPollWait(ctx, 'r') {
	case 0:
		return nil
	case 1:
		return errors.New("use of closed network connection")
	case 2:
		return errDeadlineExceeded
	default:
		return errors.New("net: not pollable")
	}
}

func netPollWaitWrite(cookie int64) error {
	ctx, ok := netPollCtxs[cookie]
	if !ok {
		return errors.New("net: invalid poll cookie")
	}
	switch netPollWait(ctx, 'w') {
	case 0:
		return nil
	case 1:
		return errors.New("use of closed network connection")
	case 2:
		return errDeadlineExceeded
	default:
		return errors.New("net: not pollable")
	}
}

func netPollSetDeadlineCookie(cookie int64, d int64, mode int) {
	if ctx, ok := netPollCtxs[cookie]; ok {
		netPollSetDeadline(ctx, d, mode)
	}
}

// errEOF is the EOF error for socket reads (matches io.EOF's message;
// the net package avoids importing io to sidestep an io/fs export issue).
var errEOF = errors.New("EOF")

// Sockaddr wire format (net-fork-contract.md): byte 0 = family;
// AF_INET (2): [2, portHi, portLo, a0..a3]; AF_INET6 (10):
// [10, portHi, portLo, a0..a15, scopeHi..scopeLo] (23 bytes).
func encodeSockaddr(ip IP, port int) []byte {
	if ip4 := ip.To4(); ip4 != nil {
		b := make([]byte, 7)
		b[0] = 2
		b[1] = byte(port >> 8)
		b[2] = byte(port)
		copy(b[3:], ip4)
		return b
	}
	ip16 := ip.To16()
	if ip16 == nil {
		ip16 = make([]byte, 16)
	}
	b := make([]byte, 23)
	b[0] = 10
	b[1] = byte(port >> 8)
	b[2] = byte(port)
	copy(b[3:], ip16)
	return b
}

type decodeSockaddrPair struct {
	ip   IP
	port int
}

func decodeSockaddr(b []byte) decodeSockaddrPair {
	if len(b) < 7 {
		return decodeSockaddrPair{nil, 0}
	}
	port := int(b[1])<<8 | int(b[2])
	switch b[0] {
	case 2:
		return decodeSockaddrPair{IP{b[3], b[4], b[5], b[6]}, port}
	case 10:
		if len(b) < 23 {
			return decodeSockaddrPair{nil, 0}
		}
		ip := make(IP, 16)
		copy(ip, b[3:19])
		return decodeSockaddrPair{ip, port}
	}
	return decodeSockaddrPair{nil, 0}
}

// netFD is a socket cookie registered with the netpoller.
// The pollDesc cookie lives in netPollCtxs (keyed by socket cookie);
// net drives the poller via linknames (no synthetic->synthetic import
// of internal/poll, whose FD struct the frontend M2 scope rejects).
type netFD struct {
	cookie int64
	net    string
	laddr  Addr
	raddr  Addr
}

type newNetFDPair struct {
	fd  *netFD
	err error
}

func newNetFD(cookie int64, net string) newNetFDPair {
	if err := netPollInit(cookie); err != nil {
		netSockClose(cookie)
		return newNetFDPair{nil, &OpError{Op: "socket", Net: net, Err: err}}
	}
	return newNetFDPair{&netFD{cookie: cookie, net: net}, nil}
}

func (fd *netFD) close() error {
	netPollCloseCookie(fd.cookie)
	if r := netSockClose(fd.cookie); r < 0 {
		return &OpError{Op: "close", Net: fd.net, Err: errno(-r)}
	}
	return nil
}

// mapPollErr converts a poll wait error to a net error.
func (fd *netFD) mapPollErr(op string, err error) error {
	if te, ok := err.(interface{ Timeout() bool }); ok && te.Timeout() {
		return &OpError{Op: op, Net: fd.net, Addr: fd.raddr, Err: errDeadlineExceeded}
	}
	return &OpError{Op: op, Net: fd.net, Addr: fd.raddr, Err: err}
}

func (fd *netFD) read(b []byte) readPair {
	for {
		r := netSockRead(fd.cookie, b)
		if r == 0 {
			return readPair{0, errEOF}
		}
		if r < 0 {
			errnoVal := errno(-r)
			if errnoVal == errnoEAGAIN {
				if werr := netPollWaitRead(fd.cookie); werr != nil {
					return readPair{0, fd.mapPollErr("read", werr)}
				}
				continue
			}
			return readPair{0, &OpError{Op: "read", Net: fd.net, Addr: fd.raddr, Err: errnoVal}}
		}
		n := int(r - 1)
		if n == 0 && len(b) > 0 {
			// 0-byte read on a nonblocking socket: EAGAIN (contract).
			if werr := netPollWaitRead(fd.cookie); werr != nil {
				return readPair{0, fd.mapPollErr("read", werr)}
			}
			continue
		}
		return readPair{n, nil}
	}
}

func (fd *netFD) write(b []byte) writePair {
	if len(b) == 0 {
		return writePair{0, nil}
	}
	for {
		r := netSockWrite(fd.cookie, b)
		if r < 0 {
			errnoVal := errno(-r)
			if errnoVal == errnoEAGAIN {
				if werr := netPollWaitWrite(fd.cookie); werr != nil {
					return writePair{0, fd.mapPollErr("write", werr)}
				}
				continue
			}
			return writePair{0, &OpError{Op: "write", Net: fd.net, Addr: fd.raddr, Err: errnoVal}}
		}
		n := int(r - 1)
		if n == 0 {
			// 0-byte write on a nonblocking socket: EAGAIN (contract).
			if werr := netPollWaitWrite(fd.cookie); werr != nil {
				return writePair{0, fd.mapPollErr("write", werr)}
			}
			continue
		}
		return writePair{n, nil}
	}
}

// getSockAddr queries getsockname/getpeername and decodes the result.
func (fd *netFD) getSockAddr(peer bool) Addr {
	buf := make([]byte, 32)
	var r int64
	if peer {
		r = netGetpeername(int64(fd.cookie), buf)
	} else {
		r = netGetsockname(int64(fd.cookie), buf)
	}
	if r <= 0 {
		return nil
	}
	__tmpSA := decodeSockaddr(buf[:r-1])
	ip, port := __tmpSA.ip, __tmpSA.port
	if strings.HasPrefix(fd.net, "tcp") {
		return &TCPAddr{IP: ip, Port: port}
	}
	return &UDPAddr{IP: ip, Port: port}
}

// ---------------------------------------------------------------------------
// TCP.
// ---------------------------------------------------------------------------

// TCPConn is a TCP network connection.
type TCPConn struct {
	fd *netFD
}

func (c *TCPConn) Read(b []byte) (int, error) {
	panic("net.TCPConn.Read: unreachable (Appendix-M desugar did not fire)")
}

func (c *TCPConn) Write(b []byte) (int, error) {
	panic("net.TCPConn.Write: unreachable (Appendix-M desugar did not fire)")
}

func (c *TCPConn) Close() error                       { return c.fd.close() }
func (c *TCPConn) LocalAddr() Addr                    { return c.fd.laddr }
func (c *TCPConn) RemoteAddr() Addr                   { return c.fd.raddr }
func (c *TCPConn) SetDeadline(t time.Time) error    { return c.fd.setDeadline(t, 'r'+'w') }
func (c *TCPConn) SetReadDeadline(t time.Time) error  { return c.fd.setDeadline(t, 'r') }
func (c *TCPConn) SetWriteDeadline(t time.Time) error { return c.fd.setDeadline(t, 'w') }

func (fd *netFD) setDeadline(t time.Time, mode int) error {
	var d int64
	if !t.IsZero() {
		d = int64(time.Until(t))
		if d == 0 {
			d = -1
		}
	}
	if ctx, ok := netPollCtxs[fd.cookie]; ok {
		netPollSetDeadline(ctx, d, mode)
	}
	return nil
}

type readPair struct {
	n   int
	err error
}

type writePair struct {
	n   int
	err error
}

func __tcpConnRead(c *TCPConn, b []byte) readPair {
	return c.fd.read(b)
}

func __tcpConnWrite(c *TCPConn, b []byte) writePair {
	// gc's TCPConn.Write loops until all bytes are written.
	var nn int
	for nn < len(b) {
		__tmpW := c.fd.write(b[nn:])
		nn += __tmpW.n
		if __tmpW.err != nil {
			return writePair{nn, __tmpW.err}
		}
		if __tmpW.n == 0 {
			break
		}
	}
	return writePair{nn, nil}
}

// TCPListener is a TCP network listener.
type TCPListener struct {
	fd *netFD
}

func (l *TCPListener) Accept() (Conn, error) {
	panic("net.TCPListener.Accept: unreachable (Appendix-M desugar did not fire)")
}

func (l *TCPListener) Close() error { return l.fd.close() }
func (l *TCPListener) Addr() Addr   { return l.fd.laddr }

type acceptPair struct {
	c   Conn
	err error
}

func __tcpListenerAccept(l *TCPListener) acceptPair {
	for {
		nc := netAccept(int64(l.fd.cookie))
		if nc < 0 {
			errnoVal := errno(-nc)
			if errnoVal == errnoEAGAIN {
				if werr := netPollWaitRead(l.fd.cookie); werr != nil {
					return acceptPair{nil, l.fd.mapPollErr("accept", werr)}
				}
				continue
			}
			return acceptPair{nil, &OpError{Op: "accept", Net: l.fd.net, Addr: l.fd.laddr, Err: errnoVal}}
		}
		__tmpNFD := newNetFD(nc, l.fd.net)
		if __tmpNFD.err != nil {
			return acceptPair{nil, __tmpNFD.err}
		}
		fd := __tmpNFD.fd
		fd.laddr = l.fd.laddr
		fd.raddr = fd.getSockAddr(true)
		return acceptPair{&TCPConn{fd: fd}, nil}
	}
}

// Dial connects to the address on the named network.
func Dial(network, address string) (Conn, error) {
	panic("net.Dial: unreachable (Appendix-M desugar did not fire)")
}

// DialTimeout acts like Dial but takes a timeout.
func DialTimeout(network, address string, timeout time.Duration) (Conn, error) {
	panic("net.DialTimeout: unreachable (Appendix-M desugar did not fire)")
}

// Listen announces on the local network address.
func Listen(network, address string) (Listener, error) {
	panic("net.Listen: unreachable (Appendix-M desugar did not fire)")
}

// Dialer dials connections. Timeout is honored via DialTimeout; the
// remaining fields are accepted for API compatibility with net.Dialer.
type Dialer struct {
	Timeout       time.Duration
	Deadline      time.Time
	LocalAddr     Addr
	DualStack     bool
	FallbackDelay time.Duration
	KeepAlive     time.Duration
}

// Dial connects to the address on the named network.
func (d *Dialer) Dial(network, address string) (Conn, error) {
	panic("net.Dialer.Dial: unreachable (Appendix-M desugar did not fire)")
}

// DialContext connects to the address on the named network using the
// provided context. (real-nethttp: needed by crypto/tls.Dialer.)
func (d *Dialer) DialContext(ctx context.Context, network, address string) (Conn, error) {
	panic("net.Dialer.DialContext: unreachable (Appendix-M desugar did not fire)")
}

// __dialerDialContext is the Appendix M desugar target behind
// (*Dialer).DialContext: the real body, returning the dialPair.
func __dialerDialContext(d *Dialer, ctx context.Context, network, address string) dialPair {
	// Context cancellation is not wired to the dial timeout in this
	// synthetic implementation; the context is accepted for API
	// compatibility. A nil context is equivalent to context.Background.
	if d.Timeout != 0 {
		return __dialTimeout(network, address, d.Timeout)
	}
	return __dial(network, address)
}

// __dialerDial is the Appendix M desugar target behind (*Dialer).Dial:
// the real body, returning the dialPair. (The 2-result method cannot be
// emitted directly under WS D, and its body calls the 2-result
// package-level Dial/DialTimeout, which cannot appear in return position.)
func __dialerDial(d *Dialer, network, address string) dialPair {
	if d.Timeout != 0 {
		return __dialTimeout(network, address, d.Timeout)
	}
	return __dial(network, address)
}

type dialPair struct {
	c   Conn
	err error
}

type listenPair struct {
	l   Listener
	err error
}

func __dial(network, address string) dialPair {
	switch network {
	case "tcp", "tcp4", "tcp6":
		__tmp := __resolveTCPAddr(network, address)
		if __tmp.err != nil {
			return dialPair{nil, __tmp.err}
		}
		return __dialTCPAddr(network, __tmp.addr)
	default:
		return dialPair{nil, &OpError{Op: "dial", Net: network, Err: errors.New("unknown network " + network)}}
	}
}

func __dialTimeout(network, address string, timeout time.Duration) dialPair {
	// M9: implement via deadline on the connect wait. For loopback the
	// connect completes immediately; the deadline path is exercised by
	// the refused/timeout corpus legs.
	deadline := time.Now().Add(timeout)
	__tmp := __dialWithDeadline(network, address, deadline)
	return __tmp
}

func __dialWithDeadline(network, address string, deadline time.Time) dialPair {
	switch network {
	case "tcp", "tcp4", "tcp6":
		__tmp := __resolveTCPAddr(network, address)
		if __tmp.err != nil {
			return dialPair{nil, __tmp.err}
		}
		return __dialTCPAddrDeadline(network, __tmp.addr, deadline)
	default:
		return dialPair{nil, &OpError{Op: "dial", Net: network, Err: errors.New("unknown network " + network)}}
	}
}

func __dialTCPAddr(network string, raddr *TCPAddr) dialPair {
	return __dialTCPAddrDeadline(network, raddr, time.Time{})
}

func __dialTCPAddrDeadline(network string, raddr *TCPAddr, deadline time.Time) dialPair {
	family := int64(2) // AF_INET
	if raddr.IP.To4() == nil {
		family = int64(10) // AF_INET6
	}
	cookie := netSocket(family, 1, 0) // SOCK_STREAM
	if cookie < 0 {
		return dialPair{nil, &OpError{Op: "dial", Net: network, Addr: raddr, Err: errno(-cookie)}}
	}
	__tmpNFD2 := newNetFD(cookie, network)
	if __tmpNFD2.err != nil {
		return dialPair{nil, __tmpNFD2.err}
	}
	fd := __tmpNFD2.fd
	// disarm clears the connect deadline; nil when not armed. The fail
	// path must disarm BEFORE fd.close() drops the pollDesc — the old
	// deferred netPollSetDeadline(ctx, 0) ran after close and threw
	// "unknown pollDesc ctx" (wave3 net/03 red, 2026-10-12).
	var disarm func()
	fail := func(opErr error) dialPair {
		if disarm != nil {
			disarm()
			disarm = nil
		}
		fd.close()
		return dialPair{nil, opErr}
	}
	// Arm the connect deadline if set.
	if !deadline.IsZero() {
		d := int64(time.Until(deadline))
		if d == 0 {
			d = -1
		}
		if ctx, ok := netPollCtxs[fd.cookie]; ok {
			netPollSetDeadline(ctx, d, 'r'+'w')
			disarm = func() {
				// The fd may already be closed (pollDesc dropped);
				// only disarm a live mapping. Cookies are never
				// reused, so a present entry is this fd's ctx.
				if ctx2, ok := netPollCtxs[fd.cookie]; ok {
					netPollSetDeadline(ctx2, 0, 'r'+'w')
				}
			}
			defer func() {
				if disarm != nil {
					disarm()
				}
			}()
		}
	}
	sa := encodeSockaddr(raddr.IP, raddr.Port)
	r := netConnect(cookie, sa)
	if r < 0 {
		errnoVal := errno(-r)
		if errnoVal != errnoEINPROGRESS {
			return fail(&OpError{Op: "dial", Net: network, Addr: raddr, Err: errnoVal})
		}
		// EINPROGRESS: wait for writability, then check SO_ERROR.
		if werr := netPollWaitWrite(fd.cookie); werr != nil {
			return fail(fd.mapPollErr("dial", werr))
		}
		soerr := netGetsockopt(cookie, 1, 4) // SOL_SOCKET, SO_ERROR
		if soerr < 0 {
			return fail(&OpError{Op: "dial", Net: network, Addr: raddr, Err: errno(-soerr)})
		}
		if soerr != 0 {
			return fail(&OpError{Op: "dial", Net: network, Addr: raddr, Err: errno(soerr)})
		}
	}
	fd.laddr = fd.getSockAddr(false)
	fd.raddr = raddr
	return dialPair{&TCPConn{fd: fd}, nil}
}

func __listen(network, address string) listenPair {
	switch network {
	case "tcp", "tcp4", "tcp6":
		__tmp := __resolveTCPAddr(network, address)
		if __tmp.err != nil {
			return listenPair{nil, __tmp.err}
		}
		return __listenTCP(network, __tmp.addr)
	default:
		return listenPair{nil, &OpError{Op: "listen", Net: network, Err: errors.New("unknown network " + network)}}
	}
}

func __listenTCP(network string, laddr *TCPAddr) listenPair {
	family := int64(2)
	if laddr.IP.To4() == nil {
		family = int64(10)
	}
	cookie := netSocket(family, 1, 0)
	if cookie < 0 {
		return listenPair{nil, &OpError{Op: "listen", Net: network, Addr: laddr, Err: errno(-cookie)}}
	}
	__tmpNFD3 := newNetFD(cookie, network)
	if __tmpNFD3.err != nil {
		return listenPair{nil, __tmpNFD3.err}
	}
	fd := __tmpNFD3.fd
	fail := func(opErr error) listenPair {
		fd.close()
		return listenPair{nil, opErr}
	}
	// SO_REUSEADDR for rapid rebind in tests.
	netSetsockopt(cookie, 1, 2, 1)
	sa := encodeSockaddr(laddr.IP, laddr.Port)
	if r := netBind(cookie, sa); r < 0 {
		return fail(&OpError{Op: "listen", Net: network, Addr: laddr, Err: errno(-r)})
	}
	if r := netListenSock(cookie, 128); r < 0 {
		return fail(&OpError{Op: "listen", Net: network, Addr: laddr, Err: errno(-r)})
	}
	fd.laddr = fd.getSockAddr(false)
	if fd.laddr == nil {
		fd.laddr = laddr
	}
	return listenPair{&TCPListener{fd: fd}, nil}
}

// ---------------------------------------------------------------------------
// UDP.
// ---------------------------------------------------------------------------

// UDPConn is a UDP network connection.
type UDPConn struct {
	fd *netFD
}

func (c *UDPConn) Read(b []byte) (int, error) {
	panic("net.UDPConn.Read: unreachable (Appendix-M desugar did not fire)")
}

func (c *UDPConn) Write(b []byte) (int, error) {
	panic("net.UDPConn.Write: unreachable (Appendix-M desugar did not fire)")
}

func (c *UDPConn) ReadFrom(b []byte) (int, Addr, error) {
	panic("net.UDPConn.ReadFrom: unreachable (Appendix-M desugar did not fire)")
}

func (c *UDPConn) WriteTo(b []byte, addr Addr) (int, error) {
	panic("net.UDPConn.WriteTo: unreachable (Appendix-M desugar did not fire)")
}

func (c *UDPConn) Close() error                     { return c.fd.close() }
func (c *UDPConn) LocalAddr() Addr                  { return c.fd.laddr }
func (c *UDPConn) SetDeadline(t time.Time) error   { return c.fd.setDeadline(t, 'r'+'w') }
func (c *UDPConn) SetReadDeadline(t time.Time) error  { return c.fd.setDeadline(t, 'r') }
func (c *UDPConn) SetWriteDeadline(t time.Time) error { return c.fd.setDeadline(t, 'w') }

type readFromPair struct {
	n    int
	addr Addr
	err  error
}

func __udpConnRead(c *UDPConn, b []byte) readPair {
	// Connected UDP read: recvfrom with nil addr.
	for {
		r := netRecvfrom(int64(c.fd.cookie), b, nil)
		if r < 0 {
			errnoVal := errno(-r)
			if errnoVal == errnoEAGAIN {
				if werr := netPollWaitRead(c.fd.cookie); werr != nil {
					return readPair{0, c.fd.mapPollErr("read", werr)}
				}
				continue
			}
			return readPair{0, &OpError{Op: "read", Net: c.fd.net, Err: errnoVal}}
		}
		n := int(r - 1)
		if n == 0 && len(b) > 0 {
			if werr := netPollWaitRead(c.fd.cookie); werr != nil {
				return readPair{0, c.fd.mapPollErr("read", werr)}
			}
			continue
		}
		return readPair{n, nil}
	}
}

func __udpConnWrite(c *UDPConn, b []byte) writePair {
	// Connected UDP write goes through the stream write path.
	return c.fd.write(b)
}

func __udpConnReadFrom(c *UDPConn, b []byte) readFromPair {
	addrBuf := make([]byte, 32)
	for {
		r := netRecvfrom(int64(c.fd.cookie), b, addrBuf)
		if r < 0 {
			errnoVal := errno(-r)
			if errnoVal == errnoEAGAIN {
				if werr := netPollWaitRead(c.fd.cookie); werr != nil {
					return readFromPair{0, nil, c.fd.mapPollErr("read", werr)}
				}
				continue
			}
			return readFromPair{0, nil, &OpError{Op: "read", Net: c.fd.net, Err: errnoVal}}
		}
		// The addr buffer is separate; r-1 is the datagram length.
		// Re-query the peer address length via a second call is not
		// possible; decode from addrBuf directly. The trap writes the
		// wire format; decode the family to know the length.
		n := int(r - 1)
		if n == 0 && len(b) > 0 {
			if werr := netPollWaitRead(c.fd.cookie); werr != nil {
				return readFromPair{0, nil, c.fd.mapPollErr("read", werr)}
			}
			continue
		}
		var raddr Addr
		if len(addrBuf) >= 7 {
			__tmpSA2 := decodeSockaddr(addrBuf)
			raddr = &UDPAddr{IP: __tmpSA2.ip, Port: __tmpSA2.port}
		}
		return readFromPair{n, raddr, nil}
	}
}

func __udpConnWriteTo(c *UDPConn, b []byte, addr Addr) writePair {
	uaddr, ok := addr.(*UDPAddr)
	if !ok {
		return writePair{0, &OpError{Op: "write", Net: c.fd.net, Err: errors.New("net: WriteTo with non-UDPAddr")}}
	}
	sa := encodeSockaddr(uaddr.IP, uaddr.Port)
	r := netSendto(int64(c.fd.cookie), b, sa)
	if r < 0 {
		return writePair{0, &OpError{Op: "write", Net: c.fd.net, Addr: addr, Err: errno(-r)}}
	}
	return writePair{int(r - 1), nil}
}

// ListenPacket announces on the local network address.
func ListenPacket(network, address string) (PacketConn, error) {
	panic("net.ListenPacket: unreachable (Appendix-M desugar did not fire)")
}

type listenPacketPair struct {
	c   PacketConn
	err error
}

func __listenPacket(network, address string) listenPacketPair {
	switch network {
	case "udp", "udp4", "udp6":
		__tmp := __resolveUDPAddr(network, address)
		if __tmp.err != nil {
			return listenPacketPair{nil, __tmp.err}
		}
		return __listenUDP(network, __tmp.addr)
	default:
		return listenPacketPair{nil, &OpError{Op: "listen", Net: network, Err: errors.New("unknown network " + network)}}
	}
}

func __listenUDP(network string, laddr *UDPAddr) listenPacketPair {
	family := int64(2)
	if laddr.IP.To4() == nil {
		family = int64(10)
	}
	cookie := netSocket(family, 2, 0) // SOCK_DGRAM
	if cookie < 0 {
		return listenPacketPair{nil, &OpError{Op: "listen", Net: network, Addr: laddr, Err: errno(-cookie)}}
	}
	__tmpNFD4 := newNetFD(cookie, network)
	if __tmpNFD4.err != nil {
		return listenPacketPair{nil, __tmpNFD4.err}
	}
	fd := __tmpNFD4.fd
	sa := encodeSockaddr(laddr.IP, laddr.Port)
	if r := netBind(cookie, sa); r < 0 {
		fd.close()
		return listenPacketPair{nil, &OpError{Op: "listen", Net: network, Addr: laddr, Err: errno(-r)}}
	}
	fd.laddr = fd.getSockAddr(false)
	if fd.laddr == nil {
		fd.laddr = laddr
	}
	return listenPacketPair{&UDPConn{fd: fd}, nil}
}

// ---------------------------------------------------------------------------
// Address resolution (IP literals resolve directly; DNS names resolve via
// the graalgo.net.lookupHost trap — see the DNS section above).
// ---------------------------------------------------------------------------

// ResolveIPAddr returns an IP address for the given network and address.
func ResolveIPAddr(network, address string) (*IPAddr, error) {
	panic("net.ResolveIPAddr: unreachable (Appendix-M desugar did not fire)")
}

// ResolveTCPAddr returns a TCP address for the given network and address.
func ResolveTCPAddr(network, address string) (*TCPAddr, error) {
	panic("net.ResolveTCPAddr: unreachable (Appendix-M desugar did not fire)")
}

// ResolveUDPAddr returns a UDP address for the given network and address.
func ResolveUDPAddr(network, address string) (*UDPAddr, error) {
	panic("net.ResolveUDPAddr: unreachable (Appendix-M desugar did not fire)")
}

type resolveIPAddrPair struct {
	ip  *IPAddr
	err error
}

type resolveTCPAddrPair struct {
	addr *TCPAddr
	err  error
}

type resolveUDPAddrPair struct {
	addr *UDPAddr
	err  error
}

func __resolveIPAddr(network, address string) resolveIPAddrPair {
	// gc parity (internetAddrList): an empty address resolves to the
	// zero IPAddr without consulting the resolver.
	if address == "" {
		return resolveIPAddrPair{&IPAddr{}, nil}
	}
	ip := ParseIP(address)
	if ip == nil {
		__tmp := __lookupIP(address)
		if __tmp.err != nil {
			return resolveIPAddrPair{nil, __tmp.err}
		}
		ip = __filterResolveIP(network, __tmp.ips)
		if ip == nil {
			return resolveIPAddrPair{nil, &AddrError{Err: "no suitable address found", Addr: address}}
		}
	}
	return resolveIPAddrPair{&IPAddr{IP: ip}, nil}
}

func __resolveTCPAddr(network, address string) resolveTCPAddrPair {
	__tmp := __splitHostPort(address)
	if __tmp.err != nil {
		return resolveTCPAddrPair{nil, __tmp.err}
	}
	host, port := __tmp.host, __tmp.port
	p, err := strconv.Atoi(port)
	if err != nil {
		return resolveTCPAddrPair{nil, &AddrError{Err: "unknown port", Addr: "tcp/" + port}}
	}
	var ip IP
	if host != "" {
		ip = ParseIP(host)
		if ip == nil {
			__tmpIP := __lookupIP(host)
			if __tmpIP.err != nil {
				return resolveTCPAddrPair{nil, __tmpIP.err}
			}
			ip = __filterResolveIP(network, __tmpIP.ips)
			if ip == nil {
				return resolveTCPAddrPair{nil, &AddrError{Err: "no suitable address found", Addr: host}}
			}
		}
	}
	return resolveTCPAddrPair{&TCPAddr{IP: ip, Port: p}, nil}
}

func __resolveUDPAddr(network, address string) resolveUDPAddrPair {
	__tmp := __splitHostPort(address)
	if __tmp.err != nil {
		return resolveUDPAddrPair{nil, __tmp.err}
	}
	host, port := __tmp.host, __tmp.port
	p, err := strconv.Atoi(port)
	if err != nil {
		return resolveUDPAddrPair{nil, &AddrError{Err: "unknown port", Addr: "udp/" + port}}
	}
	var ip IP
	if host != "" {
		ip = ParseIP(host)
		if ip == nil {
			__tmpIP := __lookupIP(host)
			if __tmpIP.err != nil {
				return resolveUDPAddrPair{nil, __tmpIP.err}
			}
			ip = __filterResolveIP(network, __tmpIP.ips)
			if ip == nil {
				return resolveUDPAddrPair{nil, &AddrError{Err: "no suitable address found", Addr: host}}
			}
		}
	}
	return resolveUDPAddrPair{&UDPAddr{IP: ip, Port: p}, nil}
}

// ---------------------------------------------------------------------------
// ParseMAC and real ParseCIDR.
// ---------------------------------------------------------------------------

// HardwareAddr represents a physical hardware address.
type HardwareAddr []byte

func (a HardwareAddr) String() string {
	if len(a) == 0 {
		return ""
	}
	const hextable = "0123456789abcdef"
	buf := make([]byte, 0, len(a)*3-1)
	for i, b := range a {
		if i > 0 {
			buf = append(buf, ':')
		}
		buf = append(buf, hextable[b>>4], hextable[b&0xf])
	}
	return string(buf)
}

// ParseMAC parses s as an IEEE 802 MAC-48, EUI-48, EUI-64, or a 20-octet
// IP over InfiniBand link-layer address.
func ParseMAC(s string) (HardwareAddr, error) {
	panic("net.ParseMAC: unreachable (Appendix-M desugar did not fire)")
}

type parseMACPair struct {
	hw  HardwareAddr
	err error
}

func __parseMAC(s string) parseMACPair {
	bad := func() parseMACPair {
		return parseMACPair{nil, &AddrError{Err: "invalid MAC address", Addr: s}}
	}
	if len(s) < 14 {
		return bad()
	}
	// Only the common xx:xx:xx:xx:xx:xx and xx-xx-xx-xx-xx-xx forms in M9.
	sep := ""
	if strings.Contains(s, ":") {
		sep = ":"
	} else if strings.Contains(s, "-") {
		sep = "-"
	} else {
		return bad()
	}
	parts := strings.Split(s, sep)
	if len(parts) != 6 && len(parts) != 8 && len(parts) != 20 {
		return bad()
	}
	hw := make(HardwareAddr, len(parts))
	for i, p := range parts {
		if len(p) != 2 {
			return bad()
		}
		__tmpHB := parseHexByte(p)
		if !__tmpHB.ok {
			return bad()
		}
		hw[i] = __tmpHB.v
	}
	return parseMACPair{hw, nil}
}

type parseHexBytePair struct {
	v  byte
	ok bool
}

func parseHexByte(s string) parseHexBytePair {
	if len(s) != 2 {
		return parseHexBytePair{0, false}
	}
	__tmpH1 := hexVal(s[0])
	__tmpH2 := hexVal(s[1])
	if !__tmpH1.ok || !__tmpH2.ok {
		return parseHexBytePair{0, false}
	}
	return parseHexBytePair{__tmpH1.v<<4 | __tmpH2.v, true}
}

func hexVal(c byte) parseHexBytePair {
	switch {
	case c >= '0' && c <= '9':
		return parseHexBytePair{c - '0', true}
	case c >= 'a' && c <= 'f':
		return parseHexBytePair{c - 'a' + 10, true}
	case c >= 'A' && c <= 'F':
		return parseHexBytePair{c - 'A' + 10, true}
	}
	return parseHexBytePair{0, false}
}

// __parseCIDR is the real desugar target for ParseCIDR (replaces the M9
// fail-fast): parses "192.0.2.0/24" or "2001:db8::/32".
func __parseCIDR(s string) parseCIDRPair {
	i := strings.LastIndexByte(s, '/')
	if i < 0 {
		return parseCIDRPair{nil, nil, &AddrError{Err: "invalid CIDR address", Addr: s}}
	}
	addr, mask := s[:i], s[i+1:]
	ip := ParseIP(addr)
	if ip == nil {
		return parseCIDRPair{nil, nil, &AddrError{Err: "invalid CIDR address", Addr: s}}
	}
	n, err := strconv.Atoi(mask)
	if err != nil {
		return parseCIDRPair{nil, nil, &AddrError{Err: "invalid CIDR address", Addr: s}}
	}
	var m IPMask
	if ip4 := ip.To4(); ip4 != nil {
		if n < 0 || n > 32 {
			return parseCIDRPair{nil, nil, &AddrError{Err: "invalid CIDR address", Addr: s}}
		}
		m = CIDRMask(n, 32)
		ip = ip4
	} else {
		if n < 0 || n > 128 {
			return parseCIDRPair{nil, nil, &AddrError{Err: "invalid CIDR address", Addr: s}}
		}
		m = CIDRMask(n, 128)
		ip = ip.To16()
	}
	// Mask the IP to get the network address.
	masked := make(IP, len(ip))
	for i := range ip {
		masked[i] = ip[i] & m[i]
	}
	ipnet := &IPNet{IP: masked, Mask: m}
	return parseCIDRPair{ip, ipnet, nil}
}

// IPv4Mask returns the IP mask (in 4-byte form) of the IPv4 mask
// a.b.c.d (gc 1.27.1 net/ip.go exact).
// P4 (driver-harden): was absent; real-world packages (e.g. pflag's
// ParseIPv4Mask) call net.IPv4Mask.
func IPv4Mask(a, b, c, d byte) IPMask {
	p := make(IPMask, netIPv4len)
	p[0] = a
	p[1] = b
	p[2] = c
	p[3] = d
	return p
}

// CIDRMask returns an IPMask consisting of 'ones' 1 bits
// followed by 0s up to a total length of 'bits' bits.
func CIDRMask(ones, bits int) IPMask {
	if bits != 8*netIPv4len && bits != 8*netIPv6len {
		return nil
	}
	if ones < 0 || ones > bits {
		return nil
	}
	l := bits / 8
	mask := make(IPMask, l)
	n := uint(ones)
	for i := 0; i < l; i++ {
		if n >= 8 {
			mask[i] = 0xff
			n -= 8
			continue
		}
		mask[i] = ^byte(0xff >> n)
		n = 0
	}
	return mask
}

const (
	netIPv4len = 4
	netIPv6len = 16
)

// IPv4len and IPv6len are the byte lengths of IPv4 and IPv6 addresses.
// Exported names matching GOROOT net (required by real crypto/x509 from disk).
const (
	IPv4len = netIPv4len
	IPv6len = netIPv6len
)

func (n *IPNet) String() string {
	if n == nil {
		return "<nil>"
	}
	__tmpMS := __ipMaskSize(n.Mask)
	return n.IP.String() + "/" + itoa(__tmpMS.ones)
}

type maskSizePair struct {
	ones int
	bits int
}

// Size returns the number of leading one bits and total bits in the mask.
func (m IPMask) Size() (ones, bits int) {
	panic("net.IPMask.Size: unreachable (Appendix-M desugar did not fire)")
}

func __ipMaskSize(m IPMask) maskSizePair {
	var ones int
	for i := 0; i < len(m); i++ {
		if m[i] == 0xff {
			ones += 8
			continue
		}
		for m[i]&0x80 != 0 {
			ones++
			m[i] <<= 1
		}
		break
	}
	return maskSizePair{ones, len(m) * 8}
}

// String returns the hexadecimal form of m (gc 1.27.1 net/ip.go exact).
// P4 (driver-harden): was absent; real-world packages (e.g. pflag's
// ipMaskValue.String) call net.IPMask.String.
func (m IPMask) String() string {
	if len(m) == 0 {
		return "<nil>"
	}
	const hextable = "0123456789abcdef"
	buf := make([]byte, 0, len(m)*2)
	for _, b := range m {
		buf = append(buf, hextable[b>>4], hextable[b&0xf])
	}
	return string(buf)
}

// ---------------------------------------------------------------------------
// Interface enumeration (P3-M9 module-probe lane: net.Interface/Interfaces,
// required by third-party packages such as github.com/google/uuid for v1
// node IDs). Backed by the graalgo.net.interfaces host trap
// (java.net.NetworkInterface). M9 scope: the Addrs/MulticastAddrs methods
// are absent (InterfaceAddrs stays out of scope per its note above).
// ---------------------------------------------------------------------------

// Flags are network interface flags (gc parity: net.Flags bit values).
type Flags uint

const (
	FlagUp Flags = 1 << iota
	FlagBroadcast
	FlagLoopback
	FlagPointToPoint
	FlagMulticast
)

// Interface describes a network interface (gc parity: net.Interface,
// fields used across the stdlib and third-party packages).
type Interface struct {
	Index        int
	MTU          int
	Name         string
	HardwareAddr HardwareAddr
	Flags        Flags
}

// errnoENOMEM: the interfaces trap reports it when the blob does not fit.
const errnoENOMEM = 12

//go:linkname netInterfaces graalgo.net.interfaces
func netInterfaces(buf []byte) int64 // bytes written +1, or -errno

// Interfaces returns the system's network interfaces.
func Interfaces() ([]Interface, error) {
	panic("net.Interfaces: unreachable (Appendix-M desugar did not fire)")
}

type interfacesPair struct {
	ifs []Interface
	err error
}

func __interfaces() interfacesPair {
	buf := make([]byte, 4096)
	for {
		r := netInterfaces(buf)
		if r == -errnoENOMEM {
			if len(buf) >= 1<<20 {
				return interfacesPair{nil, &OpError{Op: "interfaces", Err: errors.New("interface list too large")}}
			}
			buf = make([]byte, 2*len(buf))
			continue
		}
		if r <= 0 {
			return interfacesPair{nil, &OpError{Op: "interfaces", Err: errno(-r)}}
		}
		dec := decodeInterfaces(buf[:r-1])
		if !dec.ok {
			return interfacesPair{nil, &OpError{Op: "interfaces", Err: errors.New("malformed interface list")}}
		}
		return interfacesPair{dec.ifs, nil}
	}
}

// interfacesDecode is the single-result decode output (the v1 frontend
// has no multi-result functions, WS D).
type interfacesDecode struct {
	ifs []Interface
	ok  bool
}

// decodeInterfaces parses the graalgo.net.interfaces blob: int32 LE count,
// then per interface int32 LE index, mtu, nameLen+name bytes,
// hwLen+hw bytes, flags.
func decodeInterfaces(b []byte) interfacesDecode {
	rd := &ifaceReader{b: b}
	n := rd.int32()
	if n < 0 {
		return interfacesDecode{nil, false}
	}
	ifs := make([]Interface, 0, n)
	for i := int32(0); i < n; i++ {
		idx := rd.int32()
		mtu := rd.int32()
		name := rd.bytes()
		hw := rd.bytes()
		flags := rd.int32()
		if !rd.ok() {
			return interfacesDecode{nil, false}
		}
		ifs = append(ifs, Interface{
			Index:        int(idx),
			MTU:          int(mtu),
			Name:         string(name),
			HardwareAddr: HardwareAddr(hw),
			Flags:        Flags(flags),
		})
	}
	if !rd.ok() || len(rd.b) != 0 {
		return interfacesDecode{nil, false}
	}
	return interfacesDecode{ifs, true}
}

type ifaceReader struct {
	b []byte
}

func (r *ifaceReader) ok() bool { return r.b != nil }

func (r *ifaceReader) int32() int32 {
	if len(r.b) < 4 {
		r.b = nil
		return 0
	}
	v := int32(r.b[0]) | int32(r.b[1])<<8 | int32(r.b[2])<<16 | int32(r.b[3])<<24
	r.b = r.b[4:]
	return v
}

func (r *ifaceReader) bytes() []byte {
	n := r.int32()
	if n < 0 || int(n) > len(r.b) {
		r.b = nil
		return nil
	}
	v := r.b[:n]
	r.b = r.b[n:]
	return v
}
`
