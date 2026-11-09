// P3-M9 wave3: DNS — LookupHost/LookupIP + Dial by hostname.
// Loopback-only: localhost / 127.0.0.1 / ::1, ephemeral port, fixed
// outcome strings. No external host is ever resolved (the sandbox DNS
// is a wildcard, so the unknown-name case uses a syntactically invalid
// name, which gc rejects locally with a DNSError).
package main

import (
	"fmt"
	"io"
	"net"
	"strconv"
)

func has(addrs []string, want string) bool {
	for _, a := range addrs {
		if a == want {
			return true
		}
	}
	return false
}

func hasIP(ips []net.IP, want string) bool {
	for _, ip := range ips {
		if ip.String() == want {
			return true
		}
	}
	return false
}

func isDNSError(err error) bool {
	_, ok := err.(*net.DNSError)
	return ok
}

func main() {
	// LookupHost("localhost") must return 127.0.0.1 and ::1 (any order:
	// gc itself returns v4-first from LookupHost and v6-first from
	// LookupIP, so pin the set, not the order).
	addrs, err := net.LookupHost("localhost")
	fmt.Println("lookuphost-ok", err == nil && has(addrs, "127.0.0.1") && has(addrs, "::1"))

	// IP literals pass through unchanged.
	addrs, err = net.LookupHost("127.0.0.1")
	fmt.Println("lookuphost-literal", err == nil && len(addrs) == 1 && addrs[0] == "127.0.0.1")
	addrs, err = net.LookupHost("::1")
	fmt.Println("lookuphost-literal6", err == nil && len(addrs) == 1 && addrs[0] == "::1")

	// LookupIP("localhost").
	ips, err := net.LookupIP("localhost")
	fmt.Println("lookupip-ok", err == nil && hasIP(ips, "127.0.0.1") && hasIP(ips, "::1"))

	// Empty host and syntactically invalid names are DNSErrors, not panics.
	_, err = net.LookupHost("")
	fmt.Println("lookuphost-empty", isDNSError(err))
	_, err = net.LookupHost("!!!")
	fmt.Println("lookuphost-invalid", isDNSError(err))

	// LookupPort: named service from /etc/services, numeric, unknown.
	port, err := net.LookupPort("tcp", "http")
	fmt.Println("lookupport-http", err == nil && port == 80)
	port, err = net.LookupPort("tcp", "8080")
	fmt.Println("lookupport-numeric", err == nil && port == 8080)
	_, err = net.LookupPort("tcp", "nosuchservice-xyz")
	fmt.Println("lookupport-unknown", err != nil)

	// ResolveTCPAddr by hostname, with family filtering.
	tcpAddr, err := net.ResolveTCPAddr("tcp", "localhost:80")
	fmt.Println("resolve-tcp-host", err == nil && tcpAddr.IP.IsLoopback() && tcpAddr.Port == 80)
	tcpAddr, err = net.ResolveTCPAddr("tcp4", "localhost:80")
	fmt.Println("resolve-tcp4-host", err == nil && tcpAddr.IP.To4() != nil)
	tcpAddr, err = net.ResolveTCPAddr("tcp6", "localhost:80")
	fmt.Println("resolve-tcp6-host", err == nil && tcpAddr.IP.To4() == nil)

	// Dial by hostname: listen on localhost:0, dial localhost:port, echo.
	ln, err := net.Listen("tcp", "localhost:0")
	if err != nil {
		fmt.Println("dial-host-ok", false)
		return
	}
	defer ln.Close()
	addr := ln.Addr().(*net.TCPAddr)

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

	c, err := net.Dial("tcp", "localhost:"+strconv.Itoa(addr.Port))
	if err != nil {
		fmt.Println("dial-host-ok", false)
		return
	}
	defer c.Close()
	fmt.Println("dial-host-ok", c.RemoteAddr().String() == addr.String())

	msg := "hello hostname"
	_, err = c.Write([]byte(msg))
	if err != nil {
		fmt.Println("echo-ok", false)
		return
	}
	buf := make([]byte, len(msg))
	_, err = io.ReadFull(c, buf)
	if err != nil {
		fmt.Println("echo-ok", false)
		return
	}
	fmt.Println("echo-ok", string(buf) == msg)

	fmt.Println("ok")
}
