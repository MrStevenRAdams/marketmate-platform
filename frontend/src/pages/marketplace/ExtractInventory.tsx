// ============================================================================
// EXTRACT INVENTORY PAGE
// ============================================================================
// Location: frontend/src/pages/marketplace/ExtractInventory.tsx
// Route: /marketplace/extract
// Implements: IMP-01 (Extract Inventory workflow) + CLM-01 (full extract tool)
//
// Flow: Pick channel → Browse live listings → Select → Extract into MarketMate
// ============================================================================

import { useState, useEffect, useCallback, useRef } from 'react';
import { useNavigate } from 'react-router-dom';
import { extractApi, ExtractChannel, ExtractListing, ExtractResult } from '../../services/extract-api';

// ── Channel display config ────────────────────────────────────────────────────
const CHANNEL_META: Record<string, { emoji: string; color: string; label: string }> = {
  amazon:      { emoji: '📦', color: '#FF9900', label: 'Amazon'      },
  ebay:        { emoji: '🏷️',  color: '#E53238', label: 'eBay'        },
  shopify:     { emoji: '🛒', color: '#96BF48', label: 'Shopify'     },
  walmart:     { emoji: '🛍️', color: '#0071CE', label: 'Walmart'     },
  // ── Session 4 ─────────────────────────────────────────────────────────
  backmarket:  { emoji: '♻️',  color: '#14B8A6', label: 'Back Market' },
  zalando:     { emoji: '👗', color: '#FF6600', label: 'Zalando'     },
  bol:         { emoji: '🏪', color: '#0E4299', label: 'Bol.com'     },
  lazada:      { emoji: '🛒', color: '#F57224', label: 'Lazada'      },
};

// ── Styles ────────────────────────────────────────────────────────────────────
const cardStyle: React.CSSProperties = {
  background: 'var(--bg-secondary)', borderRadius: 12, border: '1px solid var(--border)',
  padding: 24, marginBottom: 20,
};
const sectionTitle: React.CSSProperties = {
  fontSize: 15, fontWeight: 700, color: 'var(--text-primary)', marginBottom: 16,
  paddingBottom: 10, borderBottom: '1px solid var(--border)',
};
const inputStyle: React.CSSProperties = {
  width: '100%', padding: '10px 14px', borderRadius: 8,
  background: 'var(--bg-primary)', border: '1px solid var(--border-bright)',
  color: 'var(--text-primary)', fontSize: 14, outline: 'none', boxSizing: 'border-box',
};

type WizardStep = 'channel' | 'browse' | 'confirm' | 'done';

