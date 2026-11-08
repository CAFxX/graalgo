#!/usr/bin/env python3
"""Regression tests for check_markdown_links.py retry logic.

Run: python3 .github/scripts/test_check_markdown_links.py

The 2026-11-06 failure (CI run 42130033841, markdown-links job): three
go.dev/cl URLs were reported dead although every one of them is live.
Root cause: go.dev/cl intermittently resets connections with zero bytes
returned (IncompleteRead(0 bytes read)) on ~15% of requests (8/54 in a
local probe, across HEAD/GET and both user agents), and the same URL
passed in one file and failed in another within a single run — so
the reset is per-request flakiness at the Google frontend, not a dead
link. The pre-fix checker made 3 immediate attempts and declared the
URL dead if all 3 were reset, which a short outage window could do to
several URLs at once.

The 2026-11-08 failure (CI run 42462394721, markdown-links job): eight
github.com URLs were reported dead although every one of them exists
(api.github.com: all 8 repos 200, all 6 blob paths 200). Root cause:
github.com's web frontend intermittently answers 504/429 for URLs
that exist — the same SHA passed 6h before failing, and live probes
showed 504/429 on 9/10 URLs including github.com/CAFxX/graalgo
itself. The fix adds an authoritative GitHub API fallback, consulted
only after a round whose failures were all transient, and a tri-state
verdict (True / False / TRANSIENT) so an unverifiable URL is reported
as unverifiable rather than dead — while still failing the run.

Fixtures below replay both exact signatures plus the neighboring cases
the fix must not change: a persistently reset URL is now TRANSIENT
(still failing in main()), a 404 still fails fast with no second
round and never consults the API, and the HEAD-403 -> GET-200 ladder
for bot-protected hosts still works.

Positive controls are URLs that MUST still fail: the checker is a
tripwire, not a rubber stamp. The fixtures use a fake urlopen/sleep
so the tests run offline in milliseconds; a live smoke check against
go.dev/cl or github.com is NOT asserted here (network-dependent).
"""

import http.client
import io
import json
import os
import sys
import tempfile
import unittest
import urllib.error
from contextlib import redirect_stdout
from pathlib import Path
from unittest import mock

sys.path.insert(0, str(Path(__file__).resolve().parent))

import check_markdown_links as cml

URL = "https://go.dev/cl/793780"
GH_BLOB = ("https://github.com/oracle/graal/blob/master/"
           "truffle/docs/bytecode_dsl/BytecodeDSL.md")


class FakeResponse:
    """Minimal urlopen response: status + context-manager protocol."""

    def __init__(self, status, body=b""):
        self.status = status
        self._body = body

    def __enter__(self):
        return self

    def __exit__(self, *exc):
        return False

    def read(self):
        return self._body


def reset_exc():
    return http.client.IncompleteRead(b"", 0)


def http_error(code, url=URL):
    return urllib.error.HTTPError(url, code, "error", {}, None)


