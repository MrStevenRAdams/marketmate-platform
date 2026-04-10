import { useState, useEffect, useCallback, useRef } from 'react';
import { getActiveTenantId } from '../contexts/TenantContext';
import './Messages.css';

const API_BASE = import.meta.env.VITE_API_URL ?? 'http://localhost:8080/api/v1';

function api(path: string, init?: RequestInit) {
  return fetch(`${API_BASE}${path}`, {
    ...init,
    headers: {
      'Content-Type': 'application/json',
      'X-Tenant-Id': getActiveTenantId(),
      ...(init?.headers || {}),
    },
  });
}

// ─── Types ────────────────────────────────────────────────────────────────────

interface ConversationCustomer { name: string; buyer_id?: string; }

interface Conversation {
  conversation_id: string;
  channel: string;
  channel_account_id?: string;
  marketplace_thread_id?: string;
  order_id?: string;
  order_number?: string;
  customer: ConversationCustomer;
  subject?: string;
  status: string;
  last_message_at: string;
  last_message_preview?: string;
  message_count?: number;
  unread: boolean;
  assigned_to?: AssignedTo;
  has_ai_draft?: boolean;
  last_ai_intent?: string;
  last_ai_action?: string;
  created_at: string;
}

interface Message {
  message_id: string;
  conversation_id: string;
  direction: string;
  body: string;
  sent_by?: string;
  sent_at: string;
  read_at?: string;
}

interface CannedResponse {
  id: string;
  title: string;
  body: string;
  channels?: string[];
}

interface AssignedTo {
  membership_id: string;
  display_name: string;
  email: string;
  assigned_at: string;
  assigned_by?: string;
}

interface Member {
  membership_id: string;
  display_name: string;
  email: string;
  avatar_url?: string;
  notif_channels?: string[];
}

interface AIDraft {
  message_id: string;
  direction: 'draft';
  body: string;
  sent_at: string;
  intent?: string;
  confidence?: number;
  decision_id?: string;
}

interface AIDecision {
  intent: string;
  confidence: number;
  action: string;
  action_reason: string;
  reply_auto_sent: boolean;
  guardrail_triggered: boolean;
  guardrail_reason?: string;
  processed_at: string;
}

// ─── Constants ────────────────────────────────────────────────────────────────

const CHANNEL_ICONS: Record<string, string> = { amazon: '📦', amazonnew: '📦', ebay: '🛒', temu: '🏷️', manual: '📝' };
const CHANNEL_COLOURS: Record<string, string> = { amazon: '#ff9900', amazonnew: '#ff9900', ebay: '#e53238', temu: '#ff4747', manual: '#60a5fa' };

const STATUS_LABELS: Record<string, string> = {
  open: 'Open',
  pending_reply: 'Pending reply',
  resolved: 'Resolved',
};

const FILTER_TABS = [
  { key: '', label: 'All' },
  { key: 'unread', label: 'Unread' },
  { key: 'open', label: 'Open' },
  { key: 'pending_reply', label: 'Pending' },
  { key: 'resolved', label: 'Resolved' },
];

const AMAZON_CHAR_LIMIT = 4000;

function timeAgo(ts: string): string {
  const d = new Date(ts);
  const diff = Date.now() - d.getTime();
  const mins = Math.floor(diff / 60000);
  if (mins < 1) return 'just now';
  if (mins < 60) return `${mins}m ago`;
  const hours = Math.floor(mins / 60);
  if (hours < 24) return `${hours}h ago`;
  const days = Math.floor(hours / 24);
  if (days < 7) return `${days}d ago`;
  return d.toLocaleDateString();
}

function applyTemplate(body: string, conv: Conversation | null): string {
  if (!conv) return body;
  return body
    .replace(/\{order_number\}/g, conv.order_number || '')
    .replace(/\{customer_name\}/g, conv.customer?.name || '');
}

// ─── Conversation List Item ───────────────────────────────────────────────────

