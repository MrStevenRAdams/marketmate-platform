// ============================================================================
// LISTING CREATE PAGE (FIXED)
// ============================================================================
// Location: frontend/src/pages/marketplace/ListingCreate.tsx

import { useState, useEffect } from 'react';
import { useNavigate, useSearchParams } from 'react-router-dom';
import {
  listingService,
  credentialService,
  MarketplaceCredential,
  CreateListingRequest,
} from '../../services/marketplace-api';
import { productService } from '../../services/api';

const adapterEmoji: Record<string, string> = { amazon: '📦', temu: '🛍️', ebay: '🏷️', shopify: '🛒', tesco: '🏪' };
const adapterColor: Record<string, string> = { amazon: '#FF9900', temu: '#FF6B35', ebay: '#E53238', shopify: '#96BF48', tesco: '#EE1C2E' };

// Channels that have a dedicated full listing form.
// ListingCreate routes to these directly; channels not listed here fall through
// to the generic step-3 title/price form.
const DEDICATED_FORMS: Record<string, string> = {
  amazon:       '/marketplace/amazon/listings/create',
  ebay:         '/marketplace/ebay/listings/create',
  temu:         '/marketplace/temu/listings/create',
  temu_sandbox: '/marketplace/temu/listings/create',
  shopify:      '/marketplace/shopify/listings/create',
  tiktok:       '/marketplace/tiktok/listings/create',
  etsy:         '/marketplace/etsy/listings/create',
  woocommerce:  '/marketplace/woocommerce/listings/create',
  walmart:      '/marketplace/walmart/listings/create',
  kaufland:     '/marketplace/kaufland/listings/create',
  magento:      '/marketplace/magento/listings/create',
  bigcommerce:  '/marketplace/bigcommerce/listings/create',
  onbuy:        '/marketplace/onbuy/listings/create',
  bluepark:     '/marketplace/bluepark/listings/create',
  wish:         '/marketplace/wish/listings/create',
};

interface SimpleProduct { product_id: string; title: string; status: string; product_type?: string; }

