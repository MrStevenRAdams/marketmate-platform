#!/usr/bin/env python3
"""
check_queues.py — Show all active/recent jobs across MarketMate queues.
Focuses on imports, enrichment, AI generation and schema sync.

Usage:
    python3 check_queues.py                     # all tenants, active jobs
    python3 check_queues.py --tenant tenant-10013
    python3 check_queues.py --all               # include completed/failed
    python3 check_queues.py --hours 2           # jobs from last N hours (default 2)
"""

import sys
import argparse
from datetime import datetime, timezone, timedelta

try:
    import firebase_admin
    from firebase_admin import credentials, firestore
except ImportError:
    print("Run: pip install firebase-admin")
    sys.exit(1)

# ── Config ────────────────────────────────────────────────────────────────────
PROJECT_ID = "marketmate-486116"

# Firestore collections to check — (collection_name, label, tenant_scoped)
# tenant_scoped=True  → tenants/{tenantID}/{collection}
# tenant_scoped=False → root {collection}
COLLECTIONS = [
    ("import_jobs",       "📦 Product Imports",        True),
    ("import_jobs_csv",   "📄 CSV Imports",             True),
    ("ebay_enrich_jobs",  "🔍 eBay Enrichment",         True),
    ("ai_generation_jobs","✨ AI Generation",            True),
    ("schema_jobs",       "🗂  Schema Sync",             True),
    ("order_sync_jobs",   "🔄 Order Sync",              True),
    ("jobs",              "⚙️  Background Jobs",         True),
]

ACTIVE_STATUSES = {"pending", "running", "processing", "active", "queued"}

# ── CLI ───────────────────────────────────────────────────────────────────────
parser = argparse.ArgumentParser()
parser.add_argument("--tenant",  default=None, help="Filter to specific tenant ID")
parser.add_argument("--all",     action="store_true", help="Include completed/failed jobs")
parser.add_argument("--hours",   type=float, default=2, help="Look back N hours (default 2)")
parser.add_argument("--errors",  action="store_true", help="Show error log for failed jobs")
args = parser.parse_args()

cutoff = datetime.now(timezone.utc) - timedelta(hours=args.hours)

# ── Init Firestore ────────────────────────────────────────────────────────────
if not firebase_admin._apps:
    firebase_admin.initialize_app(options={"projectId": PROJECT_ID})
db = firestore.client()

# ── Helpers ───────────────────────────────────────────────────────────────────
def parse_ts(ts):
    """Normalise timestamp to UTC-aware datetime, or return None."""
    if ts is None:
        return None
    if isinstance(ts, str):
        try:
            ts = datetime.fromisoformat(ts.replace("Z", "+00:00"))
        except ValueError:
            return None
    if hasattr(ts, "tzinfo"):
        if ts.tzinfo is None:
            ts = ts.replace(tzinfo=timezone.utc)
        return ts
    return None

def fmt_time(ts):
    ts = parse_ts(ts)
    if ts is None:
        return "—"
    delta = datetime.now(timezone.utc) - ts
    secs = int(delta.total_seconds())
    if secs < 60:   return f"{secs}s ago"
    if secs < 3600: return f"{secs//60}m {secs%60}s ago"
    return f"{secs//3600}h {(secs%3600)//60}m ago"

def fmt_progress(doc):
    total     = doc.get("total_items", 0)
    processed = doc.get("processed_items", 0)
    success   = doc.get("successful_items", 0)
    failed    = doc.get("failed_items", 0)
    skipped   = doc.get("skipped_items", 0)
    if total == 0:
        return f"processed={processed}"
    pct = (processed / total * 100) if total else 0
    parts = [f"{processed}/{total} ({pct:.0f}%)"]
    if success:  parts.append(f"✅{success}")
    if failed:   parts.append(f"❌{failed}")
    if skipped:  parts.append(f"⏭ {skipped}")
    return "  ".join(parts)

def fmt_enrich(doc):
    total   = doc.get("enrich_total_items", 0)
    done    = doc.get("enriched_items", 0)
    failed  = doc.get("enrich_failed_items", 0)
    skipped = doc.get("enrich_skipped_items", 0)
    if total == 0 and done == 0:
        return None
    return f"enrich: {done}/{total}  ❌{failed}  ⏭ {skipped}"

def status_icon(status):
    return {
        "running":    "🟡",
        "pending":    "🔵",
        "processing": "🟡",
        "active":     "🟡",
        "queued":     "🔵",
        "completed":  "🟢",
        "failed":     "🔴",
        "cancelled":  "⚫",
        "error":      "🔴",
    }.get(status, "⚪")

