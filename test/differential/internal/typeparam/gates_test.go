package typeparam

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"diffharness/internal/harness"
)

// G4: a negative (errorcheck) test passes only when graalgo REJECTS the
// program with a diagnostic, never when it crashes.
func TestCheckErrorcheck(t *testing.T) {
	rejected := harness.Outcome{BuildOK: false, BuildLog: "x.go:3:2: undefined: y"}
	for name, c := range map[string]struct {
		alt   harness.Outcome
		fails bool
	}{
		"diagnostic":   {rejected, false},
		"built ok":     {harness.Outcome{BuildOK: true}, true},
		"no output":    {harness.Outcome{BuildOK: false}, true},
		"jvm crash":    {harness.Outcome{BuildOK: false, BuildLog: "Exception in thread \"main\" java.lang.NullPointerException\n\tat graalgo.X"}, true},
		"internal err": {harness.Outcome{BuildOK: false, BuildLog: "graalgo-run: internal error building AST: boom"}, true},
	} {
		var msgs []string
		checkErrorcheck(rejected, c.alt, func(f string, a ...any) { msgs = append(msgs, f) })
		if (len(msgs) > 0) != c.fails {
			t.Errorf("%s: fails=%v want %v (%v)", name, len(msgs) > 0, c.fails, msgs)
		}
	}
}

// G3: a batch JVM that returns the check result but no run result must
// make the comparison INFRA — not a zero Outcome (exit 0, no output) that
// "matches" a silent program.
func TestBatchMissingRunResultIsInfra(t *testing.T) {
	if _, err := os.Stat(filepath.Join(os.Getenv("HOME"), "tools/go/bin/go")); err != nil {
		t.Skip("no reference toolchain at ~/tools/go")
	}
	dir := t.TempDir()
	src := filepath.Join(dir, "silent.go")
	if err := os.WriteFile(src, []byte("// run\n\npackage main\n\nfunc main() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Fake batch launcher: answers only the first ("check") line.
	fake := filepath.Join(dir, "fake-run")
	script := `#!/bin/sh
f=${1#--batch=}
src=$(head -1 "$f" | cut -d: -f2-)
printf '[{"file":"%s","exit":0,"stdout":"","stderr":""}]' "$src"
`
	if err := os.WriteFile(fake, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	goRoot := filepath.Join(os.Getenv("HOME"), "tools/go")
	cfg := &harness.Config{TimeoutSec: 60, Backends: []harness.Backend{
		{Name: "gc-a", Vars: map[string]string{"GO": goRoot}, Build: []string{"{GO}/bin/go", "build", "-o", "{BIN}", "{SRC}"}, Run: []string{"{BIN}"},
			Env: map[string]string{"GOTOOLCHAIN": "local", "GO111MODULE": "off"}},
		{Name: "graalgo", Vars: map[string]string{"GRAALGO_RUN": fake}, Build: []string{"{GRAALGO_RUN}", "--check", "{SRC}"}, Run: []string{"{GRAALGO_RUN}", "{SRC}"}, Batch: true},
	}}
	files := []TFile{{Name: "silent.go", Class: ClassRun, Srcs: []string{src}}}
	comps, err := Run(cfg, files, "gc-a", "graalgo", 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(comps) != 1 || !comps[0].Infra || comps[0].Same {
		t.Fatalf("missing run result must be Infra, got %+v", comps)
	}
	if !strings.Contains(strings.Join(comps[0].Reasons, " "), "no graalgo batch run result") {
		t.Errorf("reason: %v", comps[0].Reasons)
	}
}
