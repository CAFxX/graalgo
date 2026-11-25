// Copyright 2023 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Random numbers: the generators upstream's rand.go keeps reachable by
// //go:linkname for code outside the standard library ("hall of shame"
// pulls of runtime.fastrand, runtime.cheaprand, ...; go.dev/issue/67401).
// Their bodies are upstream's, over the runtime's generator.

package runtime

import _ "unsafe" // for go:linkname

// rand returns a pseudo-random uint64 (upstream: the per-M ChaCha8 state;
// here an intrinsic, registry.txt runtime.rand).
func rand() uint64

// randn is like rand() % n but faster (registry.txt runtime.randn).
func randn(n uint32) uint32

//go:linkname legacy_fastrand runtime.fastrand
func legacy_fastrand() uint32 {
	return uint32(rand())
}

//go:linkname legacy_fastrandn runtime.fastrandn
func legacy_fastrandn(n uint32) uint32 {
	return randn(n)
}

//go:linkname legacy_fastrand64 runtime.fastrand64
func legacy_fastrand64() uint64 {
	return rand()
}

// cheaprand is a non-cryptographic-quality 32-bit random generator.
// Upstream's is a per-M wyrand state; graalgo has no M, and the runtime's
// generator is as fast and better, so the cheap generators draw from it.
func cheaprand() uint32 {
	return uint32(rand())
}

// cheaprand64 is a non-cryptographic-quality 63-bit random generator.
func cheaprand64() int64 {
	return int64(rand() &^ (uint64(1) << 63))
}

// cheaprandn is like cheaprand() % n but faster.
func cheaprandn(n uint32) uint32 {
	return uint32((uint64(cheaprand()) * uint64(n)) >> 32)
}
