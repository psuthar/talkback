"""POST /extract/url — fetch a URL and convert response HTML to markdown.

Mirrors the existing Go `internal/urlextract` package's defaults (15s timeout,
2 MB body cap) so SCRUM-304's fallback path produces comparable behavior on
sidecar failure.
"""

from __future__ import annotations

import logging
from typing import Optional

from fastapi import APIRouter, Depends, HTTPException, Request, status
from fastapi.responses import JSONResponse
from pydantic import BaseModel, Field

from app.auth import require_bearer
from app.markitdown_wrapper import (
    ExtractionError,
    UpstreamHTTPError,
    extract_url_to_markdown,
)


_logger = logging.getLogger("markitdown_sidecar")


class ExtractURLRequest(BaseModel):
    url: str = Field(min_length=1, description="Absolute URL to fetch and convert")
    max_bytes: Optional[int] = Field(
        default=None, gt=0, description="Override SIDECAR_URL_MAX_BYTES for this request"
    )
    timeout_s: Optional[float] = Field(
        default=None, gt=0, description="Override SIDECAR_URL_TIMEOUT_S for this request"
    )


router = APIRouter()


@router.post("/extract/url", dependencies=[Depends(require_bearer)])
async def extract_url(request: Request, body: ExtractURLRequest) -> JSONResponse:
    if not body.url.lower().startswith(("http://", "https://")):
        raise HTTPException(
            status_code=status.HTTP_400_BAD_REQUEST,
            detail={"error": "invalid_url", "url": body.url},
        )

    try:
        result = extract_url_to_markdown(
            url=body.url,
            max_bytes=body.max_bytes,
            timeout_s=body.timeout_s,
        )
    except UpstreamHTTPError as err:
        # Pass the upstream 4xx through so callers (and SCRUM-304's Go
        # fallback decision) can react to "upstream said 404" vs "sidecar
        # is broken".
        _logger.info(
            "url extraction upstream %s for %s",
            err.status_code,
            body.url,
        )
        return JSONResponse(
            status_code=err.status_code,
            content={
                "error": "upstream_http_error",
                "status_code": err.status_code,
                "fetched_url": err.fetched_url,
                "message": str(err),
            },
        )
    except ExtractionError as err:
        # Body-too-large is a request-shape issue; surface as 413 to mirror
        # the image endpoint's contract. Everything else is a 5xx.
        if err.code == "body_too_large":
            return JSONResponse(
                status_code=413,
                content={"error": err.code, "message": str(err)},
            )
        _logger.warning("url extraction failed: code=%s message=%s", err.code, str(err))
        return JSONResponse(
            status_code=status.HTTP_500_INTERNAL_SERVER_ERROR,
            content={"error": err.code, "message": str(err)},
        )

    extra = getattr(request.state, "log_extra", {}) or {}
    extra["operation"] = "extract.url"
    extra["fetched_url"] = result.fetched_url
    extra["upstream_status"] = result.status_code
    request.state.log_extra = extra

    return JSONResponse(
        status_code=status.HTTP_200_OK,
        content={
            "text": result.text,
            "title": result.title,
            "fetched_url": result.fetched_url,
            "status_code": result.status_code,
        },
    )
