// P3-M9 wave3: net/http — query/form parsing via httptest.NewRecorder
// (pure, no sockets): multi-value query, FormValue precedence,
// percent-encoding in query vs path, DetectContentType sniffing,
// ParseMultipartForm.
// SCOPE-LIMITED: needs synthetic net/http + net/http/httptest in
// frontend/v1 (imports are not supported in frontend/v1 scope).
package main

import (
	"bytes"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
)

func main() {
	// Query multi-values and encoding.
	req := httptest.NewRequest("GET", "/search?q=go&q=graal&lang=en", nil)
	q := req.URL.Query()
	fmt.Println("query-multi", len(q["q"]) == 2, q["q"][0] == "go", q["q"][1] == "graal")
	fmt.Println("query-single", q.Get("lang") == "en", q.Get("missing") == "")

	// '+' in query decodes to space; %2F stays encoded in RawQuery.
	req = httptest.NewRequest("GET", "/x?a=b+c&d=%2F", nil)
	fmt.Println("query-plus", req.URL.Query().Get("a") == "b c")
	fmt.Println("query-encslash", req.URL.Query().Get("d") == "/")

	// '+' and %2F in the PATH stay literal (no query decoding there).
	req = httptest.NewRequest("GET", "/a+b/%2F", nil)
	fmt.Println("path-plus", req.URL.Path == "/a+b//")
	fmt.Println("path-escaped", req.URL.EscapedPath() == "/a+b/%2F")

	// FormValue precedence: POST body beats query string.
	form := url.Values{"f": {"bodyval"}}
	req = httptest.NewRequest("POST", "/submit?f=queryval", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	fmt.Println("form-precedence", req.FormValue("f") == "bodyval")

	// FormValue falls back to query when the body lacks the key.
	form2 := url.Values{"other": {"x"}}
	req = httptest.NewRequest("POST", "/submit?f=queryval", strings.NewReader(form2.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	fmt.Println("form-fallback", req.FormValue("f") == "queryval")

	// ParseForm populates r.Form with both query and body.
	req = httptest.NewRequest("POST", "/p?a=1", strings.NewReader("b=2&b=3"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.ParseForm()
	fmt.Println("parseform", req.Form.Get("a") == "1",
		len(req.Form["b"]) == 2, req.PostForm.Get("b") == "2")

	// Handler over the recorder: inspect Result().
	rec := httptest.NewRecorder()
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Q", r.URL.Query().Get("q"))
		w.WriteHeader(http.StatusTeapot)
		fmt.Fprint(w, "teapot-body")
	})
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/?q=leaf", nil))
	res := rec.Result()
	body, _ := io.ReadAll(res.Body)
	res.Body.Close()
	fmt.Println("recorder", res.StatusCode == 418, res.Header.Get("X-Q") == "leaf",
		string(body) == "teapot-body")

	// ParseMultipartForm with a 10MB limit (pure; uses TMPDIR internally).
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	mw.SetBoundary("qform123")
	fw, _ := mw.CreateFormFile("doc", "a.txt")
	fw.Write([]byte("doc-bytes"))
	mw.WriteField("note", "hello")
	mw.Close()
	req = httptest.NewRequest("POST", "/up", &buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	if err := req.ParseMultipartForm(10 << 20); err != nil {
		fmt.Println("multipart-err", err)
		return
	}
	fmt.Println("multipart", req.MultipartForm.Value["note"][0] == "hello",
		req.MultipartForm.File["doc"][0].Filename == "a.txt")

	// DetectContentType sniffing on served bytes.
	fmt.Println("sniff-html", http.DetectContentType([]byte("<html><body>x")) == "text/html; charset=utf-8")
	fmt.Println("sniff-text", http.DetectContentType([]byte("just words here")) == "text/plain; charset=utf-8")
	fmt.Println("sniff-empty", http.DetectContentType(nil) == "text/plain; charset=utf-8")
	fmt.Println("sniff-png", http.DetectContentType([]byte("\x89PNG\r\n\x1a\n....")) == "image/png")

	// URL.String re-encoding round-trip: path space is escaped, but the
	// raw query (with a literal space) is preserved verbatim.
	u, _ := url.Parse("http://x:8080/p ath?q=a b#frag")
	fmt.Println("url-string", u.String() == "http://x:8080/p%20ath?q=a b#frag")

	fmt.Println("ok")
}
