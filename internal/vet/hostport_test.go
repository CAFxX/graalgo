package vet

import (
	"strings"
	"testing"
)

// RV18-P1-21(5): runHostPort flagged net.Dial("unix", "/tmp/sock") as
// "missing port"; unix-domain sockets legitimately have no port.
func TestHostPortUnixNoPort(t *testing.T) {
	src := `package main

import "net"

func f() {
	c1, _ := net.Dial("unix", "/tmp/sock")
	c2, _ := net.Dial("unixpacket", "/tmp/sock2")
	_ = c1
	_ = c2
}
`
	joined := runVetOnSrc(t, src)
	if strings.Contains(joined, "missing port") {
		t.Errorf("false positive: unix sockets have no port:\n%s", joined)
	}
	if strings.Contains(joined, "JoinHostPort") {
		t.Errorf("false positive: JoinHostPort advice is wrong for unix sockets:\n%s", joined)
	}
}

func TestHostPortMissingPort(t *testing.T) {
	src := `package main

import "net"

func f() {
	c, _ := net.Dial("tcp", "localhost")
	_ = c
}
`
	joined := runVetOnSrc(t, src)
	if !strings.Contains(joined, "missing port") {
		t.Errorf("expected missing-port diagnostic, got:\n%s", joined)
	}
}
