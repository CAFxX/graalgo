package harness

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadConfig(t *testing.T) {
	cfg, err := LoadConfig("../../backends.json")
	if err != nil {
		t.Fatalf("load backends.json: %v", err)
	}
	if len(cfg.Backends) != 2 {
		t.Fatalf("want 2 backends, got %d", len(cfg.Backends))
	}
	if cfg.Backends[0].Name != "gc-a" || cfg.Backends[1].Name != "gc-b" {
		t.Fatalf("unexpected backend names: %+v", cfg.Backends)
	}
	if cfg.TimeoutSec != 15 {
		t.Fatalf("want timeout 15, got %d", cfg.TimeoutSec)
	}
	if _, ok := cfg.Excluded("01_hello"); ok {
		t.Fatal("01_hello should not be excluded")
	}
}

func TestLoadConfig_MissingFile(t *testing.T) {
	if _, err := LoadConfig("no-such-file.json"); err == nil {
		t.Fatal("expected error for missing file")
	}
}

func TestLoadConfig_DuplicateBackend(t *testing.T) {
	path := filepath.Join(t.TempDir(), "dup.json")
	os.WriteFile(path, []byte(`{"backends":[
		{"name":"x","build":["go","build"],"run":["x"]},
		{"name":"x","build":["go","build"],"run":["x"]}]}`), 0o644)
	if _, err := LoadConfig(path); err == nil {
		t.Fatal("expected error for duplicate backend names")
	}
}

func TestLoadConfig_DefaultTimeout(t *testing.T) {
	path := filepath.Join(t.TempDir(), "t.json")
	os.WriteFile(path, []byte(`{"backends":[{"name":"x","build":["go"],"run":["x"]}]}`), 0o644)
	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.TimeoutSec <= 0 {
		t.Fatalf("expected positive default timeout, got %d", cfg.TimeoutSec)
	}
}

func TestRenderBuild_SubstitutesSrcBin(t *testing.T) {
	b := Backend{
		Name:  "t",
		Vars:  map[string]string{"GO": "/x"},
		Build: []string{"{GO}/bin/go", "build", "-o", "{BIN}", "{SRC}"},
		Run:   []string{"{BIN}", "{SRC}"},
	}
	got := b.RenderBuild("/s/main.go", "/b/prog")
	want := []string{"/x/bin/go", "build", "-o", "/b/prog", "/s/main.go"}
	if len(got) != len(want) {
		t.Fatalf("got %v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("token %d: got %q want %q", i, got[i], want[i])
		}
	}
}

func TestDiscoverCorpus(t *testing.T) {
	progs, err := DiscoverCorpus("../../corpus")
	if err != nil {
		t.Fatal(err)
	}
	if len(progs) < 40 {
		t.Fatalf("want >=40 corpus programs, got %d", len(progs))
	}
	// sorted order check
	for i := 1; i < len(progs); i++ {
		if progs[i-1].Name >= progs[i].Name {
			t.Fatalf("corpus not sorted: %s before %s", progs[i-1].Name, progs[i].Name)
		}
	}
	found := false
	for _, p := range progs {
		if p.Name == "01_hello" {
			found = true
		}
	}
	if !found {
		t.Fatal("01_hello not discovered")
	}
}

func TestApplyExclusions(t *testing.T) {
	progs := []Program{{Name: "a"}, {Name: "b"}}
	cfg := &Config{Exclusions: []Exclusion{{Program: "b", Reason: "nondeterministic-output: test"}}}
	kept, skipped := ApplyExclusions(progs, cfg)
	if len(kept) != 1 || kept[0].Name != "a" {
		t.Fatalf("kept = %+v", kept)
	}
	if len(skipped) != 1 || skipped[0].Reason == "" {
		t.Fatalf("skipped = %+v", skipped)
	}
}
