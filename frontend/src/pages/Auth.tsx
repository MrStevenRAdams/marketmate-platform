import React, { useState, useEffect } from 'react';
import { useNavigate, Link } from 'react-router-dom';
import { useAuth, auth, createUserWithEmailAndPassword } from '../contexts/AuthContext';
import { sendEmailVerification } from 'firebase/auth';
import { clearActiveTenantId } from '../contexts/TenantContext';
import './Auth.css';

const API_URL = import.meta.env.VITE_API_URL || 'http://localhost:8080/api/v1';

// ============================================================================
// LOGIN PAGE
// ============================================================================

export const LoginPage: React.FC = () => {
  const { login, loginWithGoogle, loginWithMicrosoft, isAuthenticated, isLoading } = useAuth();
  const navigate = useNavigate();
  const [email, setEmail] = useState('');
  const [password, setPassword] = useState('');
  const [error, setError] = useState('');
  const [loading, setLoading] = useState(false);
  const [showReset, setShowReset] = useState(false);
  const [resetSent, setResetSent] = useState(false);
  const { resetPassword } = useAuth();

  // Already logged in — send to dashboard
  useEffect(() => {
    if (!isLoading && isAuthenticated) {
      navigate('/dashboard', { replace: true });
    }
  }, [isAuthenticated, isLoading, navigate]);

  const handleLogin = async (e: React.FormEvent) => {
    e.preventDefault();
    setError('');
    setLoading(true);
    try {
      await login(email.trim().toLowerCase(), password);
      navigate('/');
    } catch (err: any) {
      const msg = firebaseErrorMessage(err.code);
      setError(msg);
    } finally {
      setLoading(false);
    }
  };

  const handleReset = async (e: React.FormEvent) => {
    e.preventDefault();
    setError('');
    setLoading(true);
    try {
      await resetPassword(email.trim().toLowerCase());
      setResetSent(true);
    } catch (err: any) {
      setError(firebaseErrorMessage(err.code));
    } finally {
      setLoading(false);
    }
  };

  return (
    <div className="auth-page">
      <div className="auth-card">
        <div className="auth-logo">
          <span className="auth-logo-icon">📦</span>
          <span className="auth-logo-text">MarketMate</span>
        </div>

        {!showReset ? (
          <>
            <h1 className="auth-title">Sign in</h1>
            <p className="auth-subtitle">Welcome back — enter your details below</p>

            <form onSubmit={handleLogin} className="auth-form">
              <div className="auth-field">
                <label>Email address</label>
                <input
                  type="email"
                  value={email}
                  onChange={e => setEmail(e.target.value)}
                  placeholder="you@company.com"
                  required
                  autoFocus
                />
              </div>
              <div className="auth-field">
                <label>Password</label>
                <input
                  type="password"
                  value={password}
                  onChange={e => setPassword(e.target.value)}
                  placeholder="••••••••"
                  required
                />
              </div>

              {error && <div className="auth-error">{error}</div>}

              <button type="submit" className="auth-btn-primary" disabled={loading}>
                {loading ? 'Signing in...' : 'Sign in'}
              </button>
            </form>

            <div style={{ display: 'flex', alignItems: 'center', gap: 8, margin: '16px 0' }}>
              <hr style={{ flex: 1, border: 'none', borderTop: '1px solid var(--border,#334155)' }} />
              <span style={{ color: 'var(--text-muted,#64748b)', fontSize: 12 }}>or continue with</span>
              <hr style={{ flex: 1, border: 'none', borderTop: '1px solid var(--border,#334155)' }} />
            </div>
            <div style={{ display: 'flex', gap: 8 }}>
              <button
                type="button"
                className="auth-btn-secondary"
                style={{ flex: 1, display: 'flex', alignItems: 'center', justifyContent: 'center', gap: 8 }}
                onClick={async () => { setError(''); setLoading(true); try { await loginWithGoogle(); navigate('/'); } catch (e: any) { setError(firebaseErrorMessage(e.code)); } finally { setLoading(false); } }}
                disabled={loading}
              >
                <svg width="18" height="18" viewBox="0 0 24 24"><path fill="#4285F4" d="M22.56 12.25c0-.78-.07-1.53-.2-2.25H12v4.26h5.92c-.26 1.37-1.04 2.53-2.21 3.31v2.77h3.57c2.08-1.92 3.28-4.74 3.28-8.09z"/><path fill="#34A853" d="M12 23c2.97 0 5.46-.98 7.28-2.66l-3.57-2.77c-.98.66-2.23 1.06-3.71 1.06-2.86 0-5.29-1.93-6.16-4.53H2.18v2.84C3.99 20.53 7.7 23 12 23z"/><path fill="#FBBC05" d="M5.84 14.09c-.22-.66-.35-1.36-.35-2.09s.13-1.43.35-2.09V7.07H2.18C1.43 8.55 1 10.22 1 12s.43 3.45 1.18 4.93l3.66-2.84z"/><path fill="#EA4335" d="M12 5.38c1.62 0 3.06.56 4.21 1.64l3.15-3.15C17.45 2.09 14.97 1 12 1 7.7 1 3.99 3.47 2.18 7.07l3.66 2.84c.87-2.6 3.3-4.53 6.16-4.53z"/></svg>
                Google
              </button>
              <button
                type="button"
                className="auth-btn-secondary"
                style={{ flex: 1, display: 'flex', alignItems: 'center', justifyContent: 'center', gap: 8 }}
                onClick={async () => { setError(''); setLoading(true); try { await loginWithMicrosoft(); navigate('/'); } catch (e: any) { setError(firebaseErrorMessage(e.code)); } finally { setLoading(false); } }}
                disabled={loading}
              >
                <svg width="18" height="18" viewBox="0 0 24 24"><path fill="#f25022" d="M1 1h10v10H1z"/><path fill="#7fba00" d="M13 1h10v10H13z"/><path fill="#00a4ef" d="M1 13h10v10H1z"/><path fill="#ffb900" d="M13 13h10v10H13z"/></svg>
                Microsoft
              </button>
            </div>

            <div className="auth-links">
              <button className="auth-link" onClick={() => setShowReset(true)}>
                Forgot your password?
              </button>
              <span className="auth-divider">·</span>
              <Link to="/register" className="auth-link">Create an account</Link>
            </div>
          </>
        ) : (
          <>
            <h1 className="auth-title">Reset password</h1>
            <p className="auth-subtitle">We'll send a reset link to your email</p>

            {resetSent ? (
              <div className="auth-success">
                ✅ Reset email sent — check your inbox and spam folder.
                <button className="auth-link" style={{ marginTop: 16 }} onClick={() => { setShowReset(false); setResetSent(false); }}>
                  Back to sign in
                </button>
              </div>
            ) : (
              <form onSubmit={handleReset} className="auth-form">
                <div className="auth-field">
                  <label>Email address</label>
                  <input
                    type="email"
                    value={email}
                    onChange={e => setEmail(e.target.value)}
                    placeholder="you@company.com"
                    required
                    autoFocus
                  />
                </div>

                {error && <div className="auth-error">{error}</div>}

                <button type="submit" className="auth-btn-primary" disabled={loading}>
                  {loading ? 'Sending...' : 'Send reset link'}
                </button>
              </form>
            )}

            {!resetSent && (
              <div className="auth-links">
                <button className="auth-link" onClick={() => setShowReset(false)}>
                  ← Back to sign in
                </button>
              </div>
            )}
          </>
        )}
      </div>
    </div>
  );
};

