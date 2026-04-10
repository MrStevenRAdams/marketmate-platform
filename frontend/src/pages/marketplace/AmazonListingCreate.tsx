// ============================================================================
// AMAZON LISTING PAGE — Enhanced with conditional attributes, GPSR, restrictions
// ============================================================================
// Location: frontend/src/pages/marketplace/AmazonListingCreate.tsx
// Arrives with ?product_id=xxx&credential_id=yyy from product edit page.
// Single form view matching TemuListingCreate pattern.
//
// Enhancements over v1:
//   1. Parsed schema from backend — structured attributes with types, enums, constraints
//   2. Conditional field rendering — if/then rules (e.g. batteries_required → battery_type)
//   3. GPSR compliance section — manufacturer, responsible person, safety attestation, documents
//   4. Restrictions banner — shows approval gates before form fill
//   5. Validation Preview button — dry-run against Amazon without persisting

import React, { useState, useEffect, useCallback, useRef, useMemo } from 'react';
import { useSearchParams, useNavigate } from 'react-router-dom';
import {
  amazonApi, AmazonDraft, AmazonVariantDraft, ProductTypeResult,
  AmazonSubmitResponse, AmazonValidateResponse, ParsedSchemaResult,
  ParsedAttribute, ConditionalRuleFlat, Restriction,
} from '../../services/amazon-api';
import ConfiguratorSelector from './ConfiguratorSelector';
import { configuratorService, ConfiguratorDetail } from '../../services/configurator-api';
import { listingService } from '../../services/marketplace-api';
import { fetchKeywordIntelligence } from '../../utils/keywordUtils';
import { KeywordIntelligenceResponse } from '../../types/seo';

const CONDITION_OPTIONS = [
  { value: 'new_new', label: 'New' },
  { value: 'new_open_box', label: 'New - Open Box' },
  { value: 'refurbished_refurbished', label: 'Refurbished' },
  { value: 'used_like_new', label: 'Used - Like New' },
  { value: 'used_very_good', label: 'Used - Very Good' },
  { value: 'used_good', label: 'Used - Good' },
  { value: 'used_acceptable', label: 'Used - Acceptable' },
];

// ── Field categorization ──
// These fields are "promoted" into dedicated main form sections instead of appearing
// as generic schema attributes. This avoids duplication (e.g. weight in Dimensions AND in attributes).
const PROMOTED_FIELDS = new Set([
  // Already in the main form sections:
  'item_name', 'brand', 'product_description', 'bullet_point',
  'externally_assigned_product_identifier', 'merchant_suggested_asin',
  'condition_type', 'purchasable_offer', 'fulfillment_availability',
  'main_product_image_locator',
  'other_product_image_locator_1', 'other_product_image_locator_2',
  'other_product_image_locator_3', 'other_product_image_locator_4',
  'other_product_image_locator_5', 'other_product_image_locator_6',
  'other_product_image_locator_7', 'other_product_image_locator_8',
  'swatch_product_image_locator',
  // Promoted to Dimensions & Weight section:
  'item_dimensions', 'item_package_dimensions', 'item_package_weight',
  'item_weight', 'item_display_dimensions',
  // Promoted to Product Identity section:
  'recommended_browse_nodes', 'manufacturer', 'model_name', 'model_number',
  // Promoted to core Product Details:
  'country_of_origin', 'color', 'material', 'size',
  // Variation fields handled separately:
  'parentage_level', 'child_parent_sku_relationship', 'variation_theme',
]);

// Smart sub-groups within Safety & Compliance to avoid a 40-field dumping ground
const SAFETY_SUBGROUPS: Record<string, { title: string; fields: string[] }> = {
  battery: {
    title: '🔋 Battery Information',
    fields: [
      'batteries_required', 'batteries_included', 'battery', 'num_batteries',
      'contains_battery_or_cell', 'battery_installation_device_type',
      'has_replaceable_battery', 'has_multiple_battery_powered_components',
      'battery_contains_free_unabsorbed_liquid', 'is_battery_non_spillable',
      'has_less_than_30_percent_state_of_charge',
      'number_of_lithium_metal_cells', 'number_of_lithium_ion_cells',
      'lithium_battery', 'non_lithium_battery_energy_content', 'non_lithium_battery_packaging',
    ],
  },
  hazmat: {
    title: '☣️ Hazmat & GHS',
    fields: [
      'supplier_declared_dg_hz_regulation', 'ghs', 'ghs_chemical_h_code',
      'hazmat', 'safety_data_sheet_url',
    ],
  },
  toy_safety: {
    title: '🧸 Toy Safety (EU Directive)',
    fields: [
      'eu_toys_safety_directive_age_warning', 'eu_toys_safety_directive_warning',
      'eu_toys_safety_directive_language',
      'compliance_age_range', 'compliance_operation_mode', 'compliance_recommended_age',
      'compliance_toy_material', 'compliance_toy_type',
    ],
  },
  general_safety: {
    title: '🛡️ General Safety',
    fields: [
      'safety_warning', 'warranty_description', 'ships_globally',
      'is_this_product_subject_to_buyer_age_restrictions', 'is_oem_sourced_product',
    ],
  },
};

// Fields to hide from optional attributes — these are either too generic,
// irrelevant for most product types, or complex object types that need custom UI
const LOW_RELEVANCE_FIELDS = new Set([
  'ink', 'lens', 'ring', 'gift_options', 'list_price',
  'main_offer_image_locator', 'other_offer_image_locator_1',
  'other_offer_image_locator_2', 'other_offer_image_locator_3',
  'other_offer_image_locator_4', 'other_offer_image_locator_5',
  'supplemental_condition_information', 'package_contains_sku',
  'epr_product_packaging',
]);

const FULFILLMENT_OPTIONS = [
  { value: 'DEFAULT', label: 'Merchant Fulfilled (FBM)' },
  { value: 'AMAZON_EU', label: 'Fulfilled by Amazon EU (FBA)' },
  { value: 'AMAZON_NA', label: 'Fulfilled by Amazon NA (FBA)' },
];

// Marketplace → language_tag mapping for text attributes
const MARKETPLACE_LANG: Record<string, string> = {
  'A1F83G8C2ARO7P': 'en_GB',  // UK
  'ATVPDKIKX0DER': 'en_US',   // US
  'A1AM78C64UM0Y8': 'es_MX',  // Mexico
  'A2EUQ1WTGCTBG2': 'en_CA',  // Canada
  'A1PA6795UKMFR9': 'de_DE',  // Germany
  'A13V1IB3VIYZZH': 'fr_FR',  // France
  'APJ6JRA9NG5V4': 'it_IT',   // Italy
  'A1RKKUPIHCS9HS': 'es_ES',  // Spain
  'A1805IZSGTT6HS': 'nl_NL',  // Netherlands
  'A2NODRKZP88ZB9': 'sv_SE',  // Sweden
  'A1C3SOZRARQ6R3': 'pl_PL',  // Poland
  'ARBP9OOSHTCHU': 'en_EG',   // Egypt
  'A33AVAJ2PDY3EV': 'tr_TR',  // Turkey
  'A21TJRUUN4KGV': 'en_IN',   // India
  'A19VAU5U5O7RUS': 'en_SG',  // Singapore
  'A39IBJ37TRP1C6': 'en_AU',  // Australia
  'A1VC38T7YXB528': 'ja_JP',  // Japan
};

// Marketplace → default currency
const MARKETPLACE_CURRENCY: Record<string, string> = {
  'A1F83G8C2ARO7P': 'GBP',
  'ATVPDKIKX0DER': 'USD',
  'A1AM78C64UM0Y8': 'MXN',
  'A2EUQ1WTGCTBG2': 'CAD',
  'A1PA6795UKMFR9': 'EUR',
  'A13V1IB3VIYZZH': 'EUR',
  'APJ6JRA9NG5V4': 'EUR',
  'A1RKKUPIHCS9HS': 'EUR',
  'A1805IZSGTT6HS': 'EUR',
  'A2NODRKZP88ZB9': 'SEK',
  'A1C3SOZRARQ6R3': 'PLN',
  'A21TJRUUN4KGV': 'INR',
  'A1VC38T7YXB528': 'JPY',
  'A19VAU5U5O7RUS': 'SGD',
  'A39IBJ37TRP1C6': 'AUD',
};

const CURRENCY_SYMBOL: Record<string, string> = {
  GBP: '£', USD: '$', EUR: '€', CAD: 'C$', MXN: 'MX$', SEK: 'kr',
  PLN: 'zł', INR: '₹', JPY: '¥', SGD: 'S$', AUD: 'A$',
};

