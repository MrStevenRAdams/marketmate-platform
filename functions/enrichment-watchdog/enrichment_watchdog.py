#!/usr/bin/env python3
"""
enrichment_watchdog.py — MarketMate Enrichment Watchdog
========================================================
Runs as a Cloud Run Job, triggered every 10 minutes by Cloud Scheduler.

For every tenant, scans import jobs that are in 'running' status and:
  1. Detects stalled enrichment (unenriched products + no recent queue activity)
  2. Re-queues failed products automatically using the Cloud Tasks REST API
  3. Sends an SMS/WhatsApp notification via Twilio when a stall is detected and fixed

Runs headlessly — no manual intervention required.

Environment variables (set on the Cloud Run Job):
  GCP_PROJECT_ID       : marketmate-486116
  GCP_REGION           : europe-west2
  ENRICH_FUNCTION_URL  : https://import-enrich-lceeosuhoa-nw.a.run.app
  TWILIO_ACCOUNT_SID   : (from Secret Manager: marketmate-twilio-account-sid)
  TWILIO_AUTH_TOKEN    : (from Secret Manager: marketmate-twilio-auth-token)
  TWILIO_FROM          : +447700000000 (your Twilio number or whatsapp:+14155238886)
  ALERT_TO             : +447700000000 (your mobile number)
  STALL_THRESHOLD_MIN  : 15 (minutes without progress before declaring a stall)

Deploy:
  cd platform/functions/enrichment-watchdog
  gcloud run jobs create enrichment-watchdog \\
    --source . \\
    --region europe-west2 \\
    --project marketmate-486116 \\
    --service-account 487246736287-compute@developer.gserviceaccount.com \\
    --set-env-vars GCP_PROJECT_ID=marketmate-486116,GCP_REGION=europe-west2 \\
    --set-env-vars ENRICH_FUNCTION_URL=https://import-enrich-lceeosuhoa-nw.a.run.app \\
    --set-secrets TWILIO_ACCOUNT_SID=marketmate-twilio-account-sid:latest \\
    --set-secrets TWILIO_AUTH_TOKEN=marketmate-twilio-auth-token:latest \\
    --set-secrets TWILIO_FROM=marketmate-twilio-from:latest \\
    --set-secrets ALERT_TO=marketmate-alert-phone:latest \\
    --memory 512Mi --timeout 300s

Schedule (every 10 minutes):
  gcloud scheduler jobs create http enrichment-watchdog-schedule \\
    --schedule="*/10 * * * *" \\
    --uri="https://europe-west2-run.googleapis.com/apis/run.googleapis.com/v1/namespaces/marketmate-486116/jobs/enrichment-watchdog:run" \\
    --message-body='{}' \\
    --oauth-service-account-email=487246736287-compute@developer.gserviceaccount.com \\
    --location=europe-west2 \\
    --project=marketmate-486116
"""

import base64
import json
import logging
import os
import sys
import time
import urllib.error
import urllib.request
from datetime import datetime, timezone, timedelta

# ── CONFIG ────────────────────────────────────────────────────────────────────
PROJECT    = os.environ.get("GCP_PROJECT_ID", "marketmate-486116")
REGION     = os.environ.get("GCP_REGION",     "europe-west2")
QUEUE      = "enrich-products"
ENRICH_URL = os.environ.get("ENRICH_FUNCTION_URL",
                             "https://import-enrich-lceeosuhoa-nw.a.run.app")
SA_EMAIL   = "487246736287-compute@developer.gserviceaccount.com"
BATCH_SIZE = 10

# Alert config
TWILIO_SID   = os.environ.get("TWILIO_ACCOUNT_SID", "")
TWILIO_TOKEN = os.environ.get("TWILIO_AUTH_TOKEN",  "")
TWILIO_FROM  = os.environ.get("TWILIO_FROM",        "")   # e.g. whatsapp:+14155238886
ALERT_TO     = os.environ.get("ALERT_TO",           "")   # e.g. whatsapp:+447700000000

# How many minutes with no enrichment progress before we declare a stall
STALL_THRESHOLD_MIN = int(os.environ.get("STALL_THRESHOLD_MIN", "15"))

logging.basicConfig(level=logging.INFO,
                    format="%(asctime)s [Watchdog] %(levelname)s %(message)s")
log = logging.getLogger("watchdog")


# ── HTTP HELPERS ──────────────────────────────────────────────────────────────

_cached_token = None
_token_expiry = None

