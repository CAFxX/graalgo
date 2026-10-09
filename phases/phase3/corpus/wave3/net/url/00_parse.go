// P3-M9 wave3: net/url parsing — pure Go.
package main

import (
	"fmt"
	"net/url"
)

func main() {
	// Parse.
	u, err := url.Parse("https://user:pass@host.com:8080/path?query=1#frag")
	fmt.Println("parse-ok", err == nil)
	fmt.Println("scheme", u.Scheme == "https")
	fmt.Println("host", u.Host == "host.com:8080")
	fmt.Println("hostname", u.Hostname() == "host.com")
	fmt.Println("port", u.Port() == "8080")
	fmt.Println("path", u.Path == "/path")
	fmt.Println("query", u.RawQuery == "query=1")
	fmt.Println("frag", u.Fragment == "frag")
	pw, ok := u.User.Password()
	fmt.Println("userinfo", u.User.Username() == "user" && pw == "pass" && ok)

	// Parse errors.
	_, err = url.Parse("http://[::1%25]/")
	fmt.Println("parse-err-zone", err != nil)
	_, err = url.Parse(":")
	fmt.Println("parse-err-scheme", err != nil)

	// QueryUnescape / PathUnescape.
	s, err := url.QueryUnescape("a%20b+c")
	fmt.Println("queryunescape", err == nil && s == "a b c")
	s, err = url.PathUnescape("a%20b%2Fc")
	fmt.Println("pathunescape", err == nil && s == "a b/c")
	_, err = url.QueryUnescape("%zz")
	fmt.Println("unescape-err", err != nil)

	// ParseQuery.
	v, err := url.ParseQuery("a=1&b=2&a=3")
	fmt.Println("parsequery", err == nil && len(v["a"]) == 2 && v.Get("b") == "2")

	// URL.String round-trip.
	u2, _ := url.Parse("https://example.com/p?q=1")
	fmt.Println("string", u2.String() == "https://example.com/p?q=1")

	// ResolveReference.
	base, _ := url.Parse("https://example.com/a/b")
	ref, _ := url.Parse("../c")
	fmt.Println("resolve", base.ResolveReference(ref).String() == "https://example.com/c")

	// JoinPath.
	s, err = url.JoinPath("https://example.com/a", "b", "c")
	fmt.Println("joinpath", err == nil && s == "https://example.com/a/b/c")

	fmt.Println("ok")
}
