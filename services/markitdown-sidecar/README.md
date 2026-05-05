# MarkItDown sidecar (TalkBack)

Python service that wraps Microsoft [MarkItDown](https://github.com/microsoft/markitdown) for two TalkBack content-extraction gaps surfaced in SCRUM-296/297:

1. **Image captioning + OCR** — uploaded `kind=image` materials currently have no extracted text and are invisible to RAG.
2. **URL → Markdown** — better preservation of headings, lists, tables for verified link content.

The Go backend talks to this sidecar over HTTP. PPTX/DOCX/PDF/XLSX/Whisper continue on their existing Go paths and are out of scope.

## Endpoints

| Method | Path                | Auth     | Notes |
|--------|---------------------|----------|-------|
| GET    | `/healthz`          | none     | Liveness probe; returns `{"status": "ok", "version": "..."}` |
| POST   | `/extract/image`    | bearer   | multipart `file` upload → `{"text", "model", "tokens_used"}`. Caps body at `SIDECAR_IMAGE_MAX_BYTES` (default 10 MB). 415 on non-image content type, 413 on oversize, 500 with stable error code on LLM/MarkItDown failure. |
| POST   | `/extract/url`      | bearer   | JSON `{"url": "https://...", "max_bytes"?, "timeout_s"?}` → `{"text", "title", "fetched_url", "status_code"}`. 4xx pass-through when upstream returns 4xx, 413 on body cap exceeded, 500 with stable error code (`empty_url`, `timeout`, `fetch_failed`, `extraction_failed`, `dependency_missing`). |

## Local dev

```sh
cd services/markitdown-sidecar
python -m venv .venv && source .venv/bin/activate
pip install -e '.[dev]'
SIDECAR_SECRET=dev-secret OPENAI_API_KEY=sk-... uvicorn app.main:app --reload
# in another shell:
curl localhost:8000/healthz
curl -X POST -H "Authorization: Bearer dev-secret" \
  -F "file=@/path/to/image.png" \
  localhost:8000/extract/image
```

## Tests

```sh
cd services/markitdown-sidecar
pytest
```

Tests run without network or LLM access; the bearer secret is set per-test via `monkeypatch`.

## Container

```sh
docker build -t markitdown-sidecar:dev services/markitdown-sidecar
docker run --rm -e SIDECAR_SECRET=dev-secret -p 8000:8000 markitdown-sidecar:dev
```

The image runs as the non-root `sidecar` user on Python 3.12-slim.

## Environment variables

| Var                    | Required | Default | Purpose |
|------------------------|----------|---------|---------|
| `SIDECAR_SECRET`       | yes      | —       | Shared bearer secret. Backend sets the same value as `MARKITDOWN_SIDECAR_SECRET`. |
| `SIDECAR_VERSION`      | no       | `dev`   | Version string returned by `/healthz`. |
| `SIDECAR_LOG_LEVEL`    | no       | `INFO`  | Stdlib logging level. |
| `OPENAI_API_KEY`       | yes (for /extract/image) | — | OpenAI key for vision-based image captioning. |
| `SIDECAR_OPENAI_MODEL` | no       | `gpt-4o-mini` | Override the vision model used for image captioning. |
| `SIDECAR_IMAGE_MAX_BYTES` | no    | `10485760` | Cap on image upload size before 413. |
| `SIDECAR_URL_MAX_BYTES` | no      | `2097152` | Cap on upstream HTML body size before 413. Mirrors the existing Go urlextract package. |
| `SIDECAR_URL_TIMEOUT_S` | no      | `15` | Per-request timeout (seconds) for upstream URL fetches. |

## Project layout

```
services/markitdown-sidecar/
├── app/
│   ├── auth.py                  # Bearer auth dependency
│   ├── extract_image.py         # POST /extract/image (SCRUM-300)
│   ├── extract_url.py           # POST /extract/url   (SCRUM-301)
│   ├── logging_middleware.py    # JSON access logs
│   ├── main.py                  # FastAPI app factory
│   └── markitdown_wrapper.py    # MarkItDown SDK wrapper (testable seam)
├── tests/
│   ├── test_extract_image.py
│   ├── test_extract_url.py
│   └── test_health_and_auth.py
├── Dockerfile
├── pyproject.toml
└── README.md
```
