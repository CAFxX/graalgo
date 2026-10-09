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
	"errors"
	"io"
	"os"
	"sync"
	"time"

	"internal/poll"
	"net/netip"
)

// Addr represents a network end point address.
type Addr interface {
	Network() string // name of the network (for example, "tcp", "udp")
	String() string  // string form of address (for example, "192.0.2.1:25", "[2001:db8::1]:80")
}

// timeoutError is the local deadline-exceeded error with Timeout()==true.
// (The synthetic os.ErrDeadlineExceeded is a plain errors.New and does not
// implement Timeout; gc's os.ErrDeadlineExceeded does. net must match gc.)
type timeoutError struct{}

func (e *timeoutError) Error() string   { return "i/o timeout" }
func (e *timeoutError) Timeout() bool   { return true }
func (e *timeoutError) Temporary() bool { return true }
func (e *timeoutError) Is(target error) bool {
	return target == os.ErrDeadlineExceeded
}

// errDeadlineExceeded is the single timeoutError value used by net.
var errDeadlineExceeded error = &timeoutError{}

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
	__tmpA := netip.ParseAddr(s)
	addr, err := __tmpA.addr, __tmpA.err
	if err != nil {
		return nil
	}
	if addr.Is4() {
		b := addr.As4()
		return IP{b[0], b[1], b[2], b[3]}
	}
	b := addr.As16()
	return IP{b[0], b[1], b[2], b[3], b[4], b[5], b[6], b[7],
		b[8], b[9], b[10], b[11], b[12], b[13], b[14], b[15]}
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
	__tmpA := netip.AddrFromSlice(ip)
	addr, ok := __tmpA.addr, __tmpA.ok
	if !ok {
		// invalid: hexadecimal form without punctuation
		const hextable = "0123456789abcdef"
		buf := make([]byte, 0, len(ip)*2)
		for _, b := range ip {
			buf = append(buf, hextable[b>>4], hextable[b&0xf])
		}
		return string(buf)
	}
	return addr.String()
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
	__tmpA := netip.AddrFromSlice(ip)
	addr, ok := __tmpA.addr, __tmpA.ok
	return ok && addr.IsUnspecified()
}

