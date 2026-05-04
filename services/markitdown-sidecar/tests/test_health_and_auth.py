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


def test_protected_route_rejects_missing_bearer(client: TestClient) -> None:
    res = client.post("/extract/_ping")
    assert res.status_code == 401
    assert res.json()["detail"]["error"] == "missing_bearer_token"


def test_protected_route_rejects_wrong_bearer(client: TestClient) -> None:
    res = client.post("/extract/_ping", headers={"Authorization": "Bearer wrong"})
    assert res.status_code == 401
    assert res.json()["detail"]["error"] == "invalid_bearer_token"


def test_protected_route_rejects_non_bearer_scheme(client: TestClient) -> None:
    res = client.post("/extract/_ping", headers={"Authorization": "Basic dXNlcjpwYXNz"})
    assert res.status_code == 401
    assert res.json()["detail"]["error"] == "missing_bearer_token"


def test_protected_route_accepts_correct_bearer(client: TestClient) -> None:
    res = client.post("/extract/_ping", headers={"Authorization": "Bearer test-secret"})
    assert res.status_code == 200
    assert res.json() == {"ok": True}


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
    res = client.post("/extract/_ping", headers={"Authorization": "Bearer anything"})
    assert res.status_code == 500
    assert res.json()["detail"]["error"] == "sidecar_misconfigured"
