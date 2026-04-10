// ============================================================================
// EBAY UK SCHEMA SYNC — Standalone Script
// ============================================================================
// Downloads the full eBay UK category tree and item aspects (field structures)
// for every leaf category, writing directly to Firestore.
//
// Firestore structure written:
//   marketplaces/eBay/EBAY_GB/data/meta/category_tree        — full flattened tree
//   marketplaces/eBay/EBAY_GB/data/aspects/{categoryId}      — aspects per category (small)
//   marketplaces/eBay/EBAY_GB/data/aspects/{categoryId}_meta — chunk index (large categories)
//   marketplaces/eBay/EBAY_GB/data/aspects/{categoryId}_c000 — chunk docs (large categories)
//   marketplaces/eBay/schema_jobs/{jobId}                    — progress tracking
//
// Usage:
//   node ebay-sync.js                        — incremental (skip fresh cache <7 days)
//   node ebay-sync.js --full                 — force re-download everything
//   node ebay-sync.js --phase=tree           — category tree only
//   node ebay-sync.js --phase=aspects        — aspects only (tree must exist)
//   node ebay-sync.js --phase=both           — tree + aspects (default)
//   node ebay-sync.js --concurrency=3        — parallel aspect downloads
//
// Requirements:
//   npm install firebase-admin
//   Place serviceAccountKey.json in same folder as this script
//   backend/.env must contain EBAY_PROD_CLIENT_ID, EBAY_PROD_CLIENT_SECRET,
//   EBAY_DEV_ID, and the credential in Firestore must have refresh_token
//
// ============================================================================

'use strict';

const fs      = require('fs');
const path    = require('path');
const https   = require('https');
const http    = require('http');
const crypto  = require('crypto');
const admin   = require('firebase-admin');

// ── Load .env ────────────────────────────────────────────────────────────────

function loadEnv() {
  const candidates = [
    path.join(__dirname, '.env'),
    path.join(__dirname, 'backend', '.env'),
    path.join(__dirname, 'platform', 'backend', '.env'),
  ];
  const envPath = candidates.find(p => fs.existsSync(p));
  if (!envPath) { console.log('  (no .env file found)'); return; }
  console.log(`  (reading env from: ${envPath})`);
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
}
loadEnv();

// ── AES-256-GCM decryption (matches backend marketplace_services.go) ─────────

function decryptCredential(ciphertext, keyStr) {
  let key = Buffer.from(keyStr, 'utf8');
  if (key.length > 32) key = key.slice(0, 32);
  if (key.length < 32) { const p = Buffer.alloc(32); key.copy(p); key = p; }
  try {
    const data = Buffer.from(ciphertext, 'base64');
    const nonce = data.slice(0, 12);
    const tag   = data.slice(data.length - 16);
    const enc   = data.slice(12, data.length - 16);
    const decipher = crypto.createDecipheriv('aes-256-gcm', key, nonce);
    decipher.setAuthTag(tag);
    return Buffer.concat([decipher.update(enc), decipher.final()]).toString('utf8');
  } catch {
    return null;
  }
}

// ── Config ───────────────────────────────────────────────────────────────────

const TARGET_TENANT    = 'tenant-demo';
const MARKETPLACE_ID   = 'EBAY_GB';
const TREE_ID          = '3';        // eBay UK tree ID
const EBAY_API_ROOT    = 'https://api.ebay.com';
const EBAY_TOKEN_URL   = 'https://api.ebay.com/identity/v1/oauth2/token';
const CACHE_TTL_DAYS   = 7;
const ASPECT_DELAY_MS  = 2000;       // ~0.5 req/sec - conservative to avoid 429s
const MAX_RETRIES      = 6;          // more retries for rate limit recovery
const CHECKPOINT_EVERY = 50;
const FIRESTORE_MAX_BYTES = 900_000; // 900KB threshold — safe margin under 1MB limit
const REQUEST_TIMEOUT_MS  = 60_000;  // 60s timeout (up from 30s) — fixes JSON truncation

