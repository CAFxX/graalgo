# shellcheck shell=bash
# test/lib/jvmcds.sh — class-data sharing for the gates' launcher JVMs.
#
# Every program a gate runs is one JVM start that loads the same backend
# classes and Truffle jars; a dynamic CDS archive maps them instead
# (measured: a hello-size program on graalgo-bundle-run 0.67 s -> 0.36 s
# wall, 1.47 s -> 0.84 s CPU; a corpus program on graalgo-sync-run 12.5 s
# -> 9.6 s CPU). CDS refuses a non-empty directory on the class path, so the
# archive is made over jars of the modules' out/classes, which the
# launchers take from GRAALGO_TRUFFLE_CLASSES / GRAALGO_INTEROP_CLASSES.
# Jars and archive live in <repo>/.tmp/cds/<kind>-<key> (gitignored), the
# key a hash of every class file's name, size and mtime, the lib jars and
# the JDK: a rebuild is a new key, never a stale class. The same classes run
# either way; the archive only changes how they are loaded. A JVM that
# cannot use the archive (flags or class path differ) runs without it,
# silently (-Xlog:cds=off: the JVM's CDS notices go to stdout, which the
# gates compare).
#
# Integrity (the dev host corrupts large copies under load, BUG-0012): the
# jars and the archive are hashed into <dir>/MANIFEST, written last; every
# setup verifies them (one sha256 pass over ~100 MB, per gate, not per JVM)
# and a mismatch deletes the directory and makes it again, so a damaged
# archive or jar is never mapped.
#
#   jvm_cds_setup bundle <runner>
#                    for graalgo-bundle-run (toolexec_setup calls it last;
#                    needs toolexec_build): the truffle jar + bundle.jsa
#   jvm_cds_setup sync
#                    for graalgo-sync-run (snapshot-check, test-regression):
#                    the interop and truffle jars + sync.jsa
#   Both find or make the archive (one extra JVM run, once per key), then
#   export the class variables and add -XX:SharedArchiveFile to
#   GRAALGO_JAVA_OPTS. Any failure: no CDS, never a failed gate.
#   `bash jvmcds.sh sync-env` prints the exports (for make recipes).
# Env: GRAALGO_CDS=off disables it; GRAALGO_CDS_DIR overrides .tmp/cds.

JVMCDS_LOG="-Xlog:cds=off -Xlog:cds+dynamic=off -Xlog:aot=off"

jvm_cds_setup() {
    local kind="$1" runner="${2:-}" b="$GRAALGO_ROOT/internal/backend" root key dir mods m
    [ "${GRAALGO_CDS:-on}" = off ] && return 0
    GRAALVM_HOME="${GRAALVM_HOME:-$HOME/tools/graalvm-jdk-25}"
    [ -x "$GRAALVM_HOME/bin/jar" ] || return 0
    case "$kind" in
        bundle) mods="truffle" ;;
        sync) mods="interop truffle" ;;
        *) return 0 ;;
    esac
    root="${GRAALGO_CDS_DIR:-$GRAALGO_ROOT/.tmp/cds}"
    key=$( { echo "jvmcds-v1 $kind"
             for m in $mods; do find "$b/$m/out/classes" -type f -printf "$m %P %s %T@\n" | LC_ALL=C sort; done
             ls -l "$b"/truffle/lib/*.jar; cat "$GRAALVM_HOME/release" 2>/dev/null; } | sha256sum | cut -c1-24)
    dir="$root/$kind-$key"
    mkdir -p "$root" || return 0
    # A creator killed half-way leaves a dir without an archive: retry it
    # after 10 minutes.
    if [ -d "$dir" ] && [ ! -f "$dir/$kind.jsa" ] && [ -n "$(find "$dir" -maxdepth 0 -mmin +10)" ]; then
        rm -rf "$dir"
    fi
    # A complete entry that fails its manifest (or has none): delete, remake.
    if [ -f "$dir/$kind.jsa" ] && ! _jvm_cds_verify "$dir"; then
        echo "jvmcds: $dir fails its manifest: deleted, remade" >&2
        rm -rf "$dir"
    fi
    if mkdir "$dir" 2>/dev/null; then
        _jvm_cds_make "$kind" "$dir" "$runner" "$mods" || { rm -rf "$dir"; return 0; }
        # keep the three newest keys of this kind
        ls -1td "$root/$kind"-*/ 2>/dev/null | tail -n +4 | xargs -r rm -rf
    fi
    [ -f "$dir/$kind.jsa" ] || return 0  # another gate is still making it
    _jvm_cds_export "$dir" "$mods"
    GRAALGO_JAVA_OPTS="${GRAALGO_JAVA_OPTS:-} -XX:SharedArchiveFile=$dir/$kind.jsa $JVMCDS_LOG"
    export GRAALGO_JAVA_OPTS
}

_jvm_cds_verify() { # <dir>: 0 iff MANIFEST exists and every file matches
    [ -f "$1/MANIFEST" ] && (cd "$1" && sha256sum --quiet --strict -c MANIFEST) >/dev/null 2>&1
}

_jvm_cds_export() { # <dir> <mods>
    local m
    for m in $2; do
        case "$m" in
            truffle) export GRAALGO_TRUFFLE_CLASSES="$1/truffle.jar" ;;
            interop) export GRAALGO_INTEROP_CLASSES="$1/interop.jar" ;;
        esac
    done
}

