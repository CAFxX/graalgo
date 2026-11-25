# shellcheck shell=bash
# test/lib/batchrun.sh — the graalgo leg of the m1* gates in one JVM
# (docs/dev/test-profile.md, round 3). Sourced after test/lib/infra.sh.
#
# A gate's graalgo leg was one JVM start per program (~0.4 s wall, ~0.9 s
# CPU before the program runs a line). Here the programs a gate built run in
# one JVM, each in a fresh polyglot Context (graalgo.launcher.BundleBatchMain
# through `graalgo-bundle-run --batch`: what production does per program,
# with the program's stdin/stdout/stderr on descriptors 0-2, its working
# directory and os.Args, its timeout as `timeout` gives it: status 124).
#
#   batch_init                 start an empty batch (BATCH_DIR under $TMP)
#   batch_add <id> <cwd> <argv0> <stdin> <stdout> <stderr> <timeout> [arg...]
#                              queue a program (argv0 as the per-process leg
#                              invokes it from <cwd>)
#   batch_run <runner> <proc_fn>
#                              run the queue in one JVM; then `<proc_fn> <id>`
#                              (the caller's per-process leg; returns the
#                              exit status) for every program the batch did
#                              not finish: the JVM died or a cancel hung —
#                              that program and every one after it. Sets
#                              BATCH_STATUS[<id>] for every queued id.
#   batch_isolated <file|dir>  true when the program must run in its own
#                              process (below)
#
# What stays per process (the caller runs its leg directly, never queues):
# a program with its own environment (the batch JVM has one environ), and
# one whose source names a process-global mutator that outlives its context
# or ends the process (batch_isolated: rlimits, umask, credentials, session,
# signal disposition or delivery, a raw exit). The leak/cancel/tools probes
# and the per-process launch tests are their own JVMs anyway.
#
# Env: GRAALGO_BATCH=off runs every queued program through <proc_fn>
#      (the per-process path, for equivalence checks and debugging);
#      BATCH_SAVE=<dir> keeps every queued program's stdout, stderr (the
#      JVM's WARNING: lines dropped, as the gates drop them) and status as
#      <dir>/<gate>-<run>.<id>.{out,err,status}: run a gate both ways
#      and diff the two dirs.

BATCH_ISOLATED_RE='Setrlimit|Prlimit|Umask|Set(e|re|res)?[ug]id|Setgroups|Setsid|Setpgid|signal\.(Notify|Ignore|Reset)|syscall\.(Kill|Tgkill|Tkill|Exit)\b|SYS_EXIT|SYS_KILL|SYS_TGKILL|Chroot|Unshare|Prctl'

batch_isolated() {
    local p="$1"
    if [ -d "$p" ]; then
        grep -rqE --include='*.go' "$BATCH_ISOLATED_RE" "$p"
    else
        grep -qE "$BATCH_ISOLATED_RE" "$p"
    fi
}

batch_init() {
    BATCH_DIR="$TMP/batch"
    rm -rf "$BATCH_DIR"
    mkdir -p "$BATCH_DIR"
    : > "$BATCH_DIR/manifest"
    BATCH_IDS=()
    BATCH_TOTAL_TIMEOUT=0
    declare -gA BATCH_STATUS=()
}

batch_add() {
    local id="$1" f
    for f in "$@"; do
        case "$f" in *$'\t'*|*$'\n'*) infra_die "batch_add: a tab or newline in a field of $id" ;; esac
    done
    BATCH_IDS+=("$id")
    BATCH_TOTAL_TIMEOUT=$((BATCH_TOTAL_TIMEOUT + $7))
    (IFS=$'\t'; printf '%s\n' "$*") >> "$BATCH_DIR/manifest"
}

batch_run() {
    local runner="$1" proc_fn="$2" id st status=0 done_n rest=0
    [ "${#BATCH_IDS[@]}" -eq 0 ] && return 0
    : > "$BATCH_DIR/results"
    if [ "${GRAALGO_BATCH:-on}" != off ]; then
        # The outer bound only guards the watchdog itself: each program has
        # its own timeout inside, and a hung cancel halts the JVM.
        timeout "$((BATCH_TOTAL_TIMEOUT + 60))" "$runner" --batch "$BATCH_DIR/manifest" "$BATCH_DIR/results" \
            >"$BATCH_DIR/jvm.out" 2>"$BATCH_DIR/jvm.err" </dev/null || status=$?
        while read -r tag id st; do
            [ "$tag" = END ] && BATCH_STATUS[$id]=$st
        done < "$BATCH_DIR/results"
    fi
    done_n=${#BATCH_STATUS[@]}
    for id in "${BATCH_IDS[@]}"; do
        [ -n "${BATCH_STATUS[$id]+x}" ] && continue
        rest=$((rest + 1))
        st=0
        "$proc_fn" "$id" || st=$?
        BATCH_STATUS[$id]=$st
    done
    if [ "${GRAALGO_BATCH:-on}" != off ] && [ "$rest" -gt 0 ]; then
        echo "batch: the JVM stopped (exit $status) after $done_n of ${#BATCH_IDS[@]} programs;" \
            "ran the other $rest in their own processes" >&2
        grep -v '^WARNING: ' "$BATCH_DIR/jvm.err" | tail -5 | sed 's/^/    /' >&2
    elif [ "${GRAALGO_BATCH:-on}" != off ]; then
        echo "batch: $done_n programs in one JVM" >&2
    fi
    if [ -n "${BATCH_SAVE:-}" ]; then
        local tag f1 f2 f3 out err rest_f k=1
        mkdir -p "$BATCH_SAVE"
        tag=$(basename "$TMP")
        tag=${tag%.*}
        while [ -e "$BATCH_SAVE/$tag-$k.done" ]; do k=$((k + 1)); done
        tag="$tag-$k"
        : > "$BATCH_SAVE/$tag.done"
        while IFS=$'\t' read -r id f1 f2 f3 out err rest_f; do
            cp "$out" "$BATCH_SAVE/$tag.$id.out"
            grep -av '^WARNING: \|^NOTE: Picked up' "$err" > "$BATCH_SAVE/$tag.$id.err"
            echo "${BATCH_STATUS[$id]}" > "$BATCH_SAVE/$tag.$id.status"
        done < "$BATCH_DIR/manifest"
    fi
    return 0
}
