/**
 * activate-credentials.js
 *
 * Sets active: true on all marketplace_credentials in tenant-demo
 * so they show up in the Connections page.
 *
 * Usage:
 *   node activate-credentials.js
 */

const admin = require('firebase-admin');
const serviceAccount = require('./serviceAccountKey.json');

admin.initializeApp({
  credential: admin.credential.cert(serviceAccount),
});

const db = admin.firestore();
const TARGET_TENANT = 'tenant-demo';

async function activate() {
  console.log('=== Activate All Credentials in tenant-demo ===\n');

  const credsSnap = await db
    .collection('tenants').doc(TARGET_TENANT)
    .collection('marketplace_credentials')
    .get();

  if (credsSnap.empty) {
    console.log('No credentials found in tenant-demo.');
    return;
  }

  console.log(`Found ${credsSnap.docs.length} credential(s):\n`);

  let activated = 0;
  let alreadyActive = 0;

  for (const doc of credsSnap.docs) {
    const data = doc.data();
    const label = `${data.channel || '?'} / ${data.account_name || doc.id}`;

    if (data.active === true) {
      console.log(`  ✓ already active  — ${label}`);
      alreadyActive++;
    } else {
      await doc.ref.update({ active: true });
      console.log(`  ⚡ activated       — ${label}`);
      activated++;
    }
  }

  console.log(`\n=== Done ===`);
  console.log(`  Activated:      ${activated}`);
  console.log(`  Already active: ${alreadyActive}`);
  console.log(`  Total:          ${credsSnap.docs.length}`);
}

activate().catch(err => {
  console.error('Failed:', err);
  process.exit(1);
});
