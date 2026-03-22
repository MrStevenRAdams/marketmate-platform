import React, { useState, useRef, useEffect } from 'react';
import { useNavigate, useParams } from 'react-router-dom';
// ✅ NEW: Import ProductTypeModal
import ProductTypeModal from './product-create-components/ProductTypeModal';
import BundleItemsSection from './product-create-components/BundleItemsSection';
import ProductSearchModal from './product-create-components/ProductSearchModal';
import { BundleItem } from './product-create-components/types';
import { categoryService, attributeService, attributeSetService, fileService, productService, variantService, searchService } from '../services/api';
import { credentialService, MarketplaceCredential } from '../services/marketplace-api';
import AIGenerateModal from './product-create-components/AIGenerateModal';
import CreateWithAIModal from './product-create-components/CreateWithAIModal';
import { getActiveTenantId } from '../contexts/TenantContext';
import {
  BatchesTab,
  ExtendedPropertiesTab,
  IdentifiersTab,
  ChannelSkuMappingTab,
  StockHistoryTab,
  ItemStatsTab,
  KTypesTab,
} from '../components/ProductDetailTabs';
import { ChannelListingsTab } from '../components/ChannelListingsPanel';

// Types
type ProductType = 'simple' | 'variant' | 'bundle';

interface ProductSupplierRow {
  id: string; // local row ID for React key
  supplier_id: string;
  supplier_name: string;
  supplier_sku: string;
  unit_cost: number;
  currency: string;
  lead_time_days: number;
  priority: number;
  is_default: boolean;
}

interface SupplierOption {
  supplier_id: string;
  name: string;
  code: string;
  currency: string;
}

interface VariantOption {
  name: string;
  values: string[];
}

interface Variant {
  id: string;
  combination: Record<string, string>;
  sku: string;
  alias: string;
  stock: string;
  status: 'Active' | 'Draft' | 'Inactive';
}

interface Category {
  category_id: string;
  name: string;
  parent_id: string | null;
  children?: Category[];
}

interface Attribute {
  id: string;
  name: string;
  code: string;
  dataType: string;
  options?: { id: string; value: string; label: string }[];
}

interface AttributeSet {
  id: string;
  name: string;
  attributeIds: string[];
}

interface ProductAttribute {
  id: string;
  name: string;
  value: string;
  isCustom: boolean;
  fromSet?: boolean;
  dataType?: string;
  options?: { id: string; value: string; label: string }[];
}

interface ComplianceDoc {
  id: string;
  type: 'compliance';
  documentType: string;
  issuerLab: string;
  documentCode: string;
  issueDate: string;
  expiryDate: string;
  brandOverride: string;
  modelOverride: string;
  authorized: boolean;
  files: File[];
  name: string;
}

interface ProductDocument {
  id: string;
  type: 'document';
  name: string;
  file: File | null;
}