class CheckExternalTest(unittest.TestCase):
    def run_with(self, side_effect, api_result=None):
        """Run check_external with a scripted urlopen, a stubbed API
        fallback, and a recording fake sleep; return
        (result, urlopen_calls, sleep_calls, api_calls)."""
        sleeps = []
        api = mock.Mock(return_value=api_result)
        with mock.patch("urllib.request.urlopen",
                        side_effect=side_effect) as urlopen, \
                mock.patch.object(cml, "_github_api_alive", api):
            result = cml.check_external(
                URL, sleep=lambda s: sleeps.append(s))
        return result, urlopen.call_count, sleeps, api.call_count

    def test_ci_signature_transient_then_live(self):
        # The 2026-11-06 CI signature verbatim: all 3 first-round
        # attempts reset, the URL answers 200 in the second round.
        # Pre-fix code returned False here.
        result, calls, sleeps, api_calls = self.run_with(
            [reset_exc(), reset_exc(), reset_exc(),
             FakeResponse(200)])
        self.assertTrue(result)
        self.assertEqual(calls, 4)
        self.assertEqual(sleeps, [2.0, 2.0, 10.0])
        self.assertEqual(api_calls, 1)  # consulted, inconclusive, retried

    def test_five_transient_then_live(self):
        # Worst case the fix is built for: 5 resets, live on the 6th
        # (last) attempt.
        result, calls, _, api_calls = self.run_with(
            [reset_exc()] * 5 + [FakeResponse(200)])
        self.assertTrue(result)
        self.assertEqual(calls, 6)
        self.assertEqual(api_calls, 1)

    def test_persistently_reset_is_transient(self):
        # Updated 2026-11-08: a URL that resets on every attempt is no
        # longer declared dead — it is TRANSIENT (unverifiable). The
        # run still fails on it (see test_main_transient_only_still_fails),
        # but the diagnosis no longer claims a live link is dead.
        result, calls, sleeps, api_calls = self.run_with(
            [reset_exc()] * 6)
        self.assertIs(result, cml.TRANSIENT)
        self.assertEqual(calls, 6)
        self.assertEqual(sleeps, [2.0, 2.0, 10.0, 2.0, 2.0])
        self.assertEqual(api_calls, 2)

    def test_404_fails_fast_no_second_round(self):
        # A definitive 404 is not transient: no backoff sleeps, no
        # second round — a genuinely dead link still fails fast.
        result, calls, sleeps, api_calls = self.run_with(
            [http_error(404)] * 3)
        self.assertFalse(result)
        self.assertEqual(calls, 3)
        self.assertEqual(sleeps, [])
        self.assertEqual(api_calls, 0)

    def test_410_fails_fast_no_second_round(self):
        result, calls, sleeps, api_calls = self.run_with(
            [http_error(410)] * 3)
        self.assertFalse(result)
        self.assertEqual(calls, 3)
        self.assertEqual(sleeps, [])
        self.assertEqual(api_calls, 0)

    def test_500_is_transient_then_live(self):
        result, calls, _, api_calls = self.run_with(
            [http_error(500), FakeResponse(200)])
        self.assertTrue(result)
        self.assertEqual(calls, 2)
        self.assertEqual(api_calls, 0)

    def test_429_is_transient_then_live(self):
        result, calls, _, api_calls = self.run_with(
            [http_error(429), FakeResponse(200)])
        self.assertTrue(result)
        self.assertEqual(calls, 2)
        self.assertEqual(api_calls, 0)

    def test_200_first_attempt(self):
        result, calls, sleeps, api_calls = self.run_with([FakeResponse(200)])
        self.assertTrue(result)
        self.assertEqual(calls, 1)
        self.assertEqual(sleeps, [])
        self.assertEqual(api_calls, 0)

    def test_head_403_then_get_200(self):
        # The pre-existing bot-protection ladder is unchanged: a HEAD
        # 403 falls through to GET, which answers 200.
        result, calls, sleeps, api_calls = self.run_with(
            [http_error(403), FakeResponse(200)])
        self.assertTrue(result)
        self.assertEqual(calls, 2)
        self.assertEqual(sleeps, [])
        self.assertEqual(api_calls, 0)

    def test_remote_disconnected_is_transient(self):
        # The other reset flavour seen in the wild for the same URLs.
        result, calls, _, _ = self.run_with(
            [http.client.RemoteDisconnected("Remote end closed "
                                           "connection without response"),
             FakeResponse(200)])
        self.assertTrue(result)
        self.assertEqual(calls, 2)

    def test_bot_protected_url_skipped_without_fetch(self):
        url = next(iter(cml.BOT_PROTECTED_URLS))
        with mock.patch("urllib.request.urlopen") as urlopen:
            self.assertTrue(cml.check_external(url))
        urlopen.assert_not_called()

    # --- GitHub API fallback (2026-11-08, CI run 42462394721) ---

    def test_web504_api200_alive_no_second_round(self):
        # The 2026-11-08 signature: web 504 on every first-round
        # attempt, API 200. Alive, with no second web round spent on
        # a degraded frontend.
        result, calls, sleeps, api_calls = self.run_with(
            [http_error(504, GH_BLOB)] * 3, api_result=True)
        self.assertTrue(result)
        self.assertEqual(calls, 3)
        self.assertEqual(sleeps, [2.0, 2.0])
        self.assertEqual(api_calls, 1)

    def test_web504_api404_dead(self):
        # Web flakiness does not rubber-stamp: the API authoritatively
        # says the target does not exist, so the URL is dead.
        result, calls, sleeps, api_calls = self.run_with(
            [http_error(504, GH_BLOB)] * 3, api_result=False)
        self.assertFalse(result)
        self.assertEqual(calls, 3)
        self.assertEqual(sleeps, [2.0, 2.0])
        self.assertEqual(api_calls, 1)

    def test_web404_never_consults_api(self):
        # A definitive web 404 fails fast and never consults the API.
        result, calls, sleeps, api_calls = self.run_with(
            [http_error(404, GH_BLOB)] * 3, api_result=True)
        self.assertFalse(result)
        self.assertEqual(calls, 3)
        self.assertEqual(sleeps, [])
        self.assertEqual(api_calls, 0)

    def test_both_down_is_transient(self):
        # Web 504 everywhere and the API inconclusive (e.g. API 500):
        # neither dead nor alive — TRANSIENT, and the run still fails.
        result, calls, sleeps, api_calls = self.run_with(
            [http_error(504, GH_BLOB)] * 6)
        self.assertIs(result, cml.TRANSIENT)
        self.assertEqual(calls, 6)
        self.assertEqual(sleeps, [2.0, 2.0, 10.0, 2.0, 2.0])
        self.assertEqual(api_calls, 2)

    def test_web200_never_consults_api(self):
        # A web 200 never costs an API call.
        result, calls, sleeps, api_calls = self.run_with(
            [FakeResponse(200)], api_result=False)
        self.assertTrue(result)
        self.assertEqual(calls, 1)
        self.assertEqual(sleeps, [])
        self.assertEqual(api_calls, 0)


