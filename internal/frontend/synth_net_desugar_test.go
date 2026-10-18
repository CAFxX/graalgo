package main

import (
	"testing"
)

// TestM9NetDesugarWiring pins the 50 P3-M9 (tier-4) m9MultiResultFuncs
// entries added for the synthetic net packages (Appendix M). It follows
// the TestM7StrconvJsonDesugarWiring pattern: for each entry it verifies,
// against the synthetic sources in
// synth_net.go / synth_netip.go / synth_neturl.go:
//
//  1. exactly one table entry exists for (pkgPath, name, recvType), with
//     the expected helper name, pair type, and pair fields in order;
//  2. the multi-result function (or method on the named receiver type)
//     exists in the synthetic package scope;
//  3. the __-helper exists in the synthetic package scope and is a
//     *types.Func;
//  4. the helper's parameters accept the multi-result call's arguments
//     positionally (the desugar passes the original call's args straight
//     through, receiver prepended for methods);
//  5. the pair struct exists with exactly the listed fields in order,
//     each field's type identical to the multi-result signature's
//     corresponding result type.
//
// All 48 current entries keep their __-helpers in the same synthetic
// package (helperPkg == ""), which the checker pins as well.
//
// This test fails on the pre-net tree (no table entries for these
// functions) and fails on any wiring typo (wrong helper name, wrong pair
// type, wrong field name/order, arity mismatch).
type m9NetExpect struct {
	pkgPath    string
	name       string
	recvType   string
	helper     string
	pairType   string
	pairFields []string
}

var m9NetTable = []m9NetExpect{
	// net/netip: package-level funcs.
	{"net/netip", "ParseAddr", "", "__parseAddr", "parseAddrPair", []string{"addr", "err"}},
	{"net/netip", "ParsePrefix", "", "__parsePrefix", "parsePrefixPair", []string{"prefix", "err"}},
	{"net/netip", "ParseAddrPort", "", "__parseAddrPort", "parseAddrPortPair", []string{"ap", "err"}},
	{"net/netip", "AddrFromSlice", "", "__addrFromSlice", "addrFromSlicePair", []string{"ip", "ok"}},
	// net/netip: methods on Addr (prependRecv).
	{"net/netip", "Prefix", "Addr", "__addrPrefix", "prefixPair", []string{"prefix", "err"}},
	{"net/netip", "AppendText", "Addr", "__addrAppendText", "bytesErrPair", []string{"b", "err"}},
	{"net/netip", "MarshalText", "Addr", "__addrMarshalText", "bytesErrPair", []string{"b", "err"}},
	{"net/netip", "AppendBinary", "Addr", "__addrAppendBinary", "bytesErrPair", []string{"b", "err"}},
	{"net/netip", "MarshalBinary", "Addr", "__addrMarshalBinary", "bytesErrPair", []string{"b", "err"}},
	// net/netip: methods on AddrPort (prependRecv).
	{"net/netip", "AppendText", "AddrPort", "__addrPortAppendText", "bytesErrPair", []string{"b", "err"}},
	{"net/netip", "MarshalText", "AddrPort", "__addrPortMarshalText", "bytesErrPair", []string{"b", "err"}},
	{"net/netip", "AppendBinary", "AddrPort", "__addrPortAppendBinary", "bytesErrPair", []string{"b", "err"}},
	{"net/netip", "MarshalBinary", "AddrPort", "__addrPortMarshalBinary", "bytesErrPair", []string{"b", "err"}},
	// net/netip: methods on Prefix (prependRecv).
	{"net/netip", "AppendText", "Prefix", "__prefixAppendText", "bytesErrPair", []string{"b", "err"}},
	{"net/netip", "MarshalText", "Prefix", "__prefixMarshalText", "bytesErrPair", []string{"b", "err"}},
	{"net/netip", "AppendBinary", "Prefix", "__prefixAppendBinary", "bytesErrPair", []string{"b", "err"}},
	{"net/netip", "MarshalBinary", "Prefix", "__prefixMarshalBinary", "bytesErrPair", []string{"b", "err"}},
	// net/url: package-level funcs.
	{"net/url", "QueryUnescape", "", "__queryUnescape", "stringErrPair", []string{"s", "err"}},
	{"net/url", "PathUnescape", "", "__pathUnescape", "stringErrPair", []string{"s", "err"}},
	{"net/url", "Parse", "", "__parseURLPub", "parseURLPair", []string{"u", "err"}},
	{"net/url", "ParseRequestURI", "", "__parseRequestURIPub", "parseURLPair", []string{"u", "err"}},
	{"net/url", "ParseQuery", "", "__parseQueryPub", "parseQueryPair", []string{"v", "err"}},
	{"net/url", "JoinPath", "", "__joinPathPub", "stringErrPair", []string{"s", "err"}},
	// net/url: methods (prependRecv).
	{"net/url", "Password", "Userinfo", "__userinfoPassword", "passwordPair", []string{"s", "ok"}},
	{"net/url", "Parse", "URL", "__urlParsePub", "parseURLPair", []string{"u", "err"}},
	{"net/url", "MarshalBinary", "URL", "__urlMarshalBinary", "bytesErrPair", []string{"b", "err"}},
	{"net/url", "AppendBinary", "URL", "__urlAppendBinary", "bytesErrPair", []string{"b", "err"}},
	// net: package-level funcs (socket path; __-helpers in synth_net.go).
	{"net", "Pipe", "", "__pipe", "pipePair", []string{"c1", "c2"}},
	{"net", "Dial", "", "__dial", "dialPair", []string{"c", "err"}},
	{"net", "DialTimeout", "", "__dialTimeout", "dialPair", []string{"c", "err"}},
	{"net", "Listen", "", "__listen", "listenPair", []string{"l", "err"}},
	{"net", "ListenPacket", "", "__listenPacket", "listenPacketPair", []string{"c", "err"}},
	{"net", "ResolveIPAddr", "", "__resolveIPAddr", "resolveIPAddrPair", []string{"ip", "err"}},
	{"net", "ResolveTCPAddr", "", "__resolveTCPAddr", "resolveTCPAddrPair", []string{"addr", "err"}},
	{"net", "ResolveUDPAddr", "", "__resolveUDPAddr", "resolveUDPAddrPair", []string{"addr", "err"}},
	{"net", "LookupHost", "", "__lookupHost", "lookupHostPair", []string{"addrs", "err"}},
	{"net", "LookupIP", "", "__lookupIP", "lookupIPPair", []string{"ips", "err"}},
	{"net", "ParseCIDR", "", "__parseCIDR", "parseCIDRPair", []string{"ip", "ipnet", "err"}},
	{"net", "ParseMAC", "", "__parseMAC", "parseMACPair", []string{"hw", "err"}},
	{"net", "SplitHostPort", "", "__splitHostPort", "splitHostPortPair", []string{"host", "port", "err"}},
	{"net", "Size", "IPMask", "__ipMaskSize", "maskSizePair", []string{"ones", "bits"}},
	// net: methods on TCPListener / Conn (prependRecv).
	{"net", "Accept", "TCPListener", "__tcpListenerAccept", "acceptPair", []string{"c", "err"}},
	{"net", "Read", "TCPConn", "__tcpConnRead", "readPair", []string{"n", "err"}},
	{"net", "Write", "TCPConn", "__tcpConnWrite", "writePair", []string{"n", "err"}},
	{"net", "Read", "UDPConn", "__udpConnRead", "readPair", []string{"n", "err"}},
	{"net", "Write", "UDPConn", "__udpConnWrite", "writePair", []string{"n", "err"}},
	{"net", "ReadFrom", "UDPConn", "__udpConnReadFrom", "readFromPair", []string{"n", "addr", "err"}},
	{"net", "WriteTo", "UDPConn", "__udpConnWriteTo", "writePair", []string{"n", "err"}},
	// net: methods on pipeConn (net.Pipe; prependRecv).
	{"net", "Read", "pipeConn", "__pipeRead", "__pipeReadPair", []string{"n", "err"}},
	{"net", "Write", "pipeConn", "__pipeWrite", "__pipeWritePair", []string{"n", "err"}},
}

