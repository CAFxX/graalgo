// P3-M9 wave3: net.Pipe deadlines — pure Go, no sockets.
// Timing-sensitive: gmp=1 only.
package main

import (
	"fmt"
	"net"
	"os"
	"time"
)

func main() {
	c1, c2 := net.Pipe()
	defer c1.Close()
	defer c2.Close()

	// Read deadline in the past: immediate timeout.
	c1.SetReadDeadline(time.Now().Add(-time.Second))
	buf := make([]byte, 10)
	_, err := c1.Read(buf)
	fmt.Println("past-read-timeout", os.IsTimeout(err))

	// Write deadline in the past (no reader): immediate timeout.
	c1.SetWriteDeadline(time.Now().Add(-time.Second))
	_, err = c1.Write([]byte("hello"))
	fmt.Println("past-write-timeout", os.IsTimeout(err))

	// Clear deadlines: write/read works.
	c1.SetDeadline(time.Time{})
	done := make(chan bool)
	go func() {
		buf := make([]byte, 5)
		n, _ := c2.Read(buf)
		done <- (n == 5 && string(buf) == "hello")
	}()
	c1.Write([]byte("hello"))
	fmt.Println("clear-ok", <-done)

	// Future deadline that fires during blocked read.
	c1.SetReadDeadline(time.Now().Add(50 * time.Millisecond))
	start := time.Now()
	_, err = c1.Read(buf)
	elapsed := time.Since(start)
	fmt.Println("future-timeout", os.IsTimeout(err), elapsed < 5*time.Second)

	fmt.Println("ok")
}
