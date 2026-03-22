/**
 * migrate-credentials.js
 *
 * Copies marketplace_credentials from ALL other tenants into tenant-demo.
 * Safe to run multiple times — it will not overwrite a credential in tenant-demo
 * if one with the same ID already exists there.
 *
 * Usage (from your platform folder):
 *   node migrate-credentials.js
 */

const admin = require('firebase-admin');
const serviceAccount = require('./serviceAccountKey.json');

admin.initializeApp({
  credential: admin.credential.cert(serviceAccount),
});

const db = admin.firestore();
const TARGET_TENANT = 'tenant-demo';

async function migrate() {
  console.log('=== MarketMate Credential Migration ===');
  console.log(`Target tenant: ${TARGET_TENANT}\n`);

  // 1. Get all tenants
  const tenantsSnap = await db.collection('tenants').get();
  const allTenants = tenantsSnap.docs.map(d => d.id);
  console.log(`Found ${allTenants.length} tenants: ${allTenants.join(', ')}\n`);

  const sourceTenants = allTenants.filter(id => id !== TARGET_TENANT);
  console.log(`Source tenants (${sourceTenants.length}): ${sourceTenants.join(', ')}\n`);

  // 2. Get existing credentials in tenant-demo so we don't overwrite them
  const existingSnap = await db
    .collection('tenants').doc(TARGET_TENANT)
    .collection('marketplace_credentials')
    .get();

  const existingIds = new Set(existingSnap.docs.map(d => d.id));
  console.log(`tenant-demo already has ${existingIds.size} credential(s): ${[...existingIds].join(', ') || 'none'}\n`);

  let totalCopied = 0;
  let totalSkipped = 0;

  // 3. Loop through each source tenant
  for (const tenantId of sourceTenants) {
    const credsSnap = await db
      .collection('tenants').doc(tenantId)
      .collection('marketplace_credentials')
      .get();

    if (credsSnap.empty) {
      console.log(`  [${tenantId}] No credentials found — skipping`);
      continue;
    }

    console.log(`  [${tenantId}] Found ${credsSnap.docs.length} credential(s)`);

    for (const credDoc of credsSnap.docs) {
      const credId = credDoc.id;
      const data = credDoc.data();

      if (existingIds.has(credId)) {
        console.log(`    ↷ SKIP  ${credId} (${data.channel || '?'} / ${data.account_name || '?'}) — already exists in tenant-demo`);
        totalSkipped++;
        continue;
      }

      // Update the tenant_id field to point to tenant-demo
      const updatedData = {
        ...data,
        tenant_id: TARGET_TENANT,
      };

      await db
        .collection('tenants').doc(TARGET_TENANT)
        .collection('marketplace_credentials').doc(credId)
        .set(updatedData);

      existingIds.add(credId); // track so duplicates across source tenants are also skipped
      console.log(`    ✓ COPY  ${credId} (${data.channel || '?'} / ${data.account_name || '?'}) from ${tenantId}`);
      totalCopied++;
    }
  }

  console.log('\n=== Migration Complete ===');
  console.log(`  Copied:  ${totalCopied}`);
  console.log(`  Skipped: ${totalSkipped}`);
  console.log(`  Total in tenant-demo: ${existingIds.size}`);
  console.log('\nDone. Your credentials are now all under tenant-demo.');
}

migrate().catch(err => {
  console.error('Migration failed:', err);
  process.exit(1);
});
