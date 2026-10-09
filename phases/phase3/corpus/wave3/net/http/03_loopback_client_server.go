// P3-M9 wave3: net/http loopback leg — httptest server on 127.0.0.1:0,
// keep-alive connection reuse, http.Get/http.Post wrappers, Server.Close.
// LOOPBACK ONLY: httptest binds 127.0.0.1; no external hosts, no DNS.
// SCOPE-LIMITED: needs synthetic net/http AND TCP socket syscalls +
// netpoller (no socket intrinsics in frontend/v1).
package main

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
)

func main() {
	var mu sync.Mutex
	var remotes []string
	mux := http.NewServeMux()
	mux.HandleFunc("GET /hello", func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		remotes = append(remotes, r.RemoteAddr)
		mu.Unlock()
		fmt.Fprint(w, "hello")
	})
	mux.HandleFunc("POST /echo", func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		w.Write(b)
	})

	s := httptest.NewServer(mux)
	defer s.Close()
	cl := s.Client()

	// Three sequential GETs over keep-alive: same RemoteAddr on the server.
	for i := 0; i < 3; i++ {
		resp, err := cl.Get(s.URL + "/hello")
		if err != nil {
			fmt.Println("get-err", err)
			return
		}
		b, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode != 200 || string(b) != "hello" {
			fmt.Println("get-bad", resp.StatusCode, string(b))
			return
		}
	}
	mu.Lock()
	reuse := len(remotes) == 3 && remotes[0] == remotes[1] && remotes[1] == remotes[2]
	mu.Unlock()
	fmt.Println("keepalive-reuse", reuse)

	// http.Post convenience wrapper.
	resp, err := cl.Post(s.URL+"/echo", "text/plain", strings.NewReader("post-body"))
	if err != nil {
		fmt.Println("post-err", err)
		return
	}
	b, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	fmt.Println("post", resp.StatusCode == 200, string(b) == "post-body")

	// HEAD request: no body, Content-Length preserved.
	resp, err = cl.Head(s.URL + "/hello")
	if err != nil {
		fmt.Println("head-err", err)
		return
	}
	b, _ = io.ReadAll(resp.Body)
	resp.Body.Close()
	fmt.Println("head", resp.StatusCode == 200, len(b) == 0,
		resp.ContentLength == 5)

	// Server.Close drains: further requests fail.
	s.Close()
	_, err = cl.Get(s.URL + "/hello")
	fmt.Println("closed-err", err != nil)

	// A second server: default client via http.Get against loopback URL.
	s2 := httptest.NewServer(mux)
	defer s2.Close()
	resp, err = http.Get(s2.URL + "/hello")
	if err != nil {
		fmt.Println("default-get-err", err)
		return
	}
	b, _ = io.ReadAll(resp.Body)
	resp.Body.Close()
	fmt.Println("default-get", resp.StatusCode == 200, string(b) == "hello")

	fmt.Println("ok")
}
