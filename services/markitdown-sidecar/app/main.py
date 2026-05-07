"""MarkItDown sidecar — FastAPI app.

Public surface:
- GET  /healthz        — unauthenticated liveness probe
- POST /extract/image  — bearer-auth; multipart image -> markdown caption (SCRUM-300)
- POST /extract/url    — bearer-auth; URL -> markdown via MarkItDown HTML path (SCRUM-301)
- POST /extract/file   — bearer-auth; multipart spreadsheet -> markdown table (SCRUM-331)

SCRUM-306 layers in usage telemetry.
"""

from __future__ import annotations

import logging
import os
from typing import Any

from fastapi import FastAPI

from app.extract_file import router as extract_file_router
from app.extract_image import router as extract_image_router
from app.extract_url import router as extract_url_router
from app.logging_middleware import RequestLoggingMiddleware


_VERSION = os.environ.get("SIDECAR_VERSION", "dev")


def _configure_root_logger() -> None:
    # Idempotent: pytest may import the app multiple times.
    if logging.getLogger().handlers:
        return
    logging.basicConfig(
        level=os.environ.get("SIDECAR_LOG_LEVEL", "INFO"),
        format="%(message)s",
    )


def create_app() -> FastAPI:
    _configure_root_logger()
    app = FastAPI(title="markitdown-sidecar", version=_VERSION)
    app.add_middleware(RequestLoggingMiddleware)

    # GET + HEAD: HEAD is required for free-tier ping monitors (UptimeRobot's
    # free plan only sends HEAD). Per RFC 9110, HEAD on a GET resource returns
    # the same status and headers with no body — FastAPI handles that for us.
    @app.api_route("/healthz", methods=["GET", "HEAD"])
    async def healthz() -> dict[str, Any]:
        return {"status": "ok", "version": _VERSION}

    app.include_router(extract_image_router)
    app.include_router(extract_url_router)
    app.include_router(extract_file_router)
    return app


app = create_app()
