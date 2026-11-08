// P3-M9 wave3: connected-UDP Read — the nil-address recvfrom path (R-1108-28).
// `net.Dial("udp", ...)` returns a connected `*UDPConn`; its `Read`
// desugars to `__udpConnRead`, which calls `netRecvfrom(cookie, b, nil)`.
// Before the fix the trap returned -EMSGSIZE (90) for the nil address
// buffer and the datagram was lost, so every connected Read failed with
// an OpError. This case pins the round-trip: dial, write, server
// ReadFrom, server reply via DialUDP, client Read.
//
// The reply must come from the server's bound address (a connected
// socket only accepts datagrams from its peer), so the server dials
// back with `DialUDP("udp", serverAddr, clientAddr)` — bound to the
// server address and connected to the client. The server closes its
// listen socket first: gc does not set SO_REUSEADDR on dial sockets,
// so dialing back from an already-bound address fails with EADDRINUSE
// (verified live). An unconnected `WriteTo` reply cannot be used either:
// this sandbox blocks unconnected sendto with EPERM even under gc
// (verified live — the pre-existing 04 case prints `writeto-err`
// under `go run` here).
package main

import (
	"fmt"
	"net"
	"strconv"
)

func main() {
	// Server: bound packet socket.
	pc1, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		fmt.Println("listen1-err")
		return
	}
	defer pc1.Close()
	addr1 := pc1.LocalAddr().(*net.UDPAddr)

	// Client: connected UDP socket. The 127.0.0.1 literal must stay on
	// the Dial line (loopback egress gate); addr1 is always 127.0.0.1:port.
	c, err := net.Dial("udp", "127.0.0.1:"+strconv.Itoa(addr1.Port))
	if err != nil {
		fmt.Println("dial-err")
		return
	}
	defer c.Close()

	msg := "hello udp-read"
	_, err = c.Write([]byte(msg))
	if err != nil {
		fmt.Println("write-err")
		return
	}

	// Server receives the datagram (ReadFrom path, unaffected by the bug).
	buf := make([]byte, 100)
	n, raddr, err := pc1.ReadFrom(buf)
	if err != nil {
		fmt.Println("readfrom-err")
		return
	}
	fmt.Println("server-recv-ok", n == len(msg) && string(buf[:n]) == msg && raddr != nil)

	// Server replies from its own bound address: close the listen
	// socket (freeing the port), then DialUDP with laddr = the server
	// address, connected to the client. The reply then arrives from
	// the peer the client's connected socket expects.
	pc1.Close()
	rudp, ok := raddr.(*net.UDPAddr)
	if !ok {
		fmt.Println("raddr-type-err")
		return
	}
	c2, err := net.DialUDP("udp", addr1, rudp)
	if err != nil {
		fmt.Println("dialudp-err")
		return
	}
	defer c2.Close()

	reply := "reply udp-read"
	_, err = c2.Write([]byte(reply))
	if err != nil {
		fmt.Println("reply-err")
		return
	}

	// Client reads on the CONNECTED socket — the nil-addr recvfrom
	// path under test.
	n, err = c.Read(buf)
	if err != nil {
		fmt.Println("read-err")
		return
	}
	fmt.Println("dialudp-ok", n == len(reply) && string(buf[:n]) == reply)

	fmt.Println("ok")
}
