package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// ---------- lint rule (e): locked-text integrity ----------

// rewordStatement rewrites the statement line of a fixture YAML in place,
// the hand edit rule (e) exists to catch.
func rewordStatement(t *testing.T, reqDir, id string) {
	t.Helper()
	p := filepath.Join(reqDir, id+".yaml")
	old := readFile(t, p)
	nu := strings.Replace(old, "statement: Statement "+id+".", "statement: Reworded "+id+".", 1)
	if nu == old {
		t.Fatalf("fixture %s has no statement line to reword", id)
	}
	writeFile(t, p, nu)
}

func TestLint_RuleE_LockedTextEdited(t *testing.T) {
	root, reqDir := lintCleanFixture(t)
	rewordStatement(t, reqDir, "GG-0001")
	out, err := runLint(t, root, reqDir)
	requireLintProblem(t, out, err, "rule (e)")
	if !strings.Contains(out, "text of active requirement GG-0001 changed since it was locked") {
		t.Fatalf("missing rule (e) report: %q", out)
	}
}

func TestLint_RuleE_EveryHashedFieldCounts(t *testing.T) {
	for _, field := range []string{"title", "statement", "rationale", "parent", "verification_plan"} {
		t.Run(field, func(t *testing.T) {
			root, reqDir := stdLayout(t, map[string]string{
				"requirements/GG-0001.yaml": reqFile("GG-0001", "locked", "", ""),
				"requirements/GG-0002.yaml": reqFile("GG-0002", "locked", "", ""),
			})
			p := filepath.Join(reqDir, "GG-0001.yaml")
			lines := strings.Split(readFile(t, p), "\n")
			hit := false
			for i, l := range lines {
				if strings.HasPrefix(l, field+":") {
					if field == "parent" {
						lines[i] = "parent: GG-0002"
					} else {
						lines[i] = l + " (edited)"
					}
					hit = true
				}
			}
			if !hit {
				t.Fatalf("fixture has no %s line", field)
			}
			writeFile(t, p, strings.Join(lines, "\n"))
			out, err := runLint(t, root, reqDir)
			requireLintProblem(t, out, err, "rule (e)")
		})
	}
}

func TestLint_RuleE_LifecycleFieldsNotHashed(t *testing.T) {
	// Status, superseded_by and history change legitimately after lock.
	root, reqDir := stdLayout(t, map[string]string{
		"requirements/GG-0001.yaml": reqFile("GG-0001", "locked", "", ""),
		"requirements/GG-0002.yaml": reqFile("GG-0002", "locked", "", ""),
	})
	writeFile(t, filepath.Join(reqDir, "GG-0001.yaml"),
		reqFile("GG-0001", "superseded", "", "GG-0002", "2026-11-01: locked", "2026-11-02: superseded by GG-0002"))
	out, err := runLint(t, root, reqDir)
	if err != nil {
		t.Fatalf("lifecycle edits must not trip rule (e): %v\n%s", err, out)
	}
}

func TestLint_RuleE_MissingSum(t *testing.T) {
	root, reqDir := stdLayout(t, map[string]string{
		"requirements/GG-0001.yaml":   reqFile("GG-0001", "locked", "", ""),
		"requirements/" + textSumFile: textSumHeader,
	})
	out, err := runLint(t, root, reqDir)
	requireLintProblem(t, out, err, "rule (e): locked requirement GG-0001 has no locked-text sum")
}

func TestLint_RuleE_NoManifestAtAll(t *testing.T) {
	// Deleting the manifest must not disable the check.
	root, reqDir := stdLayout(t, map[string]string{
		"requirements/GG-0001.yaml": reqFile("GG-0001", "locked", "", ""),
	})
	if err := os.Remove(filepath.Join(reqDir, textSumFile)); err != nil {
		t.Fatal(err)
	}
	out, err := runLint(t, root, reqDir)
	requireLintProblem(t, out, err, "has no locked-text sum")
}

func TestLint_RuleE_DraftEditable(t *testing.T) {
	root, reqDir := stdLayout(t, map[string]string{
		"requirements/GG-0001.yaml": reqFile("GG-0001", "draft", "", ""),
	})
	rewordStatement(t, reqDir, "GG-0001")
	out, err := runLint(t, root, reqDir)
	if err != nil {
		t.Fatalf("drafts are editable in place: %v\n%s", err, out)
	}
}

func TestLint_RuleE_StaleAndUnknownEntries(t *testing.T) {
	draft := &Requirement{ID: "GG-0001", Title: "Title GG-0001"}
	root, reqDir := stdLayout(t, map[string]string{
		"requirements/GG-0001.yaml": reqFile("GG-0001", "draft", "", ""),
		"requirements/" + textSumFile: string(renderTextSums(map[string]string{
			"GG-0001": textSum(draft), "GG-0099": textSum(draft)})),
	})
	out, err := runLint(t, root, reqDir)
	requireLintProblem(t, out, err, "draft requirement GG-0001 has a locked-text sum")
	requireLintProblem(t, out, err, "has a sum for GG-0099, which is not in the registry")
}

