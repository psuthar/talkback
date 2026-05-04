# MarkItDown sidecar (TalkBack)

Python service that wraps Microsoft [MarkItDown](https://github.com/microsoft/markitdown) for two TalkBack content-extraction gaps surfaced in SCRUM-296/297:

1. **Image captioning + OCR** — uploaded `kind=image` materials currently have no extracted text and are invisible to RAG.
2. **URL → Markdown** — better preservation of headings, lists, tables for verified link content.

The Go backend talks to this sidecar over HTTP. PPTX/DOCX/PDF/XLSX/Whisper continue on their existing Go paths and are out of scope.

## Endpoints

| Method | Path                | Auth     | Notes |
|--------|---------------------|----------|-------|
| GET    | `/healthz`          | none     | Liveness probe; returns `{"status": "ok", "version": "..."}` |
| POST   | `/extract/_ping`    | bearer   | Skeleton stub; replaced by `/extract/image` in SCRUM-300 |

## Local dev

```sh
cd services/markitdown-sidecar
python -m venv .venv && source .venv/bin/activate
pip install -e '.[dev]'
SIDECAR_SECRET=dev-secret uvicorn app.main:app --reload
# in another shell:
curl localhost:8000/healthz
curl -X POST -H "Authorization: Bearer dev-secret" localhost:8000/extract/_ping
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

`OPENAI_API_KEY` will be required by SCRUM-300 (image captioning).

## Project layout

```
services/markitdown-sidecar/
├── app/
│   ├── auth.py                  # Bearer auth dependency
│   ├── logging_middleware.py    # JSON access logs
│   └── main.py                  # FastAPI app factory
├── tests/
│   └── test_health_and_auth.py
├── Dockerfile
├── pyproject.toml
└── README.md
```
