"""Shared-secret bearer auth dependency.

The TalkBack Go backend sets `MARKITDOWN_SIDECAR_SECRET`; the sidecar reads
the same value from `SIDECAR_SECRET`. Requests must carry
`Authorization: Bearer <secret>`. The constant-time comparison avoids
length-leak side channels.
"""

from __future__ import annotations

import hmac
import os
from typing import Optional

from fastapi import Header, HTTPException, status


_BEARER_PREFIX = "Bearer "


def _expected_secret() -> Optional[str]:
    secret = os.environ.get("SIDECAR_SECRET")
    if secret is None or secret == "":
        return None
    return secret


async def require_bearer(authorization: Optional[str] = Header(default=None)) -> None:
    """FastAPI dependency: 401 unless `Authorization: Bearer <SIDECAR_SECRET>` matches."""
    expected = _expected_secret()
    if expected is None:
        # Fail-closed: refuse to serve protected routes when the secret is missing.
        raise HTTPException(
            status_code=status.HTTP_500_INTERNAL_SERVER_ERROR,
            detail={"error": "sidecar_misconfigured", "message": "SIDECAR_SECRET not set"},
        )
    if authorization is None or not authorization.startswith(_BEARER_PREFIX):
        raise HTTPException(
            status_code=status.HTTP_401_UNAUTHORIZED,
            detail={"error": "missing_bearer_token"},
        )
    presented = authorization[len(_BEARER_PREFIX):]
    if not hmac.compare_digest(presented.encode("utf-8"), expected.encode("utf-8")):
        raise HTTPException(
            status_code=status.HTTP_401_UNAUTHORIZED,
            detail={"error": "invalid_bearer_token"},
        )
