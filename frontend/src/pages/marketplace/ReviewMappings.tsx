// ============================================================================
// REVIEW MAPPINGS PAGE
// ============================================================================

import { useState, useEffect, useCallback } from 'react';
import { useNavigate } from 'react-router-dom';
import { apiFetch } from '../../services/apiFetch';

interface MatchRow {
  row_id: string; job_id: string; channel: string; external_id: string;
  sku: string; title: string; image_url: string; price: string;
  match_type: 'exact' | 'fuzzy' | 'none'; match_score: number; match_reason: string;
  matched_product_id: string; matched_product_title: string; matched_product_sku: string;
  matched_product_image: string; matched_product_asin: string;
  decision: '' | 'accepted' | 'rejected' | 'import_as_new';
}
interface MatchResults { exact: MatchRow[]; fuzzy: MatchRow[]; unmatched: MatchRow[]; }
type Tab = 'exact' | 'fuzzy' | 'unmatched';

function scoreColor(s: number) { return s >= 0.85 ? '#10b981' : s >= 0.65 ? '#f59e0b' : '#ef4444'; }
function scoreLabel(s: number) { return s >= 0.85 ? 'High' : s >= 0.65 ? 'Medium' : 'Low'; }

function decisionBadge(d: string) {
  if (d === 'accepted')      return <span style={{ background:'#d1fae5',color:'#065f46',padding:'2px 8px',borderRadius:99,fontSize:11,fontWeight:700 }}>✓ Merged</span>;
  if (d === 'import_as_new') return <span style={{ background:'#dbeafe',color:'#1e40af',padding:'2px 8px',borderRadius:99,fontSize:11,fontWeight:700 }}>+ Importing…</span>;
  if (d === 'rejected')      return <span style={{ background:'#fee2e2',color:'#991b1b',padding:'2px 8px',borderRadius:99,fontSize:11,fontWeight:700 }}>✕ Kept Separate</span>;
  return null;
}

function channelLabel(ch: string) {
  const m: Record<string,string> = { amazon:'📦 Amazon',ebay:'🏷️ eBay',temu:'🛍️ Temu',shopify:'🛒 Shopify',etsy:'🎨 Etsy',tiktok:'🎵 TikTok',woocommerce:'🔌 WooCommerce',walmart:'🏪 Walmart' };
  return m[ch] || ch.charAt(0).toUpperCase()+ch.slice(1);
}

function ProductCard({ imageUrl, title, sku, asin, label, accent }: { imageUrl:string;title:string;sku?:string;asin?:string;label:string;accent:string }) {
  return (
    <div style={{ flex:1,minWidth:0 }}>
      <div style={{ fontSize:10,fontWeight:700,color:accent,textTransform:'uppercase',letterSpacing:1,marginBottom:6 }}>{label}</div>
      <div style={{ display:'flex',gap:10,alignItems:'center' }}>
        {imageUrl
          ? <img src={imageUrl} alt="" style={{ width:48,height:48,objectFit:'contain',borderRadius:6,border:'1px solid var(--border)',background:'#fff',flexShrink:0 }} />
          : <div style={{ width:48,height:48,borderRadius:6,border:'1px solid var(--border)',background:'var(--bg-secondary)',flexShrink:0,display:'flex',alignItems:'center',justifyContent:'center',fontSize:20,color:'var(--text-muted)' }}>□</div>
        }
        <div style={{ minWidth:0 }}>
          <div style={{ fontSize:13,fontWeight:600,color:'var(--text-primary)',whiteSpace:'nowrap',overflow:'hidden',textOverflow:'ellipsis' }}>{title||'(No title)'}</div>
          {sku  && <div style={{ fontSize:11,color:'var(--text-muted)',marginTop:2 }}>SKU: {sku}</div>}
          {asin && <div style={{ fontSize:11,color:'var(--text-muted)' }}>ASIN: {asin}</div>}
        </div>
      </div>
    </div>
  );
}