export default function AmazonListingCreate() {
  const navigate = useNavigate();
  const [searchParams] = useSearchParams();
  const productId = searchParams.get('product_id');
  const credentialId = searchParams.get('credential_id') || '';
  const existingListingId = searchParams.get('listing_id') || ''; // pre-populate from imported listing

  // Core state
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState('');
  const [draft, setDraft] = useState<AmazonDraft | null>(null);

  // Product type search
  const [productTypes, setProductTypes] = useState<ProductTypeResult[]>([]);
  const [ptSearchQuery, setPtSearchQuery] = useState('');
  const [ptSearching, setPtSearching] = useState(false);
  const [changingProductType, setChangingProductType] = useState(false);

  // Schema — parsed attributes, conditional rules, GPSR
  const [parsedSchema, setParsedSchema] = useState<ParsedSchemaResult | null>(null);

  // Restrictions
  const [restrictions, setRestrictions] = useState<Restriction[]>([]);

  // Brand approval
  const [brandStatus, setBrandStatus] = useState<'unchecked' | 'checking' | 'approved' | 'restricted'>('unchecked');
  const [brandMessage, setBrandMessage] = useState('');
  const brandCheckRef = useRef<ReturnType<typeof setTimeout> | null>(null);

  // Debug data from Firestore
  const [debugListing, setDebugListing] = useState<any>(null);
  const [debugExtendedData, setDebugExtendedData] = useState<any>(null);
  const [debugErrors, setDebugErrors] = useState<string[]>([]);
  const [prepareResponse, setPrepareResponse] = useState<any>(null);

  // Variants
  const [variants, setVariants] = useState<AmazonVariantDraft[]>([]);
  const [isVariantProduct, setIsVariantProduct] = useState(false);
  const [editingVariant, setEditingVariant] = useState<AmazonVariantDraft | null>(null);

  // Submit / Validate
  const [submitting, setSubmitting] = useState(false);
  const [submitResult, setSubmitResult] = useState<AmazonSubmitResponse | null>(null);
  const [validating, setValidating] = useState(false);
  const [validateResult, setValidateResult] = useState<AmazonValidateResponse | null>(null);

  // FLD-14 — Amazon HTML description strip warning
  const [htmlStripped, setHtmlStripped] = useState(false);

  // AI generation state
  const [aiGenerating, setAiGenerating] = useState(false);
  // ── Keyword intelligence (Session 8) ──────────────────────────────────────
  const [kwData, setKwData] = useState<KeywordIntelligenceResponse | null>(null);
  const [aiApplied, setAiApplied] = useState(false);
  const [aiError, setAiError] = useState('');

  // FLD-15: SKU duplicate detection
  const [skuDuplicateChannels, setSkuDuplicateChannels] = useState<string[]>([]);

  // ── Configurator (CFG-07) ──
  const [selectedConfigurator, setSelectedConfigurator] = useState<ConfiguratorDetail | null>(null);

  const handleConfiguratorSelect = (cfg: ConfiguratorDetail | null) => {
    setSelectedConfigurator(cfg);
    if (!cfg) return;
    // Pre-populate product type / category
    if (cfg.category_id && draft) {
      setDraft(d => d ? { ...d, productType: cfg.category_id!, productTypeName: cfg.category_path || cfg.category_id! } : d);
    }
    // Pre-populate attribute defaults
    if (cfg.attribute_defaults && cfg.attribute_defaults.length > 0 && draft) {
      const extraAttrs: Record<string, any> = {};
      for (const attr of cfg.attribute_defaults) {
        if (attr.source === 'default_value' && attr.default_value) {
          extraAttrs[attr.attribute_name] = [{ value: attr.default_value }];
        }
      }
      if (Object.keys(extraAttrs).length > 0) {
        setDraft(d => d ? { ...d, attributes: { ...(d.attributes || {}), ...extraAttrs } } : d);
      }
    }
    // Pre-populate shipping / fulfillment channel
    if (cfg.shipping_defaults?.fulfillment_channel && draft) {
      setDraft(d => d ? { ...d, fulfillmentChannel: cfg.shipping_defaults!.fulfillment_channel } : d);
    }
    // Pre-populate variation schema axis names (FLD-15)
    if (cfg.variation_schema && cfg.variation_schema.length > 0) {
      setIsVariantProduct(true);
    }
  };

  const debounceRef = useRef<ReturnType<typeof setTimeout> | null>(null);
  const log = (...args: any[]) => console.log(...args);

  // Marketplace ID from the credential — used for all attribute payloads
  const mp = draft?.marketplaceId || 'A1F83G8C2ARO7P'; // fallback to UK

  // ── Brand approval check (debounced) ──
  const checkBrandApproval = useCallback((brandName: string) => {
    if (brandCheckRef.current) clearTimeout(brandCheckRef.current);
    if (!brandName || brandName.length < 2 || !draft?.productType) {
      setBrandStatus('unchecked');
      setBrandMessage('');
      return;
    }
    setBrandStatus('checking');
    setBrandMessage('');
    brandCheckRef.current = setTimeout(async () => {
      try {
        const lang = MARKETPLACE_LANG[mp] || 'en_GB';
        const res = await amazonApi.validate({
          product_id: productId,
          credential_id: credentialId,
          sku: draft?.sku || 'BRAND_CHECK_TEMP',
          productType: draft?.productType,
          attributes: {
            brand: [{ value: brandName, language_tag: lang }],
            item_name: [{ value: draft?.title || 'Brand Check', language_tag: lang, marketplace_id: mp }],
            condition_type: [{ value: draft?.condition || 'new_new' }],
            purchasable_offer: [{ marketplace_id: mp, currency: draft?.currency || 'GBP', audience: 'ALL', our_price: [{ schedule: [{ value_with_tax: 9.99 }] }] }],
            fulfillment_availability: [{ fulfillment_channel_code: draft?.fulfillmentChannel || 'DEFAULT' }],
          },
        });
        const data = res.data;
        // Look for brand-gating issues
        const brandIssues = (data.issues || []).filter((iss: any) =>
          (iss.attributeNames || []).includes('brand') ||
          (iss.message || '').toLowerCase().includes('brand') ||
          (iss.code || '').toLowerCase().includes('brand') ||
          (iss.code || '').toLowerCase().includes('approval') ||
          (iss.message || '').toLowerCase().includes('approval_required') ||
          (iss.message || '').toLowerCase().includes('not authorized')
        );
        if (brandIssues.length > 0) {
          setBrandStatus('restricted');
          setBrandMessage(brandIssues[0]?.message || 'Brand approval may be required — you may need to apply in Seller Central');
        } else {
          setBrandStatus('approved');
          setBrandMessage('');
        }
      } catch (err: any) {
        // If validation fails entirely, don't block — just mark as unchecked
        setBrandStatus('unchecked');
        setBrandMessage('Could not verify brand approval');
      }
    }, 1200); // 1.2s debounce
  }, [draft?.productType, draft?.sku, draft?.title, draft?.condition, draft?.currency, draft?.fulfillmentChannel, mp, credentialId, productId]);

  // ── Prepare listing on mount ──
  useEffect(() => {
    if (!productId) { setError('No product_id provided.'); setLoading(false); return; }
    prepareListing(productId);
  }, [productId]);

  // Session 8: fetch keyword intelligence
  useEffect(() => {
    if (!productId) return;
    fetchKeywordIntelligence(productId, import.meta.env.VITE_API_URL || 'http://localhost:8080/api/v1')
      .then(d => setKwData(d));
  }, [productId]);

  async function prepareListing(pid: string, productTypeOverride?: string) {
    setLoading(true);
    setError('');
    try {
      const payload: any = { product_id: pid };
      if (productTypeOverride) payload.product_type = productTypeOverride;
      if (credentialId) payload.credential_id = credentialId;
      if (existingListingId) payload.listing_id = existingListingId;

      const res = await amazonApi.prepare(payload);
      const data = res.data as any;
      setPrepareResponse(data); // store full response for debugging
      if (!data?.ok) { setError(data?.error || 'Failed to prepare listing'); setLoading(false); return; }
      if (data.draft) {
        let finalDraft = data.draft;
        if (data.draft.variants && data.draft.variants.length > 0) {
          setVariants(data.draft.variants);
          setIsVariantProduct(true);
        }

        setDraft({ ...finalDraft, useMainImagesOnly: finalDraft.useMainImagesOnly ?? false });
      }
      if (data.productTypes) setProductTypes(data.productTypes);
      if (data.parsedSchema) setParsedSchema(data.parsedSchema);
      if (data.restrictions) setRestrictions(data.restrictions);
      if (data.debugListing) setDebugListing(data.debugListing);
      if (data.debugExtendedData) setDebugExtendedData(data.debugExtendedData);
      if (data.debugErrors) setDebugErrors(data.debugErrors);

      // ── AI Generation: if ?ai=pending, call AI with the resolved schema ──
      const aiFlag = searchParams.get('ai');
      if (aiFlag === 'pending' && data.parsedSchema && data.draft) {
        setAiGenerating(true);
        try {
          // Extract schema fields from parsedSchema
          const schemaFields: import('../../services/ai-api').SchemaField[] = [];
          if (data.parsedSchema?.attributes) {
            for (const attr of data.parsedSchema.attributes) {
              schemaFields.push({
                name: attr.name || attr.jsonKey || '',
                display_name: attr.displayName || attr.label || '',
                data_type: attr.type === 'SELECTION' ? 'enum' : (attr.type || 'string').toLowerCase(),
                required: attr.required || false,
                allowed_values: attr.acceptedValues || attr.enumValues || [],
                max_length: attr.maxLength || 0,
              });
            }
          }

          const categoryName = data.draft.productTypeName || data.draft.productType || '';
          const categoryId = data.draft.productType || '';

          log('[Amazon] AI generating with schema: %d fields, category=%s', schemaFields.length, categoryName);

          const { aiService: aiApi } = await import('../../services/ai-api');
          const aiRes = await aiApi.generateWithSchema({
            product_id: pid,
            channel: 'amazon',
            category_id: categoryId,
            category_name: categoryName,
            fields: schemaFields,
          });

          const aiListing = aiRes.data.data?.listings?.[0];
          if (aiListing) {
            log('[Amazon] AI result received: confidence=%s', aiListing.confidence);
            const mp = data.draft.marketplaceId || 'ATVPDKIKX0DER';

            setDraft((prev: any) => {
              if (!prev) return prev;
              const updated = { ...prev };

              // Override content fields
              if (aiListing.title) updated.title = aiListing.title;
              if (aiListing.description) updated.description = aiListing.description;
              if (aiListing.bullet_points?.length) updated.bulletPoints = aiListing.bullet_points;

              // Override/merge attributes
              if (aiListing.attributes) {
                updated.attributes = { ...updated.attributes };
                for (const [key, val] of Object.entries(aiListing.attributes)) {
                  // Wrap in marketplace array format if not already
                  if (Array.isArray(val)) {
                    updated.attributes[key] = val;
                  } else {
                    updated.attributes[key] = [{ value: String(val), marketplace_id: mp }];
                  }
                }
              }

              // Search terms
              if (aiListing.search_terms?.length) {
                updated.attributes = updated.attributes || {};
                updated.attributes['generic_keyword'] = aiListing.search_terms.map((t: string) => ({
                  value: t, marketplace_id: mp,
                }));
              }

              return updated;
            });

            setAiApplied(true);
          }
        } catch (aiErr: any) {
          log('[Amazon] AI generation failed: %s', aiErr.message);
          setAiError(aiErr.response?.data?.error || aiErr.message || 'AI generation failed');
        }
        setAiGenerating(false);
      }
      if (data.productTypes) setProductTypes(data.productTypes);
      if (data.parsedSchema) setParsedSchema(data.parsedSchema);
      if (data.restrictions) setRestrictions(data.restrictions);
      if (data.debugListing) setDebugListing(data.debugListing);
      if (data.debugExtendedData) setDebugExtendedData(data.debugExtendedData);
      if (data.debugErrors) setDebugErrors(data.debugErrors);
    } catch (err: any) {
      setError(err?.response?.data?.error || err.message || 'Network error');
    }
    setLoading(false);
  };

  // ── Product type search ──
  const handleProductTypeSearch = useCallback(async (query: string) => {
    setPtSearchQuery(query);
    if (debounceRef.current) clearTimeout(debounceRef.current);
    if (query.length < 2) return;
    debounceRef.current = setTimeout(async () => {
      setPtSearching(true);
      try {
        const res = await amazonApi.searchProductTypes({ keywords: query, credential_id: credentialId });
        if (res.data?.ok) setProductTypes(res.data.productTypes || []);
      } catch (err) { console.warn('[Amazon] Product type search failed:', err); }
      setPtSearching(false);
    }, 400);
  }, [credentialId]);

  const selectProductType = async (pt: ProductTypeResult) => {
    if (!draft || !productId) return;
    const ptCode = pt.productType || (pt as any).name || '';
    setChangingProductType(false);
    setDraft(d => d ? { ...d, productType: ptCode, productTypeName: pt.displayName } : null);
    if (ptCode) {
      await prepareListing(productId, ptCode);
    }
  };

  // ── Draft helpers ──
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

  const updateAttribute = (key: string, value: any) => {
    setDraft(d => {
      if (!d) return null;
      const attrs = { ...d.attributes };
      if (value === '' || value === null || value === undefined) {
        delete attrs[key];
      } else {
        attrs[key] = [{ value, marketplace_id: mp }];
      }
      return { ...d, attributes: attrs };
    });
  };

  const getAttributeValue = (key: string): string => {
    if (!draft?.attributes?.[key]) return '';
    const arr = draft.attributes[key];
    if (Array.isArray(arr) && arr.length > 0) return arr[0]?.value || '';
    return '';
  };

  const updateVariant = (id: string, field: keyof AmazonVariantDraft, value: any) => {
    setVariants(vs => vs.map(v => v.id === id ? { ...v, [field]: value } : v));
  };

  // ── Conditional field visibility ──
  // Compute which attributes are conditionally required based on current attribute values
  const conditionallyRequired = useMemo(() => {
    if (!parsedSchema?.conditionalRules || !draft) return new Set<string>();
    const result = new Set<string>();
    for (const rule of parsedSchema.conditionalRules) {
      const currentVal = getAttributeValue(rule.ifField);
      if (!currentVal) continue;
      // Check if value matches: exact match, or one of the pipe-separated values
      const triggerValues = rule.ifValue.split('|');
      if (triggerValues.includes(currentVal)) {
        rule.thenFields.forEach(f => result.add(f));
      }
    }
    return result;
  }, [parsedSchema?.conditionalRules, draft?.attributes]);

  // ── Build attributes payload for submit/validate ──
  const buildAttributesPayload = () => {
    if (!draft) return {};
    const attributes: Record<string, any> = { ...draft.attributes };
    const lang = MARKETPLACE_LANG[mp] || 'en_GB';
    const cur = draft.currency || MARKETPLACE_CURRENCY[mp] || 'GBP';

    // Text attributes with language_tag
    if (draft.title) attributes['item_name'] = [{ value: draft.title, language_tag: lang, marketplace_id: mp }];
    if (draft.brand) attributes['brand'] = [{ value: draft.brand, language_tag: lang }];
    if (draft.bulletPoints.length > 0) attributes['bullet_point'] = draft.bulletPoints.map(bp => ({ value: bp, language_tag: lang, marketplace_id: mp }));
    if (draft.description) attributes['product_description'] = [{ value: draft.description, language_tag: lang, marketplace_id: mp }];
    if (draft.condition) attributes['condition_type'] = [{ value: draft.condition }];
    if ((draft as any).conditionNote && draft.condition !== 'new_new') {
      attributes['condition_note'] = [{ value: (draft as any).conditionNote, language_tag: lang, marketplace_id: mp }];
    }

    // ── Purchasable Offer (B2C) ──
    if (draft.price) {
      const priceSchedule: any[] = [];

      // Main price
      const mainEntry: any = { value_with_tax: parseFloat(draft.price) };
      priceSchedule.push(mainEntry);

      const b2cOffer: any = {
        marketplace_id: mp,
        currency: cur,
        audience: 'ALL',
        our_price: [{ schedule: priceSchedule }],
      };

      // List price / RRP (strikethrough)
      if (draft.listPrice) {
        b2cOffer.list_price = [{ value_with_tax: parseFloat(draft.listPrice) }];
      }

      // Sale / discounted price with date range
      if (draft.salePrice && draft.salePriceStart && draft.salePriceEnd) {
        b2cOffer.discounted_price = [{
          schedule: [{
            value_with_tax: parseFloat(draft.salePrice),
            start_at: { value: new Date(draft.salePriceStart).toISOString() },
            end_at: { value: new Date(draft.salePriceEnd).toISOString() },
          }],
        }];
      }

      const offers = [b2cOffer];

      // ── B2B offer (Amazon Business) ──
      if (draft.b2bPrice) {
        const b2bOffer: any = {
          marketplace_id: mp,
          currency: cur,
          audience: 'B2B',
          our_price: [{ schedule: [{ value_with_tax: parseFloat(draft.b2bPrice) }] }],
        };

        // Quantity discount tiers (up to 5)
        const tiers: any[] = [];
        for (let n = 1; n <= 5; n++) {
          const qty = (draft as any)[`b2bTier${n}Qty`];
          const price = (draft as any)[`b2bTier${n}Price`];
          if (qty && price) {
            tiers.push({ lower_bound: parseInt(qty), price: [{ schedule: [{ value_with_tax: parseFloat(price) }] }] });
          }
        }
        if (tiers.length > 0) {
          b2bOffer.quantity_discount_plan = [{ discount_type: 'FIXED', quantity_discount: tiers }];
        }

        offers.push(b2bOffer);
      }

      attributes['purchasable_offer'] = offers;
    }

    // ── Fulfillment Availability ──
    if (draft.fulfillmentChannel) {
      const fa: any = { fulfillment_channel_code: draft.fulfillmentChannel };
      if (draft.fulfillmentChannel === 'DEFAULT') {
        // MFN: include quantity, handling time, restock date
        if (draft.quantity) fa.quantity = parseInt(draft.quantity);
        if (draft.handlingTime) fa.lead_time_to_ship_max_days = parseInt(draft.handlingTime);
        if (draft.restockDate) fa.restock_date = draft.restockDate;
      }
      attributes['fulfillment_availability'] = [fa];
    }

    // Images
    if (draft.images.length > 0) {
      attributes['main_product_image_locator'] = [{ media_location: draft.images[0], marketplace_id: mp, ...(((draft as any).imageAlts?.[0]) ? { item_display_name: (draft as any).imageAlts[0] } : {}) }];
      if (draft.images.length > 1) attributes['other_product_image_locator'] = draft.images.slice(1).map((img, i) => ({ media_location: img, marketplace_id: mp, ...(((draft as any).imageAlts?.[i + 1]) ? { item_display_name: (draft as any).imageAlts[i + 1] } : {}) }));
    }

    // Item dimensions (with units)
    if (draft.length || draft.width || draft.height) {
      const dimUnit = draft.lengthUnit || 'centimeters';
      const dim: any = { marketplace_id: mp };
      if (draft.length) dim.length = { unit: dimUnit, value: parseFloat(draft.length) };
      if (draft.width) dim.width = { unit: dimUnit, value: parseFloat(draft.width) };
      if (draft.height) dim.height = { unit: dimUnit, value: parseFloat(draft.height) };
      attributes['item_dimensions'] = [dim];
    }
    if (draft.weight) {
      const wUnit = draft.weightUnit || 'kilograms';
      attributes['item_weight'] = [{ unit: wUnit, value: parseFloat(draft.weight), marketplace_id: mp }];
    }

    // Package dimensions (with units)
    const d = draft as any;
    if (d.pkgLength || d.pkgWidth || d.pkgHeight) {
      const pkgDimUnit = d.pkgLengthUnit || 'centimeters';
      const pkgDim: any = { marketplace_id: mp };
      if (d.pkgLength) pkgDim.length = { unit: pkgDimUnit, value: parseFloat(d.pkgLength) };
      if (d.pkgWidth) pkgDim.width = { unit: pkgDimUnit, value: parseFloat(d.pkgWidth) };
      if (d.pkgHeight) pkgDim.height = { unit: pkgDimUnit, value: parseFloat(d.pkgHeight) };
      attributes['item_package_dimensions'] = [pkgDim];
    }
    if (d.pkgWeight) {
      const pkgWUnit = d.pkgWeightUnit || 'kilograms';
      attributes['item_package_weight'] = [{ unit: pkgWUnit, value: parseFloat(d.pkgWeight), marketplace_id: mp }];
    }

    // Browse nodes (Amazon supports 2)
    // Primary is in attributes via recommended_browse_nodes
    if (d.browseNode2) {
      // Second browse node goes as a second entry in the array
      const existing = attributes['recommended_browse_nodes'];
      if (existing && Array.isArray(existing)) {
        existing.push({ value: d.browseNode2, marketplace_id: mp });
      } else if (existing) {
        attributes['recommended_browse_nodes'] = [existing, { value: d.browseNode2, marketplace_id: mp }];
      }
    }

    // Product identifiers (EAN, UPC, ISBN)
    if (draft.ean || draft.upc || (draft as any).isbn) {
      const ids: any[] = [];
      if (draft.ean) ids.push({ type: 'ean', value: draft.ean, marketplace_id: mp });
      if (draft.upc) ids.push({ type: 'upc', value: draft.upc, marketplace_id: mp });
      if ((draft as any).isbn) ids.push({ type: 'isbn', value: (draft as any).isbn, marketplace_id: mp });
      attributes['externally_assigned_product_identifier'] = ids;
    }

    // ── Phase 2 attributes ──

    // Repricing bounds
    if ((draft as any).minPrice) {
      attributes['minimum_seller_allowed_price'] = [{ value_with_tax: parseFloat((draft as any).minPrice), currency: cur, marketplace_id: mp }];
    }
    if ((draft as any).maxPrice) {
      attributes['maximum_seller_allowed_price'] = [{ value_with_tax: parseFloat((draft as any).maxPrice), currency: cur, marketplace_id: mp }];
    }

    // Shipping template
    if ((draft as any).shippingTemplate) {
      attributes['merchant_shipping_group'] = [{ value: (draft as any).shippingTemplate, marketplace_id: mp }];
    }

    // Product tax code
    if ((draft as any).productTaxCode) {
      attributes['product_tax_code'] = [{ value: (draft as any).productTaxCode, marketplace_id: mp }];
    }

    // Max order quantity
    if ((draft as any).maxOrderQty) {
      attributes['max_order_quantity'] = [{ value: parseInt((draft as any).maxOrderQty), marketplace_id: mp }];
    }

    // Release / pre-order date
    if ((draft as any).releaseDate) {
      attributes['merchant_release_date'] = [{ value: (draft as any).releaseDate }];
    }

    return attributes;
  };

  // ── FLD-14: Strip non-Amazon HTML from description ──
  // Amazon only allows <br/> tags in product descriptions; all other HTML is rejected.
  // This strips tags automatically on submit/validate and shows a non-blocking warning.
  const stripHtmlForAmazon = (text: string): { cleaned: string; stripped: boolean } => {
    if (!text) return { cleaned: text, stripped: false };
    // Replace <br> / <br/> / <br /> variants with a placeholder, strip all other tags, restore <br/>
    const withBrPlaceholder = text.replace(/<br\s*\/?>/gi, '\uFFFD');
    const stripped = withBrPlaceholder.replace(/<[^>]+>/g, '');
    const restored = stripped.replace(/\uFFFD/g, '<br/>');
    return { cleaned: restored, stripped: stripped !== withBrPlaceholder || restored !== text };
  };

  // ── Validate (dry-run) ──
  const handleValidate = async () => {
    if (!draft || !productId) return;
    setValidating(true);
    setValidateResult(null);
    try {
      // FLD-14: strip non-Amazon HTML before validating
      const { cleaned, stripped } = stripHtmlForAmazon(draft.description || '');
      if (stripped) {
        setHtmlStripped(true);
        updateDraft('description', cleaned);
      }
      // Build payload using cleaned description (state update is async so use cleaned directly)
      const attributes = buildAttributesPayload();
      if (stripped && cleaned) {
        const lang = MARKETPLACE_LANG[mp] || 'en_GB';
        attributes['product_description'] = [{ value: cleaned, language_tag: lang, marketplace_id: mp }];
      }
      const res = await amazonApi.validate({ product_id: productId, credential_id: credentialId, sku: draft.sku, productType: draft.productType, attributes });
      setValidateResult(res.data);
    } catch (err: any) {
      setValidateResult({ ok: false, error: err?.response?.data?.error || err.message });
    }
    setValidating(false);
  };

  // ── Submit ──
  const handleSubmit = async () => {
    if (!draft || !productId) return;
    setSubmitting(true);
    setSubmitResult(null);
    try {
      // FLD-14: strip non-Amazon HTML before submitting
      const { cleaned, stripped } = stripHtmlForAmazon(draft.description || '');
      if (stripped) {
        setHtmlStripped(true);
        updateDraft('description', cleaned);
      }
      // Build payload using cleaned description (state update is async so use cleaned directly)
      const attributes = buildAttributesPayload();
      if (stripped && cleaned) {
        const lang = MARKETPLACE_LANG[mp] || 'en_GB';
        attributes['product_description'] = [{ value: cleaned, language_tag: lang, marketplace_id: mp }];
      }
      const lang = MARKETPLACE_LANG[mp] || 'en_GB';
      const cur = draft.currency || MARKETPLACE_CURRENCY[mp] || 'GBP';
      const childListings = isVariantProduct ? variants.filter(v => v.active).map(v => ({
        sku: v.sku,
        attributes: {
          item_name: [{ value: draft.title, language_tag: lang, marketplace_id: mp }],
          brand: [{ value: draft.brand, language_tag: lang }],
          condition_type: [{ value: draft.condition }],
          purchasable_offer: [{ marketplace_id: mp, currency: cur, audience: 'ALL', our_price: [{ schedule: [{ value_with_tax: v.price }] }] }],
          fulfillment_availability: [{ fulfillment_channel_code: draft.fulfillmentChannel }],
          child_parent_sku_relationship: [{ type: 'variation', parent_sku: draft.sku }],
          ...Object.fromEntries(Object.entries(v.combination).map(([k, val]) => [k.toLowerCase().replace(/\s+/g, '_'), [{ value: val, marketplace_id: mp }]])),
          ...(v.ean ? { externally_assigned_product_identifier: [{ type: 'ean', value: v.ean, marketplace_id: mp }] } : {}),
          ...(!draft.useMainImagesOnly && v.image ? { main_product_image_locator: [{ media_location: v.image, marketplace_id: mp }] } : {}),
        },
      })) : [];

      const res = await amazonApi.submit({ product_id: productId, credential_id: credentialId, sku: draft.sku, productType: draft.productType, attributes, fulfillmentChannel: draft.fulfillmentChannel, childListings });
      setSubmitResult(res.data);
      // ── Configurator join (CFG-07): assign listing to configurator after successful create ──
      if (res.data?.ok && selectedConfigurator) {
        try {
          const listRes = await listingService.list({ product_id: productId!, channel: 'amazon', limit: 10 });
          const listings: any[] = listRes.data?.listings || listRes.data?.data || [];
          if (listings.length > 0) {
            const newest = listings[listings.length - 1];
            await configuratorService.assignListings(selectedConfigurator.configurator_id, [newest.listing_id]);
          }
        } catch { /* non-fatal — join is best-effort */ }
      }
    } catch (err: any) {
      const data = err?.response?.data;
      setSubmitResult({ ok: false, error: data?.error || err.message, request: data?.request, response: data?.response });
    }
    setSubmitting(false);
  };

  // ── Computed ──
  const canSubmit = draft && draft.title && draft.sku && draft.productType && !submitting;
  const hasBlockingRestrictions = restrictions.some(r => r.reasons.length > 0);

  // ── Build trigger→children map from conditional rules ──
  // This tells us: when field X has a value, fields [A, B, C] should appear below it
  const { triggerToChildren, childToTrigger } = useMemo(() => {
    const t2c = new Map<string, Set<string>>(); // trigger field → set of child field names
    const c2t = new Map<string, string>();       // child field → its trigger field
    if (!parsedSchema?.conditionalRules) return { triggerToChildren: t2c, childToTrigger: c2t };

    for (const rule of parsedSchema.conditionalRules) {
      if (!t2c.has(rule.ifField)) t2c.set(rule.ifField, new Set());
      for (const f of rule.thenFields) {
        t2c.get(rule.ifField)!.add(f);
        c2t.set(f, rule.ifField);
      }
    }
    return { triggerToChildren: t2c, childToTrigger: c2t };
  }, [parsedSchema?.conditionalRules]);

  // ── Categorize attributes into smart groups ──
  const { coreAttrs, safetySubgroups, offerAttrs, optionalAttrs } = useMemo(() => {
    if (!parsedSchema?.attributes) return { coreAttrs: [], safetySubgroups: new Map<string, ParsedAttribute[]>(), offerAttrs: [], optionalAttrs: [] };

    const core: ParsedAttribute[] = [];
    const safety = new Map<string, ParsedAttribute[]>();
    const offer: ParsedAttribute[] = [];
    const optional: ParsedAttribute[] = [];
    const safetyFieldToSubgroup = new Map<string, string>();

    // Build reverse lookup: field name → subgroup key
    for (const [key, sg] of Object.entries(SAFETY_SUBGROUPS)) {
      for (const f of sg.fields) safetyFieldToSubgroup.set(f, key);
    }

    // Fields that are children of a conditional trigger — these will be rendered
    // inline beneath their trigger, not in their normal position
    const isConditionalChild = (name: string) => childToTrigger.has(name);

    for (const attr of parsedSchema.attributes) {
      if (attr.hidden || PROMOTED_FIELDS.has(attr.name)) continue;

      // Skip conditional children — they'll be rendered inline by their trigger
      if (isConditionalChild(attr.name) && !triggerToChildren.has(attr.name)) continue;

      const isActive = attr.required || conditionallyRequired.has(attr.name) || getAttributeValue(attr.name) !== '';
      const isTrigger = triggerToChildren.has(attr.name);

      // Safety & Compliance → split into subgroups
      if (attr.group === 'safety_and_compliance') {
        const subgroup = safetyFieldToSubgroup.get(attr.name) || 'general_safety';
        if (isActive || attr.required || isTrigger) {
          if (!safety.has(subgroup)) safety.set(subgroup, []);
          safety.get(subgroup)!.push(attr);
        } else if (!LOW_RELEVANCE_FIELDS.has(attr.name)) {
          optional.push(attr);
        }
        continue;
      }

      // Offer fields
      if (attr.group === 'offer') {
        if ((isActive || isTrigger) && !LOW_RELEVANCE_FIELDS.has(attr.name)) {
          offer.push(attr);
        } else if (!LOW_RELEVANCE_FIELDS.has(attr.name)) {
          optional.push(attr);
        }
        continue;
      }

      // Core product details / identity
      if (isActive || isTrigger) {
        core.push(attr);
      } else if (!LOW_RELEVANCE_FIELDS.has(attr.name) && attr.type !== 'object') {
        optional.push(attr);
      }
    }

    return { coreAttrs: core, safetySubgroups: safety, offerAttrs: offer, optionalAttrs: optional };
  }, [parsedSchema?.attributes, conditionallyRequired, draft?.attributes, triggerToChildren, childToTrigger]);

  const hiddenOptionalCount = optionalAttrs.length;

  // ── Helper: render a field + its conditional children inline ──
  const attrMap = useMemo(() => {
    if (!parsedSchema?.attributes) return new Map<string, ParsedAttribute>();
    const m = new Map<string, ParsedAttribute>();
    for (const a of parsedSchema.attributes) m.set(a.name, a);
    if (parsedSchema.gpsrAttributes) {
      for (const a of parsedSchema.gpsrAttributes) m.set(a.name, a);
    }
    return m;
  }, [parsedSchema]);

  const renderFieldWithChildren = (attr: ParsedAttribute) => {
    const children = triggerToChildren.get(attr.name);
    // Show children when the trigger field has a truthy value (checkbox checked, dropdown selected, etc)
    const triggerVal = getAttributeValue(attr.name);
    const triggerIsActive = triggerVal !== '' && triggerVal !== false && triggerVal !== 'false' && triggerVal !== undefined && triggerVal !== null;

    return (
      <div key={attr.name}>
        <SchemaField
          attr={attr}
          value={triggerVal}
          onChange={val => updateAttribute(attr.name, val)}
          isConditionallyRequired={conditionallyRequired.has(attr.name)}
          isEditable={attr.editable}
        />
        {/* Render conditional children inline when trigger has a value */}
        {children && triggerIsActive && (
          <div style={{ marginLeft: 16, paddingLeft: 12, borderLeft: '2px solid rgba(251,191,36,0.3)', marginBottom: 8 }}>
            {[...children].map(childName => {
              const childAttr = attrMap.get(childName);
              if (!childAttr || childAttr.hidden) return null;
              return (
                <SchemaField
                  key={childName}
                  attr={childAttr}
                  value={getAttributeValue(childName)}
                  onChange={val => updateAttribute(childName, val)}
                  isConditionallyRequired={conditionallyRequired.has(childName)}
                  isEditable={childAttr.editable}
                />
              );
            })}
          </div>
        )}
      </div>
    );
  };

  // ── RENDER: Loading ──
  if (loading) return (
    <div style={{ maxWidth: 1280, margin: '0 auto', padding: '24px 16px', textAlign: 'center' }}>
      <div style={{ fontSize: 24, marginBottom: 8 }}>⏳</div>
      <p style={{ color: 'var(--text-secondary)' }}>Preparing Amazon listing...</p>
      <p style={{ color: 'var(--text-muted)', fontSize: 12 }}>Fetching product type schema, parsing conditional rules, checking restrictions</p>
    </div>
  );

  // ── RENDER: Fatal error ──
  if (error && !draft) return (
    <div style={{ maxWidth: 1280, margin: '0 auto', padding: '24px 16px' }}>
      <div style={{ padding: 16, background: 'var(--danger-glow)', borderRadius: 8, color: 'var(--danger)' }}>{error}</div>
      <button onClick={() => navigate(-1)} style={{ ...secondaryBtnStyle, marginTop: 16 }}>← Go Back</button>
    </div>
  );

  // ── RENDER: Success ──
  if (submitResult?.ok) return (
    <div style={{ maxWidth: 1280, margin: '0 auto', padding: '24px 16px', textAlign: 'center' }}>
      <div style={{ fontSize: 48, marginBottom: 16 }}>✅</div>
      <h2 style={{ fontSize: 20, fontWeight: 700, marginBottom: 8 }}>{draft?.isUpdate ? 'Updated on Amazon!' : 'Submitted to Amazon!'}</h2>
      <p style={{ color: 'var(--text-secondary)' }}>SKU: <strong>{draft?.sku}</strong></p>
      {submitResult.submissionId && <p style={{ color: 'var(--text-muted)', fontSize: 13 }}>Submission ID: {submitResult.submissionId}</p>}
      <p style={{ color: 'var(--text-muted)', fontSize: 13, marginBottom: 24 }}>Status: {submitResult.status}</p>
      <IssuesList issues={submitResult.issues} />
      <div style={{ display: 'flex', gap: 12, justifyContent: 'center', marginTop: 16 }}>
        <button onClick={() => navigate(-1)} style={secondaryBtnStyle}>← Back to Product</button>
        <button onClick={() => navigate('/marketplace/listings')} style={primaryBtnStyle}>View Listings</button>
      </div>
      <DebugPanel result={submitResult} />
    </div>
  );

  if (!draft) return null;

  return (
    <div style={{ maxWidth: 1280, margin: '0 auto', padding: '24px 16px' }}>
      {/* Header */}
      <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', marginBottom: 24 }}>
        <div style={{ display: 'flex', alignItems: 'center', gap: 12 }}>
          <button onClick={() => navigate(-1)} style={backBtnStyle}>← Back</button>
          <h1 style={{ fontSize: 22, fontWeight: 700, margin: 0 }}>{existingListingId ? 'Edit Amazon Listing' : 'Create Amazon Listing'}</h1>
          <span style={{ fontSize: 12, padding: '2px 8px', borderRadius: 4, background: '#ff9900', color: '#000', fontWeight: 700 }}>SP-API</span>
        </div>
        <div style={{ display: 'flex', gap: 8 }}>
          <button onClick={handleValidate} disabled={!canSubmit || validating} style={{ ...secondaryBtnStyle, opacity: canSubmit ? 1 : 0.5, fontSize: 13, padding: '8px 16px' }}>
            {validating ? '⏳ Validating...' : '🔍 Validate'}
          </button>
          <button onClick={handleSubmit} disabled={!canSubmit || hasBlockingRestrictions} style={{ ...primaryBtnStyle, opacity: canSubmit && !hasBlockingRestrictions ? 1 : 0.5, background: brandStatus === 'restricted' ? '#6b7280' : '#ff9900' }}>
            {submitting ? '⏳ Submitting...' : brandStatus === 'restricted' ? '💾 Save as Draft' : draft.isUpdate ? 'Update on Amazon' : 'Upload to Amazon'}
          </button>
        </div>
      </div>

      {/* Error / submit failure banners */}
      {error && <div style={{ padding: 12, background: 'var(--danger-glow)', borderRadius: 8, color: 'var(--danger)', marginBottom: 16, fontSize: 13 }}>{error}</div>}
      {submitResult && !submitResult.ok && (
        <div style={{ marginBottom: 16 }}>
          <div style={{ padding: 12, background: 'var(--danger-glow)', borderRadius: 8, color: 'var(--danger)', fontSize: 13 }}>{submitResult.error || 'Submission failed'}</div>
          <IssuesList issues={submitResult.issues} />
        </div>
      )}

      {/* Restrictions banner */}
      {hasBlockingRestrictions && <RestrictionsBanner restrictions={restrictions} />}

      {/* Validation preview results */}
      {validateResult && <ValidationBanner result={validateResult} />}

      {/* Update banner */}
      {draft.isUpdate && (
        <div style={{ padding: '8px 12px', background: 'rgba(59,130,246,0.1)', borderRadius: 8, fontSize: 12, color: 'var(--primary-light)', marginBottom: 16, display: 'flex', gap: 8, alignItems: 'center' }}>
          <span style={{ fontWeight: 600 }}>📦 Existing Amazon Listing</span>
          <span>SKU: {draft.sku}</span>
          {draft.asin && <span>ASIN: {draft.asin}</span>}
          <span style={{ color: 'var(--text-muted)' }}>— will use <code>PUT</code> (full replace)</span>
        </div>
      )}

      {/* ── Suggested search terms (Session 8) ── */}
      {productId && kwData && (() => {
        const backendKws = kwData.keywords.slice(10, 20).map(e => e.keyword).join(' ');
        const charCount = backendKws.length;
        return (
          <div style={{ marginTop: 16, padding: '12px 14px', background: 'var(--bg-secondary)', borderRadius: 8, border: '1px solid var(--border)' }}>
            <div style={{ display: 'flex', alignItems: 'center', gap: 8, marginBottom: 6 }}>
              <span style={{ fontSize: 12, fontWeight: 600, color: 'var(--text-primary)' }}>Suggested search terms (backend keywords)</span>
              <span style={{ fontSize: 11, color: charCount > 250 ? '#fbbf24' : 'var(--text-muted)' }}>{charCount} / 250 characters</span>
            </div>
            <p style={{ fontSize: 11, color: 'var(--text-muted)', marginBottom: 8 }}>Amazon's search terms field (generic_keyword) — keywords 11–20 from your keyword intelligence. Not visible to buyers.</p>
            <div style={{ padding: '8px 10px', background: 'var(--bg-primary)', borderRadius: 6, border: '1px solid var(--border)', fontSize: 12, color: 'var(--text-primary)', fontFamily: 'monospace', marginBottom: 8, wordBreak: 'break-word' }}>
              {backendKws || 'No keyword data yet.'}
            </div>
            {backendKws && (
              <button onClick={() => {
                if (!draft) return;
                setDraft((d: any) => d ? { ...d, attributes: { ...(d.attributes || {}), generic_keyword: [{ value: backendKws }] } } : d);
              }} style={{ fontSize: 11, padding: '4px 10px', borderRadius: 6, border: '1px solid rgba(20,184,166,0.5)', background: 'rgba(20,184,166,0.08)', color: 'rgb(20,184,166)', cursor: 'pointer' }}>
                Copy to search terms field
              </button>
            )}
          </div>
        );
      })()}

      {/* AI-generated content banner */}
      {aiGenerating && (
        <div style={{ padding: '12px 14px', background: 'linear-gradient(90deg, rgba(139,92,246,0.15), rgba(59,130,246,0.15))', borderRadius: 8, fontSize: 13, color: 'var(--accent-purple)', marginBottom: 16, display: 'flex', gap: 10, alignItems: 'center', border: '1px solid rgba(139,92,246,0.3)' }}>
          <div className="spinner" style={{ width: 16, height: 16, border: '2px solid var(--accent-purple)', borderTopColor: 'transparent', borderRadius: '50%', animation: 'spin 1s linear infinite' }} />
          <span style={{ fontWeight: 600 }}>🤖 AI is generating optimised content using the {draft.productTypeName || draft.productType} schema...</span>
        </div>
      )}
      {aiApplied && (
        <div style={{ padding: '10px 14px', background: 'linear-gradient(90deg, rgba(139,92,246,0.15), rgba(59,130,246,0.15))', borderRadius: 8, fontSize: 12, color: 'var(--accent-purple)', marginBottom: 16, display: 'flex', gap: 8, alignItems: 'center', border: '1px solid rgba(139,92,246,0.3)' }}>
          <span style={{ fontSize: 16 }}>🤖</span>
          <span style={{ fontWeight: 600 }}>AI-generated content applied</span>
          <span style={{ color: 'var(--text-muted)' }}>— title, description, bullet points, attributes and search terms have been filled using the {draft.productTypeName || 'marketplace'} schema. Review and edit before submitting.</span>
        </div>
      )}
      {aiError && (
        <div style={{ padding: '10px 14px', background: 'var(--warning-glow)', borderRadius: 8, fontSize: 12, color: 'var(--warning)', marginBottom: 16, display: 'flex', gap: 8, alignItems: 'center', border: '1px solid var(--warning)' }}>
          <span style={{ fontSize: 16 }}>⚠️</span>
          <span style={{ fontWeight: 600 }}>AI generation failed:</span>
          <span>{aiError}</span>
          <span style={{ color: 'var(--text-muted)' }}>— you can fill in the fields manually.</span>
        </div>
      )}

      <div style={{ display: 'flex', flexDirection: 'column', gap: 16 }}>

        {/* ═══ CONFIGURATOR (CFG-07) ═══ */}
        <ConfiguratorSelector channel="amazon" credentialId={credentialId} onSelect={handleConfiguratorSelect} />

        {/* ═══ ESSENTIALS ═══ */}
        <div style={{ fontSize: 11, fontWeight: 700, textTransform: 'uppercase', letterSpacing: 1.5, color: '#ff9900', paddingLeft: 4 }}>Essentials</div>

        {/* Product Type (Category) */}
        <Section title="Product Type *" accent="#ff9900">
          {!changingProductType ? (
            <div style={{ display: 'flex', alignItems: 'center', gap: 12 }}>
              <div style={{ flex: 1, padding: '10px 14px', background: 'var(--bg-secondary)', borderRadius: 8, fontSize: 14 }}>
                {draft.productTypeName || draft.productType || <span style={{ color: 'var(--text-muted)' }}>Not set — click Change to search</span>}
                {draft.productType && <span style={{ fontSize: 11, color: 'var(--text-muted)', marginLeft: 8 }}>({draft.productType})</span>}
              </div>
              <button onClick={() => setChangingProductType(true)} style={linkBtnStyle}>Change</button>
            </div>
          ) : (
            <div style={{ border: '1px solid var(--border)', borderRadius: 8, padding: 12 }}>
              <input value={ptSearchQuery} onChange={e => handleProductTypeSearch(e.target.value)} style={inputStyle} placeholder="Search product types (e.g. sporting goods, laptop, t-shirt)..." autoFocus />
              {ptSearching && <p style={{ fontSize: 12, color: 'var(--text-muted)', marginTop: 4 }}>Searching...</p>}
              {productTypes.length > 0 && (
                <div style={{ marginTop: 8, maxHeight: 250, overflow: 'auto' }}>
                  {productTypes.map((pt, i) => {
                    const ptCode = pt.productType || (pt as any).name || '';
                    return (
                      <button key={ptCode || i} onClick={() => selectProductType(pt)} style={{ width: '100%', textAlign: 'left', padding: '8px 12px', background: ptCode === draft.productType ? 'var(--primary-glow)' : 'none', border: 'none', cursor: 'pointer', borderRadius: 6, fontSize: 13, color: 'var(--text-primary)', display: 'flex', justifyContent: 'space-between', alignItems: 'center' }}>
                        <span>{pt.displayName}</span>
                        <span style={{ fontSize: 11, color: 'var(--text-muted)' }}>{ptCode}</span>
                      </button>
                    );
                  })}
                </div>
              )}
              <button onClick={() => setChangingProductType(false)} style={{ ...linkBtnStyle, marginTop: 8, fontSize: 12 }}>Cancel</button>
            </div>
          )}
        </Section>

        {/* Brand — at top, with approval check */}
        <Section title="Brand *" accent="#ff9900">
          <input
            value={draft.brand}
            onChange={e => {
              updateDraft('brand', e.target.value);
              checkBrandApproval(e.target.value);
            }}
            style={{
              ...inputStyle,
              borderColor: brandStatus === 'approved' ? '#22c55e' : brandStatus === 'restricted' ? '#f59e0b' : undefined,
            }}
            placeholder="Enter brand name"
          />
          {brandStatus === 'checking' && (
            <small style={{ color: 'var(--text-muted)', display: 'flex', alignItems: 'center', gap: 4, marginTop: 4 }}>
              ⏳ Checking brand approval...
            </small>
          )}
          {brandStatus === 'approved' && (
            <small style={{ color: '#22c55e', display: 'flex', alignItems: 'center', gap: 4, marginTop: 4 }}>
              ✅ Brand approved — you can list this brand
            </small>
          )}
          {brandStatus === 'restricted' && (
            <div style={{ marginTop: 6, padding: '8px 12px', background: 'rgba(245,158,11,0.08)', borderRadius: 8, fontSize: 12, color: '#d97706' }}>
              ⚠️ Brand may require approval — {brandMessage || 'you may need to apply via Seller Central before Amazon accepts this listing'}.
              You can still save this as a draft and submit once approved.
            </div>
          )}
          {brandStatus === 'unchecked' && brandMessage && (
            <small style={{ color: 'var(--text-muted)', marginTop: 4 }}>{brandMessage}</small>
          )}
        </Section>

        {/* ═══ LISTING CONTENT ═══ */}
        <div style={{ fontSize: 11, fontWeight: 700, textTransform: 'uppercase', letterSpacing: 1.5, color: '#3b82f6', paddingLeft: 4, marginTop: 8 }}>Listing Content</div>

        {/* Title */}
        <Section title="Title *" accent="#3b82f6">
          <input value={draft.title} onChange={e => updateDraft('title', e.target.value)} style={inputStyle} maxLength={500} />
          <small style={{ color: draft.title.length > 200 ? 'var(--danger)' : 'var(--text-muted)' }}>{draft.title.length}/500</small>
        </Section>

        {/* Bullet Points */}
        <Section title="Bullet Points (5)" accent="#3b82f6" subtitle="All 5 fields are shown to buyers on the product page">
          {Array.from({ length: 5 }).map((_, i) => {
            const val = (draft.bulletPoints || [])[i] || '';
            return (
              <div key={i} style={{ display: 'flex', gap: 6, marginBottom: 4 }}>
                <input value={val} onChange={e => {
                  const bps = [...(draft.bulletPoints || [])];
                  while (bps.length <= i) bps.push('');
                  bps[i] = e.target.value;
                  updateDraft('bulletPoints', bps);
                }} style={{ ...inputStyle, flex: 1 }} placeholder={`Key feature ${i + 1}`} maxLength={500} />
              </div>
            );
          })}
        </Section>

        {/* Description */}
        <Section title="Description" accent="#3b82f6">
          <textarea value={draft.description} onChange={e => { updateDraft('description', e.target.value); setHtmlStripped(false); }} style={{ ...inputStyle, minHeight: 100, resize: 'vertical' }} maxLength={2000} />
          <small style={{ color: 'var(--text-muted)' }}>{draft.description?.length || 0}/2000</small>
          {htmlStripped && (
            <div style={{ marginTop: 6, padding: '6px 10px', background: 'rgba(251,191,36,0.1)', borderRadius: 6, fontSize: 12, color: 'var(--warning)', border: '1px solid rgba(251,191,36,0.3)', display: 'flex', gap: 6, alignItems: 'center' }}>
              ⚠️ Amazon does not allow HTML in descriptions. Tags were automatically removed — only &lt;br/&gt; is permitted. Please review the description above.
            </div>
          )}
        </Section>

        {/* Images — ENH-01: Amazon max 9 images */}
        <Section accent="#3b82f6" title={`Images (${draft.images?.length || 0}/9)`} subtitle="First image = main listing image. Amazon max: 9.">
          <div style={{ display: 'flex', gap: 8, flexWrap: 'wrap' }}>
            {(draft.images || []).map((img, i) => (
              <div key={i} style={{ display: 'flex', flexDirection: 'column', gap: 4, width: 110 }}>
                <div style={{ position: 'relative' }}>
                  <div
                    onDoubleClick={() => { if (i > 0) { const imgs = [...draft.images]; const alts = [...((draft as any).imageAlts || draft.images.map(() => ''))]; [imgs[0], imgs[i]] = [imgs[i], imgs[0]]; [alts[0], alts[i]] = [alts[i], alts[0]]; updateDraft('images', imgs); updateDraft('imageAlts' as any, alts); } }}
                    title={i === 0 ? 'Main image' : 'Double-click to set as main'}
                    style={{ width: 110, height: 80, borderRadius: 6, overflow: 'hidden', border: `2px solid ${i === 0 ? '#ff9900' : 'var(--border)'}`, cursor: i > 0 ? 'pointer' : 'default' }}>
                    <img src={img} alt={(draft as any).imageAlts?.[i] || ''} style={{ width: '100%', height: '100%', objectFit: 'cover' }} onError={e => { (e.target as HTMLImageElement).style.display = 'none'; }} />
                  </div>
                  {i === 0 && <div style={{ position: 'absolute', bottom: -4, left: '50%', transform: 'translateX(-50%)', fontSize: 9, background: '#ff9900', color: '#000', padding: '0 4px', borderRadius: 3, fontWeight: 700 }}>MAIN</div>}
                  <button onClick={() => updateDraft('images', draft.images.filter((_: any, idx: number) => idx !== i))} style={{ position: 'absolute', top: -4, right: -4, width: 18, height: 18, borderRadius: '50%', background: 'var(--danger)', color: '#fff', border: 'none', fontSize: 11, cursor: 'pointer', display: 'flex', alignItems: 'center', justifyContent: 'center' }}>×</button>
                </div>
                {/* FLD-16: Alt text input */}
                <input
                  value={((draft as any).imageAlts || [])[i] || ''}
                  onChange={e => {
                    const alts = [...(((draft as any).imageAlts) || (draft.images || []).map(() => ''))];
                    alts[i] = e.target.value;
                    updateDraft('imageAlts' as any, alts);
                  }}
                  placeholder="Alt text / caption"
                  style={{ fontSize: 10, padding: '3px 5px', borderRadius: 4, border: '1px solid var(--border)', background: 'var(--bg-primary)', color: 'var(--text-primary)', width: '100%' }}
                />
              </div>
            ))}
            {(!draft.images || draft.images.length === 0) && <p style={{ fontSize: 13, color: 'var(--text-muted)' }}>No images found</p>}
            {(draft.images || []).length < 9 && (
              <button onClick={() => { const url = prompt('Enter image URL:'); if (url?.trim()) updateDraft('images', [...(draft.images || []), url.trim()]); }}
                style={{ width: 110, height: 80, borderRadius: 6, border: '2px dashed var(--border)', background: 'transparent', color: 'var(--text-muted)', fontSize: 24, cursor: 'pointer', display: 'flex', alignItems: 'center', justifyContent: 'center' }}>+</button>
            )}
          </div>
          {(draft.images || []).length >= 9 && (
            <p style={{ fontSize: 12, color: 'var(--warning)', marginTop: 4 }}>
              ⚠ Amazon limit reached (9 images max). Remove an image to add another.
            </p>
          )}
          <p style={{ fontSize: 11, color: 'var(--text-muted)', marginTop: 4 }}>
            Amazon allows up to <strong>9 images</strong> (1 main + 8 additional). Double-click any thumbnail to set it as the main image.
          </p>
        </Section>

        {/* ═══ PRICING & IDENTIFIERS ═══ */}
        <div style={{ fontSize: 11, fontWeight: 700, textTransform: 'uppercase', letterSpacing: 1.5, color: '#22c55e', paddingLeft: 4, marginTop: 8 }}>Pricing & Identifiers</div>

        {/* Price & SKU */}
        <Section title="Pricing & Identifiers" accent="#22c55e">
          {(() => {
            const cs = CURRENCY_SYMBOL[draft.currency] || draft.currency;
            return (<>
              <div style={{ display: 'flex', gap: 12, flexWrap: 'wrap' }}>
                <div style={{ flex: 1, minWidth: 140 }}><label style={labelStyle}>SKU *</label><input value={draft.sku} onChange={e => updateDraft('sku', e.target.value)} onBlur={e => checkSKUDuplicate(e.target.value)} style={inputStyle} />
                  {skuDuplicateChannels.length > 0 && (
                    <div style={{ marginTop: 4, padding: '6px 10px', borderRadius: 6, background: 'rgba(245,158,11,0.1)', border: '1px solid rgba(245,158,11,0.4)', fontSize: 12, color: '#b45309' }}>
                      ⚠ SKU already listed on: {skuDuplicateChannels.join(', ')}
                    </div>
                  )}
                </div>
                <div style={{ flex: 1, minWidth: 120 }}><label style={labelStyle}>Price ({cs}) *</label><input value={draft.price} onChange={e => updateDraft('price', e.target.value)} style={inputStyle} type="number" step="0.01" min="0" /></div>
                <div style={{ flex: 1, minWidth: 120 }}><label style={labelStyle}>List Price / RRP ({cs})</label><input value={draft.listPrice || ''} onChange={e => updateDraft('listPrice', e.target.value)} style={inputStyle} type="number" step="0.01" min="0" placeholder="Strikethrough price" /></div>
                <div style={{ flex: 1, minWidth: 100 }}><label style={labelStyle}>Currency</label><select value={draft.currency} onChange={e => updateDraft('currency', e.target.value)} style={inputStyle}><option value="GBP">GBP</option><option value="USD">USD</option><option value="EUR">EUR</option><option value="CAD">CAD</option><option value="AUD">AUD</option><option value="JPY">JPY</option><option value="INR">INR</option><option value="SEK">SEK</option><option value="PLN">PLN</option></select></div>
              </div>

              {/* Sale Price */}
              <div style={{ marginTop: 10 }}>
                <div style={{ fontSize: 12, fontWeight: 600, color: 'var(--text-secondary)', marginBottom: 4 }}>Sale / Promotional Price</div>
                <div style={{ display: 'flex', gap: 12, flexWrap: 'wrap' }}>
                  <div style={{ flex: 1, minWidth: 120 }}><label style={labelStyle}>Sale Price ({cs})</label><input value={draft.salePrice || ''} onChange={e => updateDraft('salePrice', e.target.value)} style={inputStyle} type="number" step="0.01" min="0" placeholder="Leave blank for no sale" /></div>
                  <div style={{ flex: 1, minWidth: 160 }}><label style={labelStyle}>Sale Start</label><input value={draft.salePriceStart || ''} onChange={e => updateDraft('salePriceStart', e.target.value)} style={inputStyle} type="datetime-local" /></div>
                  <div style={{ flex: 1, minWidth: 160 }}><label style={labelStyle}>Sale End</label><input value={draft.salePriceEnd || ''} onChange={e => updateDraft('salePriceEnd', e.target.value)} style={inputStyle} type="datetime-local" /></div>
                </div>
                {draft.salePrice && (!draft.salePriceStart || !draft.salePriceEnd) && (
                  <small style={{ color: 'var(--warning)' }}>Sale price requires both start and end dates</small>
                )}
              </div>

              {/* B2B & Repricing — expandable drawers to reduce clutter */}
              <div style={{ display: 'flex', gap: 8, marginTop: 10, flexWrap: 'wrap' }}>
                <ExpandableDrawer
                  label={draft.b2bPrice ? `✓ B2B Pricing (${cs}${draft.b2bPrice})` : '+ Amazon Business (B2B) Pricing'}
                  active={!!draft.b2bPrice}
                >
                  <div style={{ padding: '10px 0' }}>
                    <div style={{ fontSize: 11, color: 'var(--text-muted)', marginBottom: 8 }}>Set a separate price for Amazon Business buyers and optional quantity discount tiers.</div>
                    <div style={{ display: 'flex', gap: 12, flexWrap: 'wrap', marginBottom: 6 }}>
                      <div style={{ flex: 1, minWidth: 140 }}><label style={labelStyle}>Business Price ({cs})</label><input value={draft.b2bPrice || ''} onChange={e => updateDraft('b2bPrice', e.target.value)} style={inputStyle} type="number" step="0.01" min="0" /></div>
                    </div>
                    {draft.b2bPrice && (
                      <div style={{ fontSize: 11, color: 'var(--text-muted)', marginBottom: 6 }}>Quantity discount tiers — min qty and price per unit for bulk buyers</div>
                    )}
                    {draft.b2bPrice && [1, 2, 3, 4, 5].map(n => {
                      const qtyKey = `b2bTier${n}Qty` as keyof typeof draft;
                      const priceKey = `b2bTier${n}Price` as keyof typeof draft;
                      const prevHasValue = n === 1 || ((draft as any)[`b2bTier${n - 1}Qty`] && (draft as any)[`b2bTier${n - 1}Price`]);
                      if (!prevHasValue) return null;
                      return (
                        <div key={n} style={{ display: 'flex', gap: 8, flexWrap: 'wrap', marginBottom: 4 }}>
                          <div style={{ width: 80 }}><label style={labelStyle}>Tier {n} Qty</label><input value={(draft as any)[qtyKey] || ''} onChange={e => updateDraft(qtyKey as any, e.target.value)} style={inputStyle} type="number" min="2" placeholder={`e.g. ${n * 5}`} /></div>
                          <div style={{ width: 120 }}><label style={labelStyle}>Price ({cs})</label><input value={(draft as any)[priceKey] || ''} onChange={e => updateDraft(priceKey as any, e.target.value)} style={inputStyle} type="number" step="0.01" min="0" /></div>
                        </div>
                      );
                    })}
                  </div>
                </ExpandableDrawer>

                <ExpandableDrawer
                  label={(draft as any).minPrice || (draft as any).maxPrice ? `✓ Repricing (${cs}${(draft as any).minPrice || '?'} – ${cs}${(draft as any).maxPrice || '?'})` : '+ Repricing Bounds'}
                  active={!!(draft as any).minPrice || !!(draft as any).maxPrice}
                >
                  <div style={{ padding: '10px 0' }}>
                    <div style={{ fontSize: 11, color: 'var(--text-muted)', marginBottom: 8 }}>Set floor and ceiling prices to prevent automated repricing rules from going too low or high.</div>
                    <div style={{ display: 'flex', gap: 12, flexWrap: 'wrap' }}>
                      <div style={{ flex: 1, minWidth: 140 }}><label style={labelStyle}>Min Price ({cs})</label><input value={(draft as any).minPrice || ''} onChange={e => updateDraft('minPrice' as any, e.target.value)} style={inputStyle} type="number" step="0.01" min="0" placeholder="Price floor" /></div>
                      <div style={{ flex: 1, minWidth: 140 }}><label style={labelStyle}>Max Price ({cs})</label><input value={(draft as any).maxPrice || ''} onChange={e => updateDraft('maxPrice' as any, e.target.value)} style={inputStyle} type="number" step="0.01" min="0" placeholder="Price ceiling" /></div>
                    </div>
                  </div>
                </ExpandableDrawer>
              </div>

              {/* Identifiers */}
              <div style={{ display: 'flex', gap: 12, marginTop: 10, flexWrap: 'wrap' }}>
                <div style={{ flex: 1, minWidth: 150 }}><label style={labelStyle}>EAN / Barcode</label><input value={draft.ean} onChange={e => updateDraft('ean', e.target.value)} style={inputStyle} placeholder="e.g. 5060000000000" /></div>
                <div style={{ flex: 1, minWidth: 150 }}><label style={labelStyle}>UPC</label><input value={draft.upc} onChange={e => updateDraft('upc', e.target.value)} style={inputStyle} /></div>
                <div style={{ flex: 1, minWidth: 150 }}><label style={labelStyle}>ISBN</label><input value={(draft as any).isbn || ''} onChange={e => updateDraft('isbn' as any, e.target.value)} style={inputStyle} placeholder="For books" /></div>
                <div style={{ flex: 1, minWidth: 150 }}><label style={labelStyle}>ASIN</label><input value={draft.asin} onChange={e => updateDraft('asin', e.target.value)} style={inputStyle} placeholder="e.g. B0XXXXXXXX" disabled={!!draft.asin} /></div>
              </div>
            </>);
          })()}
        </Section>

        {/* Condition & Fulfillment */}
        {/* ═══ LOGISTICS ═══ */}
        <div style={{ fontSize: 11, fontWeight: 700, textTransform: 'uppercase', letterSpacing: 1.5, color: '#6366f1', paddingLeft: 4, marginTop: 8 }}>Logistics & Condition</div>

        <Section title="Condition & Fulfillment" accent="#6366f1">
          <div style={{ display: 'flex', gap: 12, flexWrap: 'wrap' }}>
            <div style={{ flex: 1, minWidth: 200 }}><label style={labelStyle}>Condition</label><select value={draft.condition} onChange={e => updateDraft('condition', e.target.value)} style={inputStyle}>{CONDITION_OPTIONS.map(o => <option key={o.value} value={o.value}>{o.label}</option>)}</select></div>
            <div style={{ flex: 1, minWidth: 200 }}><label style={labelStyle}>Fulfillment</label><select value={draft.fulfillmentChannel} onChange={e => updateDraft('fulfillmentChannel', e.target.value)} style={inputStyle}>{FULFILLMENT_OPTIONS.map(o => <option key={o.value} value={o.value}>{o.label}</option>)}</select></div>
          </div>
          {/* Condition Note — show when not new */}
          {draft.condition && draft.condition !== 'new_new' && (
            <div style={{ marginTop: 8 }}>
              <label style={labelStyle}>Condition Note *</label>
              <textarea
                value={(draft as any).conditionNote || ''}
                onChange={e => updateDraft('conditionNote' as any, e.target.value)}
                style={{ ...inputStyle, minHeight: 60, resize: 'vertical' }}
                maxLength={1000}
                placeholder="Describe the item's condition — e.g. 'Minor shelf wear on box, product is unused and sealed'"
              />
              <small style={{ color: 'var(--text-muted)' }}>Required for non-new items. Visible to buyers on the product page.</small>
            </div>
          )}
          {/* MFN: quantity, handling time, restock */}
          {draft.fulfillmentChannel === 'DEFAULT' && (
            <div style={{ display: 'flex', gap: 12, marginTop: 8, flexWrap: 'wrap' }}>
              <div style={{ flex: 1, minWidth: 100 }}><label style={labelStyle}>Quantity (stock)</label><input value={draft.quantity || ''} onChange={e => updateDraft('quantity', e.target.value)} style={inputStyle} type="number" min="0" placeholder="e.g. 50" /></div>
              <div style={{ flex: 1, minWidth: 120 }}><label style={labelStyle}>Handling Time (days)</label><input value={draft.handlingTime || ''} onChange={e => updateDraft('handlingTime', e.target.value)} style={inputStyle} type="number" min="1" max="30" placeholder="e.g. 2" /></div>
              <div style={{ flex: 1, minWidth: 160 }}><label style={labelStyle}>Restock Date</label><input value={draft.restockDate || ''} onChange={e => updateDraft('restockDate', e.target.value)} style={inputStyle} type="date" /></div>
            </div>
          )}
          {/* FBA warning */}
          {draft.fulfillmentChannel !== 'DEFAULT' && (
            <div style={{ padding: '8px 12px', background: 'rgba(251,191,36,0.08)', borderRadius: 8, fontSize: 12, color: 'var(--warning)', marginTop: 8 }}>
              ⚠️ FBA requires battery info and dangerous goods declaration before Amazon accepts inventory. Fill these in Safety & Compliance below.
            </div>
          )}

          {/* Shipping, Tax, Order Limits, Release Date */}
          <div style={{ display: 'flex', gap: 12, marginTop: 10, flexWrap: 'wrap' }}>
            <div style={{ flex: 1, minWidth: 200 }}>
              <label style={labelStyle}>Shipping Template</label>
              <input value={(draft as any).shippingTemplate || ''} onChange={e => updateDraft('shippingTemplate' as any, e.target.value)} style={inputStyle} placeholder="Template name from Seller Central" />
              <small style={{ color: 'var(--text-muted)' }}>Matches your shipping settings in Seller Central</small>
            </div>
            <div style={{ flex: 1, minWidth: 160 }}>
              <label style={labelStyle}>Product Tax Code</label>
              <select value={(draft as any).productTaxCode || ''} onChange={e => updateDraft('productTaxCode' as any, e.target.value)} style={inputStyle}>
                <option value="">— Select —</option>
                <option value="A_GEN_TAX">Standard Rate (A_GEN_TAX)</option>
                <option value="A_GEN_REDUCED">Reduced Rate (A_GEN_REDUCED)</option>
                <option value="A_GEN_ZERO">Zero Rate (A_GEN_ZERO)</option>
                <option value="A_GEN_EXEMPT">Exempt (A_GEN_EXEMPT)</option>
                <option value="A_GEN_SUPER_REDUCED">Super Reduced (A_GEN_SUPER_REDUCED)</option>
                <option value="A_FOOD_GEN">Food - Standard (A_FOOD_GEN)</option>
                <option value="A_FOOD_REDUCED">Food - Reduced (A_FOOD_REDUCED)</option>
                <option value="A_FOOD_ZERO">Food - Zero (A_FOOD_ZERO)</option>
                <option value="A_CLTH_GEN">Clothing (A_CLTH_GEN)</option>
                <option value="A_BABY_GEN">Baby Products (A_BABY_GEN)</option>
                <option value="A_BOOKS_GEN">Books (A_BOOKS_GEN)</option>
                <option value="A_ELEC_GEN">Electronics (A_ELEC_GEN)</option>
              </select>
            </div>
          </div>
          <div style={{ display: 'flex', gap: 12, marginTop: 8, flexWrap: 'wrap' }}>
            <div style={{ flex: 1, minWidth: 120 }}>
              <label style={labelStyle}>Max Order Quantity</label>
              <input value={(draft as any).maxOrderQty || ''} onChange={e => updateDraft('maxOrderQty' as any, e.target.value)} style={inputStyle} type="number" min="1" placeholder="e.g. 10" />
              <small style={{ color: 'var(--text-muted)' }}>Limit units per order</small>
            </div>
            <div style={{ flex: 1, minWidth: 160 }}>
              <label style={labelStyle}>Release / Pre-Order Date</label>
              <input value={(draft as any).releaseDate || ''} onChange={e => updateDraft('releaseDate' as any, e.target.value)} style={inputStyle} type="date" />
              <small style={{ color: 'var(--text-muted)' }}>For pre-order items — date product becomes available</small>
            </div>
          </div>
        </Section>

        {/* Dimensions & Weight — Item + Package */}
        <Section title="Dimensions & Weight" accent="#8b5cf6">
          <div style={{ fontSize: 12, fontWeight: 600, color: 'var(--text-secondary)', marginBottom: 6 }}>Item Dimensions</div>
          <div style={{ display: 'flex', gap: 12, flexWrap: 'wrap' }}>
            <SmallField label="Length" value={draft.length} onChange={v => updateDraft('length', v)} />
            <SmallField label="Width" value={draft.width} onChange={v => updateDraft('width', v)} />
            <SmallField label="Height" value={draft.height} onChange={v => updateDraft('height', v)} />
            <div style={{ flex: 1, minWidth: 80 }}><label style={labelStyle}>Unit</label><select value={draft.lengthUnit || 'centimeters'} onChange={e => updateDraft('lengthUnit', e.target.value)} style={inputStyle}><option value="centimeters">cm</option><option value="inches">in</option><option value="meters">m</option></select></div>
            <SmallField label="Weight" value={draft.weight} onChange={v => updateDraft('weight', v)} />
            <div style={{ flex: 1, minWidth: 80 }}><label style={labelStyle}>Unit</label><select value={draft.weightUnit || 'kilograms'} onChange={e => updateDraft('weightUnit', e.target.value)} style={inputStyle}><option value="kilograms">kg</option><option value="grams">g</option><option value="pounds">lbs</option><option value="ounces">oz</option></select></div>
          </div>
          <div style={{ fontSize: 12, fontWeight: 600, color: 'var(--text-secondary)', marginBottom: 6, marginTop: 12 }}>Package / Shipping Dimensions</div>
          <div style={{ display: 'flex', gap: 12, flexWrap: 'wrap' }}>
            <SmallField label="Pkg Length" value={draft.pkgLength || ''} onChange={v => updateDraft('pkgLength' as any, v)} />
            <SmallField label="Pkg Width" value={draft.pkgWidth || ''} onChange={v => updateDraft('pkgWidth' as any, v)} />
            <SmallField label="Pkg Height" value={draft.pkgHeight || ''} onChange={v => updateDraft('pkgHeight' as any, v)} />
            <div style={{ flex: 1, minWidth: 80 }}><label style={labelStyle}>Unit</label><select value={draft.pkgLengthUnit || 'centimeters'} onChange={e => updateDraft('pkgLengthUnit' as any, e.target.value)} style={inputStyle}><option value="centimeters">cm</option><option value="inches">in</option><option value="meters">m</option></select></div>
            <SmallField label="Pkg Weight" value={draft.pkgWeight || ''} onChange={v => updateDraft('pkgWeight' as any, v)} />
            <div style={{ flex: 1, minWidth: 80 }}><label style={labelStyle}>Unit</label><select value={draft.pkgWeightUnit || 'kilograms'} onChange={e => updateDraft('pkgWeightUnit' as any, e.target.value)} style={inputStyle}><option value="kilograms">kg</option><option value="grams">g</option><option value="pounds">lbs</option><option value="ounces">oz</option></select></div>
          </div>
        </Section>

        {/* ── PROMOTED SCHEMA FIELDS — in their natural form sections ── */}
        {parsedSchema && (<>
          <div style={{ fontSize: 11, fontWeight: 700, textTransform: 'uppercase', letterSpacing: 1.5, color: '#f59e0b', paddingLeft: 4, marginTop: 8 }}>Product Attributes</div>

          <Section title="Product Details" accent="#f59e0b">
            <p style={{ fontSize: 12, color: 'var(--text-muted)', marginBottom: 10 }}>
              Key attributes for <strong>{draft.productTypeName || draft.productType}</strong>
            </p>
            <div style={{ display: 'flex', gap: 12, flexWrap: 'wrap' }}>
              {['country_of_origin', 'manufacturer', 'color', 'material', 'size', 'model_name', 'model_number'].map(fname => {
                const attr = parsedSchema.attributes.find(a => a.name === fname);
                if (!attr) return null;
                return (
                  <div key={fname} style={{ flex: fname === 'country_of_origin' ? '1 1 100%' : '1 1 200px', minWidth: 160 }}>
                    <SchemaField attr={attr} value={getAttributeValue(fname)} onChange={val => updateAttribute(fname, val)}
                      isConditionallyRequired={conditionallyRequired.has(fname)} isEditable={attr.editable} />
                  </div>
                );
              })}
            </div>
            {/* Browse Nodes — with lookup */}
            <BrowseNodeLookup
              marketplace={mp}
              value={getAttributeValue('recommended_browse_nodes') || ''}
              value2={(draft as any).browseNode2 || ''}
              onChange={val => updateAttribute('recommended_browse_nodes', val)}
              onChange2={val => updateDraft('browseNode2' as any, val)}
            />
          </Section>
        </>)}

        {/* ── CORE PRODUCT ATTRIBUTES (required + active) ── */}
        {coreAttrs.length > 0 && (
          <Section accent="#f59e0b" collapsible title={`Product Attributes (${coreAttrs.length})`}>
            <p style={{ fontSize: 12, color: 'var(--text-muted)', marginBottom: 12 }}>
              Type-specific fields for <strong>{draft.productType}</strong>. Required and active fields shown.
            </p>
            {coreAttrs.map(attr => renderFieldWithChildren(attr))}
          </Section>
        )}

        {/* ── OFFER ATTRIBUTES ── */}
        {offerAttrs.length > 0 && (
          <Section accent="#22c55e" collapsible title={`Offer Details (${offerAttrs.length})`}>
            {offerAttrs.map(attr => renderFieldWithChildren(attr))}
          </Section>
        )}

        {/* ═══ COMPLIANCE ═══ */}
        {(safetySubgroups.size > 0 || (parsedSchema?.gpsrAttributes && parsedSchema.gpsrAttributes.length > 0)) && (
          <div style={{ fontSize: 11, fontWeight: 700, textTransform: 'uppercase', letterSpacing: 1.5, color: '#ef4444', paddingLeft: 4, marginTop: 8 }}>Compliance & Safety</div>
        )}

        {/* ── SAFETY & COMPLIANCE — split into meaningful subgroups ── */}
        {safetySubgroups.size > 0 && (
          <Section title="Safety & Compliance" accent="#ef4444" subtitle="Battery, hazmat, toy safety and compliance declarations">
            {Array.from(safetySubgroups.entries()).map(([subgroupKey, attrs]) => {
              const subgroupMeta = SAFETY_SUBGROUPS[subgroupKey];
              return (
                <div key={subgroupKey} style={{ marginBottom: 16 }}>
                  <div style={{ fontSize: 13, fontWeight: 700, color: 'var(--text-secondary)', marginBottom: 6, borderBottom: '1px solid var(--border)', paddingBottom: 4 }}>
                    {subgroupMeta?.title || subgroupKey}
                  </div>
                  {attrs.map(attr => renderFieldWithChildren(attr))}
                </div>
              );
            })}
          </Section>
        )}

        {/* Optional attributes (collapsed, searchable) */}
        {hiddenOptionalCount > 0 && (
          <OptionalAttributes
            attributes={optionalAttrs}
            getAttributeValue={getAttributeValue}
            updateAttribute={updateAttribute}
          />
        )}

        {/* ── GPSR COMPLIANCE ── */}
        {parsedSchema?.gpsrAttributes && parsedSchema.gpsrAttributes.length > 0 && (
          <Section title="🇪🇺 GPSR Compliance (EU Mandatory)" accent="#2563eb" collapsible>
            <div style={{ padding: '8px 12px', background: 'rgba(59,130,246,0.08)', borderRadius: 8, fontSize: 12, color: 'var(--text-secondary)', marginBottom: 12 }}>
              EU General Product Safety Regulation requires manufacturer info, an EU Responsible Person, and safety documentation for all non-food products sold in EU marketplaces. Non-compliance may result in listing removal.
            </div>
            {parsedSchema.gpsrAttributes.map(attr => (
              <SchemaField
                key={attr.name}
                attr={attr}
                value={getAttributeValue(attr.name)}
                onChange={val => updateAttribute(attr.name, val)}
                isConditionallyRequired={false}
                isEditable={attr.editable}
              />
            ))}
          </Section>
        )}

        {/* Variant Grid */}
        {isVariantProduct && (() => {
          const combinationKeys = variants.length > 0 ? Object.keys(variants[0].combination) : [];
          const allActive = variants.length > 0 && variants.every(v => v.active);
          const someActive = variants.some(v => v.active);
          const currSym = CURRENCY_SYMBOL[draft.currency] || draft.currency;
          return (
          <Section accent="#d946ef" title={`Variants (${variants.length})`}>
            {/* Top controls row */}
            <div style={{ display: 'flex', alignItems: 'center', gap: 16, marginBottom: 12, flexWrap: 'wrap' }}>
              <div style={{ fontSize: 12, color: 'var(--text-muted)' }}>
                Parent SKU: <strong style={{ color: 'var(--text-primary)' }}>{draft.sku || '—'}</strong>
                {draft.variationTheme && <span> · Theme: <strong style={{ color: '#d946ef' }}>{draft.variationTheme}</strong></span>}
              </div>
              <label style={{ display: 'flex', alignItems: 'center', gap: 7, fontSize: 12, color: 'var(--text-primary)', cursor: 'pointer', padding: '5px 10px', background: 'var(--bg-tertiary)', borderRadius: 6, border: '1px solid var(--border)', marginLeft: 'auto' }}>
                <input type="checkbox" checked={!!(draft as any).useMainImagesOnly} onChange={e => updateDraft('useMainImagesOnly', e.target.checked)} />
                <span style={{ fontWeight: 600 }}>Use parent images only</span>
              </label>
            </div>

            {/* Scrollable table — full viewport width trick via negative margin */}
            <div style={{ overflowX: 'auto', margin: '0 -4px' }}>
              <table style={{ width: '100%', borderCollapse: 'collapse', fontSize: 12, tableLayout: 'auto' }}>
                <thead>
                  <tr style={{ borderBottom: '2px solid var(--border)', background: 'var(--bg-tertiary)' }}>
                    {/* Select-all checkbox */}
                    <th style={{ ...thStyle, width: 36, paddingLeft: 10 }}>
                      <input
                        type="checkbox"
                        title="Select / deselect all"
                        checked={allActive}
                        ref={el => { if (el) el.indeterminate = !allActive && someActive; }}
                        onChange={e => setVariants(vs => vs.map(v => ({ ...v, active: e.target.checked })))}
                      />
                    </th>
                    {!(draft as any).useMainImagesOnly && <th style={{ ...thStyle, width: 52 }}>IMG</th>}
                    {combinationKeys.map(k => (
                      <th key={k} style={{ ...thStyle, minWidth: 70, textTransform: 'uppercase' }}>{k}</th>
                    ))}
                    <th style={{ ...thStyle, minWidth: 110 }}>SKU</th>
                    <th style={{ ...thStyle, minWidth: 90 }}>Price ({currSym})</th>
                    <th style={{ ...thStyle, minWidth: 80 }}>Virtual Stock</th>
                    <th style={{ ...thStyle, minWidth: 120 }}>EAN</th>
                    <th style={{ ...thStyle, width: 70 }}>Details</th>
                  </tr>
                </thead>
                <tbody>
                  {variants.map(v => {
                    const hasOverrides = !!(v.weight || v.upc || v.manufacturer || v.title || v.length || v.asin);
                    return (
                      <tr key={v.id} style={{ borderBottom: '1px solid var(--border)', opacity: v.active ? 1 : 0.42, background: v.active ? 'transparent' : 'rgba(0,0,0,0.1)' }}>
                        <td style={{ ...tdStyle, paddingLeft: 10 }}>
                          <input type="checkbox" checked={v.active} onChange={e => updateVariant(v.id, 'active', e.target.checked)} />
                        </td>
                        {!(draft as any).useMainImagesOnly && (
                          <td style={tdStyle}>
                            {v.image
                              ? <div style={{ width: 40, height: 40, borderRadius: 4, overflow: 'hidden', border: '1px solid var(--border)', flexShrink: 0 }}>
                                  <img src={v.image} alt="" style={{ width: '100%', height: '100%', objectFit: 'cover' }} onError={e => { (e.target as HTMLImageElement).style.display = 'none'; }} />
                                </div>
                              : <div style={{ width: 40, height: 40, borderRadius: 4, background: 'var(--bg-secondary)', border: '1px solid var(--border)' }} />
                            }
                          </td>
                        )}
                        {combinationKeys.map(k => (
                          <td key={k} style={{ ...tdStyle, fontWeight: 500, color: 'var(--text-primary)', whiteSpace: 'nowrap' }}>{String(v.combination[k] ?? '—')}</td>
                        ))}
                        <td style={tdStyle}>
                          <input value={v.sku} onChange={e => updateVariant(v.id, 'sku', e.target.value)}
                            style={{ ...inputStyle, padding: '4px 7px', fontSize: 12, width: '100%', minWidth: 90 }} />
                        </td>
                        <td style={tdStyle}>
                          <input value={v.price} onChange={e => updateVariant(v.id, 'price', e.target.value)}
                            style={{ ...inputStyle, padding: '4px 7px', fontSize: 12, width: '100%', minWidth: 70 }} type="number" step="0.01"
                            placeholder={draft.price} />
                        </td>
                        <td style={tdStyle}>
                          <input value={v.stock} onChange={e => updateVariant(v.id, 'stock', e.target.value)}
                            style={{ ...inputStyle, padding: '4px 7px', fontSize: 12, width: '100%', minWidth: 55 }} type="number" />
                        </td>
                        <td style={tdStyle}>
                          <input value={v.ean} onChange={e => updateVariant(v.id, 'ean', e.target.value)}
                            style={{ ...inputStyle, padding: '4px 7px', fontSize: 12, width: '100%', minWidth: 100 }} />
                        </td>
                        <td style={{ ...tdStyle, whiteSpace: 'nowrap' }}>
                          <button
                            onClick={() => setEditingVariant({ ...v })}
                            style={{
                              padding: '4px 9px', borderRadius: 5,
                              border: hasOverrides ? '1px solid #22c55e' : '1px solid var(--border)',
                              background: hasOverrides ? 'rgba(34,197,94,0.1)' : 'var(--bg-secondary)',
                              color: hasOverrides ? '#22c55e' : 'var(--text-muted)',
                              fontSize: 11, cursor: 'pointer', whiteSpace: 'nowrap', display: 'flex', alignItems: 'center', gap: 4
                            }}
                          >
                            ✏ Edit{hasOverrides && <span style={{ width: 6, height: 6, borderRadius: '50%', background: '#22c55e', display: 'inline-block' }} />}
                          </button>
                        </td>
                      </tr>
                    );
                  })}
                </tbody>
              </table>
            </div>

            {/* Footer summary */}
            <div style={{ marginTop: 10, fontSize: 11, color: 'var(--text-muted)', display: 'flex', gap: 16 }}>
              <span>✅ {variants.filter(v => v.active).length} active</span>
              <span>⬜ {variants.filter(v => !v.active).length} inactive</span>
              <span>🖊 {variants.filter(v => !!(v.weight || v.upc || v.manufacturer || v.title || v.length)).length} with overrides</span>
            </div>
          </Section>
          );
        })()}

        {/* Variant Detail Modal */}
        {editingVariant && (
          <VariantDetailModal
            variant={editingVariant}
            parentDraft={draft}
            onSave={(updated, applyToAll) => {
              if (applyToAll) {
                // Apply pricing & identifiers from the edited variant to all variants
                setVariants(vs => vs.map(v => ({
                  ...v,
                  price: updated.price || v.price,
                  listPrice: (updated as any).listPrice || (v as any).listPrice,
                  salePrice: (updated as any).salePrice || (v as any).salePrice,
                  salePriceStart: (updated as any).salePriceStart || (v as any).salePriceStart,
                  salePriceEnd: (updated as any).salePriceEnd || (v as any).salePriceEnd,
                  upc: updated.upc || v.upc,
                  // EAN is variant-specific so only copy if explicitly set
                  ...(updated.id === v.id ? updated : {}),
                })));
              } else {
                setVariants(vs => vs.map(v => v.id === updated.id ? updated : v));
              }
              setEditingVariant(null);
            }}
            onClose={() => setEditingVariant(null)}
          />
        )}
        {/* Debug Panel */}
        {submitResult && <DebugPanel result={submitResult} />}
        {validateResult && <DebugPanel result={validateResult} label="Validation Preview" />}

        {/* ── DEBUG PANELS ── */}
        <Section title="🔍 Debug" collapsible defaultOpen={false}>
          {debugErrors.length > 0 && (
            <div style={{ padding: 10, background: 'rgba(251,191,36,0.1)', borderRadius: 8, marginBottom: 12, fontSize: 12 }}>
              <strong style={{ color: '#f59e0b' }}>Debug Notes:</strong>
              {debugErrors.map((e, i) => <div key={i} style={{ color: '#f59e0b', marginTop: 2 }}>• {e}</div>)}
            </div>
          )}

          {/* Parsed Schema — the actual attributes Amazon expects for this product type */}
          <CollapsibleJson
            title={`Parsed Schema: ${draft?.productType || '(no product type)'} — ${parsedSchema?.attributes?.length || 0} attributes, ${parsedSchema?.conditionalRules?.length || 0} conditional rules, ${parsedSchema?.gpsrAttributes?.length || 0} GPSR`}
            data={parsedSchema}
            emptyText="No parsed schema available — product type may not be set, or schema fetch failed"
            defaultOpen={true}
          />

          {/* Listings collection */}
          <CollapsibleJson
            title={`Listing record for product ${productId}`}
            data={debugListing}
            emptyText="No amazon listing found in Firestore for this product"
            defaultOpen={false}
          />

          {/* Extended Data collection */}
          <CollapsibleJson
            title={`Extended Data for product ${productId}`}
            data={debugExtendedData}
            emptyText="No extended_data found in Firestore for this product"
            defaultOpen={false}
          />

          {/* Full prepare response (collapsed) */}
          <CollapsibleJson
            title="Full Prepare API Response"
            data={prepareResponse}
            emptyText="No response"
            defaultOpen={false}
          />
        </Section>
      </div>
    </div>
  );
}

