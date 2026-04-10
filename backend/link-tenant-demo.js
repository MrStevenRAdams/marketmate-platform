#!/usr/bin/env node
/**
 * link-tenant-demo.js
 *
 * Creates a Firebase Auth account and links it to the existing tenant-demo
 * tenant in Firestore (global_users + user_memberships documents).
 *
 * Usage:
 *   node link-tenant-demo.js --email you@example.com --password YourPassword123
 *
 * Requirements:
 *   npm install node-fetch@2 google-auth-library uuid
 *   (run from the platform/backend directory so it can find serviceAccountKey.json)
 */

const fs   = require('fs');
const path = require('path');

// ── Parse CLI args ────────────────────────────────────────────────────────────
const args = process.argv.slice(2);
function getArg(name) {
  const idx = args.indexOf(`--${name}`);
  return idx !== -1 ? args[idx + 1] : null;
}

const email       = getArg('email');
const password    = getArg('password');
const displayName = getArg('name') || 'Demo Admin';
const tenantId    = getArg('tenant') || 'tenant-demo';

if (!email || !password) {
  console.error('Usage: node link-tenant-demo.js --email you@example.com --password YourPass123 [--name "Your Name"] [--tenant tenant-demo]');
  process.exit(1);
}

// ── Config ────────────────────────────────────────────────────────────────────
const FIREBASE_WEB_API_KEY = process.env.FIREBASE_WEB_API_KEY || require('fs').existsSync('.env') && require('fs').readFileSync('.env','utf8').match(/FIREBASE_WEB_API_KEY=([^\r\n]+)/)?.[1]?.trim() || '';
if (!FIREBASE_WEB_API_KEY) { console.error('❌  FIREBASE_WEB_API_KEY not set'); process.exit(1); }
const PROJECT_ID           = 'marketmate-486116';
const SERVICE_ACCOUNT_PATH = path.resolve(__dirname, 'serviceAccountKey.json');

// ── Load dependencies (installed via npm) ─────────────────────────────────────
let fetch, GoogleAuth, uuidv4;
try {
  fetch      = require('node-fetch');
  const gal  = require('google-auth-library');
  GoogleAuth = gal.GoogleAuth;
  uuidv4     = require('uuid').v4;
} catch (e) {
  console.error('\n❌  Missing dependencies. Run:\n');
  console.error('   npm install node-fetch@2 google-auth-library uuid\n');
  process.exit(1);
}

// ── Step 1: Create Firebase Auth user via REST API ────────────────────────────
async function createFirebaseUser(email, password, displayName) {
  console.log(`\n🔐  Creating Firebase Auth user: ${email}`);
  const url = `https://identitytoolkit.googleapis.com/v1/accounts:signUp?key=${FIREBASE_WEB_API_KEY}`;
  const res = await fetch(url, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ email, password, displayName, returnSecureToken: true }),
  });
  const data = await res.json();
  if (!res.ok) {
    const msg = data?.error?.message || JSON.stringify(data);
    // If user already exists, sign in instead to get their UID
    if (msg === 'EMAIL_EXISTS') {
      console.log('   ℹ️  Email already exists in Firebase Auth — signing in to get UID…');
      return signInFirebaseUser(email, password);
    }
    throw new Error(`Firebase signUp failed: ${msg}`);
  }
  console.log(`   ✅  Firebase UID: ${data.localId}`);
  return data.localId;
}

async function signInFirebaseUser(email, password) {
  const url = `https://identitytoolkit.googleapis.com/v1/accounts:signInWithPassword?key=${FIREBASE_WEB_API_KEY}`;
  const res = await fetch(url, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ email, password, returnSecureToken: true }),
  });
  const data = await res.json();
  if (!res.ok) throw new Error(`Firebase signIn failed: ${data?.error?.message}`);
  console.log(`   ✅  Signed in — Firebase UID: ${data.localId}`);
  return data.localId;
}

// ── Step 2: Get Firestore access token via service account ────────────────────
async function getAccessToken() {
  const auth = new GoogleAuth({
    keyFile: SERVICE_ACCOUNT_PATH,
    scopes: ['https://www.googleapis.com/auth/datastore'],
  });
  const client = await auth.getClient();
  const token  = await client.getAccessToken();
  return token.token;
}

// ── Step 3: Write Firestore documents ─────────────────────────────────────────
const FIRESTORE_BASE = `https://firestore.googleapis.com/v1/projects/${PROJECT_ID}/databases/(default)/documents`;

async function firestoreGet(token, collection, docId) {
  const url = `${FIRESTORE_BASE}/${collection}/${docId}`;
  const res = await fetch(url, { headers: { Authorization: `Bearer ${token}` } });
  return res.ok ? res.json() : null;
}

async function firestorePatch(token, collection, docId, fields) {
  const url     = `${FIRESTORE_BASE}/${collection}/${docId}`;
  const body    = { fields };
  const res     = await fetch(url, {
    method: 'PATCH',
    headers: { Authorization: `Bearer ${token}`, 'Content-Type': 'application/json' },
    body: JSON.stringify(body),
  });
  if (!res.ok) {
    const err = await res.text();
    throw new Error(`Firestore PATCH ${collection}/${docId} failed: ${err}`);
  }
  return res.json();
}

