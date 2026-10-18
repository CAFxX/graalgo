// 17_procattr: os.ProcAttr.Sys is *syscall.SysProcAttr (gc typing,
// fixed by the 2026-10-18 review-p0-os remediation) with gc's full
// field set. Compile-time surface check plus field round-trip — no
// process is spawned (StartProcess stays fail-fast in M8).
package main

import (
	"fmt"
	"os"
	"syscall"
)

func main() {
	pidfd := -1
	attr := &os.ProcAttr{
		Dir:   "/tmp",
		Env:   []string{"A=1"},
		Files: []*os.File{os.Stdin, os.Stdout, os.Stderr},
		Sys: &syscall.SysProcAttr{
			Setsid:       true,
			Pdeathsig:    syscall.SIGKILL,
			Cloneflags:   0x40000000, // CLONE_NEWUSER
			Unshareflags: 0x40000000,
			UidMappings: []syscall.SysProcIDMap{
				{ContainerID: 0, HostID: 1000, Size: 1},
			},
			GidMappings: []syscall.SysProcIDMap{
				{ContainerID: 0, HostID: 1000, Size: 1},
			},
			GidMappingsEnableSetgroups: false,
			AmbientCaps:                []uintptr{25},
			UseCgroupFD:                false,
			PidFD:                      &pidfd,
		},
	}
	// Field round-trip through the typed Sys pointer.
	if !attr.Sys.Setsid {
		fmt.Println("FAIL: Setsid")
		os.Exit(1)
	}
	if len(attr.Sys.UidMappings) != 1 || attr.Sys.UidMappings[0].HostID != 1000 {
		fmt.Println("FAIL: UidMappings")
		os.Exit(1)
	}
	if attr.Sys.UidMappings[0].Size != 1 || attr.Sys.GidMappings[0].ContainerID != 0 {
		fmt.Println("FAIL: mappings")
		os.Exit(1)
	}
	if len(attr.Sys.AmbientCaps) != 1 || attr.Sys.AmbientCaps[0] != 25 {
		fmt.Println("FAIL: AmbientCaps")
		os.Exit(1)
	}
	if attr.Sys.Pdeathsig != syscall.SIGKILL {
		fmt.Println("FAIL: Pdeathsig")
		os.Exit(1)
	}
	fmt.Println("ok")
}
