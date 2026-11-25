# shellcheck shell=bash
# test/lib/gcoracle.sh — a cache of the gc leg of the toolexec differential
# gates (test/m1a, m1r, m1c, m1d, m1w13, imgcheck). Sourced after
# test/lib/infra.sh and infra_setup_go.
#
# The gc leg (build with the pinned toolchain, run, capture stdout, stderr
# and the exit status) depends only on the module's files, the build flags,
# the run's arguments/environment/stdin and the toolchain, so its result is
# stored under a key over all of them and replayed when nothing changed:
# the graalgo leg always runs live; only gc's answer to an unchanged question
# is remembered. Never cached: a timed-out run (exit 124: host speed), a run
# killed by a signal (status > 128) or one whose stderr shows a crash of the
# gc runtime itself (an unexpected signal, a fault, out of memory: the host,
# not the program), and an output that contains the run's scratch path (the
# key cannot cover it).
#
# Integrity (the dev host corrupts large copies under load, BUG-0012): an
# entry's last file is MANIFEST, the sha256 of each file, written after them
# and moved into place with the entry; a load verifies the entry against it
# (a mismatch or a missing manifest deletes the entry: a miss, never a
# replay) and the copies it made into <dir> again (a mismatch: a miss). The
# key also hashes this file and the text of the leg's shell functions (the
# live function, gc_leg_build_run and the gate's run_leg), so a change to how
# the leg runs is a new key without a hand-bumped version.
#
#   gc_oracle <dir> <moddir> <live-fn> <material...>
#                    the whole leg: gc_oracle_load, else <live-fn> (which
#                    builds and runs gc, writing <dir>/gc.status, gc.out,
#                    gc.err, gc.build) and gc_oracle_store; $TMP is the
#                    scratch path an output must not mention to be stored
#   gc_oracle_key <moddir> <material...>
#                    prints the key: sha256 over this file, `go version`,
#                    the GO* run environment, every file under <moddir>
#                    (path and content) and <material...> (flags, args,
#                    stdin file contents, the leg's function text —
#                    whatever else the leg depends on)
#   gc_oracle_load <key> <dir>
#                    on a hit copies gc.status, gc.out, gc.err (and gc.build
#                    when the build failed) into <dir> and returns 0
#   gc_oracle_store <key> <dir> <scratch>
#                    stores <dir>'s gc.status/gc.out/gc.err[/gc.build]
#                    (unless the run timed out or mentions <scratch>)
#
# gc.status holds the run's exit status, or "build" when gc rejected the
# program (gc.build then has the build log).
# Env: GC_ORACLE_CACHE  cache directory (default <repo>/.tmp/gc-oracle,
#                       gitignored); "off" disables the cache (always live).

GC_ORACLE_DIR="${GC_ORACLE_CACHE:-$GRAALGO_ROOT/.tmp/gc-oracle}"
GC_ORACLE_SELF="${BASH_SOURCE[0]}"
GC_ORACLE_FILES="gc.status gc.out gc.err gc.build"
# gc runtime crashes (the host's fault, not the program's): never stored.
GC_ORACLE_CRASH='unexpected signal|^fatal error: fault|^runtime: out of memory|^fatal error: runtime: out of memory|^SIGSEGV|^SIGBUS|^SIGILL'

gc_oracle_key() {
    local dir="$1"
    shift
    [ "$GC_ORACLE_DIR" = off ] && { echo none; return 0; }
    {
        echo "gcoracle-v2"
        sha256sum < "$GC_ORACLE_SELF"
        "$GO" version
        env | grep -E '^(GODEBUG|GOFLAGS|GOEXPERIMENT|GOAMD64|GOARCH|GOOS|CGO_ENABLED|GOMAXPROCS|GOTRACEBACK|GOTOOLCHAIN)=' | LC_ALL=C sort
        printf '%s\n' "$@"
        (cd "$dir" && find . -type f -print0 | LC_ALL=C sort -z | xargs -0 -r sha256sum)
    } | sha256sum | cut -c1-40
}

