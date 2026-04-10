#!/usr/bin/env python3
"""
seed_inventory.py
=================
Creates inventory records for all products that don't already have one.

Rules:
- Only creates records for 'simple' and 'variant' product types
- 'parent' products are groupings only — no stock tracked at parent level
- 'bundle' products are also skipped (stock deducted from components)
- Skips any SKU that already has an inventory document
- Sets all quantities to 0 — user must then use My Inventory to set actual counts
- Assigns stock to the tenant's default warehouse (or first active warehouse)

Usage:
    python seed_inventory.py [--tenant tenant-10013] [--dry-run]

Requirements:
    pip install google-cloud-firestore
    GOOGLE_APPLICATION_CREDENTIALS or gcloud auth application-default login
"""

import argparse
import datetime
import sys
import uuid
from google.cloud import firestore

def get_default_warehouse(db, tenant_id):
    """Get the default warehouse source_id for a tenant."""
    sources = db.collection("tenants").document(tenant_id)\
                .collection("fulfilment_sources")\
                .where("active", "==", True)\
                .stream()
    
    default = None
    first = None
    for s in sources:
        data = s.to_dict()
        if data.get("type") in ("own_warehouse", "fba", "3pl"):
            if first is None:
                first = data
            if data.get("default", False):
                default = data
                break
    
    result = default or first
    if result:
        return result.get("source_id"), result.get("name", "Default Warehouse")
    return "default", "Default Warehouse"


def seed_tenant(db, tenant_id, dry_run=False):
    print(f"\n{'[DRY RUN] ' if dry_run else ''}Processing tenant: {tenant_id}")

    # Get default warehouse
    warehouse_id, warehouse_name = get_default_warehouse(db, tenant_id)
    print(f"  Default warehouse: {warehouse_name} ({warehouse_id})")

    # Load existing inventory SKUs to avoid duplicates
    print("  Loading existing inventory records...")
    existing_skus = set()
    inv_docs = db.collection("tenants").document(tenant_id)\
                 .collection("inventory").stream()
    for doc in inv_docs:
        data = doc.to_dict()
        if data.get("sku"):
            existing_skus.add(data["sku"])
    print(f"  Existing inventory records: {len(existing_skus)}")

    # Load all products
    print("  Loading products...")
    products = db.collection("tenants").document(tenant_id)\
                  .collection("products").stream()

    to_create = []
    skipped_parent = 0
    skipped_bundle = 0
    skipped_existing = 0
    skipped_no_sku = 0

    for doc in products:
        p = doc.to_dict()
        product_type = p.get("product_type", "simple")
        sku = p.get("sku", "").strip()
        title = p.get("title", "")

        # Skip parent products — no stock at parent level
        if product_type == "parent":
            skipped_parent += 1
            continue

        # Skip bundles — stock is at component level
        if product_type == "bundle":
            skipped_bundle += 1
            continue

        # Skip products with no SKU
        if not sku:
            skipped_no_sku += 1
            continue

        # Skip if inventory record already exists
        if sku in existing_skus:
            skipped_existing += 1
            continue

        to_create.append({
            "sku": sku,
            "product_name": title,
            "product_type": product_type,
        })

    print(f"  Products to create inventory for: {len(to_create)}")
    print(f"  Skipped — parent: {skipped_parent}, bundle: {skipped_bundle}, "
          f"existing: {skipped_existing}, no SKU: {skipped_no_sku}")

    if not to_create:
        print("  Nothing to create.")
        return 0

    if dry_run:
        print(f"  [DRY RUN] Would create {len(to_create)} inventory records")
        for item in to_create[:5]:
            print(f"    {item['sku']} — {item['product_name'][:60]}")
        if len(to_create) > 5:
            print(f"    ... and {len(to_create) - 5} more")
        return len(to_create)

    # Write in batches of 500
    now = datetime.datetime.now(datetime.timezone.utc)
    batch_size = 400
    total_written = 0
    
    for i in range(0, len(to_create), batch_size):
        chunk = to_create[i:i + batch_size]
        batch = db.batch()

        for item in chunk:
            inv_id = "inv_" + item["sku"].replace("/", "_").replace(" ", "_")
            ref = db.collection("tenants").document(tenant_id)\
                    .collection("inventory").document(inv_id)

            batch.set(ref, {
                "inventory_id":   inv_id,
                "sku":            item["sku"],
                "product_name":   item["product_name"],
                "locations": [{
                    "location_id":   warehouse_id,
                    "location_name": warehouse_name,
                    "on_hand":       0,
                    "reserved":      0,
                    "available":     0,
                    "inbound":       0,
                    "safety_stock":  0,
                }],
                "total_on_hand":    0,
                "total_reserved":   0,
                "total_available":  0,
                "total_inbound":    0,
                "safety_stock":     0,
                "reorder_point":    0,
                "updated_at":       now,
            })

        batch.commit()
        total_written += len(chunk)
        print(f"  Written {total_written}/{len(to_create)}...")

    print(f"  ✅ Created {total_written} inventory records for tenant {tenant_id}")
    return total_written


def main():
    parser = argparse.ArgumentParser(description="Seed inventory records for existing products")
    parser.add_argument("--tenant", help="Specific tenant ID (default: all tenants)")
    parser.add_argument("--dry-run", action="store_true", help="Preview without writing")
    parser.add_argument("--project", default="marketmate-486116", help="GCP project ID")
    args = parser.parse_args()

    print(f"Connecting to Firestore project: {args.project}")
    db = firestore.Client(project=args.project)

    if args.tenant:
        tenant_ids = [args.tenant]
    else:
        print("Loading all tenants...")
        tenant_ids = [doc.id for doc in db.collection("tenants").stream()]
        print(f"Found {len(tenant_ids)} tenants: {tenant_ids}")

    total = 0
    for tenant_id in tenant_ids:
        count = seed_tenant(db, tenant_id, dry_run=args.dry_run)
        total += count

    print(f"\n{'[DRY RUN] ' if args.dry_run else ''}Total inventory records "
          f"{'would be ' if args.dry_run else ''}created: {total}")


if __name__ == "__main__":
    main()
