#!/usr/bin/env python3
"""SCRUM-562 — qa-eval CI wrapper.

Compares ``eval/baselines/qa_eval_baseline.json`` in the working tree
against the same file on the PR's base ref (``origin/<base>``). A
threshold breach makes this script exit non-zero so the
release-readiness step fails → the gate flips to WARN.

Per-PR runs are baseline-compare-only — NOT a live measurement. The
live ``scripts/run_qa_eval.py`` is invoked separately by the
``qa-eval-refresh.yml`` ``workflow_dispatch`` workflow; a human PRs a
baseline bump after inspecting the live run.

Emits ``artifacts/qa-eval-summary.json``.
"""

from __future__ import annotations

import argparse
import datetime as dt
import json
import os
import subprocess
import sys
from pathlib import Path

import yaml

REPO_ROOT = Path(__file__).resolve().parents[1]
DEFAULT_BASELINE = REPO_ROOT / "eval" / "baselines" / "qa_eval_baseline.json"
DEFAULT_THRESHOLDS = REPO_ROOT / "eval" / "baselines" / "qa_eval_thresholds.yaml"
DEFAULT_OUTPUT = REPO_ROOT / "artifacts" / "qa-eval-summary.json"
DEFAULT_BASE_REF = os.environ.get("QA_EVAL_BASE_REF", "origin/main")
RUNNER = REPO_ROOT / "scripts" / "run_qa_eval.py"


class CIError(Exception):
    """Raised on unrecoverable input / git failures (distinct from threshold breach)."""


def _repo_relative(path: Path) -> str:
    """``str(path)`` shown relative to REPO_ROOT when possible, else absolute.

    Used for log lines + the summary JSON — never for git-show lookups,
    which only make sense inside the repo and use a strict
    ``relative_to`` check.
    """
    try:
        return str(path.relative_to(REPO_ROOT))
    except ValueError:
        return str(path)


def load_json_file(path: Path) -> dict:
    if not path.exists():
        raise CIError(f"missing file: {path}")
    try:
        return json.loads(path.read_text(encoding="utf-8"))
    except json.JSONDecodeError as exc:
        raise CIError(f"malformed JSON at {path}: {exc}") from exc


def load_yaml_file(path: Path) -> dict:
    if not path.exists():
        raise CIError(f"missing file: {path}")
    data = yaml.safe_load(path.read_text(encoding="utf-8"))
    return data or {}


def load_base_baseline(base_ref: str, path: Path) -> dict | None:
    """Read the baseline file as it exists on ``base_ref``.

    Returns ``None`` when the file doesn't exist on the base ref (a new
    baseline file landing in this PR, or a fresh branch off a tag) —
    that's an expected case, not an error.
    """
    try:
        rel = path.relative_to(REPO_ROOT).as_posix()
    except ValueError:
        # Path is outside REPO_ROOT (e.g. a test's temp dir) — no git
        # history to look up. Treat as "no prior baseline".
        return None
    try:
        out = subprocess.run(
            ["git", "show", f"{base_ref}:{rel}"],
            cwd=REPO_ROOT,
            capture_output=True,
            text=True,
            check=True,
        )
    except subprocess.CalledProcessError:
        return None
    except FileNotFoundError as exc:
        raise CIError(f"git not on PATH: {exc}") from exc
    try:
        return json.loads(out.stdout)
    except json.JSONDecodeError as exc:
        raise CIError(f"base ref {base_ref} has malformed baseline JSON: {exc}") from exc


def harness_cli_sanity() -> None:
    """Cheap insurance: `run_qa_eval.py --help` must still parse.

    Catches accidental upstream-refactor breakage of the harness CLI.
    """
    try:
        subprocess.run(
            [sys.executable, str(RUNNER), "--help"],
            cwd=REPO_ROOT,
            capture_output=True,
            text=True,
            check=True,
            timeout=10,
        )
    except subprocess.CalledProcessError as exc:
        raise CIError(f"run_qa_eval.py --help failed: {exc.stderr or exc.stdout}") from exc
    except subprocess.TimeoutExpired as exc:
        raise CIError("run_qa_eval.py --help timed out (>10s)") from exc