// Parse CLI args
const args        = process.argv.slice(2);
const FULL_SYNC   = args.includes('--full');
const PHASE       = (args.find(a => a.startsWith('--phase=')) || '--phase=both').split('=')[1];
const CONCURRENCY = Math.min(3, parseInt((args.find(a => a.startsWith('--concurrency=')) || '--concurrency=1').split('=')[1]) || 1);

// ── Firebase init ─────────────────────────────────────────────────────────────

const serviceAccount = require('./serviceAccountKey.json');
admin.initializeApp({ credential: admin.credential.cert(serviceAccount) });
const db = admin.firestore();

// ── Firestore path helpers ────────────────────────────────────────────────────

const categoryTreeDoc = () =>
  db.collection('marketplaces').doc('eBay')
    .collection(MARKETPLACE_ID).doc('data')
    .collection('meta').doc('category_tree');

const aspectsCol = () =>
  db.collection('marketplaces').doc('eBay')
    .collection(MARKETPLACE_ID).doc('data')
    .collection('aspects');

const jobsCol = () =>
  db.collection('marketplaces').doc('eBay').collection('schema_jobs');

// ── eBay OAuth2 Client ────────────────────────────────────────────────────────

class EbayClient {
  constructor(clientId, clientSecret, accessToken, refreshToken) {
    this.clientId     = clientId;
    this.clientSecret = clientSecret;
    this.accessToken  = accessToken;
    this.refreshToken = refreshToken;
    this.tokenExpiry  = accessToken ? new Date(Date.now() + 2 * 60 * 60 * 1000) : new Date(0);
  }

  async refreshAccessToken() {
    if (!this.refreshToken) throw new Error('No refresh token available');

    const scopes = [
      'https://api.ebay.com/oauth/api_scope',
      'https://api.ebay.com/oauth/api_scope/sell.inventory',
      'https://api.ebay.com/oauth/api_scope/sell.account',
      'https://api.ebay.com/oauth/api_scope/sell.fulfillment',
      'https://api.ebay.com/oauth/api_scope/sell.marketing',
    ].join(' ');

    const body = new URLSearchParams({
      grant_type:    'refresh_token',
      refresh_token: this.refreshToken,
      scope:         scopes,
    }).toString();

    const credentials = Buffer.from(`${this.clientId}:${this.clientSecret}`).toString('base64');

    const data = await httpRequest('POST', EBAY_TOKEN_URL, body, {
      'Content-Type':  'application/x-www-form-urlencoded',
      'Authorization': `Basic ${credentials}`,
    });

    const parsed = JSON.parse(data);
    if (!parsed.access_token) {
      throw new Error(`Token refresh failed: ${data.slice(0, 300)}`);
    }

    this.accessToken  = parsed.access_token;
    this.tokenExpiry  = new Date(Date.now() + (parsed.expires_in - 300) * 1000);
    if (parsed.refresh_token) this.refreshToken = parsed.refresh_token;
    console.log(`  ✓ Access token refreshed (expires in ${parsed.expires_in}s)`);
  }

  async ensureToken() {
    if (!this.accessToken || Date.now() > this.tokenExpiry.getTime()) {
      await this.refreshAccessToken();
    }
  }

  async get(urlPath) {
    await this.ensureToken();

    const data = await httpRequest('GET', `${EBAY_API_ROOT}${urlPath}`, null, {
      'Authorization':            `Bearer ${this.accessToken}`,
      'Accept':                   'application/json',
      'Content-Language':         'en-GB',
      'X-EBAY-C-MARKETPLACE-ID':  MARKETPLACE_ID,
    });

    // Guard against empty/truncated responses before parsing
    if (!data || data.trim() === '') {
      throw new Error('Empty response from eBay API');
    }

    let parsed;
    try {
      parsed = JSON.parse(data);
    } catch (e) {
      throw new Error(`Invalid JSON response (${data.length} bytes): ${e.message}`);
    }

    if (parsed.errors) {
      const err = parsed.errors[0];
      throw new Error(`eBay API error ${err.errorId}: ${err.longMessage || err.message}`);
    }
    return parsed;
  }

