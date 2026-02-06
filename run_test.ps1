# Setup Test Environment
$root = "testfiles"

if (!(Test-Path $root)) {
    Write-Host "Warning: $root directory not found. Docker may create it as root if volumes are mounted."
}

# Run Docker Compose
Write-Host "Starting 4-Way Sync Test..."
docker compose -f docker-compose.test.yml up --build -d

Write-Host "Waiting for containers to initialize..."
Start-Sleep -Seconds 10

# Show logs
docker compose -f docker-compose.test.yml logs sender

Write-Host "`nTest Environment Ready."
Write-Host "Check 'testfiles' directory for sync results."
