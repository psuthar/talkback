"""Behavior tests for POST /extract/image (SCRUM-300).

Tests stub `app.markitdown_wrapper.extract_image_to_markdown` so they run
without OpenAI access or even MarkItDown installed. All branches of the
endpoint contract are exercised: 200, 401, 413, 415, 500.
"""

from __future__ import annotations

import io

import pytest
from fastapi.testclient import TestClient

from app import markitdown_wrapper as mwrap


_AUTH = {"Authorization": "Bearer test-secret"}


@pytest.fixture
def client(monkeypatch: pytest.MonkeyPatch) -> TestClient:
    monkeypatch.setenv("SIDECAR_SECRET", "test-secret")
    monkeypatch.delenv("SIDECAR_IMAGE_MAX_BYTES", raising=False)
    import importlib

    import app.main as main_module

    importlib.reload(main_module)
    return TestClient(main_module.app)


def _stub_result(text: str, *, model: str = "gpt-4o-mini", tokens: int = 0):
    def _fn(*, image_bytes: bytes, content_type: str, **_kwargs):
        return mwrap.ImageExtractResult(text=text, model=model, tokens_used=tokens)

    return _fn


def _stub_failure(code: str, message: str):
    def _fn(*, image_bytes: bytes, content_type: str, **_kwargs):
        raise mwrap.ExtractionError(message, code=code)

    return _fn


# ─── 200 happy path ────────────────────────────────────────────────────────


def test_returns_markdown_for_valid_image(
    client: TestClient, monkeypatch: pytest.MonkeyPatch
) -> None:
    monkeypatch.setattr(
        "app.extract_image.extract_image_to_markdown",
        _stub_result("# Slide title\n\nBody text from the image."),
    )
    files = {"file": ("slide.png", io.BytesIO(b"fake-png-bytes"), "image/png")}
    res = client.post("/extract/image", headers=_AUTH, files=files)
    assert res.status_code == 200, res.text
    body = res.json()
    assert body["text"] == "# Slide title\n\nBody text from the image."
    assert body["model"] == "gpt-4o-mini"
    assert body["tokens_used"] == 0


def test_passes_jpeg_content_type_through(
    client: TestClient, monkeypatch: pytest.MonkeyPatch
) -> None:
    captured: dict[str, str] = {}

    def _capture(*, image_bytes: bytes, content_type: str, **_kwargs):
        captured["content_type"] = content_type
        captured["len"] = str(len(image_bytes))
        return mwrap.ImageExtractResult(text="ok", model="m", tokens_used=0)

    monkeypatch.setattr("app.extract_image.extract_image_to_markdown", _capture)
    files = {"file": ("photo.jpg", io.BytesIO(b"jpeg-bytes"), "image/jpeg")}
    res = client.post("/extract/image", headers=_AUTH, files=files)
    assert res.status_code == 200
    assert captured["content_type"] == "image/jpeg"
    assert captured["len"] == "10"


# ─── 401 ───────────────────────────────────────────────────────────────────


def test_rejects_missing_bearer(client: TestClient) -> None:
    files = {"file": ("slide.png", io.BytesIO(b"x"), "image/png")}
    res = client.post("/extract/image", files=files)
    assert res.status_code == 401
    assert res.json()["detail"]["error"] == "missing_bearer_token"


# ─── 415 wrong content type ───────────────────────────────────────────────


def test_rejects_non_image_content_type(client: TestClient) -> None:
    files = {"file": ("note.txt", io.BytesIO(b"hello"), "text/plain")}
    res = client.post("/extract/image", headers=_AUTH, files=files)
    assert res.status_code == 415
    assert res.json()["detail"]["error"] == "unsupported_media_type"


# ─── 413 oversized body ───────────────────────────────────────────────────


def test_rejects_oversized_body(
    client: TestClient, monkeypatch: pytest.MonkeyPatch
) -> None:
    monkeypatch.setenv("SIDECAR_IMAGE_MAX_BYTES", "1024")
    # Reload so the cap helper picks up the new env value.
    import importlib

    import app.main as main_module

    importlib.reload(main_module)
    local_client = TestClient(main_module.app)

    big = b"a" * 2048
    files = {"file": ("slide.png", io.BytesIO(big), "image/png")}
    res = local_client.post("/extract/image", headers=_AUTH, files=files)
    assert res.status_code == 413
    body = res.json()
    assert body["detail"]["error"] == "image_too_large"
    assert body["detail"]["max_bytes"] == 1024


# ─── 500 with stable error code on extraction failure ────────────────────


def test_returns_500_with_stable_code_on_extraction_failure(
    client: TestClient, monkeypatch: pytest.MonkeyPatch
) -> None:
    monkeypatch.setattr(
        "app.extract_image.extract_image_to_markdown",
        _stub_failure("extraction_failed", "boom"),
    )
    files = {"file": ("slide.png", io.BytesIO(b"x"), "image/png")}
    res = client.post("/extract/image", headers=_AUTH, files=files)
    assert res.status_code == 500
    body = res.json()
    assert body["error"] == "extraction_failed"
    assert body["message"] == "boom"


def test_returns_500_with_llm_misconfigured_when_key_missing(
    client: TestClient, monkeypatch: pytest.MonkeyPatch
) -> None:
    monkeypatch.setattr(
        "app.extract_image.extract_image_to_markdown",
        _stub_failure("llm_misconfigured", "OPENAI_API_KEY not set"),
    )
    files = {"file": ("slide.png", io.BytesIO(b"x"), "image/png")}
    res = client.post("/extract/image", headers=_AUTH, files=files)
    assert res.status_code == 500
    assert res.json()["error"] == "llm_misconfigured"


# ─── wrapper unit tests (no monkeypatch, no markitdown call) ─────────────


def test_wrapper_raises_empty_body() -> None:
    with pytest.raises(mwrap.ExtractionError) as excinfo:
        mwrap.extract_image_to_markdown(image_bytes=b"", content_type="image/png")
    assert excinfo.value.code == "empty_body"


def test_wrapper_raises_llm_misconfigured_without_key(
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    monkeypatch.delenv("OPENAI_API_KEY", raising=False)
    with pytest.raises(mwrap.ExtractionError) as excinfo:
        mwrap.extract_image_to_markdown(image_bytes=b"x", content_type="image/png")
    assert excinfo.value.code == "llm_misconfigured"


@pytest.mark.parametrize(
    "content_type,expected",
    [
        ("image/png", ".png"),
        ("image/jpeg", ".jpg"),
        ("image/gif", ".gif"),
        ("image/webp", ".webp"),
        ("", ".bin"),
    ],
)
def test_extension_guessing(content_type: str, expected: str) -> None:
    assert mwrap._guess_extension(content_type) == expected
