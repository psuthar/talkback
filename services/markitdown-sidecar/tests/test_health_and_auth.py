"""Auth + health behavior for the sidecar skeleton."""

from __future__ import annotations

import pytest
from fastapi.testclient import TestClient


@pytest.fixture
def client(monkeypatch: pytest.MonkeyPatch) -> TestClient:
    monkeypatch.setenv("SIDECAR_SECRET", "test-secret")
    # Reload the app module so it picks up the env var.
    import importlib

    import app.main as main_module

    importlib.reload(main_module)
    return TestClient(main_module.app)


def test_healthz_does_not_require_auth(client: TestClient) -> None:
    res = client.get("/healthz")
    assert res.status_code == 200
    body = res.json()
    assert body["status"] == "ok"
    assert "version" in body


def test_healthz_supports_head_method(client: TestClient) -> None:
    # Free-tier ping monitors (UptimeRobot's free plan) send HEAD by default.
    # HEAD on /healthz must return 200 with no body, per RFC 9110.
    res = client.head("/healthz")
    assert res.status_code == 200
    assert res.content == b""


def test_protected_route_rejects_missing_bearer(client: TestClient) -> None:
    res = client.post("/extract/image")
    assert res.status_code == 401
    assert res.json()["detail"]["error"] == "missing_bearer_token"


def test_protected_route_rejects_wrong_bearer(client: TestClient) -> None:
    res = client.post("/extract/image", headers={"Authorization": "Bearer wrong"})
    assert res.status_code == 401
    assert res.json()["detail"]["error"] == "invalid_bearer_token"


def test_protected_route_rejects_non_bearer_scheme(client: TestClient) -> None:
    res = client.post("/extract/image", headers={"Authorization": "Basic dXNlcjpwYXNz"})
    assert res.status_code == 401
    assert res.json()["detail"]["error"] == "missing_bearer_token"


def test_protected_route_passes_auth_with_correct_bearer(client: TestClient) -> None:
    # No multipart body — body validation runs after auth, so a 422 here proves
    # auth accepted the bearer and the request reached the body validator.
    # End-to-end happy path is covered by tests/test_extract_image.py.
    res = client.post("/extract/image", headers={"Authorization": "Bearer test-secret"})
    assert res.status_code == 422


def test_response_includes_request_id_header(client: TestClient) -> None:
    res = client.get("/healthz")
    assert "x-request-id" in res.headers


def test_caller_supplied_request_id_is_echoed(client: TestClient) -> None:
    res = client.get("/healthz", headers={"x-request-id": "trace-abc"})
    assert res.headers["x-request-id"] == "trace-abc"


def test_protected_route_500_when_secret_missing(monkeypatch: pytest.MonkeyPatch) -> None:
    monkeypatch.delenv("SIDECAR_SECRET", raising=False)
    import importlib

    import app.main as main_module

    importlib.reload(main_module)
    client = TestClient(main_module.app)
    res = client.post("/extract/image", headers={"Authorization": "Bearer anything"})
    assert res.status_code == 500
    assert res.json()["detail"]["error"] == "sidecar_misconfigured"
