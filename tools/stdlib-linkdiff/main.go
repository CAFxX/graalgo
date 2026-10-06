// Command stdlib-linkdiff is the graalgo Phase-3 linkname-diff tool
// (P3-M6, GG-0021; SPEC §3.6).
//
// It scans a Go source tree (default: the Go toolchain's GOROOT/src,
// standing in for the vendored stdlib until the vendoring worker lands)
// for //go:linkname directives, and classifies each one against the
// symbol set graalgo provides:
//
//   - SATISFIED: the linkname target is provided by graalgo
//     (P3-M4 intrinsic registry or a synthetic package export).
//   - ROUTED: the target is explicitly routed to a replacement
//     (routes.txt, with the route recorded).
//   - MISSING: the target is not provided. Every MISSING target lands
//     on the skip-list. Entries the tool cannot classify are MISSING
//     with a reason — never silently dropped.
//
// The JSON report carries skip_list_size as a top-level, machine-readable
// field: SPEC §3.6's kill trigger is skip-list growth > 15 twice per
// release, so the size must be prominent.
//
// Usage:
//
//	stdlib-linkdiff [--goroot DIR] [--graalgo DIR] [--goos OS] [--goarch ARCH]
//	                [--tags tag1,tag2] [--routes FILE] [--json PATH] [--md PATH]
//	                [--snapshot PATH] [--provided PATH] [--quiet]
//
// Exit codes: 0 ok, 1 scan/classify failure, 2 usage error.
// No external dependencies.
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const usageText = `stdlib-linkdiff — graalgo linkname-diff tool (P3-M6, GG-0021; SPEC §3.6)

Scans a Go source tree for //go:linkname directives and classifies each
target as SATISFIED (provided by graalgo), ROUTED (explicit routes.txt
entry) or MISSING (lands on the skip-list, never silently dropped).

Usage:
  stdlib-linkdiff [--goroot DIR] [--graalgo DIR] [--goos OS] [--goarch ARCH]
                  [--tags t1,t2] [--routes FILE] [--json PATH] [--md PATH]
                  [--snapshot PATH] [--provided PATH] [--quiet]

Flags:
  --goroot DIR    Go source tree to scan (default: GOROOT/src of the
                  toolchain that built this binary)
  --graalgo DIR   graalgo repo root the provided set is extracted from
                  (default: $GRAALGO_ROOT, else ~/workspace/graalgo)
  --goos, --goarch
                  target platform for build-constraint filtering
                  (default: linux, amd64)
  --tags LIST     extra build tags treated as satisfied, comma-separated
  --routes FILE   routes file (default: <graalgo>/tools/stdlib-linkdiff/routes.txt)
  --json PATH     write the JSON report to PATH (default: stdout)
  --md PATH       also write a Markdown summary to PATH
  --snapshot PATH write the extracted provided-symbol set to PATH and exit
                  (per-release record; diff snapshots across releases)
  --provided PATH read the provided set from a snapshot file instead of
                  extracting it from --graalgo sources
  --quiet         print only the headline counts

Exit codes: 0 ok, 1 failure, 2 usage error.
`

type usageError struct{ msg string }

func (e usageError) Error() string { return e.msg }

type options struct {
	goroot   string
	graalgo  string
	goos     string
	goarch   string
	tags     []string
	routes   string
	jsonOut  string
	mdOut    string
	snapshot string
	provided string
	quiet    bool
}

func defaultGoroot() string {
	if g := os.Getenv("GOROOT"); g != "" {
		return filepath.Join(g, "src")
	}
	// Fall back to the toolchain that built this binary: <GOROOT>/bin/go
	// is two levels above the src tree... actually GOROOT/bin/<tool>;
	// the reliable route is `go env GOROOT`, which needs a go binary.
	// Last resort: ~/tools/go/src (the program's pinned toolchain).
	if h, err := os.UserHomeDir(); err == nil {
		return filepath.Join(h, "tools", "go", "src")
	}
	return ""
}

