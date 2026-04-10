import { useState, useEffect, useRef } from 'react';
import { useParams, useSearchParams } from 'react-router-dom';

// ─── Types ────────────────────────────────────────────────────────────────────

interface Message {
  message_id: string;
  direction: 'inbound' | 'outbound' | 'draft';
  body: string;
  sent_by?: string;
  sent_at: string;
}

interface Conversation {
  conversation_id: string;
  channel: string;
  order_number?: string;
  subject?: string;
  status: string;
  customer: { name: string };
  last_message_at: string;
}

const API_BASE = import.meta.env.VITE_API_URL ?? 'http://localhost:8080/api/v1';

const CHANNEL_LABEL: Record<string, string> = {
  amazon: 'Amazon', amazonnew: 'Amazon', ebay: 'eBay',
  temu: 'Temu', manual: 'Manual',
};

const CHANNEL_COLOR: Record<string, string> = {
  amazon: '#FF9900', amazonnew: '#FF9900', ebay: '#E53238',
  temu: '#FF4747', manual: '#60a5fa',
};

function timeAgo(iso: string): string {
  const diff = Date.now() - new Date(iso).getTime();
  const m = Math.floor(diff / 60000);
  if (m < 1) return 'Just now';
  if (m < 60) return `${m}m ago`;
  const h = Math.floor(m / 60);
  if (h < 24) return `${h}h ago`;
  return `${Math.floor(h / 24)}d ago`;
}

// ─── Main Component ───────────────────────────────────────────────────────────