// ══════════════════════════════════════════════════════════════
// SUB-COMPONENTS
// ══════════════════════════════════════════════════════════════

function Section({ title, children, accent, subtitle, collapsible, defaultOpen = true }: { title: string; children: React.ReactNode; accent?: string; subtitle?: string; collapsible?: boolean; defaultOpen?: boolean }) {
  const [open, setOpen] = useState(defaultOpen);
  return (
    <div style={{ background: 'var(--bg-secondary)', borderRadius: 10, padding: '14px 16px', border: '1px solid var(--border)', position: 'relative', overflow: 'hidden' }}>
      {accent && <div style={{ position: 'absolute', top: 0, left: 0, width: 3, height: '100%', background: accent, borderRadius: '10px 0 0 10px' }} />}
      <div
        style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', cursor: collapsible ? 'pointer' : 'default', marginBottom: open ? 10 : 0 }}
        onClick={() => collapsible && setOpen(!open)}
      >
        <div>
          <h3 style={{ fontSize: 14, fontWeight: 700, margin: 0, color: 'var(--text-primary)' }}>{title}</h3>
          {subtitle && <p style={{ fontSize: 11, color: 'var(--text-muted)', margin: '2px 0 0' }}>{subtitle}</p>}
        </div>
        {collapsible && <span style={{ fontSize: 12, color: 'var(--text-muted)', userSelect: 'none' }}>{open ? '▼' : '▶'}</span>}
      </div>
      {open && children}
    </div>
  );
}

