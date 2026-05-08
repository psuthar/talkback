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
