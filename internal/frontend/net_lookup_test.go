package main

import (
	"reflect"
	"testing"
)

// TestSplitLookupAddrs pins the graalgo.net.lookupHost wire-format
// parsing: the trap returns "" on failure or "\n"-joined
// getHostAddress() literals on success. Empty entries are dropped; an
// empty reply parses to nil (the caller maps that to *DNSError).
func TestSplitLookupAddrs(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want []string
	}{
		{"empty", "", nil},
		{"single", "127.0.0.1", []string{"127.0.0.1"}},
		{"multi", "127.0.0.1\n::1", []string{"127.0.0.1", "::1"}},
		{"trailing-newline", "127.0.0.1\n", []string{"127.0.0.1"}},
		{"blank-lines", "127.0.0.1\n\n::1\n\n", []string{"127.0.0.1", "::1"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := __splitLookupAddrs(c.in); !reflect.DeepEqual(got, c.want) {
				t.Fatalf("__splitLookupAddrs(%q) = %q, want %q", c.in, got, c.want)
			}
		})
	}
}

// TestFilterResolveIP pins the network-family filter used by the
// __resolve*Addr DNS fallback (gc parity: net.internetAddrList's
// ipv4only/ipv6only filters). A network ending in '4' keeps only
// IPv4 addresses, ending in '6' keeps only IPv6 (excluding
// IPv4-mapped), anything else keeps the first address.
func TestFilterResolveIP(t *testing.T) {
	v4 := ParseIP("127.0.0.1")
	v6 := ParseIP("::1")
	v4mapped := ParseIP("::ffff:127.0.0.1")
	if v4 == nil || v6 == nil || v4mapped == nil {
		t.Fatal("ParseIP fixture failed")
	}
	if len(v4) != 4 || len(v6) != 16 {
		t.Fatalf("fixture shapes: v4 len %d, v6 len %d", len(v4), len(v6))
	}
	both := []IP{v4, v6}
	cases := []struct {
		name    string
		network string
		ips     []IP
		want    IP
	}{
		{"tcp-picks-first", "tcp", both, v4},
		{"empty-network-picks-first", "", both, v4},
		{"tcp4-skips-v6", "tcp4", []IP{v6, v4}, v4},
		{"tcp4-keeps-v4", "tcp4", both, v4},
		{"tcp6-skips-v4", "tcp6", both, v6},
		{"udp4-no-v4", "udp4", []IP{v6}, nil},
		{"udp6-no-v6", "udp6", []IP{v4}, nil},
		{"ip6-rejects-v4mapped", "ip6", []IP{v4mapped}, nil},
		{"no-addrs", "tcp", nil, nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := __filterResolveIP(c.network, c.ips); !reflect.DeepEqual(got, c.want) {
				t.Fatalf("__filterResolveIP(%q, %v) = %v, want %v", c.network, c.ips, got, c.want)
			}
		})
	}
}