function SmallField({ label, value, onChange }: { label: string; value: string; onChange: (v: string) => void }) {
  return <div style={{ flex: 1, minWidth: 120 }}><label style={labelStyle}>{label}</label><input value={value || ''} onChange={e => onChange(e.target.value)} style={inputStyle} /></div>;
}

// ── Expandable drawer — compact button that reveals a panel ──
function ExpandableDrawer({ label, active, children }: { label: string; active: boolean; children: React.ReactNode }) {
  const [open, setOpen] = useState(false);
  return (
    <div style={{ flex: 1, minWidth: 200 }}>
      <button
        onClick={() => setOpen(!open)}
        style={{
          width: '100%', padding: '8px 12px', border: `1px dashed ${active ? '#22c55e' : 'var(--border)'}`,
          borderRadius: 8, background: active ? 'rgba(34,197,94,0.05)' : 'transparent',
          color: active ? '#22c55e' : 'var(--text-muted)', fontSize: 12, fontWeight: 600,
          cursor: 'pointer', textAlign: 'left', display: 'flex', justifyContent: 'space-between', alignItems: 'center',
        }}
      >
        <span>{label}</span>
        <span style={{ fontSize: 10 }}>{open ? '▼' : '▶'}</span>
      </button>
      {open && (
        <div style={{ border: '1px solid var(--border)', borderTop: 'none', borderRadius: '0 0 8px 8px', padding: '4px 12px 8px', background: 'var(--bg-secondary)' }}>
          {children}
        </div>
      )}
    </div>
  );
}

