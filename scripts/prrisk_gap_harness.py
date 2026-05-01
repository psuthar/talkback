#!/usr/bin/env python3
"""scripts/prrisk_gap_harness.py — gap-test harness for PR-risk equivalence.

Runs the legacy Go ``cmd/prrisk`` binary and the
``release-readiness-pr-risk`` CLI from the pinned ``release-readiness-core``
package on the same historical diffs and reports verdicts side-by-side.

Used by SCRUM-263 to measure parity between the in-tree engine and the
package, and as an acceptance test once SCRUM-264 lands the
project ``pr-risk-config.yaml``. The harness exits non-zero when any
PR shows a risk-band mismatch or ``|score delta|`` greater than the
configured tolerance, so it can run as a CI checker during SCRUM-265's
shadow phase and SCRUM-266's reconciliation.

Usage:
  scripts/prrisk_gap_harness.py [--shas-file ops/release-readiness/gap-test-prs.txt]
                                [--config <pr-risk-config.yaml>]
                                [--score-tolerance 5.0]
                                [--output-md artifacts/gap-test/gap-report.md]
                                [<sha> ...]

Inline ``<sha>`` arguments override ``--shas-file`` when provided.
The release-readiness-pr-risk CLI must be on PATH; install via::

  pip install $(scripts/release_readiness_core_pin.sh spec)
"""

from __future__ import annotations

import argparse
import json
import os
import shutil
import signal
import subprocess
import sys
import tempfile
from dataclasses import dataclass
from pathlib import Path
from typing import List, Optional, Sequence, Tuple


@dataclass(frozen=True)
class PRRiskOutput:
    """Harness-relevant fields extracted from a single ``pr_risk.json``."""

    score: float
    band: str
    merge_recommendation: str
    factor_ids: frozenset
    action_ids: frozenset
    validation_ids: frozenset


@dataclass(frozen=True)
class PerPRDelta:
    """Side-by-side comparison of Go and RRC PR-risk outputs for one SHA."""

    sha: str
    summary: str
    go: PRRiskOutput
    rrc: PRRiskOutput

    @property
    def score_delta(self) -> float:
        return self.rrc.score - self.go.score

    @property
    def band_match(self) -> bool:
        return self.go.band == self.rrc.band

    @property
    def rec_match(self) -> bool:
        return self.go.merge_recommendation == self.rrc.merge_recommendation

    @property
    def factor_only_go(self) -> List[str]:
        return sorted(self.go.factor_ids - self.rrc.factor_ids)

    @property
    def factor_only_rrc(self) -> List[str]:
        return sorted(self.rrc.factor_ids - self.go.factor_ids)

    @property
    def action_only_go(self) -> List[str]:
        return sorted(self.go.action_ids - self.rrc.action_ids)

    @property
    def action_only_rrc(self) -> List[str]:
        return sorted(self.rrc.action_ids - self.go.action_ids)

    @property
    def validation_only_go(self) -> List[str]:
        return sorted(self.go.validation_ids - self.rrc.validation_ids)

    @property
    def validation_only_rrc(self) -> List[str]:
        return sorted(self.rrc.validation_ids - self.go.validation_ids)


def parse_pr_risk_json(payload: dict) -> PRRiskOutput:
    """Extract harness-relevant fields from either Go or RRC ``pr_risk.json``."""
    score_raw = payload.get("risk_score")
    if score_raw is None:
        raise ValueError("pr_risk.json missing 'risk_score'")
    score = float(score_raw)
    band = str(payload.get("risk_band", ""))
    enf = payload.get("enforcement") or {}
    rec = str(enf.get("merge_recommendation", ""))
    factor_ids = frozenset(
        str(f.get("id", ""))
        for f in (payload.get("factors") or [])
        if f.get("id")
    )
    action_ids = frozenset(
        str(a.get("id", ""))
        for a in (payload.get("required_actions") or [])
        if a.get("id")
    )
    validation_ids = frozenset(
        str(v) for v in (enf.get("required_validations") or [])
    )
    return PRRiskOutput(
        score=score,
        band=band,
        merge_recommendation=rec,
        factor_ids=factor_ids,
        action_ids=action_ids,
        validation_ids=validation_ids,
    )


