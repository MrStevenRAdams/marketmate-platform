// ============================================================================
// TEMU ONBOARDING WIZARD — 7-Step Flow (Approved Spec)
// ============================================================================
// Route: /temu-wizard
//
// Steps:
//   1. Connect — Primary source (Amazon, fallback to eBay) + Connect Temu
//   2. Cross-marketplace Upsell — Offer free listings on additional channels
//   3. Import — Single progress bar, enrichment runs inline
//   4. Download XLSX — Pricing spreadsheet from backend
//   5. Upload XLSX — Parse, validate, count Create Listing=Y rows (cap 100)
//   6. AI Generation — Generate Temu drafts for up to 100 products
//   7. Review & Push — Editable drafts, push to Temu
//
// After completion: redirect to Channel Command Centre (or Dashboard).
// Return behaviour: temu_wizard_stage on tenant tracks progress.
// ============================================================================

import { useState, useEffect, useCallback, useRef } from 'react';
import { useNavigate } from 'react-router-dom';
import { getActiveTenantId } from '../contexts/TenantContext';
import { useAuth } from '../contexts/AuthContext';

const API = import.meta.env.VITE_API_URL || 'http://localhost:8080/api/v1';
function api(path: string, init?: RequestInit) {
  return fetch(`${API}${path}`, {
    ...init,
    headers: { 'Content-Type': 'application/json', 'X-Tenant-Id': getActiveTenantId(), ...(init?.headers || {}) },
  });
}
function apiRaw(path: string, init?: RequestInit) {
  return fetch(`${API}${path}`, {
    ...init,
    headers: { 'X-Tenant-Id': getActiveTenantId(), ...(init?.headers || {}) },
  });
}

// ── Types ────────────────────────────────────────────────────────────────────

interface Credential {
  credential_id: string;
  channel: string;
  account_name: string;
  active: boolean;
}

interface ImportProgress {
  status: string;
  total_items: number;
  successful_items: number;
  failed_items: number;
  percent: number;
}

interface CreditBalance {
  free_remaining: number;
  free_limit: number;
  purchased: number;
  total: number;
}

interface TemuDraft {
  product_id: string;
  sku: string;
  title: string;
  source_title: string;
  temu_title: string;
  temu_description: string;
  temu_brand: string;
  temu_price: string;
  temu_category: string;
  source_image: string;
  status: 'pending' | 'ready' | 'submitted' | 'error';
  error?: string;
}

interface ChannelDraft {
  product_id: string;
  sku: string;
  channel: string;
  title: string;
  description: string;
  brand: string;
  price: string;
  category: string;
  image_url: string;
  status: string;
  bullet_points?: string[];
}

interface AllDraftsResponse {
  channels: string[];
  drafts: Record<string, ChannelDraft[]>;
  totals: Record<string, number>;
}

interface GenerationJob {
  job_id: string;
  status: 'queued' | 'running' | 'completed' | 'error';
  total: number;
  completed: number;
  credits_consumed: number;
  error?: string;
}

type WizardStep = 'connect' | 'upsell' | 'import' | 'download' | 'upload' | 'generate' | 'review';

// ── Styles ───────────────────────────────────────────────────────────────────

const pageStyle: React.CSSProperties = {
  minHeight: '100vh', display: 'flex', flexDirection: 'column',
  background: 'var(--bg-primary)',
};
const headerStyle: React.CSSProperties = {
  padding: '16px 32px', borderBottom: '1px solid var(--border)',
  display: 'flex', alignItems: 'center', justifyContent: 'space-between',
  background: 'var(--bg-secondary)',
};
const bodyStyle: React.CSSProperties = {
  flex: 1, maxWidth: 960, width: '100%', margin: '0 auto', padding: '32px 24px',
};
const cardStyle: React.CSSProperties = {
  background: 'var(--bg-secondary)', border: '1px solid var(--border)',
  borderRadius: 12, padding: '24px', marginBottom: 20,
};
const btnPrimary: React.CSSProperties = {
  padding: '10px 24px', borderRadius: 8, fontSize: 14, fontWeight: 600,
  background: '#F97316', border: 'none', color: '#fff', cursor: 'pointer',
};
const btnSecondary: React.CSSProperties = {
  padding: '10px 24px', borderRadius: 8, fontSize: 14, fontWeight: 600,
  background: 'var(--bg-tertiary)', border: '1px solid var(--border)',
  color: 'var(--text-secondary)', cursor: 'pointer',
};
const btnDisabled: React.CSSProperties = { ...btnPrimary, opacity: 0.5, cursor: 'not-allowed' };

const inputStyle: React.CSSProperties = {
  width: '100%', padding: '8px 12px', borderRadius: 8,
  border: '1px solid var(--border)', background: 'var(--bg-primary)',
  color: 'var(--text-primary)', fontSize: 13,
};

// ── Step indicator ───────────────────────────────────────────────────────────

const STEPS: { key: WizardStep; label: string }[] = [
  { key: 'connect', label: 'Connect' },
  { key: 'upsell', label: 'Channels' },
  { key: 'import', label: 'Import' },
  { key: 'download', label: 'Pricing' },
  { key: 'upload', label: 'Upload' },
  { key: 'generate', label: 'AI Generate' },
  { key: 'review', label: 'Review' },
];

function StepBar({ current }: { current: WizardStep }) {
  const idx = STEPS.findIndex(s => s.key === current);
  return (
    <div style={{ display: 'flex', alignItems: 'center', gap: 0, marginBottom: 28, overflowX: 'auto' }}>
      {STEPS.map((s, i) => (
        <div key={s.key} style={{ display: 'flex', alignItems: 'center', flexShrink: 0 }}>
          {i > 0 && <div style={{ width: 24, height: 2, background: i <= idx ? '#F97316' : 'var(--border)' }} />}
          <div style={{
            width: 26, height: 26, borderRadius: '50%', display: 'flex', alignItems: 'center', justifyContent: 'center',
            fontSize: 11, fontWeight: 700,
            background: i < idx ? '#F97316' : i === idx ? 'rgba(249,115,22,0.15)' : 'var(--bg-tertiary)',
            color: i < idx ? '#fff' : i === idx ? '#F97316' : 'var(--text-muted)',
            border: i === idx ? '2px solid #F97316' : '2px solid transparent',
          }}>
            {i < idx ? '✓' : i + 1}
          </div>
          <span style={{
            fontSize: 11, fontWeight: i === idx ? 600 : 400, marginLeft: 5, marginRight: 3,
            color: i === idx ? 'var(--text-primary)' : 'var(--text-muted)',
          }}>
            {s.label}
          </span>
        </div>
      ))}
    </div>
  );
}

// ── Progress Bar component ──────────────────────────────────────────────────

function ProgressBar({ percent, label }: { percent: number; label: string }) {
  return (
    <div style={{ marginBottom: 16 }}>
      <div style={{ display: 'flex', justifyContent: 'space-between', fontSize: 12, color: 'var(--text-muted)', marginBottom: 6 }}>
        <span>{label}</span>
        <span>{Math.round(percent)}%</span>
      </div>
      <div style={{ height: 8, borderRadius: 4, background: 'var(--bg-tertiary)', overflow: 'hidden' }}>
        <div style={{
          height: '100%', borderRadius: 4,
          background: 'linear-gradient(90deg, #F97316, #EA580C)',
          width: `${Math.min(percent, 100)}%`,
          transition: 'width 0.3s ease',
        }} />
      </div>
    </div>
  );
}

// ── Channel info ────────────────────────────────────────────────────────────