# _jvm_cds_make <kind> <dir> <runner> <mods>: the jars, then the archive
# from one run of a small program that touches goroutines, channels, maps,
# defer and panics.
_jvm_cds_make() {
    local kind="$1" dir="$2" runner="$3" mods="$4" w="$2/work" m
    for m in $mods; do
        "$GRAALVM_HOME/bin/jar" --create --file "$dir/$m.jar.new" \
            -C "$GRAALGO_ROOT/internal/backend/$m/out/classes" . || return 1
        mv "$dir/$m.jar.new" "$dir/$m.jar" || return 1
    done
    mkdir -p "$w/mod" || return 1
    cat > "$w/mod/main.go" <<'EOF'
package main

type shape interface{ area() int }
type sq struct{ n int }

func (s sq) area() int { return s.n * s.n }

func main() {
	m := map[string]int{"a": 1}
	c := make(chan int, 1)
	go func() { c <- m["a"] }()
	defer func() { println("recovered", recover() != nil) }()
	var s shape = sq{<-c + 1}
	println("area", s.area(), len(m))
	panic("x")
}
EOF
    printf 'module cds\n\ngo 1.27\n' > "$w/mod/go.mod"
    local cmd
    if [ "$kind" = bundle ]; then
        ( cd "$w/mod" && toolexec_build "$w/prog" "$runner" ) >/dev/null 2>&1 || return 1
        cmd=("$w/prog")
    else
        cmd=("$GRAALGO_ROOT/internal/backend/graalgo-sync-run" "$w/mod/main.go")
    fi
    ( cd "$w" && _jvm_cds_export "$dir" "$mods" \
        && GRAALGO_JAVA_OPTS="${GRAALGO_JAVA_OPTS:-} -XX:ArchiveClassesAtExit=$dir/$kind.jsa.new $JVMCDS_LOG" \
        "${cmd[@]}" ) >/dev/null 2>&1 </dev/null
    [ -s "$dir/$kind.jsa.new" ] || return 1
    rm -rf "$w"
    # the manifest first, the archive's final name last: an entry with a
    # .jsa always has its manifest (a reader in between sees no archive)
    (cd "$dir" && sha256sum ./*.jar "$kind.jsa.new" | sed "s/$kind\.jsa\.new\$/$kind.jsa/" > MANIFEST) || return 1
    mv "$dir/$kind.jsa.new" "$dir/$kind.jsa"
}

# `bash test/lib/jvmcds.sh sync-env`: the exports for a make recipe.
if [ "${BASH_SOURCE[0]}" = "$0" ] && [ "${1:-}" = sync-env ]; then
    GRAALGO_ROOT="${GRAALGO_ROOT:-$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/../.." && pwd)}"
    jvm_cds_setup sync
    for v in GRAALGO_TRUFFLE_CLASSES GRAALGO_INTEROP_CLASSES GRAALGO_JAVA_OPTS; do
        [ -n "${!v:-}" ] && printf 'export %s=%q\n' "$v" "${!v}"
    done
    exit 0
fi
