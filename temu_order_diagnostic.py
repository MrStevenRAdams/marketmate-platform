#!/usr/bin/env python3
"""
temu_order_diagnostic.py — Diagnostic tool for Temu order sync issues.

Calls the Temu API with multiple status/date/region combinations
and logs everything for Temu support evidence.

Usage:
    python temu_order_diagnostic.py --tenant-id tenant-10013 --cred-id <cred_id>

    # Or test all Temu credentials for a tenant automatically:
    python temu_order_diagnostic.py --tenant-id tenant-10013 --all-creds
"""

import argparse
import hashlib
import hmac
import json
import os
import subprocess
import sys
import time
import urllib.request
import urllib.error
from datetime import datetime, timedelta

# ── CONFIG ────────────────────────────────────────────────────────────────────
PROJECT = "marketmate-486116"

# Temu order status codes
STATUS_CODES = {
    0:  "ALL (no filter)",
    1:  "Pending payment",
    2:  "To be shipped",
    3:  "Shipped",
    4:  "Delivered",
    5:  "Cancelled",
    6:  "Return/Refund in progress",
    7:  "Completed",
    8:  "Closed",
}

# ── ARGS ──────────────────────────────────────────────────────────────────────
parser = argparse.ArgumentParser(description="Temu order diagnostic tool")
parser.add_argument("--tenant-id", required=True)
parser.add_argument("--cred-id",   default="")
parser.add_argument("--all-creds", action="store_true", help="Test all Temu credentials for tenant")
parser.add_argument("--days-back", type=int, default=30, help="How many days back to search (default 30)")
args = parser.parse_args()

# ── HELPERS ───────────────────────────────────────────────────────────────────

def gcloud(cmd, timeout=30):
    r = subprocess.run(cmd, shell=True, capture_output=True, text=True, timeout=timeout)
    return r.returncode, r.stdout.strip(), r.stderr.strip()

def get_token():
    rc, token, err = gcloud("gcloud auth print-access-token")
    if rc != 0:
        print(f"ERROR: {err}")
        sys.exit(1)
    return token

def firestore_get(path, token):
    url = f"https://firestore.googleapis.com/v1/projects/{PROJECT}/databases/(default)/documents/{path}"
    req = urllib.request.Request(url)
    req.add_header("Authorization", f"Bearer {token}")
    try:
        with urllib.request.urlopen(req, timeout=15) as r:
            return json.loads(r.read().decode())
    except urllib.error.HTTPError as e:
        print(f"  Firestore error {e.code}: {e.read().decode()[:100]}")
        return None

def extract_field(fields, key):
    v = fields.get(key, {})
    for t in ("stringValue", "integerValue", "booleanValue"):
        if t in v:
            return v[t]
    return ""

def temu_sign(params, app_secret):
    """Sign Temu API request."""
    sorted_params = sorted(params.items())
    sign_str = app_secret
    for k, v in sorted_params:
        if k not in ("sign", "access_token"):
            sign_str += str(k) + str(v)
    sign_str += app_secret
    return hashlib.md5(sign_str.encode()).hexdigest().upper()

def temu_call(base_url, app_key, app_secret, access_token, params):
    """Make a Temu API call via our proxy."""
    params["app_key"] = app_key
    params["access_token"] = access_token
    params["data_type"] = "JSON"
    params["timestamp"] = int(time.time())
    params["sign"] = temu_sign(params, app_secret)

    body = json.dumps(params)

    # Call via our proxy
    proxy_url = "https://marketmate-proxy-eu-487246736287.europe-west2.run.app/forward"
    proxy_body = json.dumps({
        "url": base_url,
        "method": "POST",
        "headers": {"Content-Type": "application/json"},
        "body": body
    })

    req = urllib.request.Request(proxy_url, data=proxy_body.encode(), method="POST")
    req.add_header("Content-Type", "application/json")
    req.add_header("X-Proxy-Secret", "mm-proxy-secret-2024")

    try:
        with urllib.request.urlopen(req, timeout=30) as r:
            return json.loads(r.read().decode())
    except urllib.error.HTTPError as e:
        err = e.read().decode()
        print(f"  HTTP {e.code}: {err[:200]}")
        return None
    except Exception as ex:
        print(f"  Error: {ex}")
        return None

def decrypt_credential(cred_id, tenant_id, token):
    """Get decrypted credentials via Firestore (we read raw and hope they're not encrypted)."""
    doc = firestore_get(f"tenants/{tenant_id}/marketplace_credentials/{cred_id}", token)
    if not doc:
        return None
    fields = doc.get("fields", {})
    cred_data = fields.get("credential_data", {}).get("mapValue", {}).get("fields", {})

    result = {}
    for k, v in cred_data.items():
        result[k] = extract_field(cred_data, k) or v.get("stringValue", "")

    # Also get global platform_config keys
    config = firestore_get("platform_config/temu", token)
    if config:
        cf = config.get("fields", {})
        keys = cf.get("keys", {}).get("mapValue", {}).get("fields", {})
        for k, v in keys.items():
            if k not in result:
                result[k] = extract_field(keys, k)

    return result

