"""Behavior tests for POST /extract/file (SCRUM-331).

Tests stub `app.markitdown_wrapper.extract_file_to_markdown` so they run
without pandas / openpyxl / xlrd / the upstream MarkItDown SDK installed.
The wrapper itself has its own minimal coverage to confirm the contract
without exercising the heavy dependencies.

Branches covered:
- 200 happy path for CSV / XLSX / XLS
- 200 with explicit file_extension form override (no filename)
- 401 missing bearer
- 413 oversized body (with env override)
- 415 unsupported extension (extension absent + extension not in allowlist)
- 422 caller-input failure (malformed file → markitdown raises)
- env-driven allowlist extension (e.g. add ".tsv")
"""

from __future__ import annotations

import io

import pytest
from fastapi.testclient import TestClient

from app import markitdown_wrapper as mwrap


_AUTH = {"Authorization": "Bearer test-secret"}


@pytest.fixture
def client(monkeypatch: pytest.MonkeyPatch) -> TestClient:
    monkeypatch.setenv("SIDECAR_SECRET", "test-secret")
    monkeypatch.delenv("SIDECAR_FILE_MAX_BYTES", raising=False)
    monkeypatch.delenv("SIDECAR_FILE_ALLOWED_EXTENSIONS", raising=False)
    import importlib

    import app.main as main_module

    importlib.reload(main_module)
    return TestClient(main_module.app)


def _stub_result(text: str):
    def _fn(*, file_bytes: bytes, file_extension: str):
        return mwrap.FileExtractResult(
            text=text,
            extension=file_extension,
            bytes_in=len(file_bytes),
        )

    return _fn


def _stub_failure(code: str, message: str):
    def _fn(*, file_bytes: bytes, file_extension: str):
        raise mwrap.ExtractionError(message, code=code)

    return _fn


# ─── 200 happy paths ──────────────────────────────────────────────────────


def test_csv_returns_markdown_table(
    client: TestClient, monkeypatch: pytest.MonkeyPatch
) -> None:
    monkeypatch.setattr(
        "app.extract_file.extract_file_to_markdown",
        _stub_result("| name | role |\n| --- | --- |\n| Alex | Eng |"),
    )
    files = {"file": ("customers.csv", io.BytesIO(b"name,role\nAlex,Eng\n"), "text/csv")}
    res = client.post("/extract/file", headers=_AUTH, files=files)
    assert res.status_code == 200, res.text
    body = res.json()
    assert "| name | role |" in body["text"]
    assert body["extension"] == ".csv"
    assert body["bytes"] == len(b"name,role\nAlex,Eng\n")


def test_xlsx_returns_sheet_headings(
    client: TestClient, monkeypatch: pytest.MonkeyPatch
) -> None:
    monkeypatch.setattr(
        "app.extract_file.extract_file_to_markdown",
        _stub_result("## Sheet1\n\n| a | b |\n| --- | --- |\n\n## Sheet2\n\n| x | y |"),
    )
    files = {
        "file": (
            "report.xlsx",
            io.BytesIO(b"PK\x03\x04stub-xlsx-bytes"),
            "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet",
        )
    }
    res = client.post("/extract/file", headers=_AUTH, files=files)
    assert res.status_code == 200, res.text
    body = res.json()
    assert "## Sheet1" in body["text"]
    assert "## Sheet2" in body["text"]
    assert body["extension"] == ".xlsx"


def test_xls_legacy_format_routes_through(
    client: TestClient, monkeypatch: pytest.MonkeyPatch
) -> None:
    monkeypatch.setattr(
        "app.extract_file.extract_file_to_markdown",
        _stub_result("## Sheet1\n\n| col |\n| --- |\n| 1 |"),
    )
    files = {"file": ("legacy.xls", io.BytesIO(b"\xd0\xcf\x11\xe0fake-xls"), "application/vnd.ms-excel")}
    res = client.post("/extract/file", headers=_AUTH, files=files)
    assert res.status_code == 200, res.text
    body = res.json()
    assert body["extension"] == ".xls"


def test_explicit_file_extension_override_wins(
    client: TestClient, monkeypatch: pytest.MonkeyPatch
) -> None:
    """Caller can stream raw bytes with no filename and pass file_extension via form."""
    captured: dict[str, str] = {}

    def _capture(*, file_bytes: bytes, file_extension: str):
        captured["extension"] = file_extension
        return mwrap.FileExtractResult(text="ok", extension=file_extension, bytes_in=len(file_bytes))

    monkeypatch.setattr("app.extract_file.extract_file_to_markdown", _capture)
    files = {"file": ("blob", io.BytesIO(b"raw"), "application/octet-stream")}
    data = {"file_extension": "csv"}  # no leading dot — endpoint normalizes
    res = client.post("/extract/file", headers=_AUTH, files=files, data=data)
    assert res.status_code == 200, res.text
    assert captured["extension"] == ".csv"
    assert res.json()["extension"] == ".csv"


# ─── 401 ─────────────────────────────────────────────────────────────────


def test_rejects_missing_bearer(client: TestClient) -> None:
    files = {"file": ("x.csv", io.BytesIO(b"a,b\n1,2"), "text/csv")}
    res = client.post("/extract/file", files=files)
    assert res.status_code == 401
    assert res.json()["detail"]["error"] == "missing_bearer_token"


