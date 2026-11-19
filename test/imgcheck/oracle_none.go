//go:build !imgcheck_oracle

package main

// oracle is oracle_gc.go's reflect cross-check, in the gc leg only: package
// reflect does not run on the node package before W9b.
func oracle(w *walker) {}
