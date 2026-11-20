package main

// L11 (REVIEW-2026-11-18-05): net/http/cookiejar — zero corpus
// imports before this program. Cookie storage: SetCookies/Cookies
// round-trip, domain and path matching, expiry, and Jar lookup of
// missing names.

import (
	"fmt"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"sort"
	"time"
)

func show(label string, jar *cookiejar.Jar, rawurl string) {
	u, _ := url.Parse(rawurl)
	cookies := jar.Cookies(u)
	names := make([]string, 0, len(cookies))
	vals := map[string]string{}
	for _, c := range cookies {
		names = append(names, c.Name)
		vals[c.Name] = c.Value
	}
	sort.Strings(names)
	fmt.Printf("%s %s => %v\n", label, rawurl, names)
	for _, n := range names {
		fmt.Printf("  %s=%s\n", n, vals[n])
	}
}

func main() {
	jar, err := cookiejar.New(nil)
	if err != nil {
		fmt.Printf("new-err=%v\n", err != nil)
		return
	}
	base, _ := url.Parse("http://example.com/")
	jar.SetCookies(base, []*http.Cookie{
		{Name: "session", Value: "abc123", Path: "/", MaxAge: 3600},
		{Name: "prefs", Value: "dark", Path: "/settings", MaxAge: 3600},
		{Name: "gone", Value: "x", Expires: time.Now().Add(-time.Hour)},
	})
	show("root", jar, "http://example.com/")
	show("settings", jar, "http://example.com/settings")
	show("other-path", jar, "http://example.com/other")
	show("subdomain", jar, "http://sub.example.com/")
	show("other-host", jar, "http://other.com/")
	// A domain cookie is sent to subdomains.
	jar.SetCookies(base, []*http.Cookie{
		{Name: "shared", Value: "yes", Domain: "example.com", Path: "/"},
	})
	show("subdomain-after-domain", jar, "http://sub.example.com/")
	show("other-host-after-domain", jar, "http://other.com/")
	// Overwriting a cookie replaces it.
	jar.SetCookies(base, []*http.Cookie{
		{Name: "session", Value: "def456", Path: "/", MaxAge: 3600},
	})
	show("after-overwrite", jar, "http://example.com/")
	// Secure cookies are not sent over http.
	jar.SetCookies(base, []*http.Cookie{
		{Name: "secure-one", Value: "s", Path: "/", Secure: true},
	})
	show("http-secure", jar, "http://example.com/")
	show("https-secure", jar, "https://example.com/")
}