  async getCategoryTree() {
    console.log(`  Fetching category tree (tree ID: ${TREE_ID})...`);
    return await this.get(`/commerce/taxonomy/v1/category_tree/${TREE_ID}`);
  }

  async getItemAspectsForCategory(categoryId) {
    const qs = new URLSearchParams({ category_id: categoryId }).toString();
    return await this.get(
      `/commerce/taxonomy/v1/category_tree/${TREE_ID}/get_item_aspects_for_category?${qs}`
    );
  }
}

// ── Generic HTTP request helper ───────────────────────────────────────────────
// Increased timeout to 60s to prevent premature connection closes on large
// responses (was causing "Unexpected end of JSON input" errors).

function httpRequest(method, urlStr, body, headers) {
  return new Promise((resolve, reject) => {
    const url  = new URL(urlStr);
    const lib  = url.protocol === 'https:' ? https : http;
    const opts = {
      hostname: url.hostname,
      path:     url.pathname + url.search,
      method,
      headers:  headers || {},
    };
    if (body) {
      opts.headers['Content-Length'] = Buffer.byteLength(body);
    }

    const req = lib.request(opts, (res) => {
      const chunks = [];
      res.on('data', chunk => chunks.push(chunk));
      res.on('end', () => {
        const data = Buffer.concat(chunks).toString('utf8');
        if (res.statusCode >= 400) {
          reject(new Error(`HTTP ${res.statusCode}: ${data.slice(0, 500)}`));
        } else {
          resolve(data);
        }
      });
      res.on('error', reject);
    });

    req.on('error', reject);
    req.setTimeout(REQUEST_TIMEOUT_MS, () => {
      req.destroy(new Error(`Request timeout after ${REQUEST_TIMEOUT_MS / 1000}s`));
    });
    if (body) req.write(body);
    req.end();
  });
}

// ── Sleep helper ──────────────────────────────────────────────────────────────

const sleep = ms => new Promise(r => setTimeout(r, ms));

// ── Retry wrapper ─────────────────────────────────────────────────────────────
// Uses exponential backoff. Does NOT retry on empty responses — those are
// permanently empty categories on eBay's side and retrying wastes time.

const SKIP_ERRORS = ['Empty response from eBay API'];

// Categories like "Attributes1", "eBay Tests - DO NOT BID" permanently return
// empty responses. Don't retry them - they will never succeed.
const NO_RETRY_ERRORS = ['Empty response from eBay API'];
const RATE_LIMIT_DELAY_MS = 60_000; // Wait 60s on 429 before retrying

async function withRetry(fn, label, maxRetries = MAX_RETRIES) {
  let backoff = 2000;
  for (let i = 0; i < maxRetries; i++) {
    try {
      return await fn();
    } catch (err) {
      const isPermanent = NO_RETRY_ERRORS.some(s => err.message.includes(s));
      if (isPermanent) throw err;

      const isRateLimit = err.message.includes('429') || err.message.includes('Too many requests');
      if (isRateLimit) {
        if (i === maxRetries - 1) throw err;
        console.log(`    ⏳ Rate limited - waiting ${RATE_LIMIT_DELAY_MS / 1000}s before retry ${i + 1}/${maxRetries - 1} for ${label}`);
        await sleep(RATE_LIMIT_DELAY_MS);
        continue; // don't increase backoff - always use RATE_LIMIT_DELAY_MS
      }

      if (i === maxRetries - 1) throw err;
      console.log(`    ↺ Retry ${i + 1}/${maxRetries - 1} for ${label}: ${err.message}`);
      await sleep(backoff);
      backoff = Math.min(backoff * 2, 30_000);
    }
  }
}

