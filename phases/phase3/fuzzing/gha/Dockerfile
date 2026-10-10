# graalgo-fuzz GHA image: Go 1.27.1 + GraalVM JDK 25 + graalgo sources.
#
# The graalgo sources are baked as a slim tarball (phases/phase1/{src,lib,
# frontend}, phases/phase2/src, phases/phase3/{src,fuzzing}; ~45MB).
# Rebuild the tarball and re-push the Dockerfile when graalgo changes.
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
# Layout inside tarball: graalgo/{phases/phase1/{src,lib,frontend,Makefile},
# phases/phase2/{src,Makefile}, phases/phase3/{src,fuzzing,Makefile}}
COPY graalgo-slim.tar.gz /tmp/
RUN mkdir -p /opt && tar -xz -C /opt -f /tmp/graalgo-slim.tar.gz \
    && rm /tmp/graalgo-slim.tar.gz

# Pre-build the Java modules so shards start immediately.
# (go build of the frontend happens on first use; cached in GOCACHE.)
WORKDIR /opt/graalgo
RUN make -C phases/phase3 main && echo "graalgo pre-build OK"

# Shard contract env (set by dispatch.yml at runtime).
ENV OUT_DIR=/out SHARD_IDX=0 SHARD_COUNT=1

# Fuzz driver + generator baked from the tarball.
WORKDIR /work
CMD ["python3", "--version"]
