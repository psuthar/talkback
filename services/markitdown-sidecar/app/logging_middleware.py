"""One JSON log line per request with timing + status.

Subsequent tickets (extraction endpoints, telemetry) can extend this by
attaching request-scoped fields (e.g. tokens_used) via `request.state`.
"""

from __future__ import annotations

import json
import logging
import time
import uuid

from starlette.middleware.base import BaseHTTPMiddleware
from starlette.requests import Request
from starlette.responses import Response


_logger = logging.getLogger("markitdown_sidecar")


class RequestLoggingMiddleware(BaseHTTPMiddleware):
    async def dispatch(self, request: Request, call_next):
        request_id = request.headers.get("x-request-id") or uuid.uuid4().hex
        request.state.request_id = request_id
        start = time.perf_counter()
        response: Response
        try:
            response = await call_next(request)
        except Exception:
            duration_ms = int((time.perf_counter() - start) * 1000)
            _logger.exception(
                json.dumps(
                    {
                        "event": "request",
                        "method": request.method,
                        "path": request.url.path,
                        "status_code": 500,
                        "duration_ms": duration_ms,
                        "request_id": request_id,
                    }
                )
            )
            raise
        duration_ms = int((time.perf_counter() - start) * 1000)
        # Pull any per-request fields the handler attached (e.g. tokens_used).
        extra = getattr(request.state, "log_extra", {}) or {}
        record = {
            "event": "request",
            "method": request.method,
            "path": request.url.path,
            "status_code": response.status_code,
            "duration_ms": duration_ms,
            "request_id": request_id,
        }
        record.update(extra)
        _logger.info(json.dumps(record))
        response.headers["x-request-id"] = request_id
        return response
