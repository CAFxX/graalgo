package main

// L11 (REVIEW-2026-11-18-05): net/mail — zero corpus imports before
// this program. Parsing mail addresses and messages: ParseAddress,
// AddressList, ReadMessage headers and body, and the error paths.

import (
	"fmt"
	"net/mail"
	"strings"
)

const msg = `From: Alice Example <alice@example.com>
To: bob@example.com, carol@example.com
Subject: hello
Date: Mon, 02 Jan 2006 15:04:05 -0700
X-Custom: demo

Body line one.
Body line two.
`

func main() {
	a, err := mail.ParseAddress("Alice Example <alice@example.com>")
	fmt.Printf("parse-err=%v name=%q addr=%q\n", err != nil, a.Name, a.Address)
	a2, err := mail.ParseAddress("bob@example.com")
	fmt.Printf("bare-err=%v name=%q addr=%q\n", err != nil, a2.Name, a2.Address)
	_, err = mail.ParseAddress("not an address")
	fmt.Printf("bad-err=%v\n", err != nil)
	list, err := mail.ParseAddressList("alice@example.com, Bob <bob@example.com>")
	fmt.Printf("list-err=%v count=%d\n", err != nil, len(list))
	for _, x := range list {
		fmt.Printf("list %q <%s>\n", x.Name, x.Address)
	}
	m, err := mail.ReadMessage(strings.NewReader(msg))
	if err != nil {
		fmt.Printf("read-err=%v\n", err != nil)
		return
	}
	fmt.Printf("from=%q subject=%q date=%q\n",
		m.Header.Get("From"), m.Header.Get("Subject"), m.Header.Get("Date"))
	fmt.Printf("to-count=%d\n", len(m.Header["To"]))
	fmt.Printf("x-custom=%q missing=%q\n", m.Header.Get("X-Custom"), m.Header.Get("X-Nope"))
	addr, err := m.Header.AddressList("To")
	fmt.Printf("to-list-err=%v count=%d\n", err != nil, len(addr))
	var body strings.Builder
	buf := make([]byte, 64)
	for {
		n, err := m.Body.Read(buf)
		body.Write(buf[:n])
		if err != nil {
			break
		}
	}
	fmt.Printf("body=%q\n", body.String())
	// Error path: no headers.
	_, err = mail.ReadMessage(strings.NewReader("no headers at all"))
	fmt.Printf("noheader-err=%v\n", err != nil)
}