def get_token():
    """Get a GCP access token, using metadata server when running on GCP."""
    global _cached_token, _token_expiry
    now = datetime.now(timezone.utc)
    if _cached_token and _token_expiry and now < _token_expiry:
        return _cached_token

    # Try metadata server first (Cloud Run environment)
    try:
        req = urllib.request.Request(
            "http://metadata.google.internal/computeMetadata/v1/instance/service-accounts/default/token",
            headers={"Metadata-Flavor": "Google"}
        )
        with urllib.request.urlopen(req, timeout=5) as r:
            data = json.loads(r.read().decode())
            _cached_token = data["access_token"]
            _token_expiry = now + timedelta(seconds=data.get("expires_in", 3600) - 60)
            return _cached_token
    except Exception:
        pass

    # Fall back to gcloud CLI (local dev)
    import subprocess
    r = subprocess.run("gcloud auth print-access-token",
                       shell=True, capture_output=True, text=True)
    if r.returncode == 0:
        _cached_token = r.stdout.strip()
        _token_expiry = now + timedelta(minutes=50)
        return _cached_token

    log.error("Could not obtain GCP access token")
    sys.exit(1)


def http_get(url):
    token = get_token()
    req   = urllib.request.Request(url)
    req.add_header("Authorization", f"Bearer {token}")
    try:
        with urllib.request.urlopen(req, timeout=30) as r:
            return json.loads(r.read().decode())
    except urllib.error.HTTPError as e:
        log.warning(f"GET {url} → HTTP {e.code}")
        return None
    except Exception as ex:
        log.warning(f"GET {url} → {ex}")
        return None


def http_post(url, body):
    token = get_token()
    req   = urllib.request.Request(url, data=json.dumps(body).encode(), method="POST")
    req.add_header("Authorization", f"Bearer {token}")
    req.add_header("Content-Type", "application/json")
    try:
        with urllib.request.urlopen(req, timeout=30) as r:
            return json.loads(r.read().decode()), None
    except urllib.error.HTTPError as e:
        err = e.read().decode()
        return None, f"HTTP {e.code}: {err[:200]}"
    except Exception as ex:
        return None, str(ex)


def http_patch(url, body):
    token = get_token()
    req   = urllib.request.Request(url, data=json.dumps(body).encode(), method="PATCH")
    req.add_header("Authorization", f"Bearer {token}")
    req.add_header("Content-Type", "application/json")
    try:
        with urllib.request.urlopen(req, timeout=30) as r:
            r.read()
            return True
    except Exception:
        return False


# ── FIRESTORE HELPERS ─────────────────────────────────────────────────────────

FS_BASE = f"https://firestore.googleapis.com/v1/projects/{PROJECT}/databases/(default)/documents"


def fs_get(path):
    return http_get(f"{FS_BASE}/{path}")


def fs_list(path, page_size=100, page_token=None):
    url = f"{FS_BASE}/{path}?pageSize={page_size}"
    if page_token:
        url += f"&pageToken={page_token}"
    return http_get(url)


def fs_patch(path, fields, update_mask_fields):
    mask = "".join(f"&updateMask.fieldPaths={f}" for f in update_mask_fields).lstrip("&")
    url  = f"{FS_BASE}/{path}?{mask}"
    return http_patch(url, {"fields": fields})


def fv(doc, key, default=None):
    """Extract a scalar value from a Firestore document field."""
    f = doc.get("fields", {}).get(key, {})
    for t in ("stringValue", "booleanValue", "doubleValue"):
        if t in f:
            return f[t]
    if "integerValue" in f:
        return int(f["integerValue"])
    if "timestampValue" in f:
        return f["timestampValue"]
    return default


# ── NOTIFICATION ──────────────────────────────────────────────────────────────

def send_alert(message):
    """Send SMS or WhatsApp via Twilio. Logs if credentials not configured."""
    log.info(f"ALERT: {message}")

    if not all([TWILIO_SID, TWILIO_TOKEN, TWILIO_FROM, ALERT_TO]):
        log.warning("Twilio credentials not configured — alert not sent. "
                    "Set TWILIO_ACCOUNT_SID, TWILIO_AUTH_TOKEN, TWILIO_FROM, ALERT_TO.")
        return False

    url  = f"https://api.twilio.com/2010-04-01/Accounts/{TWILIO_SID}/Messages.json"
    data = urllib.parse.urlencode({
        "From": TWILIO_FROM,
        "To":   ALERT_TO,
        "Body": f"MarketMate Watchdog: {message}"
    }).encode()

    import urllib.parse
    creds = base64.b64encode(f"{TWILIO_SID}:{TWILIO_TOKEN}".encode()).decode()
    req   = urllib.request.Request(url, data=data, method="POST")
    req.add_header("Authorization", f"Basic {creds}")
    req.add_header("Content-Type", "application/x-www-form-urlencoded")

    try:
        with urllib.request.urlopen(req, timeout=15) as r:
            resp = json.loads(r.read().decode())
            log.info(f"Alert sent: SID {resp.get('sid')}")
            return True
    except Exception as e:
        log.error(f"Failed to send alert: {e}")
        return False


