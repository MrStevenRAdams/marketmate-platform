#!/usr/bin/env python3
import sys
import firebase_admin
from firebase_admin import firestore

PROJECT_ID = "marketmate-486116"
JOB_ID     = "5bc3f187-5b98-4c17-a3ca-502557d0cd18"
TENANT_ID  = "tenant-10013"

if not firebase_admin._apps:
    firebase_admin.initialize_app(options={"projectId": PROJECT_ID})
db = firestore.client()

# Check import_jobs collection
doc = db.collection("tenants").document(TENANT_ID)\
        .collection("import_jobs").document(JOB_ID).get()

if doc.exists:
    d = doc.to_dict()
    print(f"✅ Found in import_jobs")
    for k in ["status","status_message","channel","channel_account_id","account_name",
              "job_type","total_items","processed_items","failed_items",
              "started_at","completed_at","updated_at","report_id","error_log"]:
        v = d.get(k)
        if v is not None and v != [] and v != {}:
            print(f"  {k}: {v}")
else:
    print(f"❌ Not found in tenant-10013/import_jobs/{JOB_ID}")
    # Try other tenants
    for tenant in ["tenant-10007","tenant-demo"]:
        doc2 = db.collection("tenants").document(tenant)\
                 .collection("import_jobs").document(JOB_ID).get()
        if doc2.exists:
            print(f"  Found in {tenant} instead!")
            d = doc2.to_dict()
            print(f"  status: {d.get('status')}")
            print(f"  status_message: {d.get('status_message')}")
