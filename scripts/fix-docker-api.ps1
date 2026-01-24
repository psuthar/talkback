# Fix Docker API Version Mismatch
# This script restarts Docker Desktop to sync API versions

Write-Host "Attempting to restart Docker Desktop to fix API version mismatch..." -ForegroundColor Yellow

# Try to restart Docker Desktop gracefully
$dockerDesktopProcesses = Get-Process | Where-Object { $_.ProcessName -like "*Docker*" -or $_.ProcessName -like "*com.docker*" }

if ($dockerDesktopProcesses) {
    Write-Host "Found Docker Desktop processes. Attempting restart..." -ForegroundColor Yellow
    
    # Try to stop Docker Desktop
    try {
        Stop-Process -Name "Docker Desktop" -Force -ErrorAction SilentlyContinue
        Start-Sleep -Seconds 3
    } catch {
        Write-Host "Could not stop Docker Desktop automatically. Please restart it manually." -ForegroundColor Red
        Write-Host "1. Right-click Docker Desktop icon in system tray" -ForegroundColor Yellow
        Write-Host "2. Select 'Quit Docker Desktop'" -ForegroundColor Yellow
        Write-Host "3. Start Docker Desktop again" -ForegroundColor Yellow
        exit 1
    }
    
    # Wait a bit
    Start-Sleep -Seconds 5
    
    # Start Docker Desktop (if it's installed in the default location)
    $dockerDesktopPath = "${env:ProgramFiles}\Docker\Docker\Docker Desktop.exe"
    if (Test-Path $dockerDesktopPath) {
        Start-Process $dockerDesktopPath
        Write-Host "Started Docker Desktop. Waiting for it to be ready..." -ForegroundColor Green
        Start-Sleep -Seconds 15
        
        # Verify Docker is working
        $attempts = 0
        $maxAttempts = 10
        while ($attempts -lt $maxAttempts) {
            try {
                docker ps 2>&1 | Out-Null
                if ($LASTEXITCODE -eq 0) {
                    Write-Host "✅ Docker is now working!" -ForegroundColor Green
                    exit 0
                }
            } catch {
                # Continue waiting
            }
            Start-Sleep -Seconds 3
            $attempts++
        }
        
        Write-Host "Docker Desktop started, but API may still be syncing. Please wait a moment and try again." -ForegroundColor Yellow
    } else {
        Write-Host "Could not find Docker Desktop. Please restart it manually." -ForegroundColor Red
        exit 1
    }
} else {
    Write-Host "Docker Desktop does not appear to be running. Starting it..." -ForegroundColor Yellow
    $dockerDesktopPath = "${env:ProgramFiles}\Docker\Docker\Docker Desktop.exe"
    if (Test-Path $dockerDesktopPath) {
        Start-Process $dockerDesktopPath
        Write-Host "Started Docker Desktop. Please wait for it to initialize." -ForegroundColor Green
    } else {
        Write-Host "Could not find Docker Desktop. Please start it manually." -ForegroundColor Red
        exit 1
    }
}