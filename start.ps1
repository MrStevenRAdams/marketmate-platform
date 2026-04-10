# Module A PIM - Windows Startup Script
# Run this script in PowerShell to start the development environment

Write-Host "🚀 Starting Module A (PIM) Development Environment..." -ForegroundColor Cyan
Write-Host ""

# Check if Docker is running
Write-Host "Checking Docker..." -ForegroundColor Yellow
$dockerRunning = docker ps 2>&1
if ($LASTEXITCODE -ne 0) {
    Write-Host "❌ Docker is not running!" -ForegroundColor Red
    Write-Host "Please start Docker Desktop and try again." -ForegroundColor Red
    exit 1
}
Write-Host "✅ Docker is running" -ForegroundColor Green
Write-Host ""

# Check if docker-compose.yml exists
if (-not (Test-Path "docker-compose.yml")) {
    Write-Host "❌ docker-compose.yml not found!" -ForegroundColor Red
    Write-Host "Please run this script from the module-a-pim directory." -ForegroundColor Red
    exit 1
}

# Stop any existing containers
Write-Host "Stopping any existing containers..." -ForegroundColor Yellow
docker-compose down 2>&1 | Out-Null
Write-Host ""

# Create environment files if they don't exist
Write-Host "Setting up environment files..." -ForegroundColor Yellow
if (-not (Test-Path "backend\.env")) {
    Copy-Item "backend\.env.example" "backend\.env"
    Write-Host "✅ Created backend\.env" -ForegroundColor Green
}
if (-not (Test-Path "frontend\.env")) {
    Copy-Item "frontend\.env.example" "frontend\.env"
    Write-Host "✅ Created frontend\.env" -ForegroundColor Green
}
Write-Host ""

# Start services
Write-Host "Starting services with Docker Compose..." -ForegroundColor Yellow
Write-Host ""
docker-compose up -d

if ($LASTEXITCODE -eq 0) {
    Write-Host ""
    Write-Host "✅ All services started successfully!" -ForegroundColor Green
    Write-Host ""
    Write-Host "📊 Service Status:" -ForegroundColor Cyan
    docker-compose ps
    Write-Host ""
    Write-Host "🌐 Access Points:" -ForegroundColor Cyan
    Write-Host "  Frontend:    http://localhost:5173" -ForegroundColor White
    Write-Host "  Backend API: http://localhost:8080" -ForegroundColor White
    Write-Host "  Health Check: http://localhost:8080/health" -ForegroundColor White
    Write-Host "  API Status:   http://localhost:8080/api/v1/status" -ForegroundColor White
    Write-Host ""
    Write-Host "📝 Useful Commands:" -ForegroundColor Cyan
    Write-Host "  View logs:    docker-compose logs -f" -ForegroundColor White
    Write-Host "  Stop services: docker-compose down" -ForegroundColor White
    Write-Host "  Restart:      docker-compose restart" -ForegroundColor White
    Write-Host ""
    Write-Host "🎉 Happy coding!" -ForegroundColor Green
} else {
    Write-Host ""
    Write-Host "❌ Failed to start services" -ForegroundColor Red
    Write-Host "Check the logs with: docker-compose logs" -ForegroundColor Yellow
    exit 1
}