def load_shas(text: str) -> List[Tuple[str, str]]:
    """Parse ``gap-test-prs.txt`` into ``(sha, summary)`` tuples.

    Comments (``#``) and blank lines are ignored. Each non-empty line is
    either ``<sha>`` or ``<sha>  # summary text`` — the trailing comment
    becomes the human-readable summary in the report.
    """
    out: List[Tuple[str, str]] = []
    for line in text.splitlines():
        s = line.strip()
        if not s or s.startswith("#"):
            continue
        if "#" in s:
            sha, _, summary = s.partition("#")
            sha = sha.strip()
            summary = summary.strip()
        else:
            sha, summary = s, ""
        if sha:
            out.append((sha, summary))
    return out


def is_significant_gap(delta: PerPRDelta, score_tolerance: float) -> bool:
    if not delta.band_match:
        return True
    return abs(delta.score_delta) > score_tolerance


def _format_set_diff(only_go: List[str], only_rrc: List[str]) -> str:
    parts: List[str] = []
    parts.extend(f"-{x}" for x in only_go)
    parts.extend(f"+{x}" for x in only_rrc)
    return ", ".join(parts) if parts else "—"


def render_markdown(deltas: Sequence[PerPRDelta], score_tolerance: float) -> str:
    total = len(deltas)
    fails = sum(1 for d in deltas if is_significant_gap(d, score_tolerance))
    lines = [
        "# PR-risk gap test (Go cmd/prrisk vs release-readiness-core)",
        "",
        (
            f"PRs scanned: **{total}**. Significant gaps "
            f"(band mismatch or |score Δ| > {score_tolerance:g}): **{fails}**."
        ),
        "",
        "| sha | summary | Go score / band / rec | RRC score / band / rec | Δ score | factor diff | action diff | validation diff |",
        "|---|---|---|---|---|---|---|---|",
    ]
    for d in deltas:
        gtxt = f"{d.go.score:g} / {d.go.band} / {d.go.merge_recommendation}"
        rtxt = f"{d.rrc.score:g} / {d.rrc.band} / {d.rrc.merge_recommendation}"
        f_only = _format_set_diff(d.factor_only_go, d.factor_only_rrc)
        a_only = _format_set_diff(d.action_only_go, d.action_only_rrc)
        v_only = _format_set_diff(
            d.validation_only_go, d.validation_only_rrc
        )
        marker = " ⚠️" if is_significant_gap(d, score_tolerance) else ""
        lines.append(
            f"| `{d.sha[:10]}`{marker} | {d.summary or '—'} | {gtxt} | {rtxt} "
            f"| {d.score_delta:+g} | {f_only} | {a_only} | {v_only} |"
        )
    lines.append("")
    lines.append(
        f"_Score tolerance: ±{score_tolerance:g}. Band mismatch fails regardless of score._"
    )
    return "\n".join(lines) + "\n"


def _run(cmd: Sequence[str], cwd: Optional[Path] = None) -> subprocess.CompletedProcess:
    return subprocess.run(
        list(cmd),
        cwd=str(cwd) if cwd else None,
        check=False,
        capture_output=True,
        text=True,
    )


def _add_worktree(repo_root: Path, sha: str, dest: Path) -> None:
    res = _run(
        ["git", "worktree", "add", "--detach", str(dest), sha], cwd=repo_root
    )
    if res.returncode != 0:
        raise RuntimeError(
            f"git worktree add failed for {sha}: {res.stderr.strip() or res.stdout.strip()}"
        )


def _remove_worktree(repo_root: Path, dest: Path) -> None:
    if dest.exists():
        _run(
            ["git", "worktree", "remove", "--force", str(dest)], cwd=repo_root
        )


def _evaluate_in_worktree(
    worktree: Path,
    sha: str,
    summary: str,
    config: Optional[Path],
    rrc_cli: str,
) -> PerPRDelta:
    go_out = worktree / "out-go"
    rrc_out = worktree / "out-rrc"
    go_out.mkdir(exist_ok=True)
    rrc_out.mkdir(exist_ok=True)
    base = f"{sha}~1"
    gres = _run(
        [
            "go",
            "run",
            "./cmd/prrisk",
            "--repo-root",
            str(worktree),
            "--base-ref",
            base,
            "--output-dir",
            str(go_out),
        ],
        cwd=worktree,
    )
    if gres.returncode != 0:
        raise RuntimeError(
            f"go cmd/prrisk failed at {sha}: {gres.stderr.strip() or gres.stdout.strip()}"
        )
    rrc_args = [
        rrc_cli,
        "--repo-root",
        str(worktree),
        "--base-ref",
        base,
        "--output-dir",
        str(rrc_out),
    ]
    if config is not None:
        rrc_args += ["--config", str(config)]
    rres = _run(rrc_args)
    if rres.returncode != 0:
        raise RuntimeError(
            f"release-readiness-pr-risk failed at {sha}: {rres.stderr.strip() or rres.stdout.strip()}"
        )
    go_payload = json.loads((go_out / "pr_risk.json").read_text())
    rrc_payload = json.loads((rrc_out / "pr_risk.json").read_text())
    return PerPRDelta(
        sha=sha,
        summary=summary,
        go=parse_pr_risk_json(go_payload),
        rrc=parse_pr_risk_json(rrc_payload),
    )