def elapsed(doc):
    started = parse_ts(doc.get("started_at"))
    ended   = parse_ts(doc.get("completed_at"))
    if not started:
        return ""
    end = ended if ended else datetime.now(timezone.utc)
    secs = int((end - started).total_seconds())
    if secs < 60:   return f"{secs}s"
    if secs < 3600: return f"{secs//60}m {secs%60}s"
    return f"{secs//3600}h {(secs%3600)//60}m"

def is_stuck(doc):
    """Flag jobs that are 'running' but haven't updated in >10 minutes."""
    if doc.get("status") not in ("running", "processing"):
        return False
    updated = parse_ts(doc.get("updated_at"))
    if not updated:
        return False
    age = (datetime.now(timezone.utc) - updated).total_seconds()
    return age > 600  # 10 minutes

# ── Main ──────────────────────────────────────────────────────────────────────
print(f"\n{'='*72}")
print(f"  MarketMate Queue Status  |  {datetime.now().strftime('%Y-%m-%d %H:%M:%S')}")
print(f"  Lookback: {args.hours}h  |  Tenant: {args.tenant or 'all'}  |  Mode: {'all jobs' if args.all else 'active/recent'}")
print(f"{'='*72}\n")

total_found = 0

# Get list of tenants to check
if args.tenant:
    tenant_ids = [args.tenant]
else:
    tenant_docs = db.collection("tenants").stream()
    tenant_ids = [t.id for t in tenant_docs]

for col_name, label, tenant_scoped in COLLECTIONS:
    section_printed = False

    for tenant_id in tenant_ids:
        if tenant_scoped:
            col_ref = db.collection("tenants").document(tenant_id).collection(col_name)
        else:
            col_ref = db.collection(col_name)

        # Query — filter by cutoff time on updated_at or created_at
        try:
            if args.all:
                query = col_ref.order_by("updated_at", direction=firestore.Query.DESCENDING).limit(20)
            else:
                # Active jobs OR recently updated jobs
                query = col_ref.order_by("updated_at", direction=firestore.Query.DESCENDING).limit(50)

            docs = list(query.stream())
        except Exception as e:
            # Collection may not exist for this tenant — silently skip
            continue

        for doc in docs:
            d = doc.to_dict()
            if not d:
                continue

            status = d.get("status", "unknown")

            # Filter: active only (unless --all)
            if not args.all:
                updated = d.get("updated_at") or d.get("created_at")
                if updated:
                    updated = parse_ts(updated)
                    if updated and updated < cutoff and status not in ACTIVE_STATUSES:
                        continue

            # Print section header once
            if not section_printed:
                print(f"  {label}")
                print(f"  {'─'*68}")
                section_printed = True

            total_found += 1
            stuck = is_stuck(d)
            stuck_flag = "  ⚠️  POSSIBLY STUCK" if stuck else ""

            # Identity
            job_id    = d.get("job_id") or d.get("id") or doc.id
            channel   = d.get("channel", "")
            account   = d.get("account_name", "") or d.get("channel_account_id", "")
            job_type  = d.get("job_type", "") or d.get("type", "")
            msg       = d.get("status_message", "") or d.get("message", "")

            print(f"  {status_icon(status)} {status.upper():<12}  {tenant_id}  |  {job_id[:40]}")
            if channel or account or job_type:
                print(f"     channel={channel}  account={account}  type={job_type}")
            print(f"     progress: {fmt_progress(d)}")
            enrich_str = fmt_enrich(d)
            if enrich_str:
                print(f"     {enrich_str}")
            if msg:
                print(f"     msg: {msg[:120]}")

            started_at = d.get("started_at")
            updated_at = d.get("updated_at")
            elapsed_str = elapsed(d)
            print(f"     started: {fmt_time(started_at)}  |  last update: {fmt_time(updated_at)}  |  elapsed: {elapsed_str}{stuck_flag}")

            # Show recent errors for failed jobs
            if args.errors and status in ("failed", "error"):
                errors = d.get("error_log", [])
                if errors:
                    print(f"     errors ({len(errors)}):")
                    for e in errors[-5:]:
                        print(f"       • {e.get('external_id','')} — {e.get('message','')}")

            print()

    if section_printed:
        print()

if total_found == 0:
    print(f"  No {'active ' if not args.all else ''}jobs found in the last {args.hours}h.\n")

print(f"{'='*72}\n")