// ============================================================================
// REGISTER PAGE — Two steps: account details → plan selection
// ============================================================================

interface Plan {
  plan_id: string;
  name: string;
  price_gbp: number;
  credits_per_month: number | null;
  billing_model: string;
  per_order_gbp: number | null;
}

export const RegisterPage: React.FC = () => {
  const navigate = useNavigate();
  const { isAuthenticated, isLoading } = useAuth();
  const [step, setStep] = useState<'account' | 'plan' | 'verify_email' | 'verify_phone'>('account');
  const [plans, setPlans] = useState<Plan[]>([]);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState('');

  // Step 1 fields
  const [displayName, setDisplayName] = useState('');
  const [email, setEmail] = useState('');
  const [password, setPassword] = useState('');
  const [confirmPassword, setConfirmPassword] = useState('');
  const [companyName, setCompanyName] = useState('');

  // Step 2 — Firebase user created in step 1 for step 2 to use
  const [firebaseUID, setFirebaseUID] = useState('');
  const [selectedPlan, setSelectedPlan] = useState('starter_s');

  // Step 3 — email verification
  const [resendingEmail, setResendingEmail] = useState(false);
  const [emailResent, setEmailResent] = useState(false);
  const [checkingVerified, setCheckingVerified] = useState(false);

  // Step 4 — phone verification
  const [phone, setPhone] = useState('');
  const [otpSent, setOtpSent] = useState(false);
  const [otp, setOtp] = useState('');
  const [sendingOtp, setSendingOtp] = useState(false);
  const [verifyingOtp, setVerifyingOtp] = useState(false);
  const [phoneError, setPhoneError] = useState('');

  // Detect referral source from query params (e.g. ?ref=temu)
  const referralSource = new URLSearchParams(window.location.search).get('ref') || '';

  // If already fully logged in with a tenant, redirect to dashboard
  useEffect(() => {
    if (!isLoading && isAuthenticated && step === 'account') {
      navigate('/dashboard', { replace: true });
    }
  }, [isAuthenticated, isLoading, navigate, step]);

  // ── Step 1: Create account ─────────────────────────────────────────────────
  const handleAccountSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    setError('');

    if (password !== confirmPassword) {
      setError('Passwords do not match');
      return;
    }
    if (password.length < 8) {
      setError('Password must be at least 8 characters');
      return;
    }

    setLoading(true);
    try {
      // Create Firebase user
      const credential = await createUserWithEmailAndPassword(auth, email.trim().toLowerCase(), password);
      setFirebaseUID(credential.user.uid);

      // Load plans for step 2
      const resp = await fetch(`${API_URL}/billing/plans`);
      if (resp.ok) {
        const data = await resp.json();
        // Only show starter plans at registration
        setPlans((data.plans ?? []).filter((p: Plan) => p.plan_id.startsWith('starter')));
      }

      setStep('plan');
    } catch (err: any) {
      setError(firebaseErrorMessage(err.code) ?? err.message);
    } finally {
      setLoading(false);
    }
  };

  // ── Step 2: Choose plan + complete registration ────────────────────────────
  const handlePlanSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    setError('');
    setLoading(true);

    try {
      const resp = await fetch(`${API_URL}/auth/register`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          firebase_uid: firebaseUID,
          email: email.trim().toLowerCase(),
          display_name: displayName.trim(),
          company_name: companyName.trim(),
          plan_id: selectedPlan,
          referral_source: referralSource,
        }),
      });

      const data = await resp.json();

      if (!resp.ok) {
        setError(data.error ?? 'Registration failed');
        return;
      }

      // SECURITY FIX: clear any tenant that may be stored in localStorage from
      // a previous session on this browser. AuthContext's loadUserData (triggered
      // by onAuthStateChanged) will write the correct new tenant ID once it
      // resolves the /auth/me response. Without this, the new user would briefly
      // see the previous session's marketplace connections.
      clearActiveTenantId();

      // Store the new tenant ID in localStorage so API calls work immediately
      const tenantId = data.tenant_id;
      if (tenantId) {
        localStorage.setItem('marketmate_active_tenant', tenantId);
      }
      // Send Firebase email verification
      if (auth.currentUser) {
        try { await sendEmailVerification(auth.currentUser); } catch {}
      }
      setStep('verify_email');
    } catch (err: any) {
      setError(err.message ?? 'Registration failed');
    } finally {
      setLoading(false);
    }
  };

  // ── Step 3: Email verification handlers ───────────────────────────────────
  const handleResendEmail = async () => {
    setResendingEmail(true);
    try {
      if (auth.currentUser) {
        await sendEmailVerification(auth.currentUser);
        setEmailResent(true);
        setTimeout(() => setEmailResent(false), 5000);
      }
    } catch { } finally { setResendingEmail(false); }
  };

  const handleCheckEmailVerified = async () => {
    setCheckingVerified(true);
    try {
      if (auth.currentUser) {
        await auth.currentUser.reload();
        if (auth.currentUser.emailVerified) {
          setStep('verify_phone');
        } else {
          // Show message that email not yet verified
          setEmailResent(false);
          alert('Email not yet verified. Please check your inbox and click the link.');
        }
      }
    } catch { } finally { setCheckingVerified(false); }
  };

  // ── Step 4: Phone verification handlers ──────────────────────────────────
  const handleSendOtp = async () => {
    setPhoneError('');
    if (!phone.trim()) { setPhoneError('Please enter your mobile number'); return; }
    setSendingOtp(true);
    try {
      const fbToken = auth.currentUser ? await auth.currentUser.getIdToken() : '';
      const res = await fetch(`${API_URL}/user/phone/send-otp`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json', 'Authorization': `Bearer ${fbToken}` },
        body: JSON.stringify({ phone: phone.trim(), channel: 'sms' }),
      });
      if (!res.ok) {
        const d = await res.json();
        setPhoneError(d.error ?? 'Failed to send code');
        return;
      }
      setOtpSent(true);
    } catch {
      setPhoneError('Failed to send verification code');
    } finally { setSendingOtp(false); }
  };

  const handleVerifyOtp = async () => {
    setPhoneError('');
    if (!otp.trim()) { setPhoneError('Please enter the verification code'); return; }
    setVerifyingOtp(true);
    try {
      const fbToken = auth.currentUser ? await auth.currentUser.getIdToken() : '';
      const res = await fetch(`${API_URL}/user/phone/verify-otp`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json', 'Authorization': `Bearer ${fbToken}` },
        body: JSON.stringify({ phone: phone.trim(), code: otp.trim() }),
      });
      if (!res.ok) {
        const d = await res.json();
        setPhoneError(d.error ?? 'Invalid code');
        return;
      }
      // Verification successful — redirect
      if (referralSource === 'temu') {
        navigate('/temu-wizard', { replace: true });
      } else {
        navigate('/', { replace: true });
      }
    } catch {
      setPhoneError('Verification failed');
    } finally { setVerifyingOtp(false); }
  };

  const handleSkipPhone = () => {
    if (referralSource === 'temu') {
      navigate('/temu-wizard', { replace: true });
    } else {
      navigate('/', { replace: true });
    }
  };

  return (
    <div className="auth-page">
      <div className="auth-card auth-card--wide">
        <div className="auth-logo">
          <span className="auth-logo-icon">📦</span>
          <span className="auth-logo-text">MarketMate</span>
        </div>

        {/* Step indicator */}
        <div className="auth-steps">
          <div className={`auth-step ${step === 'account' ? 'active' : 'done'}`}>
            <span className="auth-step-num">{step !== 'account' ? '✓' : '1'}</span>
            <span>Account</span>
          </div>
          <div className="auth-step-line" />
          <div className={`auth-step ${step === 'plan' ? 'active' : ['verify_email','verify_phone'].includes(step) ? 'done' : ''}`}>
            <span className="auth-step-num">{['verify_email','verify_phone'].includes(step) ? '✓' : '2'}</span>
            <span>Plan</span>
          </div>
          <div className="auth-step-line" />
          <div className={`auth-step ${step === 'verify_email' ? 'active' : step === 'verify_phone' ? 'done' : ''}`}>
            <span className="auth-step-num">{step === 'verify_phone' ? '✓' : '3'}</span>
            <span>Verify email</span>
          </div>
          <div className="auth-step-line" />
          <div className={`auth-step ${step === 'verify_phone' ? 'active' : ''}`}>
            <span className="auth-step-num">4</span>
            <span>Verify mobile</span>
          </div>
        </div>

        {step === 'account' ? (
          <>
            <h1 className="auth-title">Create your account</h1>
            <p className="auth-subtitle">Start your 14-day free trial — no card required</p>

            <form onSubmit={handleAccountSubmit} className="auth-form">
              <div className="auth-row">
                <div className="auth-field">
                  <label>Your name</label>
                  <input
                    type="text"
                    value={displayName}
                    onChange={e => setDisplayName(e.target.value)}
                    placeholder="Jane Smith"
                    required
                    autoFocus
                  />
                </div>
                <div className="auth-field">
                  <label>Company name</label>
                  <input
                    type="text"
                    value={companyName}
                    onChange={e => setCompanyName(e.target.value)}
                    placeholder="Acme Commerce Ltd"
                    required
                  />
                </div>
              </div>

              <div className="auth-field">
                <label>Work email</label>
                <input
                  type="email"
                  value={email}
                  onChange={e => setEmail(e.target.value)}
                  placeholder="jane@acme.com"
                  required
                />
              </div>

              <div className="auth-row">
                <div className="auth-field">
                  <label>Password</label>
                  <input
                    type="password"
                    value={password}
                    onChange={e => setPassword(e.target.value)}
                    placeholder="Min. 8 characters"
                    required
                    minLength={8}
                  />
                </div>
                <div className="auth-field">
                  <label>Confirm password</label>
                  <input
                    type="password"
                    value={confirmPassword}
                    onChange={e => setConfirmPassword(e.target.value)}
                    placeholder="Repeat password"
                    required
                  />
                </div>
              </div>

              {error && <div className="auth-error">{error}</div>}

              <button type="submit" className="auth-btn-primary" disabled={loading}>
                {loading ? 'Creating account...' : 'Continue →'}
              </button>
            </form>

            <div className="auth-links">
              Already have an account?{' '}
              <Link to="/login" className="auth-link">Sign in</Link>
            </div>
          </>
        ) : step === 'plan' ? (
          <>
            <h1 className="auth-title">Choose your plan</h1>
            <p className="auth-subtitle">
              All plans include a 14-day free trial. Need Premium or Enterprise?{' '}
              <a href="mailto:sales@marketmate.com" className="auth-link">Contact sales</a>
            </p>

            <form onSubmit={handlePlanSubmit} className="auth-form">
              <div className="plan-grid">
                {(plans.length === 0 ? [
                    { plan_id: 'starter_s', name: 'Starter S', price_gbp: 29, credits_per_month: 10000 },
                    { plan_id: 'starter_m', name: 'Starter M', price_gbp: 79, credits_per_month: 50000 },
                    { plan_id: 'starter_l', name: 'Starter L', price_gbp: 149, credits_per_month: 150000 },
                  ] : plans).map((plan) => (
                  <label
                    key={plan.plan_id}
                    className={`plan-card ${selectedPlan === plan.plan_id ? 'selected' : ''}`}
                  >
                    <input
                      type="radio"
                      name="plan"
                      value={plan.plan_id}
                      checked={selectedPlan === plan.plan_id}
                      onChange={() => setSelectedPlan(plan.plan_id)}
                    />
                    <div className="plan-name">{plan.name}</div>
                    <div className="plan-price">
                      <span className="plan-price-amount">£{plan.price_gbp}</span>
                      <span className="plan-price-period">/month</span>
                    </div>
                    <div className="plan-credits">
                      {plan.credits_per_month?.toLocaleString()} credits/month
                    </div>
                    <div className="plan-desc">{planDescription(plan.plan_id)}</div>
                  </label>
                ))}
              </div>

              <div className="plan-note">
                💳 No card required now. You'll be asked for payment details when your trial ends.
              </div>

              {error && <div className="auth-error">{error}</div>}

              <button type="submit" className="auth-btn-primary" disabled={loading}>
                {loading ? 'Setting up your account...' : 'Continue →'}
              </button>
            </form>
          </>
        ) : step === 'verify_email' ? (
          <>
            <h1 className="auth-title">Check your email</h1>
            <p className="auth-subtitle">We've sent a verification link to <strong>{email}</strong></p>

            <div className="auth-form">
              <div style={{
                textAlign: 'center', padding: '32px 16px',
                background: 'var(--bg-secondary)', borderRadius: 12,
                border: '1px solid var(--border)', marginBottom: 20,
              }}>
                <div style={{ fontSize: 48, marginBottom: 12 }}>📧</div>
                <p style={{ color: 'var(--text-secondary)', fontSize: 14, lineHeight: 1.6, margin: '0 0 20px' }}>
                  Click the link in your email to verify your address.<br />
                  Check your spam folder if you don't see it.
                </p>
                <button
                  type="button"
                  className="auth-btn-primary"
                  onClick={handleCheckEmailVerified}
                  disabled={checkingVerified}
                  style={{ marginBottom: 12, width: '100%' }}
                >
                  {checkingVerified ? 'Checking…' : "I've verified my email →"}
                </button>
                {emailResent && (
                  <div style={{ color: '#22c55e', fontSize: 13, marginBottom: 8 }}>
                    ✓ Verification email resent
                  </div>
                )}
                <button
                  type="button"
                  onClick={handleResendEmail}
                  disabled={resendingEmail}
                  style={{
                    background: 'none', border: 'none', color: 'var(--text-muted)',
                    fontSize: 13, cursor: 'pointer', textDecoration: 'underline',
                  }}
                >
                  {resendingEmail ? 'Sending…' : 'Resend verification email'}
                </button>
              </div>

              <button
                type="button"
                onClick={() => setStep('verify_phone')}
                style={{
                  background: 'none', border: 'none', color: 'var(--text-muted)',
                  fontSize: 12, cursor: 'pointer', textDecoration: 'underline',
                  display: 'block', width: '100%', textAlign: 'center',
                }}
              >
                Skip for now
              </button>
            </div>
          </>
        ) : step === 'verify_phone' ? (
          <>
            <h1 className="auth-title">Verify your mobile</h1>
            <p className="auth-subtitle">We'll send you SMS alerts for new orders and buyer messages</p>

            <div className="auth-form">
              <div className="auth-field">
                <label className="auth-label">Mobile number *</label>
                <div style={{ display: 'flex', gap: 8 }}>
                  <input
                    className="auth-input"
                    type="tel"
                    placeholder="+44 7700 000000"
                    value={phone}
                    onChange={e => setPhone(e.target.value)}
                    disabled={otpSent}
                    style={{ flex: 1 }}
                  />
                  <button
                    type="button"
                    className="auth-btn-secondary"
                    onClick={handleSendOtp}
                    disabled={sendingOtp || !phone.trim()}
                    style={{ whiteSpace: 'nowrap' }}
                  >
                    {sendingOtp ? 'Sending…' : otpSent ? 'Resend' : 'Send Code'}
                  </button>
                </div>
              </div>

              {otpSent && (
                <div className="auth-field">
                  <label className="auth-label">Verification code</label>
                  <p className="auth-subtitle" style={{ marginBottom: 8 }}>
                    Enter the 6-digit code sent to {phone}
                  </p>
                  <div style={{ display: 'flex', gap: 8 }}>
                    <input
                      className="auth-input"
                      type="text"
                      inputMode="numeric"
                      placeholder="000000"
                      maxLength={6}
                      value={otp}
                      onChange={e => setOtp(e.target.value.replace(/\D/g, ''))}
                      style={{ flex: 1, letterSpacing: '0.3em', fontSize: 20, textAlign: 'center' }}
                      autoFocus
                    />
                    <button
                      type="button"
                      className="auth-btn-primary"
                      onClick={handleVerifyOtp}
                      disabled={verifyingOtp || otp.length < 4}
                    >
                      {verifyingOtp ? 'Verifying…' : 'Verify →'}
                    </button>
                  </div>
                </div>
              )}

              {phoneError && <div className="auth-error">{phoneError}</div>}

              <button
                type="button"
                onClick={handleSkipPhone}
                style={{
                  background: 'none', border: 'none', color: 'var(--text-muted)',
                  fontSize: 13, cursor: 'pointer', marginTop: 16, textDecoration: 'underline',
                  display: 'block', width: '100%', textAlign: 'center',
                }}
              >
                Skip for now — I'll add this later in Settings
              </button>
            </div>
          </>
        ) : null}
      </div>
    </div>
  );
};

