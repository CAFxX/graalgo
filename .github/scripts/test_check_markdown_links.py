#!/usr/bin/env python3
"""Regression tests for check_markdown_links.py retry logic.

Run: python3 .github/scripts/test_check_markdown_links.py

The 2026-11-06 failure (CI run 42130033841, markdown-links job): three
go.dev/cl URLs were reported dead although every one of them is live.
Root cause: go.dev/cl intermittently resets connections with zero bytes
returned (IncompleteRead(0 bytes read)) on ~15% of requests (8/54 in a
local probe, across HEAD/GET and both user agents), and the same URL
passed in one file and failed in another within the same CI run — so
the reset is per-request flakiness at the Google frontend, not a dead
link. The pre-fix checker made 3 immediate attempts and declared the
URL dead if all 3 were reset, which a short outage window could do to
several URLs at once.

Fixtures below replay that exact signature (IncompleteRead on every
first-round attempt, then a live 200) plus the neighboring cases the
fix must not change: a persistently reset URL is still dead, a 404
still fails fast with no second round, and the HEAD-403 -> GET-200
ladder for bot-protected hosts still works.

Positive controls are URLs that MUST still fail: the checker is a
tripwire, not a rubber stamp. The transient fixtures use a fake
urlopen/sleep so the tests run offline in milliseconds; a live smoke
check against go.dev/cl is NOT asserted here (network-dependent).
"""

import http.client
import sys
import unittest
import urllib.error
from pathlib import Path
from unittest import mock

sys.path.insert(0, str(Path(__file__).resolve().parent))

import check_markdown_links as cml

URL = "https://go.dev/cl/793780"


class FakeResponse:
    """Minimal urlopen response: status + context-manager protocol."""

    def __init__(self, status):
        self.status = status

    def __enter__(self):
        return self

    def __exit__(self, *exc):
        return False


def reset_exc():
    return http.client.IncompleteRead(b"", 0)


def http_error(code):
    return urllib.error.HTTPError(URL, code, "error", {}, None)


class CheckExternalTest(unittest.TestCase):
    def run_with(self, side_effect):
        """Run check_external with a scripted urlopen and a recording
        fake sleep; return (result, urlopen_calls, sleep_calls)."""
        sleeps = []
        with mock.patch("urllib.request.urlopen",
                        side_effect=side_effect) as urlopen:
            result = cml.check_external(
                URL, sleep=lambda s: sleeps.append(s))
        return result, urlopen.call_count, sleeps

    def test_ci_signature_transient_then_live(self):
        # The 2026-11-06 CI signature verbatim: all 3 first-round
        # attempts reset, the URL answers 200 in the second round.
        # Pre-fix code returned False here.
        result, calls, sleeps = self.run_with(
            [reset_exc(), reset_exc(), reset_exc(),
             FakeResponse(200)])
        self.assertTrue(result)
        self.assertEqual(calls, 4)
        self.assertEqual(sleeps, [2.0, 2.0, 10.0])

    def test_five_transient_then_live(self):
        # Worst case the fix is built for: 5 resets, live on the 6th
        # (last) attempt.
        result, calls, _ = self.run_with(
            [reset_exc()] * 5 + [FakeResponse(200)])
        self.assertTrue(result)
        self.assertEqual(calls, 6)

    def test_persistently_reset_still_dead(self):
        # Positive control: a URL that resets on every attempt is
        # still declared dead — the retry must not rubber-stamp.
        result, calls, sleeps = self.run_with([reset_exc()] * 6)
        self.assertFalse(result)
        self.assertEqual(calls, 6)
        self.assertEqual(sleeps, [2.0, 2.0, 10.0, 2.0, 2.0])

    def test_404_fails_fast_no_second_round(self):
        # A definitive 404 is not transient: no backoff sleeps, no
        # second round — a genuinely dead link still fails fast.
        result, calls, sleeps = self.run_with(
            [http_error(404)] * 3)
        self.assertFalse(result)
        self.assertEqual(calls, 3)
        self.assertEqual(sleeps, [])

    def test_410_fails_fast_no_second_round(self):
        result, calls, sleeps = self.run_with(
            [http_error(410)] * 3)
        self.assertFalse(result)
        self.assertEqual(calls, 3)
        self.assertEqual(sleeps, [])

    def test_500_is_transient_then_live(self):
        result, calls, _ = self.run_with(
            [http_error(500), FakeResponse(200)])
        self.assertTrue(result)
        self.assertEqual(calls, 2)

    def test_429_is_transient_then_live(self):
        result, calls, _ = self.run_with(
            [http_error(429), FakeResponse(200)])
        self.assertTrue(result)
        self.assertEqual(calls, 2)

    def test_200_first_attempt(self):
        result, calls, sleeps = self.run_with([FakeResponse(200)])
        self.assertTrue(result)
        self.assertEqual(calls, 1)
        self.assertEqual(sleeps, [])

    def test_head_403_then_get_200(self):
        # The pre-existing bot-protection ladder is unchanged: a HEAD
        # 403 falls through to GET, which answers 200.
        result, calls, sleeps = self.run_with(
            [http_error(403), FakeResponse(200)])
        self.assertTrue(result)
        self.assertEqual(calls, 2)
        self.assertEqual(sleeps, [])

    def test_remote_disconnected_is_transient(self):
        # The other reset flavour seen in the wild for the same URLs.
        result, calls, _ = self.run_with(
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


if __name__ == "__main__":
    unittest.main()
