import { useState, useEffect } from 'react';
import { getActiveTenantId } from '../../contexts/TenantContext';
import { useAuth } from '../../contexts/AuthContext';

const API_BASE = import.meta.env.VITE_API_URL || 'http://localhost:8080/api/v1';
function api(path: string, init?: RequestInit) {
  return fetch(`${API_BASE}${path}`, {
    ...init,
    headers: { 'Content-Type': 'application/json', 'X-Tenant-Id': getActiveTenantId(), ...init?.headers },
  });
}

interface Profile {
  user_id: string;
  email: string;
  display_name: string;
  avatar_url?: string;
  created_at: string;
  last_login_at: string;
}

export default function ProfileSettings() {
  const { user } = useAuth();
  const [profile, setProfile] = useState<Profile | null>(null);
  const [displayName, setDisplayName] = useState('');
  const [avatarUrl, setAvatarUrl] = useState('');
  const [saving, setSaving] = useState(false);
  const [saved, setSaved] = useState(false);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => { loadProfile(); }, []);

  async function loadProfile() {
    try {
      const res = await api('/user/profile');
      if (res.ok) {
        const data = await res.json();
        const p: Profile = data.profile;
        setProfile(p);
        setDisplayName(p.display_name || '');
        setAvatarUrl(p.avatar_url || '');
      }
    } catch {
      setError('Failed to load profile');
    }
  }

  async function saveProfile() {
    setSaving(true);
    setError(null);
    setSaved(false);
    try {
      const res = await api('/user/profile', {
        method: 'PUT',
        body: JSON.stringify({ display_name: displayName, avatar_url: avatarUrl }),
      });
      if (!res.ok) {
        const data = await res.json();
        setError(data.error || 'Failed to save profile');
        return;
      }
      setSaved(true);
      setTimeout(() => setSaved(false), 3000);
      await loadProfile();
    } finally {
      setSaving(false);
    }
  }

  const initials = (displayName || user?.email || '?').charAt(0).toUpperCase();

  return (
    <div style={{ padding: '32px 40px', maxWidth: 640, margin: '0 auto' }}>
      <div style={{ marginBottom: 28 }}>
        <h1 style={{ margin: 0, color: 'var(--text-primary)', fontSize: 22, fontWeight: 700 }}>👤 Profile Settings</h1>
        <p style={{ margin: '4px 0 0', color: 'var(--text-muted)', fontSize: 14 }}>Manage your personal account details</p>
      </div>

      {/* Avatar */}
      <div style={{ display: 'flex', alignItems: 'center', gap: 20, marginBottom: 28, padding: 20, background: 'var(--bg-secondary)', borderRadius: 12, border: '1px solid var(--border)' }}>
        <div style={{
          width: 72, height: 72, borderRadius: '50%', overflow: 'hidden',
          background: 'var(--primary)', display: 'flex', alignItems: 'center', justifyContent: 'center',
          fontSize: 28, color: '#fff', fontWeight: 700, flexShrink: 0,
        }}>
          {avatarUrl ? (
            <img src={avatarUrl} alt="avatar" style={{ width: '100%', height: '100%', objectFit: 'cover' }} />
          ) : initials}
        </div>
        <div>
          <div style={{ fontWeight: 600, color: 'var(--text-primary)', marginBottom: 4 }}>{displayName || 'No display name'}</div>
          <div style={{ fontSize: 13, color: 'var(--text-muted)' }}>{profile?.email || user?.email || '—'}</div>
        </div>
      </div>

      {/* Form */}
      <div style={{ background: 'var(--bg-secondary)', borderRadius: 12, border: '1px solid var(--border)', padding: 24 }}>
        <div style={{ marginBottom: 18 }}>
          <label style={{ display: 'block', fontSize: 13, fontWeight: 600, color: 'var(--text-secondary)', marginBottom: 6 }}>
            Display Name
          </label>
          <input
            type="text"
            value={displayName}
            onChange={e => setDisplayName(e.target.value)}
            placeholder="Your full name"
            style={{
              width: '100%', padding: '9px 12px', background: 'var(--bg-elevated)',
              border: '1px solid var(--border)', borderRadius: 8, color: 'var(--text-primary)',
              fontSize: 14, boxSizing: 'border-box',
            }}
          />
        </div>

        <div style={{ marginBottom: 18 }}>
          <label style={{ display: 'block', fontSize: 13, fontWeight: 600, color: 'var(--text-secondary)', marginBottom: 6 }}>
            Email Address <span style={{ fontWeight: 400, color: 'var(--text-muted)' }}>(read-only — managed via authentication)</span>
          </label>
          <input
            type="email"
            value={profile?.email || user?.email || ''}
            readOnly
            style={{
              width: '100%', padding: '9px 12px', background: 'var(--bg-tertiary)',
              border: '1px solid var(--border)', borderRadius: 8, color: 'var(--text-muted)',
              fontSize: 14, boxSizing: 'border-box', cursor: 'not-allowed',
            }}
          />
        </div>

        <div style={{ marginBottom: 24 }}>
          <label style={{ display: 'block', fontSize: 13, fontWeight: 600, color: 'var(--text-secondary)', marginBottom: 6 }}>
            Avatar URL
          </label>
          <input
            type="url"
            value={avatarUrl}
            onChange={e => setAvatarUrl(e.target.value)}
            placeholder="https://example.com/avatar.jpg"
            style={{
              width: '100%', padding: '9px 12px', background: 'var(--bg-elevated)',
              border: '1px solid var(--border)', borderRadius: 8, color: 'var(--text-primary)',
              fontSize: 14, boxSizing: 'border-box',
            }}
          />
          <p style={{ margin: '5px 0 0', fontSize: 12, color: 'var(--text-muted)' }}>
            Paste a direct link to your profile photo
          </p>
        </div>

        {error && (
          <div style={{ background: 'rgba(239,68,68,0.1)', border: '1px solid rgba(239,68,68,0.3)', borderRadius: 8, padding: '10px 14px', color: '#f87171', fontSize: 13, marginBottom: 16 }}>
            {error}
          </div>
        )}

        {saved && (
          <div style={{ background: 'rgba(34,197,94,0.1)', border: '1px solid rgba(34,197,94,0.3)', borderRadius: 8, padding: '10px 14px', color: '#4ade80', fontSize: 13, marginBottom: 16 }}>
            ✓ Profile saved successfully
          </div>
        )}

        <button
          onClick={saveProfile}
          disabled={saving}
          style={{
            padding: '10px 24px', background: saving ? 'rgba(99,102,241,0.4)' : 'var(--primary)',
            border: 'none', borderRadius: 8, color: '#fff', fontWeight: 600,
            fontSize: 14, cursor: saving ? 'not-allowed' : 'pointer',
          }}
        >
          {saving ? 'Saving…' : 'Save Changes'}
        </button>
      </div>

      {profile && (
        <div style={{ marginTop: 20, fontSize: 12, color: 'var(--text-muted)', padding: '0 4px' }}>
          Member since {new Date(profile.created_at).toLocaleDateString('en-GB', { day: 'numeric', month: 'long', year: 'numeric' })}
        </div>
      )}
    </div>
  );
}