export default function ProductCreate() {
  const navigate = useNavigate();
  const { id: editProductId } = useParams<{ id: string }>();
  const isEditMode = Boolean(editProductId);
  const editorRef = useRef<HTMLDivElement>(null);
  const [activeTab, setActiveTab] = useState<string>('details');
  const [highlightVariantId, setHighlightVariantId] = useState<string | null>(
    new URLSearchParams(window.location.search).get('highlight_variant')
  );

  console.log('[ProductCreate] Mode:', isEditMode ? 'EDIT' : 'CREATE', 'ProductId:', editProductId);

  // Product Type Modal
  // ✅ UPDATED: Product Type Modal (shows on load for create, hidden for edit)
  const [showProductTypeModal, setShowProductTypeModal] = useState(!isEditMode);
  const [productType, setProductType] = useState<ProductType | null>(isEditMode ? 'simple' : null);
  const [loadingProduct, setLoadingProduct] = useState(isEditMode);

  // Basic Details
  const [title, setTitle] = useState('');
  const [brand, setBrand] = useState('');
  const [description, setDescription] = useState('');
  const [tags, setTags] = useState('');
  const [sku, setSku] = useState('');
  const [productIdentifier, setProductIdentifier] = useState('');
  const [identifierType, setIdentifierType] = useState('EAN');
  
  // Categories
  const [selectedCategories, setSelectedCategories] = useState<string[]>([]);
  const [showCategoryModal, setShowCategoryModal] = useState(false);

  // Key Features
  const [keyFeatures, setKeyFeatures] = useState(['', '', '']);

  // Dimensions
  const [itemWidth, setItemWidth] = useState('');
  const [itemHeight, setItemHeight] = useState('');
  const [itemLength, setItemLength] = useState('');
  const [itemWeight, setItemWeight] = useState('');
  const [dimensionsUnit, setDimensionsUnit] = useState('Inches');
  const [weightUnit, setWeightUnit] = useState('Pounds');

  // Shipping Dimensions
  const [packageWidth, setPackageWidth] = useState('');
  const [packageHeight, setPackageHeight] = useState('');
  const [packageLength, setPackageLength] = useState('');
  const [packageWeight, setPackageWeight] = useState('');
  const [packageWeightUnit, setPackageWeightUnit] = useState('Kilograms');

  // Forms sidebar
  const [activeForm, setActiveForm] = useState('basic');
  const [connectedCredentials, setConnectedCredentials] = useState<MarketplaceCredential[]>([]);

  // Variants
  const [variantOptions, setVariantOptions] = useState<VariantOption[]>([]);
  const [currentOptionName, setCurrentOptionName] = useState('');
  const [currentOptionValues, setCurrentOptionValues] = useState('');
  const [variants, setVariants] = useState<Variant[]>([]);
  const [isImportedFamily, setIsImportedFamily] = useState(false); // true = Amazon-imported, options are read-only
  const [selectedVariants, setSelectedVariants] = useState<Set<string>>(new Set());
  const [showBulkEditModal, setShowBulkEditModal] = useState(false);
  const [bulkEditSection, setBulkEditSection] = useState('');
  const [showCancelModal, setShowCancelModal] = useState(false);
  const [showDeleteModal, setShowDeleteModal] = useState(false);
  const [deleting, setDeleting] = useState(false);

  // Alias picker (reuses ProductSearchModal for single-select SKU alias)
  const [showAliasModal, setShowAliasModal] = useState(false);
  const [aliasTargetVariantId, setAliasTargetVariantId] = useState<string | null>(null);

  // Compliance & Documents
  const [complianceDocs, setComplianceDocs] = useState<(ComplianceDoc | ProductDocument)[]>([]);
  const [showComplianceModal, setShowComplianceModal] = useState(false);
  const [showDocumentModal, setShowDocumentModal] = useState(false);
  const [viewingDoc, setViewingDoc] = useState<ComplianceDoc | null>(null);
  const [complianceForm, setComplianceForm] = useState<ComplianceDoc>({
    id: '', type: 'compliance', documentType: '', issuerLab: '', documentCode: '',
    issueDate: '', expiryDate: '', brandOverride: '', modelOverride: '',
    authorized: false, files: [], name: '',
  });
  const [documentForm, setDocumentForm] = useState<{ name: string; file: File | null }>({ name: '', file: null });

  // Save state
  const [saving, setSaving] = useState(false);
  const [saveError, setSaveError] = useState<string | null>(null);
  const [isDirty, setIsDirty] = useState(false);
  const [pendingNavPath, setPendingNavPath] = useState<string | null>(null);
  const [showUnsavedModal, setShowUnsavedModal] = useState(false);

  // Bundle Items
  const [bundleItems, setBundleItems] = useState<BundleItem[]>([]);

  // Suppliers (sourcing)
  const [productSuppliers, setProductSuppliers] = useState<ProductSupplierRow[]>([]);
  const [supplierOptions, setSupplierOptions] = useState<SupplierOption[]>([]);
  const [suppliersLoaded, setSuppliersLoaded] = useState(false);

  // AI Generate Modal
  const [showAIModal, setShowAIModal] = useState(false);
  const [aiModalChannel, setAiModalChannel] = useState('');
  const [aiModalCredentialId, setAiModalCredentialId] = useState('');

  // Create with AI state
  const [showCreateWithAIModal, setShowCreateWithAIModal] = useState(false);
  const [aiDraftProductId, setAiDraftProductId] = useState<string | null>(null);

  // Categories state
  const [categories, setCategories] = useState<Category[]>([]);
  const [expandedCategories, setExpandedCategories] = useState<Set<string>>(new Set());

  // Attributes state
  const [allAttributes, setAllAttributes] = useState<Attribute[]>([]);
  const [attributeSets, setAttributeSets] = useState<AttributeSet[]>([]);
  const [productAttributes, setProductAttributes] = useState<ProductAttribute[]>([]);
  const [selectedAttributeSet, setSelectedAttributeSet] = useState<string>('');
  const [showAttributeModal, setShowAttributeModal] = useState(false);
  const [showCustomAttributeModal, setShowCustomAttributeModal] = useState(false);
  const [customAttributeName, setCustomAttributeName] = useState('');
  const [customAttributeValue, setCustomAttributeValue] = useState('');

  // Images state
  const [productImages, setProductImages] = useState<string[]>([]);
  const [uploadingImage, setUploadingImage] = useState(false);
  const [dragActive, setDragActive] = useState(false);
  const [draggedImageIndex, setDraggedImageIndex] = useState<number | null>(null);


  // ✅ NEW: Handle product type selection  
  const handleProductTypeSelect = (type: ProductType) => {
    setProductType(type);
    setShowProductTypeModal(false);
  };

  // Set editor content after loading in edit mode
  // Set editor content after loading in edit mode
  useEffect(() => {
    if (!loadingProduct && isEditMode && editorRef.current && description) {
      editorRef.current.innerHTML = description;
    }
  }, [loadingProduct]);

  // Load data on mount
  useEffect(() => {
    loadCategories();
    loadAttributes();
    loadAttributeSets();
    loadConnectedCredentials();
    loadSupplierOptions();
    if (isEditMode && editProductId) {
      loadProduct(editProductId);
    }
  }, []);

  // Re-load when editProductId changes (e.g. after variation→parent redirect within same component)
  const prevEditProductId = useRef<string | undefined>(editProductId);
  useEffect(() => {
    if (editProductId && editProductId !== prevEditProductId.current) {
      prevEditProductId.current = editProductId;
      setHighlightVariantId(new URLSearchParams(window.location.search).get('highlight_variant'));
      loadProduct(editProductId);
    }
  }, [editProductId]);

  async function loadSupplierOptions() {
    try {
      const res = await fetch(
        `${import.meta.env.VITE_API_URL ?? 'http://localhost:8080/api/v1'}/suppliers?active=true`,
        { headers: { 'X-Tenant-Id': getActiveTenantId() } }
      );
      if (!res.ok) return;
      const data = await res.json();
      setSupplierOptions(data.suppliers || []);
    } catch {
      // silently ignore
    } finally {
      setSuppliersLoaded(true);
    }
  };

  async function loadProduct(productId: string) {
    console.log('[ProductCreate] loadProduct called with ID:', productId);
    setLoadingProduct(true);
    try {
      console.log('[ProductCreate] Fetching GET /products/' + productId);
      const res = await productService.get(productId);
      console.log('[ProductCreate] Raw API response:', res);
      const p = res.data?.data || res.data;
      console.log('[ProductCreate] Loaded product for edit:', p);

      // Set product type — normalize API values to UI values
      // API stores: simple, parent, variable (parent), variation (child), bundle, variant
      // UI uses:    simple, variant (parent w/ variants grid), bundle
      const rawType = p.product_type || 'simple';
      let pType: ProductType = (rawType === 'variable' || rawType === 'variant' || rawType === 'parent') ? 'variant' :
                                rawType === 'bundle' ? 'bundle' : 'simple';

      // If this is a child variation, redirect to the parent with highlight
      if (rawType === 'variation' && p.parent_id) {
        setLoadingProduct(false);
        navigate(`/products/${p.parent_id}?highlight_variant=${productId}`, { replace: true });
        return;
      }

      // If stored as 'simple' but actually a variation parent — detect by:
      // Signal 1: backend query for children with parent_id filter
      // Signal 2: comma-separated variation attributes = Amazon aggregates all values on parent ASIN
      // Both signals run independently; either is sufficient.
      if (pType === 'simple') {
        // Signal 2 first (instant, no network): comma in variation attributes = parent
        const attrs = p.attributes || {};
        const variationSignals = ['color', 'size', 'style', 'flavour', 'scent', 'material', 'pattern'];
        const looksLikeParent = variationSignals.some(
          (k) => typeof attrs[k] === 'string' && (attrs[k] as string).includes(',')
        );
        if (looksLikeParent) {
          pType = 'variant';
        }

        // Signal 1: backend query (definitive — catches families without comma attributes)
        if (pType === 'simple') {
          try {
            const probe = await productService.list({ parent_id: productId, page_size: 1 });
            const probeData = probe.data?.data || probe.data?.products || [];
            if (Array.isArray(probeData) && probeData.length > 0) {
              pType = 'variant';
            }
          } catch { /* ignore */ }
        }
      }

      setProductType(pType);

      // Basic details
      // SKU: for parent products the top-level sku may be blank (Amazon virtual parents).
      // Try multiple fallbacks: sku → attributes.source_sku → attributes.parent_sku → 
      // attributes.asin → first child's prefix (resolved later if still empty)
      const sourceSku =
        p.sku ||
        p.attributes?.source_sku ||
        p.attributes?.parent_sku ||
        p.attributes?.parent_asin ||
        (p.identifiers?.asin ? `PARENT-${p.identifiers.asin}` : '') ||
        '';
      setSku(typeof sourceSku === 'string' ? sourceSku : String(sourceSku));
      setTitle(p.title || '');
      setBrand(p.brand || '');
      setDescription(p.description || '');
      setTags(Array.isArray(p.tags) ? p.tags.join(', ') : (p.tags || ''));
      setSelectedCategories(p.category_ids || []);

      // Identifiers
      if (p.identifiers) {
        const ids = p.identifiers;
        if (ids.ean) { setProductIdentifier(ids.ean); setIdentifierType('EAN'); }
        else if (ids.upc) { setProductIdentifier(ids.upc); setIdentifierType('UPC'); }
        else if (ids.gtin) { setProductIdentifier(ids.gtin); setIdentifierType('GTIN'); }
        else if (ids.isbn) { setProductIdentifier(ids.isbn); setIdentifierType('ISBN'); }
        else if (ids.asin) { setProductIdentifier(ids.asin); setIdentifierType('ASIN'); }
      }

      // Key features from attributes.bullet_points
      const bullets = p.attributes?.bullet_points;
      if (Array.isArray(bullets) && bullets.length > 0) {
        setKeyFeatures(bullets.map((b: any) => String(b)));
      } else if (Array.isArray(p.key_features) && p.key_features.length > 0) {
        setKeyFeatures(p.key_features);
      }

      // Images from assets
      if (Array.isArray(p.assets) && p.assets.length > 0) {
        const sorted = [...p.assets].sort((a: any, b: any) => (a.sort_order || 0) - (b.sort_order || 0));
        setProductImages(sorted.map((a: any) => a.url).filter(Boolean));
      }

      // Helper to normalize unit strings from API to dropdown values
      const normalizeDimUnit = (unit: string): string => {
        const u = (unit || '').toLowerCase();
        if (u.startsWith('centimeter') || u === 'cm') return 'Centimeters';
        if (u.startsWith('inch') || u === 'in') return 'Inches';
        if (u.startsWith('meter') || u === 'm') return 'Meters';
        if (u.startsWith('feet') || u.startsWith('foot') || u === 'ft') return 'Feet';
        return 'Centimeters'; // default
      };

      // Product Dimensions (from product.dimensions)
      if (p.dimensions) {
        setItemWidth(p.dimensions.width?.toString() || '');
        setItemHeight(p.dimensions.height?.toString() || '');
        setItemLength(p.dimensions.length?.toString() || '');
        // Check unit field, then fall back to per-field units (old format)
        const rawUnit = p.dimensions.unit || p.dimensions.height_unit || p.dimensions.width_unit || p.dimensions.length_unit || '';
        setDimensionsUnit(normalizeDimUnit(rawUnit));
      }
      // Weight (from product.weight or attributes.item_weight)
      if (p.weight) {
        setItemWeight(p.weight.value?.toString() || '');
        setWeightUnit(p.weight.unit || 'Kilograms');
      } else if (p.attributes?.item_weight) {
        setItemWeight(String(p.attributes.item_weight));
      }

      // Shipping Dimensions (from product.shipping_dimensions)
      if (p.shipping_dimensions) {
        setPackageWidth(p.shipping_dimensions.width?.toString() || '');
        setPackageHeight(p.shipping_dimensions.height?.toString() || '');
        setPackageLength(p.shipping_dimensions.length?.toString() || '');
      }
      if (p.shipping_weight) {
        setPackageWeight(p.shipping_weight.value?.toString() || '');
        setPackageWeightUnit(p.shipping_weight.unit || 'Kilograms');
      } else if (p.attributes?.item_package_weight) {
        setPackageWeight(String(p.attributes.item_package_weight));
      }

      // Attributes - convert from map to array, excluding system/enrichment fields
      // Variation-axis attributes — shown in Variants grid, not the Attributes panel
      const variationAxisKeys = new Set(['color', 'size', 'style', 'flavour', 'scent', 'material', 'pattern', 'edition']);

      if (p.attributes && typeof p.attributes === 'object') {
        const systemKeys = new Set([
          'bullet_points', 'source_sku', 'source_price', 'source_quantity',
          'source_currency', 'fulfillment_channel', 'item_condition',
          'amazon_status', 'item_weight', 'item_package_weight', 'parent_asin',
          // Variation axis keys only excluded for variable parents (shown in variants grid instead)
          ...(pType === 'variant' ? Array.from(variationAxisKeys) : []),
        ]);
        const attrs: ProductAttribute[] = Object.entries(p.attributes)
          .filter(([key]) => !systemKeys.has(key))
          .map(([name, value]) => ({
            id: `attr-${name}`,
            name,
            value: typeof value === 'object' ? JSON.stringify(value) : String(value),
            isCustom: true,
          }));
        setProductAttributes(attrs);
      }

      // Load child variations if this is a variable/variant parent
      if (pType === 'variant') {
        try {
          console.log('[ProductCreate] Loading child variations for parent:', productId);

          // Strategy 1: children linked by parent_id (batch-imported families)
          let childData: any[] = [];
          const childRes = await productService.list({ parent_id: productId, page_size: 100 });
          childData = childRes.data?.data || childRes.data?.products || [];

          // Strategy 2: search via Typesense using parent_id filter (indexed field, always reliable)
          if (!Array.isArray(childData) || childData.length === 0) {
            try {
              const tsRes = await searchService.searchProducts({ q: '*', parent_id: productId, per_page: 100 });
              const tsHits = tsRes.data?.data || [];
              if (Array.isArray(tsHits) && tsHits.length > 0) {
                console.log('[ProductCreate] Strategy 2 (Typesense parent_id) found', tsHits.length, 'children');
                childData = tsHits; // hits have product_id, sku, color, size etc.
              }
            } catch (e) { console.warn('[ProductCreate] Typesense parent_id lookup failed:', e); }
          }

          // Strategy 3: search via Typesense using parent_asin (for families where parent_id
          // on children points to a stub, not this product)
          if (!Array.isArray(childData) || childData.length === 0) {
            const parentAsin =
              (p.identifiers?.asin && typeof p.identifiers.asin === 'string' ? p.identifiers.asin : null) ||
              p.attributes?.asin ||
              null;
            console.log('[ProductCreate] Strategies 1+2 empty — trying parent_asin:', parentAsin, '| identifiers:', JSON.stringify(p.identifiers));
            if (parentAsin) {
              try {
                const tsRes2 = await searchService.searchProducts({ q: '*', parent_asin: parentAsin, per_page: 100 });
                const tsHits2 = tsRes2.data?.data || [];
                if (Array.isArray(tsHits2) && tsHits2.length > 0) {
                  console.log('[ProductCreate] Strategy 3 (Typesense parent_asin) found', tsHits2.length, 'children');
                  childData = tsHits2;
                }
              } catch (e) { console.warn('[ProductCreate] Typesense parent_asin lookup failed:', e); }
            }
          }

          // Strategy 4: Firestore filter by attributes.parent_asin (last resort)
          if (!Array.isArray(childData) || childData.length === 0) {
            const parentAsin =
              (p.identifiers?.asin && typeof p.identifiers.asin === 'string' ? p.identifiers.asin : null) ||
              p.attributes?.asin ||
              null;
            if (parentAsin) {
              try {
                const fsRes = await productService.list({ parent_asin: parentAsin, page_size: 100 });
                const fsData = fsRes.data?.data || fsRes.data?.products || [];
                if (Array.isArray(fsData) && fsData.length > 0) {
                  console.log('[ProductCreate] Strategy 4 (Firestore parent_asin) found', fsData.length, 'children');
                  childData = fsData;
                }
              } catch (e) { console.warn('[ProductCreate] Firestore parent_asin lookup failed:', e); }
            }
          }

          let loadedVariants: Variant[] = [];

          if (Array.isArray(childData) && childData.length > 0) {
            setIsImportedFamily(true);
            const VARIATION_AXIS = new Set(['color','size','style','flavour','scent','material','pattern','edition']);
            loadedVariants = childData.map((c: any) => {
              // c can be a full product doc (from Firestore) or a Typesense hit (flat fields)
              // Typesense hits have color/size at top level; Firestore docs have them in attributes
              const combination: Record<string, string> = {};
              VARIATION_AXIS.forEach((k) => {
                // Top-level (Typesense hit)
                if (typeof c[k] === 'string' && c[k].trim() !== '' && !c[k].includes(',')) {
                  combination[k] = c[k];
                }
                // Nested in attributes (Firestore product doc)
                else if (c.attributes && typeof c.attributes[k] === 'string' && c.attributes[k].trim() !== '' && !c.attributes[k].includes(',')) {
                  combination[k] = c.attributes[k];
                }
              });
              const sku = c.sku || c.attributes?.source_sku || c.attributes?.sku || '';
              const rawStatus = c.status || 'active';
              return {
                id: c.product_id || c.id || `var-${Date.now()}-${Math.random()}`,
                combination,
                sku,
                alias: c.alias || '',
                stock: '0',
                status: (rawStatus === 'active' ? 'Active' : rawStatus === 'draft' ? 'Draft' : 'Inactive') as 'Active' | 'Draft' | 'Inactive',
              };
            });
          } else {
            // Fall back to variants subcollection (manually-created products)
            const varRes = await variantService.list(productId);
            const varData = varRes.data?.data || varRes.data || [];
            if (Array.isArray(varData) && varData.length > 0) {
              loadedVariants = varData.map((v: any) => ({
                id: v.variant_id || v.id || `var-${Date.now()}-${Math.random()}`,
                combination: v.combination || v.attributes || {},
                sku: v.sku || '',
                alias: v.alias || '',
                stock: v.stock?.toString() || '0',
                status: (v.status === 'active' ? 'Active' : v.status === 'draft' ? 'Draft' : 'Inactive') as 'Active' | 'Draft' | 'Inactive',
              }));
            }
          }

          if (loadedVariants.length > 0) {
            setVariants(loadedVariants);
            const hid = new URLSearchParams(window.location.search).get('highlight_variant');
            if (hid) {
              setTimeout(() => {
                const el = document.getElementById(`variant-row-${hid}`);
                if (el) el.scrollIntoView({ behavior: 'smooth', block: 'center' });
              }, 400);
            }
            const optionMap: Record<string, Set<string>> = {};
            loadedVariants.forEach((v: any) => {
              Object.entries(v.combination).forEach(([key, val]) => {
                if (!optionMap[key]) optionMap[key] = new Set();
                optionMap[key].add(String(val));
              });
            });
            const opts: VariantOption[] = Object.entries(optionMap).map(([name, values]) => ({
              name,
              values: Array.from(values),
            }));
            setVariantOptions(opts);
          }
        } catch (err) {
          console.error('[ProductCreate] Failed to load child variations:', err);
        }
      }
    } catch (err) {
      console.error('[ProductCreate] Failed to load product:', err);
      setSaveError('Failed to load product for editing');
    } finally {
      // Load supplier rows from the product's suppliers field
      try {
        const res2 = await productService.get(productId);
        const p2 = res2.data?.data || res2.data;
        if (Array.isArray(p2?.suppliers)) {
          setProductSuppliers(
            p2.suppliers.map((s: any, i: number) => ({
              id: `sup-${i}-${Date.now()}`,
              supplier_id: s.supplier_id || '',
              supplier_name: s.supplier_name || '',
              supplier_sku: s.supplier_sku || '',
              unit_cost: s.unit_cost || 0,
              currency: s.currency || 'GBP',
              lead_time_days: s.lead_time_days || 0,
              priority: s.priority || i + 1,
              is_default: s.is_default || false,
            }))
          );
        }
      } catch { /* ignore */ }
      setLoadingProduct(false);
      setTimeout(() => setIsDirty(false), 100);
    }
  };

  async function loadConnectedCredentials() {
    try {
      const res = await credentialService.list();
      const creds: MarketplaceCredential[] = res.data?.data || [];
      setConnectedCredentials(creds.filter(c => c.active));
    } catch (err) {
      console.error('Failed to load marketplace credentials:', err);
    }
  };

  async function loadCategories() {
    try {
      const response = await categoryService.tree();
      console.log('Categories response:', response);
      const tree = response.data?.data?.data || response.data?.data || response.data || [];
      console.log('Parsed categories tree:', tree);
      setCategories(Array.isArray(tree) ? tree : []);
      // Auto-expand root level
      if (Array.isArray(tree) && tree.length > 0) {
        const rootIds = tree.map((c: Category) => c.category_id);
        setExpandedCategories(new Set(rootIds));
      }
    } catch (error) {
      console.error('Failed to load categories:', error);
    }
  };

  async function loadAttributes() {
    try {
      const response = await attributeService.list();
      const data = response.data?.data?.data || response.data?.data || response.data || [];
      setAllAttributes(Array.isArray(data) ? data : []);
    } catch (error) {
      console.error('Failed to load attributes:', error);
    }
  };

  async function loadAttributeSets() {
    try {
      const response = await attributeSetService.list();
      const data = response.data?.data?.data || response.data?.data || response.data || [];
      setAttributeSets(Array.isArray(data) ? data : []);
    } catch (error) {
      console.error('Failed to load attribute sets:', error);
    }
  };

  // Category tree functions
  const toggleCategory = (categoryId: string, e: React.MouseEvent) => {
    e.stopPropagation();
    const newExpanded = new Set(expandedCategories);
    if (newExpanded.has(categoryId)) {
      newExpanded.delete(categoryId);
    } else {
      newExpanded.add(categoryId);
    }
    setExpandedCategories(newExpanded);
  };

  const toggleCategorySelection = (categoryId: string) => {
    if (selectedCategories.includes(categoryId)) {
      setSelectedCategories(selectedCategories.filter(id => id !== categoryId));
    } else {
      setSelectedCategories([...selectedCategories, categoryId]);
    }
  };

  const renderCategoryTree = (cats: Category[], level = 0): React.ReactNode => {
    return cats.map((cat) => {
      const hasChildren = cat.children && cat.children.length > 0;
      const isExpanded = expandedCategories.has(cat.category_id);
      const isSelected = selectedCategories.includes(cat.category_id);
      
      const levelColors = ['#1e40af', '#059669', '#d97706', '#dc2626', '#7c3aed'];
      const levelColor = levelColors[Math.min(level, levelColors.length - 1)];

      return (
        <div key={cat.category_id} style={{ position: 'relative' }}>
          {level > 0 && (
            <div
              style={{
                position: 'absolute',
                left: `${level * 20 - 8}px`,
                top: 0,
                width: '20px',
                height: '50%',
                borderLeft: `2px solid ${levelColor}`,
                borderBottom: `2px solid ${levelColor}`,
                opacity: 0.3,
              }}
            />
          )}
          
          <div
            style={{
              display: 'flex',
              alignItems: 'center',
              padding: '8px',
              paddingLeft: `${level * 20 + 12}px`,
              cursor: 'pointer',
              backgroundColor: isSelected ? '#eff6ff' : 'transparent',
              borderRadius: '4px',
            }}
          >
            {hasChildren ? (
              <button
                onClick={(e) => toggleCategory(cat.category_id, e)}
                style={{
                  width: '24px',
                  height: '24px',
                  border: '1px solid var(--border)',
                  backgroundColor: 'var(--bg-tertiary)',
                  borderRadius: '4px',
                  cursor: 'pointer',
                  display: 'flex',
                  alignItems: 'center',
                  justifyContent: 'center',
                  marginRight: '8px',
                }}
                onMouseEnter={(e) => {
                  e.currentTarget.style.backgroundColor = 'var(--primary)';
                  const icon = e.currentTarget.querySelector('i');
                  if (icon) (icon as HTMLElement).style.color = 'white';
                }}
                onMouseLeave={(e) => {
                  e.currentTarget.style.backgroundColor = 'var(--bg-tertiary)';
                  const icon = e.currentTarget.querySelector('i');
                  if (icon) (icon as HTMLElement).style.color = levelColor;
                }}
              >
                <i
                  className={`ri-arrow-${isExpanded ? 'down' : 'right'}-s-line`}
                  style={{ color: levelColor, fontSize: '16px', fontWeight: 'bold' }}
                ></i>
              </button>
            ) : (
              <span style={{ width: '24px', marginRight: '8px' }}></span>
            )}
            
            <input
              type="checkbox"
              checked={isSelected}
              onChange={() => toggleCategorySelection(cat.category_id)}
              style={{ marginRight: '8px', cursor: 'pointer' }}
              onClick={(e) => e.stopPropagation()}
            />
            
            <i className="ri-folder-line" style={{ color: levelColor, marginRight: '8px' }}></i>
            
            <span style={{ fontSize: '14px', color: isSelected ? levelColor : 'var(--text-primary)', fontWeight: isSelected ? '600' : '500' }}>
              {cat.name}
            </span>
            
            {hasChildren && (
              <span
                style={{
                  marginLeft: '8px',
                  fontSize: '12px',
                  padding: '2px 8px',
                  borderRadius: '12px',
                  backgroundColor: levelColor,
                  color: 'white',
                  fontWeight: '600',
                }}
              >
                {cat.children!.length}
              </span>
            )}
          </div>
          
          {hasChildren && isExpanded && (
            <div style={{ position: 'relative' }}>
              {cat.children!.length > 1 && (
                <div
                  style={{
                    position: 'absolute',
                    left: `${(level + 1) * 20 - 8}px`,
                    top: 0,
                    bottom: 0,
                    width: '2px',
                    backgroundColor: levelColor,
                    opacity: 0.3,
                  }}
                />
              )}
              {renderCategoryTree(cat.children!, level + 1)}
            </div>
          )}
        </div>
      );
    });
  };

  // Attribute functions
  const handleAttributeSetChange = (setId: string) => {
    setSelectedAttributeSet(setId);
    
    if (!setId) return;
    
    const attributeSet = attributeSets.find(s => s.id === setId);
    if (!attributeSet) return;

    const setAttributeIds = attributeSet.attributeIds || [];
    
    const attributesToKeep = productAttributes.filter(attr => 
      attr.isCustom || attr.value !== '' || setAttributeIds.includes(attr.id)
    );

    const existingIds = new Set(attributesToKeep.map(a => a.id));
    const newAttributes = setAttributeIds
      .filter(id => !existingIds.has(id))
      .map(id => {
        const attr = allAttributes.find(a => a.id === id);
        if (!attr) return null;
        return {
          id: attr.id,
          name: attr.name,
          value: '',
          isCustom: false,
          fromSet: true,
          dataType: attr.dataType,
          options: attr.options,
        };
      })
      .filter(Boolean) as ProductAttribute[];

    setProductAttributes([...attributesToKeep, ...newAttributes]);
  };

  const handleAddExistingAttribute = (attrId: string) => {
    const attr = allAttributes.find(a => a.id === attrId);
    if (!attr) return;

    if (productAttributes.some(pa => pa.id === attr.id)) {
      alert('This attribute is already added');
      return;
    }

    setProductAttributes([
      ...productAttributes,
      {
        id: attr.id,
        name: attr.name,
        value: '',
        isCustom: false,
        fromSet: false,
        dataType: attr.dataType,
        options: attr.options,
      },
    ]);
    
    setShowAttributeModal(false);
  };

  const handleAddCustomAttribute = () => {
    if (!customAttributeName.trim()) {
      alert('Please enter an attribute name');
      return;
    }

    const customId = `custom_${Date.now()}`;
    setProductAttributes([
      ...productAttributes,
      {
        id: customId,
        name: customAttributeName,
        value: customAttributeValue,
        isCustom: true,
        fromSet: false,
      },
    ]);

    setCustomAttributeName('');
    setCustomAttributeValue('');
    setShowCustomAttributeModal(false);
  };

  const handleUpdateAttributeValue = (id: string, value: string) => {
    setProductAttributes(productAttributes.map(attr =>
      attr.id === id ? { ...attr, value } : attr
    ));
  };

  const handleDeleteAttribute = (id: string) => {
    setProductAttributes(productAttributes.filter(attr => attr.id !== id));
  };

  // Image upload functions
  const handleImageUpload = async (file: File) => {
    if (!file.type.startsWith('image/')) {
      alert('Please upload an image file');
      return;
    }

    if (file.size > 5 * 1024 * 1024) {
      alert('Image must be less than 5MB');
      return;
    }

    setUploadingImage(true);
    try {
      const formDataUpload = new FormData();
      formDataUpload.append('file', file);
      formDataUpload.append('entity_type', 'products');
      formDataUpload.append('entity_id', 'new-product');
      formDataUpload.append('sub_folder', 'images');

      const response = await fileService.upload(formDataUpload);
      const uploadData = response.data?.data || response.data;
      
      if (uploadData?.url) {
        setProductImages([...productImages, uploadData.url]);
      }
    } catch (error) {
      console.error('Failed to upload image:', error);
      alert('Failed to upload image');
    } finally {
      setUploadingImage(false);
    }
  };

  const handleMultipleImageUpload = async (files: FileList) => {
    const fileArray = Array.from(files);
    
    // Validate all files first
    for (const file of fileArray) {
      if (!file.type.startsWith('image/')) {
        alert('All files must be images');
        return;
      }
      if (file.size > 5 * 1024 * 1024) {
        alert(`${file.name} is larger than 5MB`);
        return;
      }
    }

    setUploadingImage(true);
    try {
      const uploadPromises = fileArray.map(async (file) => {
        const formDataUpload = new FormData();
        formDataUpload.append('file', file);
        formDataUpload.append('entity_type', 'products');
        formDataUpload.append('entity_id', 'new-product');
        formDataUpload.append('sub_folder', 'images');

        const response = await fileService.upload(formDataUpload);
        const uploadData = response.data?.data || response.data;
        return uploadData?.url;
      });

      const urls = await Promise.all(uploadPromises);
      const validUrls = urls.filter(url => url);
      
      if (validUrls.length > 0) {
        setProductImages([...productImages, ...validUrls]);
        setIsDirty(true);
      }
    } catch (error) {
      console.error('Failed to upload images:', error);
      alert('Failed to upload one or more images');
    } finally {
      setUploadingImage(false);
    }
  };

  const handleDrop = (e: React.DragEvent) => {
    e.preventDefault();
    e.stopPropagation();
    setDragActive(false);

    if (e.dataTransfer.files && e.dataTransfer.files.length > 0) {
      if (e.dataTransfer.files.length === 1) {
        handleImageUpload(e.dataTransfer.files[0]);
      } else {
        handleMultipleImageUpload(e.dataTransfer.files);
      }
    }
  };

  const handleDrag = (e: React.DragEvent) => {
    e.preventDefault();
    e.stopPropagation();
    if (e.type === 'dragenter' || e.type === 'dragover') {
      setDragActive(true);
    } else if (e.type === 'dragleave') {
      setDragActive(false);
    }
  };

  const handleRemoveImage = (index: number) => {
    setProductImages(productImages.filter((_, i) => i !== index));
  };

  // Image reordering functions
  const handleImageDragStart = (index: number) => {
    setDraggedImageIndex(index);
  };

  const handleImageDragOver = (e: React.DragEvent, index: number) => {
    e.preventDefault();
    if (draggedImageIndex === null || draggedImageIndex === index) return;

    const newImages = [...productImages];
    const draggedImage = newImages[draggedImageIndex];
    newImages.splice(draggedImageIndex, 1);
    newImages.splice(index, 0, draggedImage);
    
    setProductImages(newImages);
    setDraggedImageIndex(index);
  };

  const handleImageDragEnd = () => {
    setDraggedImageIndex(null);
  };

  // ✅ NEW: Handle product type selection  
  const handleProductTypeSelect_old = (type: ProductType) => {
    setProductType(type);
    setShowProductTypeModal(false);
  };

  // Generate all variant combinations from options
  const generateVariants = () => {
    if (variantOptions.length === 0) {
      setVariants([]);
      return;
    }

    const combinations: Array<Record<string, string>> = [{}];
    
    for (const option of variantOptions) {
      const newCombinations: Array<Record<string, string>> = [];
      for (const combination of combinations) {
        for (const value of option.values) {
          newCombinations.push({
            ...combination,
            [option.name]: value
          });
        }
      }
      combinations.length = 0;
      combinations.push(...newCombinations);
    }

    const newVariants = combinations.map((combo, index) => ({
      id: `variant-${Date.now()}-${index}`,
      combination: combo,
      sku: sku ? `${sku}-${index + 1}` : `VAR-${index + 1}`,
      alias: '',
      stock: '0',
      status: 'Active' as const
    }));

    setVariants(newVariants);
    setSelectedVariants(new Set());
  };

  // Add variant option
  const handleAddOption = () => {
    if (!currentOptionName.trim() || !currentOptionValues.trim()) return;

    const values = currentOptionValues.split(',').map(v => v.trim()).filter(Boolean);
    if (values.length === 0) return;

    const newOption: VariantOption = {
      name: currentOptionName.trim(),
      values
    };

    // Clear API-loaded variants so regeneration can proceed
    setVariants([]);
    setVariantOptions([...variantOptions, newOption]);
    setCurrentOptionName('');
    setCurrentOptionValues('');
  };

  // Remove variant option
  const handleRemoveOption = (index: number) => {
    const newOptions = variantOptions.filter((_, i) => i !== index);
    // Clear API-loaded variants so regeneration can proceed
    setVariants([]);
    setVariantOptions(newOptions);
  };

  // Auto-generate variants when options change
  // Skip regeneration if variants were loaded from API (have real UUIDs, not temp IDs)
  React.useEffect(() => {
    // If current variants have real IDs (loaded from API), don't overwrite them
    const hasApiVariants = variants.length > 0 && variants.some(v => !v.id.startsWith('variant-') && !v.id.startsWith('var-'));
    if (hasApiVariants) {
      return;
    }
    if (variantOptions.length > 0) {
      generateVariants();
    } else {
      setVariants([]);
    }
  }, [variantOptions]);

  // Toggle variant selection
  const toggleVariantSelection = (variantId: string) => {
    const newSelected = new Set(selectedVariants);
    if (newSelected.has(variantId)) {
      newSelected.delete(variantId);
    } else {
      newSelected.add(variantId);
    }
    setSelectedVariants(newSelected);
  };

  // Select all variants
  const toggleSelectAll = () => {
    if (selectedVariants.size === variants.length) {
      setSelectedVariants(new Set());
    } else {
      setSelectedVariants(new Set(variants.map(v => v.id)));
    }
  };

  // Open bulk edit modal
  const handleOpenBulkEdit = () => {
    const section = (document.querySelector('[name="bulk-edit-section"]') as HTMLSelectElement)?.value;
    if (!section || selectedVariants.size === 0) return;
    
    setBulkEditSection(section);
    setShowBulkEditModal(true);
  };

  // Get variant display name
  const getVariantName = (variant: Variant) => {
    return Object.values(variant.combination).join(' / ');
  };

  // Get variant color (if color option exists)
  const getVariantColor = (variant: Variant) => {
    const colorKey = Object.keys(variant.combination).find(k => 
      k.toLowerCase().includes('color') || k.toLowerCase().includes('colour')
    );
    if (!colorKey) return '#6b7280';
    
    const colorName = variant.combination[colorKey].toLowerCase();
    const colorMap: Record<string, string> = {
      'red': '#ef4444',
      'blue': '#3b82f6',
      'green': '#10b981',
      'yellow': '#eab308',
      'purple': '#a855f7',
      'pink': '#ec4899',
      'orange': '#f97316',
      'black': '#1f2937',
      'white': '#f9fafb',
      'gray': '#6b7280',
      'grey': '#6b7280'
    };
    
    return colorMap[colorName] || '#6b7280';
  };

  // Update variant field
  const updateVariant = (variantId: string, field: keyof Variant, value: any) => {
    setVariants(variants.map(v => 
      v.id === variantId ? { ...v, [field]: value } : v
    ));
  };

  // Delete variant
  const deleteVariant = (variantId: string) => {
    setVariants(variants.filter(v => v.id !== variantId));
    const newSelected = new Set(selectedVariants);
    newSelected.delete(variantId);
    setSelectedVariants(newSelected);
  };

  // Handle alias selection from ProductSearchModal
  const handleAliasSelect = (items: BundleItem[]) => {
    if (items.length > 0 && aliasTargetVariantId) {
      const selectedSku = items[0].sku;
      if (selectedSku) {
        setVariants(variants.map(v => 
          v.id === aliasTargetVariantId ? { ...v, alias: selectedSku } : v
        ));
      } else {
        console.warn('[ProductCreate] Alias product has no SKU — alias not set');
      }
    }
    setShowAliasModal(false);
    setAliasTargetVariantId(null);
  };

  // Editor functions
  const formatText = (command: string) => {
    document.execCommand(command, false);
  };

  const handleEditorInput = () => {
    if (editorRef.current) {
      setDescription(editorRef.current.innerHTML);
    }
  };

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    setSaveError(null);

    // Validation
    if (!sku.trim() && productType !== 'variant') {
      setSaveError('SKU is required');
      document.getElementById('field-sku')?.focus();
      return;
    }
    if (!title.trim()) {
      setSaveError('Product Title is required');
      document.getElementById('field-title')?.focus();
      return;
    }
    if (productType === 'variant' && variants.length === 0) {
      setSaveError('Variation products must have at least one variant. Add variant options and generate variants.');
      return;
    }
    if (productType === 'variant') {
      const emptySkuVariant = variants.find(v => !v.sku.trim());
      if (emptySkuVariant) {
        setSaveError('All variants must have a SKU');
        return;
      }
    }

    setSaving(true);
    try {
      // Build product payload
      // Build identifiers
      const identifiers: any = {};
      if (productIdentifier.trim()) {
        identifiers[identifierType.toLowerCase()] = productIdentifier.trim();
      }

      // Build assets from images
      const assets = productImages.map((url, i) => ({
        asset_id: `asset-${i}`,
        url,
        path: '',
        role: i === 0 ? 'primary_image' : 'gallery',
        sort_order: i,
      }));

      // Build dimensions
      const parseDim = (w: string, h: string, l: string, unit: string) => {
        const width = parseFloat(w) || null;
        const height = parseFloat(h) || null;
        const length = parseFloat(l) || null;
        if (!width && !height && !length) return null;
        return { width, height, length, unit };
      };
      const parseWeight = (v: string, unit: string) => {
        const value = parseFloat(v) || null;
        if (!value) return null;
        return { value, unit };
      };

      const productData: any = {
        sku: sku.trim(),
        title: title.trim(),
        brand: brand.trim() || null,
        description: description || null,
        product_type: productType,
        tags: tags.split(',').map(t => t.trim()).filter(Boolean),
        identifiers: Object.keys(identifiers).length > 0 ? identifiers : null,
        category_ids: selectedCategories,
        key_features: keyFeatures.filter(f => f.trim()),
        assets,
        dimensions: parseDim(itemWidth, itemHeight, itemLength, dimensionsUnit),
        weight: parseWeight(itemWeight, weightUnit),
        shipping_dimensions: parseDim(packageWidth, packageHeight, packageLength, dimensionsUnit),
        shipping_weight: parseWeight(packageWeight, packageWeightUnit),
        attributes: productAttributes.reduce((acc: Record<string, any>, attr) => {
          acc[attr.name] = attr.value;
          return acc;
        }, {}),
        suppliers: productSuppliers.map(s => ({
          supplier_id: s.supplier_id,
          supplier_name: s.supplier_name,
          supplier_sku: s.supplier_sku,
          unit_cost: s.unit_cost,
          currency: s.currency,
          lead_time_days: s.lead_time_days,
          priority: s.priority,
          is_default: s.is_default,
        })),
      };

      console.log('[ProductCreate] Saving product:', productData);
      let createdProduct: any;
      if (isEditMode && editProductId) {
        const response = await productService.update(editProductId, productData);
        createdProduct = response.data?.data || response.data;
        console.log('[ProductCreate] Product updated:', createdProduct);
      } else {
        const response = await productService.create(productData);
        createdProduct = response.data?.data || response.data;
        console.log('[ProductCreate] Product created:', createdProduct);
      }

      const productId = createdProduct?.product_id || editProductId;
      console.log('[ProductCreate] productId for variant save:', productId, 'productType:', productType, 'variants count:', variants.length);

      // Create/update variants if variation product
      if (productType === 'variant' && productId) {
        console.log('[ProductCreate] Starting variant save for', variants.length, 'variants');
        for (const variant of variants) {
          const variantData: any = {
            sku: variant.sku,
            combination: variant.combination,
            attributes: variant.combination, // Backend uses 'attributes' not 'combination'
            status: variant.status.toLowerCase(),
            images: productImages,
          };
          // Only include alias if it has a value — avoid sending null which gets ignored
          if (variant.alias && variant.alias.trim()) {
            variantData.alias = variant.alias.trim();
          }
          console.log('[ProductCreate] Saving variant:', variantData);
          try {
            if (isEditMode && variant.id && !variant.id.startsWith('variant-')) {
              const updateRes = await variantService.update(variant.id, variantData);
              console.log('[ProductCreate] Variant updated:', variant.id, updateRes.data);
            } else {
              const createRes = await variantService.create(productId, variantData);
              console.log('[ProductCreate] Variant created:', createRes.data);
              // If alias was set, also patch it explicitly on the newly created variant
              const newVariantId = createRes.data?.data?.variant_id;
              if (variant.alias && variant.alias.trim() && newVariantId) {
                await variantService.update(newVariantId, { alias: variant.alias.trim() });
                console.log('[ProductCreate] Alias patched on new variant:', newVariantId, variant.alias);
              }
            }
          } catch (varErr: any) {
            console.error('[ProductCreate] Variant save FAILED:', variant.sku, varErr?.response?.data || varErr.message);
          }
        }
      } else {
        console.log('[ProductCreate] Skipping variant save — productType:', productType, 'productId:', productId);
      }

      // Success — clear AI draft flag (product is now intentionally saved) then navigate
      setIsDirty(false);
      sessionStorage.removeItem('ai_draft_product_id');
      setAiDraftProductId(null);
      navigate('/products');
    } catch (err: any) {
      console.error('[ProductCreate] Save failed:', err);
      const msg = err?.response?.data?.error || err?.response?.data?.message || err?.message || 'Failed to create product';
      setSaveError(msg);
    } finally {
      setSaving(false);
    }
  };

  const channelEmoji: Record<string, string> = {
    amazon: '📦', ebay: '🏷️', shopify: '🛒', temu: '🛍️', tesco: '🏪',
  };

  // Build marketplace options from connected credentials only
  const connectedMarketplaces = connectedCredentials.map(cred => ({
    id: cred.credential_id,
    name: cred.account_name || `${cred.channel}${cred.marketplace_id ? ' ' + cred.marketplace_id.toUpperCase() : ''}`,
    channel: cred.channel,
    emoji: channelEmoji[cred.channel] || '🔗',
  }));

  // ── Browser close/refresh guard ──
  useEffect(() => {
    const handler = (e: BeforeUnloadEvent) => {
      if (isDirty) { e.preventDefault(); e.returnValue = ''; }
    };
    window.addEventListener('beforeunload', handler);
    return () => window.removeEventListener('beforeunload', handler);
  }, [isDirty]);

  // Helper: navigate with unsaved check
  const guardedNavigate = (path: string) => {
    if (isDirty) {
      setPendingNavPath(path);
      setShowUnsavedModal(true);
    } else {
      navigate(path);
    }
  };

  const handleUnsavedDiscard = () => {
    setIsDirty(false);
    setShowUnsavedModal(false);
    if (pendingNavPath) {
      navigate(pendingNavPath);
      setPendingNavPath(null);
    }
  };

  const handleUnsavedSave = async () => {
    setShowUnsavedModal(false);
    const form = document.querySelector('form');
    if (form) form.requestSubmit();
  };

  const handleUnsavedCancel = () => {
    setShowUnsavedModal(false);
    setPendingNavPath(null);
  };

  return (
    <>
      {/* ✅ NEW: Product Type Selection Modal */}
      <ProductTypeModal
        isOpen={showProductTypeModal}
        onClose={() => setShowProductTypeModal(false)}
        onSelect={handleProductTypeSelect}
      />

      {/* AI Listing Generation Modal */}
      <AIGenerateModal
        isOpen={showAIModal}
        productId={editProductId || ''}
        productTitle={title || 'Product'}
        channel={aiModalChannel}
        credentialId={aiModalCredentialId}
        onClose={() => setShowAIModal(false)}
        onSkip={() => {
          setShowAIModal(false);
          const ch = aiModalChannel;
          if (ch === 'temu') {
            guardedNavigate(`/marketplace/listings/create/temu?product_id=${editProductId}&credential_id=${aiModalCredentialId}`);
          } else {
            guardedNavigate(`/marketplace/listings/create/${ch}?product_id=${editProductId}&credential_id=${aiModalCredentialId}`);
          }
        }}
      />

      {/* Create with AI Modal */}
      <CreateWithAIModal
        isOpen={showCreateWithAIModal}
        onClose={() => setShowCreateWithAIModal(false)}
        onProductCreated={(productId) => {
          // Store in sessionStorage so the edit page knows this is an AI draft
          // and can offer to delete it if the user cancels without saving
          sessionStorage.setItem('ai_draft_product_id', productId);
          setAiDraftProductId(productId);
          setShowCreateWithAIModal(false);
          navigate(`/products/${productId}`);
        }}
      />

      {/* Category Selection Modal */}
      {showCategoryModal && (
        <div style={{
          position: 'fixed',
          top: 0,
          left: 0,
          right: 0,
          bottom: 0,
          backgroundColor: 'rgba(0, 0, 0, 0.7)',
          zIndex: 1000,
          display: 'flex',
          alignItems: 'center',
          justifyContent: 'center'
        }}>
          <div style={{
            backgroundColor: 'var(--bg-secondary)',
            borderRadius: '12px',
            width: '90%',
            maxWidth: '500px',
            padding: '24px',
            border: '1px solid var(--border)'
          }}>
            <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: '20px' }}>
              <h3 style={{ fontSize: '18px', fontWeight: '600', color: 'var(--text-primary)' }}>
                Select Categories
              </h3>
              <button
                onClick={() => setShowCategoryModal(false)}
                style={{
                  background: 'none',
                  border: 'none',
                  color: 'var(--text-muted)',
                  fontSize: '24px',
                  cursor: 'pointer',
                  padding: '4px'
                }}
              >
                ×
              </button>
            </div>
            
            <div style={{ maxHeight: '400px', overflowY: 'auto' }}>
              {categories.length === 0 ? (
                <p style={{ textAlign: 'center', color: 'var(--text-muted)' }}>No categories available</p>
              ) : (
                renderCategoryTree(categories)
              )}
            </div>
            
            <div style={{ marginTop: '20px', display: 'flex', justifyContent: 'flex-end', gap: '12px' }}>
              <button
                type="button"
                className="btn btn-secondary"
                onClick={() => setShowCategoryModal(false)}
              >
                Cancel
              </button>
              <button
                type="button"
                className="btn btn-primary"
                onClick={() => setShowCategoryModal(false)}
              >
                Done ({selectedCategories.length} selected)
              </button>
            </div>
          </div>
        </div>
      )}


      <div style={{ maxWidth: '1600px', margin: '0 auto', padding: 'var(--spacing-xl)' }}>
      {/* Header */}
      <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: 'var(--spacing-xl)' }}>
        <div>
          <a
            onClick={() => navigate('/products')}
            style={{
              color: 'var(--text-muted)',
              textDecoration: 'none',
              fontSize: '14px',
              display: 'flex',
              alignItems: 'center',
              gap: '4px',
              marginBottom: '8px',
              cursor: 'pointer'
            }}
          >
            <i className="ri-arrow-left-line"></i>
            Back to Products
          </a>
          <h1 style={{ fontSize: '28px', fontWeight: 600 }}>{isEditMode ? 'Edit Product' : 'Create Product'}</h1>
          <div style={{ fontSize: '14px', color: 'var(--text-muted)', marginTop: '4px' }}>
            Product Type: {productType ? 
              (productType === 'simple' ? '🔵 Simple Product' : 
               productType === 'variant' ? '📦 Variation Product' : 
               '🎁 Bundled Product') 
              : 'Select type to continue'}
          </div>
        </div>

        {/* Create with AI button — only on create page */}
        {!isEditMode && (
          <button
            type="button"
            className="btn btn-secondary"
            onClick={() => setShowCreateWithAIModal(true)}
            style={{ display: 'flex', alignItems: 'center', gap: 6, whiteSpace: 'nowrap', flexShrink: 0 }}
          >
            ✨ Create with AI
          </button>
        )}
        <select
          value={activeForm}
          onChange={(e) => {
            const val = e.target.value;
            if (val === 'basic') {
              setActiveForm(val);
              return;
            }
            const mp = connectedMarketplaces.find(m => m.id === val);
            if (mp && editProductId) {
              // Show AI generation modal instead of navigating directly
              setAiModalChannel(mp.channel);
              setAiModalCredentialId(mp.id);
              setShowAIModal(true);
              e.target.value = 'basic';
              return;
            }
            if (mp && !editProductId) {
              setSaveError('Please save the product first before listing on a marketplace.');
              e.target.value = 'basic';
              return;
            }
            setActiveForm(val);
          }}
          style={{
            width: '280px',
            background: 'var(--bg-secondary)',
            border: '1px solid var(--border)',
            borderRadius: 'var(--radius-md)',
            padding: '10px 14px',
            color: 'var(--text-primary)',
            fontSize: '14px',
            fontWeight: 500,
            cursor: 'pointer',
            outline: 'none',
            appearance: 'auto',
          }}
        >
          <option value="basic">📋 Basic Details</option>
          {connectedMarketplaces.length > 0 && (
            <optgroup label="Connected Marketplaces">
              {connectedMarketplaces.map((mp) => (
                <option key={mp.id} value={mp.id}>
                  {mp.emoji} {mp.name}
                </option>
              ))}
            </optgroup>
          )}
        </select>
      </div>

      {/* Loading state for edit mode */}
      {loadingProduct && (
        <div style={{ textAlign: 'center', padding: '60px 0' }}>
          <div className="spinner" style={{ margin: '0 auto 16px', width: '40px', height: '40px', border: '4px solid var(--border)', borderTopColor: 'var(--primary)', borderRadius: '50%', animation: 'spin 1s linear infinite' }}></div>
          <p style={{ color: 'var(--text-muted)' }}>Loading product...</p>
        </div>
      )}

      {/* ── Edit mode tabs (shown above the form) ──────────────────────────── */}
      {isEditMode && editProductId && !loadingProduct && (
        <div style={{ marginBottom: '24px' }}>
          <div style={{ display: 'flex', borderBottom: '1px solid var(--border)', overflowX: 'auto' }}>
            {([
              { id: 'details',              label: '📋 Details' },
              { id: 'inventory',            label: '📦 Inventory' },
              { id: 'batches',              label: '🗂️ Batches' },
              { id: 'history',              label: '📋 Stock History' },
              { id: 'stats',               label: '📊 Stats' },
              { id: 'identifiers',          label: '🔍 Identifiers' },
              { id: 'extended',             label: '⚙️ Extended' },
              { id: 'channel_skus',         label: '🔗 Channel SKUs' },
              { id: 'listings',             label: '🛒 Listings' },
              { id: 'ktypes',              label: '🚗 kTypes' },
              { id: 'ai_debug',            label: '🐛 AI Debug' },
            ] as { id: string; label: string }[]).map(t => (
              <button
                key={t.id}
                type="button"
                onClick={() => setActiveTab(t.id)}
                style={{
                  padding: '10px 16px', background: 'none', border: 'none', whiteSpace: 'nowrap',
                  borderBottom: activeTab === t.id ? '2px solid var(--accent-cyan)' : '2px solid transparent',
                  color: activeTab === t.id ? 'var(--accent-cyan)' : 'var(--text-muted)',
                  fontWeight: activeTab === t.id ? 600 : 400, fontSize: '13px', cursor: 'pointer',
                  marginBottom: '-1px',
                }}
              >
                {t.label}
              </button>
            ))}
          </div>

          {/* Non-details tab content */}
          {activeTab !== 'details' && (
            <div style={{ marginTop: '24px' }}>
              {activeTab === 'batches'              && <BatchesTab productId={editProductId} />}
              {activeTab === 'extended'             && <ExtendedPropertiesTab productId={editProductId} />}
              {activeTab === 'identifiers'          && <IdentifiersTab productId={editProductId} initialIdentifiers={undefined} />}
              {activeTab === 'channel_skus'         && <ChannelSkuMappingTab productId={editProductId} />}
              {activeTab === 'listings'             && <ChannelListingsTab productId={editProductId} />}
              {activeTab === 'history'              && <StockHistoryTab productId={editProductId} />}
              {activeTab === 'stats'                && <ItemStatsTab productId={editProductId} />}
              {activeTab === 'ktypes'               && <KTypesTab productId={editProductId} />}
              {activeTab === 'ai_debug'             && <AIDebugPanel productId={editProductId} />}

            </div>
          )}
        </div>
      )}

      {/* Form Grid */}
      {!loadingProduct && (activeTab === 'details' || !isEditMode) && (
      <form onSubmit={handleSubmit} onChange={() => { if (!isDirty) setIsDirty(true); }}>
        <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: 'var(--spacing-lg)', alignItems: 'start' }}>
          {/* LEFT COLUMN */}
          <div style={{ minWidth: 0 }}>
            {/* Basic Details Card */}
            <div className="card" style={{ marginBottom: 'var(--spacing-lg)' }}>
              <div className="card-header">
                <h2 style={{ fontSize: '18px', fontWeight: 600 }}>Basic Details</h2>
              </div>
              <div style={{ padding: 'var(--spacing-lg)' }}>
                <div style={{ display: 'flex', flexDirection: 'column', gap: '12px' }}>
                  {/* SKU / Parent SKU */}
                  <div>
                    <div style={{ fontSize: '12px', color: 'var(--text-muted)', marginBottom: '4px', fontWeight: 500 }}>
                      {productType === 'variant' ? 'Parent SKU' : 'SKU'} <span style={{ color: 'var(--danger)' }}>*</span>
                    </div>
                    <input
                      type="text"
                      id="field-sku"
                      value={sku}
                      onChange={(e) => setSku(e.target.value)}
                      className="input" style={{ width: '100%' }}
                      placeholder={productType === 'variant' ? 'Enter parent SKU' : 'Enter SKU'}
                    />
                  </div>

                  {/* Product Title */}
                  <div>
                    <div style={{ fontSize: '12px', color: 'var(--text-muted)', marginBottom: '4px', fontWeight: 500 }}>
                      Product Title <span style={{ color: 'var(--danger)' }}>*</span>
                    </div>
                    <input
                      type="text"
                      id="field-title"
                      value={title}
                      onChange={(e) => setTitle(e.target.value)}
                      className="input"
                      style={{ width: '100%' }}
                      placeholder="Enter product title"
                    />
                  </div>

                  {/* Brand */}
                  <div>
                    <div style={{ fontSize: '12px', color: 'var(--text-muted)', marginBottom: '4px', fontWeight: 500 }}>Brand</div>
                    <input
                      type="text"
                      value={brand}
                      onChange={(e) => setBrand(e.target.value)}
                      className="input"
                      style={{ width: '100%' }}
                      placeholder="Enter brand name"
                    />
                  </div>

                  {/* Description */}
                  <div>
                    <div style={{ fontSize: '12px', color: 'var(--text-muted)', marginBottom: '4px', fontWeight: 500 }}>Description</div>
                    <div style={{ border: '1px solid var(--border)', borderRadius: '8px', overflow: 'hidden' }}>
                      <div style={{ display: 'flex', gap: '4px', padding: '8px', borderBottom: '1px solid var(--border)', background: 'var(--bg-tertiary)' }}>
                        <button
                          type="button"
                          onClick={() => formatText('bold')}
                          style={{ padding: '6px', background: 'transparent', border: 'none', borderRadius: '4px', cursor: 'pointer', color: 'var(--text-primary)', fontWeight: 'bold' }}
                        >
                          B
                        </button>
                        <button
                          type="button"
                          onClick={() => formatText('italic')}
                          style={{ padding: '6px', background: 'transparent', border: 'none', borderRadius: '4px', cursor: 'pointer', color: 'var(--text-primary)', fontStyle: 'italic' }}
                        >
                          I
                        </button>
                        <button
                          type="button"
                          onClick={() => formatText('underline')}
                          style={{ padding: '6px', background: 'transparent', border: 'none', borderRadius: '4px', cursor: 'pointer', color: 'var(--text-primary)', textDecoration: 'underline' }}
                        >
                          U
                        </button>
                      </div>
                      <div
                        ref={editorRef}
                        contentEditable
                        onInput={handleEditorInput}
                        style={{
                          padding: '12px',
                          minHeight: '100px',
                          fontSize: '14px',
                          color: 'var(--text-primary)',
                          background: 'var(--bg-primary)',
                          outline: 'none'
                        }}
                      />
                    </div>
                  </div>

                  {/* Categories */}
                  <div>
                    <div style={{ fontSize: '12px', color: 'var(--text-muted)', marginBottom: '4px', fontWeight: 500 }}>Categories</div>
                    <button
                      type="button"
                      onClick={() => setShowCategoryModal(!showCategoryModal)}
                      style={{
                        width: '100%',
                        padding: '12px',
                        border: '2px dashed var(--border)',
                        background: 'transparent',
                        color: 'var(--text-muted)',
                        borderRadius: '8px',
                        cursor: 'pointer',
                        display: 'flex',
                        alignItems: 'center',
                        justifyContent: 'center',
                        gap: '8px',
                        fontSize: '14px'
                      }}
                    >
                      <i className="ri-add-line"></i>
                      Add Categories
                    </button>
                    {selectedCategories.length > 0 && (
                      <div style={{ display: 'flex', flexWrap: 'wrap', gap: '8px', marginTop: '8px' }}>
                        {selectedCategories.map(catId => {
                          const findCategory = (cats: Category[]): Category | null => {
                            for (const cat of cats) {
                              if (cat.category_id === catId) return cat;
                              if (cat.children) {
                                const found = findCategory(cat.children);
                                if (found) return found;
                              }
                            }
                            return null;
                          };
                          const cat = findCategory(categories);
                          return cat ? (
                            <div
                              key={catId}
                              style={{
                                padding: '4px 10px',
                                background: 'var(--bg-tertiary)',
                                border: '1px solid var(--border)',
                                borderRadius: '6px',
                                fontSize: '13px',
                                display: 'flex',
                                alignItems: 'center',
                                gap: '6px'
                              }}
                            >
                              {cat.name}
                              <button
                                type="button"
                                onClick={() => setSelectedCategories(selectedCategories.filter(id => id !== catId))}
                                style={{ background: 'none', border: 'none', color: 'var(--text-muted)', cursor: 'pointer', padding: 0, display: 'flex' }}
                              >
                                <i className="ri-close-line"></i>
                              </button>
                            </div>
                          ) : null;
                        })}
                      </div>
                    )}
                  </div>

                  {/* Tags */}
                  <div>
                    <div style={{ fontSize: '12px', color: 'var(--text-muted)', marginBottom: '4px', fontWeight: 500 }}>Tags</div>
                    <input
                      type="text"
                      value={tags}
                      onChange={(e) => setTags(e.target.value)}
                      className="input" style={{ width: '100%' }}
                      placeholder="Enter tags (comma separated)"
                    />
                  </div>

                  {/* Product Identifier */}
                  <div style={{ display: 'grid', gridTemplateColumns: '1fr auto', gap: '10px' }}>
                    <div>
                      <div style={{ fontSize: '12px', color: 'var(--text-muted)', marginBottom: '4px', fontWeight: 500 }}>Product Identifier</div>
                      <input
                        type="text"
                        value={productIdentifier}
                        onChange={(e) => setProductIdentifier(e.target.value)}
                        className="input"
                        style={{ width: '100%' }}
                        placeholder="Enter identifier"
                      />
                    </div>
                    <div>
                      <div style={{ fontSize: '12px', color: 'var(--text-muted)', marginBottom: '4px', fontWeight: 500 }}>Type</div>
                      <select
                        value={identifierType}
                        onChange={(e) => setIdentifierType(e.target.value)}
                        className="select"
                      >
                        <option value="EAN">EAN</option>
                        <option value="UPC">UPC</option>
                        <option value="GTIN">GTIN</option>
                        <option value="ISBN">ISBN</option>
                        <option value="ASIN">ASIN</option>
                      </select>
                    </div>
                  </div>
                </div>
              </div>
            </div>

            {/* Key Features Card */}
            <div className="card" style={{ marginBottom: 'var(--spacing-lg)' }}>
              <div className="card-header">
                <h2 style={{ fontSize: '18px', fontWeight: 600 }}>Key Features</h2>
              </div>
              <div style={{ padding: 'var(--spacing-lg)' }}>
                {keyFeatures.map((feature, index) => (
                  <div key={index} style={{ display: 'flex', gap: '8px', marginBottom: '8px' }}>
                    <input
                      type="text"
                      value={feature}
                      onChange={(e) => {
                        const newFeatures = [...keyFeatures];
                        newFeatures[index] = e.target.value;
                        setKeyFeatures(newFeatures);
                      }}
                      className="input"
                      style={{ flex: 1 }}
                      placeholder="Enter key feature"
                    />
                    <button
                      type="button"
                      onClick={() => {
                        if (keyFeatures.length > 1) {
                          setKeyFeatures(keyFeatures.filter((_, i) => i !== index));
                        }
                      }}
                      style={{
                        padding: '10px',
                        background: 'transparent',
                        border: 'none',
                        color: 'var(--text-muted)',
                        cursor: 'pointer',
                        borderRadius: '6px'
                      }}
                    >
                      <i className="ri-delete-bin-line"></i>
                    </button>
                  </div>
                ))}
                <button
                  type="button"
                  onClick={() => setKeyFeatures([...keyFeatures, ''])}
                  style={{
                    color: 'var(--primary)',
                    background: 'none',
                    border: 'none',
                    cursor: 'pointer',
                    fontSize: '14px',
                    fontWeight: 500
                  }}
                >
                  + Add Feature
                </button>
              </div>
            </div>

          </div>

          {/* RIGHT COLUMN */}
          <div style={{ minWidth: 0 }}>
            {/* Dimensions Card */}
            <div className="card" style={{ marginBottom: 'var(--spacing-lg)' }}>
              <div className="card-header">
                <h2 style={{ fontSize: '18px', fontWeight: 600 }}>Dimensions</h2>
              </div>
              <div style={{ padding: 'var(--spacing-lg)' }}>
                {/* Product Dimensions */}
                <div style={{ marginBottom: '20px' }}>
                  <div style={{ fontSize: '14px', fontWeight: 500, marginBottom: '12px' }}>Product Dimensions</div>
                  <div style={{ display: 'grid', gridTemplateColumns: 'repeat(3, 1fr) 1fr', gap: '10px', marginBottom: '10px' }}>
                    <div>
                      <div style={{ fontSize: '11px', color: 'var(--text-muted)', marginBottom: '4px' }}>Width</div>
                      <input type="text" inputMode="decimal" value={itemWidth} onChange={(e) => { if (/^\d{0,5}(\.\d{0,2})?$/.test(e.target.value) || e.target.value === '') setItemWidth(e.target.value); }} className="input" style={{ width: '100%' }} placeholder="0.00" />
                    </div>
                    <div>
                      <div style={{ fontSize: '11px', color: 'var(--text-muted)', marginBottom: '4px' }}>Height</div>
                      <input type="text" inputMode="decimal" value={itemHeight} onChange={(e) => { if (/^\d{0,5}(\.\d{0,2})?$/.test(e.target.value) || e.target.value === '') setItemHeight(e.target.value); }} className="input" style={{ width: '100%' }} placeholder="0.00" />
                    </div>
                    <div>
                      <div style={{ fontSize: '11px', color: 'var(--text-muted)', marginBottom: '4px' }}>Length</div>
                      <input type="text" inputMode="decimal" value={itemLength} onChange={(e) => { if (/^\d{0,5}(\.\d{0,2})?$/.test(e.target.value) || e.target.value === '') setItemLength(e.target.value); }} className="input" style={{ width: '100%' }} placeholder="0.00" />
                    </div>
                    <div>
                      <div style={{ fontSize: '11px', color: 'var(--text-muted)', marginBottom: '4px' }}>Dim Unit</div>
                      <select value={dimensionsUnit} onChange={(e) => setDimensionsUnit(e.target.value)} className="select" style={{ width: '100%' }}>
                        <option value="Inches">in</option>
                        <option value="Feet">ft</option>
                        <option value="Centimeters">cm</option>
                        <option value="Meters">m</option>
                      </select>
                    </div>
                  </div>
                  <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: '10px', maxWidth: '50%' }}>
                    <div>
                      <div style={{ fontSize: '11px', color: 'var(--text-muted)', marginBottom: '4px' }}>Weight</div>
                      <input type="text" inputMode="decimal" value={itemWeight} onChange={(e) => { if (/^\d{0,5}(\.\d{0,2})?$/.test(e.target.value) || e.target.value === '') setItemWeight(e.target.value); }} className="input" style={{ width: '100%' }} placeholder="0.00" />
                    </div>
                    <div>
                      <div style={{ fontSize: '11px', color: 'var(--text-muted)', marginBottom: '4px' }}>Weight Unit</div>
                      <select value={weightUnit} onChange={(e) => setWeightUnit(e.target.value)} className="select" style={{ width: '100%' }}>
                        <option value="Kilograms">kg</option>
                        <option value="Pounds">lb</option>
                        <option value="Grams">g</option>
                        <option value="Ounces">oz</option>
                      </select>
                    </div>
                  </div>
                </div>

                {/* Shipping Dimensions */}
                <div>
                  <div style={{ fontSize: '14px', fontWeight: 500, marginBottom: '12px' }}>Shipping Dimensions</div>
                  <div style={{ display: 'grid', gridTemplateColumns: 'repeat(3, 1fr) 1fr', gap: '10px', marginBottom: '10px' }}>
                    <div>
                      <div style={{ fontSize: '11px', color: 'var(--text-muted)', marginBottom: '4px' }}>Width</div>
                      <input type="text" inputMode="decimal" value={packageWidth} onChange={(e) => { if (/^\d{0,5}(\.\d{0,2})?$/.test(e.target.value) || e.target.value === '') setPackageWidth(e.target.value); }} className="input" style={{ width: '100%' }} placeholder="0.00" />
                    </div>
                    <div>
                      <div style={{ fontSize: '11px', color: 'var(--text-muted)', marginBottom: '4px' }}>Height</div>
                      <input type="text" inputMode="decimal" value={packageHeight} onChange={(e) => { if (/^\d{0,5}(\.\d{0,2})?$/.test(e.target.value) || e.target.value === '') setPackageHeight(e.target.value); }} className="input" style={{ width: '100%' }} placeholder="0.00" />
                    </div>
                    <div>
                      <div style={{ fontSize: '11px', color: 'var(--text-muted)', marginBottom: '4px' }}>Length</div>
                      <input type="text" inputMode="decimal" value={packageLength} onChange={(e) => { if (/^\d{0,5}(\.\d{0,2})?$/.test(e.target.value) || e.target.value === '') setPackageLength(e.target.value); }} className="input" style={{ width: '100%' }} placeholder="0.00" />
                    </div>
                    <div>
                      <div style={{ fontSize: '11px', color: 'var(--text-muted)', marginBottom: '4px' }}>Dim Unit</div>
                      <select value={dimensionsUnit} onChange={(e) => setDimensionsUnit(e.target.value)} className="select" style={{ width: '100%' }}>
                        <option value="Inches">in</option>
                        <option value="Feet">ft</option>
                        <option value="Centimeters">cm</option>
                        <option value="Meters">m</option>
                      </select>
                    </div>
                  </div>
                  <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: '10px', maxWidth: '50%' }}>
                    <div>
                      <div style={{ fontSize: '11px', color: 'var(--text-muted)', marginBottom: '4px' }}>Weight</div>
                      <input type="text" inputMode="decimal" value={packageWeight} onChange={(e) => { if (/^\d{0,5}(\.\d{0,2})?$/.test(e.target.value) || e.target.value === '') setPackageWeight(e.target.value); }} className="input" style={{ width: '100%' }} placeholder="0.00" />
                    </div>
                    <div>
                      <div style={{ fontSize: '11px', color: 'var(--text-muted)', marginBottom: '4px' }}>Weight Unit</div>
                      <select value={packageWeightUnit} onChange={(e) => setPackageWeightUnit(e.target.value)} className="select" style={{ width: '100%' }}>
                        <option value="Kilograms">kg</option>
                        <option value="Pounds">lb</option>
                        <option value="Grams">g</option>
                        <option value="Ounces">oz</option>
                      </select>
                    </div>
                  </div>
                </div>
              </div>
            </div>

            {/* Shipping & Customs Card */}
            <div className="card" style={{ marginBottom: 'var(--spacing-lg)' }}>
              <div className="card-header">
                <h2 style={{ fontSize: '18px', fontWeight: 600 }}>Shipping & Customs</h2>
              </div>
              <div style={{ padding: 'var(--spacing-lg)' }}>
                <div style={{ display: 'flex', flexDirection: 'column', gap: '12px' }}>
                  {/* Shipping Method and Declared Value */}
                  <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: '12px' }}>
                    <div>
                      <div style={{ fontSize: '12px', color: 'var(--text-muted)', marginBottom: '4px', fontWeight: 500 }}>Shipping Method</div>
                      <select className="select" style={{ width: '100%' }}>
                        <option>Standard</option>
                        <option>Express</option>
                        <option>Priority</option>
                        <option>Economy</option>
                      </select>
                    </div>
                    <div>
                      <div style={{ fontSize: '12px', color: 'var(--text-muted)', marginBottom: '4px', fontWeight: 500 }}>Declared Value</div>
                      <input type="number" className="input" style={{ width: '100%' }} placeholder="£ 0.00" step="0.01" />
                    </div>
                  </div>

                  {/* HS Code and Country of Origin */}
                  <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: '12px' }}>
                    <div>
                      <div style={{ fontSize: '12px', color: 'var(--text-muted)', marginBottom: '4px', fontWeight: 500 }}>HS Code</div>
                      <input type="text" className="input" style={{ width: '100%' }} placeholder="Enter HS Code" />
                    </div>
                    <div>
                      <div style={{ fontSize: '12px', color: 'var(--text-muted)', marginBottom: '4px', fontWeight: 500 }}>Country of Origin</div>
                      <select className="select" style={{ width: '100%' }}>
                        <option>Select country</option>
                        <option>United Kingdom</option>
                        <option>United States</option>
                        <option>China</option>
                        <option>Germany</option>
                        <option>Japan</option>
                      </select>
                    </div>
                  </div>

                  {/* Customs Description */}
                  <div>
                    <div style={{ fontSize: '12px', color: 'var(--text-muted)', marginBottom: '4px', fontWeight: 500 }}>Customs Description</div>
                    <textarea
                      className="input"
                      style={{ width: '100%', resize: 'vertical' }}
                      rows={3}
                      placeholder="Enter customs description"
                    ></textarea>
                  </div>
                </div>
              </div>
            </div>

            {/* Suppliers Card */}
            <div className="card" style={{ marginBottom: 'var(--spacing-lg)' }}>
              <div className="card-header" style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between' }}>
                <h2 style={{ fontSize: '18px', fontWeight: 600 }}>Suppliers</h2>
                <button
                  type="button"
                  className="btn btn-secondary"
                  style={{ fontSize: '13px', padding: '6px 14px' }}
                  onClick={() => {
                    const defaultCurrency = productSuppliers.length === 0 ? 'GBP' : productSuppliers[0].currency;
                    setProductSuppliers(prev => [...prev, {
                      id: `new-${Date.now()}`,
                      supplier_id: '',
                      supplier_name: '',
                      supplier_sku: '',
                      unit_cost: 0,
                      currency: defaultCurrency,
                      lead_time_days: 3,
                      priority: prev.length + 1,
                      is_default: prev.length === 0,
                    }]);
                  }}
                >
                  + Add Supplier
                </button>
              </div>
              <div style={{ padding: 'var(--spacing-lg)' }}>
                {productSuppliers.length === 0 ? (
                  <p style={{ color: 'var(--text-muted)', fontSize: '14px', margin: 0 }}>
                    No suppliers assigned. Click "+ Add Supplier" to link a supplier to this product for purchase order automation.
                  </p>
                ) : (
                  <>
                    {productSuppliers.some(s => s.is_default) === false && productSuppliers.length > 0 && (
                      <div style={{ background: 'rgba(245,158,11,0.1)', border: '1px solid var(--warning)', borderRadius: '6px', padding: '8px 12px', marginBottom: '12px', fontSize: '13px', color: 'var(--warning)' }}>
                        ⚠ No default supplier set. Mark one supplier as default for purchase order auto-generation.
                      </div>
                    )}
                    <div style={{ overflowX: 'auto' }}>
                      <table style={{ width: '100%', borderCollapse: 'collapse', fontSize: '13px' }}>
                        <thead>
                          <tr style={{ background: 'var(--bg-tertiary)' }}>
                            <th style={{ padding: '8px 10px', textAlign: 'left', fontWeight: 600, fontSize: '11px', textTransform: 'uppercase', letterSpacing: '0.05em', color: 'var(--text-muted)', borderBottom: '1px solid var(--border)' }}>Supplier</th>
                            <th style={{ padding: '8px 10px', textAlign: 'left', fontWeight: 600, fontSize: '11px', textTransform: 'uppercase', letterSpacing: '0.05em', color: 'var(--text-muted)', borderBottom: '1px solid var(--border)' }}>Supplier SKU</th>
                            <th style={{ padding: '8px 10px', textAlign: 'left', fontWeight: 600, fontSize: '11px', textTransform: 'uppercase', letterSpacing: '0.05em', color: 'var(--text-muted)', borderBottom: '1px solid var(--border)' }}>Unit Cost</th>
                            <th style={{ padding: '8px 10px', textAlign: 'left', fontWeight: 600, fontSize: '11px', textTransform: 'uppercase', letterSpacing: '0.05em', color: 'var(--text-muted)', borderBottom: '1px solid var(--border)' }}>Currency</th>
                            <th style={{ padding: '8px 10px', textAlign: 'left', fontWeight: 600, fontSize: '11px', textTransform: 'uppercase', letterSpacing: '0.05em', color: 'var(--text-muted)', borderBottom: '1px solid var(--border)' }}>Lead Time</th>
                            <th style={{ padding: '8px 10px', textAlign: 'left', fontWeight: 600, fontSize: '11px', textTransform: 'uppercase', letterSpacing: '0.05em', color: 'var(--text-muted)', borderBottom: '1px solid var(--border)' }}>Priority</th>
                            <th style={{ padding: '8px 10px', textAlign: 'center', fontWeight: 600, fontSize: '11px', textTransform: 'uppercase', letterSpacing: '0.05em', color: 'var(--text-muted)', borderBottom: '1px solid var(--border)' }}>Default</th>
                            <th style={{ padding: '8px 10px', borderBottom: '1px solid var(--border)', width: 40 }}></th>
                          </tr>
                        </thead>
                        <tbody>
                          {productSuppliers.map((row, idx) => (
                            <tr key={row.id} style={{ borderBottom: '1px solid rgba(255,255,255,0.04)' }}>
                              <td style={{ padding: '6px 8px', verticalAlign: 'middle' }}>
                                {suppliersLoaded ? (
                                  <select
                                    className="input"
                                    style={{ fontSize: '13px', padding: '6px 8px', minWidth: 160 }}
                                    value={row.supplier_id}
                                    onChange={e => {
                                      const opt = supplierOptions.find(s => s.supplier_id === e.target.value);
                                      setProductSuppliers(prev => prev.map((r, i) => i === idx
                                        ? { ...r, supplier_id: e.target.value, supplier_name: opt?.name || '', currency: opt?.currency || r.currency }
                                        : r));
                                    }}
                                  >
                                    <option value="">— Select supplier —</option>
                                    {supplierOptions.map(s => (
                                      <option key={s.supplier_id} value={s.supplier_id}>{s.name} ({s.code})</option>
                                    ))}
                                  </select>
                                ) : (
                                  <input className="input" style={{ fontSize: '13px', padding: '6px 8px' }} value={row.supplier_name} placeholder="Loading…" disabled />
                                )}
                              </td>
                              <td style={{ padding: '6px 8px', verticalAlign: 'middle' }}>
                                <input
                                  className="input"
                                  style={{ fontSize: '13px', padding: '6px 8px', fontFamily: 'monospace', width: 110 }}
                                  value={row.supplier_sku}
                                  onChange={e => setProductSuppliers(prev => prev.map((r, i) => i === idx ? { ...r, supplier_sku: e.target.value } : r))}
                                  placeholder="SUP-SKU-001"
                                />
                              </td>
                              <td style={{ padding: '6px 8px', verticalAlign: 'middle' }}>
                                <input
                                  className="input"
                                  type="number"
                                  min={0}
                                  step={0.01}
                                  style={{ fontSize: '13px', padding: '6px 8px', width: 90 }}
                                  value={row.unit_cost || ''}
                                  onChange={e => setProductSuppliers(prev => prev.map((r, i) => i === idx ? { ...r, unit_cost: parseFloat(e.target.value) || 0 } : r))}
                                  placeholder="0.00"
                                />
                              </td>
                              <td style={{ padding: '6px 8px', verticalAlign: 'middle' }}>
                                <select
                                  className="input"
                                  style={{ fontSize: '13px', padding: '6px 8px', width: 80 }}
                                  value={row.currency}
                                  onChange={e => setProductSuppliers(prev => prev.map((r, i) => i === idx ? { ...r, currency: e.target.value } : r))}
                                >
                                  {['GBP','USD','EUR','AUD','CAD','JPY','CNY'].map(c => <option key={c} value={c}>{c}</option>)}
                                </select>
                              </td>
                              <td style={{ padding: '6px 8px', verticalAlign: 'middle' }}>
                                <input
                                  className="input"
                                  type="number"
                                  min={0}
                                  style={{ fontSize: '13px', padding: '6px 8px', width: 70 }}
                                  value={row.lead_time_days}
                                  onChange={e => setProductSuppliers(prev => prev.map((r, i) => i === idx ? { ...r, lead_time_days: parseInt(e.target.value) || 0 } : r))}
                                />
                              </td>
                              <td style={{ padding: '6px 8px', verticalAlign: 'middle' }}>
                                <input
                                  className="input"
                                  type="number"
                                  min={1}
                                  style={{ fontSize: '13px', padding: '6px 8px', width: 60 }}
                                  value={row.priority}
                                  onChange={e => setProductSuppliers(prev => prev.map((r, i) => i === idx ? { ...r, priority: parseInt(e.target.value) || 1 } : r))}
                                />
                              </td>
                              <td style={{ padding: '6px 8px', verticalAlign: 'middle', textAlign: 'center' }}>
                                <input
                                  type="radio"
                                  name="supplier_default"
                                  checked={row.is_default}
                                  onChange={() => setProductSuppliers(prev => prev.map((r, i) => ({ ...r, is_default: i === idx })))}
                                  style={{ accentColor: 'var(--primary)', width: 16, height: 16, cursor: 'pointer' }}
                                />
                              </td>
                              <td style={{ padding: '6px 8px', verticalAlign: 'middle' }}>
                                <button
                                  type="button"
                                  style={{ background: 'none', border: 'none', color: 'var(--danger)', cursor: 'pointer', fontSize: '16px', padding: '2px 4px' }}
                                  onClick={() => {
                                    const updated = productSuppliers.filter((_, i) => i !== idx);
                                    // If we deleted the default, make first remaining default
                                    if (row.is_default && updated.length > 0) updated[0].is_default = true;
                                    setProductSuppliers(updated);
                                  }}
                                >
                                  ×
                                </button>
                              </td>
                            </tr>
                          ))}
                        </tbody>
                      </table>
                    </div>
                    <p style={{ fontSize: '12px', color: 'var(--text-muted)', marginTop: 10, marginBottom: 0 }}>
                      The default supplier is used for purchase order auto-generation. Priority 1 = highest preference.
                    </p>
                  </>
                )}
              </div>
            </div>

            {/* Compliance & Documents Card */}
            <div className="card" style={{ marginBottom: 'var(--spacing-lg)' }}>
              <div className="card-header">
                <h2 style={{ fontSize: '18px', fontWeight: 600 }}>Compliance & Documents</h2>
              </div>
              <div style={{ padding: 'var(--spacing-lg)' }}>
                {/* Document list */}
                {complianceDocs.length > 0 && (
                  <div style={{ marginBottom: '16px', display: 'flex', flexDirection: 'column', gap: '6px' }}>
                    {complianceDocs.map((doc) => (
                      <div
                        key={doc.id}
                        style={{
                          display: 'flex',
                          alignItems: 'center',
                          justifyContent: 'space-between',
                          padding: '10px 12px',
                          background: 'var(--bg-tertiary)',
                          borderRadius: '6px',
                          border: '1px solid var(--border)',
                        }}
                      >
                        <div
                          style={{ display: 'flex', alignItems: 'center', gap: '8px', cursor: 'pointer', flex: 1 }}
                          onClick={() => {
                            if (doc.type === 'compliance') {
                              setViewingDoc(doc as ComplianceDoc);
                              setComplianceForm(doc as ComplianceDoc);
                              setShowComplianceModal(true);
                            } else if ((doc as ProductDocument).file) {
                              const url = URL.createObjectURL((doc as ProductDocument).file!);
                              window.open(url, '_blank');
                            }
                          }}
                        >
                          <i className={doc.type === 'compliance' ? 'ri-shield-check-line' : 'ri-file-text-line'}
                            style={{ fontSize: '16px', color: doc.type === 'compliance' ? 'var(--success)' : 'var(--primary)' }}
                          ></i>
                          <span style={{ fontSize: '13px', fontWeight: 500 }}>{doc.name}</span>
                          <span style={{
                            fontSize: '10px',
                            padding: '2px 6px',
                            borderRadius: '4px',
                            background: doc.type === 'compliance' ? 'rgba(16, 185, 129, 0.15)' : 'rgba(59, 130, 246, 0.15)',
                            color: doc.type === 'compliance' ? 'var(--success)' : 'var(--primary)',
                            fontWeight: 600,
                          }}>
                            {doc.type === 'compliance' ? 'COMPLIANCE' : 'DOCUMENT'}
                          </span>
                        </div>
                        <button
                          type="button"
                          onClick={() => setComplianceDocs(complianceDocs.filter(d => d.id !== doc.id))}
                          style={{ border: 'none', background: 'none', color: 'var(--danger)', cursor: 'pointer', padding: '4px' }}
                        >
                          <i className="ri-delete-bin-line"></i>
                        </button>
                      </div>
                    ))}
                  </div>
                )}

                {complianceDocs.length === 0 && (
                  <div style={{ textAlign: 'center', padding: '24px', color: 'var(--text-muted)', marginBottom: '16px' }}>
                    <i className="ri-shield-line" style={{ fontSize: '32px', marginBottom: '8px' }}></i>
                    <p style={{ fontSize: '13px' }}>No documents added yet</p>
                  </div>
                )}

                <div style={{ display: 'flex', gap: '12px' }}>
                  <button
                    type="button"
                    className="btn btn-secondary"
                    style={{ fontSize: '13px' }}
                    onClick={() => {
                      setComplianceForm({
                        id: `comp-${Date.now()}`, type: 'compliance', documentType: '', issuerLab: '', documentCode: '',
                        issueDate: '', expiryDate: '', brandOverride: '', modelOverride: '',
                        authorized: false, files: [], name: '',
                      });
                      setViewingDoc(null);
                      setShowComplianceModal(true);
                    }}
                  >
                    <i className="ri-shield-check-line" style={{ marginRight: '6px' }}></i>
                    Add Compliance
                  </button>
                  <button
                    type="button"
                    className="btn btn-secondary"
                    style={{ fontSize: '13px' }}
                    onClick={() => {
                      setDocumentForm({ name: '', file: null });
                      setShowDocumentModal(true);
                    }}
                  >
                    <i className="ri-file-add-line" style={{ marginRight: '6px' }}></i>
                    Add Document
                  </button>
                </div>
              </div>
            </div>
          </div>
        </div>

        {/* FULL-WIDTH SECTIONS */}

        {/* Media Upload Card - Full Width */}
        <div className="card" style={{ marginTop: 'var(--spacing-lg)', marginBottom: 'var(--spacing-lg)' }}>
          <div className="card-header">
            <h2 style={{ fontSize: '18px', fontWeight: 600 }}>Media</h2>
            <p style={{ fontSize: '13px', color: 'var(--text-muted)', marginTop: '4px' }}>
              Upload product images
            </p>
          </div>
          <div style={{ padding: 'var(--spacing-lg)' }}>
            <div
              onDragEnter={handleDrag}
              onDragLeave={handleDrag}
              onDragOver={handleDrag}
              onDrop={handleDrop}
              style={{
                border: `2px dashed ${dragActive ? 'var(--primary)' : 'var(--border)'}`,
                borderRadius: '12px',
                padding: '40px 20px',
                textAlign: 'center',
                cursor: 'pointer',
                backgroundColor: dragActive ? 'var(--bg-tertiary)' : 'var(--bg-secondary)',
                marginBottom: productImages.length > 0 ? '20px' : '0',
              }}
              onClick={() => document.getElementById('product-image-upload')?.click()}
            >
              <input
                id="product-image-upload"
                type="file"
                accept="image/*"
                multiple
                onChange={(e) => {
                  if (e.target.files) {
                    if (e.target.files.length === 1) {
                      handleImageUpload(e.target.files[0]);
                    } else {
                      handleMultipleImageUpload(e.target.files);
                    }
                  }
                }}
                style={{ display: 'none' }}
              />
              {uploadingImage ? (
                <div>
                  <div className="spinner" style={{ margin: '0 auto 16px', width: '40px', height: '40px', border: '4px solid var(--border)', borderTopColor: 'var(--primary)', borderRadius: '50%', animation: 'spin 1s linear infinite' }}></div>
                  <p style={{ color: 'var(--text-muted)' }}>Uploading...</p>
                </div>
              ) : (
                <>
                  <i className="ri-upload-cloud-line" style={{ fontSize: '48px', marginBottom: '16px', color: 'var(--text-muted)' }}></i>
                  <p style={{ color: 'var(--text-muted)', marginBottom: '8px' }}>
                    Drag and drop images, or click to browse
                  </p>
                  <p style={{ fontSize: '12px', color: 'var(--text-muted)' }}>
                    PNG, JPG up to 5MB • Select multiple images at once
                  </p>
                </>
              )}
            </div>

            {productImages.length > 0 && (
              <div>
                <p style={{ fontSize: '12px', color: 'var(--text-muted)', marginBottom: '12px' }}>
                  <i className="ri-information-line" style={{ marginRight: '4px' }}></i>
                  Drag images to reorder. First image is the main product image.
                </p>
                <div style={{ display: 'grid', gridTemplateColumns: 'repeat(auto-fill, minmax(120px, 1fr))', gap: '12px' }}>
                  {productImages.map((url, index) => (
                    <div
                      key={index}
                      draggable
                      onDragStart={() => handleImageDragStart(index)}
                      onDragOver={(e) => handleImageDragOver(e, index)}
                      onDragEnd={handleImageDragEnd}
                      style={{
                        position: 'relative',
                        paddingTop: '100%',
                        borderRadius: '8px',
                        overflow: 'hidden',
                        border: index === 0 ? '3px solid var(--primary)' : '1px solid var(--border)',
                        cursor: 'move',
                        opacity: draggedImageIndex === index ? 0.5 : 1,
                      }}
                    >
                      <img
                        src={url}
                        alt={`Product ${index + 1}`}
                        style={{
                          position: 'absolute',
                          top: 0,
                          left: 0,
                          width: '100%',
                          height: '100%',
                          objectFit: 'cover',
                        }}
                      />
                      {index === 0 && (
                        <div
                          style={{
                            position: 'absolute',
                            top: '8px',
                            left: '8px',
                            padding: '4px 8px',
                            backgroundColor: 'var(--primary)',
                            color: 'white',
                            fontSize: '10px',
                            fontWeight: '600',
                            borderRadius: '4px',
                          }}
                        >
                          MAIN
                        </div>
                      )}
                      <button
                        type="button"
                        onClick={() => handleRemoveImage(index)}
                        style={{
                          position: 'absolute',
                          top: '8px',
                          right: '8px',
                          width: '24px',
                          height: '24px',
                          borderRadius: '50%',
                          border: 'none',
                          backgroundColor: 'var(--danger)',
                          color: 'white',
                          cursor: 'pointer',
                          display: 'flex',
                          alignItems: 'center',
                          justifyContent: 'center',
                        }}
                      >
                        <i className="ri-close-line"></i>
                      </button>
                    </div>
                  ))}
                </div>
              </div>
            )}
          </div>
        </div>

        {/* VARIANTS SECTION - Full Width */}
        {productType === 'variant' && (
          <div className="card" style={{ marginBottom: 'var(--spacing-lg)' }}>
            <div className="card-header">
              <h2 style={{ fontSize: '18px', fontWeight: 600 }}>Variants</h2>
            </div>
            <div style={{ padding: 'var(--spacing-lg)' }}>
              {/* Variant Options — read-only for imported families, editable for manual */}
              {isImportedFamily ? (
                <div style={{ marginBottom: '16px' }}>
                  {variantOptions.map((option, index) => (
                    <div
                      key={index}
                      style={{ marginBottom: '8px', padding: '8px 12px', background: 'var(--bg-tertiary)', borderRadius: '6px', fontSize: '14px', display: 'flex', alignItems: 'center', gap: '12px' }}
                    >
                      <span style={{ fontWeight: 600, color: 'var(--text-muted)', minWidth: '80px', textTransform: 'capitalize' }}>{option.name}</span>
                      <div style={{ display: 'flex', flexWrap: 'wrap', gap: '6px' }}>
                        {option.values.map((val) => (
                          <span key={val} style={{ padding: '3px 10px', background: 'var(--bg-primary)', border: '1px solid var(--border)', borderRadius: '12px', fontSize: '13px', color: 'var(--text-primary)' }}>
                            {val}
                          </span>
                        ))}
                      </div>
                    </div>
                  ))}
                </div>
              ) : (
                <>
                  {/* Variant Options Input */}
                  <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr auto auto', gap: '12px', marginBottom: '16px' }}>
                    <div>
                      <div style={{ fontSize: '12px', color: 'var(--text-muted)', marginBottom: '4px', fontWeight: 500 }}>Option name</div>
                      <input
                        type="text"
                        className="input" style={{ width: '100%' }}
                        placeholder="e.g., Color"
                        value={currentOptionName}
                        onChange={(e) => setCurrentOptionName(e.target.value)}
                      />
                    </div>
                    <div>
                      <div style={{ fontSize: '12px', color: 'var(--text-muted)', marginBottom: '4px', fontWeight: 500 }}>Values (comma-separated)</div>
                      <input
                        type="text"
                        className="input" style={{ width: '100%' }}
                        placeholder="e.g., Red, Blue, Green"
                        value={currentOptionValues}
                        onChange={(e) => setCurrentOptionValues(e.target.value)}
                        onKeyPress={(e) => e.key === 'Enter' && handleAddOption()}
                      />
                    </div>
                    <div style={{ alignSelf: 'flex-end' }}>
                      <button type="button" className="btn btn-primary" onClick={handleAddOption}>Add</button>
                    </div>
                    <div style={{ alignSelf: 'flex-end' }}>
                      <button
                        type="button"
                        className="btn btn-secondary"
                        onClick={() => { setCurrentOptionName(''); setCurrentOptionValues(''); }}
                      >
                        Clear
                      </button>
                    </div>
                  </div>

                  {/* Current Options (editable) */}
                  {variantOptions.map((option, index) => (
                    <div
                      key={index}
                      style={{ marginBottom: '8px', padding: '8px 12px', background: 'var(--bg-tertiary)', borderRadius: '6px', fontSize: '14px', display: 'flex', justifyContent: 'space-between', alignItems: 'center' }}
                    >
                      <span>
                        <strong>{option.name}:</strong> {option.values.join(', ')}
                      </span>
                      <div>
                        <button
                          type="button"
                          onClick={() => handleRemoveOption(index)}
                          style={{ background: 'none', border: 'none', color: 'var(--danger)', cursor: 'pointer', padding: '4px 8px' }}
                        >
                          <i className="ri-delete-bin-line"></i>
                        </button>
                      </div>
                    </div>
                  ))}
                </>
              )}

              {variants.length > 0 && (
                <>
                  {/* Bulk Edit Controls */}
                  <div style={{ display: 'flex', gap: '12px', marginTop: '16px', marginBottom: '16px', alignItems: 'center', flexWrap: 'wrap' }}>
                    <span style={{ fontSize: '14px', color: 'var(--text-muted)' }}>
                      <strong>{selectedVariants.size}</strong> of <strong>{variants.length}</strong> variants selected
                    </span>
                    
                    <div style={{ borderLeft: '1px solid var(--border)', height: '32px' }}></div>
                    
                    <select className="select" name="bulk-edit-section" style={{ width: '220px' }}>
                      <option value="">Edit selected variants...</option>
                      <option value="basic">Basic Details</option>
                      <option value="dimensions">Dimensions</option>
                      <option value="inventory">Inventory</option>
                      <option value="shipping">Shipping & Customs</option>
                      <option value="compliance">Compliance</option>
                      <option value="media">Media</option>
                    </select>
                    
                    <button 
                      type="button" 
                      className="btn btn-primary"
                      onClick={handleOpenBulkEdit}
                      disabled={selectedVariants.size === 0}
                    >
                      <i className="ri-edit-box-line" style={{ marginRight: '8px' }}></i>
                      Edit Selected
                    </button>
                  </div>

                  {/* Variants Table */}
                  <div style={{ overflowX: 'auto' }}>
                    <table style={{ width: '100%', borderCollapse: 'collapse', fontSize: '14px' }}>
                      <thead>
                        <tr style={{ background: 'var(--bg-tertiary)', borderBottom: '1px solid var(--border)' }}>
                          <th style={{ padding: '12px', width: '40px' }}>
                            <input 
                              type="checkbox" 
                              checked={selectedVariants.size === variants.length && variants.length > 0}
                              onChange={toggleSelectAll}
                            />
                          </th>
                          <th style={{ padding: '12px', textAlign: 'left', fontSize: '12px', fontWeight: 600, color: 'var(--text-muted)' }}>Variant</th>
                          <th style={{ padding: '12px', textAlign: 'left', fontSize: '12px', fontWeight: 600, color: 'var(--text-muted)' }}>SKU</th>
                          <th style={{ padding: '12px', textAlign: 'left', fontSize: '12px', fontWeight: 600, color: 'var(--text-muted)' }}>Alias</th>
                          <th style={{ padding: '12px', textAlign: 'left', fontSize: '12px', fontWeight: 600, color: 'var(--text-muted)' }}>Stock</th>
                          <th style={{ padding: '12px', textAlign: 'left', fontSize: '12px', fontWeight: 600, color: 'var(--text-muted)' }}>Status</th>
                          <th style={{ padding: '12px', textAlign: 'left', fontSize: '12px', fontWeight: 600, color: 'var(--text-muted)' }}>Actions</th>
                        </tr>
                      </thead>
                      <tbody>
                        {variants.map((variant) => (
                          <tr key={variant.id} id={`variant-row-${variant.id}`} style={{
                            borderBottom: '1px solid var(--border)',
                            background: highlightVariantId === variant.id
                              ? 'rgba(0, 212, 255, 0.08)'
                              : undefined,
                            outline: highlightVariantId === variant.id
                              ? '2px solid var(--accent-cyan)'
                              : undefined,
                            outlineOffset: '-2px',
                          }}>
                            <td style={{ padding: '12px' }}>
                              <input 
                                type="checkbox" 
                                checked={selectedVariants.has(variant.id)}
                                onChange={() => toggleVariantSelection(variant.id)}
                              />
                            </td>
                            <td style={{ padding: '12px' }}>
                              <div style={{ display: 'flex', alignItems: 'center', gap: '8px' }}>
                                <div style={{ 
                                  width: '32px', 
                                  height: '32px', 
                                  background: getVariantColor(variant), 
                                  borderRadius: '6px', 
                                  border: '1px solid var(--border)' 
                                }}></div>
                                <span>{getVariantName(variant)}</span>
                              </div>
                            </td>
                            <td style={{ padding: '12px' }}>
                              <input 
                                type="text" 
                                className="input" 
                                placeholder="SKU" 
                                style={{ width: '120px' }}
                                value={variant.sku}
                                onChange={(e) => updateVariant(variant.id, 'sku', e.target.value)}
                              />
                            </td>
                            <td style={{ padding: '12px' }}>
                              <div style={{ display: 'flex', alignItems: 'center', gap: '6px' }}>
                                <input 
                                  type="text" 
                                  className="input" 
                                  placeholder="No alias" 
                                  style={{ width: '110px', fontSize: '13px', fontFamily: 'var(--font-mono, monospace)' }}
                                  value={variant.alias}
                                  readOnly
                                />
                                <button 
                                  type="button" 
                                  onClick={() => {
                                    setAliasTargetVariantId(variant.id);
                                    setShowAliasModal(true);
                                  }}
                                  style={{ 
                                    padding: '6px 8px', 
                                    background: 'var(--bg-tertiary)', 
                                    border: '1px solid var(--border)', 
                                    color: 'var(--primary)', 
                                    cursor: 'pointer', 
                                    borderRadius: '4px',
                                    fontSize: '12px',
                                    display: 'flex',
                                    alignItems: 'center',
                                    gap: '4px',
                                    whiteSpace: 'nowrap'
                                  }}
                                  title="Select product SKU to use as alias"
                                >
                                  <i className="ri-search-line"></i>
                                </button>
                                {variant.alias && (
                                  <button 
                                    type="button" 
                                    onClick={() => updateVariant(variant.id, 'alias', '')}
                                    style={{ 
                                      padding: '6px', 
                                      background: 'transparent', 
                                      border: 'none', 
                                      color: 'var(--text-muted)', 
                                      cursor: 'pointer', 
                                      borderRadius: '4px',
                                      fontSize: '12px'
                                    }}
                                    title="Clear alias"
                                  >
                                    <i className="ri-close-line"></i>
                                  </button>
                                )}
                              </div>
                            </td>
                            <td style={{ padding: '12px' }}>
                              <span style={{ fontSize: '14px', color: 'var(--text-muted)', padding: '0 4px' }}>0</span>
                            </td>
                            <td style={{ padding: '12px' }}>
                              <select 
                                className="select" 
                                style={{ width: '100px' }}
                                value={variant.status}
                                onChange={(e) => updateVariant(variant.id, 'status', e.target.value as any)}
                              >
                                <option>Active</option>
                                <option>Draft</option>
                                <option>Inactive</option>
                              </select>
                            </td>
                            <td style={{ padding: '12px' }}>
                              <div style={{ display: 'flex', gap: '4px' }}>
                                <button 
                                  type="button" 
                                  onClick={() => {
                                    setSelectedVariants(new Set([variant.id]));
                                    handleOpenBulkEdit();
                                  }}
                                  style={{ padding: '6px', background: 'transparent', border: 'none', color: 'var(--text-muted)', cursor: 'pointer', borderRadius: '4px' }}
                                >
                                  <i className="ri-edit-line"></i>
                                </button>
                                <button 
                                  type="button" 
                                  onClick={() => deleteVariant(variant.id)}
                                  style={{ padding: '6px', background: 'transparent', border: 'none', color: 'var(--danger)', cursor: 'pointer', borderRadius: '4px' }}
                                >
                                  <i className="ri-delete-bin-line"></i>
                                </button>
                              </div>
                            </td>
                          </tr>
                        ))}
                      </tbody>
                    </table>
                  </div>
                </>
              )}
            </div>
          </div>
        )}

        {/* Attributes Card - Full Width */}
        <div className="card" style={{ marginBottom: 'var(--spacing-lg)' }}>
          <div className="card-header" style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center' }}>
            <div>
              <h2 style={{ fontSize: '18px', fontWeight: 600 }}>Attributes</h2>
              <p style={{ fontSize: '13px', color: 'var(--text-muted)', marginTop: '4px' }}>
                Product attributes and custom fields
              </p>
            </div>
            <select
              value={selectedAttributeSet}
              onChange={(e) => handleAttributeSetChange(e.target.value)}
              className="select"
              style={{ width: '200px' }}
            >
              <option value="">Attribute Set</option>
              {attributeSets.map(set => (
                <option key={set.id} value={set.id}>{set.name}</option>
              ))}
            </select>
          </div>
          <div style={{ padding: 'var(--spacing-lg)' }}>
            <div style={{ display: 'flex', gap: '12px', marginBottom: '20px' }}>
              <button
                type="button"
                onClick={() => setShowAttributeModal(true)}
                className="btn btn-secondary"
              >
                <i className="ri-add-line" style={{ marginRight: '8px' }}></i>
                Add Existing Attribute
              </button>
              <button
                type="button"
                onClick={() => setShowCustomAttributeModal(true)}
                className="btn btn-secondary"
              >
                <i className="ri-add-line" style={{ marginRight: '8px' }}></i>
                Add Custom Attribute
              </button>
            </div>

            {productAttributes.length === 0 ? (
              <div style={{ textAlign: 'center', padding: '40px', color: 'var(--text-muted)' }}>
                <i className="ri-list-check" style={{ fontSize: '48px', marginBottom: '16px' }}></i>
                <p>No attributes added yet</p>
              </div>
            ) : (
              <div style={{ display: 'flex', flexDirection: 'column', gap: '12px' }}>
                {productAttributes.map(attr => (
                  <div
                    key={attr.id}
                    style={{
                      display: 'flex',
                      alignItems: 'center',
                      gap: '12px',
                      padding: '12px',
                      backgroundColor: 'var(--bg-tertiary)',
                      borderRadius: '8px',
                      border: '1px solid var(--border)',
                    }}
                  >
                    <div style={{ flex: '0 0 200px' }}>
                      <label style={{ fontSize: '13px', fontWeight: 600, color: 'var(--text-secondary)' }}>
                        {attr.name}
                        {attr.isCustom && (
                          <span style={{ marginLeft: '8px', fontSize: '11px', color: '#8b5cf6', fontWeight: 'normal' }}>
                            (Custom)
                          </span>
                        )}
                        {attr.fromSet && (
                          <span style={{ marginLeft: '8px', fontSize: '11px', color: 'var(--primary)', fontWeight: 'normal' }}>
                            (From Set)
                          </span>
                        )}
                      </label>
                    </div>
                    {attr.dataType === 'list' ? (
                      <select
                        value={attr.value}
                        onChange={(e) => handleUpdateAttributeValue(attr.id, e.target.value)}
                        className="input"
                        style={{ flex: 1 }}
                      >
                        <option value="">Select {attr.name}...</option>
                        {attr.options?.map(opt => (
                          <option key={opt.id} value={opt.value}>{opt.label}</option>
                        ))}
                      </select>
                    ) : attr.dataType === 'multiselect' ? (
                      <select
                        multiple
                        value={attr.value ? attr.value.split(',') : []}
                        onChange={(e) => {
                          const selected = Array.from(e.target.selectedOptions, option => option.value);
                          handleUpdateAttributeValue(attr.id, selected.join(','));
                        }}
                        className="input"
                        style={{ flex: 1, minHeight: '80px' }}
                      >
                        {attr.options?.map(opt => (
                          <option key={opt.id} value={opt.value}>{opt.label}</option>
                        ))}
                      </select>
                    ) : attr.dataType === 'boolean' ? (
                      <select
                        value={attr.value}
                        onChange={(e) => handleUpdateAttributeValue(attr.id, e.target.value)}
                        className="input"
                        style={{ flex: 1 }}
                      >
                        <option value="">Select...</option>
                        <option value="true">Yes</option>
                        <option value="false">No</option>
                      </select>
                    ) : attr.dataType === 'date' ? (
                      <input
                        type="date"
                        value={attr.value}
                        onChange={(e) => handleUpdateAttributeValue(attr.id, e.target.value)}
                        className="input"
                        style={{ flex: 1 }}
                      />
                    ) : attr.dataType === 'number' ? (
                      <input
                        type="number"
                        value={attr.value}
                        onChange={(e) => handleUpdateAttributeValue(attr.id, e.target.value)}
                        className="input"
                        placeholder="Enter number..."
                        style={{ flex: 1 }}
                      />
                    ) : (
                      <input
                        type="text"
                        value={attr.value}
                        onChange={(e) => handleUpdateAttributeValue(attr.id, e.target.value)}
                        className="input"
                        placeholder="Enter value..."
                        style={{ flex: 1 }}
                      />
                    )}
                    <button
                      type="button"
                      onClick={() => handleDeleteAttribute(attr.id)}
                      style={{
                        border: 'none',
                        background: 'none',
                        color: 'var(--danger)',
                        cursor: 'pointer',
                        padding: '8px',
                      }}
                    >
                      <i className="ri-delete-bin-line"></i>
                    </button>
                  </div>
                ))}
              </div>
            )}
          </div>
        </div>

        {/* Error Banner */}
        {saveError && (
          <div style={{
            marginTop: '16px',
            padding: '12px 16px',
            background: 'rgba(239, 68, 68, 0.1)',
            border: '1px solid rgba(239, 68, 68, 0.3)',
            borderRadius: 'var(--radius-md)',
            color: '#ef4444',
            fontSize: '14px',
            display: 'flex',
            alignItems: 'center',
            gap: '8px',
          }}>
            <i className="ri-error-warning-line" style={{ fontSize: '18px' }}></i>
            {saveError}
            <button type="button" onClick={() => setSaveError(null)} style={{ marginLeft: 'auto', border: 'none', background: 'none', color: '#ef4444', cursor: 'pointer' }}>
              <i className="ri-close-line"></i>
            </button>
          </div>
        )}

        {/* Submit Buttons */}
        <div style={{ display: 'flex', justifyContent: 'flex-end', gap: '12px', marginTop: '24px' }}>
          {isEditMode && editProductId && (
            <button
              type="button"
              className="btn btn-danger"
              onClick={() => setShowDeleteModal(true)}
              style={{ marginRight: 'auto' }}
            >
              <i className="ri-delete-bin-line"></i> Delete Product
            </button>
          )}
          <button type="button" className="btn btn-secondary" onClick={() => setShowCancelModal(true)}>Cancel</button>
          <button type="submit" className="btn btn-primary" disabled={saving}>
            {saving ? (
              <><span className="spinner" style={{ width: '16px', height: '16px', border: '2px solid rgba(255,255,255,0.3)', borderTopColor: 'white', borderRadius: '50%', animation: 'spin 1s linear infinite', display: 'inline-block', marginRight: '8px' }}></span>Saving...</>
            ) : isEditMode ? 'Save Changes' : 'Create Product'}
          </button>
        </div>
      </form>
      )}

      {/* Bundle Items Section - Only show for bundle products */}
      {productType === 'bundle' && (
        <BundleItemsSection
          items={bundleItems}
          onChange={setBundleItems}
        />
      )}

      {/* Alias Product Search Modal - For variant alias selection */}
      <ProductSearchModal
        show={showAliasModal}
        onClose={() => { setShowAliasModal(false); setAliasTargetVariantId(null); }}
        onAddItems={handleAliasSelect}
        excludeIds={[]}
        title="Select Alias Product"
        confirmLabel="Set as Alias"
        singleSelect
      />

      {/* BULK EDIT MODAL */}
      {showBulkEditModal && (
        <div style={{
          position: 'fixed',
          inset: 0,
          background: 'rgba(0, 0, 0, 0.6)',
          display: 'flex',
          alignItems: 'center',
          justifyContent: 'center',
          zIndex: 1000,
          backdropFilter: 'blur(4px)'
        }}>
          <div style={{
            background: 'var(--bg-secondary)',
            borderRadius: '12px',
            width: '90%',
            maxWidth: '800px',
            maxHeight: '90vh',
            overflow: 'hidden',
            display: 'flex',
            flexDirection: 'column',
            border: '1px solid var(--border)'
          }}>
            {/* Modal Header */}
            <div style={{
              padding: 'var(--spacing-lg)',
              borderBottom: '1px solid var(--border)',
              display: 'flex',
              justifyContent: 'space-between',
              alignItems: 'center'
            }}>
              <div>
                <h2 style={{ fontSize: '18px', fontWeight: 600 }}>
                  Edit {bulkEditSection === 'basic' ? 'Basic Details' :
                       bulkEditSection === 'dimensions' ? 'Dimensions' :
                       bulkEditSection === 'inventory' ? 'Inventory' :
                       bulkEditSection === 'shipping' ? 'Shipping & Customs' :
                       bulkEditSection === 'compliance' ? 'Compliance' :
                       bulkEditSection === 'media' ? 'Media' : 'Details'}
                </h2>
                <p style={{ fontSize: '13px', color: 'var(--text-muted)', marginTop: '4px' }}>
                  Editing {selectedVariants.size} variant{selectedVariants.size > 1 ? 's' : ''}
                </p>
              </div>
              <button
                type="button"
                onClick={() => setShowBulkEditModal(false)}
                style={{
                  background: 'none',
                  border: 'none',
                  color: 'var(--text-muted)',
                  cursor: 'pointer',
                  fontSize: '24px',
                  padding: '4px'
                }}
              >
                <i className="ri-close-line"></i>
              </button>
            </div>

            {/* Modal Body - Scrollable */}
            <div style={{
              padding: 'var(--spacing-lg)',
              overflowY: 'auto',
              flex: 1
            }}>
              {/* Basic Details Form */}
              {bulkEditSection === 'basic' && (
                <div style={{ display: 'flex', flexDirection: 'column', gap: '12px' }}>
                  <div>
                    <label className="block text-sm font-medium mb-1.5">Product Title</label>
                    <input type="text" className="input" style={{ width: '100%' }} placeholder="Enter product title" />
                  </div>
                  <div>
                    <label className="block text-sm font-medium mb-1.5">Brand</label>
                    <input type="text" className="input" style={{ width: '100%' }} placeholder="Enter brand name" />
                  </div>
                  <div>
                    <label className="block text-sm font-medium mb-1.5">Description</label>
                    <textarea className="input" style={{ width: '100%' }} rows={4} placeholder="Enter description"></textarea>
                  </div>
                  <div>
                    <label className="block text-sm font-medium mb-1.5">Product Identifier</label>
                    <input type="text" className="input" style={{ width: '100%' }} placeholder="Enter identifier" />
                  </div>
                  <div>
                    <label className="block text-sm font-medium mb-1.5">Identifier Type</label>
                    <select className="select" style={{ width: '100%' }}>
                      <option>EAN</option>
                      <option>UPC</option>
                      <option>GTIN</option>
                      <option>ISBN</option>
                      <option>ASIN</option>
                    </select>
                  </div>
                </div>
              )}

              {/* Dimensions Form */}
              {bulkEditSection === 'dimensions' && (
                <div style={{ display: 'flex', flexDirection: 'column', gap: '16px' }}>
                  <div>
                    <div style={{ fontSize: '14px', fontWeight: 500, marginBottom: '12px' }}>Product Dimensions</div>
                    <div style={{ display: 'grid', gridTemplateColumns: 'repeat(4, 1fr)', gap: '8px', marginBottom: '8px' }}>
                      <div>
                        <label className="block text-xs mb-1">Width</label>
                        <input type="number" className="input" style={{ width: '100%' }} placeholder="0.00" step="0.01" />
                      </div>
                      <div>
                        <label className="block text-xs mb-1">Height</label>
                        <input type="number" className="input" style={{ width: '100%' }} placeholder="0.00" step="0.01" />
                      </div>
                      <div>
                        <label className="block text-xs mb-1">Length</label>
                        <input type="number" className="input" style={{ width: '100%' }} placeholder="0.00" step="0.01" />
                      </div>
                      <div>
                        <label className="block text-xs mb-1">Unit</label>
                        <select className="select" style={{ width: '100%' }}>
                          <option>Inches</option>
                          <option>Centimeters</option>
                        </select>
                      </div>
                    </div>
                    <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: '8px' }}>
                      <div>
                        <label className="block text-xs mb-1">Weight</label>
                        <input type="number" className="input" style={{ width: '100%' }} placeholder="0.00" step="0.01" />
                      </div>
                      <div>
                        <label className="block text-xs mb-1">Weight Unit</label>
                        <select className="select" style={{ width: '100%' }}>
                          <option>Pounds</option>
                          <option>Kilograms</option>
                        </select>
                      </div>
                    </div>
                  </div>
                  <div>
                    <div style={{ fontSize: '14px', fontWeight: 500, marginBottom: '12px' }}>Shipping Dimensions</div>
                    <div style={{ display: 'grid', gridTemplateColumns: 'repeat(4, 1fr)', gap: '8px', marginBottom: '8px' }}>
                      <div>
                        <label className="block text-xs mb-1">Width</label>
                        <input type="number" className="input" style={{ width: '100%' }} placeholder="0.00" step="0.01" />
                      </div>
                      <div>
                        <label className="block text-xs mb-1">Height</label>
                        <input type="number" className="input" style={{ width: '100%' }} placeholder="0.00" step="0.01" />
                      </div>
                      <div>
                        <label className="block text-xs mb-1">Length</label>
                        <input type="number" className="input" style={{ width: '100%' }} placeholder="0.00" step="0.01" />
                      </div>
                      <div>
                        <label className="block text-xs mb-1">Unit</label>
                        <select className="select" style={{ width: '100%' }}>
                          <option>Inches</option>
                          <option>Centimeters</option>
                        </select>
                      </div>
                    </div>
                    <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: '8px' }}>
                      <div>
                        <label className="block text-xs mb-1">Weight</label>
                        <input type="number" className="input" style={{ width: '100%' }} placeholder="0.00" step="0.01" />
                      </div>
                      <div>
                        <label className="block text-xs mb-1">Weight Unit</label>
                        <select className="select" style={{ width: '100%' }}>
                          <option>Pounds</option>
                          <option>Kilograms</option>
                        </select>
                      </div>
                    </div>
                  </div>
                </div>
              )}

              {/* Inventory Form */}
              {bulkEditSection === 'inventory' && (
                <div style={{ display: 'flex', flexDirection: 'column', gap: '12px' }}>
                  <div>
                    <label className="block text-sm font-medium mb-1.5">Stock Quantity</label>
                    <input type="number" className="input" style={{ width: '100%' }} placeholder="0" />
                  </div>
                  <div>
                    <label className="block text-sm font-medium mb-1.5">Warehouse Location</label>
                    <input type="text" className="input" style={{ width: '100%' }} placeholder="Enter location" />
                  </div>
                </div>
              )}

              {/* Shipping Form */}
              {bulkEditSection === 'shipping' && (
                <div style={{ display: 'flex', flexDirection: 'column', gap: '12px' }}>
                  <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: '12px' }}>
                    <div>
                      <label className="block text-sm font-medium mb-1.5">Shipping Method</label>
                      <select className="select" style={{ width: '100%' }}>
                        <option>Standard</option>
                        <option>Express</option>
                        <option>Priority</option>
                      </select>
                    </div>
                    <div>
                      <label className="block text-sm font-medium mb-1.5">Declared Value</label>
                      <input type="number" className="input" style={{ width: '100%' }} placeholder="0.00" step="0.01" />
                    </div>
                  </div>
                  <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: '12px' }}>
                    <div>
                      <label className="block text-sm font-medium mb-1.5">HS Code</label>
                      <input type="text" className="input" style={{ width: '100%' }} placeholder="Enter HS Code" />
                    </div>
                    <div>
                      <label className="block text-sm font-medium mb-1.5">Country of Origin</label>
                      <select className="select" style={{ width: '100%' }}>
                        <option>Select country</option>
                        <option>United Kingdom</option>
                        <option>United States</option>
                        <option>China</option>
                      </select>
                    </div>
                  </div>
                  <div>
                    <label className="block text-sm font-medium mb-1.5">Customs Description</label>
                    <textarea className="input" style={{ width: '100%' }} rows={3} placeholder="Enter description"></textarea>
                  </div>
                </div>
              )}

              {/* Compliance Form */}
              {bulkEditSection === 'compliance' && (
                <div style={{ display: 'flex', flexDirection: 'column', gap: '12px' }}>
                  <div>
                    <label className="block text-sm font-medium mb-1.5">Document Name</label>
                    <input type="text" className="input" style={{ width: '100%' }} placeholder="Enter document name" />
                  </div>
                  <div>
                    <label className="block text-sm font-medium mb-1.5">Upload Document</label>
                    <input type="file" className="w-full" />
                  </div>
                </div>
              )}

              {/* Media Form */}
              {bulkEditSection === 'media' && (
                <div>
                  <div style={{ fontSize: '12px', color: 'var(--text-muted)', marginBottom: '12px' }}>
                    Images inherited from master product. Reorder, remove, or add images for this variant.
                  </div>

                  {/* Existing images from master product */}
                  {productImages.length > 0 && (
                    <div style={{ marginBottom: '16px' }}>
                      <div style={{ fontSize: '12px', color: 'var(--text-muted)', marginBottom: '8px' }}>
                        <i className="ri-information-line" style={{ marginRight: '4px' }}></i>
                        Drag to reorder. First image is the main image for this variant.
                      </div>
                      <div style={{ display: 'grid', gridTemplateColumns: 'repeat(auto-fill, minmax(100px, 1fr))', gap: '10px' }}>
                        {productImages.map((url, index) => (
                          <div
                            key={index}
                            draggable
                            onDragStart={() => handleImageDragStart(index)}
                            onDragOver={(e) => handleImageDragOver(e, index)}
                            onDragEnd={handleImageDragEnd}
                            style={{
                              position: 'relative',
                              paddingTop: '100%',
                              borderRadius: '8px',
                              overflow: 'hidden',
                              border: index === 0 ? '3px solid var(--primary)' : '1px solid var(--border)',
                              cursor: 'move',
                              opacity: draggedImageIndex === index ? 0.5 : 1,
                            }}
                          >
                            <img
                              src={url}
                              alt={`Image ${index + 1}`}
                              style={{
                                position: 'absolute',
                                top: 0,
                                left: 0,
                                width: '100%',
                                height: '100%',
                                objectFit: 'cover',
                              }}
                            />
                            {index === 0 && (
                              <div
                                style={{
                                  position: 'absolute',
                                  top: '4px',
                                  left: '4px',
                                  padding: '2px 6px',
                                  backgroundColor: 'var(--primary)',
                                  color: 'white',
                                  fontSize: '9px',
                                  fontWeight: '600',
                                  borderRadius: '3px',
                                }}
                              >
                                MAIN
                              </div>
                            )}
                            <button
                              type="button"
                              onClick={() => handleRemoveImage(index)}
                              style={{
                                position: 'absolute',
                                top: '4px',
                                right: '4px',
                                width: '20px',
                                height: '20px',
                                borderRadius: '50%',
                                border: 'none',
                                backgroundColor: 'var(--danger)',
                                color: 'white',
                                cursor: 'pointer',
                                display: 'flex',
                                alignItems: 'center',
                                justifyContent: 'center',
                                fontSize: '12px',
                              }}
                            >
                              <i className="ri-close-line"></i>
                            </button>
                          </div>
                        ))}
                      </div>
                    </div>
                  )}

                  {productImages.length === 0 && (
                    <div style={{ textAlign: 'center', padding: '20px', color: 'var(--text-muted)', marginBottom: '16px' }}>
                      <i className="ri-image-line" style={{ fontSize: '32px', marginBottom: '8px' }}></i>
                      <p style={{ fontSize: '13px' }}>No master product images. Upload images below.</p>
                    </div>
                  )}

                  {/* Upload additional images */}
                  <div
                    style={{
                      border: '2px dashed var(--border)',
                      borderRadius: '8px',
                      padding: '24px',
                      textAlign: 'center',
                      cursor: 'pointer',
                      backgroundColor: 'var(--bg-secondary)',
                    }}
                    onClick={() => document.getElementById('variant-image-upload')?.click()}
                  >
                    <input
                      id="variant-image-upload"
                      type="file"
                      accept="image/*"
                      multiple
                      onChange={(e) => {
                        if (e.target.files) {
                          if (e.target.files.length === 1) {
                            handleImageUpload(e.target.files[0]);
                          } else {
                            handleMultipleImageUpload(e.target.files);
                          }
                        }
                      }}
                      style={{ display: 'none' }}
                    />
                    <i className="ri-add-circle-line" style={{ fontSize: '28px', color: 'var(--text-muted)', marginBottom: '8px' }}></i>
                    <div style={{ color: 'var(--text-muted)', fontSize: '13px' }}>
                      Click to add more images for this variant
                    </div>
                  </div>
                </div>
              )}
            </div>

            {/* Modal Footer */}
            <div style={{
              padding: 'var(--spacing-lg)',
              borderTop: '1px solid var(--border)',
              display: 'flex',
              justifyContent: 'flex-end',
              gap: '12px'
            }}>
              <button 
                type="button" 
                className="btn btn-secondary"
                onClick={() => setShowBulkEditModal(false)}
              >
                Cancel
              </button>
              <button 
                type="button" 
                className="btn btn-primary"
                onClick={() => {
                  // Apply changes to selected variants
                  setShowBulkEditModal(false);
                }}
              >
                Apply to Selected Variants
              </button>
            </div>
          </div>
        </div>
      )}

      {/* Cancel Confirmation Modal */}
      {showCancelModal && (
        <div style={{
          position: 'fixed',
          inset: 0,
          background: 'rgba(0, 0, 0, 0.6)',
          display: 'flex',
          alignItems: 'center',
          justifyContent: 'center',
          zIndex: 1000,
          backdropFilter: 'blur(4px)'
        }}>
          <div style={{
            background: 'var(--bg-secondary)',
            borderRadius: 'var(--radius-xl)',
            border: '1px solid var(--border)',
            padding: 'var(--spacing-xl)',
            width: '100%',
            maxWidth: '420px',
            textAlign: 'center',
          }}>
            <i className="ri-error-warning-line" style={{ fontSize: '48px', color: 'var(--warning)', marginBottom: '16px' }}></i>
            <h3 style={{ fontSize: '18px', fontWeight: 600, marginBottom: '8px' }}>Discard Changes?</h3>
            <p style={{ fontSize: '14px', color: 'var(--text-muted)', marginBottom: '24px' }}>
              Are you sure you want to cancel? All unsaved changes will be lost.
            </p>
            <div style={{ display: 'flex', gap: '12px', justifyContent: 'center' }}>
              <button
                type="button"
                className="btn btn-secondary"
                onClick={() => setShowCancelModal(false)}
              >
                Keep Editing
              </button>
              <button
                type="button"
                className="btn btn-primary"
                style={{ background: 'var(--danger)', borderColor: 'var(--danger)' }}
                onClick={async () => {
                  // If this product was created via AI lookup and never manually saved, delete it
                  const aiDraftId = aiDraftProductId || sessionStorage.getItem('ai_draft_product_id');
                  const isAIDraft = aiDraftId && (editProductId === aiDraftId || !isEditMode);
                  if (isAIDraft && aiDraftId) {
                    try { await productService.delete(aiDraftId); } catch { /* non-fatal */ }
                    sessionStorage.removeItem('ai_draft_product_id');
                    setAiDraftProductId(null);
                  }
                  navigate('/products');
                }}
              >
                Discard & Leave
              </button>
            </div>
          </div>
        </div>
      )}

      {/* Delete Product Modal */}
      {showDeleteModal && (
        <div style={{
          position: 'fixed',
          inset: 0,
          background: 'rgba(0, 0, 0, 0.6)',
          display: 'flex',
          alignItems: 'center',
          justifyContent: 'center',
          zIndex: 1000,
          backdropFilter: 'blur(4px)'
        }}>
          <div style={{
            background: 'var(--bg-secondary)',
            borderRadius: 'var(--radius-xl)',
            border: '1px solid var(--border)',
            padding: 'var(--spacing-xl)',
            width: '100%',
            maxWidth: '420px',
            textAlign: 'center',
          }}>
            <i className="ri-delete-bin-line" style={{ fontSize: '48px', color: 'var(--danger)', marginBottom: '16px' }}></i>
            <h3 style={{ fontSize: '18px', fontWeight: 600, marginBottom: '8px' }}>Delete Product?</h3>
            <p style={{ fontSize: '14px', color: 'var(--text-muted)', marginBottom: '8px' }}>
              This will permanently delete <strong>{title || 'this product'}</strong> and all its variants.
            </p>
            <p style={{ fontSize: '13px', color: 'var(--danger)', marginBottom: '24px' }}>
              This action cannot be undone.
            </p>
            <div style={{ display: 'flex', gap: '12px', justifyContent: 'center' }}>
              <button
                type="button"
                className="btn btn-secondary"
                onClick={() => setShowDeleteModal(false)}
                disabled={deleting}
              >
                Cancel
              </button>
              <button
                type="button"
                className="btn btn-danger"
                disabled={deleting}
                onClick={async () => {
                  setDeleting(true);
                  try {
                    // Delete all variants first
                    if (productType === 'variant' && variants.length > 0) {
                      for (const v of variants) {
                        if (v.id && !v.id.startsWith('variant-') && !v.id.startsWith('var-')) {
                          try {
                            await variantService.delete(v.id);
                          } catch (e) {
                            console.error('Failed to delete variant:', v.id, e);
                          }
                        }
                      }
                    }
                    // Delete the product
                    await productService.delete(editProductId!);
                    setIsDirty(false);
                    navigate('/products');
                  } catch (err: any) {
                    console.error('Delete failed:', err);
                    setSaveError(err?.response?.data?.error || 'Failed to delete product');
                    setShowDeleteModal(false);
                  } finally {
                    setDeleting(false);
                  }
                }}
              >
                {deleting ? (
                  <><span style={{ width: '16px', height: '16px', border: '2px solid rgba(255,255,255,0.3)', borderTopColor: 'white', borderRadius: '50%', animation: 'spin 1s linear infinite', display: 'inline-block', marginRight: '8px' }}></span>Deleting...</>
                ) : (
                  <><i className="ri-delete-bin-line"></i> Delete Permanently</>
                )}
              </button>
            </div>
          </div>
        </div>
      )}

      {/* Unsaved Changes Modal */}
      {showUnsavedModal && (
        <div style={{
          position: 'fixed', inset: 0, background: 'rgba(0, 0, 0, 0.6)',
          display: 'flex', alignItems: 'center', justifyContent: 'center',
          zIndex: 1001, backdropFilter: 'blur(4px)'
        }}>
          <div style={{
            background: 'var(--bg-secondary)', borderRadius: 'var(--radius-xl)',
            border: '1px solid var(--border)', padding: 'var(--spacing-xl)',
            width: '100%', maxWidth: '440px', textAlign: 'center',
          }}>
            <i className="ri-save-line" style={{ fontSize: '48px', color: 'var(--warning)', marginBottom: '16px' }}></i>
            <h3 style={{ fontSize: '18px', fontWeight: 600, marginBottom: '8px' }}>You have unsaved changes</h3>
            <p style={{ fontSize: '14px', color: 'var(--text-muted)', marginBottom: '24px' }}>
              Would you like to save your changes before leaving?
            </p>
            <div style={{ display: 'flex', gap: '12px', justifyContent: 'center' }}>
              <button type="button" className="btn btn-secondary" onClick={handleUnsavedCancel}>
                Keep Editing
              </button>
              <button type="button" className="btn btn-primary" style={{ background: 'var(--danger)', borderColor: 'var(--danger)' }} onClick={handleUnsavedDiscard}>
                Discard Changes
              </button>
              <button type="button" className="btn btn-primary" onClick={handleUnsavedSave}>
                Save & Leave
              </button>
            </div>
          </div>
        </div>
      )}

      {/* Add Compliance Document Modal */}
      {showComplianceModal && (
        <div style={{
          position: 'fixed', inset: 0, background: 'rgba(0, 0, 0, 0.6)',
          display: 'flex', alignItems: 'center', justifyContent: 'center', zIndex: 1000, backdropFilter: 'blur(4px)',
        }}>
          <div style={{
            background: 'var(--bg-secondary)', borderRadius: 'var(--radius-xl)', border: '1px solid var(--border)',
            width: '100%', maxWidth: '560px', maxHeight: '90vh', overflow: 'auto',
          }}>
            <div style={{ padding: 'var(--spacing-lg)', borderBottom: '1px solid var(--border)', display: 'flex', justifyContent: 'space-between', alignItems: 'center' }}>
              <h3 style={{ fontSize: '18px', fontWeight: 600 }}>{viewingDoc ? 'Compliance Document' : 'Add Compliance Document'}</h3>
              <button type="button" onClick={() => setShowComplianceModal(false)} style={{ border: 'none', background: 'none', color: 'var(--text-muted)', cursor: 'pointer', fontSize: '20px' }}>
                <i className="ri-close-line"></i>
              </button>
            </div>
            <div style={{ padding: 'var(--spacing-lg)', display: 'flex', flexDirection: 'column', gap: '14px' }}>
              <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: '12px' }}>
                <div>
                  <div style={{ fontSize: '12px', color: 'var(--text-muted)', marginBottom: '4px', fontWeight: 500 }}>Document Type <span style={{ color: 'var(--danger)' }}>*</span></div>
                  <input type="text" className="input" style={{ width: '100%' }} placeholder="e.g., CE Certificate, FDA Approval"
                    value={complianceForm.documentType} onChange={(e) => setComplianceForm({ ...complianceForm, documentType: e.target.value })} />
                </div>
                <div>
                  <div style={{ fontSize: '12px', color: 'var(--text-muted)', marginBottom: '4px', fontWeight: 500 }}>Issuer/Lab <span style={{ color: 'var(--danger)' }}>*</span></div>
                  <input type="text" className="input" style={{ width: '100%' }} placeholder="e.g., TÜV SÜD, SGS"
                    value={complianceForm.issuerLab} onChange={(e) => setComplianceForm({ ...complianceForm, issuerLab: e.target.value })} />
                </div>
              </div>
              <div>
                <div style={{ fontSize: '12px', color: 'var(--text-muted)', marginBottom: '4px', fontWeight: 500 }}>Document Code <span style={{ color: 'var(--danger)' }}>*</span></div>
                <input type="text" className="input" style={{ width: '100%' }} placeholder="e.g., CE-2024-12345"
                  value={complianceForm.documentCode} onChange={(e) => setComplianceForm({ ...complianceForm, documentCode: e.target.value })} />
              </div>
              <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: '12px' }}>
                <div>
                  <div style={{ fontSize: '12px', color: 'var(--text-muted)', marginBottom: '4px', fontWeight: 500 }}>Issue Date</div>
                  <input type="date" className="input" style={{ width: '100%' }}
                    value={complianceForm.issueDate} onChange={(e) => setComplianceForm({ ...complianceForm, issueDate: e.target.value })} />
                </div>
                <div>
                  <div style={{ fontSize: '12px', color: 'var(--text-muted)', marginBottom: '4px', fontWeight: 500 }}>Expiry Date</div>
                  <input type="date" className="input" style={{ width: '100%' }}
                    value={complianceForm.expiryDate} onChange={(e) => setComplianceForm({ ...complianceForm, expiryDate: e.target.value })} />
                </div>
              </div>
              <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: '12px' }}>
                <div>
                  <div style={{ fontSize: '12px', color: 'var(--text-muted)', marginBottom: '4px', fontWeight: 500 }}>Brand Override</div>
                  <input type="text" className="input" style={{ width: '100%' }} placeholder="Optional"
                    value={complianceForm.brandOverride} onChange={(e) => setComplianceForm({ ...complianceForm, brandOverride: e.target.value })} />
                </div>
                <div>
                  <div style={{ fontSize: '12px', color: 'var(--text-muted)', marginBottom: '4px', fontWeight: 500 }}>Model Override</div>
                  <input type="text" className="input" style={{ width: '100%' }} placeholder="Optional"
                    value={complianceForm.modelOverride} onChange={(e) => setComplianceForm({ ...complianceForm, modelOverride: e.target.value })} />
                </div>
              </div>
              <div style={{ display: 'flex', alignItems: 'center', gap: '8px' }}>
                <input type="checkbox" id="compliance-auth" checked={complianceForm.authorized}
                  onChange={(e) => setComplianceForm({ ...complianceForm, authorized: e.target.checked })} />
                <label htmlFor="compliance-auth" style={{ fontSize: '13px', color: 'var(--text-secondary)' }}>I have authorization to use this document</label>
              </div>
              <div>
                <div style={{ fontSize: '12px', color: 'var(--text-muted)', marginBottom: '4px', fontWeight: 500 }}>Supporting Documents</div>
                <input type="file" multiple onChange={(e) => {
                  if (e.target.files) setComplianceForm({ ...complianceForm, files: Array.from(e.target.files) });
                }} />
              </div>
            </div>
            <div style={{ padding: 'var(--spacing-lg)', borderTop: '1px solid var(--border)', display: 'flex', gap: '12px', justifyContent: 'flex-end' }}>
              <button type="button" className="btn btn-secondary" onClick={() => setShowComplianceModal(false)}>Cancel</button>
              <button type="button" className="btn btn-primary" onClick={() => {
                if (!complianceForm.documentType || !complianceForm.issuerLab || !complianceForm.documentCode) return;
                const docName = `${complianceForm.documentType} - ${complianceForm.documentCode}`;
                const newDoc: ComplianceDoc = { ...complianceForm, name: docName, id: complianceForm.id || `comp-${Date.now()}` };
                if (viewingDoc) {
                  setComplianceDocs(complianceDocs.map(d => d.id === viewingDoc.id ? newDoc : d));
                } else {
                  setComplianceDocs([...complianceDocs, newDoc]);
                }
                setShowComplianceModal(false);
              }}>
                {viewingDoc ? 'Update Document' : 'Add Document'}
              </button>
            </div>
          </div>
        </div>
      )}

      {/* Add Simple Document Modal */}
      {showDocumentModal && (
        <div style={{
          position: 'fixed', inset: 0, background: 'rgba(0, 0, 0, 0.6)',
          display: 'flex', alignItems: 'center', justifyContent: 'center', zIndex: 1000, backdropFilter: 'blur(4px)',
        }}>
          <div style={{
            background: 'var(--bg-secondary)', borderRadius: 'var(--radius-xl)', border: '1px solid var(--border)',
            width: '100%', maxWidth: '420px',
          }}>
            <div style={{ padding: 'var(--spacing-lg)', borderBottom: '1px solid var(--border)', display: 'flex', justifyContent: 'space-between', alignItems: 'center' }}>
              <h3 style={{ fontSize: '18px', fontWeight: 600 }}>Add Document</h3>
              <button type="button" onClick={() => setShowDocumentModal(false)} style={{ border: 'none', background: 'none', color: 'var(--text-muted)', cursor: 'pointer', fontSize: '20px' }}>
                <i className="ri-close-line"></i>
              </button>
            </div>
            <div style={{ padding: 'var(--spacing-lg)', display: 'flex', flexDirection: 'column', gap: '14px' }}>
              <div>
                <div style={{ fontSize: '12px', color: 'var(--text-muted)', marginBottom: '4px', fontWeight: 500 }}>Document Name <span style={{ color: 'var(--danger)' }}>*</span></div>
                <input type="text" className="input" style={{ width: '100%' }} placeholder="Enter document name"
                  value={documentForm.name} onChange={(e) => setDocumentForm({ ...documentForm, name: e.target.value })} />
              </div>
              <div>
                <div style={{ fontSize: '12px', color: 'var(--text-muted)', marginBottom: '4px', fontWeight: 500 }}>File</div>
                <input type="file" onChange={(e) => {
                  if (e.target.files?.[0]) setDocumentForm({ ...documentForm, file: e.target.files[0] });
                }} />
              </div>
            </div>
            <div style={{ padding: 'var(--spacing-lg)', borderTop: '1px solid var(--border)', display: 'flex', gap: '12px', justifyContent: 'flex-end' }}>
              <button type="button" className="btn btn-secondary" onClick={() => setShowDocumentModal(false)}>Cancel</button>
              <button type="button" className="btn btn-primary" onClick={() => {
                if (!documentForm.name) return;
                const newDoc: ProductDocument = { id: `doc-${Date.now()}`, type: 'document', name: documentForm.name, file: documentForm.file };
                setComplianceDocs([...complianceDocs, newDoc]);
                setShowDocumentModal(false);
              }}>
                Add Document
              </button>
            </div>
          </div>
        </div>
      )}

      {/* Add Existing Attribute Modal */}
      {showAttributeModal && (
        <div
          style={{
            position: 'fixed',
            top: 0,
            left: 0,
            right: 0,
            bottom: 0,
            backgroundColor: 'rgba(0, 0, 0, 0.7)',
            zIndex: 1000,
            display: 'flex',
            alignItems: 'center',
            justifyContent: 'center'
          }}
          onClick={() => setShowAttributeModal(false)}
        >
          <div
            style={{
              backgroundColor: 'var(--bg-secondary)',
              borderRadius: '12px',
              width: '500px',
              maxHeight: '600px',
              display: 'flex',
              flexDirection: 'column',
              border: '1px solid var(--border)',
            }}
            onClick={(e) => e.stopPropagation()}
          >
            <div
              style={{
                padding: '20px',
                borderBottom: '1px solid var(--border)',
                display: 'flex',
                justifyContent: 'space-between',
                alignItems: 'center',
              }}
            >
              <h3 style={{ fontSize: '18px', fontWeight: 600 }}>Add Existing Attribute</h3>
              <button
                onClick={() => setShowAttributeModal(false)}
                style={{
                  background: 'none',
                  border: 'none',
                  color: 'var(--text-muted)',
                  fontSize: '24px',
                  cursor: 'pointer',
                  padding: '4px'
                }}
              >
                ×
              </button>
            </div>
            <div style={{ padding: '20px', overflowY: 'auto', flex: 1 }}>
              {allAttributes.length === 0 ? (
                <p style={{ textAlign: 'center', color: 'var(--text-muted)' }}>No attributes available</p>
              ) : (
                <div style={{ display: 'flex', flexDirection: 'column', gap: '8px' }}>
                  {allAttributes.map(attr => (
                    <button
                      key={attr.id}
                      onClick={() => handleAddExistingAttribute(attr.id)}
                      disabled={productAttributes.some(pa => pa.id === attr.id)}
                      style={{
                        padding: '12px',
                        textAlign: 'left',
                        border: '1px solid var(--border)',
                        borderRadius: '8px',
                        backgroundColor: productAttributes.some(pa => pa.id === attr.id) ? 'var(--bg-tertiary)' : 'var(--bg-primary)',
                        cursor: productAttributes.some(pa => pa.id === attr.id) ? 'not-allowed' : 'pointer',
                        color: 'var(--text-primary)',
                      }}
                    >
                      <div style={{ fontSize: '14px', fontWeight: 600, marginBottom: '4px' }}>
                        {attr.name}
                      </div>
                      <div style={{ fontSize: '12px', color: 'var(--text-muted)' }}>
                        Type: {attr.dataType}
                        {productAttributes.some(pa => pa.id === attr.id) && (
                          <span style={{ marginLeft: '8px', color: 'var(--primary)' }}>✓ Already added</span>
                        )}
                      </div>
                    </button>
                  ))}
                </div>
              )}
            </div>
          </div>
        </div>
      )}

      {/* Add Custom Attribute Modal */}
      {showCustomAttributeModal && (
        <div
          style={{
            position: 'fixed',
            top: 0,
            left: 0,
            right: 0,
            bottom: 0,
            backgroundColor: 'rgba(0, 0, 0, 0.7)',
            zIndex: 1000,
            display: 'flex',
            alignItems: 'center',
            justifyContent: 'center'
          }}
          onClick={() => setShowCustomAttributeModal(false)}
        >
          <div
            style={{
              backgroundColor: 'var(--bg-secondary)',
              borderRadius: '12px',
              width: '500px',
              border: '1px solid var(--border)',
            }}
            onClick={(e) => e.stopPropagation()}
          >
            <div
              style={{
                padding: '20px',
                borderBottom: '1px solid var(--border)',
                display: 'flex',
                justifyContent: 'space-between',
                alignItems: 'center',
              }}
            >
              <h3 style={{ fontSize: '18px', fontWeight: 600 }}>Add Custom Attribute</h3>
              <button
                onClick={() => setShowCustomAttributeModal(false)}
                style={{
                  background: 'none',
                  border: 'none',
                  color: 'var(--text-muted)',
                  fontSize: '24px',
                  cursor: 'pointer',
                  padding: '4px'
                }}
              >
                ×
              </button>
            </div>
            <div style={{ padding: '20px' }}>
              <div style={{ marginBottom: '16px' }}>
                <label className="block text-sm font-medium mb-1.5">
                  Attribute Name
                </label>
                <input
                  type="text"
                  value={customAttributeName}
                  onChange={(e) => setCustomAttributeName(e.target.value)}
                  className="input" style={{ width: '100%' }}
                  placeholder="e.g., Custom Field"
                />
              </div>
              <div style={{ marginBottom: '20px' }}>
                <label className="block text-sm font-medium mb-1.5">
                  Value (Optional)
                </label>
                <input
                  type="text"
                  value={customAttributeValue}
                  onChange={(e) => setCustomAttributeValue(e.target.value)}
                  className="input" style={{ width: '100%' }}
                  placeholder="Enter value..."
                />
              </div>
              <div style={{ display: 'flex', justifyContent: 'flex-end', gap: '12px' }}>
                <button onClick={() => setShowCustomAttributeModal(false)} className="btn btn-secondary">
                  Cancel
                </button>
                <button onClick={handleAddCustomAttribute} className="btn btn-primary">
                  Add Attribute
                </button>
              </div>
            </div>
          </div>
        </div>
      )}
    </div>
    </>
  );
}