# ─── 415 unsupported extension ───────────────────────────────────────────


def test_rejects_when_no_extension_resolvable(client: TestClient) -> None:
    files = {"file": ("noext", io.BytesIO(b"raw"), "application/octet-stream")}
    res = client.post("/extract/file", headers=_AUTH, files=files)
    assert res.status_code == 415
    detail = res.json()["detail"]
    assert detail["error"] == "unsupported_extension"
    assert detail["extension"] == ""
    assert ".csv" in detail["allowed"]


def test_rejects_pdf_extension_not_in_default_allowlist(client: TestClient) -> None:
    files = {"file": ("doc.pdf", io.BytesIO(b"%PDF-stub"), "application/pdf")}
    res = client.post("/extract/file", headers=_AUTH, files=files)
    assert res.status_code == 415
    detail = res.json()["detail"]
    assert detail["error"] == "unsupported_extension"
    assert detail["extension"] == ".pdf"


# ─── 413 oversized body ──────────────────────────────────────────────────


def test_rejects_oversized_body(
    client: TestClient, monkeypatch: pytest.MonkeyPatch
) -> None:
    monkeypatch.setenv("SIDECAR_FILE_MAX_BYTES", "1024")
    import importlib

    import app.main as main_module

    importlib.reload(main_module)
    local_client = TestClient(main_module.app)

    big = b"a,b\n" + (b"x" * 4096)
    files = {"file": ("big.csv", io.BytesIO(big), "text/csv")}
    res = local_client.post("/extract/file", headers=_AUTH, files=files)
    assert res.status_code == 413
    detail = res.json()["detail"]
    assert detail["error"] == "spreadsheet_too_large"
    assert detail["max_bytes"] == 1024


# ─── 422 malformed input ─────────────────────────────────────────────────


def test_returns_422_when_extraction_fails(
    client: TestClient, monkeypatch: pytest.MonkeyPatch
) -> None:
    monkeypatch.setattr(
        "app.extract_file.extract_file_to_markdown",
        _stub_failure("extraction_failed", "corrupt zip"),
    )
    files = {"file": ("bad.xlsx", io.BytesIO(b"not-a-zip"), "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet")}
    res = client.post("/extract/file", headers=_AUTH, files=files)
    assert res.status_code == 422
    body = res.json()
    assert body["error"] == "extraction_failed"
    assert "corrupt zip" in body["message"]


def test_returns_500_when_dependency_missing(
    client: TestClient, monkeypatch: pytest.MonkeyPatch
) -> None:
    monkeypatch.setattr(
        "app.extract_file.extract_file_to_markdown",
        _stub_failure("dependency_missing", "markitdown not installed"),
    )
    files = {"file": ("x.csv", io.BytesIO(b"a,b\n1,2"), "text/csv")}
    res = client.post("/extract/file", headers=_AUTH, files=files)
    assert res.status_code == 500
    body = res.json()
    assert body["error"] == "dependency_missing"


# ─── env-driven allowlist extension ──────────────────────────────────────


def test_env_extends_allowlist(monkeypatch: pytest.MonkeyPatch) -> None:
    monkeypatch.setenv("SIDECAR_SECRET", "test-secret")
    monkeypatch.setenv("SIDECAR_FILE_ALLOWED_EXTENSIONS", "csv,xls,xlsx,tsv")
    import importlib

    import app.main as main_module

    importlib.reload(main_module)
    local_client = TestClient(main_module.app)

    monkeypatch.setattr(
        "app.extract_file.extract_file_to_markdown",
        _stub_result("| col |\n| --- |\n| 1 |"),
    )
    files = {"file": ("data.tsv", io.BytesIO(b"col\n1"), "text/tab-separated-values")}
    res = local_client.post("/extract/file", headers=_AUTH, files=files)
    assert res.status_code == 200
    assert res.json()["extension"] == ".tsv"


# ─── wrapper unit tests (no markitdown call) ─────────────────────────────


def test_wrapper_raises_empty_body() -> None:
    with pytest.raises(mwrap.ExtractionError) as excinfo:
        mwrap.extract_file_to_markdown(file_bytes=b"", file_extension=".csv")
    assert excinfo.value.code == "empty_body"


def test_wrapper_raises_missing_extension() -> None:
    with pytest.raises(mwrap.ExtractionError) as excinfo:
        mwrap.extract_file_to_markdown(file_bytes=b"x", file_extension="")
    assert excinfo.value.code == "missing_extension"


def test_wrapper_normalizes_extension_without_dot() -> None:
    """Caller may pass 'csv' or '.csv' — both must work."""
    # Stub MarkItDown so we don't actually call pandas
    import sys
    from types import SimpleNamespace

    class _FakeMD:
        def __init__(self, *args, **kwargs):
            pass

        def convert_stream(self, stream, file_extension: str):
            assert file_extension == ".csv"
            return SimpleNamespace(text_content="| a |\n| --- |\n| 1 |")

    fake_module = SimpleNamespace(MarkItDown=_FakeMD)
    sys.modules["markitdown"] = fake_module
    try:
        result = mwrap.extract_file_to_markdown(
            file_bytes=b"a\n1",
            file_extension="csv",  # no leading dot
        )
        assert result.extension == ".csv"
        assert "| a |" in result.text
    finally:
        del sys.modules["markitdown"]
