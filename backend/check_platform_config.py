#!/usr/bin/env python3
import firebase_admin
from firebase_admin import firestore

if not firebase_admin._apps:
    firebase_admin.initialize_app(options={"projectId": "marketmate-486116"})
db = firestore.client()

for channel in ["amazon", "amazonnew"]:
    doc = db.collection("platform_config").document(channel).get()
    if doc.exists:
        d = doc.to_dict()
        keys = d.get("keys", {}) or {}
        print(f"\nplatform_config/{channel}:")
        print(f"  keys fields ({len(keys)}): {sorted(keys.keys())}")
        for k, v in sorted(keys.items()):
            print(f"    {k}: {'[SET - ' + str(len(str(v))) + ' chars]' if v else '[EMPTY]'}")
    else:
        print(f"\nplatform_config/{channel}: NOT FOUND")
