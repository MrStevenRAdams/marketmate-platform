import { useState, useEffect } from 'react';
import { Link } from 'react-router-dom';
import { getActiveTenantId } from '../../contexts/TenantContext';
import { useAuth } from '../../contexts/AuthContext';
import '../../components/SettingsLayout.css';

const API_BASE = import.meta.env.VITE_API_URL || 'http://localhost:8080/api/v1';
function api(path: string, init?: RequestInit) {
  return fetch(`${API_BASE}${path}`, {
    ...init,
    headers: { 'Content-Type': 'application/json', 'X-Tenant-Id': getActiveTenantId(), ...init?.headers },
  });
}

interface NotifPrefs { email: boolean; sms: boolean; whatsapp: boolean; }
interface Profile {
  user_id: string;
  email: string;
  display_name: string;
  avatar_url?: string;
  phone?: string;
  phone_verified: boolean;
  phone_channel?: string;
  notif_prefs: NotifPrefs;
  created_at: string;
  last_login_at: string;
}

const sectionStyle: React.CSSProperties = {
  background: 'var(--bg-secondary)',
  borderRadius: 12,
  border: '1px solid var(--border)',
  padding: 24,
  marginBottom: 20,
};

const labelStyle: React.CSSProperties = {
  display: 'block', fontSize: 13, fontWeight: 600,
  color: 'var(--text-secondary)', marginBottom: 6,
};

const inputStyle: React.CSSProperties = {
  width: '100%', padding: '9px 12px',
  background: 'var(--bg-elevated)', border: '1px solid var(--border)',
  borderRadius: 8, color: 'var(--text-primary)', fontSize: 14,
  boxSizing: 'border-box' as const, outline: 'none',
};

const readOnlyInputStyle: React.CSSProperties = {
  ...inputStyle,
  background: 'var(--bg-tertiary)',
  color: 'var(--text-muted)',
  cursor: 'not-allowed',
};

const btnPrimary = (disabled = false): React.CSSProperties => ({
  padding: '9px 20px',
  background: disabled ? 'var(--bg-elevated)' : 'var(--primary)',
  border: '1px solid var(--border)',
  borderRadius: 8,
  color: disabled ? 'var(--text-muted)' : '#fff',
  fontWeight: 600, fontSize: 14,
  cursor: disabled ? 'not-allowed' : 'pointer',
  whiteSpace: 'nowrap' as const,
  opacity: disabled ? 0.6 : 1,
});

const btnSecondary: React.CSSProperties = {
  padding: '9px 20px',
  background: 'var(--bg-elevated)',
  border: '1px solid var(--border)',
  borderRadius: 8, color: 'var(--text-primary)',
  fontWeight: 600, fontSize: 14,
  cursor: 'pointer', whiteSpace: 'nowrap' as const,
};

const channelBtnStyle = (active: boolean): React.CSSProperties => ({
  padding: '8px 18px', borderRadius: 8, fontWeight: 600, fontSize: 13,
  border: `1px solid ${active ? 'var(--primary)' : 'var(--border)'}`,
  background: active ? 'var(--bg-elevated)' : 'var(--bg-tertiary)',
  color: active ? 'var(--primary)' : 'var(--text-muted)',
  cursor: 'pointer',
  outline: active ? '2px solid var(--primary)' : 'none',
  outlineOffset: -1,
});

