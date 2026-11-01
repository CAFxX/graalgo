#!/usr/bin/env python3
"""Build a corpus program's gc *reference* leg against the extracted phase-1
synthetic sources.

Some wave-1 corpus programs use M7-extension constructors (csv.NewReaderString,
csv.NewBufferWriter, gob.NewBufferEncoder, gob.NewBufferDecoder) that do not
exist in real std, so the stock-gc leg cannot build them against GOROOT (and a
GOPATH cannot shadow the standard library). For programs marked with an empty
<base>.synthetic file, the runner calls this script to materialize a temp
GOPATH:

  <outdir>/gopath/src/m7synth/csv/csv.go      <- syntheticCsvSource
  <outdir>/gopath/src/m7synth/gob/gob.go      <- syntheticGobSource
  <outdir>/gopath/src/m7prog/main.go          <- corpus program, imports
                                                 rewritten to m7synth/...

RETIRED (rem-review-20261031-p1-1, 2026-10-31): the phase-1 synthetic
errors package is deleted — phase-1 resolves real GOROOT errors via the
disk importer now. The synthetic csv/gob sources' `import "errors"`
therefore stays as real errors on both legs (no m7synth/errors copy is
extracted anymore), and corpus programs importing "errors" need no
.synthetic marker for errors alone. The reference behavior for csv/gob
is still the synthetic source executed by stock gc — the same sources
§11 of the P3-M7 code review validated as gc-faithful on adversarial
inputs — not real std.

Usage: extract_synth_ref.py <phase1-frontend-dir> <prog.go> <outdir>
Prints the GOPATH on stdout.
"""

import os
import re
import sys

# Corpus import path -> (phase-1 const name, phase-1 file, synthetic package name).
# errors was retired from this map by rem-review-20261031-p1-1 (2026-10-31):
# the phase-1 synthetic errors source is deleted and real GOROOT errors
# resolves via the disk importer, so no extraction or import rewrite is
# needed for it anymore.
SYNTH = {
    "encoding/csv": ("syntheticCsvSource", "synth_enc_csv_gob.go", "csv"),
    "encoding/gob": ("syntheticGobSource", "synth_enc_csv_gob.go", "gob"),
    "encoding/xml": ("syntheticXmlSource", "synth_xml.go", "xml"),
}


def extract_const(path, const):
    with open(path) as f:
        text = f.read()
    m = re.search(r"const\s+" + re.escape(const) + r"\s*=\s*`", text)
    if not m:
        raise SystemExit(f"extract_synth_ref: const {const} not found in {path}")
    start = m.end()
    # Raw string literals cannot contain backticks: the next one closes it.
    end = text.index("`", start)
    return text[start:end]


def main():
    p1dir, prog, outdir = sys.argv[1], sys.argv[2], sys.argv[3]
    with open(prog) as f:
        progsrc = f.read()

    def imports_ip(progsrc, ip):
        return re.search(
            r'(?m)^(\s*(?:[A-Za-z_]\w*\s+)?)"%s"' % re.escape(ip), progsrc
        ) is not None

    needed = [ip for ip in SYNTH if imports_ip(progsrc, ip)]
    consts = {SYNTH[ip][0] for ip in needed}

    gopath = os.path.join(outdir, "gopath")
    for const in sorted(consts):
        _, srcfile, pkgname = next(v for v in SYNTH.values() if v[0] == const)
        src = extract_const(os.path.join(p1dir, srcfile), const)
        # No errors rewrite anymore (rem-review-20261031-p1-1): the
        # csv/gob sources' `import "errors"` stays real on both legs.
        pkgdir = os.path.join(gopath, "src", "m7synth", pkgname)
        os.makedirs(pkgdir, exist_ok=True)
        with open(os.path.join(pkgdir, pkgname + ".go"), "w") as f:
            f.write(src)

    for ip, (_, _, pkgname) in SYNTH.items():
        # Rewrite only import paths (start of an import line, optional
        # import name), never string literals in program output.
        progsrc = re.sub(
            r'(?m)^(\s*(?:[A-Za-z_]\w*\s+)?)"%s"' % re.escape(ip),
            r'\1"m7synth/%s"' % pkgname,
            progsrc,
        )
    progdir = os.path.join(gopath, "src", "m7prog")
    os.makedirs(progdir, exist_ok=True)
    with open(os.path.join(progdir, "main.go"), "w") as f:
        f.write(progsrc)

    print(gopath)


if __name__ == "__main__":
    main()