export default function MobileConversation() {
  const { id } = useParams<{ id: string }>();
  const [params] = useSearchParams();
  const tenant = params.get('tenant') ?? '';
  const exp    = params.get('exp') ?? '';
  const token  = params.get('token') ?? '';

  const [conv, setConv]       = useState<Conversation | null>(null);
  const [messages, setMessages] = useState<Message[]>([]);
  const [reply, setReply]     = useState('');
  const [sending, setSending] = useState(false);
  const [error, setError]     = useState('');
  const [sent, setSent]       = useState(false);
  const [expired, setExpired] = useState(false);
  const [loading, setLoading] = useState(true);
  const bottomRef = useRef<HTMLDivElement>(null);
  const textareaRef = useRef<HTMLTextAreaElement>(null);

  // ── Load conversation ──────────────────────────────────────────────────────
  useEffect(() => {
    if (!id || !tenant || !token) { setExpired(true); setLoading(false); return; }

    const expNum = parseInt(exp, 10);
    if (expNum && Date.now() / 1000 > expNum) { setExpired(true); setLoading(false); return; }

    fetch(`${API_BASE}/mobile/conversation/${id}?tenant=${tenant}&exp=${exp}&token=${token}`)
      .then(r => {
        if (r.status === 401) { setExpired(true); return null; }
        return r.json();
      })
      .then(data => {
        if (!data) return;
        setConv(data.conversation);
        setMessages(data.messages ?? []);
      })
      .catch(() => setError('Failed to load conversation'))
      .finally(() => setLoading(false));
  }, [id, tenant, exp, token]);

  // ── Scroll to bottom on new messages ──────────────────────────────────────
  useEffect(() => {
    bottomRef.current?.scrollIntoView({ behavior: 'smooth' });
  }, [messages]);

  // ── Auto-resize textarea ───────────────────────────────────────────────────
  const handleReplyChange = (e: React.ChangeEvent<HTMLTextAreaElement>) => {
    setReply(e.target.value);
    const el = textareaRef.current;
    if (el) { el.style.height = 'auto'; el.style.height = Math.min(el.scrollHeight, 160) + 'px'; }
  };

  // ── Send reply ─────────────────────────────────────────────────────────────
  const handleSend = async () => {
    if (!reply.trim() || sending) return;
    setSending(true);
    setError('');
    try {
      const res = await fetch(
        `${API_BASE}/mobile/conversation/${id}/reply?tenant=${tenant}&exp=${exp}&token=${token}`,
        { method: 'POST', headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ body: reply.trim() }) }
      );
      if (res.status === 401) { setExpired(true); return; }
      if (!res.ok) {
        const d = await res.json().catch(() => ({}));
        setError(d.error ?? 'Failed to send. Please try again.');
        return;
      }
      const now = new Date().toISOString();
      setMessages(prev => [...prev, {
        message_id: `local_${Date.now()}`, direction: 'outbound',
        body: reply.trim(), sent_by: 'You', sent_at: now,
      }]);
      setReply('');
      setSent(true);
      if (textareaRef.current) textareaRef.current.style.height = 'auto';
      setTimeout(() => setSent(false), 3000);
    } finally { setSending(false); }
  };

  const handleKeyDown = (e: React.KeyboardEvent) => {
    if (e.key === 'Enter' && !e.shiftKey) { e.preventDefault(); handleSend(); }
  };

  // ── States ─────────────────────────────────────────────────────────────────
  if (loading) return <Spinner />;
  if (expired) return <Expired />;
  if (!conv)   return <NotFound />;

  const channelColor = CHANNEL_COLOR[conv.channel] ?? '#60a5fa';
  const channelLabel = CHANNEL_LABEL[conv.channel] ?? conv.channel;
  const isResolved = conv.status === 'resolved';
  const isTemu = conv.channel === 'temu';

  return (
    <div style={styles.root}>
      {/* ── Header ── */}
      <div style={styles.header}>
        <div style={{ ...styles.channelBadge, background: channelColor }}>
          {channelLabel}
        </div>
        <div style={styles.headerInfo}>
          <div style={styles.customerName}>{conv.customer.name}</div>
          {conv.order_number && (
            <div style={styles.orderNum}>Order #{conv.order_number}</div>
          )}
        </div>
        <div style={{ ...styles.statusPill, background: isResolved ? '#22c55e22' : '#f59e0b22',
          color: isResolved ? '#22c55e' : '#f59e0b' }}>
          {isResolved ? '✓ Resolved' : '⏳ Open'}
        </div>
      </div>

      {/* ── Subject ── */}
      {conv.subject && (
        <div style={styles.subject}>{conv.subject}</div>
      )}

      {/* ── Message thread ── */}
      <div style={styles.thread}>
        {messages.length === 0 && (
          <div style={styles.emptyThread}>No messages yet</div>
        )}
        {messages.map((msg, i) => {
          const isOut = msg.direction === 'outbound';
          return (
            <div key={msg.message_id ?? i} style={{ ...styles.msgRow, justifyContent: isOut ? 'flex-end' : 'flex-start' }}>
              <div style={{ ...styles.bubble, ...(isOut ? styles.bubbleOut : styles.bubbleIn) }}>
                <div style={styles.bubbleBody}>{msg.body}</div>
                <div style={styles.bubbleMeta}>
                  {isOut ? (msg.sent_by === 'mobile' ? 'You (mobile)' : msg.sent_by ?? 'You') : 'Buyer'}
                  {' · '}{timeAgo(msg.sent_at)}
                </div>
              </div>
            </div>
          );
        })}
        <div ref={bottomRef} />
      </div>

      {/* ── Reply box ── */}
      <div style={styles.replyBox}>
        {isTemu ? (
          <div style={styles.temuWarning}>
            ⚠️ Temu doesn't support external messaging. Please reply via{' '}
            <a href="https://seller.temu.com" target="_blank" rel="noreferrer"
               style={{ color: '#ff4747', textDecoration: 'underline' }}>
              Temu Seller Centre
            </a>
          </div>
        ) : isResolved ? (
          <div style={styles.resolvedNote}>This conversation is resolved. No further replies needed.</div>
        ) : (
          <>
            {error && <div style={styles.errorBar}>{error}</div>}
            {sent && <div style={styles.sentBar}>✓ Message sent to buyer</div>}
            <div style={styles.inputRow}>
              <textarea
                ref={textareaRef}
                style={styles.textarea}
                placeholder="Type your reply to the buyer…"
                value={reply}
                onChange={handleReplyChange}
                onKeyDown={handleKeyDown}
                rows={2}
                disabled={sending}
              />
              <button
                style={{ ...styles.sendBtn, opacity: (!reply.trim() || sending) ? 0.5 : 1 }}
                onClick={handleSend}
                disabled={!reply.trim() || sending}
              >
                {sending ? '…' : '➤'}
              </button>
            </div>
            <div style={styles.hint}>
              Your reply will be sent to the buyer via {channelLabel}
            </div>
          </>
        )}
      </div>
    </div>
  );
}

// ─── Sub-screens ──────────────────────────────────────────────────────────────

function Spinner() {
  return (
    <div style={styles.centerScreen}>
      <div style={styles.spinnerRing} />
      <div style={{ color: '#888', marginTop: 16, fontSize: 14 }}>Loading conversation…</div>
    </div>
  );
}

function Expired() {
  return (
    <div style={styles.centerScreen}>
      <div style={styles.bigIcon}>🔗</div>
      <div style={styles.bigTitle}>Link expired</div>
      <div style={styles.bigSub}>This link is only valid for 24 hours. Please open MarketMate to respond.</div>
    </div>
  );
}

function NotFound() {
  return (
    <div style={styles.centerScreen}>
      <div style={styles.bigIcon}>💬</div>
      <div style={styles.bigTitle}>Conversation not found</div>
      <div style={styles.bigSub}>This conversation may have been deleted or moved.</div>
    </div>
  );
}

// ─── Styles ───────────────────────────────────────────────────────────────────