export default function ProfileSettings() {
  const { user } = useAuth();
  const [profile, setProfile] = useState<Profile | null>(null);

  // Personal info state
  const [displayName, setDisplayName] = useState('');
  const [avatarUrl, setAvatarUrl] = useState('');
  const [saving, setSaving] = useState(false);
  const [saved, setSaved] = useState(false);

  // Phone state
  const [phone, setPhone] = useState('');
  const [phoneChannel, setPhoneChannel] = useState<'sms' | 'whatsapp'>('sms');
  const [otpSent, setOtpSent] = useState(false);
  const [otp, setOtp] = useState('');
  const [sendingOtp, setSendingOtp] = useState(false);
  const [verifyingOtp, setVerifyingOtp] = useState(false);
  const [phoneVerified, setPhoneVerified] = useState(false);

  // Notif prefs state
  const [notifEmail, setNotifEmail] = useState(true);
  const [notifSms, setNotifSms] = useState(false);
  const [notifWhatsapp, setNotifWhatsapp] = useState(false);
  const [savingNotif, setSavingNotif] = useState(false);
  const [savedNotif, setSavedNotif] = useState(false);

  // Error/success
  const [error, setError] = useState<string | null>(null);
  const [phoneError, setPhoneError] = useState<string | null>(null);
  const [phoneSuccess, setPhoneSuccess] = useState<string | null>(null);

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
        setPhone(p.phone || '');
        setPhoneChannel((p.phone_channel as 'sms' | 'whatsapp') || 'sms');
        setPhoneVerified(p.phone_verified || false);
        if (p.notif_prefs) {
          setNotifEmail(p.notif_prefs.email ?? true);
          setNotifSms(p.notif_prefs.sms ?? false);
          setNotifWhatsapp(p.notif_prefs.whatsapp ?? false);
        }
      }
    } catch { setError('Failed to load profile'); }
  }

  async function saveProfile() {
    setSaving(true); setError(null); setSaved(false);
    try {
      const res = await api('/user/profile', {
        method: 'PUT',
        body: JSON.stringify({ display_name: displayName, avatar_url: avatarUrl }),
      });
      if (!res.ok) { const d = await res.json(); setError(d.error || 'Failed to save'); return; }
      setSaved(true);
      setTimeout(() => setSaved(false), 3000);
      await loadProfile();
    } finally { setSaving(false); }
  }

  async function sendOtp() {
    setPhoneError(null); setPhoneSuccess(null);
    if (!phone.trim()) { setPhoneError('Please enter a phone number'); return; }
    setSendingOtp(true);
    try {
      const res = await api('/user/phone/send-otp', {
        method: 'POST',
        body: JSON.stringify({ phone: phone.trim(), channel: phoneChannel }),
      });
      const data = await res.json();
      if (!res.ok) { setPhoneError(data.error || 'Failed to send code'); return; }
      setOtpSent(true);
      setPhoneSuccess(`Verification code sent via ${phoneChannel === 'whatsapp' ? 'WhatsApp' : 'SMS'}`);
    } finally { setSendingOtp(false); }
  }

  async function verifyOtp() {
    setPhoneError(null); setPhoneSuccess(null);
    if (!otp.trim()) { setPhoneError('Please enter the verification code'); return; }
    setVerifyingOtp(true);
    try {
      const res = await api('/user/phone/verify-otp', {
        method: 'POST',
        body: JSON.stringify({ phone: phone.trim(), code: otp.trim() }),
      });
      const data = await res.json();
      if (!res.ok) { setPhoneError(data.error || 'Incorrect code'); return; }
      setPhoneVerified(true);
      setOtpSent(false);
      setOtp('');
      setPhoneSuccess('✅ Phone number verified successfully');
      await loadProfile();
    } finally { setVerifyingOtp(false); }
  }

  function changePhone() {
    setPhoneVerified(false);
    setOtpSent(false);
    setOtp('');
    setPhoneSuccess(null);
    setPhoneError(null);
  }

  async function saveNotifPrefs() {
    setSavingNotif(true); setSavedNotif(false);
    try {
      const res = await api('/user/notif-prefs', {
        method: 'PUT',
        body: JSON.stringify({
          email: notifEmail,
          sms: notifSms,
          whatsapp: notifWhatsapp,
          phone: phoneVerified ? phone : '',
          email_address: profile?.email || '',
        }),
      });
      if (res.ok) { setSavedNotif(true); setTimeout(() => setSavedNotif(false), 3000); }
    } finally { setSavingNotif(false); }
  }

  const initials = (displayName || user?.email || '?').charAt(0).toUpperCase();

  return (
    <div className="settings-page">
      <div className="settings-breadcrumb">
        <Link to="/settings">Settings</Link>
        <span className="settings-breadcrumb-sep">›</span>
        <span className="settings-breadcrumb-current">My Profile</span>
      </div>

      <h1 className="settings-page-title">My Profile</h1>
      <p className="settings-page-sub">Manage your personal details and notification preferences.</p>

      {/* ── Avatar strip ── */}
      <div style={{ ...sectionStyle, display: 'flex', alignItems: 'center', gap: 20, marginBottom: 20 }}>
        <div style={{
          width: 72, height: 72, borderRadius: '50%', overflow: 'hidden',
          background: 'var(--primary)', display: 'flex', alignItems: 'center',
          justifyContent: 'center', fontSize: 28, color: '#fff', fontWeight: 700, flexShrink: 0,
        }}>
          {avatarUrl
            ? <img src={avatarUrl} alt="avatar" style={{ width: '100%', height: '100%', objectFit: 'cover' }} />
            : initials}
        </div>
        <div>
          <div style={{ fontWeight: 600, color: 'var(--text-primary)', fontSize: 16 }}>
            {displayName || 'No display name'}
          </div>
          <div style={{ fontSize: 13, color: 'var(--text-muted)', marginTop: 2 }}>
            {profile?.email || user?.email || '—'}
          </div>
          {phoneVerified && phone && (
            <div style={{ fontSize: 12, color: '#4ade80', marginTop: 4 }}>
              ✅ {phone} verified
            </div>
          )}
        </div>
      </div>

      {/* ── Personal info ── */}
      <div style={sectionStyle}>
        <div style={{ fontWeight: 700, color: 'var(--text-primary)', marginBottom: 18, fontSize: 15 }}>
          Personal Information
        </div>

        <div style={{ marginBottom: 16 }}>
          <label style={labelStyle}>Display Name</label>
          <input type="text" value={displayName} onChange={e => setDisplayName(e.target.value)}
            placeholder="Your full name" style={inputStyle} />
        </div>

        <div style={{ marginBottom: 16 }}>
          <label style={labelStyle}>
            Email Address <span style={{ fontWeight: 400, color: 'var(--text-muted)' }}>(read-only)</span>
          </label>
          <input type="email" value={profile?.email || user?.email || ''} readOnly style={readOnlyInputStyle} />
        </div>

        <div style={{ marginBottom: 24 }}>
          <label style={labelStyle}>Avatar URL</label>
          <input type="url" value={avatarUrl} onChange={e => setAvatarUrl(e.target.value)}
            placeholder="https://example.com/avatar.jpg" style={inputStyle} />
          <p style={{ margin: '5px 0 0', fontSize: 12, color: 'var(--text-muted)' }}>
            Paste a direct link to your profile photo
          </p>
        </div>

        {error && <div style={{ background: 'rgba(239,68,68,0.1)', border: '1px solid rgba(239,68,68,0.3)', borderRadius: 8, padding: '10px 14px', color: '#f87171', fontSize: 13, marginBottom: 16 }}>{error}</div>}
        {saved && <div style={{ background: 'rgba(34,197,94,0.1)', border: '1px solid rgba(34,197,94,0.3)', borderRadius: 8, padding: '10px 14px', color: '#4ade80', fontSize: 13, marginBottom: 16 }}>✓ Profile saved</div>}

        <button onClick={saveProfile} disabled={saving} style={btnPrimary(saving)}>
          {saving ? 'Saving…' : 'Save Changes'}
        </button>
      </div>

      {/* ── Phone Verification ── */}
      <div style={sectionStyle}>
        <div style={{ fontWeight: 700, color: 'var(--text-primary)', marginBottom: 4, fontSize: 15 }}>
          📱 Phone Number
        </div>
        <p style={{ margin: '0 0 18px', fontSize: 13, color: 'var(--text-muted)' }}>
          Required for WhatsApp and SMS order alerts. Your number is only used for notifications — never shared.
        </p>

        {phoneVerified ? (
          <div>
            <div style={{ display: 'flex', alignItems: 'center', gap: 12, marginBottom: 12 }}>
              <div style={{
                padding: '9px 14px', background: 'rgba(34,197,94,0.1)',
                border: '1px solid rgba(34,197,94,0.3)', borderRadius: 8,
                color: '#4ade80', fontSize: 14, fontWeight: 600, flex: 1,
              }}>
                ✅ {phone}
              </div>
              <button onClick={changePhone} style={btnSecondary}>Change</button>
            </div>
          </div>
        ) : (
          <div>
            {/* Channel selector */}
            <div style={{ marginBottom: 14 }}>
              <label style={labelStyle}>Send code via</label>
              <div style={{ display: 'flex', gap: 8 }}>
                {(['sms', 'whatsapp'] as const).map(ch => (
                  <button key={ch} onClick={() => setPhoneChannel(ch)} style={channelBtnStyle(phoneChannel === ch)}>
                    {ch === 'sms' ? '💬 SMS' : '💚 WhatsApp'}
                  </button>
                ))}
              </div>
            </div>

            {/* Phone input + send button */}
            <div style={{ marginBottom: 14 }}>
              <label style={labelStyle}>Mobile Number</label>
              <div style={{ display: 'flex', gap: 8 }}>
                <input type="tel" value={phone} onChange={e => setPhone(e.target.value)}
                  placeholder="+447700900000" style={{ ...inputStyle, flex: 1 }}
                  disabled={otpSent} />
                <button onClick={sendOtp} disabled={sendingOtp || !phone.trim()} style={btnPrimary(sendingOtp || !phone.trim())}>
                  {sendingOtp ? 'Sending…' : otpSent ? 'Resend' : 'Send Code'}
                </button>
              </div>
              <p style={{ margin: '5px 0 0', fontSize: 12, color: 'var(--text-muted)' }}>
                Include country code, e.g. +44 for UK. No spaces.
              </p>
            </div>

            {/* OTP input */}
            {otpSent && (
              <div style={{ marginBottom: 14 }}>
                <label style={labelStyle}>Verification Code</label>
                <div style={{ display: 'flex', gap: 8 }}>
                  <input type="text" value={otp} onChange={e => setOtp(e.target.value)}
                    placeholder="6-digit code" maxLength={6}
                    style={{ ...inputStyle, flex: 1, letterSpacing: 4, fontSize: 18, fontWeight: 700 }} />
                  <button onClick={verifyOtp} disabled={verifyingOtp || otp.length < 4} style={btnPrimary(verifyingOtp || otp.length < 4)}>
                    {verifyingOtp ? 'Verifying…' : 'Verify'}
                  </button>
                </div>
              </div>
            )}
          </div>
        )}

        {phoneError && <div style={{ background: 'rgba(239,68,68,0.1)', border: '1px solid rgba(239,68,68,0.3)', borderRadius: 8, padding: '10px 14px', color: '#f87171', fontSize: 13, marginTop: 8 }}>{phoneError}</div>}
        {phoneSuccess && <div style={{ background: 'rgba(34,197,94,0.1)', border: '1px solid rgba(34,197,94,0.3)', borderRadius: 8, padding: '10px 14px', color: '#4ade80', fontSize: 13, marginTop: 8 }}>{phoneSuccess}</div>}
      </div>

      {/* ── Notification Preferences ── */}
      <div style={sectionStyle}>
        <div style={{ fontWeight: 700, color: 'var(--text-primary)', marginBottom: 4, fontSize: 15 }}>
          🔔 Notification Preferences
        </div>
        <p style={{ margin: '0 0 18px', fontSize: 13, color: 'var(--text-muted)' }}>
          Choose how you receive alerts when a conversation is assigned to you.
        </p>

        {[
          { key: 'email', label: 'Email', desc: 'Alerts sent to your email address', icon: '✉️', val: notifEmail, set: setNotifEmail },
          { key: 'sms', label: 'SMS', desc: 'Text message alerts (requires verified phone)', icon: '💬', val: notifSms, set: setNotifSms, needsPhone: true },
          { key: 'whatsapp', label: 'WhatsApp', desc: 'WhatsApp message alerts (requires verified phone)', icon: '💚', val: notifWhatsapp, set: setNotifWhatsapp, needsPhone: true },
        ].map(item => (
          <div key={item.key} style={{
            display: 'flex', alignItems: 'center', justifyContent: 'space-between',
            padding: '14px 0', borderBottom: '1px solid var(--border)',
          }}>
            <div style={{ display: 'flex', alignItems: 'center', gap: 12 }}>
              <span style={{ fontSize: 20 }}>{item.icon}</span>
              <div>
                <div style={{ fontWeight: 600, color: 'var(--text-primary)', fontSize: 14 }}>{item.label}</div>
                <div style={{ fontSize: 12, color: 'var(--text-muted)' }}>
                  {item.desc}
                  {item.needsPhone && !phoneVerified && (
                    <span style={{ color: '#f59e0b', marginLeft: 6 }}>⚠ Verify phone first</span>
                  )}
                </div>
              </div>
            </div>
            <label style={{ position: 'relative', display: 'inline-block', width: 44, height: 24, cursor: (item.needsPhone && !phoneVerified) ? 'not-allowed' : 'pointer' }}>
              <input type="checkbox" checked={item.val}
                onChange={e => { if (!item.needsPhone || phoneVerified) item.set(e.target.checked); }}
                disabled={!!(item.needsPhone && !phoneVerified)}
                style={{ opacity: 0, width: 0, height: 0 }} />
              <span style={{
                position: 'absolute', cursor: 'inherit',
                top: 0, left: 0, right: 0, bottom: 0,
                background: item.val && (!item.needsPhone || phoneVerified) ? 'var(--primary)' : 'var(--bg-elevated)',
                border: '1px solid var(--border)', borderRadius: 24,
                transition: 'background 0.2s',
              }}>
                <span style={{
                  position: 'absolute', top: 2, left: item.val ? 20 : 2,
                  width: 18, height: 18, borderRadius: '50%',
                  background: '#fff', transition: 'left 0.2s',
                  boxShadow: '0 1px 3px rgba(0,0,0,0.3)',
                }} />
              </span>
            </label>
          </div>
        ))}

        <div style={{ marginTop: 20, display: 'flex', alignItems: 'center', gap: 12 }}>
          <button onClick={saveNotifPrefs} disabled={savingNotif} style={btnPrimary(savingNotif)}>
            {savingNotif ? 'Saving…' : 'Save Preferences'}
          </button>
          {savedNotif && <span style={{ color: '#4ade80', fontSize: 13 }}>✓ Saved</span>}
        </div>
      </div>

      {profile && (
        <div style={{ fontSize: 12, color: 'var(--text-muted)', padding: '0 4px', marginBottom: 32 }}>
          Member since {new Date(profile.created_at).toLocaleDateString('en-GB', { day: 'numeric', month: 'long', year: 'numeric' })}
        </div>
      )}
    </div>
  );
}
