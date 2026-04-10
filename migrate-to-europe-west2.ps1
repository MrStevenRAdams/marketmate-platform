# ============================================================================
# MarketMate - Full Migration: us-central1 to europe-west2
# ============================================================================
# Run from PowerShell with gcloud authenticated.
# Run each PHASE separately and verify before moving to the next.
# ============================================================================

$PROJECT     = "marketmate-486116"
$PROJECT_NUM = "487246736287"
$REGION      = "europe-west2"
$ZONE        = "europe-west2-a"
$SA_EMAIL    = "$PROJECT_NUM-compute@developer.gserviceaccount.com"
$ENC_KEY     = "default-32-char-key-change-me!!!"
$TS_API_KEY  = "marketmate-ts-key"
$VPC_CONNECTOR = "projects/$PROJECT/locations/$REGION/connectors/temu-egress-connector"

Write-Host ""
Write-Host "============================================================" -ForegroundColor Cyan
Write-Host "  MarketMate Migration: us-central1 to europe-west2" -ForegroundColor Cyan
Write-Host "============================================================" -ForegroundColor Cyan
Write-Host "PROJECT : $PROJECT" -ForegroundColor White
Write-Host "REGION  : $REGION" -ForegroundColor White
Write-Host "ZONE    : $ZONE" -ForegroundColor White
Write-Host ""

# ============================================================================
# PHASE 1 - Cloud Tasks Queues (europe-west2)
# ============================================================================

Write-Host "PHASE 1 - Creating Cloud Tasks queues in $REGION..." -ForegroundColor Cyan

$QUEUES = @(
    "marketmate-workflow-queue",
    "marketmate-order-sync",
    "import-batches",
    "enrich-products",
    "ebay-ai-enrich",
    "ai-generate"
)

foreach ($Q in $QUEUES) {
    Write-Host "  Creating queue: $Q" -ForegroundColor White
    $result = gcloud tasks queues create $Q --location=$REGION --project=$PROJECT 2>&1
    if ($result -match "already exists") {
        Write-Host "    (already exists - skipping)" -ForegroundColor DarkGray
    } else {
        Write-Host "    $result" -ForegroundColor DarkGray
    }
}

Write-Host "Phase 1 complete." -ForegroundColor Green
Write-Host ""

# ============================================================================
# PHASE 2 - Deploy Cloud Run Functions (europe-west2)
# ============================================================================
# Run from the root platform/ directory

Write-Host "PHASE 2 - Deploying Cloud Run functions to $REGION..." -ForegroundColor Cyan

Write-Host "  [2a] Deploying import-orchestrator..." -ForegroundColor White
gcloud run deploy import-orchestrator `
    --source ./functions/import-orchestrator `
    --region $REGION `
    --project $PROJECT `
    --platform managed `
    --no-allow-unauthenticated `
    --service-account $SA_EMAIL `
    --memory 512Mi `
    --timeout 540 `
    --set-env-vars "GCP_PROJECT_ID=$PROJECT,GCP_REGION=$REGION,GCP_PROJECT_NUMBER=$PROJECT_NUM,CREDENTIAL_ENCRYPTION_KEY=$ENC_KEY"

Write-Host "  [2b] Deploying import-batch..." -ForegroundColor White
gcloud run deploy import-batch `
    --source ./functions/import-batch `
    --region $REGION `
    --project $PROJECT `
    --platform managed `
    --no-allow-unauthenticated `
    --service-account $SA_EMAIL `
    --memory 512Mi `
    --timeout 540 `
    --set-env-vars "GCP_PROJECT_ID=$PROJECT,GCP_REGION=$REGION,GCP_PROJECT_NUMBER=$PROJECT_NUM"

Write-Host "  [2c] Deploying import-enrich..." -ForegroundColor White
gcloud run deploy import-enrich `
    --source ./functions/import-enrich `
    --region $REGION `
    --project $PROJECT `
    --platform managed `
    --no-allow-unauthenticated `
    --service-account $SA_EMAIL `
    --memory 512Mi `
    --timeout 540 `
    --set-env-vars "GCP_PROJECT_ID=$PROJECT,GCP_REGION=$REGION,CREDENTIAL_ENCRYPTION_KEY=$ENC_KEY"

Write-Host "  [2d] Deploying ebay-ai-enrich..." -ForegroundColor White
gcloud run deploy ebay-ai-enrich `
    --source ./functions/ebay-ai-enrich `
    --region $REGION `
    --project $PROJECT `
    --platform managed `
    --no-allow-unauthenticated `
    --service-account $SA_EMAIL `
    --memory 512Mi `
    --timeout 540 `
    --set-env-vars "GCP_PROJECT_ID=$PROJECT,GCP_REGION=$REGION"

Write-Host "Phase 2 complete." -ForegroundColor Green
Write-Host ""

# ============================================================================
# PHASE 3 - Wire function URLs together
# ============================================================================

Write-Host "PHASE 3 - Wiring function URLs..." -ForegroundColor Cyan

