$token = (gcloud auth print-access-token)

$creds = @(
    @{tenant="tenant-10013"; id="27489f43-0e2e-450a-97a5-3158d7c85317"},
    @{tenant="tenant-10013"; id="3895c0bf-85bc-4e31-bcbf-357fa7d1f661"},
    @{tenant="tenant-10014"; id="1475a332-4120-4215-874e-60d663651f26"},
    @{tenant="tenant-10014"; id="37e06e59-6206-4d4f-b109-933d42d2e648"},
    @{tenant="tenant-10014"; id="9743c233-82ec-42d5-93db-b38a01d37e29"},
    @{tenant="tenant-10015"; id="48490f89-3a21-4aec-8cd0-1e4bfab6ede6"}
)

foreach ($c in $creds) {
    $r = Invoke-WebRequest -Uri "https://firestore.googleapis.com/v1/projects/marketmate-486116/databases/(default)/documents/tenants/$($c.tenant)/marketplace_credentials/$($c.id)?mask.fieldPaths=channel&mask.fieldPaths=credential_name&mask.fieldPaths=active&mask.fieldPaths=connected&mask.fieldPaths=credential_data" -Headers @{"Authorization"="Bearer $token"} -UseBasicParsing | ConvertFrom-Json
    $f = $r.fields
    $name = $f.credential_name.stringValue
    $active = $f.active.booleanValue
    $connected = $f.connected.booleanValue
    $channel = $f.channel.stringValue
    Write-Host "--- $($c.tenant) | $($c.id)"
    Write-Host "    Name=$name | Channel=$channel | Active=$active | Connected=$connected"
    # Show credential_data keys
    $cd = $f.credential_data.mapValue.fields
    if ($cd) {
        $keys = $cd.PSObject.Properties.Name
        Write-Host "    CredData keys: $($keys -join ', ')"
    }
}
