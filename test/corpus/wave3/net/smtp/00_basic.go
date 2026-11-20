package main

// L11 (REVIEW-2026-11-18-05): net/smtp — zero corpus imports before
// this program. Sending mail through a minimal fake SMTP server on
// loopback: the client handshake (Hello/Mail/Rcpt/Data/Quit) and
// SendMail; the server records the commands it saw, in order.

import (
	"bufio"
	"fmt"
	"net"
	"net/smtp"
	"strings"
)

func fakeSMTP(ln net.Listener, cmds chan<- string, done chan<- struct{}) {
	defer close(done)
	conn, err := ln.Accept()
	if err != nil {
		return
	}
	defer conn.Close()
	r := bufio.NewReader(conn)
	fmt.Fprint(conn, "220 fake smtp ready\r\n")
	for {
		line, err := r.ReadString('\n')
		if err != nil {
			return
		}
		line = strings.TrimRight(line, "\r\n")
		cmds <- line
		up := strings.ToUpper(line)
		switch {
		case strings.HasPrefix(up, "EHLO"), strings.HasPrefix(up, "HELO"):
			fmt.Fprint(conn, "250-fake\r\n250 AUTH PLAIN\r\n")
		case strings.HasPrefix(up, "MAIL FROM"):
			fmt.Fprint(conn, "250 ok\r\n")
		case strings.HasPrefix(up, "RCPT TO"):
			fmt.Fprint(conn, "250 ok\r\n")
		case up == "DATA":
			fmt.Fprint(conn, "354 go ahead\r\n")
			for {
				dl, err := r.ReadString('\n')
				if err != nil {
					return
				}
				if strings.TrimRight(dl, "\r\n") == "." {
					break
				}
			}
			fmt.Fprint(conn, "250 queued\r\n")
		case up == "QUIT":
			fmt.Fprint(conn, "221 bye\r\n")
			return
		case up == "RSET":
			fmt.Fprint(conn, "250 ok\r\n")
		default:
			fmt.Fprint(conn, "500 unknown\r\n")
		}
	}
}

func main() {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		fmt.Printf("listen-err=%v\n", err != nil)
		return
	}
	defer ln.Close()
	cmds := make(chan string, 32)
	done := make(chan struct{})
	go fakeSMTP(ln, cmds, done)
	addr := ln.Addr().String()
	// Full client conversation.
	c, err := smtp.Dial(addr)
	if err != nil {
		fmt.Printf("dial-err=%v\n", err != nil)
		return
	}
	if err := c.Hello("client.example"); err != nil {
		fmt.Printf("hello-err=%v\n", err != nil)
		return
	}
	if err := c.Mail("sender@example.com"); err != nil {
		fmt.Printf("mail-err=%v\n", err != nil)
		return
	}
	if err := c.Rcpt("rcpt@example.com"); err != nil {
		fmt.Printf("rcpt-err=%v\n", err != nil)
		return
	}
	wc, err := c.Data()
	if err != nil {
		fmt.Printf("data-err=%v\n", err != nil)
		return
	}
	fmt.Fprint(wc, "Subject: hi\r\n\r\nbody\r\n")
	wc.Close()
	if err := c.Quit(); err != nil {
		fmt.Printf("quit-err=%v\n", err != nil)
		return
	}
	<-done
	close(cmds)
	var seen []string
	for cmd := range cmds {
		seen = append(seen, cmd)
	}
	fmt.Printf("commands=%q\n", seen)
	// SendMail against a fresh server instance.
	ln2, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		fmt.Printf("listen2-err=%v\n", err != nil)
		return
	}
	defer ln2.Close()
	cmds2 := make(chan string, 32)
	done2 := make(chan struct{})
	go fakeSMTP(ln2, cmds2, done2)
	err = smtp.SendMail(ln2.Addr().String(), nil, "a@example.com",
		[]string{"b@example.com"}, []byte("Subject: s\r\n\r\nB\r\n"))
	fmt.Printf("sendmail-err=%v\n", err != nil)
	<-done2
}
