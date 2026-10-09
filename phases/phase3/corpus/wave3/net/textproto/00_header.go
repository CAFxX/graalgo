// P3-M9 wave3: net/textproto MIMEHeader — pure Go.
package main

import (
	"fmt"
	"net/textproto"
)

func main() {
	// CanonicalMIMEHeaderKey.
	fmt.Println("canon", textproto.CanonicalMIMEHeaderKey("content-type") == "Content-Type")
	fmt.Println("canon2", textproto.CanonicalMIMEHeaderKey("X-Custom-Header") == "X-Custom-Header")
	fmt.Println("canon-empty", textproto.CanonicalMIMEHeaderKey("") == "")

	// TrimString.
	fmt.Println("trim", textproto.TrimString("  hello  ") == "hello")
	fmt.Println("trim-tabs", textproto.TrimString("\t\nhello\r\n") == "hello")

	// MIMEHeader.
	h := make(textproto.MIMEHeader)
	h.Set("Content-Type", "text/html")
	h.Add("X-Custom", "a")
	h.Add("X-Custom", "b")
	fmt.Println("get", h.Get("content-type") == "text/html")
	fmt.Println("values", len(h.Values("X-Custom")) == 2)
	fmt.Println("get-missing", h.Get("Missing") == "")
	h.Del("X-Custom")
	fmt.Println("del", h.Get("X-Custom") == "")

	fmt.Println("ok")
}