gc_oracle_load() {
    local e="$GC_ORACLE_DIR/$1" f
    [ "$GC_ORACLE_DIR" = off ] && return 1
    [ -d "$e" ] || return 1
    if [ ! -f "$e/gc.status" ] || [ ! -f "$e/MANIFEST" ] \
        || ! (cd "$e" && sha256sum --quiet --strict -c MANIFEST) >/dev/null 2>&1; then
        echo "gcoracle: entry $1 fails its manifest: deleted (a miss)" >&2
        rm -rf "$e"
        return 1
    fi
    for f in $GC_ORACLE_FILES; do
        rm -f "$2/$f"
        [ -f "$e/$f" ] && cp "$e/$f" "$2/$f"
    done
    # the copy itself is checked too (BUG-0012 loses bytes in copies)
    (cd "$2" && sha256sum --quiet --strict -c "$e/MANIFEST") >/dev/null 2>&1 || return 1
    return 0
}

gc_oracle_store() {
    local key="$1" dir="$2" scratch="$3" st t f
    [ "$GC_ORACLE_DIR" = off ] && return 0
    st=$(cat "$dir/gc.status" 2>/dev/null) || return 0
    [ "$st" = 124 ] && return 0
    case "$st" in ''|*[!0-9]*) [ "$st" = build ] || return 0 ;; *) [ "$st" -gt 128 ] && return 0 ;; esac
    [ -f "$dir/gc.err" ] && grep -aqE -- "$GC_ORACLE_CRASH" "$dir/gc.err" && return 0
    for f in gc.out gc.err gc.build; do
        [ -f "$dir/$f" ] && grep -aqF -- "$scratch" "$dir/$f" && return 0
    done
    mkdir -p "$GC_ORACLE_DIR" || return 0
    t=$(mktemp -d "$GC_ORACLE_DIR/.new.XXXXXX") || return 0
    for f in $GC_ORACLE_FILES; do
        [ -f "$dir/$f" ] && { cp "$dir/$f" "$t/$f" || { rm -rf "$t"; return 0; }; }
    done
    # the manifest hashes the live files (not the copies): a copy that lost
    # bytes fails it at the first load instead of being replayed
    (cd "$dir" && for f in $GC_ORACLE_FILES; do [ -f "$f" ] && sha256sum "$f"; done) > "$t/MANIFEST" \
        || { rm -rf "$t"; return 0; }
    # rename is atomic: a concurrent reader sees the whole entry or none
    mv -T "$t" "$GC_ORACLE_DIR/$key" 2>/dev/null || rm -rf "$t"
}

gc_oracle() {
    local dir="$1" mod="$2" live="$3" key
    shift 3
    # the leg's own text is an input too (run_leg is the gate's)
    key=$(gc_oracle_key "$mod" "$@" "$(declare -f "$live" gc_leg_build_run run_leg 2>/dev/null)")
    gc_oracle_load "$key" "$dir" && return 0
    "$live"
    gc_oracle_store "$key" "$dir" "$TMP"
}

# gc_leg_build_run <moddir> <dir> <run-fn> [build flags...]: the usual live
# leg — `go build` in <moddir> to <dir>/gc.bin, then <run-fn> <exe> <stdout>
# <stderr> (the gate's run_leg) — writing gc.status ("build" on a rejected
# program).
gc_leg_build_run() {
    local mod="$1" dir="$2" run="$3" st=0
    shift 3
    if ( cd "$mod" && "$GO" build "$@" -o "$dir/gc.bin" . ) >"$dir/gc.build" 2>&1; then
        "$run" "$dir/gc.bin" "$dir/gc.out" "$dir/gc.err" || st=$?
        echo "$st" > "$dir/gc.status"
    else
        echo build > "$dir/gc.status"
    fi
}
