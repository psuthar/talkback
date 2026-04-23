# Test runner script for Windows
# Ensures database is available and runs tests

param(
    [string]$Package = "./..."
)

Write-Host "Setting up test environment..." -ForegroundColor Cyan

# Start postgres only (full `up -d` also starts api on :8080 and can conflict with a local API).
Write-Host "Ensuring Postgres container..." -ForegroundColor Cyan
docker compose -f deploy/docker-compose.yml up -d postgres
Start-Sleep -Seconds 3

# Set DATABASE_URL for tests
$env:DATABASE_URL = "postgres://talkback:talkback@localhost:5432/talkback?sslmode=disable"

Write-Host "Running tests..." -ForegroundColor Cyan
# Run tests sequentially (-p 1) to avoid database deadlocks
go test -v -p 1 $Package
