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

#final stage
FROM alpine:latest
RUN apk --no-cache add ca-certificates poppler-utils
COPY --from=builder /go/bin/app /app
COPY --from=builder /go/bin/reset-db /reset-db
ENTRYPOINT ["/app"]
LABEL Name=talkback Version=0.0.1
EXPOSE 8080
