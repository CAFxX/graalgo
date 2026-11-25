//m1a:ldflags -X main.version=1.2.3 -X main.commit=c=0.1 -X main.unset=set -X main.dynamic=lost -X main.inlined=won -X main.konst=K -X main.empty=E -X main.version=1.2.4 -X example.com/xflags/build.Stamp=today -X example.com/xflags/build.when=noon -X example.com/xflags/ver.v2.Tag=v2tag -X example.com/xflags/build.Derived=lost -X main.nosuch=x -X example.com/none.v=x

// BUG-0048: `go build -ldflags=-X importpath.name=value` sets string
// variables as gc's linker does: a variable gc initializes statically (no
// initializer, a constant one, or a call its static-init pass inlines)
// takes the value; a dynamic initializer runs afterwards and wins; a
// setting naming no variable (a constant, a function, a missing symbol or
// package) is ignored; a repeated setting's last value wins; package paths
// with a dotted last element are matched unescaped.
package main

import (
	"example.com/xflags/build"
	"example.com/xflags/ver.v2"
)

const konst = "konst"

var version = "dev"
var commit string
var unset string
var dynamic = build.Name() + "!"
var inlined = id("in")
var empty = ""
var banner = "v" + version // initializer code: sees the -X value

func id(s string) string { return s }

func main() {
	println(version, commit, unset, dynamic, inlined, "["+empty+"]", konst, banner)
	println(build.Stamp, build.When(), build.Derived, ver.Tag)
}
