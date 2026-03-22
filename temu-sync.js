/**
 * temu-sync.js
 * ============================================================================
 * Standalone Temu category tree walker + template downloader.
 * Runs completely independently of the MarketMate backend.
 * Safe to run while backend is being deployed or restarted.
 *
 * Features:
 *   - Full category tree walk (all ~250,000 leaf categories)
 *   - Downloads attribute templates for each leaf category
 *   - Writes directly to Firestore in the exact same structure the backend reads
 *   - Resumes automatically from where it left off if interrupted
 *   - Skips templates cached within the last 7 days (use --full to override)
 *   - Configurable concurrency to avoid hammering the Temu API
 *   - Tracks progress in Firestore so you can monitor from the app UI
 *
 * Usage:
 *   node temu-sync.js                        — incremental (skip fresh cache)
 *   node temu-sync.js --full                 — force re-download everything
 *   node temu-sync.js --phase=walk           — tree walk only (no templates)
 *   node temu-sync.js --phase=templates      — templates only (tree already done)
 *   node temu-sync.js --concurrency=3        — parallel template downloads (default: 1)
 *
 * Requirements:
 *   npm install firebase-admin node-fetch crypto
 *   Place serviceAccountKey.json in same folder
 *
 * Temu credentials are read from Firestore (same as the backend uses).
 * ============================================================================
 */

const admin = require('firebase-admin');
const crypto = require('crypto');
const https = require('https');
const http = require('http');

// ── Config ──────────────────────────────────────────────────────────────────

const fs = require('fs');
const path = require('path');

