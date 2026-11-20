// std_packages2.go — more packages from upstream source: flag, log,
// encoding/hex, base64 and binary, math/rand, context with a timer deadline,
// text/template (reflect.Value.Call through the patched call path), and
// os.Setenv/Unsetenv with the GODEBUG hook of internal/godebug.
package main

import (
	"context"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"flag"
	"fmt"
	"log"
	"math/rand"
	"os"
	"strings"
	"sync"
	"text/template"
	"time"
	"unicode"
)

type T struct {
	Name  string
	Items []int
}

func (t T) Upper() string { return strings.ToUpper(t.Name) }

func main() {
	fs := flag.NewFlagSet("x", flag.ContinueOnError)
	n := fs.Int("n", 1, "count")
	v := fs.Bool("v", false, "verbose")
	fmt.Println(fs.Parse([]string{"-n", "5", "-v", "rest"}), *n, *v, fs.Args())
	log.SetFlags(0)
	log.SetOutput(os.Stdout)
	log.Printf("log %d", 1)
	fmt.Println(hex.EncodeToString([]byte("hi")), base64.StdEncoding.EncodeToString([]byte("hello")))
	b := make([]byte, 8)
	binary.LittleEndian.PutUint32(b, 0xdeadbeef)
	fmt.Println(b, binary.BigEndian.Uint16(b))
	r := rand.New(rand.NewSource(42))
	fmt.Println(r.Intn(100), r.Intn(100), unicode.IsUpper('A'))
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	var wg sync.WaitGroup
	wg.Add(1)
	go func() { defer wg.Done(); <-ctx.Done() }()
	wg.Wait()
	fmt.Println(ctx.Err(), errors.Is(ctx.Err(), context.DeadlineExceeded))
	tpl := template.Must(template.New("t").Parse("{{.Name}} {{.Upper}} {{range $i, $x := .Items}}[{{$i}}:{{$x}}]{{end}} {{len .Items | printf \"%03d\"}}\n"))
	fmt.Println(tpl.Execute(os.Stdout, T{"tpl", []int{4, 5}}))
	os.Setenv("GODEBUG", "panicnil=1")
	os.Setenv("FMTPATH_X", "1")
	fmt.Println(os.Getenv("GODEBUG"), os.Getenv("FMTPATH_X"))
	os.Unsetenv("FMTPATH_X")
	_, ok := os.LookupEnv("FMTPATH_X")
	fmt.Println(ok)
}
