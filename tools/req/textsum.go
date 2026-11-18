package main

// Locked-text integrity.
//
// A locked requirement is immutable except via supersede/retire, but the
// registry is plain YAML: nothing stopped a hand edit from rewording a
// locked statement in place. The text sum closes that hole: `req lock`
// records a content hash of each requirement's normative text in
// requirements/LOCKED-TEXT.sum, and lint rule (e) fails when a non-draft
// requirement has no recorded sum or its text no longer matches it.
//
// The hashed text is everything a sign-off covers: id, title, statement,
// rationale, parent and verification plan. Status, superseded_by and
// history are lifecycle bookkeeping and change legitimately after lock.
//
// `req seal` records sums for non-draft requirements that have none (the
// one-time baseline of requirements locked before the sum existed). It
// never overwrites a recorded sum, so it cannot launder an in-place edit.

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// textSumFile is the manifest name inside the registry directory.
const textSumFile = "LOCKED-TEXT.sum"

const textSumHeader = `# Locked-text sums: sha256 of each non-draft requirement's normative text
# (id, title, statement, rationale, parent, verification_plan). Written by
# 'req lock' (and once by 'req seal' for the pre-existing baseline); checked
# by 'req lint' rule (e). Never edit by hand: a locked text changes only
# through 'req supersede'.
`

// textSum returns the canonical hash of r's normative text.
func textSum(r *Requirement) string {
	h := sha256.New()
	for _, field := range []string{r.ID, r.Title, r.Statement, r.Rationale, r.Parent, r.VerificationPlan} {
		// Length-prefix each field so no two field tuples collide.
		fmt.Fprintf(h, "%d:%s\n", len(field), field)
	}
	return "sha256:" + hex.EncodeToString(h.Sum(nil))
}

// loadTextSums reads the manifest; a missing file is an empty manifest.
func loadTextSums(reqDir string) (map[string]string, error) {
	sums := map[string]string{}
	f, err := os.Open(filepath.Join(reqDir, textSumFile))
	if os.IsNotExist(err) {
		return sums, nil
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	n := 0
	for sc.Scan() {
		n++
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) != 2 || !idPattern.MatchString(fields[0]) || !strings.HasPrefix(fields[1], "sha256:") {
			return nil, fmt.Errorf("%s:%d: malformed line %q (want \"GG-XXXX sha256:<hex>\")", textSumFile, n, line)
		}
		if _, dup := sums[fields[0]]; dup {
			return nil, fmt.Errorf("%s:%d: duplicate entry for %s", textSumFile, n, fields[0])
		}
		sums[fields[0]] = fields[1]
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	return sums, nil
}

// renderTextSums serializes the manifest deterministically (sorted by id).
func renderTextSums(sums map[string]string) []byte {
	ids := make([]string, 0, len(sums))
	for id := range sums {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	var b strings.Builder
	b.WriteString(textSumHeader)
	for _, id := range ids {
		b.WriteString(id + " " + sums[id] + "\n")
	}
	return []byte(b.String())
}

// textSumProblems implements lint rule (e).
func textSumProblems(reg map[string]*Requirement, sums map[string]string) []string {
	var problems []string
	for _, id := range sortedIDs(reg) {
		rec := reg[id]
		want, ok := sums[id]
		if rec.Status == "draft" {
			if ok {
				problems = append(problems, fmt.Sprintf(
					"rule (e): draft requirement %s has a locked-text sum (drafts are not locked; remove the stale entry)", id))
			}
			continue
		}
		if !ok {
			problems = append(problems, fmt.Sprintf(
				"rule (e): %s requirement %s has no locked-text sum in %s (lock records it; `req seal` baselines pre-existing locks)",
				rec.Status, id, textSumFile))
			continue
		}
		if got := textSum(rec); got != want {
			problems = append(problems, fmt.Sprintf(
				"rule (e): %s: text of %s requirement %s changed since it was locked (recorded %s, now %s); locked text changes only via `req supersede`",
				rec.file, rec.Status, id, want, got))
		}
	}
	for id := range sums {
		if _, ok := reg[id]; !ok {
			problems = append(problems, fmt.Sprintf("rule (e): %s has a sum for %s, which is not in the registry", textSumFile, id))
		}
	}
	return problems
}

// ---------- req seal ----------

// cmdSeal records locked-text sums for every non-draft requirement that has
// none. Existing sums are never rewritten: a mismatch is reported and the
// command fails without writing, so seal cannot hide an in-place edit.
func cmdSeal(args []string) error {
	f := parseFlags(args)
	c, err := f.ctx()
	if err != nil {
		return err
	}
	reg, err := loadRegistry(c.reqDir)
	if err != nil {
		return err
	}
	sums, err := loadTextSums(c.reqDir)
	if err != nil {
		return err
	}
	var added []string
	for _, id := range sortedIDs(reg) {
		rec := reg[id]
		if rec.Status == "draft" {
			continue
		}
		if want, ok := sums[id]; ok {
			if got := textSum(rec); got != want {
				return fmt.Errorf("%s: text changed since it was sealed (recorded %s, now %s); refusing to reseal", id, want, got)
			}
			continue
		}
		sums[id] = textSum(rec)
		added = append(added, id)
	}
	if len(added) == 0 {
		fmt.Println("seal: nothing to do (every non-draft requirement already has a sum)")
		return nil
	}
	if err := os.WriteFile(filepath.Join(c.reqDir, textSumFile), renderTextSums(sums), 0644); err != nil {
		return err
	}
	fmt.Printf("sealed %d requirement(s): %s\n", len(added), strings.Join(added, " "))
	return nil
}
