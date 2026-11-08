#!/usr/bin/env python3
"""Mechanical markdown link checker (project standing order, 2026-10-12).

Fails on:
  - broken relative links (target file/dir does not exist on disk,
    resolved relative to the doc's own directory),
  - dangling #anchors (no matching heading in the target file,
    GitHub slug rules),
  - dead external URLs (non-2xx/3xx on HEAD, 10s timeout). Transient
    transport failures (connection reset, timeout, 5xx/429) are retried
    with backoff before a URL is declared dead.

External verdicts are tri-state: True (alive), False (dead), TRANSIENT
(unverifiable — every web attempt failed transiently and the
authoritative fallback below could not settle it). A TRANSIENT link is
still a failure (exit 1, listed separately as "unverifiable"), so the
tripwire stays intact; it only changes the diagnosis, never the verdict.

GitHub API fallback (2026-11-08, CI run 42462394721): github.com's web
frontend intermittently answers 504/429 for URLs that exist — the same
SHA passed markdown-links 6h before failing, api.github.com said all 8
reported URLs exist, and live probes showed 504/429 on 9/10 URLs
including github.com/CAFxX/graalgo itself, with the single 200 flipping
between rounds. A dead frontend must never red master again, so when
(and only when) every web attempt failed transiently, a github.com URL
is re-checked against the authoritative REST API: API 200 means the
target exists (alive), API 404/410/422 means it does not (dead), and
anything else leaves the URL TRANSIENT. A web 404 still fails fast and
never consults the API; a web 200 never costs an API call. The API
token comes from GITHUB_TOKEN when set (the workflow passes the
built-in token); without it the fallback still works unauthenticated
at the public rate limit.

Usage:
  python3 .github/scripts/check_markdown_links.py [--skip-external] [root]

Links inside fenced code blocks are ignored (examples, not references).
Every file/directory named in our docs must be a working relative link;
this script is the mechanical enforcement.
"""

import http.client
import json
import os
import re
import socket
import sys
import time
import urllib.error
import urllib.parse
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


# Transport failures that mean "no answer arrived at all" — the host is
# alive but this request was lost on the way (connection reset before
# any response bytes, timeout, 5xx/429). These are retried with backoff;
# only a failure that survives every attempt counts as a dead link.
#
# Mechanism, observed 2026-11-06 (CI run 42130033841): go.dev/cl
# intermittently resets connections with zero bytes returned
# (IncompleteRead(0 bytes read)) on ~15% of requests (8/54 in a local
# probe, across HEAD/GET and both user agents), and the same URL passes
# in one file and fails in another within a single run — so the reset
# is per-request flakiness at the Google frontend, not a dead link.
# The old code made 3 immediate attempts and declared the URL dead if
# all 3 were reset, which a short outage window could do to several
# URLs at once.
_TRANSIENT_EXC = (
    http.client.IncompleteRead,      # server closed connection, 0 bytes read
    http.client.RemoteDisconnected,  # connection reset without response
    http.client.BadStatusLine,       # empty/garbled reply (same family)
    TimeoutError,                    # socket.timeout is an alias of this
    socket.timeout,
    ConnectionError,                 # ConnectionResetError, BrokenPipeError, ...
)


class _Transient:
    """Sentinel for "unverifiable": every check failed transiently, so
    the URL is neither proven alive nor proven dead. Falsy, so any
    caller that only tests truthiness fails closed."""

    def __repr__(self):
        return "TRANSIENT"

    def __bool__(self):
        return False


TRANSIENT = _Transient()


def _is_transient(exc):
    """True if the failure is a lost request rather than a dead URL:
    a 429/5xx answer, or a transport error raised before any response
    arrived. A definitive 4xx (e.g. 404/410) is not transient."""
    if isinstance(exc, urllib.error.HTTPError):
        return exc.code == 429 or 500 <= exc.code < 600
    if isinstance(exc, urllib.error.URLError):
        return isinstance(exc.reason, _TRANSIENT_EXC)
    return isinstance(exc, _TRANSIENT_EXC)


