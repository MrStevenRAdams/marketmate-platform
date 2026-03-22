#!/usr/bin/env python3
"""
fix_broken_credentials.py — Find and deactivate Amazon credentials missing lwa_client_id.

These credentials have incomplete data (only 3 fields) and cause all enrichment
tasks assigned to them to fail with "missing required parameter: client_id".

Usage:
    python3 fix_broken_credentials.py --list       # show broken credentials
    python3 fix_broken_credentials.py --deactivate # deactivate them
"""

import argparse
import firebase_admin
from firebase_admin import firestore
from datetime import datetime, timezone

PROJECT_ID = "marketmate-486116"

if not firebase_admin._apps:
    firebase_admin.initialize_app(options={"projectId": PROJECT_ID})
db = firestore.client()

parser = argparse.ArgumentParser()
parser.add_argument("--list",       action="store_true", help="List broken credentials")
parser.add_argument("--deactivate", action="store_true", help="Deactivate broken credentials")
args = parser.parse_args()

print(f"\nScanning Amazon credentials across all tenants...\n")

broken = []
healthy = []

tenants = [t.id for t in db.collection("tenants").stream()]
for tenant_id in tenants:
    for channel in ["amazon", "amazonnew"]:
        creds = db.collection("tenants").document(tenant_id)\
                  .collection("marketplace_credentials")\
                  .where("channel", "==", channel)\
                  .stream()
        for cred in creds:
            d = cred.to_dict()
            cred_data = d.get("credential_data", {}) or {}
            active = d.get("active", False)
            has_lwa_id = bool(cred_data.get("lwa_client_id", ""))
            has_refresh = bool(cred_data.get("refresh_token", ""))
            account = d.get("account_name", cred.id)
            fields = len(cred_data)

            entry = {
                "tenant_id":   tenant_id,
                "cred_id":     cred.id,
                "channel":     channel,
                "account":     account,
                "active":      active,
                "fields":      fields,
                "has_lwa_id":  has_lwa_id,
                "has_refresh": has_refresh,
            }

            if active and not has_lwa_id:
                broken.append(entry)
            elif active and has_lwa_id:
                healthy.append(entry)

print(f"{'─'*60}")
print(f"  Healthy credentials ({len(healthy)}):")
for c in healthy:
    print(f"  ✅ {c['tenant_id']} | {c['cred_id'][:20]}... | {c['channel']} | {c['account']} ({c['fields']} fields)")

print(f"\n{'─'*60}")
print(f"  Broken credentials — missing lwa_client_id ({len(broken)}):")
for c in broken:
    print(f"  ❌ {c['tenant_id']} | {c['cred_id']} | {c['channel']} | {c['account']} ({c['fields']} fields)")

if not broken:
    print("  None found — all active Amazon credentials look healthy.")
else:
    print(f"\n  These will cause ALL enrichment tasks assigned to them to fail.")
    if args.deactivate:
        print(f"\n  Deactivating {len(broken)} broken credential(s)...")
        now = datetime.now(timezone.utc)
        for c in broken:
            ref = db.collection("tenants").document(c["tenant_id"])\
                    .collection("marketplace_credentials").document(c["cred_id"])
            ref.update({
                "active": False,
                "deactivated_reason": "auto-deactivated: missing lwa_client_id (incomplete credential data)",
                "deactivated_at": now,
            })
            print(f"  ✅ Deactivated: {c['tenant_id']} / {c['cred_id']} ({c['account']})")
        print(f"\n  Done. Re-run import to use only healthy credentials.")
    else:
        print(f"\n  Run with --deactivate to deactivate them.")
        print(f"  Or fix them manually in Firestore by adding lwa_client_id and lwa_client_secret.")
print()
