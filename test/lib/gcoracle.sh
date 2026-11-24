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
# is remembered. Never cached: a timed-out run (exit 124: host speed), and an
# output that contains the run's scratch path (the key cannot cover it).
#
#   gc_oracle <dir> <moddir> <live-fn> <material...>
#                    the whole leg: gc_oracle_load, else <live-fn> (which
#                    builds and runs gc, writing <dir>/gc.status, gc.out,
#                    gc.err, gc.build) and gc_oracle_store; $TMP is the
#                    scratch path an output must not mention to be stored
#   gc_oracle_key <moddir> <material...>
#                    prints the key: sha256 over `go version`, the GO* run
#                    environment, every file under <moddir> (path and
#                    content) and <material...> (flags, args, stdin file
#                    contents — whatever else the leg depends on)
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

gc_oracle_key() {
    local dir="$1"
    shift
    [ "$GC_ORACLE_DIR" = off ] && { echo none; return 0; }
    {
        echo "gcoracle-v1"
        "$GO" version
        env | grep -E '^(GODEBUG|GOFLAGS|GOEXPERIMENT|GOAMD64|GOARCH|GOOS|CGO_ENABLED|GOMAXPROCS|GOTRACEBACK|GOTOOLCHAIN)=' | LC_ALL=C sort
        printf '%s\n' "$@"
        (cd "$dir" && find . -type f -print0 | LC_ALL=C sort -z | xargs -0 -r sha256sum)
    } | sha256sum | cut -c1-40
}

gc_oracle_load() {
    local e="$GC_ORACLE_DIR/$1" f
    [ "$GC_ORACLE_DIR" = off ] && return 1
    [ -f "$e/gc.status" ] || return 1
    for f in gc.status gc.out gc.err gc.build; do
        [ -f "$e/$f" ] && cp "$e/$f" "$2/$f"
    done
    return 0
}

gc_oracle_store() {
    local key="$1" dir="$2" scratch="$3" st t f
    [ "$GC_ORACLE_DIR" = off ] && return 0
    st=$(cat "$dir/gc.status" 2>/dev/null) || return 0
    [ "$st" = 124 ] && return 0
    for f in gc.out gc.err gc.build; do
        [ -f "$dir/$f" ] && grep -aqF -- "$scratch" "$dir/$f" && return 0
    done
    mkdir -p "$GC_ORACLE_DIR" || return 0
    t=$(mktemp -d "$GC_ORACLE_DIR/.new.XXXXXX") || return 0
    for f in gc.status gc.out gc.err gc.build; do
        [ -f "$dir/$f" ] && cp "$dir/$f" "$t/$f"
    done
    # rename is atomic: a concurrent reader sees the whole entry or none
    mv -T "$t" "$GC_ORACLE_DIR/$key" 2>/dev/null || rm -rf "$t"
}

gc_oracle() {
    local dir="$1" mod="$2" live="$3" key
    shift 3
    key=$(gc_oracle_key "$mod" "$@")
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