def _delta(baseline_val, current_val):
    if not isinstance(baseline_val, (int, float)) or not isinstance(current_val, (int, float)):
        return None
    return round(float(current_val) - float(baseline_val), 4)


def evaluate_thresholds(
    current_metrics: dict,
    prior_metrics: dict | None,
    thresholds: dict,
) -> list[dict]:
    """One entry per evaluated metric with explicit ``skipped`` and ``pass`` flags."""
    evals: list[dict] = []

    def _add(metric, baseline_val, current_val, threshold_val, kind, passed, *, skipped=False, note=""):
        evals.append(
            {
                "metric": metric,
                "baseline": baseline_val,
                "current": current_val,
                "delta": _delta(baseline_val, current_val),
                "threshold": threshold_val,
                "threshold_kind": kind,
                "pass": passed,
                "skipped": skipped,
                "note": note,
            }
        )

    if prior_metrics is None:
        evals.append(
            {
                "metric": "_compare",
                "pass": True,
                "skipped": True,
                "note": "no prior baseline on base ref (new file, or base ref unreadable); compare skipped",
            }
        )
        return evals

    # correctness_percentage drop
    threshold = thresholds.get("correctness_percentage_min_delta_pp")
    base = prior_metrics.get("correctness_percentage")
    cur = current_metrics.get("correctness_percentage")
    if threshold is None or not isinstance(base, (int, float)) or not isinstance(cur, (int, float)):
        _add("correctness_percentage", base, cur, threshold, "min_delta_pp", True, skipped=True, note="missing metric or threshold")
    else:
        delta = cur - base
        passed = delta >= float(threshold)
        note = "" if passed else f"dropped {delta:+.2f}pp; threshold floor is {float(threshold):+.2f}pp"
        _add("correctness_percentage", base, cur, threshold, "min_delta_pp", passed, note=note)

    # hallucination_count rise
    threshold = thresholds.get("hallucination_count_max_delta")
    base = prior_metrics.get("hallucination_count")
    cur = current_metrics.get("hallucination_count")
    if threshold is None or not isinstance(base, int) or not isinstance(cur, int):
        _add("hallucination_count", base, cur, threshold, "max_delta", True, skipped=True, note="missing metric or threshold")
    else:
        delta = cur - base
        passed = delta <= int(threshold)
        note = "" if passed else f"increased by {delta:+d}; threshold ceiling is {int(threshold):+d}"
        _add("hallucination_count", base, cur, threshold, "max_delta", passed, note=note)

    # weighted_correctness drop
    threshold = thresholds.get("weighted_correctness_min_delta")
    base = prior_metrics.get("weighted_correctness")
    cur = current_metrics.get("weighted_correctness")
    if threshold is None or not isinstance(base, (int, float)) or not isinstance(cur, (int, float)):
        _add("weighted_correctness", base, cur, threshold, "min_delta", True, skipped=True, note="missing metric or threshold")
    else:
        delta = cur - base
        passed = delta >= float(threshold)
        note = "" if passed else f"dropped {delta:+.4f}; threshold floor is {float(threshold):+.4f}"
        _add("weighted_correctness", base, cur, threshold, "min_delta", passed, note=note)

    # p95_latency_ms percent rise — skip if either side is null or baseline=0
    threshold = thresholds.get("p95_latency_ms_max_delta_pct")
    base = prior_metrics.get("p95_latency_ms")
    cur = current_metrics.get("p95_latency_ms")
    if (
        threshold is None
        or not isinstance(base, (int, float))
        or not isinstance(cur, (int, float))
        or base == 0
    ):
        _add("p95_latency_ms", base, cur, threshold, "max_delta_pct", True, skipped=True, note="missing metric/threshold or baseline=0")
    else:
        pct = ((cur - base) / float(base)) * 100.0
        passed = pct <= float(threshold)
        note = "" if passed else f"grew by {pct:+.1f}%; threshold ceiling is {float(threshold):+.1f}%"
        _add("p95_latency_ms", base, cur, threshold, "max_delta_pct", passed, note=note)

    # overall_pass must remain true in current
    if thresholds.get("overall_pass_required") is True:
        cur = current_metrics.get("overall_pass")
        passed = cur is True
        note = "" if passed else f"overall_pass is {cur!r} (required: true)"
        _add("overall_pass", prior_metrics.get("overall_pass"), cur, True, "must_be_true", passed, note=note)

    # SCRUM-564 (Slice 3): refusal_when_oos_rate — true-positive rate of
    # the input guardrail against the labelled fixture
    # (eval/qa/fixture_input_guardrail.json). May drop by at most the
    # configured amount before the gate WARNs. Skipped if either side
    # is missing (graceful rollout from before the metric existed).
    threshold = thresholds.get("refusal_when_oos_rate_min_delta")
    base = prior_metrics.get("refusal_when_oos_rate")
    cur = current_metrics.get("refusal_when_oos_rate")
    if threshold is None or not isinstance(base, (int, float)) or not isinstance(cur, (int, float)):
        _add("refusal_when_oos_rate", base, cur, threshold, "min_delta", True, skipped=True, note="missing metric or threshold")
    else:
        delta = cur - base
        passed = delta >= float(threshold)
        note = "" if passed else f"dropped {delta:+.4f}; threshold floor is {float(threshold):+.4f}"
        _add("refusal_when_oos_rate", base, cur, threshold, "min_delta", passed, note=note)

    # SCRUM-564 (Slice 3): legitimate_false_positive_rate — fraction of
    # legitimate session questions over-blocked by the input guardrail.
    # May rise by at most the configured amount before the gate WARNs.
    threshold = thresholds.get("legitimate_false_positive_rate_max_delta")
    base = prior_metrics.get("legitimate_false_positive_rate")
    cur = current_metrics.get("legitimate_false_positive_rate")
    if threshold is None or not isinstance(base, (int, float)) or not isinstance(cur, (int, float)):
        _add("legitimate_false_positive_rate", base, cur, threshold, "max_delta", True, skipped=True, note="missing metric or threshold")
    else:
        delta = cur - base
        passed = delta <= float(threshold)
        note = "" if passed else f"rose by {delta:+.4f}; threshold ceiling is {float(threshold):+.4f}"
        _add("legitimate_false_positive_rate", base, cur, threshold, "max_delta", passed, note=note)

    # SCRUM-565 (Slice 4a): citation_rate — fraction of `answered`
    # responses from the live qa-eval harness that cited at least one
    # of the retrieved chunks. Measured by the workflow_dispatch
    # qa-eval-refresh path (qa.go's CheckCitations is now enforce — so
    # any answered response on a refreshed baseline already passed the
    # guardrail). The threshold catches a regression in the underlying
    # rate (e.g. a regex change that lets ungrounded answers through).
    threshold = thresholds.get("citation_rate_min_delta")
    base = prior_metrics.get("citation_rate")
    cur = current_metrics.get("citation_rate")
    if threshold is None or not isinstance(base, (int, float)) or not isinstance(cur, (int, float)):
        _add("citation_rate", base, cur, threshold, "min_delta", True, skipped=True, note="missing metric or threshold")
    else:
        delta = cur - base
        passed = delta >= float(threshold)
        note = "" if passed else f"dropped {delta:+.4f}; threshold floor is {float(threshold):+.4f}"
        _add("citation_rate", base, cur, threshold, "min_delta", passed, note=note)

    # SCRUM-566 (Slice 4b): groundedness_rate — fraction of `answered`
    # responses where the grounding LLM-as-judge verdict was `grounded`
    # (every factual claim supported by the cited chunks). Measured by
    # the qa-eval-refresh workflow (CheckGrounding fires inline in
    # qa.go on every QA request that passed CheckCitations and was
    # not rate-limited). May drop by at most the configured amount
    # before WARN. Negative = drop allowed.
    threshold = thresholds.get("groundedness_rate_min_delta")
    base = prior_metrics.get("groundedness_rate")
    cur = current_metrics.get("groundedness_rate")
    if threshold is None or not isinstance(base, (int, float)) or not isinstance(cur, (int, float)):
        _add("groundedness_rate", base, cur, threshold, "min_delta", True, skipped=True, note="missing metric or threshold")
    else:
        delta = cur - base
        passed = delta >= float(threshold)
        note = "" if passed else f"dropped {delta:+.4f}; threshold floor is {float(threshold):+.4f}"
        _add("groundedness_rate", base, cur, threshold, "min_delta", passed, note=note)

    return evals


