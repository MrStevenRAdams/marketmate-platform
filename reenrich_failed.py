#!/usr/bin/env python3
"""
reenrich_failed.py — Re-queue enrichment for products that were never enriched.

Identifies unenriched products by querying Firestore for products that have
no description AND no brand AND have an ASIN — these were imported but the
enrichment task either timed out or was dropped.

Uses the Cloud Tasks REST API directly (not gcloud CLI) so that DispatchDeadline
can be set to 1500s, preventing the 540s timeout that caused the original failure.

Usage:
    python reenrich_failed.py --job-id JOB_ID --tenant-id TENANT_ID [--dry-run] [--limit N]

Examples:
    # Dry run — see how many products need re-enriching
    python reenrich_failed.py --job-id 4f3a8e09-da0b-4241-9481-d68e238a0d26 --tenant-id tenant-10013 --dry-run

    # Re-enrich all failed products
    python reenrich_failed.py --job-id 4f3a8e09-da0b-4241-9481-d68e238a0d26 --tenant-id tenant-10013

    # Re-enrich with a limit (for testing)
    python reenrich_failed.py --job-id 4f3a8e09-da0b-4241-9481-d68e238a0d26 --tenant-id tenant-10013 --limit 100
"""

import argparse
import base64
import json
import os
import subprocess
import sys
import time
import urllib.request
import urllib.error
from datetime import datetime

# ── CONFIG ────────────────────────────────────────────────────────────────────
PROJECT    = "marketmate-486116"
REGION     = "europe-west2"
QUEUE      = "enrich-products"
ENRICH_URL = os.environ.get(
    "ENRICH_URL",
    "https://import-enrich-lceeosuhoa-nw.a.run.app"
)
BATCH_SIZE = 10       # products per Cloud Task (same as original)
RATE_DELAY = 0.05     # seconds between task creation calls
SA_EMAIL   = "487246736287-compute@developer.gserviceaccount.com"

# ── ARGS ──────────────────────────────────────────────────────────────────────
parser = argparse.ArgumentParser(description="Re-queue enrichment for failed products")
parser.add_argument("--job-id",    required=True,       help="Import job ID")
parser.add_argument("--tenant-id", required=True,       help="Tenant ID e.g. tenant-10013")
parser.add_argument("--dry-run",   action="store_true", help="Count only, don't queue tasks")
parser.add_argument("--limit",     type=int, default=0, help="Max products to re-enrich (0=all)")
parser.add_argument("--cred-id",   default="",          help="Override credential ID")
parser.add_argument("--run-id",    default="",          help="Unique run ID to avoid task name collisions (auto-generated if not set)")
args = parser.parse_args()

# Auto-generate run-id from timestamp if not provided — ensures task names are
# unique across retries so Cloud Tasks doesn't silently deduplicate them.
RUN_ID = args.run_id or datetime.now().strftime("%H%M%S")


# ── HELPERS ───────────────────────────────────────────────────────────────────

def gcloud(cmd, timeout=120):
    r = subprocess.run(cmd, shell=True, capture_output=True, text=True, timeout=timeout)
    return r.returncode, r.stdout, r.stderr


def get_token():
    rc, token, err = gcloud("gcloud auth print-access-token")
    if rc != 0:
        print(f"  ERROR: could not get access token: {err[:100]}")
        sys.exit(1)
    return token.strip()


def firestore_get(path, token):
    url = (f"https://firestore.googleapis.com/v1/projects/{PROJECT}"
           f"/databases/(default)/documents/{path}")
    req = urllib.request.Request(url)
    req.add_header("Authorization", f"Bearer {token}")
    try:
        with urllib.request.urlopen(req, timeout=30) as r:
            return json.loads(r.read().decode())
    except urllib.error.HTTPError as e:
        print(f"  Firestore GET error {e.code}: {e.read().decode()[:200]}")
        return None


def extract_field(fields, key):
    """Extract a scalar value from a Firestore fields map."""
    v = fields.get(key, {})
    for t in ("stringValue", "integerValue", "booleanValue", "doubleValue"):
        if t in v:
            return v[t]
    return ""


