// ============================================================================
// LISTING DETAIL PAGE
// ============================================================================
// Location: frontend/src/pages/marketplace/ListingDetail.tsx
// Route: /marketplace/listings/:id

import React, { useState, useEffect } from 'react';
import { useParams, useNavigate } from 'react-router-dom';
import { listingService } from '../../services/marketplace-api';

const adapterEmoji: Record<string, string> = { amazon: '📦', temu: '🛍️', ebay: '🏷️', shopify: '🛒', tesco: '🏪' };
const adapterColor: Record<string, string> = { amazon: '#FF9900', temu: '#FF6B35', ebay: '#E53238', shopify: '#96BF48', tesco: '#EE1C2E' };

const stateColors: Record<string, { bg: string; fg: string }> = {
  published: { bg: 'var(--success-glow)', fg: 'var(--success)' },
  ready: { bg: 'var(--info-glow)', fg: 'var(--info)' },
  imported: { bg: 'var(--warning-glow)', fg: 'var(--warning)' },
  draft: { bg: 'var(--bg-tertiary)', fg: 'var(--text-secondary)' },
  error: { bg: 'var(--danger-glow)', fg: 'var(--danger)' },
  blocked: { bg: 'var(--danger-glow)', fg: 'var(--danger)' },
  paused: { bg: 'var(--warning-glow)', fg: 'var(--warning)' },
};

function formatDate(d?: string): string {
  if (!d) return '—';
  return new Date(d).toLocaleDateString('en-GB', { day: '2-digit', month: 'short', year: 'numeric', hour: '2-digit', minute: '2-digit' });
}

// Label style
const labelStyle: React.CSSProperties = {
  display: 'block', fontSize: 11, fontWeight: 700, color: 'var(--text-muted)',
  textTransform: 'uppercase', letterSpacing: '0.5px', marginBottom: 6,
};
const inputStyle: React.CSSProperties = {
  width: '100%', padding: '10px 14px', borderRadius: 8,
  background: 'var(--bg-primary)', border: '1px solid var(--border-bright)',
  color: 'var(--text-primary)', fontSize: 14, outline: 'none',
};
const cardStyle: React.CSSProperties = {
  background: 'var(--bg-secondary)', borderRadius: 12, border: '1px solid var(--border)',
  padding: 24, marginBottom: 20,
};
const sectionTitle: React.CSSProperties = {
  fontSize: 15, fontWeight: 700, color: 'var(--text-primary)', marginBottom: 16,
  paddingBottom: 10, borderBottom: '1px solid var(--border)',
};

// Attribute display names for common fields
const ATTR_LABELS: Record<string, string> = {
  author: 'Author', binding: 'Binding', edition: 'Edition',
  format: 'Format', genre: 'Genre', language: 'Language',
  publication_date: 'Publication Date', runtime: 'Runtime',
  item_weight: 'Item Weight', item_package_weight: 'Package Weight',
  target_audience: 'Target Audience', number_of_items: 'Number of Items',
  unspsc_code: 'UNSPSC Code', street_date: 'Street Date',
  batteries_required: 'Batteries Required', batteries_included: 'Batteries Included',
  supplier_declared_dg_hz_regulation: 'DG/HZ Regulation',
  gpsr_safety_attestation: 'GPSR Safety', subject: 'Subject',
  recommended_browse_nodes: 'Browse Node', list_price: 'List Price',
  product_site_launch_date: 'Launch Date', item_name: 'Item Name',
  brand: 'Brand', manufacturer: 'Manufacturer',
};

function humanizeKey(key: string): string {
  return ATTR_LABELS[key] || key.replace(/_/g, ' ').replace(/\b\w/g, c => c.toUpperCase());
}

function formatAttrValue(value: any): string {
  if (value === null || value === undefined) return '—';
  if (typeof value === 'boolean') return value ? 'Yes' : 'No';
  if (Array.isArray(value)) {
    // Deduplicate and join
    return [...new Set(value.map(v => typeof v === 'object' ? JSON.stringify(v) : String(v)))].join(', ');
  }
  if (typeof value === 'object') {
    // Special cases
    if ('value_with_tax' in value && 'currency' in value) {
      return `${value.currency} ${value.value_with_tax}`;
    }
    if ('value' in value && 'unit' in value) {
      return `${value.value} ${value.unit}`;
    }
    return JSON.stringify(value);
  }
  return String(value);
}

// Fields to skip in attributes display (already shown elsewhere)
const SKIP_ATTRS = new Set([
  'bullet_point', 'item_name', 'brand', 'manufacturer',
  'item_dimensions', 'item_package_dimensions',
  'externally_assigned_product_identifier',
]);

