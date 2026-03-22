# ============================================================================
# MarketMate - Post-Migration Fix Script
# ============================================================================
# Run this from the platform/ root directory.
# This fixes three things:
#   1. Discovers the actual new europe-west2 Cloud Run URLs
#   2. Fixes the frontend so the app loads again
#   3. Updates backend/.env for local development
# ============================================================================

$PROJECT = "marketmate-486116"
$REGION  = "europe-west2"
$ZONE    = "europe-west2-a"

Write-Host ""
Write-Host "============================================================" -ForegroundColor Cyan
Write-Host "  MarketMate - Post-Migration App Fix" -ForegroundColor Cyan
Write-Host "============================================================" -ForegroundColor Cyan
Write-Host ""

# ============================================================================
# STEP 1 - Discover actual deployed URLs
# ============================================================================

Write-Host "STEP 1 - Fetching current europe-west2 service URLs..." -ForegroundColor Cyan

$API_URL = (gcloud run services describe marketmate-api `
    --region $REGION --project $PROJECT --format "value(status.url)" 2>&1)

$ORCH_URL = (gcloud run services describe import-orchestrator `
    --region $REGION --project $PROJECT --format "value(status.url)" 2>&1)

$BATCH_URL = (gcloud run services describe import-batch `
    --region $REGION --project $PROJECT --format "value(status.url)" 2>&1)

$ENRICH_URL = (gcloud run services describe import-enrich `
    --region $REGION --project $PROJECT --format "value(status.url)" 2>&1)

$TS_IP = (gcloud compute instances describe typesense-server `
    --zone=$ZONE --project=$PROJECT `
    --format="get(networkInterfaces[0].networkIP)" 2>&1)

Write-Host ""
Write-Host "  marketmate-api      : $API_URL" -ForegroundColor White
Write-Host "  import-orchestrator : $ORCH_URL" -ForegroundColor White
Write-Host "  import-batch        : $BATCH_URL" -ForegroundColor White
Write-Host "  import-enrich       : $ENRICH_URL" -ForegroundColor White
Write-Host "  typesense (internal): $TS_IP" -ForegroundColor White
Write-Host ""

if (-not $API_URL -or $API_URL -like "*ERROR*") {
    Write-Host "ERROR: Could not fetch marketmate-api URL." -ForegroundColor Red
    Write-Host "Make sure you have run: gcloud auth login and gcloud config set project marketmate-486116" -ForegroundColor Yellow
    exit 1
}

Write-Host "Step 1 complete." -ForegroundColor Green
Write-Host ""

# ============================================================================
# STEP 2 - Fix frontend .env.production
# ============================================================================

Write-Host "STEP 2 - Writing frontend/.env.production..." -ForegroundColor Cyan

# Prompt for tenant ID
$TENANT_ID = Read-Host "  Enter your tenant ID (e.g. tenant-10007)"
if (-not $TENANT_ID) { $TENANT_ID = "tenant-10007" }

$ENV_CONTENT = @"
VITE_API_URL=$API_URL/api/v1
VITE_TENANT_ID=$TENANT_ID
"@

Set-Content -Path ".\frontend\.env.production" -Value $ENV_CONTENT
Write-Host "  Written: frontend/.env.production" -ForegroundColor White
Write-Host "  VITE_API_URL  = $API_URL/api/v1" -ForegroundColor DarkGray
Write-Host "  VITE_TENANT_ID = $TENANT_ID" -ForegroundColor DarkGray

Write-Host "Step 2 complete." -ForegroundColor Green
Write-Host ""

# ============================================================================
# STEP 3 - Build the frontend
# ============================================================================

Write-Host "STEP 3 - Building frontend..." -ForegroundColor Cyan
Set-Location ".\frontend"

npm run build
if ($LASTEXITCODE -ne 0) {
    Write-Host "ERROR: Frontend build failed. Check the output above." -ForegroundColor Red
    Set-Location ".."
    exit 1
}

Set-Location ".."
Write-Host "Step 3 complete." -ForegroundColor Green
Write-Host ""

# ============================================================================
# STEP 4 - Deploy frontend to Firebase Hosting
# ============================================================================

Write-Host "STEP 4 - Deploying frontend to Firebase Hosting (marketmate-486116)..." -ForegroundColor Cyan

npx firebase-tools deploy --only hosting --project marketmate-486116

if ($LASTEXITCODE -ne 0) {
    Write-Host "ERROR: Firebase deploy failed." -ForegroundColor Red
    exit 1
}

Write-Host "Step 4 complete." -ForegroundColor Green
Write-Host ""