def create_enrich_task(job_id, tenant_id, cred_id, items, task_suffix, token):
    """Create a Cloud Task via REST API with 1500s dispatch deadline."""
    queue_path = f"projects/{PROJECT}/locations/{REGION}/queues/{QUEUE}"
    task_name  = f"{queue_path}/tasks/reenrich-{job_id[:8]}-{RUN_ID}-{task_suffix}"

    payload = {
        "job_id":        job_id,
        "tenant_id":     tenant_id,
        "credential_id": cred_id,
        "items":         items,
    }
    body_b64 = base64.b64encode(json.dumps(payload).encode()).decode()

    api_body = {
        "task": {
            "name": task_name,
            "dispatchDeadline": "1500s",
            "httpRequest": {
                "httpMethod": "POST",
                "url": ENRICH_URL,
                "headers": {"Content-Type": "application/json"},
                "body": body_b64,
                "oidcToken": {"serviceAccountEmail": SA_EMAIL},
            },
        }
    }

    url = f"https://cloudtasks.googleapis.com/v2/{queue_path}/tasks"
    req = urllib.request.Request(
        url,
        data=json.dumps(api_body).encode(),
        method="POST"
    )
    req.add_header("Authorization", f"Bearer {token}")
    req.add_header("Content-Type", "application/json")

    try:
        with urllib.request.urlopen(req, timeout=30) as r:
            r.read()
            return True
    except urllib.error.HTTPError as e:
        err = e.read().decode()
        if e.code == 409 or "ALREADY_EXISTS" in err:
            return True   # idempotent
        print(f"  Task create error: HTTP {e.code} — {err[:120]}")
        return False
    except Exception as ex:
        print(f"  Task create error: {ex}")
        return False


# ── MAIN ──────────────────────────────────────────────────────────────────────

print(f"\n{'='*60}")
print(f"  Re-Enrich Failed Products")
print(f"  Job:    {args.job_id}")
print(f"  Tenant: {args.tenant_id}")
print(f"  Mode:   {'DRY RUN' if args.dry_run else 'LIVE'}")
print(f"{'='*60}\n")

token = get_token()

# ── Step 1: Load import job ───────────────────────────────────────────────────
print("Step 1: Loading import job...")
job_doc = firestore_get(
    f"tenants/{args.tenant_id}/import_jobs/{args.job_id}",
    token
)
if not job_doc:
    print(f"  ERROR: Job {args.job_id} not found.")
    sys.exit(1)

job_fields    = job_doc.get("fields", {})
cred_id       = args.cred_id or extract_field(job_fields, "channel_account_id")
job_status    = extract_field(job_fields, "status")
enriched      = int(extract_field(job_fields, "enriched_items")     or 0)
enrich_failed = int(extract_field(job_fields, "enrich_failed_items") or 0)
enrich_total  = int(extract_field(job_fields, "enrich_total_items")  or 0)

print(f"  Status:         {job_status}")
print(f"  Enriched:       {enriched:,}")
print(f"  Enrich failed:  {enrich_failed:,}")
print(f"  Enrich total:   {enrich_total:,}")
print(f"  Credential:     {cred_id}")

if not cred_id:
    print("\n  ERROR: Could not determine credential ID. Use --cred-id to specify.")
    sys.exit(1)

# ── Step 2: Find unenriched products ─────────────────────────────────────────
print(f"\nStep 2: Scanning for unenriched products (this may take a minute)...")
print(f"  Strategy: products with no enriched_at timestamp AND have an ASIN")

unenriched    = []
next_page     = None
total_scanned = 0
page_num      = 0
token_age     = 0

while True:
    page_num  += 1
    token_age += 1

    if token_age >= 50:
        token     = get_token()
        token_age = 0

    url = (
        f"https://firestore.googleapis.com/v1/projects/{PROJECT}"
        f"/databases/(default)/documents/tenants/{args.tenant_id}/products"
        f"?pageSize=300"
        f"&mask.fieldPaths=enriched_at"
        f"&mask.fieldPaths=identifiers"
    )
    if next_page:
        url += f"&pageToken={next_page}"

    req = urllib.request.Request(url)
    req.add_header("Authorization", f"Bearer {token}")
    try:
        with urllib.request.urlopen(req, timeout=30) as r:
            data = json.loads(r.read().decode())
    except Exception as e:
        print(f"  Page {page_num} error: {e}")
        break

    docs           = data.get("documents", [])
    total_scanned += len(docs)

    for doc in docs:
        f           = doc.get("fields", {})
        enriched_at = f.get("enriched_at", {}).get("timestampValue", "").strip()
        identifiers = f.get("identifiers", {}).get("mapValue", {}).get("fields", {})
        asin        = identifiers.get("asin", {}).get("stringValue", "").strip()
        pid         = doc.get("name", "").split("/")[-1]

        # Unenriched = never had enriched_at set AND has an ASIN to look up
        if not enriched_at and asin:
            unenriched.append({"product_id": pid, "asin": asin})

    if page_num % 10 == 0:
        print(f"  Scanned {total_scanned:,} products, "
              f"found {len(unenriched):,} unenriched so far...")

    next_page = data.get("nextPageToken")
    if not next_page:
        break

    if args.limit and len(unenriched) >= args.limit:
        unenriched = unenriched[:args.limit]
        break

