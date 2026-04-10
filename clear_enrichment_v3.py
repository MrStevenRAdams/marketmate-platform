#!/usr/bin/env python3
"""
clear_enrichment_v3.py
======================
Clears all enrichment data from products, leaving only the original import data.

What is DELETED:
  - extended_data subcollection (the amazon document inside it)

What is REMOVED from the product document:
  - brand, description, assets, enriched_at, key_features
  - attributes.bullet_points, part_number, style, manufacturer,
    color, size, model_number

What is KEPT (original import data):
  - product_id, tenant_id, product_type, status, title
  - created_at, updated_at
  - identifiers (asin, ean, upc)
  - attributes.source_sku, source_price, source_currency, source_quantity
  - attributes.amazon_product_type, amazon_status
  - attributes.fulfillment_channel, item_condition

v3 changes vs v2:
  - Token stored in a mutable container so all helpers share one live reference
  - Auto-refresh triggered by wall-clock time (every 45 min) not product count
  - All HTTP calls go through authed_request() which retries once on 401
    by refreshing the token first — no more crashes from stale tokens
  - Last-processed product_id printed on crash/exit for easy --resume-after

Usage:
    python clear_enrichment_v3.py --tenant tenant-10013 --token $token
    python clear_enrichment_v3.py --tenant tenant-10013 --token $token --dry-run
    python clear_enrichment_v3.py --tenant tenant-10013 --token $token --resume-after <product_id>
"""

import argparse
import time
import requests
import subprocess
import json
import sys

# ── Constants ──────────────────────────────────────────────────────────────────
FS_BASE  = "https://firestore.googleapis.com/v1"
PROJECT  = "marketmate-486116"
DATABASE = "(default)"

FIELDS_TO_DELETE = [
    "brand", "description", "assets", "enriched_at", "key_features",
    "attributes.bullet_points", "attributes.part_number", "attributes.style",
    "attributes.manufacturer", "attributes.color", "attributes.size",
    "attributes.model_number",
]

# Token refresh interval: 45 minutes (tokens last 60 min, giving 15 min buffer)
REFRESH_INTERVAL = 45 * 60

# ── Token management ───────────────────────────────────────────────────────────
# Stored as a mutable dict so all functions share one live reference without
# needing to pass it around or use globals explicitly.
_token_state = {
    "token": "",
    "refreshed_at": 0.0,
}


def _refresh_via_gcloud():
    import shutil
    gcloud = (shutil.which("gcloud")
              or r"C:\Program Files (x86)\Google\Cloud SDK\google-cloud-sdk\bin\gcloud.cmd")
    result = subprocess.run(
        [gcloud, "auth", "print-access-token"],
        capture_output=True, text=True, shell=True
    )
    token = result.stdout.strip()
    if not token:
        raise RuntimeError("gcloud returned empty token. Run: gcloud auth login")
    return token


def init_token(provided=None):
    """Call once at startup with the --token value (or None to use gcloud)."""
    token = provided.strip() if provided else _refresh_via_gcloud()
    _token_state["token"]        = token
    _token_state["refreshed_at"] = time.time()
    return token


def maybe_refresh_token():
    """Refresh if the token is older than REFRESH_INTERVAL seconds."""
    age = time.time() - _token_state["refreshed_at"]
    if age >= REFRESH_INTERVAL:
        try:
            token = _refresh_via_gcloud()
            _token_state["token"]        = token
            _token_state["refreshed_at"] = time.time()
            print(f"  [token refreshed — was {age/60:.0f} min old]")
        except Exception as e:
            print(f"  WARNING: scheduled token refresh failed: {e}")


def force_refresh_token():
    """Force an immediate refresh (called after a 401)."""
    token = _refresh_via_gcloud()
    _token_state["token"]        = token
    _token_state["refreshed_at"] = time.time()
    print(f"  [token force-refreshed after 401]")


def current_token():
    return _token_state["token"]


def fs_headers():
    return {
        "Authorization": f"Bearer {current_token()}",
        "Content-Type":  "application/json",
    }


# ── HTTP helpers with 401-retry ────────────────────────────────────────────────

