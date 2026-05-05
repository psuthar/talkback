FROM python:3.12-slim AS base

ENV PYTHONDONTWRITEBYTECODE=1 \
    PYTHONUNBUFFERED=1 \
    PIP_NO_CACHE_DIR=1

WORKDIR /app

COPY pyproject.toml ./
RUN pip install --upgrade pip \
 && pip install \
    "fastapi>=0.115,<1.0" \
    "uvicorn[standard]>=0.30,<1.0" \
    "pydantic>=2.7,<3.0" \
    "python-multipart>=0.0.9" \
    "markitdown>=0.0.1" \
    "openai>=1.40,<2.0" \
    "httpx>=0.27,<1.0"

COPY app ./app

# Drop privileges; uvicorn does not need root.
RUN useradd --create-home --shell /usr/sbin/nologin sidecar \
 && chown -R sidecar:sidecar /app
USER sidecar

EXPOSE 8000

CMD ["uvicorn", "app.main:app", "--host", "0.0.0.0", "--port", "8000"]
