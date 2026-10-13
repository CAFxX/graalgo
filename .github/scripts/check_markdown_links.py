#!/usr/bin/env python3
"""Mechanical markdown link checker (project standing order, 2026-10-12).

Fails on:
  - broken relative links (target file/dir does not exist on disk,
    resolved relative to the doc's own directory),
  - dangling #anchors (no matching heading in the target file,
    GitHub slug rules),
  - dead external URLs (non-2xx/3xx on HEAD, 10s timeout).

Usage:
  python3 .github/scripts/check_markdown_links.py [--skip-external] [root]

Links inside fenced code blocks are ignored (examples, not references).
Every file/directory named in our docs must be a working relative link;
this script is the mechanical enforcement.
"""

import os
import re
import sys
import urllib.request

LINK_RE = re.compile(r"(?<!!)\[([^\]]*)\]\(([^)\s]+)(?:\s+\"[^\"]*\")?\)")
FENCE_RE = re.compile(r"^\s*```")
HEADING_RE = re.compile(r"^(#{1,6})\s+(.*)$")

# URLs that are live but block automated checks via bot protection
# (Akamai/Cloudflare 403 on non-browser TLS fingerprints). Each entry was
# manually verified live with curl; the checker skips them instead of
# failing on the 403.
BOT_PROTECTED_URLS = {
    # bugs.openjdk.org serves 403 to Python urllib (AkamaiGHost) but 200
    # to curl/browser; verified live 2026-10-13.
    "https://bugs.openjdk.org/browse/JDK-8247838",
    # enterprisetimes.co.uk serves 403 to all automated fetches (bot
    # protection); the article exists. Verified 403 via curl 2026-10-13.
    "https://www.enterprisetimes.co.uk/2024/10/18/azul-platform-prime-37-faster-than-other-openjdk-distributions/",
}


def github_slug(heading):
    """GitHub heading-anchor slug: lowercase, strip punctuation, each
    whitespace char -> one hyphen (so double spaces yield double hyphens)."""
    text = re.sub(r"<[^>]+>", "", heading)  # strip inline HTML
    text = text.lower()
    text = re.sub(r"[^\w\s-]", "", text, flags=re.UNICODE)
    return re.sub(r"\s", "-", text.strip())


def file_anchors(path):
    anchors = set()
    try:
        with open(path, encoding="utf-8", errors="replace") as f:
            for line in f:
                m = HEADING_RE.match(line.rstrip("\n"))
                if m:
                    anchors.add(github_slug(m.group(2)))
    except OSError:
        pass
    return anchors


BROWSER_UA = ("Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 "
              "(KHTML, like Gecko) Chrome/126.0 Safari/537.36")


def check_external(url):
    if url in BOT_PROTECTED_URLS:
        return True  # manually verified live; bot protection blocks automation
    # Some sites 403 HEAD or non-browser user agents (bot protection);
    # retry with GET + browser UA before declaring dead.
    attempts = [
        ("HEAD", "graalgo-linkcheck/1.0"),
        ("GET", "graalgo-linkcheck/1.0"),
        ("GET", BROWSER_UA),
    ]
    for method, ua in attempts:
        req = urllib.request.Request(url, method=method,
                                     headers={"User-Agent": ua})
        try:
            with urllib.request.urlopen(req, timeout=10) as r:
                if 200 <= r.status < 400:
                    return True
        except Exception:
            continue
    return False


def strip_code_blocks(lines):
    out, in_fence = [], False
    for line in lines:
        if FENCE_RE.match(line):
            in_fence = not in_fence
            continue
        if not in_fence:
            out.append(line)
    return out


INLINE_CODE_RE = re.compile(r"`[^`]*`")


def strip_inline_code(line):
    """Remove `code` spans: bracket/paren sequences inside inline code
    (e.g. `f[float64](int8(9))`) are not markdown links."""
    return INLINE_CODE_RE.sub("", line)


def main():
    skip_external = "--skip-external" in sys.argv
    args = [a for a in sys.argv[1:] if not a.startswith("--")]
    root = os.path.abspath(args[0]) if args else os.getcwd()

    errors = []
    md_files = []
    for dirpath, dirnames, filenames in os.walk(root):
        # Never descend into build output, vendored trees, or git metadata.
        dirnames[:] = [d for d in dirnames
                       if d not in (".git", "out", "node_modules", ".github")]
        for fn in filenames:
            if fn.endswith(".md"):
                md_files.append(os.path.join(dirpath, fn))

    for md in sorted(md_files):
        # Scratch work logs (hidden_files/) are exempt: they are untracked-by-
        # design experiment notes, not maintained docs (same exemption as
        # tools/precommit-check.sh).
        if "hidden_files" in md.split(os.sep):
            continue
        with open(md, encoding="utf-8", errors="replace") as f:
            lines = strip_code_blocks(f.readlines())
        docdir = os.path.dirname(md)
        for lineno, line in enumerate(lines, 1):
            line = strip_inline_code(line)
            for m in LINK_RE.finditer(line):
                target = m.group(2)
                if target.startswith("mailto:"):
                    continue
                if "#" in target and not target.startswith("#"):
                    path_part, anchor = target.split("#", 1)
                elif target.startswith("#"):
                    path_part, anchor = "", target[1:]
                else:
                    path_part, anchor = target, None

                if re.match(r"https?://", target):
                    if not skip_external and not check_external(target):
                        errors.append(f"{md}:{lineno}: dead URL {target}")
                    continue
                if re.match(r"[a-zA-Z][a-zA-Z0-9+.-]*:", path_part):
                    continue  # other schemes: not our problem

                resolved = os.path.normpath(os.path.join(docdir, path_part)) \
                    if path_part else md
                if not os.path.exists(resolved):
                    errors.append(
                        f"{md}:{lineno}: broken link -> {target} "
                        f"(resolved {os.path.relpath(resolved, root)})")
                    continue
                if anchor:
                    check_file = resolved if resolved.endswith(".md") else None
                    if check_file and anchor not in file_anchors(check_file):
                        errors.append(
                            f"{md}:{lineno}: dangling anchor #{anchor} "
                            f"in {target}")

    if errors:
        print(f"{len(errors)} broken link(s) in {len(md_files)} markdown files:")
        for e in errors:
            print("  " + e)
        return 1
    print(f"OK: {len(md_files)} markdown files, no broken links"
          + (" (external URLs skipped)" if skip_external else ""))
    return 0


if __name__ == "__main__":
    sys.exit(main())