// ── Firestore aspect writer — handles documents that exceed the 1MB limit ─────
//
// Splits by actual byte size, not fixed aspect count. Trading card categories
// can have individual aspects of 10-50KB each, so a chunk of 200 can still be
// 2MB+. We measure each aspect and fill chunks up to the safe byte limit.

function estimateJsonBytes(obj) {
  return Buffer.byteLength(JSON.stringify(obj), 'utf8');
}

// Split aspects into groups where each group fits within maxBytes
function splitAspectsBySize(aspects, maxBytes) {
  const groups  = [];
  let current   = [];
  let currentSz = 2; // for []

  for (const aspect of aspects) {
    const itemSz = Buffer.byteLength(JSON.stringify(aspect), 'utf8') + 1; // +1 for comma

    // Single oversized aspect must go alone
    if (current.length === 0 && itemSz > maxBytes) {
      groups.push([aspect]);
      continue;
    }

    if (current.length > 0 && currentSz + itemSz > maxBytes) {
      groups.push(current);
      current   = [];
      currentSz = 2;
    }
    current.push(aspect);
    currentSz += itemSz;
  }
  if (current.length > 0) groups.push(current);
  return groups;
}

async function writeAspectToFirestore(categoryId, categoryName, aspects) {
  const docId   = String(categoryId);
  const baseDoc = {
    categoryId:    docId,
    categoryName,
    marketplaceId: MARKETPLACE_ID,
    aspectCount:   aspects.length,
    cachedAt:      new Date(),
  };

  const fullDoc   = { ...baseDoc, aspects };
  const sizeBytes = estimateJsonBytes(fullDoc);

  if (sizeBytes <= FIRESTORE_MAX_BYTES) {
    await aspectsCol().doc(docId).set(fullDoc);
    return { chunked: false };
  }

  // Chunked: split by byte size (reserve 2KB per chunk for metadata fields)
  const chunkGroups = splitAspectsBySize(aspects, FIRESTORE_MAX_BYTES - 2048);
  const chunkCount  = chunkGroups.length;

  for (let ci = 0; ci < chunkCount; ci++) {
    const chunkDocId = `${docId}_c${String(ci).padStart(3, '0')}`;
    await aspectsCol().doc(chunkDocId).set({
      categoryId:    docId,
      categoryName,
      marketplaceId: MARKETPLACE_ID,
      chunkIndex:    ci,
      chunkCount,
      aspects:       chunkGroups[ci],
      cachedAt:      new Date(),
    });
  }

  // Lightweight meta doc at base path (no aspects array)
  await aspectsCol().doc(docId).set({
    ...baseDoc,
    chunked:    true,
    chunkCount,
    sizeBytes,
  });

  return { chunked: true, chunkCount };
}

// ── Credential loader ─────────────────────────────────────────────────────────

