# Manual test: UI mic STT when server uses Whisper CLI (STT_MODE=cli).
# Server must be started with STT_MODE=cli and WHISPER_CLI + ffmpeg on PATH.
# Usage: .\scripts\stt_mic_cli_test.ps1 -SessionId "<session-uuid>" [-BaseUrl http://localhost:8081]

param(
    [string]$BaseUrl = "http://localhost:8081",
    [string]$SessionId = ""
)

if (-not $SessionId) {
    Write-Host "Usage: .\stt_mic_cli_test.ps1 -SessionId <session-uuid> [-BaseUrl http://localhost:8081]"
    exit 1
}

$uri = "$BaseUrl/sessions/$SessionId/questions/voice"
$AudioPath = Join-Path $env:TEMP "talkback_stt_test_1s.wav"
if (-not (Test-Path $AudioPath)) {
    $header = [byte[]]@(0x52,0x49,0x46,0x46,0x24,0x08,0x00,0x00,0x57,0x41,0x56,0x45,0x66,0x6d,0x74,0x20,0x10,0x00,0x00,0x00,0x01,0x00,0x01,0x00,0x80,0x3e,0x00,0x00,0x00,0x7d,0x00,0x00,0x02,0x00,0x10,0x00,0x64,0x61,0x74,0x61,0x00,0x08,0x00,0x00)
    $zeros = New-Object byte[] 32000
    [System.IO.File]::WriteAllBytes($AudioPath, $header + $zeros)
}

& curl.exe -s -w "`n%{http_code}" -X POST -F "file=@$AudioPath" $uri
if ($LASTEXITCODE -ne 0) { exit 1 }
