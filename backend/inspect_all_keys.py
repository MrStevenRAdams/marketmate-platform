#!/usr/bin/env python3
"""Show all marketplace_keys documents and the full credential structure."""
import firebase_admin
from firebase_admin import firestore

if not firebase_admin._apps:
    firebase_admin.initialize_app(options={"projectId": "marketmate-486116"})
db = firestore.client()

# Check all platform_config / marketplace_keys documents
print("=== platform_config collection ===")
for doc in db.collection("platform_config").stream():
    d = doc.to_dict()
    print(f"  {doc.id}: {sorted(d.keys())}")

print("\n=== marketplace_keys collection ===")
for doc in db.collection("marketplace_keys").stream():
    d = doc.to_dict()
    print(f"  {doc.id}: fields={sorted(d.keys())}")
    for k, v in sorted(d.items()):
        has = bool(v)
        print(f"    {k}: {'[SET]' if has else '[EMPTY]'}")

# Check one of the broken credentials in full
print("\n=== Full credential doc for aa0c63c2 ===")
doc = db.collection("tenants").document("tenant-10007")\
        .collection("marketplace_credentials")\
        .document("aa0c63c2-d47d-46f0-bf8a-fe172d45f494").get()
if doc.exists:
    d = doc.to_dict()
    # Show all top-level keys
    print(f"  Top-level keys: {sorted(d.keys())}")
    # Show credential_data keys
    cred_data = d.get("credential_data", {}) or {}
    print(f"  credential_data ({len(cred_data)} fields): {sorted(cred_data.keys())}")
    # Show encrypted_fields
    print(f"  encrypted_fields: {d.get('encrypted_fields', [])}")
    # Any other data fields
    for k in sorted(d.keys()):
        if k not in ("credential_data", "encrypted_fields"):
            v = d[k]
            if isinstance(v, str) and len(v) > 40:
                v = v[:20] + "..."
            print(f"  {k}: {v}")