async function loadEbayCredential() {
  console.log(`Loading eBay credentials (tenant: ${TARGET_TENANT})...`);

  const encKey = process.env.CREDENTIAL_ENCRYPTION_KEY || 'default-32-char-key-change-me!!';

  const clientId     = process.env.EBAY_PROD_CLIENT_ID     || '';
  const clientSecret = process.env.EBAY_PROD_CLIENT_SECRET || '';

  if (!clientId || !clientSecret) {
    throw new Error(
      'EBAY_PROD_CLIENT_ID or EBAY_PROD_CLIENT_SECRET not found.\n' +
      'These should be in your backend/.env file.'
    );
  }

  console.log(`  client_id:     ${clientId.slice(0, 10)}****`);
  console.log(`  client_secret: ${clientSecret.slice(0, 6)}****`);

  const credsSnap = await db
    .collection('tenants').doc(TARGET_TENANT)
    .collection('marketplace_credentials')
    .where('channel', '==', 'ebay')
    .where('active', '==', true)
    .limit(1)
    .get();

  if (credsSnap.empty) {
    throw new Error(
      `No active eBay credential found in ${TARGET_TENANT}.\n` +
      'Check Marketplace Connections.'
    );
  }

  const cred = credsSnap.docs[0].data();
  const data = cred.credential_data || {};
  const encryptedFields = cred.encrypted_fields || cred.encryptedFields || [];
  console.log(`  Credential: ${cred.account_name || credsSnap.docs[0].id}`);
  console.log(`  Firestore fields: ${Object.keys(cred).join(', ')}`);
  console.log(`  credential_data fields: ${Object.keys(data).join(', ') || '(empty)'}`);
  console.log(`  encrypted_fields: ${encryptedFields.join(', ') || '(none)'}`);

  function maybeDecrypt(raw, fieldName) {
    if (!raw) return raw;
    if (encryptedFields.includes(fieldName)) {
      const dec = decryptCredential(raw, encKey);
      if (dec !== null) return dec;
      console.log(`  Warning: could not decrypt ${fieldName} — using raw value`);
    }
    return raw;
  }

  const refreshToken = maybeDecrypt(
    cred.refresh_token || data.refresh_token || process.env.EBAY_REFRESH_TOKEN || '',
    'refresh_token'
  );
  const accessToken = maybeDecrypt(
    cred.access_token || data.access_token || '',
    'access_token'
  );

  console.log(`  refresh_token: ${refreshToken ? refreshToken.slice(0, 8) + '****' : 'MISSING'}`);
  console.log(`  access_token:  ${accessToken  ? accessToken.slice(0, 8)  + '****' : '(none — will refresh)'}`);

  if (!refreshToken) {
    throw new Error(
      'eBay refresh_token not found in Firestore or .env.\n' +
      'Connect your eBay account via the MarketMate UI first, or add\n' +
      'EBAY_REFRESH_TOKEN=... to your backend/.env'
    );
  }

  return new EbayClient(clientId, clientSecret, accessToken, refreshToken);
}

// ── Flatten category tree ─────────────────────────────────────────────────────

function flattenTree(node, parentId, level, result = []) {
  if (!node) return result;

  const cat = node.category || node;
  const categoryId   = cat.categoryId   || node.categoryId;
  const categoryName = cat.categoryName || node.categoryName || '';

  if (!categoryId) return result;

  result.push({
    categoryId:   String(categoryId),
    categoryName,
    parentId:     parentId ? String(parentId) : null,
    level,
    leaf:         node.leafCategoryTreeNode === true,
  });

  const children = node.childCategoryTreeNodes || [];
  for (const child of children) {
    flattenTree(child, categoryId, level + 1, result);
  }
  return result;
}

// ── Job tracking ──────────────────────────────────────────────────────────────

async function createJob(phase, leafCount) {
  const jobId = crypto.randomUUID
    ? crypto.randomUUID()
    : crypto.randomBytes(16).toString('hex').replace(/(.{8})(.{4})(.{4})(.{4})(.{12})/, '$1-$2-$3-$4-$5');
  const now = new Date();
  await jobsCol().doc(jobId).set({
    jobId,
    status:        'running',
    source:        'standalone-script',
    marketplaceId: MARKETPLACE_ID,
    phase,
    fullSync:      FULL_SYNC,
    total:         leafCount || 0,
    downloaded:    0,
    skipped:       0,
    failed:        0,
    chunked:       0,
    errors:        [],
    startedAt:     now,
    updatedAt:     now,
  });
  console.log(`✓ Job created: ${jobId}`);
  return jobId;
}

async function updateJob(jobId, patch) {
  await jobsCol().doc(jobId).set({ ...patch, updatedAt: new Date() }, { merge: true });
}

async function completeJob(jobId, stats) {
  const now = new Date();
  await jobsCol().doc(jobId).set({
    status:      'completed',
    ...stats,
    updatedAt:   now,
    completedAt: now,
  }, { merge: true });
}

// ── PHASE 1: Download and cache the category tree ─────────────────────────────