// Convert a plain JS value to a Firestore Value object
function fsVal(v) {
  if (v === null || v === undefined) return { nullValue: null };
  if (typeof v === 'string')  return { stringValue: v };
  if (typeof v === 'boolean') return { booleanValue: v };
  if (typeof v === 'number')  return Number.isInteger(v)
    ? { integerValue: String(v) }
    : { doubleValue: v };
  if (v instanceof Date)      return { timestampValue: v.toISOString() };
  if (Array.isArray(v))       return { arrayValue: { values: v.map(fsVal) } };
  if (typeof v === 'object')  return { mapValue: { fields: Object.fromEntries(Object.entries(v).map(([k,vv]) => [k, fsVal(vv)])) } };
  return { stringValue: String(v) };
}

function fsFields(obj) {
  return Object.fromEntries(Object.entries(obj).map(([k, v]) => [k, fsVal(v)]));
}

// ── Main ──────────────────────────────────────────────────────────────────────
(async () => {
  try {
    // 1. Create / retrieve Firebase Auth user
    const firebaseUID = await createFirebaseUser(email, password, displayName);

    // 2. Get Firestore access token
    console.log('\n🔑  Authenticating with Firestore…');
    const token = await getAccessToken();
    console.log('   ✅  Service account authenticated');

    // 3. Check if the tenant-demo tenant exists
    console.log(`\n🏢  Checking tenant "${tenantId}" exists…`);
    const tenantDoc = await firestoreGet(token, 'tenants', tenantId);
    if (!tenantDoc) {
      console.error(`   ❌  Tenant "${tenantId}" not found in Firestore.`);
      console.error('   Make sure the tenant-demo document exists in the tenants collection.');
      process.exit(1);
    }
    const tenantName = tenantDoc.fields?.name?.stringValue || tenantId;
    console.log(`   ✅  Found tenant: "${tenantName}"`);

    // 4. Check if a global_user already exists for this Firebase UID
    console.log('\n👤  Checking for existing global_user…');
    const usersUrl = `${FIRESTORE_BASE}/global_users?pageSize=500`;
    const usersRes = await fetch(usersUrl, { headers: { Authorization: `Bearer ${token}` } });
    const usersData = await usersRes.json();
    const docs = usersData.documents || [];

    let userId = null;
    for (const doc of docs) {
      if (doc.fields?.firebase_uid?.stringValue === firebaseUID) {
        userId = doc.fields?.user_id?.stringValue || doc.name.split('/').pop();
        console.log(`   ℹ️  Found existing global_user: ${userId}`);
        break;
      }
    }

    const now = new Date();

    if (!userId) {
      // 5a. Create global_user
      userId = 'usr_' + uuidv4();
      console.log(`\n📝  Creating global_user: ${userId}`);
      await firestorePatch(token, 'global_users', userId, fsFields({
        user_id:      userId,
        firebase_uid: firebaseUID,
        email:        email.toLowerCase(),
        display_name: displayName,
        created_at:   now,
        last_login_at: now,
      }));
      console.log('   ✅  global_user created');
    }

    // 6. Check for existing membership
    console.log(`\n🔗  Checking for existing membership (user=${userId}, tenant=${tenantId})…`);
    const memUrl = `${FIRESTORE_BASE}/user_memberships?pageSize=500`;
    const memRes = await fetch(memUrl, { headers: { Authorization: `Bearer ${token}` } });
    const memData = await memRes.json();
    const memDocs = memData.documents || [];

    let existingMembership = null;
    for (const doc of memDocs) {
      const f = doc.fields || {};
      if (f.user_id?.stringValue === userId && f.tenant_id?.stringValue === tenantId) {
        existingMembership = doc.name.split('/').pop();
        break;
      }
    }

    if (existingMembership) {
      console.log(`   ℹ️  Membership already exists (${existingMembership}) — ensuring it is active…`);
      await firestorePatch(token, 'user_memberships', existingMembership, fsFields({
        status:     'active',
        updated_at: now,
      }));
      console.log('   ✅  Membership confirmed active');
    } else {
      // 7. Create membership
      const membershipId = 'mem_' + uuidv4();
      console.log(`\n🔗  Creating membership: ${membershipId}`);
      await firestorePatch(token, 'user_memberships', membershipId, fsFields({
        membership_id: membershipId,
        user_id:       userId,
        tenant_id:     tenantId,
        role:          'owner',
        status:        'active',
        joined_at:     now,
        created_at:    now,
        updated_at:    now,
      }));
      console.log('   ✅  Membership created');
    }

    // Done!
    console.log('\n' + '═'.repeat(60));
    console.log('✅  DONE — account linked successfully!');
    console.log('═'.repeat(60));
    console.log(`   Email:      ${email}`);
    console.log(`   Firebase:   ${firebaseUID}`);
    console.log(`   User ID:    ${userId}`);
    console.log(`   Tenant:     ${tenantId}  ("${tenantName}")`);
    console.log(`   Role:       owner`);
    console.log('\n   You can now log in at https://marketmate-486116.web.app');
    console.log('═'.repeat(60) + '\n');

  } catch (err) {
    console.error('\n❌  Script failed:', err.message);
    process.exit(1);
  }
})();
