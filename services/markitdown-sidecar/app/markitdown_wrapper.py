"""Thin wrapper over the MarkItDown SDK so tests can monkeypatch one symbol.

Production calls `extract_image_to_markdown(...)`; tests replace the function
on `app.markitdown_wrapper` with a stub that returns canned content. This
keeps the endpoint handler free of MarkItDown / OpenAI imports at module
load and makes pytest fast.
"""

from __future__ import annotations

import io
import mimetypes
import os
from dataclasses import dataclass
from typing import Optional


_DEFAULT_MODEL = "gpt-4o-mini"


@dataclass(frozen=True)
class ImageExtractResult:
    text: str
    model: str
    tokens_used: int


class ExtractionError(Exception):
    """Raised when MarkItDown / the upstream LLM cannot produce a result."""

    def __init__(self, message: str, *, code: str = "extraction_failed") -> None:
        super().__init__(message)
        self.code = code


def _guess_extension(content_type: str) -> str:
    ext = mimetypes.guess_extension(content_type or "")
    if ext:
        return ext
    # Reasonable defaults so MarkItDown's stream conversion picks the right
    # converter even when the client sends an obscure mime type.
    if content_type == "image/jpeg":
        return ".jpg"
    if content_type == "image/png":
        return ".png"
    return ".bin"


def extract_image_to_markdown(
    *,
    image_bytes: bytes,
    content_type: str,
    api_key: Optional[str] = None,
    model: Optional[str] = None,
) -> ImageExtractResult:
    """Convert image bytes to markdown via MarkItDown + an LLM vision model.

    Tests typically monkeypatch this function entirely. The real
    implementation reads the OpenAI key from env if not supplied so callers
    don't need to pipe it through the request handler.
    """
    if not image_bytes:
        raise ExtractionError("empty image body", code="empty_body")
    resolved_key = api_key or os.environ.get("OPENAI_API_KEY")
    if not resolved_key:
        raise ExtractionError(
            "OPENAI_API_KEY not configured on the sidecar",
            code="llm_misconfigured",
        )
    resolved_model = model or os.environ.get("SIDECAR_OPENAI_MODEL") or _DEFAULT_MODEL

    try:
        from markitdown import MarkItDown
        from openai import OpenAI
    except ImportError as exc:  # pragma: no cover — surfaces only if deps drift
        raise ExtractionError(
            f"markitdown/openai not installed: {exc}",
            code="dependency_missing",
        )

    client = OpenAI(api_key=resolved_key)
    converter = MarkItDown(llm_client=client, llm_model=resolved_model)
    stream = io.BytesIO(image_bytes)
    extension = _guess_extension(content_type)
    try:
        result = converter.convert_stream(stream, file_extension=extension)
    except Exception as exc:  # pragma: no cover — depends on MarkItDown internals
        raise ExtractionError(f"markitdown conversion failed: {exc}", code="extraction_failed")

    text = (getattr(result, "text_content", "") or "").strip()
    return ImageExtractResult(text=text, model=resolved_model, tokens_used=0)
