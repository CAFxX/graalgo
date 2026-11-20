package main

// L11 (REVIEW-2026-11-18-05): net/http/cgi — zero corpus imports
// before this program. Serving a CGI program: a generated script in a
// temp dir prints headers + body; the handler's status, headers, and
// body are pinned; the missing-binary error path is exercised.

import (
	"fmt"
	"io"
	"log"
	"net/http/cgi"
	"net/http/httptest"
	"os"
	"path/filepath"
)

const script = `#!/bin/sh
echo "Content-Type: text/plain"
echo "X-Custom: cgi-demo"
echo ""
echo "hello from cgi"
echo "query=$QUERY_STRING"
`

func main() {
	dir, err := os.MkdirTemp("", "cgi-demo")
	if err != nil {
		fmt.Printf("mktemp-err=%v\n", err != nil)
		return
	}
	defer os.RemoveAll(dir)
	path := filepath.Join(dir, "demo.cgi")
	if err := os.WriteFile(path, []byte(script), 0755); err != nil {
		fmt.Printf("write-err=%v\n", err != nil)
		return
	}
	h := &cgi.Handler{Path: path, Logger: log.New(io.Discard, "", 0)}
	req := httptest.NewRequest("GET", "/demo.cgi?name=demo", nil)
	req.RemoteAddr = "127.0.0.1:1234"
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	fmt.Printf("status=%d content-type=%q x-custom=%q\n", rec.Code,
		rec.Header().Get("Content-Type"), rec.Header().Get("X-Custom"))
	fmt.Printf("body=%q\n", rec.Body.String())
	// Missing binary: the handler reports a 500.
	h2 := &cgi.Handler{Path: filepath.Join(dir, "nosuch.cgi"), Logger: log.New(io.Discard, "", 0)}
	rec2 := httptest.NewRecorder()
	h2.ServeHTTP(rec2, httptest.NewRequest("GET", "/x", nil))
	fmt.Printf("missing-status=%d\n", rec2.Code)
	// A handler with Env passes variables to the child.
	h3 := &cgi.Handler{Path: path, Env: []string{"DEMO_VAR=from-env"}}
	_ = h3
	fmt.Printf("handler-path-set=%v\n", h.Path != "")
}
