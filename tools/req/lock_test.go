package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// ---------- req lock ----------

func lockFixture(t *testing.T) (root, reqDir string) {
	t.Helper()
	return stdLayout(t, map[string]string{
		"requirements/GG-0001.yaml": reqFile("GG-0001", "draft", "", ""),
		"requirements/GG-0002.yaml": reqFile("GG-0002", "draft", "", ""),
	})
}

func TestCmdLock_Single(t *testing.T) {
	root, reqDir := lockFixture(t)
	if err := cmdLock(append(ctxFlags(root, reqDir, root), "GG-0001")); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(reqDir, "GG-0001.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "status: locked") {
		t.Fatalf("GG-0001 not locked:\n%s", raw)
	}
	if !strings.Contains(string(raw), today()+": locked") {
		t.Fatalf("lock history line missing:\n%s", raw)
	}
	// The other draft is untouched.
	raw2, _ := os.ReadFile(filepath.Join(reqDir, "GG-0002.yaml"))
	if !strings.Contains(string(raw2), "status: draft") {
		t.Fatalf("GG-0002 should still be draft:\n%s", raw2)
	}
}

func TestCmdLock_All(t *testing.T) {
	root, reqDir := lockFixture(t)
	if err := cmdLock(append(ctxFlags(root, reqDir, root), "--all")); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"GG-0001", "GG-0002"} {
		raw, _ := os.ReadFile(filepath.Join(reqDir, id+".yaml"))
		if !strings.Contains(string(raw), "status: locked") {
			t.Fatalf("%s not locked:\n%s", id, raw)
		}
	}
}

func TestCmdLock_RelockError(t *testing.T) {
	root, reqDir := lockFixture(t)
	args := append(ctxFlags(root, reqDir, root), "GG-0001")
	if err := cmdLock(args); err != nil {
		t.Fatal(err)
	}
	if err := cmdLock(args); err == nil {
		t.Fatal("expected relock error, got nil")
	} else {
		requireErrContains(t, err, "already locked")
	}
}

func TestCmdLock_Atomicity(t *testing.T) {
	// GG-0002 is already locked: locking --all must fail and leave GG-0001 draft.
	root, reqDir := stdLayout(t, map[string]string{
		"requirements/GG-0001.yaml": reqFile("GG-0001", "draft", "", ""),
		"requirements/GG-0002.yaml": reqFile("GG-0002", "locked", "", ""),
	})
	if err := cmdLock(append(ctxFlags(root, reqDir, root), "--all")); err == nil {
		t.Fatal("expected lock error, got nil")
	}
	raw, _ := os.ReadFile(filepath.Join(reqDir, "GG-0001.yaml"))
	if !strings.Contains(string(raw), "status: draft") {
		t.Fatalf("GG-0001 must remain draft after failed --all:\n%s", raw)
	}
}

func TestCmdLock_UnknownID(t *testing.T) {
	root, reqDir := lockFixture(t)
	err := cmdLock(append(ctxFlags(root, reqDir, root), "GG-9999"))
	if err == nil {
		t.Fatal("expected unknown-id error, got nil")
	}
	requireErrContains(t, err, "unknown requirement id")
}

func TestCmdLock_NonDraftError(t *testing.T) {
	root, reqDir := stdLayout(t, map[string]string{
		"requirements/GG-0001.yaml": reqFile("GG-0001", "active", "", ""),
	})
	err := cmdLock(append(ctxFlags(root, reqDir, root), "GG-0001"))
	if err == nil {
		t.Fatal("expected non-draft error, got nil")
	}
	requireErrContains(t, err, "not draft")
}

func TestCmdLint_AcceptsLocked(t *testing.T) {
	// A locked requirement is valid and exempt from rule (c) until activated.
	root, reqDir := stdLayout(t, map[string]string{
		"requirements/GG-0001.yaml": reqFile("GG-0001", "locked", "", ""),
	})
	if err := cmdLint(ctxFlags(root, reqDir, root)); err != nil {
		t.Fatalf("lint rejected locked requirement: %v", err)
	}
}

func TestCmdActivate_Happy(t *testing.T) {
	root, reqDir := stdLayout(t, map[string]string{
		"requirements/GG-0001.yaml": reqFile("GG-0001", "locked", "", ""),
	})
	if err := cmdActivate(append(ctxFlags(root, reqDir, root), "GG-0001")); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(filepath.Join(reqDir, "GG-0001.yaml"))
	if !strings.Contains(string(raw), "status: active") {
		t.Fatalf("GG-0001 not active:\n%s", raw)
	}
	if !strings.Contains(string(raw), "activated (implementation started)") {
		t.Fatalf("activation history line missing:\n%s", raw)
	}
}

func TestCmdActivate_DraftError(t *testing.T) {
	root, reqDir := stdLayout(t, map[string]string{
		"requirements/GG-0001.yaml": reqFile("GG-0001", "draft", "", ""),
	})
	err := cmdActivate(append(ctxFlags(root, reqDir, root), "GG-0001"))
	if err == nil {
		t.Fatal("expected not-locked error, got nil")
	}
	requireErrContains(t, err, "not locked")
	// atomicity: untouched
	raw, _ := os.ReadFile(filepath.Join(reqDir, "GG-0001.yaml"))
	if !strings.Contains(string(raw), "status: draft") {
		t.Fatalf("GG-0001 must remain draft:\n%s", raw)
	}
}

func TestCmdActivate_AlreadyActiveError(t *testing.T) {
	root, reqDir := stdLayout(t, map[string]string{
		"requirements/GG-0001.yaml": reqFile("GG-0001", "active", "", ""),
	})
	err := cmdActivate(append(ctxFlags(root, reqDir, root), "GG-0001"))
	if err == nil {
		t.Fatal("expected already-active error, got nil")
	}
	requireErrContains(t, err, "already active")
}
