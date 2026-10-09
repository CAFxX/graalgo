// P3-M9 tier-5: synthetic `crypto/internal/fips140cache` stub.
// The real package memoizes derived keys with weak pointers (needs
// runtime.AddCleanup, not in frontend/v1 scope). This stub drops
// memoization: Get always invokes new. Behavior-identical for
// callers (the cache is a pure optimization).
// DEVIATION (documented): Get takes k K, not k *K — the two ed25519
// call sites are rewritten from &priv[0] to priv[0] (frontend/v1 has
// no & on index expressions). Synthetic-only.
package fips140cache

// Cache is a no-memoization key cache.
type Cache[K, V any] struct{}

// Get returns new() without memoizing. The check function is ignored.
func (c *Cache[K, V]) Get(k K, new func() (*V, error), check func(*V) bool) (*V, error) {
	return new()
}