function ConvItem({ conv, active, onClick }: { conv: Conversation; active: boolean; onClick: () => void }) {
  const channelColor = CHANNEL_COLOURS[conv.channel] || '#60a5fa';
  return (
    <button
      className={`msg-conv-item ${active ? 'active' : ''} ${conv.unread ? 'unread' : ''}`}
      onClick={onClick}
      style={conv.unread ? { borderLeftColor: channelColor } : {}}
    >
      <div className="msg-conv-top">
        <span className="msg-conv-customer">{conv.customer?.name || 'Unknown'}</span>
        <span className="msg-conv-time">{timeAgo(conv.last_message_at)}</span>
      </div>
      <div className="msg-conv-middle">
        <span className="msg-conv-channel" style={{ color: channelColor }}>
          {CHANNEL_ICONS[conv.channel]} {conv.channel === 'amazonnew' ? 'Amazon' : conv.channel}
        </span>
        {conv.order_number && <span className="msg-conv-order">#{conv.order_number}</span>}
        {conv.unread && <span className="msg-unread-dot" />}
      </div>
      <div className="msg-conv-preview">{conv.last_message_preview || conv.subject || '—'}</div>
      {conv.assigned_to && (
        <div className="msg-conv-assignee">👤 {conv.assigned_to.display_name}</div>
      )}
      {conv.has_ai_draft && (
        <div className="msg-conv-ai-draft">🤖 AI draft ready</div>
      )}
    </button>
  );
}

// ─── Message Bubble ───────────────────────────────────────────────────────────

function MsgBubble({ msg }: { msg: Message }) {
  const outbound = msg.direction === 'outbound';
  return (
    <div className={`msg-bubble-wrap ${outbound ? 'outbound' : 'inbound'}`}>
      <div className={`msg-bubble ${outbound ? 'msg-bubble-out' : 'msg-bubble-in'}`}>
        <div className="msg-bubble-body">{msg.body}</div>
        <div className="msg-bubble-time">
          {new Date(msg.sent_at).toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' })}
          {' · '}
          {new Date(msg.sent_at).toLocaleDateString()}
        </div>
      </div>
    </div>
  );
}

// ─── Assignee Picker ─────────────────────────────────────────────────────────

function AssigneePicker({
  conv,
  members,
  onAssign,
}: {
  conv: Conversation;
  members: Member[];
  onAssign: (membershipId: string | null) => Promise<void>;
}) {
  const [open, setOpen] = useState(false);
  const [assigning, setAssigning] = useState(false);
  const assigned = conv.assigned_to;

  const assign = async (membershipId: string | null) => {
    setAssigning(true);
    setOpen(false);
    try { await onAssign(membershipId); } finally { setAssigning(false); }
  };

  return (
    <div style={{ position: 'relative', display: 'inline-block' }}>
      <button
        className="msg-btn-secondary"
        onClick={() => setOpen(o => !o)}
        disabled={assigning}
        title="Assign to team member"
        style={{ display: 'flex', alignItems: 'center', gap: 6, fontSize: 12 }}
      >
        <span>👤</span>
        <span>{assigning ? 'Assigning…' : assigned ? assigned.display_name : 'Assign'}</span>
        <span style={{ opacity: 0.5, fontSize: 10 }}>▾</span>
      </button>

      {open && (
        <div className="msg-assignee-dropdown">
          {assigned && (
            <button className="msg-assignee-option msg-assignee-unassign" onClick={() => assign(null)}>
              ✕ Unassign
            </button>
          )}
          {members.length === 0 && (
            <div style={{ padding: '8px 12px', fontSize: 12, color: 'var(--text-muted)' }}>No team members</div>
          )}
          {members.map(m => (
            <button
              key={m.membership_id}
              className={`msg-assignee-option ${assigned?.membership_id === m.membership_id ? 'active' : ''}`}
              onClick={() => assign(m.membership_id)}
            >
              <span className="msg-assignee-avatar">
                {m.avatar_url
                  ? <img src={m.avatar_url} alt={m.display_name} />
                  : m.display_name.charAt(0).toUpperCase()}
              </span>
              <div>
                <div style={{ fontWeight: 600, fontSize: 13 }}>{m.display_name}</div>
                <div style={{ fontSize: 11, color: 'var(--text-muted)' }}>{m.email}</div>
              </div>
              {assigned?.membership_id === m.membership_id && (
                <span style={{ marginLeft: 'auto', color: 'var(--success)', fontSize: 12 }}>✓</span>
              )}
            </button>
          ))}
        </div>
      )}
    </div>
  );
}

// ─── AI Draft Bubble ─────────────────────────────────────────────────────────

function AIDraftBubble({
  draft,
  onApprove,
  onDiscard,
  sending,
}: {
  draft: AIDraft;
  onApprove: (body: string) => Promise<void>;
  onDiscard: () => void;
  sending: boolean;
}) {
  const [editing, setEditing] = useState(false);
  const [text, setText] = useState(draft.body);

  const intentColors: Record<string, string> = {
    cancel_request: '#ef4444',
    return_request: '#f59e0b',
    delivery_query: '#06b6d4',
    product_query: '#8b5cf6',
    other: '#64748b',
  };
  const intentColor = intentColors[draft.intent || 'other'] || '#64748b';
  const confidence = draft.confidence ? Math.round(draft.confidence * 100) : null;

  return (
    <div className="msg-ai-draft">
      <div className="msg-ai-draft-header">
        <span className="msg-ai-badge">🤖 AI Draft</span>
        {draft.intent && (
          <span className="msg-ai-intent" style={{ background: intentColor + '20', color: intentColor, border: `1px solid ${intentColor}40` }}>
            {draft.intent.replace(/_/g, ' ')}
          </span>
        )}
        {confidence !== null && (
          <span className="msg-ai-confidence">{confidence}% confidence</span>
        )}
      </div>

      {editing ? (
        <textarea
          className="input msg-ai-edit"
          value={text}
          onChange={e => setText(e.target.value)}
          rows={5}
        />
      ) : (
        <div className="msg-ai-body">{text}</div>
      )}

      <div className="msg-ai-actions">
        <button className="msg-btn-ghost" onClick={() => setEditing(e => !e)}>
          {editing ? '✓ Done editing' : '✏ Edit'}
        </button>
        <button className="msg-btn-ghost msg-ai-discard" onClick={onDiscard}>
          ✕ Discard
        </button>
        <button
          className="msg-btn-primary"
          onClick={() => onApprove(text)}
          disabled={sending || !text.trim()}
        >
          {sending ? '⏳ Sending…' : '✓ Approve & Send'}
        </button>
      </div>
    </div>
  );
}

// ─── Thread Panel ─────────────────────────────────────────────────────────────

function ThreadPanel({
  conv,
  messages,
  cannedResponses,
  members,
  onReply,
  onResolve,
  onAssign,
  onAIProcess,
  sending,
}: {
  conv: Conversation;
  messages: Message[];
  cannedResponses: CannedResponse[];
  members: Member[];
  onReply: (body: string) => Promise<void>;
  onResolve: () => void;
  onAssign: (membershipId: string | null) => Promise<void>;
  onAIProcess: () => Promise<void>;
  sending: boolean;
}) {
  const [replyText, setReplyText] = useState('');
  const [showCanned, setShowCanned] = useState(false);
  const [warning, setWarning] = useState('');
  const [aiProcessing, setAIProcessing] = useState(false);
  const endRef = useRef<HTMLDivElement>(null);
  const charLimit = (conv.channel === 'amazon' || conv.channel === 'amazonnew') ? AMAZON_CHAR_LIMIT : 0;

  // Separate drafts from real messages
  const realMessages = messages.filter((m: any) => m.direction !== 'draft');
  const drafts = messages.filter((m: any) => m.direction === 'draft') as unknown as AIDraft[];

  const handleAIProcess = async () => {
    setAIProcessing(true);
    try { await onAIProcess(); } finally { setAIProcessing(false); }
  };

  const handleDraftApprove = async (body: string) => {
    await send(body);
  };

  const handleDraftDiscard = async (draftId: string) => {
    await fetch(`${API_BASE}/messages/${conv.conversation_id}/drafts/${draftId}`, {
      method: 'DELETE',
      headers: { 'X-Tenant-Id': getActiveTenantId() },
    }).catch(() => {});
  };

  useEffect(() => {
    endRef.current?.scrollIntoView({ behavior: 'smooth' });
  }, [messages]);

  const send = async (bodyOverride?: string) => {
    const body = bodyOverride ?? replyText;
    if (!body.trim()) return;
    setWarning('');
    await onReply(body);
    if (!bodyOverride) setReplyText('');
  };

  const insertCanned = (cr: CannedResponse) => {
    setReplyText(applyTemplate(cr.body, conv));
    setShowCanned(false);
  };

  const channelColor = CHANNEL_COLOURS[conv.channel] || '#60a5fa';

  return (
    <div className="msg-thread">
      {/* Thread header */}
      <div className="msg-thread-header">
        <div className="msg-thread-header-left">
          <div className="msg-thread-customer">{conv.customer?.name || 'Unknown buyer'}</div>
          <div className="msg-thread-meta">
            <span className="msg-channel-badge" style={{ color: channelColor, borderColor: channelColor + '40', background: channelColor + '15' }}>
              {CHANNEL_ICONS[conv.channel]} {conv.channel === 'amazonnew' ? 'Amazon' : conv.channel}
            </span>
            {conv.order_number && (
              <span className="msg-order-ref">Order #{conv.order_number}</span>
            )}
            <span className={`msg-status-badge msg-status-${conv.status}`}>
              {STATUS_LABELS[conv.status] || conv.status}
            </span>
          </div>
        </div>
        <div style={{ display: 'flex', alignItems: 'center', gap: 8 }}>
          <AssigneePicker conv={conv} members={members} onAssign={onAssign} />
          <button className="msg-btn-ghost" onClick={handleAIProcess} disabled={aiProcessing} title="Ask AI agent to analyse this message">
            {aiProcessing ? '⏳' : '🤖'} {aiProcessing ? 'Analysing…' : 'AI Assist'}
          </button>
          {conv.status !== 'resolved' && (
            <button className="msg-btn-ghost" onClick={onResolve}>✓ Mark resolved</button>
          )}
        </div>
      </div>

      {/* Temu notice */}
      {conv.channel === 'temu' && (
        <div className="msg-temu-notice">
          Temu doesn't support external messaging.{' '}
          <a href="https://seller.temu.com" target="_blank" rel="noopener noreferrer">
            Open Temu Seller Centre →
          </a>
        </div>
      )}

      {/* Messages */}
      <div className="msg-thread-body">
        {realMessages.length === 0 ? (
          <div className="msg-thread-empty">No messages in this conversation yet.</div>
        ) : (
          realMessages.map(msg => <MsgBubble key={msg.message_id} msg={msg} />)
        )}
        {/* AI Drafts */}
        {drafts.map(draft => (
          <AIDraftBubble
            key={draft.message_id}
            draft={draft}
            onApprove={handleDraftApprove}
            onDiscard={() => handleDraftDiscard(draft.message_id)}
            sending={sending}
          />
        ))}
        <div ref={endRef} />
      </div>

      {/* Reply box */}
      {conv.status !== 'resolved' && (
        <div className="msg-reply-box">
          {warning && <div className="msg-warning">{warning}</div>}
          <div className="msg-reply-toolbar">
            <div className="msg-canned-wrap">
              <button className="msg-btn-ghost msg-canned-btn" onClick={() => setShowCanned(s => !s)}>
                💬 Canned responses
              </button>
              {showCanned && cannedResponses.length > 0 && (
                <div className="msg-canned-dropdown">
                  {cannedResponses
                    .filter(cr => !cr.channels?.length || cr.channels.includes(conv.channel) || (conv.channel === 'amazonnew' && cr.channels.includes('amazon')))
                    .map(cr => (
                      <button key={cr.id} className="msg-canned-option" onClick={() => insertCanned(cr)}>
                        <span className="msg-canned-title">{cr.title}</span>
                        <span className="msg-canned-preview">{cr.body.slice(0, 60)}…</span>
                      </button>
                    ))}
                  {cannedResponses.filter(cr => !cr.channels?.length || cr.channels.includes(conv.channel) || (conv.channel === 'amazonnew' && cr.channels.includes('amazon'))).length === 0 && (
                    <div className="msg-canned-empty">No canned responses for {conv.channel}</div>
                  )}
                </div>
              )}
            </div>
          </div>
          <textarea
            className="msg-reply-textarea"
            placeholder={`Reply to ${conv.customer?.name || 'buyer'}…`}
            value={replyText}
            onChange={e => setReplyText(e.target.value)}
            rows={4}
            maxLength={charLimit || undefined}
          />
          <div className="msg-reply-footer">
            <span className="msg-char-count">
              {charLimit > 0 && `${replyText.length} / ${charLimit}`}
            </span>
            <div className="msg-reply-actions">
              <button
                className="msg-btn-primary"
                onClick={send}
                disabled={sending || !replyText.trim()}
              >
                {sending ? 'Sending…' : 'Send →'}
              </button>
            </div>
          </div>
        </div>
      )}
    </div>
  );
}

// ─── Empty / No-selection state ───────────────────────────────────────────────

function EmptyState({ onSync, syncing }: { onSync: () => void; syncing: boolean }) {
  return (
    <div className="msg-empty-state">
      <div className="msg-empty-icon">💬</div>
      <div className="msg-empty-title">No conversation selected</div>
      <div className="msg-empty-sub">
        Messages from Amazon and eBay buyers will appear here.<br />
        Sync to pull the latest messages from your connected accounts.
      </div>
      <button className="msg-btn-primary" onClick={onSync} disabled={syncing}>
        {syncing ? '⏳ Syncing…' : '🔄 Sync messages'}
      </button>
    </div>
  );
}

// ─── Main Page ────────────────────────────────────────────────────────────────

export default function Messages() {
  const [conversations, setConversations] = useState<Conversation[]>([]);
  const [filtered, setFiltered] = useState<Conversation[]>([]);
  const [activeConv, setActiveConv] = useState<Conversation | null>(null);
  const [messages, setMessages] = useState<Message[]>([]);
  const [cannedResponses, setCannedResponses] = useState<CannedResponse[]>([]);
  const [loading, setLoading] = useState(true);
  const [loadingThread, setLoadingThread] = useState(false);
  const [sending, setSending] = useState(false);
  const [syncing, setSyncing] = useState(false);
  const [filterTab, setFilterTab] = useState('');
  const [search, setSearch] = useState('');
  const [unreadTotal, setUnreadTotal] = useState(0);
  const [members, setMembers] = useState<Member[]>([]);

  // Load conversation list
  const loadConversations = useCallback(async () => {
    setLoading(true);
    try {
      let url = '/messages?';
      if (filterTab === 'unread') url += 'unread=true&';
      else if (filterTab) url += `status=${filterTab}&`;
      const res = await api(url);
      if (!res.ok) throw new Error('Failed');
      const data = await res.json();
      const convs: Conversation[] = data.conversations || [];
      setConversations(convs);
      setUnreadTotal(data.unread || 0);
    } catch { setConversations([]); } finally { setLoading(false); }
  }, [filterTab]);

  // Load canned responses + team members once
  useEffect(() => {
    api('/messages/canned').then(r => r.json()).then(d => setCannedResponses(d.canned_responses || [])).catch(() => {});
    api('/messages/members').then(r => r.json()).then(d => setMembers(d.members || [])).catch(() => {});
  }, []);

  useEffect(() => { loadConversations(); }, [loadConversations]);

  // Client-side search filter
  useEffect(() => {
    if (!search.trim()) { setFiltered(conversations); return; }
    const q = search.toLowerCase();
    setFiltered(conversations.filter(c =>
      c.customer?.name?.toLowerCase().includes(q) ||
      c.order_number?.toLowerCase().includes(q) ||
      c.subject?.toLowerCase().includes(q) ||
      c.last_message_preview?.toLowerCase().includes(q)
    ));
  }, [search, conversations]);

  // Load thread
  const openConversation = async (conv: Conversation) => {
    setActiveConv(conv);
    setLoadingThread(true);
    try {
      const res = await api(`/messages/${conv.conversation_id}`);
      const data = await res.json();
      setMessages(data.messages || []);
      // Mark as read locally
      setConversations(prev => prev.map(c =>
        c.conversation_id === conv.conversation_id ? { ...c, unread: false } : c
      ));
    } catch { setMessages([]); } finally { setLoadingThread(false); }
  };

  const handleReply = async (body: string) => {
    if (!activeConv) return;
    setSending(true);
    try {
      const res = await api(`/messages/${activeConv.conversation_id}/reply`, {
        method: 'POST',
        body: JSON.stringify({ body }),
      });
      const data = await res.json();
      // Append message locally
      if (data.message) {
        setMessages(prev => [...prev, data.message]);
      }
      // Update conversation preview
      setConversations(prev => prev.map(c =>
        c.conversation_id === activeConv.conversation_id
          ? { ...c, last_message_preview: 'You: ' + body.slice(0, 80), last_message_at: new Date().toISOString(), status: 'pending_reply' }
          : c
      ));
      setActiveConv(prev => prev ? { ...prev, status: 'pending_reply' } : prev);
    } finally { setSending(false); }
  };

  const handleResolve = async () => {
    if (!activeConv) return;
    await api(`/messages/${activeConv.conversation_id}/resolve`, { method: 'POST', body: '{}' });
    setActiveConv(prev => prev ? { ...prev, status: 'resolved' } : prev);
    setConversations(prev => prev.map(c =>
      c.conversation_id === activeConv.conversation_id ? { ...c, status: 'resolved' } : c
    ));
  };

  const handleAIProcess = async () => {
    if (!activeConv) return;
    const res = await api(`/messages/${activeConv.conversation_id}/ai-process`, {
      method: 'POST', body: '{}',
    });
    const data = await res.json();
    if (data.ok && data.decision) {
      // Reload thread to pick up any draft/sent messages
      const threadRes = await api(`/messages/${activeConv.conversation_id}`);
      const threadData = await threadRes.json();
      setMessages(threadData.messages || []);
      // Update conversation state
      if (data.decision.action === 'auto_cancelled') {
        setConversations(prev => prev.map(c =>
          c.conversation_id === activeConv.conversation_id
            ? { ...c, last_ai_action: data.decision.action }
            : c
        ));
      }
    }
  };

  const handleAIDiscardDraft = async (draftId: string, convId: string) => {
    await api(`/messages/${convId}/drafts/${draftId}`, { method: 'DELETE' }).catch(() => {});
    const threadRes = await api(`/messages/${convId}`);
    const data = await threadRes.json();
    setMessages(data.messages || []);
  };

  const handleAssign = async (membershipId: string | null) => {
    if (!activeConv) return;
    const res = await api(`/messages/${activeConv.conversation_id}/assign`, {
      method: 'POST',
      body: JSON.stringify({ membership_id: membershipId || '' }),
    });
    const data = await res.json();
    const assignedTo = data.assigned_to || null;
    setActiveConv(prev => prev ? { ...prev, assigned_to: assignedTo } : prev);
    setConversations(prev => prev.map(c =>
      c.conversation_id === activeConv.conversation_id ? { ...c, assigned_to: assignedTo } : c
    ));
  };

  const handleSync = async () => {
    setSyncing(true);
    try {
      await api('/messages/sync', { method: 'POST', body: '{}' });
      await loadConversations();
    } finally { setSyncing(false); }
  };

  return (
    <div className="msg-page">
      {/* Left panel */}
      <div className="msg-left">
        {/* Header */}
        <div className="msg-left-header">
          <div className="msg-left-header-top">
            <h2>Messages {unreadTotal > 0 && <span className="msg-unread-badge">{unreadTotal}</span>}</h2>
            <button className="msg-btn-icon" title="Sync" onClick={handleSync} disabled={syncing}>
              {syncing ? '⏳' : '🔄'}
            </button>
          </div>
          <input
            className="msg-search"
            placeholder="Search conversations…"
            value={search}
            onChange={e => setSearch(e.target.value)}
          />
        </div>

        {/* Filter tabs */}
        <div className="msg-filter-tabs">
          {FILTER_TABS.map(tab => (
            <button
              key={tab.key}
              className={`msg-filter-tab ${filterTab === tab.key ? 'active' : ''}`}
              onClick={() => setFilterTab(tab.key)}
            >
              {tab.label}
              {tab.key === 'unread' && unreadTotal > 0 && (
                <span className="msg-tab-badge">{unreadTotal}</span>
              )}
            </button>
          ))}
        </div>

        {/* Conversation list */}
        <div className="msg-conv-list">
          {loading ? (
            <div className="msg-list-loading">Loading…</div>
          ) : filtered.length === 0 ? (
            <div className="msg-list-empty">
              {search ? 'No results' : 'No conversations'}
            </div>
          ) : (
            filtered.map(conv => (
              <ConvItem
                key={conv.conversation_id}
                conv={conv}
                active={activeConv?.conversation_id === conv.conversation_id}
                onClick={() => openConversation(conv)}
              />
            ))
          )}
        </div>
      </div>

      {/* Right panel */}
      <div className="msg-right">
        {activeConv ? (
          loadingThread ? (
            <div className="msg-thread-loading">Loading conversation…</div>
          ) : (
            <ThreadPanel
              conv={activeConv}
              messages={messages}
              cannedResponses={cannedResponses}
              members={members}
              onReply={handleReply}
              onResolve={handleResolve}
              onAssign={handleAssign}
              onAIProcess={handleAIProcess}
              sending={sending}
            />
          )
        ) : (
          <EmptyState onSync={handleSync} syncing={syncing} />
        )}
      </div>
    </div>
  );
}
