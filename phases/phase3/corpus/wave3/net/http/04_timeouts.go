// P3-M9 wave3: net/http loopback leg — client Timeout and server
// ReadTimeout. Timing-sensitive: prints fixed outcome classes only.
// LOOPBACK ONLY: httptest / 127.0.0.1; no external hosts, no DNS.
// SCOPE-LIMITED: needs synthetic net/http AND TCP socket syscalls +
// netpoller (no socket intrinsics in frontend/v1).
package main

import (
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"time"
)

func main() {
	// Client timeout: handler sleeps 2s, client gives up after 100ms.
	slow := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(2 * time.Second)
		fmt.Fprint(w, "too-late")
	}))
	defer slow.Close()
	cl := &http.Client{Timeout: 100 * time.Millisecond}
	_, err := cl.Get(slow.URL)
	var ue *url.Error
	timeout := errors.As(err, &ue) && ue.Timeout()
	fmt.Println("client-timeout", timeout)
	if !timeout {
		fmt.Println("client-timeout-err", err)
	}

	// Server ReadTimeout: slow-loris header drip; server closes the conn.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		fmt.Println("listen-err", err)
		return
	}
	srv := &http.Server{
		Handler:     http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, "never") }),
		ReadTimeout: 50 * time.Millisecond,
	}
	done := make(chan struct{})
	go func() { srv.Serve(ln); close(done) }()
	addr := ln.Addr().(*net.TCPAddr)
	port := strconv.Itoa(addr.Port)

	// The 127.0.0.1 literal must stay on the Dial line (egress gate).
	c, err := net.Dial("tcp", "127.0.0.1:"+port)
	if err != nil {
		fmt.Println("dial-err", err)
		return
	}
	c.Write([]byte("GET / HTTP/1.1\r\nHost: x\r\nX-Drip: "))
	time.Sleep(300 * time.Millisecond) // exceed the server's ReadTimeout
	c.Write([]byte("done\r\n\r\n"))
	c.SetReadDeadline(time.Now().Add(2 * time.Second))
	b, _ := io.ReadAll(c)
	c.Close()
	// The server never got complete headers, so it never wrote a byte.
	fmt.Println("slowloris-closed", len(b) == 0)
	srv.Close()
	<-done

	// No timeout on a fast server: control case.
	fast := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "fast")
	}))
	defer fast.Close()
	cl2 := &http.Client{Timeout: 5 * time.Second}
	resp, err := cl2.Get(fast.URL)
	if err != nil {
		fmt.Println("fast-err", err)
		return
	}
	b2, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	fmt.Println("fast-ok", resp.StatusCode == 200, string(b2) == "fast")

	fmt.Println("ok")
}