print(f"\n  Total scanned:    {total_scanned:,}")
print(f"  Unenriched found: {len(unenriched):,}")

if not unenriched:
    print("\n  Nothing to re-enrich. All products have been attempted (enriched_at is set).")
    sys.exit(0)

total_tasks = (len(unenriched) + BATCH_SIZE - 1) // BATCH_SIZE

if args.dry_run:
    print(f"\n  DRY RUN — would create {total_tasks:,} Cloud Tasks")
    print(f"  Sample products:")
    for p in unenriched[:5]:
        print(f"    {p['product_id']} — ASIN: {p['asin']}")
    print(f"\n  Run without --dry-run to queue the tasks.")
    sys.exit(0)

# ── Step 3: Queue enrichment tasks ───────────────────────────────────────────
print(f"\nStep 3: Creating {total_tasks:,} Cloud Tasks ({BATCH_SIZE} products each)...")
print(f"  Dispatch deadline: 1500s (25 min)")
print(f"  Queue: {QUEUE}")
print()

ok         = 0
fail       = 0
task_index = 0
token_age  = 0

for i in range(0, len(unenriched), BATCH_SIZE):
    batch  = unenriched[i : i + BATCH_SIZE]
    suffix = f"{task_index:06d}"

    token_age += 1
    if token_age >= 200:
        token     = get_token()
        token_age = 0

    success = create_enrich_task(
        args.job_id, args.tenant_id, cred_id, batch, suffix, token
    )

    if success:
        ok += 1
    else:
        fail += 1

    task_index += 1

    if task_index % 50 == 0:
        pct = 100 * task_index // total_tasks
        print(f"  {task_index:,}/{total_tasks:,} ({pct}%) — {ok} ok, {fail} failed")

    time.sleep(RATE_DELAY)

print(f"\n{'='*60}")
print(f"  COMPLETE")
print(f"  Tasks created:   {ok:,}")
print(f"  Tasks failed:    {fail:,}")
print(f"  Products queued: ~{ok * BATCH_SIZE:,}")
print(f"\n  Monitor progress in the MarketMate UI or check Firestore:")
print(f"  tenants/{args.tenant_id}/import_jobs/{args.job_id}")
print(f"{'='*60}\n")

# ── Step 4: Reset job status ─────────────────────────────────────────────────
if ok > 0:
    print("Step 4: Resetting job status to 'running'...")
    token = get_token()

    patch_body = json.dumps({
        "fields": {
            "status":         {"stringValue": "running"},
            "status_message": {"stringValue": f"Re-enrichment queued: ~{ok * BATCH_SIZE} products"},
            "updated_at":     {"timestampValue": datetime.utcnow().strftime("%Y-%m-%dT%H:%M:%SZ")},
        }
    })

    url = (
        f"https://firestore.googleapis.com/v1/projects/{PROJECT}"
        f"/databases/(default)/documents"
        f"/tenants/{args.tenant_id}/import_jobs/{args.job_id}"
        f"?updateMask.fieldPaths=status"
        f"&updateMask.fieldPaths=status_message"
        f"&updateMask.fieldPaths=updated_at"
    )
    req = urllib.request.Request(url, data=patch_body.encode(), method="PATCH")
    req.add_header("Authorization", f"Bearer {token}")
    req.add_header("Content-Type", "application/json")
    try:
        with urllib.request.urlopen(req, timeout=30) as r:
            r.read()
            print("  Job status reset to 'running' ✅")
    except Exception as e:
        print(f"  Could not reset job status (non-critical): {e}")