def get_temu_creds(tenant_id, token):
    """Get all Temu credentials for a tenant."""
    url = (f"https://firestore.googleapis.com/v1/projects/{PROJECT}"
           f"/databases/(default)/documents/tenants/{tenant_id}/marketplace_credentials"
           f"?pageSize=50&mask.fieldPaths=channel&mask.fieldPaths=active&mask.fieldPaths=credential_name")
    req = urllib.request.Request(url)
    req.add_header("Authorization", f"Bearer {token}")
    try:
        with urllib.request.urlopen(req, timeout=15) as r:
            data = json.loads(r.read().decode())
    except Exception as e:
        print(f"  Error listing creds: {e}")
        return []

    result = []
    for doc in data.get("documents", []):
        f = doc.get("fields", {})
        if f.get("channel", {}).get("stringValue", "") == "temu":
            cred_id = doc.get("name", "").split("/")[-1]
            name = f.get("credential_name", {}).get("stringValue", "unnamed")
            active = f.get("active", {}).get("booleanValue", False)
            result.append({"id": cred_id, "name": name, "active": active})
    return result

# ── MAIN ──────────────────────────────────────────────────────────────────────

print(f"\n{'='*70}")
print(f"  TEMU ORDER DIAGNOSTIC")
print(f"  Tenant: {args.tenant_id}")
print(f"  Date:   {datetime.now().strftime('%Y-%m-%d %H:%M:%S')}")
print(f"{'='*70}\n")

token = get_token()

# Resolve credentials
if args.all_creds:
    creds_list = get_temu_creds(args.tenant_id, token)
    print(f"Found {len(creds_list)} Temu credentials:")
    for c in creds_list:
        print(f"  {'✅' if c['active'] else '❌'} {c['id']} — {c['name']}")
    cred_ids = [c["id"] for c in creds_list]
elif args.cred_id:
    cred_ids = [args.cred_id]
else:
    print("ERROR: Specify --cred-id or --all-creds")
    sys.exit(1)

# Date ranges to test
now = datetime.utcnow()
date_ranges = [
    ("Last 7 days",  int((now - timedelta(days=7)).timestamp()),  int(now.timestamp())),
    ("Last 30 days", int((now - timedelta(days=30)).timestamp()), int(now.timestamp())),
    ("Last 90 days", int((now - timedelta(days=90)).timestamp()), int(now.timestamp())),
    ("Last 180 days",int((now - timedelta(days=180)).timestamp()),int(now.timestamp())),
]

