// Copyright 2012 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

//go:build race

package runtime

import (
	"internal/abi"
	"unsafe"
)

// The race-detector hooks of internal/race, which upstream's runtime
// provides under these //go:linkname names. graalgo has no detector
// (docs/arch/toolexec-design.md §2.6); a build with -race and
// GRAALGO_RACE=ignore gets these no-ops, exactly as internal/race's
// norace.go would give a build without -race.

//go:linkname race_Read internal/race.Read
func race_Read(addr unsafe.Pointer) {}

//go:linkname race_Write internal/race.Write
func race_Write(addr unsafe.Pointer) {}

//go:linkname race_ReadRange internal/race.ReadRange
func race_ReadRange(addr unsafe.Pointer, len int) {}

//go:linkname race_WriteRange internal/race.WriteRange
func race_WriteRange(addr unsafe.Pointer, len int) {}

//go:linkname race_Errors internal/race.Errors
func race_Errors() int { return 0 }

//go:linkname race_Acquire internal/race.Acquire
func race_Acquire(addr unsafe.Pointer) {}

//go:linkname race_Release internal/race.Release
func race_Release(addr unsafe.Pointer) {}

//go:linkname race_ReleaseMerge internal/race.ReleaseMerge
func race_ReleaseMerge(addr unsafe.Pointer) {}

//go:linkname race_Disable internal/race.Disable
func race_Disable() {}

//go:linkname race_Enable internal/race.Enable
func race_Enable() {}

//go:linkname race_ReadObjectPC internal/race.ReadObjectPC
func race_ReadObjectPC(t *abi.Type, addr unsafe.Pointer, callerpc, pc uintptr) {}

//go:linkname race_WriteObjectPC internal/race.WriteObjectPC
func race_WriteObjectPC(t *abi.Type, addr unsafe.Pointer, callerpc, pc uintptr) {}

//go:linkname race_ReadPC internal/race.ReadPC
func race_ReadPC(addr unsafe.Pointer, callerpc, pc uintptr) {}

//go:linkname race_WritePC internal/race.WritePC
func race_WritePC(addr unsafe.Pointer, callerpc, pc uintptr) {}
