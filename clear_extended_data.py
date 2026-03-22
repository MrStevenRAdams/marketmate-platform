"""
Clears two things for tenant-10007:
1. extended_data subcollection docs under every products/{id}/extended_data/
2. The stale top-level tenants/tenant-10007/extended_data collection
Deletes one document at a time to avoid Firestore transaction size limits.
"""

from google.cloud import firestore

PROJECT = "marketmate-486116"
TENANT  = "tenant-10007"

db = firestore.Client(project=PROJECT)

def delete_collection_one_by_one(col_ref):
    deleted = 0
    while True:
        docs = list(col_ref.limit(100).stream())
        if not docs:
            break
        for doc in docs:
            doc.reference.delete()
            deleted += 1
            if deleted % 50 == 0:
                print(f"  deleted {deleted} docs so far...")
    return deleted

# 1. products/{id}/extended_data/*
print("Scanning products for extended_data subcollections...")
products_ref = db.collection("tenants").document(TENANT).collection("products")
total_ext = 0
product_count = 0

for prod in products_ref.stream():
    product_count += 1
    ext_ref = prod.reference.collection("extended_data")
    ext_docs = list(ext_ref.limit(1).stream())
    if ext_docs:
        print(f"  Clearing extended_data for product {prod.id}...")
        n = delete_collection_one_by_one(ext_ref)
        total_ext += n

print(f"Done. Scanned {product_count} products, deleted {total_ext} extended_data docs.\n")

# 2. tenants/tenant-10007/extended_data (top-level stray collection)
print("Clearing top-level extended_data collection...")
top_ext_ref = db.collection("tenants").document(TENANT).collection("extended_data")
total_top = delete_collection_one_by_one(top_ext_ref)
print(f"Done. Deleted {total_top} docs from top-level extended_data.\n")

print("All done.")