def authed_get(url):
    """GET with one automatic retry on 401."""
    maybe_refresh_token()
    r = requests.get(url, headers=fs_headers())
    if r.status_code == 401:
        force_refresh_token()
        r = requests.get(url, headers=fs_headers())
    return r


def authed_delete(url):
    """DELETE with one automatic retry on 401."""
    maybe_refresh_token()
    r = requests.delete(url, headers=fs_headers())
    if r.status_code == 401:
        force_refresh_token()
        r = requests.delete(url, headers=fs_headers())
    return r


def authed_patch(url, body):
    """PATCH with one automatic retry on 401."""
    maybe_refresh_token()
    r = requests.patch(url, headers=fs_headers(), data=json.dumps(body))
    if r.status_code == 401:
        force_refresh_token()
        r = requests.patch(url, headers=fs_headers(), data=json.dumps(body))
    return r


# ── Firestore operations ───────────────────────────────────────────────────────

def list_products(tenant_id, page_token=None):
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
    r = authed_get(url)
    r.raise_for_status()
    return r.json()


def list_extended_data_docs(tenant_id, product_id):
    url = (f"{FS_BASE}/projects/{PROJECT}/databases/{DATABASE}/documents"
           f"/tenants/{tenant_id}/products/{product_id}/extended_data"
           f"?pageSize=20&mask.fieldPaths=product_id")
    r = authed_get(url)
    if r.status_code == 404:
        return []
    r.raise_for_status()
    return [d["name"] for d in r.json().get("documents", [])]


def delete_document(doc_name):
    url = f"{FS_BASE}/{doc_name}"
    r = authed_delete(url)
    return r.status_code in (200, 204)


def clear_product_fields(tenant_id, product_id, product_fields):
    """Remove enrichment fields via PATCH + updateMask."""
    existing_keys = set(product_fields.keys()) if product_fields else set()

    top_level_to_delete = [f for f in FIELDS_TO_DELETE
                           if "." not in f and f in existing_keys]

    attr_subfields_to_delete = []
    if "attributes" in existing_keys:
        attr_fields = (product_fields
                       .get("attributes", {})
                       .get("mapValue", {})
                       .get("fields", {}))
        for f in FIELDS_TO_DELETE:
            if f.startswith("attributes."):
                subkey = f.split(".", 1)[1]
                if subkey in attr_fields:
                    attr_subfields_to_delete.append(subkey)

    if not top_level_to_delete and not attr_subfields_to_delete:
        return False  # nothing to clear

    doc_url = (f"{FS_BASE}/projects/{PROJECT}/databases/{DATABASE}/documents"
               f"/tenants/{tenant_id}/products/{product_id}")

    mask_fields = list(top_level_to_delete)
    doc_body    = {"fields": {}}

    if attr_subfields_to_delete and "attributes" in existing_keys:
        attr_fields = (product_fields
                       .get("attributes", {})
                       .get("mapValue", {})
                       .get("fields", {}))
        surviving = {k: v for k, v in attr_fields.items()
                     if k not in attr_subfields_to_delete}
        if surviving:
            doc_body["fields"]["attributes"] = {"mapValue": {"fields": surviving}}
        mask_fields = [f for f in mask_fields if not f.startswith("attributes.")]
        if "attributes" not in mask_fields:
            mask_fields.append("attributes")

    mask_param = "&".join(f"updateMask.fieldPaths={f}" for f in mask_fields)
    r = authed_patch(f"{doc_url}?{mask_param}", doc_body)
    return r.status_code == 200


# ── Main processing loop ───────────────────────────────────────────────────────

