package main

// P3-M8 (wave 2): synthetic `hmac` package.
// Software implementation only (no intrinsification).
// Rationale and retirement plan: docs/arch/bridge-inventory.md (B-4).
const syntheticHmacSource = `package hmac

// Copyright 2009 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

/*
Package hmac implements the Keyed-Hash Message Authentication Code (HMAC) as
defined in U.S. Federal Information Processing Standards Publication 198.
An HMAC is a cryptographic hash that uses a key to sign a message.
The receiver verifies the hash by recomputing it using the same key.

Receivers should be careful to use Equal to compare MACs in order to avoid
timing side-channels:
*/
import (
	"crypto/subtle"
	"errors"
	"hash"
)

// P3-M8: non-generic port of crypto/internal/fips140/hmac (which is generic
// over hash.Hash). The "marshaled" fast path and the slow path are preserved
// exactly; the marshalable interface uses the synthetic single-result
// MarshalSingle (served via the mrM8 desugar; see hash.__binaryMarshal).
// P3-M8: standard non-FIPS pure-Go path only (boring/fips140 trimmed;
// re-test trigger: FIPS mode).
// KAT: differential vs gc on (hmac × digest) vectors, incl. Reset-after-Sum.

// marshalable is the synthetic single-result form of the stdlib's
// encoding.BinaryMarshaler+BinaryUnmarshaler combination.
type marshalable interface {
	MarshalSingle() []byte
	UnmarshalBinary([]byte) error
}

type hmac struct {
	// opad and ipad may share underlying storage with hmac clones.
	opad, ipad   []byte
	outer, inner hash.Hash

	// If marshaled is true, then opad and ipad do not contain a padded
	// copy of the key, but rather the marshaled state of outer/inner after
	// opad/ipad has been fed into it.
	marshaled bool
}

// WriteSingle writes p to h, which must be a synthetic hash (all hashes
// produced by the h func passed to New are synthetic in P3-M8).
func WriteSingle(h hash.Hash, p []byte) {
	if w, ok := h.(interface{ WriteSingle([]byte) int }); ok {
		w.WriteSingle(p)
		return
	}
	panic("crypto/hmac: non-synthetic hash.Hash (needs WS D; re-test when it lands)")
}

func New(h func() hash.Hash, key []byte) hash.Hash {
	hm := &hmac{}
	hm.outer = h()
	hm.inner = h()
	// Key setup: hash long keys, pad to blocksize.
	bs := hm.inner.BlockSize()
	if len(key) > bs {
		kh := h()
		WriteSingle(kh, key)
		key = kh.Sum(nil)
	}
	hm.ipad = make([]byte, bs)
	hm.opad = make([]byte, bs)
	copy(hm.ipad, key)
	copy(hm.opad, key)
	for i := range hm.ipad {
		hm.ipad[i] ^= 0x36
	}
	for i := range hm.opad {
		hm.opad[i] ^= 0x5c
	}
	hm.Reset()
	return hm
}

func (h *hmac) Sum(in []byte) []byte {
	origLen := len(in)
	in = h.inner.Sum(in)

	if h.marshaled {
		if err := h.outer.(marshalable).UnmarshalBinary(h.opad); err != nil {
			panic(err)
		}
	} else {
		h.outer.Reset()
		WriteSingle(h.outer, h.opad)
	}
	WriteSingle(h.outer, in[origLen:])
	return h.outer.Sum(in[:origLen])
}

func (h *hmac) WriteSingle(p []byte) int {
	WriteSingle(h.inner, p)
	return len(p)
}

// Write implements hash.Hash (fail-fast stub; real logic is WriteSingle,
// served via the mrM8 desugar to hash.__hashWrite). Re-test trigger: WS D.
func (h *hmac) Write(p []byte) (nn int, err error) {
	panic("crypto/hmac: Write: multi-result call needs the mrM8 desugar (re-test: WS D)")
}

func (h *hmac) Size() int      { return h.outer.Size() }
func (h *hmac) BlockSize() int { return h.inner.BlockSize() }

func (h *hmac) Reset() {
	if h.marshaled {
		if err := h.inner.(marshalable).UnmarshalBinary(h.ipad); err != nil {
			panic(err)
		}
		return
	}

	h.inner.Reset()
	WriteSingle(h.inner, h.ipad)

	// If the underlying hash is marshalable, save the state for later.
	marshalableInner, innerOK := h.inner.(marshalable)
	if !innerOK {
		return
	}
	marshalableOuter, outerOK := h.outer.(marshalable)
	if !outerOK {
		return
	}

	imarshal := marshalableInner.MarshalSingle()

	h.outer.Reset()
	WriteSingle(h.outer, h.opad)
	omarshal := marshalableOuter.MarshalSingle()

	// Marshaling succeeded; save the marshaled state for later.
	h.ipad = imarshal
	h.opad = omarshal
	h.marshaled = true
}

func (h *hmac) CloneSinglePair() hash.ClonePair {
	// P3-M8b: &r is not expressible in frontend/v1 (WS D); &hmac{...}
	// on a composite literal is. inner/outer are cloned below; the
	// remaining fields copy by value (slices share storage with the
	// original, exactly as in the stdlib).
	ic, ok := h.inner.(interface{ CloneSinglePair() hash.ClonePair })
	if !ok {
		return hash.ClonePair{Err: errors.New("crypto/hmac: hash does not support hash.Cloner")}
	}
	oc, ok := h.outer.(interface{ CloneSinglePair() hash.ClonePair })
	if !ok {
		return hash.ClonePair{Err: errors.New("crypto/hmac: hash does not support hash.Cloner")}
	}
	ip := ic.CloneSinglePair()
	if ip.Err != nil {
		return hash.ClonePair{Err: ip.Err}
	}
	op := oc.CloneSinglePair()
	if op.Err != nil {
		return hash.ClonePair{Err: op.Err}
	}
	rinner := ip.C.(hash.Hash)
	router := op.C.(hash.Hash)
	// opad/ipad may share storage with the original (as in the stdlib).
	return hash.ClonePair{C: &hmac{opad: h.opad, ipad: h.ipad, outer: router, inner: rinner, marshaled: h.marshaled}}
}

// Clone implements hash.Cloner (fail-fast stub; real logic is CloneSinglePair,
// served via the mrM8 desugar to hash.__hashClone). Re-test trigger: WS D.
func (h *hmac) Clone() (hash.Cloner, error) {
	panic("crypto/hmac: Clone: multi-result call needs the mrM8 desugar (re-test: WS D)")
}

// Equal compares two MACs for equality without leaking timing information.
func Equal(mac1, mac2 []byte) bool {
	// We don't have to be constant time if the lengths of the MACs are
	// different as that suggests that a completely different hash function
	// was used.
	return subtle.ConstantTimeCompare(mac1, mac2) == 1
}
`
