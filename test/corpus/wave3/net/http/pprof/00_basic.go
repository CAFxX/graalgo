package main

// L11 (REVIEW-2026-11-18-05): net/http/pprof — zero corpus imports
// before this program. The pprof handlers are registered by the blank
// import: the mux resolves them, and /debug/pprof/cmdline answers
// 200. Only statuses and presence booleans are printed (profile
// bodies carry live goroutine counts and binary paths).

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	_ "net/http/pprof"
)

func main() {
	for _, path := range []string{"/debug/pprof/", "/debug/pprof/cmdline", "/debug/pprof/heap", "/debug/pprof/goroutine", "/debug/pprof/nosuch"} {
		req := httptest.NewRequest("GET", path, nil)
		h, pattern := http.DefaultServeMux.Handler(req)
		fmt.Printf("mux %s handler=%v pattern=%q\n", path, h != nil, pattern)
	}
	// cmdline answers 200 with a non-empty body.
	req := httptest.NewRequest("GET", "/debug/pprof/cmdline", nil)
	rec := httptest.NewRecorder()
	http.DefaultServeMux.ServeHTTP(rec, req)
	body, _ := io.ReadAll(rec.Result().Body)
	fmt.Printf("cmdline status=%d nonempty=%v\n", rec.Code, len(body) > 0)
	// The index lists the known profiles (names only, no counts).
	req2 := httptest.NewRequest("GET", "/debug/pprof/", nil)
	rec2 := httptest.NewRecorder()
	http.DefaultServeMux.ServeHTTP(rec2, req2)
	b2, _ := io.ReadAll(rec2.Result().Body)
	for _, name := range []string{"goroutine", "heap", "threadcreate", "block", "mutex"} {
		fmt.Printf("index has %s=%v\n", name, bytes.Contains(b2, []byte(name)))
	}
}