async function runTreeWalk(client) {
  console.log('\n=== PHASE 1: Downloading eBay UK Category Tree ===');
  console.log('Fetching full tree from eBay Taxonomy API...');

  const jobId = await createJob('tree', 0);

  let tree;
  try {
    tree = await withRetry(() => client.getCategoryTree(), 'getCategoryTree');
  } catch (err) {
    await updateJob(jobId, { status: 'failed', error: err.message });
    throw new Error(`Category tree download failed: ${err.message}`);
  }

  console.log(`  API response keys: ${Object.keys(tree).join(', ')}`);
  const rootNode = tree.rootCategoryNode || tree.rootNode || {};
  console.log(`  Root node keys: ${Object.keys(rootNode).join(', ')}`);
  if (rootNode.category) {
    console.log(`  Root category: ${JSON.stringify(rootNode.category)}`);
  }
  const firstChild = (rootNode.childCategoryTreeNodes || [])[0];
  if (firstChild) {
    console.log(`  First child keys: ${Object.keys(firstChild).join(', ')}`);
    console.log(`  First child sample: ${JSON.stringify(firstChild).slice(0, 200)}`);
  }

  const flatCategories = flattenTree(rootNode, null, 0);
  const leafCategories = flatCategories.filter(c => c.leaf);

  console.log(`  Total categories: ${flatCategories.length.toLocaleString()}`);
  console.log(`  Leaf categories:  ${leafCategories.length.toLocaleString()}`);
  console.log(`  Tree version:     ${tree.categoryTreeVersion || 'unknown'}`);

  const CHUNK_SIZE = 1000;
  const chunks = [];
  for (let i = 0; i < flatCategories.length; i += CHUNK_SIZE) {
    chunks.push(flatCategories.slice(i, i + CHUNK_SIZE));
  }

  console.log(`  Writing category tree to Firestore (${chunks.length} chunks of ${CHUNK_SIZE})...`);

  await categoryTreeDoc().set({
    marketplaceId: MARKETPLACE_ID,
    treeId:        TREE_ID,
    treeVersion:   tree.categoryTreeVersion || '',
    totalCount:    flatCategories.length,
    leafCount:     leafCategories.length,
    chunkCount:    chunks.length,
    chunkSize:     CHUNK_SIZE,
    cachedAt:      new Date(),
  });

  const chunksCol  = categoryTreeDoc().parent;
  const batch_size = 5;
  for (let ci = 0; ci < chunks.length; ci += batch_size) {
    const batch = db.batch();
    for (let bi = ci; bi < Math.min(ci + batch_size, chunks.length); bi++) {
      const chunkRef = chunksCol.doc(`category_chunk_${String(bi).padStart(4, '0')}`);
      batch.set(chunkRef, {
        chunkIndex: bi,
        categories: chunks[bi],
        cachedAt:   new Date(),
      });
    }
    await batch.commit();
    process.stdout.write(`\r  Writing chunks... ${Math.min(ci + batch_size, chunks.length)}/${chunks.length}`);
  }
  process.stdout.write('\n');

  await completeJob(jobId, {
    total:      flatCategories.length,
    leafCount:  leafCategories.length,
    downloaded: flatCategories.length,
  });

  console.log(`✓ Category tree cached (${flatCategories.length.toLocaleString()} categories, ${leafCategories.length.toLocaleString()} leaf, ${chunks.length} chunks)\n`);
  return leafCategories;
}

// ── Load leaf categories from Firestore (for aspects-only phase) ──────────────

