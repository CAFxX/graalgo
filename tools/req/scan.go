package main

import (
	"bytes"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// Ref is one requirement-ID mention found while scanning the repo.
type Ref struct {
	ID   string // requirement ID in GG-XXXX format
	File string // repo-relative path
	Line int    // 1-based
	Text string // trimmed line content
	Kind RefKind
}

type RefKind int

const (
	KindRef RefKind = iota
	KindVerifies
)

func (k RefKind) String() string {
	if k == KindVerifies {
		return "verifies"
	}
	return "ref"
}

var (
	tokenRe    = regexp.MustCompile(`\bGG-(?:` + idBody + `)\b`)
	verifiesRe = regexp.MustCompile(`verifies:\s*(GG-(?:` + idBody + `))`)
	sloppyRe   = regexp.MustCompile(`(?i)\bGG-(?:[A-Z]+-\d+|\d+)\b`)
	fenceRe    = regexp.MustCompile("(?s)```.*?```")
	inlineRe   = regexp.MustCompile("`[^`\n]*`")
	idLineRe   = regexp.MustCompile(`^\s*id\s*:`)
)

// stripMarkdownCode removes fenced code blocks and inline code spans.
// IDs inside them are illustrative (docs examples), not normative, and are
// therefore exempt from lint rule (a).
func stripMarkdownCode(s string) string {
	s = fenceRe.ReplaceAllString(s, "")
	s = inlineRe.ReplaceAllString(s, "")
	return s
}

// scanAll walks scanDir and returns every requirement-ID mention.
// ownIDLines suppresses the self-identifying `id:` line of requirement files.
func scanAll(c *workCtx) ([]Ref, []string, error) {
	var refs []Ref
	var fmts []string
	err := filepath.WalkDir(c.scanDir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil // unreadable path: skip, don't fail the scan
		}
		if d.IsDir() {
			if d.Name() == ".git" {
				return filepath.SkipDir
			}
			return nil
		}
		rel, err := filepath.Rel(c.root, path)
		if err != nil {
			rel = path
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return nil
		}
		if bytes.IndexByte(data, 0) >= 0 {
			return nil // binary file
		}
		text := string(data)
		if strings.HasSuffix(strings.ToLower(path), ".md") {
			text = stripMarkdownCode(text)
		}
		ownID := ""
		if filepath.Dir(path) == c.reqDir {
			if m := yamlNameRe.FindStringSubmatch(filepath.Base(path)); m != nil {
				ownID = m[1]
			}
		}
		verifiesAt := map[int]string{} // line -> id for verifies: annotations
		for i, line := range strings.Split(text, "\n") {
			for _, m := range verifiesRe.FindAllStringSubmatch(line, -1) {
				verifiesAt[i+1] = m[1]
			}
		}
		// In *_test.go files only `verifies: GG-XXXX` annotations are
		// meaningful references: test sources deliberately contain
		// fake/invalid IDs inside fixtures and string literals. Plain
		// refs and format checks are skipped there; verifies refs still
		// count (rule: every active requirement needs ≥1 verifies:).
		testOnly := strings.HasSuffix(d.Name(), "_test.go")
		for i, line := range strings.Split(text, "\n") {
			ln := i + 1
			if ownID != "" && idLineRe.MatchString(line) && strings.Contains(line, ownID) {
				continue // the record's own `id:` line is not a reference
			}
			for _, tok := range tokenRe.FindAllString(line, -1) {
				kind := KindRef
				if verifiesAt[ln] == tok {
					kind = KindVerifies
				}
				if testOnly && kind != KindVerifies {
					continue
				}
				refs = append(refs, Ref{ID: tok, File: rel, Line: ln, Text: strings.TrimSpace(line), Kind: kind})
			}
			if testOnly {
				continue
			}
			for _, tok := range sloppyRe.FindAllString(line, -1) {
				if !idPattern.MatchString(tok) {
					fmts = append(fmts, rel+":"+itoa(ln)+": non-canonical requirement id "+tok)
				}
			}
		}
		return nil
	})
	if err != nil {
		return nil, nil, err
	}
	sort.Slice(refs, func(i, j int) bool {
		if refs[i].File != refs[j].File {
			return refs[i].File < refs[j].File
		}
		return refs[i].Line < refs[j].Line
	})
	return refs, fmts, nil
}

func itoa(n int) string { return strconv.Itoa(n) }