# ── CLOUD TASKS HELPERS ───────────────────────────────────────────────────────

CT_BASE = f"https://cloudtasks.googleapis.com/v2/projects/{PROJECT}/locations/{REGION}/queues/{QUEUE}"


def queue_task(job_id, tenant_id, cred_id, items, task_suffix):
    """Create a single Cloud Task with 1500s dispatch deadline."""
    task_name = f"{CT_BASE}/tasks/watchdog-{job_id[:8]}-{task_suffix}"
    body_b64  = base64.b64encode(json.dumps({
        "job_id":        job_id,
        "tenant_id":     tenant_id,
        "credential_id": cred_id,
        "items":         items,
    }).encode()).decode()

    task = {
        "task": {
            "name":             task_name,
            "dispatchDeadline": "1500s",
            "httpRequest": {
                "httpMethod": "POST",
                "url":        ENRICH_URL,
                "headers":    {"Content-Type": "application/json"},
                "body":       body_b64,
                "oidcToken":  {"serviceAccountEmail": SA_EMAIL},
            },
        }
    }
    result, err = http_post(f"{CT_BASE}/tasks", task)
    if err:
        if "ALREADY_EXISTS" in err or "409" in err:
            return True
        log.warning(f"Task create failed: {err}")
        return False
    return True


def get_queue_task_count():
    """Approximate queue depth by listing up to 1000 tasks."""
    result = http_get(f"{CT_BASE}/tasks?pageSize=1000&responseView=BASIC")
    if not result:
        return 0
    return len(result.get("tasks", []))


# ── CREDENTIAL POOL ───────────────────────────────────────────────────────────

def get_pooled_credentials(marketplace_id):
    """
    Get all active Amazon/amazonnew credentials across all tenants
    that match the given marketplace_id — same logic as batch.go.
    """
    cred_ids = []
    seen     = set()

    # List all tenants
    result = fs_list("tenants", page_size=50)
    if not result:
        return cred_ids

    tenant_docs = result.get("documents", [])
    for tdoc in tenant_docs:
        tid = tdoc.get("name", "").split("/")[-1]

        for channel in ("amazon", "amazonnew"):
            # Query credentials for this tenant+channel
            query_url = f"{FS_BASE}/tenants/{tid}/marketplace_credentials"
            page      = None
            while True:
                url = f"{query_url}?pageSize=50"
                if page:
                    url += f"&pageToken={page}"
                resp = http_get(url)
                if not resp:
                    break
                for cdoc in resp.get("documents", []):
                    cid = cdoc.get("name", "").split("/")[-1]
                    if cid in seen:
                        continue
                    fields = cdoc.get("fields", {})
                    if fv(cdoc, "channel") != channel:
                        continue
                    if not fv(cdoc, "active"):
                        continue
                    # Check marketplace_id in credential_data or top-level
                    cd = fields.get("credential_data", {}).get("mapValue", {}).get("fields", {})
                    cred_mkt = (cd.get("marketplace_id", {}).get("stringValue", "") or
                                fv(cdoc, "marketplace_id") or "")
                    if marketplace_id and cred_mkt != marketplace_id:
                        continue
                    seen.add(cid)
                    cred_ids.append(cid)
                page = resp.get("nextPageToken")
                if not page:
                    break

    log.info(f"Found {len(cred_ids)} pooled credentials for marketplace {marketplace_id}")
    return cred_ids


# ── UNENRICHED PRODUCT SCAN ───────────────────────────────────────────────────