const styles: Record<string, React.CSSProperties> = {
  root: {
    display: 'flex', flexDirection: 'column',
    height: '100dvh', width: '100%', maxWidth: 640,
    margin: '0 auto', background: '#0f1117',
    fontFamily: "'SF Pro Text', -apple-system, BlinkMacSystemFont, 'Segoe UI', sans-serif",
    color: '#e8e8f0', overflow: 'hidden',
  },
  header: {
    display: 'flex', alignItems: 'center', gap: 12,
    padding: '16px 16px 12px', borderBottom: '1px solid #1e2030',
    background: '#13151f', flexShrink: 0,
  },
  channelBadge: {
    padding: '3px 9px', borderRadius: 20,
    fontSize: 11, fontWeight: 700, letterSpacing: '0.05em',
    color: '#fff', flexShrink: 0, textTransform: 'uppercase',
  },
  headerInfo: { flex: 1, minWidth: 0 },
  customerName: { fontSize: 15, fontWeight: 600, color: '#e8e8f0', overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' },
  orderNum: { fontSize: 12, color: '#666', marginTop: 1 },
  statusPill: {
    padding: '3px 9px', borderRadius: 20,
    fontSize: 11, fontWeight: 600, flexShrink: 0,
  },
  subject: {
    padding: '10px 16px', fontSize: 13, color: '#888',
    borderBottom: '1px solid #1e2030', background: '#13151f',
    flexShrink: 0, fontStyle: 'italic',
  },
  thread: {
    flex: 1, overflowY: 'auto', padding: '16px 12px',
    display: 'flex', flexDirection: 'column', gap: 10,
  },
  emptyThread: { textAlign: 'center', color: '#444', fontSize: 14, marginTop: 40 },
  msgRow: { display: 'flex', width: '100%' },
  bubble: {
    maxWidth: '80%', padding: '10px 14px',
    borderRadius: 18, wordBreak: 'break-word',
  },
  bubbleIn: {
    background: '#1e2030', color: '#e8e8f0',
    borderBottomLeftRadius: 4,
  },
  bubbleOut: {
    background: '#2563eb', color: '#fff',
    borderBottomRightRadius: 4,
  },
  bubbleBody: { fontSize: 15, lineHeight: '1.5', whiteSpace: 'pre-wrap' },
  bubbleMeta: { fontSize: 11, marginTop: 5, opacity: 0.55 },
  replyBox: {
    flexShrink: 0, borderTop: '1px solid #1e2030',
    background: '#13151f', padding: '12px 12px 20px',
  },
  inputRow: { display: 'flex', gap: 8, alignItems: 'flex-end' },
  textarea: {
    flex: 1, background: '#1e2030', border: '1px solid #2a2d3e',
    borderRadius: 14, color: '#e8e8f0', fontSize: 15, padding: '10px 14px',
    resize: 'none', outline: 'none', fontFamily: 'inherit',
    lineHeight: '1.5', minHeight: 44, maxHeight: 160,
    transition: 'border-color 0.2s',
  },
  sendBtn: {
    width: 44, height: 44, borderRadius: '50%',
    background: '#2563eb', border: 'none', color: '#fff',
    fontSize: 18, cursor: 'pointer', flexShrink: 0,
    display: 'flex', alignItems: 'center', justifyContent: 'center',
    transition: 'opacity 0.2s',
  },
  hint: { fontSize: 11, color: '#444', marginTop: 8, textAlign: 'center' },
  errorBar: {
    background: '#ff444422', border: '1px solid #ff444444',
    color: '#ff8888', borderRadius: 8, padding: '8px 12px',
    fontSize: 13, marginBottom: 10,
  },
  sentBar: {
    background: '#22c55e22', border: '1px solid #22c55e44',
    color: '#22c55e', borderRadius: 8, padding: '8px 12px',
    fontSize: 13, marginBottom: 10,
  },
  temuWarning: {
    background: '#ff474722', border: '1px solid #ff474744',
    color: '#ff8888', borderRadius: 8, padding: '12px',
    fontSize: 14, textAlign: 'center',
  },
  resolvedNote: {
    color: '#555', fontSize: 13, textAlign: 'center', padding: '8px 0',
  },
  centerScreen: {
    height: '100dvh', display: 'flex', flexDirection: 'column',
    alignItems: 'center', justifyContent: 'center',
    background: '#0f1117', color: '#e8e8f0', padding: 32,
    fontFamily: "'SF Pro Text', -apple-system, sans-serif",
  },
  bigIcon: { fontSize: 48, marginBottom: 16 },
  bigTitle: { fontSize: 20, fontWeight: 700, marginBottom: 8 },
  bigSub: { fontSize: 14, color: '#666', textAlign: 'center', maxWidth: 280, lineHeight: '1.6' },
  spinnerRing: {
    width: 40, height: 40, borderRadius: '50%',
    border: '3px solid #1e2030', borderTopColor: '#2563eb',
    animation: 'spin 0.8s linear infinite',
  },
};

// Inject keyframe for spinner
if (typeof document !== 'undefined') {
  const style = document.createElement('style');
  style.textContent = `@keyframes spin { to { transform: rotate(360deg); } }
  * { box-sizing: border-box; } body { margin: 0; background: #0f1117; }`;
  document.head.appendChild(style);
}
