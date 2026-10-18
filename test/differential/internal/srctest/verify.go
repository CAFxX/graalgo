package srctest

import (
	"fmt"
	"sort"
	"strings"

	"diffharness/internal/typeparam"
)

// ValidExclusionReasons are the GG-0033 reason codes accepted in
// srctest-exclusions.yaml (the same vocabulary as the typeparam list;
// enforced for typeparam by TypeparamDifferentialTest, for srctest here).
var ValidExclusionReasons = map[string]bool{
	"backend-specific":     true,
	"unspecified-behavior": true,
	"deferred-parity":      true,
}

// VerifyExclusions checks the versioned YAML exclusion list against a fresh
// scan of dir (GG-0033 coverage for the srctest adapter; the typeparam
// adapter's equivalent is TypeparamDifferentialTest in Java). It reports:
//
//   - stale entries: a listed file the scan never sees (renamed/removed
//     upstream, or a typo in the file name);
//   - invalid reason codes: anything outside ValidExclusionReasons;
//   - empty details: an exclusion without a human-readable justification is
//     unauditable (mirrors the Java test's P1-7 rule);
//   - shadowed entries: a listed file whose natural class (scan with no
//     YAML applied) is already an exclusion — the scanner excludes it
//     structurally, so the YAML entry is redundant dead weight that can
//     only rot.
//
// It returns one problem string per violation, sorted; an empty slice
// means the list is consistent with the scan.
func VerifyExclusions(dir, yamlPath string) ([]string, error) {
	version, excl, err := typeparam.LoadExclusions(yamlPath)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(version) == "" {
		return nil, fmt.Errorf("exclusions file %s has no version", yamlPath)
	}
	// Natural classes: the scan with no YAML applied.
	files, err := Scan(dir, nil)
	if err != nil {
		return nil, err
	}
	byName := make(map[string]typeparam.TFile, len(files))
	for _, f := range files {
		byName[f.Name] = f
	}
	var problems []string
	for _, name := range sortedKeys(excl) {
		x := excl[name]
		f, ok := byName[name]
		if !ok {
			problems = append(problems,
				fmt.Sprintf("%s: stale exclusion-list entry (file not in scan of %s)", name, dir))
			continue
		}
		if !ValidExclusionReasons[x.Reason] {
			problems = append(problems,
				fmt.Sprintf("%s: invalid reason code %q (want one of backend-specific, unspecified-behavior, deferred-parity)", name, x.Reason))
		}
		if strings.TrimSpace(x.Detail) == "" {
			problems = append(problems,
				fmt.Sprintf("%s: empty exclusion detail (GG-0033: every exclusion must say why)", name))
		}
		if strings.HasPrefix(f.Class, "exclude") {
			problems = append(problems,
				fmt.Sprintf("%s: shadowed exclusion (scanner already excludes it as %q; YAML entry is redundant)", name, f.Class))
		}
	}
	sort.Strings(problems)
	return problems, nil
}

func sortedKeys(m map[string]typeparam.Exclusion) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
