# Wrapper to run LibreOffice (soffice) inside the TalkBack Docker image.
# Use when the API runs on the host (e.g. Windows) but LibreOffice is not installed.
# Requires: TALKBACK_UPLOAD_ROOT set to the same value as the API (e.g. C:\Users\...\talkback).
# Requires: talkback-api image built (docker build -t talkback-api .).
# Set in .env: TALKBACK_SOFFICE_CMD=<repo>\scripts\soffice-docker.cmd

$ErrorActionPreference = "Stop"
$root = $env:TALKBACK_UPLOAD_ROOT
if (-not $root) {
    Write-Error "TALKBACK_UPLOAD_ROOT must be set when using soffice-docker (e.g. your repo root or project directory)."
    exit 1
}
$root = $root.TrimEnd('\', '/')
$mapped = $args | ForEach-Object {
    ($_ -replace [regex]::Escape($root), "/data") -replace '\\', '/'
}
& docker run --rm --entrypoint soffice -v "${root}:/data" talkback-api @mapped
exit $LASTEXITCODE
