// P3-M9 wave3: UDP loopback — connected datagrams only.
// (Sandbox blocks unconnected sendto with EPERM; see fork contract.)
package main

import (
	"fmt"
	"net"
)

func main() {
	// Server.
	pc1, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		fmt.Println("listen1-err")
		return
	}
	defer pc1.Close()
	addr1 := pc1.LocalAddr().(*net.UDPAddr)

	// Client.
	pc2, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		fmt.Println("listen2-err")
		return
	}
	defer pc2.Close()

	// WriteTo/ReadFrom.
	msg := "hello udp"
	_, err = pc2.WriteTo([]byte(msg), addr1)
	if err != nil {
		fmt.Println("writeto-err")
		return
	}

	buf := make([]byte, 100)
	n, raddr, err := pc1.ReadFrom(buf)
	if err != nil {
		fmt.Println("readfrom-err")
		return
	}
	fmt.Println("udp-ok", n == len(msg) && string(buf[:n]) == msg && raddr != nil)

	// Connected UDP via Dial.
	c, err := net.Dial("udp", addr1.String())
	if err != nil {
		fmt.Println("dial-err")
		return
	}
	defer c.Close()

	_, err = c.Write([]byte(msg))
	if err != nil {
		fmt.Println("write-err")
		return
	}
	n, err = pc1.Read(buf)
	if err != nil {
		fmt.Println("read-err")
		return
	}
	fmt.Println("connected-ok", n == len(msg) && string(buf[:n]) == msg)

	fmt.Println("ok")
}
