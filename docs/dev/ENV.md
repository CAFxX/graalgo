# graalgo development environment

Pinned 2026-10-03; host layout updated 2026-11-18 for the current box.

- Go toolchain: `~/tools/go` — `go version go1.27.1 linux/amd64`
  (`export PATH=~/tools/go/bin:$PATH`; Makefiles honor `GO=`).
- GraalVM: `~/tools/graalvm-jdk-25` — Oracle GraalVM 25.0.4+7.1 (JDK 25 LTS, JVMCI)
  (`export JAVA_HOME=~/tools/graalvm-jdk-25; export PATH=$JAVA_HOME/bin:$PATH`;
  Makefiles honor `GRAALVM_HOME=`).
  - Downloaded from `https://download.oracle.com/graalvm/25/latest/graalvm-jdk-25_linux-x64_bin.tar.gz`
  - sha256 `76007c309f821aaf435bce63162ea0395587fc77350801c81643fe7feea37276`
    (verified against the published `.sha256`; note it is a bare hash, not `sha256sum -c` format)
- OS: linux/amd64.
- Repository: integration checkout `~/dev/graalgo` (`master`, fast-forward
  only); one worktree per lane under `~/dev/graalgo-wt/<lane>`.
- Vendored stdlib tree (external, not in the repo): `~/dev/graalgo-stdlib/<goVersion>/`
  ([tools/stdlib-vendor/](../../tools/stdlib-vendor/)).
- Requirements CLI: [tools/req/](../../tools/req/) (`cd tools/req && go run . lint`;
  it walks up to find [requirements/](../../requirements/); `make check` runs it).
- Resource limits on this shared host: see [AGENTS.md](../../AGENTS.md) §6.
