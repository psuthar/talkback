"""POST /extract/image — multipart upload, returns markdown + model + tokens_used."""

from __future__ import annotations

import logging
import os

from fastapi import APIRouter, Depends, HTTPException, Request, UploadFile, status
from fastapi.responses import JSONResponse

from app.auth import require_bearer
from app.markitdown_wrapper import ExtractionError, extract_image_to_markdown


_logger = logging.getLogger("markitdown_sidecar")

# Body size cap; default 10 MB. Env override lets ops dial this up for
# unusual workloads without redeploying the sidecar.
_DEFAULT_MAX_BYTES = 10 * 1024 * 1024


def _max_bytes() -> int:
    raw = os.environ.get("SIDECAR_IMAGE_MAX_BYTES")
    if not raw:
        return _DEFAULT_MAX_BYTES
    try:
        value = int(raw)
    except ValueError:
        return _DEFAULT_MAX_BYTES
    return value if value > 0 else _DEFAULT_MAX_BYTES


router = APIRouter()


@router.post("/extract/image", dependencies=[Depends(require_bearer)])
async def extract_image(request: Request, file: UploadFile) -> JSONResponse:
    content_type = (file.content_type or "").lower()
    if not content_type.startswith("image/"):
        raise HTTPException(
            status_code=status.HTTP_415_UNSUPPORTED_MEDIA_TYPE,
            detail={"error": "unsupported_media_type", "content_type": content_type},
        )

    cap = _max_bytes()
    image_bytes = await file.read(cap + 1)
    if len(image_bytes) > cap:
        # Use the literal 413 — Starlette renamed the constant; this avoids
        # a fragile import and a deprecation warning either way.
        raise HTTPException(
            status_code=413,
            detail={"error": "image_too_large", "max_bytes": cap},
        )

    try:
        result = extract_image_to_markdown(
            image_bytes=image_bytes,
            content_type=content_type,
        )
    except ExtractionError as err:
        _logger.warning(
            "image extraction failed: code=%s message=%s",
            err.code,
            str(err),
        )
        return JSONResponse(
            status_code=status.HTTP_500_INTERNAL_SERVER_ERROR,
            content={"error": err.code, "message": str(err)},
        )

    # Stash telemetry on request.state so the logging middleware (and
    # SCRUM-306) can pick it up alongside the access log.
    extra = getattr(request.state, "log_extra", {}) or {}
    extra["tokens_used"] = result.tokens_used
    extra["model"] = result.model
    extra["operation"] = "extract.image"
    request.state.log_extra = extra

    return JSONResponse(
        status_code=status.HTTP_200_OK,
        content={
            "text": result.text,
            "model": result.model,
            "tokens_used": result.tokens_used,
        },
    )
