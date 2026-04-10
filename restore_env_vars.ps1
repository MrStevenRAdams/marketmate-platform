# restore_env_vars.ps1
# Restores all marketmate-api env vars to known-good state.
# Run this after any gcloud run services update that may have wiped env vars.
# Usage: .\restore_env_vars.ps1

$PROJECT  = "marketmate-486116"
$REGION   = "europe-west2"
$SERVICE  = "marketmate-api"

Write-Host "Restoring marketmate-api env vars..." -ForegroundColor Cyan

gcloud run services update $SERVICE `
  --project=$PROJECT `
  --region=$REGION `
  --update-env-vars=`
"EGRESS_PROXY_URL=https://marketmate-proxy-eu-487246736287.europe-west2.run.app,`
EGRESS_PROXY_SECRET=mm-proxy-secret-2024,`
TYPESENSE_URL=http://35.246.78.209:8108,`
TYPESENSE_GCE_ZONE=europe-west2-a,`
SHOPIFY_CLIENT_ID=46f184684603f21c6df23cbc890e0e57,`
SHOPIFY_REDIRECT_URI=https://marketmate-api-487246736287.europe-west2.run.app/api/v1/shopify/oauth/callback,`
AMAZON_LWA_CLIENT_ID=amzn1.application-oa2-client.73b96779af624d94b5eb139c923a2114,`
AMAZON_APP_ID=amzn1.sp.solution.f52f5c79-c56e-4d6d-9e9b-02f312be257a,`
AMAZON_REDIRECT_URI=https://marketmate-api-487246736287.europe-west2.run.app/api/v1/amazonnew/oauth/callback,`
FIREBASE_PROJECT_ID=marketmate-486116,`
ORCHESTRATOR_FUNCTION_URL=https://import-orchestrator-487246736287.europe-west2.run.app,`
AMZ_LWA_CLIENT_ID=amzn1.application-oa2-client.76eb5e03b5b24321a2fde0f9aa631730,`
SMTP_HOST=smtp-relay.brevo.com,`
SMTP_PORT=587,`
SMTP_FROM=noreply@e-lister.co.uk,`
SMTP_FROM_NAME=MarketMate,`
FRONTEND_URL=https://marketmate-486116.web.app" `
  --update-secrets=`
"CREDENTIAL_ENCRYPTION_KEY=marketmate-credential-encryption-key:latest,`
SHOPIFY_CLIENT_SECRET=marketmate-shopify-client-secret:latest,`
CLAUDE_API_KEY=marketmate-claude-api-key:latest,`
GEMINI_API_KEY=marketmate-gemini-api-key:latest,`
AMZ_LWA_CLIENT_SECRET=marketmate-amazon-lwa-secret:latest,`
AMAZON_LWA_CLIENT_SECRET=marketmate-amazon-lwa-secret:latest,`
EBAY_PROD_CLIENT_SECRET=marketmate-ebay-client-secret:latest,`
PAYPAL_CLIENT_SECRET=marketmate-paypal-client-secret:latest,`
TYPESENSE_API_KEY=marketmate-typesense-api-key:latest,`
TWILIO_ACCOUNT_SID=marketmate-twilio-account-sid:latest,`
TWILIO_AUTH_TOKEN=marketmate-twilio-auth-token:latest,`
TWILIO_FROM=marketmate-twilio-from:latest,`
FIREBASE_WEB_API_KEY=FIREBASE_WEB_API_KEY:latest,`
SMTP_PASSWORD=marketmate-smtp-password:latest,`
SMTP_USER=marketmate-smtp-user:latest,`
SMTP_USERNAME=marketmate-smtp-user:latest"

if ($LASTEXITCODE -eq 0) {
    Write-Host "Deployed. Waiting 15s for revision to warm up..." -ForegroundColor Green
    Start-Sleep -Seconds 15
    Write-Host "Checking Typesense health..." -ForegroundColor Cyan
    try {
        $r = Invoke-WebRequest -Uri "https://marketmate-api-487246736287.europe-west2.run.app/api/v1/search/health" `
             -Headers @{"X-Tenant-Id"="tenant-10013"} -UseBasicParsing -ErrorAction Stop
        Write-Host "Typesense: UP ($($r.StatusCode))" -ForegroundColor Green
    } catch {
        Write-Host "Typesense: still unavailable - check logs" -ForegroundColor Red
        Write-Host $_.Exception.Message
    }
} else {
    Write-Host "Deployment failed" -ForegroundColor Red
}
