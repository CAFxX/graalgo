package main

// L11 (REVIEW-2026-11-18-05): net/http/fcgi — zero corpus imports
// before this program. Serving an HTTP handler over FastCGI: a
// loopback listener, one request through the FCGI server, status and
// body pinned.

import (
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/fcgi"
	"time"
)

func main() {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		fmt.Printf("listen-err=%v\n", err != nil)
		return
	}
	defer ln.Close()
	done := make(chan struct{})
	go func() {
		defer close(done)
		fcgi.Serve(ln, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			fmt.Fprintf(w, "fcgi hello %s %s", r.Method, r.URL.Path)
		}))
	}()
	url := "http://" + ln.Addr().String() + "/demo"
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		fmt.Printf("get-err=%v\n", err != nil)
		ln.Close()
		<-done
		return
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	fmt.Printf("status=%d body=%q\n", resp.StatusCode, string(body))
	ln.Close()
	<-done
	fmt.Printf("served=true\n")
}