def _github_api_endpoint(url):
    """Map a github.com URL to the authoritative REST endpoint whose 200
    proves the target exists, or None when the shape has no mapping.
    Unmapped shapes get NO fallback (conservative): the web verdict
    stands and the URL is reported unverifiable rather than alive.

    Mapped shapes (owner/repo may contain dots, dashes, underscores):
      repo root            -> GET /repos/{o}/{r}
      blob/tree/raw + path -> GET /repos/{o}/{r}/contents/{path}?ref={ref}
      tree + ref only      -> GET /repos/{o}/{r}
      issues/{n}           -> GET /repos/{o}/{r}/issues/{n}
      pull/{n}             -> GET /repos/{o}/{r}/pulls/{n}
      commit/{sha}         -> GET /repos/{o}/{r}/commits/{sha}
      commits[/{ref}]      -> GET /repos/{o}/{r}/commits[?sha={ref}]
      releases/download/{tag}/{asset}
                           -> ("release-asset", o, r, tag, asset): the
                              release-by-tag endpoint is fetched and the
                              asset list is checked for the file name.
    """
    parsed = urllib.parse.urlparse(url)
    if parsed.netloc.lower() not in ("github.com", "www.github.com"):
        return None
    parts = [p for p in parsed.path.split("/") if p]
    if len(parts) < 2:
        return None
    owner = parts[0]
    repo = parts[1][:-4] if parts[1].endswith(".git") else parts[1]
    rest = parts[2:]
    if not rest:
        return f"https://api.github.com/repos/{owner}/{repo}"
    kind = rest[0]
    if kind in ("blob", "tree", "raw") and len(rest) >= 3:
        ref, path = rest[1], "/".join(rest[2:])
        return ("https://api.github.com/repos/%s/%s/contents/%s?ref=%s"
                % (owner, repo, path, urllib.parse.quote(ref, safe="")))
    if kind == "tree" and len(rest) == 2:
        return f"https://api.github.com/repos/{owner}/{repo}"
    if kind == "issues" and len(rest) == 2 and rest[1].isdigit():
        return f"https://api.github.com/repos/{owner}/{repo}/issues/{rest[1]}"
    if kind == "pull" and len(rest) == 2 and rest[1].isdigit():
        return f"https://api.github.com/repos/{owner}/{repo}/pulls/{rest[1]}"
    if kind == "commit" and len(rest) == 2:
        return f"https://api.github.com/repos/{owner}/{repo}/commits/{rest[1]}"
    if kind == "commits" and len(rest) == 1:
        return f"https://api.github.com/repos/{owner}/{repo}/commits"
    if kind == "commits" and len(rest) == 2:
        return ("https://api.github.com/repos/%s/%s/commits?sha=%s"
                % (owner, repo, urllib.parse.quote(rest[1], safe="")))
    if (kind == "releases" and len(rest) == 4 and rest[1] == "download"):
        return ("release-asset", owner, repo, rest[2], rest[3])
    return None


def _github_api_get(api_url):
    """One authenticated GET against api.github.com. Returns
    (status, body); status is None when no answer arrived at all."""
    headers = {"Accept": "application/vnd.github+json",
               "User-Agent": "graalgo-linkcheck/1.0"}
    token = os.environ.get("GITHUB_TOKEN")
    if token:
        headers["Authorization"] = f"Bearer {token}"
    req = urllib.request.Request(api_url, method="GET", headers=headers)
    try:
        with urllib.request.urlopen(req, timeout=10) as r:
            return r.status, r.read()
    except urllib.error.HTTPError as e:
        return e.code, b""
    except Exception:
        return None, b""


