// 13_failfast: the §6 fail-fast list. One case per run, selected by
// argv[1]. Under graalgo the call panics with a pinned "not in M8
// scope" message; under gc it succeeds. Both legs print the same
// "pinned <name>" line and exit 2 — the graalgo leg re-panics the
// first message so the runner's .panic pin sees it (uncaught-panic
// exit code parity).
package main

import (
	"fmt"
	"io/fs"
	"os"
	"strings"
	"time"
)

type dummyFS struct{}

func (dummyFS) Open(name string) (fs.File, error) {
	return nil, fmt.Errorf("dummyFS: no files")
}

func pin(name, prefix string, f func()) {
	defer func() {
		if r := recover(); r != nil {
			msg := fmt.Sprint(r)
			if !strings.HasPrefix(msg, prefix) {
				fmt.Println("UNPINNED PANIC:", msg)
				os.Exit(1)
			}
			fmt.Println("pinned", name)
			panic(msg) // re-panic: .panic pins the message, exit 2
		}
		fmt.Println("pinned", name)
		os.Exit(2) // gc leg: match graalgo's uncaught-panic exit code
	}()
	f()
}

func main() {
	if len(os.Args) < 2 {
		fmt.Println("usage: 13_failfast <case>")
		os.Exit(1)
	}
	switch os.Args[1] {
	case "StartProcess":
		pin("StartProcess", "os.StartProcess: not in M8 scope", func() {
			p, err := os.StartProcess("/bin/true", []string{"true"}, &os.ProcAttr{})
			if err == nil {
				p.Wait()
			}
		})
	case "Chown":
		pin("Chown", "os.Chown: not in M8 scope", func() {
			os.Chown("x", 0, 0)
		})
	case "Lchown":
		pin("Lchown", "os.Lchown: not in M8 scope", func() {
			os.Lchown("x", 0, 0)
		})
	case "SyscallConn":
		pin("SyscallConn", "os.File.SyscallConn: not in M8 scope", func() {
			os.Stdout.SyscallConn()
		})
	case "OpenRoot":
		pin("OpenRoot", "os.OpenRoot: not in M8 scope", func() {
			os.OpenRoot("/tmp")
		})
	case "Executable":
		pin("Executable", "os.Executable: not in M8 scope", func() {
			os.Executable()
		})
	case "CopyFS":
		pin("CopyFS", "os.CopyFS: not in M8 scope", func() {
			// A dummy FS: under gc CopyFS fails with a plain error
			// (no panic); under graalgo CopyFS panics before touching it.
			os.CopyFS("x", dummyFS{})
		})
	case "DirFS":
		pin("DirFS", "os.DirFS: not in M8 scope", func() {
			_ = os.DirFS("x")
		})
	case "PipeDeadline":
		pin("PipeDeadline", "os.File.SetDeadline: not in M8 scope for pipes", func() {
			r, w, err := os.Pipe()
			if err != nil {
				panic(err)
			}
			defer r.Close()
			defer w.Close()
			r.SetReadDeadline(time.Now())
		})
	default:
		fmt.Println("unknown case:", os.Args[1])
		os.Exit(1)
	}
}