def find_unenriched_products(tenant_id, limit=0):
    """
    Page through all products for a tenant and return those with
    no brand AND no description AND have an ASIN.
    """
    unenriched = []
    next_page  = None

    while True:
        url = (f"{FS_BASE}/tenants/{tenant_id}/products"
               f"?pageSize=300"
               f"&mask.fieldPaths=enriched_at"
               f"&mask.fieldPaths=identifiers")
        if next_page:
            url += f"&pageToken={next_page}"

        resp = http_get(url)
        if not resp:
            break

        for doc in resp.get("documents", []):
            f           = doc.get("fields", {})
            enriched_at = f.get("enriched_at", {}).get("timestampValue", "").strip()
            identifiers = f.get("identifiers", {}).get("mapValue", {}).get("fields", {})
            asin        = identifiers.get("asin", {}).get("stringValue", "").strip()
            pid         = doc.get("name", "").split("/")[-1]

            # Unenriched = no enriched_at timestamp AND has an ASIN
            if not enriched_at and asin:
                unenriched.append({"product_id": pid, "asin": asin})

        next_page = resp.get("nextPageToken")
        if not next_page:
            break
        if limit and len(unenriched) >= limit:
            unenriched = unenriched[:limit]
            break

    return unenriched


# ── JOB STALENESS CHECK ───────────────────────────────────────────────────────

def is_stalled(job_doc):
    """
    A job is stalled if ALL of:
    - status is 'running'
    - enriched_items < enrich_total_items (not yet complete)
    - enrich-products queue is empty (no tasks running or pending)
    - updated_at is more than STALL_THRESHOLD_MIN minutes ago
      (guards against false positives right after a re-queue)
    """
    status = fv(job_doc, "status", "")
    if status != "running":
        return False

    enriched     = fv(job_doc, "enriched_items",    0) or 0
    enrich_total = fv(job_doc, "enrich_total_items", 0) or 0
    updated_at   = fv(job_doc, "updated_at", "")

    # No enrichment needed
    if enrich_total == 0:
        return False

    # Job is complete
    if enriched >= enrich_total > 0:
        return False

    # Check updated_at — must be older than threshold to avoid
    # triggering immediately after a manual patch or re-queue
    if updated_at:
        try:
            ts      = updated_at.rstrip("Z")
            if "." in ts:
                ts  = ts[:26]
            updated = datetime.fromisoformat(ts).replace(tzinfo=timezone.utc)
            age_min = (datetime.now(timezone.utc) - updated).total_seconds() / 60
            if age_min < STALL_THRESHOLD_MIN:
                log.info(f"  updated_at is only {age_min:.1f} min ago — not stalled yet")
                return False
        except Exception as e:
            log.warning(f"Could not parse updated_at {updated_at}: {e}")

    # Queue must be empty — if tasks are still running, not stalled
    queue_depth = get_queue_task_count()
    if queue_depth > 0:
        log.info(f"  Queue has {queue_depth} tasks — not stalled")
        return False

    log.info(f"  Queue empty, enriched={enriched}/{enrich_total}, "
             f"updated_at age > {STALL_THRESHOLD_MIN}min — STALLED")
    return True


# ── MAIN WATCHDOG LOGIC ───────────────────────────────────────────────────────