def _github_api_alive(url):
    """Authoritative liveness verdict for a github.com URL via the REST
    API. True = the API says the target exists (200). False = the API
    says it does not (404/410/422). None = inconclusive: unmapped URL
    shape, no API answer, or an API status that proves nothing
    (401/403/429/5xx)."""
    endpoint = _github_api_endpoint(url)
    if endpoint is None:
        return None
    if isinstance(endpoint, tuple) and endpoint[0] == "release-asset":
        _, owner, repo, tag, asset = endpoint
        api_url = ("https://api.github.com/repos/%s/%s/releases/tags/%s"
                   % (owner, repo, urllib.parse.quote(tag, safe="")))
        status, body = _github_api_get(api_url)
        if status == 200:
            try:
                names = [a.get("name")
                         for a in json.loads(body).get("assets", [])]
            except (ValueError, AttributeError):
                return None
            return asset in names
        if status in (404, 410, 422):
            return False
        return None
    status, _ = _github_api_get(endpoint)
    if status == 200:
        return True
    if status in (404, 410, 422):
        return False
    return None


def check_external(url, sleep=None):
    """Tri-state liveness check: True (alive), False (dead), TRANSIENT
    (unverifiable). `sleep` resolves at call time so tests can stub it
    via the time module. A web 200 never costs an API call; a web 404
    fails fast and never consults the API; the API fallback runs only
    after a round whose failures were all transient."""
    if sleep is None:
        sleep = time.sleep
    if url in BOT_PROTECTED_URLS:
        return True  # manually verified live; bot protection blocks automation
    # Some sites 403 HEAD or non-browser user agents (bot protection);
    # retry with GET + browser UA before declaring dead.
    attempts = [
        ("HEAD", "graalgo-linkcheck/1.0"),
        ("GET", "graalgo-linkcheck/1.0"),
        ("GET", BROWSER_UA),
    ]
    # Two rounds over the (method, UA) ladder. A transient failure
    # sleeps with backoff before the next attempt so a short outage
    # window cannot swallow the whole ladder; a second round runs only
    # if every failure was transient. A definitive failure (4xx other
    # than 429) moves to the next attempt immediately and never triggers
    # a second round, so a genuinely dead link still fails fast.
    for round_no in range(2):
        saw_transient = False
        for i, (method, ua) in enumerate(attempts):
            req = urllib.request.Request(url, method=method,
                                         headers={"User-Agent": ua})
            try:
                with urllib.request.urlopen(req, timeout=10) as r:
                    if 200 <= r.status < 400:
                        return True
            except Exception as e:
                if _is_transient(e):
                    saw_transient = True
                    if i < len(attempts) - 1:
                        sleep(2.0)
                continue
        if not saw_transient:
            return False
        # Every failure this round was transient: the web frontend
        # cannot be trusted for this URL right now. Ask the
        # authoritative API (github.com only) before spending another
        # round on a degraded frontend; an API verdict settles the
        # URL with no second round. If the API is inconclusive too,
        # sleep and try the web once more; after the second round the
        # URL stays TRANSIENT so the run still fails, with the right
        # diagnosis.
        api_result = _github_api_alive(url)
        if api_result is True:
            return True
        if api_result is False:
            return False
        if round_no == 0:
            sleep(10.0)
    return TRANSIENT


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
    transient = []
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
                    if not skip_external:
                        verdict = check_external(target)
                        if verdict is False:
                            errors.append(f"{md}:{lineno}: dead URL {target}")
                        elif verdict is TRANSIENT:
                            transient.append(
                                f"{md}:{lineno}: unverifiable URL {target}")
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

    if errors or transient:
        if errors:
            print(f"{len(errors)} broken link(s) in {len(md_files)} markdown files:")
            for e in errors:
                print("  " + e)
        if transient:
            print(f"{len(transient)} unverifiable link(s) in "
                  f"{len(md_files)} markdown files "
                  "(transient failures — re-run to confirm):")
            for t in transient:
                print("  " + t)
        return 1
    print(f"OK: {len(md_files)} markdown files, no broken links"
          + (" (external URLs skipped)" if skip_external else ""))
    return 0


if __name__ == "__main__":
    sys.exit(main())
