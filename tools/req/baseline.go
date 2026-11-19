package main

// Rule (f): the registry only moves forward relative to its base.
//
// Rule (e) checks the locked text against LOCKED-TEXT.sum, but the sum file
// and the statuses are themselves plain files: a change could rewrite a sum
// line together with the text it covers, drop a line, or flip a locked
// requirement back to draft (which exempts it from rule (e)). Rule (f)
// compares the working tree with the registry at a base commit:
//
//   - every LOCKED-TEXT.sum line of the base is still present, unchanged
//     (lines may only be added);
//   - no requirement of the base disappears, and none moves back in the
//     lifecycle (draft < locked < active < superseded/retired).
//
// The base is $GATE_BASE when it names a commit (CI: the push's `before`
// sha or the PR base), else the merge-base of HEAD with origin/master (else
// master). Outside a git work tree, or with no resolvable base, rule (f)
// has nothing to compare and is skipped (the hermetic unit fixtures).

import (
	"archive/tar"
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

var zeroSHA = regexp.MustCompile(`^0+$`)

func git(dir string, args ...string) (string, error) {
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	var out, errb bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errb
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("git %s: %v: %s", strings.Join(args, " "), err, strings.TrimSpace(errb.String()))
	}
	return strings.TrimSpace(out.String()), nil
}

func gitCommit(dir, ref string) string {
	if ref == "" || zeroSHA.MatchString(ref) {
		return ""
	}
	sha, err := git(dir, "rev-parse", "--verify", "--quiet", ref+"^{commit}")
	if err != nil {
		return ""
	}
	return sha
}

// gitBase resolves the comparison base for dir ("" if none).
func gitBase(dir string) string {
	if sha := gitCommit(dir, os.Getenv("GATE_BASE")); sha != "" {
		return sha
	}
	for _, up := range []string{"origin/master", "master"} {
		if gitCommit(dir, up) == "" {
			continue
		}
		if mb, err := git(dir, "merge-base", "HEAD", up); err == nil && mb != "" {
			return mb
		}
	}
	return ""
}

// baseRegistry materializes reqDir as of base into a temp dir and loads it.
// found is false when reqDir did not exist at base.
func baseRegistry(reqDir, base string) (reg map[string]*Requirement, sums map[string]string, found bool, err error) {
	top, err := git(reqDir, "rev-parse", "--show-toplevel")
	if err != nil {
		return nil, nil, false, err
	}
	absReq, err := filepath.Abs(reqDir)
	if err != nil {
		return nil, nil, false, err
	}
	if r, err := filepath.EvalSymlinks(absReq); err == nil {
		absReq = r
	}
	if r, err := filepath.EvalSymlinks(top); err == nil {
		top = r
	}
	rel, err := filepath.Rel(top, absReq)
	if err != nil {
		return nil, nil, false, err
	}
	rel = filepath.ToSlash(rel)
	if _, err := git(top, "cat-file", "-e", base+":"+rel); err != nil {
		return nil, nil, false, nil // no registry at base: nothing to compare
	}
	cmd := exec.Command("git", "-C", top, "archive", "--format=tar", base, "--", rel)
	var out, errb bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errb
	if err := cmd.Run(); err != nil {
		return nil, nil, false, fmt.Errorf("git archive %s -- %s: %v: %s", base, rel, err, errb.String())
	}
	tmp, err := os.MkdirTemp("", "req-base-")
	if err != nil {
		return nil, nil, false, err
	}
	defer os.RemoveAll(tmp)
	tr := tar.NewReader(&out)
	for {
		h, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, nil, false, err
		}
		if h.Typeflag != tar.TypeReg {
			continue
		}
		name := filepath.FromSlash(h.Name)
		if !strings.HasPrefix(h.Name, rel+"/") || strings.Contains(h.Name, "..") {
			continue
		}
		p := filepath.Join(tmp, name)
		if err := os.MkdirAll(filepath.Dir(p), 0755); err != nil {
			return nil, nil, false, err
		}
		data, err := io.ReadAll(tr)
		if err != nil {
			return nil, nil, false, err
		}
		if err := os.WriteFile(p, data, 0644); err != nil {
			return nil, nil, false, err
		}
	}
	dir := filepath.Join(tmp, filepath.FromSlash(rel))
	reg, err = loadRegistry(dir)
	if err != nil {
		return nil, nil, false, fmt.Errorf("registry at base %s: %v", base, err)
	}
	sums, err = loadTextSums(dir)
	if err != nil {
		return nil, nil, false, fmt.Errorf("registry at base %s: %v", base, err)
	}
	return reg, sums, true, nil
}

// statusRank orders the lifecycle; a requirement may only move up.
var statusRank = map[string]int{"draft": 0, "locked": 1, "active": 2, "superseded": 3, "retired": 3}

// baselineProblems implements rule (f) against an already-loaded base.
func baselineProblems(base string, reg, baseReg map[string]*Requirement, sums, baseSums map[string]string) []string {
	var problems []string
	short := base
	if len(short) > 12 {
		short = short[:12]
	}
	ids := make([]string, 0, len(baseSums))
	for id := range baseSums {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		got, ok := sums[id]
		switch {
		case !ok:
			problems = append(problems, fmt.Sprintf(
				"rule (f): %s line for %s removed (base %s); sum lines may only be added", textSumFile, id, short))
		case got != baseSums[id]:
			problems = append(problems, fmt.Sprintf(
				"rule (f): %s line for %s changed (base %s: %s, now %s); sum lines may only be added", textSumFile, id, short, baseSums[id], got))
		}
	}
	for _, id := range sortedIDs(baseReg) {
		was := baseReg[id].Status
		cur, ok := reg[id]
		if !ok {
			problems = append(problems, fmt.Sprintf(
				"rule (f): requirement %s (%s at base %s) was deleted; requirements are retired or superseded, never removed", id, was, short))
			continue
		}
		if statusRank[cur.Status] < statusRank[was] {
			problems = append(problems, fmt.Sprintf(
				"rule (f): %s: status regressed %s -> %s relative to base %s", id, was, cur.Status, short))
		}
	}
	return problems
}

// baselineCheck runs rule (f) for the registry at reqDir; it returns no
// problems (and a note) when there is no git base to compare with.
func baselineCheck(reqDir string, reg map[string]*Requirement, sums map[string]string) (problems []string, note string, err error) {
	if _, err := git(reqDir, "rev-parse", "--is-inside-work-tree"); err != nil {
		return nil, "rule (f) skipped: not in a git work tree", nil
	}
	base := gitBase(reqDir)
	if base == "" {
		return nil, "rule (f) skipped: no base (set GATE_BASE or fetch origin/master)", nil
	}
	baseReg, baseSums, found, err := baseRegistry(reqDir, base)
	if err != nil {
		return nil, "", err
	}
	if !found {
		return nil, "rule (f): no registry at base " + base, nil
	}
	return baselineProblems(base, reg, baseReg, sums, baseSums), "", nil
}
