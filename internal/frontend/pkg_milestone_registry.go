package main

// multiResultSyntheticMilestones is the single source of truth for the
// fail-fast multi-result scope (review 2026-11-04, P1-02). It maps a
// synthetic package path to the milestone tag its out-of-scope
// multi-result calls fail fast with.
//
// It is consumed by exactly the two sites that previously duplicated this
// package list by hand and could drift apart silently:
//   - isMultiResultSynthetic (synth_machinery.go): membership in this map
//     is the package half of the predicate (the receiver-shape and
//     result-count checks still live there, unchanged);
//   - multiResultSyntheticError (synth_machinery.go): the map value is
//     the milestone tag in the scope error; a package absent from the
//     map keeps the old default tag "M5".
//
// The map is an exact transcription of the two pre-registry lists, so
// the refactor is behaviour-preserving by construction (see the
// equivalence note below). It was verified mechanically against those
// lists, key for key and value for value: the predicate listed 99
// packages, the error function's M7/M8/M9 blocks listed 18/21/20 of
// them (disjoint), and the remaining 40 fall through to the old "M5"
// default.
//
// Equivalence proof sketch (checked by pkg_milestone_registry_test.go):
//   - predicate: the old code returned false unless the package was in
//     its 99-name chain; the new code returns false unless the package
//     is a map key. Same 99 keys, same result.
//   - label: the old code applied the M7, then M8, then M9 blocks in
//     order, defaulting to "M5". The three blocks are disjoint and all
//     contained in the 99 keys, so for a key the map value equals the
//     block that would have fired, and for a non-key the old code also
//     produced "M5" (no block could fire outside the key set). The new
//     code returns the map value for keys and "M5" otherwise. Same
//     label for every package path.
//
// Two deliberate non-merges, so a future reader does not "finish" this
// refactor into a behaviour change:
//   - The export.go synthetic-import switch (~85 cases) is NOT folded
//     in. It is a different domain: an import-closure table (which
//     synthetic packages a user import pulls in, with dependency edges
//     and import order in the need-blocks), and its package set is
//     neither a subset nor a superset of this map (it adds import-only
//     packages such as flag, testing and container/heap, and omits
//     synthetic-only internal packages such as crypto/internal/fips140
//     and internal/poll that no user import names).
//   - The mrdesugar_m5.go per-function tables (m7/m8/os/m9/io) are NOT
//     folded in. They are keyed by (package, function, receiver) and
//     carry helper names, pair struct names and field lists that no
//     package-level map can hold; their package set also differs (they
//     add table-only packages such as bufio, math/big and net/http,
//     and omit fail-fast-only packages such as sort and hash). What
//     keeps those two sites honest is the cross-site drift test in
//     pkg_milestone_registry_test.go: a package added to the export
//     switch or to any desugar table without either a registry entry
//     or a documented allowlist entry fails the test.
//
// One stale comment corrected by this move, behaviour unchanged: the
// old isMultiResultSynthetic comment claimed the asymmetric crypto
// packages fail fast with "the named M7 error". The old code gave them
// the "M5" default (they appear in no M7/M8/M9 block), and the registry
// pins "M5" for them. The comment, not the behaviour, was wrong.
//
// No new package special cases are added here: every key below already
// appeared in the pre-registry predicate. Adding a package means adding
// one map entry (and updating the drift test's expectations), not
// touching three hand-written lists.
var multiResultSyntheticMilestones = map[string]string{
	// M5: the original milestone (time/context) plus every fail-fast
	// package the old error function reached only via its "M5" default:
	// the asymmetric crypto packages, the crypto/internal/fips140
	// family and the golang.org/x/crypto/cryptobyte packages.
	"time":    "M5",
	"context": "M5",
	// Asymmetric crypto (M5 by the old default, not M7).
	"crypto/elliptic": "M5",
	"crypto/ecdh":     "M5",
	"crypto/dsa":      "M5",
	"crypto/ecdsa":    "M5",
	"crypto/ed25519":  "M5",
	"crypto/rsa":      "M5",
	"crypto/hkdf":     "M5",
	"crypto/pbkdf2":   "M5",
	// Cryptobyte (third-party synthetic dependency of crypto/x509).
	"golang.org/x/crypto/cryptobyte":      "M5",
	"golang.org/x/crypto/cryptobyte/asn1": "M5",
	// crypto/internal/fips140 family (synthetic-only internal deps).
	"crypto/internal/fips140":                    "M5",
	"crypto/internal/fips140/check":              "M5",
	"crypto/internal/fips140/alias":              "M5",
	"crypto/internal/fips140only":                "M5",
	"crypto/internal/fips140cache":               "M5",
	"crypto/internal/fips140hash":                "M5",
	"crypto/internal/fips140/drbg":               "M5",
	"crypto/internal/rand":                       "M5",
	"crypto/internal/boring/bbig":                "M5",
	"crypto/internal/boring":                     "M5",
	"crypto/internal/constanttime":               "M5",
	"crypto/internal/fips140deps/byteorder":      "M5",
	"crypto/internal/fips140/nistec/fiat":        "M5",
	"crypto/internal/fips140/nistec":             "M5",
	"crypto/internal/fips140/edwards25519/field": "M5",
	"crypto/internal/fips140/edwards25519":       "M5",
	"crypto/internal/fips140/bigmod":             "M5",
	"crypto/internal/fips140/subtle":             "M5",
	"crypto/internal/fips140/sha256":             "M5",
	"crypto/internal/fips140/sha512":             "M5",
	"crypto/internal/fips140/sha3":               "M5",
	"crypto/internal/fips140/hmac":               "M5",
	"crypto/internal/fips140/hkdf":               "M5",
	"crypto/internal/fips140/pbkdf2":             "M5",
	"crypto/internal/fips140/ecdh":               "M5",
	"crypto/internal/fips140/ecdsa":              "M5",
	"crypto/internal/fips140/ed25519":            "M5",
	"crypto/internal/fips140/rsa":                "M5",

	// M7: wave-1 packages (the old multiResultSyntheticError M7 block).
	"errors":           "M7",
	"sort":             "M7",
	"bytes":            "M7",
	"math":             "M7",
	"strings":          "M7",
	"encoding/xml":     "M7",
	"encoding/base32":  "M7",
	"encoding/base64":  "M7",
	"encoding/hex":     "M7",
	"encoding/binary":  "M7",
	"encoding/ascii85": "M7",
	"encoding/pem":     "M7",
	"encoding/asn1":    "M7",
	"encoding":         "M7",
	"encoding/csv":     "M7",
	"encoding/gob":     "M7",
	"strconv":          "M7",
	"encoding/json":    "M7",

	// M8: wave-2 packages (the old multiResultSyntheticError M8 block).
	"io":                   "M8",
	"os":                   "M8",
	"io/fs":                "M8",
	"syscall":              "M8",
	"reflect":              "M8",
	"internal/reflectlite": "M8",
	"iter":                 "M8",
	"hash":                 "M8",
	"crypto":               "M8",
	"crypto/aes":           "M8",
	"crypto/cipher":        "M8",
	"crypto/des":           "M8",
	"crypto/hmac":          "M8",
	"crypto/md5":           "M8",
	"crypto/rand":          "M8",
	"crypto/rc4":           "M8",
	"crypto/sha1":          "M8",
	"crypto/sha256":        "M8",
	"crypto/sha3":          "M8",
	"crypto/sha512":        "M8",
	"crypto/subtle":        "M8",

	// M9: tier-1d / tier-4 / tier-5 packages (the old
	// multiResultSyntheticError M9 block).
	"unique":                "M9",
	"structs":               "M9",
	"internal/bytealg":      "M9",
	"net":                   "M9",
	"net/netip":             "M9",
	"net/url":               "M9",
	"net/textproto":         "M9",
	"internal/poll":         "M9",
	"internal/nettrace":     "M9",
	"internal/singleflight": "M9",
	"compress/flate":        "M9",
	"compress/gzip":         "M9",
	"compress/zlib":         "M9",
	"compress/lzw":          "M9",
	"compress/bzip2":        "M9",
	"archive/tar":           "M9",
	"archive/zip":           "M9",
	"mime":                  "M9",
	"mime/multipart":        "M9",
	"mime/quotedprintable":  "M9",
}
