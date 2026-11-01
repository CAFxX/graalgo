// P3-M9 (tier-1a, wave 3): synthetic `iter` package.
//
// Moved here verbatim from synth_reflect.go (review R7, 2026-11-01):
// the synthetic reflect package retired to the disk importer, but the
// minimal iter package is still live (registered as "iter" in
// synth_machinery.go and imported by slices/maps and by user programs
// importing iter). It keeps its own file so the retired reflect source
// can be deleted without taking iter with it. The content below is
// byte-identical to the moved block; only this header changed.
//
// REAL Go source, embedded as text and type-checked on demand with the
// overlay importer. The resulting *types.Package is what
// overlayImporter.Import("iter") returns; the exporter emits the
// synthetic declarations' bodies into the same wire document via
// exportSyntheticPackages.
package main

// P3-M8 (wave 2): minimal synthetic iter package. Only the two generic
// sequence type declarations, so the exact gc reflect signatures that
// mention iter.Seq/iter.Seq2 type-check. The iter methods are fail-fast
// (re-test: iter package bring-up).
const syntheticIterSource = `package iter

type Seq[V any] func(yield func(V) bool)

type Seq2[K, V any] func(yield func(K, V) bool)

// P3-M9 (tier-1a, wave 3): Pull/Pull2 added. The stdlib implements these
// with runtime-internal coroutines (runtime.newcoro/coroswitch via
// go:linkname), which v1 cannot import. This adapter collects the sequence
// eagerly into a slice and serves next() from an index -- correct for all
// finite sequences; observable next/stop semantics are preserved.
func Pull[V any](seq Seq[V]) (next func() (V, bool), stop func()) {
	var values []V
	seq(func(v V) bool {
		values = append(values, v)
		return true
	})
	index := 0
	stopped := false
	next = func() (V, bool) {
		if stopped || index >= len(values) {
			var zero V
			return zero, false
		}
		v := values[index]
		index++
		return v, true
	}
	stop = func() {
		stopped = true
	}
	return next, stop
}

func Pull2[K, V any](seq Seq2[K, V]) (next func() (K, V, bool), stop func()) {
	type pair struct {
		k K
		v V
	}
	var pairs []pair
	seq(func(k K, v V) bool {
		pairs = append(pairs, pair{k, v})
		return true
	})
	index := 0
	stopped := false
	next = func() (K, V, bool) {
		if stopped || index >= len(pairs) {
			var zk K
			var zv V
			return zk, zv, false
		}
		p := pairs[index]
		index++
		return p.k, p.v, true
	}
	stop = func() {
		stopped = true
	}
	return next, stop
}
`