class GithubApiTest(unittest.TestCase):
    def test_endpoint_mapping_all_8_ci_urls(self):
        # Every URL CI run 42462394721 reported dead maps to the
        # endpoint whose 200 proves it exists.
        cases = {
            "https://github.com/openjdk/loom/blob/HEAD/loom-docs/CustomSchedulers.md":
                "https://api.github.com/repos/openjdk/loom/contents/loom-docs/CustomSchedulers.md?ref=HEAD",
            "https://github.com/franz1981/Netty-VirtualThread-Scheduler":
                "https://api.github.com/repos/franz1981/Netty-VirtualThread-Scheduler",
            "https://github.com/mvysny/vaadin-loom/blob/HEAD/CLAUDE.md":
                "https://api.github.com/repos/mvysny/vaadin-loom/contents/CLAUDE.md?ref=HEAD",
            "https://github.com/v8/v8.dev/blob/HEAD/src/blog/elements-kinds.md":
                "https://api.github.com/repos/v8/v8.dev/contents/src/blog/elements-kinds.md?ref=HEAD",
            "https://github.com/franz1981/c2-writestring-spill-jmh":
                "https://api.github.com/repos/franz1981/c2-writestring-spill-jmh",
            "https://github.com/oracle/graal/blob/master/truffle/docs/bytecode_dsl/BytecodeDSL.md":
                "https://api.github.com/repos/oracle/graal/contents/truffle/docs/bytecode_dsl/BytecodeDSL.md?ref=master",
            "https://github.com/dotnet/core/blob/main/release-notes/10.0/preview/preview2/runtime.md":
                "https://api.github.com/repos/dotnet/core/contents/release-notes/10.0/preview/preview2/runtime.md?ref=main",
            "https://github.com/openjdk/valhalla-docs/blob/HEAD/site/_index.md":
                "https://api.github.com/repos/openjdk/valhalla-docs/contents/site/_index.md?ref=HEAD",
        }
        for url, expected in cases.items():
            with self.subTest(url=url):
                self.assertEqual(cml._github_api_endpoint(url), expected)

    def test_endpoint_mapping_other_shapes(self):
        cases = {
            "https://github.com/o/r":
                "https://api.github.com/repos/o/r",
            "https://github.com/o/r/":
                "https://api.github.com/repos/o/r",
            "https://github.com/o/r.git":
                "https://api.github.com/repos/o/r",
            "https://github.com/o/r/issues/12":
                "https://api.github.com/repos/o/r/issues/12",
            "https://github.com/o/r/pull/34":
                "https://api.github.com/repos/o/r/pulls/34",
            "https://github.com/o/r/commit/abc1234":
                "https://api.github.com/repos/o/r/commits/abc1234",
            "https://github.com/o/r/commits":
                "https://api.github.com/repos/o/r/commits",
            "https://github.com/o/r/commits/main":
                "https://api.github.com/repos/o/r/commits?sha=main",
            "https://github.com/o/r/tree/main/docs":
                "https://api.github.com/repos/o/r/contents/docs?ref=main",
            "https://github.com/o/r/tree/main":
                "https://api.github.com/repos/o/r",
            "https://github.com/o/r/raw/main/file.txt":
                "https://api.github.com/repos/o/r/contents/file.txt?ref=main",
            "https://github.com/o/r/releases/download/v2.0/app.zip":
                ("release-asset", "o", "r", "v2.0", "app.zip"),
        }
        for url, expected in cases.items():
            with self.subTest(url=url):
                self.assertEqual(cml._github_api_endpoint(url), expected)
        unmapped = [
            "https://example.com/o/r",
            "https://gist.github.com/user/abc123",
            "https://github.com/o/r/settings",
            "https://github.com/o/r/actions",
            "https://github.com/o",
            "https://github.com/o/r/blob",
            "https://github.com/o/r/issues/abc",
            "https://github.com/o/r/blob/main/",
        ]
        for url in unmapped:
            with self.subTest(url=url):
                self.assertIsNone(cml._github_api_endpoint(url))

    def test_release_asset_present_and_absent(self):
        url = "https://github.com/o/r/releases/download/v2.0/app.zip"
        body = json.dumps({"assets": [{"name": "app.zip"},
                                      {"name": "other.tar.gz"}]}).encode()
        with mock.patch.object(cml, "_github_api_get",
                               return_value=(200, body)):
            self.assertTrue(cml._github_api_alive(url))
        body_absent = json.dumps({"assets": [{"name": "other.tar.gz"}]}).encode()
        with mock.patch.object(cml, "_github_api_get",
                               return_value=(200, body_absent)):
            self.assertFalse(cml._github_api_alive(url))
        with mock.patch.object(cml, "_github_api_get",
                               return_value=(404, b"")):
            self.assertFalse(cml._github_api_alive(url))
        with mock.patch.object(cml, "_github_api_get",
                               return_value=(500, b"")):
            self.assertIsNone(cml._github_api_alive(url))

    def test_github_api_alive_status_mapping(self):
        # 200 -> alive; 404/410/422 -> dead; anything else -> None.
        for status, expected in [(200, True), (404, False), (410, False),
                                 (422, False), (500, None), (429, None),
                                 (401, None), (403, None)]:
            with self.subTest(status=status):
                with mock.patch("urllib.request.urlopen",
                                return_value=FakeResponse(status)):
                    self.assertIs(cml._github_api_alive(GH_BLOB), expected)
        # The token is sent when set (the workflow passes GITHUB_TOKEN).
        seen = {}

        def capture(req, timeout=None):
            seen["auth"] = req.get_header("Authorization")
            return FakeResponse(200)

        with mock.patch.dict(os.environ, {"GITHUB_TOKEN": "tok123"}), \
                mock.patch("urllib.request.urlopen", side_effect=capture):
            self.assertTrue(cml._github_api_alive(GH_BLOB))
        self.assertEqual(seen["auth"], "Bearer tok123")

    def test_github_api_alive_transport_error_and_unmapped(self):
        with mock.patch("urllib.request.urlopen",
                        side_effect=urllib.error.URLError(
                            ConnectionResetError("reset"))):
            self.assertIsNone(cml._github_api_alive(GH_BLOB))
        # Non-github and unmapped github shapes: no API call at all.
        with mock.patch("urllib.request.urlopen") as urlopen:
            self.assertIsNone(
                cml._github_api_alive("https://example.com/o/r"))
            self.assertIsNone(
                cml._github_api_alive("https://github.com/o/r/actions"))
        urlopen.assert_not_called()


