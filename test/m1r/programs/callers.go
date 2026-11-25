// Call stacks from Truffle frames (WP-TR-AUDIT-6, BUG-0047): runtime.Caller,
// Callers, CallersFrames, FuncForPC and Func's methods, runtime.Stack's frame
// lines, and log's Lshortfile, each reporting gc's function names and
// file:line. Frames below main (runtime.main, runtime.goexit) and gc's
// argument words are outside the comparison: the program stops at the
// first runtime frame and prints a traceback frame's name without its
// argument list.
package main

import (
	"fmt"
	"iter"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

type T struct{ n int }

func (T) M()  { report("T.M") }
func (*T) P() { report("(*T).P") }

type Shape interface{ Area() int }

func (t T) Area() int { report("iface"); return t.n }

type List[E any] struct{ items []E }

func (l *List[E]) Push(e E) { l.items = append(l.items, e); report("generic method") }

// where names the function skip frames above its caller, and where that call is.
func where(skip int) string {
	pc, file, line, ok := runtime.Caller(skip + 1)
	if !ok {
		return "?"
	}
	return fmt.Sprintf("%s %s:%d", runtime.FuncForPC(pc).Name(), filepath.Base(file), line)
}

func report(tag string) {
	fmt.Println(tag, "|", where(0), "|", where(1))
}

// helper reports its caller's position, as testing.T.Helper users do.
func helper() string { return where(1) }

func G[X any](x X) X {
	report("generic")
	return x
}

func seq(n int) iter.Seq[int] {
	return func(yield func(int) bool) {
		for i := range n {
			if !yield(i) {
				return
			}
		}
	}
}

func frames(skip int) {
	pcs := make([]uintptr, 16)
	n := runtime.Callers(skip, pcs)
	fs := runtime.CallersFrames(pcs[:n])
	for {
		f, more := fs.Next()
		if strings.HasPrefix(f.Function, "runtime.") && f.Function != "runtime.Callers" {
			break
		}
		fmt.Println("  frame", f.Function, filepath.Base(f.File), f.Line)
		if !more {
			break
		}
	}
}

// exitLine's deferred call sees exitLine at its exit: the closing brace.
func exitLine() {
	defer func() {
		fmt.Println("deferred at exit |", where(1))
	}()
	fmt.Println("exitLine body")
}

func deep(n int) {
	if n == 0 {
		frames(1)
		return
	}
	deep(n - 1)
}

func main() {
	report("main")
	T{}.M()
	(&T{}).P()
	var s Shape = T{n: 3}
	s.Area()
	(&List[string]{}).Push("x")
	func() {
		report("closure")
		func() {
			report("nested closure")
		}()
	}()
	G(1)
	fmt.Println("helper |", helper())
	m := T{}.M
	m()
	for i := range seq(2) {
		report(fmt.Sprint("range-over-func ", i))
	}
	defer func() {
		report("deferred")
	}()

	exitLine()
	fmt.Println("callers:")
	deep(2)

	// A pc's function and lines: Entry is the function's, FileLine follows the pc.
	pc, _, line, _ := runtime.Caller(0)
	f := runtime.FuncForPC(pc)
	file, l := f.FileLine(pc)
	fmt.Println("FuncForPC:", f.Name(), filepath.Base(file), l == line, runtime.FuncForPC(f.Entry()).Name())
	fmt.Println("FuncForPC(0) nil:", runtime.FuncForPC(0) == nil, (*runtime.Func)(nil).Name() == "")

	// runtime.Stack: the header and the first frame's function.
	buf := make([]byte, 4096)
	n := runtime.Stack(buf, false)
	lines := strings.Split(string(buf[:n]), "\n")
	fmt.Println("Stack:", strings.HasPrefix(lines[0], "goroutine "), strings.HasSuffix(lines[0], " [running]:"),
		lines[1][:strings.Index(lines[1], "(")], strings.HasSuffix(strings.Fields(lines[2])[0], "/"+filepath.Base(file)+":"+fmt.Sprint(line+8)))

	log.SetFlags(log.Lshortfile)
	log.SetOutput(os.Stdout)
	log.Println("log with Lshortfile")
	done := make(chan bool)
	go func() {
		report("goroutine")
		done <- true
	}()
	<-done
}
