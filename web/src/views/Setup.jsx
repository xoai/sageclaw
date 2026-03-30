import { useState } from 'preact/hooks';

export function Setup({ onComplete }) {
  const [password, setPassword] = useState('');
  const [confirm, setConfirm] = useState('');
  const [error, setError] = useState('');
  const [loading, setLoading] = useState(false);

  const submit = async (e) => {
    e.preventDefault();
    if (password !== confirm) { setError('Passwords do not match'); return; }
    if (password.length < 8) { setError('Minimum 8 characters'); return; }

    setLoading(true);
    setError('');
    try {
      const res = await fetch('/api/auth/setup', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ password, confirm }),
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
        <p style="color:var(--text-muted);margin-bottom:24px;font-size:13px">Create a password to secure your dashboard.</p>

        <form onSubmit={submit}>
          <input type="password" class="search-input" placeholder="Password (min 8 chars)"
            value={password} onInput={e => setPassword(e.target.value)} style="margin-bottom:12px" />
          <input type="password" class="search-input" placeholder="Confirm password"
            value={confirm} onInput={e => setConfirm(e.target.value)} style="margin-bottom:16px" />

          {error && <div style="color:var(--error);font-size:13px;margin-bottom:12px">{error}</div>}

          <button class="btn-primary" style="width:100%" disabled={loading}>
            {loading ? 'Setting up...' : 'Create Password'}
          </button>
        </form>
      </div>
    </div>
  );
}
