const admin = require("firebase-admin");

// Initialize Firebase Admin SDK
// Make sure GOOGLE_APPLICATION_CREDENTIALS env var is set, or pass your service account key:
// admin.initializeApp({ credential: admin.credential.cert(require("./serviceAccountKey.json")) });
admin.initializeApp();

const db = admin.firestore();

const SOURCE_TENANTS = [
  "tenant-demo",
  "tenant-champions-1e63be",
  "tenant-devils-in-the-detail-baa338",
];

const TARGET_TENANT = "tenant-10007";

async function copyMarketplaceCredentials() {
  let totalCopied = 0;

  for (const sourceTenant of SOURCE_TENANTS) {
    const sourcePath = `tenants/${sourceTenant}/marketplace_credentials`;
    const targetPath = `tenants/${TARGET_TENANT}/marketplace_credentials`;

    console.log(`\n--- Reading from: ${sourcePath} ---`);

    const snapshot = await db.collection(sourcePath).get();

    if (snapshot.empty) {
      console.log(`  No documents found in ${sourceTenant}`);
      continue;
    }

    console.log(`  Found ${snapshot.size} document(s)`);

    for (const doc of snapshot.docs) {
      const data = doc.data();
      const targetDocRef = db.collection(targetPath).doc(doc.id);

      // Check if it already exists to avoid accidental overwrites
      const existing = await targetDocRef.get();
      if (existing.exists) {
        console.log(`  SKIP: ${doc.id} already exists in ${TARGET_TENANT}`);
        continue;
      }

      await targetDocRef.set(data);
      console.log(`  COPIED: ${doc.id} (from ${sourceTenant})`);
      totalCopied++;
    }
  }

  console.log(`\n=== Done. ${totalCopied} document(s) copied to ${TARGET_TENANT} ===`);
}

copyMarketplaceCredentials()
  .then(() => process.exit(0))
  .catch((err) => {
    console.error("Error:", err);
    process.exit(1);
  });
