package netdns

// Regression tests for the P1-net remediation (2026-10-18): DNS
// resolution in the synthetic net package via the graalgo.net.lookupHost
// trap (JDK java.net.InetAddress delegation).
//
// All names resolve through the platform resolver only: "localhost"
// (hosts file) and IP literals. No external DNS — the sandbox and CI
// resolvers differ (the dev sandbox even wildcard-resolves), so unknown
// names are deliberately NOT asserted here; the trap's null-on-failure
// path is covered by the GoNet.lookupHost Java unit test instead.
//
// These run through `graalgo test` (test/regression), exercising the
// Appendix-M desugar (net.LookupHost -> __lookupHost) and the linkname
// trap end to end.

import (
	"net"
	"testing"
)

func contains(ss []string, want string) bool {
	for _, s := range ss {
		if s == want {
			return true
		}
	}
	return false
}

// TestLookupHostLocalhost proves the chosen DNS path resolves a name:
// localhost must resolve via the platform resolver to 127.0.0.1.
func TestLookupHostLocalhost(t *testing.T) {
	addrs, err := net.LookupHost("localhost")
	if err != nil {
		t.Fatalf("LookupHost(localhost): unexpected err: %v", err)
	}
	if !contains(addrs, "127.0.0.1") {
		t.Fatalf("LookupHost(localhost) = %q, want it to contain 127.0.0.1", addrs)
	}
}

// TestLookupHostEmpty pins gc parity: "" rejects before the trap with
// *net.DNSError (the JDK would map "" to localhost).
func TestLookupHostEmpty(t *testing.T) {
	addrs, err := net.LookupHost("")
	if err == nil {
		t.Fatalf("LookupHost(\"\") = %q, want error", addrs)
	}
	if _, ok := err.(*net.DNSError); !ok {
		t.Fatalf("LookupHost(\"\") err = %T (%v), want *net.DNSError", err, err)
	}
}

// TestLookupIPLiteral pins the IP-literal fast path (no trap round-trip).
func TestLookupIPLiteral(t *testing.T) {
	ips, err := net.LookupIP("127.0.0.1")
	if err != nil {
		t.Fatalf("LookupIP(127.0.0.1): unexpected err: %v", err)
	}
	if len(ips) != 1 || ips[0].String() != "127.0.0.1" {
		t.Fatalf("LookupIP(127.0.0.1) = %v, want [127.0.0.1]", ips)
	}
}

// TestLookupIPEmpty pins gc parity for the empty host.
func TestLookupIPEmpty(t *testing.T) {
	ips, err := net.LookupIP("")
	if err == nil {
		t.Fatalf("LookupIP(\"\") = %v, want error", ips)
	}
	if _, ok := err.(*net.DNSError); !ok {
		t.Fatalf("LookupIP(\"\") err = %T (%v), want *net.DNSError", err, err)
	}
}

// TestResolveTCPAddrDNS proves __resolveTCPAddr falls back to DNS for
// non-literal hosts (the P1 gap: it was ParseIP-only).
func TestResolveTCPAddrDNS(t *testing.T) {
	addr, err := net.ResolveTCPAddr("tcp", "localhost:80")
	if err != nil {
		t.Fatalf("ResolveTCPAddr(tcp, localhost:80): unexpected err: %v", err)
	}
	if addr.Port != 80 {
		t.Fatalf("Port = %d, want 80", addr.Port)
	}
	if !addr.IP.IsLoopback() {
		t.Fatalf("IP = %v, want a loopback address", addr.IP)
	}
}

// TestResolveIPAddrDNSFamily pins the network-family filter: "ip4" must
// yield an IPv4 address even though localhost also resolves to ::1.
func TestResolveIPAddrDNSFamily(t *testing.T) {
	addr, err := net.ResolveIPAddr("ip4", "localhost")
	if err != nil {
		t.Fatalf("ResolveIPAddr(ip4, localhost): unexpected err: %v", err)
	}
	if addr.IP.To4() == nil {
		t.Fatalf("IP = %v, want IPv4 for network ip4", addr.IP)
	}
}

// TestResolveIPAddrEmpty pins gc parity: an empty address resolves to
// the zero IPAddr without consulting the resolver.
func TestResolveIPAddrEmpty(t *testing.T) {
	addr, err := net.ResolveIPAddr("ip", "")
	if err != nil {
		t.Fatalf("ResolveIPAddr(ip, \"\"): unexpected err: %v", err)
	}
	if len(addr.IP) != 0 {
		t.Fatalf("IP = %v, want zero IP for empty address", addr.IP)
	}
}
