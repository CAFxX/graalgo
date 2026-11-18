package harness

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"regexp"
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
//	  signature: 3f2a9c0d1e4b5a6c   # optional: DiffSignature of the
//	                                # expected divergence (global review G2)
//	groups:                         # optional: one cause, many files
//	- group: v1-multi-file
//	  reason: deferred-parity
//	  expires: M3
//	  detail: free text
//	  files: a.go b.go
//	  files: c.go                   # repeatable
//
// A group expands to one entry per file whose detail is prefixed with
// "[group <name>, expires <milestone>]". A group needs a reason, a detail,
// an expiry and at least one file.
//
// signature pins WHAT the expected divergence is: without it any new
// divergence of a listed program (a fresh crash, a different wrong output)
// would still read as EXPECTED. A gate that enforces pinning reports a
// changed signature as a divergence and an unpinned entry as a failure.

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
	File      string `json:"file"`
	Reason    string `json:"reason"`
	Detail    string `json:"detail"`
	Signature string `json:"signature,omitempty"`
	Group     string `json:"group,omitempty"`
}

// DiffSignature is the short content hash a `signature:` line pins: the
// first 16 hex digits of the SHA-256 of the gate's divergence text.
func DiffSignature(diff string) string {
	sum := sha256.Sum256([]byte(diff))
	return hex.EncodeToString(sum[:])[:16]
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

type groupEntry struct {
	name, reason, detail, expires string
	files                         []string
	line                          int
}

// ParseReviewedExclusions is LoadReviewedExclusions on in-memory text
// (name is used in error messages).
func ParseReviewedExclusions(name, text string) (version string, excl map[string]ReviewedExclusion, err error) {
	excl = map[string]ReviewedExclusion{}
	var errs []string
	add := func(line int, x ReviewedExclusion) {
		if _, dup := excl[x.File]; dup {
			errs = append(errs, fmt.Sprintf("line %d: %s: duplicate entry", line, x.File))
			return
		}
		excl[x.File] = x
	}
	var cur ReviewedExclusion
	var grp *groupEntry
	curLine := 0
	inEntry := false
	inCodes := false // a reason_codes: vocabulary section (stdlib-fork list)
	flush := func() {
		if grp != nil {
			g := grp
			grp = nil
			switch {
			case g.name == "":
				errs = append(errs, fmt.Sprintf("line %d: group without a name", g.line))
			case !ValidReason(g.reason):
				errs = append(errs, fmt.Sprintf("line %d: group %s: reason %q is not one of %v", g.line, g.name, g.reason, ReasonCodes))
			case g.detail == "" || g.expires == "":
				errs = append(errs, fmt.Sprintf("line %d: group %s: needs detail and expires", g.line, g.name))
			case len(g.files) == 0:
				errs = append(errs, fmt.Sprintf("line %d: group %s: no files", g.line, g.name))
			default:
				for _, f := range g.files {
					add(g.line, ReviewedExclusion{File: f, Reason: g.reason, Group: g.name,
						Detail: fmt.Sprintf("[group %s, expires %s] %s", g.name, g.expires, g.detail)})
				}
			}
			return
		}
		if !inEntry {
			return
		}
		switch {
		case cur.File == "":
			errs = append(errs, fmt.Sprintf("line %d: entry without a name", curLine))
		case !ValidReason(cur.Reason):
			errs = append(errs, fmt.Sprintf("line %d: %s: reason %q is not one of %v", curLine, cur.File, cur.Reason, ReasonCodes))
		default:
			add(curLine, cur)
		}
		cur = ReviewedExclusion{}
		inEntry = false
	}
	for i, line := range strings.Split(text, "\n") {
		t := strings.TrimSpace(line)
		if t == "" || strings.HasPrefix(t, "#") {
			continue
		}
		val := func(key string) string { return strings.TrimSpace(strings.TrimPrefix(t, key)) }
		if t == "reason_codes:" {
			flush()
			inCodes = true
			continue
		}
		if inCodes {
			if t != "exclusions:" && t != "groups:" && !strings.HasPrefix(t, "version:") {
				continue // the vocabulary is validated by tools/stdlib-excl
			}
			inCodes = false
		}
		switch {
		case strings.HasPrefix(t, "version:"):
			version = val("version:")
		case t == "exclusions:" || t == "exclusions: []" || t == "groups:" || t == "groups: []":
		case strings.HasPrefix(t, "- file:"):
			flush()
			cur.File = val("- file:")
			curLine = i + 1
			inEntry = true
		case strings.HasPrefix(t, "- group:"):
			flush()
			grp = &groupEntry{name: val("- group:"), line: i + 1}
		case grp != nil && strings.HasPrefix(t, "reason:"):
			grp.reason = val("reason:")
		case grp != nil && strings.HasPrefix(t, "detail:"):
			grp.detail = val("detail:")
		case grp != nil && strings.HasPrefix(t, "expires:"):
			grp.expires = val("expires:")
		case grp != nil && strings.HasPrefix(t, "files:"):
			grp.files = append(grp.files, strings.Fields(val("files:"))...)
		case strings.HasPrefix(t, "reason:") && inEntry:
			cur.Reason = val("reason:")
		case strings.HasPrefix(t, "detail:") && inEntry:
			cur.Detail = val("detail:")
		case strings.HasPrefix(t, "signature:") && inEntry:
			cur.Signature = val("signature:")
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

// pinnedSig is a DiffSignature; unpinnedSig is the explicit, reasoned
// "cannot pin" marker: `signature: unpinned (<why>)` for an entry whose
// divergence text is not reproducible (timings) or that never runs.
var (
	pinnedSig   = regexp.MustCompile(`^[0-9a-f]{16}$`)
	unpinnedSig = regexp.MustCompile(`^unpinned \(\s*\S.*\)$`)
)

// IsPinned reports whether sig is a DiffSignature (not empty, not the
// unpinned marker): only a pinned signature is compared with a divergence.
func IsPinned(sig string) bool { return pinnedSig.MatchString(sig) }

// bugRef matches a bug-register id (docs/dev/bugs.md).
var bugRef = regexp.MustCompile(`\bBUG-[0-9]{4}\b`)

// bugRow matches a register row of docs/dev/bugs.md: "| BUG-NNNN | ...".
var bugRow = regexp.MustCompile(`(?m)^\|\s*(BUG-[0-9]{4})\s*\|`)

// LoadBugIDs returns the ids of the bug register's rows.
func LoadBugIDs(path string) (map[string]bool, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read bug register: %w", err)
	}
	ids := map[string]bool{}
	for _, m := range bugRow.FindAllStringSubmatch(string(data), -1) {
		ids[m[1]] = true
	}
	if len(ids) == 0 {
		return nil, fmt.Errorf("bug register %s has no BUG-NNNN rows", path)
	}
	return ids, nil
}

// RegisterProblems is the register gate (AGENTS.md §3, docs/dev/bugs.md):
// a deferred-parity entry is a graalgo bug, so it must cite at least one id
// of the bug register (every cited id must exist), and — in a differential
// register (requireSignature) — pin the signature of its divergence. A
// group entry (one cause, many files, with an expiry) needs the bug id but
// no signature: there is no single divergence text to pin.
func RegisterProblems(name string, excl map[string]ReviewedExclusion, bugs map[string]bool, requireSignature bool) []string {
	var problems []string
	for _, f := range SortedExclusionNames(excl) {
		x := excl[f]
		for _, id := range bugRef.FindAllString(x.Detail, -1) {
			if !bugs[id] {
				problems = append(problems, fmt.Sprintf("%s: %s: cites %s, which is not in the bug register", name, f, id))
			}
		}
		if x.Reason != "deferred-parity" {
			continue
		}
		if !bugRef.MatchString(x.Detail) {
			problems = append(problems, fmt.Sprintf("%s: %s: deferred-parity entry cites no BUG id (a graalgo bug needs a docs/dev/bugs.md row)", name, f))
		}
		if requireSignature && x.Group == "" && !IsPinned(x.Signature) && !unpinnedSig.MatchString(x.Signature) {
			problems = append(problems, fmt.Sprintf("%s: %s: deferred-parity entry pins no signature (a 16-hex DiffSignature, or `unpinned (<why>)` when the text is not reproducible)", name, f))
		}
	}
	return problems
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
