# Kill process using a specific port
param(
    [int]$Port = 8080
)

Write-Host "Finding process using port $Port..." -ForegroundColor Cyan

$connections = netstat -ano | Select-String ":$Port.*LISTENING"
if ($connections) {
    $pid = ($connections[0] -split '\s+')[-1]
    Write-Host "Found process ID: $pid" -ForegroundColor Yellow
    
    $process = Get-Process -Id $pid -ErrorAction SilentlyContinue
    if ($process) {
        Write-Host "Process: $($process.ProcessName) (PID: $pid)" -ForegroundColor Yellow
        $confirm = Read-Host "Kill this process? (y/N)"
        if ($confirm -eq 'y' -or $confirm -eq 'Y') {
            Stop-Process -Id $pid -Force
            Write-Host "Process killed." -ForegroundColor Green
        } else {
            Write-Host "Cancelled." -ForegroundColor Yellow
        }
    } else {
        Write-Host "Process not found (may have already terminated)" -ForegroundColor Yellow
    }
} else {
    Write-Host "No process found using port $Port" -ForegroundColor Green
}
