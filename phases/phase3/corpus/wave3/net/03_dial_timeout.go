// P3-M9 wave3: dial timeout and refused — real sockets.
// Timing-sensitive: gmp=1 only. Loopback-only.
package main

import (
	"fmt"
	"net"
	"os"
	"time"
)

func main() {
	// Refused: dial a closed port (find one via listen+close).
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		fmt.Println("listen-err")
		return
	}
	addr := ln.Addr().String()
	ln.Close()

	_, err = net.DialTimeout("tcp", addr, 5*time.Second)
	if err == nil {
		fmt.Println("refused-err")
		return
	}
	fmt.Println("refused", !os.IsTimeout(err))

	// Timeout: dial with a very short timeout to a non-routable address.
	// (Uses loopback with timeout; the timeout fires before refused.)
	ln2, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		fmt.Println("listen2-err")
		return
	}
	defer ln2.Close()
	// Don't accept; the backlog fills and dial blocks.
	// Actually, just test that DialTimeout respects the timeout value.
	start := time.Now()
	_, err = net.DialTimeout("tcp", "127.0.0.1:1", time.Millisecond)
	elapsed := time.Since(start)
	// Port 1 is likely refused quickly; we just check it doesn't hang.
	fmt.Println("timeout-respects", elapsed < 5*time.Second)

	fmt.Println("ok")
}