def parse_args(argv: Optional[Sequence[str]] = None) -> argparse.Namespace:
    p = argparse.ArgumentParser(
        description=(
            "Compare PR-risk verdicts from in-tree Go cmd/prrisk and "
            "release-readiness-core release-readiness-pr-risk on historical SHAs."
        )
    )
    p.add_argument(
        "--shas-file",
        type=Path,
        default=Path("ops/release-readiness/gap-test-prs.txt"),
        help="File with one SHA per line; '#' starts a comment / inline summary.",
    )
    p.add_argument(
        "shas", nargs="*", help="Inline SHAs override --shas-file when provided."
    )
    p.add_argument(
        "--config",
        type=Path,
        default=None,
        help=(
            "Optional pr-risk-config.yaml passed to release-readiness-pr-risk. "
            "Without it the package uses its bundled language-agnostic default; "
            "expect a wider gap until SCRUM-264 lands the project YAML."
        ),
    )
    p.add_argument(
        "--rrc-cli",
        default="release-readiness-pr-risk",
        help="Override the release-readiness-pr-risk binary name (must be on PATH).",
    )
    p.add_argument(
        "--score-tolerance",
        type=float,
        default=5.0,
        help="|score delta| above this is a significant gap (default 5).",
    )
    p.add_argument(
        "--output-md",
        type=Path,
        default=Path("artifacts/gap-test/gap-report.md"),
        help="Where to write the rendered Markdown report.",
    )
    p.add_argument(
        "--repo-root",
        type=Path,
        default=Path("."),
        help="Root of the git repository to check out worktrees from.",
    )
    p.add_argument(
        "--keep-worktrees",
        action="store_true",
        help="Skip worktree cleanup (debugging only).",
    )
    return p.parse_args(argv)


def main(argv: Optional[Sequence[str]] = None) -> int:
    args = parse_args(argv)

    if shutil.which(args.rrc_cli) is None:
        print(
            f"error: {args.rrc_cli} not on PATH; install via "
            f"`pip install $(scripts/release_readiness_core_pin.sh spec)`",
            file=sys.stderr,
        )
        return 3

    if args.shas:
        shas: List[Tuple[str, str]] = [(s, "") for s in args.shas]
    else:
        if not args.shas_file.exists():
            print(
                f"error: --shas-file {args.shas_file} not found", file=sys.stderr
            )
            return 3
        shas = load_shas(args.shas_file.read_text())
    if not shas:
        print("error: no SHAs to evaluate", file=sys.stderr)
        return 3

    repo_root = args.repo_root.resolve()
    args.output_md.parent.mkdir(parents=True, exist_ok=True)

    workdir = Path(tempfile.mkdtemp(prefix="prrisk-gap-"))
    worktrees: List[Path] = []

    def _cleanup() -> None:
        if args.keep_worktrees:
            return
        for w in worktrees:
            _remove_worktree(repo_root, w)
        shutil.rmtree(workdir, ignore_errors=True)

    def _on_signal(signum, _frame):
        _cleanup()
        sys.exit(128 + signum)

    signal.signal(signal.SIGINT, _on_signal)
    signal.signal(signal.SIGTERM, _on_signal)

    deltas: List[PerPRDelta] = []
    try:
        for sha, summary in shas:
            print(f"[gap-harness] {sha[:10]} {summary}", file=sys.stderr)
            wt = workdir / f"prrisk-gap-{sha[:12]}"
            worktrees.append(wt)
            try:
                _add_worktree(repo_root, sha, wt)
                delta = _evaluate_in_worktree(
                    wt, sha, summary, args.config, args.rrc_cli
                )
                deltas.append(delta)
            except Exception as exc:
                print(f"  ERROR: {exc}", file=sys.stderr)
                return 4

        report = render_markdown(deltas, args.score_tolerance)
        args.output_md.write_text(report)
        print(report)
        return 1 if any(is_significant_gap(d, args.score_tolerance) for d in deltas) else 0
    finally:
        _cleanup()


if __name__ == "__main__":
    sys.exit(main())