// ── Browse Node Lookup — searchable with common categories ──
function BrowseNodeLookup({ marketplace, value, value2, onChange, onChange2 }: {
  marketplace: string; value: string; value2: string;
  onChange: (v: string) => void; onChange2: (v: string) => void;
}) {
  const [search, setSearch] = useState('');
  const [showLookup, setShowLookup] = useState(false);

  // Common browse nodes by marketplace — top-level categories
  const COMMON_NODES: Record<string, { id: string; label: string }[]> = {
    'A1F83G8C2ARO7P': [ // UK
      { id: '340831031', label: 'Electronics' }, { id: '344155031', label: 'Computers' },
      { id: '11052671', label: 'Garden & Outdoors' }, { id: '2826600031', label: 'Beauty' },
      { id: '77198031', label: 'Toys & Games' }, { id: '560800', label: 'Books' },
      { id: '11052681', label: 'DIY & Tools' }, { id: '1769516031', label: 'Kitchen & Home' },
      { id: '83451031', label: 'Sports & Outdoors' }, { id: '560798', label: 'Music' },
      { id: '1730530031', label: 'Baby Products' }, { id: '344155031', label: 'PC Accessories' },
      { id: '216956031', label: 'Clothing' }, { id: '84603031', label: 'Pet Supplies' },
      { id: '1642231031', label: 'Automotive' }, { id: '11052641', label: 'Health & Personal Care' },
      { id: '3147171', label: 'Grocery' }, { id: '117332031', label: 'Stationery & Office' },
      { id: '10529459031', label: 'Handmade' }, { id: '5866055031', label: 'Gift Cards' },
    ],
    'ATVPDKIKX0DER': [ // US
      { id: '172282', label: 'Electronics' }, { id: '541966', label: 'Computers' },
      { id: '2972638011', label: 'Toys & Games' }, { id: '3760901', label: 'Sports & Outdoors' },
      { id: '1055398', label: 'Home & Kitchen' }, { id: '3760911', label: 'Beauty' },
      { id: '165793011', label: 'Baby Products' }, { id: '7141123011', label: 'Clothing' },
      { id: '283155', label: 'Books' }, { id: '2619525011', label: 'Grocery' },
      { id: '2619533011', label: 'Pet Supplies' }, { id: '228013', label: 'Tools & Home' },
      { id: '3375251', label: 'Health & Household' }, { id: '15684181', label: 'Automotive' },
      { id: '1084128', label: 'Office Products' }, { id: '229534', label: 'Garden & Outdoor' },
    ],
  };

  const nodes = COMMON_NODES[marketplace] || COMMON_NODES['A1F83G8C2ARO7P'] || [];
  const filtered = search ? nodes.filter(n => n.label.toLowerCase().includes(search.toLowerCase()) || n.id.includes(search)) : nodes;

  return (
    <div style={{ marginTop: 8 }}>
      <div style={{ display: 'flex', alignItems: 'center', gap: 8, marginBottom: 4 }}>
        <div style={{ fontSize: 12, fontWeight: 600, color: 'var(--text-secondary)' }}>
          Browse Nodes <span style={{ color: 'var(--danger)' }}>*</span>
        </div>
        <button onClick={() => setShowLookup(!showLookup)} style={{ fontSize: 11, color: 'var(--primary-light)', background: 'none', border: 'none', cursor: 'pointer', textDecoration: 'underline' }}>
          {showLookup ? 'Hide lookup' : '🔍 Browse categories'}
        </button>
      </div>

      {showLookup && (
        <div style={{ border: '1px solid var(--border)', borderRadius: 8, padding: 10, marginBottom: 8, maxHeight: 220, overflow: 'auto', background: 'var(--bg-primary)' }}>
          <input
            value={search} onChange={e => setSearch(e.target.value)}
            style={{ ...inputStyle, marginBottom: 6, fontSize: 12 }}
            placeholder="Search categories..."
            autoFocus
          />
          <div style={{ display: 'flex', flexWrap: 'wrap', gap: 4 }}>
            {filtered.map(n => (
              <button
                key={n.id}
                onClick={() => {
                  if (!value) { onChange(n.id); }
                  else if (!value2) { onChange2(n.id); }
                  else { onChange(n.id); } // replace primary
                }}
                style={{
                  padding: '4px 10px', borderRadius: 6, fontSize: 11, cursor: 'pointer',
                  border: (value === n.id || value2 === n.id) ? '1px solid #22c55e' : '1px solid var(--border)',
                  background: (value === n.id || value2 === n.id) ? 'rgba(34,197,94,0.1)' : 'transparent',
                  color: 'var(--text-primary)',
                }}
              >
                {n.label} <span style={{ color: 'var(--text-muted)', fontSize: 10 }}>({n.id})</span>
              </button>
            ))}
          </div>
          <div style={{ fontSize: 10, color: 'var(--text-muted)', marginTop: 4 }}>
            Click a category to set it as primary or secondary browse node. For sub-categories, enter the specific node ID manually.
          </div>
        </div>
      )}

      <div style={{ display: 'flex', gap: 12, flexWrap: 'wrap' }}>
        <div style={{ flex: 1, minWidth: 200 }}>
          <label style={labelStyle}>Primary Browse Node *</label>
          <input value={value || ''} onChange={e => onChange(e.target.value)} style={inputStyle} placeholder="e.g. 165793011" maxLength={15} />
        </div>
        <div style={{ flex: 1, minWidth: 200 }}>
          <label style={labelStyle}>Secondary Browse Node</label>
          <input value={value2 || ''} onChange={e => onChange2(e.target.value)} style={inputStyle} placeholder="e.g. 166024011" maxLength={15} />
        </div>
      </div>
      <small style={{ color: 'var(--text-muted)', display: 'block', marginTop: 2 }}>
        Browse nodes determine which Amazon categories your product appears in. Use the lookup above or enter IDs from Seller Central.
      </small>
    </div>
  );
}