export default function ListingCreate() {
  const navigate = useNavigate();
  const [searchParams] = useSearchParams();
  const preselectedProductId = searchParams.get('product_id');
  const preselectedChannel   = searchParams.get('channel') || '';

  const [step, setStep] = useState(preselectedProductId ? 2 : 1);
  const [loading, setLoading] = useState(true);

  const [products, setProducts] = useState<SimpleProduct[]>([]);
  const [credentials, setCredentials] = useState<MarketplaceCredential[]>([]);
  const [productSearch, setProductSearch] = useState('');

  const [selectedProduct, setSelectedProduct] = useState<SimpleProduct | null>(null);
  const [selectedChannel, setSelectedChannel] = useState('');
  const [selectedCredentialId, setSelectedCredentialId] = useState('');
  const [selectedCredentialIds, setSelectedCredentialIds] = useState<Set<string>>(new Set());

  // Form fields (these go into overrides)
  const [title, setTitle] = useState('');
  const [description, setDescription] = useState('');
  const [price, setPrice] = useState('');
  const [quantity, setQuantity] = useState('');

  const [validating, setValidating] = useState(false);
  const [validated, setValidated] = useState(false);
  const [validationErrors, setValidationErrors] = useState<string[]>([]);
  const [publishing, setPublishing] = useState(false);
  const [published, setPublished] = useState(false);
  const [publishError, setPublishError] = useState('');

  useEffect(() => { loadData(); }, []);

  async function loadData() {
    setLoading(true);
    try {
      const [prodRes, credRes] = await Promise.allSettled([
        productService.list({ page_size: 100 }),
        credentialService.list(),
      ]);
      if (prodRes.status === 'fulfilled') {
        const prods = prodRes.value.data?.data || [];
        setProducts(prods);
        if (preselectedProductId) {
          const found = prods.find((p: SimpleProduct) => p.product_id === preselectedProductId);
          if (found) {
            setSelectedProduct(found);
            setTitle(found.title || '');
            // If channel was also supplied (e.g. from unlisted products view),
            // skip the channel picker and go straight to the dedicated form.
            if (preselectedChannel && DEDICATED_FORMS[preselectedChannel]) {
              const creds = credRes.status === 'fulfilled' ? (credRes.value.data?.data || []) : [];
              const credId = creds.find((c: MarketplaceCredential) => c.channel === preselectedChannel && c.active)?.credential_id || '';
              const params = new URLSearchParams({
                product_id: found.product_id,
                ...(credId ? { credential_id: credId } : {}),
              });
              navigate(`${DEDICATED_FORMS[preselectedChannel]}?${params.toString()}`, { replace: true });
              return;
            }
          }
        }
      }
      if (credRes.status === 'fulfilled') setCredentials(credRes.value.data?.data || []);
    } catch { /* ignore */ }
    finally { setLoading(false); }
  }

  const activeCreds = credentials.filter(c => c.active);
  const availableChannels = [...new Set(activeCreds.map(c => c.channel))];
  const channelCreds = activeCreds.filter(c => c.channel === selectedChannel);

  function handleMultiContinue() {
    if (!selectedProduct || selectedCredentialIds.size === 0) return;
    const credArr = activeCreds.filter(c => selectedCredentialIds.has(c.credential_id));
    if (credArr.length === 0) return;
    // For single selection, go directly to the dedicated form
    if (credArr.length === 1) {
      const cr = credArr[0];
      setSelectedChannel(cr.channel);
      setSelectedCredentialId(cr.credential_id);
      const dedicatedPath = DEDICATED_FORMS[cr.channel];
      if (dedicatedPath) {
        navigate(`${dedicatedPath}?product_id=${selectedProduct.product_id}&credential_id=${cr.credential_id}`);
      } else {
        setStep(3);
      }
      return;
    }
    // For multiple selections, route to the first dedicated form with all credential IDs
    // Each channel's form creates the listing then the caller loops
    const cr = credArr[0];
    setSelectedChannel(cr.channel);
    setSelectedCredentialId(cr.credential_id);
    const allCredIds = credArr.map(c => c.credential_id).join(',');
    const dedicatedPath = DEDICATED_FORMS[cr.channel];
    if (dedicatedPath) {
      navigate(`${dedicatedPath}?product_id=${selectedProduct.product_id}&credential_id=${cr.credential_id}&all_credential_ids=${allCredIds}`);
    } else {
      setStep(3);
    }
  }

  function handleContinue() {
    if (!selectedChannel || !selectedProduct) return;
    const dedicatedPath = DEDICATED_FORMS[selectedChannel];
    if (dedicatedPath) {
      const params = new URLSearchParams({
        product_id: selectedProduct.product_id,
        ...(selectedCredentialId ? { credential_id: selectedCredentialId } : {}),
      });
      navigate(`${dedicatedPath}?${params.toString()}`);
    } else {
      setStep(3);
    }
  }

  const filteredProducts = products.filter(p => {
    if (!productSearch) return true;
    const q = productSearch.toLowerCase();
    return p.title?.toLowerCase().includes(q) || p.product_id?.toLowerCase().includes(q);
  });

  function handleValidate() {
    setValidating(true); setValidated(false); setValidationErrors([]);
    setTimeout(() => {
      const errors: string[] = [];
      if (!title.trim()) errors.push('Title is required');
      if (!price || parseFloat(price) <= 0) errors.push('Price must be greater than 0');
      if (!quantity || parseInt(quantity) < 0) errors.push('Quantity must be 0 or greater');
      setValidationErrors(errors);
      setValidated(errors.length === 0);
      setValidating(false);
    }, 800);
  }

  async function handlePublish() {
    if (!selectedProduct || !selectedCredentialId) return;
    setPublishing(true); setPublishError('');
    try {
      // Build payload matching models.CreateListingRequest exactly
      const payload: CreateListingRequest = {
        product_id: selectedProduct.product_id,
        channel: selectedChannel,
        channel_account_id: selectedCredentialId,
        overrides: {
          title: title.trim() || undefined,
          description: description.trim() || undefined,
          price: price ? parseFloat(price) : undefined,
          quantity: quantity ? parseInt(quantity) : undefined,
        },
        auto_publish: true,
      };
      await listingService.create(payload);
      setPublished(true);
    } catch (err: any) {
      setPublishError(err.response?.data?.details || err.response?.data?.error || err.message || 'Failed to create listing');
    } finally { setPublishing(false); }
  }

  if (loading) return <div className="page"><div className="loading-state"><div className="spinner"></div><p>Loading...</p></div></div>;

  if (published) {
    return (
      <div className="page">
        <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'center', minHeight: 400 }}>
          <div style={{ textAlign: 'center' }}>
            <div style={{ width: 80, height: 80, borderRadius: '50%', background: 'var(--success-glow)', border: '2px solid var(--success)', display: 'flex', alignItems: 'center', justifyContent: 'center', fontSize: 40, margin: '0 auto 20px' }}>✓</div>
            <h2 style={{ fontSize: 22, fontWeight: 700, marginBottom: 8 }}>Listing Created!</h2>
            <p style={{ color: 'var(--text-secondary)', marginBottom: 24 }}><strong>{title}</strong> has been submitted to <span style={{ textTransform: 'capitalize' }}>{selectedChannel}</span></p>
            <div style={{ display: 'flex', gap: 12, justifyContent: 'center' }}>
              <button className="btn btn-secondary" onClick={() => navigate('/marketplace/listings')}>View All Listings</button>
              <button className="btn btn-primary" onClick={() => { setStep(1); setSelectedProduct(null); setSelectedChannel(''); setSelectedCredentialId(''); setTitle(''); setDescription(''); setPrice(''); setQuantity(''); setValidated(false); setPublished(false); }}>Create Another</button>
            </div>
          </div>
        </div>
      </div>
    );
  }

  return (
    <div className="page">
      <div style={{ display: 'flex', alignItems: 'center', gap: 12, marginBottom: 24 }}>
        <button className="btn btn-secondary" onClick={() => navigate('/marketplace/listings')} style={{ padding: '8px 14px' }}>← Back</button>
        <h1 className="page-title">Create Listing</h1>
      </div>

      {/* Stepper */}
      <div style={{ display: 'flex', gap: 0, marginBottom: 32, alignItems: 'center' }}>
        {[{ n: 1, l: 'Select Product' }, { n: 2, l: 'Choose Channel' }, { n: 3, l: 'Configure & Publish' }].map((s, i) => (
          <div key={s.n} style={{ flex: 1, display: 'flex', alignItems: 'center' }}>
            <div style={{ display: 'flex', alignItems: 'center', gap: 10 }}>
              <div style={{ width: 32, height: 32, borderRadius: '50%', display: 'flex', alignItems: 'center', justifyContent: 'center', fontSize: 14, fontWeight: 700, background: step >= s.n ? 'var(--primary)' : 'var(--bg-tertiary)', color: step >= s.n ? '#fff' : 'var(--text-muted)', border: `2px solid ${step >= s.n ? 'var(--primary)' : 'var(--border-bright)'}` }}>{s.n}</div>
              <span style={{ fontSize: 13, fontWeight: step === s.n ? 700 : 400, color: step === s.n ? 'var(--text-primary)' : 'var(--text-muted)' }}>{s.l}</span>
            </div>
            {i < 2 && <div style={{ flex: 1, height: 2, margin: '0 16px', background: step > s.n ? 'var(--primary)' : 'var(--border)' }} />}
          </div>
        ))}
      </div>

      {/* Step 1 */}
      {step === 1 && (
        <div className="card" style={{ padding: 24 }}>
          <h3 style={{ fontSize: 16, fontWeight: 700, marginBottom: 16 }}>Select a product to list</h3>
          <input className="input" style={{ width: '100%', marginBottom: 16 }} placeholder="Search products..." value={productSearch} onChange={e => setProductSearch(e.target.value)} />
          {filteredProducts.length === 0 ? (
            <div className="empty-state"><div className="empty-icon">📦</div><h3>No products found</h3><p>Create products first, then come back to list them.</p></div>
          ) : (
            <div style={{ display: 'flex', flexDirection: 'column', gap: 8, maxHeight: 400, overflow: 'auto' }}>
              {filteredProducts.map(p => (
                <div key={p.product_id} onClick={() => { setSelectedProduct(p); setTitle(p.title || ''); setStep(2); }}
                  style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', padding: 16, borderRadius: 10, cursor: 'pointer', background: 'var(--bg-tertiary)', border: '1px solid var(--border-bright)' }}>
                  <div style={{ display: 'flex', alignItems: 'center', gap: 14 }}>
                    <div style={{ width: 44, height: 44, borderRadius: 8, background: 'var(--bg-elevated)', display: 'flex', alignItems: 'center', justifyContent: 'center', fontSize: 20 }}>📦</div>
                    <div><div style={{ fontWeight: 600, fontSize: 14 }}>{p.title}</div><div style={{ fontSize: 12, color: 'var(--text-muted)' }}>ID: {p.product_id.slice(0, 16)}...</div></div>
                  </div>
                  <span className={`badge ${p.status === 'active' ? 'badge-success' : ''}`} style={p.status !== 'active' ? { background: 'var(--bg-tertiary)', color: 'var(--text-secondary)', border: '1px solid var(--border)' } : undefined}>{p.status}</span>
                </div>
              ))}
            </div>
          )}
        </div>
      )}

      {/* Step 2 — select which channel accounts to list on */}
      {step === 2 && (
          <div className="card" style={{ padding: 24 }}>
            <h3 style={{ fontSize: 16, fontWeight: 700, marginBottom: 4 }}>Choose channels to list on</h3>
            <p style={{ fontSize: 13, color: 'var(--text-muted)', marginBottom: 16 }}>
              Product: <strong>{selectedProduct?.title}</strong>
            </p>

            {/* Info banner */}
            <div style={{
              display: 'flex', alignItems: 'flex-start', gap: 10, padding: '12px 14px', marginBottom: 20,
              borderRadius: 8, background: 'rgba(59,130,246,0.08)', border: '1px solid rgba(59,130,246,0.25)',
              fontSize: 13, color: 'var(--text-secondary)', lineHeight: 1.5,
            }}>
              <span style={{ fontSize: 16, flexShrink: 0 }}>ℹ️</span>
              <span>
                Select one or more channel accounts. <strong>Listings will not be created where one already exists on that account</strong> — existing listings are kept as-is.
              </span>
            </div>

            {activeCreds.length === 0 ? (
              <div className="empty-state">
                <div className="empty-icon">🔗</div>
                <h3>No channels connected</h3>
                <p>Connect a marketplace first.</p>
                <button className="btn btn-primary" onClick={() => navigate('/marketplace/connections')}>Connect Marketplace</button>
              </div>
            ) : (
              <>
                <div style={{ display: 'flex', flexDirection: 'column', gap: 8, marginBottom: 24 }}>
                  {activeCreds.map(cr => {
                    const isSelected = selectedCredentialIds.has(cr.credential_id);
                    return (
                      <label key={cr.credential_id}
                        style={{
                          display: 'flex', alignItems: 'center', gap: 14, padding: '14px 16px',
                          borderRadius: 10, cursor: 'pointer',
                          background: isSelected ? 'var(--primary-glow)' : 'var(--bg-tertiary)',
                          border: `1px solid ${isSelected ? 'var(--primary)' : 'var(--border-bright)'}`,
                          transition: 'all 0.15s',
                        }}>
                        <input
                          type="checkbox"
                          checked={isSelected}
                          onChange={() => {
                            setSelectedCredentialIds(prev => {
                              const n = new Set(prev);
                              n.has(cr.credential_id) ? n.delete(cr.credential_id) : n.add(cr.credential_id);
                              return n;
                            });
                          }}
                          style={{ width: 16, height: 16, cursor: 'pointer', flexShrink: 0 }}
                        />
                        <span style={{ fontSize: 24, flexShrink: 0 }}>{adapterEmoji[cr.channel] || '🌐'}</span>
                        <div>
                          <div style={{ fontWeight: 600, fontSize: 14 }}>{cr.account_name}</div>
                          <div style={{ fontSize: 12, color: 'var(--text-muted)', textTransform: 'capitalize' }}>{cr.channel}</div>
                        </div>
                        {isSelected && <span style={{ marginLeft: 'auto', fontSize: 18 }}>✓</span>}
                      </label>
                    );
                  })}
                </div>
                <div style={{ display: 'flex', gap: 12, justifyContent: 'space-between', alignItems: 'center' }}>
                  <div style={{ fontSize: 13, color: 'var(--text-muted)' }}>
                    {selectedCredentialIds.size === 0 ? 'Select at least one channel' : `${selectedCredentialIds.size} channel${selectedCredentialIds.size !== 1 ? 's' : ''} selected`}
                  </div>
                  <div style={{ display: 'flex', gap: 12 }}>
                    <button className="btn btn-secondary" onClick={() => setStep(1)}>← Back</button>
                    <button className="btn btn-primary" onClick={handleMultiContinue} disabled={selectedCredentialIds.size === 0}>
                      Continue →
                    </button>
                  </div>
                </div>
              </>
            )}
          </div>
      )}

      {/* Step 3 */}
      {step === 3 && (
        <div className="card" style={{ padding: 24 }}>
          <div style={{ display: 'flex', alignItems: 'center', gap: 12, marginBottom: 20, padding: 14, background: 'var(--bg-tertiary)', borderRadius: 10 }}>
            <span style={{ fontSize: 24 }}>{adapterEmoji[selectedChannel] || '🌐'}</span>
            <div>
              <div style={{ fontWeight: 600 }}>Listing on <span style={{ textTransform: 'capitalize', color: adapterColor[selectedChannel] || 'var(--primary)' }}>{selectedChannel}</span></div>
              <div style={{ fontSize: 12, color: 'var(--text-muted)' }}>Product: {selectedProduct?.title}</div>
            </div>
          </div>

          {channelCreds.length > 1 && (
            <div style={{ marginBottom: 16 }}>
              <label style={{ display: 'block', fontSize: 12, fontWeight: 600, color: 'var(--text-secondary)', marginBottom: 6, textTransform: 'uppercase' }}>Account</label>
              <select className="select" style={{ width: '100%' }} value={selectedCredentialId} onChange={e => setSelectedCredentialId(e.target.value)}>
                {channelCreds.map(c => <option key={c.credential_id} value={c.credential_id}>{c.account_name}</option>)}
              </select>
            </div>
          )}

          <div style={{ marginBottom: 16 }}>
            <label style={{ display: 'block', fontSize: 12, fontWeight: 600, color: 'var(--text-secondary)', marginBottom: 6, textTransform: 'uppercase' }}>Listing Title <span style={{ color: 'var(--danger)' }}>*</span></label>
            <input className="input" style={{ width: '100%' }} placeholder="Product title for marketplace" value={title} onChange={e => setTitle(e.target.value)} />
          </div>
          <div style={{ marginBottom: 16 }}>
            <label style={{ display: 'block', fontSize: 12, fontWeight: 600, color: 'var(--text-secondary)', marginBottom: 6, textTransform: 'uppercase' }}>Description</label>
            <textarea className="input" style={{ width: '100%', minHeight: 100, resize: 'vertical', fontFamily: 'inherit' }} placeholder="Product description..." value={description} onChange={e => setDescription(e.target.value)} />
          </div>
          <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: 14, marginBottom: 16 }}>
            <div>
              <label style={{ display: 'block', fontSize: 12, fontWeight: 600, color: 'var(--text-secondary)', marginBottom: 6, textTransform: 'uppercase' }}>Price (£) <span style={{ color: 'var(--danger)' }}>*</span></label>
              <input className="input" style={{ width: '100%' }} type="number" step="0.01" placeholder="29.99" value={price} onChange={e => setPrice(e.target.value)} />
            </div>
            <div>
              <label style={{ display: 'block', fontSize: 12, fontWeight: 600, color: 'var(--text-secondary)', marginBottom: 6, textTransform: 'uppercase' }}>Quantity <span style={{ color: 'var(--danger)' }}>*</span></label>
              <input className="input" style={{ width: '100%' }} type="number" placeholder="100" value={quantity} onChange={e => setQuantity(e.target.value)} />
            </div>
          </div>

          {validated && validationErrors.length === 0 && (
            <div style={{ padding: 12, marginBottom: 16, borderRadius: 8, background: 'var(--success-glow)', border: '1px solid var(--success)', color: 'var(--success)', fontSize: 13, fontWeight: 600 }}>✓ Validation passed</div>
          )}
          {validationErrors.length > 0 && (
            <div style={{ padding: 12, marginBottom: 16, borderRadius: 8, background: 'var(--danger-glow)', border: '1px solid var(--danger)', color: 'var(--danger)', fontSize: 13 }}>
              <div style={{ fontWeight: 600, marginBottom: 4 }}>⚠ Validation errors:</div>
              {validationErrors.map((e, i) => <div key={i}>• {e}</div>)}
            </div>
          )}
          {publishError && (
            <div style={{ padding: 12, marginBottom: 16, borderRadius: 8, background: 'var(--danger-glow)', border: '1px solid var(--danger)', color: 'var(--danger)', fontSize: 13, fontWeight: 600 }}>✕ {publishError}</div>
          )}

          <div style={{ display: 'flex', gap: 12, justifyContent: 'flex-end', marginTop: 16 }}>
            <button className="btn btn-secondary" onClick={() => setStep(2)}>← Back</button>
            <button className="btn btn-secondary" onClick={handleValidate} disabled={validating}>{validating ? '⏳ Validating...' : '✓ Validate'}</button>
            <button className="btn btn-primary" onClick={handlePublish} disabled={publishing || !title.trim()}>{publishing ? '⏳ Publishing...' : '🚀 Create & Publish'}</button>
          </div>
        </div>
      )}
    </div>
  );
}
