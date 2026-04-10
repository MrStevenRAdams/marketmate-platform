// ============================================================================
// TEMU LISTING PAGE
// ============================================================================
// Arrives with ?product_id=xxx from product edit page dropdown.
// Single form view — no step wizard.

import { useState, useEffect } from 'react';
import { useSearchParams, useNavigate } from 'react-router-dom';
import { getActiveTenantId } from '../../contexts/TenantContext';
import { temuApi, TemuCategory, TemuShippingTemplate, TemuBrand } from '../../services/temu-api';
import { ChannelVariantDraft } from '../../services/channel-types';
import ConfiguratorSelector from './ConfiguratorSelector';
import { configuratorService, ConfiguratorDetail } from '../../services/configurator-api';
import { listingService } from '../../services/marketplace-api';
import { assembleTemuTitle, fetchKeywordIntelligence } from '../../utils/keywordUtils';
import { KeywordIntelligenceResponse } from '../../types/seo';

const CHINA_PROVINCES = [
  'Anhui','Beijing','Chongqing','Fujian','Gansu','Guangdong','Guangxi','Guizhou',
  'Hainan','Hebei','Heilongjiang','Henan','Hubei','Hunan','Inner Mongolia',
  'Jiangsu','Jiangxi','Jilin','Liaoning','Ningxia','Qinghai','Shaanxi',
  'Shandong','Shanghai','Shanxi','Sichuan','Tianjin','Tibet','Xinjiang',
  'Yunnan','Zhejiang',
];

interface DraftData {
  goodsId: number;
  title: string;
  description: string;
  bulletPoints: string[];
  catId: number;
  catName: string;
  catPath: string[];
  sku: string;
  // Prices stored in CURRENCY (e.g. 8.22), converted to pence on submit
  retailPrice: string;
  listPrice: string;
  currency: string;
  images: string[];
  dimensions: { lengthCm: string; widthCm: string; heightCm: string } | null;
  weight: { weightG: string } | null;
  quantity: number;
  goodsProperties: Record<string, any>[];
  shippingTemplate: string;
  brand: Record<string, any> | null;
  fulfillmentType: number;
  shipmentLimitDay: number;
  originRegion1: string;
  originRegion2: string;
}

// Compliance types parsed from Temu API
interface CertUploadItem {
  alias: string;
  contentType: number; // 1=document, 2=test report, 4=actual photos, 5=directives
  uploadRequire: string;
  examplePics: string[];
}

interface CertEntry {
  certName: string;
  certType: number;
  isRequired: boolean;
  uploadItems: CertUploadItem[];
}

interface CheckItem {
  checkShowName: string;
  exampleDesc: string;
  examplePics: string[];
}

interface TemplateProp {
  pid: number;
  templatePid: number;
  refPid: number;
  name: string;
  required: boolean;
  isSale: boolean;
  values: { vid: number; label: string }[];
  inputType: string;
}

// TemuVariant extends ChannelVariantDraft with Temu-specific pricing fields
interface TemuVariant extends ChannelVariantDraft {
  retailPrice: string;  // Temu base price (maps to ChannelVariantDraft.price)
  listPrice: string;    // Temu strikethrough price
}

// Normalise a price string coming from the backend into a 2-decimal currency
// string.  The backend now always sends currency values (e.g. "7.50"), but
// historically it could send a pence integer string (e.g. "750").  We detect
// the pence case by checking whether the value, when treated as pence, would
// be implausibly large for a typical product price — instead we use a simple
// heuristic: if the value has no decimal point AND is >= 100, treat it as
// pence; otherwise treat it as a currency value.
const normalisePriceFromBackend = (raw: string): string => {
  if (!raw || raw === '') return '';
  const trimmed = raw.trim();
  // If the string already contains a decimal point it's already a currency value
  if (trimmed.includes('.')) {
    const f = parseFloat(trimmed);
    return isNaN(f) ? '' : f.toFixed(2);
  }
  // No decimal — could be pence (e.g. "750") or a whole-pound value (e.g. "8")
  const n = parseInt(trimmed, 10);
  if (isNaN(n)) return '';
  // Heuristic: whole-number values >= 100 are almost certainly pence
  if (n >= 100) return (n / 100).toFixed(2);
  return n.toFixed(2);
};

const currencySymbol = (code: string) => code === 'GBP' ? '£' : code === 'EUR' ? '€' : '$';

