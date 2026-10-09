// P3-M9 wave3: net address parsing — pure Go, no sockets.
package main

import (
	"fmt"
	"net"
)

func main() {
	// ParseIP.
	fmt.Println("parseip4", net.ParseIP("192.0.2.1").String() == "192.0.2.1")
	fmt.Println("parseip6", net.ParseIP("2001:db8::68").String() == "2001:db8::68")
	fmt.Println("parseip-nil", net.ParseIP("not-an-ip") == nil)
	fmt.Println("parseip-loopback", net.ParseIP("127.0.0.1").IsLoopback())

	// ParseCIDR.
	ip, ipnet, err := net.ParseCIDR("192.0.2.0/24")
	fmt.Println("cidr-ok", err == nil && ip.String() == "192.0.2.0" && ipnet.String() == "192.0.2.0/24")
	_, _, err = net.ParseCIDR("not-a-cidr")
	fmt.Println("cidr-err", err != nil)

	// ParseMAC.
	mac, err := net.ParseMAC("01:23:45:67:89:ab")
	fmt.Println("mac-ok", err == nil && mac.String() == "01:23:45:67:89:ab")
	_, err = net.ParseMAC("not-a-mac")
	fmt.Println("mac-err", err != nil)

	// JoinHostPort / SplitHostPort.
	fmt.Println("join4", net.JoinHostPort("192.0.2.1", "80") == "192.0.2.1:80")
	fmt.Println("join6", net.JoinHostPort("2001:db8::1", "80") == "[2001:db8::1]:80")
	host, port := net.SplitHostPort("192.0.2.1:80")
	fmt.Println("split", host == "192.0.2.1" && port == "80")
	host, port = net.SplitHostPort("[2001:db8::1]:80")
	fmt.Println("split6", host == "2001:db8::1" && port == "80")

	// ResolveTCPAddr / ResolveUDPAddr (IP literals, no DNS).
	tcpAddr, err := net.ResolveTCPAddr("tcp", "127.0.0.1:8080")
	fmt.Println("resolve-tcp", err == nil && tcpAddr.IP.String() == "127.0.0.1" && tcpAddr.Port == 8080)
	udpAddr, err := net.ResolveUDPAddr("udp", "[::1]:9090")
	fmt.Println("resolve-udp", err == nil && udpAddr.Port == 9090)

	fmt.Println("ok")
}
