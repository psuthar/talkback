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

# Mirror internal/urlextract/urlextract.go so the sidecar and legacy fallback
# fetch with identical headers — Wikipedia and others 403 the default httpx UA.
_BROWSER_USER_AGENT = (
    "Mozilla/5.0 (Windows NT 10.0; Win64; x64) "
    "AppleWebKit/537.36 (KHTML, like Gecko) "
    "Chrome/120.0.0.0 Safari/537.36"
)


@dataclass(frozen=True)
class ImageExtractResult:
    text: str
    model: str
    tokens_used: int


@dataclass(frozen=True)
class URLExtractResult:
    text: str
    title: str
    fetched_url: str
    status_code: int


@dataclass(frozen=True)
class FileExtractResult:
    """Generic file → markdown result (SCRUM-331).

    Distinct from ImageExtractResult because spreadsheet/document extraction
    via MarkItDown's built-in converters (pandas / openpyxl / xlrd / csv)
    does not consume LLM tokens — `tokens_used` would always be zero, so we
    omit it instead of carrying a dead field.
    """

    text: str
    extension: str
    bytes_in: int


class ExtractionError(Exception):
    """Raised when MarkItDown / the upstream LLM cannot produce a result."""

    def __init__(self, message: str, *, code: str = "extraction_failed") -> None:
        super().__init__(message)
        self.code = code


class UpstreamHTTPError(Exception):
    """Raised when the upstream URL returns a 4xx — caller should pass-through."""

    def __init__(self, status_code: int, message: str, *, fetched_url: str = "") -> None:
        super().__init__(message)
        self.status_code = status_code
        self.fetched_url = fetched_url


# Default URL extraction limits — mirror the existing Go urlextract package
# (15s timeout, 2 MB body cap) so behavior is comparable when SCRUM-304's
# fallback kicks in.
_DEFAULT_URL_TIMEOUT_S = 15.0
_DEFAULT_URL_MAX_BYTES = 2 * 1024 * 1024


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


def _resolve_url_timeout(override: Optional[float]) -> float:
    if override is not None and override > 0:
        return override
    raw = os.environ.get("SIDECAR_URL_TIMEOUT_S")
    if raw:
        try:
            value = float(raw)
            if value > 0:
                return value
        except ValueError:
            pass
    return _DEFAULT_URL_TIMEOUT_S


def _resolve_url_max_bytes(override: Optional[int]) -> int:
    if override is not None and override > 0:
        return override
    raw = os.environ.get("SIDECAR_URL_MAX_BYTES")
    if raw:
        try:
            value = int(raw)
            if value > 0:
                return value
        except ValueError:
            pass
    return _DEFAULT_URL_MAX_BYTES


def extract_file_to_markdown(
    *,
    file_bytes: bytes,
    file_extension: str,
) -> FileExtractResult:
    """Convert arbitrary supported file bytes to markdown via MarkItDown (SCRUM-331).

    No LLM call — MarkItDown's CSV / XLSX / XLS converters use pandas /
    openpyxl / xlrd locally and emit deterministic markdown tables (one
    `## SheetName` heading per sheet for multi-sheet workbooks).

    Tests typically monkeypatch this function entirely. The real
    implementation runs against the actual MarkItDown SDK so the contract
    is exercised in pytest CI.
    """
    if not file_bytes:
        raise ExtractionError("empty file body", code="empty_body")
    extension = (file_extension or "").lower().strip()
    if extension and not extension.startswith("."):
        extension = "." + extension
    if not extension:
        raise ExtractionError("missing file extension", code="missing_extension")

    try:
        from markitdown import MarkItDown
    except ImportError as exc:  # pragma: no cover — surfaces only if deps drift
        raise ExtractionError(
            f"markitdown not installed: {exc}",
            code="dependency_missing",
        )

    converter = MarkItDown()
    stream = io.BytesIO(file_bytes)
    try:
        result = converter.convert_stream(stream, file_extension=extension)
    except Exception as exc:  # depends on MarkItDown / pandas / openpyxl internals
        raise ExtractionError(
            f"markitdown conversion failed: {exc}",
            code="extraction_failed",
        )

    text = (getattr(result, "text_content", "") or "").strip()
    return FileExtractResult(
        text=text,
        extension=extension,
        bytes_in=len(file_bytes),
    )


def extract_url_to_markdown(
    *,
    url: str,
    max_bytes: Optional[int] = None,
    timeout_s: Optional[float] = None,
) -> URLExtractResult:
    """Fetch `url` and convert the HTML response to markdown via MarkItDown.

    On upstream 4xx, raises `UpstreamHTTPError` so the endpoint can mirror
    the upstream status to the caller. On internal failure raises
    `ExtractionError` with a stable code.

    Tests typically monkeypatch this function entirely; the body still
    works end-to-end against a real URL when invoked directly.
    """
    if not url:
        raise ExtractionError("empty url", code="empty_url")

    cap = _resolve_url_max_bytes(max_bytes)
    timeout = _resolve_url_timeout(timeout_s)

    try:
        import httpx
    except ImportError as exc:  # pragma: no cover
        raise ExtractionError(
            f"httpx not installed: {exc}",
            code="dependency_missing",
        )

    try:
        with httpx.Client(
            timeout=timeout,
            follow_redirects=True,
            headers={"User-Agent": _BROWSER_USER_AGENT},
        ) as client:
            try:
                resp = client.get(url)
            except httpx.TimeoutException as exc:
                raise ExtractionError(f"upstream timeout: {exc}", code="timeout")
            except httpx.HTTPError as exc:
                raise ExtractionError(f"fetch failed: {exc}", code="fetch_failed")
    except ExtractionError:
        raise

    fetched_url = str(resp.url)
    if 400 <= resp.status_code < 500:
        raise UpstreamHTTPError(
            resp.status_code,
            f"upstream returned {resp.status_code}",
            fetched_url=fetched_url,
        )
    if resp.status_code >= 500:
        raise ExtractionError(
            f"upstream returned {resp.status_code}",
            code="fetch_failed",
        )

    body_bytes = resp.content
    if len(body_bytes) > cap:
        raise ExtractionError(
            f"upstream body exceeds cap ({len(body_bytes)} > {cap})",
            code="body_too_large",
        )

    try:
        from markitdown import MarkItDown
    except ImportError as exc:  # pragma: no cover
        raise ExtractionError(
            f"markitdown not installed: {exc}",
            code="dependency_missing",
        )

    converter = MarkItDown()
    try:
        result = converter.convert_stream(io.BytesIO(body_bytes), file_extension=".html")
    except Exception as exc:  # pragma: no cover — depends on MarkItDown internals
        raise ExtractionError(f"markitdown conversion failed: {exc}", code="extraction_failed")

    text = (getattr(result, "text_content", "") or "").strip()
    title = (getattr(result, "title", "") or "").strip()
    return URLExtractResult(
        text=text,
        title=title,
        fetched_url=fetched_url,
        status_code=resp.status_code,
    )
