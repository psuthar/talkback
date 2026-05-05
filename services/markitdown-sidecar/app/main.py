"""MarkItDown sidecar — FastAPI app skeleton.

Public surface today:
- GET  /healthz                — unauthenticated liveness probe
- POST /extract/_ping          — protected stub (proves bearer auth wiring;
                                  removed by SCRUM-300 once /extract/image lands)

Subsequent tickets add /extract/image (SCRUM-300) and /extract/url (SCRUM-301).
"""

from __future__ import annotations

import logging
import os
from typing import Any

from fastapi import Depends, FastAPI

from app.auth import require_bearer
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

    @app.get("/healthz")
    async def healthz() -> dict[str, Any]:
        return {"status": "ok", "version": _VERSION}

    @app.post("/extract/_ping", dependencies=[Depends(require_bearer)])
    async def extract_ping() -> dict[str, Any]:
        return {"ok": True}

    return app


app = create_app()
