// P3-M9 wave3: TCP loopback — real sockets via netpoller.
// Loopback-only: 127.0.0.1:0, ephemeral port, fixed outcome strings.
package main

import (
	"fmt"
	"io"
	"net"
	"strconv"
)

func main() {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		fmt.Println("listen-err")
		return
	}
	defer ln.Close()

	addr := ln.Addr().(*net.TCPAddr)
	fmt.Println("listening", addr.IP.IsLoopback())

	// Echo server.
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

	// Dial and echo. The 127.0.0.1 literal must stay on the Dial line
	// (loopback egress gate); addr is always 127.0.0.1:port.
	c, err := net.Dial("tcp", "127.0.0.1:"+strconv.Itoa(addr.Port))
	if err != nil {
		fmt.Println("dial-err")
		return
	}
	defer c.Close()

	fmt.Println("dial-ok", c.RemoteAddr().String() == addr.String())

	msg := "hello loopback"
	_, err = c.Write([]byte(msg))
	if err != nil {
		fmt.Println("write-err")
		return
	}

	buf := make([]byte, len(msg))
	_, err = io.ReadFull(c, buf)
	if err != nil {
		fmt.Println("read-err")
		return
	}
	fmt.Println("echo-ok", string(buf) == msg)

	fmt.Println("ok")
}
