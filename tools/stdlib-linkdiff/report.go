package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// Statuses for a classified linkname.
const (
	StatusSatisfied = "SATISFIED"
	StatusMissing   = "MISSING"
	StatusRouted    = "ROUTED"
)

// Entry is one classified linkname.
type Entry struct {
	File         string `json:"file"`
	Package      string `json:"package"`
	Local        string `json:"local"`
	Declared     string `json:"declared"`
	Target       string `json:"target"` // normalized target symbol
	Status       string `json:"status"`
	Reason       string `json:"reason,omitempty"`
	Route        string `json:"route,omitempty"`        // ROUTED: target -> replacement
	RouteReason  string `json:"route_reason,omitempty"` // ROUTED: why
	ProvidedFrom string `json:"provided_from,omitempty"`
}

// SkipEntry is one skip-list row: a distinct MISSING target.
type SkipEntry struct {
	Target       string   `json:"target"`
	LinknameRefs int      `json:"linkname_refs"`
	ExampleFiles []string `json:"example_files"`
	Reason       string   `json:"reason"`
}

// Duplicate records a local name declared by linkname more than once.
type Duplicate struct {
	Local string   `json:"local"` // package-qualified local name
	Files []string `json:"files"`
}

// Report is the full linkname-diff report.
type Report struct {
	Tool      string `json:"tool"`
	Version   string `json:"version"`
	Generated string `json:"generated"`
	Goroot    string `json:"goroot"`
	Graalgo   string `json:"graalgo"`
	Goos      string `json:"goos"`
	Goarch    string `json:"goarch"`

	Counts struct {
		Linknames          int `json:"linknames"`
		Satisfied          int `json:"satisfied"`
		Missing            int `json:"missing"`
		Routed             int `json:"routed"`
		FilesScanned       int `json:"files_scanned"`
		FilesWithLinknames int `json:"files_with_linknames"`
		TestFilesExcluded  int `json:"test_files_excluded"`
		ConstraintExcluded int `json:"constraint_excluded_files"`
		ParseErrors        int `json:"parse_errors"`
	} `json:"counts"`

	// SkipListSize is the headline kill-trigger metric (SPEC §3.6:
	// skip-list growth > 15 twice per release). Duplicated at top level
	// so release tooling can read it without walking the report.
	SkipListSize int         `json:"skip_list_size"`
	SkipList     []SkipEntry `json:"skip_list"`

	Entries    []Entry     `json:"entries"`
	Duplicates []Duplicate `json:"duplicates"`

	ProvidedCount   int               `json:"provided_symbol_count"`
	ProvidedSources map[string]int    `json:"provided_by_source"`
	RoutesApplied   []Route           `json:"routes_applied"`
	ParseErrors     []string          `json:"parse_errors_detail,omitempty"`
	ExcludedSample  []string          `json:"constraint_excluded_sample,omitempty"`
}

const toolVersion = "1.0.0"

// normalizeTarget resolves a raw linkname target to the symbol used for
// classification: unqualified targets are qualified with the file's
// package; go:-prefixed generated symbols and already-qualified
// targets pass through unchanged.
func normalizeTarget(ln Linkname) string {
	if ln.SelfLink || ln.Target == "" {
		return ln.Package + "." + ln.Local
	}
	t := ln.Target
	if strings.HasPrefix(t, "go:") || strings.Contains(t, ".") {
		return t
	}
	return ln.Package + "." + t
}

