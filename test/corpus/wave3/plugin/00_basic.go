package main

// L11 (REVIEW-2026-11-18-05): plugin — zero corpus imports before
// this program. Building a real plugin at run time (go build
// -buildmode=plugin in a temp dir) and loading it: Open, Lookup of
// an exported var and func, and the error path on a missing file.
// (Marked .scopelimited: the graalgo frontend has no plugin support;
// retest when it lands. The gc leg still builds and runs the flow.)

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"plugin"
)

const pluginSrc = `package main

var Answer = 42

func Greet(name string) string { return "hello " + name }
`

func main() {
	goBin, err := exec.LookPath("go")
	fmt.Printf("go-lookpath-err=%v\n", err != nil)
	if err != nil {
		return
	}
	dir, err := os.MkdirTemp("", "plugin-demo")
	if err != nil {
		fmt.Printf("mktemp-err=%v\n", err != nil)
		return
	}
	defer os.RemoveAll(dir)
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module demoplugin\n\ngo 1.21\n"), 0644); err != nil {
		fmt.Printf("gomod-err=%v\n", err != nil)
		return
	}
	if err := os.WriteFile(filepath.Join(dir, "plug.go"), []byte(pluginSrc), 0644); err != nil {
		fmt.Printf("write-err=%v\n", err != nil)
		return
	}
	soPath := filepath.Join(dir, "plug.so")
	cmd := exec.Command(goBin, "build", "-buildmode=plugin", "-o", soPath, ".")
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	fmt.Printf("build-err=%v output-bytes=%d\n", err != nil, len(out))
	if err != nil {
		return
	}
	p, err := plugin.Open(soPath)
	fmt.Printf("open-err=%v\n", err != nil)
	if err != nil {
		return
	}
	v, err := p.Lookup("Answer")
	fmt.Printf("lookup-var-err=%v\n", err != nil)
	if err == nil {
		if n, ok := v.(*int); ok {
			fmt.Printf("answer=%d\n", *n)
		} else {
			fmt.Printf("answer-wrong-type=true\n")
		}
	}
	f, err := p.Lookup("Greet")
	fmt.Printf("lookup-func-err=%v\n", err != nil)
	if err == nil {
		if g, ok := f.(func(string) string); ok {
			fmt.Printf("greet=%q\n", g("world"))
		} else {
			fmt.Printf("greet-wrong-type=true\n")
		}
	}
	_, err = p.Lookup("Nosuch")
	fmt.Printf("lookup-missing-err=%v\n", err != nil)
	_, err = plugin.Open(filepath.Join(dir, "nosuch.so"))
	fmt.Printf("open-missing-err=%v\n", err != nil)
}