class MainVerdictTest(unittest.TestCase):
    def run_main(self, files, verdicts):
        """Run main() over a temp tree; files maps name->body, verdicts
        maps URL->True/False/TRANSIENT. Returns (exit_code, stdout)."""
        with tempfile.TemporaryDirectory() as tmp:
            for name, body in files.items():
                Path(tmp, name).write_text(body)
            with mock.patch.object(cml, "check_external",
                                   side_effect=lambda u: verdicts[u]), \
                    mock.patch.object(sys, "argv",
                                      ["check_markdown_links.py", tmp]):
                buf = io.StringIO()
                with redirect_stdout(buf):
                    code = cml.main()
            return code, buf.getvalue()

    def test_main_dead_and_transient_separated(self):
        code, out = self.run_main(
            {"a.md": "[dead](https://example.com/dead)\n",
             "b.md": "[flaky](https://github.com/o/r)\n"},
            {"https://example.com/dead": False,
             "https://github.com/o/r": cml.TRANSIENT})
        self.assertEqual(code, 1)
        self.assertIn("1 broken link(s)", out)
        self.assertIn("dead URL https://example.com/dead", out)
        self.assertIn("1 unverifiable link(s)", out)
        self.assertIn("unverifiable URL https://github.com/o/r", out)
        # Dead list comes first, transient list second.
        self.assertLess(out.index("broken link(s)"),
                        out.index("unverifiable link(s)"))

    def test_main_transient_only_still_fails(self):
        # Positive control: TRANSIENT still fails the run — the
        # tripwire is intact, only the diagnosis changed.
        code, out = self.run_main(
            {"a.md": "[flaky](https://github.com/o/r)\n"},
            {"https://github.com/o/r": cml.TRANSIENT})
        self.assertEqual(code, 1)
        self.assertIn("unverifiable link(s)", out)
        self.assertIn("re-run to confirm", out)
        self.assertNotIn("broken link(s)", out)

    def test_main_all_live_ok(self):
        code, out = self.run_main(
            {"a.md": "[live](https://github.com/o/r) and "
                     "[local](b.md)\n",
             "b.md": "# B\n"},
            {"https://github.com/o/r": True})
        self.assertEqual(code, 0)
        self.assertIn("OK: 2 markdown files, no broken links", out)


if __name__ == "__main__":
    unittest.main()
