# graalgo-fuzz GHA image: Go 1.27.1 + GraalVM JDK 25 + graalgo sources.
#
# The graalgo sources are baked as a slim tarball of the Phase-4 tree:
#   graalgo/{Makefile,
#     internal/backend/truffle/{src,lib,Makefile},
#     internal/backend/{runtime,interop}/{src,Makefile},
#     internal/backend/{graalgo-run,graalgo-sync-run},
#     internal/frontend, tools/fuzz-soak}
# (the pre-Phase-4 layout was deleted 2026-10-14; see
# gha/README.md for the exact tar command). Rebuild the tarball and
# re-push the Dockerfile when graalgo changes.
#
# Build via the fanout build-image.yml workflow (push trigger on
# projects/graalgo-fuzz/Dockerfile), or locally with docker build.
FROM ubuntu:24.04

ENV DEBIAN_FRONTEND=noninteractive

RUN apt-get update && apt-get install -y --no-install-recommends \
        curl ca-certificates python3 git build-essential \
    && rm -rf /var/lib/apt/lists/*

# Go 1.27.1 (pinned to graalgo toolchain).
RUN curl -fsSL https://go.dev/dl/go1.27.1.linux-amd64.tar.gz \
    | tar -xz -C /usr/local
ENV PATH=/usr/local/go/bin:$PATH

# GraalVM JDK 25 (pinned: Oracle GraalVM 25.0.4.1.1 LTS).
# NOTE: update URL when the pinned version changes.
RUN curl -fsSL -o /tmp/graalvm.tar.gz \
        https://download.oracle.com/graalvm/25/latest/graalvm-jdk-25_linux-x64_bin.tar.gz \
    && mkdir -p /opt/graalvm \
    && tar -xz -C /opt/graalvm --strip-components=1 -f /tmp/graalvm.tar.gz \
    && rm /tmp/graalvm.tar.gz
ENV JAVA_HOME=/opt/graalvm
ENV PATH=/opt/graalvm/bin:$PATH
ENV GRAALVM_HOME=/opt/graalvm

# graalgo slim sources (tarball committed alongside this Dockerfile).
# Layout inside tarball: graalgo/{Makefile,
#   internal/backend/truffle/{src,lib,Makefile},
#   internal/backend/{runtime,interop}/{src,Makefile},
#   internal/backend/{graalgo-run,graalgo-sync-run},
#   internal/frontend, tools/fuzz-soak}
COPY graalgo-slim.tar.gz /tmp/
RUN mkdir -p /opt && tar -xz -C /opt -f /tmp/graalgo-slim.tar.gz \
    && rm /tmp/graalgo-slim.tar.gz

# Pre-build the backend so shards start immediately. Root `make build`
# builds the truffle, runtime and interop Java modules AND the
# gofrontend binary (via the truffle Makefile's $(GOFRONTEND)
# dependency). GO must point at the image Go: the Makefiles default
# GO to ~/tools/go/bin/go, which does not exist in this image.
WORKDIR /opt/graalgo
RUN make build GO=/usr/local/go/bin/go && echo "graalgo pre-build OK"

# Shard contract env (set by dispatch.yml at runtime).
ENV OUT_DIR=/out SHARD_IDX=0 SHARD_COUNT=1

# Fuzz driver + generator baked from the tarball
# (tools/fuzz-soak/gha/driver/fuzz_driver.py + gen/generator.py).
WORKDIR /work
CMD ["python3", "--version"]
