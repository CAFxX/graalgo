// Copyright 2013 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package race is graalgo's replacement of upstream's runtime/race, which
// links the ThreadSanitizer runtime into -race builds. graalgo has no race
// detector (docs/arch/toolexec-design.md §2.6): the link refuses -race
// unless GRAALGO_RACE=ignore is set, and then this empty package is what
// cmd/go's implicit runtime/race dependency resolves to. The hooks
// internal/race declares under the race build tag are no-ops provided by
// the facade runtime (runtime/race.go).
package race
