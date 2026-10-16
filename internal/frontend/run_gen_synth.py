#!/usr/bin/env python3
"""Regenerate the synthetic stdlib sources (synth_*.go) from gc 1.27.1.

This is the wired entry point for the 10 gen_synth_*.py generators
(P1-R1-2, P1-R7-7). `make gen-synth` runs it; `make all` runs `generate`
(which includes it), and CI runs `git diff --exit-code` afterwards — so any
drift between the generators (+ adaptation patches) and the committed files
fails the build loudly instead of silently.

Pipeline, per generator:
  1. run gen_synth_<name>.py with GEN_SYNTH_OUTDIR=<stage dir>
  2. apply gen_synth_adapt/<output>.patch when present. These patches are the
     frontend-constraint adaptations (WS D/G: no multi-result calls, no
     StarExpr assignment, no max builtin, ...) that the generator's verbatim
     gc extraction does not model. They are explicit, version-controlled, and
     fail loudly if the generator output they were written against changes.
  3. copy the staged outputs over internal/frontend/

Environment:
  GO              go binary to use (default: "go" on PATH). Must report
                  go1.27.1 — the generators read that exact toolchain's
                  sources; anything else fails fast.
  GRAALGO_GOROOT  passed through to the generators (default: `$GO env GOROOT`).
"""

import os
import shutil
import subprocess
import sys
import tempfile

HERE = os.path.dirname(os.path.abspath(__file__))

# (generator script, [emitted files]); patch application is driven by the
# presence of gen_synth_adapt/<file minus .go>.patch.
GENERATORS = [
    ("gen_synth_archive_mime.py", ["synth_archive_tar.go", "synth_archive_zip.go",
                                   "synth_mime.go", "synth_mime_multipart.go",
                                   "synth_mime_quotedprintable.go",
                                   # Table-entry fragment for manual insertion
                                   # into m7MultiResultFuncs; drift-gated like
                                   # the sources (P1-1: never lose it again).
                                   "archive_mime_table_entries.txt"]),
    ("gen_synth_bufio.py", ["synth_bufio.go"]),
    ("gen_synth_compress.py", ["synth_compress_bzip2.go", "synth_compress_flate.go",
                               "synth_compress_gzip.go", "synth_compress_lzw.go",
                               "synth_compress_zlib.go"]),
    ("gen_synth_mathbits.py", ["synth_mathbits.go"]),
    ("gen_synth_mathcmplx.py", ["synth_mathcmplx.go"]),
    ("gen_synth_mathrand.py", ["synth_mathrand.go"]),
    ("gen_synth_mathrandv2.py", ["synth_mathrandv2.go"]),
    ("gen_synth_path.py", ["synth_path.go"]),
    ("gen_synth_regexpsyntax.py", ["synth_regexpsyntax.go"]),
    ("gen_synth_textscanner.py", ["synth_textscanner.go"]),
]

GO_VERSION_PIN = "go1.27.1"


def run(cmd, **kw):
    r = subprocess.run(cmd, capture_output=True, text=True, **kw)
    if r.returncode != 0:
        raise SystemExit("gen-synth: command failed: %s\n%s%s"
                         % (" ".join(cmd), r.stdout, r.stderr))
    return r


def main():
    go = os.environ.get("GO", "go")
    ver = run([go, "version"]).stdout.strip()
    if GO_VERSION_PIN not in ver:
        raise SystemExit(
            "gen-synth: requires go %s (generators read that toolchain's "
            "sources); got: %s" % (GO_VERSION_PIN, ver))
    goroot = os.environ.get("GRAALGO_GOROOT") or \
        run([go, "env", "GOROOT"]).stdout.strip()
    if shutil.which("patch") is None:
        raise SystemExit("gen-synth: the `patch` utility is required "
                         "(applies gen_synth_adapt/*.patch)")

    stage = tempfile.mkdtemp(prefix="gen-synth-")
    try:
        # Build the extract_decls helper (replaces the lost ad-hoc
        # /tmp/extract_decls binary; P1-R1-2).
        extract_decls = os.path.join(stage, "extract_decls")
        run([go, "build", "-o", extract_decls,
             "./gen_synth_helper/extract_decls"], cwd=HERE)

        env = dict(os.environ, GRAALGO_GOROOT=goroot,
                   GEN_SYNTH_OUTDIR=stage,
                   GEN_SYNTH_EXTRACT_DECLS=extract_decls,
                   GEN_SYNTH_TMPDIR=stage)
        for script, outputs in GENERATORS:
            r = subprocess.run([sys.executable, os.path.join(HERE, script)],
                               capture_output=True, text=True, env=env)
            if r.returncode != 0:
                raise SystemExit("gen-synth: %s failed\n%s%s"
                                 % (script, r.stdout, r.stderr))
            for out in outputs:
                staged = os.path.join(stage, out)
                if not os.path.isfile(staged):
                    raise SystemExit(
                        "gen-synth: %s did not emit %s" % (script, out))
                patch = os.path.join(HERE, "gen_synth_adapt",
                                     out[:-3] + ".patch")
                if os.path.isfile(patch):
                    run(["patch", "-s", "-p1", "-d", stage, "-i", patch])
                    print("gen-synth: %-28s + adapt patch" % out)
                else:
                    print("gen-synth: %-28s (verbatim)" % out)
                shutil.copyfile(staged, os.path.join(HERE, out))
        print("gen-synth: all %d files regenerated into %s"
              % (sum(len(o) for _, o in GENERATORS), HERE))
    finally:
        shutil.rmtree(stage, ignore_errors=True)


if __name__ == "__main__":
    main()
