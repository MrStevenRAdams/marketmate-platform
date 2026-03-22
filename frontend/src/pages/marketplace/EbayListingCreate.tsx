// ============================================================================
// EBAY LISTING PAGE — Full listing creation/update form
// ============================================================================
// Location: frontend/src/pages/marketplace/EbayListingCreate.tsx
// Arrives with ?product_id=xxx&credential_id=yyy from product edit page.
// Single form view matching AmazonListingCreate pattern.

import { useState, useEffect, useCallback, useRef, useMemo } from 'react';
import { useSearchParams, useNavigate } from 'react-router-dom';
import {
  ebayApi, EbayDraft, EbaySubmitResponse, CategorySuggestion,
  ItemAspect, FulfillmentPolicy, PaymentPolicy, ReturnPolicy, InventoryLocation,
  CatalogProduct, ChannelVariantDraft,
} from '../../services/ebay-api';
import ConfiguratorSelector from './ConfiguratorSelector';
import { configuratorService, ConfiguratorDetail } from '../../services/configurator-api';
import { listingService } from '../../services/marketplace-api';

const EBAY_BLUE = '#0064D2';

const CONDITION_OPTIONS = [
  { value: 'NEW', label: 'New' }, { value: 'NEW_OTHER', label: 'New (Other)' },
  { value: 'NEW_WITH_DEFECTS', label: 'New with Defects' }, { value: 'LIKE_NEW', label: 'Like New / Open Box' },
  { value: 'USED_EXCELLENT', label: 'Used — Excellent' }, { value: 'USED_VERY_GOOD', label: 'Used — Very Good' },
  { value: 'USED_GOOD', label: 'Used — Good' }, { value: 'USED_ACCEPTABLE', label: 'Used — Acceptable' },
  { value: 'REFURBISHED', label: 'Certified Refurbished' }, { value: 'FOR_PARTS_OR_NOT_WORKING', label: 'For Parts / Not Working' },
];

const FORMAT_OPTIONS = [
  { value: 'FIXED_PRICE', label: 'Fixed Price (Buy It Now)' },
  { value: 'AUCTION', label: 'Auction' },
];

const DURATION_OPTIONS = [
  { value: 'GTC', label: "Good 'Til Cancelled" }, { value: 'DAYS_3', label: '3 Days' },
  { value: 'DAYS_5', label: '5 Days' }, { value: 'DAYS_7', label: '7 Days' },
  { value: 'DAYS_10', label: '10 Days' }, { value: 'DAYS_30', label: '30 Days' },
];

const MARKETPLACE_OPTIONS = [
  { value: 'EBAY_GB', label: '🇬🇧 eBay UK' }, { value: 'EBAY_US', label: '🇺🇸 eBay US' },
  { value: 'EBAY_DE', label: '🇩🇪 eBay Germany' }, { value: 'EBAY_FR', label: '🇫🇷 eBay France' },
  { value: 'EBAY_IT', label: '🇮🇹 eBay Italy' }, { value: 'EBAY_ES', label: '🇪🇸 eBay Spain' },
  { value: 'EBAY_AU', label: '🇦🇺 eBay Australia' }, { value: 'EBAY_CA', label: '🇨🇦 eBay Canada' },
];

const MARKETPLACE_CURRENCY: Record<string, string> = {
  EBAY_GB: 'GBP', EBAY_US: 'USD', EBAY_DE: 'EUR', EBAY_FR: 'EUR',
  EBAY_IT: 'EUR', EBAY_ES: 'EUR', EBAY_AU: 'AUD', EBAY_CA: 'CAD',
};
const CURRENCY_SYMBOL: Record<string, string> = { GBP: '£', USD: '$', EUR: '€', CAD: 'C$', AUD: 'A$' };
const DIM_UNITS = [{ value: 'CENTIMETER', label: 'cm' }, { value: 'INCH', label: 'in' }];
const WT_UNITS = [{ value: 'KILOGRAM', label: 'kg' }, { value: 'GRAM', label: 'g' }, { value: 'POUND', label: 'lb' }, { value: 'OUNCE', label: 'oz' }];
const PKG_TYPES = [{ value: 'LETTER', label: 'Letter' }, { value: 'LARGE_ENVELOPE', label: 'Large Envelope' }, { value: 'PACKAGE_THICK_ENVELOPE', label: 'Package / Thick Envelope' }, { value: 'LARGE_PACKAGE', label: 'Large Package' }, { value: 'EXTRA_LARGE_PACKAGE', label: 'Extra Large Package' }];

// ============================================================================
// COMPONENT
// ============================================================================

