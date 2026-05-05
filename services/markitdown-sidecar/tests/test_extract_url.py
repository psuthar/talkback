"""Behavior tests for POST /extract/url (SCRUM-301).

Tests stub `app.markitdown_wrapper.extract_url_to_markdown` so they run
offline without httpx network calls or MarkItDown installed. Mirrors the
shape of test_extract_image.py.
"""

from __future__ import annotations

import pytest
from fastapi.testclient import TestClient

from app import markitdown_wrapper as mwrap


_AUTH = {"Authorization": "Bearer test-secret"}


@pytest.fixture
def client(monkeypatch: pytest.MonkeyPatch) -> TestClient:
    monkeypatch.setenv("SIDECAR_SECRET", "test-secret")
    monkeypatch.delenv("SIDECAR_URL_MAX_BYTES", raising=False)
    monkeypatch.delenv("SIDECAR_URL_TIMEOUT_S", raising=False)
    import importlib

    import app.main as main_module

    importlib.reload(main_module)
    return TestClient(main_module.app)


def _stub_result(
    text: str = "# Heading\n\n- a\n- b",
    *,
    title: str = "Example Domain",
    fetched_url: str = "https://example.com/",
    status_code: int = 200,
):
    def _fn(*, url: str, max_bytes=None, timeout_s=None):
        return mwrap.URLExtractResult(
            text=text,
            title=title,
            fetched_url=fetched_url,
            status_code=status_code,
        )

    return _fn


def _stub_failure(code: str, message: str):
    def _fn(*, url: str, max_bytes=None, timeout_s=None):
        raise mwrap.ExtractionError(message, code=code)

    return _fn


def _stub_upstream_4xx(status_code: int, fetched_url: str = "https://example.com/missing"):
    def _fn(*, url: str, max_bytes=None, timeout_s=None):
        raise mwrap.UpstreamHTTPError(
            status_code,
            f"upstream returned {status_code}",
            fetched_url=fetched_url,
        )

    return _fn


# ─── 200 happy path ────────────────────────────────────────────────────────


def test_returns_markdown_for_valid_url(
    client: TestClient, monkeypatch: pytest.MonkeyPatch
) -> None:
    monkeypatch.setattr(
        "app.extract_url.extract_url_to_markdown",
        _stub_result(text="# Doc\n\n- bullet 1\n- bullet 2", title="Doc Title"),
    )
    res = client.post(
        "/extract/url",
        headers=_AUTH,
        json={"url": "https://example.com/article"},
    )
    assert res.status_code == 200, res.text
    body = res.json()
    assert body["text"] == "# Doc\n\n- bullet 1\n- bullet 2"
    assert body["title"] == "Doc Title"
    assert body["fetched_url"] == "https://example.com/"
    assert body["status_code"] == 200


def test_passes_overrides_to_wrapper(
    client: TestClient, monkeypatch: pytest.MonkeyPatch
) -> None:
    captured: dict = {}

    def _capture(*, url, max_bytes=None, timeout_s=None):
        captured["url"] = url
        captured["max_bytes"] = max_bytes
        captured["timeout_s"] = timeout_s
        return mwrap.URLExtractResult(
            text="ok", title="t", fetched_url=url, status_code=200
        )

    monkeypatch.setattr("app.extract_url.extract_url_to_markdown", _capture)
    res = client.post(
        "/extract/url",
        headers=_AUTH,
        json={"url": "https://example.com", "max_bytes": 4096, "timeout_s": 5.0},
    )
    assert res.status_code == 200
    assert captured["url"] == "https://example.com"
    assert captured["max_bytes"] == 4096
    assert captured["timeout_s"] == 5.0


# ─── 400 — invalid URL ────────────────────────────────────────────────────


def test_rejects_non_http_scheme(client: TestClient) -> None:
    res = client.post(
        "/extract/url",
        headers=_AUTH,
        json={"url": "ftp://example.com/file"},
    )
    assert res.status_code == 400
    assert res.json()["detail"]["error"] == "invalid_url"


def test_rejects_empty_url(client: TestClient) -> None:
    # Pydantic catches min_length=1 first → 422.
    res = client.post("/extract/url", headers=_AUTH, json={"url": ""})
    assert res.status_code == 422


# ─── 401 ───────────────────────────────────────────────────────────────────


def test_rejects_missing_bearer(client: TestClient) -> None:
    res = client.post("/extract/url", json={"url": "https://example.com"})
    assert res.status_code == 401
    assert res.json()["detail"]["error"] == "missing_bearer_token"


# ─── 4xx pass-through ─────────────────────────────────────────────────────


