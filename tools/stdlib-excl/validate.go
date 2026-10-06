package main

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// Validate-time reason for rejection; runValidate returns these as errors
// so "validate" exits 1 with a precise message.
func runValidate(args []string, stdout, stderr io.Writer) error {
	fs := newFlagSet("validate")
	listPath := fs.String("list", "", "path to exclusions.yaml (default: <repo>/phases/phase3/stdlib-fork/exclusions.yaml)")
	srcRoot := fs.String("src", "", "stdlib source root (default: vendored tree when present, else GOROOT/src)")
	maxEntries := fs.Int("max", 25, "hard cap on exclusion entries (review cap)")
	testBinaries := fs.Int("test-binaries", 0, "stdlib test-binary count for the 5% clause (0 = count from --src)")
	baselinePath := fs.String("baseline", "", "baseline list for the version-stale check (default: git HEAD version)")
	if err := fs.Parse(args); err != nil {
		return &usageError{fmt.Sprintf("validate: %v", err)}
	}
	if *listPath == "" {
		root, err := repoRoot()
		if err != nil {
			return fmt.Errorf("validate: cannot locate repo root: %w", err)
		}
		*listPath = filepath.Join(root, "phases", "phase3", "stdlib-fork", "exclusions.yaml")
	}
	var err error
	*listPath, err = filepath.Abs(*listPath)
	if err != nil {
		return fmt.Errorf("validate: %w", err)
	}
	if *srcRoot == "" {
		var err error
		*srcRoot, err = defaultSrcRoot()
		if err != nil {
			return fmt.Errorf("validate: %w", err)
		}
	}

	ef, err := ParseExclusionFile(*listPath)
	if err != nil {
		return fmt.Errorf("validate: %w", err)
	}

	var problems []string
	add := func(format string, a ...any) {
		problems = append(problems, fmt.Sprintf(format, a...))
	}

	// 1. Vocabulary must be defined: without it the known-code check below
	// is vacuous.
	known := map[string]bool{}
	for _, c := range ef.Codes {
		if c.Code == "" {
			add("reason_codes: entry with empty code")
			continue
		}
		if known[c.Code] {
			add("reason_codes: duplicate code %q", c.Code)
		}
		known[c.Code] = true
	}
	if len(known) == 0 {
		add("no reason_codes: vocabulary defined; every reason: would be unknown")
	}

	// 2. Per-entry checks: file present, reason mandatory + known, no dupes.
	seen := map[string]bool{}
	for i, e := range ef.Exclusions {
		where := fmt.Sprintf("entry %d", i+1)
		if e.File == "" {
			add("%s: empty file:", where)
			continue
		}
		where = fmt.Sprintf("entry %q", e.File)
		if seen[e.File] {
			add("%s: duplicate file entry", where)
		}
		seen[e.File] = true
		if e.Reason == "" {
			add("%s: missing reason code (reason: is mandatory)", where)
			continue
		}
		if !known[e.Reason] {
			add("%s: unknown reason code %q (not in reason_codes: vocabulary)", where, e.Reason)
		}
		if e.Detail == "" {
			add("%s: missing detail: (every v1 entry must carry evidence)", where)
		}
	}

	// 3. Cap: 25 packages OR 5% of stdlib test binaries, whichever binds first.
	nTest := *testBinaries
	if nTest <= 0 {
		var err error
		nTest, err = countTestBinaries(*srcRoot)
		if err != nil {
			return fmt.Errorf("validate: count test binaries: %w", err)
		}
	}
	fivePct := (5 * nTest) / 100 // floor; documented
	binding := *maxEntries
	bindingWhy := fmt.Sprintf("--max=%d", *maxEntries)
	if fivePct < binding {
		binding = fivePct
		bindingWhy = fmt.Sprintf("5%% of %d stdlib test binaries = %d", nTest, fivePct)
	}
	if len(ef.Exclusions) > binding {
		add("%d entries exceed the review cap (%s binds)", len(ef.Exclusions), bindingWhy)
	}

	// 4. Version-stale check: entries changed without a version bump.
	base := *baselinePath
	baseFromGit := false
	if base == "" {
		root, err := repoRoot()
		if err == nil {
			if rel, err := filepath.Rel(root, *listPath); err == nil {
				baseFromGit = true
				base = "git:HEAD:" + filepath.ToSlash(rel)
			}
		}
	}
	if base != "" {
		var baseFile *ExclusionFile
		if baseFromGit {
			rel := strings.TrimPrefix(base, "git:HEAD:")
			baseFile, err = parseGitHead(rel)
			if err != nil {
				fmt.Fprintf(stderr, "warning: version-stale check skipped: %v\n", err)
			}
		} else {
			baseFile, err = ParseExclusionFile(base)
			if err != nil {
				fmt.Fprintf(stderr, "warning: version-stale check skipped: %v\n", err)
			}
		}
		if baseFile != nil {
			if baseFile.CanonicalSignature() != ef.CanonicalSignature() &&
				baseFile.Version == ef.Version {
				add("list changed without a version bump (still version %s); bump version: to %s",
					ef.Version, bumpHint(ef.Version))
			}
		}
	}

	// 5. Freshness: every entry's package must exist in the tree.
	for _, e := range ef.Exclusions {
		if e.File == "" {
			continue
		}
		abs := filepath.Join(*srcRoot, filepath.FromSlash(e.File))
		fi, err := os.Stat(abs)
		if err != nil || !fi.IsDir() {
			add("stale entry %q: %s is not a package dir in %s", e.File, abs, *srcRoot)
		}
	}

	if len(problems) > 0 {
		for _, p := range problems {
			fmt.Fprintf(stderr, "validate: REJECT: %s\n", p)
		}
		return fmt.Errorf("%d problem(s) in %s", len(problems), *listPath)
	}
	fmt.Fprintf(stdout, "validate: OK: %s version %s, %d entries, cap %d (%s), src %s\n",
		*listPath, ef.Version, len(ef.Exclusions), binding, bindingWhy, *srcRoot)
	return nil
}