export default function ListingDetail() {
  const { id } = useParams<{ id: string }>();
  const navigate = useNavigate();
  const [loading, setLoading] = useState(true);
  const [listing, setListing] = useState<any>(null);
  const [product, setProduct] = useState<any>(null);
  const [extendedData, setExtendedData] = useState<any>(null);
  const [selectedImage, setSelectedImage] = useState(0);
  const [activeTab, setActiveTab] = useState<'details' | 'attributes' | 'raw' | 'downloaded' | 'performance'>('details');
  const [saving, setSaving] = useState(false);
  const [saveError, setSaveError] = useState<string | null>(null);
  const [saveSuccess, setSaveSuccess] = useState(false);
  // Editable overrides — mirrors listing.overrides
  const [overrides, setOverrides] = useState<{ title?: string; description?: string }>({});

  useEffect(() => { if (id) loadDetail(); }, [id]);

  async function loadDetail() {
    setLoading(true);
    try {
      const res = await listingService.getDetail(id!);
      const data = res.data?.data;
      const listingData = data?.listing || null;
      setListing(listingData);
      setProduct(data?.product || null);
      setExtendedData(data?.extended_data || null);
      // Seed editable overrides from loaded listing
      setOverrides({
        title: listingData?.overrides?.title ?? '',
        description: listingData?.overrides?.description ?? '',
      });
    } catch (err) {
      console.error('Failed to load listing detail:', err);
    } finally {
      setLoading(false);
    }
  }

  async function saveListing() {
    if (!id) return;
    setSaving(true);
    setSaveError(null);
    setSaveSuccess(false);
    try {
      await listingService.update(id, { overrides } as any);
      setSaveSuccess(true);
      setTimeout(() => setSaveSuccess(false), 3000);
    } catch (err: any) {
      setSaveError(err?.response?.data?.error || err.message || 'Save failed');
    } finally {
      setSaving(false);
    }
  }

  if (loading) return (
    <div className="page">
      <div className="loading-state"><div className="spinner"></div><p>Loading listing...</p></div>
    </div>
  );

  if (!listing) return (
    <div className="page">
      <div className="empty-state"><div className="empty-icon">❌</div><h3>Listing not found</h3>
        <button className="btn btn-primary" onClick={() => navigate('/marketplace/listings')}>← Back to Listings</button>
      </div>
    </div>
  );

  const enriched = listing.enriched_data || {};
  const attrs = enriched.attributes || {};
  const images = enriched.images || product?.assets || [];
  const bullets = enriched.bullets || product?.attributes?.bullet_points || [];
  const dimensions = enriched.dimensions || product?.dimensions || {};
  const identifiers = enriched.identifiers || product?.identifiers || {};
  const title = enriched.title || product?.title || listing.overrides?.title || '(Untitled)';
  const brand = enriched.brand || product?.brand || '';
  const manufacturer = enriched.manufacturer || product?.attributes?.manufacturer || '';
  const description = enriched.description || product?.description || listing.overrides?.description || '';
  const asin = enriched.asin || '';
  const channelColor = adapterColor[listing.channel] || 'var(--primary)';
  const channelEmoji = adapterEmoji[listing.channel] || '🌐';
  const stateStyle = stateColors[listing.state] || stateColors.draft;

  // Filter out skip attrs and empty values
  const displayAttrs = Object.entries(attrs).filter(
    ([key, val]) => !SKIP_ATTRS.has(key) && val !== null && val !== undefined && val !== ''
  );

  return (
    <div className="page">
      {/* Header */}
      <div style={{ display: 'flex', alignItems: 'flex-start', justifyContent: 'space-between', marginBottom: 24 }}>
        <div style={{ display: 'flex', alignItems: 'center', gap: 16 }}>
          <button className="btn btn-secondary" onClick={() => navigate('/marketplace/listings')} style={{ padding: '8px 16px' }}>← Back</button>
          <div>
            <h1 style={{ fontSize: 22, fontWeight: 700, color: 'var(--text-primary)', marginBottom: 4 }}>{title}</h1>
            <div style={{ display: 'flex', alignItems: 'center', gap: 12 }}>
              <span style={{ display: 'inline-flex', alignItems: 'center', gap: 6, padding: '3px 10px', borderRadius: 6, background: channelColor + '1A', color: channelColor, fontSize: 12, fontWeight: 700 }}>
                {channelEmoji} {listing.channel?.toUpperCase()}
              </span>
              <span style={{ padding: '3px 10px', borderRadius: 6, background: stateStyle.bg, color: stateStyle.fg, fontSize: 12, fontWeight: 700, border: `1px solid ${stateStyle.fg}30` }}>
                {listing.state?.toUpperCase()}
              </span>
              {asin && <span style={{ fontSize: 12, color: 'var(--text-muted)' }}>ASIN: {asin}</span>}
              {listing.enriched_at && <span style={{ fontSize: 12, color: 'var(--success)' }}>✓ Enriched</span>}
            </div>
          </div>
        </div>
        <div style={{ display: 'flex', gap: 8 }}>
          <button className="btn btn-secondary">Copy from Basic Details</button>
          {saveError && <span style={{ color: 'var(--danger)', fontSize: 13, alignSelf: 'center' }}>{saveError}</span>}
          {saveSuccess && <span style={{ color: 'var(--success)', fontSize: 13, alignSelf: 'center' }}>✓ Saved</span>}
          <button className="btn btn-primary" onClick={saveListing} disabled={saving}>
            {saving ? 'Saving…' : 'Save'}
          </button>
        </div>
      </div>

      {/* Tabs */}
      <div style={{ display: 'flex', gap: 0, marginBottom: 20, borderBottom: '2px solid var(--border)' }}>
        {(['details', 'attributes', 'raw', 'downloaded', 'performance'] as const).map(tab => (
          <button key={tab} onClick={() => setActiveTab(tab)}
            style={{
              padding: '10px 24px', fontSize: 13, fontWeight: 600, border: 'none', cursor: 'pointer',
              background: 'transparent', color: activeTab === tab ? 'var(--primary)' : 'var(--text-muted)',
              borderBottom: activeTab === tab ? '2px solid var(--primary)' : '2px solid transparent',
              marginBottom: -2, textTransform: 'capitalize',
            }}>
            {tab === 'details' ? 'Listing Details' : tab === 'attributes' ? `Attributes (${displayAttrs.length})` : tab === 'raw' ? 'Raw Data' : tab === 'downloaded' ? 'Downloaded Data' : '📊 Performance'}
          </button>
        ))}
      </div>

      {/* Tab: Details */}
      {activeTab === 'details' && (
        <>
          {/* Images + Product Details side by side */}
          <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: 20 }}>
            {/* Left: Images */}
            <div style={cardStyle}>
              <div style={sectionTitle}>Images ({images.length})</div>
              {images.length > 0 ? (
                <>
                  <div style={{ width: '100%', aspectRatio: '1/1', borderRadius: 10, overflow: 'hidden', background: '#fff', display: 'flex', alignItems: 'center', justifyContent: 'center', marginBottom: 12 }}>
                    <img src={images[selectedImage]?.url} alt={title}
                      style={{ maxWidth: '100%', maxHeight: '100%', objectFit: 'contain' }} />
                  </div>
                  <div style={{ display: 'flex', gap: 8, flexWrap: 'wrap' }}>
                    {images.map((img: any, i: number) => (
                      <div key={i} onClick={() => setSelectedImage(i)}
                        style={{
                          width: 56, height: 56, borderRadius: 6, overflow: 'hidden', cursor: 'pointer',
                          border: selectedImage === i ? '2px solid var(--primary)' : '2px solid var(--border)',
                          background: '#fff', display: 'flex', alignItems: 'center', justifyContent: 'center',
                        }}>
                        <img src={img.url} alt="" style={{ maxWidth: '100%', maxHeight: '100%', objectFit: 'contain' }} />
                      </div>
                    ))}
                  </div>
                </>
              ) : (
                <div style={{ textAlign: 'center', padding: 40, color: 'var(--text-muted)' }}>No images available</div>
              )}
            </div>

            {/* Right: Product Details + Pricing */}
            <div>
              {/* Product Details */}
              <div style={cardStyle}>
                <div style={sectionTitle}>Product Details</div>
                <div style={{ display: 'grid', gap: 14 }}>
                  <div>
                    <label style={labelStyle}>Product Name</label>
                    <input style={inputStyle} value={title} readOnly />
                  </div>
                  <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: 14 }}>
                    <div>
                      <label style={labelStyle}>Brand</label>
                      <input style={inputStyle} value={brand} readOnly />
                    </div>
                    <div>
                      <label style={labelStyle}>Manufacturer</label>
                      <input style={inputStyle} value={manufacturer} readOnly />
                    </div>
                  </div>
                  {asin && (
                    <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: 14 }}>
                      <div>
                        <label style={labelStyle}>ASIN</label>
                        <input style={inputStyle} value={asin} readOnly />
                      </div>
                      <div>
                        <label style={labelStyle}>Product Type</label>
                        <input style={inputStyle} value={enriched.product_type || '—'} readOnly />
                      </div>
                    </div>
                  )}
                </div>
              </div>

              {/* Identifiers */}
              {Object.keys(identifiers).length > 0 && (
                <div style={cardStyle}>
                  <div style={sectionTitle}>Identifiers</div>
                  <div style={{ display: 'grid', gridTemplateColumns: 'repeat(auto-fill, minmax(160px, 1fr))', gap: 14 }}>
                    {Object.entries(identifiers).map(([key, val]) => (
                      <div key={key}>
                        <label style={labelStyle}>{key.toUpperCase()}</label>
                        <input style={inputStyle} value={String(val)} readOnly />
                      </div>
                    ))}
                  </div>
                </div>
              )}

              {/* Pricing */}
              <div style={cardStyle}>
                <div style={sectionTitle}>Pricing</div>
                <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: 14 }}>
                  <div>
                    <label style={labelStyle}>Fixed Price</label>
                    <input style={inputStyle} value={listing.overrides?.price || attrs.list_price?.value_with_tax || ''} placeholder="Enter fixed price" readOnly />
                  </div>
                  <div>
                    <label style={labelStyle}>Currency</label>
                    <input style={inputStyle} value={attrs.list_price?.currency || 'GBP'} readOnly />
                  </div>
                </div>
              </div>
            </div>
          </div>

          {/* Bullet Points + Condition */}
          <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: 20 }}>
            <div style={cardStyle}>
              <div style={sectionTitle}>Bullet Points</div>
              <div style={{ display: 'grid', gap: 10 }}>
                {[0, 1, 2, 3, 4].map(i => (
                  <div key={i}>
                    <label style={labelStyle}>Bullet Point {i + 1}</label>
                    <input style={inputStyle} value={bullets[i] || ''} placeholder={`Bullet point ${i + 1}`} readOnly />
                  </div>
                ))}
              </div>
            </div>

            <div>
              {/* Dimensions */}
              {Object.keys(dimensions).length > 0 && (
                <div style={cardStyle}>
                  <div style={sectionTitle}>Dimensions</div>
                  <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr 1fr', gap: 14 }}>
                    {dimensions.length != null && (
                      <div>
                        <label style={labelStyle}>Length</label>
                        <input style={inputStyle} value={`${dimensions.length} ${dimensions.length_unit || 'mm'}`} readOnly />
                      </div>
                    )}
                    {dimensions.width != null && (
                      <div>
                        <label style={labelStyle}>Width</label>
                        <input style={inputStyle} value={`${dimensions.width} ${dimensions.width_unit || 'mm'}`} readOnly />
                      </div>
                    )}
                    {dimensions.height != null && (
                      <div>
                        <label style={labelStyle}>Height</label>
                        <input style={inputStyle} value={`${dimensions.height} ${dimensions.height_unit || 'mm'}`} readOnly />
                      </div>
                    )}
                  </div>
                </div>
              )}

              {/* Weight */}
              {enriched.weight && Object.keys(enriched.weight).length > 0 && (
                <div style={cardStyle}>
                  <div style={sectionTitle}>Weight</div>
                  <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: 14 }}>
                    <div>
                      <label style={labelStyle}>Item Weight</label>
                      <input style={inputStyle} value={enriched.weight.value ? `${enriched.weight.value} ${enriched.weight.unit || 'g'}` : '—'} readOnly />
                    </div>
                  </div>
                </div>
              )}

              {/* Condition */}
              <div style={cardStyle}>
                <div style={sectionTitle}>Condition</div>
                <div>
                  <label style={labelStyle}>Condition</label>
                  <select style={{ ...inputStyle, appearance: 'auto' }} defaultValue="new">
                    <option value="new">New</option>
                    <option value="used_like_new">Used - Like New</option>
                    <option value="used_very_good">Used - Very Good</option>
                    <option value="used_good">Used - Good</option>
                    <option value="used_acceptable">Used - Acceptable</option>
                  </select>
                </div>
                <div style={{ marginTop: 14 }}>
                  <label style={labelStyle}>Condition Notes</label>
                  <textarea style={{ ...inputStyle, minHeight: 80, resize: 'vertical', fontFamily: 'inherit' }} placeholder="Enter condition notes..." />
                </div>
              </div>
            </div>
          </div>

          {/* Description */}
          <div style={cardStyle}>
            <div style={sectionTitle}>Description</div>
            <textarea style={{ ...inputStyle, minHeight: 160, resize: 'vertical', fontFamily: 'inherit' }}
              value={description} placeholder="Enter product description..." readOnly />
          </div>

          {/* Variations (if present) */}
          {enriched.variations && Array.isArray(enriched.variations) && enriched.variations.length > 0 && (
            <div style={cardStyle}>
              <div style={sectionTitle}>Amazon Variants</div>
              <div style={{ overflowX: 'auto' }}>
                <table style={{ width: '100%', borderCollapse: 'collapse' }}>
                  <thead>
                    <tr style={{ borderBottom: '1px solid var(--border)' }}>
                      <th style={{ textAlign: 'left', padding: '10px 14px', fontSize: 12, fontWeight: 700, color: 'var(--text-muted)', textTransform: 'uppercase' }}>Variation</th>
                      <th style={{ textAlign: 'left', padding: '10px 14px', fontSize: 12, fontWeight: 700, color: 'var(--text-muted)', textTransform: 'uppercase' }}>ASIN</th>
                    </tr>
                  </thead>
                  <tbody>
                    {enriched.variations.map((v: any, i: number) => (
                      <tr key={i} style={{ borderBottom: '1px solid var(--border)' }}>
                        <td style={{ padding: '10px 14px', fontSize: 14 }}>{JSON.stringify(v)}</td>
                        <td style={{ padding: '10px 14px', fontSize: 14, color: 'var(--text-muted)' }}>{v?.asin || '—'}</td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            </div>
          )}

          {/* Timestamps */}
          <div style={cardStyle}>
            <div style={sectionTitle}>Metadata</div>
            <div style={{ display: 'grid', gridTemplateColumns: 'repeat(auto-fill, minmax(200px, 1fr))', gap: 14 }}>
              <div><label style={labelStyle}>Created</label><div style={{ fontSize: 13, color: 'var(--text-secondary)' }}>{formatDate(listing.created_at)}</div></div>
              <div><label style={labelStyle}>Updated</label><div style={{ fontSize: 13, color: 'var(--text-secondary)' }}>{formatDate(listing.updated_at)}</div></div>
              <div><label style={labelStyle}>Enriched</label><div style={{ fontSize: 13, color: listing.enriched_at ? 'var(--success)' : 'var(--text-muted)' }}>{listing.enriched_at ? formatDate(listing.enriched_at) : 'Pending'}</div></div>
              <div><label style={labelStyle}>Product ID</label><div style={{ fontSize: 12, color: 'var(--text-muted)', fontFamily: 'monospace' }}>{listing.product_id}</div></div>
              <div><label style={labelStyle}>Listing ID</label><div style={{ fontSize: 12, color: 'var(--text-muted)', fontFamily: 'monospace' }}>{listing.listing_id}</div></div>
            </div>
          </div>
        </>
      )}

      {/* Tab: Attributes */}
      {activeTab === 'attributes' && (
        <div style={cardStyle}>
          <div style={sectionTitle}>All Attributes ({displayAttrs.length})</div>
          {displayAttrs.length === 0 ? (
            <div style={{ textAlign: 'center', padding: 40, color: 'var(--text-muted)' }}>
              No enriched attributes available yet. Attributes will appear once enrichment completes.
            </div>
          ) : (
            <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: 12 }}>
              {displayAttrs.map(([key, val]) => (
                <div key={key} style={{ display: 'flex', alignItems: 'flex-start', gap: 12, padding: '10px 14px', borderRadius: 8, background: 'var(--bg-tertiary)', border: '1px solid var(--border)' }}>
                  <div style={{ minWidth: 140 }}>
                    <div style={{ fontSize: 11, fontWeight: 700, color: 'var(--text-muted)', textTransform: 'uppercase', letterSpacing: '0.3px' }}>{humanizeKey(key)}</div>
                  </div>
                  <div style={{ fontSize: 14, color: 'var(--text-primary)', wordBreak: 'break-word', flex: 1 }}>{formatAttrValue(val)}</div>
                </div>
              ))}
            </div>
          )}
        </div>
      )}

      {/* Tab: Raw Data */}
      {activeTab === 'raw' && (
        <div style={cardStyle}>
          <div style={sectionTitle}>Raw Extended Data</div>
          <div style={{ display: 'flex', gap: 12, marginBottom: 16 }}>
            <button className="btn btn-secondary" style={{ fontSize: 12 }}
              onClick={() => { navigator.clipboard.writeText(JSON.stringify(extendedData, null, 2)); }}>
              📋 Copy to Clipboard
            </button>
          </div>
          <pre style={{
            background: 'var(--bg-primary)', border: '1px solid var(--border)', borderRadius: 8,
            padding: 16, fontSize: 12, color: 'var(--text-secondary)', overflow: 'auto',
            maxHeight: 600, whiteSpace: 'pre-wrap', wordBreak: 'break-word', fontFamily: 'monospace',
          }}>
            {extendedData ? JSON.stringify(extendedData, null, 2) : 'No extended data available'}
          </pre>
        </div>
      )}

      {/* Tab: Downloaded Data (IMP-02) */}
      {activeTab === 'downloaded' && (
        <DownloadedDataTab enrichedData={listing.enriched_data} />
      )}

      {/* Tab: Performance (USP-02) */}
      {activeTab === 'performance' && (
        <PerformanceDashboardTab listingId={listing.listing_id} channel={listing.channel} />
      )}
    </div>
  );
}