func TestLint_RuleE_MalformedManifest(t *testing.T) {
	root, reqDir := stdLayout(t, map[string]string{
		"requirements/GG-0001.yaml":   reqFile("GG-0001", "draft", "", ""),
		"requirements/" + textSumFile: "GG-0001 deadbeef\n",
	})
	err := cmdLint(ctxFlags(root, reqDir, root))
	requireErrContains(t, err, "malformed line")
}

func TestTextSum_FieldBoundaries(t *testing.T) {
	a := &Requirement{ID: "GG-0001", Title: "ab", Statement: "c"}
	b := &Requirement{ID: "GG-0001", Title: "a", Statement: "bc"}
	if textSum(a) == textSum(b) {
		t.Fatal("moving text across a field boundary must change the sum")
	}
}

func TestCmdLock_RecordsSum(t *testing.T) {
	root, reqDir := stdLayout(t, map[string]string{
		"requirements/GG-0001.yaml": reqFile("GG-0001", "draft", "", ""),
	})
	if err := cmdLock(append(ctxFlags(root, reqDir, root), "GG-0001")); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(readFile(t, filepath.Join(reqDir, textSumFile)), "GG-0001 sha256:") {
		t.Fatal("lock did not record the locked-text sum")
	}
	if out, err := runLint(t, root, reqDir); err != nil {
		t.Fatalf("freshly locked registry must lint: %v\n%s", err, out)
	}
	rewordStatement(t, reqDir, "GG-0001")
	out, err := runLint(t, root, reqDir)
	requireLintProblem(t, out, err, "rule (e)")
}

// ---------- req seal ----------

func TestCmdSeal_WritesFirstBaseline(t *testing.T) {
	root, reqDir := stdLayout(t, map[string]string{
		"requirements/GG-0001.yaml": reqFile("GG-0001", "locked", "", ""),
		"requirements/GG-0002.yaml": reqFile("GG-0002", "draft", "", ""),
	})
	if err := os.Remove(filepath.Join(reqDir, textSumFile)); err != nil {
		t.Fatal(err)
	}
	if err := cmdSeal(ctxFlags(root, reqDir, root)); err != nil {
		t.Fatal(err)
	}
	sums, err := loadTextSums(reqDir)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := sums["GG-0001"]; !ok {
		t.Fatal("seal did not record GG-0001")
	}
	if _, ok := sums["GG-0002"]; ok {
		t.Fatal("seal must not record a draft")
	}
	if out, err := runLint(t, root, reqDir); err != nil {
		t.Fatalf("sealed registry must lint: %v\n%s", err, out)
	}
}

func TestCmdSeal_IsOneShot(t *testing.T) {
	// A manifest exists (even one lacking GG-0002's sum): seal refuses and
	// writes nothing — a requirement that skipped `req lock` stays a lint
	// failure instead of being baselined after the fact.
	root, reqDir := stdLayout(t, map[string]string{
		"requirements/GG-0001.yaml":   reqFile("GG-0001", "locked", "", ""),
		"requirements/GG-0002.yaml":   reqFile("GG-0002", "locked", "", ""),
		"requirements/" + textSumFile: textSumHeader,
	})
	snap := snapshotTree(t, root)
	err := cmdSeal(ctxFlags(root, reqDir, root))
	requireErrContains(t, err, "seal is one-shot")
	assertTreeIdentical(t, root, snap)
}

func TestCmdSeal_RefusesToLaunderEdit(t *testing.T) {
	root, reqDir := stdLayout(t, map[string]string{
		"requirements/GG-0001.yaml": reqFile("GG-0001", "locked", "", ""),
	})
	rewordStatement(t, reqDir, "GG-0001")
	snap := snapshotTree(t, root)
	err := cmdSeal(ctxFlags(root, reqDir, root))
	requireErrContains(t, err, "seal is one-shot")
	assertTreeIdentical(t, root, snap)
	out, lerr := runLint(t, root, reqDir)
	requireLintProblem(t, out, lerr, "rule (e)")
}

func TestScan_ManifestIsNotAReference(t *testing.T) {
	root, reqDir := stdLayout(t, map[string]string{
		"requirements/GG-0001.yaml": reqFile("GG-0001", "locked", "", ""),
	})
	c := &workCtx{root: root, reqDir: reqDir, scanDir: root}
	refs, _, err := scanAll(c)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range refs {
		if strings.HasSuffix(r.File, textSumFile) {
			t.Fatalf("manifest line counted as a reference: %+v", r)
		}
	}
}
