# Frontend build stage
FROM node:20-alpine AS webbuilder
WORKDIR /app/web
COPY web/package.json web/package-lock.json ./
RUN npm ci || npm ci
COPY web ./
RUN npm run build

# Go build stage
FROM golang:alpine AS builder
RUN apk add --no-cache git
WORKDIR /app
# Copy module files first for better layer caching
COPY go.mod go.sum ./
RUN go mod download
COPY . .
# Copy built React app into cmd/api/ so `//go:embed dist` can embed it.
RUN rm -rf cmd/api/dist && mkdir -p cmd/api/dist
COPY --from=webbuilder /app/web/dist ./cmd/api/dist
# Build API and reset-db (for preDeployCommand)
RUN CGO_ENABLED=0 go build -o /go/bin/app -v ./cmd/api
RUN CGO_ENABLED=0 go build -o /go/bin/reset-db -v ./cmd/reset-db

# Final stage: runtime image
# Use debian-slim so we can install LibreOffice (soffice) for PPTX -> slides/image/text processing
# Pinned to bookworm-slim (not floating debian:stable-slim) to avoid repo/tag desync on Render.
FROM debian:bookworm-slim

# Retry once on transient mirror failures (exit 100 = package fetch error).
RUN apt-get update && \
    DEBIAN_FRONTEND=noninteractive apt-get install -y --no-install-recommends \
      ca-certificates \
      poppler-utils \
      libreoffice-nogui \
      unoconv && \
    rm -rf /var/lib/apt/lists/* || \
    (sleep 5 && apt-get update && \
    DEBIAN_FRONTEND=noninteractive apt-get install -y --no-install-recommends \
      ca-certificates \
      poppler-utils \
      libreoffice-nogui \
      unoconv && \
    rm -rf /var/lib/apt/lists/*)

# Ensure soffice is on PATH and verify headless conversion works (build fails if missing)
ENV PATH="/usr/bin:${PATH}"
RUN which soffice && soffice --version

# Pre-bake the LibreOffice user profile into the image layer.
# Running a dummy conversion forces LO to create ~/.config/libreoffice/ and load font metadata
# at build time — not at first-request time — so the cold-start overhead is paid once during
# the Docker build rather than on every fresh deploy.
RUN echo "<html><body>warmup</body></html>" > /tmp/lo-warmup.html && \
    soffice --headless --convert-to pdf --outdir /tmp /tmp/lo-warmup.html 2>/dev/null || true && \
    rm -f /tmp/lo-warmup.html /tmp/lo-warmup.pdf

COPY --from=builder /go/bin/app /app
COPY --from=builder /go/bin/reset-db /reset-db

WORKDIR /
ENTRYPOINT ["/app"]
LABEL Name=talkback Version=0.0.1
EXPOSE 8080
