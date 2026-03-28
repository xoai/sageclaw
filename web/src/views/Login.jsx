import { useState } from 'preact/hooks';

export function Login({ onComplete }) {
  const [password, setPassword] = useState('');
  const [totpCode, setTotpCode] = useState('');
  const [nonce, setNonce] = useState('');
  const [step, setStep] = useState('password'); // 'password' | 'totp'
  const [error, setError] = useState('');
  const [loading, setLoading] = useState(false);

  const submitPassword = async (e) => {
    e.preventDefault();
    setLoading(true);
    setError('');
    try {
      const res = await fetch('/api/auth/login', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ password }),
        credentials: 'include',
      });
      const data = await res.json();
      if (data.error) { setError(data.error); return; }

      // Check if TOTP is required.
      if (data.status === 'totp_required') {
        setNonce(data.nonce);
        setStep('totp');
        return;
      }

      onComplete();
    } catch (err) {
      setError('Connection failed');
    } finally {
      setLoading(false);
    }
  };

  const submitTOTP = async (e) => {
    e.preventDefault();
    setLoading(true);
    setError('');
    try {
      const res = await fetch('/api/auth/login/totp', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ nonce, code: totpCode }),
        credentials: 'include',
      });
      const data = await res.json();
      if (data.error) { setError(data.error); return; }
      onComplete();
    } catch (err) {
      setError('Connection failed');
    } finally {
      setLoading(false);
    }
  };

  return (
    <div style="display:flex;justify-content:center;align-items:center;height:100vh;background:var(--bg)">
      <div style="width:360px;background:var(--surface);border:1px solid var(--border);border-radius:12px;padding:32px">
        <h1 style="font-family:var(--mono);color:var(--primary);margin-bottom:8px;font-size:24px">SageClaw</h1>

        {step === 'password' ? (
          <div>
            <p style="color:var(--text-muted);margin-bottom:24px;font-size:13px">Enter your password to continue.</p>
            <form onSubmit={submitPassword}>
              <input type="password" class="search-input" placeholder="Password"
                value={password} onInput={e => setPassword(e.target.value)} style="margin-bottom:16px"
                autofocus />
              {error && <div style="color:var(--error);font-size:13px;margin-bottom:12px">{error}</div>}
              <button class="btn-primary" style="width:100%" disabled={loading}>
                {loading ? 'Logging in...' : 'Login'}
              </button>
            </form>
          </div>
        ) : (
          <div>
            <p style="color:var(--text-muted);margin-bottom:24px;font-size:13px">
              Enter the 6-digit code from your authenticator app.
            </p>
            <form onSubmit={submitTOTP}>
              <input type="text" class="search-input" placeholder="000000"
                value={totpCode} onInput={e => setTotpCode(e.target.value)}
                style="margin-bottom:16px;text-align:center;font-family:var(--mono);font-size:20px;letter-spacing:8px"
                maxLength={6} pattern="[0-9]*" inputMode="numeric" autofocus />
              {error && <div style="color:var(--error);font-size:13px;margin-bottom:12px">{error}</div>}
              <button class="btn-primary" style="width:100%" disabled={loading || totpCode.length !== 6}>
                {loading ? 'Verifying...' : 'Verify'}
              </button>
              <button type="button" style="width:100%;margin-top:8px;background:none;border:none;color:var(--text-muted);cursor:pointer;font-size:13px"
                onClick={() => { setStep('password'); setError(''); setTotpCode(''); }}>
                Back to password
              </button>
            </form>
          </div>
        )}
      </div>
    </div>
  );
}