$ORCHESTRATOR_URL = (gcloud run services describe import-orchestrator `
    --region $REGION --project $PROJECT --format "value(status.url)")

$BATCH_URL = (gcloud run services describe import-batch `
    --region $REGION --project $PROJECT --format "value(status.url)")

$ENRICH_URL = (gcloud run services describe import-enrich `
    --region $REGION --project $PROJECT --format "value(status.url)")

Write-Host "  Orchestrator : $ORCHESTRATOR_URL" -ForegroundColor White
Write-Host "  Batch        : $BATCH_URL" -ForegroundColor White
Write-Host "  Enrich       : $ENRICH_URL" -ForegroundColor White

gcloud run services update import-orchestrator `
    --region $REGION --project $PROJECT `
    --update-env-vars "BATCH_FUNCTION_URL=$BATCH_URL"

gcloud run services update import-batch `
    --region $REGION --project $PROJECT `
    --update-env-vars "ENRICH_FUNCTION_URL=$ENRICH_URL"

Write-Host "Phase 3 complete." -ForegroundColor Green
Write-Host ""

# ============================================================================
# PHASE 4 - Typesense VM (europe-west2)
# ============================================================================

Write-Host "PHASE 4 - Deploying Typesense VM in $ZONE..." -ForegroundColor Cyan

$STARTUP = "#!/bin/bash`nmkdir -p /mnt/disks/typesense-data`ndocker run -d --name typesense --restart=always -p 8108:8108 -v /mnt/disks/typesense-data:/data typesense/typesense:27.1 --data-dir /data --api-key=$TS_API_KEY --enable-cors"

gcloud compute instances create typesense-server `
    --project=$PROJECT `
    --zone=$ZONE `
    --machine-type=e2-small `
    --image-family=cos-stable `
    --image-project=cos-cloud `
    --boot-disk-size=20GB `
    --tags=typesense-server `
    --metadata=startup-script=$STARTUP

$fwResult = gcloud compute firewall-rules create allow-typesense `
    --project=$PROJECT `
    --direction=INGRESS `
    --priority=1000 `
    --network=default `
    --action=ALLOW `
    --rules=tcp:8108 `
    --target-tags=typesense-server `
    --source-ranges=0.0.0.0/0 2>&1
if ($fwResult -match "already exists") {
    Write-Host "  Firewall rule already exists - skipping." -ForegroundColor DarkGray
}

Write-Host "  Waiting 45s for VM and Docker to start..." -ForegroundColor Yellow
Start-Sleep -Seconds 45

$TS_IP = (gcloud compute instances describe typesense-server `
    --zone=$ZONE --project=$PROJECT `
    --format="get(networkInterfaces[0].accessConfigs[0].natIP)")

Write-Host "  Typesense IP: $TS_IP" -ForegroundColor White

try {
    $health = Invoke-RestMethod `
        -Uri "http://${TS_IP}:8108/health" `
        -Headers @{"X-TYPESENSE-API-KEY" = $TS_API_KEY}
    Write-Host "  Health check: $($health | ConvertTo-Json)" -ForegroundColor Green
} catch {
    Write-Host "  Not ready yet - Docker may still be pulling the image." -ForegroundColor Yellow
    Write-Host "  Test manually: curl http://${TS_IP}:8108/health" -ForegroundColor Yellow
}

Write-Host "Phase 4 complete. Typesense URL: http://${TS_IP}:8108" -ForegroundColor Green
Write-Host ""

# ============================================================================
# PHASE 5 - Deploy marketmate-api (europe-west2)
# ============================================================================
# Run from the backend/ directory

Write-Host "PHASE 5 - Deploying marketmate-api to $REGION..." -ForegroundColor Cyan
Write-Host "  NOTE: Run this from the backend/ directory." -ForegroundColor Yellow
Write-Host ""

if (-not $TS_IP) {
    $TS_IP = (gcloud compute instances describe typesense-server `
        --zone=$ZONE --project=$PROJECT `
        --format="get(networkInterfaces[0].accessConfigs[0].natIP)")
}

if (-not $ORCHESTRATOR_URL) {
    $ORCHESTRATOR_URL = (gcloud run services describe import-orchestrator `
        --region $REGION --project $PROJECT --format "value(status.url)")
}

Write-Host "  Typesense URL    : http://${TS_IP}:8108" -ForegroundColor White
Write-Host "  Orchestrator URL : $ORCHESTRATOR_URL" -ForegroundColor White
Write-Host "  VPC Connector    : $VPC_CONNECTOR" -ForegroundColor White
Write-Host ""

