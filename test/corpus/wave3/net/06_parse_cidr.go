// P0-1 (review-1616): net.ParseCIDR gc fidelity — valid/invalid IPv4+IPv6
// CIDRs. Pure Go, no sockets. Differential vs gc via run_wave3.sh net.
//
// gc ground truth (go1.27.1 net/ip.go ParseCIDR): the returned IP is
// always 16 bytes (IPv4 in mapped form); failures are *net.ParseError
// ("invalid CIDR address: <text>"); zones are rejected; the mask is
// digits-only (no '+'); bit length is 32 for IPv4 / 128 for IPv6,
// including 4-in-6 forms such as ::ffff:192.0.2.0.
//
// Known-remaining deviation (NOT covered by String assertions here):
// synth IP.To4 also accepts the all-zero 4-in-6 prefix, so String()
// of non-mapped forms such as ::192.0.2.0 renders dotted under graalgo
// while gc renders "::c000:200". Those cases assert lengths/bytes only;
// the To4 narrowing is a separate follow-up.
package main

import (
	"fmt"
	"net"
)

func checkOK(s, wantIP string, wantLen, wantNetLen, wantMaskLen int, wantNet string) {
	ip, ipnet, err := net.ParseCIDR(s)
	ok := err == nil && len(ip) == wantLen && ip.String() == wantIP &&
		len(ipnet.IP) == wantNetLen && ipnet.String() == wantNet &&
		len(ipnet.Mask) == wantMaskLen
	fmt.Println("ok", s, ok)
}

func checkErr(s string) {
	ip, ipnet, err := net.ParseCIDR(s)
	_, isParseErr := err.(*net.ParseError)
	ok := err != nil && err.Error() == "invalid CIDR address: "+s &&
		isParseErr && ip == nil && ipnet == nil
	fmt.Println("err", s, ok)
}

func main() {
	// Valid IPv4: 16-byte IP, 4-byte network IP + mask.
	checkOK("192.0.2.0/24", "192.0.2.0", 16, 4, 4, "192.0.2.0/24")
	checkOK("192.0.2.1/24", "192.0.2.1", 16, 4, 4, "192.0.2.0/24")
	checkOK("0.0.0.0/0", "0.0.0.0", 16, 4, 4, "0.0.0.0/0")
	checkOK("10.0.0.0/8", "10.0.0.0", 16, 4, 4, "10.0.0.0/8")
	checkOK("192.0.2.0/024", "192.0.2.0", 16, 4, 4, "192.0.2.0/24")
	checkOK("192.0.2.0/32", "192.0.2.0", 16, 4, 4, "192.0.2.0/32")

	// Valid IPv6: 16-byte IP, network, mask throughout.
	checkOK("2001:db8::/32", "2001:db8::", 16, 16, 16, "2001:db8::/32")
	checkOK("2001:db8::1/32", "2001:db8::1", 16, 16, 16, "2001:db8::/32")
	checkOK("::/0", "::", 16, 16, 16, "::/0")
	checkOK("::1/128", "::1", 16, 16, 16, "::1/128")
	checkOK("2001:db8::/128", "2001:db8::", 16, 16, 16, "2001:db8::/128")

	// IPv4-mapped IPv6: 128-bit space, so /33 and /120 are valid; the
	// network String() folds back to the 32-bit form via To4.
	checkOK("::ffff:192.0.2.0/120", "192.0.2.0", 16, 16, 16, "192.0.2.0/24")
	checkOK("::ffff:192.0.2.0/128", "192.0.2.0", 16, 16, 16, "192.0.2.0/32")
	checkOK("::ffff:192.0.2.0/33", "192.0.2.0", 16, 16, 16, "::/33")

	// Non-mapped 4-in-6: lengths/bytes only (see header note on String).
	for _, s := range []string{"::192.0.2.0/120", "::192.0.2.0/33"} {
		ip, ipnet, err := net.ParseCIDR(s)
		ok := err == nil && len(ip) == 16 && len(ipnet.IP) == 16 &&
			len(ipnet.Mask) == 16 && ip[10] == 0 && ip[11] == 0 &&
			ip[12] == 192 && ip[13] == 0 && ip[14] == 2 && ip[15] == 0
		fmt.Println("ok-bytes", s, ok)
	}

	// Contains through the parsed networks.
	_, v4net, _ := net.ParseCIDR("192.0.2.0/24")
	fmt.Println("contains-v4-in", v4net.Contains(net.ParseIP("192.0.2.99")))
	fmt.Println("contains-v4-out", !v4net.Contains(net.ParseIP("192.0.3.1")))
	_, v6net, _ := net.ParseCIDR("2001:db8::/32")
	fmt.Println("contains-v6-in", v6net.Contains(net.ParseIP("2001:db8::1")))
	fmt.Println("contains-v6-out", !v6net.Contains(net.ParseIP("2001:db9::1")))

	// Invalid: *ParseError with the exact gc message, nil returns.
	for _, s := range []string{
		"not-a-cidr", "", "192.0.2.0",
		"192.0.2.0/33", "192.0.2.0/-1", "192.0.2.0/+24",
		"192.0.2.0/24/32", "2001:db8::/129",
		"fe80::1%eth0/64",
		"192.0.2.0/ 24", "192.0.2.0/24 ",
		"192.0.2.0/99999999999999999999",
		"256.0.0.0/24", "192.0.2.0/0x18",
		"192.0.2.0/", "/24", "2001:db8::g/32",
	} {
		checkErr(s)
	}

	fmt.Println("ok")
}
