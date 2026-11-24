# test/lib/toolexec.sh — the graalgo -toolexec toolchain for test scripts
# (sourced after test/lib/infra.sh; docs/arch/toolexec-design.md §2, §5.1).
#
#   toolexec_setup   after infra_setup_go and infra_mktemp: builds
#                    graalgo-tool, graalgo-stub and graalgo-goroot with the
#                    tracked gc toolchain into $TMP/bin and writes the
#                    `go build -overlay` file that applies graalgo's
#                    standard-library fork (stdlib/) to the toolchain's
#                    GOROOT. Sets TOOLEXEC_TOOL, TOOLEXEC_STUB and
#                    TOOLEXEC_OVERLAY. Last, jvm_cds_setup
#                    (test/lib/jvmcds.sh): the runner's JVMs map a
#                    class-data-sharing archive of the truffle classes.
#   toolexec_build <out> <runner>
#                    `go build` of the package in the current directory
#                    through graalgo-tool with the fork applied; the linked
#                    program runs on <runner>.
#
# A build through graalgo-tool without the fork fails at link: the upstream
# runtime's assembly has no intrinsics (design §5.3).

toolexec_setup() {
    local root="$GRAALGO_ROOT" stub_id
    stub_id="GRAALGO-STUB-BUILDID/GRAALGO-STUB-BUILDID/GRAALGO-STUB-BUILDID/GRAALGO-STUB-BUILDID"
    mkdir -p "$TMP/bin" "$TMP/gotmp" "$TMP/fork"
    export GOTMPDIR="$TMP/gotmp" GOFLAGS= CGO_ENABLED=0 GOTOOLCHAIN=local
    TOOLEXEC_TOOL="$TMP/bin/graalgo-tool"
    TOOLEXEC_STUB="$TMP/bin/graalgo-stub"
    TOOLEXEC_OVERLAY="$TMP/fork/overlay.json"
    ( cd "$root/internal/toolexec" \
        && "$GO" build -o "$TOOLEXEC_TOOL" ./cmd/graalgo-tool \
        && "$GO" build -ldflags="-buildid=$stub_id" -o "$TOOLEXEC_STUB" ./cmd/graalgo-stub \
        && "$GO" build -o "$TMP/bin/graalgo-goroot" ./cmd/graalgo-goroot ) \
        || infra_die "cannot build graalgo-tool / graalgo-stub / graalgo-goroot"
    "$TMP/bin/graalgo-goroot" -stdlib "$root/stdlib" -upstream "$("$GO" env GOROOT)" \
        -overlay "$TOOLEXEC_OVERLAY" -work "$TMP/fork/work" \
        || infra_die "cannot apply the standard-library fork (stdlib/)"
    # shellcheck source=jvmcds.sh
    . "$root/test/lib/jvmcds.sh"
    GRAALVM_HOME="${GRAALVM_HOME:-$HOME/tools/graalvm-jdk-25}"
    jvm_cds_setup bundle "$root/internal/backend/graalgo-bundle-run"
}

toolexec_build() {
    GRAALGO_STUB="$TOOLEXEC_STUB" GRAALGO_RUNNER="$2" \
        "$GO" build -overlay "$TOOLEXEC_OVERLAY" -toolexec "$TOOLEXEC_TOOL" -o "$1" .
}
