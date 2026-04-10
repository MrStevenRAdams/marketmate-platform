#!/usr/bin/env pwsh
# ============================================================================
# CANCEL STUCK AMAZON JOB
# ============================================================================
# Cancels the job showing as "running" in Job Monitor
# ============================================================================

$API_BASE = "https://marketmate-api-487246736287.us-central1.run.app/api/v1"
$TENANT_ID = "tenant-devils-in-the-detail-baa338"  # Your actual tenant from the screenshot
$JOB_ID = "3f4c2b53-9b85-8e41-a5e2-b559fc16206e"  # The stuck job from Job Monitor

Write-Host "🛑 Cancelling stuck Amazon schema job..." -ForegroundColor Yellow
Write-Host "   Job ID: $JOB_ID" -ForegroundColor Gray
Write-Host ""

try {
    $response = Invoke-RestMethod -Uri "$API_BASE/amazon/schemas/jobs/$JOB_ID/cancel" `
        -Method POST `
        -Headers @{"X-Tenant-Id"=$TENANT_ID} `
        -ErrorAction Stop
    
    Write-Host "✅ Job cancelled successfully!" -ForegroundColor Green
    Write-Host ""
    Write-Host "Now you can start a fresh sync from the Schema Cache Manager." -ForegroundColor Cyan
} catch {
    Write-Host "❌ Failed to cancel job: $($_.Exception.Message)" -ForegroundColor Red
    Write-Host ""
    Write-Host "Trying manual Firestore update..." -ForegroundColor Yellow
    Write-Host ""
    Write-Host "Go to Firestore Console:" -ForegroundColor White
    Write-Host "https://console.cloud.google.com/firestore/databases/-default-/data/panel/marketplaces/Amazon/schema_jobs/$JOB_ID?project=marketmate-486116" -ForegroundColor Cyan
    Write-Host ""
    Write-Host "And update these fields:" -ForegroundColor White
    Write-Host "  status: 'cancelled'" -ForegroundColor Gray
    Write-Host "  completedAt: [current timestamp]" -ForegroundColor Gray
}
