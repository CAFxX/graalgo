//go:build linux && amd64

package pkggate

//go:linkname selLocal runtime.nanotime
func selLocal() int64
