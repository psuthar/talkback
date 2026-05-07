"""POST /extract/file — multipart upload of supported file types -> markdown.

SCRUM-331 (under SCRUM-329 spreadsheet-ingestion epic). Initially scoped to
CSV / XLS / XLSX; the upstream MarkItDown SDK handles each natively. The Go
backend's session-materials handler will call this endpoint when a creator
uploads a spreadsheet, store the returned markdown in materials.extracted_text,
and the frontend will render it via react-markdown + remark-gfm.

Contract:
- Auth: bearer-token (same shared secret as /extract/image, /extract/url).
- Body: multipart with `file` field. Optional form field `file_extension`
  overrides the filename-derived extension when callers stream raw bytes
  with no filename.
- Allowlist: csv / xls / xlsx by default. Env override
  `SIDECAR_FILE_ALLOWED_EXTENSIONS` accepts a comma-separated list of
  extensions (with or without the leading dot).
- Cap: default 5 MiB body. Env override `SIDECAR_FILE_MAX_BYTES`.
- 200: {"text": "<markdown>", "extension": ".csv", "bytes": N}
- 413: oversized body, code=spreadsheet_too_large
- 415: extension not in allowlist, code=unsupported_extension
- 422: malformed input that markitdown can't parse, code=extraction_failed
- 500: dependency missing / unknown failure
"""

from __future__ import annotations

import logging
import os
from typing import Optional

from fastapi import APIRouter, Depends, Form, HTTPException, Request, UploadFile, status
from fastapi.responses import JSONResponse

from app.auth import require_bearer
from app.markitdown_wrapper import ExtractionError, extract_file_to_markdown


_logger = logging.getLogger("markitdown_sidecar")

# Default 5 MiB. Spreadsheets at this scale already strain pandas / openpyxl;
# the cap is the inner-defense layer behind any upload-side cap the Go
# handler may add (SCRUM-334).
_DEFAULT_MAX_BYTES = 5 * 1024 * 1024
_DEFAULT_ALLOWED_EXTENSIONS = (".csv", ".xls", ".xlsx")


def _max_bytes() -> int:
    raw = os.environ.get("SIDECAR_FILE_MAX_BYTES")
    if not raw:
        return _DEFAULT_MAX_BYTES
    try:
        value = int(raw)
    except ValueError:
        return _DEFAULT_MAX_BYTES
    return value if value > 0 else _DEFAULT_MAX_BYTES


def _allowed_extensions() -> tuple[str, ...]:
    raw = os.environ.get("SIDECAR_FILE_ALLOWED_EXTENSIONS")
    if not raw:
        return _DEFAULT_ALLOWED_EXTENSIONS
    parts = [p.strip().lower() for p in raw.split(",") if p.strip()]
    out: list[str] = []
    for p in parts:
        out.append(p if p.startswith(".") else "." + p)
    return tuple(out) if out else _DEFAULT_ALLOWED_EXTENSIONS


def _resolve_extension(filename: Optional[str], explicit: Optional[str]) -> str:
    """Prefer the explicit form field, fall back to the filename suffix.

    Returns a lowercase extension with a leading dot, or "" if neither
    source supplies one.
    """
    candidate = (explicit or "").strip().lower()
    if not candidate and filename:
        _, _, suffix = filename.rpartition(".")
        if suffix and suffix != filename:
            candidate = "." + suffix.lower()
    if candidate and not candidate.startswith("."):
        candidate = "." + candidate
    return candidate


router = APIRouter()


@router.post("/extract/file", dependencies=[Depends(require_bearer)])
async def extract_file(
    request: Request,
    file: UploadFile,
    file_extension: Optional[str] = Form(default=None),
) -> JSONResponse:
    extension = _resolve_extension(file.filename, file_extension)
    allowed = _allowed_extensions()
    if not extension:
        raise HTTPException(
            status_code=status.HTTP_415_UNSUPPORTED_MEDIA_TYPE,
            detail={
                "error": "unsupported_extension",
                "extension": "",
                "allowed": list(allowed),
            },
        )
    if extension not in allowed:
        raise HTTPException(
            status_code=status.HTTP_415_UNSUPPORTED_MEDIA_TYPE,
            detail={
                "error": "unsupported_extension",
                "extension": extension,
                "allowed": list(allowed),
            },
        )

    cap = _max_bytes()
    file_bytes = await file.read(cap + 1)
    if len(file_bytes) > cap:
        raise HTTPException(
            status_code=413,
            detail={
                "error": "spreadsheet_too_large",
                "max_bytes": cap,
            },
        )

    try:
        result = extract_file_to_markdown(
            file_bytes=file_bytes,
            file_extension=extension,
        )
    except ExtractionError as err:
        _logger.warning(
            "file extraction failed: code=%s message=%s extension=%s",
            err.code,
            str(err),
            extension,
        )
        # 422 for caller-side input issues (malformed CSV / corrupt XLSX);
        # 500 only when the sidecar itself is misconfigured.
        if err.code in {"extraction_failed", "empty_body", "missing_extension"}:
            return JSONResponse(
                status_code=status.HTTP_422_UNPROCESSABLE_ENTITY,
                content={"error": err.code, "message": str(err)},
            )
        return JSONResponse(
            status_code=status.HTTP_500_INTERNAL_SERVER_ERROR,
            content={"error": err.code, "message": str(err)},
        )

    extra = getattr(request.state, "log_extra", {}) or {}
    extra["operation"] = "extract.file"
    extra["extension"] = extension
    extra["bytes_in"] = len(file_bytes)
    request.state.log_extra = extra

    return JSONResponse(
        status_code=status.HTTP_200_OK,
        content={
            "text": result.text,
            "extension": result.extension,
            "bytes": result.bytes_in,
        },
    )