export default function ExtractInventory() {
  const navigate = useNavigate();

  // ── Wizard state ──────────────────────────────────────────────────────────
  const [step, setStep] = useState<WizardStep>('channel');

  // Step 1: Channel selection
  const [channels, setChannels] = useState<ExtractChannel[]>([]);
  const [channelsLoading, setChannelsLoading] = useState(true);
  const [selectedChannel, setSelectedChannel] = useState<ExtractChannel | null>(null);

  // Step 2: Browsing
  const [listings, setListings] = useState<ExtractListing[]>([]);
  const [browseLoading, setBrowseLoading] = useState(false);
  const [browseError, setBrowseError] = useState<string | null>(null);
  const [browseNote, setBrowseNote] = useState<string | null>(null);
  const [searchTerm, setSearchTerm] = useState('');
  const [cursor, setCursor] = useState<string | undefined>(undefined);
  const [hasMore, setHasMore] = useState(false);
  const [selectedIds, setSelectedIds] = useState<Set<string>>(new Set());

  // Step 3: Confirm + extract
  const [extracting, setExtracting] = useState(false);
  const [extractResult, setExtractResult] = useState<ExtractResult | null>(null);
  const [extractError, setExtractError] = useState<string | null>(null);

  const searchDebounce = useRef<ReturnType<typeof setTimeout> | null>(null);

  // ── Load channels on mount ─────────────────────────────────────────────────
  useEffect(() => {
    loadChannels();
  }, []);

  async function loadChannels() {
    setChannelsLoading(true);
    try {
      const res = await extractApi.listChannels();
      setChannels(res.data.channels || []);
    } catch {
      setChannels([]);
    } finally {
      setChannelsLoading(false);
    }
  }

  // ── Browse listings ────────────────────────────────────────────────────────
  const browseListings = useCallback(async (
    channel: ExtractChannel,
    search: string,
    nextCursor?: string,
    append = false,
  ) => {
    setBrowseLoading(true);
    setBrowseError(null);
    setBrowseNote(null);
    try {
      const res = await extractApi.browseListings(channel.channel, {
        credential_id: channel.credential_id,
        limit: 50,
        cursor: nextCursor,
        search: search || undefined,
      });
      const data = res.data;
      if (!data.ok) throw new Error(data.error || 'Browse failed');

      const newListings = data.listings || [];
      setListings(prev => append ? [...prev, ...newListings] : newListings);
      setCursor(data.next_cursor || undefined);
      setHasMore(!!data.next_cursor);
      if (data.note) setBrowseNote(data.note);
    } catch (err: any) {
      setBrowseError(err.response?.data?.error || err.message || 'Failed to load listings');
      if (!append) setListings([]);
    } finally {
      setBrowseLoading(false);
    }
  }, []);

  function onChannelSelect(ch: ExtractChannel) {
    setSelectedChannel(ch);
    setStep('browse');
    setSelectedIds(new Set());
    setListings([]);
    setSearchTerm('');
    setCursor(undefined);
    browseListings(ch, '', undefined, false);
  }

  function handleSearchChange(val: string) {
    setSearchTerm(val);
    if (searchDebounce.current) clearTimeout(searchDebounce.current);
    searchDebounce.current = setTimeout(() => {
      if (selectedChannel) {
        setCursor(undefined);
        browseListings(selectedChannel, val, undefined, false);
      }
    }, 400);
  }

  function loadMore() {
    if (selectedChannel && hasMore && !browseLoading) {
      browseListings(selectedChannel, searchTerm, cursor, true);
    }
  }

  function toggleSelect(id: string) {
    setSelectedIds(prev => {
      const next = new Set(prev);
      if (next.has(id)) next.delete(id);
      else next.add(id);
      return next;
    });
  }

  function toggleSelectAll() {
    if (selectedIds.size === listings.length) {
      setSelectedIds(new Set());
    } else {
      setSelectedIds(new Set(listings.map(l => l.external_id)));
    }
  }

  async function handleExtract() {
    if (!selectedChannel || selectedIds.size === 0) return;
    setExtracting(true);
    setExtractError(null);
    try {
      const res = await extractApi.extractListings(selectedChannel.channel, {
        credential_id: selectedChannel.credential_id,
        external_ids: Array.from(selectedIds),
      });
      if (!res.data.ok) throw new Error('Extract failed');
      setExtractResult(res.data.result);
      setStep('done');
    } catch (err: any) {
      setExtractError(err.response?.data?.error || err.message || 'Extraction failed');
    } finally {
      setExtracting(false);
    }
  }

  // ── Render: Step Indicator ─────────────────────────────────────────────────
  const STEPS: { key: WizardStep; label: string }[] = [
    { key: 'channel', label: 'Select Channel' },
    { key: 'browse',  label: 'Browse Listings' },
    { key: 'done',    label: 'Extract' },
  ];
  const stepIdx = step === 'channel' ? 0 : step === 'browse' ? 1 : 2;

  return (
    <div style={{ maxWidth: 1100, margin: '0 auto', padding: '24px 20px' }}>
      {/* Header */}
      <div style={{ marginBottom: 28 }}>
        <button
          onClick={() => navigate('/marketplace/listings')}
          style={{ background: 'none', border: 'none', color: 'var(--text-muted)', cursor: 'pointer', fontSize: 13, padding: 0, marginBottom: 8 }}
        >
          ← Back to Listings
        </button>
        <h1 style={{ fontSize: 26, fontWeight: 800, color: 'var(--text-primary)', margin: '0 0 4px' }}>
          Extract Inventory
        </h1>
        <p style={{ color: 'var(--text-muted)', fontSize: 14, margin: 0 }}>
          Browse your live channel listings and extract them into MarketMate as editable templates.
        </p>
      </div>

      {/* Step indicator */}
      <div style={{ display: 'flex', alignItems: 'center', gap: 0, marginBottom: 32 }}>
        {STEPS.map((s, i) => (
          <div key={s.key} style={{ display: 'flex', alignItems: 'center', flex: i < STEPS.length - 1 ? 1 : 0 }}>
            <div style={{
              display: 'flex', alignItems: 'center', gap: 8,
              color: i <= stepIdx ? 'var(--primary)' : 'var(--text-muted)',
            }}>
              <div style={{
                width: 28, height: 28, borderRadius: '50%', display: 'flex', alignItems: 'center', justifyContent: 'center',
                fontSize: 12, fontWeight: 700,
                background: i < stepIdx ? 'var(--primary)' : i === stepIdx ? 'var(--primary)' : 'var(--bg-tertiary)',
                color: i <= stepIdx ? '#fff' : 'var(--text-muted)',
                border: i <= stepIdx ? 'none' : '1px solid var(--border)',
              }}>
                {i < stepIdx ? '✓' : i + 1}
              </div>
              <span style={{ fontSize: 13, fontWeight: i === stepIdx ? 700 : 400 }}>{s.label}</span>
            </div>
            {i < STEPS.length - 1 && (
              <div style={{ flex: 1, height: 1, background: i < stepIdx ? 'var(--primary)' : 'var(--border)', margin: '0 16px' }} />
            )}
          </div>
        ))}
      </div>

      {/* ── STEP 1: Channel Selection ─────────────────────────────────────── */}
      {step === 'channel' && (
        <div style={cardStyle}>
          <div style={sectionTitle}>Choose a channel to extract from</div>
          {channelsLoading ? (
            <div style={{ textAlign: 'center', padding: 40, color: 'var(--text-muted)' }}>Loading connected channels…</div>
          ) : channels.length === 0 ? (
            <div style={{ textAlign: 'center', padding: 40 }}>
              <div style={{ fontSize: 40, marginBottom: 12 }}>🔌</div>
              <div style={{ fontSize: 16, fontWeight: 600, color: 'var(--text-primary)', marginBottom: 8 }}>No extractable channels connected</div>
              <div style={{ fontSize: 14, color: 'var(--text-muted)', marginBottom: 20 }}>
                Connect Amazon, eBay, Shopify, or Walmart to extract their listings.
              </div>
              <button className="btn btn-primary" onClick={() => navigate('/marketplace/connections')}>
                Connect Channels
              </button>
            </div>
          ) : (
            <div style={{ display: 'grid', gridTemplateColumns: 'repeat(auto-fill, minmax(260px, 1fr))', gap: 16 }}>
              {channels.map(ch => {
                const meta = CHANNEL_META[ch.channel] || { emoji: '🔗', color: 'var(--primary)', label: ch.channel };
                return (
                  <button
                    key={ch.credential_id}
                    onClick={() => onChannelSelect(ch)}
                    style={{
                      background: 'var(--bg-primary)', border: `1px solid var(--border)`,
                      borderRadius: 12, padding: 20, cursor: 'pointer', textAlign: 'left',
                      transition: 'border-color 0.15s, box-shadow 0.15s',
                      display: 'flex', alignItems: 'center', gap: 16,
                    }}
                    onMouseEnter={e => {
                      (e.currentTarget as HTMLElement).style.borderColor = meta.color;
                      (e.currentTarget as HTMLElement).style.boxShadow = `0 0 0 1px ${meta.color}22`;
                    }}
                    onMouseLeave={e => {
                      (e.currentTarget as HTMLElement).style.borderColor = 'var(--border)';
                      (e.currentTarget as HTMLElement).style.boxShadow = 'none';
                    }}
                  >
                    <div style={{ fontSize: 36 }}>{meta.emoji}</div>
                    <div>
                      <div style={{ fontSize: 15, fontWeight: 700, color: 'var(--text-primary)' }}>{meta.label}</div>
                      <div style={{ fontSize: 13, color: 'var(--text-muted)', marginTop: 2 }}>{ch.account_name}</div>
                    </div>
                  </button>
                );
              })}
            </div>
          )}
        </div>
      )}

      {/* ── STEP 2: Browse + Select ───────────────────────────────────────── */}
      {step === 'browse' && selectedChannel && (
        <>
          {/* Channel header strip */}
          <div style={{
            display: 'flex', alignItems: 'center', justifyContent: 'space-between',
            padding: '12px 20px', borderRadius: 10, marginBottom: 16,
            background: 'var(--bg-secondary)', border: '1px solid var(--border)',
          }}>
            <div style={{ display: 'flex', alignItems: 'center', gap: 12 }}>
              <span style={{ fontSize: 24 }}>{CHANNEL_META[selectedChannel.channel]?.emoji || '🔗'}</span>
              <div>
                <div style={{ fontWeight: 700, color: 'var(--text-primary)' }}>
                  {CHANNEL_META[selectedChannel.channel]?.label || selectedChannel.channel}
                </div>
                <div style={{ fontSize: 12, color: 'var(--text-muted)' }}>{selectedChannel.account_name}</div>
              </div>
            </div>
            <button
              onClick={() => { setStep('channel'); setSelectedChannel(null); setListings([]); }}
              style={{ background: 'none', border: '1px solid var(--border)', borderRadius: 8, padding: '6px 14px', cursor: 'pointer', color: 'var(--text-muted)', fontSize: 13 }}
            >
              Change Channel
            </button>
          </div>

          {/* Search + selection toolbar */}
          <div style={{
            display: 'flex', alignItems: 'center', gap: 12, marginBottom: 16, flexWrap: 'wrap',
          }}>
            <input
              style={{ ...inputStyle, maxWidth: 340 }}
              placeholder={`Search ${CHANNEL_META[selectedChannel.channel]?.label || selectedChannel.channel} listings…`}
              value={searchTerm}
              onChange={e => handleSearchChange(e.target.value)}
            />
            <div style={{ marginLeft: 'auto', display: 'flex', alignItems: 'center', gap: 12 }}>
              {selectedIds.size > 0 && (
                <div style={{
                  padding: '6px 14px', borderRadius: 20,
                  background: 'var(--primary)', color: '#fff',
                  fontSize: 13, fontWeight: 600,
                }}>
                  {selectedIds.size} selected
                </div>
              )}
              {listings.length > 0 && (
                <button
                  onClick={toggleSelectAll}
                  style={{ background: 'none', border: '1px solid var(--border)', borderRadius: 8, padding: '6px 14px', cursor: 'pointer', color: 'var(--text-muted)', fontSize: 13 }}
                >
                  {selectedIds.size === listings.length ? 'Deselect All' : 'Select All'}
                </button>
              )}
              <button
                className="btn btn-primary"
                disabled={selectedIds.size === 0 || extracting}
                onClick={() => setStep('confirm')}
                style={{ opacity: selectedIds.size === 0 ? 0.4 : 1 }}
              >
                Extract {selectedIds.size > 0 ? `(${selectedIds.size})` : ''} →
              </button>
            </div>
          </div>

          {/* Browse note */}
          {browseNote && (
            <div style={{ padding: '10px 16px', background: 'var(--info-glow, #e8f4fd)', borderRadius: 8, border: '1px solid var(--info, #3b82f6)', color: 'var(--info, #3b82f6)', fontSize: 13, marginBottom: 16 }}>
              💡 {browseNote}
            </div>
          )}

          {/* Error */}
          {browseError && (
            <div style={{ padding: '10px 16px', background: 'var(--danger-glow)', borderRadius: 8, border: '1px solid var(--danger)', color: 'var(--danger)', fontSize: 13, marginBottom: 16 }}>
              ⚠️ {browseError}
            </div>
          )}

          {/* Listings grid */}
          {browseLoading && listings.length === 0 ? (
            <div style={{ textAlign: 'center', padding: 60, color: 'var(--text-muted)' }}>
              <div style={{ fontSize: 32, marginBottom: 12 }}>⏳</div>
              Loading listings…
            </div>
          ) : listings.length === 0 && !browseLoading ? (
            <div style={{ textAlign: 'center', padding: 60, color: 'var(--text-muted)' }}>
              <div style={{ fontSize: 32, marginBottom: 12 }}>🔍</div>
              {searchTerm ? `No listings found for "${searchTerm}"` : 'No listings found.'}
            </div>
          ) : (
            <>
              <div style={{ display: 'grid', gridTemplateColumns: 'repeat(auto-fill, minmax(280px, 1fr))', gap: 12 }}>
                {listings.map(listing => {
                  const isSelected = selectedIds.has(listing.external_id);
                  return (
                    <div
                      key={listing.external_id}
                      onClick={() => toggleSelect(listing.external_id)}
                      style={{
                        borderRadius: 10, border: `2px solid ${isSelected ? 'var(--primary)' : 'var(--border)'}`,
                        background: isSelected ? 'var(--primary-glow, rgba(99,102,241,0.08))' : 'var(--bg-secondary)',
                        padding: 14, cursor: 'pointer', position: 'relative',
                        transition: 'border-color 0.15s, background 0.15s',
                        display: 'flex', gap: 12,
                      }}
                    >
                      {/* Checkbox */}
                      <div style={{
                        width: 20, height: 20, borderRadius: 6, flexShrink: 0, marginTop: 2,
                        background: isSelected ? 'var(--primary)' : 'var(--bg-primary)',
                        border: `2px solid ${isSelected ? 'var(--primary)' : 'var(--border-bright)'}`,
                        display: 'flex', alignItems: 'center', justifyContent: 'center',
                        color: '#fff', fontSize: 11, fontWeight: 700,
                      }}>
                        {isSelected ? '✓' : ''}
                      </div>

                      {/* Image */}
                      {listing.image_url ? (
                        <img
                          src={listing.image_url}
                          alt={listing.title}
                          style={{ width: 48, height: 48, borderRadius: 6, objectFit: 'cover', flexShrink: 0 }}
                          onError={e => { (e.currentTarget as HTMLImageElement).style.display = 'none'; }}
                        />
                      ) : (
                        <div style={{ width: 48, height: 48, borderRadius: 6, background: 'var(--bg-tertiary)', flexShrink: 0, display: 'flex', alignItems: 'center', justifyContent: 'center', fontSize: 20 }}>
                          {CHANNEL_META[selectedChannel.channel]?.emoji || '📦'}
                        </div>
                      )}

                      <div style={{ flex: 1, minWidth: 0 }}>
                        <div style={{ fontSize: 13, fontWeight: 600, color: 'var(--text-primary)', marginBottom: 4, overflow: 'hidden', textOverflow: 'ellipsis', display: '-webkit-box', WebkitLineClamp: 2, WebkitBoxOrient: 'vertical' }}>
                          {listing.title}
                        </div>
                        <div style={{ fontSize: 11, color: 'var(--text-muted)' }}>
                          {listing.sku && <span>SKU: {listing.sku}</span>}
                          {listing.asin && <span> · ASIN: {listing.asin}</span>}
                        </div>
                        <div style={{ display: 'flex', gap: 8, marginTop: 4, flexWrap: 'wrap' }}>
                          {listing.price != null && listing.price > 0 && (
                            <span style={{ fontSize: 12, color: 'var(--success)', fontWeight: 600 }}>
                              £{listing.price.toFixed(2)}
                            </span>
                          )}
                          {listing.quantity != null && (
                            <span style={{ fontSize: 12, color: 'var(--text-muted)' }}>
                              Qty: {listing.quantity}
                            </span>
                          )}
                          <span style={{
                            fontSize: 11, fontWeight: 600, padding: '1px 6px', borderRadius: 4,
                            background: listing.status === 'active' ? 'var(--success-glow)' : 'var(--bg-tertiary)',
                            color: listing.status === 'active' ? 'var(--success)' : 'var(--text-muted)',
                          }}>
                            {listing.status}
                          </span>
                        </div>
                      </div>
                    </div>
                  );
                })}
              </div>

              {/* Load more */}
              {hasMore && (
                <div style={{ textAlign: 'center', marginTop: 20 }}>
                  <button
                    className="btn btn-secondary"
                    onClick={loadMore}
                    disabled={browseLoading}
                  >
                    {browseLoading ? 'Loading…' : 'Load More Listings'}
                  </button>
                </div>
              )}
              {browseLoading && listings.length > 0 && (
                <div style={{ textAlign: 'center', padding: 12, color: 'var(--text-muted)', fontSize: 13 }}>Loading…</div>
              )}
            </>
          )}
        </>
      )}

      {/* ── STEP 3: Confirm ──────────────────────────────────────────────────── */}
      {step === 'confirm' && selectedChannel && (
        <div style={cardStyle}>
          <div style={sectionTitle}>Confirm Extraction</div>

          <div style={{
            background: 'var(--bg-primary)', borderRadius: 10, border: '1px solid var(--border)',
            padding: 20, marginBottom: 20,
          }}>
            <div style={{ display: 'flex', alignItems: 'center', gap: 12, marginBottom: 16 }}>
              <span style={{ fontSize: 28 }}>{CHANNEL_META[selectedChannel.channel]?.emoji || '🔗'}</span>
              <div>
                <div style={{ fontWeight: 700, color: 'var(--text-primary)', fontSize: 16 }}>
                  {selectedIds.size} listing{selectedIds.size !== 1 ? 's' : ''} from {CHANNEL_META[selectedChannel.channel]?.label || selectedChannel.channel}
                </div>
                <div style={{ fontSize: 13, color: 'var(--text-muted)' }}>{selectedChannel.account_name}</div>
              </div>
            </div>
            <div style={{ fontSize: 13, color: 'var(--text-muted)', lineHeight: 1.6 }}>
              Selected listings will be extracted as <strong style={{ color: 'var(--text-primary)' }}>imported</strong> drafts in MarketMate,
              pre-populated with all available channel data including title, description, images, and attributes.
              You can review and link each to a product in your PIM after extraction.
            </div>
          </div>

          {extractError && (
            <div style={{ padding: '10px 16px', background: 'var(--danger-glow)', borderRadius: 8, border: '1px solid var(--danger)', color: 'var(--danger)', fontSize: 13, marginBottom: 16 }}>
              ⚠️ {extractError}
            </div>
          )}

          <div style={{ display: 'flex', gap: 12 }}>
            <button
              className="btn btn-secondary"
              onClick={() => setStep('browse')}
              disabled={extracting}
            >
              ← Back to Selection
            </button>
            <button
              className="btn btn-primary"
              onClick={handleExtract}
              disabled={extracting}
            >
              {extracting ? 'Extracting…' : `Extract ${selectedIds.size} Listing${selectedIds.size !== 1 ? 's' : ''}`}
            </button>
          </div>
        </div>
      )}

      {/* ── STEP 4: Done ─────────────────────────────────────────────────────── */}
      {step === 'done' && extractResult && (
        <div style={cardStyle}>
          <div style={{ textAlign: 'center', padding: '20px 0 8px' }}>
            <div style={{ fontSize: 56, marginBottom: 12 }}>
              {extractResult.extracted > 0 ? '✅' : '⚠️'}
            </div>
            <div style={{ fontSize: 22, fontWeight: 800, color: 'var(--text-primary)', marginBottom: 8 }}>
              {extractResult.extracted > 0
                ? `${extractResult.extracted} listing${extractResult.extracted !== 1 ? 's' : ''} extracted successfully`
                : 'No listings extracted'}
            </div>
            {extractResult.skipped > 0 && (
              <div style={{ fontSize: 14, color: 'var(--warning)', marginBottom: 8 }}>
                {extractResult.skipped} listing{extractResult.skipped !== 1 ? 's' : ''} skipped
              </div>
            )}
            {extractResult.errors && extractResult.errors.length > 0 && (
              <div style={{ marginTop: 12, textAlign: 'left', maxWidth: 480, margin: '12px auto 0' }}>
                <div style={{ fontSize: 13, fontWeight: 600, color: 'var(--danger)', marginBottom: 8 }}>Errors:</div>
                {extractResult.errors.map((e, i) => (
                  <div key={i} style={{ fontSize: 12, color: 'var(--danger)', padding: '4px 0', borderBottom: '1px solid var(--border)' }}>
                    {e}
                  </div>
                ))}
              </div>
            )}
            <div style={{ marginTop: 28, display: 'flex', gap: 12, justifyContent: 'center', flexWrap: 'wrap' }}>
              <button
                className="btn btn-primary"
                onClick={() => navigate('/marketplace/listings?state=imported')}
              >
                View Extracted Listings
              </button>
              <button
                className="btn btn-secondary"
                onClick={() => {
                  setStep('channel');
                  setSelectedChannel(null);
                  setListings([]);
                  setSelectedIds(new Set());
                  setExtractResult(null);
                  setExtractError(null);
                }}
              >
                Extract More
              </button>
            </div>
          </div>
        </div>
      )}
    </div>
  );
}
