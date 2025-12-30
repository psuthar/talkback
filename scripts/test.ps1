# Test runner script for Windows
# Ensures database is available and runs tests

param(
    [string]$Package = "./..."
)

Write-Host "Setting up test environment..." -ForegroundColor Cyan

# Check if Postgres is running
$postgresRunning = docker compose -f deploy/docker-compose.yml ps --format json | ConvertFrom-Json | Where-Object { $_.State -eq "running" }
if (-not $postgresRunning) {
    Write-Host "Starting Postgres container..." -ForegroundColor Yellow
    docker compose -f deploy/docker-compose.yml up -d
    Start-Sleep -Seconds 5
}

# Set DATABASE_URL for tests
$env:DATABASE_URL = "postgres://talkback:talkback@localhost:5432/talkback?sslmode=disable"

Write-Host "Running tests..." -ForegroundColor Cyan
# Run tests sequentially (-p 1) to avoid database deadlocks
go test -v -p 1 $Package