def main(argv: list[str] | None = None) -> int:
    parser = argparse.ArgumentParser(description="qa-eval baseline-compare CI gate (SCRUM-562).")
    parser.add_argument("--baseline", type=Path, default=DEFAULT_BASELINE)
    parser.add_argument("--thresholds", type=Path, default=DEFAULT_THRESHOLDS)
    parser.add_argument("--base-ref", default=DEFAULT_BASE_REF)
    parser.add_argument("--output", type=Path, default=DEFAULT_OUTPUT)
    parser.add_argument(
        "--skip-cli-sanity",
        action="store_true",
        help="Skip the run_qa_eval.py --help sanity check (used by unit tests).",
    )
    args = parser.parse_args(argv)

    try:
        current = load_json_file(args.baseline)
        thresholds = load_yaml_file(args.thresholds)
        if not args.skip_cli_sanity:
            harness_cli_sanity()
        prior = load_base_baseline(args.base_ref, args.baseline)
    except CIError as exc:
        print(f"qa-eval-ci: ERROR — {exc}", file=sys.stderr)
        return 2

    current_metrics = current.get("metrics") or {}
    prior_metrics = prior.get("metrics") if isinstance(prior, dict) else None

    evaluations = evaluate_thresholds(current_metrics, prior_metrics, thresholds)
    overall_pass = all(e["pass"] for e in evaluations)

    summary = {
        "schema": "qa-eval-summary/v1",
        "generated_at": dt.datetime.now(dt.timezone.utc).isoformat(),
        "baseline_source": {
            "path": _repo_relative(args.baseline),
            "schema": current.get("schema"),
            "source_recorded_at": current.get("source_recorded_at"),
            "source_commit": current.get("source_commit"),
        },
        "base_ref": args.base_ref,
        "prior_baseline_present": prior is not None,
        "current_metrics": current_metrics,
        "prior_metrics": prior_metrics,
        "threshold_evaluations": evaluations,
        "overall_pass": overall_pass,
        "notes": [
            "Per-PR run is baseline-compare-only; not a live measurement.",
            "Live refresh via .github/workflows/qa-eval-refresh.yml (workflow_dispatch).",
            "refusal_when_oos_rate + legitimate_false_positive_rate landed in Slice 3 (SCRUM-564) — measured by the Go input-guardrail eval test against eval/qa/fixture_input_guardrail.json.",
            "citation_rate landed in Slice 4a (SCRUM-565) and is now computed by run_qa_eval.py from guardrail-refusal HTTP responses (SCRUM-571). The metric here gates baseline regressions; the live qa-eval-refresh path produces the values.",
            "groundedness_rate landed in Slice 4b (SCRUM-566) and is now computed by run_qa_eval.py from guardrail-refusal HTTP responses (SCRUM-571). Per-user rate-limited via GUARDRAIL_JUDGE_MAX_PER_USER_PER_HOUR (default 100).",
        ],
    }

    args.output.parent.mkdir(parents=True, exist_ok=True)
    args.output.write_text(json.dumps(summary, indent=2) + "\n", encoding="utf-8")
    print(f"qa-eval-ci: wrote {_repo_relative(args.output)}")
    for e in evaluations:
        status = "skip" if e.get("skipped") else ("pass" if e["pass"] else "FAIL")
        print(f"  [{status}] {e['metric']}: {e.get('note') or '(within threshold)'}")
    print(f"qa-eval-ci: overall_pass={overall_pass}")

    return 0 if overall_pass else 1


if __name__ == "__main__":
    raise SystemExit(main())