export default function EbayListingCreate() {
  const navigate = useNavigate();
  const [searchParams] = useSearchParams();
  const productId = searchParams.get('product_id');
  const credentialId = searchParams.get('credential_id') || '';

  const [loading, setLoading] = useState(true);
  const [error, setError] = useState('');
  const [draft, setDraft] = useState<EbayDraft | null>(null);
  const [categorySuggestions, setCategorySuggestions] = useState<CategorySuggestion[]>([]);
  const [categorySearchQuery, setCategorySearchQuery] = useState('');
  const [categorySearching, setCategorySearching] = useState(false);
  const [showCategorySearch, setShowCategorySearch] = useState(false);
  const [itemAspects, setItemAspects] = useState<ItemAspect[]>([]);
  const [aspectsLoading, setAspectsLoading] = useState(false);
  const [fulfillmentPolicies, setFulfillmentPolicies] = useState<FulfillmentPolicy[]>([]);
  const [paymentPolicies, setPaymentPolicies] = useState<PaymentPolicy[]>([]);
  const [returnPolicies, setReturnPolicies] = useState<ReturnPolicy[]>([]);
  const [locations, setLocations] = useState<InventoryLocation[]>([]);
  const [debugErrors, setDebugErrors] = useState<string[]>([]);
  const [submitting, setSubmitting] = useState(false);
  const [submitResult, setSubmitResult] = useState<EbaySubmitResponse | null>(null);
  const [publishOnSubmit, setPublishOnSubmit] = useState(true);
  const [aiGenerating, setAiGenerating] = useState(false);
  const [aiApplied, setAiApplied] = useState(false);
  const [aiError, setAiError] = useState('');

  // ── Catalog Lookup (FLD-09) ──
  const [catalogOpen, setCatalogOpen] = useState(false);
  const [catalogQuery, setCatalogQuery] = useState('');
  const [catalogSearching, setCatalogSearching] = useState(false);
  const [catalogResults, setCatalogResults] = useState<CatalogProduct[]>([]);
  const catalogDebounceRef = useRef<ReturnType<typeof setTimeout> | null>(null);

  // ── GPSR section (FLD-07) ──
  const [gpsrOpen, setGpsrOpen] = useState(false);

  // ── FLD-15: SKU duplicate detection ─────────────────────────────────────
  const [skuDuplicateChannels, setSkuDuplicateChannels] = useState<string[]>([]);

  // ── Configurator (CFG-07) ──
  const [selectedConfigurator, setSelectedConfigurator] = useState<ConfiguratorDetail | null>(null);

  // VAR-01 — Variation listings
  const [variants, setVariants] = useState<ChannelVariantDraft[]>([]);
  const [isVariantProduct, setIsVariantProduct] = useState(false);
  const [editingVariant, setEditingVariant] = useState<ChannelVariantDraft | null>(null);
  const updateVariant = (id: string, field: keyof ChannelVariantDraft, value: any) => {
    setVariants(vs => vs.map(v => v.id === id ? { ...v, [field]: value } : v));
  };

  const handleConfiguratorSelect = (cfg: ConfiguratorDetail | null) => {
    setSelectedConfigurator(cfg);
    if (!cfg) return;
    // Pre-populate category
    if (cfg.category_id && cfg.category_path && draft) {
      setDraft(d => d ? { ...d, categoryId: cfg.category_id!, categoryName: cfg.category_path! } : d);
    }
    // Pre-populate aspect/attribute defaults
    if (cfg.attribute_defaults && cfg.attribute_defaults.length > 0 && draft) {
      const extraAspects: Record<string, string[]> = {};
      for (const attr of cfg.attribute_defaults) {
        if (attr.source === 'default_value' && attr.default_value) {
          extraAspects[attr.attribute_name] = [attr.default_value];
        }
      }
      if (Object.keys(extraAspects).length > 0) {
        setDraft(d => d ? { ...d, aspects: { ...(d.aspects || {}), ...extraAspects } } : d);
      }
    }
    // Pre-populate fulfillment policy
    if (cfg.shipping_defaults?.fulfillment_policy_id && draft) {
      setDraft(d => d ? { ...d, fulfillmentPolicyId: cfg.shipping_defaults!.fulfillment_policy_id } : d);
    }
  };

  // ── Catalog search handler (FLD-09) ──
  const handleCatalogSearch = (query: string) => {
    setCatalogQuery(query);
    if (catalogDebounceRef.current) clearTimeout(catalogDebounceRef.current);
    if (query.length < 2) { setCatalogResults([]); return; }
    catalogDebounceRef.current = setTimeout(async () => {
      setCatalogSearching(true);
      try {
        const res = await ebayApi.catalogSearch({ q: query, marketplace: mp });
        setCatalogResults(res.data?.products || []);
      } catch { setCatalogResults([]); }
      setCatalogSearching(false);
    }, 500);
  };

  const handleCatalogSelect = (product: CatalogProduct) => {
    setDraft(d => d ? { ...d, epid: product.epid } : d);
    setCatalogOpen(false);
    setCatalogQuery('');
    setCatalogResults([]);
  };

  // ── Pricing tier helpers (FLD-10) ──
  const getPricingTiers = () => (draft as any)?.pricingTiers || [];
  const setPricingTiers = (tiers: { minQty: number; pricePerUnit: string }[]) =>
    setDraft(d => d ? { ...d, pricingTiers: tiers } : d);
  const addPricingTier = () => setPricingTiers([...getPricingTiers(), { minQty: 2, pricePerUnit: '' }]);
  const removePricingTier = (idx: number) => setPricingTiers(getPricingTiers().filter((_: any, i: number) => i !== idx));
  const updatePricingTier = (idx: number, field: 'minQty' | 'pricePerUnit', value: string | number) =>
    setPricingTiers(getPricingTiers().map((t: any, i: number) => i === idx ? { ...t, [field]: value } : t));

  const debounceRef = useRef<ReturnType<typeof setTimeout> | null>(null);

  const mp = draft?.marketplaceId || 'EBAY_GB';
  const cs = CURRENCY_SYMBOL[draft?.currency || MARKETPLACE_CURRENCY[mp] || 'GBP'] || '£';

  // ── Prepare ──
  useEffect(() => {
    if (!productId) { setError('No product_id provided.'); setLoading(false); return; }
    prepareListing(productId);
  }, [productId]);

  async function prepareListing(pid: string) {
    setLoading(true); setError('');
    try {
      const payload: any = { product_id: pid };
      if (credentialId) payload.credential_id = credentialId;
      const res = await ebayApi.prepare(payload);
      const data = res.data as any;
      if (!data?.ok) { setError(data?.error || 'Failed to prepare listing'); setLoading(false); return; }
      if (data.draft) {
        setDraft({
          ...data.draft,
          // FLD-12 defaults
          clickAndCollectEnabled: data.draft.clickAndCollectEnabled ?? false,
          pickupLeadTimeDays: data.draft.pickupLeadTimeDays ?? 0,
          pickupDropOffEnabled: data.draft.pickupDropOffEnabled ?? false,
          // FLD-01 / FLD-02 defaults
          bulletPoints: data.draft.bulletPoints ?? [],
          shortDescription: data.draft.shortDescription ?? '',
          paymentMethods: data.draft.paymentMethods ?? [],
        });
        // VAR-01: load variants
        if (data.draft.variants && data.draft.variants.length > 0) {
          setVariants(data.draft.variants);
          setIsVariantProduct(true);
        }
      }
      if (data.categorySuggestions) setCategorySuggestions(data.categorySuggestions);
      if (data.itemAspects) setItemAspects(data.itemAspects);
      if (data.fulfillmentPolicies) setFulfillmentPolicies(data.fulfillmentPolicies);
      if (data.paymentPolicies) setPaymentPolicies(data.paymentPolicies);
      if (data.returnPolicies) setReturnPolicies(data.returnPolicies);
      if (data.locations) setLocations(data.locations);
      if (data.debugErrors) setDebugErrors(data.debugErrors);

      // ── AI Generation: if ?ai=pending, call AI with item aspects as schema ──
      const aiFlag = searchParams.get('ai');
      if (aiFlag === 'pending' && data.draft && data.itemAspects) {
        setAiGenerating(true);
        try {
          const schemaFields: import('../../services/ai-api').SchemaField[] = [];
          for (const aspect of (data.itemAspects || [])) {
            schemaFields.push({
              name: aspect.localizedAspectName || aspect.aspectName || '',
              display_name: aspect.localizedAspectName || '',
              data_type: aspect.aspectValues ? 'enum' : 'string',
              required: aspect.aspectConstraint?.aspectRequired || false,
              allowed_values: (aspect.aspectValues || []).map((v: any) => v.localizedValue || v),
              max_length: aspect.aspectConstraint?.itemToAspectCardinality === 'SINGLE' ? 0 : 0,
            });
          }

          const categoryName = data.draft.categoryName || '';
          const categoryId = data.draft.categoryId || '';

          const { aiService: aiApi } = await import('../../services/ai-api');
          const aiRes = await aiApi.generateWithSchema({
            product_id: pid,
            channel: 'ebay',
            category_id: categoryId,
            category_name: categoryName,
            fields: schemaFields,
          });

          const aiListing = aiRes.data.data?.listings?.[0];
          if (aiListing) {
            setDraft((prev: any) => {
              if (!prev) return prev;
              const updated = { ...prev };
              if (aiListing.title) updated.title = aiListing.title;
              if (aiListing.description) updated.description = aiListing.description;
              if (aiListing.attributes) {
                updated.itemSpecifics = { ...updated.itemSpecifics };
                for (const [key, val] of Object.entries(aiListing.attributes)) {
                  updated.itemSpecifics[key] = String(val);
                }
              }
              return updated;
            });
            setAiApplied(true);
          }
        } catch (aiErr: any) {
          setAiError(aiErr.response?.data?.error || aiErr.message || 'AI generation failed');
        }
        setAiGenerating(false);
      }
      if (data.categorySuggestions) setCategorySuggestions(data.categorySuggestions);
      if (data.itemAspects) setItemAspects(data.itemAspects);
      if (data.fulfillmentPolicies) setFulfillmentPolicies(data.fulfillmentPolicies);
      if (data.paymentPolicies) setPaymentPolicies(data.paymentPolicies);
      if (data.returnPolicies) setReturnPolicies(data.returnPolicies);
      if (data.locations) setLocations(data.locations);
      if (data.debugErrors) setDebugErrors(data.debugErrors);
    } catch (err: any) { setError(err?.response?.data?.error || err.message || 'Network error'); }
    setLoading(false);
  };

  // ── Category search ──
  const handleCategorySearch = useCallback(async (query: string) => {
    setCategorySearchQuery(query);
    if (debounceRef.current) clearTimeout(debounceRef.current);
    if (query.length < 2) return;
    debounceRef.current = setTimeout(async () => {
      setCategorySearching(true);
      try {
        const res = await ebayApi.suggestCategories(query, mp);
        if (res.data?.ok) setCategorySuggestions(res.data.suggestions || []);
      } catch (err) { console.warn('[eBay] Category search failed:', err); }
      setCategorySearching(false);
    }, 400);
  }, [mp]);

  const selectCategory = async (cat: CategorySuggestion) => {
    if (!draft) return;
    setDraft(d => d ? { ...d, categoryId: cat.category.categoryId, categoryName: cat.category.categoryName } : null);
    setShowCategorySearch(false);
    setAspectsLoading(true);
    try {
      const res = await ebayApi.getItemAspects(cat.category.categoryId, mp);
      if (res.data?.ok) setItemAspects(res.data.aspects || []);
    } catch (err) { console.warn('[eBay] Fetch aspects failed:', err); }
    setAspectsLoading(false);
  };

  // ── Helpers ──
  const updateDraft = (field: string, value: any) => setDraft(d => d ? { ...d, [field]: value } : null);

  // FLD-15: Check for duplicate SKU across channels on blur
  const checkSKUDuplicate = async (sku: string) => {
    if (!sku.trim()) { setSkuDuplicateChannels([]); return; }
    try {
      const apiBase = (import.meta.env.VITE_API_URL || 'http://localhost:8080/api/v1');
      const { getActiveTenantId } = await import('../../contexts/TenantContext');
      const res = await fetch(`${apiBase}/listings/check-sku?sku=${encodeURIComponent(sku)}`, {
        headers: { 'X-Tenant-Id': getActiveTenantId() || '' },
      });
      if (res.ok) {
        const data = await res.json();
        setSkuDuplicateChannels(data.isDuplicate ? (data.existingChannels || []) : []);
      }
    } catch { /* non-blocking */ }
  };

  const updateAspect = (name: string, values: string[]) => {
    setDraft(d => {
      if (!d) return null;
      const aspects = { ...d.aspects };
      if (values.length === 0 || (values.length === 1 && values[0] === '')) { delete aspects[name]; } else { aspects[name] = values; }
      return { ...d, aspects };
    });
  };
  const getAspectValue = (name: string): string => draft?.aspects?.[name]?.[0] || '';
  const handleMarketplaceChange = (newMp: string) => setDraft(d => d ? { ...d, marketplaceId: newMp, currency: MARKETPLACE_CURRENCY[newMp] || 'GBP' } : null);
  const addImage = (url: string) => { if (draft && url.trim()) setDraft(d => d ? { ...d, images: [...d.images, url.trim()] } : null); };
  const removeImage = (idx: number) => setDraft(d => d ? { ...d, images: d.images.filter((_, i) => i !== idx) } : null);
  const moveImage = (idx: number, dir: -1 | 1) => {
    if (!draft) return;
    const imgs = [...draft.images]; const ni = idx + dir;
    if (ni < 0 || ni >= imgs.length) return;
    [imgs[idx], imgs[ni]] = [imgs[ni], imgs[idx]];
    setDraft(d => d ? { ...d, images: imgs } : null);
  };

  const canSubmit = useMemo(() => {
    if (!draft) return false;
    return !!(draft.title && draft.sku && draft.categoryId && draft.price && draft.images.length > 0 &&
      (draft.fulfillmentPolicyId || fulfillmentPolicies.length === 0) &&
      (draft.paymentPolicyId || paymentPolicies.length === 0) &&
      (draft.returnPolicyId || returnPolicies.length === 0));
  }, [draft, fulfillmentPolicies, paymentPolicies, returnPolicies]);

  const handleSubmit = async () => {
    if (!draft || !productId) return;
    setSubmitting(true); setSubmitResult(null);
    try {
      const res = await ebayApi.submit({ product_id: productId, credential_id: credentialId, draft: { ...draft, variants }, publish: publishOnSubmit });
      setSubmitResult(res.data);
      // ── Configurator join (CFG-07) ──
      if (res.data?.ok && selectedConfigurator) {
        try {
          const listRes = await listingService.list({ product_id: productId!, channel: 'ebay', limit: 10 });
          const listings: any[] = listRes.data?.listings || listRes.data?.data || [];
          if (listings.length > 0) {
            const newest = listings[listings.length - 1];
            await configuratorService.assignListings(selectedConfigurator.configurator_id, [newest.listing_id]);
          }
        } catch { /* non-fatal */ }
      }
    } catch (err: any) { setSubmitResult({ ok: false, error: err?.response?.data?.error || err.message }); }
    setSubmitting(false);
  };

  const { requiredAspects, recommendedAspects, optionalAspects, filledRequired, filledRecommended } = useMemo(() => {
    const req = itemAspects.filter(a => a.aspectConstraint.aspectRequired);
    const rec = itemAspects.filter(a => !a.aspectConstraint.aspectRequired && a.aspectConstraint.aspectUsage === 'RECOMMENDED');
    const opt = itemAspects.filter(a => !a.aspectConstraint.aspectRequired && a.aspectConstraint.aspectUsage !== 'RECOMMENDED');
    return { requiredAspects: req, recommendedAspects: rec, optionalAspects: opt,
      filledRequired: req.filter(a => draft?.aspects?.[a.localizedAspectName]?.length).length,
      filledRecommended: rec.filter(a => draft?.aspects?.[a.localizedAspectName]?.length).length };
  }, [itemAspects, draft?.aspects]);

  // ── Render: Loading / Error / Success ──
  if (loading) return (
    <div style={{ maxWidth: 900, margin: '0 auto', padding: '24px 16px', textAlign: 'center' }}>
      <div style={{ fontSize: 24, marginBottom: 8 }}>⏳</div>
      <p style={{ color: 'var(--text-secondary)' }}>Preparing eBay listing...</p>
      <p style={{ color: 'var(--text-muted)', fontSize: 12 }}>Loading product data, categories, policies, and item specifics</p>
    </div>
  );

  if (!draft && error) return (
    <div style={{ maxWidth: 900, margin: '0 auto', padding: '24px 16px' }}>
      <div style={{ padding: 16, background: 'var(--danger-glow)', borderRadius: 8, color: 'var(--danger)' }}>{error}</div>
      <button onClick={() => navigate(-1)} style={{ ...secondaryBtnStyle, marginTop: 16 }}>← Go Back</button>
    </div>
  );

  if (submitResult?.ok) return (
    <div style={{ maxWidth: 900, margin: '0 auto', padding: '24px 16px', textAlign: 'center' }}>
      <div style={{ fontSize: 48, marginBottom: 16 }}>✅</div>
      <h2 style={{ fontSize: 20, fontWeight: 700, marginBottom: 8 }}>{draft?.isUpdate ? 'Updated on eBay!' : 'Submitted to eBay!'}</h2>
      <p style={{ color: 'var(--text-secondary)' }}>SKU: <strong>{draft?.sku}</strong></p>
      {submitResult.listingId && <p style={{ color: 'var(--text-muted)', fontSize: 13 }}>eBay Item #: {submitResult.listingId}</p>}
      {submitResult.offerId && <p style={{ color: 'var(--text-muted)', fontSize: 13 }}>Offer ID: {submitResult.offerId}</p>}
      <p style={{ color: 'var(--text-muted)', fontSize: 13, marginBottom: 24 }}>
        {publishOnSubmit && submitResult.listingId ? 'Published and live on eBay' : 'Saved as draft offer (not published)'}
      </p>
      {submitResult.warnings && submitResult.warnings.length > 0 && (
        <div style={{ padding: 12, background: 'var(--warning-glow)', borderRadius: 8, color: 'var(--warning)', fontSize: 13, marginBottom: 16, textAlign: 'left' }}>
          <strong>Warnings:</strong>{submitResult.warnings.map((w, i) => <div key={i}>• {w}</div>)}
        </div>
      )}
      {submitResult.listingId && (
        <a href={`https://www.ebay.co.uk/itm/${submitResult.listingId}`} target="_blank" rel="noopener noreferrer"
          style={{ ...primaryBtnStyle, display: 'inline-block', textDecoration: 'none', marginBottom: 16 }}>View on eBay ↗</a>
      )}
      <div style={{ display: 'flex', gap: 12, justifyContent: 'center', marginTop: 16 }}>
        <button onClick={() => navigate(-1)} style={secondaryBtnStyle}>← Back to Product</button>
        <button onClick={() => navigate('/marketplace/listings')} style={primaryBtnStyle}>View Listings</button>
      </div>
    </div>
  );

  // ── Render: Main Form ──
  return (
    <div style={{ maxWidth: 900, margin: '0 auto', padding: '24px 16px' }}>

      {/* Header */}
      <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', marginBottom: 24 }}>
        <div style={{ display: 'flex', alignItems: 'center', gap: 12 }}>
          <button onClick={() => navigate(-1)} style={backBtnStyle}>← Back</button>
          <h1 style={{ fontSize: 22, fontWeight: 700, margin: 0 }}>eBay Listing</h1>
          <span style={{ fontSize: 12, padding: '2px 8px', borderRadius: 4, background: EBAY_BLUE, color: '#fff', fontWeight: 700 }}>Inventory API</span>
          {draft?.isUpdate && <span style={{ fontSize: 12, padding: '2px 8px', borderRadius: 4, background: 'var(--warning)', color: '#000', fontWeight: 700 }}>UPDATE</span>}
        </div>
        <div style={{ display: 'flex', gap: 8, alignItems: 'center' }}>
          <label style={{ display: 'flex', alignItems: 'center', gap: 6, fontSize: 12, color: 'var(--text-secondary)', cursor: 'pointer' }}>
            <input type="checkbox" checked={publishOnSubmit} onChange={e => setPublishOnSubmit(e.target.checked)} /> Publish immediately
          </label>
          <button onClick={handleSubmit} disabled={!canSubmit || submitting}
            style={{ ...primaryBtnStyle, opacity: canSubmit && !submitting ? 1 : 0.5 }}>
            {submitting ? '⏳ Submitting...' : draft?.isUpdate ? '🔄 Update' : '🚀 Submit to eBay'}
          </button>
        </div>
      </div>

      {/* Error/info banners */}
      {error && <div style={{ padding: 12, background: 'var(--danger-glow)', borderRadius: 8, color: 'var(--danger)', marginBottom: 16, fontSize: 13 }}>{error}</div>}
      {submitResult && !submitResult.ok && <div style={{ padding: 12, background: 'var(--danger-glow)', borderRadius: 8, color: 'var(--danger)', marginBottom: 16, fontSize: 13 }}>{submitResult.error || 'Submission failed'}</div>}
      {draft?.isUpdate && (
        <div style={{ padding: '8px 12px', background: 'rgba(59,130,246,0.1)', borderRadius: 8, fontSize: 12, color: 'var(--primary-light)', marginBottom: 16 }}>
          ℹ️ Existing listing found — this will update the inventory item and offer.
          {draft.existingListingId && <> Item #: <strong>{draft.existingListingId}</strong></>}
          {draft.existingOfferId && <> • Offer: <strong>{draft.existingOfferId}</strong></>}
        </div>
      )}
      {debugErrors.length > 0 && (() => {
        const filteredErrors = debugErrors.filter(e => !e.includes('not eligible for Business Policy'));
        return filteredErrors.length > 0 ? (
          <div style={{ padding: '8px 12px', background: 'var(--warning-glow)', borderRadius: 8, fontSize: 12, color: 'var(--warning)', marginBottom: 16 }}>
            <strong>⚠️ Prepare warnings:</strong>{filteredErrors.map((e, i) => <div key={i}>• {e}</div>)}
          </div>
        ) : null;
      })()}

      {/* AI-generated content banner */}
      {aiGenerating && (
        <div style={{ padding: '12px 14px', background: 'linear-gradient(90deg, rgba(139,92,246,0.15), rgba(59,130,246,0.15))', borderRadius: 8, fontSize: 13, color: 'var(--accent-purple)', marginBottom: 16, display: 'flex', gap: 10, alignItems: 'center', border: '1px solid rgba(139,92,246,0.3)' }}>
          <div className="spinner" style={{ width: 16, height: 16, border: '2px solid var(--accent-purple)', borderTopColor: 'transparent', borderRadius: '50%', animation: 'spin 1s linear infinite' }} />
          <span style={{ fontWeight: 600 }}>🤖 AI is generating optimised content using eBay item aspects...</span>
        </div>
      )}
      {aiApplied && (
        <div style={{ padding: '10px 14px', background: 'linear-gradient(90deg, rgba(139,92,246,0.15), rgba(59,130,246,0.15))', borderRadius: 8, fontSize: 12, color: 'var(--accent-purple)', marginBottom: 16, display: 'flex', gap: 8, alignItems: 'center', border: '1px solid rgba(139,92,246,0.3)' }}>
          <span style={{ fontSize: 16 }}>🤖</span>
          <span style={{ fontWeight: 600 }}>AI-generated content applied</span>
          <span style={{ color: 'var(--text-muted)' }}>— title, description and item specifics have been filled. Review and edit before submitting.</span>
        </div>
      )}
      {aiError && (
        <div style={{ padding: '10px 14px', background: 'var(--warning-glow)', borderRadius: 8, fontSize: 12, color: 'var(--warning)', marginBottom: 16, border: '1px solid var(--warning)' }}>
          ⚠️ AI generation failed: {aiError} — fill in fields manually.
        </div>
      )}

      {draft && <div style={{ display: 'flex', flexDirection: 'column', gap: 16 }}>
        {/* ── Configurator (CFG-07) ── */}
        <ConfiguratorSelector channel="ebay" credentialId={credentialId} onSelect={handleConfiguratorSelect} />
        {/* SECTION: Marketplace & Identification */}
        <Section title="Marketplace & Identification" accent={EBAY_BLUE}>
          <div style={{ display: 'flex', gap: 12, flexWrap: 'wrap' }}>
            <div style={{ flex: 1, minWidth: 160 }}><label style={labelStyle}>Marketplace *</label>
              <select value={draft.marketplaceId} onChange={e => handleMarketplaceChange(e.target.value)} style={inputStyle}>
                {MARKETPLACE_OPTIONS.map(o => <option key={o.value} value={o.value}>{o.label}</option>)}
              </select></div>
            <div style={{ flex: 1, minWidth: 140 }}><label style={labelStyle}>SKU *</label>
              <input value={draft.sku} onChange={e => updateDraft('sku', e.target.value)} onBlur={e => checkSKUDuplicate(e.target.value)} style={inputStyle} maxLength={50} />
              {skuDuplicateChannels.length > 0 && (
                <div style={{ marginTop: 4, padding: '6px 10px', borderRadius: 6, background: 'rgba(245,158,11,0.1)', border: '1px solid rgba(245,158,11,0.4)', fontSize: 12, color: '#b45309' }}>
                  ⚠ SKU already listed on: {skuDuplicateChannels.join(', ')}
                </div>
              )}
            </div>
            <div style={{ flex: 1, minWidth: 120 }}><label style={labelStyle}>Condition *</label>
              <select value={draft.condition} onChange={e => updateDraft('condition', e.target.value)} style={inputStyle}>
                {CONDITION_OPTIONS.map(o => <option key={o.value} value={o.value}>{o.label}</option>)}
              </select></div>
          </div>
          {draft.condition !== 'NEW' && (
            <div style={{ marginTop: 8 }}><label style={labelStyle}>Condition Description</label>
              <textarea value={draft.conditionDescription} onChange={e => updateDraft('conditionDescription', e.target.value)}
                style={{ ...inputStyle, minHeight: 60, resize: 'vertical' }} placeholder="Describe the condition..." maxLength={1000} /></div>
          )}
        </Section>

        {/* SECTION: Core Fields */}
        <Section title="Core Fields" accent={EBAY_BLUE} subtitle="Title, description, brand, identifiers">
          <div style={{ marginBottom: 8 }}><label style={labelStyle}>Title * <span style={{ color: 'var(--text-muted)', fontWeight: 400 }}>({draft.title.length}/80)</span>
            {draft.title.length > 80 && <span style={{ color: 'var(--danger)', marginLeft: 8 }}>⚠ Over limit</span>}</label>
            <input value={draft.title} onChange={e => updateDraft('title', e.target.value)}
              style={{ ...inputStyle, borderColor: draft.title.length > 80 ? 'var(--danger)' : undefined }} maxLength={80} /></div>
          <div style={{ marginBottom: 8 }}><label style={labelStyle}>Subtitle <span style={{ color: 'var(--text-muted)', fontWeight: 400 }}>(optional — fee applies)</span></label>
            <input value={draft.subtitle} onChange={e => updateDraft('subtitle', e.target.value)} style={inputStyle} maxLength={55} /></div>
          <div style={{ marginBottom: 8 }}>
            <label style={labelStyle}>Description *</label>
            <EbayDescriptionEditor value={draft.description} onChange={val => updateDraft('description', val)} inputStyle={inputStyle} />
          </div>

          {/* FLD-02 — Bullet Points */}
          <div style={{ marginBottom: 8 }}>
            <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', marginBottom: 6 }}>
              <div>
                <label style={{ ...labelStyle, marginBottom: 0 }}>Bullet Points <span style={{ fontWeight: 400, color: 'var(--text-muted)' }}>(up to 5 — prepended to description)</span></label>
              </div>
              {((draft as any).bulletPoints || []).length < 5 && (
                <button onClick={() => updateDraft('bulletPoints', [...((draft as any).bulletPoints || []), ''])}
                  style={{ fontSize: 11, padding: '3px 10px', borderRadius: 5, border: `1px solid ${EBAY_BLUE}`, background: 'transparent', color: EBAY_BLUE, cursor: 'pointer', fontWeight: 600 }}>
                  + Add Bullet
                </button>
              )}
            </div>
            {((draft as any).bulletPoints || []).length === 0 && (
              <p style={{ fontSize: 12, color: 'var(--text-muted)', margin: '0 0 4px' }}>No bullet points — buyers will see the description only.</p>
            )}
            {((draft as any).bulletPoints || []).map((bp: string, i: number) => (
              <div key={i} style={{ display: 'flex', gap: 6, alignItems: 'center', marginBottom: 5 }}>
                <span style={{ fontSize: 12, color: 'var(--text-muted)', minWidth: 18 }}>•</span>
                <input
                  value={bp}
                  onChange={e => { const bps = [...((draft as any).bulletPoints || [])]; bps[i] = e.target.value; updateDraft('bulletPoints', bps); }}
                  style={{ ...inputStyle, flex: 1 }}
                  placeholder={`Bullet point ${i + 1}`}
                  maxLength={200}
                />
                <button onClick={() => updateDraft('bulletPoints', ((draft as any).bulletPoints || []).filter((_: string, idx: number) => idx !== i))}
                  style={{ background: 'none', border: 'none', color: 'var(--danger)', cursor: 'pointer', fontSize: 18, padding: '0 4px', lineHeight: 1 }}>×</button>
              </div>
            ))}
          </div>

          {/* FLD-02 — Short Description (mobile-first summary) */}
          <div style={{ marginBottom: 8 }}>
            <label style={labelStyle}>Short Description <span style={{ fontWeight: 400, color: 'var(--text-muted)' }}>(mobile summary — stored in listing record, up to 200 chars)</span></label>
            <input
              value={(draft as any).shortDescription || ''}
              onChange={e => updateDraft('shortDescription', e.target.value)}
              style={inputStyle}
              placeholder="Brief one-line summary for mobile views"
              maxLength={200}
            />
          </div>
          <div style={{ display: 'flex', gap: 12, flexWrap: 'wrap' }}>
            <div style={{ flex: 1, minWidth: 140 }}><label style={labelStyle}>Brand</label>
              <input value={draft.brand} onChange={e => { updateDraft('brand', e.target.value); updateAspect('Brand', e.target.value ? [e.target.value] : []); }} style={inputStyle} /></div>
            <div style={{ flex: 1, minWidth: 140 }}><label style={labelStyle}>MPN</label>
              <input value={draft.mpn} onChange={e => { updateDraft('mpn', e.target.value); updateAspect('MPN', e.target.value ? [e.target.value] : []); }} style={inputStyle} /></div>
            <div style={{ flex: 1, minWidth: 100 }}><label style={labelStyle}>EAN</label>
              <input value={draft.ean} onChange={e => updateDraft('ean', e.target.value)} style={inputStyle} /></div>
            <div style={{ flex: 1, minWidth: 100 }}><label style={labelStyle}>UPC</label>
              <input value={draft.upc} onChange={e => updateDraft('upc', e.target.value)} style={inputStyle} /></div>
            <div style={{ flex: 1, minWidth: 100 }}><label style={labelStyle}>ISBN</label>
              <input value={draft.isbn} onChange={e => updateDraft('isbn', e.target.value)} style={inputStyle} /></div>
          </div>
          {/* eBay Catalog EPID (FLD-09) */}
          <div style={{ marginTop: 10, display: 'flex', gap: 10, alignItems: 'center', flexWrap: 'wrap' }}>
            <div style={{ flex: 1, minWidth: 240 }}>
              <label style={labelStyle}>eBay Product ID (EPID) <span style={{ color: 'var(--text-muted)', fontWeight: 400 }}>— links listing to eBay catalogue</span></label>
              <input
                value={(draft as any).epid || ''}
                onChange={e => updateDraft('epid', e.target.value)}
                style={inputStyle}
                placeholder="e.g. 241040876 — or use the catalogue search below"
              />
            </div>
            <div style={{ paddingTop: 20 }}>
              <button
                onClick={() => { setCatalogOpen(true); setCatalogQuery(draft.title || ''); if (draft.title) handleCatalogSearch(draft.title); }}
                style={{ padding: '10px 14px', borderRadius: 8, border: `1px solid ${EBAY_BLUE}`, background: 'transparent', color: EBAY_BLUE, fontWeight: 600, fontSize: 13, cursor: 'pointer', whiteSpace: 'nowrap' }}
              >
                🔍 Match to eBay Catalogue
              </button>
            </div>
          </div>
        </Section>

        {/* SECTION: Category & Item Specifics */}
        <Section title="Category & Item Specifics" accent="#F5AF02"
          subtitle={draft.categoryId ? `${draft.categoryName} (${draft.categoryId})` : 'No category selected'}>
          <div style={{ marginBottom: 12 }}>
            <div style={{ display: 'flex', gap: 8, alignItems: 'center', marginBottom: 8 }}>
              <label style={{ ...labelStyle, marginBottom: 0 }}>Primary Category *</label>
              {draft.categoryId && <span style={{ fontSize: 12, padding: '2px 8px', borderRadius: 4, background: 'rgba(16,185,129,0.1)', color: 'var(--success)' }}>✓ {draft.categoryName}</span>}
              <button onClick={() => setShowCategorySearch(!showCategorySearch)}
                style={{ fontSize: 11, padding: '2px 8px', borderRadius: 4, border: '1px solid var(--border)', background: 'transparent', color: 'var(--text-muted)', cursor: 'pointer' }}>
                {showCategorySearch ? 'Cancel' : 'Change Category'}</button>
            </div>
            {showCategorySearch && (
              <div style={{ marginBottom: 8 }}>
                <input value={categorySearchQuery} onChange={e => handleCategorySearch(e.target.value)} style={inputStyle} placeholder="Search eBay categories..." autoFocus />
                {categorySearching && <p style={{ fontSize: 11, color: 'var(--text-muted)', marginTop: 4 }}>Searching...</p>}
                {categorySuggestions.length > 0 && (
                  <div style={{ maxHeight: 200, overflow: 'auto', border: '1px solid var(--border)', borderRadius: 6, marginTop: 4 }}>
                    {categorySuggestions.map((s, i) => (
                      <div key={i} onClick={() => selectCategory(s)}
                        style={{ padding: '8px 12px', cursor: 'pointer', borderBottom: '1px solid var(--border)', fontSize: 13, color: 'var(--text-primary)',
                          background: s.category.categoryId === draft.categoryId ? 'rgba(0,100,210,0.1)' : 'transparent' }}
                        onMouseEnter={e => (e.currentTarget.style.background = 'var(--bg-tertiary)')}
                        onMouseLeave={e => (e.currentTarget.style.background = s.category.categoryId === draft.categoryId ? 'rgba(0,100,210,0.1)' : 'transparent')}>
                        <strong>{s.category.categoryName}</strong>
                        <span style={{ color: 'var(--text-muted)', marginLeft: 8, fontSize: 11 }}>#{s.category.categoryId}</span>
                      </div>
                    ))}
                  </div>
                )}
              </div>
            )}
            <div style={{ marginTop: 8 }}><label style={labelStyle}>Secondary Category <span style={{ color: 'var(--text-muted)', fontWeight: 400 }}>(optional — fee)</span></label>
              <input value={draft.secondaryCategoryId} onChange={e => updateDraft('secondaryCategoryId', e.target.value)} style={inputStyle} placeholder="Category ID" /></div>
          </div>
          {/* Item Specifics */}
          {aspectsLoading ? <p style={{ fontSize: 12, color: 'var(--text-muted)' }}>Loading item specifics...</p> : itemAspects.length > 0 && (
            <div>
              <div style={{ display: 'flex', gap: 16, marginBottom: 12, fontSize: 12 }}>
                <span style={{ color: filledRequired >= requiredAspects.length ? 'var(--success)' : 'var(--warning)' }}>Required: {filledRequired}/{requiredAspects.length}</span>
                <span style={{ color: 'var(--text-muted)' }}>Recommended: {filledRecommended}/{recommendedAspects.length}</span>
              </div>
              {requiredAspects.length > 0 && <div style={{ marginBottom: 12 }}>
                <h4 style={{ fontSize: 12, fontWeight: 700, color: 'var(--warning)', marginBottom: 8 }}>Required Item Specifics</h4>
                <div style={{ display: 'flex', flexDirection: 'column', gap: 6 }}>
                  {requiredAspects.map(a => <AspectField key={a.localizedAspectName} aspect={a} value={getAspectValue(a.localizedAspectName)} values={draft.aspects[a.localizedAspectName] || []} onChange={vals => updateAspect(a.localizedAspectName, vals)} tag="REQ" tagColor="var(--warning)" />)}
                </div></div>}
              {recommendedAspects.length > 0 && <Subsection title={`Recommended (${recommendedAspects.length})`} defaultOpen={false}>
                <div style={{ display: 'flex', flexDirection: 'column', gap: 6 }}>
                  {recommendedAspects.map(a => <AspectField key={a.localizedAspectName} aspect={a} value={getAspectValue(a.localizedAspectName)} values={draft.aspects[a.localizedAspectName] || []} onChange={vals => updateAspect(a.localizedAspectName, vals)} tag="REC" tagColor="var(--primary-light)" />)}
                </div></Subsection>}
              {optionalAspects.length > 0 && <Subsection title={`Optional (${optionalAspects.length})`} defaultOpen={false}>
                <div style={{ display: 'flex', flexDirection: 'column', gap: 6 }}>
                  {optionalAspects.map(a => <AspectField key={a.localizedAspectName} aspect={a} value={getAspectValue(a.localizedAspectName)} values={draft.aspects[a.localizedAspectName] || []} onChange={vals => updateAspect(a.localizedAspectName, vals)} tag="OPT" tagColor="var(--text-muted)" />)}
                </div></Subsection>}
            </div>
          )}
        </Section>

        {/* SECTION: Pricing & Format */}
        <Section title="Pricing & Format" accent="#10b981">
          <div style={{ display: 'flex', gap: 12, flexWrap: 'wrap', marginBottom: 8 }}>
            <div style={{ flex: 1, minWidth: 160 }}><label style={labelStyle}>Listing Format *</label>
              <select value={draft.listingFormat} onChange={e => updateDraft('listingFormat', e.target.value)} style={inputStyle}>
                {FORMAT_OPTIONS.map(o => <option key={o.value} value={o.value}>{o.label}</option>)}</select></div>
            <div style={{ flex: 1, minWidth: 120 }}><label style={labelStyle}>{draft.listingFormat === 'AUCTION' ? `Starting Bid (${cs})` : `Price (${cs})`} *</label>
              <input value={draft.price} onChange={e => updateDraft('price', e.target.value)} style={inputStyle} type="number" step="0.01" min="0" /></div>
            <div style={{ flex: 1, minWidth: 100 }}><label style={labelStyle}>Currency</label>
              <select value={draft.currency} onChange={e => updateDraft('currency', e.target.value)} style={inputStyle}>
                <option value="GBP">GBP</option><option value="USD">USD</option><option value="EUR">EUR</option><option value="CAD">CAD</option><option value="AUD">AUD</option></select></div>
            <div style={{ flex: 1, minWidth: 100 }}><label style={labelStyle}>Quantity *</label>
              <input value={draft.quantity} onChange={e => updateDraft('quantity', e.target.value)} style={inputStyle} type="number" min="1" /></div>
          </div>
          {draft.listingFormat === 'AUCTION' && (
            <div style={{ display: 'flex', gap: 12, flexWrap: 'wrap', marginBottom: 8 }}>
              <div style={{ flex: 1, minWidth: 140 }}><label style={labelStyle}>Reserve Price ({cs})</label>
                <input value={draft.reservePrice} onChange={e => updateDraft('reservePrice', e.target.value)} style={inputStyle} type="number" step="0.01" min="0" placeholder="Optional" /></div>
            </div>)}
          {draft.listingFormat === 'FIXED_PRICE' && (<div style={{ marginBottom: 8 }}>
            <label style={{ display: 'flex', alignItems: 'center', gap: 8, fontSize: 13, color: 'var(--text-primary)', cursor: 'pointer', marginBottom: 8 }}>
              <input type="checkbox" checked={draft.bestOfferEnabled} onChange={e => updateDraft('bestOfferEnabled', e.target.checked)} /> Enable Best Offer</label>
            {draft.bestOfferEnabled && (<div style={{ display: 'flex', gap: 12, flexWrap: 'wrap', marginLeft: 24 }}>
              <div style={{ flex: 1, minWidth: 160 }}><label style={labelStyle}>Auto-accept above ({cs})</label>
                <input value={draft.bestOfferAutoAcceptPrice} onChange={e => updateDraft('bestOfferAutoAcceptPrice', e.target.value)} style={inputStyle} type="number" step="0.01" min="0" /></div>
              <div style={{ flex: 1, minWidth: 160 }}><label style={labelStyle}>Auto-decline below ({cs})</label>
                <input value={draft.bestOfferAutoDeclinePrice} onChange={e => updateDraft('bestOfferAutoDeclinePrice', e.target.value)} style={inputStyle} type="number" step="0.01" min="0" /></div>
            </div>)}
          </div>)}
          <div style={{ display: 'flex', gap: 12, flexWrap: 'wrap' }}>
            <div style={{ minWidth: 120 }}><label style={labelStyle}>VAT %</label>
              <input value={draft.vatPercentage} onChange={e => updateDraft('vatPercentage', e.target.value)} style={inputStyle} type="number" step="0.1" min="0" max="100" placeholder="e.g. 20" /></div>
            <div style={{ minWidth: 120 }}><label style={labelStyle}>Lot Size</label>
              <input value={draft.lotSize} onChange={e => updateDraft('lotSize', e.target.value)} style={inputStyle} type="number" min="1" placeholder="1" /></div>
          </div>
          {/* FLD-10 — Quantity-Based Pricing Tiers */}
          {draft.listingFormat === 'FIXED_PRICE' && (
            <div style={{ marginTop: 12 }}>
              <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', marginBottom: 6 }}>
                <div>
                  <span style={{ fontSize: 12, fontWeight: 700, color: 'var(--text-secondary)' }}>Quantity Pricing Tiers</span>
                  <span style={{ fontSize: 11, color: 'var(--text-muted)', marginLeft: 8 }}>— price breaks for buying in bulk</span>
                </div>
                <button onClick={addPricingTier} style={{ fontSize: 11, padding: '3px 10px', borderRadius: 5, border: `1px solid ${EBAY_BLUE}`, background: 'transparent', color: EBAY_BLUE, cursor: 'pointer', fontWeight: 600 }}>+ Add Tier</button>
              </div>
              {getPricingTiers().length === 0 && (
                <p style={{ fontSize: 12, color: 'var(--text-muted)', margin: '4px 0 0' }}>No tiers set — buyers pay the standard price above.</p>
              )}
              {getPricingTiers().map((tier: { minQty: number; pricePerUnit: string }, idx: number) => (
                <div key={idx} style={{ display: 'flex', gap: 8, alignItems: 'center', marginBottom: 6, flexWrap: 'wrap' }}>
                  <div style={{ display: 'flex', alignItems: 'center', gap: 4 }}>
                    <span style={{ fontSize: 12, color: 'var(--text-muted)' }}>Buy</span>
                    <input
                      value={tier.minQty}
                      onChange={e => updatePricingTier(idx, 'minQty', parseInt(e.target.value) || 2)}
                      style={{ ...inputStyle, width: 70, padding: '6px 10px', fontSize: 13 }}
                      type="number" min="2" placeholder="2"
                    />
                    <span style={{ fontSize: 12, color: 'var(--text-muted)' }}>or more, get</span>
                  </div>
                  <div style={{ display: 'flex', alignItems: 'center', gap: 4 }}>
                    <span style={{ fontSize: 12, color: 'var(--text-muted)' }}>{cs}</span>
                    <input
                      value={tier.pricePerUnit}
                      onChange={e => updatePricingTier(idx, 'pricePerUnit', e.target.value)}
                      style={{ ...inputStyle, width: 110, padding: '6px 10px', fontSize: 13 }}
                      type="number" step="0.01" min="0" placeholder="each"
                    />
                    <span style={{ fontSize: 12, color: 'var(--text-muted)' }}>each</span>
                  </div>
                  <button onClick={() => removePricingTier(idx)} style={{ background: 'none', border: 'none', color: 'var(--danger)', cursor: 'pointer', fontSize: 16, padding: '0 4px', lineHeight: 1 }}>×</button>
                </div>
              ))}
              {/* PRC-02 — eBay B2B pricing note */}
              <div style={{ marginTop: 8, padding: '8px 10px', background: 'rgba(0,100,210,0.06)', borderRadius: 6, fontSize: 11, color: 'var(--text-muted)', border: '1px solid rgba(0,100,210,0.15)' }}>
                ℹ️ <strong>B2B / Business Pricing:</strong> eBay does not expose a dedicated B2B price via the Inventory API. For business-specific discounts, use the quantity tiers above or set up a <a href="https://www.ebay.co.uk/help/selling/listings/selling-buy-now/business-seller-features" target="_blank" rel="noopener noreferrer" style={{ color: EBAY_BLUE }}>Volume Pricing programme</a> in eBay Seller Hub.
              </div>

              {/* PRC-04 — Promoted Listings Ad Rate */}
              <div style={{ marginTop: 16 }}>
                <label style={labelStyle}>Promoted Listing Ad Rate (%) <span style={{ fontWeight: 400, color: 'var(--text-muted)' }}>— optional, 1–20</span></label>
                <div style={{ display: 'flex', alignItems: 'center', gap: 8 }}>
                  <input
                    type="number"
                    min={1}
                    max={20}
                    step={0.1}
                    placeholder="e.g. 5.0"
                    value={(draft as any).promotedListingRate || ''}
                    onChange={e => updateDraft('promotedListingRate' as any, e.target.value)}
                    style={{ ...inputStyle, maxWidth: 140 }}
                  />
                  <span style={{ fontSize: 12, color: 'var(--text-muted)' }}>Cost-Per-Sale. Leave blank to submit without promoted listing.</span>
                </div>
                {(draft as any).promotedListingRate && (parseFloat((draft as any).promotedListingRate) < 1 || parseFloat((draft as any).promotedListingRate) > 20) && (
                  <p style={{ fontSize: 12, color: 'var(--danger)', marginTop: 4 }}>⚠ Rate must be between 1 and 20%</p>
                )}
              </div>
            </div>
          )}
        </Section>

        {/* SECTION: Images */}
        <Section title={`Images (${draft.images.length}/24)`} accent="#8b5cf6" subtitle="First image = gallery image">
          <div style={{ display: 'flex', flexWrap: 'wrap', gap: 8, marginBottom: 8 }}>
            {draft.images.map((img, idx) => (
              <div key={idx} style={{ display: 'flex', flexDirection: 'column', gap: 4, width: 110 }}>
                <div onDoubleClick={() => { if (idx > 0) { const imgs = [...draft.images]; const alts = [...((draft as any).imageAlts || draft.images.map(() => ''))]; [imgs[0], imgs[idx]] = [imgs[idx], imgs[0]]; [alts[0], alts[idx]] = [alts[idx], alts[0]]; updateDraft('images', imgs); updateDraft('imageAlts' as any, alts); } }} title={idx === 0 ? 'Primary image' : 'Double-click to set as primary'} style={{ position: 'relative', width: 110, height: 80, borderRadius: 6, border: idx === 0 ? `2px solid ${EBAY_BLUE}` : '1px solid var(--border)', overflow: 'hidden', cursor: idx > 0 ? 'pointer' : 'default' }}>
                  <img src={img} alt={(draft as any).imageAlts?.[idx] || ''} style={{ width: '100%', height: '100%', objectFit: 'cover' }} onError={e => { (e.target as HTMLImageElement).alt = '⚠️'; }} />
                  <div style={{ position: 'absolute', top: 0, right: 0, display: 'flex', gap: 2 }}>
                    {idx > 0 && <button onClick={() => moveImage(idx, -1)} style={imgBtnStyle}>◀</button>}
                    {idx < draft.images.length - 1 && <button onClick={() => moveImage(idx, 1)} style={imgBtnStyle}>▶</button>}
                    <button onClick={() => removeImage(idx)} style={{ ...imgBtnStyle, background: 'rgba(239,68,68,0.9)' }}>✕</button>
                  </div>
                  {idx === 0 && <div style={{ position: 'absolute', bottom: 0, left: 0, right: 0, background: EBAY_BLUE, color: '#fff', fontSize: 9, textAlign: 'center', padding: '1px 0' }}>MAIN</div>}
                </div>
                {/* FLD-16: Alt text input */}
                <input
                  value={((draft as any).imageAlts || [])[idx] || ''}
                  onChange={e => {
                    const alts = [...(((draft as any).imageAlts) || draft.images.map(() => ''))];
                    alts[idx] = e.target.value;
                    updateDraft('imageAlts' as any, alts);
                  }}
                  placeholder="Alt text / caption"
                  style={{ fontSize: 10, padding: '3px 5px', borderRadius: 4, border: '1px solid var(--border)', background: 'var(--bg-primary)', color: 'var(--text-primary)', width: '100%' }}
                />
              </div>
            ))}
            {draft.images.length < 24 && (
              <button onClick={() => { const url = prompt('Enter image URL:'); if (url) addImage(url); }}
                style={{ width: 110, height: 80, borderRadius: 6, border: '2px dashed var(--border)', background: 'transparent', color: 'var(--text-muted)', fontSize: 24, cursor: 'pointer', display: 'flex', alignItems: 'center', justifyContent: 'center' }}>+</button>
            )}
          </div>
          {draft.images.length === 0 && <p style={{ fontSize: 12, color: 'var(--danger)' }}>⚠ At least one image is required</p>}
          {draft.images.length >= 24 && (
            <p style={{ fontSize: 12, color: 'var(--warning)', marginTop: 4 }}>
              ⚠ eBay limit reached (24 images max). Remove an image to add another.
            </p>
          )}
          <p style={{ fontSize: 11, color: 'var(--text-muted)', marginTop: 4 }}>
            eBay allows up to <strong>24 images</strong>. First image = gallery/main image (shown with MAIN badge).
            Use ◀ ▶ to reorder. Double-click any thumbnail to set it as primary.
          </p>
        </Section>

        {/* SECTION: Business Policies */}
        <Section title="Business Policies" accent="#f97316" subtitle="Shipping, returns, and payment from your eBay account">
          {fulfillmentPolicies.length === 0 && paymentPolicies.length === 0 && returnPolicies.length === 0 ? (
            <div style={{ padding: 12, borderRadius: 8, background: 'rgba(249,115,22,0.08)', border: '1px solid rgba(249,115,22,0.2)', fontSize: 13, color: 'var(--text-secondary)', lineHeight: 1.5 }}>
              <strong>ℹ️ Business Policies not enabled</strong>
              <div style={{ marginTop: 4, fontSize: 12, color: 'var(--text-muted)' }}>
                This eBay account doesn't use Business Policies. Shipping, payment, and return details will need to be set directly on eBay.
                To enable Business Policies, go to <strong>eBay → Account Settings → Business Policies</strong> and opt in.
              </div>
            </div>
          ) : (
            <>
              <div style={{ display: 'flex', gap: 12, flexWrap: 'wrap', marginBottom: 8 }}>
                <div style={{ flex: 1, minWidth: 200 }}><label style={labelStyle}>Shipping Policy {fulfillmentPolicies.length > 0 && '*'}</label>
                  <select value={draft.fulfillmentPolicyId} onChange={e => updateDraft('fulfillmentPolicyId', e.target.value)} style={inputStyle}>
                    <option value="">— Select —</option>{fulfillmentPolicies.map(p => <option key={p.fulfillmentPolicyId} value={p.fulfillmentPolicyId}>{p.name}</option>)}</select></div>
                <div style={{ flex: 1, minWidth: 200 }}><label style={labelStyle}>Return Policy {returnPolicies.length > 0 && '*'}</label>
                  <select value={draft.returnPolicyId} onChange={e => updateDraft('returnPolicyId', e.target.value)} style={inputStyle}>
                    <option value="">— Select —</option>{returnPolicies.map(p => <option key={p.returnPolicyId} value={p.returnPolicyId}>{p.name}</option>)}</select></div>
                <div style={{ flex: 1, minWidth: 200 }}><label style={labelStyle}>Payment Policy {paymentPolicies.length > 0 && '*'}</label>
                  <select value={draft.paymentPolicyId} onChange={e => updateDraft('paymentPolicyId', e.target.value)} style={inputStyle}>
                    <option value="">— Select —</option>{paymentPolicies.map(p => <option key={p.paymentPolicyId} value={p.paymentPolicyId}>{p.name}</option>)}</select></div>
              </div>
            </>
          )}
          <div style={{ flex: 1, minWidth: 200, marginTop: 8 }}><label style={labelStyle}>Inventory Location</label>
            <select value={draft.merchantLocationKey} onChange={e => updateDraft('merchantLocationKey', e.target.value)} style={inputStyle}>
              <option value="">— Select —</option>{locations.map(l => <option key={l.merchantLocationKey} value={l.merchantLocationKey}>{l.name || l.merchantLocationKey}</option>)}</select></div>

          {/* FLD-12: Click & Collect / in-store pickup */}
          <div style={{ width: '100%', marginTop: 16, padding: '14px 16px', background: 'var(--bg-tertiary)', borderRadius: 8, border: '1px solid var(--border)' }}>
            <div style={{ fontSize: 13, fontWeight: 700, marginBottom: 10, color: 'var(--text-primary)' }}>🏪 Click & Collect / In-Store Pickup</div>
            <div style={{ display: 'flex', gap: 16, flexWrap: 'wrap', alignItems: 'flex-start' }}>
              <label style={{ display: 'flex', alignItems: 'center', gap: 8, cursor: 'pointer', fontSize: 13, color: 'var(--text-primary)' }}>
                <input type="checkbox"
                  checked={!!(draft as any).clickAndCollectEnabled}
                  onChange={e => updateDraft('clickAndCollectEnabled' as any, e.target.checked)}
                />
                Enable Click & Collect
              </label>
              {(draft as any).clickAndCollectEnabled && (
                <>
                  <div>
                    <label style={labelStyle}>Pickup Lead Time (days)</label>
                    <input type="number" min={0} max={30}
                      value={(draft as any).pickupLeadTimeDays ?? 0}
                      onChange={e => updateDraft('pickupLeadTimeDays' as any, Math.max(0, Math.min(30, parseInt(e.target.value) || 0)))}
                      style={{ ...inputStyle, width: 90 }}
                    />
                    <p style={{ fontSize: 11, color: 'var(--text-muted)', margin: '2px 0 0' }}>0 = same day collection</p>
                  </div>
                  <label style={{ display: 'flex', alignItems: 'center', gap: 8, cursor: 'pointer', fontSize: 13, color: 'var(--text-primary)', alignSelf: 'flex-end', paddingBottom: 6 }}>
                    <input type="checkbox"
                      checked={!!(draft as any).pickupDropOffEnabled}
                      onChange={e => updateDraft('pickupDropOffEnabled' as any, e.target.checked)}
                    />
                    Allow customer drop-off (IN_STORE_PICKUP location type)
                  </label>
                </>
              )}
            </div>
            {(draft as any).clickAndCollectEnabled && !draft.merchantLocationKey && (
              <p style={{ marginTop: 8, fontSize: 12, color: 'var(--warning)' }}>⚠ Select an Inventory Location above to enable Click & Collect.</p>
            )}
          </div>
        </Section>

        {/* SECTION: GPSR — EU Product Safety (FLD-07) */}
        <div style={{ background: 'var(--bg-secondary)', borderRadius: 10, border: '1px solid var(--border)', overflow: 'hidden', position: 'relative' }}>
          <div style={{ position: 'absolute', top: 0, left: 0, width: 3, height: '100%', background: '#2563eb', borderRadius: '10px 0 0 10px' }} />
          <div
            style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', padding: '14px 16px', cursor: 'pointer' }}
            onClick={() => setGpsrOpen(o => !o)}
          >
            <div>
              <h3 style={{ fontSize: 14, fontWeight: 700, margin: 0, color: 'var(--text-primary)' }}>
                🇪🇺 GPSR / EU Product Safety
                {((draft as any).gpsrManufacturerName || (draft as any).gpsrResponsiblePersonName) && (
                  <span style={{ marginLeft: 8, fontSize: 11, padding: '1px 6px', borderRadius: 4, background: 'rgba(34,197,94,0.1)', color: 'var(--success)', fontWeight: 400 }}>filled</span>
                )}
              </h3>
              <p style={{ fontSize: 11, color: 'var(--text-muted)', margin: '2px 0 0' }}>Required for EU sellers from Dec 2024 — manufacturer, responsible person, safety attestation</p>
            </div>
            <span style={{ fontSize: 12, color: 'var(--text-muted)', userSelect: 'none' }}>{gpsrOpen ? '▼' : '▶'}</span>
          </div>
          {gpsrOpen && (
            <div style={{ padding: '0 16px 16px' }}>
              <div style={{ padding: '8px 12px', background: 'rgba(37,99,235,0.06)', borderRadius: 6, fontSize: 11, color: 'var(--text-muted)', marginBottom: 12, border: '1px solid rgba(37,99,235,0.15)' }}>
                ℹ️ The EU General Product Safety Regulation (GPSR) requires sellers to provide manufacturer and responsible person details for products sold to EU buyers. These fields are submitted as item specifics to eBay.
              </div>
              <div style={{ display: 'flex', gap: 12, flexWrap: 'wrap', marginBottom: 8 }}>
                <div style={{ flex: 1, minWidth: 200 }}>
                  <label style={labelStyle}>Manufacturer Name</label>
                  <input value={(draft as any).gpsrManufacturerName || ''} onChange={e => updateDraft('gpsrManufacturerName', e.target.value)} style={inputStyle} placeholder="e.g. Acme Corp Ltd" />
                </div>
                <div style={{ flex: 2, minWidth: 260 }}>
                  <label style={labelStyle}>Manufacturer Address</label>
                  <input value={(draft as any).gpsrManufacturerAddress || ''} onChange={e => updateDraft('gpsrManufacturerAddress', e.target.value)} style={inputStyle} placeholder="Full registered address" />
                </div>
              </div>
              <div style={{ display: 'flex', gap: 12, flexWrap: 'wrap', marginBottom: 8 }}>
                <div style={{ flex: 1, minWidth: 200 }}>
                  <label style={labelStyle}>EU Responsible Person / Importer</label>
                  <input value={(draft as any).gpsrResponsiblePersonName || ''} onChange={e => updateDraft('gpsrResponsiblePersonName', e.target.value)} style={inputStyle} placeholder="Name of EU representative" />
                </div>
                <div style={{ flex: 1, minWidth: 200 }}>
                  <label style={labelStyle}>Responsible Person Contact</label>
                  <input value={(draft as any).gpsrResponsiblePersonContact || ''} onChange={e => updateDraft('gpsrResponsiblePersonContact', e.target.value)} style={inputStyle} placeholder="Email or phone" />
                </div>
              </div>
              <div style={{ marginBottom: 8 }}>
                <label style={labelStyle}>Safety / Compliance Document URLs</label>
                <textarea
                  value={(draft as any).gpsrDocumentUrls || ''}
                  onChange={e => updateDraft('gpsrDocumentUrls', e.target.value)}
                  style={{ ...inputStyle, minHeight: 60, resize: 'vertical', fontSize: 12 }}
                  placeholder="One URL per line — links to CE declaration, test reports, safety data sheets, etc."
                />
                <small style={{ color: 'var(--text-muted)' }}>These will be submitted as "Regulatory Documentation" item specifics.</small>
              </div>
              <label style={{ display: 'flex', alignItems: 'center', gap: 8, fontSize: 13, color: 'var(--text-primary)', cursor: 'pointer' }}>
                <input
                  type="checkbox"
                  checked={(draft as any).gpsrSafetyAttestation || false}
                  onChange={e => updateDraft('gpsrSafetyAttestation', e.target.checked)}
                />
                I confirm a safety attestation has been completed for this product
              </label>
            </div>
          )}
        </div>

        {/* SECTION: Package Dimensions & Weight */}
        <Section title="Package Dimensions & Weight" accent="#14b8a6" collapsible defaultOpen={false}>
          <div style={{ display: 'flex', gap: 12, flexWrap: 'wrap', marginBottom: 8 }}>
            <div style={{ flex: 1, minWidth: 80 }}><label style={labelStyle}>Length</label><input value={draft.packageLength} onChange={e => updateDraft('packageLength', e.target.value)} style={inputStyle} type="number" step="0.1" min="0" /></div>
            <div style={{ flex: 1, minWidth: 80 }}><label style={labelStyle}>Width</label><input value={draft.packageWidth} onChange={e => updateDraft('packageWidth', e.target.value)} style={inputStyle} type="number" step="0.1" min="0" /></div>
            <div style={{ flex: 1, minWidth: 80 }}><label style={labelStyle}>Height</label><input value={draft.packageHeight} onChange={e => updateDraft('packageHeight', e.target.value)} style={inputStyle} type="number" step="0.1" min="0" /></div>
            <div style={{ flex: 1, minWidth: 100 }}><label style={labelStyle}>Dim. Unit</label>
              <select value={draft.dimensionUnit} onChange={e => updateDraft('dimensionUnit', e.target.value)} style={inputStyle}>{DIM_UNITS.map(o => <option key={o.value} value={o.value}>{o.label}</option>)}</select></div>
          </div>
          <div style={{ display: 'flex', gap: 12, flexWrap: 'wrap' }}>
            <div style={{ flex: 1, minWidth: 100 }}><label style={labelStyle}>Weight</label><input value={draft.packageWeightValue} onChange={e => updateDraft('packageWeightValue', e.target.value)} style={inputStyle} type="number" step="0.01" min="0" /></div>
            <div style={{ flex: 1, minWidth: 100 }}><label style={labelStyle}>Weight Unit</label>
              <select value={draft.weightUnit} onChange={e => updateDraft('weightUnit', e.target.value)} style={inputStyle}>{WT_UNITS.map(o => <option key={o.value} value={o.value}>{o.label}</option>)}</select></div>
            <div style={{ flex: 1, minWidth: 140 }}><label style={labelStyle}>Package Type</label>
              <select value={draft.packageType} onChange={e => updateDraft('packageType', e.target.value)} style={inputStyle}>{PKG_TYPES.map(o => <option key={o.value} value={o.value}>{o.label}</option>)}</select></div>
          </div>
        </Section>

        {/* SECTION: Listing Settings */}
        <Section title="Listing Settings" accent="#6366f1" collapsible defaultOpen={false}>
          <div style={{ display: 'flex', gap: 12, flexWrap: 'wrap', marginBottom: 8 }}>
            <div style={{ flex: 1, minWidth: 160 }}><label style={labelStyle}>Duration</label>
              <select value={draft.listingDuration} onChange={e => updateDraft('listingDuration', e.target.value)} style={inputStyle}>
                {DURATION_OPTIONS.map(o => <option key={o.value} value={o.value}>{o.label}</option>)}</select></div>
            <div style={{ flex: 1, minWidth: 200 }}><label style={labelStyle}>Scheduled Start</label>
              <input value={draft.scheduledStartTime} onChange={e => updateDraft('scheduledStartTime', e.target.value)} style={inputStyle} type="datetime-local" /></div>
          </div>
          <div style={{ display: 'flex', gap: 16, flexWrap: 'wrap' }}>
            <label style={{ display: 'flex', alignItems: 'center', gap: 8, fontSize: 13, color: 'var(--text-primary)', cursor: 'pointer' }}>
              <input type="checkbox" checked={draft.privateListing} onChange={e => updateDraft('privateListing', e.target.checked)} /> Private Listing</label>
            <label style={{ display: 'flex', alignItems: 'center', gap: 8, fontSize: 13, color: 'var(--text-primary)', cursor: 'pointer' }}>
              <input type="checkbox" checked={draft.includeCatalogProductDetails} onChange={e => updateDraft('includeCatalogProductDetails', e.target.checked)} /> Include eBay Catalog Details</label>
          </div>
        </Section>

        {/* SECTION: FLD-01 — Payment Methods */}
        <Section title="Payment Methods" accent="#6366f1" collapsible defaultOpen={false}
          subtitle="Informational — stored in listing record, not sent to eBay API">
          <p style={{ fontSize: 12, color: 'var(--text-muted)', marginBottom: 10 }}>
            Select accepted payment methods for your records. eBay manages payment processing centrally via Managed Payments — this field is stored as listing metadata only.
          </p>
          <div style={{ display: 'flex', flexWrap: 'wrap', gap: 8 }}>
            {['PayPal', 'Credit/Debit Card', 'Klarna', 'Bank Transfer', 'Apple Pay', 'Google Pay', 'Cash on Delivery'].map(method => {
              const selected = ((draft as any).paymentMethods || []).includes(method);
              return (
                <button key={method} onClick={() => {
                  const current: string[] = (draft as any).paymentMethods || [];
                  updateDraft('paymentMethods', selected ? current.filter((m: string) => m !== method) : [...current, method]);
                }} style={{
                  padding: '6px 14px', borderRadius: 20, fontSize: 12, cursor: 'pointer', fontWeight: 600,
                  border: selected ? `1px solid ${EBAY_BLUE}` : '1px solid var(--border)',
                  background: selected ? `${EBAY_BLUE}18` : 'transparent',
                  color: selected ? EBAY_BLUE : 'var(--text-muted)',
                }}>{method}</button>
              );
            })}
          </div>
        </Section>

        {/* VAR-01 — Variant Grid */}
        {isVariantProduct && (() => {
          const combKeys = variants.length > 0 ? Object.keys(variants[0].combination) : [];
          const cs = CURRENCY_SYMBOL[draft.currency] || draft.currency;
          const activeCount = variants.filter(v => v.active).length;
          const overrideCount = variants.filter(v => v.title || v.description || v.brand || v.weight || v.length || v.images?.length).length;

          const cellIn: React.CSSProperties = {
            padding: '5px 8px', borderRadius: 6, border: '1px solid var(--border)',
            background: 'var(--bg-primary)', color: 'var(--text-primary)',
            fontSize: 12, width: '100%', boxSizing: 'border-box', outline: 'none',
          };

          return (
            <Section accent="#d946ef" title={`Variations (${variants.length})`}>
              {/* Summary bar */}
              <div style={{ display: 'flex', gap: 16, marginBottom: 12, fontSize: 12 }}>
                <span style={{ color: 'var(--success)' }}>✓ {activeCount} active</span>
                <span style={{ color: 'var(--text-muted)' }}>{variants.length - activeCount} inactive</span>
                {overrideCount > 0 && <span style={{ color: '#d946ef' }}>✏ {overrideCount} with overrides</span>}
                <span style={{ color: 'var(--text-muted)', marginLeft: 'auto' }}>
                  Each active variant = one child SKU in eBay InventoryItemGroup
                </span>
              </div>

              {/* Select-all header action */}
              <div style={{ display: 'flex', gap: 8, marginBottom: 8 }}>
                <button onClick={() => setVariants(vs => vs.map(v => ({ ...v, active: true })))}
                  style={{ fontSize: 11, padding: '3px 10px', borderRadius: 5, border: '1px solid var(--border)', background: 'var(--bg-tertiary)', color: 'var(--text-muted)', cursor: 'pointer' }}>
                  Select all
                </button>
                <button onClick={() => setVariants(vs => vs.map(v => ({ ...v, active: false })))}
                  style={{ fontSize: 11, padding: '3px 10px', borderRadius: 5, border: '1px solid var(--border)', background: 'var(--bg-tertiary)', color: 'var(--text-muted)', cursor: 'pointer' }}>
                  Deselect all
                </button>
              </div>

              <div style={{ overflowX: 'auto' }}>
                <table style={{ width: '100%', borderCollapse: 'collapse', fontSize: 13 }}>
                  <thead>
                    <tr style={{ borderBottom: '2px solid var(--border)', background: 'var(--bg-tertiary)' }}>
                      <th style={{ ...thStyle, width: 36 }}>
                        <input type="checkbox"
                          checked={variants.every(v => v.active)}
                          ref={el => { if (el) el.indeterminate = activeCount > 0 && activeCount < variants.length; }}
                          onChange={e => setVariants(vs => vs.map(v => ({ ...v, active: e.target.checked })))}
                        />
                      </th>
                      <th style={{ ...thStyle, width: 52 }}>Image</th>
                      {combKeys.map(k => <th key={k} style={{ ...thStyle, textTransform: 'capitalize' as const }}>{k}</th>)}
                      <th style={thStyle}>SKU</th>
                      <th style={thStyle}>Price ({cs})</th>
                      <th style={thStyle}>Stock</th>
                      <th style={thStyle}>EAN</th>
                      <th style={{ ...thStyle, width: 60 }}>Edit</th>
                    </tr>
                  </thead>
                  <tbody>
                    {variants.map(v => {
                      const hasOverrides = !!(v.title || v.description || v.brand || v.weight || v.length || (v.images && v.images.length > 0));
                      return (
                        <tr key={v.id} style={{ borderBottom: '1px solid var(--border)', opacity: v.active ? 1 : 0.42, background: v.active ? 'transparent' : 'var(--bg-tertiary)' }}>
                          <td style={{ ...tdStyle, textAlign: 'center' as const }}>
                            <input type="checkbox" checked={v.active} onChange={e => updateVariant(v.id, 'active', e.target.checked)} />
                          </td>
                          <td style={{ ...tdStyle, padding: '4px 6px' }}>
                            {v.image ? (
                              <img src={v.image} alt="" style={{ width: 40, height: 40, objectFit: 'contain', borderRadius: 4, border: '1px solid var(--border)', cursor: 'pointer', display: 'block' }}
                                onClick={() => setEditingVariant(v)} onError={e => { (e.target as HTMLImageElement).style.display = 'none'; }} />
                            ) : (
                              <div onClick={() => setEditingVariant(v)} style={{ width: 40, height: 40, borderRadius: 4, border: '1px dashed var(--border)', display: 'flex', alignItems: 'center', justifyContent: 'center', color: 'var(--text-muted)', fontSize: 16, cursor: 'pointer' }}>+</div>
                            )}
                          </td>
                          {combKeys.map(k => (
                            <td key={k} style={{ ...tdStyle, fontWeight: 500 }}>{v.combination[k] || '—'}</td>
                          ))}
                          <td style={tdStyle}><input value={v.sku} onChange={e => updateVariant(v.id, 'sku', e.target.value)} style={cellIn} /></td>
                          <td style={tdStyle}><input value={v.price} onChange={e => updateVariant(v.id, 'price', e.target.value)} style={{ ...cellIn, width: 80 }} type="number" step="0.01" /></td>
                          <td style={tdStyle}><input value={v.stock} onChange={e => updateVariant(v.id, 'stock', e.target.value)} style={{ ...cellIn, width: 65 }} type="number" /></td>
                          <td style={tdStyle}><input value={v.ean} onChange={e => updateVariant(v.id, 'ean', e.target.value)} style={{ ...cellIn, width: 130 }} /></td>
                          <td style={{ ...tdStyle, textAlign: 'center' as const }}>
                            <button onClick={() => setEditingVariant(v)} style={{
                              padding: '4px 10px', borderRadius: 6, fontSize: 11, cursor: 'pointer', fontWeight: 600,
                              border: hasOverrides ? '1px solid #d946ef' : '1px solid var(--border)',
                              background: hasOverrides ? 'rgba(217,70,239,0.1)' : 'var(--bg-tertiary)',
                              color: hasOverrides ? '#d946ef' : 'var(--text-muted)',
                            }}>✏</button>
                          </td>
                        </tr>
                      );
                    })}
                  </tbody>
                </table>
              </div>
            </Section>
          );
        })()}

        {/* eBay Variant Edit Modal */}
        {editingVariant && (
          <EbayVariantModal
            variant={editingVariant}
            parentDraft={draft}
            onSave={(updated, applyPricingToAll) => {
              if (applyPricingToAll) {
                setVariants(vs => vs.map(v => ({
                  ...v,
                  price: updated.price || v.price,
                  stock: updated.stock || v.stock,
                  upc: updated.upc || v.upc,
                  ...(v.id === updated.id ? updated : {}),
                })));
              } else {
                setVariants(vs => vs.map(v => v.id === updated.id ? updated : v));
              }
              setEditingVariant(null);
            }}
            onClose={() => setEditingVariant(null)}
          />
        )}

        {/* Submit footer */}
        <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', padding: '16px 0', borderTop: '1px solid var(--border)', marginTop: 8 }}>
          <div style={{ fontSize: 12, color: 'var(--text-muted)' }}>
            {!canSubmit ? <span style={{ color: 'var(--warning)' }}>Missing: {[!draft.title && 'title', !draft.sku && 'SKU', !draft.categoryId && 'category', !draft.price && 'price', draft.images.length === 0 && 'images',
              !draft.fulfillmentPolicyId && fulfillmentPolicies.length > 0 && 'shipping policy',
              !draft.paymentPolicyId && paymentPolicies.length > 0 && 'payment policy',
              !draft.returnPolicyId && returnPolicies.length > 0 && 'return policy'].filter(Boolean).join(', ')}</span>
              : <span style={{ color: 'var(--success)' }}>✓ Ready to submit</span>}
          </div>
          <div style={{ display: 'flex', gap: 8, alignItems: 'center' }}>
            <label style={{ display: 'flex', alignItems: 'center', gap: 6, fontSize: 12, color: 'var(--text-secondary)', cursor: 'pointer' }}>
              <input type="checkbox" checked={publishOnSubmit} onChange={e => setPublishOnSubmit(e.target.checked)} /> Publish</label>
            <button onClick={handleSubmit} disabled={!canSubmit || submitting}
              style={{ ...primaryBtnStyle, opacity: canSubmit && !submitting ? 1 : 0.5 }}>
              {submitting ? '⏳ Submitting...' : draft?.isUpdate ? '🔄 Update' : '🚀 Submit'}</button>
          </div>
        </div>

      </div>}

      {/* ── Catalog Search Modal (FLD-09) ── */}
      {catalogOpen && (
        <div style={{ position: 'fixed', inset: 0, background: 'rgba(0,0,0,0.6)', zIndex: 1000, display: 'flex', alignItems: 'center', justifyContent: 'center', padding: 24 }}
          onClick={e => { if (e.target === e.currentTarget) setCatalogOpen(false); }}>
          <div style={{ background: 'var(--bg-primary)', borderRadius: 12, width: '100%', maxWidth: 580, maxHeight: '80vh', overflow: 'hidden', display: 'flex', flexDirection: 'column', border: '1px solid var(--border)', boxShadow: '0 20px 60px rgba(0,0,0,0.4)' }}>
            {/* Modal header */}
            <div style={{ padding: '16px 20px', borderBottom: '1px solid var(--border)', display: 'flex', alignItems: 'center', justifyContent: 'space-between' }}>
              <div>
                <h3 style={{ margin: 0, fontSize: 15, fontWeight: 700 }}>🔍 Match to eBay Catalogue</h3>
                <p style={{ margin: '2px 0 0', fontSize: 12, color: 'var(--text-muted)' }}>Linking to an eBay product fills in catalogue details and boosts search visibility</p>
              </div>
              <button onClick={() => setCatalogOpen(false)} style={{ background: 'none', border: 'none', fontSize: 20, cursor: 'pointer', color: 'var(--text-muted)', lineHeight: 1, padding: '0 4px' }}>×</button>
            </div>
            {/* Search input */}
            <div style={{ padding: '12px 20px', borderBottom: '1px solid var(--border)' }}>
              <input
                autoFocus
                value={catalogQuery}
                onChange={e => handleCatalogSearch(e.target.value)}
                style={inputStyle}
                placeholder="Search by product name, brand, or scan EAN/UPC into the GTIN field..."
              />
              {catalogSearching && <p style={{ fontSize: 12, color: 'var(--text-muted)', margin: '6px 0 0' }}>⏳ Searching eBay Catalogue...</p>}
              {!catalogSearching && catalogQuery.length >= 2 && catalogResults.length === 0 && (
                <p style={{ fontSize: 12, color: 'var(--text-muted)', margin: '6px 0 0' }}>No matching catalogue products found. You can enter an EPID manually in the form field.</p>
              )}
            </div>
            {/* Results */}
            <div style={{ flex: 1, overflowY: 'auto', padding: '8px 0' }}>
              {catalogResults.map(product => (
                <div
                  key={product.epid}
                  onClick={() => handleCatalogSelect(product)}
                  style={{ display: 'flex', gap: 12, padding: '10px 20px', cursor: 'pointer', borderBottom: '1px solid var(--border)', alignItems: 'center' }}
                  onMouseEnter={e => (e.currentTarget.style.background = 'var(--bg-secondary)')}
                  onMouseLeave={e => (e.currentTarget.style.background = 'transparent')}
                >
                  {product.imageUrl && (
                    <img src={product.imageUrl} alt="" style={{ width: 48, height: 48, objectFit: 'contain', borderRadius: 4, border: '1px solid var(--border)', flexShrink: 0 }} onError={e => { (e.target as HTMLImageElement).style.display = 'none'; }} />
                  )}
                  <div style={{ flex: 1, minWidth: 0 }}>
                    <div style={{ fontSize: 13, fontWeight: 600, color: 'var(--text-primary)', overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>{product.title}</div>
                    <div style={{ fontSize: 11, color: 'var(--text-muted)', marginTop: 2 }}>
                      EPID: <strong style={{ color: EBAY_BLUE }}>{product.epid}</strong>
                      {product.brand && <> · {product.brand}</>}
                      {product.categoryName && <> · {product.categoryName}</>}
                    </div>
                    {product.gtins && product.gtins.length > 0 && (
                      <div style={{ fontSize: 10, color: 'var(--text-muted)', marginTop: 1 }}>GTINs: {product.gtins.slice(0, 3).join(', ')}</div>
                    )}
                  </div>
                  <span style={{ fontSize: 11, color: EBAY_BLUE, fontWeight: 600, flexShrink: 0 }}>Select →</span>
                </div>
              ))}
            </div>
            {/* Footer */}
            <div style={{ padding: '10px 20px', borderTop: '1px solid var(--border)', display: 'flex', justifyContent: 'flex-end' }}>
              <button onClick={() => setCatalogOpen(false)} style={{ ...secondaryBtnStyle, fontSize: 13, padding: '8px 16px' }}>Cancel</button>
            </div>
          </div>
        </div>
      )}
    </div>
  );
}

// ============================================================================
// SUB-COMPONENTS
// ============================================================================

function Section({ title, children, accent, subtitle, collapsible, defaultOpen = true }: {
  title: string; children: React.ReactNode; accent?: string; subtitle?: string; collapsible?: boolean; defaultOpen?: boolean;
}) {
  const [open, setOpen] = useState(defaultOpen);
  return (
    <div style={{ background: 'var(--bg-secondary)', borderRadius: 10, padding: '14px 16px', border: '1px solid var(--border)', position: 'relative', overflow: 'hidden' }}>
      {accent && <div style={{ position: 'absolute', top: 0, left: 0, width: 3, height: '100%', background: accent, borderRadius: '10px 0 0 10px' }} />}
      <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', cursor: collapsible ? 'pointer' : 'default', marginBottom: open ? 10 : 0 }}
        onClick={() => collapsible && setOpen(!open)}>
        <div><h3 style={{ fontSize: 14, fontWeight: 700, margin: 0, color: 'var(--text-primary)' }}>{title}</h3>
          {subtitle && <p style={{ fontSize: 11, color: 'var(--text-muted)', margin: '2px 0 0' }}>{subtitle}</p>}</div>
        {collapsible && <span style={{ fontSize: 12, color: 'var(--text-muted)', userSelect: 'none' }}>{open ? '▼' : '▶'}</span>}
      </div>
      {open && children}
    </div>
  );
}

function Subsection({ title, children, defaultOpen = false }: { title: string; children: React.ReactNode; defaultOpen?: boolean }) {
  const [open, setOpen] = useState(defaultOpen);
  return (<div style={{ marginTop: 8 }}>
    <button onClick={() => setOpen(!open)} style={{ background: 'transparent', border: 'none', color: 'var(--text-secondary)', fontSize: 12, fontWeight: 600, cursor: 'pointer', padding: '4px 0', display: 'flex', alignItems: 'center', gap: 6 }}>
      <span style={{ fontSize: 10 }}>{open ? '▼' : '▶'}</span> {title}</button>
    {open && <div style={{ marginTop: 6 }}>{children}</div>}
  </div>);
}

function AspectField({ aspect, value, values, onChange, tag, tagColor }: {
  aspect: ItemAspect; value: string; values: string[]; onChange: (vals: string[]) => void; tag: string; tagColor: string;
}) {
  const isSel = aspect.aspectConstraint.aspectMode === 'SELECTION_ONLY';
  const isMulti = aspect.aspectConstraint.itemToAspectCardinality === 'MULTI';
  const opts = aspect.aspectValues || [];
  return (
    <div style={{ display: 'flex', gap: 8, alignItems: 'center' }}>
      <span style={{ fontSize: 9, padding: '1px 4px', borderRadius: 3, background: `${tagColor}22`, color: tagColor, fontWeight: 700, minWidth: 28, textAlign: 'center' }}>{tag}</span>
      <label style={{ fontSize: 12, color: 'var(--text-secondary)', minWidth: 160, maxWidth: 200, overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }} title={aspect.localizedAspectName}>{aspect.localizedAspectName}</label>
      {isSel && opts.length > 0 ? (isMulti ? (
        <div style={{ flex: 1, display: 'flex', flexWrap: 'wrap', gap: 4 }}>
          {opts.slice(0, 20).map(o => { const sel = values.includes(o.localizedValue); return (
            <button key={o.localizedValue} onClick={() => sel ? onChange(values.filter(v => v !== o.localizedValue)) : onChange([...values, o.localizedValue])}
              style={{ fontSize: 11, padding: '2px 8px', borderRadius: 4, cursor: 'pointer', border: sel ? '1px solid #0064D2' : '1px solid var(--border)', background: sel ? '#0064D222' : 'transparent', color: sel ? '#0064D2' : 'var(--text-muted)' }}>{o.localizedValue}</button>
          ); })}
          {opts.length > 20 && <span style={{ fontSize: 10, color: 'var(--text-muted)' }}>+{opts.length - 20} more</span>}
        </div>
      ) : (
        <select value={value} onChange={e => onChange(e.target.value ? [e.target.value] : [])} style={{ ...inputStyle, flex: 1, fontSize: 12, padding: '6px 10px' }}>
          <option value="">— Select —</option>{opts.map(o => <option key={o.localizedValue} value={o.localizedValue}>{o.localizedValue}</option>)}</select>
      )) : (
        <input value={value} onChange={e => onChange(e.target.value ? [e.target.value] : [])} style={{ ...inputStyle, flex: 1, fontSize: 12, padding: '6px 10px' }} placeholder={`Enter ${aspect.localizedAspectName}`} />
      )}
    </div>
  );
}

// ============================================================================
// WYSIWYG DESCRIPTION EDITOR (eBay-compatible HTML)
// ============================================================================
// Supports: bold, italic, underline, strikethrough, headings, lists (ul/ol),
// links, images, text alignment, font size, colors, horizontal rules, tables.
// Banned: script, iframe, form, embed, object, applet, JavaScript events.
// Three modes: Visual (default), HTML source, Preview.
// ============================================================================

type EditorMode = 'visual' | 'html' | 'preview';

function EbayDescriptionEditor({ value, onChange, inputStyle: baseStyle }: {
  value: string; onChange: (html: string) => void; inputStyle: React.CSSProperties;
}) {
  const [mode, setMode] = useState<EditorMode>('visual');
  const editorRef = useRef<HTMLDivElement>(null);
  const htmlRef = useRef<string>(value);
  const isInternalUpdate = useRef(false);

  // Sync external value changes into the editor
  useEffect(() => {
    if (isInternalUpdate.current) {
      isInternalUpdate.current = false;
      return;
    }
    htmlRef.current = value;
    if (mode === 'visual' && editorRef.current && editorRef.current.innerHTML !== value) {
      editorRef.current.innerHTML = value;
    }
  }, [value, mode]);

  const exec = (cmd: string, val?: string) => {
    document.execCommand(cmd, false, val);
    editorRef.current?.focus();
    syncFromEditor();
  };

  function syncFromEditor() {
    if (editorRef.current) {
      const html = editorRef.current.innerHTML;
      htmlRef.current = html;
      isInternalUpdate.current = true;
      onChange(html);
    }
  };

  const handleInsertLink = () => {
    const url = prompt('Enter URL (eBay pages only):');
    if (url) exec('createLink', url);
  };

  const handleInsertImage = () => {
    const url = prompt('Enter image URL (HTTPS only):');
    if (url) exec('insertImage', url);
  };

  const handleInsertTable = () => {
    const rows = parseInt(prompt('Number of rows:', '2') || '0');
    const cols = parseInt(prompt('Number of columns:', '2') || '0');
    if (rows > 0 && cols > 0) {
      let table = '<table border="1" cellpadding="6" cellspacing="0" style="border-collapse:collapse;width:100%">';
      for (let r = 0; r < rows; r++) {
        table += '<tr>';
        for (let c = 0; c < cols; c++) table += `<td>${r === 0 ? 'Header' : '&nbsp;'}</td>`;
        table += '</tr>';
      }
      table += '</table><br>';
      exec('insertHTML', table);
    }
  };

  const handleHtmlChange = (raw: string) => {
    htmlRef.current = raw;
    isInternalUpdate.current = true;
    onChange(raw);
  };

  const tb: React.CSSProperties = {
    background: 'none', border: '1px solid var(--border)', borderRadius: 4,
    padding: '3px 7px', fontSize: 12, cursor: 'pointer', color: 'var(--text-secondary)',
    lineHeight: 1.2, minWidth: 26, textAlign: 'center', fontFamily: 'inherit',
  };
  const sep: React.CSSProperties = { width: 1, background: 'var(--border)', margin: '2px 3px', alignSelf: 'stretch' };
  const modeBtn = (m: EditorMode, label: string) => (
    <button type="button" onClick={() => setMode(m)}
      style={{ ...tb, background: mode === m ? 'var(--primary)' : 'none', color: mode === m ? '#fff' : 'var(--text-muted)', fontWeight: mode === m ? 700 : 400, fontSize: 11 }}>{label}</button>
  );

  return (
    <div style={{ border: '1px solid var(--border)', borderRadius: 8, overflow: 'hidden', background: 'var(--bg-primary)' }}>
      {/* Toolbar */}
      <div style={{ display: 'flex', flexWrap: 'wrap', gap: 3, padding: '6px 8px', background: 'var(--bg-secondary)', borderBottom: '1px solid var(--border)', alignItems: 'center' }}>
        {/* Mode switcher */}
        {modeBtn('visual', '✏️ Visual')}
        {modeBtn('html', '</> HTML')}
        {modeBtn('preview', '👁 Preview')}
        <div style={sep} />

        {mode === 'visual' && (<>
          {/* Text formatting */}
          <button type="button" onClick={() => exec('bold')} style={{ ...tb, fontWeight: 700 }} title="Bold">B</button>
          <button type="button" onClick={() => exec('italic')} style={{ ...tb, fontStyle: 'italic' }} title="Italic">I</button>
          <button type="button" onClick={() => exec('underline')} style={{ ...tb, textDecoration: 'underline' }} title="Underline">U</button>
          <button type="button" onClick={() => exec('strikeThrough')} style={{ ...tb, textDecoration: 'line-through' }} title="Strikethrough">S</button>
          <div style={sep} />

          {/* Headings */}
          <select onChange={e => { if (e.target.value) exec('formatBlock', e.target.value); e.target.value = ''; }}
            style={{ ...tb, width: 90, padding: '3px 4px' }} defaultValue="">
            <option value="">Heading…</option>
            <option value="h1">Heading 1</option>
            <option value="h2">Heading 2</option>
            <option value="h3">Heading 3</option>
            <option value="h4">Heading 4</option>
            <option value="p">Paragraph</option>
          </select>

          {/* Font size */}
          <select onChange={e => { if (e.target.value) exec('fontSize', e.target.value); e.target.value = ''; }}
            style={{ ...tb, width: 70, padding: '3px 4px' }} defaultValue="">
            <option value="">Size…</option>
            <option value="1">Small</option>
            <option value="3">Normal</option>
            <option value="5">Large</option>
            <option value="7">Huge</option>
          </select>
          <div style={sep} />

          {/* Colors */}
          <label title="Text color" style={{ ...tb, padding: '1px 4px', display: 'flex', alignItems: 'center', gap: 2 }}>
            🎨 <input type="color" onChange={e => exec('foreColor', e.target.value)} style={{ width: 16, height: 16, border: 'none', padding: 0, cursor: 'pointer' }} />
          </label>
          <label title="Highlight" style={{ ...tb, padding: '1px 4px', display: 'flex', alignItems: 'center', gap: 2 }}>
            🖍 <input type="color" defaultValue="#ffff00" onChange={e => exec('hiliteColor', e.target.value)} style={{ width: 16, height: 16, border: 'none', padding: 0, cursor: 'pointer' }} />
          </label>
          <div style={sep} />

          {/* Alignment */}
          <button type="button" onClick={() => exec('justifyLeft')} style={tb} title="Align left">⬅</button>
          <button type="button" onClick={() => exec('justifyCenter')} style={tb} title="Center">⬛</button>
          <button type="button" onClick={() => exec('justifyRight')} style={tb} title="Align right">➡</button>
          <div style={sep} />

          {/* Lists */}
          <button type="button" onClick={() => exec('insertUnorderedList')} style={tb} title="Bullet list">• List</button>
          <button type="button" onClick={() => exec('insertOrderedList')} style={tb} title="Numbered list">1. List</button>
          <div style={sep} />

          {/* Insert */}
          <button type="button" onClick={handleInsertLink} style={tb} title="Insert link">🔗</button>
          <button type="button" onClick={handleInsertImage} style={tb} title="Insert image">🖼</button>
          <button type="button" onClick={() => exec('insertHorizontalRule')} style={tb} title="Horizontal rule">—</button>
          <button type="button" onClick={handleInsertTable} style={tb} title="Insert table">⊞</button>
          <div style={sep} />

          {/* Cleanup */}
          <button type="button" onClick={() => exec('removeFormat')} style={tb} title="Remove formatting">✕</button>
        </>)}
      </div>

      {/* Editor area */}
      {mode === 'visual' && (
        <div
          ref={editorRef}
          contentEditable
          suppressContentEditableWarning
          onInput={syncFromEditor}
          onBlur={syncFromEditor}
          onPaste={e => {
            // Clean paste — strip scripts and event handlers
            e.preventDefault();
            const html = e.clipboardData.getData('text/html');
            const text = e.clipboardData.getData('text/plain');
            if (html) {
              const cleaned = html
                .replace(/<script[\s\S]*?<\/script>/gi, '')
                .replace(/\bon\w+\s*=\s*["'][^"']*["']/gi, '')
                .replace(/<(iframe|embed|object|applet|form)[^>]*>[\s\S]*?<\/\1>/gi, '');
              exec('insertHTML', cleaned);
            } else {
              exec('insertText', text);
            }
          }}
          dangerouslySetInnerHTML={{ __html: value }}
          style={{
            minHeight: 300, maxHeight: 600, overflow: 'auto', padding: 14,
            fontSize: 14, lineHeight: 1.6, color: 'var(--text-primary)',
            outline: 'none', cursor: 'text',
          }}
        />
      )}

      {mode === 'html' && (
        <textarea
          value={htmlRef.current}
          onChange={e => handleHtmlChange(e.target.value)}
          style={{
            width: '100%', minHeight: 300, maxHeight: 600, resize: 'vertical',
            padding: 14, border: 'none', outline: 'none', fontFamily: "'SF Mono', 'Fira Code', 'Consolas', monospace",
            fontSize: 12, lineHeight: 1.5, color: '#93c5fd', background: '#0f172a',
            boxSizing: 'border-box',
          }}
          spellCheck={false}
        />
      )}

      {mode === 'preview' && (
        <div
          style={{
            minHeight: 300, maxHeight: 600, overflow: 'auto', padding: 14,
            fontSize: 14, lineHeight: 1.6, color: 'var(--text-primary)',
            background: '#fff',
          }}
          dangerouslySetInnerHTML={{ __html: value }}
        />
      )}

      {/* Footer */}
      <div style={{ display: 'flex', justifyContent: 'space-between', padding: '4px 10px', borderTop: '1px solid var(--border)', background: 'var(--bg-secondary)', fontSize: 11, color: 'var(--text-muted)' }}>
        <span>{value.length.toLocaleString()} / 500,000 chars</span>
        <span>eBay: no scripts, iframes, forms, or active content</span>
      </div>
    </div>
  );
}

// ============================================================================
// STYLES
// ============================================================================
const inputStyle: React.CSSProperties = { width: '100%', padding: '10px 14px', borderRadius: 8, border: '1px solid var(--border)', background: 'var(--bg-primary)', color: 'var(--text-primary)', fontSize: 14, outline: 'none', boxSizing: 'border-box' };
const thStyle: React.CSSProperties = { padding: '8px 10px', textAlign: 'left', fontWeight: 600, fontSize: 12, color: 'var(--text-secondary)', whiteSpace: 'nowrap' };
const tdStyle: React.CSSProperties = { padding: '6px 10px', verticalAlign: 'middle' };
const labelStyle: React.CSSProperties = { display: 'block', fontSize: 12, fontWeight: 600, color: 'var(--text-secondary)', marginBottom: 4 };
const primaryBtnStyle: React.CSSProperties = { padding: '10px 24px', borderRadius: 8, border: 'none', background: '#0064D2', color: '#fff', fontWeight: 700, fontSize: 14, cursor: 'pointer' };
const secondaryBtnStyle: React.CSSProperties = { padding: '10px 24px', borderRadius: 8, border: '1px solid var(--border)', background: 'var(--bg-secondary)', color: 'var(--text-primary)', fontWeight: 600, fontSize: 14, cursor: 'pointer' };
const backBtnStyle: React.CSSProperties = { background: 'var(--bg-secondary)', border: '1px solid var(--border)', borderRadius: 8, padding: '6px 14px', fontSize: 13, cursor: 'pointer', color: 'var(--text-secondary)' };
const imgBtnStyle: React.CSSProperties = { background: 'rgba(0,0,0,0.7)', border: 'none', color: '#fff', fontSize: 10, width: 18, height: 18, borderRadius: 3, cursor: 'pointer', display: 'flex', alignItems: 'center', justifyContent: 'center', padding: 0 };

// ============================================================================
// EBAY VARIANT EDIT MODAL
// ============================================================================

interface EbayVariantModalProps {
  variant: ChannelVariantDraft;
  parentDraft: EbayDraft;
  onSave: (updated: ChannelVariantDraft, applyPricingToAll: boolean) => void;
  onClose: () => void;
}

function EbayVariantModal({ variant, parentDraft, onSave, onClose }: EbayVariantModalProps) {
  const [local, setLocal] = React.useState<ChannelVariantDraft>({ ...variant });
  const [applyPricingToAll, setApplyPricingToAll] = React.useState(false);
  const [imageInputUrl, setImageInputUrl] = React.useState('');

  const set = <K extends keyof ChannelVariantDraft>(field: K, value: ChannelVariantDraft[K]) =>
    setLocal(v => ({ ...v, [field]: value }));

  const cs = CURRENCY_SYMBOL[parentDraft.currency] || parentDraft.currency;
  const variantLabel = Object.entries(variant.combination)
    .filter(([, v]) => v).map(([k, v]) => `${k}: ${v}`).join(' / ');

  const mInput: React.CSSProperties = {
    width: '100%', padding: '10px 14px', borderRadius: 8,
    border: '1px solid var(--border)', background: 'var(--bg-primary)',
    color: 'var(--text-primary)', fontSize: 14, outline: 'none', boxSizing: 'border-box',
  };
  const mLabel: React.CSSProperties = {
    display: 'block', fontSize: 12, fontWeight: 600,
    color: 'var(--text-secondary)', marginBottom: 4,
  };
  const mSection: React.CSSProperties = {
    marginBottom: 14, padding: '14px 16px',
    background: 'var(--bg-tertiary, #1a1e28)',
    border: '1px solid var(--border)', borderRadius: 10,
  };
  const mRow: React.CSSProperties = { display: 'flex', gap: 12, flexWrap: 'wrap' as const, marginBottom: 10 };
  const mFlex = (min: number): React.CSSProperties => ({ flex: 1, minWidth: min });
  const mAccent = (c: string): React.CSSProperties => ({ ...mSection, borderLeftWidth: 3, borderLeftColor: c });

  const addImageUrl = () => {
    const url = imageInputUrl.trim();
    if (!url) return;
    const images = [...(local.images || [])];
    if (!images.includes(url)) images.push(url);
    setLocal(v => ({ ...v, images, image: images[0] || v.image }));
    setImageInputUrl('');
  };

  const removeImage = (idx: number) => {
    const images = (local.images || []).filter((_, i) => i !== idx);
    setLocal(v => ({ ...v, images, image: images[0] || '' }));
  };

  const moveImageFirst = (idx: number) => {
    const images = [...(local.images || [])];
    const [item] = images.splice(idx, 1);
    images.unshift(item);
    setLocal(v => ({ ...v, images, image: images[0] }));
  };

  return (
    <div style={{ position: 'fixed', inset: 0, zIndex: 1100, background: 'rgba(0,0,0,0.75)', display: 'flex', alignItems: 'center', justifyContent: 'center', padding: 20 }}
      onClick={e => { if (e.target === e.currentTarget) onClose(); }}>
      <div style={{ background: 'var(--bg-secondary, #13161e)', border: '1px solid var(--border)', borderRadius: 14, width: '100%', maxWidth: 940, maxHeight: '92vh', overflowY: 'auto', padding: '24px 28px', boxShadow: '0 28px 72px rgba(0,0,0,0.6)' }}>

        {/* Header */}
        <div style={{ display: 'flex', alignItems: 'flex-start', justifyContent: 'space-between', marginBottom: 20 }}>
          <div>
            <div style={{ fontSize: 16, fontWeight: 700, color: 'var(--text-primary)', marginBottom: 2 }}>Edit Variant</div>
            <div style={{ fontSize: 13, color: '#d946ef', fontWeight: 600 }}>{variantLabel}</div>
            <div style={{ fontSize: 11, color: 'var(--text-muted)', marginTop: 3 }}>
              Blank fields inherit from the parent listing · SKU: <span style={{ fontFamily: 'monospace' }}>{variant.sku || '—'}</span>
            </div>
          </div>
          <button onClick={onClose} style={{ background: 'none', border: 'none', color: 'var(--text-muted)', fontSize: 22, cursor: 'pointer', lineHeight: 1 }}>✕</button>
        </div>

        {/* ── PRICING & IDENTIFIERS ── */}
        <div style={{ fontSize: 11, fontWeight: 700, textTransform: 'uppercase', letterSpacing: 1.5, color: EBAY_BLUE, marginBottom: 6 }}>Pricing &amp; Identifiers</div>
        <div style={mAccent(EBAY_BLUE)}>
          {/* Apply to all */}
          <label style={{ display: 'flex', alignItems: 'center', gap: 8, padding: '7px 10px', background: applyPricingToAll ? 'rgba(0,100,210,0.1)' : 'var(--bg-secondary)', border: `1px solid ${applyPricingToAll ? EBAY_BLUE : 'var(--border)'}`, borderRadius: 7, marginBottom: 12, cursor: 'pointer', fontSize: 13, color: applyPricingToAll ? EBAY_BLUE : 'var(--text-muted)' }}>
            <input type="checkbox" checked={applyPricingToAll} onChange={e => setApplyPricingToAll(e.target.checked)} style={{ accentColor: EBAY_BLUE }} />
            <span style={{ fontWeight: 600 }}>Apply price &amp; stock to all variants</span>
            <span style={{ fontSize: 11, fontWeight: 400, color: 'var(--text-muted)' }}>— overwrites price and stock on all rows</span>
          </label>
          <div style={mRow}>
            <div style={mFlex(120)}><label style={mLabel}>SKU *</label><input style={mInput} value={local.sku} onChange={e => set('sku', e.target.value)} /></div>
            <div style={mFlex(100)}><label style={mLabel}>Price ({cs}) *</label><input style={mInput} type="number" step="0.01" value={local.price} onChange={e => set('price', e.target.value)} placeholder={parentDraft.price} /></div>
            <div style={mFlex(80)}><label style={mLabel}>Stock</label><input style={mInput} type="number" value={local.stock} onChange={e => set('stock', e.target.value)} placeholder="0" /></div>
          </div>
          <div style={mRow}>
            <div style={mFlex(140)}><label style={mLabel}>EAN / Barcode</label><input style={mInput} value={local.ean} onChange={e => set('ean', e.target.value)} placeholder="e.g. 5060000000000" /></div>
            <div style={mFlex(140)}><label style={mLabel}>UPC</label><input style={mInput} value={local.upc || ''} onChange={e => set('upc', e.target.value)} /></div>
          </div>
        </div>

        {/* ── IMAGES ── */}
        <div style={{ fontSize: 11, fontWeight: 700, textTransform: 'uppercase', letterSpacing: 1.5, color: '#22c55e', marginBottom: 6 }}>Images</div>
        <div style={mAccent('#22c55e')}>
          <div style={{ fontSize: 12, color: 'var(--text-muted)', marginBottom: 10 }}>
            Per-variant images override the parent listing images for this specific variation on eBay.
          </div>

          {/* Image grid */}
          {(local.images || []).length > 0 && (
            <div style={{ display: 'flex', gap: 8, flexWrap: 'wrap', marginBottom: 10 }}>
              {(local.images || []).map((url, idx) => (
                <div key={idx} style={{ position: 'relative', border: idx === 0 ? '2px solid #22c55e' : '1px solid var(--border)', borderRadius: 8, overflow: 'hidden' }}>
                  <img src={url} alt="" style={{ width: 72, height: 72, objectFit: 'contain', display: 'block', background: 'var(--bg-primary)' }} onError={e => { (e.target as HTMLImageElement).src = ''; }} />
                  {idx === 0 && <div style={{ position: 'absolute', bottom: 0, left: 0, right: 0, background: '#22c55e', color: '#fff', fontSize: 9, fontWeight: 700, textAlign: 'center', padding: '2px 0' }}>PRIMARY</div>}
                  <div style={{ position: 'absolute', top: 2, right: 2, display: 'flex', gap: 2 }}>
                    {idx > 0 && <button onClick={() => moveImageFirst(idx)} title="Set as primary" style={{ ...imgBtnStyle, background: 'rgba(34,197,94,0.8)' }}>★</button>}
                    <button onClick={() => removeImage(idx)} style={{ ...imgBtnStyle, background: 'rgba(220,38,38,0.8)' }}>✕</button>
                  </div>
                </div>
              ))}
            </div>
          )}

          {/* Add image URL */}
          <div style={{ display: 'flex', gap: 8 }}>
            <input style={{ ...mInput, flex: 1 }} value={imageInputUrl} onChange={e => setImageInputUrl(e.target.value)}
              onKeyDown={e => e.key === 'Enter' && addImageUrl()}
              placeholder="Paste image URL and press Enter or Add" />
            <button onClick={addImageUrl} style={{ padding: '10px 18px', borderRadius: 8, border: 'none', background: '#22c55e', color: '#fff', fontSize: 13, fontWeight: 700, cursor: 'pointer', whiteSpace: 'nowrap' as const }}>+ Add</button>
          </div>
          <div style={{ fontSize: 11, color: 'var(--text-muted)', marginTop: 5 }}>
            eBay allows up to 12 images per variant. First image is the primary. Click ★ to promote.
          </div>
        </div>

        {/* ── PRODUCT DETAILS ── */}
        <div style={{ fontSize: 11, fontWeight: 700, textTransform: 'uppercase', letterSpacing: 1.5, color: '#f59e0b', marginBottom: 6 }}>Product Details</div>
        <div style={mAccent('#f59e0b')}>
          <div style={mRow}>
            <div style={{ flex: 1, minWidth: 300 }}>
              <label style={mLabel}>Title (variant-specific override)</label>
              <input style={mInput} value={local.title || ''} onChange={e => set('title', e.target.value)} placeholder={parentDraft.title || 'Leave blank to use parent title'} />
            </div>
          </div>
          <div style={mRow}>
            <div style={mFlex(140)}><label style={mLabel}>Brand</label><input style={mInput} value={local.brand || ''} onChange={e => set('brand', e.target.value)} placeholder={parentDraft.brand || 'Brand name'} /></div>
            <div style={mFlex(140)}><label style={mLabel}>Condition</label>
              <select style={{ ...mInput, appearance: 'auto' }} value={local.condition || parentDraft.condition || 'NEW'} onChange={e => set('condition', e.target.value)}>
                {CONDITION_OPTIONS.map(o => <option key={o.value} value={o.value}>{o.label}</option>)}
              </select>
            </div>
          </div>
          <div>
            <label style={mLabel}>Description (variant-specific override)</label>
            <textarea style={{ ...mInput, minHeight: 80, resize: 'vertical' as const }} value={local.description || ''} onChange={e => set('description', e.target.value)}
              placeholder={`Leave blank to use parent description`} />
          </div>
        </div>

        {/* ── DIMENSIONS & WEIGHT ── */}
        <div style={{ fontSize: 11, fontWeight: 700, textTransform: 'uppercase', letterSpacing: 1.5, color: '#8b5cf6', marginBottom: 6 }}>Dimensions &amp; Weight</div>
        <div style={mAccent('#8b5cf6')}>
          <div style={mRow}>
            <div style={mFlex(80)}><label style={mLabel}>Length</label><input style={mInput} type="number" step="0.1" value={local.length || ''} onChange={e => set('length', e.target.value)} placeholder="—" /></div>
            <div style={mFlex(80)}><label style={mLabel}>Width</label><input style={mInput} type="number" step="0.1" value={local.width || ''} onChange={e => set('width', e.target.value)} placeholder="—" /></div>
            <div style={mFlex(80)}><label style={mLabel}>Height</label><input style={mInput} type="number" step="0.1" value={local.height || ''} onChange={e => set('height', e.target.value)} placeholder="—" /></div>
            <div style={mFlex(80)}><label style={mLabel}>Dim Unit</label>
              <select style={{ ...mInput, appearance: 'auto' }} value={local.lengthUnit || 'CENTIMETER'} onChange={e => set('lengthUnit', e.target.value)}>
                {DIM_UNITS.map(u => <option key={u.value} value={u.value}>{u.label}</option>)}
              </select>
            </div>
            <div style={mFlex(80)}><label style={mLabel}>Weight</label><input style={mInput} type="number" step="0.001" value={local.weight || ''} onChange={e => set('weight', e.target.value)} placeholder="—" /></div>
            <div style={mFlex(80)}><label style={mLabel}>Weight Unit</label>
              <select style={{ ...mInput, appearance: 'auto' }} value={local.weightUnit || 'KILOGRAM'} onChange={e => set('weightUnit', e.target.value)}>
                {WT_UNITS.map(u => <option key={u.value} value={u.value}>{u.label}</option>)}
              </select>
            </div>
          </div>
        </div>

        {/* Actions */}
        <div style={{ display: 'flex', gap: 10, justifyContent: 'flex-end', marginTop: 4 }}>
          <button onClick={onClose} style={{ padding: '9px 20px', borderRadius: 7, border: '1px solid var(--border)', background: 'var(--bg-tertiary)', color: 'var(--text-muted)', fontSize: 13, cursor: 'pointer', fontWeight: 500 }}>Cancel</button>
          <button onClick={() => onSave(local, applyPricingToAll)} style={{ padding: '9px 24px', borderRadius: 7, border: 'none', background: EBAY_BLUE, color: '#fff', fontSize: 13, cursor: 'pointer', fontWeight: 700 }}>
            {applyPricingToAll ? 'Save & Apply Pricing to All' : 'Save Variant'}
          </button>
        </div>
      </div>
    </div>
  );
}