// Load .env file from same directory as this script
function loadEnv() {
  const candidates = [
    path.join(__dirname, ".env"),
    path.join(__dirname, "backend", ".env"),
    path.join(__dirname, "platform", "backend", ".env"),
  ];
  const envPath = candidates.find(p => fs.existsSync(p));
  if (!envPath) { console.log("  (no .env file found)"); return; }
  console.log(`  (reading env from: ${envPath})`);
  const lines = fs.readFileSync(envPath, "utf8").split("\n");
  for (const line of lines) {
    const trimmed = line.trim();
    if (!trimmed || trimmed.startsWith("#")) continue;
    const eq = trimmed.indexOf("=");
    if (eq === -1) continue;
    const key = trimmed.slice(0, eq).trim();
    const val = trimmed.slice(eq + 1).trim().replace(/^["']|["']$/g, "");
    if (key && !(key in process.env)) process.env[key] = val;
  }
}
loadEnv();

// ── AES-256-GCM decryption (matches backend services/marketplace_services.go) ─
const crypto2 = require('crypto');

function decryptCredential(ciphertext, keyStr) {
  // Key is trimmed/padded to exactly 32 bytes, same as backend
  let key = Buffer.from(keyStr, 'utf8');
  if (key.length > 32) key = key.slice(0, 32);
  if (key.length < 32) {
    const padded = Buffer.alloc(32);
    key.copy(padded);
    key = padded;
  }
  try {
    const data = Buffer.from(ciphertext, 'base64');
    const nonceSize = 12; // GCM standard nonce size
    const nonce = data.slice(0, nonceSize);
    const encrypted = data.slice(nonceSize);
    const decipher = crypto2.createDecipheriv('aes-256-gcm', key, nonce);
    // Last 16 bytes of encrypted data is the GCM auth tag
    const tag = encrypted.slice(encrypted.length - 16);
    const encryptedData = encrypted.slice(0, encrypted.length - 16);
    decipher.setAuthTag(tag);
    const decrypted = Buffer.concat([decipher.update(encryptedData), decipher.final()]);
    return decrypted.toString('utf8');
  } catch (e) {
    return null; // not encrypted or wrong key — return null so caller uses raw value
  }
}

const TARGET_TENANT   = 'tenant-demo';  // uses the working credential
const TEMU_BASE_URL   = 'https://openapi-b-eu.temu.com/openapi/router';
const CACHE_TTL_DAYS  = 7;
const WALK_DELAY_MS   = 120;   // delay between category tree API calls
const TEMPLATE_DELAY_MS = 150; // delay between template API calls
const MAX_RETRIES     = 3;
const CHECKPOINT_EVERY = 10;   // write progress to Firestore every N templates

// Parse CLI args
const args = process.argv.slice(2);
const FULL_SYNC    = args.includes('--full');
const PHASE        = (args.find(a => a.startsWith('--phase=')) || '--phase=both').split('=')[1];
const CONCURRENCY  = parseInt((args.find(a => a.startsWith('--concurrency=')) || '--concurrency=1').split('=')[1]);

// ── Firebase init ────────────────────────────────────────────────────────────

const serviceAccount = require('./serviceAccountKey.json');
admin.initializeApp({ credential: admin.credential.cert(serviceAccount) });
const db = admin.firestore();

// ── Firestore path helpers ───────────────────────────────────────────────────

const templatesCol   = () => db.collection('marketplaces').doc('Temu').collection('templates');
const categoriesCol  = () => db.collection('marketplaces').doc('Temu').collection('categories');
const metaDoc        = () => db.collection('marketplaces').doc('Temu').collection('meta').doc('categories');
const jobsCol        = () => db.collection('marketplaces').doc('Temu').collection('schema_jobs');

// ── Temu API client ──────────────────────────────────────────────────────────

class TemuClient {
  constructor(baseURL, appKey, appSecret, accessToken) {
    this.baseURL = baseURL;
    this.appKey = appKey;
    this.appSecret = appSecret;
    this.accessToken = accessToken;
  }

  sign(params) {
    // Flatten all params to strings
    const flat = {};
    for (const [k, v] of Object.entries(params)) {
      if (k === 'sign' || v == null) continue;
      if (typeof v === 'object') {
        flat[k] = JSON.stringify(v);
      } else {
        flat[k] = String(v);
      }
    }

    // Sort keys and concatenate: secret + k1v1k2v2... + secret
    const sorted = Object.keys(flat).sort();
    let str = this.appSecret;
    for (const k of sorted) {
      str += k + flat[k];
    }
    str += this.appSecret;

    return crypto.createHash('md5').update(str, 'utf8').digest('hex').toUpperCase();
  }

  async post(params) {
    const payload = {
      ...params,
      app_key: this.appKey,
      access_token: this.accessToken,
      data_type: 'JSON',
      timestamp: Math.floor(Date.now() / 1000),
    };
    payload.sign = this.sign(payload);

    const body = JSON.stringify(payload);

    return new Promise((resolve, reject) => {
      const url = new URL(this.baseURL);
      const options = {
        hostname: url.hostname,
        path: url.pathname,
        method: 'POST',
        headers: {
          'Content-Type': 'application/json',
          'Content-Length': Buffer.byteLength(body),
        },
      };

      const req = https.request(options, (res) => {
        let data = '';
        res.on('data', chunk => data += chunk);
        res.on('end', () => {
          try {
            const parsed = JSON.parse(data);
            // Log first failure in detail so we can diagnose
            if (!parsed.success && !TemuClient._logged) {
              console.log('  [Temu raw response]', JSON.stringify(parsed));
              console.log('  [Temu request body]', body.replace(/"access_token":"[^"]{6}/g, '"access_token":"REDACTED'));
              TemuClient._logged = true;
            }
            resolve(parsed);
          } catch (e) {
            reject(new Error(`Failed to parse response: ${data.slice(0, 500)}`));
          }
        });
      });

      req.on('error', reject);
      req.setTimeout(30000, () => {
        req.destroy(new Error('Request timeout'));
      });
      req.write(body);
      req.end();
    });
  }

  async getCategories(parentCatId) {
    const params = { type: 'bg.local.goods.cats.get' };
    if (parentCatId != null) params.parentCatId = parentCatId;

    const resp = await this.post(params);
    if (!resp.success) {
      // Try alternate param name
      if (parentCatId != null) {
        const params2 = { type: 'bg.local.goods.cats.get', parentId: parentCatId };
        const resp2 = await this.post(params2);
        if (!resp2.success) throw new Error(`getCategories failed: ${resp2.errorMsg}`);
        return resp2.result?.goodsCatsList || resp2.result || [];
      }
      throw new Error(`getCategories failed: ${resp.errorMsg}`);
    }
    return resp.result?.goodsCatsList || resp.result || [];
  }

  async getTemplate(catId) {
    const resp = await this.post({ type: 'bg.local.goods.template.get', catId });
    if (!resp.success) throw new Error(`getTemplate failed for ${catId}: ${resp.errorMsg}`);
    return resp.result || {};
  }
}

// ── Credential loader ────────────────────────────────────────────────────────

async function loadTemuCredential() {
  console.log('Loading Temu credentials...');

  // ── 1. app_key + app_secret from platform_config/temu (global keys) ──
  let appKey = process.env.TEMU_APP_KEY || '';
  let appSecret = process.env.TEMU_APP_SECRET || '';

  try {
    const globalDoc = await db.collection('platform_config').doc('temu').get();
    if (globalDoc.exists) {
      const g = globalDoc.data();
      appKey    = g.app_key    || g.TEMU_APP_KEY    || appKey;
      appSecret = g.app_secret || g.TEMU_APP_SECRET || appSecret;
      console.log(`  ✓ Global keys from platform_config/temu`);
    } else {
      console.log('  platform_config/temu not found — using .env');
    }
  } catch (e) {
    console.log(`  Could not read platform_config/temu: ${e.message} — using .env`);
  }

  // ── 2. access_token from tenant marketplace_credentials ──
  let accessToken = process.env.TEMU_ACCESS_TOKEN || '';
  let baseURL = process.env.TEMU_BASE_URL || TEMU_BASE_URL;

  const credsSnap = await db
    .collection('tenants').doc(TARGET_TENANT)
    .collection('marketplace_credentials')
    .where('channel', '==', 'temu')
    .where('active', '==', true)
    .limit(1)
    .get();

  if (!credsSnap.empty) {
    const cred = credsSnap.docs[0].data();
    const data = cred.credential_data || {};
    // Log ALL fields so we can see exactly what Firestore has
    console.log('  Firestore doc keys:', Object.keys(cred).join(', '));
    console.log('  credential_data keys:', Object.keys(data).join(', ') || '(empty)');
    // Credentials may be AES-256-GCM encrypted in Firestore
    // Backend uses CREDENTIAL_ENCRYPTION_KEY env var (defaults to 'default-32-char-key-change-me!!')
    const encKey = process.env.CREDENTIAL_ENCRYPTION_KEY || 'default-32-char-key-change-me!!';
    const encryptedFields = cred.encrypted_fields || cred.encryptedFields || [];
    console.log('  encrypted_fields:', encryptedFields.join(', ') || '(none)');

    // Helper: decrypt if field is in encrypted_fields list, else return raw
    function maybeDecrypt(raw, fieldName) {
      if (!raw) return raw;
      const isEnc = encryptedFields.includes(fieldName);
      if (isEnc) {
        const dec = decryptCredential(raw, encKey);
        if (dec) return dec;
        console.log(`  Warning: could not decrypt ${fieldName} — using raw value`);
      }
      return raw;
    }

    // Try every possible location the token could be stored
    const rawToken =
      cred.access_token ||
      cred.accessToken ||
      data.access_token ||
      data.accessToken ||
      cred.token ||
      data.token ||
      '';
    const tokenFromFirestore = rawToken ? maybeDecrypt(rawToken, 'access_token') : '';

    if (tokenFromFirestore) {
      accessToken = tokenFromFirestore;
      console.log(`  Token from Firestore (${tokenFromFirestore.length} chars): ${tokenFromFirestore.slice(0,8)}...${tokenFromFirestore.slice(-8)}`);
    } else {
      console.log('  No token found in Firestore doc — falling back to .env');
    }
    baseURL = cred.base_url || data.base_url || baseURL;
    console.log(`  ✓ Credential: ${cred.account_name || credsSnap.docs[0].id}`);
  } else {
    console.log(`  No active Temu credential in ${TARGET_TENANT} — using .env access token`);
  }

  // ── 3. Report what we have ──
  console.log(`  app_key:      ${appKey      ? appKey.slice(0,6)+'****'      : 'MISSING'}`);
  console.log(`  app_secret:   ${appSecret   ? appSecret.slice(0,4)+'****'   : 'MISSING'}`);
  console.log(`  access_token: ${accessToken ? accessToken.slice(0,6)+'****' : 'MISSING'}`);
  console.log(`  base_url:     ${baseURL}`);

  if (!appKey || !appSecret) {
    throw new Error(
      'TEMU_APP_KEY / TEMU_APP_SECRET not found.\n' +
      'Fix the typo in backend/.env (TEMU=ACCESS_TOKEN should be TEMU_ACCESS_TOKEN)\n' +
      'and make sure TEMU_APP_KEY and TEMU_APP_SECRET are present.'
    );
  }
  if (!accessToken) {
    throw new Error(
      'TEMU_ACCESS_TOKEN not found in Firestore or .env.\n' +
      'Check backend/.env has: TEMU_ACCESS_TOKEN=rAgX4310...'
    );
  }

  return new TemuClient(baseURL, appKey, appSecret, accessToken);
}

// ── Job tracking ─────────────────────────────────────────────────────────────

async function createJob(leafCount, phase) {
  const jobId = crypto.randomBytes(16).toString('hex').replace(/(.{8})(.{4})(.{4})(.{4})(.{12})/, '$1-$2-$3-$4-$5');
  const now = new Date();
  await jobsCol().doc(jobId).set({
    jobId,
    status: 'running',
    source: 'standalone-script',
    fullSync: FULL_SYNC,
    phase,
    startedAt: now,
    updatedAt: now,
    downloaded: 0,
    skipped: 0,
    failed: 0,
    total: leafCount,
    leafFound: leafCount,
    treeWalkDone: phase === 'templates',
    lastCatId: 0,
    currentCatName: '',
    errors: [],
  });
  console.log(`✓ Job created: ${jobId}`);
  return jobId;
}

async function updateJob(jobId, data) {
  await jobsCol().doc(jobId).set({ ...data, updatedAt: new Date() }, { merge: true });
}

async function completeJob(jobId, stats) {
  const now = new Date();
  await jobsCol().doc(jobId).set({
    status: 'completed',
    ...stats,
    updatedAt: now,
    completedAt: now,
  }, { merge: true });
}

// ── Phase 1: Tree walk ───────────────────────────────────────────────────────

async function walkCategoryTree(client, jobId) {
  console.log('\n=== PHASE 1: Walking Temu Category Tree ===');
  console.log('This discovers all leaf categories. Can take 30-60 minutes.\n');

  let leafFound = 0;
  const leafCategories = []; // { catId, catName }
  const errors = [];

  async function walk(parentCatId, level) {
    let cats;
    try {
      cats = await client.getCategories(parentCatId);
    } catch (err) {
      const msg = `get categories (parent=${parentCatId}): ${err.message}`;
      console.error(`  ✗ ${msg}`);
      errors.push(msg);
      return;
    }

    if (!Array.isArray(cats)) return;

    for (const cat of cats) {
      const catId   = cat.catId   || cat.cat_id;
      const catName = cat.catName || cat.cat_name || `Cat ${catId}`;
      const leaf    = cat.leaf === true || cat.leaf === 1;
      const parentId = cat.parentCatId || cat.parentId || parentCatId || 0;

      // Write to Firestore immediately
      await categoriesCol().doc(String(catId)).set({
        catId,
        catName,
        parentId,
        leaf,
        level,
        cachedAt: new Date(),
      });

      if (leaf) {
        leafFound++;
        leafCategories.push({ catId, catName });

        if (leafFound % 25 === 0) {
          process.stdout.write(`\r  🌿 Leaves found: ${leafFound.toLocaleString()} (current: ${catName.slice(0, 50)})`);
          await updateJob(jobId, {
            leafFound,
            total: leafFound,
            currentCatName: `Walking: ${catName} (level ${level})`,
          });
        }
      } else {
        // Recurse into children
        await sleep(WALK_DELAY_MS);
        await walk(catId, level + 1);
      }
    }
  }

  await walk(null, 0);

  console.log(`\n\n✓ Tree walk complete: ${leafFound.toLocaleString()} leaf categories found`);
  if (errors.length > 0) {
    console.log(`  ⚠ ${errors.length} errors during walk (partial data may be missing)`);
  }

  // Update meta doc
  await metaDoc().set({
    leafCount: leafFound,
    totalCount: leafFound,
    cachedAt: new Date(),
  }, { merge: true });

  await updateJob(jobId, {
    treeWalkDone: true,
    leafFound,
    total: leafFound,
    currentCatName: `Tree walk complete — ${leafFound.toLocaleString()} categories found`,
  });

  return leafCategories;
}

// ── Phase 2: Template download ───────────────────────────────────────────────

async function downloadTemplates(client, jobId, leafCategories) {
  console.log(`\n=== PHASE 2: Downloading Templates ===`);
  console.log(`Total categories: ${leafCategories.length.toLocaleString()}`);
  console.log(`Full sync: ${FULL_SYNC} | Concurrency: ${CONCURRENCY}`);
  console.log(`Estimated time: ~${Math.round(leafCategories.length * TEMPLATE_DELAY_MS / 1000 / 60)} minutes at concurrency 1\n`);

  let downloaded = 0;
  let skipped    = 0;
  let failed     = 0;
  const errors   = [];
  const startTime = Date.now();

  // Process in batches based on concurrency
  const queue = [...leafCategories];

  async function processOne({ catId, catName }) {
    // Check cache unless full sync
    if (!FULL_SYNC) {
      try {
        const existing = await templatesCol().doc(String(catId)).get();
        if (existing.exists) {
          const cachedAt = existing.data()?.cachedAt?.toDate?.() || existing.data()?.cachedAt;
          if (cachedAt) {
            const ageMs = Date.now() - new Date(cachedAt).getTime();
            if (ageMs < CACHE_TTL_DAYS * 24 * 60 * 60 * 1000) {
              skipped++;
              return;
            }
          }
        }
      } catch (_) {}
    }

    // Download with retries
    let template = null;
    let lastErr = null;
    let backoff = 2000;

    for (let attempt = 0; attempt < MAX_RETRIES; attempt++) {
      try {
        template = await client.getTemplate(catId);
        lastErr = null;
        break;
      } catch (err) {
        lastErr = err;
        if (attempt < MAX_RETRIES - 1) {
          await sleep(backoff);
          backoff *= 2;
        }
      }
    }

    if (lastErr) {
      const msg = `${catId} (${catName}): ${lastErr.message}`;
      if (errors.length < 100) errors.push(msg);
      failed++;
      return;
    }

    // Store in Firestore
    try {
      await templatesCol().doc(String(catId)).set({
        catId,
        catName,
        goodsProperties: template.goodsProperties || null,
        rawTemplate: template,
        cachedAt: new Date(),
      });
      downloaded++;
    } catch (err) {
      const msg = `${catId}: store failed: ${err.message}`;
      if (errors.length < 100) errors.push(msg);
      failed++;
    }
  }

  // Run with concurrency
  let i = 0;
  while (i < queue.length) {
    const batch = queue.slice(i, i + CONCURRENCY);
    await Promise.all(batch.map(item => processOne(item).then(() => sleep(TEMPLATE_DELAY_MS))));
    i += CONCURRENCY;

    const total = downloaded + skipped + failed;
    if (total % CHECKPOINT_EVERY === 0 || i >= queue.length) {
      const elapsed = (Date.now() - startTime) / 1000;
      const rate = total / elapsed;
      const remaining = queue.length - total;
      const etaMins = rate > 0 ? Math.round(remaining / rate / 60) : '?';

      process.stdout.write(
        `\r  📦 ${total.toLocaleString()} / ${queue.length.toLocaleString()} | ` +
        `✓${downloaded} ↷${skipped} ✗${failed} | ETA: ${etaMins}m    `
      );

      await updateJob(jobId, {
        downloaded,
        skipped,
        failed,
        errors: errors.slice(0, 20),
        lastCatId: batch[batch.length - 1]?.catId || 0,
        currentCatName: `Downloading: ${batch[0]?.catName || ''} (${total}/${queue.length})`,
      });
    }
  }

  console.log(`\n\n✓ Template download complete`);
  console.log(`  Downloaded: ${downloaded.toLocaleString()}`);
  console.log(`  Skipped (cached): ${skipped.toLocaleString()}`);
  console.log(`  Failed: ${failed.toLocaleString()}`);

  return { downloaded, skipped, failed, errors };
}

// ── Load existing leaf categories from Firestore ─────────────────────────────

async function loadLeafCategoriesFromFirestore() {
  console.log('Loading leaf categories from Firestore (tree walk already done)...');

  const snap = await categoriesCol().where('leaf', '==', true).get();
  const cats = snap.docs.map(d => ({
    catId: d.data().catId,
    catName: d.data().catName || `Cat ${d.id}`,
  })).filter(c => c.catId > 0);

  console.log(`✓ Loaded ${cats.length.toLocaleString()} leaf categories from Firestore`);
  return cats;
}

// ── Helpers ──────────────────────────────────────────────────────────────────

function sleep(ms) {
  return new Promise(resolve => setTimeout(resolve, ms));
}

function formatDuration(ms) {
  const mins = Math.floor(ms / 60000);
  const secs = Math.floor((ms % 60000) / 1000);
  return mins > 0 ? `${mins}m ${secs}s` : `${secs}s`;
}

// ── Main ─────────────────────────────────────────────────────────────────────

async function main() {
  console.log('╔════════════════════════════════════════════╗');
  console.log('║   Temu Schema Sync — Standalone Script     ║');
  console.log('╚════════════════════════════════════════════╝');
  console.log(`Phase: ${PHASE} | Full sync: ${FULL_SYNC} | Concurrency: ${CONCURRENCY}\n`);

  const startTime = Date.now();

  // Load Temu API credentials
  const client = await loadTemuCredential();

  let leafCategories = [];

  if (PHASE === 'walk' || PHASE === 'both') {
    const jobId = await createJob(0, 'walk');
    leafCategories = await walkCategoryTree(client, jobId);
    await completeJob(jobId, { leafFound: leafCategories.length });

    if (PHASE === 'walk') {
      console.log('\n✓ Walk complete. Run with --phase=templates to download templates.');
      process.exit(0);
    }
  }

  if (PHASE === 'templates' || PHASE === 'both') {
    if (leafCategories.length === 0) {
      leafCategories = await loadLeafCategoriesFromFirestore();
    }

    if (leafCategories.length === 0) {
      console.error('✗ No leaf categories found. Run with --phase=walk first.');
      process.exit(1);
    }

    const jobId = await createJob(leafCategories.length, 'templates');
    const stats = await downloadTemplates(client, jobId, leafCategories);
    await completeJob(jobId, stats);
  }

  const duration = formatDuration(Date.now() - startTime);
  console.log(`\n✓ All done in ${duration}`);
  process.exit(0);
}

main().catch(err => {
  console.error('\n✗ Fatal error:', err.message);
  process.exit(1);
});
