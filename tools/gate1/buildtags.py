#!/usr/bin/env python3
"""Single source of truth for GOOS/GOARCH build-constraint filtering (R20B-05).

gate1 used to inline every ``*_test.go`` file it found, including files
whose ``//go:build`` constraints exclude the target platform (e.g. the
``js && wasm`` ``bytes`` test, which allocates 2 GiB and pins 32-bit
address-space behaviour, was synthesized into a linux/amd64 driver).
``discover.py`` collected the ``//go:build`` lines but nothing ever read
them back.

This module answers the one question both ``discover.py`` (filter at
discovery) and ``synthesize.py`` (re-check before inlining, defense in
depth) need: *is this test file part of package ``pkg`` for target
GOOS/GOARCH, and if not, why?*

The inclusion decision is delegated to the Go toolchain itself --
``go list -e -json -test`` -- so ``//go:build`` lines, legacy
``// +build`` lines, GOOS/GOARCH filename suffixes, ``go1.x`` release
tags, ``unix``/``cgo`` and every other constraint the toolchain
understands are evaluated exactly as ``go test`` would evaluate them.
The human-readable skip reason is derived from the file itself (its
``//go:build`` lines and its filename suffixes) for the discovery JSON.

Any toolchain failure raises :class:`BuildTagError`; callers fail loudly
rather than silently running a wrong-platform test.
"""
import json
import os
import subprocess

GOOS_LIST = {
    "aix", "android", "darwin", "dragonfly", "freebsd", "hurd", "illumos",
    "ios", "js", "linux", "netbsd", "openbsd", "plan9", "solaris",
    "wasip1", "windows",
}
GOARCH_LIST = {
    "386", "amd64", "arm", "arm64", "loong64", "mips", "mips64",
    "mips64le", "mipsle", "ppc64", "ppc64le", "riscv64", "s390x",
    "sparc64", "wasm",
}


class BuildTagError(Exception):
    """Build constraints could not be evaluated (toolchain failure)."""


def go_binary(goroot):
    return os.path.join(goroot, "bin", "go")


def default_target(go_bin):
    """(GOOS, GOARCH) for the host toolchain, via ``go env``."""
    try:
        p = subprocess.run([go_bin, "env", "GOOS", "GOARCH"],
                           capture_output=True, text=True, timeout=60)
    except (OSError, subprocess.TimeoutExpired) as e:
        raise BuildTagError(f"cannot run {go_bin} env: {e}")
    if p.returncode != 0:
        raise BuildTagError(f"{go_bin} env failed: {p.stderr.strip()[:300]}")
    parts = p.stdout.split()
    if len(parts) != 2:
        raise BuildTagError(f"{go_bin} env returned unexpected output: {p.stdout!r}")
    return parts[0], parts[1]


def _decode_all_objects(text):
    """Decode the (possibly multi-object) JSON stream ``go list -json`` emits."""
    dec = json.JSONDecoder()
    objs = []
    idx = 0
    while idx < len(text):
        while idx < len(text) and text[idx].isspace():
            idx += 1
        if idx >= len(text):
            break
        try:
            obj, idx = dec.raw_decode(text, idx)
        except json.JSONDecodeError as e:
            raise BuildTagError(f"cannot parse go list output: {e}")
        objs.append(obj)
    return objs


def go_list_files(go_bin, goroot, pkg, goos, goarch):
    """(included, ignored) basename sets for ``pkg`` at the target.

    ``included`` is GoFiles + CgoFiles + TestGoFiles + XTestGoFiles of the
    package object whose ImportPath is exactly ``pkg``; ``ignored`` is its
    IgnoredGoFiles. ``go list -test`` emits several JSON objects (the
    package, the ``.test`` main package, the test-augmented package); only
    the exact-ImportPath object carries the file lists for the target.
    """
    env = dict(os.environ)
    env["GOOS"] = goos
    env["GOARCH"] = goarch
    env["GOROOT"] = goroot
    try:
        p = subprocess.run([go_bin, "list", "-e", "-json", "-test", pkg],
                           capture_output=True, text=True, timeout=300, env=env)
    except (OSError, subprocess.TimeoutExpired) as e:
        raise BuildTagError(f"cannot run {go_bin} list: {e}")
    if p.returncode != 0:
        raise BuildTagError(
            f"go list -e -json -test {pkg} failed for {goos}/{goarch}: "
            f"{p.stderr.strip()[:500]}")
    objs = _decode_all_objects(p.stdout)
    target = None
    for obj in objs:
        if obj.get("ImportPath") == pkg:
            target = obj
            break
    if target is None:
        raise BuildTagError(
            f"go list -e -json -test {pkg} returned no object for {pkg!r} "
            f"({goos}/{goarch})")
    included = set()
    for key in ("GoFiles", "CgoFiles", "TestGoFiles", "XTestGoFiles"):
        included.update(target.get(key) or [])
    ignored = set(target.get("IgnoredGoFiles") or [])
    return included, ignored


def constraint_lines(path):
    """``//go:build`` / ``// +build`` lines from the file's leading comments."""
    lines = []
    try:
        with open(path, encoding="utf-8", errors="replace") as f:
            for _ in range(50):
                line = f.readline()
                if not line:
                    break
                s = line.rstrip("\n")
                if s.startswith("//go:build") or s.startswith("// +build"):
                    lines.append(s.strip())
                elif s == "" or s.startswith("//"):
                    continue
                else:
                    break
    except OSError:
        return []
    return lines


def filename_suffix(path):
    """GOOS/GOARCH filename suffix (e.g. ``_js_wasm``) or None.

    Go filename constraints are trailing ``_GOOS`` / ``_GOARCH`` /
    ``_GOOS_GOARCH`` components before ``.go`` / ``_test.go``.
    """
    base = os.path.basename(path)
    stem = base
    for suffix in ("_test.go", ".go"):
        if stem.endswith(suffix):
            stem = stem[:-len(suffix)]
            break
    parts = stem.split("_")
    tags = []
    i = len(parts)
    while i > 0 and (parts[i - 1] in GOOS_LIST or parts[i - 1] in GOARCH_LIST):
        tags.append(parts[i - 1])
        i -= 1
    if not tags:
        return None
    tags.reverse()
    return "_" + "_".join(tags)


def skip_reason(path, goos, goarch):
    """Human-readable reason a file is excluded for the target."""
    bits = []
    tags = constraint_lines(path)
    if tags:
        bits.append("excluded by " + "; ".join(tags))
    suffix = filename_suffix(path)
    if suffix:
        bits.append(f"filename suffix {suffix}")
    if not bits:
        bits.append("excluded by build constraints")
    return "; ".join(bits) + f" (target {goos}/{goarch})"


def classify_files(go_bin, goroot, pkg, paths, goos, goarch):
    """Split ``paths`` into (included_paths, {path: skip_reason}) for target.

    A file is skipped iff ``go list`` lists it under IgnoredGoFiles. A
    file listed in neither set (the toolchain does not know it at all for
    this target) is skipped too, with an explicit reason -- failing safe
    toward exclusion, never toward running a wrong-platform test.
    """
    included, ignored = go_list_files(go_bin, goroot, pkg, goos, goarch)
    kept = []
    skipped = {}
    for path in paths:
        base = os.path.basename(path)
        if base in ignored:
            skipped[path] = skip_reason(path, goos, goarch)
        elif base in included:
            kept.append(path)
        else:
            skipped[path] = (
                f"not selected by go list for target {goos}/{goarch} "
                f"(target {goos}/{goarch})")
    return kept, skipped
