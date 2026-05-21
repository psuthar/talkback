#!/usr/bin/env python3
"""Unit tests for scripts/full_auto/lib/auth.py (SCRUM-528)."""

from __future__ import annotations

import os
import sys
import unittest
from pathlib import Path
from unittest import mock

_REPO_ROOT = Path(__file__).resolve().parents[1]
sys.path.insert(0, str(_REPO_ROOT / "scripts"))

from full_auto.lib import auth  # noqa: E402


def _clear_lru_caches() -> None:
    auth.github_token.cache_clear()
    auth.jira_auth.cache_clear()


class GitHubTokenTest(unittest.TestCase):
    def setUp(self):
        _clear_lru_caches()

    def test_env_var_wins(self):
        with mock.patch.dict(os.environ, {"GITHUB_TOKEN": "ghp_from_env"}, clear=False):
            with mock.patch("full_auto.lib.auth.subprocess.run") as run_mock:
                self.assertEqual(auth.github_token(), "ghp_from_env")
                run_mock.assert_not_called()

    def test_gh_cli_fallback_when_env_missing(self):
        env_without = {k: v for k, v in os.environ.items() if k != "GITHUB_TOKEN"}
        with mock.patch.dict(os.environ, env_without, clear=True):
            fake = mock.Mock(returncode=0, stdout="ghp_from_gh\n")
            with mock.patch(
                "full_auto.lib.auth.subprocess.run", return_value=fake
            ) as run_mock:
                self.assertEqual(auth.github_token(), "ghp_from_gh")
                run_mock.assert_called_once()
                args = run_mock.call_args.args[0]
                self.assertEqual(args, ["gh", "auth", "token"])

    def test_raises_when_no_env_and_gh_fails(self):
        env_without = {k: v for k, v in os.environ.items() if k != "GITHUB_TOKEN"}
        with mock.patch.dict(os.environ, env_without, clear=True):
            fake = mock.Mock(returncode=1, stdout="", stderr="not logged in")
            with mock.patch("full_auto.lib.auth.subprocess.run", return_value=fake):
                with self.assertRaises(RuntimeError) as cm:
                    auth.github_token()
                self.assertIn("gh auth login", str(cm.exception))
                self.assertIn("GITHUB_TOKEN", str(cm.exception))

    def test_raises_when_gh_returns_empty_token(self):
        # gh CLI exit 0 but empty stdout — treat as no credentials.
        env_without = {k: v for k, v in os.environ.items() if k != "GITHUB_TOKEN"}
        with mock.patch.dict(os.environ, env_without, clear=True):
            fake = mock.Mock(returncode=0, stdout="\n")
            with mock.patch("full_auto.lib.auth.subprocess.run", return_value=fake):
                with self.assertRaises(RuntimeError):
                    auth.github_token()

    def test_result_is_memoised(self):
        with mock.patch.dict(os.environ, {"GITHUB_TOKEN": "ghp_memo"}, clear=False):
            t1 = auth.github_token()
        # Even after the env var is "removed", the cached value returns —
        # proves the lru_cache is doing its job (avoids reshelling gh).
        env_without = {k: v for k, v in os.environ.items() if k != "GITHUB_TOKEN"}
        with mock.patch.dict(os.environ, env_without, clear=True):
            with mock.patch("full_auto.lib.auth.subprocess.run") as run_mock:
                t2 = auth.github_token()
                run_mock.assert_not_called()
        self.assertEqual(t1, t2)
        self.assertEqual(t1, "ghp_memo")


class JiraAuthTest(unittest.TestCase):
    def setUp(self):
        _clear_lru_caches()

    def test_both_vars_present_returns_tuple(self):
        with mock.patch.dict(
            os.environ,
            {"ATLASSIAN_EMAIL": "you@example.com", "ATLASSIAN_API_TOKEN": "tok_x"},
            clear=False,
        ):
            self.assertEqual(auth.jira_auth(), ("you@example.com", "tok_x"))

    def test_missing_email_raises(self):
        env = {k: v for k, v in os.environ.items() if k != "ATLASSIAN_EMAIL"}
        env["ATLASSIAN_API_TOKEN"] = "tok"
        with mock.patch.dict(os.environ, env, clear=True):
            with self.assertRaises(RuntimeError) as cm:
                auth.jira_auth()
            self.assertIn("ATLASSIAN_EMAIL", str(cm.exception))
            self.assertIn("manage-profile/security/api-tokens", str(cm.exception))

    def test_missing_token_raises(self):
        env = {k: v for k, v in os.environ.items() if k != "ATLASSIAN_API_TOKEN"}
        env["ATLASSIAN_EMAIL"] = "you@example.com"
        with mock.patch.dict(os.environ, env, clear=True):
            with self.assertRaises(RuntimeError) as cm:
                auth.jira_auth()
            self.assertIn("ATLASSIAN_API_TOKEN", str(cm.exception))

    def test_empty_values_raise(self):
        # Whitespace-only values are treated as missing.
        with mock.patch.dict(
            os.environ,
            {"ATLASSIAN_EMAIL": "  ", "ATLASSIAN_API_TOKEN": "tok"},
            clear=False,
        ):
            with self.assertRaises(RuntimeError):
                auth.jira_auth()


class AtlassianBaseUrlTest(unittest.TestCase):
    def test_default_is_talkback_tenant(self):
        env = {k: v for k, v in os.environ.items() if k != "ATLASSIAN_BASE_URL"}
        with mock.patch.dict(os.environ, env, clear=True):
            self.assertEqual(auth.atlassian_base_url(), auth.DEFAULT_ATLASSIAN_BASE_URL)

    def test_env_override(self):
        with mock.patch.dict(
            os.environ, {"ATLASSIAN_BASE_URL": "https://other.atlassian.net"}, clear=False
        ):
            self.assertEqual(auth.atlassian_base_url(), "https://other.atlassian.net")

    def test_strips_trailing_slash(self):
        with mock.patch.dict(
            os.environ, {"ATLASSIAN_BASE_URL": "https://other.atlassian.net/"}, clear=False
        ):
            self.assertEqual(auth.atlassian_base_url(), "https://other.atlassian.net")


if __name__ == "__main__":
    unittest.main()