gcloud run deploy marketmate-api `
    --source . `
    --region $REGION `
    --project $PROJECT `
    --platform managed `
    --no-allow-unauthenticated `
    --service-account $SA_EMAIL `
    --vpc-connector $VPC_CONNECTOR `
    --vpc-egress all-traffic `
    --set-env-vars "GCP_PROJECT_ID=$PROJECT,GCP_REGION=$REGION,GCP_PROJECT_NUMBER=$PROJECT_NUM,CREDENTIAL_ENCRYPTION_KEY=$ENC_KEY,CLOUD_TASKS_LOCATION=$REGION,ORCHESTRATOR_FUNCTION_URL=$ORCHESTRATOR_URL,TYPESENSE_URL=http://${TS_IP}:8108,TYPESENSE_API_KEY=$TS_API_KEY,TYPESENSE_GCE_ZONE=$ZONE"

$API_URL = (gcloud run services describe marketmate-api `
    --region $REGION --project $PROJECT --format "value(status.url)")

Write-Host "  New API URL: $API_URL" -ForegroundColor Green

gcloud run services update marketmate-api `
    --region $REGION --project $PROJECT `
    --update-env-vars "API_BASE_URL=$API_URL"

Write-Host "Phase 5 complete." -ForegroundColor Green
Write-Host ""

# ============================================================================
# PHASE 6 - Verification
# ============================================================================

Write-Host "PHASE 6 - Verification checks..." -ForegroundColor Cyan

if (-not $API_URL) {
    $API_URL = (gcloud run services describe marketmate-api `
        --region $REGION --project $PROJECT --format "value(status.url)")
}

Write-Host ""
Write-Host "  Services in $REGION :" -ForegroundColor White
gcloud run services list --region $REGION --project $PROJECT `
    --format "table(metadata.name,status.url,status.conditions[0].status)"

Write-Host ""
Write-Host "  Cloud Tasks queues in $REGION :" -ForegroundColor White
gcloud tasks queues list --location $REGION --project $PROJECT

Write-Host ""
Write-Host "  Typesense VM:" -ForegroundColor White
gcloud compute instances describe typesense-server `
    --zone=$ZONE --project=$PROJECT `
    --format="table(name,status,networkInterfaces[0].accessConfigs[0].natIP)"

$TEMU_UK_IP = (gcloud compute addresses describe temu-egress-uk `
    --region $REGION --project $PROJECT --format "value(address)")

Write-Host ""
Write-Host "Phase 6 complete." -ForegroundColor Green
Write-Host ""

# ============================================================================
# PHASE 7 - Cleanup old us-central1 resources
# ============================================================================
# COMMENTED OUT - uncomment only after verifying europe-west2 is fully working

Write-Host "PHASE 7 - Cleanup (SKIPPED - uncomment in script when ready)" -ForegroundColor Yellow
Write-Host ""

<#  UNCOMMENT WHEN READY TO CLEAN UP

$OLD_REGION = "us-central1"

Write-Host "Deleting us-central1 Cloud Run services..." -ForegroundColor Yellow
foreach ($SVC in @("marketmate-api","import-orchestrator","import-batch","import-enrich","ebay-ai-enrich")) {
    gcloud run services delete $SVC --region $OLD_REGION --project $PROJECT --quiet
}

Write-Host "Deleting us-central1 Cloud Tasks queues..." -ForegroundColor Yellow
foreach ($Q in @("marketmate-workflow-queue","marketmate-order-sync","import-batches","enrich-products","ebay-ai-enrich","ai-generate")) {
    gcloud tasks queues delete $Q --location $OLD_REGION --project $PROJECT --quiet
}

Write-Host "Deleting old Typesense VM..." -ForegroundColor Yellow
gcloud compute instances delete typesense-server --zone=us-central1-a --project=$PROJECT --quiet

Write-Host "Deleting unused us-central1 static IP..." -ForegroundColor Yellow
gcloud compute addresses delete temu-egress-ip --region=us-central1 --project=$PROJECT --quiet

#>

# ============================================================================
# SUMMARY
# ============================================================================

Write-Host "============================================================" -ForegroundColor Green
Write-Host "  Migration Complete" -ForegroundColor Green
Write-Host "============================================================" -ForegroundColor Green
Write-Host ""
Write-Host "  Region         : $REGION" -ForegroundColor White
Write-Host "  API URL        : $API_URL" -ForegroundColor White
Write-Host "  Typesense      : http://${TS_IP}:8108" -ForegroundColor White
Write-Host "  Temu Egress IP : $TEMU_UK_IP  (whitelist this in Temu)" -ForegroundColor White
Write-Host ""
Write-Host "  REMAINING ACTIONS:" -ForegroundColor Yellow
Write-Host "  1. Update frontend API URL to: $API_URL" -ForegroundColor White
Write-Host "  2. Whitelist $TEMU_UK_IP in Temu seller portal" -ForegroundColor White
Write-Host "  3. Trigger Typesense re-index from MarketMate UI" -ForegroundColor White
Write-Host "  4. Set PII_AES_KEY and PII_HMAC_KEY env vars" -ForegroundColor White
Write-Host "  5. When verified, uncomment Phase 7 cleanup and re-run" -ForegroundColor White
Write-Host ""
Write-Host "============================================================" -ForegroundColor Green
