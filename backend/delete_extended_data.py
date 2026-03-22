#!/usr/bin/env python3
"""
Deletes the extended_data subcollection for a tenant, one doc at a time,
skipping any that fail due to size limits.

Usage:
  pip install firebase-admin
  python delete_extended_data.py
"""

import firebase_admin
from firebase_admin import credentials, firestore

TENANT_ID = "tenant-demo"
SERVICE_ACCOUNT_KEY = "./serviceAccountKey.json"  # adjust path if needed

cred = credentials.Certificate(SERVICE_ACCOUNT_KEY)
firebase_admin.initialize_app(cred)
db = firestore.client()

collection_ref = (
    db.collection("tenants")
      .document(TENANT_ID)
      .collection("extended_data")
)

deleted = 0
skipped = 0

print(f"Deleting extended_data for tenant: {TENANT_ID}")
print("Fetching in batches of 50...\n")

while True:
    docs = list(collection_ref.limit(50).stream())
    if not docs:
        break

    for doc in docs:
        try:
            doc.reference.delete()
            deleted += 1
            if deleted % 100 == 0:
                print(f"  [{deleted} deleted, {skipped} skipped]")
        except Exception as e:
            print(f"  SKIP {doc.id[:20]}...: {e}")
            skipped += 1

print(f"\nFinished. Deleted: {deleted} | Skipped (too large): {skipped}")
