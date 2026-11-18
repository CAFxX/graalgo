// Command girstat runs the GIR emitter over a package set (default: all of
// std, in graalgo's build configuration) and reports emitter coverage per
// AST node kind, unsupported constructs, GIR size by section, and encode /
// decode speed (docs/arch/toolexec-design.md §12 P3). Every package that
// emits is also encoded, decoded, validated and re-encoded (Go round
// trip); with -out its GIR file is written for the Java round trip
// (make -C internal/backend/gir test GIR_EXTRA=<dir>).
//
//	girstat [-out dir] [patterns...]
package main

import (
	"bytes"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"graalgo-toolexec/emit"
	"graalgo-toolexec/ir"
)

func main() {
	out := flag.String("out", "", "write each package's GIR to `dir`/<import path>.gir")
	flag.Parse()
	patterns := flag.Args()
	if len(patterns) == 0 {
		patterns = []string{"std"}
	}
	t0 := time.Now()
	pkgs, loadErrs, err := emit.Load(patterns...)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	loadTime := time.Since(t0)
	for _, e := range loadErrs {
		fmt.Printf("load error: %v\n", e)
	}
	total := &emit.Stats{Nodes: map[string]int{}, NodesOK: map[string]int{}, Unsupported: map[string]int{}}
	var emitT, encT, decT time.Duration
	var okPkgs, bytesTotal, funcs, bodies, encodedNodes int
	sections := map[string]int{}
	var failed []string
	for _, p := range pkgs {
		t := time.Now()
		f, st, err := emit.Emit(p.Config)
		emitT += time.Since(t)
		total.Add(st)
		if err != nil {
			failed = append(failed, fmt.Sprintf("%s: %v", p.ImportPath, err))
			continue
		}
		t = time.Now()
		data, err := ir.Encode(f)
		encT += time.Since(t)
		if err != nil {
			fail("%s: encode: %v", p.ImportPath, err)
		}
		t = time.Now()
		g, err := ir.DecodeValidate(data)
		decT += time.Since(t)
		if err != nil {
			fail("%s: decode: %v", p.ImportPath, err)
		}
		again, err := ir.Encode(g)
		if err != nil || !bytes.Equal(again, data) {
			fail("%s: Go round trip differs (%v)", p.ImportPath, err)
		}
		for name, n := range ir.SectionSizes(data) {
			sections[name] += n
		}
		okPkgs++
		for _, v := range st.NodesOK {
			encodedNodes += v
		}
		bytesTotal += len(data)
		funcs += len(f.Funcs)
		for _, b := range f.Bodies {
			if b != nil {
				bodies++
			}
		}
		if *out != "" {
			path := filepath.Join(*out, strings.ReplaceAll(p.ImportPath, "/", "_")+".gir")
			if err := os.MkdirAll(*out, 0o755); err != nil {
				fail("%v", err)
			}
			if err := os.WriteFile(path, data, 0o644); err != nil {
				fail("%v", err)
			}
		}
	}
	fmt.Printf("packages: %d type-checked (load %.1fs), %d emitted completely, %d with unsupported constructs\n",
		len(pkgs), loadTime.Seconds(), okPkgs, len(failed))
	fmt.Printf("function bodies: %d of %d emitted completely (%.1f%%); max nesting %d\n",
		total.FuncsOK, total.Funcs, 100*float64(total.FuncsOK)/float64(max(total.Funcs, 1)), total.MaxDepth)
	allNodes, okNodes := 0, 0
	for k, v := range total.Nodes {
		allNodes += v
		okNodes += total.NodesOK[k]
	}
	fmt.Printf("AST nodes in bodies/initializers: %d, in completely emitted ones: %d (%.1f%%)\n\n", allNodes, okNodes, 100*float64(okNodes)/float64(max(allNodes, 1)))
	fmt.Printf("%-16s %10s %10s %7s\n", "node kind", "seen", "emitted", "%")
	kinds := mapKeys(total.Nodes)
	slices.SortFunc(kinds, func(a, b string) int { return total.Nodes[b] - total.Nodes[a] })
	for _, k := range kinds {
		fmt.Printf("%-16s %10d %10d %6.1f%%\n", k, total.Nodes[k], total.NodesOK[k], 100*float64(total.NodesOK[k])/float64(total.Nodes[k]))
	}
	fmt.Printf("\nunsupported constructs (occurrences; each aborts its function):\n")
	reasons := mapKeys(total.Unsupported)
	slices.SortFunc(reasons, func(a, b string) int { return total.Unsupported[b] - total.Unsupported[a] })
	for _, r := range reasons {
		fmt.Printf("  %6d  %s\n", total.Unsupported[r], r)
	}
	fmt.Printf("\nGIR of the %d completely emitted packages: %d bytes (%.2f MB), %d funcs (%d with bodies); %.2f bytes per emitted AST node\n",
		okPkgs, bytesTotal, float64(bytesTotal)/1e6, funcs, bodies, float64(bytesTotal)/float64(max(encodedNodes, 1)))
	for _, name := range mapKeys(sections) {
		fmt.Printf("  %-10s %10d bytes (%.1f%%)\n", name, sections[name], 100*float64(sections[name])/float64(max(bytesTotal, 1)))
	}
	fmt.Printf("time: emit %.2fs, encode %.2fs (%.1f MB/s), Go decode+validate %.2fs (%.1f MB/s)\n",
		emitT.Seconds(), encT.Seconds(), float64(bytesTotal)/1e6/encT.Seconds(), decT.Seconds(), float64(bytesTotal)/1e6/decT.Seconds())
	if len(failed) > 0 {
		fmt.Printf("\nfirst unsupported construct per failing package:\n")
		for _, f := range failed {
			fmt.Printf("  %s\n", f)
		}
	}
}

func mapKeys(m map[string]int) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	return keys
}

func fail(format string, a ...any) {
	fmt.Fprintf(os.Stderr, "girstat: "+format+"\n", a...)
	os.Exit(1)
}