func TestM9NetDesugarWiring(t *testing.T) {
	if len(m9NetTable) != 50 {
		t.Fatalf("m9NetTable has %d entries, want 50", len(m9NetTable))
	}
	for _, want := range m9NetTable {
		name := want.pkgPath + "." + want.name
		if want.recvType != "" {
			name = want.pkgPath + "." + want.recvType + "." + want.name
		}
		t.Run(name, func(t *testing.T) {
			checkM9DesugarWiring(t, want)
		})
	}
}

// checkM9DesugarWiring verifies a single m9MultiResultFuncs table entry
// against its synthetic package. It mirrors checkM7DesugarWiring (same 5
// checks) but reads the m9 table and pins helperPkg == "" (all net
// entries keep the __-helper in the declaring package).
func checkM9DesugarWiring(t *testing.T, want m9NetExpect) {
	t.Helper()
	name := want.pkgPath + "." + want.name
	if want.recvType != "" {
		name = want.pkgPath + "." + want.recvType + "." + want.name
	}
	// 1. Exactly one m9 table entry for (pkgPath, name, recvType)
	//    carrying the expected helper/pair/fields, in the declaring
	//    package.
	var got *m9MultiResultFunc
	for i := range m9MultiResultFuncs {
		mf := &m9MultiResultFuncs[i]
		if mf.pkgPath == want.pkgPath && mf.name == want.name && mf.recvType == want.recvType {
			if got != nil {
				t.Fatalf("duplicate m9MultiResultFuncs entry for %s (recv %q)", name, want.recvType)
			}
			got = mf
		}
	}
	if got == nil {
		t.Fatalf("no m9MultiResultFuncs entry for %s (recv %q)", name, want.recvType)
	}
	if got.helperPkg != "" {
		t.Fatalf("table helperPkg = %q for %s, want \"\" (helper must live in %s)", got.helperPkg, name, want.pkgPath)
	}
	if got.helper != want.helper {
		t.Fatalf("table helper = %q, want %q", got.helper, want.helper)
	}
	if got.pairType != want.pairType {
		t.Fatalf("table pairType = %q, want %q", got.pairType, want.pairType)
	}
	if len(got.pairFields) != len(want.pairFields) {
		t.Fatalf("table pairFields = %v, want %v", got.pairFields, want.pairFields)
	}
	for i, f := range want.pairFields {
		if got.pairFields[i] != f {
			t.Fatalf("table pairFields = %v, want %v", got.pairFields, want.pairFields)
		}
	}

	// Reuse the shared synthetic-package verification (steps 2-5) by
	// converting to the strconv/json expectation type (identical shape).
	// The m9 table entry itself was already pinned above.
	checkDesugarWiringSynthetic(t, m7StrconvJsonExpect{
		pkgPath:    want.pkgPath,
		name:       want.name,
		recvType:   want.recvType,
		helper:     want.helper,
		pairType:   want.pairType,
		pairFields: want.pairFields,
	})
}