async function loadLeafCategoriesFromFirestore() {
  console.log('Loading leaf categories from Firestore...');
  const metaDoc = await categoryTreeDoc().get();
  if (!metaDoc.exists) {
    throw new Error(
      'No category tree found in Firestore.\n' +
      'Run with --phase=tree or --phase=both first.'
    );
  }
  const meta = metaDoc.data();

  let allCategories = [];

  if (meta.chunkCount) {
    const chunksCol = categoryTreeDoc().parent;
    process.stdout.write(`  Reading ${meta.chunkCount} chunk documents...`);
    for (let ci = 0; ci < meta.chunkCount; ci++) {
      const chunkDoc = await chunksCol.doc(`category_chunk_${String(ci).padStart(4, '0')}`).get();
      if (chunkDoc.exists) {
        allCategories = allCategories.concat(chunkDoc.data().categories || []);
      }
      if ((ci + 1) % 5 === 0) process.stdout.write(` ${ci + 1}`);
    }
    process.stdout.write('\n');
  } else if (meta.categories) {
    allCategories = meta.categories;
  } else {
    throw new Error('Category tree doc exists but has no categories or chunks. Re-run --phase=tree.');
  }

  const leaf = allCategories.filter(c => c.leaf);
  console.log(`✓ Loaded ${allCategories.length.toLocaleString()} total, ${leaf.length.toLocaleString()} leaf categories\n`);
  return leaf;
}

// ── PHASE 2: Download item aspects for every leaf category ────────────────────

async function runAspectsDownload(client, leafCategories) {
  console.log('=== PHASE 2: Downloading Item Aspects ===');
  console.log(`Total leaf categories: ${leafCategories.length.toLocaleString()}`);
  console.log(`Full sync: ${FULL_SYNC} | Concurrency: ${CONCURRENCY}`);

  const estimateMins = Math.ceil(leafCategories.length * ASPECT_DELAY_MS / 1000 / 60);
  console.log(`Estimated time: ~${estimateMins} minutes at concurrency ${CONCURRENCY}\n`);

  const jobId = await createJob('aspects', leafCategories.length);

  let downloaded = 0;
  let skipped    = 0;
  let failed     = 0;
  let chunked    = 0;
  const errors   = [];
  let tokenRefreshCount = 0;

  const batches = [];
  for (let i = 0; i < leafCategories.length; i += CONCURRENCY) {
    batches.push(leafCategories.slice(i, i + CONCURRENCY));
  }

  for (let bi = 0; bi < batches.length; bi++) {
    const batch = batches[bi];

    await Promise.all(batch.map(async (cat) => {
      const categoryId   = cat.categoryId;
      const categoryName = cat.categoryName;

      // Freshness check
      if (!FULL_SYNC) {
        try {
          const existing = await aspectsCol().doc(String(categoryId)).get();
          if (existing.exists) {
            const cachedAt = existing.data().cachedAt;
            if (cachedAt) {
              const ageMs = Date.now() - (cachedAt.toDate ? cachedAt.toDate().getTime() : new Date(cachedAt).getTime());
              if (ageMs < CACHE_TTL_DAYS * 24 * 60 * 60 * 1000) {
                skipped++;
                return;
              }
            }
          }
        } catch { /* if we can't check, just download */ }
      }

      try {
        const result = await withRetry(
          () => client.getItemAspectsForCategory(String(categoryId)),
          `aspects(${categoryId})`
        );

        const aspects = result.aspects || [];

        const writeResult = await writeAspectToFirestore(categoryId, categoryName, aspects);

        if (writeResult.chunked) {
          chunked++;
          console.log(`  ℹ ${categoryId} (${categoryName}): stored in ${writeResult.chunkCount} chunks`);
        }

        downloaded++;
      } catch (err) {
        // Silently skip permanently-empty categories (test/placeholder categories on eBay)
        const isPermanent = NO_RETRY_ERRORS.some(s => err.message.includes(s));
        if (isPermanent) {
          skipped++;
          return;
        }
        const msg = `${categoryId} (${categoryName}): ${err.message}`;
        failed++;
        if (errors.length < 100) errors.push(msg);
        console.log(`  ✗ ${msg}`);
      }

      await sleep(ASPECT_DELAY_MS);
    }));

    // Refresh token every 200 categories to avoid expiry mid-run
    const processed = (bi + 1) * CONCURRENCY;
    if (processed % 200 < CONCURRENCY && processed > CONCURRENCY) {
      try {
        await client.refreshAccessToken();
        tokenRefreshCount++;
      } catch (err) {
        console.log(`  ⚠ Token refresh failed (will retry on next call): ${err.message}`);
      }
    }

    // Progress + checkpoint
    const total     = downloaded + skipped + failed;
    const pct       = ((total / leafCategories.length) * 100).toFixed(1);
    const remaining = leafCategories.length - total;
    const etaMins   = Math.ceil(remaining * ASPECT_DELAY_MS / 1000 / 60 / CONCURRENCY);
    process.stdout.write(`\r  📋 ${total.toLocaleString()} / ${leafCategories.length.toLocaleString()} (${pct}%) | ✓${downloaded} ↷${skipped} ✗${failed} 🗂${chunked} | ETA: ${etaMins}m    `);

    if ((bi + 1) % (CHECKPOINT_EVERY / CONCURRENCY) === 0) {
      await updateJob(jobId, { downloaded, skipped, failed, chunked, errors });
    }
  }

  process.stdout.write('\n');

  await completeJob(jobId, { downloaded, skipped, failed, chunked, errors });

  console.log('\n' + '─'.repeat(50));
  console.log(`✓ Aspects download complete`);
  console.log(`  Downloaded:      ${downloaded.toLocaleString()}`);
  console.log(`  Skipped:         ${skipped.toLocaleString()} (cached < ${CACHE_TTL_DAYS} days)`);
  console.log(`  Chunked (large): ${chunked.toLocaleString()}`);
  console.log(`  Failed:          ${failed}`);
  console.log(`  Token refreshes: ${tokenRefreshCount}`);
  if (errors.length > 0) {
    console.log(`\n  First ${Math.min(errors.length, 10)} errors:`);
    errors.slice(0, 10).forEach(e => console.log(`    - ${e}`));
  }
  console.log('─'.repeat(50));
}

