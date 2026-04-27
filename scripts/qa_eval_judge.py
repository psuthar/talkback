#!/usr/bin/env python3
"""Strict-JSON LLM judge for Q&A eval cases."""

from __future__ import annotations

import json
import urllib.error
import urllib.request
from dataclasses import dataclass
from typing import Any, Callable


DEFAULT_JUDGE_MODEL = "gpt-4o-mini"
DEFAULT_MAX_ATTEMPTS = 2


@dataclass
class JudgeException(Exception):
    code: str
    message: str
    retryable: bool

    def __str__(self) -> str:
        return f"{self.code}: {self.message}"


def _safe_float(value: Any) -> float | None:
    if isinstance(value, (int, float)):
        return float(value)
    return None


def validate_judge_payload(payload: Any) -> dict[str, Any]:
    if not isinstance(payload, dict):
        raise JudgeException(
            code="judge_schema_error",
            message="judge response is not a JSON object",
            retryable=True,
        )

    required = ("is_correct", "hallucination", "score_0_to_1", "reason")
    missing = [k for k in required if k not in payload]
    if missing:
        raise JudgeException(
            code="judge_schema_error",
            message=f"missing required keys: {', '.join(missing)}",
            retryable=True,
        )

    is_correct = payload["is_correct"]
    hallucination = payload["hallucination"]
    score = _safe_float(payload["score_0_to_1"])
    reason = payload["reason"]

    if not isinstance(is_correct, bool):
        raise JudgeException(
            code="judge_schema_error",
            message="is_correct must be boolean",
            retryable=True,
        )
    if not isinstance(hallucination, bool):
        raise JudgeException(
            code="judge_schema_error",
            message="hallucination must be boolean",
            retryable=True,
        )
    if score is None or score < 0.0 or score > 1.0:
        raise JudgeException(
            code="judge_schema_error",
            message="score_0_to_1 must be a number in [0,1]",
            retryable=True,
        )
    if not isinstance(reason, str) or not reason.strip():
        raise JudgeException(
            code="judge_schema_error",
            message="reason must be a non-empty string",
            retryable=True,
        )

    return {
        "is_correct": is_correct,
        "hallucination": hallucination,
        "score_0_to_1": score,
        "reason": reason.strip(),
    }


def parse_judge_text(raw_text: str) -> dict[str, Any]:
    try:
        parsed = json.loads(raw_text)
    except json.JSONDecodeError as e:
        raise JudgeException(
            code="judge_invalid_json",
            message=f"invalid JSON from judge: {e}",
            retryable=True,
        ) from e
    return validate_judge_payload(parsed)


def _build_prompt(
    *,
    case_contract: dict[str, Any],
    score_target: dict[str, Any],
    model_answer: str,
    response_http_status: int | None,
) -> str:
    return (
        "You are grading a TalkBack Q&A answer against strict fixture-backed expectations.\n"
        "Return ONLY valid JSON with keys exactly:\n"
        '{"is_correct": <bool>, "hallucination": <bool>, "score_0_to_1": <number>, "reason": <string>}.\n'
        "No markdown. No extra keys.\n\n"
        f"Case contract:\n{json.dumps(case_contract, ensure_ascii=False)}\n\n"
        f"Score target:\n{json.dumps(score_target, ensure_ascii=False)}\n\n"
        f"SessionAsk HTTP status: {response_http_status}\n"
        f"Model answer text:\n{model_answer}\n\n"
        "Scoring rubric:\n"
        "- is_correct=true only if answer matches expected status and facts/keywords constraints.\n"
        "- hallucination=true when unsupported or contradictory claims are present.\n"
        "- score_0_to_1 should reflect overall quality under the provided target constraints.\n"
        "- Keep reason concise and specific.\n"
    )


def invoke_openai_judge(
    *,
    api_key: str,
    model: str,
    prompt: str,
    timeout_seconds: float,
) -> str:
    url = "https://api.openai.com/v1/chat/completions"
    req_body = {
        "model": model,
        "temperature": 0,
        "response_format": {"type": "json_object"},
        "messages": [
            {
                "role": "system",
                "content": "You are a strict JSON grader for Q&A correctness and hallucination.",
            },
            {"role": "user", "content": prompt},
        ],
    }
    req = urllib.request.Request(
        url,
        data=json.dumps(req_body).encode("utf-8"),
        method="POST",
        headers={
            "Authorization": f"Bearer {api_key}",
            "Content-Type": "application/json",
        },
    )
    try:
        with urllib.request.urlopen(req, timeout=timeout_seconds) as resp:
            payload = json.loads(resp.read().decode("utf-8", errors="replace"))
    except urllib.error.HTTPError as e:
        body = e.read().decode("utf-8", errors="replace")
        if e.code == 429 or 500 <= e.code <= 599:
            raise JudgeException(
                code="judge_http_retryable",
                message=f"HTTP {e.code}: {body}",
                retryable=True,
            ) from e
        raise JudgeException(
            code="judge_http_error",
            message=f"HTTP {e.code}: {body}",
            retryable=False,
        ) from e
    except OSError as e:
        raise JudgeException(
            code="judge_transport_error",
            message=str(e),
            retryable=True,
        ) from e

    choices = payload.get("choices")
    if not isinstance(choices, list) or len(choices) == 0:
        raise JudgeException(
            code="judge_schema_error",
            message="OpenAI response missing choices",
            retryable=True,
        )
    first = choices[0]
    message = first.get("message") if isinstance(first, dict) else None
    content = message.get("content") if isinstance(message, dict) else None
    if not isinstance(content, str) or not content.strip():
        raise JudgeException(
            code="judge_schema_error",
            message="OpenAI response missing choices[0].message.content",
            retryable=True,
        )
    return content


def judge_case_with_retry(
    *,
    case_contract: dict[str, Any],
    score_target: dict[str, Any],
    model_answer: str,
    response_http_status: int | None,
    api_key: str,
    model: str = DEFAULT_JUDGE_MODEL,
    timeout_seconds: float = 45.0,
    max_attempts: int = DEFAULT_MAX_ATTEMPTS,
    invoke_fn: Callable[[str], str] | None = None,
) -> dict[str, Any]:
    if not api_key.strip():
        return {
            "ok": False,
            "error_code": "judge_not_configured",
            "error_message": "OPENAI_API_KEY is required for judge invocation",
            "attempts": 0,
            "verdict": None,
        }

    invoke = invoke_fn
    if invoke is None:
        invoke = lambda p: invoke_openai_judge(  # noqa: E731
            api_key=api_key, model=model, prompt=p, timeout_seconds=timeout_seconds
        )

    prompt = _build_prompt(
        case_contract=case_contract,
        score_target=score_target,
        model_answer=model_answer,
        response_http_status=response_http_status,
    )

    last_error: JudgeException | None = None
    attempts = 0
    for idx in range(max_attempts):
        attempts = idx + 1
        try:
            raw = invoke(prompt)
            verdict = parse_judge_text(raw)
            return {
                "ok": True,
                "error_code": None,
                "error_message": None,
                "attempts": attempts,
                "verdict": verdict,
            }
        except JudgeException as e:
            last_error = e
            if not e.retryable or attempts >= max_attempts:
                break
            continue
        except Exception as e:  # defensive classification
            last_error = JudgeException(
                code="judge_unexpected_error",
                message=str(e),
                retryable=False,
            )
            break

    assert last_error is not None
    return {
        "ok": False,
        "error_code": last_error.code,
        "error_message": last_error.message,
        "attempts": attempts,
        "verdict": None,
    }
