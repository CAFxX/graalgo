// P3-M9 wave3: net/netip parsing — pure Go.
package main

import (
	"fmt"
	"net/netip"
)

func main() {
	// ParseAddr.
	a, err := netip.ParseAddr("192.0.2.1")
	fmt.Println("parse4", err == nil && a.String() == "192.0.2.1" && a.Is4())
	a, err = netip.ParseAddr("2001:db8::68")
	fmt.Println("parse6", err == nil && a.String() == "2001:db8::68" && a.Is6())
	_, err = netip.ParseAddr("not-an-ip")
	fmt.Println("parse-err", err != nil)

	// ParsePrefix.
	p, err := netip.ParsePrefix("192.0.2.0/24")
	fmt.Println("prefix", err == nil && p.String() == "192.0.2.0/24" && p.Bits() == 24)
	_, err = netip.ParsePrefix("192.0.2.0/33")
	fmt.Println("prefix-err", err != nil)

	// ParseAddrPort.
	ap, err := netip.ParseAddrPort("192.0.2.1:80")
	fmt.Println("addrport", err == nil && ap.String() == "192.0.2.1:80" && ap.Port() == 80)
	_, err = netip.ParseAddrPort("192.0.2.1:99999")
	fmt.Println("addrport-err", err != nil)

	// AddrFromSlice.
	a, ok := netip.AddrFromSlice([]byte{192, 0, 2, 1})
	fmt.Println("fromslice", ok && a.String() == "192.0.2.1")
	_, ok = netip.AddrFromSlice([]byte{1, 2, 3})
	fmt.Println("fromslice-err", !ok)

	// Methods.
	a, _ = netip.ParseAddr("192.0.2.1")
	fmt.Println("is4", a.Is4(), "is6", a.Is6(), "isloopback", a.IsLoopback())
	a, _ = netip.ParseAddr("127.0.0.1")
	fmt.Println("loopback", a.IsLoopback())
	a, _ = netip.ParseAddr("::1")
	fmt.Println("loopback6", a.IsLoopback())

	fmt.Println("ok")
}
