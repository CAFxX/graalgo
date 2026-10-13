#!/usr/bin/env python3
"""Build a corpus program's gc *reference* leg against the extracted phase-1
synthetic sources (wave-2 edition).

Some wave-2 corpus programs use synthetic-only surface (fail-fast bodies
whose messages the .panic pins compare, multi-result helpers, //go:linkname
binders) that does not exist in real std, so the stock-gc leg cannot build
them against GOROOT (and a GOPATH cannot shadow the standard library). For
programs marked with an empty <base>.synthetic file, the runner calls this
script to materialize a temp GOPATH:

  <outdir>/gopath/src/m8synth/syscall/syscall.go <- syntheticSyscallSource
  <outdir>/gopath/src/graalgo.os/os.go             <- linkname target stubs
  <outdir>/gopath/src/m8prog/main.go               <- corpus program, imports
                                                      rewritten to m8synth/...

The synthetic syscall source binds graalgo.os.* symbols via //go:linkname.
Under stock gc those symbols must resolve at link time, so the script also
materializes a stub package at import path "graalgo.os" (its functions are
never called by panic-program legs) and adds a blank import of it to the
extracted main package. The reference behavior is therefore the synthetic
source executed by stock gc — the same sources the wave-2 review validates
as gc-faithful — not real std.

Usage: extract_synth_ref.py <phase1-frontend-dir> <prog.go> <outdir>
Prints the GOPATH on stdout.
"""

import os
import re
import sys

# Corpus import path -> (phase-1 const name, phase-1 file, synthetic package name).
SYNTH = {
    "syscall": ("syntheticSyscallSource", "synth_syscall.go", "syscall"),
}

# Go signatures of the graalgo.os.* linkname targets bound by the synthetic
# syscall source. Must match the //go:linkname declarations there exactly
# (the linker matches by symbol name: import path + "." + func name).
GRAALGO_OS_STUBS = """\
// Go stubs for the graalgo.os.* linkname targets, for the gc reference leg
// of .synthetic corpus programs. The synthetic syscall source binds these
// symbols via //go:linkname; under stock gc the bodies below satisfy the
// linker. Panic-program legs never call them.
package os

func getenvFound(key string) int64 { return 0 }
func getenv(key string) string      { return "" }
func setenv(key, value string) int64 { return 0 }
func unsetenv(key string) int64    { return 0 }
func clearenv() int64              { return 0 }
func environ() string              { return "" }
func getpid() int64                { return 1 }
func getppid() int64               { return 0 }
func getuid() int64                { return 0 }
func geteuid() int64               { return 0 }
func getgid() int64                { return 0 }
func getegid() int64               { return 0 }
func getgroups() string            { return "" }
func getcwd() string               { return "" }
func exit(code int64)              {}
"""


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
        pkgdir = os.path.join(gopath, "src", "m8synth", pkgname)
        os.makedirs(pkgdir, exist_ok=True)
        with open(os.path.join(pkgdir, pkgname + ".go"), "w") as f:
            f.write(src)

    for ip, (_, _, pkgname) in SYNTH.items():
        # Rewrite only import paths (start of an import line, optional
        # import name), never string literals in program output.
        progsrc = re.sub(
            r'(?m)^(\s*(?:[A-Za-z_]\w*\s+)?)"%s"' % re.escape(ip),
            r'\1"m8synth/%s"' % pkgname,
            progsrc,
        )

    if "syntheticSyscallSource" in consts:
        # Materialize the graalgo.os linkname-target stubs and blank-import
        # them so the gc link resolves the renamed symbols.
        ospkg = os.path.join(gopath, "src", "graalgo.os")
        os.makedirs(ospkg, exist_ok=True)
        with open(os.path.join(ospkg, "os.go"), "w") as f:
            f.write(GRAALGO_OS_STUBS)
        progsrc = re.sub(
            r"(?m)^(package\s+\w+)",
            r'\1\n\nimport _ "graalgo.os"',
            progsrc,
            count=1,
        )

    progdir = os.path.join(gopath, "src", "m8prog")
    os.makedirs(progdir, exist_ok=True)
    with open(os.path.join(progdir, "main.go"), "w") as f:
        f.write(progsrc)

    print(gopath)


if __name__ == "__main__":
    main()