// ── Main ──────────────────────────────────────────────────────────────────────

async function main() {
  console.log('╔════════════════════════════════════════════╗');
  console.log('║   eBay UK Schema Sync — Standalone Script  ║');
  console.log('╚════════════════════════════════════════════╝');
  console.log(`Phase: ${PHASE} | Full sync: ${FULL_SYNC} | Concurrency: ${CONCURRENCY}\n`);

  if (!['tree', 'aspects', 'both'].includes(PHASE)) {
    console.error(`✗ Invalid phase "${PHASE}". Use: tree, aspects, or both`);
    process.exit(1);
  }

  let client;
  try {
    client = await loadEbayCredential();
  } catch (err) {
    console.error(`\n✗ Fatal error: ${err.message}`);
    process.exit(1);
  }

  console.log('\nRefreshing access token...');
  try {
    await client.refreshAccessToken();
  } catch (err) {
    console.error(`✗ Fatal: could not get access token: ${err.message}`);
    process.exit(1);
  }

  try {
    let leafCategories;

    if (PHASE === 'tree' || PHASE === 'both') {
      leafCategories = await runTreeWalk(client);
    }

    if (PHASE === 'aspects' || PHASE === 'both') {
      if (!leafCategories) {
        leafCategories = await loadLeafCategoriesFromFirestore();
      }
      await runAspectsDownload(client, leafCategories);
    }

    console.log('\n✅ All done!');
    console.log(`   Category tree: marketplaces/eBay/${MARKETPLACE_ID}/data/meta/category_tree`);
    console.log(`   Aspects:       marketplaces/eBay/${MARKETPLACE_ID}/data/aspects/{categoryId}`);
    console.log(`   Jobs:          marketplaces/eBay/schema_jobs/`);

  } catch (err) {
    console.error(`\n✗ Fatal error: ${err.message}`);
    process.exit(1);
  } finally {
    process.exit(0);
  }
}

main();