// repoRoot returns the enclosing git repository root.
func repoRoot() (string, error) {
	out, err := exec.Command("git", "rev-parse", "--show-toplevel").Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

// parseGitHead parses the HEAD-committed version of a repo-relative path.
// It returns (nil, nil) when the file is new in the working tree (not in
// HEAD): a new file cannot be version-stale.
func parseGitHead(repoRel string) (*ExclusionFile, error) {
	out, err := exec.Command("git", "show", "HEAD:"+repoRel).Output()
	if err != nil {
		return nil, nil // new file: no baseline, check skipped
	}
	tmp, err := os.CreateTemp("", "excl-baseline-*.yaml")
	if err != nil {
		return nil, err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(out); err != nil {
		tmp.Close()
		return nil, err
	}
	tmp.Close()
	return ParseExclusionFile(tmp.Name())
}

// defaultSrcRoot resolves the stdlib tree: the vendored tree when the
// vendoring worker has landed (pinned-version.txt + target dir exist),
// else GOROOT/src of the pinned toolchain.
func defaultSrcRoot() (string, error) {
	if root, err := repoRoot(); err == nil {
		pinPath := filepath.Join(root, "phases", "phase3", "stdlib-fork", "pinned-version.txt")
		if data, err := os.ReadFile(pinPath); err == nil {
			ver := strings.TrimSpace(string(data))
			if ver != "" {
				cand := filepath.Join(os.Getenv("HOME"), "workspace", "graalgo-stdlib", ver, "src")
				if fi, err := os.Stat(cand); err == nil && fi.IsDir() {
					return cand, nil
				}
			}
		}
	}
	out, err := exec.Command(goBinary(), "env", "GOROOT").Output()
	if err != nil {
		return "", fmt.Errorf("no --src and cannot determine GOROOT: %w", err)
	}
	src := filepath.Join(strings.TrimSpace(string(out)), "src")
	if fi, err := os.Stat(src); err != nil || !fi.IsDir() {
		return "", fmt.Errorf("no --src and GOROOT/src %s not usable", src)
	}
	return src, nil
}

// goBinary returns the pinned Go toolchain binary.
func goBinary() string {
	if p := os.Getenv("STDLIB_EXCL_GO"); p != "" {
		return p
	}
	return filepath.Join(os.Getenv("HOME"), "tools", "go", "bin", "go")
}

// bumpHint suggests the next version for a numeric version string.
func bumpHint(v string) string {
	v = strings.TrimSpace(v)
	n := 0
	for _, r := range v {
		if r < '0' || r > '9' {
			return "<next>"
		}
		n = n*10 + int(r-'0')
	}
	return fmt.Sprintf("%d", n+1)
}