# ============================================================================
# STEP 5 - Update backend/.env for local development
# ============================================================================

Write-Host "STEP 5 - Updating backend/.env..." -ForegroundColor Cyan

$ENV_FILE = ".\backend\.env"
$ENV_CONTENT_RAW = Get-Content $ENV_FILE -Raw

# Replace ORCHESTRATOR_FUNCTION_URL
$ENV_CONTENT_RAW = $ENV_CONTENT_RAW -replace `
    "ORCHESTRATOR_FUNCTION_URL=.*", `
    "ORCHESTRATOR_FUNCTION_URL=$ORCH_URL"

# Replace CLOUD_TASKS_QUEUE_EBAY_ENRICH
$ENV_CONTENT_RAW = $ENV_CONTENT_RAW -replace `
    "CLOUD_TASKS_QUEUE_EBAY_ENRICH=.*", `
    "CLOUD_TASKS_QUEUE_EBAY_ENRICH=projects/$PROJECT/locations/$REGION/queues/ebay-ai-enrich"

# Replace TYPESENSE_URL if internal IP was found
if ($TS_IP -and -not ($TS_IP -like "*ERROR*")) {
    $ENV_CONTENT_RAW = $ENV_CONTENT_RAW -replace `
        "TYPESENSE_URL=.*", `
        "TYPESENSE_URL=http://${TS_IP}:8108"
}

Set-Content -Path $ENV_FILE -Value $ENV_CONTENT_RAW
Write-Host "  Updated backend/.env:" -ForegroundColor White
Write-Host "    ORCHESTRATOR_FUNCTION_URL = $ORCH_URL" -ForegroundColor DarkGray
Write-Host "    CLOUD_TASKS_QUEUE_EBAY_ENRICH = projects/$PROJECT/locations/$REGION/queues/ebay-ai-enrich" -ForegroundColor DarkGray
if ($TS_IP -and -not ($TS_IP -like "*ERROR*")) {
    Write-Host "    TYPESENSE_URL = http://${TS_IP}:8108" -ForegroundColor DarkGray
}

Write-Host "Step 5 complete." -ForegroundColor Green
Write-Host ""

# ============================================================================
# STEP 6 - Update import functions' env vars with correct URLs
# ============================================================================

Write-Host "STEP 6 - Syncing function env vars..." -ForegroundColor Cyan

# Make sure orchestrator knows the batch URL
gcloud run services update import-orchestrator `
    --region $REGION --project $PROJECT `
    --update-env-vars "BATCH_FUNCTION_URL=$BATCH_URL"

# Make sure batch knows the enrich URL
gcloud run services update import-batch `
    --region $REGION --project $PROJECT `
    --update-env-vars "ENRICH_FUNCTION_URL=$ENRICH_URL"

# Make sure main API knows orchestrator URL
gcloud run services update marketmate-api `
    --region $REGION --project $PROJECT `
    --update-env-vars "ORCHESTRATOR_FUNCTION_URL=$ORCH_URL"

Write-Host "Step 6 complete." -ForegroundColor Green
Write-Host ""

# ============================================================================
# SUMMARY
# ============================================================================

Write-Host "============================================================" -ForegroundColor Green
Write-Host "  All fixes applied successfully!" -ForegroundColor Green
Write-Host "============================================================" -ForegroundColor Green
Write-Host ""
Write-Host "  App URL     : https://marketmate-486116.web.app" -ForegroundColor White
Write-Host "  API URL     : $API_URL" -ForegroundColor White
Write-Host "  Typesense   : http://${TS_IP}:8108" -ForegroundColor White
Write-Host ""
Write-Host "  REMAINING ACTIONS:" -ForegroundColor Yellow
Write-Host "  1. Open https://marketmate-486116.web.app and verify the app loads" -ForegroundColor White
Write-Host "  2. Trigger a Typesense re-index from the MarketMate UI (search index is empty)" -ForegroundColor White
Write-Host "  3. Whitelist 34.142.20.65 in the Temu seller portal if not already done" -ForegroundColor White
Write-Host "  4. Change CREDENTIAL_ENCRYPTION_KEY to a secure value before going live" -ForegroundColor White
Write-Host "  5. Set PII_AES_KEY and PII_HMAC_KEY on marketmate-api" -ForegroundColor White
Write-Host "  6. When satisfied, run Phase 7 of migrate-to-europe-west2.ps1 to clean up us-central1" -ForegroundColor White
Write-Host ""
Write-Host "============================================================" -ForegroundColor Green