// ── Schema-driven attribute field ──
function SchemaField({ attr, value, onChange, isConditionallyRequired, isEditable }: {
  attr: ParsedAttribute; value: string; onChange: (v: string) => void;
  isConditionallyRequired: boolean; isEditable: boolean;
}) {
  const isRequired = attr.required || isConditionallyRequired;
  const badge = isConditionallyRequired && !attr.required ? (
    <span style={{ fontSize: 10, padding: '1px 5px', borderRadius: 3, background: 'rgba(251,191,36,0.15)', color: '#f59e0b', marginLeft: 6 }}>conditional</span>
  ) : null;

  const enumLabels = attr.enumNames && attr.enumNames.length === attr.enumValues?.length ? attr.enumNames : null;

  if (attr.enumValues && attr.enumValues.length > 0) {
    return (
      <div style={{ marginBottom: 8, borderLeft: isRequired ? '3px solid #ff9900' : '3px solid transparent', paddingLeft: isRequired ? 8 : 0 }}>
        <label style={labelStyle}>{attr.title}{isRequired && <span style={{ color: 'var(--danger)' }}> *</span>}{badge}</label>
        <select value={value} onChange={e => onChange(e.target.value)} style={inputStyle} disabled={!isEditable}>
          <option value="">-- Select --</option>
          {attr.enumValues.map((ev, i) => <option key={ev} value={ev}>{enumLabels ? enumLabels[i] : ev}</option>)}
        </select>
        {attr.description && <small style={{ color: 'var(--text-muted)', display: 'block', marginTop: 2 }}>{attr.description}</small>}
      </div>
    );
  }
  if (attr.type === 'number' || attr.type === 'integer') {
    return (
      <div style={{ marginBottom: 8, borderLeft: isRequired ? '3px solid #ff9900' : '3px solid transparent', paddingLeft: isRequired ? 8 : 0 }}>
        <label style={labelStyle}>{attr.title}{isRequired && <span style={{ color: 'var(--danger)' }}> *</span>}{badge}</label>
        <input value={value} onChange={e => onChange(e.target.value)} style={inputStyle} type="number" disabled={!isEditable}
          placeholder={attr.examples?.[0] || ''} min={attr.minimum} max={attr.maximum} />
        {attr.description && <small style={{ color: 'var(--text-muted)', display: 'block', marginTop: 2 }}>{attr.description}</small>}
      </div>
    );
  }
  if (attr.type === 'boolean') {
    return (
      <div style={{ marginBottom: 8, display: 'flex', alignItems: 'center', gap: 8, borderLeft: isRequired ? '3px solid #ff9900' : '3px solid transparent', paddingLeft: isRequired ? 8 : 0 }}>
        <input type="checkbox" checked={value === 'true'} onChange={e => onChange(e.target.checked ? 'true' : 'false')} disabled={!isEditable} />
        <label style={{ ...labelStyle, margin: 0 }}>{attr.title}{isRequired && <span style={{ color: 'var(--danger)' }}> *</span>}{badge}</label>
        {attr.description && <small style={{ color: 'var(--text-muted)', marginLeft: 8 }}>{attr.description}</small>}
      </div>
    );
  }
  // Default: text input
  return (
    <div style={{ marginBottom: 8, borderLeft: isRequired ? '3px solid #ff9900' : '3px solid transparent', paddingLeft: isRequired ? 8 : 0 }}>
      <label style={labelStyle}>{attr.title}{isRequired && <span style={{ color: 'var(--danger)' }}> *</span>}{badge}</label>
      <input value={value} onChange={e => onChange(e.target.value)} style={inputStyle} disabled={!isEditable}
        placeholder={attr.examples?.[0] || ''} maxLength={attr.maxLength || undefined} />
      {(attr.description || attr.maxLength) && (
        <small style={{ color: 'var(--text-muted)', display: 'block', marginTop: 2 }}>
          {attr.description}{attr.maxLength ? ` (max ${attr.maxLength} chars)` : ''}
        </small>
      )}
    </div>
  );
}