export default function TemuListingCreate() {
  const navigate = useNavigate();
  const [searchParams] = useSearchParams();
  const productId = searchParams.get('product_id');
  const credentialId = searchParams.get('credential_id') || '';
  const listingId = searchParams.get('listing_id') || '';  // present when editing an existing listing

  const [loading, setLoading] = useState(true);
  const [error, setError] = useState('');
  const [draft, setDraft] = useState<DraftData | null>(null);

  const [brands, setBrands] = useState<TemuBrand[]>([]);
  const [selectedBrand, setSelectedBrand] = useState<TemuBrand | null>(null);
  const [brandsError, setBrandsError] = useState('');

  const [shippingTemplates, setShippingTemplates] = useState<TemuShippingTemplate[]>([]);
  const [selectedShipping, setSelectedShipping] = useState('');

  const [templateProperties, setTemplateProperties] = useState<TemplateProp[]>([]);
  const [propertyValues, setPropertyValues] = useState<Record<string, any>>({});

  // Compliance
  const [certEntries, setCertEntries] = useState<CertEntry[]>([]);
  const [checkItems, setCheckItems] = useState<CheckItem[]>([]);

  // Category picker
  const [catLevels, setCatLevels] = useState<TemuCategory[][]>([]);
  const [catSelections, setCatSelections] = useState<(TemuCategory | null)[]>([]);
  const [catLoading, setCatLoading] = useState(false);
  const [catModalOpen, setCatModalOpen] = useState(false);
  const [catError, setCatError] = useState('');
  const [brandSearch, setBrandSearch] = useState('');

  const [submitting, setSubmitting] = useState(false);
  const [submitResult, setSubmitResult] = useState<{ ok: boolean; goodsId?: number; isUpdate?: boolean; error?: string; request?: any; response?: any } | null>(null);
  const [aiGenerating, setAiGenerating] = useState(false);
  const [aiApplied, setAiApplied] = useState(false);
  const [aiError, setAiError] = useState('');

  // ── Keyword intelligence (Session 8) ──────────────────────────────────────
  const [kwData, setKwData] = useState<KeywordIntelligenceResponse | null>(null);
  const [showTitlePopover, setShowTitlePopover] = useState(false);

  // ── Configurator (CFG-07) ──
  const [selectedConfigurator, setSelectedConfigurator] = useState<ConfiguratorDetail | null>(null);

  const handleConfiguratorSelect = (cfg: ConfiguratorDetail | null) => {
    setSelectedConfigurator(cfg);
    if (!cfg) return;
    // Pre-populate category id + path
    if (cfg.category_id && draft) {
      const catIdNum = parseInt(cfg.category_id, 10);
      if (!isNaN(catIdNum)) {
        // category_path is stored as "Parent › Sub › Leaf" string — split it back to array
        const pathArr = cfg.category_path ? cfg.category_path.split(' › ').map((s: string) => s.trim()).filter(Boolean) : [];
        setDraft(d => d ? { ...d, catId: catIdNum, catName: cfg.category_path || cfg.category_id!, catPath: pathArr } : d);
      }
    }
    // Pre-populate attribute defaults as goods properties
    if (cfg.attribute_defaults && cfg.attribute_defaults.length > 0 && draft) {
      const props: Record<string, any>[] = [];
      for (const attr of cfg.attribute_defaults) {
        if (attr.source === 'default_value' && attr.default_value) {
          props.push({ name: attr.attribute_name, value: attr.default_value });
        }
      }
      if (props.length > 0) {
        setDraft(d => d ? { ...d, goodsProperties: [...(d.goodsProperties || []), ...props] } : d);
      }
    }
    // Pre-populate shipping template
    if (cfg.shipping_defaults?.shipping_template) {
      setSelectedShipping(cfg.shipping_defaults.shipping_template);
    }
    if (cfg.shipping_defaults?.fulfillment_type) {
      setDraft(d => d ? { ...d, fulfillmentType: Number(cfg.shipping_defaults!.fulfillment_type) } : d);
    }
    if (cfg.shipping_defaults?.shipment_limit_day) {
      setDraft(d => d ? { ...d, shipmentLimitDay: Number(cfg.shipping_defaults!.shipment_limit_day) } : d);
    }
  };

  // Variants
  const [variants, setVariants] = useState<TemuVariant[]>([]);
  const [isVariantProduct, setIsVariantProduct] = useState(false);
  const [editingVariant, setEditingVariant] = useState<TemuVariant | null>(null);

  useEffect(() => {
    if (!productId) { setError('No product_id provided.'); setLoading(false); return; }
    if (listingId) {
      // Edit mode: load existing listing from Firestore, then call prepare
      // only to get template/shipping/brand data (not AI draft)
      loadExistingListing(productId, listingId);
    } else {
      prepareListing(productId);
    }
  }, [productId, listingId]);

  // Session 8: fetch keyword intelligence
  useEffect(() => {
    if (!productId) return;
    fetchKeywordIntelligence(productId, import.meta.env.VITE_API_URL || 'http://localhost:8080/api/v1')
      .then(d => setKwData(d));
  }, [productId]);

  // ── Edit mode: load existing listing from Firestore ───────────────────────
  // Called when listing_id is in the URL. Fetches the stored listing, builds the
  // draft from saved overrides/channel_identifiers, then calls prepare() with
  // skip_ai=true to get shipping templates, brands, and attribute template only
  // — no AI generation.
  async function loadExistingListing(pid: string, lId: string) {
    setLoading(true);
    setError('');
    try {
      // Fetch the existing listing + shipping templates + brands in parallel
      const [detailRes, shipRes, brandRes] = await Promise.allSettled([
        listingService.getDetail(lId),
        temuApi.getShippingTemplates(credentialId || undefined),
        temuApi.listBrands(credentialId || undefined),
      ]);

      if (detailRes.status === 'rejected') {
        setError('Failed to load existing listing');
        setLoading(false);
        return;
      }

      const listingData = detailRes.value.data?.listing ?? detailRes.value.data?.data?.listing;
      if (!listingData) {
        // Fallback to full prepare if listing not found
        prepareListing(pid);
        return;
      }

      const ov = listingData.overrides || {};
      const ci = listingData.channel_identifiers || {};

      // Only treat as full overrides if user has previously submitted this listing
      // (which saves both title AND category). Imported listings only have title+images
      // from ensureListingRecord — fall back to prepare() to get full data from
      // Temu API + PIM (description, category, bullet points etc).
      const hasFullOverrides = !!(ov.title && ov.category_mapping);
      if (!hasFullOverrides) {
        // Imported listing — let prepare() fetch live data from Temu
        prepareListing(pid);
        return;
      }

      // Build draft from stored listing data
      const storedPrice = ov.price != null ? String(ov.price) : '';
      const draftObj: any = {
        goodsId:         ci.listing_id ? parseInt(ci.listing_id) || 0 : 0,
        title:           ov.title || '',
        description:     ov.description || '',
        bulletPoints:    ov.bullet_points || [],
        catId:           ov.category_mapping ? parseInt(ov.category_mapping) || 0 : 0,
        catName:         ov.category_mapping ? `Category ${ov.category_mapping}` : '',
        catPath:         [],
        sku:             ci.sku || listingData.sku || '',
        retailPrice:     storedPrice,
        listPrice:       '',
        currency:        'GBP',
        images:          ov.images || [],
        dimensions:      null,
        weight:          null,
        quantity:        ov.quantity || 1,
        goodsProperties: ov.attributes?.goodsProperties || [],
        shippingTemplate: ov.attributes?.shippingTemplate || '',
        brand:           null,
        fulfillmentType: ov.attributes?.fulfillmentType || 1,
        shipmentLimitDay: ov.attributes?.shipmentLimitDay || 2,
        originRegion1:   ov.attributes?.originRegion1 || '',
        originRegion2:   ov.attributes?.originRegion2 || '',
      };

      setDraft(draftObj);

      // Load shipping templates
      if (shipRes.status === 'fulfilled') {
        const shipData = shipRes.value.data;
        const templates = shipData?.templates || shipData?.data?.templates || [];
        setShippingTemplates(templates);
        if (!draftObj.shippingTemplate && shipData?.defaultId) {
          setDraft((d: any) => ({ ...d, shippingTemplate: String(shipData.defaultId) }));
        }
      }

      // Load brands
      if (brandRes.status === 'fulfilled') {
        setBrands(brandRes.value.data?.brands || brandRes.value.data?.data || []);
      }

      // Fetch template for the category to show attributes
      if (draftObj.catId) {
        try {
          const tmplRes = await temuApi.getTemplate(draftObj.catId);
          if (tmplRes.data?.ok) {
            const props = parseTemplate(tmplRes.data.template);
            setTemplateProperties(props);
            // Pre-fill property values from stored goodsProperties
            const preValues: Record<string, string> = {};
            for (const gp of (draftObj.goodsProperties || [])) {
              if (gp.pid) preValues[String(gp.pid)] = gp.vid ? String(gp.vid) : gp.value || '';
            }
            setPropertyValues(preValues);
          }
        } catch { /* non-fatal — attributes section will be empty */ }
      }

    } catch (err: any) {
      setError(err?.message || 'Failed to load listing');
    } finally {
      setLoading(false);
    }
  }

  async function prepareListing(pid: string, catIdOverride?: number) {
    setLoading(true);
    setError('');
    // Clear attributes immediately so stale properties from the previous
    // category never flash while the new template is being fetched
    setTemplateProperties([]);
    setPropertyValues({});
    setCertEntries([]);
    setCheckItems([]);
    try {
      const payload: any = { product_id: pid };
      if (catIdOverride != null) payload.catId = catIdOverride;
      if (credentialId) payload.credential_id = credentialId;

      const [prepRes, shipRes, brandRes] = await Promise.allSettled([
        temuApi.prepare(payload),
        temuApi.getShippingTemplates(credentialId || undefined),
        temuApi.listBrands(credentialId || undefined),
      ]);

      // Fetch channel config defaults (non-fatal if missing)
      let temuDefaults: any = null;
      if (credentialId) {
        try {
          const cfgRes = await fetch(
            `${(import.meta as any).env?.VITE_API_URL || 'http://localhost:8080/api/v1'}/marketplace/credentials/${credentialId}/config`,
            { headers: { 'Content-Type': 'application/json', 'X-Tenant-Id': getActiveTenantId() } }
          );
          if (cfgRes.ok) {
            const cfgData = await cfgRes.json();
            temuDefaults = cfgData?.data?.temu_defaults || cfgData?.temu_defaults || null;
          }
        } catch { /* non-fatal */ }
      }

      if (prepRes.status === 'rejected' || !prepRes.value.data?.ok) {
        setError(prepRes.status === 'fulfilled' ? prepRes.value.data?.error || 'Failed' : 'Network error');
        setLoading(false);
        return;
      }
      const data = prepRes.value.data;
      const d = data.draft;
      if (d) {
        // Convert prices from pence to currency
        const currency = d.price?.currency || 'GBP';
        const draftObj: any = {
          goodsId: d.goodsId || 0,
          title: d.title || '', description: d.description || '', bulletPoints: d.bulletPoints || [],
          catId: d.catId || 0, catName: d.catName || '', catPath: d.catPath || [],
          sku: d.sku || '',
          retailPrice: d.price?.baseAmount ? normalisePriceFromBackend(d.price.baseAmount) : '',
          listPrice: d.price?.listAmount ? normalisePriceFromBackend(d.price.listAmount) : '',
          currency,
          images: d.images || [],
          dimensions: d.dimensions || null, weight: d.weight || null,
          quantity: d.quantity || 0,
          goodsProperties: d.goodsProperties || [], shippingTemplate: d.shippingTemplate || '',
          brand: d.brand || null, fulfillmentType: d.fulfillmentType || 1,
          shipmentLimitDay: d.shipmentLimitDay || 2,
          originRegion1: d.originRegion1 || '', originRegion2: d.originRegion2 || '',
        };

        // Apply channel-level listing defaults for fields not already set by the
        // prepare response (i.e. no stored Temu raw data). Only applied on first
        // load (catIdOverride == null) to avoid overwriting a category change.
        if (temuDefaults && catIdOverride == null) {
          if (!draftObj.fulfillmentType || draftObj.fulfillmentType === 1) {
            if (temuDefaults.fulfillment_type) draftObj.fulfillmentType = temuDefaults.fulfillment_type;
          }
          if (!draftObj.shipmentLimitDay || draftObj.shipmentLimitDay === 2) {
            if (temuDefaults.shipment_limit_day) draftObj.shipmentLimitDay = temuDefaults.shipment_limit_day;
          }
          if (!draftObj.originRegion1 && temuDefaults.origin_region1) {
            draftObj.originRegion1 = temuDefaults.origin_region1;
          }
          if (!draftObj.originRegion2 && temuDefaults.origin_region2) {
            draftObj.originRegion2 = temuDefaults.origin_region2;
          }
        }

        // ── AI Override: check sessionStorage for AI-generated content ──
        const aiFlag = searchParams.get('ai');
        if (aiFlag === 'pending') {
          // AI will be triggered after template is parsed — see below
        }

        setDraft(prev => ({
          ...draftObj,
          // Preserve pre-fetched catPath if backend couldn't build one
          catPath: (d.catPath && d.catPath.length > 0) ? d.catPath : (prev?.catPath || []),
          catName: d.catName || prev?.catName || '',
        }));
        if (data.template) {
          console.log('[prepareListing] Template received, top-level keys:', Object.keys(data.template));
          parseTemplate(data.template, d.goodsProperties);
        } else {
          console.warn('[prepareListing] No template in response');
        }
        if (d.brand?.brandId) setSelectedBrand({ brandId: Number(d.brand.brandId), brandName: '', trademarkId: Number(d.brand.trademarkId || 0), trademarkBizId: Number(d.brand.trademarkBizId || 0) } as TemuBrand);

        // Auto-open category picker if no category was resolved — user must pick before submitting
        if (!draftObj.catId) {
          setCatModalOpen(true);
          try { const res = await temuApi.getCategories(undefined, credentialId || undefined); if (res.data?.ok) { setCatLevels([res.data.items || []]); setCatSelections([null]); setCatError(''); } else { setCatError(res.data?.error || 'Failed to load categories'); setCatLevels([[]]); } }
          catch (e: any) { setCatError(e?.response?.data?.error || e?.message || 'Failed to load categories'); setCatLevels([[]]); setCatSelections([null]); }
        }
      }

      // Parse compliance from prepare response
      if (data.compliance) {
        console.log('[TemuListing] Compliance data received:', JSON.stringify(data.compliance).substring(0, 200));
        parseCompliance(data.compliance);
      } else {
        console.log('[TemuListing] No compliance data in prepare response');
      }

      // ── AI Generation: if ?ai=pending, call AI with template properties as schema ──
      const aiFlag = searchParams.get('ai');
      if (aiFlag === 'pending' && data.draft && data.template) {
        setAiGenerating(true);
        try {
          // Extract schema fields from the Temu template
          const schemaFields: import('../../services/ai-api').SchemaField[] = [];
          let goodsProps: any[] = [];
          for (const path of [['goodsProperties'], ['templateInfo', 'goodsProperties'], ['rawTemplate', 'templateInfo', 'goodsProperties'], ['template', 'goodsProperties']]) {
            let node: any = data.template;
            for (const key of path) node = node?.[key];
            if (Array.isArray(node)) { goodsProps = node; break; }
          }
          for (const prop of goodsProps) {
            if (!prop) continue;
            const name = prop.name || prop.propertyName || '';
            if (name.toLowerCase() === 'brand' || name.toLowerCase() === 'brand name') continue;
            const propValues = (prop.values || prop.valueList || prop.options || [])
              .map((v: any) => v.value || v.label || v.name || '').filter(Boolean);
            schemaFields.push({
              name,
              display_name: name,
              data_type: propValues.length > 0 ? 'enum' : 'string',
              required: prop.required === true || prop.isRequired === true,
              allowed_values: propValues,
            });
          }

          const categoryName = data.draft.catName || '';
          const categoryId = String(data.draft.catId || '');

          const { aiService: aiApi } = await import('../../services/ai-api');
          const aiRes = await aiApi.generateWithSchema({
            product_id: pid,
            channel: 'temu',
            category_id: categoryId,
            category_name: categoryName,
            category_path: data.draft.catPath || [],
            fields: schemaFields,
          });

          const aiListing = aiRes.data.data?.listings?.[0];
          if (aiListing) {
            setDraft((prev: any) => {
              if (!prev) return prev;
              const updated = { ...prev };
              if (aiListing.title) updated.title = aiListing.title;
              if (aiListing.description) updated.description = aiListing.description;
              if (aiListing.bullet_points?.length) updated.bulletPoints = aiListing.bullet_points;
              return updated;
            });

            // Try to map AI attributes to template property values
            if (aiListing.attributes) {
              setPropertyValues((prev: any) => {
                const updated = { ...prev };
                // This is best-effort — template props use PIDs, AI returns by name
                // The user will need to review
                return updated;
              });
            }
            setAiApplied(true);
          }
        } catch (aiErr: any) {
          setAiError(aiErr.response?.data?.error || aiErr.message || 'AI generation failed');
        }
        setAiGenerating(false);
      }

      if (shipRes.status === 'fulfilled' && shipRes.value.data?.ok) {
        setShippingTemplates(shipRes.value.data.templates || []);
        // Priority: draft's own template > channel default > API default
        const defaultTemplate = d?.shippingTemplate
          || (temuDefaults?.shipping_template_id || '')
          || shipRes.value.data.defaultId
          || '';
        setSelectedShipping(defaultTemplate);
      }

      // Brands sorted ascending
      if (brandRes.status === 'fulfilled' && brandRes.value.data?.ok) {
        const sorted = (brandRes.value.data.brands || []).sort((a: TemuBrand, b: TemuBrand) =>
          (a.brandName || '').localeCompare(b.brandName || '')
        );
        setBrands(sorted);
        if (sorted.length === 0) setBrandsError('API returned 0 brands — check Cloud Run logs for raw response keys');
      } else if (brandRes.status === 'fulfilled') {
        setBrandsError(brandRes.value.data?.error || 'Failed to load brands');
      } else {
        setBrandsError('Network error loading brands');
      }

      // VAR-01: load variants from prepare response (uses parent_id child-product strategy)
      if (d?.variants && d.variants.length > 0) {
        setIsVariantProduct(true);
        const fallbackPrice = d.price?.baseAmount ? normalisePriceFromBackend(d.price.baseAmount) : '';
        const loadedVariants: TemuVariant[] = d.variants.map(v => ({
          ...v,
          retailPrice: v.price ? normalisePriceFromBackend(String(v.price)) : fallbackPrice,
          listPrice: '',
        }));
        setVariants(loadedVariants);
      }
    } catch (err: any) {
      setError(err?.response?.data?.error || err.message || 'Network error');
    }
    setLoading(false);
  };

  function parseCompliance(raw: Record<string, any>) {
    console.log('[TemuListing] parseCompliance keys:', Object.keys(raw));
    // The compliance data may be at root or nested under result/data
    const data = raw.result || raw.data || raw;
    
    // Parse goodsCertList → certificate upload entries
    const certs: CertEntry[] = [];
    const certList = data.goodsCertList || raw.goodsCertList || [];
    console.log('[TemuListing] goodsCertList count:', certList.length);
    for (const cert of certList) {
      if (!cert || typeof cert !== 'object') continue;
      const items: CertUploadItem[] = [];
      for (const item of (cert.goodsCertNeedUploadItemList || [])) {
        // Only show uploadable items (contentType 1=doc, 2=test report, 4=photos). Skip 5 (directives checkboxes).
        if (!item || (item.contentType !== 1 && item.contentType !== 2 && item.contentType !== 4)) continue;
        const alias = item.alias || (item.contentType === 2 ? 'Test Report' : item.contentType === 4 ? 'Actual Photos' : 'Document');
        items.push({
          alias,
          contentType: item.contentType,
          uploadRequire: item.uploadExample?.uploadRequire || '',
          examplePics: item.uploadExample?.uploadExamplePicUrl || [],
        });
      }
      if (items.length > 0) {
        certs.push({ certName: cert.certName || 'Certificate', certType: cert.certType || 0, isRequired: cert.isRequired === true, uploadItems: items });
      }
    }
    setCertEntries(certs);
    console.log('[TemuListing] Parsed cert entries:', certs.length);

    // Parse checkInfoList → labelling/marking checklist
    const checks: CheckItem[] = [];
    const checkList = data.checkInfoList || raw.checkInfoList || [];
    console.log('[TemuListing] checkInfoList count:', checkList.length);
    for (const ch of checkList) {
      if (!ch || typeof ch !== 'object') continue;
      checks.push({
        checkShowName: ch.checkShowName || ch.checkName || 'Check',
        exampleDesc: ch.exampleDesc || '',
        examplePics: ch.examplePicList || [],
      });
    }
    setCheckItems(checks);
    console.log('[TemuListing] Parsed check items:', checks.length);
  };

  function parseTemplate(tmpl: Record<string, any>, existingProps?: Record<string, any>[]) {
    const props: TemplateProp[] = [];
    const values: Record<string, any> = {};
    let goodsProps: any[] = [];
    const searchPaths = [
      ['goodsProperties'],
      ['templateInfo', 'goodsProperties'],          // primary: GetTemplate returns result which has templateInfo
      ['rawTemplate', 'templateInfo', 'goodsProperties'], // cache: rawTemplate stores the full result
      ['rawTemplate', 'goodsProperties'],
      ['rawTemplate', 'result', 'templateInfo', 'goodsProperties'], // full API response stored as rawTemplate
      ['rawTemplate', 'result', 'goodsProperties'],
      ['template', 'goodsProperties'],
      ['info', 'goodsProperties'],
      ['result', 'goodsProperties'],
    ];
    for (const path of searchPaths) {
      let node: any = tmpl;
      for (const key of path) node = node?.[key];
      if (Array.isArray(node) && node.length > 0) {
        goodsProps = node;
        console.log(`[parseTemplate] Found ${node.length} props at: ${path.join('.')}`);
        break;
      }
    }
    if (goodsProps.length === 0) {
      console.warn('[parseTemplate] No goodsProperties found. Top-level keys:', Object.keys(tmpl || {}));
      if (tmpl?.rawTemplate) console.warn('[parseTemplate] rawTemplate keys:', Object.keys(tmpl.rawTemplate));
    }
    for (const prop of goodsProps) {
      if (!prop || typeof prop !== 'object') continue;
      const name = prop.name || prop.propertyName || prop.label || '';
      if (name.toLowerCase() === 'brand' || name.toLowerCase() === 'brand name') continue;
      const pid = prop.pid || prop.propertyId || 0;
      const templatePid = prop.templatePid || prop.pid || 0;
      const refPid = prop.refPid || 0;
      const required = prop.required === true || prop.isRequired === true || prop.isRequired === 1;
      const isSale = prop.isSale === true || prop.isSale === 1 || prop.type === 'sale';
      const propValues: { vid: number; label: string }[] = [];
      for (const v of (prop.values || prop.valueList || prop.options || [])) {
        if (typeof v === 'object' && v !== null) propValues.push({ vid: v.vid || v.valueId || v.id || 0, label: v.value || v.label || v.name || '' });
      }
      props.push({ pid, templatePid, refPid, name, required, isSale, values: propValues, inputType: propValues.length > 0 ? 'select' : 'text' });
      if (existingProps) {
        const match = existingProps.find(ep => ep.pid === pid || ep.templatePid === templatePid);
        if (match) values[String(pid)] = { vid: match.vid, value: match.value || '', pid: match.pid, templatePid: match.templatePid, refPid: match.refPid };
      }
    }
    setTemplateProperties(props);
    setPropertyValues(values);
  };

  // ── Category cascading picker (modal) ──
  const openCategoryPicker = async () => {
    setCatModalOpen(true);
    if (catLevels.length === 0) {
      setCatLoading(true);
      try { const res = await temuApi.getCategories(undefined, credentialId || undefined); if (res.data?.ok) { setCatLevels([res.data.items || []]); setCatSelections([null]); setCatError(''); } else { setCatError(res.data?.error || 'Failed to load categories'); setCatLevels([[]]); } }
      catch (e: any) { setCatError(e?.response?.data?.error || e?.message || 'Failed to load categories'); setCatLevels([[]]); setCatSelections([null]); }
      setCatLoading(false);
    }
  };

  const handleCatSelect = async (levelIndex: number, catId: string) => {
    if (!catId) {
      setCatLevels(prev => prev.slice(0, levelIndex + 1));
      setCatSelections(prev => { const next = prev.slice(0, levelIndex); next[levelIndex] = null; return next; });
      return;
    }
    const cat = catLevels[levelIndex]?.find(c => String(c.catId) === catId);
    if (!cat) return;
    const newSelections = catSelections.slice(0, levelIndex);
    newSelections[levelIndex] = cat;
    setCatSelections(newSelections);
    if (cat.leaf) {
      setCatModalOpen(false);
      // Fetch full ancestor path from backend before calling prepare
      try {
        const pathRes = await temuApi.getCategoryPath(cat.catId);
        if (pathRes.data?.ok && pathRes.data.path.length > 0) {
          // Update draft with path so categoryDisplay shows immediately
          setDraft(d => d ? { ...d, catId: cat.catId, catName: cat.catName, catPath: pathRes.data.path } : d);
        }
      } catch { /* non-fatal — prepare will still run */ }
      prepareListing(productId!, cat.catId);
    } else {
      setCatLoading(true);
      const newLevels = catLevels.slice(0, levelIndex + 1);
      try { const res = await temuApi.getCategories(cat.catId, credentialId || undefined); setCatLevels([...newLevels, res.data?.items || []]); setCatSelections([...newSelections, null]); }
      catch { setCatLevels([...newLevels, []]); }
      setCatLoading(false);
    }
  };

  const handleBrandSelect = (brandId: string) => {
    if (!brandId) { setSelectedBrand(null); updateDraft('brand', null); return; }
    const brand = brands.find(b => String(b.brandId) === brandId);
    if (brand) { setSelectedBrand(brand); updateDraft('brand', { brandId: brand.brandId, trademarkId: brand.trademarkId, trademarkBizId: brand.trademarkBizId }); }
  };

  useEffect(() => {
    if (brands.length > 0 && selectedBrand && !selectedBrand.brandName) {
      const match = brands.find(b => b.brandId === selectedBrand.brandId);
      if (match) setSelectedBrand(match);
    }
  }, [brands, selectedBrand]);

  const handlePropertyChange = (pid: number, prop: TemplateProp, value: string, vid?: number) => {
    setPropertyValues(prev => ({ ...prev, [String(pid)]: { pid: prop.pid, templatePid: prop.templatePid, refPid: prop.refPid, vid: vid || undefined, value } }));
  };

  const buildGoodsProperties = (): Record<string, any>[] => {
    const props: Record<string, any>[] = [];
    for (const [, val] of Object.entries(propertyValues)) {
      if (!val || (!val.value && !val.vid)) continue;
      const row: Record<string, any> = {};
      if (val.pid) row.pid = val.pid; if (val.templatePid) row.templatePid = val.templatePid;
      if (val.refPid) row.refPid = val.refPid; if (val.vid) row.vid = val.vid; if (val.value) row.value = val.value;
      props.push(row);
    }
    return props;
  };

  function updateDraft(field: string, value: any) { if (draft) setDraft(prev => prev ? { ...prev, [field]: value } : prev); };

  const updateVariant = (id: string, field: keyof TemuVariant, value: any) => {
    setVariants(prev => prev.map(v => v.id === id ? { ...v, [field]: value } : v));
  };

  const handleSubmit = async () => {
    if (!draft || !productId) return;
    setSubmitting(true);
    setSubmitResult(null);
    try {
      const res = await temuApi.submit({
        product_id: productId, credential_id: credentialId, goodsId: draft.goodsId || 0,
        catId: draft.catId, title: draft.title, description: draft.description,
        bulletPoints: draft.bulletPoints, sku: draft.sku, images: draft.images,
        // Temu's goods.add API expects amount as a decimal currency string (e.g. "7.50"),
        // NOT pence. The pence conversion was incorrect and caused prices to be 100× too high.
        price: { baseAmount: parseFloat(draft.retailPrice).toFixed(2), listAmount: draft.listPrice ? parseFloat(draft.listPrice).toFixed(2) : undefined, currency: draft.currency },
        dimensions: draft.dimensions, weight: draft.weight, quantity: draft.quantity || 0,
        goodsProperties: buildGoodsProperties(), shippingTemplate: selectedShipping,
        brand: selectedBrand ? { brandId: selectedBrand.brandId, trademarkId: selectedBrand.trademarkId, trademarkBizId: selectedBrand.trademarkBizId } : draft.brand,
        specIdList: [], fulfillmentType: draft.fulfillmentType || 1, prepDays: draft.shipmentLimitDay || 2,
        originInfo: draft.originRegion1 ? { originRegionName1: draft.originRegion1, originRegionName2: draft.originRegion2 } : undefined,
        // VAR-01: pass active variants mapped to ChannelVariantDraft shape
        variants: variants.filter(v => v.active).map(v => ({
          id: v.id,
          sku: v.sku,
          combination: v.combination,
          price: v.retailPrice,
          stock: v.stock,
          image: v.image,
          images: v.images || [],
          ean: v.ean || '',
          upc: v.upc || '',
          active: true,
        })),
      });
      setSubmitResult(res.data);
      // ── Configurator join (CFG-07) ──
      if (res.data?.ok && selectedConfigurator) {
        try {
          const listRes = await listingService.list({ product_id: productId!, channel: 'temu', limit: 10 });
          const listings: any[] = listRes.data?.listings || listRes.data?.data || [];
          if (listings.length > 0) {
            const newest = listings[listings.length - 1];
            await configuratorService.assignListings(selectedConfigurator.configurator_id, [newest.listing_id]);
          }
        } catch { /* non-fatal */ }
      }
    } catch (err: any) {
      // The backend returns 200 with ok:false (so it won't normally hit catch),
      // but if there's a network/binding error, extract what we can
      const data = err?.response?.data;
      setSubmitResult({
        ok: false,
        error: data?.error || err.message,
        request: data?.request,
        response: data?.response,
      });
    }
    setSubmitting(false);
  };

  const missingRequired = templateProperties.filter(p => p.required && !propertyValues[String(p.pid)]?.value && !propertyValues[String(p.pid)]?.vid).length;
  const canSubmit = draft && draft.title && draft.sku && selectedShipping && !submitting;
  const categoryDisplay = draft?.catPath?.length
    ? `${draft.catPath.join(' › ')} (${draft.catId})`
    : draft?.catName
      ? `${draft.catName} (${draft.catId})`
      : draft?.catId
        ? `Category ${draft.catId}`
        : 'Not set';

  // ── RENDER ──
  if (loading) return (
    <div style={{ maxWidth: 900, margin: '0 auto', padding: '24px 16px', textAlign: 'center' }}>
      <div style={{ fontSize: 24, marginBottom: 8 }}>⏳</div>
      <p style={{ color: 'var(--text-secondary)' }}>Preparing Temu listing...</p>
      <p style={{ color: 'var(--text-tertiary)', fontSize: 12 }}>Auto-categorizing and mapping product data</p>
    </div>
  );

  if (error && !draft) return (
    <div style={{ maxWidth: 900, margin: '0 auto', padding: '24px 16px' }}>
      <div style={{ padding: 16, background: 'var(--danger-glow)', borderRadius: 8, color: 'var(--danger)' }}>{error}</div>
      <button onClick={() => navigate(-1)} style={{ ...secondaryBtnStyle, marginTop: 16 }}>← Go Back</button>
    </div>
  );

  if (submitResult?.ok) return (
    <div style={{ maxWidth: 900, margin: '0 auto', padding: '24px 16px', textAlign: 'center' }}>
      <div style={{ fontSize: 48, marginBottom: 16 }}>✅</div>
      <h2 style={{ fontSize: 20, fontWeight: 700, marginBottom: 8 }}>{submitResult.isUpdate ? 'Updated on Temu!' : 'Listed on Temu!'}</h2>
      <p style={{ color: 'var(--text-secondary)' }}>Temu Goods ID: <strong>{submitResult.goodsId}</strong></p>
      <p style={{ color: 'var(--text-tertiary)', fontSize: 13, marginBottom: 24 }}>{submitResult.isUpdate ? 'Product updated and resubmitted for review.' : 'Submitted for Temu review.'}</p>
      <div style={{ display: 'flex', gap: 12, justifyContent: 'center' }}>
        <button onClick={() => navigate(-1)} style={secondaryBtnStyle}>← Back to Product</button>
        <button onClick={() => navigate('/marketplace/listings')} style={primaryBtnStyle}>View Listings</button>
      </div>
    </div>
  );

  if (!draft) return null;

  return (
    <div style={{ maxWidth: 900, margin: '0 auto', padding: '24px 16px' }}>
      <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', marginBottom: 24 }}>
        <div style={{ display: 'flex', alignItems: 'center', gap: 12 }}>
          <button onClick={() => navigate(-1)} style={backBtnStyle}>← Back</button>
          <h1 style={{ fontSize: 22, fontWeight: 700, margin: 0 }}>Temu Listing</h1>
        </div>
        <button onClick={handleSubmit} disabled={!canSubmit} style={{ ...primaryBtnStyle, opacity: canSubmit ? 1 : 0.5 }}>
          {submitting ? '⏳ Submitting...' : draft?.goodsId ? 'Update on Temu' : 'Submit to Temu'}
        </button>
      </div>

      {error && <div style={{ padding: 12, background: 'var(--danger-glow)', borderRadius: 8, color: 'var(--danger)', marginBottom: 16, fontSize: 13 }}>{error}</div>}
      {submitResult && !submitResult.ok && <div style={{ padding: 12, background: 'var(--danger-glow)', borderRadius: 8, color: 'var(--danger)', marginBottom: 16, fontSize: 13 }}>{submitResult.error}</div>}

      {draft?.goodsId ? (
        <div style={{ padding: '8px 12px', background: '#e8f4fd', borderRadius: 8, fontSize: 12, color: '#1a73a7', marginBottom: 16, display: 'flex', gap: 8, alignItems: 'center' }}>
          <span style={{ fontWeight: 600 }}>📦 Existing Temu Product</span>
          <span>goodsId: {draft.goodsId}</span>
          <span style={{ color: '#666' }}>— will use <code>goods.update</code></span>
        </div>
      ) : null}

      {/* AI-generated content banner */}
      {aiGenerating && (
        <div style={{ padding: '12px 14px', background: 'linear-gradient(90deg, rgba(139,92,246,0.15), rgba(59,130,246,0.15))', borderRadius: 8, fontSize: 13, color: 'var(--accent-purple)', marginBottom: 16, display: 'flex', gap: 10, alignItems: 'center', border: '1px solid rgba(139,92,246,0.3)' }}>
          <div className="spinner" style={{ width: 16, height: 16, border: '2px solid var(--accent-purple)', borderTopColor: 'transparent', borderRadius: '50%', animation: 'spin 1s linear infinite' }} />
          <span style={{ fontWeight: 600 }}>🤖 AI is generating optimised content using the Temu category template...</span>
        </div>
      )}
      {aiApplied && (
        <div style={{ padding: '10px 14px', background: 'linear-gradient(90deg, rgba(139,92,246,0.15), rgba(59,130,246,0.15))', borderRadius: 8, fontSize: 12, color: 'var(--accent-purple)', marginBottom: 16, display: 'flex', gap: 8, alignItems: 'center', border: '1px solid rgba(139,92,246,0.3)' }}>
          <span style={{ fontSize: 16 }}>🤖</span>
          <span style={{ fontWeight: 600 }}>AI-generated content applied</span>
          <span style={{ color: 'var(--text-muted)' }}>— title, description and bullet points have been filled. Review and edit before submitting.</span>
        </div>
      )}
      {aiError && (
        <div style={{ padding: '10px 14px', background: 'var(--warning-glow)', borderRadius: 8, fontSize: 12, color: 'var(--warning)', marginBottom: 16, border: '1px solid var(--warning)' }}>
          ⚠️ AI generation failed: {aiError} — fill in fields manually.
        </div>
      )}

      <div style={{ display: 'flex', flexDirection: 'column', gap: 20 }}>

        {/* ── Configurator (CFG-07) ── */}
        <ConfiguratorSelector channel="temu" credentialId={credentialId} onSelect={handleConfiguratorSelect} />

        {/* Category */}
        <Section title="Category *">
          <div style={{ display: 'flex', alignItems: 'center', gap: 12 }}>
            <div style={{ flex: 1, padding: '10px 14px', background: 'var(--bg-secondary)', borderRadius: 8, fontSize: 14, color: draft.catId ? 'var(--text-primary)' : 'var(--text-muted)' }}>
              {categoryDisplay}
            </div>
            <button onClick={openCategoryPicker} style={{ ...linkBtnStyle, padding: '8px 16px', border: '1px solid var(--border)', borderRadius: 7, background: 'var(--bg-tertiary)', fontSize: 13 }}>
              {draft.catId ? 'Change ✎' : '+ Pick Category'}
            </button>
          </div>
          {!draft.catId && <p style={{ fontSize: 12, color: 'var(--danger)', marginTop: 4, margin: '4px 0 0' }}>A category is required before submitting.</p>}
        </Section>

        {/* Brand */}
        <Section title="Brand">
          {brandsError && brands.length === 0 ? (
            <div style={{ padding: '8px 12px', background: 'var(--bg-secondary)', borderRadius: 8, fontSize: 12, color: 'var(--danger)', marginBottom: 8 }}>
              {brandsError}
            </div>
          ) : null}
          <select
            value={selectedBrand ? String(selectedBrand.brandId) : ''}
            onChange={e => handleBrandSelect(e.target.value)}
            style={{ ...inputStyle, width: '100%' }}
            disabled={brands.length === 0}
          >
            <option value="">{brands.length === 0 ? 'No authorized brands found' : '— No brand —'}</option>
            {brands.map(b => (
              <option key={b.brandId} value={String(b.brandId)}>
                {b.brandName || `Brand ${b.brandId}`}
              </option>
            ))}
          </select>
          {selectedBrand && (
            <div style={{ marginTop: 6, fontSize: 11, color: 'var(--text-muted)' }}>
              ID: {selectedBrand.brandId} · Trademark: {selectedBrand.trademarkId}
            </div>
          )}
        </Section>


        {/* Title — Session 8: enhanced with template guide and keyword popover */}
        <Section title="Title *">
          <input value={draft.title} onChange={e => updateDraft('title', e.target.value)} style={inputStyle} maxLength={500} />
          {/* Character counter with colour zones */}
          <div style={{ display: 'flex', alignItems: 'center', gap: 8, marginTop: 4 }}>
            <small style={{ color: draft.title.length > 500 ? 'var(--danger)' : draft.title.length > 200 ? 'var(--text-tertiary)' : draft.title.length >= 80 ? 'rgb(20,184,166)' : draft.title.length > 0 ? '#fbbf24' : 'var(--text-tertiary)', fontWeight: 500 }}>
              {draft.title.length}/500
              {draft.title.length > 0 && draft.title.length < 80 && ' — too short (aim for 80–200)'}
              {draft.title.length >= 80 && draft.title.length <= 200 && ' — ideal'}
              {draft.title.length > 200 && draft.title.length <= 500 && ' — long but OK'}
            </small>
            {kwData && productId && (
              <div style={{ position: 'relative' }}>
                <button onClick={() => setShowTitlePopover(v => !v)} style={{ fontSize: 11, padding: '2px 8px', borderRadius: 6, border: '1px solid rgba(20,184,166,0.5)', background: 'rgba(20,184,166,0.08)', color: 'rgb(20,184,166)', cursor: 'pointer' }}>
                  ✨ Suggested title
                </button>
                {showTitlePopover && (() => {
                  const suggested = assembleTemuTitle(kwData.keywords.map(e => e.keyword));
                  return (
                    <div style={{ position: 'absolute', top: '110%', left: 0, zIndex: 50, background: 'var(--bg-primary)', border: '1px solid var(--border)', borderRadius: 8, padding: 12, width: 340, boxShadow: '0 4px 16px rgba(0,0,0,0.15)' }}>
                      <p style={{ fontSize: 11, fontWeight: 600, color: 'var(--text-muted)', marginBottom: 6 }}>AI-suggested Temu title</p>
                      <p style={{ fontSize: 13, color: 'var(--text-primary)', marginBottom: 10, lineHeight: 1.5 }}>{suggested || 'No keywords available yet.'}</p>
                      <div style={{ display: 'flex', gap: 6 }}>
                        {suggested && <button onClick={() => { updateDraft('title', suggested); setShowTitlePopover(false); }} style={{ fontSize: 12, padding: '4px 10px', borderRadius: 6, border: 'none', background: 'rgb(20,184,166)', color: '#fff', cursor: 'pointer' }}>Use this</button>}
                        <button onClick={() => setShowTitlePopover(false)} style={{ fontSize: 12, padding: '4px 10px', borderRadius: 6, border: '1px solid var(--border)', background: 'none', color: 'var(--text-muted)', cursor: 'pointer' }}>Dismiss</button>
                      </div>
                    </div>
                  );
                })()}
              </div>
            )}
          </div>
          {/* Template guide bar */}
          <div style={{ marginTop: 10 }}>
            <p style={{ fontSize: 10, color: 'var(--text-muted)', marginBottom: 4 }}>Temu title structure guide (heuristic)</p>
            {(() => {
              const segments = ['Brand', 'Details', 'Application', 'Type', 'Features'];
              const parts = draft.title.split(/[·,\-]/).map((p: string) => p.trim()).filter(Boolean);
              return (
                <div style={{ display: 'flex', gap: 3 }}>
                  {segments.map((seg, i) => {
                    const filled = i < parts.length && parts[i].length > 0;
                    return (
                      <div key={seg} style={{ flex: 1, padding: '4px 6px', borderRadius: 4, fontSize: 10, textAlign: 'center', background: filled ? 'rgba(20,184,166,0.15)' : 'var(--bg-secondary)', border: `1px solid ${filled ? 'rgba(20,184,166,0.4)' : 'var(--border)'}`, color: filled ? 'rgb(20,184,166)' : 'var(--text-muted)', fontWeight: filled ? 600 : 400, transition: 'all 0.15s' }}>{seg}</div>
                    );
                  })}
                </div>
              );
            })()}
          </div>
        </Section>

        {/* Description */}
        <Section title="Description">
          <textarea value={draft.description} onChange={e => updateDraft('description', e.target.value)} style={{ ...inputStyle, minHeight: 100, resize: 'vertical' }} maxLength={10000} />
        </Section>

        {/* Bullet Points */}
        <Section title="Bullet Points">
          {(draft.bulletPoints || []).map((bp, i) => (
            <div key={i} style={{ display: 'flex', gap: 6, marginBottom: 4 }}>
              <input value={bp} onChange={e => { const bps = [...draft.bulletPoints]; bps[i] = e.target.value; updateDraft('bulletPoints', bps); }} style={{ ...inputStyle, flex: 1 }} placeholder={`Bullet point ${i + 1}`} />
              <button onClick={() => updateDraft('bulletPoints', draft.bulletPoints.filter((_: any, idx: number) => idx !== i))} style={{ ...linkBtnStyle, color: 'var(--danger)', fontSize: 16, padding: '0 6px' }}>×</button>
            </div>
          ))}
          {(draft.bulletPoints?.length || 0) < 6 && <button onClick={() => updateDraft('bulletPoints', [...(draft.bulletPoints || []), ''])} style={linkBtnStyle}>+ Add bullet point</button>}
        </Section>

        {/* Images */}
        <Section title={`Images (${draft.images?.length || 0})`}>
          <div style={{ display: 'flex', gap: 8, flexWrap: 'wrap' }}>
            {(draft.images || []).map((img, i) => (
              <div key={i} style={{ position: 'relative' }}>
                <div style={{ width: 80, height: 80, borderRadius: 6, overflow: 'hidden', border: '1px solid var(--border)' }}>
                  <img src={img} alt="" style={{ width: '100%', height: '100%', objectFit: 'cover' }} onError={e => { (e.target as HTMLImageElement).style.display = 'none'; }} />
                </div>
                <button onClick={() => updateDraft('images', draft.images.filter((_: any, idx: number) => idx !== i))} style={{ position: 'absolute', top: -4, right: -4, width: 18, height: 18, borderRadius: '50%', background: 'var(--danger)', color: '#fff', border: 'none', fontSize: 11, cursor: 'pointer', display: 'flex', alignItems: 'center', justifyContent: 'center' }}>×</button>
              </div>
            ))}
            {(!draft.images || draft.images.length === 0) && <p style={{ fontSize: 13, color: 'var(--text-tertiary)' }}>No images found</p>}
          </div>
        </Section>

        {/* Price / SKU — simple or variant */}
        {isVariantProduct ? (() => {
          const combKeys = variants.length > 0 ? Object.keys(variants[0].combination) : [];
          const cs = currencySymbol(draft.currency);
          const activeCount = variants.filter(v => v.active).length;
          const overrideCount = variants.filter(v => v.title || v.description || v.images?.length).length;

          const cellIn: React.CSSProperties = {
            padding: '5px 8px', borderRadius: 6, border: '1px solid var(--border)',
            background: 'var(--bg-primary)', color: 'var(--text-primary)',
            fontSize: 12, width: '100%', boxSizing: 'border-box' as const, outline: 'none',
          };

          return (
            <Section title={`Variations (${variants.length})`}>
              {/* Summary + currency */}
              <div style={{ display: 'flex', alignItems: 'center', gap: 16, marginBottom: 10, flexWrap: 'wrap' as const }}>
                <span style={{ fontSize: 12, color: 'var(--success)' }}>✓ {activeCount} active</span>
                <span style={{ fontSize: 12, color: 'var(--text-muted)' }}>{variants.length - activeCount} inactive</span>
                {overrideCount > 0 && <span style={{ fontSize: 12, color: '#f97316' }}>✏ {overrideCount} with overrides</span>}
                <div style={{ marginLeft: 'auto', display: 'flex', alignItems: 'center', gap: 6 }}>
                  <label style={{ fontSize: 11, color: 'var(--text-muted)' }}>Currency</label>
                  <select value={draft.currency} onChange={e => updateDraft('currency', e.target.value)}
                    style={{ ...inputStyle, width: 80, padding: '4px 8px', fontSize: 12 }}>
                    <option value="GBP">GBP</option><option value="USD">USD</option><option value="EUR">EUR</option>
                  </select>
                </div>
              </div>

              {/* Select-all row */}
              <div style={{ display: 'flex', gap: 8, marginBottom: 8 }}>
                <button onClick={() => setVariants(vs => vs.map(v => ({ ...v, active: true })))}
                  style={{ fontSize: 11, padding: '3px 10px', borderRadius: 5, border: '1px solid var(--border)', background: 'var(--bg-tertiary)', color: 'var(--text-muted)', cursor: 'pointer' }}>Select all</button>
                <button onClick={() => setVariants(vs => vs.map(v => ({ ...v, active: false })))}
                  style={{ fontSize: 11, padding: '3px 10px', borderRadius: 5, border: '1px solid var(--border)', background: 'var(--bg-tertiary)', color: 'var(--text-muted)', cursor: 'pointer' }}>Deselect all</button>
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
                      <th style={thStyle}>Base ({cs})</th>
                      <th style={thStyle}>List ({cs})</th>
                      <th style={thStyle}>Virtual Stock</th>
                      <th style={{ ...thStyle, width: 60 }}>Edit</th>
                    </tr>
                  </thead>
                  <tbody>
                    {variants.map(v => {
                      const hasOverrides = !!(v.title || v.description || v.images?.length);
                      return (
                        <tr key={v.id} style={{ borderBottom: '1px solid var(--border)', opacity: v.active ? 1 : 0.42, background: v.active ? 'transparent' : 'var(--bg-tertiary)' }}>
                          <td style={{ ...tdStyle, textAlign: 'center' as const }}>
                            <input type="checkbox" checked={v.active} onChange={e => updateVariant(v.id, 'active', e.target.checked)} />
                          </td>
                          <td style={{ ...tdStyle, padding: '4px 6px' }}>
                            {v.image ? (
                              <img src={v.image} alt="" onClick={() => setEditingVariant(v)}
                                style={{ width: 40, height: 40, objectFit: 'cover', borderRadius: 4, border: '1px solid var(--border)', cursor: 'pointer', display: 'block' }}
                                onError={e => { (e.target as HTMLImageElement).style.display = 'none'; }} />
                            ) : (
                              <div onClick={() => setEditingVariant(v)}
                                style={{ width: 40, height: 40, borderRadius: 4, border: '1px dashed var(--border)', display: 'flex', alignItems: 'center', justifyContent: 'center', color: 'var(--text-muted)', fontSize: 16, cursor: 'pointer' }}>+</div>
                            )}
                          </td>
                          {combKeys.map(k => <td key={k} style={{ ...tdStyle, fontWeight: 500 }}>{v.combination[k] || '—'}</td>)}
                          <td style={tdStyle}><input value={v.sku} onChange={e => updateVariant(v.id, 'sku', e.target.value)} style={cellIn} /></td>
                          <td style={tdStyle}><input type="number" step="0.01" min="0" value={v.retailPrice} onChange={e => updateVariant(v.id, 'retailPrice', e.target.value)} style={{ ...cellIn, width: 80 }} /></td>
                          <td style={tdStyle}><input type="number" step="0.01" min="0" value={v.listPrice} onChange={e => updateVariant(v.id, 'listPrice', e.target.value)} style={{ ...cellIn, width: 80 }} placeholder="—" /></td>
                          <td style={tdStyle}><input type="number" min="0" value={v.stock} onChange={e => updateVariant(v.id, 'stock', e.target.value)} style={{ ...cellIn, width: 65 }} /></td>
                          <td style={{ ...tdStyle, textAlign: 'center' as const }}>
                            <button onClick={() => setEditingVariant(v)} style={{
                              padding: '4px 10px', borderRadius: 6, fontSize: 11, cursor: 'pointer', fontWeight: 600,
                              border: hasOverrides ? '1px solid #f97316' : '1px solid var(--border)',
                              background: hasOverrides ? 'rgba(249,115,22,0.1)' : 'var(--bg-tertiary)',
                              color: hasOverrides ? '#f97316' : 'var(--text-muted)',
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
        })() : (
          <>
            {/* Price — currency values (simple product) */}
            <Section title="Price *">
              <div style={{ display: 'flex', gap: 12 }}>
                <div style={{ flex: 1 }}>
                  <label style={labelStyle}>Base Price ({currencySymbol(draft.currency)})</label>
                  <input value={draft.retailPrice} onChange={e => updateDraft('retailPrice', e.target.value)} style={inputStyle} type="number" step="0.01" min="0" placeholder="e.g. 17.99" />
                </div>
                <div style={{ flex: 1 }}>
                  <label style={labelStyle}>List Price ({currencySymbol(draft.currency)}) — strikethrough</label>
                  <input value={draft.listPrice} onChange={e => updateDraft('listPrice', e.target.value)} style={inputStyle} type="number" step="0.01" min="0" placeholder="Optional — must be higher than base price" />
                </div>
                <div style={{ width: 100 }}>
                  <label style={labelStyle}>Currency</label>
                  <select value={draft.currency} onChange={e => updateDraft('currency', e.target.value)} style={inputStyle}>
                    <option value="GBP">GBP</option><option value="USD">USD</option><option value="EUR">EUR</option>
                  </select>
                </div>
              </div>
            </Section>

            {/* SKU & Virtual Stock (simple product) */}
            <Section title="SKU & Virtual Stock *">
              <div style={{ display: 'flex', gap: 12 }}>
                <div style={{ flex: 2 }}>
                  <label style={labelStyle}>External SKU (outGoodsSn)</label>
                  <input value={draft.sku} onChange={e => updateDraft('sku', e.target.value)} style={inputStyle} maxLength={40} />
                </div>
                <div style={{ flex: 1 }}>
                  <label style={labelStyle}>Virtual Stock</label>
                  <input value={draft.quantity} onChange={e => updateDraft('quantity', parseInt(e.target.value) || 0)} style={inputStyle} type="number" min={0} />
                </div>
              </div>
            </Section>
          </>
        )}

        {/* Dimensions & Weight */}
        <Section title="Dimensions & Weight">
          <div style={{ display: 'flex', gap: 12, flexWrap: 'wrap' }}>
            <SmallField label="Length (cm)" value={draft.dimensions?.lengthCm || ''} onChange={v => updateDraft('dimensions', { ...draft.dimensions, lengthCm: v })} />
            <SmallField label="Width (cm)" value={draft.dimensions?.widthCm || ''} onChange={v => updateDraft('dimensions', { ...draft.dimensions, widthCm: v })} />
            <SmallField label="Height (cm)" value={draft.dimensions?.heightCm || ''} onChange={v => updateDraft('dimensions', { ...draft.dimensions, heightCm: v })} />
            <SmallField label="Weight (g)" value={draft.weight?.weightG || ''} onChange={v => updateDraft('weight', { weightG: v })} />
          </div>
        </Section>

        {/* Fulfilment & Shipping */}
        <Section title="Fulfilment & Shipping *">
          <div style={{ display: 'flex', gap: 12, flexWrap: 'wrap' }}>
            <div style={{ flex: 1, minWidth: 160 }}>
              <label style={labelStyle}>Fulfilment Type</label>
              <select value={draft.fulfillmentType} onChange={e => updateDraft('fulfillmentType', parseInt(e.target.value))} style={inputStyle}>
                <option value={1}>Merchant Fulfilled</option><option value={2}>Temu Fulfilled</option>
              </select>
            </div>
            <div style={{ flex: 1, minWidth: 160 }}>
              <label style={labelStyle}>Shipment Limit (days)</label>
              <input value={draft.shipmentLimitDay} onChange={e => updateDraft('shipmentLimitDay', parseInt(e.target.value) || 0)} style={inputStyle} type="number" min={0} max={30} />
            </div>
            <div style={{ flex: 2, minWidth: 200 }}>
              <label style={labelStyle}>Shipping Profile</label>
              <select value={selectedShipping} onChange={e => setSelectedShipping(e.target.value)} style={inputStyle}>
                <option value="">-- Select shipping template --</option>
                {shippingTemplates.map(t => <option key={t.templateId} value={t.templateId}>{t.templateName}</option>)}
              </select>
              {!selectedShipping && <small style={{ color: 'var(--danger)' }}>Required</small>}
            </div>
          </div>
        </Section>

        {/* Country / Region of Origin */}
        <Section title="Country / Region of Origin">
          <div style={{ display: 'flex', gap: 12 }}>
            <div style={{ flex: 1 }}>
              <label style={labelStyle}>Region</label>
              <select
                value={draft.originRegion1}
                onChange={e => {
                  const val = e.target.value;
                  setDraft(prev => prev ? { ...prev, originRegion1: val, originRegion2: val === 'Mainland China' ? prev.originRegion2 : '' } : prev);
                }}
                style={inputStyle}
              >
                <option value="">-- Select --</option>
                <option value="Mainland China">Mainland China</option>
                <option value="Hong Kong">Hong Kong</option>
                <option value="Taiwan">Taiwan</option>
                <option value="United States">United States</option>
                <option value="United Kingdom">United Kingdom</option>
                <option value="Germany">Germany</option>
                <option value="Japan">Japan</option>
                <option value="South Korea">South Korea</option>
                <option value="India">India</option>
                <option value="Vietnam">Vietnam</option>
                <option value="Thailand">Thailand</option>
                <option value="Other">Other</option>
              </select>
            </div>
            {draft.originRegion1 === 'Mainland China' && (
              <div style={{ flex: 1 }}>
                <label style={labelStyle}>Province</label>
                <select value={draft.originRegion2} onChange={e => updateDraft('originRegion2', e.target.value)} style={inputStyle}>
                  <option value="">-- Select province --</option>
                  {CHINA_PROVINCES.map(p => <option key={p} value={p}>{p}</option>)}
                </select>
              </div>
            )}
          </div>
        </Section>

        {/* Category Attributes */}
        {templateProperties.length > 0 && (
          <Section title={`Category Attributes (${templateProperties.length})`}>
            {missingRequired > 0 && <div style={{ padding: '8px 12px', background: '#fff8e6', borderRadius: 6, fontSize: 12, color: '#b38600', marginBottom: 12 }}>{missingRequired} required attribute{missingRequired > 1 ? 's' : ''} not yet filled</div>}
            {templateProperties.filter(p => p.required).map(prop => (
              <PropertyField key={prop.pid} prop={prop} value={propertyValues[String(prop.pid)]} onChange={(val, vid) => handlePropertyChange(prop.pid, prop, val, vid)} />
            ))}
            <OptionalProps properties={templateProperties.filter(p => !p.required)} values={propertyValues} onChange={(pid, prop, val, vid) => handlePropertyChange(pid, prop, val, vid)} />
          </Section>
        )}

        {/* Certificates & Documents */}
        {certEntries.length > 0 && (
          <Section title={`Certificates & Documents (${certEntries.length})`}>
            <div style={{ display: 'flex', flexDirection: 'column', gap: 10 }}>
              {certEntries.map((cert, ci) => (
                <CertCard key={ci} cert={cert} />
              ))}
            </div>
          </Section>
        )}

        {/* Labelling & Marking Checklist */}
        {checkItems.length > 0 && (
          <Section title={`Labelling & Marking Checklist (${checkItems.length})`}>
            <p style={{ fontSize: 12, color: 'var(--text-tertiary)', marginBottom: 8 }}>Temu will check your product photos for these markings. Ensure they are visible.</p>
            <div style={{ display: 'flex', flexDirection: 'column', gap: 6 }}>
              {checkItems.map((ch, i) => (
                <CheckRow key={i} item={ch} />
              ))}
            </div>
          </Section>
        )}

        {/* Temu Category Picker Modal */}
        {catModalOpen && (
          <TemuCategoryModal
            catLevels={catLevels}
            catSelections={catSelections}
            catLoading={catLoading}
            catError={catError}
            onSelect={handleCatSelect}
            onClose={() => setCatModalOpen(false)}
          />
        )}

                {/* Temu Variant Edit Modal */}
        {editingVariant && (
          <TemuVariantModal
            variant={editingVariant}
            parentImages={draft.images}
            currency={draft.currency}
            onSave={(updated, applyToAll) => {
              if (applyToAll) {
                setVariants(vs => vs.map(v => ({
                  ...v,
                  retailPrice: updated.retailPrice || v.retailPrice,
                  stock: updated.stock || v.stock,
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

        <div style={{ display: 'flex', gap: 12, paddingTop: 16, borderTop: '1px solid var(--border)' }}>
          <button onClick={() => navigate(-1)} style={secondaryBtnStyle}>← Back to Product</button>
          <button onClick={handleSubmit} disabled={!canSubmit} style={{ ...primaryBtnStyle, opacity: canSubmit ? 1 : 0.5 }}>{submitting ? '⏳ Submitting...' : draft?.goodsId ? 'Update on Temu' : 'Submit to Temu'}</button>
        </div>

        {/* Debug Panel — Temu API Request / Response */}
        {submitResult && (submitResult.request || submitResult.response) && (
          <div style={{ marginTop: 20, borderTop: '2px solid var(--border)' }}>
            <h3 style={{ fontSize: 14, fontWeight: 700, color: 'var(--text-tertiary)', margin: '16px 0 8px' }}>🔧 Temu API Debug</h3>
            {submitResult.request && (
              <div style={{ marginBottom: 12 }}>
                <div style={{ fontSize: 12, fontWeight: 600, color: 'var(--text-secondary)', marginBottom: 4 }}>REQUEST →</div>
                <pre style={{
                  background: '#1e1e2e', color: '#cdd6f4', padding: 16, borderRadius: 8,
                  fontSize: 11, lineHeight: 1.5, overflow: 'auto', maxHeight: 500,
                  fontFamily: '"Fira Code", "JetBrains Mono", "Consolas", monospace',
                  border: '1px solid #313244', whiteSpace: 'pre-wrap', wordBreak: 'break-word',
                }}>{JSON.stringify(submitResult.request, null, 2)}</pre>
              </div>
            )}
            {submitResult.response && (
              <div>
                <div style={{ fontSize: 12, fontWeight: 600, color: 'var(--text-secondary)', marginBottom: 4 }}>← RESPONSE</div>
                <pre style={{
                  background: '#1e1e2e', color: submitResult.ok ? '#a6e3a1' : '#f38ba8', padding: 16, borderRadius: 8,
                  fontSize: 11, lineHeight: 1.5, overflow: 'auto', maxHeight: 500,
                  fontFamily: '"Fira Code", "JetBrains Mono", "Consolas", monospace',
                  border: `1px solid ${submitResult.ok ? '#313244' : '#45293a'}`, whiteSpace: 'pre-wrap', wordBreak: 'break-word',
                }}>{JSON.stringify(submitResult.response, null, 2)}</pre>
              </div>
            )}
          </div>
        )}
      </div>
    </div>
  );
}

function Section({ title, children }: { title: string; children: React.ReactNode }) {
  return <div><h3 style={{ fontSize: 14, fontWeight: 700, marginBottom: 8, color: 'var(--text-primary)' }}>{title}</h3>{children}</div>;
}
function SmallField({ label, value, onChange }: { label: string; value: string; onChange: (v: string) => void }) {
  return <div style={{ flex: 1, minWidth: 120 }}><label style={labelStyle}>{label}</label><input value={value} onChange={e => onChange(e.target.value)} style={inputStyle} /></div>;
}
function PropertyField({ prop, value, onChange }: { prop: TemplateProp; value: any; onChange: (val: string, vid?: number) => void }) {
  const cv = value?.value || '', cVid = value?.vid;
  if (prop.values.length > 0) return (
    <div style={{ marginBottom: 8 }}>
      <label style={labelStyle}>{prop.name}{prop.required && <span style={{ color: 'var(--danger)' }}> *</span>}{prop.isSale && <span style={{ fontSize: 10, marginLeft: 6, color: 'var(--accent)', background: '#e8f0fe', padding: '1px 6px', borderRadius: 4 }}>variant</span>}</label>
      <select value={cVid ? String(cVid) : cv} onChange={e => { const vid = parseInt(e.target.value); const m = prop.values.find(v => v.vid === vid); m ? onChange(m.label, m.vid) : onChange(e.target.value); }} style={inputStyle}>
        <option value="">-- Select --</option>{prop.values.map(v => <option key={v.vid} value={String(v.vid)}>{v.label}</option>)}
      </select>
    </div>
  );
  return <div style={{ marginBottom: 8 }}><label style={labelStyle}>{prop.name}{prop.required && <span style={{ color: 'var(--danger)' }}> *</span>}</label><input value={cv} onChange={e => onChange(e.target.value)} style={inputStyle} placeholder={`Enter ${prop.name.toLowerCase()}`} /></div>;
}
function OptionalProps({ properties, values, onChange }: { properties: TemplateProp[]; values: Record<string, any>; onChange: (pid: number, prop: TemplateProp, val: string, vid?: number) => void }) {
  if (properties.length === 0) return null;
  return (
    <div style={{ borderTop: '1px solid var(--border)', paddingTop: 8, marginTop: 8 }}>
      {properties.map(prop => (
        <PropertyField key={prop.pid} prop={prop} value={values[String(prop.pid)]} onChange={(val, vid) => onChange(prop.pid, prop, val, vid)} />
      ))}
    </div>
  );
}

// ── Compliance components ──

function CertCard({ cert }: { cert: CertEntry }) {
  const [expanded, setExpanded] = useState(false);
  return (
    <div style={{ border: cert.isRequired ? '1px solid var(--danger)' : '1px solid var(--border)', borderRadius: 8, overflow: 'hidden' }}>
      <button onClick={() => setExpanded(!expanded)} style={{ width: '100%', display: 'flex', alignItems: 'center', justifyContent: 'space-between', padding: '10px 14px', background: 'var(--bg-secondary)', border: 'none', cursor: 'pointer', textAlign: 'left' }}>
        <div style={{ display: 'flex', alignItems: 'center', gap: 8 }}>
          <span style={{ transform: expanded ? 'rotate(90deg)' : 'none', transition: 'transform 0.2s', display: 'inline-block', fontSize: 11, color: 'var(--text-tertiary)' }}>▶</span>
          <span style={{ fontWeight: 600, fontSize: 13, color: 'var(--text-primary)' }}>{cert.certName}</span>
          {cert.isRequired && <span style={{ fontSize: 10, padding: '1px 6px', borderRadius: 4, background: '#fee', color: 'var(--danger)', fontWeight: 700 }}>Required</span>}
          <span style={{ fontSize: 11, color: 'var(--text-tertiary)' }}>({cert.uploadItems.length} upload{cert.uploadItems.length > 1 ? 's' : ''})</span>
        </div>
      </button>
      {expanded && (
        <div style={{ padding: '8px 14px 12px', display: 'flex', flexDirection: 'column', gap: 8 }}>
          {cert.uploadItems.map((item, i) => (
            <div key={i} style={{ display: 'flex', alignItems: 'center', gap: 8 }}>
              <div style={{ flex: 1, display: 'flex', alignItems: 'center', gap: 6 }}>
                <span style={{ fontSize: 13 }}>
                  {item.contentType === 2 ? '📄' : item.contentType === 4 ? '📷' : '📋'}
                </span>
                <span style={{ fontSize: 13, color: 'var(--text-primary)' }}>{item.alias}</span>
                {(item.uploadRequire || item.examplePics.length > 0) && (
                  <InfoPopup title={item.alias} description={item.uploadRequire} examplePics={item.examplePics} />
                )}
              </div>
              <button style={{ padding: '4px 14px', borderRadius: 6, border: '1px solid var(--border)', background: 'var(--bg-primary)', fontSize: 12, cursor: 'pointer', color: 'var(--text-secondary)', fontWeight: 600 }}>
                Upload
              </button>
            </div>
          ))}
        </div>
      )}
    </div>
  );
}

function CheckRow({ item }: { item: CheckItem }) {
  return (
    <div style={{ display: 'flex', alignItems: 'center', gap: 8, padding: '6px 10px', background: 'var(--bg-secondary)', borderRadius: 6 }}>
      <span style={{ fontSize: 11, color: 'var(--text-tertiary)' }}>☐</span>
      <span style={{ fontSize: 13, color: 'var(--text-primary)', flex: 1 }}>{item.checkShowName}</span>
      {(item.exampleDesc || item.examplePics.length > 0) && (
        <InfoPopup title={item.checkShowName} description={item.exampleDesc} examplePics={item.examplePics} />
      )}
    </div>
  );
}

function InfoPopup({ title, description, examplePics }: { title: string; description: string; examplePics: string[] }) {
  const [open, setOpen] = useState(false);
  // Strip HTML tags for cleaner display, but keep line breaks
  const cleanDesc = description
    .replace(/<br\s*\/?>/gi, '\n')
    .replace(/<\/?(span|ul|li|strong|p|div)[^>]*>/gi, (m) => {
      if (m.startsWith('</')) return '';
      if (m.startsWith('<li')) return '\n• ';
      if (m.startsWith('<br') || m.startsWith('<p') || m.startsWith('<div')) return '\n';
      return '';
    })
    .replace(/<[^>]+>/g, '')
    .trim();

  return (
    <>
      <button
        onClick={e => { e.stopPropagation(); setOpen(true); }}
        style={{ background: 'none', border: 'none', cursor: 'pointer', padding: 0, fontSize: 14, lineHeight: 1, color: 'var(--accent)', opacity: 0.7 }}
        title="More info"
      >ℹ️</button>
      {open && (
        <div style={{ position: 'fixed', top: 0, left: 0, right: 0, bottom: 0, background: 'rgba(0,0,0,0.5)', zIndex: 1000, display: 'flex', alignItems: 'center', justifyContent: 'center' }} onClick={() => setOpen(false)}>
          <div style={{ background: 'var(--bg-primary)', borderRadius: 12, padding: 24, maxWidth: 600, maxHeight: '80vh', overflow: 'auto', border: '1px solid var(--border)', boxShadow: '0 8px 32px rgba(0,0,0,0.3)' }} onClick={e => e.stopPropagation()}>
            <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: 12 }}>
              <h3 style={{ fontSize: 15, fontWeight: 700, margin: 0, color: 'var(--text-primary)' }}>{title}</h3>
              <button onClick={() => setOpen(false)} style={{ background: 'none', border: 'none', fontSize: 20, cursor: 'pointer', color: 'var(--text-tertiary)', padding: '0 4px' }}>×</button>
            </div>
            {cleanDesc && <p style={{ fontSize: 13, color: 'var(--text-secondary)', whiteSpace: 'pre-wrap', lineHeight: 1.5, margin: '0 0 12px' }}>{cleanDesc}</p>}
            {examplePics.length > 0 && (
              <div>
                <p style={{ fontSize: 11, fontWeight: 600, color: 'var(--text-tertiary)', marginBottom: 6 }}>Example images:</p>
                <div style={{ display: 'flex', gap: 8, flexWrap: 'wrap' }}>
                  {examplePics.map((pic, i) => (
                    <img key={i} src={pic} alt="" style={{ maxWidth: 200, maxHeight: 150, borderRadius: 6, border: '1px solid var(--border)' }} />
                  ))}
                </div>
              </div>
            )}
          </div>
        </div>
      )}
    </>
  );
}

const inputStyle: React.CSSProperties = { width: '100%', padding: '10px 14px', borderRadius: 8, border: '1px solid var(--border)', background: 'var(--bg-primary)', color: 'var(--text-primary)', fontSize: 14, outline: 'none', boxSizing: 'border-box' };
const labelStyle: React.CSSProperties = { display: 'block', fontSize: 12, fontWeight: 600, color: 'var(--text-secondary)', marginBottom: 4 };
const primaryBtnStyle: React.CSSProperties = { padding: '10px 24px', borderRadius: 8, border: 'none', background: 'var(--accent)', color: '#fff', fontWeight: 700, fontSize: 14, cursor: 'pointer' };
const secondaryBtnStyle: React.CSSProperties = { padding: '10px 24px', borderRadius: 8, border: '1px solid var(--border)', background: 'var(--bg-secondary)', color: 'var(--text-primary)', fontWeight: 600, fontSize: 14, cursor: 'pointer' };
const linkBtnStyle: React.CSSProperties = { background: 'none', border: 'none', color: 'var(--accent)', cursor: 'pointer', fontSize: 13, fontWeight: 600, padding: '4px 0' };
const backBtnStyle: React.CSSProperties = { background: 'var(--bg-secondary)', border: '1px solid var(--border)', borderRadius: 8, padding: '6px 14px', fontSize: 13, cursor: 'pointer', color: 'var(--text-secondary)' };
const thStyle: React.CSSProperties = { textAlign: 'left', padding: '8px 10px', fontSize: 11, fontWeight: 700, color: 'var(--text-tertiary)', textTransform: 'uppercase', letterSpacing: '0.5px', whiteSpace: 'nowrap' };
const tdStyle: React.CSSProperties = { padding: '8px 10px', verticalAlign: 'middle' };
const imgBtnStyle: React.CSSProperties = { background: 'rgba(0,0,0,0.7)', border: 'none', color: '#fff', fontSize: 10, width: 18, height: 18, borderRadius: 3, cursor: 'pointer', display: 'flex', alignItems: 'center', justifyContent: 'center', padding: 0 };

// ============================================================================
// TEMU VARIANT EDIT MODAL
// ============================================================================

// ============================================================================
// TEMU CATEGORY PICKER MODAL  
// ============================================================================
// Side-by-side scrollable listboxes — one per category level.
// Selecting a non-leaf loads its children as the next column.
// Selecting a leaf triggers prepareListing and closes the modal.

interface TemuCategoryModalProps {
  catLevels: TemuCategory[][];
  catSelections: (TemuCategory | null)[];
  catLoading: boolean;
  catError?: string;
  onSelect: (levelIndex: number, catId: string) => void;
  onClose: () => void;
}
function TemuCategoryModal({ catLevels, catSelections, catLoading, catError, onSelect, onClose }: TemuCategoryModalProps) {
  const levelLabels = ['Category', 'Subcategory', 'Sub-subcategory', 'Level 4', 'Level 5'];

  const colStyle: React.CSSProperties = {
    flex: '0 0 220px',
    display: 'flex',
    flexDirection: 'column',
    border: '1px solid var(--border)',
    borderRadius: 8,
    overflow: 'hidden',
    background: 'var(--bg-primary)',
  };

  const colHeaderStyle: React.CSSProperties = {
    padding: '8px 12px',
    fontSize: 11,
    fontWeight: 700,
    color: 'var(--text-muted)',
    textTransform: 'uppercase',
    letterSpacing: 1,
    background: 'var(--bg-tertiary)',
    borderBottom: '1px solid var(--border)',
  };

  const itemStyle = (selected: boolean, isLeaf: boolean): React.CSSProperties => ({
    padding: '8px 12px',
    fontSize: 13,
    cursor: 'pointer',
    borderBottom: '1px solid var(--border)',
    background: selected ? 'rgba(249,115,22,0.15)' : 'transparent',
    color: selected ? '#f97316' : isLeaf ? 'var(--text-primary)' : 'var(--text-secondary)',
    fontWeight: selected ? 600 : 400,
    display: 'flex',
    alignItems: 'center',
    justifyContent: 'space-between',
    gap: 6,
  });

  return (
    <div
      style={{ position: 'fixed', inset: 0, zIndex: 1200, background: 'rgba(0,0,0,0.75)', display: 'flex', alignItems: 'center', justifyContent: 'center', padding: 20 }}
      onClick={e => { if (e.target === e.currentTarget) onClose(); }}
    >
      <div style={{ background: 'var(--bg-secondary)', border: '1px solid var(--border)', borderRadius: 14, width: '90vw', maxWidth: 1100, maxHeight: '85vh', display: 'flex', flexDirection: 'column', boxShadow: '0 28px 72px rgba(0,0,0,0.6)' }}>

        {/* Header */}
        <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', padding: '18px 24px', borderBottom: '1px solid var(--border)' }}>
          <div>
            <div style={{ fontSize: 16, fontWeight: 700, color: 'var(--text-primary)' }}>Select Temu Category</div>
            <div style={{ fontSize: 12, color: 'var(--text-muted)', marginTop: 2 }}>
              {catSelections.filter(Boolean).length > 0
                ? catSelections.filter(Boolean).map(s => s!.catName).join(' › ')
                : 'Browse the category tree — select a leaf category (✓) to confirm'}
            </div>
          </div>
          <button onClick={onClose} style={{ background: 'none', border: 'none', fontSize: 22, cursor: 'pointer', color: 'var(--text-muted)' }}>✕</button>
        </div>

        {/* Column browser */}
        <div style={{ flex: 1, overflowX: 'auto', overflowY: 'hidden', padding: '16px 24px', display: 'flex', gap: 10 }}>
          {catLevels.map((cats, levelIdx) => (
            <div key={levelIdx} style={colStyle}>
              <div style={colHeaderStyle}>{levelLabels[levelIdx] || `Level ${levelIdx + 1}`}</div>
              <div style={{ flex: 1, overflowY: 'auto' }}>
                {cats.length === 0 && (
                  <div style={{ padding: '12px', fontSize: 12, color: 'var(--text-muted)' }}>No items</div>
                )}
                {cats.map(cat => {
                  const selected = catSelections[levelIdx]?.catId === cat.catId;
                  return (
                    <div
                      key={cat.catId}
                      style={itemStyle(selected, cat.leaf)}
                      onClick={() => onSelect(levelIdx, String(cat.catId))}
                    >
                      <span style={{ flex: 1 }}>{cat.catName}</span>
                      {cat.leaf
                        ? <span style={{ fontSize: 10, color: selected ? '#f97316' : '#22c55e', fontWeight: 700, flexShrink: 0 }}>✓ leaf</span>
                        : <span style={{ fontSize: 12, color: 'var(--text-muted)', flexShrink: 0 }}>›</span>
                      }
                    </div>
                  );
                })}
              </div>
            </div>
          ))}

          {catLoading && (
            <div style={{ flex: '0 0 220px', display: 'flex', alignItems: 'center', justifyContent: 'center', color: 'var(--text-muted)', fontSize: 13 }}>
              ⏳ Loading…
            </div>
          )}

          {catLevels.length === 0 && !catLoading && (
            <div style={{ flex: 1, display: 'flex', flexDirection: 'column', alignItems: 'center', justifyContent: 'center', gap: 12, padding: 24 }}>
              {catError ? (
                <>
                  <div style={{ fontSize: 22 }}>⚠️</div>
                  <div style={{ fontSize: 14, fontWeight: 600, color: 'var(--danger)' }}>Failed to load categories</div>
                  <div style={{ fontSize: 12, color: 'var(--text-muted)', maxWidth: 420, textAlign: 'center' }}>{catError}</div>
                  <div style={{ fontSize: 12, color: 'var(--text-muted)', maxWidth: 420, textAlign: 'center' }}>
                    This usually means the Temu credential is missing <code>app_key</code>, <code>app_secret</code>, or <code>access_token</code>. Check your Temu connection in Settings.
                  </div>
                </>
              ) : (
                <div style={{ fontSize: 13, color: 'var(--text-muted)' }}>No categories found.</div>
              )}
            </div>
          )}
        </div>

        {/* Footer */}
        <div style={{ padding: '12px 24px', borderTop: '1px solid var(--border)', display: 'flex', alignItems: 'center', justifyContent: 'space-between' }}>
          <div style={{ fontSize: 12, color: 'var(--text-muted)' }}>
            {catSelections.filter(s => s?.leaf).length > 0
              ? <span style={{ color: '#22c55e', fontWeight: 600 }}>✓ Leaf category selected — click to confirm or keep browsing</span>
              : 'Navigate to a leaf category (marked ✓) to set the category'}
          </div>
          <button onClick={onClose} style={{ padding: '8px 18px', borderRadius: 7, border: '1px solid var(--border)', background: 'var(--bg-tertiary)', color: 'var(--text-muted)', fontSize: 13, cursor: 'pointer' }}>
            Cancel
          </button>
        </div>
      </div>
    </div>
  );
}

const TEMU_ORANGE = '#f97316';

interface TemuVariantModalProps {
  variant: TemuVariant;
  parentImages: string[];
  currency: string;
  onSave: (updated: TemuVariant, applyToAll: boolean) => void;
  onClose: () => void;
}

function TemuVariantModal({ variant, parentImages, currency, onSave, onClose }: TemuVariantModalProps) {
  const [local, setLocal] = React.useState<TemuVariant>({ ...variant });
  const [applyToAll, setApplyToAll] = React.useState(false);
  const [imageInputUrl, setImageInputUrl] = React.useState('');

  const set = <K extends keyof TemuVariant>(field: K, value: TemuVariant[K]) =>
    setLocal(v => ({ ...v, [field]: value }));

  const cs = currencySymbol(currency);
  const variantLabel = Object.entries(variant.combination)
    .filter(([, v]) => v).map(([k, v]) => `${k}: ${v}`).join(' / ');

  const mInput: React.CSSProperties = {
    width: '100%', padding: '10px 14px', borderRadius: 8,
    border: '1px solid var(--border)', background: 'var(--bg-primary)',
    color: 'var(--text-primary)', fontSize: 14, outline: 'none', boxSizing: 'border-box',
  };
  const mLabel: React.CSSProperties = {
    display: 'block', fontSize: 12, fontWeight: 600, color: 'var(--text-secondary)', marginBottom: 4,
  };
  const mSection = (accent: string): React.CSSProperties => ({
    marginBottom: 14, padding: '14px 16px',
    background: 'var(--bg-tertiary, #1a1e28)',
    border: '1px solid var(--border)',
    borderLeftWidth: 3, borderLeftColor: accent,
    borderRadius: 10,
  });
  const mRow: React.CSSProperties = { display: 'flex', gap: 12, flexWrap: 'wrap' as const, marginBottom: 10 };
  const mFlex = (min: number): React.CSSProperties => ({ flex: 1, minWidth: min });
  const mHead = (color: string): React.CSSProperties => ({
    fontSize: 11, fontWeight: 700, textTransform: 'uppercase' as const,
    letterSpacing: 1.5, color, marginBottom: 6,
  });

  // Image helpers
  const allImages = [...new Set([...(local.images || []), ...parentImages])];
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
      <div style={{ background: 'var(--bg-secondary, #13161e)', border: '1px solid var(--border)', borderRadius: 14, width: '100%', maxWidth: 900, maxHeight: '92vh', overflowY: 'auto', padding: '24px 28px', boxShadow: '0 28px 72px rgba(0,0,0,0.6)' }}>

        {/* Header */}
        <div style={{ display: 'flex', alignItems: 'flex-start', justifyContent: 'space-between', marginBottom: 20 }}>
          <div>
            <div style={{ fontSize: 16, fontWeight: 700, color: 'var(--text-primary)', marginBottom: 2 }}>Edit Variant</div>
            <div style={{ fontSize: 13, color: TEMU_ORANGE, fontWeight: 600 }}>{variantLabel}</div>
            <div style={{ fontSize: 11, color: 'var(--text-muted)', marginTop: 3 }}>
              Blank fields inherit from the parent listing · SKU: <span style={{ fontFamily: 'monospace' }}>{variant.sku || '—'}</span>
            </div>
          </div>
          <button onClick={onClose} style={{ background: 'none', border: 'none', color: 'var(--text-muted)', fontSize: 22, cursor: 'pointer', lineHeight: 1 }}>✕</button>
        </div>

        {/* ── PRICING ── */}
        <div style={mHead(TEMU_ORANGE)}>Pricing</div>
        <div style={mSection(TEMU_ORANGE)}>
          <label style={{ display: 'flex', alignItems: 'center', gap: 8, padding: '7px 10px', background: applyToAll ? 'rgba(249,115,22,0.1)' : 'var(--bg-secondary)', border: `1px solid ${applyToAll ? TEMU_ORANGE : 'var(--border)'}`, borderRadius: 7, marginBottom: 12, cursor: 'pointer', fontSize: 13, color: applyToAll ? TEMU_ORANGE : 'var(--text-muted)' }}>
            <input type="checkbox" checked={applyToAll} onChange={e => setApplyToAll(e.target.checked)} style={{ accentColor: TEMU_ORANGE }} />
            <span style={{ fontWeight: 600 }}>Apply pricing &amp; stock to all variants</span>
          </label>
          <div style={mRow}>
            <div style={mFlex(120)}><label style={mLabel}>SKU</label><input style={mInput} value={local.sku} onChange={e => set('sku', e.target.value)} /></div>
            <div style={mFlex(100)}><label style={mLabel}>Base Price ({cs}) *</label><input style={mInput} type="number" step="0.01" value={local.retailPrice} onChange={e => set('retailPrice', e.target.value)} /></div>
            <div style={mFlex(100)}><label style={mLabel}>List Price ({cs})</label><input style={mInput} type="number" step="0.01" value={local.listPrice} onChange={e => set('listPrice', e.target.value)} placeholder="Strikethrough" /></div>
            <div style={mFlex(80)}><label style={mLabel}>Virtual Stock</label><input style={mInput} type="number" value={local.stock} onChange={e => set('stock', e.target.value)} /></div>
          </div>
          <div style={mRow}>
            <div style={mFlex(140)}><label style={mLabel}>EAN / Barcode</label><input style={mInput} value={local.ean} onChange={e => set('ean', e.target.value)} placeholder="e.g. 5060000000000" /></div>
            <div style={mFlex(140)}><label style={mLabel}>UPC</label><input style={mInput} value={local.upc || ''} onChange={e => set('upc', e.target.value)} /></div>
          </div>
        </div>

        {/* ── IMAGES ── */}
        <div style={mHead('#22c55e')}>Images</div>
        <div style={mSection('#22c55e')}>
          <div style={{ fontSize: 12, color: 'var(--text-muted)', marginBottom: 10 }}>
            Per-variant images appear alongside this specific colour/size on Temu. First image shown as primary.
          </div>

          {/* Image grid — variant-specific images */}
          {(local.images || []).length > 0 && (
            <div style={{ display: 'flex', gap: 8, flexWrap: 'wrap', marginBottom: 10 }}>
              {(local.images || []).map((url, idx) => (
                <div key={idx} style={{ position: 'relative', border: idx === 0 ? '2px solid #22c55e' : '1px solid var(--border)', borderRadius: 8, overflow: 'hidden' }}>
                  <img src={url} alt="" style={{ width: 72, height: 72, objectFit: 'contain', display: 'block', background: 'var(--bg-primary)' }} />
                  {idx === 0 && <div style={{ position: 'absolute', bottom: 0, left: 0, right: 0, background: '#22c55e', color: '#fff', fontSize: 9, fontWeight: 700, textAlign: 'center', padding: '2px 0' }}>PRIMARY</div>}
                  <div style={{ position: 'absolute', top: 2, right: 2, display: 'flex', gap: 2 }}>
                    {idx > 0 && <button onClick={() => moveImageFirst(idx)} title="Set as primary" style={{ ...imgBtnStyle, background: 'rgba(34,197,94,0.85)' }}>★</button>}
                    <button onClick={() => removeImage(idx)} style={{ ...imgBtnStyle, background: 'rgba(220,38,38,0.85)' }}>✕</button>
                  </div>
                </div>
              ))}
            </div>
          )}

          {/* Pick from parent images */}
          {parentImages.length > 0 && (
            <div style={{ marginBottom: 10 }}>
              <div style={{ fontSize: 11, color: 'var(--text-muted)', marginBottom: 6 }}>Or pick from parent listing images:</div>
              <div style={{ display: 'flex', gap: 6, flexWrap: 'wrap' }}>
                {parentImages.map((url, i) => {
                  const already = (local.images || []).includes(url);
                  return (
                    <div key={i} style={{ position: 'relative', cursor: already ? 'default' : 'pointer', opacity: already ? 0.4 : 1 }}
                      onClick={() => { if (!already) { const imgs = [...(local.images || []), url]; setLocal(v => ({ ...v, images: imgs, image: imgs[0] || v.image })); } }}>
                      <img src={url} alt="" style={{ width: 52, height: 52, objectFit: 'cover', borderRadius: 6, border: `1px solid ${already ? '#22c55e' : 'var(--border)'}` }} onError={e => { (e.target as HTMLImageElement).style.display = 'none'; }} />
                      {already && <div style={{ position: 'absolute', inset: 0, display: 'flex', alignItems: 'center', justifyContent: 'center', fontSize: 18 }}>✓</div>}
                    </div>
                  );
                })}
              </div>
            </div>
          )}

          {/* Add by URL */}
          <div style={{ display: 'flex', gap: 8 }}>
            <input style={{ ...mInput, flex: 1 }} value={imageInputUrl} onChange={e => setImageInputUrl(e.target.value)}
              onKeyDown={e => e.key === 'Enter' && addImageUrl()} placeholder="Paste image URL and press Enter" />
            <button onClick={addImageUrl} style={{ padding: '10px 18px', borderRadius: 8, border: 'none', background: '#22c55e', color: '#fff', fontSize: 13, fontWeight: 700, cursor: 'pointer' }}>+ Add</button>
          </div>
        </div>

        {/* ── PRODUCT DETAILS ── */}
        <div style={mHead('#f59e0b')}>Product Details</div>
        <div style={mSection('#f59e0b')}>
          <div style={mRow}>
            <div style={{ flex: 1, minWidth: 300 }}>
              <label style={mLabel}>Title (variant-specific override)</label>
              <input style={mInput} value={local.title || ''} onChange={e => set('title', e.target.value)} placeholder="Leave blank to use parent title" />
            </div>
          </div>
          <div>
            <label style={mLabel}>Description (variant-specific override)</label>
            <textarea style={{ ...mInput, minHeight: 80, resize: 'vertical' as const }} value={local.description || ''} onChange={e => set('description', e.target.value)} placeholder="Leave blank to use parent description" />
          </div>
        </div>

        {/* ── DIMENSIONS ── */}
        <div style={mHead('#8b5cf6')}>Dimensions &amp; Weight</div>
        <div style={mSection('#8b5cf6')}>
          <div style={mRow}>
            <div style={mFlex(80)}><label style={mLabel}>Length (cm)</label><input style={mInput} type="number" step="0.1" value={local.length || ''} onChange={e => set('length', e.target.value)} placeholder="—" /></div>
            <div style={mFlex(80)}><label style={mLabel}>Width (cm)</label><input style={mInput} type="number" step="0.1" value={local.width || ''} onChange={e => set('width', e.target.value)} placeholder="—" /></div>
            <div style={mFlex(80)}><label style={mLabel}>Height (cm)</label><input style={mInput} type="number" step="0.1" value={local.height || ''} onChange={e => set('height', e.target.value)} placeholder="—" /></div>
            <div style={mFlex(80)}><label style={mLabel}>Weight (g)</label><input style={mInput} type="number" step="1" value={local.weight || ''} onChange={e => set('weight', e.target.value)} placeholder="—" /></div>
          </div>
        </div>

        {/* Actions */}
        <div style={{ display: 'flex', gap: 10, justifyContent: 'flex-end', marginTop: 4 }}>
          <button onClick={onClose} style={{ padding: '9px 20px', borderRadius: 7, border: '1px solid var(--border)', background: 'var(--bg-tertiary)', color: 'var(--text-muted)', fontSize: 13, cursor: 'pointer', fontWeight: 500 }}>Cancel</button>
          <button onClick={() => onSave(local, applyToAll)} style={{ padding: '9px 24px', borderRadius: 7, border: 'none', background: TEMU_ORANGE, color: '#fff', fontSize: 13, cursor: 'pointer', fontWeight: 700 }}>
            {applyToAll ? 'Save & Apply Pricing to All' : 'Save Variant'}
          </button>
        </div>
      </div>
    </div>
  );
}
