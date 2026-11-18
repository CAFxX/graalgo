package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// gitRegistry builds a git repo whose master commit holds a sealed
// registry (GG-0001 locked, GG-0002 active, GG-0003 draft), then checks
// out a lane branch. It returns (root, reqDir).
func gitRegistry(t *testing.T) (string, string) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	t.Setenv("GATE_BASE", "")
	root, reqDir := stdLayout(t, map[string]string{
		"requirements/GG-0001.yaml": reqFile("GG-0001", "locked", "", ""),
		"requirements/GG-0002.yaml": reqFile("GG-0002", "active", "", ""),
		"requirements/GG-0003.yaml": reqFile("GG-0003", "draft", "", ""),
		"src/a.go":                  "// verifies: GG-0002\npackage a\n",
	})
	g := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", root}, args...)...)
		cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@example.invalid",
			"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@example.invalid")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	g("init", "-q", "-b", "master")
	g("add", "-A")
	g("commit", "-q", "-m", "base")
	g("checkout", "-q", "-b", "lane")
	return root, reqDir
}

func setStatus(t *testing.T, reqDir, id, from, to string) {
	t.Helper()
	p := filepath.Join(reqDir, id+".yaml")
	data, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	s := strings.Replace(string(data), "status: "+from+"\n", "status: "+to+"\n", 1)
	if err := os.WriteFile(p, []byte(s), 0644); err != nil {
		t.Fatal(err)
	}
}

func TestRuleF_UnchangedRegistryLints(t *testing.T) {
	root, reqDir := gitRegistry(t)
	if out, err := runLint(t, root, reqDir); err != nil {
		t.Fatalf("unchanged registry must lint: %v\n%s", err, out)
	}
}

func TestRuleF_LockedToDraftRegressionFails(t *testing.T) {
	root, reqDir := gitRegistry(t)
	// Flip GG-0001 back to draft and drop its sum: rule (e) alone is
	// satisfied (drafts carry no sum), so only rule (f) can catch it.
	setStatus(t, reqDir, "GG-0001", "locked", "draft")
	sums, err := loadTextSums(reqDir)
	if err != nil {
		t.Fatal(err)
	}
	delete(sums, "GG-0001")
	if err := os.WriteFile(filepath.Join(reqDir, textSumFile), renderTextSums(sums), 0644); err != nil {
		t.Fatal(err)
	}
	out, err := runLint(t, root, reqDir)
	requireLintProblem(t, out, err, "rule (f)")
	if !strings.Contains(out, "status regressed locked -> draft") || !strings.Contains(out, "line for GG-0001 removed") {
		t.Fatalf("want regression and removed-line problems, got:\n%s", out)
	}
	for _, line := range strings.Split(out, "\n") {
		if strings.Contains(line, "rule (e)") {
			t.Fatalf("control: rule (e) must not fire on this edit:\n%s", out)
		}
	}
}

func TestRuleF_ActiveToLockedRegressionFails(t *testing.T) {
	root, reqDir := gitRegistry(t)
	setStatus(t, reqDir, "GG-0002", "active", "locked")
	out, err := runLint(t, root, reqDir)
	requireLintProblem(t, out, err, "rule (f)")
}

func TestRuleF_ResealedTextFails(t *testing.T) {
	root, reqDir := gitRegistry(t)
	// Reword a locked statement AND rewrite its sum to match: rule (e) is
	// satisfied, rule (f) sees the changed line.
	rewordStatement(t, reqDir, "GG-0001")
	reg, err := loadRegistry(reqDir)
	if err != nil {
		t.Fatal(err)
	}
	sums, err := loadTextSums(reqDir)
	if err != nil {
		t.Fatal(err)
	}
	sums["GG-0001"] = textSum(reg["GG-0001"])
	if err := os.WriteFile(filepath.Join(reqDir, textSumFile), renderTextSums(sums), 0644); err != nil {
		t.Fatal(err)
	}
	out, err := runLint(t, root, reqDir)
	requireLintProblem(t, out, err, "rule (f)")
	if !strings.Contains(out, "line for GG-0001 changed") {
		t.Fatalf("want a changed-line problem, got:\n%s", out)
	}
}

func TestRuleF_DeletedRequirementFails(t *testing.T) {
	root, reqDir := gitRegistry(t)
	if err := os.Remove(filepath.Join(reqDir, "GG-0003.yaml")); err != nil {
		t.Fatal(err)
	}
	out, err := runLint(t, root, reqDir)
	requireLintProblem(t, out, err, "rule (f)")
}

func TestRuleF_AddedSumAndForwardMovePass(t *testing.T) {
	root, reqDir := gitRegistry(t)
	// draft -> locked with its sum added: the lifecycle moving forward.
	setStatus(t, reqDir, "GG-0003", "draft", "locked")
	reg, err := loadRegistry(reqDir)
	if err != nil {
		t.Fatal(err)
	}
	sums, err := loadTextSums(reqDir)
	if err != nil {
		t.Fatal(err)
	}
	sums["GG-0003"] = textSum(reg["GG-0003"])
	if err := os.WriteFile(filepath.Join(reqDir, textSumFile), renderTextSums(sums), 0644); err != nil {
		t.Fatal(err)
	}
	if out, err := runLint(t, root, reqDir); err != nil {
		t.Fatalf("forward move must lint: %v\n%s", err, out)
	}
}

func TestRuleF_GateBaseOverridesMergeBase(t *testing.T) {
	root, reqDir := gitRegistry(t)
	setStatus(t, reqDir, "GG-0002", "active", "locked")
	// Commit the regression on master itself: merge-base(HEAD, master) is
	// then HEAD and sees nothing; GATE_BASE (the push's before sha) does.
	before, err := git(root, "rev-parse", "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"checkout", "-q", "master"}, {"add", "-A"},
		{"-c", "user.name=t", "-c", "user.email=t@example.invalid", "commit", "-q", "-m", "regress"}} {
		if _, err := git(root, args...); err != nil {
			t.Fatal(err)
		}
	}
	if out, err := runLint(t, root, reqDir); err != nil {
		t.Fatalf("control: without GATE_BASE the committed regression is invisible, got: %v\n%s", err, out)
	}
	t.Setenv("GATE_BASE", before)
	out, err := runLint(t, root, reqDir)
	requireLintProblem(t, out, err, "rule (f)")
}