// ── Optional attributes (collapsed, searchable) ──
function OptionalAttributes({ attributes, getAttributeValue, updateAttribute }: {
  attributes: ParsedAttribute[]; getAttributeValue: (k: string) => string; updateAttribute: (k: string, v: string) => void;
}) {
  const [expanded, setExpanded] = useState(false);
  const [search, setSearch] = useState('');

  const filtered = search
    ? attributes.filter(a => a.title.toLowerCase().includes(search.toLowerCase()) || a.name.toLowerCase().includes(search.toLowerCase()) || (a.description || '').toLowerCase().includes(search.toLowerCase()))
    : attributes;

  // Group by groupTitle for better organization
  const grouped = new Map<string, ParsedAttribute[]>();
  for (const attr of filtered) {
    const g = attr.groupTitle || 'Other';
    if (!grouped.has(g)) grouped.set(g, []);
    grouped.get(g)!.push(attr);
  }

  return (
    <div style={{ marginTop: 8 }}>
      <button onClick={() => setExpanded(!expanded)} style={{ background: 'none', border: 'none', cursor: 'pointer', fontSize: 13, fontWeight: 600, color: 'var(--text-secondary)', display: 'flex', alignItems: 'center', gap: 6, padding: '8px 0' }}>
        <span style={{ transform: expanded ? 'rotate(90deg)' : 'none', transition: 'transform 0.2s', display: 'inline-block' }}>▶</span>
        Optional Attributes ({attributes.length} available)
      </button>
      {expanded && (
        <div style={{ marginTop: 8, borderTop: '1px solid var(--border)', paddingTop: 10 }}>
          <input
            value={search} onChange={e => setSearch(e.target.value)}
            placeholder="Search optional fields (e.g. scale, scent, power)..."
            style={{ width: '100%', padding: '8px 12px', borderRadius: 6, border: '1px solid var(--border)', background: 'var(--bg-secondary)', color: 'var(--text-primary)', fontSize: 13, marginBottom: 12 }}
          />
          {filtered.length === 0 && <p style={{ fontSize: 12, color: 'var(--text-muted)' }}>No matching fields</p>}
          {Array.from(grouped.entries()).map(([groupTitle, attrs]) => (
            <div key={groupTitle} style={{ marginBottom: 12 }}>
              <div style={{ fontSize: 11, fontWeight: 700, color: 'var(--text-muted)', textTransform: 'uppercase', letterSpacing: '0.5px', marginBottom: 4 }}>{groupTitle}</div>
              {attrs.map(attr => <SchemaField key={attr.name} attr={attr} value={getAttributeValue(attr.name)} onChange={val => updateAttribute(attr.name, val)} isConditionallyRequired={false} isEditable={attr.editable} />)}
            </div>
          ))}
        </div>
      )}
    </div>
  );
}

// ── Restrictions banner ──
function RestrictionsBanner({ restrictions }: { restrictions: Restriction[] }) {
  return (
    <div style={{ padding: 16, background: 'rgba(239,68,68,0.08)', border: '1px solid rgba(239,68,68,0.3)', borderRadius: 8, marginBottom: 16 }}>
      <div style={{ fontWeight: 700, fontSize: 14, color: 'var(--danger)', marginBottom: 8 }}>⚠️ Listing Restrictions</div>
      {restrictions.filter(r => r.reasons.length > 0).map((r, ri) => (
        <div key={ri} style={{ marginBottom: 8 }}>
          {r.conditionType && <span style={{ fontSize: 11, color: 'var(--text-muted)', marginRight: 6 }}>[{r.conditionType}]</span>}
          {r.reasons.map((reason, i) => (
            <div key={i} style={{ fontSize: 13, color: 'var(--danger)', marginBottom: 4 }}>
              <strong>{reason.reasonCode}</strong>: {reason.message}
              {reason.links?.map((link, li) => (
                <a key={li} href={link.resource} target="_blank" rel="noreferrer" style={{ marginLeft: 8, color: '#ff9900', fontSize: 12 }}>
                  {link.title || 'Apply for Approval'} →
                </a>
              ))}
            </div>
          ))}
        </div>
      ))}
      <p style={{ fontSize: 12, color: 'var(--text-muted)', margin: '8px 0 0' }}>You must resolve these restrictions before submitting. Submit is disabled.</p>
    </div>
  );
}

// ── Validation preview banner ──
function ValidationBanner({ result }: { result: AmazonValidateResponse }) {
  if (!result) return null;
  const bgColor = result.ok ? 'rgba(34,197,94,0.08)' : result.errorCount ? 'rgba(239,68,68,0.08)' : 'rgba(251,191,36,0.08)';
  const borderColor = result.ok ? 'rgba(34,197,94,0.3)' : result.errorCount ? 'rgba(239,68,68,0.3)' : 'rgba(251,191,36,0.3)';
  return (
    <div style={{ padding: 12, background: bgColor, border: `1px solid ${borderColor}`, borderRadius: 8, marginBottom: 16 }}>
      <div style={{ fontWeight: 700, fontSize: 13, marginBottom: 4, color: result.ok ? 'var(--success)' : result.errorCount ? 'var(--danger)' : 'var(--warning)' }}>
        {result.ok ? '✅ Validation Passed' : `🔍 Validation: ${result.errorCount || 0} error(s), ${result.warningCount || 0} warning(s)`}
      </div>
      <IssuesList issues={result.issues} />
      {result.ok && <p style={{ fontSize: 12, color: 'var(--text-muted)', margin: '4px 0 0' }}>Your listing should be accepted by Amazon. Click Submit to publish.</p>}
    </div>
  );
}

// ── Issues list (shared by submit + validate) ──
function IssuesList({ issues }: { issues?: { code?: string; message: string; severity: string; attributeNames?: string[] }[] }) {
  if (!issues || issues.length === 0) return null;
  return (
    <div style={{ marginTop: 8 }}>
      {issues.map((iss, i) => (
        <div key={i} style={{ padding: '6px 10px', background: iss.severity === 'ERROR' ? 'var(--danger-glow)' : 'var(--warning-glow)', borderRadius: 6, fontSize: 12, marginBottom: 4, color: iss.severity === 'ERROR' ? 'var(--danger)' : 'var(--warning)' }}>
          <strong>[{iss.severity}]</strong> {iss.message}
          {iss.attributeNames?.length ? <span style={{ color: 'var(--text-muted)', marginLeft: 4 }}>({iss.attributeNames.join(', ')})</span> : null}
        </div>
      ))}
    </div>
  );
}

// ── Debug Panel ──
function DebugPanel({ result, label }: { result: { request?: any; response?: any; ok?: boolean; childResults?: any[] }; label?: string }) {
  const [expanded, setExpanded] = useState(false);
  if (!result.request && !result.response) return null;
  return (
    <div style={{ marginTop: 20, borderTop: '1px solid var(--border)', paddingTop: 12 }}>
      <button onClick={() => setExpanded(!expanded)} style={{ background: 'none', border: 'none', cursor: 'pointer', fontSize: 13, fontWeight: 600, color: 'var(--text-secondary)', display: 'flex', alignItems: 'center', gap: 6, padding: 0 }}>
        <span style={{ transform: expanded ? 'rotate(90deg)' : 'none', transition: 'transform 0.2s', display: 'inline-block' }}>▶</span>
        {label || 'Debug Panel'} (Request / Response JSON)
      </button>
      {expanded && (
        <div style={{ marginTop: 8 }}>
          {result.request && <><div style={{ fontSize: 12, fontWeight: 600, color: 'var(--text-secondary)', marginBottom: 4 }}>REQUEST →</div><pre style={preStyle}>{JSON.stringify(result.request, null, 2)}</pre></>}
          {result.response && <><div style={{ fontSize: 12, fontWeight: 600, color: 'var(--text-secondary)', marginBottom: 4, marginTop: 12 }}>← RESPONSE</div><pre style={{ ...preStyle, color: result.ok ? '#a6e3a1' : '#f38ba8' }}>{JSON.stringify(result.response, null, 2)}</pre></>}
          {result.childResults && result.childResults.length > 0 && <><div style={{ fontSize: 12, fontWeight: 600, color: 'var(--text-secondary)', marginBottom: 4, marginTop: 12 }}>CHILD LISTINGS ({result.childResults.length})</div><pre style={preStyle}>{JSON.stringify(result.childResults, null, 2)}</pre></>}
        </div>
      )}
    </div>
  );
}

// ── Collapsible JSON debug box ──
function CollapsibleJson({ title, data, emptyText, defaultOpen = false }: {
  title: string; data: any; emptyText: string; defaultOpen?: boolean;
}) {
  const [expanded, setExpanded] = useState(defaultOpen);
  return (
    <div style={{ marginBottom: 16, border: '1px solid var(--border)', borderRadius: 8, overflow: 'hidden' }}>
      <button onClick={() => setExpanded(!expanded)} style={{
        width: '100%', textAlign: 'left', padding: '10px 14px', background: 'var(--bg-secondary)', border: 'none', cursor: 'pointer',
        fontSize: 13, fontWeight: 600, color: 'var(--text-primary)', display: 'flex', alignItems: 'center', gap: 8,
      }}>
        <span style={{ transform: expanded ? 'rotate(90deg)' : 'none', transition: 'transform 0.2s', display: 'inline-block', fontSize: 10 }}>▶</span>
        {title}
        {data ? (
          <span style={{ fontSize: 11, color: '#22c55e', fontWeight: 400, marginLeft: 'auto' }}>
            {Array.isArray(data) ? `${data.length} record(s)` : 'has data'}
          </span>
        ) : (
          <span style={{ fontSize: 11, color: '#f59e0b', fontWeight: 400, marginLeft: 'auto' }}>empty</span>
        )}
      </button>
      {expanded && (
        <div style={{ padding: 12 }}>
          {data ? (
            <pre style={{ ...preStyle, maxHeight: 600, margin: 0 }}>
              {JSON.stringify(data, null, 2)}
            </pre>
          ) : (
            <p style={{ fontSize: 13, color: 'var(--text-muted)', margin: 0 }}>{emptyText}</p>
          )}
        </div>
      )}
    </div>
  );
}

// ══════════════════════════════════════════════════════════════
// STYLES
// ══════════════════════════════════════════════════════════════

