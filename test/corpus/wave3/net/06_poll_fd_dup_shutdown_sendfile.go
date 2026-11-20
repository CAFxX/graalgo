// P3-M9 wave3: poll FD surface — Dup, Shutdown, SendFile fallback.
// Loopback-only: 127.0.0.1:0, ephemeral port, fixed outcome strings.
package main

import (
	"fmt"
	"io"
	"net"
	"os"
	"strconv"
	"time"
)

func main() {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		fmt.Println("listen-err")
		return
	}
	defer ln.Close()
	addr := ln.Addr().(*net.TCPAddr)

	// Dup: File() dups the listener fd (poll.DupCloseOnExec); closing
	// the file must not kill the listener — it still accepts after.
	f, err := ln.(*net.TCPListener).File()
	fmt.Println("dup-ok", err == nil)
	if err == nil {
		f.Close()
	}

	// Temp file payload for the SendFile fallback (io.Copy from a
	// *os.File to a *net.TCPConn uses poll.SendFile; graalgo returns
	// handled=false and net falls back to the generic copy).
	payload := "sendfile-payload-12345"
	tmp, err := os.CreateTemp("", "pollfd-sendfile-*")
	if err != nil {
		fmt.Println("tempfile-err")
		return
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.WriteString(payload); err != nil {
		fmt.Println("tempfile-err")
		return
	}
	tmp.Close()

	accepted := make(chan net.Conn, 1)
	go func() {
		c, err := ln.Accept()
		if err != nil {
			return
		}
		accepted <- c
	}()

	c, err := net.Dial("tcp", "127.0.0.1:"+strconv.Itoa(addr.Port))
	if err != nil {
		fmt.Println("dial-err")
		return
	}
	defer c.Close()
	var srv net.Conn
	select {
	case srv = <-accepted:
	case <-time.After(10 * time.Second):
		fmt.Println("accept-timeout")
		return
	}
	defer srv.Close()

	// Server sends the file, then half-closes its write side so the
	// client's ReadAll terminates.
	go func() {
		file, err := os.Open(tmp.Name())
		if err != nil {
			return
		}
		defer file.Close()
		io.Copy(srv, file)
		srv.(*net.TCPConn).CloseWrite()
	}()
	got, err := io.ReadAll(c)
	fmt.Println("sendfile-ok", err == nil && string(got) == payload)

	// Shutdown: client half-closes its write side; the server's next
	// read must see EOF.
	tc := c.(*net.TCPConn)
	tc.CloseWrite()
	buf := make([]byte, 1)
	_, err = srv.Read(buf)
	fmt.Println("shutdown-ok", err == io.EOF)

	fmt.Println("ok")
}