// ============================================================================
// DownloadedDataTab — IMP-02
// Renders listing.enriched_data from Firestore in a structured viewer.
// View-layer only — no new backend endpoints required.
// ============================================================================

interface DownloadedDataTabProps {
  enrichedData?: Record<string, any> | null;
}

function DownloadedDataTab({ enrichedData }: DownloadedDataTabProps) {
  const cardSt: React.CSSProperties = {
    background: 'var(--bg-secondary)', borderRadius: 12, border: '1px solid var(--border)',
    padding: 24, marginBottom: 20,
  };
  const secTitle: React.CSSProperties = {
    fontSize: 14, fontWeight: 700, color: 'var(--text-primary)', marginBottom: 14,
    paddingBottom: 10, borderBottom: '1px solid var(--border)',
  };

  if (!enrichedData || Object.keys(enrichedData).length === 0) {
    return (
      <div style={cardSt}>
        <div style={{ textAlign: 'center', padding: '40px 0', color: 'var(--text-muted)' }}>
          <div style={{ fontSize: 40, marginBottom: 12 }}>📥</div>
          <div style={{ fontWeight: 600, fontSize: 15, marginBottom: 8 }}>No downloaded channel data</div>
          <div style={{ fontSize: 13 }}>
            This listing has not been enriched from a channel API yet. Data appears here after import or enrichment.
          </div>
        </div>
      </div>
    );
  }

  // Extract well-known top-level sections for structured rendering
  const source = enrichedData.source || '—';
  const extractedAt = enrichedData.extracted_at || enrichedData.enriched_at || null;
  const asin = enrichedData.asin || null;
  const mainImage = enrichedData.main_image || null;
  const productData = enrichedData.product || null;
  const attributes = enrichedData.attributes || null;
  const summaries = enrichedData.summaries || null;

  const knownKeys = new Set(['source','extracted_at','enriched_at','asin','main_image','product','attributes','summaries','marketplace']);
  const otherEntries = Object.entries(enrichedData).filter(([k]) => !knownKeys.has(k));

  function renderValue(val: any, depth = 0): React.ReactNode {
    if (val === null || val === undefined) return <span style={{ color: 'var(--text-muted)' }}>—</span>;
    if (typeof val === 'boolean') return <span style={{ color: val ? 'var(--success)' : 'var(--danger)' }}>{val ? 'Yes' : 'No'}</span>;
    if (typeof val === 'number') return <span style={{ fontFamily: 'monospace' }}>{val}</span>;
    if (typeof val === 'string') {
      if (val.startsWith('http')) return <a href={val} target="_blank" rel="noreferrer" style={{ color: 'var(--primary)', wordBreak: 'break-all', fontSize: 12 }}>{val}</a>;
      return <span style={{ color: 'var(--text-primary)' }}>{val}</span>;
    }
    if (Array.isArray(val)) {
      if (val.length === 0) return <span style={{ color: 'var(--text-muted)' }}>(empty)</span>;
      if (val.every(v => typeof v !== 'object')) return <span>{val.join(', ')}</span>;
      return (
        <div>
          {val.map((item, i) => (
            <div key={i} style={{ marginBottom: 6, paddingLeft: 12, borderLeft: '2px solid var(--border)' }}>
              {renderValue(item, depth + 1)}
            </div>
          ))}
        </div>
      );
    }
    if (typeof val === 'object') {
      const entries = Object.entries(val);
      if (entries.length === 0) return <span style={{ color: 'var(--text-muted)' }}>(empty)</span>;
      return (
        <div style={{ paddingLeft: depth > 0 ? 12 : 0 }}>
          {entries.map(([k, v]) => (
            <div key={k} style={{ display: 'flex', gap: 8, marginBottom: 4, fontSize: 12 }}>
              <span style={{ color: 'var(--text-muted)', minWidth: 120, flexShrink: 0, fontWeight: 600 }}>{k}</span>
              <span style={{ flex: 1 }}>{renderValue(v, depth + 1)}</span>
            </div>
          ))}
        </div>
      );
    }
    return <span>{String(val)}</span>;
  }

  return (
    <div>
      {/* Meta strip */}
      <div style={{ ...cardSt, display: 'flex', flexWrap: 'wrap', gap: 24, alignItems: 'center' }}>
        {mainImage && (
          <img src={mainImage} alt="Main" style={{ width: 80, height: 80, borderRadius: 8, objectFit: 'cover', border: '1px solid var(--border)' }} />
        )}
        <div style={{ flex: 1, minWidth: 200 }}>
          <div style={{ display: 'grid', gridTemplateColumns: 'repeat(auto-fill, minmax(180px, 1fr))', gap: 12 }}>
            <DField label="Source" value={source} />
            {asin && <DField label="ASIN" value={asin} />}
            {extractedAt && (
              <DField label="Downloaded At" value={new Date(extractedAt).toLocaleDateString('en-GB', { day: '2-digit', month: 'short', year: 'numeric', hour: '2-digit', minute: '2-digit' })} />
            )}
            {enrichedData.marketplace && <DField label="Marketplace" value={enrichedData.marketplace} />}
          </div>
        </div>
        <button
          className="btn btn-secondary"
          style={{ fontSize: 12, alignSelf: 'flex-start' }}
          onClick={() => navigator.clipboard.writeText(JSON.stringify(enrichedData, null, 2))}
        >
          📋 Copy JSON
        </button>
      </div>

      {/* Product section */}
      {productData && typeof productData === 'object' && (
        <div style={cardSt}>
          <div style={secTitle}>Product Data</div>
          <DGrid data={productData} renderValue={renderValue} />
        </div>
      )}

      {/* Attributes section */}
      {attributes && typeof attributes === 'object' && Object.keys(attributes).length > 0 && (
        <div style={cardSt}>
          <div style={secTitle}>Attributes ({Object.keys(attributes).length})</div>
          <DGrid data={attributes} renderValue={renderValue} />
        </div>
      )}

      {/* Summaries */}
      {summaries && Array.isArray(summaries) && summaries.length > 0 && (
        <div style={cardSt}>
          <div style={secTitle}>Channel Summaries</div>
          {(summaries as any[]).map((s: any, i: number) => (
            <div key={i} style={{ marginBottom: 12, padding: '12px 16px', background: 'var(--bg-primary)', borderRadius: 8, border: '1px solid var(--border)' }}>
              {renderValue(s)}
            </div>
          ))}
        </div>
      )}

      {/* Other fields */}
      {otherEntries.length > 0 && (
        <div style={cardSt}>
          <div style={secTitle}>Other Channel Data</div>
          <DGrid data={Object.fromEntries(otherEntries)} renderValue={renderValue} />
        </div>
      )}
    </div>
  );
}

