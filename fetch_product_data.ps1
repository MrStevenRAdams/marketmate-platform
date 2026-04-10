# ============================================================================
# Fetch Product, Listings, and Extended Data from Firestore
# Run from PowerShell on your local machine
# ============================================================================

$PROJECT = "marketmate-486116"
$TENANT = "tenant-demo"
$PRODUCT_ID = "7d02b010-6147-4481-8a69-21b328bfbe1a"

# Get your Cloud Run URL
$API_URL = (gcloud run services describe marketmate-api --region=us-central1 --format="value(status.url)" 2>$null)
if (-not $API_URL) { $API_URL = "http://localhost:8080" }
$API = "$API_URL/api/v1"

Write-Host "Using API: $API" -ForegroundColor Yellow

# 1. Product
Write-Host "`n=== PRODUCT ===" -ForegroundColor Cyan
$product = Invoke-RestMethod -Uri "$API/products/$PRODUCT_ID" -Headers @{"X-Tenant-Id"=$TENANT}
$product | ConvertTo-Json -Depth 20 | Out-File "product.json"
Write-Host "Saved to product.json"
$product | ConvertTo-Json -Depth 20

# 2. All listings (filter client-side for this product)
Write-Host "`n=== LISTINGS FOR PRODUCT ===" -ForegroundColor Cyan
$allListings = Invoke-RestMethod -Uri "$API/marketplace/listings?limit=200" -Headers @{"X-Tenant-Id"=$TENANT}
$productListings = $allListings.data | Where-Object { $_.product_id -eq $PRODUCT_ID }
$productListings | ConvertTo-Json -Depth 20 | Out-File "listings.json"
Write-Host "Found $($productListings.Count) listing(s). Saved to listings.json"
$productListings | ConvertTo-Json -Depth 20

# 3. Extended Data (subcollection - query Firestore REST API directly)
Write-Host "`n=== EXTENDED DATA ===" -ForegroundColor Cyan
$token = gcloud auth print-access-token
$firestoreUrl = "https://firestore.googleapis.com/v1/projects/$PROJECT/databases/(default)/documents/tenants/$TENANT/products/$PRODUCT_ID/extended_data"
try {
    $extResponse = Invoke-RestMethod -Uri $firestoreUrl -Headers @{"Authorization"="Bearer $token"}
    $extResponse | ConvertTo-Json -Depth 20 | Out-File "extended_data.json"
    Write-Host "Saved to extended_data.json"
    $extResponse | ConvertTo-Json -Depth 20
} catch {
    Write-Host "Error fetching extended data: $_" -ForegroundColor Red
}

Write-Host "`n=== DONE ===" -ForegroundColor Green
Write-Host "Files saved: product.json, listings.json, extended_data.json"