// ── AI Debug Panel ────────────────────────────────────────────────────────────

const API_BASE_DEBUG = (import.meta as any).env?.VITE_API_URL || 'http://localhost:8080/api/v1';

function AIDebugPanel({ productId }: { productId: string }) {
  const [data, setData] = React.useState<any>(null);
  const [loading, setLoading] = React.useState(false);
  const [error, setError] = React.useState('');
  const [expanded, setExpanded] = React.useState<Record<string, boolean>>({});

  const load = async () => {
    setLoading(true); setError('');
    try {
      const res = await fetch(`${API_BASE_DEBUG}/products/${productId}/ai-debug`, {
        headers: {
          'Content-Type': 'application/json',
          'X-Tenant-Id': getActiveTenantId(),
        },
      });
      if (!res.ok) throw new Error(`HTTP ${res.status}`);
      setData(await res.json());
    } catch (e: any) {
      setError(e.message);
    } finally {
      setLoading(false); }
  };

  React.useEffect(() => { load(); }, [productId]);

  const toggle = (key: string) => setExpanded(prev => ({ ...prev, [key]: !prev[key] }));

  const pre: React.CSSProperties = {
    margin: 0, padding: '12px 14px', borderRadius: 6,
    background: 'var(--bg-primary)', border: '1px solid var(--border)',
    fontSize: 11, fontFamily: 'monospace', color: 'var(--text-secondary)',
    overflowX: 'auto', whiteSpace: 'pre-wrap', wordBreak: 'break-all',
    maxHeight: 400, overflowY: 'auto',
  };
  const sectionHdr: React.CSSProperties = {
    display: 'flex', alignItems: 'center', gap: 8, cursor: 'pointer',
    padding: '10px 14px', background: 'var(--bg-tertiary)',
    border: '1px solid var(--border)', borderRadius: 8, marginBottom: 4,
    userSelect: 'none',
  };

  const renderValue = (val: any): React.ReactNode => {
    if (val === null || val === undefined) return <span style={{ color: 'var(--text-muted)' }}>null</span>;
    if (typeof val === 'boolean') return <span style={{ color: '#f59e0b' }}>{String(val)}</span>;
    if (typeof val === 'number') return <span style={{ color: '#34d399' }}>{val}</span>;
    if (typeof val === 'string') return <span style={{ color: '#93c5fd' }}>"{val}"</span>;
    if (Array.isArray(val)) {
      if (val.length === 0) return <span style={{ color: 'var(--text-muted)' }}>[]</span>;
      return (
        <div style={{ paddingLeft: 16, borderLeft: '1px solid var(--border)' }}>
          {val.map((v, i) => (
            <div key={i} style={{ marginBottom: 2 }}>
              <span style={{ color: 'var(--text-muted)', fontSize: 10 }}>[{i}] </span>
              {renderValue(v)}
            </div>
          ))}
        </div>
      );
    }
    if (typeof val === 'object') {
      return (
        <div style={{ paddingLeft: 16, borderLeft: '1px solid var(--border)' }}>
          {Object.entries(val).map(([k, v]) => (
            <div key={k} style={{ marginBottom: 2, display: 'flex', gap: 6, flexWrap: 'wrap' }}>
              <span style={{ color: '#f9a8d4', fontWeight: 600, flexShrink: 0 }}>{k}:</span>
              {renderValue(v)}
            </div>
          ))}
        </div>
      );
    }
    return <span>{String(val)}</span>;
  };

  if (loading) return (
    <div style={{ display: 'flex', alignItems: 'center', gap: 10, color: 'var(--text-muted)', padding: 24 }}>
      <div className="spinner" style={{ width: 18, height: 18 }} /> Loading debug data…
    </div>
  );

  if (error) return (
    <div style={{ padding: 16, borderRadius: 8, background: 'rgba(239,68,68,0.08)', border: '1px solid rgba(239,68,68,0.3)', color: '#f87171', fontSize: 13 }}>
      ⚠ {error} <button onClick={load} style={{ marginLeft: 8, background: 'none', border: 'none', color: '#f87171', cursor: 'pointer', textDecoration: 'underline', fontSize: 12 }}>Retry</button>
    </div>
  );

  if (!data) return null;

  return (
    <div style={{ display: 'flex', flexDirection: 'column', gap: 12 }}>

      {/* Header */}
      <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', marginBottom: 4 }}>
        <div>
          <div style={{ fontSize: 15, fontWeight: 700, color: 'var(--text-primary)' }}>🐛 AI Lookup Debug</div>
          <div style={{ fontSize: 12, color: 'var(--text-muted)', marginTop: 2 }}>
            {data.branch_count} extended data branch{data.branch_count !== 1 ? 'es' : ''} · product_type: <strong>{data.product?.product_type || '—'}</strong>
          </div>
        </div>
        <button onClick={load} style={{ padding: '6px 12px', background: 'var(--bg-tertiary)', border: '1px solid var(--border)', borderRadius: 6, color: 'var(--text-muted)', fontSize: 12, cursor: 'pointer' }}>
          🔄 Refresh
        </button>
      </div>

      {/* Product document — key fields summary */}
      <div>
        <div style={sectionHdr} onClick={() => toggle('product')}>
          <span style={{ fontSize: 13, fontWeight: 600, color: 'var(--text-primary)' }}>📄 Product Document</span>
          <span style={{ fontSize: 11, color: 'var(--text-muted)', marginLeft: 'auto' }}>{expanded['product'] ? '▲ collapse' : '▼ expand'}</span>
        </div>
        {expanded['product'] && (
          <pre style={pre}>{JSON.stringify(data.product, null, 2)}</pre>
        )}
        {!expanded['product'] && (
          <div style={{ padding: '10px 14px', border: '1px solid var(--border)', borderRadius: 8, display: 'grid', gridTemplateColumns: 'repeat(auto-fill, minmax(240px, 1fr))', gap: '8px 16px' }}>
            {[
              ['product_type', data.product?.product_type],
              ['parent_id', data.product?.parent_id],
              ['title', data.product?.title],
              ['brand', data.product?.brand],
              ['sku', data.product?.sku],
              ['status', data.product?.status],
              ['asin', data.product?.identifiers?.asin],
              ['ean', data.product?.identifiers?.ean],
              ['upc', data.product?.identifiers?.upc],
              ['description (chars)', data.product?.description?.length ?? 0],
              ['key_features count', Array.isArray(data.product?.key_features) ? data.product.key_features.length : 0],
              ['assets count', Array.isArray(data.product?.assets) ? data.product.assets.length : 0],
              ['ai_lookup_source', data.product?.attributes?.ai_lookup_source],
              ['ai_lookup_draft', String(data.product?.attributes?.ai_lookup_draft ?? '—')],
              ['manufacturer', data.product?.attributes?.manufacturer],
              ['color', data.product?.attributes?.color],
              ['size', data.product?.attributes?.size],
              ['model_number', data.product?.attributes?.model_number],
            ].map(([label, val]) => (
              <div key={String(label)} style={{ fontSize: 12 }}>
                <span style={{ color: 'var(--text-muted)', fontWeight: 600 }}>{label}: </span>
                <span style={{ color: val ? 'var(--text-primary)' : 'var(--text-muted)' }}>
                  {val !== undefined && val !== null ? String(val) : '—'}
                </span>
              </div>
            ))}
          </div>
        )}
      </div>

      {/* Key features / bullet points */}
      {Array.isArray(data.product?.key_features) && data.product.key_features.length > 0 && (
        <div>
          <div style={sectionHdr} onClick={() => toggle('bullets')}>
            <span style={{ fontSize: 13, fontWeight: 600, color: 'var(--text-primary)' }}>📝 Key Features / Bullet Points ({data.product.key_features.length})</span>
            <span style={{ fontSize: 11, color: 'var(--text-muted)', marginLeft: 'auto' }}>{expanded['bullets'] ? '▲' : '▼'}</span>
          </div>
          {expanded['bullets'] && (
            <div style={{ padding: '10px 14px', border: '1px solid var(--border)', borderRadius: 8, display: 'flex', flexDirection: 'column', gap: 4 }}>
              {data.product.key_features.map((f: string, i: number) => (
                <div key={i} style={{ fontSize: 13, color: 'var(--text-primary)', display: 'flex', gap: 8 }}>
                  <span style={{ color: 'var(--text-muted)', flexShrink: 0 }}>{i + 1}.</span>
                  <span>{f}</span>
                </div>
              ))}
            </div>
          )}
        </div>
      )}

      {/* Extended data branches */}
      {data.extended_data?.length > 0 ? (
        data.extended_data.map((branch: any, i: number) => {
          const key = `branch_${i}`;
          const sourceKey = branch._source_key || `branch_${i}`;
          const attrs = branch.attributes || {};
          const bulletPts: string[] = branch.bullet_points || [];
          const identifiers = branch.identifiers || [];
          return (
            <div key={key}>
              <div style={{ ...sectionHdr, background: 'rgba(99,102,241,0.08)', borderColor: 'rgba(99,102,241,0.25)' }} onClick={() => toggle(key)}>
                <span style={{ fontSize: 13, fontWeight: 700, color: 'var(--primary)' }}>🗂 {sourceKey}</span>
                <span style={{ fontSize: 11, color: 'var(--text-muted)', marginLeft: 4 }}>· {branch.source || ''}</span>
                <span style={{ fontSize: 11, color: 'var(--text-muted)', marginLeft: 'auto' }}>{expanded[key] ? '▲ collapse' : '▼ expand raw'}</span>
              </div>
              {/* Summary line always visible */}
              <div style={{ padding: '8px 14px', border: '1px solid var(--border)', borderRadius: 8, fontSize: 12, display: 'grid', gridTemplateColumns: 'repeat(auto-fill, minmax(200px, 1fr))', gap: '6px 16px', marginBottom: expanded[key] ? 4 : 0 }}>
                <div><span style={{ color: 'var(--text-muted)' }}>ASIN: </span><span style={{ color: 'var(--text-primary)' }}>{branch.asin || '—'}</span></div>
                <div><span style={{ color: 'var(--text-muted)' }}>identifier: </span><span>{branch.identifier || '—'} ({branch.identifier_type || '—'})</span></div>
                <div><span style={{ color: 'var(--text-muted)' }}>main_image: </span>
                  {branch.main_image
                    ? <a href={branch.main_image} target="_blank" rel="noreferrer" style={{ color: 'var(--primary)', fontSize: 11 }}>view ↗</a>
                    : <span style={{ color: 'var(--text-muted)' }}>—</span>}
                </div>
                <div><span style={{ color: 'var(--text-muted)' }}>bullet_points: </span><span>{bulletPts.length} found</span></div>
                <div><span style={{ color: 'var(--text-muted)' }}>description: </span><span>{branch.description ? `${String(branch.description).length} chars` : '—'}</span></div>
                <div><span style={{ color: 'var(--text-muted)' }}>attributes keys: </span><span>{Object.keys(attrs).join(', ') || '—'}</span></div>
                <div><span style={{ color: 'var(--text-muted)' }}>identifiers groups: </span><span>{Array.isArray(identifiers) ? identifiers.length : 0}</span></div>
              </div>
              {/* Identifiers from API */}
              {Array.isArray(identifiers) && identifiers.length > 0 && (
                <div style={{ padding: '8px 14px', border: '1px solid var(--border)', borderTop: 'none', borderRadius: '0 0 8px 8px', fontSize: 12, marginBottom: expanded[key] ? 4 : 0 }}>
                  <span style={{ color: 'var(--text-muted)', fontWeight: 600 }}>Identifiers from SP-API: </span>
                  {identifiers.map((group: any, gi: number) =>
                    (group.identifiers || []).map((iv: any, ii: number) => (
                      <span key={`${gi}-${ii}`} style={{ marginLeft: 8, padding: '1px 6px', borderRadius: 4, background: 'var(--bg-tertiary)', border: '1px solid var(--border)' }}>
                        {iv.identifierType}: {iv.identifier}
                      </span>
                    ))
                  )}
                </div>
              )}
              {/* Raw JSON */}
              {expanded[key] && (
                <pre style={{ ...pre, borderRadius: '0 0 8px 8px', borderTop: 'none' }}>
                  {JSON.stringify(branch, null, 2)}
                </pre>
              )}
            </div>
          );
        })
      ) : (
        <div style={{ padding: '24px', textAlign: 'center', color: 'var(--text-muted)', fontSize: 13, border: '1px dashed var(--border)', borderRadius: 8 }}>
          No extended_data branches found. The enrichment may still be running, or the AI lookup did not save extended data.
        </div>
      )}
    </div>
  );
}