function DField({ label, value }: { label: string; value: string }) {
  return (
    <div>
      <div style={{ fontSize: 10, fontWeight: 700, color: 'var(--text-muted)', textTransform: 'uppercase', letterSpacing: '0.5px', marginBottom: 3 }}>{label}</div>
      <div style={{ fontSize: 13, color: 'var(--text-primary)', fontWeight: 500 }}>{value}</div>
    </div>
  );
}

function DGrid({ data, renderValue }: { data: Record<string, any>; renderValue: (v: any, d?: number) => React.ReactNode }) {
  return (
    <div style={{ display: 'grid', gridTemplateColumns: 'repeat(auto-fill, minmax(300px, 1fr))', gap: 10 }}>
      {Object.entries(data).map(([key, val]) => (
        <div key={key} style={{ padding: '10px 14px', background: 'var(--bg-primary)', borderRadius: 8, border: '1px solid var(--border)' }}>
          <div style={{ fontSize: 11, fontWeight: 700, color: 'var(--text-muted)', textTransform: 'uppercase', letterSpacing: '0.4px', marginBottom: 5 }}>
            {key.replace(/_/g, ' ')}
          </div>
          <div style={{ fontSize: 13 }}>{renderValue(val)}</div>
        </div>
      ))}
    </div>
  );
}

