#!/usr/bin/env python3
"""
clear_enrichment.py
===================
Clears all enrichment data from products, leaving only the original import data.

What is DELETED:
  - extended_data subcollection (the amazon document inside it)

What is REMOVED from the product document:
  - brand
  - description
  - assets
  - enriched_at
  - attributes.bullet_points
  - attributes.part_number
  - attributes.style
  - attributes.manufacturer
  - attributes.color
  - attributes.size
  - attributes.model_number

What is KEPT (original import data):
  - product_id, tenant_id, product_type, status, title
  - created_at, updated_at
  - identifiers (asin, ean, upc)
  - attributes.source_sku
  - attributes.source_price, source_currency, source_quantity
  - attributes.amazon_product_type, amazon_status
  - attributes.fulfillment_channel, item_condition

Usage:
    python clear_enrichment.py --tenant tenant-10013 --dry-run
    python clear_enrichment.py --tenant tenant-10013
    python clear_enrichment.py  # all tenants

Requirements:
    pip install google-cloud-firestore
    gcloud auth application-default login
"""

import argparse
import time
import requests
import subprocess
import json
import sys

# Firestore REST base URL
FS_BASE = "https://firestore.googleapis.com/v1"
PROJECT = "marketmate-486116"
DATABASE = "(default)"

# Fields to DELETE from the product document (Firestore field paths)
FIELDS_TO_DELETE = [
    "brand",
    "description",
    "assets",
    "enriched_at",
    "key_features",
    "attributes.bullet_points",
    "attributes.part_number",
    "attributes.style",
    "attributes.manufacturer",
    "attributes.color",
    "attributes.size",
    "attributes.model_number",
]


def get_token(provided=None):
    if provided:
        return provided.strip()
    return _refresh_token_gcloud()


def _refresh_token_gcloud():
    """Refresh token via gcloud subprocess."""
    import shutil
    gcloud = shutil.which("gcloud") or r"C:\Program Files (x86)\Google\Cloud SDK\google-cloud-sdk\bin\gcloud.cmd"
    result = subprocess.run(
        [gcloud, "auth", "print-access-token"],
        capture_output=True, text=True, shell=True
    )
    token = result.stdout.strip()
    if not token:
        raise RuntimeError("Token refresh failed. Run: gcloud auth login")
    return token


def fs_headers(token):
    return {
        "Authorization": f"Bearer {token}",
        "Content-Type": "application/json",
    }


def list_products(token, tenant_id, page_token=None):
    url = (f"{FS_BASE}/projects/{PROJECT}/databases/{DATABASE}/documents"
           f"/tenants/{tenant_id}/products?pageSize=300"
           f"&mask.fieldPaths=product_id"
           f"&mask.fieldPaths=brand"
           f"&mask.fieldPaths=enriched_at"
           f"&mask.fieldPaths=description"
           f"&mask.fieldPaths=assets"
           f"&mask.fieldPaths=attributes")
    if page_token:
        url += f"&pageToken={page_token}"
    r = requests.get(url, headers=fs_headers(token))
    r.raise_for_status()
    return r.json()


def list_extended_data_docs(token, tenant_id, product_id):
    url = (f"{FS_BASE}/projects/{PROJECT}/databases/{DATABASE}/documents"
           f"/tenants/{tenant_id}/products/{product_id}/extended_data"
           f"?pageSize=20&mask.fieldPaths=product_id")
    r = requests.get(url, headers=fs_headers(token))
    if r.status_code == 404:
        return []
    r.raise_for_status()
    data = r.json()
    return [d["name"] for d in data.get("documents", [])]


def delete_document(token, doc_name):
    url = f"{FS_BASE}/{doc_name}"
    r = requests.delete(url, headers=fs_headers(token))
    return r.status_code in (200, 204)


def clear_product_fields(token, tenant_id, product_id, product_fields):
    """Remove enrichment fields from product document using PATCH with field mask."""
    
    # Build the set of fields that actually exist on this product
    existing_keys = set(product_fields.keys()) if product_fields else set()
    
    # Check which enrichment fields actually exist on this doc
    top_level_to_delete = [f for f in FIELDS_TO_DELETE 
                            if "." not in f and f in existing_keys]
    
    # For attributes, check sub-fields
    attr_subfields_to_delete = []
    if "attributes" in existing_keys:
        attr_fields = product_fields.get("attributes", {}).get("mapValue", {}).get("fields", {})
        for f in FIELDS_TO_DELETE:
            if f.startswith("attributes."):
                subkey = f.split(".", 1)[1]
                if subkey in attr_fields:
                    attr_subfields_to_delete.append(subkey)
    
    if not top_level_to_delete and not attr_subfields_to_delete:
        return False  # Nothing to clear on this product
    
    # Build PATCH request using updateMask to delete specific fields
    # Firestore REST PATCH with updateMask removes fields not included in the document
    # We send the current attributes minus the enrichment sub-fields
    
    doc_url = (f"{FS_BASE}/projects/{PROJECT}/databases/{DATABASE}/documents"
               f"/tenants/{tenant_id}/products/{product_id}")
    
    # Build update mask - all fields we want to touch
    mask_fields = list(top_level_to_delete)
    if attr_subfields_to_delete:
        for sf in attr_subfields_to_delete:
            mask_fields.append(f"attributes.{sf}")
    
    # Build the document body - only include attributes if we have sub-fields to clear
    # Fields in the mask but NOT in the document body get deleted
    doc_body = {"fields": {}}
    
    # If we're touching attributes sub-fields, we need to send the surviving attributes
    if attr_subfields_to_delete and "attributes" in existing_keys:
        attr_fields = product_fields.get("attributes", {}).get("mapValue", {}).get("fields", {})
        surviving_attrs = {k: v for k, v in attr_fields.items() 
                          if k not in attr_subfields_to_delete}
        if surviving_attrs:
            doc_body["fields"]["attributes"] = {
                "mapValue": {"fields": surviving_attrs}
            }
        # attributes itself is in the mask so it will be replaced with surviving_attrs
        # Remove the individual attr.* from mask since we're replacing the whole map
        mask_fields = [f for f in mask_fields if not f.startswith("attributes.")]
        if "attributes" not in mask_fields:
            mask_fields.append("attributes")
    
    mask_param = "&".join(f"updateMask.fieldPaths={f}" for f in mask_fields)
    patch_url = f"{doc_url}?{mask_param}"
    
    r = requests.patch(patch_url, headers=fs_headers(token), 
                       data=json.dumps(doc_body))
    return r.status_code == 200


