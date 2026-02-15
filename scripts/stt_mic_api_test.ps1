# Manual test: UI mic STT using OpenAI Whisper API (STT_MODE=api or hybrid with OPENAI_API_KEY).
# Requires: API running, OPENAI_API_KEY set, a valid session ID.
# Usage: .\scripts\stt_mic_api_test.ps1 -BaseUrl "http://localhost:8081" -SessionId "<session-uuid>"
# Optional: -AudioPath path to a short WAV/WebM file.

param(
    [string]$BaseUrl = "http://localhost:8081",
    [string]$SessionId = "",
    [string]$AudioPath = ""
)

if (-not $SessionId) {
    Write-Host "Usage: .\stt_mic_api_test.ps1 -SessionId <session-uuid> [-BaseUrl http://localhost:8081] [-AudioPath path/to/audio.wav]"
    exit 1
}

$uri = "$BaseUrl/sessions/$SessionId/questions/voice"
if (-not $AudioPath) {
    $AudioPath = Join-Path $env:TEMP "talkback_stt_test_1s.wav"
    if (-not (Test-Path $AudioPath)) {
        $header = [byte[]]@(0x52,0x49,0x46,0x46,0x24,0x08,0x00,0x00,0x57,0x41,0x56,0x45,0x66,0x6d,0x74,0x20,0x10,0x00,0x00,0x00,0x01,0x00,0x01,0x00,0x80,0x3e,0x00,0x00,0x00,0x7d,0x00,0x00,0x02,0x00,0x10,0x00,0x64,0x61,0x74,0x61,0x00,0x08,0x00,0x00)
        $zeros = New-Object byte[] 32000
        [System.IO.File]::WriteAllBytes($AudioPath, $header + $zeros)
    }
}

# Use curl for reliable multipart upload
$curlArgs = @("-s", "-w", "`n%{http_code}", "-X", "POST", "-F", "file=@$AudioPath", $uri)
& curl.exe @curlArgs
if ($LASTEXITCODE -ne 0) { exit 1 }
