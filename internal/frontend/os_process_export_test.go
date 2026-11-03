package main

import (
	"os"
	"path/filepath"
	"testing"
)

// TestSyntheticOsProcessFamily is the REVIEW-2026-11-03-16 S8
// regression test: os.Executable and the whole os.Process family
// (StartProcess/FindProcess/Wait/Kill/Signal/Release, ProcessState)
// panicked at runtime as M8 scope-limits. They are now implemented
// (Executable via the launcher argv[0]; the Process family via the
// graalgo.os.spawn/proc* intrinsics on ProcessBuilder). This test
// pins export of a program exercising every member; execution-level
// assertions (spawn echo, wait exit code, kill, env inheritance,
// Executable == argv[0]) live in the lane's sync-run verification.
func TestSyntheticOsProcessFamily(t *testing.T) {
	src := `package main

import (
	"os"
)

func main() {
	exe, err := os.Executable()
	if err != nil {
		println("exe err")
	}
	println(exe)
	proc, err := os.StartProcess("/bin/echo", []string{"echo", "hi"}, &os.ProcAttr{
		Files: []*os.File{os.Stdin, os.Stdout, os.Stderr},
	})
	if err != nil {
		println("start err")
	}
	println(proc.Pid)
	state, err := proc.Wait()
	if err != nil {
		println("wait err")
	}
	println(state.ExitCode())
	println(state.Success())
	println(state.String())
	found, err := os.FindProcess(proc.Pid)
	if err != nil {
		println("find err")
	}
	println(found.Pid)
	if err := found.Release(); err != nil {
		println("release err")
	}
	proc2, err := os.StartProcess("/bin/sleep", []string{"sleep", "30"}, &os.ProcAttr{
		Files: []*os.File{nil, nil, nil},
	})
	if err != nil {
		println("start2 err")
	}
	if err := proc2.Kill(); err != nil {
		println("kill err")
	}
	state2, err := proc2.Wait()
	if err != nil {
		println("wait2 err")
	}
	println(state2.Exited())
	println(state2.ExitCode())
}
`
	dir := t.TempDir()
	p := filepath.Join(dir, "synosproc.go")
	if err := os.WriteFile(p, []byte(src), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := ExportFile(p); err != nil {
		t.Fatalf("ExportFile: %v", err)
	}
}
