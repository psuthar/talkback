#!/usr/bin/env python3
"""TalkBack PR Risk runner with content-aware CreatorMode classification.

Wraps ``release-readiness-pr-risk`` and adds one heuristic before scoring:
``web/src/modes/CreatorMode*`` is reclassified out of the ``orchestration``
domain and into ``web`` when its diff content does not actually reference
orchestration tokens. The upstream classifier is path-only, so any edit to
that file (icon swap, accept-list bump) otherwise fires the
"Creator orchestration/recommendation flow changed" signal — see SCRUM-336.

CLI surface mirrors ``release-readiness-pr-risk`` so the workflow can call
this script in the existing step.
"""

from __future__ import annotations

import argparse
import os
import os.path
import re
import subprocess
import sys
from typing import Iterable, List, Optional, Sequence, Tuple

from release_readiness_core.pr_risk._runtime import PRRiskRuntime
from release_readiness_core.pr_risk.gitdiff import extract_signals
from release_readiness_core.pr_risk.integrations import ENV_JIRA_ISSUE_KEY
from release_readiness_core.pr_risk.report import write_json, write_markdown
from release_readiness_core.pr_risk.score import score
from release_readiness_core.pr_risk.semantic_json import write_semantic_pr_risk_json
from release_readiness_core.pr_risk.types import (
    DOMAIN_ORCHESTRATION,
    DOMAIN_WEB,
    Signals,
    default_weights,
)
from release_readiness_core.pr_risk.version import (
    VERSION,
    VERSION_MINOR,
    report_version_string,
)


CREATOR_MODE_PATH_RE = re.compile(r"^web/src/modes/creatormode", re.IGNORECASE)
ORCHESTRATION_TOKEN_RE = re.compile(r"orchestration|recommendation", re.IGNORECASE)
STYLE_ONLY_PREFIXES = ("style-only:", "style only:")


def _run_diff(repo_root: str, base_ref: str, path: str) -> str:
    """Return the unified=0 diff body for one path. Empty string on failure."""
    out = subprocess.run(
        [
            "git",
            "-C",
            repo_root,
            "diff",
            "--unified=0",
            f"{base_ref}...HEAD",
            "--",
            path,
        ],
        capture_output=True,
        text=True,
        check=False,
    )
    if out.returncode != 0:
        out = subprocess.run(
            ["git", "-C", repo_root, "diff", "--unified=0", base_ref, "--", path],
            capture_output=True,
            text=True,
            check=False,
        )
        if out.returncode != 0:
            return ""
    return out.stdout


def _changed_lines(diff_text: str) -> str:
    """Extract added/removed line bodies from a unified diff, excluding headers."""
    lines: List[str] = []
    for line in diff_text.splitlines():
        if line.startswith("+++") or line.startswith("---"):
            continue
        if line.startswith("+") or line.startswith("-"):
            lines.append(line[1:])
    return "\n".join(lines)


def diff_mentions_orchestration(diff_text: str) -> bool:
    """True when the unified diff body references orchestration tokens."""
    body = _changed_lines(diff_text)
    return bool(ORCHESTRATION_TOKEN_RE.search(body))


def _is_creatormode_path(path: str) -> bool:
    return bool(CREATOR_MODE_PATH_RE.match(path.replace("\\", "/")))


def reclassify_creatormode(
    signals: Signals,
    repo_root: str,
    base_ref: str,
    *,
    diff_reader=_run_diff,
) -> List[str]:
    """Move CreatorMode files out of the orchestration domain when their diff
    is purely UI (no orchestration/recommendation tokens in the changed lines).

    Mutates ``signals.domain_hits`` in place. Returns the list of paths that
    were moved (for logging / test assertions).
    """
    moved: List[str] = []
    if signals.domain_hits.get(DOMAIN_ORCHESTRATION, 0) <= 0:
        return moved
    for f in signals.files:
        if not _is_creatormode_path(f.path):
            continue
        diff_text = diff_reader(repo_root, base_ref, f.path)
        if diff_mentions_orchestration(diff_text):
            continue
        moved.append(f.path)
        signals.domain_hits[DOMAIN_ORCHESTRATION] = (
            signals.domain_hits.get(DOMAIN_ORCHESTRATION, 0) - 1
        )
        signals.domain_hits[DOMAIN_WEB] = signals.domain_hits.get(DOMAIN_WEB, 0) + 1
    if signals.domain_hits.get(DOMAIN_ORCHESTRATION, 0) <= 0:
        signals.domain_hits.pop(DOMAIN_ORCHESTRATION, None)
    return moved


def _git(repo_root: str, *args: str) -> Tuple[str, int]:
    """Run a git subcommand against repo_root. Returns (stdout, returncode)."""
    out = subprocess.run(
        ["git", "-C", repo_root, *args],
        capture_output=True,
        text=True,
        check=False,
    )
    return out.stdout, out.returncode


def _scan_body_for_style_only(body: str) -> Tuple[bool, str]:
    """Same prefix-scan the upstream uses, applied to a single commit body."""
    for line in body.split("\n"):
        stripped = line.strip()
        if stripped == "":
            continue
        lower = stripped.lower()
        for prefix in STYLE_ONLY_PREFIXES:
            if lower.startswith(prefix):
                return True, stripped[:120] if len(stripped) > 120 else stripped
    return False, ""


