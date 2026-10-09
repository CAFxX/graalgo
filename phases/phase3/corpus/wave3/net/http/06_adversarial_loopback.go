// P3-M9 wave3: net/http loopback leg — raw-socket adversarial client:
// split-packet request (1-byte header writes), pipelined requests on one
// conn, Expect: 100-continue handshake, Hijacker upgrade path.
// LOOPBACK ONLY: 127.0.0.1 dial targets; no external hosts, no DNS.
// SCOPE-LIMITED: needs synthetic net/http AND TCP socket syscalls +
// netpoller (no socket intrinsics in frontend/v1).
package main

import (
	"bufio"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"
)

func main() {
	mux := http.NewServeMux()
	mux.HandleFunc("/echo", func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		fmt.Fprintf(w, "echo:%s", b)
	})
	mux.HandleFunc("/hijack", func(w http.ResponseWriter, r *http.Request) {
		hj, ok := w.(http.Hijacker)
		if !ok {
			http.Error(w, "no hijacker", 500)
			return
		}
		conn, _, err := hj.Hijack()
		if err != nil {
			return
		}
		conn.Write([]byte("HTTP/1.1 200 OK\r\nContent-Length: 8\r\nConnection: close\r\n\r\nhijacked"))
		conn.Close()
	})
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		fmt.Println("listen-err", err)
		return
	}
	srv := &http.Server{Handler: mux, ErrorLog: log.New(io.Discard, "", 0)}
	done := make(chan struct{})
	go func() { srv.Serve(ln); close(done) }()
	defer func() { srv.Close(); <-done }()
	// The 127.0.0.1 literal must stay on each Dial line (egress gate).
	port := strconv.Itoa(ln.Addr().(*net.TCPAddr).Port)

	// Split-packet: header written 1 byte at a time.
	c, err := net.Dial("tcp", "127.0.0.1:"+port)
	if err != nil {
		fmt.Println("dial-err", err)
		return
	}
	c.SetDeadline(time.Now().Add(5 * time.Second))
	raw := "POST /echo HTTP/1.1\r\nHost: x\r\nContent-Length: 5\r\n\r\nhello"
	for i := 0; i < len(raw); i++ {
		c.Write([]byte{raw[i]})
	}
	br := bufio.NewReader(c)
	line, _ := br.ReadString('\n')
	var hlen int
	for {
		hline, _ := br.ReadString('\n')
		if hline == "\r\n" || hline == "\n" || hline == "" {
			break
		}
		if strings.HasPrefix(strings.ToLower(hline), "content-length:") {
			fmt.Sscanf(hline, "Content-Length: %d", &hlen)
		}
	}
	body := make([]byte, hlen)
	io.ReadFull(br, body)
	c.Close()
	fmt.Println("split", line == "HTTP/1.1 200 OK\r\n", string(body) == "echo:hello")

	// Pipelined: two requests on one conn without waiting.
	c2, err := net.Dial("tcp", "127.0.0.1:"+port)
	if err != nil {
		fmt.Println("dial2-err", err)
		return
	}
	c2.SetDeadline(time.Now().Add(5 * time.Second))
	c2.Write([]byte("POST /echo HTTP/1.1\r\nHost: x\r\nContent-Length: 2\r\n\r\nhi"))
	c2.Write([]byte("POST /echo HTTP/1.1\r\nHost: x\r\nContent-Length: 3\r\n\r\nbye"))
	br2 := bufio.NewReader(c2)
	ok2 := true
	for i := 0; i < 2; i++ {
		resp, err := http.ReadResponse(br2, nil)
		if err != nil {
			ok2 = false
			break
		}
		b, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		want := []string{"echo:hi", "echo:bye"}[i]
		if resp.StatusCode != 200 || string(b) != want {
			ok2 = false
		}
	}
	c2.Close()
	fmt.Println("pipelined", ok2)

	// Expect: 100-continue handshake.
	c3, err := net.Dial("tcp", "127.0.0.1:"+port)
	if err != nil {
		fmt.Println("dial3-err", err)
		return
	}
	c3.SetDeadline(time.Now().Add(5 * time.Second))
	c3.Write([]byte("POST /echo HTTP/1.1\r\nHost: x\r\nContent-Length: 4\r\nExpect: 100-continue\r\n\r\n"))
	br3 := bufio.NewReader(c3)
	interim, _ := br3.ReadString('\n')
	blank, _ := br3.ReadString('\n') // blank line after the 100 status
	c3.Write([]byte("data"))
	resp3, err := http.ReadResponse(br3, nil)
	ok3 := err == nil
	var b3 []byte
	if ok3 {
		b3, _ = io.ReadAll(resp3.Body)
		resp3.Body.Close()
		ok3 = resp3.StatusCode == 200 && string(b3) == "echo:data"
	}
	c3.Close()
	fmt.Println("continue", interim == "HTTP/1.1 100 Continue\r\n", blank == "\r\n", ok3)

	// Hijacker upgrade path.
	c4, err := net.Dial("tcp", "127.0.0.1:"+port)
	if err != nil {
		fmt.Println("dial4-err", err)
		return
	}
	c4.SetDeadline(time.Now().Add(5 * time.Second))
	c4.Write([]byte("GET /hijack HTTP/1.1\r\nHost: x\r\n\r\n"))
	b4, _ := io.ReadAll(c4)
	c4.Close()
	fmt.Println("hijacked", string(b4) ==
		"HTTP/1.1 200 OK\r\nContent-Length: 8\r\nConnection: close\r\n\r\nhijacked")

	fmt.Println("ok")
}
