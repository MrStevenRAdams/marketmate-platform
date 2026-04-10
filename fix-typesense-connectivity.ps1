# ============================================================================
# Fix Cloud Run → Typesense connectivity
# Uses the VM's internal IP via the existing VPC connector
# ============================================================================

$PROJECT = "marketmate-486116"

# 1. Get Typesense VM internal IP
Write-Host "Getting Typesense VM internal IP..." -ForegroundColor Cyan
$INTERNAL_IP = (gcloud compute instances describe typesense-server --zone=us-central1-a --project=$PROJECT --format="get(networkInterfaces[0].networkIP)")
Write-Host "Internal IP: $INTERNAL_IP" -ForegroundColor Green

# 2. Check current VPC connector on Cloud Run
Write-Host "`nChecking VPC connector..." -ForegroundColor Cyan
$VPC_CONNECTOR = (gcloud run services describe marketmate-api --region=us-central1 --project=$PROJECT --format="value(spec.template.metadata.annotations.'run.googleapis.com/vpc-access-connector')" 2>$null)
Write-Host "VPC Connector: $VPC_CONNECTOR" -ForegroundColor Green

# 3. Also check VPC egress setting
$VPC_EGRESS = (gcloud run services describe marketmate-api --region=us-central1 --project=$PROJECT --format="value(spec.template.metadata.annotations.'run.googleapis.com/vpc-access-egress')" 2>$null)
Write-Host "VPC Egress: $VPC_EGRESS" -ForegroundColor Green

# 4. Update Cloud Run to use internal IP
Write-Host "`nUpdating Cloud Run env vars to use internal IP..." -ForegroundColor Cyan
gcloud run services update marketmate-api `
  --region=us-central1 `
  --project=$PROJECT `
  --update-env-vars="TYPESENSE_URL=http://${INTERNAL_IP}:8108,TYPESENSE_API_KEY=marketmate-ts-key"

# 5. Also ensure VPC egress routes private traffic through connector
Write-Host "`nEnsuring VPC egress is set to private-ranges-only..." -ForegroundColor Cyan
gcloud run services update marketmate-api `
  --region=us-central1 `
  --project=$PROJECT `
  --vpc-egress=private-ranges-only

# 6. Create firewall rule for internal traffic if not exists
Write-Host "`nEnsuring internal firewall rule exists..." -ForegroundColor Cyan
gcloud compute firewall-rules describe allow-typesense-internal --project=$PROJECT 2>$null
if ($LASTEXITCODE -ne 0) {
    gcloud compute firewall-rules create allow-typesense-internal `
      --project=$PROJECT `
      --direction=INGRESS `
      --priority=1000 `
      --network=default `
      --action=ALLOW `
      --rules=tcp:8108 `
      --target-tags=typesense-server `
      --source-ranges=10.0.0.0/8
    Write-Host "Created internal firewall rule" -ForegroundColor Green
} else {
    Write-Host "Internal firewall rule already exists" -ForegroundColor Green
}

# 7. Wait and test
Write-Host "`nWaiting for deployment..." -ForegroundColor Yellow
Start-Sleep -Seconds 10

Write-Host "`nTesting search health endpoint..." -ForegroundColor Cyan
try {
    $result = Invoke-RestMethod -Uri "https://marketmate-api-487246736287.us-central1.run.app/api/v1/search/health" -Headers @{"X-Tenant-Id"="tenant-demo"}
    Write-Host "Search health: $($result | ConvertTo-Json)" -ForegroundColor Green
} catch {
    Write-Host "Still not reachable. Error: $_" -ForegroundColor Red
    Write-Host "`nTry checking VPC connector egress setting:" -ForegroundColor Yellow
    Write-Host "  gcloud run services update marketmate-api --region=us-central1 --vpc-egress=all-traffic" -ForegroundColor Yellow
}