export default function ReviewMappings() {
  const navigate = useNavigate();
  const [tab, setTab]             = useState<Tab>('exact');
  const [results, setResults]     = useState<MatchResults>({ exact:[],fuzzy:[],unmatched:[] });
  const [loading, setLoading]     = useState(true);
  const [error, setError]         = useState('');
  const [selected, setSelected]   = useState<Set<string>>(new Set());
  const [busyRows, setBusyRows]   = useState<Set<string>>(new Set());
  const [bulkBusy, setBulkBusy]   = useState(false);
  const [processing, setProcessing] = useState<Set<string>>(new Set());
  const [processingMsg, setProcessingMsg] = useState('');
  const [page, setPage]           = useState(1);
  const [pageSize, setPageSize]   = useState(25);

  const load = useCallback(async () => {
    try {
      const res = await apiFetch('/marketplace/pending-review');
      if (!res.ok) { setError('Failed to load pending review items'); return; }
      const data = await res.json();
      const r = data.results || {};
      setResults({
        exact:     Array.isArray(r.exact)     ? r.exact     : [],
        fuzzy:     Array.isArray(r.fuzzy)     ? r.fuzzy     : [],
        unmatched: Array.isArray(r.unmatched) ? r.unmatched : [],
      });
    } catch { setError('Network error loading pending review items'); }
    finally  { setLoading(false); }
  }, []);

  useEffect(() => { load(); }, [load]);

  function groupByJob(ids: string[], rows: MatchRow[]): Record<string,string[]> {
    const map: Record<string,string[]> = {};
    const rm = Object.fromEntries(rows.map(r => [r.row_id, r.job_id]));
    for (const id of ids) { const j = rm[id]; if (!j) continue; (map[j] ??= []).push(id); }
    return map;
  }

  async function acceptRow(row: MatchRow) {
    setBusyRows(p => new Set([...p,row.row_id]));
    try {
      await apiFetch(`/marketplace/import/jobs/${row.job_id}/matches/accept`,{ method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({row_ids:[row.row_id]}) });
      await load(); setSelected(p => { const n=new Set(p); n.delete(row.row_id); return n; });
    } finally { setBusyRows(p => { const n=new Set(p); n.delete(row.row_id); return n; }); }
  }

  async function rejectRow(row: MatchRow) {
    setBusyRows(p => new Set([...p,row.row_id]));
    try {
      await apiFetch(`/marketplace/import/jobs/${row.job_id}/matches/reject`,{ method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({row_ids:[row.row_id]}) });
      await load(); setSelected(p => { const n=new Set(p); n.delete(row.row_id); return n; });
    } finally { setBusyRows(p => { const n=new Set(p); n.delete(row.row_id); return n; }); }
  }

  async function importNewRow(row: MatchRow) {
    setBusyRows(p => new Set([...p,row.row_id]));
    setProcessing(p => new Set([...p,row.row_id]));
    try {
      await apiFetch(`/marketplace/import/jobs/${row.job_id}/unmatched/import-new`,{ method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({row_ids:[row.row_id]}) });
      await load(); setSelected(p => { const n=new Set(p); n.delete(row.row_id); return n; });
    } finally {
      setBusyRows(p => { const n=new Set(p); n.delete(row.row_id); return n; });
      setProcessing(p => { const n=new Set(p); n.delete(row.row_id); return n; });
    }
  }

  async function bulkAccept() {
    if (!selected.size) return;
    const allRows = [...results.exact,...results.fuzzy,...results.unmatched];
    setBulkBusy(true);
    try {
      const byJob = groupByJob([...selected], allRows);
      await Promise.all(Object.entries(byJob).map(([j,ids]) =>
        apiFetch(`/marketplace/import/jobs/${j}/matches/accept`,{ method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({row_ids:ids}) })
      ));
      await load(); setSelected(new Set());
    } finally { setBulkBusy(false); }
  }

  async function bulkReject() {
    if (!selected.size) return;
    const allRows = [...results.exact,...results.fuzzy,...results.unmatched];
    setBulkBusy(true);
    try {
      const byJob = groupByJob([...selected], allRows);
      await Promise.all(Object.entries(byJob).map(([j,ids]) =>
        apiFetch(`/marketplace/import/jobs/${j}/matches/reject`,{ method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({row_ids:ids}) })
      ));
      await load(); setSelected(new Set());
    } finally { setBulkBusy(false); }
  }

  async function bulkImportNew() {
    if (!selected.size) return;
    const allRows = [...results.exact,...results.fuzzy,...results.unmatched];
    const ids = [...selected];
    setProcessing(new Set(ids));
    setProcessingMsg(`Queuing ${ids.length} product${ids.length!==1?'s':''} for import — they'll appear in your catalogue shortly.`);
    setSelected(new Set());
    setBulkBusy(true);
    try {
      const byJob = groupByJob(ids, allRows);
      await Promise.all(Object.entries(byJob).map(([j,rids]) =>
        apiFetch(`/marketplace/import/jobs/${j}/unmatched/import-new`,{ method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({row_ids:rids}) })
      ));
      await load();
    } finally { setBulkBusy(false); setProcessing(new Set()); }
  }

  async function importAllUnmatched() {
    const undecided = results.unmatched.filter(r => r.decision==='' && !processing.has(r.row_id));
    if (!undecided.length) return;
    const ids = undecided.map(r => r.row_id);
    setProcessing(new Set(ids));
    setProcessingMsg(`Queuing all ${ids.length} product${ids.length!==1?'s':''} for import. This runs in the background — your catalogue will update automatically over the next few minutes.`);
    setBulkBusy(true);
    try {
      const byJob = groupByJob(ids, results.unmatched);
      await Promise.all(Object.entries(byJob).map(([j,rids]) =>
        apiFetch(`/marketplace/import/jobs/${j}/unmatched/import-new`,{ method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({row_ids:rids}) })
      ));
      await load();
    } finally { setBulkBusy(false); setProcessing(new Set()); }
  }

  function toggleSelectAll(rows: MatchRow[]) {
    const undecided = rows.filter(r => r.decision==='' && !processing.has(r.row_id));
    const allSel = undecided.length>0 && undecided.every(r => selected.has(r.row_id));
    setSelected(p => { const n=new Set(p); undecided.forEach(r => allSel ? n.delete(r.row_id) : n.add(r.row_id)); return n; });
  }

  const vis = (rows: MatchRow[]) => rows.filter(r => !processing.has(r.row_id));
  const exactUndecided     = vis(results.exact).filter(r => r.decision==='').length;
  const fuzzyUndecided     = vis(results.fuzzy).filter(r => r.decision==='').length;
  const unmatchedUndecided = vis(results.unmatched).filter(r => r.decision==='').length;
  const totalUndecided     = exactUndecided + fuzzyUndecided + unmatchedUndecided;
  const total              = results.exact.length + results.fuzzy.length + results.unmatched.length;
  const activeRows         = tab==='exact' ? vis(results.exact) : tab==='fuzzy' ? vis(results.fuzzy) : vis(results.unmatched);
  const totalPages         = Math.max(1, Math.ceil(activeRows.length / pageSize));
  const safePage           = Math.min(page, totalPages);
  const pageRows           = activeRows.slice((safePage-1)*pageSize, safePage*pageSize);
  const selectedOnTab      = [...selected].filter(id => activeRows.some(r => r.row_id===id));
  const allTabUndecided    = activeRows.filter(r => r.decision==='');
  // "Select all on page" state — checked when every undecided row on the current page is selected
  const pageUndecided      = pageRows.filter(r => r.decision==='');
  const allPageSelected    = pageUndecided.length>0 && pageUndecided.every(r => selected.has(r.row_id));
  const somePageSelected   = pageUndecided.some(r => selected.has(r.row_id)) && !allPageSelected;

  if (loading) return <div className="page"><div className="loading-state"><div className="spinner"/><p>Loading pending reviews…</p></div></div>;
  if (error)   return <div className="page"><div style={{ padding:20,background:'var(--danger-glow)',borderRadius:8,color:'var(--danger)' }}>{error}</div></div>;

  if (total===0 && !processingMsg) return (
    <div className="page">
      <div style={{ marginBottom:24 }}>
        <h1 style={{ fontSize:22,fontWeight:700,color:'var(--text-primary)',marginBottom:4 }}>Review Mappings</h1>
        <p style={{ color:'var(--text-secondary)',fontSize:14 }}>Review products downloaded from any marketplace before they enter your catalogue.</p>
      </div>
      <div style={{ textAlign:'center',padding:'60px 0' }}>
        <div style={{ fontSize:56,marginBottom:16 }}>✅</div>
        <h2 style={{ fontSize:20,fontWeight:700,color:'var(--text-primary)',marginBottom:8 }}>All caught up!</h2>
        <p style={{ color:'var(--text-secondary)',fontSize:14,marginBottom:24 }}>No products are waiting for review. When a marketplace is connected or activated, any newly discovered products will appear here.</p>
        <button className="btn btn-secondary" onClick={() => navigate('/products')}>← Back to Products</button>
      </div>
    </div>
  );

  return (
    <div className="page">
      <div style={{ display:'flex',alignItems:'flex-start',justifyContent:'space-between',marginBottom:20 }}>
        <div>
          <h1 style={{ fontSize:22,fontWeight:700,color:'var(--text-primary)',marginBottom:4 }}>Review Mappings</h1>
          <p style={{ color:'var(--text-secondary)',fontSize:14 }}>
            {totalUndecided>0 ? `${totalUndecided} product${totalUndecided!==1?'s':''} waiting for your decision`
              : processingMsg ? 'Processing your selections…'
              : 'All decisions made — changes are being applied'}
          </p>
        </div>
        <button className="btn btn-secondary" onClick={() => navigate('/products')} style={{ padding:'8px 16px' }}>← Products</button>
      </div>

      {processingMsg && (
        <div style={{ padding:'12px 16px',background:'var(--primary-glow)',border:'1px solid var(--primary)',borderRadius:8,marginBottom:16,display:'flex',alignItems:'center',gap:10 }}>
          <div style={{ width:14,height:14,border:'2px solid var(--primary)',borderTopColor:'transparent',borderRadius:'50%',animation:'spin 0.8s linear infinite',flexShrink:0 }}/>
          <div>
            <span style={{ fontSize:13,fontWeight:600,color:'var(--primary)' }}>{processingMsg}</span>
            <span style={{ fontSize:12,color:'var(--text-muted)',marginLeft:8 }}>Products will appear in your catalogue automatically — you can navigate away.</span>
          </div>
          <button style={{ marginLeft:'auto',background:'none',border:'none',color:'var(--text-muted)',cursor:'pointer',fontSize:18,lineHeight:1 }} onClick={() => setProcessingMsg('')}>×</button>
        </div>
      )}

      <div style={{ display:'flex',gap:0,borderBottom:'2px solid var(--border)',marginBottom:16 }}>
        {([
          { key:'exact',     label:'Exact Matches',    count:vis(results.exact).length,     undecided:exactUndecided,     tip:'Same ASIN or SKU found in an existing product' },
          { key:'fuzzy',     label:'Possible Matches', count:vis(results.fuzzy).length,     undecided:fuzzyUndecided,     tip:'Similar title found — verify before deciding' },
          { key:'unmatched', label:'No Match',          count:vis(results.unmatched).length, undecided:unmatchedUndecided, tip:'No existing product found — import as new' },
        ] as const).map(t => (
          <button key={t.key} onClick={() => { setTab(t.key); setSelected(new Set()); setPage(1); }} title={t.tip}
            style={{ padding:'10px 20px',fontSize:13,fontWeight:600,border:'none',cursor:'pointer',background:'transparent',color:tab===t.key?'var(--primary)':'var(--text-muted)',borderBottom:tab===t.key?'2px solid var(--primary)':'2px solid transparent',marginBottom:-2,display:'flex',alignItems:'center',gap:8 }}>
            {t.label}
            {t.count>0 && (
              <span style={{ background:t.undecided>0?(tab===t.key?'var(--primary)':'#f59e0b'):'var(--bg-tertiary)',color:t.undecided>0?'#fff':'var(--text-muted)',borderRadius:99,fontSize:11,fontWeight:700,padding:'1px 7px',lineHeight:'16px' }}>
                {t.count}
              </span>
            )}
          </button>
        ))}
      </div>

      {allTabUndecided.length>0 && (
        <div style={{ display:'flex',alignItems:'center',gap:8,padding:'8px 12px',background:'var(--bg-secondary)',border:'1px solid var(--border)',borderRadius:8,marginBottom:12 }}>
          {/* Select All checkbox — prominent, acts on current page */}
          <label style={{ display:'flex',alignItems:'center',gap:8,cursor:'pointer',userSelect:'none',flexShrink:0 }}>
            <input
              type="checkbox"
              checked={allPageSelected}
              ref={el => { if (el) el.indeterminate = somePageSelected; }}
              onChange={() => {
                setSelected(p => {
                  const n = new Set(p);
                  if (allPageSelected) {
                    pageUndecided.forEach(r => n.delete(r.row_id));
                  } else {
                    pageUndecided.forEach(r => n.add(r.row_id));
                  }
                  return n;
                });
              }}
              style={{ accentColor:'var(--primary)', width:16, height:16, cursor:'pointer' }}
            />
            <span style={{ fontSize:13,fontWeight:600,color:'var(--text-secondary)' }}>
              {allPageSelected ? 'Deselect page' : `Select page`}
            </span>
          </label>

          {/* Select ALL across all pages */}
          {allTabUndecided.length > pageSize && (
            <button
              onClick={() => toggleSelectAll(activeRows)}
              style={{ fontSize:12,background:'none',border:'none',color:'var(--primary)',cursor:'pointer',textDecoration:'underline',padding:0 }}>
              {allTabUndecided.every(r => selected.has(r.row_id))
                ? 'Deselect all'
                : `Select all ${allTabUndecided.length}`}
            </button>
          )}

          {selectedOnTab.length > 0 && <>
            <span style={{ color:'var(--border)',margin:'0 4px' }}>|</span>
            <span style={{ fontSize:12,color:'var(--text-muted)',fontWeight:600 }}>{selectedOnTab.length} selected</span>
            {(tab==='exact'||tab==='fuzzy') && <>
              <button className="btn btn-primary" style={{ padding:'5px 14px',fontSize:12 }} onClick={bulkAccept} disabled={bulkBusy}>
                {bulkBusy?'…':`✓ Merge ${selectedOnTab.length}`}
              </button>
              <button className="btn btn-secondary" style={{ padding:'5px 14px',fontSize:12 }} onClick={bulkReject} disabled={bulkBusy}>
                {bulkBusy?'…':'✕ Keep separate'}
              </button>
            </>}
            {tab==='unmatched' && (
              <button className="btn btn-primary" style={{ padding:'5px 14px',fontSize:12 }} onClick={bulkImportNew} disabled={bulkBusy}>
                {bulkBusy?'…':`+ Import ${selectedOnTab.length}`}
              </button>
            )}
            <button style={{ background:'none',border:'none',color:'var(--text-muted)',cursor:'pointer',fontSize:12 }} onClick={() => setSelected(new Set())}>Clear</button>
          </>}

          {/* Import all — right-aligned, only for unmatched with nothing selected */}
          {tab==='unmatched' && selectedOnTab.length===0 && unmatchedUndecided>0 && (
            <button className="btn btn-primary" style={{ marginLeft:'auto',fontSize:12,padding:'5px 16px' }} onClick={importAllUnmatched} disabled={bulkBusy}>
              {bulkBusy?'⏳ Queuing…':`Import all ${unmatchedUndecided}`}
            </button>
          )}

          {/* Page size selector — right-aligned */}
          <div style={{ marginLeft: tab==='unmatched' && selectedOnTab.length===0 ? '12px' : 'auto', display:'flex',alignItems:'center',gap:6,flexShrink:0 }}>
            <span style={{ fontSize:12,color:'var(--text-muted)' }}>Show</span>
            <select
              value={pageSize}
              onChange={e => { setPageSize(Number(e.target.value)); setPage(1); setSelected(new Set()); }}
              style={{ fontSize:12,background:'var(--bg-tertiary)',border:'1px solid var(--border)',borderRadius:4,color:'var(--text-primary)',padding:'2px 6px',cursor:'pointer' }}>
              {[10,25,50,100].map(n => <option key={n} value={n}>{n} per page</option>)}
            </select>
          </div>
        </div>
      )}

      {activeRows.length===0 ? (
        <div style={{ textAlign:'center',padding:'40px 0',color:'var(--text-muted)',fontSize:14 }}>
          {processingMsg ? '⏳ Products are being processed and will appear in your catalogue shortly.'
            : `No ${tab==='exact'?'exact matches':tab==='fuzzy'?'possible matches':'unmatched products'} to review.`}
        </div>
      ) : (
        <>
          <div style={{ display:'flex',flexDirection:'column',gap:8 }}>
            {pageRows.map(row => {
            const isBusy=busyRows.has(row.row_id), isSelected=selected.has(row.row_id), isDone=row.decision!=='';
            return (
              <div key={row.row_id} style={{ background:'var(--bg-secondary)',border:`1px solid ${isSelected?'var(--primary)':'var(--border)'}`,borderRadius:10,padding:'14px 16px',opacity:isDone?0.6:1,transition:'border-color 0.15s,opacity 0.2s' }}>
                <div style={{ display:'flex',gap:12,alignItems:'flex-start' }}>
                  {!isDone
                    ? <input type="checkbox" checked={isSelected} onChange={e => setSelected(p => { const n=new Set(p); e.target.checked?n.add(row.row_id):n.delete(row.row_id); return n; })} style={{ marginTop:2,accentColor:'var(--primary)',flexShrink:0 }} />
                    : <div style={{ width:16,flexShrink:0 }}/>
                  }
                  <div style={{ flex:1,minWidth:0 }}>
                    <div style={{ display:'flex',alignItems:'center',gap:8,marginBottom:10,flexWrap:'wrap' }}>
                      <span style={{ fontSize:11,padding:'2px 8px',borderRadius:99,background:'var(--bg-tertiary)',color:'var(--text-muted)',fontWeight:600 }}>{channelLabel(row.channel)}</span>
                      {row.match_type!=='none' && <span style={{ fontSize:11,fontWeight:700,color:scoreColor(row.match_score) }}>{scoreLabel(row.match_score)} confidence ({Math.round(row.match_score*100)}%)</span>}
                      {row.match_reason && <span style={{ fontSize:11,color:'var(--text-muted)' }}>{row.match_reason}</span>}
                      {isDone && decisionBadge(row.decision)}
                    </div>
                    <div style={{ display:'flex',gap:16,alignItems:'flex-start' }}>
                      <ProductCard imageUrl={row.image_url} title={row.title} sku={row.sku} label="From marketplace" accent="#f59e0b"/>
                      {row.matched_product_id && <>
                        <div style={{ display:'flex',alignItems:'center',color:'var(--text-muted)',fontSize:18,flexShrink:0,paddingTop:16 }}>→</div>
                        <ProductCard imageUrl={row.matched_product_image} title={row.matched_product_title} sku={row.matched_product_sku} asin={row.matched_product_asin} label="Existing in catalogue" accent="var(--primary)"/>
                      </>}
                      {tab==='unmatched' && !row.matched_product_id && (
                        <div style={{ flex:1,display:'flex',alignItems:'center',justifyContent:'center',padding:'12px 0',color:'var(--text-muted)',fontSize:13,fontStyle:'italic' }}>No existing product found — will be created as new</div>
                      )}
                    </div>
                  </div>
                  {!isDone && (
                    <div style={{ display:'flex',flexDirection:'column',gap:6,flexShrink:0 }}>
                      {(tab==='exact'||tab==='fuzzy') && <>
                        <button onClick={() => acceptRow(row)} disabled={isBusy} style={{ padding:'6px 14px',borderRadius:6,border:'none',background:'#10b981',color:'#fff',fontSize:12,fontWeight:700,cursor:'pointer',opacity:isBusy?0.6:1 }} title="Merge with existing product">
                          {isBusy?'…':'✓ Merge'}
                        </button>
                        <button onClick={() => rejectRow(row)} disabled={isBusy} style={{ padding:'6px 14px',borderRadius:6,border:'1px solid var(--border)',background:'var(--bg-tertiary)',color:'var(--text-secondary)',fontSize:12,fontWeight:600,cursor:'pointer',opacity:isBusy?0.6:1 }}>
                          {isBusy?'…':'Keep separate'}
                        </button>
                      </>}
                      {tab==='unmatched' && (
                        <button onClick={() => importNewRow(row)} disabled={isBusy} style={{ padding:'6px 14px',borderRadius:6,border:'none',background:'var(--primary)',color:'#fff',fontSize:12,fontWeight:700,cursor:'pointer',opacity:isBusy?0.6:1 }}>
                          {isBusy?'…':'+ Import'}
                        </button>
                      )}
                    </div>
                  )}
                </div>
              </div>
            );
          })}
          </div>

          {/* Pagination footer */}
          {totalPages > 1 && (
            <div style={{ display:'flex',alignItems:'center',justifyContent:'center',gap:8,marginTop:16,paddingTop:16,borderTop:'1px solid var(--border)' }}>
              <button onClick={() => { setPage(1); setSelected(new Set()); }} disabled={safePage===1}
                style={{ padding:'5px 10px',borderRadius:6,border:'1px solid var(--border)',background:'var(--bg-secondary)',color:'var(--text-muted)',cursor:safePage===1?'default':'pointer',fontSize:12,opacity:safePage===1?0.4:1 }}>«</button>
              <button onClick={() => { setPage(p => Math.max(1,p-1)); setSelected(new Set()); }} disabled={safePage===1}
                style={{ padding:'5px 12px',borderRadius:6,border:'1px solid var(--border)',background:'var(--bg-secondary)',color:'var(--text-muted)',cursor:safePage===1?'default':'pointer',fontSize:12,opacity:safePage===1?0.4:1 }}>‹ Prev</button>
              {Array.from({ length:totalPages },(_,i)=>i+1)
                .filter(n => n===1||n===totalPages||Math.abs(n-safePage)<=2)
                .reduce<(number|'…')[]>((acc,n,i,arr) => { if(i>0&&n-(arr[i-1] as number)>1) acc.push('…'); acc.push(n); return acc; },[])
                .map((n,i) => n==='…'
                  ? <span key={`e${i}`} style={{ fontSize:12,color:'var(--text-muted)',padding:'0 4px' }}>…</span>
                  : <button key={n} onClick={() => { setPage(n as number); setSelected(new Set()); }}
                      style={{ padding:'5px 11px',borderRadius:6,border:'1px solid var(--border)',fontSize:12,cursor:'pointer',fontWeight:n===safePage?700:400,background:n===safePage?'var(--primary)':'var(--bg-secondary)',color:n===safePage?'#fff':'var(--text-secondary)' }}>{n}</button>
                )}
              <button onClick={() => { setPage(p => Math.min(totalPages,p+1)); setSelected(new Set()); }} disabled={safePage===totalPages}
                style={{ padding:'5px 12px',borderRadius:6,border:'1px solid var(--border)',background:'var(--bg-secondary)',color:'var(--text-muted)',cursor:safePage===totalPages?'default':'pointer',fontSize:12,opacity:safePage===totalPages?0.4:1 }}>Next ›</button>
              <button onClick={() => { setPage(totalPages); setSelected(new Set()); }} disabled={safePage===totalPages}
                style={{ padding:'5px 10px',borderRadius:6,border:'1px solid var(--border)',background:'var(--bg-secondary)',color:'var(--text-muted)',cursor:safePage===totalPages?'default':'pointer',fontSize:12,opacity:safePage===totalPages?0.4:1 }}>»</button>
              <span style={{ fontSize:12,color:'var(--text-muted)',marginLeft:8 }}>
                {(safePage-1)*pageSize+1}–{Math.min(safePage*pageSize,activeRows.length)} of {activeRows.length}
              </span>
            </div>
          )}
        </>
      )}

      {total>0 && totalUndecided===0 && !processingMsg && (
        <div style={{ marginTop:24,padding:'14px 18px',background:'#d1fae5',border:'1px solid #10b981',borderRadius:8,display:'flex',alignItems:'center',gap:12 }}>
          <span style={{ fontSize:20 }}>✅</span>
          <div>
            <div style={{ fontSize:14,fontWeight:700,color:'#065f46' }}>All decisions made</div>
            <div style={{ fontSize:12,color:'#047857',marginTop:2 }}>Your catalogue is being updated in the background.</div>
          </div>
          <button className="btn btn-secondary" style={{ marginLeft:'auto',fontSize:12 }} onClick={() => navigate('/products')}>View Products →</button>
        </div>
      )}
      <style>{`@keyframes spin { to { transform: rotate(360deg); } }`}</style>
    </div>
  );
}