func defaultGraalgo() string {
	if g := os.Getenv("GRAALGO_ROOT"); g != "" {
		return g
	}
	if h, err := os.UserHomeDir(); err == nil {
		return filepath.Join(h, "workspace", "graalgo")
	}
	return ""
}

func parseFlags(args []string) (options, error) {
	var o options
	fs := flag.NewFlagSet("stdlib-linkdiff", flag.ContinueOnError)
	fs.StringVar(&o.goroot, "goroot", "", "Go source tree to scan")
	fs.StringVar(&o.graalgo, "graalgo", "", "graalgo repo root")
	fs.StringVar(&o.goos, "goos", "linux", "target GOOS for constraint filtering")
	fs.StringVar(&o.goarch, "goarch", "amd64", "target GOARCH for constraint filtering")
	tags := fs.String("tags", "", "extra build tags treated as satisfied (comma-separated)")
	fs.StringVar(&o.routes, "routes", "", "routes file")
	fs.StringVar(&o.jsonOut, "json", "", "write JSON report to PATH (default stdout)")
	fs.StringVar(&o.mdOut, "md", "", "also write Markdown summary to PATH")
	fs.StringVar(&o.snapshot, "snapshot", "", "write provided-symbol snapshot to PATH and exit")
	fs.StringVar(&o.provided, "provided", "", "read provided set from snapshot PATH instead of extracting")
	fs.BoolVar(&o.quiet, "quiet", false, "print only headline counts")
	if err := fs.Parse(args); err != nil {
		return o, usageError{err.Error()}
	}
	if fs.NArg() > 0 {
		return o, usageError{fmt.Sprintf("unexpected arguments: %s", strings.Join(fs.Args(), " "))}
	}
	if *tags != "" {
		for _, t := range strings.Split(*tags, ",") {
			if t = strings.TrimSpace(t); t != "" {
				o.tags = append(o.tags, t)
			}
		}
	}
	if o.goroot == "" {
		o.goroot = defaultGoroot()
	}
	if o.graalgo == "" {
		o.graalgo = defaultGraalgo()
	}
	if o.routes == "" {
		o.routes = filepath.Join(o.graalgo, "tools", "stdlib-linkdiff", "routes.txt")
	}
	return o, nil
}

func main() {
	os.Exit(run(os.Args[1:]))
}

func run(args []string) int {
	o, err := parseFlags(args)
	if err != nil {
		if ue, ok := err.(usageError); ok {
			fmt.Fprint(os.Stderr, "stdlib-linkdiff: "+ue.msg+"\n"+usageText)
			return 2
		}
		fmt.Fprintln(os.Stderr, "stdlib-linkdiff: "+err.Error())
		return 1
	}

	provided, err := loadProvided(o)
	if err != nil {
		fmt.Fprintln(os.Stderr, "stdlib-linkdiff: provided set: "+err.Error())
		return 1
	}
	if o.snapshot != "" {
		if err := writeSnapshot(o.snapshot, provided); err != nil {
			fmt.Fprintln(os.Stderr, "stdlib-linkdiff: snapshot: "+err.Error())
			return 1
		}
		if !o.quiet {
			fmt.Printf("wrote %d provided symbols to %s\n", len(provided.Symbols), o.snapshot)
		}
		return 0
	}

	routes, err := loadRoutes(o.routes)
	if err != nil {
		fmt.Fprintln(os.Stderr, "stdlib-linkdiff: routes: "+err.Error())
		return 1
	}

	scan, err := ScanTree(o.goroot, o.goos, o.goarch, o.tags)
	if err != nil {
		fmt.Fprintln(os.Stderr, "stdlib-linkdiff: scan: "+err.Error())
		return 1
	}

	rep := Classify(scan, provided, routes, o)
	if err := emitReport(rep, o); err != nil {
		fmt.Fprintln(os.Stderr, "stdlib-linkdiff: report: "+err.Error())
		return 1
	}
	return 0
}
