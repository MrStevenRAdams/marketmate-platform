#!/usr/bin/env python3
"""
Temu Clean Slate Script — tenant-10013
=======================================
Deletes all Temu-related data for tenant-10013 so you can run a fresh import.

What it deletes:
  - listings          where channel == "temu"
  - import_mappings   where channel == "temu"
  - import_jobs       where channel == "temu"
  - products          where status == "pending_review" (staged from Temu import)
    + their extended_data subcollections
  - products          that have NO listings on any channel and NO import_mappings
    (orphaned products created by Temu import with no other channel presence)
    + their extended_data subcollections

What it DOES NOT delete:
  - Products that have Amazon/eBay/Shopify listings (they existed before Temu import)
  - Products that have non-Temu import mappings
  - Any orders, shipments, purchase orders, customers

USAGE:
  # Step 1 — dry run (safe, no changes made):
  python cleanup_temu_tenant10013.py --dry-run

  # Step 2 — review the output, then run for real:
  python cleanup_temu_tenant10013.py

REQUIREMENTS:
  pip install google-cloud-firestore
  gcloud auth application-default login
"""

import sys
import argparse
from google.cloud import firestore

PROJECT_ID  = "marketmate-486116"
TENANT_ID   = "tenant-10013"
TEMU_CHANNEL = "temu"

