// P3-M9 tier-5: synthetic `crypto/internal/rand` stub.
// Adapted from go1.27.1 crypto/internal/rand/rand.go:
//   - internal/godebug dropped (cryptocustomrand pinned to ""; custom
//     rand readers stay ignored, exactly like gc's default).
//   - boring branch dropped (boring.Enabled is false).
//   - drbg replaced by the synthetic crypto/internal/fips140/drbg
//     (reads from graalgo.os.getrandom).
//   - randutil.MaybeReadByte inlined (only used when
//     cryptocustomrand=1, which is pinned off; kept as no-op for
//     shape fidelity).
// Synthetic-only.
package rand

import (
	"crypto/internal/fips140/drbg"
	"io"
)

type defaultReader = drbg.DefaultReader

type reader struct{ defaultReader }

func (r reader) Read(b []byte) (n int, err error) {
	drbg.Read(b)
	return len(b), nil
}

// Reader is an io.Reader that returns cryptographically secure random
// bytes. See crypto/internal/rand in go1.27.1.
var Reader io.Reader = reader{}

// IsDefaultReader reports whether r is the default crypto/rand.Reader.
func IsDefaultReader(r io.Reader) bool {
	return drbg.IsDefaultReader(r)
}

// CustomReader returns Reader (GODEBUG cryptocustomrand is pinned to
// "", so the provided reader is ignored, exactly like gc's default).
func CustomReader(r io.Reader) io.Reader {
	return Reader
}