def process_job(tenant_id, job_id, job_doc):
    """Detect stall and re-queue enrichment for a single import job."""
    enriched      = fv(job_doc, "enriched_items",     0) or 0
    enrich_total  = fv(job_doc, "enrich_total_items",  0) or 0
    enrich_failed = fv(job_doc, "enrich_failed_items", 0) or 0
    cred_id       = fv(job_doc, "channel_account_id",  "")
    channel       = fv(job_doc, "channel", "amazonnew")

    log.info(f"Job {job_id} ({tenant_id}): enriched={enriched}/{enrich_total} "
             f"failed={enrich_failed} cred={cred_id}")

    # Scan for unenriched products
    log.info(f"Scanning products for tenant {tenant_id}...")
    unenriched = find_unenriched_products(tenant_id)
    log.info(f"Found {len(unenriched)} unenriched products")

    if not unenriched:
        log.info("Nothing to re-enrich — marking job complete")
        fs_patch(
            f"tenants/{tenant_id}/import_jobs/{job_id}",
            {
                "status":         {"stringValue": "completed"},
                "status_message": {"stringValue": "Enrichment complete (watchdog verified)"},
                "updated_at":     {"timestampValue": datetime.now(timezone.utc).isoformat()},
            },
            ["status", "status_message", "updated_at"]
        )
        return 0

    # Get credential pool for this marketplace
    # Use UK marketplace by default — detect from credential if possible
    marketplace_id = "A1F83G8C2ARO7P"  # UK — fallback
    if cred_id:
        cred_doc = fs_get(f"tenants/{tenant_id}/marketplace_credentials/{cred_id}")
        if cred_doc:
            cd = cred_doc.get("fields", {}).get(
                "credential_data", {}).get("mapValue", {}).get("fields", {})
            marketplace_id = (cd.get("marketplace_id", {}).get("stringValue", "")
                              or marketplace_id)

    pool = get_pooled_credentials(marketplace_id)
    if not pool:
        log.warning(f"No pooled credentials found for {marketplace_id} — using job credential")
        pool = [cred_id] if cred_id else []

    if not pool:
        log.error("No credentials available — cannot re-enrich")
        send_alert(f"Enrichment stalled for job {job_id[:8]} ({tenant_id}) — "
                   f"no valid credentials found. Manual intervention required.")
        return 0

    # Queue enrichment tasks
    run_id     = datetime.now(timezone.utc).strftime("%H%M%S")
    ok         = 0
    fail       = 0
    task_index = 0
    cred_count = len(pool)

    log.info(f"Queuing {(len(unenriched)+BATCH_SIZE-1)//BATCH_SIZE} tasks "
             f"across {cred_count} credentials...")

    for i in range(0, len(unenriched), BATCH_SIZE):
        batch   = unenriched[i : i + BATCH_SIZE]
        cred    = pool[task_index % cred_count]
        suffix  = f"{run_id}-{task_index:06d}"
        success = queue_task(job_id, tenant_id, cred, batch, suffix)
        if success:
            ok += 1
        else:
            fail += 1
        task_index += 1
        time.sleep(0.05)  # 20 tasks/sec max

    log.info(f"Tasks queued: {ok} ok, {fail} failed")

    # Reset failure counters and update status — prevents accumulation across retries
    fs_patch(
        f"tenants/{tenant_id}/import_jobs/{job_id}",
        {
            "status":               {"stringValue": "running"},
            "status_message":       {"stringValue": f"Watchdog re-queued ~{ok*BATCH_SIZE} products"},
            "updated_at":           {"timestampValue": datetime.now(timezone.utc).isoformat()},
            "enrich_failed_items":  {"integerValue": "0"},
        },
        ["status", "status_message", "updated_at", "enrich_failed_items"]
    )

    return ok


def main():
    log.info("=" * 60)
    log.info("MarketMate Enrichment Watchdog starting")
    log.info(f"Stall threshold: {STALL_THRESHOLD_MIN} minutes")
    log.info("=" * 60)

    stalls_fixed = 0
    errors       = []

    # List all tenants
    result = fs_list("tenants", page_size=50)
    if not result:
        log.error("Could not list tenants — aborting")
        sys.exit(1)

    tenant_docs = result.get("documents", [])
    log.info(f"Checking {len(tenant_docs)} tenants...")

    for tdoc in tenant_docs:
        tid = tdoc.get("name", "").split("/")[-1]

        # List import jobs for this tenant
        jobs_result = fs_list(f"tenants/{tid}/import_jobs", page_size=20)
        if not jobs_result:
            continue

        for jdoc in jobs_result.get("documents", []):
            jid    = jdoc.get("name", "").split("/")[-1]
            status = fv(jdoc, "status", "")

            if status != "running":
                continue

            if not is_stalled(jdoc):
                log.info(f"Job {jid} ({tid}): not stalled, skipping")
                continue

            log.info(f"Job {jid} ({tid}): STALLED — triggering re-enrichment")

            try:
                tasks_queued = process_job(tid, jid, jdoc)
                if tasks_queued > 0:
                    stalls_fixed += 1
                    enriched     = fv(jdoc, "enriched_items",    0) or 0
                    enrich_total = fv(jdoc, "enrich_total_items", 0) or 0
                    send_alert(
                        f"Enrichment stall fixed for {tid} job {jid[:8]}. "
                        f"Re-queued ~{tasks_queued*BATCH_SIZE} products. "
                        f"Progress was {enriched}/{enrich_total}."
                    )
            except Exception as e:
                log.error(f"Error processing job {jid}: {e}")
                errors.append(f"{tid}/{jid}: {e}")

    log.info("=" * 60)
    log.info(f"Watchdog complete. Stalls fixed: {stalls_fixed}. Errors: {len(errors)}")
    if errors:
        for err in errors:
            log.error(f"  {err}")
    log.info("=" * 60)

    if errors:
        sys.exit(1)


if __name__ == "__main__":
    main()
