// Command graalgo-goroot applies graalgo's standard-library fork
// (docs/arch/toolexec-design.md D8, §5.1) to the upstream Go distribution:
//
//	graalgo-goroot -stdlib DIR [-upstream GOROOT] -overlay FILE -work DIR
//	graalgo-goroot -stdlib DIR [-upstream GOROOT] -o GOROOT [-shallow]
//	graalgo-goroot -stdlib DIR [-upstream GOROOT] -audit
//
// -overlay writes a `go build -overlay` file that applies the fork to the
// upstream tree in place (patched files are written under -work); -o
// assembles the GOROOT a graalgo distribution ships (with -shallow:
// untouched source directories are symbolic links to upstream's, for
// development — `std` patterns do not see them); -audit prints the
// fork-size report (GG-0009, GG-0044). Every mode first validates the fork
// (package fork): a replaced package or patched file that upstream does not
// have, or a patch that does not apply exactly, is an error.
//
// The tool has no list of packages: the fork is whatever DIR/fork and
// DIR/patches contain.
package main

import (
	"flag"
	"fmt"
	"os"
	"runtime"

	"graalgo-toolexec/fork"
	"graalgo-toolexec/release"
)

func main() {
	stdlib := flag.String("stdlib", "", "the repository's stdlib directory (holds fork/ and patches/)")
	upstream := flag.String("upstream", runtime.GOROOT(), "upstream GOROOT of the tracked release")
	overlay := flag.String("overlay", "", "write a go build -overlay file here")
	work := flag.String("work", "", "directory for patched files referenced by -overlay")
	out := flag.String("o", "", "assemble a GOROOT at this path (must not exist)")
	shallow := flag.Bool("shallow", false, "with -o: link source directories the fork does not touch instead of copying them")
	audit := flag.Bool("audit", false, "print the fork-size report")
	flag.Parse()
	if *stdlib == "" || flag.NArg() != 0 || (*overlay == "" && *out == "" && !*audit) {
		flag.Usage()
		os.Exit(2)
	}
	f, err := fork.Load(*stdlib, *upstream, release.GoVersion)
	if err != nil {
		fatal(err)
	}
	if *overlay != "" {
		if *work == "" {
			fatal(fmt.Errorf("-overlay needs -work"))
		}
		data, err := f.Overlay(*work)
		if err != nil {
			fatal(err)
		}
		if err := os.WriteFile(*overlay, append(data, '\n'), 0o644); err != nil {
			fatal(err)
		}
	}
	if *out != "" {
		if err := f.Assemble(*out, *shallow); err != nil {
			fatal(err)
		}
	}
	if *audit {
		report, err := f.Audit()
		if err != nil {
			fatal(err)
		}
		fmt.Print(report)
	}
}

func fatal(err error) {
	fmt.Fprintf(os.Stderr, "graalgo-goroot: %v\n", err)
	os.Exit(1)
}
