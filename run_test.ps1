# Setup Test Environment
$root = "testfiles"
$dirs = @(
    "$root\sender1",
    "$root\sender2",
    "$root\sender3",
    "$root\sender4",
    "$root\receiver1",
    "$root\receiver2",
    "$root\receiver3",
    "$root\receiver4"
)

# Clean up previous test
# Remove-Item -Path $root -Recurse -Force -ErrorAction SilentlyContinue

foreach ($d in $dirs) {
    if (!(Test-Path $d)) {
        New-Item -ItemType Directory -Force -Path $d | Out-Null
        Write-Host "Created $d"
    }
}

# Run Docker Compose
Write-Host "Starting 4-Way Sync Test..."
docker compose -f docker-compose.test.yml up --build -d

Write-Host "Waiting for containers to initialize..."
Start-Sleep -Seconds 5

# Show logs
docker compose -f docker-compose.test.yml logs sender

Write-Host "`nTest Environment Ready."
Write-Host "Put files in 'testfiles\senderX' and watch them appear in 'testfiles\receiverX'."
# Create Test Directories
$dirs = @(
    "test_data\sender\series1",
    "test_data\sender\series2",
    "test_data\sender\movies1",
    "test_data\sender\movies2",
    "test_data\receiver"
)

foreach ($d in $dirs) {
    New-Item -ItemType Directory -Force -Path $d | Out-Null
    Write-Host "Created $d"
}

# Run Docker Compose
Write-Host "Starting Multi-Sync Test..."
docker compose -f docker-compose.test.yml up --build -d

Write-Host "Containers started. Checking logs for config generation..."
Start-Sleep -Seconds 5
docker compose -f docker-compose.test.yml logs sender
