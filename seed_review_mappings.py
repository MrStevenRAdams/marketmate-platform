#!/usr/bin/env python3
"""
Review Mappings Test Seeder
============================
Plants 5-10 fake products into pending_imports and creates matching
import_jobs and match_results so the Review Mappings page has
something to show without needing a real import.

Creates one product in each category:
  - 2 × Exact match (matched_product_id set to an existing product)
  - 2 × Fuzzy match (title-similar, with score)
  - 4 × Unmatched (no match found)

Requirements:
    pip install google-cloud-firestore

Run:
    python seed_review_mappings.py --tenant tenant-10013 [--clear]

    --clear  removes seeded data first (looks for seed_marker field)
"""

import argparse
import sys
import uuid
from datetime import datetime, timezone
from google.cloud import firestore

PROJECT_ID = "marketmate-486116"

SEED_PRODUCTS = [
    # (title, sku, external_id, match_type, match_score, match_reason)
    ("LEGO Star Wars Millennium Falcon 75192", "LEGO-75192new",    "601111111111001", "exact", 1.0,  "SKU matched existing product"),
    ("LEGO Technic Bugatti Chiron 42083",      "LEGO-42083new",    "601111111111002", "exact", 1.0,  "SKU matched existing product"),
    ("Barbie Dreamhouse Playset",              "BARBIE-DH-2024",   "601111111111003", "fuzzy", 0.82, "Title similarity 82%"),
    ("Hot Wheels Ultimate Garage Playset",     "HW-GARAGE-2024",   "601111111111004", "fuzzy", 0.71, "Title similarity 71%"),
    ("Transformers Optimus Prime Deluxe",      "TF-OP-DELUXEnew",  "601111111111005", "none",  0.0,  ""),
    ("Play-Doh Kitchen Creations Set",         "PD-KITCHEN-2024",  "601111111111006", "none",  0.0,  ""),
    ("Nerf Elite 2.0 Commander RD-6",          "NERF-ELITE2-CMD",  "601111111111007", "none",  0.0,  ""),
    ("Fisher-Price Laugh Learn Smart Stages",  "FP-LL-SMART-2024", "601111111111008", "none",  0.0,  ""),
]

FAKE_IMAGE = "https://via.placeholder.com/80x80/1a1a2e/4ade80?text=TEST"