const inputStyle: React.CSSProperties = { width: '100%', padding: '10px 14px', borderRadius: 8, border: '1px solid var(--border)', background: 'var(--bg-primary)', color: 'var(--text-primary)', fontSize: 14, outline: 'none', boxSizing: 'border-box' };
const labelStyle: React.CSSProperties = { display: 'block', fontSize: 12, fontWeight: 600, color: 'var(--text-secondary)', marginBottom: 4 };
const primaryBtnStyle: React.CSSProperties = { padding: '10px 24px', borderRadius: 8, border: 'none', background: '#ff9900', color: '#000', fontWeight: 700, fontSize: 14, cursor: 'pointer' };
const secondaryBtnStyle: React.CSSProperties = { padding: '10px 24px', borderRadius: 8, border: '1px solid var(--border)', background: 'var(--bg-secondary)', color: 'var(--text-primary)', fontWeight: 600, fontSize: 14, cursor: 'pointer' };
const linkBtnStyle: React.CSSProperties = { background: 'none', border: 'none', color: '#ff9900', cursor: 'pointer', fontSize: 13, fontWeight: 600, padding: '4px 0' };
const backBtnStyle: React.CSSProperties = { background: 'var(--bg-secondary)', border: '1px solid var(--border)', borderRadius: 8, padding: '6px 14px', fontSize: 13, cursor: 'pointer', color: 'var(--text-secondary)' };
const thStyle: React.CSSProperties = { textAlign: 'left', padding: '8px 10px', fontSize: 11, fontWeight: 700, color: 'var(--text-muted)', textTransform: 'uppercase', letterSpacing: '0.5px', whiteSpace: 'nowrap' };
const tdStyle: React.CSSProperties = { padding: '8px 10px', verticalAlign: 'middle' };
const preStyle: React.CSSProperties = { background: '#1e1e2e', color: '#cdd6f4', padding: 16, borderRadius: 8, fontSize: 11, lineHeight: 1.5, overflow: 'auto', maxHeight: 500, fontFamily: '"Fira Code","JetBrains Mono","Consolas",monospace', border: '1px solid #313244', whiteSpace: 'pre-wrap', wordBreak: 'break-word' };

// ══════════════════════════════════════════════════════════════
// VARIANT DETAIL MODAL
// ══════════════════════════════════════════════════════════════

// ══════════════════════════════════════════════════════════════
// VARIANT DETAIL MODAL — mirrors main form sections exactly
// ══════════════════════════════════════════════════════════════

interface VariantDetailModalProps {
  variant: AmazonVariantDraft;
  parentDraft: AmazonDraft;
  onSave: (updated: AmazonVariantDraft, applyToAll: boolean) => void;
  onClose: () => void;
}

function VariantDetailModal({ variant, parentDraft, onSave, onClose }: VariantDetailModalProps) {
  const [local, setLocal] = React.useState<AmazonVariantDraft>({ ...variant });
  const [applyToAll, setApplyToAll] = React.useState(false);
  const set = (field: keyof AmazonVariantDraft, value: string) =>
    setLocal(v => ({ ...v, [field]: value }));

  const variantLabel = Object.entries(variant.combination)
    .filter(([, v]) => v)
    .map(([k, v]) => `${k}: ${v}`).join(' / ');

  const cs = CURRENCY_SYMBOL[parentDraft.currency] || parentDraft.currency;

  // Styles mirroring the main form exactly
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
    marginBottom: 16, padding: '16px 18px',
    background: 'var(--bg-tertiary, #1a1e28)',
    border: '1px solid var(--border)',
    borderRadius: 10,
  };
  const mSectionTitle: React.CSSProperties = {
    fontSize: 11, fontWeight: 700, color: 'var(--text-muted)',
    textTransform: 'uppercase' as const, letterSpacing: '0.08em',
    marginBottom: 12, paddingBottom: 8,
    borderBottom: '1px solid var(--border)',
  };
  const mRow: React.CSSProperties = { display: 'flex', gap: 12, flexWrap: 'wrap' as const, marginBottom: 10 };
  const mFlex = (min: number): React.CSSProperties => ({ flex: 1, minWidth: min });
  const mSmall: React.CSSProperties = { color: 'var(--text-muted)', fontSize: 12, marginTop: 3, display: 'block' };

  return (
    <div style={{
      position: 'fixed', inset: 0, zIndex: 1000,
      background: 'rgba(0,0,0,0.72)',
      display: 'flex', alignItems: 'center', justifyContent: 'center',
      padding: 20,
    }} onClick={e => { if (e.target === e.currentTarget) onClose(); }}>
      <div style={{
        background: 'var(--bg-secondary, #13161e)',
        border: '1px solid var(--border)',
        borderRadius: 14,
        width: '100%', maxWidth: 900,
        maxHeight: '92vh', overflowY: 'auto',
        padding: '24px 28px',
        boxShadow: '0 28px 72px rgba(0,0,0,0.55)',
      }}>
        {/* ── Header ── */}
        <div style={{ display: 'flex', alignItems: 'flex-start', justifyContent: 'space-between', marginBottom: 20 }}>
          <div>
            <div style={{ fontSize: 16, fontWeight: 700, color: 'var(--text-primary)', marginBottom: 2 }}>
              Edit Variant
            </div>
            <div style={{ fontSize: 13, color: '#d946ef', fontWeight: 600 }}>{variantLabel}</div>
            <div style={{ fontSize: 11, color: 'var(--text-muted)', marginTop: 3 }}>
              Blank fields inherit from the parent listing · SKU: <span style={{ fontFamily: 'monospace' }}>{variant.sku || '—'}</span>
            </div>
          </div>
          <button onClick={onClose} style={{ background: 'none', border: 'none', color: 'var(--text-muted)', fontSize: 22, cursor: 'pointer', lineHeight: 1, padding: '0 4px' }}>✕</button>
        </div>

        {/* ════════ PRICING & IDENTIFIERS ════════ */}
        <div style={{ fontSize: 11, fontWeight: 700, textTransform: 'uppercase', letterSpacing: 1.5, color: '#22c55e', marginBottom: 6 }}>Pricing &amp; Identifiers</div>
        <div style={{ ...mSection, borderLeftColor: '#22c55e', borderLeftWidth: 3 }}>

          {/* Apply to all banner */}
          <label style={{ display: 'flex', alignItems: 'center', gap: 8, padding: '7px 10px', background: applyToAll ? 'rgba(34,197,94,0.1)' : 'var(--bg-secondary)', border: `1px solid ${applyToAll ? '#22c55e' : 'var(--border)'}`, borderRadius: 7, marginBottom: 12, cursor: 'pointer', fontSize: 13, color: applyToAll ? '#22c55e' : 'var(--text-muted)' }}>
            <input type="checkbox" checked={applyToAll} onChange={e => setApplyToAll(e.target.checked)} style={{ accentColor: '#22c55e' }} />
            <span style={{ fontWeight: 600 }}>Apply pricing &amp; identifiers to all variants</span>
            <span style={{ fontSize: 11, fontWeight: 400, color: 'var(--text-muted)' }}>— overwrites price, list price, sale, EAN, UPC on all rows</span>
          </label>

          {/* Row 1: SKU / Price / List Price / Currency */}
          <div style={mRow}>
            <div style={mFlex(130)}><label style={mLabel}>SKU *</label><input style={mInput} value={local.sku} onChange={e => set('sku', e.target.value)} /></div>
            <div style={mFlex(110)}><label style={mLabel}>Price ({cs}) *</label><input style={mInput} type="number" step="0.01" value={local.price} onChange={e => set('price', e.target.value)} placeholder={parentDraft.price} /></div>
            <div style={mFlex(110)}><label style={mLabel}>List Price / RRP ({cs})</label><input style={mInput} type="number" step="0.01" value={(local as any).listPrice || ''} onChange={e => set('listPrice' as any, e.target.value)} placeholder="Strikethrough price" /></div>
            <div style={mFlex(90)}><label style={mLabel}>Currency</label>
              <select style={{ ...mInput, appearance: 'auto' }} value={(local as any).currency || parentDraft.currency} onChange={e => set('currency' as any, e.target.value)}>
                {['GBP','USD','EUR','CAD','AUD','JPY','INR','SEK','PLN'].map(c => <option key={c} value={c}>{c}</option>)}
              </select>
            </div>
          </div>

          {/* Row 2: Sale Price / Start / End */}
          <div style={{ fontSize: 12, fontWeight: 600, color: 'var(--text-secondary)', marginBottom: 6 }}>Sale / Promotional Price</div>
          <div style={mRow}>
            <div style={mFlex(110)}><label style={mLabel}>Sale Price ({cs})</label><input style={mInput} type="number" step="0.01" value={(local as any).salePrice || ''} onChange={e => set('salePrice' as any, e.target.value)} placeholder="Leave blank for no sale" /></div>
            <div style={mFlex(150)}><label style={mLabel}>Sale Start</label><input style={mInput} type="datetime-local" value={(local as any).salePriceStart || ''} onChange={e => set('salePriceStart' as any, e.target.value)} /></div>
            <div style={mFlex(150)}><label style={mLabel}>Sale End</label><input style={mInput} type="datetime-local" value={(local as any).salePriceEnd || ''} onChange={e => set('salePriceEnd' as any, e.target.value)} /></div>
          </div>

          {/* Row 3: Identifiers */}
          <div style={mRow}>
            <div style={mFlex(140)}><label style={mLabel}>EAN / Barcode</label><input style={mInput} value={local.ean} onChange={e => set('ean', e.target.value)} placeholder={parentDraft.ean || 'e.g. 5060000000000'} /></div>
            <div style={mFlex(140)}><label style={mLabel}>UPC</label><input style={mInput} value={local.upc || ''} onChange={e => set('upc', e.target.value)} placeholder={parentDraft.upc} /></div>
            <div style={mFlex(140)}><label style={mLabel}>ISBN</label><input style={mInput} value={(local as any).isbn || ''} onChange={e => set('isbn' as any, e.target.value)} placeholder="For books" /></div>
            <div style={mFlex(140)}><label style={mLabel}>ASIN</label><input style={mInput} value={local.asin || ''} onChange={e => set('asin', e.target.value)} placeholder={parentDraft.asin || 'e.g. B0XXXXXXXX'} /></div>
          </div>
        </div>

        {/* ════════ DIMENSIONS & WEIGHT ════════ */}
        <div style={{ fontSize: 11, fontWeight: 700, textTransform: 'uppercase', letterSpacing: 1.5, color: '#8b5cf6', marginBottom: 6 }}>Dimensions &amp; Weight</div>
        <div style={{ ...mSection, borderLeftColor: '#8b5cf6', borderLeftWidth: 3 }}>
          <div style={{ fontSize: 12, fontWeight: 600, color: 'var(--text-secondary)', marginBottom: 6 }}>Item Dimensions</div>
          <div style={mRow}>
            <div style={mFlex(80)}><label style={mLabel}>Length</label><input style={mInput} type="number" step="0.1" value={local.length || ''} onChange={e => set('length', e.target.value)} placeholder={parentDraft.length || '—'} /></div>
            <div style={mFlex(80)}><label style={mLabel}>Width</label><input style={mInput} type="number" step="0.1" value={local.width || ''} onChange={e => set('width', e.target.value)} placeholder={parentDraft.width || '—'} /></div>
            <div style={mFlex(80)}><label style={mLabel}>Height</label><input style={mInput} type="number" step="0.1" value={local.height || ''} onChange={e => set('height', e.target.value)} placeholder={parentDraft.height || '—'} /></div>
            <div style={mFlex(80)}><label style={mLabel}>Unit</label>
              <select style={{ ...mInput, appearance: 'auto' }} value={local.lengthUnit || parentDraft.lengthUnit || 'centimeters'} onChange={e => set('lengthUnit', e.target.value)}>
                <option value="centimeters">cm</option><option value="inches">in</option><option value="meters">m</option>
              </select>
            </div>
            <div style={mFlex(80)}><label style={mLabel}>Weight</label><input style={mInput} type="number" step="0.001" value={local.weight || ''} onChange={e => set('weight', e.target.value)} placeholder={parentDraft.weight || '—'} /></div>
            <div style={mFlex(80)}><label style={mLabel}>Unit</label>
              <select style={{ ...mInput, appearance: 'auto' }} value={local.weightUnit || parentDraft.weightUnit || 'kilograms'} onChange={e => set('weightUnit', e.target.value)}>
                <option value="kilograms">kg</option><option value="grams">g</option><option value="pounds">lbs</option><option value="ounces">oz</option>
              </select>
            </div>
          </div>
          <div style={{ fontSize: 12, fontWeight: 600, color: 'var(--text-secondary)', marginBottom: 6, marginTop: 4 }}>Package / Shipping Dimensions</div>
          <div style={mRow}>
            <div style={mFlex(80)}><label style={mLabel}>Pkg Length</label><input style={mInput} type="number" step="0.1" value={(local as any).pkgLength || ''} onChange={e => set('pkgLength' as any, e.target.value)} placeholder={parentDraft.pkgLength || '—'} /></div>
            <div style={mFlex(80)}><label style={mLabel}>Pkg Width</label><input style={mInput} type="number" step="0.1" value={(local as any).pkgWidth || ''} onChange={e => set('pkgWidth' as any, e.target.value)} placeholder={parentDraft.pkgWidth || '—'} /></div>
            <div style={mFlex(80)}><label style={mLabel}>Pkg Height</label><input style={mInput} type="number" step="0.1" value={(local as any).pkgHeight || ''} onChange={e => set('pkgHeight' as any, e.target.value)} placeholder={parentDraft.pkgHeight || '—'} /></div>
            <div style={mFlex(80)}><label style={mLabel}>Unit</label>
              <select style={{ ...mInput, appearance: 'auto' }} value={(local as any).pkgLengthUnit || parentDraft.pkgLengthUnit || 'centimeters'} onChange={e => set('pkgLengthUnit' as any, e.target.value)}>
                <option value="centimeters">cm</option><option value="inches">in</option><option value="meters">m</option>
              </select>
            </div>
            <div style={mFlex(80)}><label style={mLabel}>Pkg Weight</label><input style={mInput} type="number" step="0.001" value={(local as any).pkgWeight || ''} onChange={e => set('pkgWeight' as any, e.target.value)} placeholder={parentDraft.pkgWeight || '—'} /></div>
            <div style={mFlex(80)}><label style={mLabel}>Unit</label>
              <select style={{ ...mInput, appearance: 'auto' }} value={(local as any).pkgWeightUnit || parentDraft.pkgWeightUnit || 'kilograms'} onChange={e => set('pkgWeightUnit' as any, e.target.value)}>
                <option value="kilograms">kg</option><option value="grams">g</option><option value="pounds">lbs</option><option value="ounces">oz</option>
              </select>
            </div>
          </div>
        </div>

        {/* ════════ PRODUCT DETAILS ════════ */}
        <div style={{ fontSize: 11, fontWeight: 700, textTransform: 'uppercase', letterSpacing: 1.5, color: '#f59e0b', marginBottom: 6 }}>Product Details</div>
        <div style={{ ...mSection, borderLeftColor: '#f59e0b', borderLeftWidth: 3 }}>
          <div style={mRow}>
            <div style={{ flex: 1, minWidth: 300 }}><label style={mLabel}>Title (variant-specific override)</label><input style={mInput} value={local.title || ''} onChange={e => set('title', e.target.value)} placeholder={parentDraft.title || 'Leave blank to use parent title'} /></div>
          </div>
          <div style={mRow}>
            <div style={mFlex(140)}><label style={mLabel}>Brand</label><input style={mInput} value={local.brand || ''} onChange={e => set('brand', e.target.value)} placeholder={parentDraft.brand || 'Brand name'} /></div>
            <div style={mFlex(140)}><label style={mLabel}>Manufacturer</label><input style={mInput} value={local.manufacturer || ''} onChange={e => set('manufacturer', e.target.value)} placeholder="Manufacturer name" /></div>
            <div style={mFlex(140)}><label style={mLabel}>Condition</label>
              <select style={{ ...mInput, appearance: 'auto' }} value={local.condition || parentDraft.condition || 'new_new'} onChange={e => set('condition', e.target.value)}>
                {CONDITION_OPTIONS.map(o => <option key={o.value} value={o.value}>{o.label}</option>)}
              </select>
            </div>
          </div>
        </div>

        {/* ── Actions ── */}
        <div style={{ display: 'flex', gap: 10, justifyContent: 'flex-end', marginTop: 4 }}>
          <button onClick={onClose} style={{ padding: '9px 20px', borderRadius: 7, border: '1px solid var(--border)', background: 'var(--bg-tertiary)', color: 'var(--text-muted)', fontSize: 13, cursor: 'pointer', fontWeight: 500 }}>
            Cancel
          </button>
          <button onClick={() => onSave(local, applyToAll)} style={{ padding: '9px 24px', borderRadius: 7, border: 'none', background: '#d946ef', color: '#fff', fontSize: 13, cursor: 'pointer', fontWeight: 700 }}>
            {applyToAll ? 'Save & Apply to All' : 'Save Variant'}
          </button>
        </div>
      </div>
    </div>
  );
}
