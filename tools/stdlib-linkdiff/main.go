// Command stdlib-linkdiff is the graalgo Phase-3 linkname-diff tool
// (P3-M6, GG-0021; SPEC §3.6).
//
// It scans a Go source tree (default: the vendored stdlib tree for the
// pinned version when present, else the live toolchain's GOROOT/src --
// the choice is announced loudly, never silently)
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
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

const usageText = `stdlib-linkdiff — graalgo linkname-diff tool (P3-M6, GG-0021; SPEC §3.6)

Scans a Go source tree for //go:linkname directives and classifies each
target as SATISFIED (provided by graalgo), ROUTED (explicit routes.txt
entry) or MISSING (lands on the skip-list, never silently dropped).

Also scans graalgo's embedded synthetic stdlib packages (their linknames
are real needs) and marks vendored-tree entries superseded by the
overlay as shadowed, so the report splits the skip-list into the
comparable total and the live (actionable) subset.

Usage:
  stdlib-linkdiff [--goroot DIR] [--graalgo DIR] [--goos OS] [--goarch ARCH]
                  [--tags t1,t2] [--routes FILE] [--json PATH] [--md PATH]
                  [--snapshot PATH] [--provided PATH] [--quiet]

Flags:
  --goroot DIR    Go source tree to scan (default: canonical resolution --
                  the vendored tree for the pinned version when present,
                  else live GOROOT/src with a loud warning)
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

func defaultGoroot(stderr io.Writer) (string, error) {
	// Canonical stdlib-tree resolution (same rule as tools/stdlib-excl's
	// resolveStdlibTree; the two copies are documented as twins -- keep
	// in sync): the vendored tree for the pinned version when present,
	// else the live toolchain -- announced loudly, never silently.
	if graalgo := defaultGraalgo(); graalgo != "" {
		pinPath := filepath.Join(graalgo, "tools", "stdlib-fork", "pinned-version.txt")
		// A pin read failure is LOUD, never swallowed: the vendored tree
		// is the canonical resolution, so an unreadable pin is a
		// repo-state problem the operator must see (twin of
		// tools/stdlib-excl's resolveStdlibTree -- keep in sync).
		data, err := os.ReadFile(pinPath)
		switch {
		case err != nil:
			fmt.Fprintf(stderr, "stdlib tree: WARNING: cannot read pin %s (%v); falling back to live GOROOT\n", pinPath, err)
		case strings.TrimSpace(string(data)) == "":
			fmt.Fprintf(stderr, "stdlib tree: WARNING: pin %s is empty; falling back to live GOROOT\n", pinPath)
		default:
			ver := strings.TrimSpace(string(data))
			home, herr := os.UserHomeDir()
			if herr != nil {
				fmt.Fprintf(stderr, "stdlib tree: WARNING: cannot determine home dir (%v); falling back to live GOROOT\n", herr)
				break
			}
			cand := filepath.Join(home, "workspace", "graalgo-stdlib", ver)
			if fi, serr := os.Stat(cand); serr == nil && fi.IsDir() {
				if err := linkdiffCrossCheckPin(graalgo, ver); err != nil {
					return "", err
				}
				fmt.Fprintf(stderr, "stdlib tree: using vendored tree %s (pin %s)\n", cand, ver)
				return cand, nil
			}
			fmt.Fprintf(stderr, "stdlib tree: WARNING: no vendored tree at %s; falling back to live GOROOT (results pin to the live toolchain, not the pinned %s)\n", cand, ver)
		}
	}
	if g := os.Getenv("GOROOT"); g != "" {
		fmt.Fprintf(stderr, "stdlib tree: WARNING: no vendored tree; scanning $GOROOT/src (%s) -- results pin to the live toolchain\n", g)
		return filepath.Join(g, "src"), nil
	}
	// Fall back to the toolchain that built this binary: <GOROOT>/bin/go
	// is two levels above the src tree... actually GOROOT/bin/<tool>;
	// the reliable route is `go env GOROOT`, which needs a go binary.
	// Last resort: ~/tools/go/src (the program's pinned toolchain).
	if h, err := os.UserHomeDir(); err == nil {
		fmt.Fprintf(stderr, "stdlib tree: WARNING: no vendored tree and no $GOROOT; scanning pinned toolchain src (%s) -- results pin to the live toolchain\n", filepath.Join(h, "tools", "go", "src"))
		return filepath.Join(h, "tools", "go", "src"), nil
	}
	return "", fmt.Errorf("no --goroot and no stdlib tree resolvable")
}

// linkdiffCrossCheckPin fails loud when the committed vendor manifest
// records a different Go version than the pin (twin of
// tools/stdlib-excl's crossCheckPin -- keep in sync).
func linkdiffCrossCheckPin(graalgo, ver string) error {
	data, err := os.ReadFile(filepath.Join(graalgo, "tools", "stdlib-fork", "vendor-manifest.json"))
	if err != nil {
		return fmt.Errorf("stdlib tree: cannot read vendor manifest: %w", err)
	}
	var m struct {
		GoVersion string `json:"goVersion"`
	}
	if err := json.Unmarshal(data, &m); err != nil {
		return fmt.Errorf("stdlib tree: cannot parse vendor manifest: %w", err)
	}
	if m.GoVersion != ver {
		return fmt.Errorf("stdlib tree: vendor manifest records goVersion %q but pinned-version.txt says %q -- re-vendor state is inconsistent; refusing to scan the wrong tree", m.GoVersion, ver)
	}
	return nil
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
		var err error
		o.goroot, err = defaultGoroot(os.Stderr)
		if err != nil {
			return o, err
		}
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

	// M10: the embedded synthetic packages are compiled by graalgo, so
	// their //go:linkname directives are real needs (the exporter's
	// linkname scan covers them). Mark vendored-tree entries whose
	// package is superseded by the overlay as shadowed.
	reg, err := loadSyntheticRegistry(o.graalgo)
	if err != nil {
		fmt.Fprintln(os.Stderr, "stdlib-linkdiff: synthetic registry: "+err.Error())
		return 1
	}
	synthLNs, synthDecls, synthSkipped := scanSyntheticLinknames(reg)
	scan.Linknames = append(scan.Linknames, synthLNs...)
	for file, names := range synthDecls {
		scan.addFileDecls(file, names)
	}
	scan.SyntheticSourcesScanned = len(reg.Sources)
	scan.SyntheticSkipped = synthSkipped
	markShadowed(scan, reg.Shadowed)
	for _, s := range synthSkipped {
		fmt.Fprintf(os.Stderr, "stdlib-linkdiff: WARNING: synthetic scan skipped: %s\n", s)
	}

	rep := Classify(scan, provided, routes, o)
	if len(rep.StaleRoutes) > 0 {
		fmt.Fprintf(os.Stderr, "stdlib-linkdiff: WARNING: %d route(s) matched zero linknames (stale or mistyped): %s\n",
			len(rep.StaleRoutes), strings.Join(rep.StaleRoutes, ", "))
	}
	if err := emitReport(rep, o); err != nil {
		fmt.Fprintln(os.Stderr, "stdlib-linkdiff: report: "+err.Error())
		return 1
	}
	return 0
}