// ============================================================================
// INVITE ACCEPTANCE PAGE
// ============================================================================

export const AcceptInvitePage: React.FC = () => {
  const navigate = useNavigate();
  const token = window.location.pathname.split('/invite/')[1];
  const [inviteInfo, setInviteInfo] = useState<any>(null);
  const [inviteError, setInviteError] = useState('');
  const [loading, setLoading] = useState(true);

  // Form state
  const [displayName, setDisplayName] = useState('');
  const [password, setPassword] = useState('');
  const [isNewUser, setIsNewUser] = useState(true);
  const [submitting, setSubmitting] = useState(false);
  const [formError, setFormError] = useState('');

  React.useEffect(() => {
    fetch(`${API_URL}/auth/invite/${token}`)
      .then(r => r.json())
      .then(data => {
        if (data.error) setInviteError(data.error);
        else setInviteInfo(data);
      })
      .catch(() => setInviteError('Failed to load invitation'))
      .finally(() => setLoading(false));
  }, [token]);

  const handleAccept = async (e: React.FormEvent) => {
    e.preventDefault();
    setFormError('');
    setSubmitting(true);

    try {
      let firebaseUID = '';

      if (isNewUser) {
        // Create Firebase user
        const credential = await createUserWithEmailAndPassword(
          auth,
          inviteInfo.invited_email,
          password
        );
        firebaseUID = credential.user.uid;
      } else {
        // Existing user — get current user UID
        const currentUser = auth.currentUser;
        if (!currentUser) {
          setFormError('Please sign in first, then click the invite link again');
          return;
        }
        firebaseUID = currentUser.uid;
      }

      const resp = await fetch(`${API_URL}/auth/invite/accept`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          token,
          firebase_uid: firebaseUID,
          display_name: displayName || inviteInfo.invited_email,
        }),
      });

      const data = await resp.json();
      if (!resp.ok) {
        setFormError(data.error ?? 'Failed to accept invitation');
        return;
      }

      // SECURITY FIX: clear stale tenant so the invited user's correct tenant
      // is loaded fresh from /auth/me rather than inheriting any previous session.
      clearActiveTenantId();

      navigate('/', { replace: true });
    } catch (err: any) {
      setFormError(firebaseErrorMessage(err.code) ?? err.message);
    } finally {
      setSubmitting(false);
    }
  };

  if (loading) return <div className="auth-page"><div className="auth-card">Loading invitation...</div></div>;

  if (inviteError) return (
    <div className="auth-page">
      <div className="auth-card">
        <div className="auth-logo">
          <span className="auth-logo-icon">📦</span>
          <span className="auth-logo-text">MarketMate</span>
        </div>
        <div className="auth-error" style={{ marginTop: 24 }}>
          {inviteError === 'invitation already used' ? '✅ This invitation has already been accepted.' :
           inviteError === 'invitation has expired' ? '⏰ This invitation has expired. Ask your admin to resend it.' :
           '❌ ' + inviteError}
        </div>
        <div className="auth-links" style={{ marginTop: 16 }}>
          <Link to="/login" className="auth-link">Go to sign in</Link>
        </div>
      </div>
    </div>
  );

  return (
    <div className="auth-page">
      <div className="auth-card">
        <div className="auth-logo">
          <span className="auth-logo-icon">📦</span>
          <span className="auth-logo-text">MarketMate</span>
        </div>

        <h1 className="auth-title">You're invited!</h1>
        <p className="auth-subtitle">
          Join <strong>{inviteInfo?.tenant_name}</strong> as <strong>{inviteInfo?.role}</strong>
        </p>

        <div className="invite-email-badge">{inviteInfo?.invited_email}</div>

        <div className="invite-toggle">
          <button
            className={`invite-toggle-btn ${isNewUser ? 'active' : ''}`}
            onClick={() => setIsNewUser(true)}
          >
            New to MarketMate
          </button>
          <button
            className={`invite-toggle-btn ${!isNewUser ? 'active' : ''}`}
            onClick={() => setIsNewUser(false)}
          >
            Already have an account
          </button>
        </div>

        <form onSubmit={handleAccept} className="auth-form">
          {isNewUser && (
            <>
              <div className="auth-field">
                <label>Your name</label>
                <input
                  type="text"
                  value={displayName}
                  onChange={e => setDisplayName(e.target.value)}
                  placeholder="Jane Smith"
                  autoFocus
                />
              </div>
              <div className="auth-field">
                <label>Choose a password</label>
                <input
                  type="password"
                  value={password}
                  onChange={e => setPassword(e.target.value)}
                  placeholder="Min. 8 characters"
                  required={isNewUser}
                  minLength={8}
                />
              </div>
            </>
          )}

          {!isNewUser && (
            <p className="auth-subtitle" style={{ marginBottom: 16 }}>
              Make sure you're signed in to your existing account first, then click Accept below.
            </p>
          )}

          {formError && <div className="auth-error">{formError}</div>}

          <button type="submit" className="auth-btn-primary" disabled={submitting}>
            {submitting ? 'Accepting...' : 'Accept invitation →'}
          </button>
        </form>
      </div>
    </div>
  );
};

// ============================================================================
// HELPERS
// ============================================================================

function firebaseErrorMessage(code: string): string {
  const messages: Record<string, string> = {
    'auth/user-not-found':        'No account found with that email',
    'auth/wrong-password':        'Incorrect password',
    'auth/email-already-in-use':  'An account already exists with that email',
    'auth/weak-password':         'Password must be at least 6 characters',
    'auth/invalid-email':         'Please enter a valid email address',
    'auth/too-many-requests':     'Too many attempts — please try again in a few minutes',
    'auth/network-request-failed': 'Network error — please check your connection',
    'auth/invalid-credential':    'Incorrect email or password',
  };
  return messages[code] ?? 'An error occurred — please try again';
}

function planDescription(planId: string): string {
  const descs: Record<string, string> = {
    starter_s: 'Solo sellers & small catalogues. AI listing generation included.',
    starter_m: 'Growing businesses with active marketplace presence.',
    starter_l: 'High-volume cataloguing and frequent AI generation.',
  };
  return descs[planId] ?? '';
}