def detect_style_only_from_pr_head(
    repo_root: str,
    *,
    git=_git,
) -> Tuple[bool, str]:
    """SCRUM-442 fallback for the upstream detect_style_only_note when CI uses
    fetch-depth: 1 (the standard GHA shallow checkout). In that case HEAD is a
    synthetic refs/pull/N/merge commit and HEAD^2 (the PR's actual commit
    carrying the Style-only marker) is not in local objects. The upstream
    detector's ``git log origin/main...HEAD`` then sees only the merge commit's
    "Merge X into Y" message and returns False.

    This wrapper:
      1. Resolves HEAD^2's SHA from the merge commit's parent pointer (works
         without the object being local because rev-parse reads the merge's
         own data).
      2. Fetches HEAD^2 if it isn't local — ``git fetch --depth=50 origin <SHA>``.
      3. Reads HEAD^2's commit body and scans for the Style-only prefix.

    Returns (False, "") if HEAD is not a 2-parent merge, if HEAD^2 cannot be
    fetched, or if the body has no Style-only line. Never raises.
    """
    sha_out, sha_rc = git(repo_root, "rev-parse", "--verify", "HEAD^2")
    sha = sha_out.strip()
    if sha_rc != 0 or not sha:
        return False, ""
    _, cat_rc = git(repo_root, "cat-file", "-e", sha)
    if cat_rc != 0:
        # Object not local — fetch it from origin. Depth=50 covers anything
        # reasonable; we only need this single commit's body.
        _, fetch_rc = git(repo_root, "fetch", "--depth=50", "origin", sha)
        if fetch_rc != 0:
            return False, ""
    body_out, body_rc = git(repo_root, "log", "-1", "--format=%B", sha)
    if body_rc != 0:
        return False, ""
    return _scan_body_for_style_only(body_out)


def apply_style_only_fallback(
    signals: Signals,
    repo_root: str,
    *,
    detector=detect_style_only_from_pr_head,
) -> bool:
    """If the upstream detector missed the Style-only marker (commonly the
    GHA fetch-depth: 1 case), run the PR-head fallback and mutate signals so
    the score() pipeline emits the style_only_note reducer and policy.recommend
    waives the tests_missing gate.

    Returns True if the fallback fired (signals were mutated), False otherwise.
    Idempotent: no-op when signals.style_only_note_found is already True.
    """
    if signals.style_only_note_found:
        return False
    found, snippet = detector(repo_root)
    if not found:
        return False
    signals.style_only_note_found = True
    signals.style_only_note_snippet = snippet
    return True


def build_parser() -> argparse.ArgumentParser:
    parser = argparse.ArgumentParser(
        prog="pr_risk_run",
        description=(
            "Compute PR risk score for TalkBack with content-aware "
            "CreatorMode reclassification (SCRUM-336). "
            f"Wraps release-readiness-pr-risk schema {report_version_string()}."
        ),
    )
    parser.add_argument("--repo-root", default=".")
    parser.add_argument("--base-ref", default="origin/main")
    parser.add_argument(
        "--output-dir", default="artifacts/release-readiness"
    )
    parser.add_argument("--jira-key", default=None)
    parser.add_argument("--config", default=None)
    return parser


def main(argv: Optional[Sequence[str]] = None) -> int:
    parser = build_parser()
    args = parser.parse_args(argv)

    jira_key = (args.jira_key or "").strip()
    if not jira_key:
        jira_key = os.environ.get(ENV_JIRA_ISSUE_KEY, "").strip()

    runtime = (
        PRRiskRuntime.from_config(args.config)
        if args.config
        else None
    )

    signals = extract_signals(args.repo_root, args.base_ref, runtime=runtime)
    moved = reclassify_creatormode(signals, args.repo_root, args.base_ref)
    if moved:
        sys.stderr.write(
            "pr_risk_run: reclassified CreatorMode pure-UI edits out of "
            "orchestration domain: " + ", ".join(moved) + "\n"
        )
    if apply_style_only_fallback(signals, args.repo_root):
        sys.stderr.write(
            "pr_risk_run: SCRUM-442 fallback found Style-only marker on "
            "HEAD^2 (CI shallow-checkout case); upstream detector missed it.\n"
        )

    res = score(signals, default_weights(), jira_key, runtime=runtime)

    out = os.path.normpath(args.output_dir)
    os.makedirs(out, exist_ok=True)

    json_out = os.path.join(out, "pr_risk.json")
    md_out = os.path.join(out, "pr_risk.md")
    semantic_out = os.path.normpath(os.path.join(out, "..", "pr-risk.json"))

    try:
        write_json(json_out, res)
        write_markdown(md_out, res)
        write_semantic_pr_risk_json(semantic_out, res)
    except OSError as e:
        sys.stderr.write(f"pr_risk_run: write failed: {e}\n")
        return 1

    print(
        f"PR risk v{VERSION}.{VERSION_MINOR}: score={res.risk_score:.1f} "
        f"({res.risk_band}) — wrote "
        f"{os.path.basename(json_out)}/{os.path.basename(md_out)} "
        f"+ {os.path.basename(semantic_out)}"
    )
    if signals.git_error:
        sys.stderr.write(f"warning: git diff issue: {signals.git_error}\n")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