def process_tenant(token, tenant_id, dry_run=False, resume_after=None):
    print(f"\n{'[DRY RUN] ' if dry_run else ''}Processing tenant: {tenant_id}")
    
    total = 0
    cleared_fields = 0
    deleted_extended = 0
    already_clean = 0
    errors = 0
    page_token = None
    
    while True:
        # Refresh token every 400 products (GCP tokens expire after 60 min)
        if total % 400 == 0 and total > 0:
            try:
                token = _refresh_token_gcloud()
                print(f"  [token refreshed at {total} products]")
            except Exception as e:
                print(f"  WARNING: token refresh failed: {e}")
        
        data = list_products(token, tenant_id, page_token)
        docs = data.get("documents", [])
        
        if not docs:
            break
        
        for doc in docs:
            total += 1
            product_id = doc["name"].split("/")[-1]
            fields = doc.get("fields", {})

            # Resume support: skip until we pass the resume_after product_id
            if resume_after:
                if product_id == resume_after:
                    resume_after = None  # found it, start processing from next
                    already_clean += 1
                    continue
                else:
                    already_clean += 1
                    continue

            # Check if this product has any enrichment data
            has_enrichment = any(f in fields for f in ["brand", "enriched_at", "assets", "description"])
            
            # Check extended_data subcollection
            ext_docs = list_extended_data_docs(token, tenant_id, product_id)
            
            if not has_enrichment and not ext_docs:
                already_clean += 1
                if total % 1000 == 0:
                    print(f"  Progress: {total} processed, {cleared_fields} cleared, "
                          f"{deleted_extended} subcollections deleted, {already_clean} already clean")
                continue
            
            if dry_run:
                if has_enrichment:
                    cleared_fields += 1
                if ext_docs:
                    deleted_extended += len(ext_docs)
            else:
                # Delete extended_data documents
                for doc_name in ext_docs:
                    success = delete_document(token, doc_name)
                    if success:
                        deleted_extended += 1
                    else:
                        errors += 1
                
                # Clear enrichment fields from product document
                if has_enrichment:
                    success = clear_product_fields(token, tenant_id, product_id, fields)
                    if success:
                        cleared_fields += 1
                    else:
                        errors += 1
            
            if total % 500 == 0:
                print(f"  Progress: {total} processed, {cleared_fields} cleared, "
                      f"{deleted_extended} subcollections deleted, {already_clean} already clean, "
                      f"{errors} errors")
        
        page_token = data.get("nextPageToken")
        if not page_token:
            break
        
        # Small sleep to avoid hammering the API
        time.sleep(0.1)
    
    print(f"\n  {'[DRY RUN] ' if dry_run else ''}Results for {tenant_id}:")
    print(f"    Total products scanned: {total}")
    print(f"    Products with enrichment fields cleared: {cleared_fields}")
    print(f"    extended_data subcollection docs deleted: {deleted_extended}")
    print(f"    Already clean (no enrichment data): {already_clean}")
    if errors:
        print(f"    Errors: {errors}")
    
    return cleared_fields, deleted_extended


def main():
    parser = argparse.ArgumentParser(description="Clear enrichment data from products")
    parser.add_argument("--tenant", help="Specific tenant ID (default: all tenants)")
    parser.add_argument("--dry-run", action="store_true", 
                        help="Preview counts without making changes")
    parser.add_argument("--token", help="Pass gcloud auth token directly (avoids subprocess issues on Windows)")
    parser.add_argument("--resume-after", help="Skip products before this product_id (resume after a crash)")
    args = parser.parse_args()
    
    print("Getting auth token...")
    token = get_token(args.token)
    if not token:
        print("ERROR: Could not get auth token. Run: gcloud auth login")
        sys.exit(1)
    
    if args.tenant:
        tenant_ids = [args.tenant]
    else:
        print("Loading all tenants...")
        url = (f"{FS_BASE}/projects/{PROJECT}/databases/{DATABASE}/documents/tenants"
               f"?pageSize=100&mask.fieldPaths=tenant_id")
        r = requests.get(url, headers=fs_headers(token))
        r.raise_for_status()
        data = r.json()
        tenant_ids = [d["name"].split("/")[-1] for d in data.get("documents", [])]
        print(f"Found {len(tenant_ids)} tenants: {tenant_ids}")
    
    total_cleared = 0
    total_deleted = 0
    
    for tenant_id in tenant_ids:
        resume_after = getattr(args, "resume_after", None)
        cleared, deleted = process_tenant(token, tenant_id, dry_run=args.dry_run, resume_after=resume_after)
        total_cleared += cleared
        total_deleted += deleted
    
    print(f"\n{'[DRY RUN] ' if args.dry_run else ''}COMPLETE")
    print(f"  Total products cleared: {total_cleared}")
    print(f"  Total extended_data docs deleted: {total_deleted}")
    
    if args.dry_run:
        print("\nRun without --dry-run to apply changes.")


if __name__ == "__main__":
    main()
