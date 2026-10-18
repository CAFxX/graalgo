package main

import (
	"fmt"
	"os"
	"strings"
)

// Exclusion is one flat "- file:" entry of the exclusion list.
type Exclusion struct {
	File   string
	Reason string
	Detail string
}

// ReasonCode is one vocabulary entry from the file's reason_codes section.
type ReasonCode struct {
	Code        string
	Description string
}

// ExclusionFile is the parsed exclusion list.
type ExclusionFile struct {
	Version    string
	Codes      []ReasonCode
	Exclusions []Exclusion
}

// ParseExclusionFile parses the versioned YAML exclusion file with the
// GG-0033 line-based semantics shared with test/differential LoadExclusions
// and the Java readExclusions in TypeparamDifferentialTest:
//
//	# comments allowed
//	version: 1
//	reason_codes:            # vocabulary; every reason: below must name one code
//	- code: PLATFORM
//	  description: free text (single line)
//	exclusions:
//	- file: runtime/cgo
//	  reason: CGO
//	  detail: free text (single line)
//
// Lines are matched on their trimmed form; "reason:" is only recognized
// inside an entry (after "- file:"), "detail:" likewise. The "version:"
// line wins on last occurrence, matching LoadExclusions. A "reason_codes:"
// section is invisible to the GG-0033 readers: "reason_codes:" does not
// carry the "reason:" prefix, "- code:" does not carry the "- file:"
// prefix, so existing readers skip the section unchanged.
//
// An empty "- file:" value is a parse error (reported with the physical
// line number): such an entry would otherwise be silently dropped by
// flushExcl, and an entry with no file is a typo, not an exclusion.
func ParseExclusionFile(path string) (*ExclusionFile, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read exclusions: %w", err)
	}
	out := &ExclusionFile{}
	var curExcl Exclusion
	inExcl := false
	var curCode ReasonCode
	inCode := false
	flushExcl := func() {
		if inExcl && curExcl.File != "" {
			out.Exclusions = append(out.Exclusions, curExcl)
		}
		curExcl = Exclusion{}
		inExcl = false
	}
	flushCode := func() {
		if inCode && curCode.Code != "" {
			out.Codes = append(out.Codes, curCode)
		}
		curCode = ReasonCode{}
		inCode = false
	}
	for i, line := range strings.Split(string(data), "\n") {
		t := strings.TrimSpace(line)
		if t == "" || strings.HasPrefix(t, "#") {
			continue
		}
		switch {
		case strings.HasPrefix(t, "version:"):
			out.Version = strings.TrimSpace(strings.TrimPrefix(t, "version:"))
		case strings.HasPrefix(t, "reason_codes:"):
			flushExcl()
		case strings.HasPrefix(t, "exclusions:"):
			flushCode()
		case strings.HasPrefix(t, "- code:"):
			flushCode()
			curCode.Code = strings.TrimSpace(strings.TrimPrefix(t, "- code:"))
			inCode = true
		case strings.HasPrefix(t, "description:") && inCode:
			curCode.Description = strings.TrimSpace(strings.TrimPrefix(t, "description:"))
		case strings.HasPrefix(t, "- file:"):
			flushExcl()
			val := strings.TrimSpace(strings.TrimPrefix(t, "- file:"))
			if val == "" {
				return nil, fmt.Errorf("exclusions file %s: line %d: empty file: value (an entry with no file is a typo, not an exclusion)", path, i+1)
			}
			curExcl.File = val
			inExcl = true
		case strings.HasPrefix(t, "reason:") && inExcl:
			curExcl.Reason = strings.TrimSpace(strings.TrimPrefix(t, "reason:"))
		case strings.HasPrefix(t, "detail:") && inExcl:
			curExcl.Detail = strings.TrimSpace(strings.TrimPrefix(t, "detail:"))
		}
	}
	flushExcl()
	flushCode()
	if out.Version == "" {
		return nil, fmt.Errorf("exclusions file %s has no version: line", path)
	}
	return out, nil
}

// CanonicalSignature returns a stable string identifying the semantic
// content of the list (vocabulary + entries, sorted). The version-stale
// check compares signatures: a signature change without a version bump
// is rejected.
func (f *ExclusionFile) CanonicalSignature() string {
	var b strings.Builder
	codes := append([]ReasonCode(nil), f.Codes...)
	sortReasonCodes(codes)
	for _, c := range codes {
		fmt.Fprintf(&b, "code\x00%s\x00%s\n", c.Code, c.Description)
	}
	excls := append([]Exclusion(nil), f.Exclusions...)
	sortExclusions(excls)
	for _, e := range excls {
		fmt.Fprintf(&b, "entry\x00%s\x00%s\x00%s\n", e.File, e.Reason, e.Detail)
	}
	return b.String()
}

func sortReasonCodes(cs []ReasonCode) {
	for i := 1; i < len(cs); i++ {
		for j := i; j > 0 && cs[j].Code < cs[j-1].Code; j-- {
			cs[j], cs[j-1] = cs[j-1], cs[j]
		}
	}
}

func sortExclusions(es []Exclusion) {
	for i := 1; i < len(es); i++ {
		for j := i; j > 0 && es[j].File < es[j-1].File; j-- {
			es[j], es[j-1] = es[j-1], es[j]
		}
	}
}
