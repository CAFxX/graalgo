package p3frontend

import "go/types"

// syntheticBuild constructs the *types.Package for one synthetic import
// path. It may resolve synthetic dependencies through imp (the Importer
// caches every built package in imp.pkgs, so dependency order is enforced
// in exactly one place — here — instead of being spread across call sites).
type syntheticBuild func(imp *Importer) (*types.Package, error)

// syntheticPackages is the SINGLE SOURCE OF TRUTH for p3frontend's
// synthetic-package set (L22: triple-list parity).
//
// Both consumers consult this table, never a private copy:
//   - Loader.ResolveImport (loader.go) answers kindSynthetic for exactly
//     these paths;
//   - Importer.Import (importer.go) builds the package via the entry's
//     builder.
//
// Adding a synthetic package means adding ONE table entry. The parity test
// (synthetic_parity_test.go) additionally asserts that every entry is
// importable by the frozen gofrontend overlay (syntheticSources /
// dedicated stubs / disk), so the flattened output the p3frontend pipeline
// feeds to gofrontend always type-checks there.
var syntheticPackages = map[string]syntheticBuild{
	"runtime": func(imp *Importer) (*types.Package, error) {
		return syntheticRuntimePackage(), nil
	},
	"fmt": func(imp *Importer) (*types.Package, error) {
		return syntheticFmtPackage(), nil
	},
	"sync": func(imp *Importer) (*types.Package, error) {
		return syntheticSyncPackage(), nil
	},
	"sync/atomic": func(imp *Importer) (*types.Package, error) {
		return syntheticAtomicPackage(), nil
	},
	"time": func(imp *Importer) (*types.Package, error) {
		return syntheticTimePackage(), nil
	},
	"context": func(imp *Importer) (*types.Package, error) {
		// Context references time.Time/time.Duration: ensure the
		// synthetic time package is built and cached first.
		timePkg, ok := imp.pkgs["time"]
		if !ok {
			timePkg = syntheticTimePackage()
			imp.pkgs["time"] = timePkg
		}
		return syntheticContextPackage(timePkg), nil
	},
	// P3-M7 (wave 1): synthetic errors package (real-Go New/Is/Unwrap/
	// Join/AsType; As fail-fast). Self-contained: no synthetic
	// dependencies.
	"errors": func(imp *Importer) (*types.Package, error) {
		return syntheticErrorsPackage(), nil
	},
	// P3-M7 (wave 1): synthetic sort package (real-Go pdqsort/stable/
	// search; Find/Slice/SliceStable/SliceIsSorted fail-fast).
	// Self-contained: no synthetic dependencies.
	"sort": func(imp *Importer) (*types.Package, error) {
		return syntheticSortPackage(), nil
	},
	// P3-M10: synthetic io package (minimal: Reader/Writer only;
	// see synth_io.go). Required by bytes/strings (P3-M8).
	"io": func(imp *Importer) (*types.Package, error) {
		return syntheticIoPackage(), nil
	},
	// P3-M7 (wave 1): synthetic bytes package (synth_bytes.go).
	// Requires the synthetic io package for io.Reader/io.Writer
	// (P3-M8): ensure the io mirror is built and cached first (the
	// context->time precedent).
	"bytes": func(imp *Importer) (*types.Package, error) {
		ioPkg, ok := imp.pkgs["io"]
		if !ok {
			ioPkg = syntheticIoPackage()
			imp.pkgs["io"] = ioPkg
		}
		return syntheticBytesPackage(ioPkg), nil
	},
	// P3-M7 (wave 1): synthetic strings package (synth_strings.go).
	// Self-contained: no synthetic dependencies.
	"strings": func(imp *Importer) (*types.Package, error) {
		return syntheticStringsPackage(), nil
	},
	"unsafe": func(imp *Importer) (*types.Package, error) {
		return syntheticUnsafePackage(), nil
	},
	// P3-M7 (wave 1): synthetic math package (real-Go ports +
	// bit-conversion/Sqrt/FMA intrinsics; see synth_math.go).
	// Self-contained: no synthetic dependencies.
	"math": func(imp *Importer) (*types.Package, error) {
		return syntheticMathPackage(), nil
	},
	// P3-M7 (wave 1): synthetic encoding/xml (see synth_xml.go).
	// Self-contained: no synthetic->synthetic imports.
	"encoding/xml": func(imp *Importer) (*types.Package, error) {
		return syntheticXmlPackage(), nil
	},
	// P3-M7 (wave 1): synthetic encoding/csv (see synth_enc_csv_gob.go).
	"encoding/csv": func(imp *Importer) (*types.Package, error) {
		return syntheticCsvPackage(), nil
	},
	// P3-M7 (wave 1): synthetic encoding/gob (see synth_enc_csv_gob.go).
	"encoding/gob": func(imp *Importer) (*types.Package, error) {
		return syntheticGobPackage(), nil
	},
	// P3-M7 (wave 1): synthetic strconv (see synth_strconv.go).
	"strconv": func(imp *Importer) (*types.Package, error) {
		return syntheticStrconvPackage(), nil
	},
	// P3-M7 (wave 1): synthetic encoding/json (see synth_json.go).
	"encoding/json": func(imp *Importer) (*types.Package, error) {
		return syntheticJsonPackage(), nil
	},
	// P3-M7 (wave 1): synthetic encoding/base32 (m7-wenc-core).
	// Self-contained: no synthetic dependencies.
	"encoding/base32": func(imp *Importer) (*types.Package, error) {
		return syntheticBase32Package(), nil
	},
	// P3-M7 (wave 1): synthetic encoding/base64 (m7-wenc-core).
	// Self-contained: no synthetic dependencies.
	"encoding/base64": func(imp *Importer) (*types.Package, error) {
		return syntheticBase64Package(), nil
	},
	// P3-M7 (wave 1): synthetic encoding/hex (m7-wenc-core).
	// Self-contained: no synthetic dependencies.
	"encoding/hex": func(imp *Importer) (*types.Package, error) {
		return syntheticHexPackage(), nil
	},
	// P3-M7 (wave 1): synthetic encoding/binary (m7-wenc-core).
	// Self-contained: no synthetic dependencies.
	"encoding/binary": func(imp *Importer) (*types.Package, error) {
		return syntheticBinaryPackage(), nil
	},
	// P3-M7 (wave 1): synthetic encoding/ascii85 (m7-wenc-core).
	// Self-contained: no synthetic dependencies.
	"encoding/ascii85": func(imp *Importer) (*types.Package, error) {
		return syntheticAscii85Package(), nil
	},
	// P3-M7 (wave 1): synthetic encoding/pem (m7-wenc-core).
	// Imports synthetic encoding/base64: ensure the base64 mirror is
	// built and cached first (the context->time precedent).
	"encoding/pem": func(imp *Importer) (*types.Package, error) {
		base64Pkg, ok := imp.pkgs["encoding/base64"]
		if !ok {
			base64Pkg = syntheticBase64Package()
			imp.pkgs["encoding/base64"] = base64Pkg
		}
		_ = base64Pkg
		return syntheticPemPackage(), nil
	},
	// P3-M7 (wave 1): synthetic encoding/asn1 (m7-wenc-core).
	// Self-contained: no synthetic dependencies.
	"encoding/asn1": func(imp *Importer) (*types.Package, error) {
		return syntheticAsn1Package(), nil
	},
	// P3-M7 (wave 1): synthetic top-level encoding (m7-wenc-core):
	// the six marshaling interfaces only.
	// Self-contained: no synthetic dependencies.
	"encoding": func(imp *Importer) (*types.Package, error) {
		return syntheticEncodingPackage(), nil
	},
}