const CHANNEL_INFO: Record<string, { emoji: string; color: string; label: string }> = {
  amazon: { emoji: '📦', color: '#FF9900', label: 'Amazon' },
  ebay: { emoji: '🏷️', color: '#E53238', label: 'eBay' },
  temu: { emoji: '🛍️', color: '#F97316', label: 'Temu' },
  shopify: { emoji: '🛒', color: '#96BF48', label: 'Shopify' },
  etsy: { emoji: '🎨', color: '#F16521', label: 'Etsy' },
  tiktok: { emoji: '🎵', color: '#000000', label: 'TikTok Shop' },
  onbuy: { emoji: '🏪', color: '#0095DA', label: 'OnBuy' },
  walmart: { emoji: '🏬', color: '#0071CE', label: 'Walmart' },
};

// ============================================================================
// MAIN COMPONENT
// ============================================================================

export default function TemuWizard() {
  const navigate = useNavigate();
  const { activeTenant } = useAuth();

  // Wizard state
  const [step, setStep] = useState<WizardStep>('connect');
  const [loading, setLoading] = useState(true);

  // Step 1: Connect
  const [credentials, setCredentials] = useState<Credential[]>([]);
  const [sourceChannel, setSourceChannel] = useState<'amazon' | 'ebay'>('amazon');
  const [sourceCredId, setSourceCredId] = useState('');
  const [temuCredId, setTemuCredId] = useState('');
  const [noAmazon, setNoAmazon] = useState(false);

  // Step 2: Upsell
  const [additionalChannels, setAdditionalChannels] = useState<Set<string>>(new Set());
  const upsellChannels = ['shopify', 'etsy', 'tiktok', 'onbuy', 'walmart']
    .filter(ch => !credentials.some(c => c.channel === ch));

  // Step 3: Import
  const [importJobId, setImportJobId] = useState('');
  const [importProgress, setImportProgress] = useState<ImportProgress | null>(null);
  const [importDone, setImportDone] = useState(false);
  const [importError, setImportError] = useState('');
  const pollRef = useRef<ReturnType<typeof setInterval> | null>(null);

  // Step 4: Download
  const [xlsxUrl, setXlsxUrl] = useState('');
  const [xlsxGenerating, setXlsxGenerating] = useState(false);
  const [xlsxError, setXlsxError] = useState('');

  // Step 5: Upload
  const [uploadFile, setUploadFile] = useState<File | null>(null);
  const [uploadResult, setUploadResult] = useState<{ total: number; create_count: number; capped: boolean } | null>(null);
  const [uploading, setUploading] = useState(false);
  const [uploadError, setUploadError] = useState('');

  // Step 6: AI Generate
  const [genProgress, setGenProgress] = useState({ done: 0, total: 0 });
  const [generating, setGenerating] = useState(false);
  const [credits, setCredits] = useState<CreditBalance | null>(null);

  // Step 7: Review
  const [drafts, setDrafts] = useState<TemuDraft[]>([]);
  const [allDrafts, setAllDrafts] = useState<AllDraftsResponse | null>(null);
  const [activeReviewChannel, setActiveReviewChannel] = useState<string>('temu');
  const [submitting, setSubmitting] = useState(false);
  const [submitProgress, setSubmitProgress] = useState({ done: 0, total: 0 });
  const [channelSubmitProgress, setChannelSubmitProgress] = useState<Record<string, { done: number; total: number; submitting: boolean }>>({});
  const [channelCredIds, setChannelCredIds] = useState<Record<string, string>>({});

  // Async generation job
  const [genJobId, setGenJobId] = useState('');
  const [genJob, setGenJob] = useState<GenerationJob | null>(null);
  const jobPollRef = useRef<ReturnType<typeof setInterval> | null>(null);

  // ── Persist wizard stage to backend ─────────────────────────────────────
  const updateStage = useCallback(async (stage: string) => {
    try {
      await api('/temu-wizard/status', {
        method: 'PUT',
        body: JSON.stringify({ stage }),
      });
    } catch { /* non-fatal */ }
  }, []);

  // ── Load initial state ──────────────────────────────────────────────────
  useEffect(() => {
    const init = async () => {
      try {
        // Load credentials
        const credRes = await api('/marketplace/credentials');
        const credData = await credRes.json();
        const creds: Credential[] = (credData.data || credData.credentials || []).filter((c: Credential) => c.active !== false);
        setCredentials(creds);

        const amz = creds.find(c => c.channel === 'amazon');
        const eby = creds.find(c => c.channel === 'ebay');
        const tem = creds.find(c => c.channel === 'temu');
        if (amz) { setSourceChannel('amazon'); setSourceCredId(amz.credential_id); }
        else if (eby) { setSourceChannel('ebay'); setSourceCredId(eby.credential_id); setNoAmazon(true); }
        if (tem) setTemuCredId(tem.credential_id);
        // Build a lookup of channel → credential_id for additional channel submit
        const credMap: Record<string, string> = {};
        creds.forEach((c: Credential) => { credMap[c.channel] = c.credential_id; });
        setChannelCredIds(credMap);

        // Check if we should resume from a saved stage
        const statusRes = await api('/temu-wizard/status');
        if (statusRes.ok) {
          const statusData = await statusRes.json();
          const stage = statusData.stage;
          if (stage && stage !== 'completed') {
            const stageMap: Record<string, WizardStep> = {
              connected: 'upsell',
              importing: 'import',
              awaiting_upload: 'download',
              uploaded: 'generate',
              generating: 'generate',
              reviewing: 'review',
            };
            if (stageMap[stage]) setStep(stageMap[stage]);
          }
        }

        // Load credits
        const creditsRes = await api('/ai/credits');
        if (creditsRes.ok) {
          const creditsData = await creditsRes.json();
          setCredits(creditsData);
        }
      } catch { /* non-fatal */ }
      setLoading(false);
    };
    init();
  }, []);

  // Cleanup polling on unmount
  useEffect(() => {
    return () => {
      if (pollRef.current) clearInterval(pollRef.current);
      if (jobPollRef.current) clearInterval(jobPollRef.current);
    };
  }, []);

  // ── Derived state ───────────────────────────────────────────────────────
  const sourceCreds = credentials.filter(c => c.channel === 'amazon' || c.channel === 'ebay');
  const temuCreds = credentials.filter(c => c.channel === 'temu');
  const hasSource = sourceCreds.length > 0 && sourceCredId;
  const hasTemu = temuCreds.length > 0 && temuCredId;
  const canProceed1 = hasSource && hasTemu;

  // ── Step 1 handlers ─────────────────────────────────────────────────────
  const proceedFromConnect = () => {
    updateStage('connected');
    setStep('upsell');
  };

  // ── Step 2 handlers ─────────────────────────────────────────────────────
  const toggleUpsell = (ch: string) => {
    setAdditionalChannels(prev => {
      const next = new Set(prev);
      if (next.has(ch)) next.delete(ch); else next.add(ch);
      return next;
    });
  };

  const proceedFromUpsell = async () => {
    try {
      await api('/settings/selected-channels', {
        method: 'PUT',
        body: JSON.stringify({ channels: [sourceChannel, 'temu', ...Array.from(additionalChannels)] }),
      });
    } catch { /* non-fatal */ }
    startImport();
  };

  // ── Step 3: Import ──────────────────────────────────────────────────────
  const startImport = async () => {
    setStep('import');
    updateStage('importing');
    setImportError('');
    try {
      const res = await api('/import/products', {
        method: 'POST',
        body: JSON.stringify({
          channel: sourceChannel,
          channel_account_id: sourceCredId,
          job_type: 'full',
          sync_stock: true,
        }),
      });
      const data = await res.json();
      if (!res.ok) throw new Error(data.error || 'Failed to start import');
      setImportJobId(data.job_id);

      pollRef.current = setInterval(async () => {
        try {
          const pollRes = await api(`/import/products/${data.job_id}`);
          const pollData = await pollRes.json();
          const total = pollData.total_items || 0;
          const done = (pollData.successful_items || 0) + (pollData.failed_items || 0);
          const percent = total > 0 ? (done / total) * 100 : 0;

          setImportProgress({
            status: pollData.status,
            total_items: total,
            successful_items: pollData.successful_items || 0,
            failed_items: pollData.failed_items || 0,
            percent,
          });

          if (pollData.status === 'completed' || pollData.status === 'failed' || pollData.status === 'completed_with_errors') {
            if (pollRef.current) clearInterval(pollRef.current);
            pollRef.current = null;
            setImportDone(true);
            if (pollData.status === 'failed') {
              setImportError('Import failed. You can retry or skip to continue.');
            }
          }
        } catch { /* ignore poll errors */ }
      }, 2000);
    } catch (err: any) {
      setImportError(err.message || 'Failed to start import');
    }
  };

  const proceedFromImport = () => {
    updateStage('awaiting_upload');
    setStep('download');
  };

  // ── Step 4: Generate & Download XLSX ────────────────────────────────────
  const generateXlsx = async () => {
    setXlsxGenerating(true);
    setXlsxError('');
    try {
      const res = await api('/temu-wizard/generate-xlsx', {
        method: 'POST',
        body: JSON.stringify({
          source_channel: sourceChannel,
          credential_id: sourceCredId,
          additional_channels: Array.from(additionalChannels),
        }),
      });
      if (!res.ok) {
        const errData = await res.json();
        throw new Error(errData.error || 'Failed to generate spreadsheet');
      }
      const data = await res.json();
      setXlsxUrl(data.download_url || '');
    } catch (err: any) {
      setXlsxError(err.message);
    } finally {
      setXlsxGenerating(false);
    }
  };

  useEffect(() => {
    if (step === 'download' && !xlsxUrl && !xlsxGenerating) {
      generateXlsx();
    }
  }, [step]);

  // ── Step 5: Upload XLSX ─────────────────────────────────────────────────
  const handleUpload = async () => {
    if (!uploadFile) return;
    setUploading(true);
    setUploadError('');
    try {
      const formData = new FormData();
      formData.append('file', uploadFile);

      const res = await apiRaw('/temu-wizard/upload-xlsx', {
        method: 'POST',
        body: formData,
      });
      const data = await res.json();
      if (!res.ok) throw new Error(data.error || 'Upload failed');

      setUploadResult({
        total: data.total_rows || 0,
        create_count: data.create_count || 0,
        capped: (data.create_count || 0) > 100,
      });
      updateStage('uploaded');
    } catch (err: any) {
      setUploadError(err.message || 'Upload failed');
    } finally {
      setUploading(false);
    }
  };

  // ── Step 6: AI Generation (async) ──────────────────────────────────────
  const startGeneration = async () => {
    setGenerating(true);
    updateStage('generating');
    const cap = Math.min(uploadResult?.create_count || 0, 500);
    setGenProgress({ done: 0, total: cap });
    setGenJob(null);
    setGenJobId('');

    try {
      const res = await api('/temu-wizard/generate-listings-async', {
        method: 'POST',
        body: JSON.stringify({
          credential_id: temuCredId,
          max_products: cap,
          additional_channels: Array.from(additionalChannels),
        }),
      });
      const data = await res.json();
      if (!res.ok) throw new Error(data.error || 'Generation failed');

      const jobId: string = data.job_id;
      setGenJobId(jobId);

      // Poll the job every 2 seconds until completed or error.
      jobPollRef.current = setInterval(async () => {
        try {
          const pollRes = await api(`/temu-wizard/generation-job/${jobId}`);
          if (!pollRes.ok) return;
          const job: GenerationJob = await pollRes.json();
          setGenJob(job);
          setGenProgress({ done: job.completed, total: job.total || cap });

          if (job.status === 'completed' || job.status === 'error') {
            if (jobPollRef.current) clearInterval(jobPollRef.current);
            jobPollRef.current = null;
            setGenerating(false);

            if (job.status === 'error') {
              setUploadError(job.error || 'Generation job failed');
              return;
            }

            // Load temu drafts for the review step
            await loadAllDrafts();

            // Refresh credits
            const creditsRes = await api('/ai/credits');
            if (creditsRes.ok) setCredits(await creditsRes.json());

            updateStage('reviewing');
            setStep('review');
          }
        } catch { /* ignore transient poll errors */ }
      }, 2000);
    } catch (err: any) {
      setUploadError(err.message || 'Generation failed');
      setGenerating(false);
    }
  };

  // Load all-drafts (temu + additional channels) for Step 7
  const loadAllDrafts = async () => {
    try {
      const res = await api('/temu-wizard/all-drafts');
      if (!res.ok) return;
      const data: AllDraftsResponse = await res.json();
      setAllDrafts(data);
      setActiveReviewChannel(data.channels?.[0] || 'temu');

      // Populate legacy `drafts` array from temu channel for submit logic
      const temuChannel = data.drafts?.['temu'] || [];
      const mapped: TemuDraft[] = temuChannel.map((d: ChannelDraft) => ({
        product_id: d.product_id || '',
        sku: d.sku || '',
        title: d.title || '',
        source_title: d.title || '',
        temu_title: d.title || '',
        temu_description: d.description || '',
        temu_brand: d.brand || '',
        temu_price: d.price || '',
        temu_category: d.category || '',
        source_image: d.image_url || '',
        status: 'ready' as const,
      }));
      setDrafts(mapped);
    } catch { /* non-fatal */ }
  };

  // Load drafts when returning to review step
  useEffect(() => {
    if (step === 'review' && !allDrafts) {
      loadAllDrafts();
    }
  }, [step]);

  // ── Step 7: Submit to Temu ──────────────────────────────────────────────
  const readyDrafts = drafts.filter(d => d.status === 'ready');
  const errorDrafts = drafts.filter(d => d.status === 'error');
  const submittedDrafts = drafts.filter(d => d.status === 'submitted');

  const submitToTemu = async () => {
    if (readyDrafts.length === 0) return;
    setSubmitting(true);
    setSubmitProgress({ done: 0, total: readyDrafts.length });

    const updated = [...drafts];

    for (let i = 0; i < readyDrafts.length; i++) {
      const draft = readyDrafts[i];
      const idx = updated.findIndex(d => d.sku === draft.sku);
      setSubmitProgress({ done: i, total: readyDrafts.length });

      try {
        const res = await api('/temu/submit', {
          method: 'POST',
          body: JSON.stringify({
            product_id: draft.product_id,
            credential_id: temuCredId,
          }),
        });
        const data = await res.json();
        if (data.ok) {
          updated[idx] = { ...updated[idx], status: 'submitted' };
        } else {
          updated[idx] = { ...updated[idx], status: 'error', error: data.error || 'Submit failed' };
        }
      } catch (err: any) {
        updated[idx] = { ...updated[idx], status: 'error', error: err.message };
      }

      setDrafts([...updated]);
    }

    setSubmitProgress({ done: readyDrafts.length, total: readyDrafts.length });
    setSubmitting(false);
    updateStage('completed');
  };

  // ── Step 7: Submit additional channel drafts ─────────────────────────────
  const CHANNEL_SUBMIT_ENDPOINT: Record<string, string> = {
    amazon: '/amazon/submit',
    ebay:   '/ebay/submit',
  };

  const submitChannel = async (channel: string) => {
    if (!allDrafts) return;
    const channelDrafts = (allDrafts.drafts[channel] || []).filter(d => d.status !== 'error' && d.status !== 'submitted');
    if (channelDrafts.length === 0) return;

    const endpoint = CHANNEL_SUBMIT_ENDPOINT[channel];    if (!endpoint) {
      console.warn(`[TemuWizard] No submit endpoint for channel: ${channel}`);
      return;
    }

    setChannelSubmitProgress(prev => ({ ...prev, [channel]: { done: 0, total: channelDrafts.length, submitting: true } }));

    const updatedDrafts = { ...allDrafts.drafts };
    const channelList = [...(updatedDrafts[channel] || [])];

    for (let i = 0; i < channelDrafts.length; i++) {
      const draft = channelDrafts[i];
      const idx = channelList.findIndex(d => d.product_id === draft.product_id);
      setChannelSubmitProgress(prev => ({ ...prev, [channel]: { ...prev[channel], done: i } }));

      try {
        const credentialId = channelCredIds[channel] || '';
        const res = await api(endpoint, {
          method: 'POST',
          body: JSON.stringify({
            product_id: draft.product_id,
            sku:         draft.sku,
            credential_id: credentialId,
            draft,
          }),
        });
        const data = await res.json();
        if (data.ok) {
          if (idx >= 0) channelList[idx] = { ...channelList[idx], status: 'submitted' };
        } else {
          if (idx >= 0) channelList[idx] = { ...channelList[idx], status: 'error' };
        }
      } catch {
        if (idx >= 0) channelList[idx] = { ...channelList[idx], status: 'error' };
      }

      updatedDrafts[channel] = channelList;
      setAllDrafts(prev => prev ? { ...prev, drafts: { ...updatedDrafts } } : prev);
    }

    setChannelSubmitProgress(prev => ({
      ...prev,
      [channel]: { done: channelDrafts.length, total: channelDrafts.length, submitting: false },
    }));
  };

  const updateDraft = (sku: string, field: keyof TemuDraft, value: string) => {
    setDrafts(prev => prev.map(d => d.sku === sku ? { ...d, [field]: value } : d));
  };

  const finishWizard = () => {
    navigate('/dashboard');
  };

  // ══════════════════════════════════════════════════════════════════════════
  // RENDER
  // ══════════════════════════════════════════════════════════════════════════

  if (loading) {
    return (
      <div style={{ ...pageStyle, alignItems: 'center', justifyContent: 'center' }}>
        <span style={{ color: 'var(--text-muted)', fontSize: 14 }}>Loading...</span>
      </div>
    );
  }

  return (
    <div style={pageStyle}>
      {/* Header */}
      <div style={headerStyle}>
        <div style={{ display: 'flex', alignItems: 'center', gap: 12 }}>
          <div style={{
            width: 36, height: 36, borderRadius: 10,
            background: 'linear-gradient(135deg, #F97316, #EA580C)',
            display: 'flex', alignItems: 'center', justifyContent: 'center', fontSize: 20,
          }}>🛍️</div>
          <div>
            <div style={{ fontSize: 16, fontWeight: 700, color: 'var(--text-primary)' }}>List on Temu</div>
            <div style={{ fontSize: 11, color: 'var(--text-muted)' }}>
              Convert your existing products into Temu listings with AI
            </div>
          </div>
        </div>
        {credits && (
          <div style={{
            display: 'flex', alignItems: 'center', gap: 8, padding: '6px 14px',
            background: 'rgba(249,115,22,0.08)', borderRadius: 8, border: '1px solid rgba(249,115,22,0.2)',
          }}>
            <span style={{ fontSize: 13, fontWeight: 600, color: '#F97316' }}>
              {credits.total} credits
            </span>
            <span style={{ fontSize: 11, color: 'var(--text-muted)' }}>remaining</span>
          </div>
        )}
      </div>

      <div style={bodyStyle}>
        <StepBar current={step} />

        {/* ════════════════════════════════════════════════════════════════ */}
        {/* STEP 1: Connect Source + Temu                                  */}
        {/* ════════════════════════════════════════════════════════════════ */}
        {step === 'connect' && (
          <div>
            <h2 style={{ fontSize: 20, fontWeight: 700, color: 'var(--text-primary)', marginBottom: 6 }}>
              Connect your accounts
            </h2>
            <p style={{ fontSize: 13, color: 'var(--text-muted)', marginBottom: 24 }}>
              We'll import your product catalogue from your existing marketplace, then help you create Temu listings.
            </p>

            {/* Source channel */}
            <div style={cardStyle}>
              <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', marginBottom: 14 }}>
                <div style={{ fontSize: 13, fontWeight: 700, color: 'var(--text-muted)', textTransform: 'uppercase', letterSpacing: '0.5px' }}>
                  {noAmazon ? 'Source: eBay' : 'Source: Amazon (Primary)'}
                </div>
                {!noAmazon && !credentials.some(c => c.channel === 'amazon') && (
                  <button
                    onClick={() => setNoAmazon(true)}
                    style={{ background: 'none', border: 'none', color: 'var(--text-muted)', fontSize: 12, cursor: 'pointer', textDecoration: 'underline' }}
                  >
                    I don't have Amazon
                  </button>
                )}
              </div>

              {(() => {
                const channelFilter = noAmazon ? 'ebay' : 'amazon';
                const channelCreds = credentials.filter(c => c.channel === channelFilter);
                if (channelCreds.length > 0) {
                  return (
                    <div style={{ display: 'flex', flexDirection: 'column', gap: 8 }}>
                      {channelCreds.map(c => (
                        <label key={c.credential_id} style={{
                          display: 'flex', alignItems: 'center', gap: 12, padding: '12px 16px', borderRadius: 8, cursor: 'pointer',
                          background: sourceCredId === c.credential_id ? 'rgba(34,197,94,0.08)' : 'var(--bg-tertiary)',
                          border: sourceCredId === c.credential_id ? '2px solid #22c55e' : '2px solid var(--border)',
                        }}>
                          <input type="radio" name="source" checked={sourceCredId === c.credential_id}
                            onChange={() => { setSourceCredId(c.credential_id); setSourceChannel(c.channel as 'amazon' | 'ebay'); }}
                            style={{ accentColor: '#22c55e' }} />
                          <span style={{ fontSize: 18 }}>{CHANNEL_INFO[c.channel]?.emoji || '📦'}</span>
                          <div>
                            <div style={{ fontWeight: 600, fontSize: 14, color: 'var(--text-primary)' }}>{c.account_name || c.channel}</div>
                            <div style={{ fontSize: 12, color: 'var(--text-muted)' }}>{CHANNEL_INFO[c.channel]?.label || c.channel} — Connected</div>
                          </div>
                          <span style={{ marginLeft: 'auto', color: '#22c55e', fontSize: 12, fontWeight: 600 }}>✓</span>
                        </label>
                      ))}
                    </div>
                  );
                }
                return (
                  <div style={{ padding: 20, textAlign: 'center', background: 'var(--bg-tertiary)', borderRadius: 8, border: '1px dashed var(--border)' }}>
                    <p style={{ color: 'var(--text-muted)', fontSize: 13, marginBottom: 12 }}>
                      No {noAmazon ? 'eBay' : 'Amazon'} account connected yet.
                    </p>
                    <button onClick={() => navigate('/marketplace/connections')} style={btnPrimary}>
                      Connect {noAmazon ? 'eBay' : 'Amazon'} →
                    </button>
                  </div>
                );
              })()}
            </div>

            {/* Temu */}
            <div style={cardStyle}>
              <div style={{ fontSize: 13, fontWeight: 700, color: 'var(--text-muted)', textTransform: 'uppercase', letterSpacing: '0.5px', marginBottom: 14 }}>
                Temu Seller Account
              </div>
              {temuCreds.length > 0 ? (
                <div style={{ display: 'flex', flexDirection: 'column', gap: 8 }}>
                  {temuCreds.map(c => (
                    <label key={c.credential_id} style={{
                      display: 'flex', alignItems: 'center', gap: 12, padding: '12px 16px', borderRadius: 8, cursor: 'pointer',
                      background: temuCredId === c.credential_id ? 'rgba(249,115,22,0.08)' : 'var(--bg-tertiary)',
                      border: temuCredId === c.credential_id ? '2px solid #F97316' : '2px solid var(--border)',
                    }}>
                      <input type="radio" name="temu" checked={temuCredId === c.credential_id}
                        onChange={() => setTemuCredId(c.credential_id)}
                        style={{ accentColor: '#F97316' }} />
                      <span style={{ fontSize: 18 }}>🛍️</span>
                      <div>
                        <div style={{ fontWeight: 600, fontSize: 14, color: 'var(--text-primary)' }}>{c.account_name || 'Temu'}</div>
                        <div style={{ fontSize: 12, color: 'var(--text-muted)' }}>Temu — Connected</div>
                      </div>
                      <span style={{ marginLeft: 'auto', color: '#F97316', fontSize: 12, fontWeight: 600 }}>✓</span>
                    </label>
                  ))}
                </div>
              ) : (
                <div style={{ padding: 20, textAlign: 'center', background: 'var(--bg-tertiary)', borderRadius: 8, border: '1px dashed var(--border)' }}>
                  <p style={{ color: 'var(--text-muted)', fontSize: 13, marginBottom: 12 }}>No Temu seller account connected yet.</p>
                  <button onClick={() => navigate('/marketplace/connections')} style={btnPrimary}>
                    Connect Temu →
                  </button>
                </div>
              )}
            </div>

            <div style={{ display: 'flex', justifyContent: 'flex-end', marginTop: 24 }}>
              <button onClick={proceedFromConnect} disabled={!canProceed1}
                style={canProceed1 ? btnPrimary : btnDisabled}>
                Continue →
              </button>
            </div>
          </div>
        )}

        {/* ════════════════════════════════════════════════════════════════ */}
        {/* STEP 2: Cross-Marketplace Upsell                               */}
        {/* ════════════════════════════════════════════════════════════════ */}
        {step === 'upsell' && (
          <div>
            <h2 style={{ fontSize: 20, fontWeight: 700, color: 'var(--text-primary)', marginBottom: 6 }}>
              Sell on more channels — free!
            </h2>
            <p style={{ fontSize: 13, color: 'var(--text-muted)', marginBottom: 24, lineHeight: 1.6 }}>
              While we import your products, we can also prepare listings for other marketplaces at no extra cost.
              Select any channels you're interested in — we'll add pricing columns to your spreadsheet.
            </p>

            <div style={{ display: 'grid', gridTemplateColumns: 'repeat(auto-fill, minmax(200px, 1fr))', gap: 12, marginBottom: 24 }}>
              {upsellChannels.map(ch => {
                const info = CHANNEL_INFO[ch] || { emoji: '🏪', color: '#666', label: ch };
                const isSelected = additionalChannels.has(ch);
                return (
                  <button
                    key={ch}
                    onClick={() => toggleUpsell(ch)}
                    style={{
                      display: 'flex', alignItems: 'center', gap: 10, padding: '14px 16px',
                      borderRadius: 10, cursor: 'pointer', textAlign: 'left',
                      background: isSelected ? `${info.color}10` : 'var(--bg-secondary)',
                      border: isSelected ? `2px solid ${info.color}` : '2px solid var(--border)',
                      transition: 'all 0.15s ease',
                    }}
                  >
                    <span style={{ fontSize: 22 }}>{info.emoji}</span>
                    <div style={{ flex: 1 }}>
                      <div style={{ fontWeight: 600, fontSize: 13, color: 'var(--text-primary)' }}>{info.label}</div>
                      <div style={{ fontSize: 11, color: 'var(--text-muted)' }}>Free listing generation</div>
                    </div>
                    {isSelected && <span style={{ color: info.color, fontWeight: 700, fontSize: 16 }}>✓</span>}
                  </button>
                );
              })}
            </div>

            {additionalChannels.size > 0 && (
              <div style={{
                padding: '10px 16px', borderRadius: 8, marginBottom: 20,
                background: 'rgba(34,197,94,0.08)', border: '1px solid rgba(34,197,94,0.2)',
                fontSize: 13, color: '#22c55e',
              }}>
                Extra price columns for {Array.from(additionalChannels).map(ch => CHANNEL_INFO[ch]?.label || ch).join(', ')} will be added to your pricing spreadsheet.
              </div>
            )}

            <div style={{ display: 'flex', justifyContent: 'space-between' }}>
              <button onClick={() => setStep('connect')} style={btnSecondary}>← Back</button>
              <button onClick={proceedFromUpsell} style={btnPrimary}>
                {additionalChannels.size > 0 ? `Continue with ${additionalChannels.size + 2} channels →` : 'Skip — just Temu →'}
              </button>
            </div>
          </div>
        )}

        {/* ════════════════════════════════════════════════════════════════ */}
        {/* STEP 3: Import                                                 */}
        {/* ════════════════════════════════════════════════════════════════ */}
        {step === 'import' && (
          <div>
            <h2 style={{ fontSize: 20, fontWeight: 700, color: 'var(--text-primary)', marginBottom: 6 }}>
              Importing your products
            </h2>
            <p style={{ fontSize: 13, color: 'var(--text-muted)', marginBottom: 24 }}>
              We're pulling your {sourceChannel === 'amazon' ? 'Amazon' : 'eBay'} catalogue into MarketMate.
              Each product is fully enriched before continuing.
            </p>

            <div style={cardStyle}>
              {importError && !importDone ? (
                <div style={{ textAlign: 'center', padding: 20 }}>
                  <div style={{ color: '#ef4444', fontSize: 14, marginBottom: 16 }}>{importError}</div>
                  <button onClick={startImport} style={btnPrimary}>Retry Import</button>
                </div>
              ) : importProgress ? (
                <>
                  <ProgressBar
                    percent={importProgress.percent}
                    label={importDone
                      ? `Import complete — ${importProgress.successful_items} products imported`
                      : `Importing... ${importProgress.successful_items + importProgress.failed_items}/${importProgress.total_items}`
                    }
                  />
                  {importProgress.failed_items > 0 && (
                    <div style={{ fontSize: 12, color: '#ef4444', marginTop: 8 }}>
                      {importProgress.failed_items} product{importProgress.failed_items !== 1 ? 's' : ''} failed to import
                    </div>
                  )}
                </>
              ) : (
                <div style={{ textAlign: 'center', padding: 40, color: 'var(--text-muted)' }}>
                  <div style={{ fontSize: 28, marginBottom: 12 }}>📦</div>
                  Starting import...
                </div>
              )}
            </div>

            {importDone && (
              <div style={{ display: 'flex', justifyContent: 'flex-end', marginTop: 16 }}>
                <button onClick={proceedFromImport} style={btnPrimary}>
                  Continue — Set Pricing →
                </button>
              </div>
            )}
          </div>
        )}

        {/* ════════════════════════════════════════════════════════════════ */}
        {/* STEP 4: Download XLSX                                          */}
        {/* ════════════════════════════════════════════════════════════════ */}
        {step === 'download' && (
          <div>
            <h2 style={{ fontSize: 20, fontWeight: 700, color: 'var(--text-primary)', marginBottom: 6 }}>
              Download your pricing spreadsheet
            </h2>
            <p style={{ fontSize: 13, color: 'var(--text-muted)', marginBottom: 24, lineHeight: 1.6 }}>
              We've generated a spreadsheet with all your imported products. Fill in Temu prices, select Temu
              brands from the dropdown, and mark which products you want to list by setting "Create Listing" to Y.
            </p>

            <div style={cardStyle}>
              {xlsxGenerating ? (
                <div style={{ textAlign: 'center', padding: 40, color: 'var(--text-muted)' }}>
                  <div style={{ fontSize: 28, marginBottom: 12 }}>📊</div>
                  Generating spreadsheet...
                </div>
              ) : xlsxUrl ? (
                <div style={{ textAlign: 'center', padding: 20 }}>
                  <div style={{ fontSize: 40, marginBottom: 12 }}>✅</div>
                  <p style={{ fontSize: 14, fontWeight: 600, color: 'var(--text-primary)', marginBottom: 16 }}>
                    Your pricing spreadsheet is ready!
                  </p>
                  <a href={xlsxUrl} download style={{ ...btnPrimary, textDecoration: 'none', display: 'inline-block' }}>
                    ⬇ Download XLSX
                  </a>
                  <div style={{ fontSize: 12, color: 'var(--text-muted)', marginTop: 16, lineHeight: 1.6 }}>
                    Open the file in Excel or Google Sheets. Fill in the <strong>Temu Price</strong> column,
                    select a <strong>Temu Brand</strong> from the dropdown (Sheet 2 has the brand registry),
                    and set <strong>Create Listing</strong> to <strong>Y</strong> for products you want to list.
                    {additionalChannels.size > 0 && (
                      <span> Extra price columns for {Array.from(additionalChannels).map(ch => CHANNEL_INFO[ch]?.label || ch).join(', ')} are included too.</span>
                    )}
                  </div>
                </div>
              ) : xlsxError ? (
                <div style={{ textAlign: 'center', padding: 20, color: '#ef4444' }}>
                  <p>{xlsxError}</p>
                  <button onClick={generateXlsx} style={{ ...btnSecondary, marginTop: 12 }}>Retry</button>
                </div>
              ) : null}
            </div>

            {xlsxUrl && (
              <div style={{ display: 'flex', justifyContent: 'space-between', marginTop: 16 }}>
                <button onClick={() => setStep('import')} style={btnSecondary}>← Back</button>
                <button onClick={() => setStep('upload')} style={btnPrimary}>
                  I've filled it in — Upload →
                </button>
              </div>
            )}
          </div>
        )}

        {/* ════════════════════════════════════════════════════════════════ */}
        {/* STEP 5: Upload XLSX                                            */}
        {/* ════════════════════════════════════════════════════════════════ */}
        {step === 'upload' && (
          <div>
            <h2 style={{ fontSize: 20, fontWeight: 700, color: 'var(--text-primary)', marginBottom: 6 }}>
              Upload your completed spreadsheet
            </h2>
            <p style={{ fontSize: 13, color: 'var(--text-muted)', marginBottom: 24 }}>
              Upload the XLSX file with your pricing and brand selections. We'll process the products
              marked with Create Listing = Y (up to 100 free credits).
            </p>

            <div style={cardStyle}>
              {!uploadResult ? (
                <>
                  <div style={{
                    border: '2px dashed var(--border)', borderRadius: 10, padding: '40px 20px',
                    textAlign: 'center', marginBottom: 16,
                    background: uploadFile ? 'rgba(34,197,94,0.05)' : 'var(--bg-tertiary)',
                  }}>
                    {uploadFile ? (
                      <div>
                        <div style={{ fontSize: 28, marginBottom: 8 }}>📄</div>
                        <div style={{ fontSize: 14, fontWeight: 600, color: 'var(--text-primary)' }}>{uploadFile.name}</div>
                        <div style={{ fontSize: 12, color: 'var(--text-muted)', marginTop: 4 }}>
                          {(uploadFile.size / 1024).toFixed(1)} KB
                        </div>
                        <button
                          onClick={() => setUploadFile(null)}
                          style={{ ...btnSecondary, padding: '4px 12px', fontSize: 11, marginTop: 12 }}
                        >
                          Remove
                        </button>
                      </div>
                    ) : (
                      <div>
                        <div style={{ fontSize: 28, marginBottom: 8 }}>📤</div>
                        <label style={{ ...btnPrimary, display: 'inline-block', cursor: 'pointer' }}>
                          Choose File
                          <input
                            type="file"
                            accept=".xlsx,.xls"
                            onChange={e => setUploadFile(e.target.files?.[0] || null)}
                            style={{ display: 'none' }}
                          />
                        </label>
                        <p style={{ fontSize: 12, color: 'var(--text-muted)', marginTop: 12 }}>
                          Accepts .xlsx files
                        </p>
                      </div>
                    )}
                  </div>

                  {uploadError && (
                    <div style={{ color: '#ef4444', fontSize: 13, marginBottom: 12, padding: '8px 12px', background: 'rgba(239,68,68,0.08)', borderRadius: 8 }}>
                      {uploadError}
                    </div>
                  )}

                  <div style={{ display: 'flex', justifyContent: 'space-between' }}>
                    <button onClick={() => setStep('download')} style={btnSecondary}>← Back</button>
                    <button
                      onClick={handleUpload}
                      disabled={!uploadFile || uploading}
                      style={uploadFile && !uploading ? btnPrimary : btnDisabled}
                    >
                      {uploading ? 'Uploading...' : 'Upload & Validate →'}
                    </button>
                  </div>
                </>
              ) : (
                <>
                  <div style={{ textAlign: 'center', padding: 20 }}>
                    <div style={{ fontSize: 40, marginBottom: 12 }}>✅</div>
                    <p style={{ fontSize: 16, fontWeight: 700, color: 'var(--text-primary)', marginBottom: 8 }}>
                      Spreadsheet validated!
                    </p>
                    <div style={{ display: 'flex', justifyContent: 'center', gap: 24, marginBottom: 16 }}>
                      <div>
                        <div style={{ fontSize: 24, fontWeight: 700, color: 'var(--text-primary)' }}>{uploadResult.total}</div>
                        <div style={{ fontSize: 11, color: 'var(--text-muted)' }}>Total products</div>
                      </div>
                      <div>
                        <div style={{ fontSize: 24, fontWeight: 700, color: '#F97316' }}>
                          {Math.min(uploadResult.create_count, 100)}
                        </div>
                        <div style={{ fontSize: 11, color: 'var(--text-muted)' }}>To generate</div>
                      </div>
                    </div>
                    {uploadResult.capped && (
                      <div style={{
                        padding: '8px 16px', borderRadius: 8, fontSize: 12,
                        background: 'rgba(249,115,22,0.08)', border: '1px solid rgba(249,115,22,0.2)',
                        color: '#EA580C', marginBottom: 12,
                      }}>
                        You marked {uploadResult.create_count} products but your free credits cover 100.
                        We'll generate the first 100 — you can purchase more credits later.
                      </div>
                    )}
                  </div>

                  <div style={{ display: 'flex', justifyContent: 'space-between' }}>
                    <button onClick={() => { setUploadResult(null); setUploadFile(null); }} style={btnSecondary}>
                      ← Re-upload
                    </button>
                    <button onClick={() => setStep('generate')} style={btnPrimary}>
                      Generate {Math.min(uploadResult.create_count, 100)} Temu Listings →
                    </button>
                  </div>
                </>
              )}
            </div>
          </div>
        )}

        {/* ════════════════════════════════════════════════════════════════ */}
        {/* STEP 6: AI Generation                                          */}
        {/* ════════════════════════════════════════════════════════════════ */}
        {step === 'generate' && (
          <div>
            <h2 style={{ fontSize: 20, fontWeight: 700, color: 'var(--text-primary)', marginBottom: 6 }}>
              AI Listing Generation
            </h2>
            <p style={{ fontSize: 13, color: 'var(--text-muted)', marginBottom: 24 }}>
              Our AI will create optimised listings for Temu{additionalChannels.size > 0 ? ` and ${additionalChannels.size} additional channel${additionalChannels.size > 1 ? 's' : ''}` : ''} from your product data.
              Each listing uses 1 credit.
            </p>

            <div style={cardStyle}>
              {!generating && genProgress.done === 0 ? (
                <div style={{ textAlign: 'center', padding: 24 }}>
                  <div style={{ fontSize: 40, marginBottom: 16 }}>🤖</div>
                  <p style={{ fontSize: 14, color: 'var(--text-secondary)', marginBottom: 20 }}>
                    Ready to generate listings for up to {Math.min(uploadResult?.create_count || 0, 500)} products.
                  </p>
                  {additionalChannels.size > 0 && (
                    <p style={{ fontSize: 12, color: 'var(--text-muted)', marginBottom: 12 }}>
                      Also generating for: {Array.from(additionalChannels).map(ch => CHANNEL_INFO[ch]?.label || ch).join(', ')}
                    </p>
                  )}
                  {credits && (
                    <p style={{ fontSize: 12, color: 'var(--text-muted)', marginBottom: 20 }}>
                      Current balance: {credits.total} credits ({credits.free_remaining} free + {credits.purchased} purchased)
                    </p>
                  )}
                  <button onClick={startGeneration} style={btnPrimary}>
                    Start AI Generation →
                  </button>
                </div>
              ) : (
                <>
                  <ProgressBar
                    percent={genProgress.total > 0 ? (genProgress.done / genProgress.total) * 100 : (generating ? 5 : 100)}
                    label={generating
                      ? `Generating... ${genProgress.done}/${genProgress.total || '?'}`
                      : `Complete — ${genProgress.done} listings generated`
                    }
                  />
                  {genJob && (
                    <div style={{ display: 'flex', gap: 12, marginTop: 12, flexWrap: 'wrap' }}>
                      <span style={{ fontSize: 12, color: 'var(--text-muted)' }}>
                        Status: <strong style={{ color: genJob.status === 'error' ? '#ef4444' : 'var(--text-primary)' }}>{genJob.status}</strong>
                      </span>
                      <span style={{ fontSize: 12, color: 'var(--text-muted)' }}>
                        Credits used: <strong style={{ color: 'var(--text-primary)' }}>{genJob.credits_consumed}</strong>
                      </span>
                      {genJob.error && (
                        <span style={{ fontSize: 12, color: '#ef4444' }}>{genJob.error}</span>
                      )}
                    </div>
                  )}
                  {generating && (
                    <p style={{ fontSize: 12, color: 'var(--text-muted)', textAlign: 'center', marginTop: 8 }}>
                      Running in the background — you can safely leave this tab open.
                    </p>
                  )}
                </>
              )}
            </div>
          </div>
        )}

        {/* ════════════════════════════════════════════════════════════════ */}
        {/* STEP 7: Review & Push                                          */}
        {/* ════════════════════════════════════════════════════════════════ */}
        {step === 'review' && (
          <div>
            <h2 style={{ fontSize: 20, fontWeight: 700, color: 'var(--text-primary)', marginBottom: 6 }}>
              Review your listings
            </h2>
            <p style={{ fontSize: 13, color: 'var(--text-muted)', marginBottom: 12 }}>
              Edit titles, descriptions, and prices before pushing to each marketplace.
            </p>

            {/* Channel tabs */}
            {allDrafts && allDrafts.channels.length > 1 && (
              <div style={{ display: 'flex', gap: 8, marginBottom: 16, flexWrap: 'wrap' }}>
                {allDrafts.channels.map(ch => {
                  const info = CHANNEL_INFO[ch];
                  const count = allDrafts.totals[ch] || 0;
                  const isActive = activeReviewChannel === ch;
                  return (
                    <button
                      key={ch}
                      onClick={() => setActiveReviewChannel(ch)}
                      style={{
                        display: 'flex', alignItems: 'center', gap: 6,
                        padding: '7px 14px', borderRadius: 8, fontSize: 13, fontWeight: 600,
                        border: isActive ? `2px solid ${info?.color || '#F97316'}` : '2px solid var(--border)',
                        background: isActive ? `${info?.color || '#F97316'}18` : 'var(--bg-tertiary)',
                        color: isActive ? (info?.color || '#F97316') : 'var(--text-secondary)',
                        cursor: 'pointer',
                      }}
                    >
                      <span>{info?.emoji || '🏪'}</span>
                      <span>{info?.label || ch}</span>
                      <span style={{
                        fontSize: 11, padding: '1px 6px', borderRadius: 10,
                        background: isActive ? (info?.color || '#F97316') : 'var(--border)',
                        color: isActive ? '#fff' : 'var(--text-muted)',
                      }}>{count}</span>
                    </button>
                  );
                })}
              </div>
            )}

            {/* Stats for active channel */}
            {(() => {
              const channelDrafts = allDrafts?.drafts[activeReviewChannel] || [];
              const ready = activeReviewChannel === 'temu' ? readyDrafts : channelDrafts.filter(d => d.status !== 'error');
              const errors = activeReviewChannel === 'temu' ? errorDrafts : channelDrafts.filter(d => d.status === 'error');
              const submitted = activeReviewChannel === 'temu' ? submittedDrafts : [];
              return (
                <>
                  <div style={{ display: 'flex', gap: 12, marginBottom: 20 }}>
                    <div style={{
                      padding: '8px 16px', borderRadius: 8, fontSize: 13, fontWeight: 600,
                      background: 'rgba(34,197,94,0.1)', border: '1px solid rgba(34,197,94,0.3)', color: '#22c55e',
                    }}>
                      ✓ {ready.length} ready
                    </div>
                    {errors.length > 0 && (
                      <div style={{
                        padding: '8px 16px', borderRadius: 8, fontSize: 13, fontWeight: 600,
                        background: 'rgba(239,68,68,0.1)', border: '1px solid rgba(239,68,68,0.3)', color: '#ef4444',
                      }}>
                        ✕ {errors.length} failed
                      </div>
                    )}
                    {submitted.length > 0 && (
                      <div style={{
                        padding: '8px 16px', borderRadius: 8, fontSize: 13, fontWeight: 600,
                        background: 'rgba(99,102,241,0.1)', border: '1px solid rgba(99,102,241,0.3)', color: '#818cf8',
                      }}>
                        ↑ {submitted.length} submitted
                      </div>
                    )}
                  </div>

                  {/* Draft list */}
                  <div style={{ background: 'var(--bg-secondary)', border: '1px solid var(--border)', borderRadius: 12, overflow: 'hidden' }}>
                    {activeReviewChannel === 'temu' ? (
                      // Temu tab — editable inline fields
                      drafts.length === 0 ? (
                        <div style={{ padding: 40, textAlign: 'center', color: 'var(--text-muted)' }}>
                          No listings generated yet.
                        </div>
                      ) : drafts.map((draft, i) => (
                        <div key={draft.sku || i} style={{
                          padding: '14px 18px',
                          borderBottom: i < drafts.length - 1 ? '1px solid var(--border)' : 'none',
                          opacity: draft.status === 'error' ? 0.6 : 1,
                        }}>
                          <div style={{ display: 'flex', alignItems: 'flex-start', gap: 14 }}>
                            {draft.source_image && (
                              <img src={draft.source_image} alt="" style={{
                                width: 48, height: 48, objectFit: 'cover', borderRadius: 6,
                                flexShrink: 0, background: 'var(--bg-tertiary)',
                              }} />
                            )}
                            <div style={{ flex: 1, minWidth: 0 }}>
                              <div style={{ fontSize: 11, color: 'var(--text-muted)', marginBottom: 4 }}>
                                {draft.sku} — {draft.source_title}
                              </div>
                              {draft.status === 'error' ? (
                                <div style={{
                                  fontSize: 12, color: '#ef4444', background: 'rgba(239,68,68,0.08)',
                                  padding: '6px 10px', borderRadius: 6,
                                }}>
                                  {draft.error}
                                </div>
                              ) : (
                                <div style={{ display: 'flex', flexDirection: 'column', gap: 6 }}>
                                  <input
                                    value={draft.temu_title}
                                    onChange={e => updateDraft(draft.sku, 'temu_title', e.target.value)}
                                    style={inputStyle}
                                    placeholder="Temu listing title"
                                  />
                                  <div style={{ display: 'flex', gap: 8, alignItems: 'center' }}>
                                    <span style={{
                                      fontSize: 11, color: 'var(--text-muted)', padding: '4px 8px',
                                      background: 'var(--bg-tertiary)', borderRadius: 4,
                                    }}>
                                      {draft.temu_brand || 'No brand'} · {draft.temu_category || 'No category'}
                                    </span>
                                    <input
                                      value={draft.temu_price}
                                      onChange={e => updateDraft(draft.sku, 'temu_price', e.target.value)}
                                      style={{ ...inputStyle, width: 80, textAlign: 'right' }}
                                      placeholder="Price"
                                    />
                                  </div>
                                </div>
                              )}
                            </div>
                            <div style={{
                              padding: '3px 10px', borderRadius: 6, fontSize: 11, fontWeight: 600, flexShrink: 0,
                              background: draft.status === 'ready' ? 'rgba(34,197,94,0.1)' :
                                draft.status === 'submitted' ? 'rgba(99,102,241,0.1)' : 'rgba(239,68,68,0.1)',
                              color: draft.status === 'ready' ? '#22c55e' :
                                draft.status === 'submitted' ? '#818cf8' : '#ef4444',
                            }}>
                              {draft.status === 'ready' ? 'Ready' : draft.status === 'submitted' ? 'Submitted' : 'Error'}
                            </div>
                          </div>
                        </div>
                      ))
                    ) : (
                      // Additional channel tab — read-only preview with channel badge
                      channelDrafts.length === 0 ? (
                        <div style={{ padding: 40, textAlign: 'center', color: 'var(--text-muted)' }}>
                          No {CHANNEL_INFO[activeReviewChannel]?.label || activeReviewChannel} listings generated.
                        </div>
                      ) : channelDrafts.map((draft, i) => {
                        const info = CHANNEL_INFO[activeReviewChannel];
                        return (
                          <div key={draft.sku || i} style={{
                            padding: '14px 18px',
                            borderBottom: i < channelDrafts.length - 1 ? '1px solid var(--border)' : 'none',
                          }}>
                            <div style={{ display: 'flex', alignItems: 'flex-start', gap: 14 }}>
                              {draft.image_url && (
                                <img src={draft.image_url} alt="" style={{
                                  width: 48, height: 48, objectFit: 'cover', borderRadius: 6,
                                  flexShrink: 0, background: 'var(--bg-tertiary)',
                                }} />
                              )}
                              <div style={{ flex: 1, minWidth: 0 }}>
                                <div style={{ fontSize: 11, color: 'var(--text-muted)', marginBottom: 4 }}>
                                  {draft.sku}
                                </div>
                                <div style={{ fontSize: 13, fontWeight: 600, color: 'var(--text-primary)', marginBottom: 4 }}>
                                  {draft.title}
                                </div>
                                {draft.description && (
                                  <div style={{ fontSize: 12, color: 'var(--text-secondary)', marginBottom: 4 }}>
                                    {draft.description.slice(0, 120)}{draft.description.length > 120 ? '…' : ''}
                                  </div>
                                )}
                                <div style={{ display: 'flex', gap: 8, alignItems: 'center', flexWrap: 'wrap' }}>
                                  {draft.brand && (
                                    <span style={{ fontSize: 11, color: 'var(--text-muted)', padding: '2px 8px', background: 'var(--bg-tertiary)', borderRadius: 4 }}>
                                      {draft.brand}
                                    </span>
                                  )}
                                  {draft.category && (
                                    <span style={{ fontSize: 11, color: 'var(--text-muted)', padding: '2px 8px', background: 'var(--bg-tertiary)', borderRadius: 4 }}>
                                      {draft.category}
                                    </span>
                                  )}
                                  {draft.price && (
                                    <span style={{ fontSize: 12, fontWeight: 700, color: info?.color || '#F97316' }}>
                                      £{draft.price}
                                    </span>
                                  )}
                                </div>
                              </div>
                              <div style={{
                                padding: '3px 10px', borderRadius: 6, fontSize: 11, fontWeight: 600, flexShrink: 0,
                                background: `${info?.color || '#F97316'}18`,
                                color: info?.color || '#F97316',
                                border: `1px solid ${info?.color || '#F97316'}44`,
                              }}>
                                {info?.emoji} {info?.label || activeReviewChannel}
                              </div>
                            </div>
                          </div>
                        );
                      })
                    )}
                  </div>
                </>
              );
            })()}

            <div style={{ display: 'flex', justifyContent: 'space-between', marginTop: 24 }}>
              <button onClick={() => setStep('generate')} style={btnSecondary}>← Back</button>
              {activeReviewChannel === 'temu' ? (
                submittedDrafts.length === drafts.filter(d => d.status !== 'error').length && submittedDrafts.length > 0 ? (
                  <button onClick={finishWizard} style={btnPrimary}>
                    Go to Dashboard →
                  </button>
                ) : (
                  <button
                    onClick={submitToTemu}
                    disabled={readyDrafts.length === 0 || submitting}
                    style={readyDrafts.length > 0 && !submitting ? btnPrimary : btnDisabled}
                  >
                    {submitting
                      ? `Submitting (${submitProgress.done}/${submitProgress.total})...`
                      : `Push ${readyDrafts.length} Listing${readyDrafts.length !== 1 ? 's' : ''} to Temu →`}
                  </button>
                )
              ) : (() => {
                const chProgress = channelSubmitProgress[activeReviewChannel];
                const chDrafts = allDrafts?.drafts[activeReviewChannel] || [];
                const pushable = chDrafts.filter(d => d.status !== 'error' && d.status !== 'submitted');
                const allSubmitted = chDrafts.length > 0 && chDrafts.every(d => d.status === 'submitted');
                const channelLabel = CHANNEL_INFO[activeReviewChannel]?.label || activeReviewChannel;
                const hasEndpoint = !!CHANNEL_SUBMIT_ENDPOINT[activeReviewChannel];

                if (allSubmitted) {
                  return (
                    <button onClick={finishWizard} style={btnPrimary}>
                      Go to Dashboard →
                    </button>
                  );
                }
                if (!hasEndpoint) {
                  return (
                    <button onClick={finishWizard} style={btnPrimary}>
                      Go to Dashboard →
                    </button>
                  );
                }
                return (
                  <button
                    onClick={() => submitChannel(activeReviewChannel)}
                    disabled={pushable.length === 0 || chProgress?.submitting}
                    style={pushable.length > 0 && !chProgress?.submitting ? btnPrimary : btnDisabled}
                  >
                    {chProgress?.submitting
                      ? `Submitting (${chProgress.done}/${chProgress.total})…`
                      : `Push ${pushable.length} Listing${pushable.length !== 1 ? 's' : ''} to ${channelLabel} →`}
                  </button>
                );
              })()}
            </div>
          </div>
        )}
      </div>
    </div>
  );
}