def main(tenant_id: str, clear: bool):
    db = firestore.Client(project=PROJECT_ID)
    tenant_ref = db.collection("tenants").document(tenant_id)

    print(f"\nReview Mappings Test Seeder")
    print(f"Tenant:  {tenant_id}")
    print(f"Project: {PROJECT_ID}")
    print("─" * 50)

    # ── Optional clear ─────────────────────────────────────────────────────────
    if clear:
        print("\n[1/4] Clearing previous seed data…")
        _clear_seed(db, tenant_ref)
        print("      Done.")

    # ── Create a fake import job ───────────────────────────────────────────────
    print("\n[2/4] Creating seed import job…")
    job_id = "seed-job-" + str(uuid.uuid4())[:8]
    now    = datetime.now(timezone.utc)

    job_doc = {
        "job_id":            job_id,
        "tenant_id":         tenant_id,
        "channel":           "temu",
        "channel_account_id":"d05902d0-7c75-4741-a105-164fa20e417e",
        "account_name":      "Temu Toys (SEED)",
        "job_type":          "auto_connect",
        "status":            "completed",
        "pending_review":    True,
        "match_status":      "review_required",
        "match_result_count": len(SEED_PRODUCTS),
        "successful_items":  len(SEED_PRODUCTS),
        "processed_items":   len(SEED_PRODUCTS),
        "total_items":       len(SEED_PRODUCTS),
        "failed_items":      0,
        "skipped_items":     0,
        "updated_items":     0,
        "enrich_data":       True,
        "fulfillment_filter":"all",
        "stock_filter":      "all",
        "status_message":    f"Seeded {len(SEED_PRODUCTS)} test products",
        "created_at":        now,
        "started_at":        now,
        "updated_at":        now,
        "completed_at":      now,
        "_seed_marker":      True,   # used by --clear to find seeded docs
    }
    tenant_ref.collection("import_jobs").document(job_id).set(job_doc)
    print(f"      Job ID: {job_id}")

    # ── Find a real existing product to use as matched target ──────────────────
    print("\n[3/4] Finding existing product for exact match targets…")
    existing_product_id    = ""
    existing_product_title = "(existing product)"
    existing_product_sku   = "existing-sku"

    prod_iter = tenant_ref.collection("products").where("status", "==", "active").limit(1).stream()
    for doc in prod_iter:
        d = doc.to_dict()
        existing_product_id    = doc.id
        existing_product_title = d.get("title") or d.get("name") or "(existing product)"
        existing_product_sku   = d.get("sku") or "existing-sku"
        print(f"      Using: {existing_product_title[:50]} ({existing_product_id})")
        break

    if not existing_product_id:
        print("      ⚠  No active products found — exact match rows will have no target.")

    # ── Create pending_imports + match_results ─────────────────────────────────
    print(f"\n[4/4] Seeding {len(SEED_PRODUCTS)} products…")

    match_results_ref = tenant_ref.collection("import_jobs").document(job_id).collection("match_results")

    for title, sku, external_id, match_type, match_score, match_reason in SEED_PRODUCTS:
        pending_id = str(uuid.uuid4())

        # pending_imports document
        pending_doc = {
            "product_id":        pending_id,
            "tenant_id":         tenant_id,
            "channel":           "temu",
            "channel_account_id":"d05902d0-7c75-4741-a105-164fa20e417e",
            "import_job_id":     job_id,
            "external_id":       external_id,
            "title":             title,
            "sku":               sku,
            "status":            "active",
            "product_type":      "simple",
            "description":       f"Test product seeded for Review Mappings UI testing. SKU: {sku}",
            "brand":             "",
            "enrichment_status": "complete",
            "attributes":        {},
            "end_of_life":       False,
            "use_serial_numbers":False,
            "created_at":        now,
            "updated_at":        now,
            "_seed_marker":      True,
        }
        tenant_ref.collection("pending_imports").document(pending_id).set(pending_doc)

        # import_mapping
        mapping_id = str(uuid.uuid4())
        mapping_doc = {
            "mapping_id":         mapping_id,
            "tenant_id":          tenant_id,
            "channel":            "temu",
            "channel_account_id": "d05902d0-7c75-4741-a105-164fa20e417e",
            "external_id":        external_id,
            "product_id":         pending_id,
            "source_collection":  "pending_imports",
            "sync_enabled":       True,
            "created_at":         now,
            "updated_at":         now,
            "_seed_marker":       True,
        }
        tenant_ref.collection("import_mappings").document(mapping_id).set(mapping_doc)

        # match_results row
        row_id = str(uuid.uuid4())
        row = {
            "row_id":                 row_id,
            "job_id":                 job_id,
            "tenant_id":              tenant_id,
            "channel":                "temu",
            "external_id":            external_id,
            "sku":                    sku,
            "title":                  title,
            "image_url":              FAKE_IMAGE,
            "match_type":             match_type,
            "match_score":            match_score,
            "match_reason":           match_reason,
            "matched_product_id":     existing_product_id   if match_type in ("exact","fuzzy") else "",
            "matched_product_title":  existing_product_title if match_type in ("exact","fuzzy") else "",
            "matched_product_sku":    existing_product_sku   if match_type in ("exact","fuzzy") else "",
            "matched_product_image":  "",
            "matched_product_asin":   "",
            "decision":               "",
            "created_at":             now,
            "updated_at":             now,
            "_seed_marker":           True,
        }
        match_results_ref.document(row_id).set(row)

        icon = "🎯" if match_type=="exact" else "🔍" if match_type=="fuzzy" else "❓"
        print(f"      {icon} [{match_type:8s}] {title[:45]}")

    print("\n✓ Seed complete!")
    print(f"\n  Job ID:   {job_id}")
    print(f"  Products: {len(SEED_PRODUCTS)}")
    print(f"            2 exact matches")
    print(f"            2 fuzzy matches")
    print(f"            4 unmatched (import as new)")
    print("\n  Open Review Mappings in the app — it should show these products immediately.")
    print("  Run with --clear to remove all seeded data before re-seeding.\n")


def _clear_seed(db, tenant_ref):
    """Delete all documents that have _seed_marker=True in relevant collections."""
    collections = [
        tenant_ref.collection("pending_imports"),
        tenant_ref.collection("import_mappings"),
    ]
    for col in collections:
        for doc in col.where("_seed_marker", "==", True).stream():
            doc.reference.delete()

    # Delete seeded import jobs + their match_results subcollections
    for job_doc in tenant_ref.collection("import_jobs").where("_seed_marker", "==", True).stream():
        mr_ref = job_doc.reference.collection("match_results")
        for mr in mr_ref.stream():
            mr.reference.delete()
        job_doc.reference.delete()


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description="Seed Review Mappings test data")
    parser.add_argument("--tenant", default="tenant-10013", help="Tenant ID (default: tenant-10013)")
    parser.add_argument("--clear",  action="store_true",    help="Clear previously seeded data before seeding")
    args = parser.parse_args()

    try:
        from google.cloud import firestore as _fs  # noqa
    except ImportError:
        print("Installing google-cloud-firestore…")
        import subprocess
        subprocess.check_call([sys.executable, "-m", "pip", "install", "google-cloud-firestore"])

    main(args.tenant, args.clear)