def test_404_passthrough_when_upstream_404(
    client: TestClient, monkeypatch: pytest.MonkeyPatch
) -> None:
    monkeypatch.setattr(
        "app.extract_url.extract_url_to_markdown",
        _stub_upstream_4xx(404, fetched_url="https://example.com/missing"),
    )
    res = client.post(
        "/extract/url",
        headers=_AUTH,
        json={"url": "https://example.com/missing"},
    )
    assert res.status_code == 404
    body = res.json()
    assert body["error"] == "upstream_http_error"
    assert body["status_code"] == 404
    assert body["fetched_url"] == "https://example.com/missing"


def test_403_passthrough_when_upstream_403(
    client: TestClient, monkeypatch: pytest.MonkeyPatch
) -> None:
    monkeypatch.setattr(
        "app.extract_url.extract_url_to_markdown",
        _stub_upstream_4xx(403),
    )
    res = client.post(
        "/extract/url",
        headers=_AUTH,
        json={"url": "https://example.com/forbidden"},
    )
    assert res.status_code == 403
    assert res.json()["status_code"] == 403


# ─── 413 — body too large maps to 413 ─────────────────────────────────────


def test_413_when_upstream_body_exceeds_cap(
    client: TestClient, monkeypatch: pytest.MonkeyPatch
) -> None:
    monkeypatch.setattr(
        "app.extract_url.extract_url_to_markdown",
        _stub_failure("body_too_large", "upstream body exceeds cap (5000 > 4096)"),
    )
    res = client.post(
        "/extract/url",
        headers=_AUTH,
        json={"url": "https://example.com", "max_bytes": 4096},
    )
    assert res.status_code == 413
    body = res.json()
    assert body["error"] == "body_too_large"


# ─── 500 — stable error codes ─────────────────────────────────────────────


def test_500_on_timeout(client: TestClient, monkeypatch: pytest.MonkeyPatch) -> None:
    monkeypatch.setattr(
        "app.extract_url.extract_url_to_markdown",
        _stub_failure("timeout", "upstream timeout: read"),
    )
    res = client.post("/extract/url", headers=_AUTH, json={"url": "https://example.com"})
    assert res.status_code == 500
    assert res.json()["error"] == "timeout"


def test_500_on_fetch_failure(client: TestClient, monkeypatch: pytest.MonkeyPatch) -> None:
    monkeypatch.setattr(
        "app.extract_url.extract_url_to_markdown",
        _stub_failure("fetch_failed", "DNS lookup failed"),
    )
    res = client.post("/extract/url", headers=_AUTH, json={"url": "https://example.invalid"})
    assert res.status_code == 500
    assert res.json()["error"] == "fetch_failed"


def test_500_on_extraction_failure(client: TestClient, monkeypatch: pytest.MonkeyPatch) -> None:
    monkeypatch.setattr(
        "app.extract_url.extract_url_to_markdown",
        _stub_failure("extraction_failed", "markitdown crashed"),
    )
    res = client.post("/extract/url", headers=_AUTH, json={"url": "https://example.com"})
    assert res.status_code == 500
    assert res.json()["error"] == "extraction_failed"


# ─── wrapper unit tests ──────────────────────────────────────────────────


def test_wrapper_raises_empty_url() -> None:
    with pytest.raises(mwrap.ExtractionError) as excinfo:
        mwrap.extract_url_to_markdown(url="")
    assert excinfo.value.code == "empty_url"


def test_wrapper_resolves_timeout_default(monkeypatch: pytest.MonkeyPatch) -> None:
    monkeypatch.delenv("SIDECAR_URL_TIMEOUT_S", raising=False)
    assert mwrap._resolve_url_timeout(None) == mwrap._DEFAULT_URL_TIMEOUT_S


def test_wrapper_resolves_timeout_env_override(monkeypatch: pytest.MonkeyPatch) -> None:
    monkeypatch.setenv("SIDECAR_URL_TIMEOUT_S", "8.5")
    assert mwrap._resolve_url_timeout(None) == 8.5


def test_wrapper_resolves_timeout_arg_overrides_env(monkeypatch: pytest.MonkeyPatch) -> None:
    monkeypatch.setenv("SIDECAR_URL_TIMEOUT_S", "8.5")
    assert mwrap._resolve_url_timeout(2.0) == 2.0


def test_wrapper_ignores_invalid_timeout_env(monkeypatch: pytest.MonkeyPatch) -> None:
    monkeypatch.setenv("SIDECAR_URL_TIMEOUT_S", "garbage")
    assert mwrap._resolve_url_timeout(None) == mwrap._DEFAULT_URL_TIMEOUT_S


def test_wrapper_resolves_max_bytes_env_override(monkeypatch: pytest.MonkeyPatch) -> None:
    monkeypatch.setenv("SIDECAR_URL_MAX_BYTES", "8192")
    assert mwrap._resolve_url_max_bytes(None) == 8192


def test_wrapper_max_bytes_arg_overrides_env(monkeypatch: pytest.MonkeyPatch) -> None:
    monkeypatch.setenv("SIDECAR_URL_MAX_BYTES", "8192")
    assert mwrap._resolve_url_max_bytes(1024) == 1024