// Classify classifies every scanned linkname against the provided set
// and the routes. No entry is ever dropped: unclassifiable directives
// become MISSING with a reason.
func Classify(scan *ScanResult, provided *ProvidedSet, routes []Route, o options) *Report {
	rep := &Report{
		Tool:      "stdlib-linkdiff",
		Version:   toolVersion,
		Generated: time.Now().UTC().Format(time.RFC3339),
		Goroot:    scan.Root,
		Graalgo:   o.graalgo,
		Goos:      scan.Goos,
		Goarch:    scan.Goarch,
	}
	rep.Counts.FilesScanned = scan.FilesScanned
	rep.Counts.FilesWithLinknames = scan.FilesWithLinknames
	rep.Counts.TestFilesExcluded = scan.TestFilesExcluded
	rep.Counts.ConstraintExcluded = scan.ConstraintExcluded
	rep.Counts.ParseErrors = len(scan.ParseErrors)
	rep.ParseErrors = scan.ParseErrors
	rep.ExcludedSample = scan.ExcludedByConstraint

	routeByTarget := map[string]Route{}
	for _, r := range routes {
		routeByTarget[r.Target] = r
	}
	rep.RoutesApplied = routes

	skipIdx := map[string]int{}
	skipCount := map[string]int{}
	skipFiles := map[string][]string{}
	skipReason := map[string]string{}
	dupFiles := map[string][]string{}

	for _, ln := range scan.Linknames {
		e := Entry{
			File:     ln.File,
			Package:  ln.Package,
			Local:    ln.Local,
			Declared: ln.Declared,
			Target:   normalizeTarget(ln),
		}
		dupKey := ln.Package + "." + ln.Local
		dupFiles[dupKey] = append(dupFiles[dupKey], ln.File)

		switch {
		case ln.Malformed != "":
			e.Status = StatusMissing
			e.Reason = "malformed //go:linkname directive: " + ln.Malformed
		case ln.Declared != "" && ln.Declared != ln.Local:
			// gc rejects a local name that does not match the declared
			// function; record it as MISSING with the reason rather
			// than silently accepting it.
			e.Status = StatusMissing
			e.Reason = fmt.Sprintf("local name %q does not match declared identifier %q (gc rejects this)",
				ln.Local, ln.Declared)
		default:
			if r, ok := routeByTarget[e.Target]; ok {
				e.Status = StatusRouted
				e.Route = r.Target + " -> " + r.Replacement
				e.RouteReason = r.Reason
			} else if prov, ok := provided.Symbols[e.Target]; ok {
				e.Status = StatusSatisfied
				e.ProvidedFrom = prov
			} else {
				e.Status = StatusMissing
				e.Reason = "target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages)"
			}
		}

		switch e.Status {
		case StatusSatisfied:
			rep.Counts.Satisfied++
		case StatusRouted:
			rep.Counts.Routed++
		case StatusMissing:
			rep.Counts.Missing++
			skipCount[e.Target]++
			if _, ok := skipIdx[e.Target]; !ok {
				skipIdx[e.Target] = len(rep.SkipList)
				rep.SkipList = append(rep.SkipList, SkipEntry{Target: e.Target})
				skipReason[e.Target] = e.Reason
			}
			if len(skipFiles[e.Target]) < 5 {
				skipFiles[e.Target] = append(skipFiles[e.Target], e.File)
			}
		}
		rep.Counts.Linknames++
		rep.Entries = append(rep.Entries, e)
	}

	// Deterministic order: sort entries by file, then local name.
	sort.Slice(rep.Entries, func(i, j int) bool {
		if rep.Entries[i].File != rep.Entries[j].File {
			return rep.Entries[i].File < rep.Entries[j].File
		}
		return rep.Entries[i].Local < rep.Entries[j].Local
	})

	// Skip-list: fill in counts/files/reasons, sort by target.
	for i := range rep.SkipList {
		t := rep.SkipList[i].Target
		rep.SkipList[i].LinknameRefs = skipCount[t]
		rep.SkipList[i].ExampleFiles = skipFiles[t]
		rep.SkipList[i].Reason = skipReason[t]
	}
	sort.Slice(rep.SkipList, func(i, j int) bool { return rep.SkipList[i].Target < rep.SkipList[j].Target })
	rep.SkipListSize = len(rep.SkipList)

	// Duplicates: local names linked more than once.
	var dupKeys []string
	for k, files := range dupFiles {
		uniq := uniqueSorted(files)
		if len(uniq) > 1 {
			dupKeys = append(dupKeys, k)
		}
	}
	sort.Strings(dupKeys)
	for _, k := range dupKeys {
		rep.Duplicates = append(rep.Duplicates, Duplicate{Local: k, Files: uniqueSorted(dupFiles[k])})
	}

	rep.ProvidedCount = len(provided.Symbols)
	rep.ProvidedSources = map[string]int{}
	for _, prov := range provided.Symbols {
		// Group by the part before the first ": " (the source family).
		fam := prov
		if i := strings.Index(prov, ":"); i >= 0 {
			fam = strings.TrimSpace(prov[:i])
		}
		rep.ProvidedSources[fam]++
	}
	return rep
}