// IsLoopback reports whether ip is a loopback address.
func (ip IP) IsLoopback() bool {
	__tmpA := netip.AddrFromSlice(ip)
	addr, ok := __tmpA.addr, __tmpA.ok
	return ok && addr.IsLoopback()
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
	// Compare via netip (handles 4 vs 16 forms).
	__tmpA := netip.AddrFromSlice(ip)
	a, okA := __tmpA.addr, __tmpA.ok
	__tmpB := netip.AddrFromSlice(x)
	b, okB := __tmpB.addr, __tmpB.ok
	return okA && okB && a == b
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
func ParseCIDR(s string) (IP, *IPNet, error) {
	__tmpP := netip.ParsePrefix(s)
	prefix, err := __tmpP.prefix, __tmpP.err
	if err != nil {
		return nil, nil, err
	}
	addr := prefix.Addr()
	var ip IP
	if addr.Is4() {
		b := addr.As4()
		ip = IP{b[0], b[1], b[2], b[3]}
	} else {
		b := addr.As16()
		ip = IP{b[0], b[1], b[2], b[3], b[4], b[5], b[6], b[7],
			b[8], b[9], b[10], b[11], b[12], b[13], b[14], b[15]}
	}
	bits := prefix.Bits()
	mask := CIDRMask(bits, 32)
	if !addr.Is4() {
		mask = CIDRMask(bits, 128)
	}
	return ip, &IPNet{IP: ip.Mask(mask), Mask: mask}, nil
}

// CIDRMask returns an IPMask consisting of 'ones' 1 bits
// followed by 0s up to a total length of 'bits' bits.
func CIDRMask(ones, bits int) IPMask {
	if bits != 32 && bits != 128 {
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

// Mask returns the result of masking ip with mask.
func (ip IP) Mask(mask IPMask) IP {
	if len(mask) == 16 && len(ip) == 4 {
		// IPv4 mask on IPv4 address: use last 4 bytes.
		mask = mask[12:]
	}
	if len(mask) == 4 && len(ip) == 16 {
		// IPv4 mask on IPv6: only valid for 4-in-6.
		return nil
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

// String returns the CIDR notation of ip and its mask.
func (n *IPNet) String() string {
	if n == nil {
		return "<nil>"
	}
	ones, _ := n.Mask.Size()
	return n.IP.String() + "/" + itoa(ones)
}

// Size returns the number of leading ones and total bits in the mask.
func (m IPMask) Size() (ones, bits int) {
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
	return ones, len(m) * 8
}

// itoa is strconv.Itoa (strconv is imported by callers; kept local to
// avoid an import cycle in the synthetic package graph).
func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	neg := i < 0
	if neg {
		i = -i
	}
	var buf [20]byte
	pos := len(buf)
	for i > 0 {
		pos--
		buf[pos] = byte('0' + i%10)
		i /= 10
	}
	if neg {
		pos--
		buf[pos] = '-'
	}
	return string(buf[pos:])
}

// ---------------------------------------------------------------- sockets
// Socket path: wired to the netpoller runtime traps per the fork contract
// (phases/phase3/hidden_files/m9-tmp/net-fork-contract.md). All trap
// returns are int64: negative = -errno (Linux amd64), non-negative = value.

//go:linkname socketTrap graalgo.net.socket
func socketTrap(domain, typ, proto int64) int64

//go:linkname bindTrap graalgo.net.bind
func bindTrap(cookie int64, addr []byte) int64

//go:linkname listenTrap graalgo.net.listen
func listenTrap(cookie int64, backlog int64) int64

//go:linkname acceptTrap graalgo.net.accept
func acceptTrap(cookie int64) int64

//go:linkname connectTrap graalgo.net.connect
func connectTrap(cookie int64, addr []byte) int64

//go:linkname setsockoptTrap graalgo.net.setsockopt
func setsockoptTrap(cookie, level, opt, value int64) int64

//go:linkname getsockoptTrap graalgo.net.getsockopt
func getsockoptTrap(cookie, level, opt int64) int64

//go:linkname getsocknameTrap graalgo.net.getsockname
func getsocknameTrap(cookie int64, buf []byte) int64

//go:linkname getpeernameTrap graalgo.net.getpeername
func getpeernameTrap(cookie int64, buf []byte) int64

//go:linkname shutdownTrap graalgo.net.shutdown
func shutdownTrap(cookie, how int64) int64

//go:linkname setnonblockTrap graalgo.net.setnonblock
func setnonblockTrap(cookie, nonblocking int64) int64

//go:linkname sendtoTrap graalgo.net.sendto
func sendtoTrap(cookie int64, buf []byte, addr []byte) int64

//go:linkname recvfromTrap graalgo.net.recvfrom
func recvfromTrap(cookie int64, buf []byte, addr []byte) int64

// Errno numbers (Linux amd64, per the fork contract).
const (
	eagain       = 11
	einprogress  = 115
	econnrefused = 111
	eafnosupport = 97
	eperm        = 1
	einval       = 22
)

// errFromErrno maps a Linux errno number to an error.
func errFromErrno(errno int64) error {
	switch errno {
	case eagain:
		return errors.New("resource temporarily unavailable")
	case einprogress:
		return errors.New("operation now in progress")
	case econnrefused:
		return errors.New("connection refused")
	case eafnosupport:
		return errors.New("address family not supported by protocol")
	case eperm:
		return errors.New("operation not permitted")
	case einval:
		return errors.New("invalid argument")
	}
	return errors.New("errno " + itoa(int(errno)))
}

// sockAddr is a parsed IP address + port.
type sockAddr struct {
	ip   IP
	port int
	// zone string // M9: no zones
}

// sockaddrWire encodes a sockAddr in the trap wire format:
// byte 0 = family; AF_INET (2): [2, portHi, portLo, a0..a3];
// AF_INET6 (10): [10, portHi, portLo, a0..a15, scopeHi, scopeLo].
func sockaddrWire(sa *sockAddr) []byte {
	ip4 := sa.ip.To4()
	if ip4 != nil {
		b := make([]byte, 7)
		b[0] = 2
		b[1] = byte(sa.port >> 8)
		b[2] = byte(sa.port)
		copy(b[3:], ip4)
		return b
	}
	ip16 := sa.ip.To16()
	if ip16 == nil {
		return nil
	}
	b := make([]byte, 23)
	b[0] = 10
	b[1] = byte(sa.port >> 8)
	b[2] = byte(sa.port)
	copy(b[3:], ip16)
	// scope id = 0
	return b
}

// parseSockaddrWire decodes the trap wire format into a sockAddr.
func parseSockaddrWire(b []byte) *sockAddr {
	if len(b) < 3 {
		return nil
	}
	port := int(b[1])<<8 | int(b[2])
	switch b[0] {
	case 2:
		if len(b) < 7 {
			return nil
		}
		return &sockAddr{ip: IP{b[3], b[4], b[5], b[6]}, port: port}
	case 10:
		if len(b) < 21 {
			return nil
		}
		ip := make(IP, 16)
		copy(ip, b[3:19])
		return &sockAddr{ip: ip, port: port}
	}
	return nil
}

// netFD wraps a socket cookie and its poll.FD (for deadlines and
// readiness). The cookie is the trap/socket identity; the poll.FD
// registers it with the netpoller.
type netFD struct {
	cookie int64
	pfd    poll.FD
	// family: 2 (AF_INET) or 10 (AF_INET6); socktype: 1 (STREAM) or 2 (DGRAM)
	family   int
	socktype int
	// local and remote addresses (for Addr methods)
	laddr Addr
	raddr Addr
	// closed tracks Close (for Read/Write fail-fast)
	mu     sync.Mutex
	closed bool
}

func newNetFD(cookie int64, family, socktype int) (*netFD, error) {
	fd := &netFD{cookie: cookie, family: family, socktype: socktype}
	fd.pfd.Sysfd = uintptr(cookie)
	if err := fd.pfd.Init(); err != nil {
		return nil, err
	}
	return fd, nil
}

func (fd *netFD) close() error {
	fd.mu.Lock()
	if fd.closed {
		fd.mu.Unlock()
		return nil
	}
	fd.closed = true
	fd.mu.Unlock()
	fd.pfd.Evict()
	fd.pfd.ClosePoll()
	// Socket close goes through the unified graalgo.os.close on the cookie.
	if r := netOsClose(fd.cookie); r < 0 {
		return &OpError{Op: "close", Net: fd.netName(), Err: errFromErrno(-r)}
	}
	return nil
}

func (fd *netFD) isClosed() bool {
	fd.mu.Lock()
	defer fd.mu.Unlock()
	return fd.closed
}

func (fd *netFD) netName() string {
	if fd.socktype == 1 {
		return "tcp"
	}
	return "udp"
}

// Unified fd-domain linknames (the synthetic os package's osClose/osRead/
// osWrite are unexported; net declares its own linknames to the same
// graalgo.os.* targets per the fork contract).

//go:linkname netOsClose graalgo.os.close
func netOsClose(h int64) int64 // 0 or -errno

//go:linkname netOsRead graalgo.os.read
func netOsRead(h int64, b []byte) int64 // n+1 or -errno (0 = EOF)

//go:linkname netOsWrite graalgo.os.write
func netOsWrite(h int64, b []byte) int64 // n+1 or -errno


// ---------------------------------------------------------------- resolve
// IPAddr represents the address of an IP end point.
type IPAddr struct {
	IP   IP
	Port int
	Zone string // IPv6 addressing zone
}

func (a *IPAddr) Network() string { return "ip" }
func (a *IPAddr) String() string {
	if a == nil {
		return "<nil>"
	}
	return JoinHostPort(a.IP.String(), itoa(a.Port))
}

// TCPAddr represents the address of a TCP end point.
type TCPAddr struct {
	IP   IP
	Port int
	Zone string // IPv6 addressing zone
}

func (a *TCPAddr) Network() string { return "tcp" }
func (a *TCPAddr) String() string {
	if a == nil {
		return "<nil>"
	}
	return JoinHostPort(a.IP.String(), itoa(a.Port))
}

// UDPAddr represents the address of a UDP end point.
type UDPAddr struct {
	IP   IP
	Port int
	Zone string // IPv6 addressing zone
}

func (a *UDPAddr) Network() string { return "udp" }
func (a *UDPAddr) String() string {
	if a == nil {
		return "<nil>"
	}
	return JoinHostPort(a.IP.String(), itoa(a.Port))
}

// JoinHostPort combines host and port into a network address.
func JoinHostPort(host, port string) string {
	// If host is an IPv6 literal, wrap in brackets.
	__tmpA := netip.ParseAddr(host)
	if __tmpA.err == nil && __tmpA.addr.Is6() {
		return "[" + host + "]:" + port
	}
	return host + ":" + port
}

// SplitHostPort splits a network address hostport.
func SplitHostPort(hostport string) (host, port string) {
	panic("net.SplitHostPort: unreachable (Appendix-M desugar did not fire; re-test when WS D lands)")
}

// __splitHostPort is the desugar target for SplitHostPort.
func __splitHostPort(hostport string) splitHostPortPair {
	// Simplified; the full gc version handles brackets and zones.
	// M9: loopback literals only.
	if len(hostport) > 0 && hostport[0] == '[' {
		// IPv6 with brackets.
		i := 0
		for i < len(hostport) && hostport[i] != ']' {
			i++
		}
		if i >= len(hostport) {
			return splitHostPortPair{"", ""}
		}
		host := hostport[1:i]
		port := ""
		if i+1 < len(hostport) && hostport[i+1] == ':' {
			port = hostport[i+2:]
		}
		return splitHostPortPair{host, port}
	}
	// IPv4 or hostname: split on last colon.
	for i := len(hostport) - 1; i >= 0; i-- {
		if hostport[i] == ':' {
			return splitHostPortPair{hostport[:i], hostport[i+1:]}
		}
	}
	return splitHostPortPair{hostport, ""}
}

// resolveIPAddr parses an IP address string (no DNS in M9).
func resolveIPAddr(s string) (IP, error) {
	if s == "" {
		return nil, nil
	}
	ip := ParseIP(s)
	if ip == nil {
		return nil, &DNSError{Err: "no such host", Name: s}
	}
	return ip, nil
}

// ResolveIPAddr returns an address of IP end point ipAddr.
func ResolveIPAddr(network, address string) (*IPAddr, error) {
	__tmpR := __resolveIPAddr(network, address)
	return __tmpR.ip, __tmpR.err
}

func __resolveIPAddr(network, address string) resolveIPAddrPair {
	ip, err := resolveIPAddr(address)
	if err != nil {
		return resolveIPAddrPair{nil, err}
	}
	return resolveIPAddrPair{&IPAddr{IP: ip}, nil}
}

// ResolveTCPAddr returns an address of TCP end point.
func ResolveTCPAddr(network, address string) (*TCPAddr, error) {
	__tmpR := __resolveTCPAddr(network, address)
	return __tmpR.addr, __tmpR.err
}

func __resolveTCPAddr(network, address string) resolveTCPAddrPair {
	__tmpS := __splitHostPort(address)
	host, portStr := __tmpS.host, __tmpS.port
	ip, err := resolveIPAddr(host)
	if err != nil {
		return resolveTCPAddrPair{nil, err}
	}
	port, err := parsePort(portStr)
	if err != nil {
		return resolveTCPAddrPair{nil, err}
	}
	return resolveTCPAddrPair{&TCPAddr{IP: ip, Port: port}, nil}
}

// ResolveUDPAddr returns an address of UDP end point.
func ResolveUDPAddr(network, address string) (*UDPAddr, error) {
	__tmpR := __resolveUDPAddr(network, address)
	return __tmpR.addr, __tmpR.err
}

func __resolveUDPAddr(network, address string) resolveUDPAddrPair {
	__tmpS := __splitHostPort(address)
	host, portStr := __tmpS.host, __tmpS.port
	ip, err := resolveIPAddr(host)
	if err != nil {
		return resolveUDPAddrPair{nil, err}
	}
	port, err := parsePort(portStr)
	if err != nil {
		return resolveUDPAddrPair{nil, err}
	}
	return resolveUDPAddrPair{&UDPAddr{IP: ip, Port: port}, nil}
}


func parsePort(portStr string) (int, error) {
	if portStr == "" {
		return 0, nil
	}
	// Numeric port only (no service names in M9).
	n := 0
	for i := 0; i < len(portStr); i++ {
		c := portStr[i]
		if c < '0' || c > '9' {
			return 0, &DNSError{Err: "unknown port", Name: portStr}
		}
		n = n*10 + int(c-'0')
		if n > 65535 {
			return 0, errors.New("invalid port")
		}
	}
	return n, nil
}

// Pair structs for the resolve __-helpers.
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

type splitHostPortPair struct {
	host, port string
}

// ---------------------------------------------------------------- Listener
// Listener is a generic network listener for stream-oriented protocols.
type Listener interface {
	Accept() (Conn, error)
	Close() error
	Addr() Addr
}

// TCPListener is a TCP network listener.
type TCPListener struct {
	fd *netFD
}

// Listen announces on the local network address.
func Listen(network, address string) (Listener, error) {
	__tmpL := __listen(network, address)
	return __tmpL.l, __tmpL.err
}

func __listen(network, address string) listenPair {
	if network != "tcp" && network != "tcp4" && network != "tcp6" {
		return listenPair{nil, &OpError{Op: "listen", Net: network, Err: errors.New("unknown network " + network)}}
	}
	__tmpR := __resolveTCPAddr(network, address)
	laddr, err := __tmpR.addr, __tmpR.err
	if err != nil {
		return listenPair{nil, &OpError{Op: "listen", Net: network, Addr: nil, Err: err}}
	}
	family := 2
	if laddr.IP != nil && laddr.IP.To4() == nil {
		family = 10
	}
	if network == "tcp4" {
		family = 2
	} else if network == "tcp6" {
		family = 10
	}
	cookie := socketTrap(int64(family), 1, 0)
	if cookie < 0 {
		return listenPair{nil, &OpError{Op: "listen", Net: network, Addr: laddr, Err: errFromErrno(-cookie)}}
	}
	fd, err := newNetFD(cookie, family, 1)
	if err != nil {
		netOsClose(cookie)
		return listenPair{nil, &OpError{Op: "listen", Net: network, Addr: laddr, Err: err}}
	}
	sa := &sockAddr{ip: laddr.IP, port: laddr.Port}
	if r := bindTrap(cookie, sockaddrWire(sa)); r < 0 {
		fd.close()
		return listenPair{nil, &OpError{Op: "listen", Net: network, Addr: laddr, Err: errFromErrno(-r)}}
	}
	if r := listenTrap(cookie, 128); r < 0 {
		fd.close()
		return listenPair{nil, &OpError{Op: "listen", Net: network, Addr: laddr, Err: errFromErrno(-r)}}
	}
	// Record the local address.
	fd.laddr = laddr
	// Update with the actual bound port (for :0).
	if laddr.Port == 0 {
		buf := make([]byte, 23)
		if r := getsocknameTrap(cookie, buf); r > 0 {
			if sa2 := parseSockaddrWire(buf[:r-1]); sa2 != nil {
				fd.laddr = &TCPAddr{IP: sa2.ip, Port: sa2.port}
			}
		}
	}
	return listenPair{&TCPListener{fd: fd}, nil}
}

// Accept waits for and returns the next connection to the listener.
func (l *TCPListener) Accept() (Conn, error) {
	__tmpA := __tcpListenerAccept(l)
	return __tmpA.c, __tmpA.err
}

func __tcpListenerAccept(l *TCPListener) acceptPair {
	for {
		if err := l.fd.pfd.WaitRead(); err != nil {
			return acceptPair{nil, &OpError{Op: "accept", Net: "tcp", Addr: l.fd.laddr, Err: err}}
		}
		cookie := acceptTrap(l.fd.cookie)
		if cookie == -eagain {
			continue
		}
		if cookie < 0 {
			return acceptPair{nil, &OpError{Op: "accept", Net: "tcp", Addr: l.fd.laddr, Err: errFromErrno(-cookie)}}
		}
		fd, err := newNetFD(cookie, l.fd.family, 1)
		if err != nil {
			netOsClose(cookie)
			return acceptPair{nil, &OpError{Op: "accept", Net: "tcp", Addr: l.fd.laddr, Err: err}}
		}
		fd.laddr = l.fd.laddr
		// Get peer address.
		buf := make([]byte, 23)
		if r := getpeernameTrap(cookie, buf); r > 0 {
			if sa := parseSockaddrWire(buf[:r-1]); sa != nil {
				fd.raddr = &TCPAddr{IP: sa.ip, Port: sa.port}
			}
		}
		return acceptPair{&TCPConn{fd: fd}, nil}
	}
}

// Close closes the listener.
func (l *TCPListener) Close() error {
	return l.fd.close()
}

// Addr returns the listener's network address.
func (l *TCPListener) Addr() Addr {
	return l.fd.laddr
}

type listenPair struct {
	l   Listener
	err error
}

type acceptPair struct {
	c   Conn
	err error
}

// ---------------------------------------------------------------- Dial
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

// TCPConn is an implementation of the Conn interface for TCP network
// connections.
type TCPConn struct {
	fd *netFD
}

// Dial connects to the address on the named network.
func Dial(network, address string) (Conn, error) {
	__tmpD := __dial(network, address)
	return __tmpD.c, __tmpD.err
}

func __dial(network, address string) dialPair {
	return __dialTimeout(network, address, 0)
}

// DialTimeout acts like Dial but takes a timeout.
func DialTimeout(network, address string, timeout time.Duration) (Conn, error) {
	__tmpD := __dialTimeout(network, address, timeout)
	return __tmpD.c, __tmpD.err
}

func __dialTimeout(network, address string, timeout time.Duration) dialPair {
	isTCP := network == "tcp" || network == "tcp4" || network == "tcp6"
	isUDP := network == "udp" || network == "udp4" || network == "udp6"
	if !isTCP && !isUDP {
		return dialPair{nil, &OpError{Op: "dial", Net: network, Err: errors.New("unknown network " + network)}}
	}
	if isUDP {
		return __dialUDP(network, address)
	}
	__tmpR := __resolveTCPAddr(network, address)
	raddr, err := __tmpR.addr, __tmpR.err
	if err != nil {
		return dialPair{nil, &OpError{Op: "dial", Net: network, Addr: nil, Err: err}}
	}
	family := 2
	if raddr.IP != nil && raddr.IP.To4() == nil {
		family = 10
	}
	if network == "tcp4" {
		family = 2
	} else if network == "tcp6" {
		family = 10
	}
	cookie := socketTrap(int64(family), 1, 0)
	if cookie < 0 {
		return dialPair{nil, &OpError{Op: "dial", Net: network, Addr: raddr, Err: errFromErrno(-cookie)}}
	}
	fd, err := newNetFD(cookie, family, 1)
	if err != nil {
		netOsClose(cookie)
		return dialPair{nil, &OpError{Op: "dial", Net: network, Addr: raddr, Err: err}}
	}
	// Non-blocking connect.
	sa := &sockAddr{ip: raddr.IP, port: raddr.Port}
	r := connectTrap(cookie, sockaddrWire(sa))
	if r == -einprogress {
		// Wait for writability (completion).
		if timeout > 0 {
			fd.pfd.SetWriteDeadline(time.Now().Add(timeout))
		}
		if err := fd.pfd.WaitWrite(); err != nil {
			fd.close()
			if err == errDeadlineExceeded {
				return dialPair{nil, &OpError{Op: "dial", Net: network, Addr: raddr, Err: errDeadlineExceeded}}
			}
			return dialPair{nil, &OpError{Op: "dial", Net: network, Addr: raddr, Err: err}}
		}
		// Check SO_ERROR for async connect failure (gc parity).
		if se := getsockoptTrap(cookie, 1, 7); se < 0 {
			// getsockopt failed; treat as error.
			fd.close()
			return dialPair{nil, &OpError{Op: "dial", Net: network, Addr: raddr, Err: errFromErrno(-se)}}
		} else if se > 0 {
			fd.close()
			return dialPair{nil, &OpError{Op: "dial", Net: network, Addr: raddr, Err: &os.SyscallError{Syscall: "connect", Err: errFromErrno(se)}}}
		}
	} else if r < 0 {
		fd.close()
		return dialPair{nil, &OpError{Op: "dial", Net: network, Addr: raddr, Err: &os.SyscallError{Syscall: "connect", Err: errFromErrno(-r)}}}
	}
	fd.raddr = raddr
	// Get local address.
	buf := make([]byte, 23)
	if r := getsocknameTrap(cookie, buf); r > 0 {
		if sa2 := parseSockaddrWire(buf[:r-1]); sa2 != nil {
			fd.laddr = &TCPAddr{IP: sa2.ip, Port: sa2.port}
		}
	}
	return dialPair{&TCPConn{fd: fd}, nil}
}

// __dialUDP dials a UDP address (connected UDP).
func __dialUDP(network, address string) dialPair {
	__tmpR := __resolveUDPAddr(network, address)
	raddr, err := __tmpR.addr, __tmpR.err
	if err != nil {
		return dialPair{nil, &OpError{Op: "dial", Net: network, Addr: nil, Err: err}}
	}
	family := 2
	if raddr.IP != nil && raddr.IP.To4() == nil {
		family = 10
	}
	if network == "udp4" {
		family = 2
	} else if network == "udp6" {
		family = 10
	}
	cookie := socketTrap(int64(family), 2, 0)
	if cookie < 0 {
		return dialPair{nil, &OpError{Op: "dial", Net: network, Addr: raddr, Err: errFromErrno(-cookie)}}
	}
	fd, err := newNetFD(cookie, family, 2)
	if err != nil {
		netOsClose(cookie)
		return dialPair{nil, &OpError{Op: "dial", Net: network, Addr: raddr, Err: err}}
	}
	// Connect the UDP socket.
	sa := &sockAddr{ip: raddr.IP, port: raddr.Port}
	if r := connectTrap(cookie, sockaddrWire(sa)); r < 0 && r != -einprogress {
		fd.close()
		return dialPair{nil, &OpError{Op: "dial", Net: network, Addr: raddr, Err: errFromErrno(-r)}}
	}
	fd.raddr = raddr
	buf := make([]byte, 23)
	if r := getsocknameTrap(cookie, buf); r > 0 {
		if sa2 := parseSockaddrWire(buf[:r-1]); sa2 != nil {
			fd.laddr = &UDPAddr{IP: sa2.ip, Port: sa2.port}
		}
	}
	return dialPair{&UDPConn{fd: fd}, nil}
}

type dialPair struct {
	c   Conn
	err error
}

// Read reads data from the connection.
func (c *TCPConn) Read(b []byte) (int, error) {
	__tmpR := __tcpConnRead(c, b)
	return __tmpR.n, __tmpR.err
}

func __tcpConnRead(c *TCPConn, b []byte) readPair {
	if c.fd.isClosed() {
		return readPair{0, &OpError{Op: "read", Net: "tcp", Addr: c.fd.raddr, Err: errors.New("use of closed network connection")}}
	}
	for {
		if err := c.fd.pfd.WaitRead(); err != nil {
			return readPair{0, &OpError{Op: "read", Net: "tcp", Addr: c.fd.raddr, Err: err}}
		}
		r := netOsRead(c.fd.cookie, b)
		if r == -eagain {
			continue
		}
		if r < 0 {
			return readPair{0, &OpError{Op: "read", Net: "tcp", Addr: c.fd.raddr, Err: errFromErrno(-r)}}
		}
		if r == 0 {
			// EOF (n+1 convention: 0 means EOF).
			return readPair{0, io.EOF}
		}
		return readPair{int(r - 1), nil}
	}
}

// Write writes data to the connection.
func (c *TCPConn) Write(b []byte) (int, error) {
	__tmpW := __tcpConnWrite(c, b)
	return __tmpW.n, __tmpW.err
}

func __tcpConnWrite(c *TCPConn, b []byte) writePair {
	if c.fd.isClosed() {
		return writePair{0, &OpError{Op: "write", Net: "tcp", Addr: c.fd.raddr, Err: errors.New("use of closed network connection")}}
	}
	total := 0
	for len(b) > 0 {
		if err := c.fd.pfd.WaitWrite(); err != nil {
			if total > 0 {
				return writePair{total, nil}
			}
			return writePair{0, &OpError{Op: "write", Net: "tcp", Addr: c.fd.raddr, Err: err}}
		}
		r := netOsWrite(c.fd.cookie, b)
		if r == -eagain {
			continue
		}
		if r < 0 {
			if total > 0 {
				return writePair{total, nil}
			}
			return writePair{0, &OpError{Op: "write", Net: "tcp", Addr: c.fd.raddr, Err: errFromErrno(-r)}}
		}
		n := int(r - 1)
		total += n
		b = b[n:]
	}
	return writePair{total, nil}
}

// Close closes the connection.
func (c *TCPConn) Close() error {
	return c.fd.close()
}

// LocalAddr returns the local network address.
func (c *TCPConn) LocalAddr() Addr { return c.fd.laddr }

// RemoteAddr returns the remote network address.
func (c *TCPConn) RemoteAddr() Addr { return c.fd.raddr }

// SetDeadline sets the read and write deadlines.
func (c *TCPConn) SetDeadline(t time.Time) error { return c.fd.pfd.SetDeadline(t) }

// SetReadDeadline sets the deadline for future Read calls.
func (c *TCPConn) SetReadDeadline(t time.Time) error { return c.fd.pfd.SetReadDeadline(t) }

// SetWriteDeadline sets the deadline for future Write calls.
func (c *TCPConn) SetWriteDeadline(t time.Time) error { return c.fd.pfd.SetWriteDeadline(t) }

type readPair struct {
	n   int
	err error
}

type writePair struct {
	n   int
	err error
}

// ---------------------------------------------------------------- UDP
// PacketConn is a generic packet-oriented network connection.
type PacketConn interface {
	ReadFrom(p []byte) (n int, addr Addr, err error)
	WriteTo(p []byte, addr Addr) (n int, err error)
	Close() error
	LocalAddr() Addr
	SetDeadline(t time.Time) error
	SetReadDeadline(t time.Time) error
	SetWriteDeadline(t time.Time) error
}

// UDPConn is an implementation of the PacketConn and Conn interfaces
// for UDP network connections.
type UDPConn struct {
	fd *netFD
}

// ListenPacket announces on the local network address.
func ListenPacket(network, address string) (PacketConn, error) {
	__tmpL := __listenPacket(network, address)
	return __tmpL.c, __tmpL.err
}

func __listenPacket(network, address string) listenPacketPair {
	if network != "udp" && network != "udp4" && network != "udp6" {
		return listenPacketPair{nil, &OpError{Op: "listen", Net: network, Err: errors.New("unknown network " + network)}}
	}
	__tmpR := __resolveUDPAddr(network, address)
	laddr, err := __tmpR.addr, __tmpR.err
	if err != nil {
		return listenPacketPair{nil, &OpError{Op: "listen", Net: network, Addr: nil, Err: err}}
	}
	family := 2
	if laddr.IP != nil && laddr.IP.To4() == nil {
		family = 10
	}
	if network == "udp4" {
		family = 2
	} else if network == "udp6" {
		family = 10
	}
	cookie := socketTrap(int64(family), 2, 0)
	if cookie < 0 {
		return listenPacketPair{nil, &OpError{Op: "listen", Net: network, Addr: laddr, Err: errFromErrno(-cookie)}}
	}
	fd, err := newNetFD(cookie, family, 2)
	if err != nil {
		netOsClose(cookie)
		return listenPacketPair{nil, &OpError{Op: "listen", Net: network, Addr: laddr, Err: err}}
	}
	sa := &sockAddr{ip: laddr.IP, port: laddr.Port}
	if r := bindTrap(cookie, sockaddrWire(sa)); r < 0 {
		fd.close()
		return listenPacketPair{nil, &OpError{Op: "listen", Net: network, Addr: laddr, Err: errFromErrno(-r)}}
	}
	fd.laddr = laddr
	if laddr.Port == 0 {
		buf := make([]byte, 23)
		if r := getsocknameTrap(cookie, buf); r > 0 {
			if sa2 := parseSockaddrWire(buf[:r-1]); sa2 != nil {
				fd.laddr = &UDPAddr{IP: sa2.ip, Port: sa2.port}
			}
		}
	}
	return listenPacketPair{&UDPConn{fd: fd}, nil}
}

type listenPacketPair struct {
	c   PacketConn
	err error
}

// ReadFrom reads a packet from the connection.
func (c *UDPConn) ReadFrom(b []byte) (int, Addr, error) {
	__tmpR := __udpConnReadFrom(c, b)
	return __tmpR.n, __tmpR.addr, __tmpR.err
}

func __udpConnReadFrom(c *UDPConn, b []byte) readFromPair {
	if c.fd.isClosed() {
		return readFromPair{0, nil, &OpError{Op: "read", Net: "udp", Addr: c.fd.laddr, Err: errors.New("use of closed network connection")}}
	}
	for {
		if err := c.fd.pfd.WaitRead(); err != nil {
			return readFromPair{0, nil, &OpError{Op: "read", Net: "udp", Addr: c.fd.laddr, Err: err}}
		}
		addrBuf := make([]byte, 23)
		r := recvfromTrap(c.fd.cookie, b, addrBuf)
		if r == -eagain {
			continue
		}
		if r < 0 {
			return readFromPair{0, nil, &OpError{Op: "read", Net: "udp", Addr: c.fd.laddr, Err: errFromErrno(-r)}}
		}
		n := int(r - 1)
		var addr Addr
		if sa := parseSockaddrWire(addrBuf); sa != nil {
			addr = &UDPAddr{IP: sa.ip, Port: sa.port}
		}
		return readFromPair{n, addr, nil}
	}
}

// WriteTo writes a packet to addr.
func (c *UDPConn) WriteTo(b []byte, addr Addr) (int, error) {
	__tmpW := __udpConnWriteTo(c, b, addr)
	return __tmpW.n, __tmpW.err
}

func __udpConnWriteTo(c *UDPConn, b []byte, addr Addr) writePair {
	if c.fd.isClosed() {
		return writePair{0, &OpError{Op: "write", Net: "udp", Addr: c.fd.laddr, Err: errors.New("use of closed network connection")}}
	}
	// Resolve the destination address.
	var sa *sockAddr
	switch a := addr.(type) {
	case *UDPAddr:
		sa = &sockAddr{ip: a.IP, port: a.Port}
	default:
		return writePair{0, &OpError{Op: "write", Net: "udp", Addr: c.fd.laddr, Err: errors.New("unknown address type")}}
	}
	if err := c.fd.pfd.WaitWrite(); err != nil {
		return writePair{0, &OpError{Op: "write", Net: "udp", Addr: c.fd.laddr, Err: err}}
	}
	r := sendtoTrap(c.fd.cookie, b, sockaddrWire(sa))
	if r < 0 {
		return writePair{0, &OpError{Op: "write", Net: "udp", Addr: c.fd.laddr, Err: errFromErrno(-r)}}
	}
	return writePair{int(r - 1), nil}
}

// Read reads data from the connection (connected UDP).
func (c *UDPConn) Read(b []byte) (int, error) {
	__tmpR := __udpConnRead(c, b)
	return __tmpR.n, __tmpR.err
}

func __udpConnRead(c *UDPConn, b []byte) readPair {
	// Connected UDP read: like TCP read.
	if c.fd.isClosed() {
		return readPair{0, &OpError{Op: "read", Net: "udp", Addr: c.fd.raddr, Err: errors.New("use of closed network connection")}}
	}
	for {
		if err := c.fd.pfd.WaitRead(); err != nil {
			return readPair{0, &OpError{Op: "read", Net: "udp", Addr: c.fd.raddr, Err: err}}
		}
		r := netOsRead(c.fd.cookie, b)
		if r == -eagain {
			continue
		}
		if r < 0 {
			return readPair{0, &OpError{Op: "read", Net: "udp", Addr: c.fd.raddr, Err: errFromErrno(-r)}}
		}
		if r == 0 {
			return readPair{0, io.EOF}
		}
		return readPair{int(r - 1), nil}
	}
}

// Write writes data to the connection (connected UDP).
func (c *UDPConn) Write(b []byte) (int, error) {
	__tmpW := __udpConnWrite(c, b)
	return __tmpW.n, __tmpW.err
}

func __udpConnWrite(c *UDPConn, b []byte) writePair {
	if c.fd.isClosed() {
		return writePair{0, &OpError{Op: "write", Net: "udp", Addr: c.fd.raddr, Err: errors.New("use of closed network connection")}}
	}
	if err := c.fd.pfd.WaitWrite(); err != nil {
		return writePair{0, &OpError{Op: "write", Net: "udp", Addr: c.fd.raddr, Err: err}}
	}
	r := netOsWrite(c.fd.cookie, b)
	if r < 0 {
		return writePair{0, &OpError{Op: "write", Net: "udp", Addr: c.fd.raddr, Err: errFromErrno(-r)}}
	}
	return writePair{int(r - 1), nil}
}

// Close closes the connection.
func (c *UDPConn) Close() error { return c.fd.close() }

// LocalAddr returns the local network address.
func (c *UDPConn) LocalAddr() Addr { return c.fd.laddr }

// RemoteAddr returns the remote network address.
func (c *UDPConn) RemoteAddr() Addr { return c.fd.raddr }

// SetDeadline sets the read and write deadlines.
func (c *UDPConn) SetDeadline(t time.Time) error { return c.fd.pfd.SetDeadline(t) }

// SetReadDeadline sets the deadline for future Read calls.
func (c *UDPConn) SetReadDeadline(t time.Time) error { return c.fd.pfd.SetReadDeadline(t) }

// SetWriteDeadline sets the deadline for future Write calls.
func (c *UDPConn) SetWriteDeadline(t time.Time) error { return c.fd.pfd.SetWriteDeadline(t) }

type readFromPair struct {
	n    int
	addr Addr
	err  error
}

// ---------------------------------------------------------------- ParseMAC
// HardwareAddr represents a physical hardware address.
type HardwareAddr []byte

func (a HardwareAddr) String() string {
	if len(a) == 0 {
		return ""
	}
	buf := make([]byte, 0, len(a)*3-1)
	const hextable = "0123456789abcdef"
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
	__tmpM := __parseMAC(s)
	return __tmpM.hw, __tmpM.err
}

func __parseMAC(s string) parseMACPair {
	// Simplified: handles xx:xx:xx:xx:xx:xx, xx-xx-xx-xx-xx-xx,
	// and xxxxxxxxxxxx forms.
	var hw []byte
	if len(s) == 17 && (s[2] == ':' || s[2] == '-') {
		sep := s[2]
		hw = make([]byte, 6)
		for i := 0; i < 6; i++ {
			if i > 0 && s[i*3-1] != sep {
				return parseMACPair{nil, errors.New("invalid MAC address")}
			}
			hi := fromHex(s[i*3])
			lo := fromHex(s[i*3+1])
			if hi > 15 || lo > 15 {
				return parseMACPair{nil, errors.New("invalid MAC address")}
			}
			hw[i] = byte(hi<<4 | lo)
		}
		return parseMACPair{HardwareAddr(hw), nil}
	}
	if len(s) == 12 {
		hw = make([]byte, 6)
		for i := 0; i < 6; i++ {
			hi := fromHex(s[i*2])
			lo := fromHex(s[i*2+1])
			if hi > 15 || lo > 15 {
				return parseMACPair{nil, errors.New("invalid MAC address")}
			}
			hw[i] = byte(hi<<4 | lo)
		}
		return parseMACPair{HardwareAddr(hw), nil}
	}
	return parseMACPair{nil, errors.New("invalid MAC address")}
}

func fromHex(c byte) int {
	switch {
	case '0' <= c && c <= '9':
		return int(c - '0')
	case 'a' <= c && c <= 'f':
		return int(c-'a') + 10
	case 'A' <= c && c <= 'F':
		return int(c-'A') + 10
	}
	return 16
}

type parseMACPair struct {
	hw  HardwareAddr
	err error
}

// ---------------------------------------------------------------- Pipe
// pipeDeadline is an abstraction for handling timeouts.
type pipeDeadline struct {
	mu     sync.Mutex // Guards timer and cancel
	timer  *time.Timer
	cancel chan struct{} // Must be non-nil
}

func makePipeDeadline() pipeDeadline {
	return pipeDeadline{cancel: make(chan struct{})}
}

func (d *pipeDeadline) set(t time.Time) {
	d.mu.Lock()
	defer d.mu.Unlock()

	if d.timer != nil && !d.timer.Stop() {
		<-d.cancel // Wait for the timer callback to finish and close cancel
	}
	d.timer = nil

	closed := isClosedChan(d.cancel)
	if t.IsZero() {
		if closed {
			d.cancel = make(chan struct{})
		}
		return
	}

	if dur := time.Until(t); dur > 0 {
		if closed {
			d.cancel = make(chan struct{})
		}
		d.timer = time.AfterFunc(dur, func() {
			close(d.cancel)
		})
		return
	}

	if !closed {
		close(d.cancel)
	}
}

func (d *pipeDeadline) wait() chan struct{} {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.cancel
}

func isClosedChan(c <-chan struct{}) bool {
	select {
	case <-c:
		return true
	default:
		return false
	}
}

type pipeAddr struct{}

func (pipeAddr) Network() string { return "pipe" }
func (pipeAddr) String() string  { return "pipe" }

type pipe struct {
	wrMu sync.Mutex // Serialize Write operations

	rdRx <-chan []byte
	rdTx chan<- int

	wrTx chan<- []byte
	wrRx <-chan int

	once       sync.Once // Protects closing localDone
	localDone  chan struct{}
	remoteDone <-chan struct{}

	readDeadline  pipeDeadline
	writeDeadline pipeDeadline
}

// Pipe creates a synchronous, in-memory, full duplex
// network connection; both ends implement the [Conn] interface.
func Pipe() (Conn, Conn) {
	__tmpP := __pipe()
	return __tmpP.c1, __tmpP.c2
}

func __pipe() pipePair {
	cb1 := make(chan []byte)
	cb2 := make(chan []byte)
	cn1 := make(chan int)
	cn2 := make(chan int)
	done1 := make(chan struct{})
	done2 := make(chan struct{})

	p1 := &pipe{
		rdRx: cb1, rdTx: cn1,
		wrTx: cb2, wrRx: cn2,
		localDone: done1, remoteDone: done2,
		readDeadline:  makePipeDeadline(),
		writeDeadline: makePipeDeadline(),
	}
	p2 := &pipe{
		rdRx: cb2, rdTx: cn2,
		wrTx: cb1, wrRx: cn1,
		localDone: done2, remoteDone: done1,
		readDeadline:  makePipeDeadline(),
		writeDeadline: makePipeDeadline(),
	}
	return pipePair{p1, p2}
}

type pipePair struct {
	c1, c2 Conn
}

func (*pipe) LocalAddr() Addr  { return pipeAddr{} }
func (*pipe) RemoteAddr() Addr { return pipeAddr{} }

func (p *pipe) Read(b []byte) (int, error) {
	__tmpR := __pipeRead(p, b)
	n, err := __tmpR.n, __tmpR.err
	if err != nil && err != io.EOF && err != io.ErrClosedPipe {
		err = &OpError{Op: "read", Net: "pipe", Err: err}
	}
	return n, err
}

func __pipeRead(p *pipe, b []byte) readPair {
	switch {
	case isClosedChan(p.localDone):
		return readPair{0, io.ErrClosedPipe}
	case isClosedChan(p.remoteDone):
		return readPair{0, io.EOF}
	case isClosedChan(p.readDeadline.wait()):
		return readPair{0, errDeadlineExceeded}
	}

	select {
	case bw := <-p.rdRx:
		nr := copy(b, bw)
		p.rdTx <- nr
		return readPair{nr, nil}
	case <-p.localDone:
		return readPair{0, io.ErrClosedPipe}
	case <-p.remoteDone:
		return readPair{0, io.EOF}
	case <-p.readDeadline.wait():
		return readPair{0, errDeadlineExceeded}
	}
}

func (p *pipe) Write(b []byte) (int, error) {
	__tmpW := __pipeWrite(p, b)
	n, err := __tmpW.n, __tmpW.err
	if err != nil && err != io.ErrClosedPipe {
		err = &OpError{Op: "write", Net: "pipe", Err: err}
	}
	return n, err
}

func __pipeWrite(p *pipe, b []byte) writePair {
	switch {
	case isClosedChan(p.localDone):
		return writePair{0, io.ErrClosedPipe}
	case isClosedChan(p.remoteDone):
		return writePair{0, io.ErrClosedPipe}
	case isClosedChan(p.writeDeadline.wait()):
		return writePair{0, errDeadlineExceeded}
	}

	p.wrMu.Lock()
	defer p.wrMu.Unlock()
	n := 0
	for once := true; once || len(b) > 0; once = false {
		select {
		case p.wrTx <- b:
			nw := <-p.wrRx
			b = b[nw:]
			n += nw
		case <-p.localDone:
			return writePair{n, io.ErrClosedPipe}
		case <-p.remoteDone:
			return writePair{n, io.ErrClosedPipe}
		case <-p.writeDeadline.wait():
			return writePair{n, errDeadlineExceeded}
		}
	}
	return writePair{n, nil}
}

func (p *pipe) SetDeadline(t time.Time) error {
	if isClosedChan(p.localDone) || isClosedChan(p.remoteDone) {
		return io.ErrClosedPipe
	}
	p.readDeadline.set(t)
	p.writeDeadline.set(t)
	return nil
}

func (p *pipe) SetReadDeadline(t time.Time) error {
	if isClosedChan(p.localDone) || isClosedChan(p.remoteDone) {
		return io.ErrClosedPipe
	}
	p.readDeadline.set(t)
	return nil
}

func (p *pipe) SetWriteDeadline(t time.Time) error {
	if isClosedChan(p.localDone) || isClosedChan(p.remoteDone) {
		return io.ErrClosedPipe
	}
	p.writeDeadline.set(t)
	return nil
}

func (p *pipe) Close() error {
	p.once.Do(func() { close(p.localDone) })
	return nil
}

// ---------------------------------------------------------------- ParseCIDR
// __parseCIDR is the desugar target for ParseCIDR (3-result).
func __parseCIDR(s string) parseCIDRPair {
	__tmpP := netip.ParsePrefix(s)
	prefix, err := __tmpP.prefix, __tmpP.err
	if err != nil {
		return parseCIDRPair{nil, nil, err}
	}
	addr := prefix.Addr()
	var ip IP
	if addr.Is4() {
		b := addr.As4()
		ip = IP{b[0], b[1], b[2], b[3]}
	} else {
		b := addr.As16()
		ip = IP{b[0], b[1], b[2], b[3], b[4], b[5], b[6], b[7],
			b[8], b[9], b[10], b[11], b[12], b[13], b[14], b[15]}
	}
	bits := prefix.Bits()
	mask := CIDRMask(bits, 32)
	if !addr.Is4() {
		mask = CIDRMask(bits, 128)
	}
	return parseCIDRPair{ip, &IPNet{IP: ip.Mask(mask), Mask: mask}, nil}
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
`