def process_tenant(tenant_id, dry_run=False, resume_after=None):
    print(f"\n{'[DRY RUN] ' if dry_run else ''}Processing tenant: {tenant_id}")
    if resume_after:
        print(f"  Resuming after product_id: {resume_after}")

    total           = 0
    cleared_fields  = 0
    deleted_extended= 0
    already_clean   = 0
    errors          = 0
    page_token      = None
    last_product_id = None   # tracked for crash recovery

    try:
        while True:
            data = list_products(tenant_id, page_token)
            docs = data.get("documents", [])
            if not docs:
                break

            for doc in docs:
                total      += 1
                product_id  = doc["name"].split("/")[-1]
                fields      = doc.get("fields", {})
                last_product_id = product_id

                # ── Resume skip ────────────────────────────────────────────
                if resume_after:
                    if product_id == resume_after:
                        resume_after = None   # found the marker, start next
                    already_clean += 1
                    continue

                # ── Detect enrichment ──────────────────────────────────────
                has_enrichment = any(
                    f in fields
                    for f in ["brand", "enriched_at", "assets", "description"]
                )
                ext_docs = list_extended_data_docs(tenant_id, product_id)

                if not has_enrichment and not ext_docs:
                    already_clean += 1
                    if total % 1000 == 0:
                        print(f"  Progress: {total} processed, {cleared_fields} cleared, "
                              f"{deleted_extended} subcollections deleted, "
                              f"{already_clean} already clean")
                    continue

                # ── Clear ──────────────────────────────────────────────────
                if dry_run:
                    if has_enrichment: cleared_fields   += 1
                    if ext_docs:       deleted_extended += len(ext_docs)
                else:
                    for doc_name in ext_docs:
                        if delete_document(doc_name):
                            deleted_extended += 1
                        else:
                            errors += 1

                    if has_enrichment:
                        if clear_product_fields(tenant_id, product_id, fields):
                            cleared_fields += 1
                        else:
                            errors += 1

                if total % 500 == 0:
                    print(f"  Progress: {total} processed, {cleared_fields} cleared, "
                          f"{deleted_extended} subcollections deleted, "
                          f"{already_clean} already clean, {errors} errors")

            page_token = data.get("nextPageToken")
            if not page_token:
                break

            time.sleep(0.05)   # reduced from 0.1 — token refresh handles rate

    except Exception as e:
        print(f"\n  *** CRASHED at product {total} ***")
        print(f"  Last product_id: {last_product_id}")
        print(f"  To resume: --resume-after {last_product_id}")
        print(f"  Error: {e}")
        raise

    print(f"\n  {'[DRY RUN] ' if dry_run else ''}Results for {tenant_id}:")
    print(f"    Total products scanned:              {total}")
    print(f"    Products with enrichment cleared:    {cleared_fields}")
    print(f"    extended_data docs deleted:          {deleted_extended}")
    print(f"    Already clean:                       {already_clean}")
    if errors:
        print(f"    Errors:                              {errors}")

    return cleared_fields, deleted_extended


# ── Entry point ────────────────────────────────────────────────────────────────

def main():
    parser = argparse.ArgumentParser(description="Clear enrichment data from products")
    parser.add_argument("--tenant",       help="Tenant ID (default: all tenants)")
    parser.add_argument("--dry-run",      action="store_true")
    parser.add_argument("--token",        help="gcloud access token (avoids subprocess on Windows)")
    parser.add_argument("--resume-after", help="Skip products up to and including this product_id")
    args = parser.parse_args()

    print("Getting auth token...")
    token = init_token(args.token)
    if not token:
        print("ERROR: Could not get auth token. Run: gcloud auth login")
        sys.exit(1)
    print(f"  Token acquired. Will auto-refresh every {REFRESH_INTERVAL//60} min "
          f"and retry once on any 401.")

    if args.tenant:
        tenant_ids = [args.tenant]
    else:
        print("Loading all tenants...")
        url = (f"{FS_BASE}/projects/{PROJECT}/databases/{DATABASE}/documents/tenants"
               f"?pageSize=100&mask.fieldPaths=tenant_id")
        r = authed_get(url)
        r.raise_for_status()
        tenant_ids = [d["name"].split("/")[-1] for d in r.json().get("documents", [])]
        print(f"Found {len(tenant_ids)} tenants: {tenant_ids}")

    total_cleared = 0
    total_deleted = 0

    for tenant_id in tenant_ids:
        c, d = process_tenant(
            tenant_id,
            dry_run=args.dry_run,
            resume_after=args.resume_after,
        )
        total_cleared += c
        total_deleted += d

    print(f"\n{'[DRY RUN] ' if args.dry_run else ''}COMPLETE")
    print(f"  Total products cleared:          {total_cleared}")
    print(f"  Total extended_data docs deleted: {total_deleted}")
    if args.dry_run:
        print("\nRun without --dry-run to apply changes.")


if __name__ == "__main__":
    main()
