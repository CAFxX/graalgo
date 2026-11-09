// Regression tests for R08-S-05 (REVIEW-2026-11-09-08): net.LookupHost /
// net.LookupIP panicked ("not in M9 scope (no resolver)") and Dial by
// hostname failed with "no such host" because the synthetic net package
// had no resolver. The fix delegates name resolution to the JDK
// (java.net.InetAddress) via the graalgo.net.lookuphost trap, validates
// names with gc's isDomainName rule before touching the trap, and resolves
// service names from /etc/services in pure Go (gc's pure-Go resolver).
//
// Pre-fix, every test below fails: LookupHost/LookupIP/LookupPort panic at
// guest runtime and Dial("tcp", "localhost:port") fails with "no such
// host". All tests are loopback-only: no external host is ever resolved.
// The sandbox DNS is a wildcard (any name resolves), so the unknown-name
// case uses a syntactically invalid name ("!!!"), which gc rejects locally
// with a DNSError — no DNS query is involved on either leg.
package netdns

import (
	"io"
	"net"
	"strconv"
	"testing"
)

func containsAddr(t *testing.T, addrs []string, want string) bool {
	t.Helper()
	for _, a := range addrs {
		if a == want {
			return true
		}
	}
	return false
}

func containsIP(t *testing.T, ips []net.IP, want string) bool {
	t.Helper()
	for _, ip := range ips {
		if ip.String() == want {
			return true
		}
	}
	return false
}

func TestLookupHostLocalhost(t *testing.T) {
	addrs, err := net.LookupHost("localhost")
	if err != nil {
		t.Fatalf("LookupHost(localhost) err = %v, want nil", err)
	}
	// Order is resolver-dependent (gc itself returns v4-first from
	// LookupHost and v6-first from LookupIP); pin the set, not the order.
	if !containsAddr(t, addrs, "127.0.0.1") || !containsAddr(t, addrs, "::1") {
		t.Fatalf("LookupHost(localhost) = %q, want it to contain 127.0.0.1 and ::1", addrs)
	}
}

func TestLookupHostLiteral(t *testing.T) {
	addrs, err := net.LookupHost("127.0.0.1")
	if err != nil {
		t.Fatalf("LookupHost(127.0.0.1) err = %v, want nil", err)
	}
	if len(addrs) != 1 || addrs[0] != "127.0.0.1" {
		t.Fatalf("LookupHost(127.0.0.1) = %q, want [127.0.0.1]", addrs)
	}
	addrs, err = net.LookupHost("::1")
	if err != nil {
		t.Fatalf("LookupHost(::1) err = %v, want nil", err)
	}
	if len(addrs) != 1 || addrs[0] != "::1" {
		t.Fatalf("LookupHost(::1) = %q, want [::1]", addrs)
	}
}

func TestLookupIPLocalhost(t *testing.T) {
	ips, err := net.LookupIP("localhost")
	if err != nil {
		t.Fatalf("LookupIP(localhost) err = %v, want nil", err)
	}
	if !containsIP(t, ips, "127.0.0.1") || !containsIP(t, ips, "::1") {
		t.Fatalf("LookupIP(localhost) = %v, want it to contain 127.0.0.1 and ::1", ips)
	}
}

func TestLookupHostEmpty(t *testing.T) {
	_, err := net.LookupHost("")
	if err == nil {
		t.Fatalf("LookupHost(\"\") err = nil, want *net.DNSError")
	}
	if _, ok := err.(*net.DNSError); !ok {
		t.Fatalf("LookupHost(\"\") err = %T, want *net.DNSError", err)
	}
}

func TestLookupHostInvalid(t *testing.T) {
	// "!!!" is not a domain name (gc's isDomainName rule): both legs
	// reject it locally with a DNSError, without a DNS query.
	_, err := net.LookupHost("!!!")
	if err == nil {
		t.Fatalf("LookupHost(\"!!!\") err = nil, want *net.DNSError")
	}
	if _, ok := err.(*net.DNSError); !ok {
		t.Fatalf("LookupHost(\"!!!\") err = %T, want *net.DNSError", err)
	}
}

func TestLookupPort(t *testing.T) {
	port, err := net.LookupPort("tcp", "http")
	if err != nil || port != 80 {
		t.Fatalf("LookupPort(tcp, http) = %d, %v, want 80, nil", port, err)
	}
	port, err = net.LookupPort("tcp", "8080")
	if err != nil || port != 8080 {
		t.Fatalf("LookupPort(tcp, 8080) = %d, %v, want 8080, nil", port, err)
	}
	port, err = net.LookupPort("tcp", "")
	if err != nil || port != 0 {
		t.Fatalf("LookupPort(tcp, \"\") = %d, %v, want 0, nil", port, err)
	}
	if _, err := net.LookupPort("tcp", "nosuchservice-xyz"); err == nil {
		t.Fatalf("LookupPort(tcp, nosuchservice-xyz) err = nil, want non-nil")
	}
	if _, err := net.LookupPort("tcp", "99999"); err == nil {
		t.Fatalf("LookupPort(tcp, 99999) err = nil, want invalid-port error")
	}
}

func TestResolveTCPAddrByHostname(t *testing.T) {
	addr, err := net.ResolveTCPAddr("tcp", "localhost:80")
	if err != nil {
		t.Fatalf("ResolveTCPAddr(tcp, localhost:80) err = %v, want nil", err)
	}
	if !addr.IP.IsLoopback() || addr.Port != 80 {
		t.Fatalf("ResolveTCPAddr(tcp, localhost:80) = %v, want loopback:80", addr)
	}
	addr, err = net.ResolveTCPAddr("tcp4", "localhost:80")
	if err != nil {
		t.Fatalf("ResolveTCPAddr(tcp4, localhost:80) err = %v, want nil", err)
	}
	if addr.IP.To4() == nil {
		t.Fatalf("ResolveTCPAddr(tcp4, localhost:80) = %v, want an IPv4 address", addr)
	}
	addr, err = net.ResolveTCPAddr("tcp6", "localhost:80")
	if err != nil {
		t.Fatalf("ResolveTCPAddr(tcp6, localhost:80) err = %v, want nil", err)
	}
	if addr.IP.To4() != nil {
		t.Fatalf("ResolveTCPAddr(tcp6, localhost:80) = %v, want an IPv6 address", addr)
	}
}

func TestDialByHostname(t *testing.T) {
	ln, err := net.Listen("tcp", "localhost:0")
	if err != nil {
		t.Fatalf("Listen(tcp, localhost:0) err = %v, want nil", err)
	}
	defer ln.Close()
	port := ln.Addr().(*net.TCPAddr).Port

	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go func() {
				defer c.Close()
				io.Copy(c, c)
			}()
		}
	}()

	c, err := net.Dial("tcp", "localhost:"+strconv.Itoa(port))
	if err != nil {
		t.Fatalf("Dial(tcp, localhost:%d) err = %v, want nil", port, err)
	}
	defer c.Close()
	if !c.RemoteAddr().(*net.TCPAddr).IP.IsLoopback() {
		t.Fatalf("Dial remote addr = %v, want a loopback address", c.RemoteAddr())
	}

	msg := "hello hostname"
	if _, err := c.Write([]byte(msg)); err != nil {
		t.Fatalf("Write err = %v, want nil", err)
	}
	buf := make([]byte, len(msg))
	if _, err := io.ReadFull(c, buf); err != nil {
		t.Fatalf("ReadFull err = %v, want nil", err)
	}
	if string(buf) != msg {
		t.Fatalf("echo = %q, want %q", string(buf), msg)
	}
}
