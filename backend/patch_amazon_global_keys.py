#!/usr/bin/env python3
"""
patch_amazon_global_keys.py — Set lwa_client_id and/or lwa_client_secret in platform_config/amazon.

Usage:
    python3 patch_amazon_global_keys.py --client-id amzn1.application-oa2-client.XXX
    python3 patch_amazon_global_keys.py --client-secret amzn1.oa2-cs.XXX
    python3 patch_amazon_global_keys.py --client-id XXX --client-secret YYY
    python3 patch_amazon_global_keys.py --show
"""
import argparse
import firebase_admin
from firebase_admin import firestore

parser = argparse.ArgumentParser()
parser.add_argument("--client-id",     default=None, help="LWA client ID to set")
parser.add_argument("--client-secret", default=None, help="LWA client secret to set")
parser.add_argument("--show",          action="store_true", help="Show current values")
args = parser.parse_args()

if not firebase_admin._apps:
    firebase_admin.initialize_app(options={"projectId": "marketmate-486116"})
db = firestore.client()

ref = db.collection("platform_config").document("amazon")
doc = ref.get()
current_keys = doc.to_dict().get("keys", {}) or {} if doc.exists else {}

print(f"\nCurrent platform_config/amazon.keys:")
for k, v in sorted(current_keys.items()):
    display = f"[SET - {len(str(v))} chars]  {str(v)[:20]}..." if v else "[EMPTY]"
    print(f"  {k}: {display}")

if args.show:
    exit(0)

updates = {}
if args.client_id:
    updates["lwa_client_id"] = args.client_id
if args.client_secret:
    updates["lwa_client_secret"] = args.client_secret

if not updates:
    print("\nNo changes — pass --client-id and/or --client-secret")
    exit(0)

updated_keys = {**current_keys, **updates}
ref.update({"keys": updated_keys})

print(f"\n✅ Updated platform_config/amazon.keys:")
for k, v in sorted(updated_keys.items()):
    display = f"[SET - {len(str(v))} chars]" if v else "[EMPTY]"
    print(f"  {k}: {display}")
print("\nEnrichment tasks will pick up new values on next retry.")
