# graalgo Phase-0 environment (pinned 2026-10-03)

- Go toolchain: `~/tools/go` — `go version go1.27.1 linux/amd64`
  (`export PATH=~/tools/go/bin:$PATH`)
- GraalVM: `~/tools/graalvm-jdk-25` — Oracle GraalVM 25.0.4+7.1 (JDK 25 LTS, JVMCI)
  (`export JAVA_HOME=~/tools/graalvm-jdk-25; export PATH=$JAVA_HOME/bin:$PATH`)
  - Downloaded from `https://download.oracle.com/graalvm/25/latest/graalvm-jdk-25_linux-x64_bin.tar.gz`
  - sha256 `76007c309f821aaf435bce63162ea0395587fc77350801c81643fe7feea37276`
    (verified against the published `.sha256`; note it is a bare hash, not `sha256sum -c` format)
- OS: linux/amd64
- Requirements CLI: `~/workspace/graalgo/tools/req` (`go run . --root ~/workspace/graalgo`)
