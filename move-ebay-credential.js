'use strict';

const fs    = require('fs');
const path  = require('path');
const admin = require('firebase-admin');

// Load .env
function loadEnv() {
  const candidates = [
    path.join(__dirname, '.env'),
    path.join(__dirname, 'backend', '.env'),
    path.join(__dirname, 'platform', 'backend', '.env'),
  ];
  const envPath = candidates.find(p => fs.existsSync(p));
  if (envPath) {
    const lines = fs.readFileSync(envPath, 'utf8').split('\n');
    for (const line of lines) {
      const trimmed = line.trim();
      if (!trimmed || trimmed.startsWith('#')) continue;
      const eq = trimmed.indexOf('=');
      if (eq === -1) continue;
      const key = trimmed.slice(0, eq).trim();
      const val = trimmed.slice(eq + 1).trim().replace(/^["']|["']$/g, '');
      if (key && !(key in process.env)) process.env[key] = val;
    }
    console.log(`Loaded env from: ${envPath}`);
  }
}
loadEnv();

const serviceAccount = require('./serviceAccountKey.json');
admin.initializeApp({ credential: admin.credential.cert(serviceAccount) });
const db = admin.firestore();

const SOURCE_TENANT = 'tenant-collins-fceeeb';
const SOURCE_CRED   = 'cred-ebay-1771005994952';
const DEST_TENANT   = 'tenant-demo';

async function main() {
  console.log('╔════════════════════════════════════════╗');
  console.log('║   eBay Credential Move — One-Off Tool  ║');
  console.log('╚════════════════════════════════════════╝\n');
  console.log(`Source: tenants/${SOURCE_TENANT}/marketplace_credentials/${SOURCE_CRED}`);
  console.log(`Dest:   tenants/${DEST_TENANT}/marketplace_credentials/${SOURCE_CRED}\n`);

  // 1. Read the source document
  const sourceRef = db
    .collection('tenants').doc(SOURCE_TENANT)
    .collection('marketplace_credentials').doc(SOURCE_CRED);

  const sourceSnap = await sourceRef.get();

  if (!sourceSnap.exists) {
    console.error(`✗ Source document not found: ${SOURCE_TENANT}/${SOURCE_CRED}`);
    process.exit(1);
  }

  const data = sourceSnap.data();
  console.log('✓ Source document found');
  console.log(`  account_name:     ${data.account_name || '(none)'}`);
  console.log(`  channel:          ${data.channel || '(none)'}`);
  console.log(`  active:           ${data.active}`);
  console.log(`  tenant_id (old):  ${data.tenant_id || '(none)'}`);
  console.log(`  Fields:           ${Object.keys(data).join(', ')}\n`);

  // 2. Update tenant_id in the data to point to the new tenant
  const updatedData = {
    ...data,
    tenant_id:  DEST_TENANT,
    moved_from: SOURCE_TENANT,
    moved_at:   new Date(),
  };

  // 3. Write to destination
  const destRef = db
    .collection('tenants').doc(DEST_TENANT)
    .collection('marketplace_credentials').doc(SOURCE_CRED);

  // Check destination doesn't already exist
  const destSnap = await destRef.get();
  if (destSnap.exists) {
    console.warn(`⚠ Destination document already exists in ${DEST_TENANT}.`);
    console.warn('  Overwriting with source data...\n');
  }

  await destRef.set(updatedData);
  console.log(`✓ Written to tenants/${DEST_TENANT}/marketplace_credentials/${SOURCE_CRED}`);

  // 4. Delete the source
  await sourceRef.delete();
  console.log(`✓ Deleted source: tenants/${SOURCE_TENANT}/marketplace_credentials/${SOURCE_CRED}`);

  // 5. Verify
  const verify = await destRef.get();
  if (verify.exists) {
    console.log('\n✅ Move complete and verified.');
    console.log(`   tenant_id is now: ${verify.data().tenant_id}`);
    console.log(`   channel:          ${verify.data().channel}`);
    console.log(`   account_name:     ${verify.data().account_name}`);
  } else {
    console.error('\n✗ Verification failed — destination document not found after write!');
    process.exit(1);
  }

  process.exit(0);
}

main().catch(err => {
  console.error(`\n✗ Fatal: ${err.message}`);
  process.exit(1);
});