// ============================================================================
// PerformanceDashboardTab — USP-02
// Per-listing analytics fetched from channel APIs (Amazon, eBay).
// Shows KPI cards + a simple CSS bar chart for channels that support it.
// ============================================================================

interface PerformanceDashboardTabProps {
  listingId: string;
  channel: string;
}

function PerformanceDashboardTab({ listingId, channel }: PerformanceDashboardTabProps) {
  const [days, setDays] = React.useState(30);
  const [loading, setLoading] = React.useState(false);
  const [data, setData] = React.useState<any>(null);
  const [error, setError] = React.useState('');

  const CHANNEL_SUPPORTED = ['amazon', 'ebay'];
  const isSupported = CHANNEL_SUPPORTED.includes(channel?.toLowerCase());

  React.useEffect(() => {
    if (!isSupported) return;
    setLoading(true);
    setError('');
    const API_BASE = (import.meta as any).env?.VITE_API_URL || 'http://localhost:8080/api/v1';
    const tenantId = (window as any).__TENANT_ID__ || '';
    fetch(`${API_BASE}/marketplace/listings/${listingId}/analytics?days=${days}`, {
      headers: { 'X-Tenant-Id': tenantId },
    })
      .then(r => r.json())
      .then(d => setData(d))
      .catch(() => setError('Failed to load analytics data.'))
      .finally(() => setLoading(false));
  }, [listingId, days, isSupported]);

  const cardStyle: React.CSSProperties = {
    background: 'var(--bg-secondary)', border: '1px solid var(--border)', borderRadius: 10, padding: '20px 24px',
  };

  function KpiCard({ label, value, unit }: { label: string; value: string | number | undefined; unit?: string }) {
    return (
      <div style={{ background: 'var(--bg-primary)', border: '1px solid var(--border)', borderRadius: 10, padding: '16px 20px', textAlign: 'center' }}>
        <div style={{ fontSize: 24, fontWeight: 800, color: 'var(--text-primary)', marginBottom: 4 }}>
          {value === undefined || value === null ? '—' : value}{unit && value !== undefined && <span style={{ fontSize: 14, fontWeight: 500, color: 'var(--text-muted)', marginLeft: 3 }}>{unit}</span>}
        </div>
        <div style={{ fontSize: 11, fontWeight: 700, color: 'var(--text-muted)', textTransform: 'uppercase', letterSpacing: '0.5px' }}>{label}</div>
      </div>
    );
  }

  if (!isSupported) {
    return (
      <div style={{ ...cardStyle, textAlign: 'center', padding: '40px 24px' }}>
        <div style={{ fontSize: 40, marginBottom: 12 }}>📊</div>
        <h3 style={{ fontSize: 16, fontWeight: 700, color: 'var(--text-primary)', marginBottom: 8 }}>Analytics Not Available</h3>
        <p style={{ fontSize: 14, color: 'var(--text-muted)' }}>
          Performance analytics are currently available for Amazon and eBay listings.
          Support for additional channels is coming soon.
        </p>
      </div>
    );
  }

  return (
    <div>
      {/* Period selector */}
      <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: 20 }}>
        <h3 style={{ fontSize: 16, fontWeight: 700, color: 'var(--text-primary)', margin: 0 }}>📊 Performance</h3>
        <div style={{ display: 'flex', gap: 8 }}>
          {[7, 30, 90].map(d => (
            <button key={d} onClick={() => setDays(d)}
              style={{ padding: '6px 14px', borderRadius: 6, border: '1px solid var(--border)', cursor: 'pointer', fontSize: 13, fontWeight: days === d ? 700 : 500, background: days === d ? 'var(--primary)' : 'var(--bg-secondary)', color: days === d ? '#fff' : 'var(--text-muted)' }}>
              {d}d
            </button>
          ))}
        </div>
      </div>

      {loading && (
        <div style={{ ...cardStyle, textAlign: 'center', padding: 40, color: 'var(--text-muted)', fontSize: 14 }}>
          Loading analytics…
        </div>
      )}

      {error && (
        <div style={{ background: 'var(--danger-glow)', border: '1px solid var(--danger)', borderRadius: 8, padding: '12px 16px', color: 'var(--danger)', fontSize: 13, marginBottom: 16 }}>
          {error}
        </div>
      )}

      {data && !loading && (
        <>
          {!data.supported && (
            <div style={{ ...cardStyle, color: 'var(--text-muted)', fontSize: 14 }}>
              {data.message || 'Analytics not available for this channel.'}
            </div>
          )}

          {data.ok === false && data.error && (
            <div style={{ background: 'var(--warning-glow)', border: '1px solid var(--warning)', borderRadius: 8, padding: '14px 18px', color: 'var(--warning)', fontSize: 13, marginBottom: 16 }}>
              ⚠️ {data.error}
            </div>
          )}

          {data.supported && data.metrics && (
            <>
              <div style={{ display: 'grid', gridTemplateColumns: 'repeat(auto-fill, minmax(150px, 1fr))', gap: 12, marginBottom: 20 }}>
                {data.metrics.revenue !== undefined && (
                  <KpiCard label="Revenue" value={`${data.metrics.currency || ''}${(data.metrics.revenue ?? 0).toFixed(2)}`} />
                )}
                {data.metrics.units_sold !== undefined && (
                  <KpiCard label="Units Sold" value={data.metrics.units_sold ?? 0} />
                )}
                {data.metrics.sessions !== undefined && (
                  <KpiCard label="Sessions" value={data.metrics.sessions ?? 0} />
                )}
                {data.metrics.page_views !== undefined && (
                  <KpiCard label="Page Views" value={data.metrics.page_views ?? 0} />
                )}
                {data.metrics.impressions !== undefined && (
                  <KpiCard label="Impressions" value={data.metrics.impressions ?? 0} />
                )}
                {data.metrics.clicks !== undefined && (
                  <KpiCard label="Clicks" value={data.metrics.clicks ?? 0} />
                )}
                {data.metrics.conversion_rate !== undefined && (
                  <KpiCard label="Conversion" value={(data.metrics.conversion_rate ?? 0).toFixed(2)} unit="%" />
                )}
              </div>

              {/* Simple visual bar: conversion rate */}
              {data.metrics.conversion_rate !== undefined && (
                <div style={cardStyle}>
                  <div style={{ fontSize: 13, fontWeight: 600, color: 'var(--text-muted)', marginBottom: 10 }}>CONVERSION RATE — LAST {days} DAYS</div>
                  <div style={{ background: 'var(--bg-primary)', borderRadius: 6, height: 12, overflow: 'hidden', marginBottom: 8 }}>
                    <div style={{ height: '100%', background: 'var(--primary)', borderRadius: 6, width: `${Math.min((data.metrics.conversion_rate ?? 0), 100)}%`, transition: 'width 0.6s ease' }} />
                  </div>
                  <div style={{ fontSize: 12, color: 'var(--text-muted)' }}>
                    {(data.metrics.conversion_rate ?? 0).toFixed(2)}% of impressions resulted in a purchase
                  </div>
                </div>
              )}

              <div style={{ ...cardStyle, background: 'var(--bg-primary)', border: '1px solid var(--border)', marginTop: 12 }}>
                <div style={{ fontSize: 12, color: 'var(--text-muted)' }}>
                  📅 Period: last {data.period_days || days} days · Channel: {data.channel} · Listing ID: {data.listing_id}
                </div>
              </div>
            </>
          )}
        </>
      )}
    </div>
  );
}