def main(dry_run: bool):
    mode = "DRY RUN" if dry_run else "LIVE DELETE"
    print(f"\n{'='*60}")
    print(f"  Temu Clean Slate — {mode}")
    print(f"  Project : {PROJECT_ID}")
    print(f"  Tenant  : {TENANT_ID}")
    print(f"{'='*60}\n")

    db = firestore.Client(project=PROJECT_ID)
    tenant_ref = db.collection("tenants").document(TENANT_ID)

    stats = {
        "listings_deleted": 0,
        "mappings_deleted": 0,
        "jobs_deleted": 0,
        "products_deleted": 0,
        "extended_data_deleted": 0,
    }

    # ------------------------------------------------------------------
    # 1. Delete Temu listings
    # ------------------------------------------------------------------
    print("── Step 1: Temu listings ──────────────────────────────────")
    listings_ref = tenant_ref.collection("listings")
    temu_listings = listings_ref.where("channel", "==", TEMU_CHANNEL).stream()

    for doc in temu_listings:
        d = doc.to_dict()
        label = f"{d.get('channel_identifiers', {}).get('listing_id', '?')} / SKU:{d.get('channel_identifiers', {}).get('sku', '?')}"
        print(f"  {'[DELETE]' if not dry_run else '[DRY]'} listing {doc.id} — {label}")
        if not dry_run:
            doc.reference.delete()
        stats["listings_deleted"] += 1

    print(f"  → {stats['listings_deleted']} Temu listings {'deleted' if not dry_run else 'would be deleted'}\n")

    # ------------------------------------------------------------------
    # 2. Delete Temu import_mappings
    # ------------------------------------------------------------------
    print("── Step 2: Temu import_mappings ───────────────────────────")
    mappings_ref = tenant_ref.collection("import_mappings")
    temu_mappings = mappings_ref.where("channel", "==", TEMU_CHANNEL).stream()

    temu_mapped_product_ids = set()
    for doc in temu_mappings:
        d = doc.to_dict()
        pid = d.get("product_id", "")
        if pid:
            temu_mapped_product_ids.add(pid)
        print(f"  {'[DELETE]' if not dry_run else '[DRY]'} mapping {doc.id} — product:{pid} external:{d.get('external_id','?')}")
        if not dry_run:
            doc.reference.delete()
        stats["mappings_deleted"] += 1

    print(f"  → {stats['mappings_deleted']} Temu mappings {'deleted' if not dry_run else 'would be deleted'}\n")

    # ------------------------------------------------------------------
    # 3. Delete Temu import_jobs
    # ------------------------------------------------------------------
    print("── Step 3: Temu import_jobs ───────────────────────────────")
    jobs_ref = tenant_ref.collection("import_jobs")
    temu_jobs = jobs_ref.where("channel", "==", TEMU_CHANNEL).stream()

    for doc in temu_jobs:
        d = doc.to_dict()
        print(f"  {'[DELETE]' if not dry_run else '[DRY]'} job {doc.id} — status:{d.get('status','?')} processed:{d.get('processed_items',0)}")
        if not dry_run:
            doc.reference.delete()
        stats["jobs_deleted"] += 1

    print(f"  → {stats['jobs_deleted']} Temu jobs {'deleted' if not dry_run else 'would be deleted'}\n")

    # ------------------------------------------------------------------
    # 4. Find products to delete
    #    A) All pending_review products (staged by Temu import)
    #    B) Products mapped to Temu that have NO other channel mappings
    #       and NO non-Temu listings
    # ------------------------------------------------------------------
    print("── Step 4: Products ───────────────────────────────────────")

    # Build set of product_ids with non-Temu mappings (keep these)
    all_mappings = tenant_ref.collection("import_mappings").stream()
    non_temu_mapped_pids = set()
    for doc in all_mappings:
        d = doc.to_dict()
        if d.get("channel") != TEMU_CHANNEL and d.get("product_id"):
            non_temu_mapped_pids.add(d["product_id"])

    # Build set of product_ids with non-Temu listings (keep these)
    all_listings = tenant_ref.collection("listings").stream()
    non_temu_listed_pids = set()
    for doc in all_listings:
        d = doc.to_dict()
        if d.get("channel") != TEMU_CHANNEL and d.get("product_id"):
            non_temu_listed_pids.add(d["product_id"])

    safe_pids = non_temu_mapped_pids | non_temu_listed_pids
    print(f"  Products with non-Temu presence (SAFE — will not delete): {len(safe_pids)}")

    products_to_delete = set()

    # A) pending_review products
    pending = tenant_ref.collection("products").where("status", "==", "pending_review").stream()
    for doc in pending:
        if doc.id not in safe_pids:
            products_to_delete.add(doc.id)

    # B) Temu-mapped products with no other channel presence
    for pid in temu_mapped_product_ids:
        if pid not in safe_pids:
            products_to_delete.add(pid)

    print(f"  Products to delete: {len(products_to_delete)}")

    for pid in products_to_delete:
        prod_ref = tenant_ref.collection("products").document(pid)
        prod_doc = prod_ref.get()
        if not prod_doc.exists:
            continue
        d = prod_doc.to_dict()
        title = d.get("title", "?")[:60]
        sku   = d.get("sku", "?")
        status = d.get("status", "?")
        print(f"  {'[DELETE]' if not dry_run else '[DRY]'} product {pid} — [{status}] {sku} | {title}")

        # Delete extended_data subcollection first
        ext_docs = prod_ref.collection("extended_data").stream()
        ext_count = 0
        for ext_doc in ext_docs:
            if not dry_run:
                ext_doc.reference.delete()
            ext_count += 1
        if ext_count:
            print(f"             └─ {ext_count} extended_data docs {'deleted' if not dry_run else 'would be deleted'}")
            stats["extended_data_deleted"] += ext_count

        if not dry_run:
            prod_ref.delete()
        stats["products_deleted"] += 1

    print(f"  → {stats['products_deleted']} products {'deleted' if not dry_run else 'would be deleted'}\n")

    # ------------------------------------------------------------------
    # Summary
    # ------------------------------------------------------------------
    print(f"{'='*60}")
    print(f"  SUMMARY ({mode})")
    print(f"{'='*60}")
    print(f"  Listings deleted      : {stats['listings_deleted']}")
    print(f"  Import mappings deleted: {stats['mappings_deleted']}")
    print(f"  Import jobs deleted   : {stats['jobs_deleted']}")
    print(f"  Products deleted      : {stats['products_deleted']}")
    print(f"  Extended data deleted : {stats['extended_data_deleted']}")
    print(f"{'='*60}\n")

    if dry_run:
        print("  ✓ Dry run complete — nothing was changed.")
        print("  Run without --dry-run to apply changes.\n")
    else:
        print("  ✓ Done. You now have a clean slate for Temu.")
        print("  Trigger a fresh import from Channel Config → Product Import tab.\n")


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description="Temu clean slate for tenant-10013")
    parser.add_argument(
        "--dry-run",
        action="store_true",
        help="Print what would be deleted without making any changes",
    )
    args = parser.parse_args()
    main(dry_run=args.dry_run)
