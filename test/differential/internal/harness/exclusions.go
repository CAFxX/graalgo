package harness

import (
	"fmt"
	"os"
	"sort"
	"strings"
)

// Reviewed exclusions (SPEC §5.6, GG-0033): one vocabulary, one file format,
// for every differential layer (corpus snapshot gate, GOROOT/test srctest
// adapter, typeparam adapter).
//
// An entry names a test that is EXPECTED to diverge from gc, with a reason
// code and a free-text detail. Entries are never "skip silently": the
// gates still run excluded tests and fail when one unexpectedly matches gc
// (a stale exclusion must be pruned — a shrinking list is the health
// metric).
//
// File format (deliberately minimal, no YAML dependency):
//
//	# comments allowed
//	version: 1
//	exclusions:
//	- file: foo.go          # or a corpus program name
//	  reason: deferred-parity
//	  detail: free text (bug id / what retires the entry)

// ReasonCodes is the closed reason-code vocabulary of SPEC §5.6.
var ReasonCodes = []string{"backend-specific", "unspecified-behavior", "deferred-parity"}

// ValidReason reports whether code is one of ReasonCodes.
func ValidReason(code string) bool {
	for _, c := range ReasonCodes {
		if c == code {
			return true
		}
	}
	return false
}

// ReviewedExclusion is one entry of a versioned exclusion file.
type ReviewedExclusion struct {
	File   string `json:"file"`
	Reason string `json:"reason"`
	Detail string `json:"detail"`
}

// LoadReviewedExclusions parses and validates a versioned exclusion file:
// every entry needs a reason from ReasonCodes, and no name may appear
// twice. (Detail is free text; reviewers require it, the parser does not,
// to stay byte-compatible with the Java readExclusions parser — P2-8.)
func LoadReviewedExclusions(path string) (version string, excl map[string]ReviewedExclusion, err error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", nil, fmt.Errorf("read exclusions: %w", err)
	}
	return ParseReviewedExclusions(path, string(data))
}

// ParseReviewedExclusions is LoadReviewedExclusions on in-memory text
// (name is used in error messages).
func ParseReviewedExclusions(name, text string) (version string, excl map[string]ReviewedExclusion, err error) {
	excl = map[string]ReviewedExclusion{}
	var errs []string
	var cur ReviewedExclusion
	curLine := 0
	inEntry := false
	flush := func() {
		if !inEntry {
			return
		}
		switch {
		case cur.File == "":
			errs = append(errs, fmt.Sprintf("line %d: entry without a name", curLine))
		case !ValidReason(cur.Reason):
			errs = append(errs, fmt.Sprintf("line %d: %s: reason %q is not one of %v", curLine, cur.File, cur.Reason, ReasonCodes))
		default:
			if _, dup := excl[cur.File]; dup {
				errs = append(errs, fmt.Sprintf("line %d: %s: duplicate entry", curLine, cur.File))
			}
			excl[cur.File] = cur
		}
		cur = ReviewedExclusion{}
		inEntry = false
	}
	for i, line := range strings.Split(text, "\n") {
		t := strings.TrimSpace(line)
		if t == "" || strings.HasPrefix(t, "#") {
			continue
		}
		switch {
		case strings.HasPrefix(t, "version:"):
			version = strings.TrimSpace(strings.TrimPrefix(t, "version:"))
		case t == "exclusions:" || t == "exclusions: []":
		case strings.HasPrefix(t, "- file:"):
			flush()
			cur.File = strings.TrimSpace(strings.TrimPrefix(t, "- file:"))
			curLine = i + 1
			inEntry = true
		case strings.HasPrefix(t, "reason:") && inEntry:
			cur.Reason = strings.TrimSpace(strings.TrimPrefix(t, "reason:"))
		case strings.HasPrefix(t, "detail:") && inEntry:
			cur.Detail = strings.TrimSpace(strings.TrimPrefix(t, "detail:"))
		default:
			errs = append(errs, fmt.Sprintf("line %d: unrecognized line %q", i+1, t))
		}
	}
	flush()
	if version == "" {
		errs = append(errs, "no version: line")
	}
	if len(errs) > 0 {
		return "", nil, fmt.Errorf("exclusions file %s invalid:\n  %s", name, strings.Join(errs, "\n  "))
	}
	return version, excl, nil
}

// SortedExclusionNames returns the entry names in sorted order.
func SortedExclusionNames(excl map[string]ReviewedExclusion) []string {
	names := make([]string, 0, len(excl))
	for n := range excl {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}
