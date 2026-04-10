# ============================================================================
# Deploy Typesense on GCP Compute Engine
# Run from PowerShell with gcloud authenticated
# ============================================================================

$PROJECT = "marketmate-486116"
$ZONE = "us-central1-a"
$INSTANCE = "typesense-server"
$TS_API_KEY = "marketmate-ts-key"

# 1. Create a small VM with Docker
Write-Host "Creating Compute Engine instance..." -ForegroundColor Cyan
gcloud compute instances create $INSTANCE `
  --project=$PROJECT `
  --zone=$ZONE `
  --machine-type=e2-small `
  --image-family=cos-stable `
  --image-project=cos-cloud `
  --boot-disk-size=20GB `
  --tags=typesense-server `
  --metadata=startup-script="#!/bin/bash
mkdir -p /mnt/disks/typesense-data
docker run -d --name typesense --restart=always \
  -p 8108:8108 \
  -v /mnt/disks/typesense-data:/data \
  typesense/typesense:27.1 \
  --data-dir /data \
  --api-key=$TS_API_KEY \
  --enable-cors"

# 2. Create firewall rule to allow Cloud Run (and your IP) to reach port 8108
Write-Host "Creating firewall rule..." -ForegroundColor Cyan
gcloud compute firewall-rules create allow-typesense `
  --project=$PROJECT `
  --direction=INGRESS `
  --priority=1000 `
  --network=default `
  --action=ALLOW `
  --rules=tcp:8108 `
  --target-tags=typesense-server `
  --source-ranges=0.0.0.0/0

# 3. Get the external IP
Write-Host "`nWaiting for instance to start..." -ForegroundColor Yellow
Start-Sleep -Seconds 15

$EXTERNAL_IP = (gcloud compute instances describe $INSTANCE --zone=$ZONE --project=$PROJECT --format="get(networkInterfaces[0].accessConfigs[0].natIP)")
Write-Host "`n============================================" -ForegroundColor Green
Write-Host "Typesense deployed!" -ForegroundColor Green
Write-Host "External IP: $EXTERNAL_IP" -ForegroundColor Green
Write-Host "URL: http://${EXTERNAL_IP}:8108" -ForegroundColor Green
Write-Host "API Key: $TS_API_KEY" -ForegroundColor Green
Write-Host "============================================" -ForegroundColor Green

# 4. Test health (may take a minute for Docker to pull the image)
Write-Host "`nWaiting for Typesense to start (pulling Docker image)..." -ForegroundColor Yellow
Start-Sleep -Seconds 30

try {
    $health = Invoke-RestMethod -Uri "http://${EXTERNAL_IP}:8108/health" -Headers @{"X-TYPESENSE-API-KEY"=$TS_API_KEY}
    Write-Host "Health check: $($health | ConvertTo-Json)" -ForegroundColor Green
} catch {
    Write-Host "Typesense not ready yet. Wait a minute and try:" -ForegroundColor Yellow
    Write-Host "  curl http://${EXTERNAL_IP}:8108/health -H 'X-TYPESENSE-API-KEY: $TS_API_KEY'" -ForegroundColor Yellow
}

# 5. Instructions for Cloud Run
Write-Host "`n============================================" -ForegroundColor Cyan
Write-Host "NEXT STEPS:" -ForegroundColor Cyan
Write-Host "1. Update Cloud Run env vars:" -ForegroundColor White
Write-Host "   TYPESENSE_URL=http://${EXTERNAL_IP}:8108" -ForegroundColor White
Write-Host "   TYPESENSE_API_KEY=$TS_API_KEY" -ForegroundColor White
Write-Host "`n2. Redeploy backend or update env vars:" -ForegroundColor White
Write-Host "   gcloud run services update marketmate-api --region=us-central1 --update-env-vars=TYPESENSE_URL=http://${EXTERNAL_IP}:8108,TYPESENSE_API_KEY=$TS_API_KEY" -ForegroundColor White
Write-Host "`n3. Trigger initial sync:" -ForegroundColor White
Write-Host "   Invoke-RestMethod -Method POST -Uri 'https://marketmate-api-lceeosuhoa-uc.a.run.app/api/v1/search/sync' -Headers @{'X-Tenant-Id'='tenant-demo'} -ContentType 'application/json' -Body '{}'" -ForegroundColor White
Write-Host "============================================" -ForegroundColor Cyan