func uniqueSorted(in []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, s := range in {
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	sort.Strings(out)
	return out
}

// emitReport writes the JSON report (to --json or stdout) and,
// when requested, the Markdown summary. It also prints the headline
// counts unless --quiet.
func emitReport(rep *Report, o options) error {
	data, err := json.MarshalIndent(rep, "", "  ")
	if err != nil {
		return err
	}
	if o.jsonOut != "" {
		if err := os.MkdirAll(filepath.Dir(o.jsonOut), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(o.jsonOut, append(data, '\n'), 0o644); err != nil {
			return err
		}
	} else {
		fmt.Println(string(data))
	}
	if o.mdOut != "" {
		if err := os.MkdirAll(filepath.Dir(o.mdOut), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(o.mdOut, []byte(markdownSummary(rep)), 0o644); err != nil {
			return err
		}
	}
	if !o.quiet {
		fmt.Printf("linknames=%d satisfied=%d routed=%d missing=%d SKIP_LIST_SIZE=%d\n",
			rep.Counts.Linknames, rep.Counts.Satisfied, rep.Counts.Routed,
			rep.Counts.Missing, rep.SkipListSize)
	}
	return nil
}

// markdownSummary renders the human-readable report.
func markdownSummary(rep *Report) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# linkname-diff report — %s\n\n", rep.Generated)
	fmt.Fprintf(&b, "Tree: `%s` — target: `%s/%s` — graalgo: `%s`\n\n",
		rep.Goroot, rep.Goos, rep.Goarch, rep.Graalgo)
	fmt.Fprintf(&b, "## Headline\n\n")
	fmt.Fprintf(&b, "| metric | value |\n|---|---|\n")
	fmt.Fprintf(&b, "| linkname directives | %d |\n", rep.Counts.Linknames)
	fmt.Fprintf(&b, "| SATISFIED | %d |\n", rep.Counts.Satisfied)
	fmt.Fprintf(&b, "| ROUTED | %d |\n", rep.Counts.Routed)
	fmt.Fprintf(&b, "| MISSING | %d |\n", rep.Counts.Missing)
	fmt.Fprintf(&b, "| **SKIP_LIST_SIZE** | **%d** |\n", rep.SkipListSize)
	fmt.Fprintf(&b, "| files scanned | %d |\n", rep.Counts.FilesScanned)
	fmt.Fprintf(&b, "| files with linknames | %d |\n", rep.Counts.FilesWithLinknames)
	fmt.Fprintf(&b, "| _test.go files excluded | %d |\n", rep.Counts.TestFilesExcluded)
	fmt.Fprintf(&b, "| constraint-excluded files | %d |\n", rep.Counts.ConstraintExcluded)
	fmt.Fprintf(&b, "| provided symbols | %d |\n", rep.ProvidedCount)
	b.WriteString("\n> SPEC §3.6 kill trigger: skip-list growth > 15 twice per release.\n")
	b.WriteString("> Compare SKIP_LIST_SIZE against the previous release's snapshot.\n\n")
	if len(rep.RoutesApplied) > 0 {
		b.WriteString("## Routes applied\n\n")
		for _, r := range rep.RoutesApplied {
			fmt.Fprintf(&b, "- `%s` → `%s`: %s\n", r.Target, r.Replacement, r.Reason)
		}
		b.WriteString("\n")
	}
	b.WriteString("## Skip-list (distinct MISSING targets)\n\n")
	b.WriteString("| target | refs | reason |\n|---|---|---|\n")
	for _, s := range rep.SkipList {
		fmt.Fprintf(&b, "| `%s` | %d | %s |\n", s.Target, s.LinknameRefs, s.Reason)
	}
	if len(rep.Duplicates) > 0 {
		b.WriteString("\n## Duplicate local names\n\n")
		for _, d := range rep.Duplicates {
			fmt.Fprintf(&b, "- `%s`: %s\n", d.Local, strings.Join(d.Files, ", "))
		}
	}
	if len(rep.ParseErrors) > 0 {
		b.WriteString("\n## Parse errors\n\n")
		for _, pe := range rep.ParseErrors {
			fmt.Fprintf(&b, "- %s\n", pe)
		}
	}
	b.WriteString("\n## Provided symbols by source\n\n")
	var fams []string
	for fam := range rep.ProvidedSources {
		fams = append(fams, fam)
	}
	sort.Strings(fams)
	for _, fam := range fams {
		fmt.Fprintf(&b, "- %s: %d\n", fam, rep.ProvidedSources[fam])
	}
	return b.String()
}

// writeSnapshot writes the provided set as JSON for the per-release record.
func writeSnapshot(path string, p *ProvidedSet) error {
	type snap struct {
		Generated string            `json:"generated"`
		Symbols   map[string]string `json:"symbols"`
	}
	data, err := json.MarshalIndent(snap{
		Generated: time.Now().UTC().Format(time.RFC3339),
		Symbols:   p.Symbols,
	}, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, append(data, '\n'), 0o644)
}

// readSnapshot loads a provided set written by --snapshot.
func readSnapshot(path string) (*ProvidedSet, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	var snap struct {
		Symbols map[string]string `json:"symbols"`
	}
	if err := json.Unmarshal(data, &snap); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	if len(snap.Symbols) == 0 {
		return nil, fmt.Errorf("%s: no symbols in snapshot", path)
	}
	return &ProvidedSet{Symbols: snap.Symbols}, nil
}
