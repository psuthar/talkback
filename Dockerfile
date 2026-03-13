# Build stage
FROM golang:alpine AS builder
RUN apk add --no-cache git
WORKDIR /app
# Copy module files first for better layer caching
COPY go.mod go.sum ./
RUN go mod download
COPY . .
# Build API and reset-db (for preDeployCommand)
RUN CGO_ENABLED=0 go build -o /go/bin/app -v ./cmd/api
RUN CGO_ENABLED=0 go build -o /go/bin/reset-db -v ./cmd/reset-db

# Final stage: runtime image
# Use debian-slim so we can install LibreOffice (soffice) for PPTX -> slides/image/text processing
FROM debian:stable-slim

RUN apt-get update && \
    DEBIAN_FRONTEND=noninteractive apt-get install -y --no-install-recommends \
      ca-certificates \
      poppler-utils \
      libreoffice-nogui && \
    rm -rf /var/lib/apt/lists/*

# Ensure soffice is on PATH and verify headless conversion works (build fails if missing)
ENV PATH="/usr/bin:${PATH}"
RUN which soffice && soffice --version

COPY --from=builder /go/bin/app /app
COPY --from=builder /go/bin/reset-db /reset-db

WORKDIR /
ENTRYPOINT ["/app"]
LABEL Name=talkback Version=0.0.1
EXPOSE 8080
