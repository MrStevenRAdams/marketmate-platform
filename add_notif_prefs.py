#!/usr/bin/env python3
"""
add_notif_prefs.py

Finds all user_memberships for a given email address and ensures
messaging_notif_prefs is set with the specified phone number and
all notification channels (email, sms, whatsapp).

Usage:
    python add_notif_prefs.py
"""

import sys
from google.cloud import firestore

PROJECT_ID   = "marketmate-486116"
TARGET_EMAIL = "ian@marketmate.co.uk"   # update if your login email differs
PHONE        = "+447780311499"
CHANNELS     = ["email", "sms", "whatsapp"]

def main():
    db = firestore.Client(project=PROJECT_ID)

    # ── Step 1: Find user in global_users by email ────────────────────────────
    print(f"Looking up user with email: {TARGET_EMAIL}")
    user_docs = db.collection("global_users").where("email", "==", TARGET_EMAIL).limit(5).stream()
    users = list(user_docs)

    if not users:
        print(f"No user found in global_users with email {TARGET_EMAIL}")
        print("Listing all global_users to help identify the correct email...")
        all_users = db.collection("global_users").limit(30).stream()
        for u in all_users:
            d = u.to_dict()
            print(f"  {u.id}  email={d.get('email','?')}  name={d.get('display_name','?')}")
        sys.exit(1)

    user_id   = users[0].id
    user_data = users[0].to_dict()
    print(f"Found user: {user_id}  name={user_data.get('display_name','?')}")

    # ── Step 2: Find all memberships for this user ────────────────────────────
    print(f"\nSearching user_memberships for user_id={user_id}...")
    memberships = db.collection("user_memberships") \
        .where("user_id", "==", user_id) \
        .stream()

    updated = 0
    skipped = 0

    for mem in memberships:
        data          = mem.to_dict()
        tenant_id     = data.get("tenant_id", "?")
        membership_id = mem.id
        status        = data.get("status", "?")
        existing      = data.get("messaging_notif_prefs", {}) or {}

        print(f"\n  Membership: {membership_id}")
        print(f"  Tenant:     {tenant_id}  status={status}")
        print(f"  Existing:   {existing}")

        new_prefs = {
            "email":    user_data.get("email", TARGET_EMAIL),
            "phone":    PHONE,
            "channels": CHANNELS,
        }

        needs_update = (
            existing.get("phone") != PHONE or
            set(existing.get("channels", [])) != set(CHANNELS)
        )

        if needs_update:
            db.collection("user_memberships").document(membership_id).update({
                "messaging_notif_prefs": new_prefs
            })
            print(f"  ✅ Updated → phone={PHONE} channels={CHANNELS}")
            updated += 1
        else:
            print(f"  ✓  Already correct, skipped")
            skipped += 1

    print(f"\n{'='*50}")
    print(f"Done. {updated} membership(s) updated, {skipped} already correct.")
    if updated == 0 and skipped == 0:
        print("WARNING: No memberships found for this user.")
        print("You may not be a member of any tenant yet.")
        print("Use the invitation flow in the UI, or create a membership manually.")

if __name__ == "__main__":
    main()
