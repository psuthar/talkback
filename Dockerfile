# Build stage
FROM golang:alpine AS builder
RUN apk add --no-cache git
WORKDIR /app
# Copy module files first for better layer caching
COPY go.mod go.sum ./
RUN go mod download
COPY . .
# Build only the API binary (./... with -o is invalid; use the main package path)
RUN CGO_ENABLED=0 go build -o /go/bin/app -v ./cmd/api

#final stage
FROM alpine:latest
RUN apk --no-cache add ca-certificates poppler-utils
COPY --from=builder /go/bin/app /app
ENTRYPOINT ["/app"]
LABEL Name=talkback Version=0.0.1
EXPOSE 8080
