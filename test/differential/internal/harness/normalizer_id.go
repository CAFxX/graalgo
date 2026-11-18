package harness

import (
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"sort"
	"strings"
)

//go:embed normalize.go
var normalizeSource string

// NormalizerID identifies the normalization rule set by CONTENT (global
// review G9): the SHA-256 of normalize.go plus the canonical NoiseSets.
// It used to be a hand-bumped constant, so a rule change that forgot the
// bump silently compared new graalgo output against old-rule snapshots.
// Any edit to the rules changes the ID, and a snapshot recorded under a
// different ID fails loudly until re-recorded (`make snapshot`).
func NormalizerID() string {
	h := sha256.New()
	h.Write([]byte(normalizeSource))
	names := make([]string, 0, len(NoiseSets))
	for n := range NoiseSets {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		h.Write([]byte("\x00noiseSet:" + n + "\x00" + strings.Join(NoiseSets[n], "\x00")))
	}
	return "harness.Normalize/sha256:" + hex.EncodeToString(h.Sum(nil))[:16]
}