for cred_id in cred_ids:
    print(f"\n{'─'*70}")
    print(f"  Testing credential: {cred_id}")
    print(f"{'─'*70}")

    creds = decrypt_credential(cred_id, args.tenant_id, token)
    if not creds:
        print("  ❌ Could not load credentials")
        continue

    app_key     = creds.get("app_key", "")
    app_secret  = creds.get("app_secret", "")
    access_token = creds.get("access_token", "")
    base_url    = creds.get("base_url", "https://openapi-b-eu.temu.com/openapi/router")

    # Try to get mall_id from Firestore
    mall_id = None
    cred_doc = firestore_get(f"tenants/{args.tenant_id}/marketplace_credentials/{cred_id}", token)
    if cred_doc:
        cf = cred_doc.get("fields", {})
        mall_id = cf.get("mall_id", {}).get("stringValue", "")

    print(f"  App key:    {app_key}")
    print(f"  Base URL:   {base_url}")
    print(f"  Has secret: {'Yes' if app_secret else 'No'}")
    print(f"  Has token:  {'Yes' if access_token else 'No'}")

    if not app_key or not app_secret or not access_token:
        print("  ❌ Missing credentials — skipping")
        continue

    # Test 1: All statuses, last 30 days, no region filter
    print(f"\n  TEST 1: All orders (no status filter), last 30 days, no region")
    result = temu_call(base_url, app_key, app_secret, access_token, {
        "type": "bg.order.list.v2.get",
        "pageNumber": 1,
        "pageSize": 50,
        "createAfter": int((now - timedelta(days=30)).timestamp()),
        "createBefore": int(now.timestamp()),
    })
    if result:
        total = result.get("result", {}).get("totalItemNum", "N/A")
        items = result.get("result", {}).get("pageItems", [])
        error = result.get("errorMsg", "") or result.get("error_msg", "")
        req_id = result.get("requestId", "") or result.get("request_id", "")
        print(f"  Result: success={result.get('success', result.get('data', {}).get('success', '?'))} total={total} items={len(items)} error='{error}' requestId={req_id}")
        print(f"  Raw response (first 500 chars): {json.dumps(result)[:500]}")

    # Test 2: Status 2 (to be shipped), last 30 days, with region 1
    print(f"\n  TEST 2: Status=2 (to be shipped), last 30 days, regionId=1")
    result = temu_call(base_url, app_key, app_secret, access_token, {
        "type": "bg.order.list.v2.get",
        "pageNumber": 1,
        "pageSize": 50,
        "createAfter": int((now - timedelta(days=30)).timestamp()),
        "createBefore": int(now.timestamp()),
        "parentOrderStatus": 2,
        "regionId": 1,
    })
    if result:
        total = result.get("result", {}).get("totalItemNum", "N/A")
        items = result.get("result", {}).get("pageItems", [])
        error = result.get("errorMsg", "") or result.get("error_msg", "")
        print(f"  Result: total={total} items={len(items)} error='{error}'")

    # Test 2b: With mallId instead of regionId
    print(f"\n  TEST 2b: Status=2, last 30 days, mallId={mall_id} (from GetMallInfo)")
    if mall_id:
        result = temu_call(base_url, app_key, app_secret, access_token, {
            "type": "bg.order.list.v2.get",
            "pageNumber": 1,
            "pageSize": 50,
            "createAfter": int((now - timedelta(days=30)).timestamp()),
            "createBefore": int(now.timestamp()),
            "parentOrderStatus": 2,
            "mallId": int(mall_id),
        })
        if result:
            res = result.get("result") or {}
            total = res.get("totalItemNum", "N/A") if isinstance(res, dict) else "N/A"
            items = res.get("pageItems", []) if isinstance(res, dict) else []
            error = result.get("errorMsg", "")
            req_id = result.get("requestId", "")
            marker = "⭐" if total and str(total) != "0" and total != "N/A" else "  "
            print(f"  {marker} Result: total={total} items={len(items)} error='{error}' requestId={req_id}")
    else:
        print("  (skipped — no mall_id available, run Test Connection in UI first)")

    # Test 2c: mallId + no status filter
    print(f"\n  TEST 2c: NO status filter, last 30 days, mallId={mall_id}")
    if mall_id:
        result = temu_call(base_url, app_key, app_secret, access_token, {
            "type": "bg.order.list.v2.get",
            "pageNumber": 1,
            "pageSize": 50,
            "createAfter": int((now - timedelta(days=30)).timestamp()),
            "createBefore": int(now.timestamp()),
            "mallId": int(mall_id),
        })
        if result:
            res = result.get("result") or {}
            total = res.get("totalItemNum", "N/A") if isinstance(res, dict) else "N/A"
            error = result.get("errorMsg", "")
            req_id = result.get("requestId", "")
            marker = "⭐" if total and str(total) != "0" and total != "N/A" else "  "
            print(f"  {marker} Result: total={total} error='{error}' requestId={req_id}")

    # Test 3: Each status code, last 90 days
    print(f"\n  TEST 3: Each status code individually, last 90 days, no region")
    for status_code, status_name in STATUS_CODES.items():
        params = {
            "type": "bg.order.list.v2.get",
            "pageNumber": 1,
            "pageSize": 10,
            "createAfter": int((now - timedelta(days=90)).timestamp()),
            "createBefore": int(now.timestamp()),
        }
        if status_code > 0:
            params["parentOrderStatus"] = status_code

        result = temu_call(base_url, app_key, app_secret, access_token, params)
        if result:
            # Try multiple response structures
            res = result.get("result") or result.get("data", {}).get("result") or {}
            total = res.get("totalItemNum", 0) if isinstance(res, dict) else 0
            error = result.get("errorMsg", "") or result.get("error_msg", "")
            marker = "⭐" if total and int(total) > 0 else "  "
            print(f"  {marker} Status {status_code} ({status_name}): total={total} error='{error}'")
        else:
            print(f"     Status {status_code} ({status_name}): NO RESPONSE")
        time.sleep(0.3)  # Rate limit

    # Test 4: Wider date range, no filters at all
    print(f"\n  TEST 4: Absolute minimal request — page 1, size 10, NO other params")
    result = temu_call(base_url, app_key, app_secret, access_token, {
        "type": "bg.order.list.v2.get",
        "pageNumber": 1,
        "pageSize": 10,
    })
    if result:
        res = result.get("result") or {}
        total = res.get("totalItemNum", "N/A") if isinstance(res, dict) else "N/A"
        error = result.get("errorMsg", "") or ""
        print(f"  Result: total={total} error='{error}'")
        print(f"  Full response: {json.dumps(result)[:800]}")

print(f"\n{'='*70}")
print(f"  DIAGNOSTIC COMPLETE")
print(f"  Evidence gathered above — share with Temu support including")
print(f"  the requestId values from responses that return 0 orders.")
print(f"{'='*70}\n")
