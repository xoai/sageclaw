import { h } from 'preact';
import { useState } from 'preact/hooks';

const PLATFORMS = [
  { id: 'telegram', label: 'Telegram', icon: '\u2708', desc: 'Paste bot token from @BotFather',
    fields: [{ key: 'token', label: 'Bot Token', hint: 'Get from @BotFather on Telegram' }] },
  { id: 'discord', label: 'Discord', icon: '\uD83C\uDFAE', desc: 'Paste bot token from Developer Portal',
    fields: [{ key: 'token', label: 'Bot Token', hint: 'Get from Discord Developer Portal' }] },
  { id: 'zalo_bot', label: 'Zalo Bot', icon: '\uD83E\uDD16', desc: 'Paste bot token from Zalo',
    fields: [{ key: 'token', label: 'Bot Token', hint: 'Get from Zalo Bot Manager' }] },
  { id: 'zalo', label: 'Zalo OA', icon: '\uD83D\uDCAC', desc: 'OA credentials',
    fields: [
      { key: 'oa_id', label: 'OA ID' },
      { key: 'secret_key', label: 'Secret Key' },
      { key: 'access_token', label: 'Access Token' },
    ] },
  { id: 'whatsapp', label: 'WhatsApp', icon: '\uD83D\uDCF1', desc: 'Business API credentials',
    fields: [
      { key: 'phone_number_id', label: 'Phone Number ID' },
      { key: 'access_token', label: 'Access Token' },
      { key: 'verify_token', label: 'Verify Token' },
      { key: 'app_secret', label: 'App Secret' },
    ] },
];

export default function StepChannel({ progress, onComplete, onBack }) {
  const [selected, setSelected] = useState(null); // platform id or 'web'
  const [token, setToken] = useState('');
  const [creds, setCreds] = useState({});
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState('');
  const [connResult, setConnResult] = useState(null);

  const platform = PLATFORMS.find(p => p.id === selected);
  const isMultiField = platform && platform.fields.length > 1;

  const handleConnect = async () => {
    setLoading(true);
    setError('');

    try {
      // Step 1: Create connection.
      let body;
      if (isMultiField) {
        body = { platform: selected, credentials: creds };
      } else {
        body = { platform: selected, token };
      }

      const res = await fetch('/api/v2/connections', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(body),
      });
      const data = await res.json();
      if (!res.ok) {
        setError(data.error || 'Failed to connect');
        setLoading(false);
        return;
      }

      // Step 2: Auto-bind to agent from previous step.
      if (progress.agentId) {
        await fetch(`/api/v2/connections/${data.id}`, {
          method: 'PUT',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ agent_id: progress.agentId }),
        });
      }

      setConnResult(data);
      onComplete({
        connectionId: data.id,
        connectionLabel: data.label || data.id,
        connectionPlatform: selected,
        skippedChannel: false,
      });
    } catch (e) {
      setError('Connection failed: ' + e.message);
    }
    setLoading(false);
  };

  const handleSkip = () => {
    onComplete({
      connectionId: null,
      connectionLabel: 'Web Chat',
      connectionPlatform: 'web',
      skippedChannel: true,
    });
  };

  // Platform selection.
  if (!selected) {
    return (
      <div class="card" style="padding:24px">
        <h2 style="font-size:18px;font-weight:700;margin-bottom:4px">Connect a Channel</h2>
        <p style="color:var(--text-muted);font-size:13px;margin-bottom:20px">
          Where should <strong>{progress.agentName || 'your agent'}</strong> be available? Pick a messaging platform or start with web chat.
        </p>

        <div style="display:flex;flex-direction:column;gap:8px;margin-bottom:16px">
          {PLATFORMS.map(p => (
            <div key={p.id} class="card clickable" style="padding:12px;margin-bottom:0;display:flex;align-items:center;gap:12px"
              onClick={() => setSelected(p.id)}>
              <span style="font-size:20px">{p.icon}</span>
              <div>
                <div style="font-weight:600;font-size:13px">{p.label}</div>
                <div style="font-size:12px;color:var(--text-muted)">{p.desc}</div>
              </div>
            </div>
          ))}

          {/* Web only option */}
          <div class="card clickable" style="padding:12px;margin-bottom:0;display:flex;align-items:center;gap:12px;border-style:dashed"
            onClick={handleSkip}>
            <span style="font-size:20px">{'\uD83C\uDF10'}</span>
            <div>
              <div style="font-weight:600;font-size:13px">Web Only</div>
              <div style="font-size:12px;color:var(--text-muted)">Start with web chat only. Add other platforms later.</div>
            </div>
          </div>
        </div>

        <button class="btn-secondary" style="width:100%" onClick={onBack}>Back</button>
      </div>
    );
  }

  // Credential entry.
  return (
    <div class="card" style="padding:24px">
      <h2 style="font-size:18px;font-weight:700;margin-bottom:4px">Connect {platform.label}</h2>
      <p style="color:var(--text-muted);font-size:13px;margin-bottom:20px">
        Enter your bot credentials. We'll verify the connection with the platform.
      </p>

      {isMultiField ? (
        platform.fields.map(f => (
          <div class="form-group" key={f.key}>
            <label>{f.label}</label>
            <input type="password" placeholder={f.label}
              value={creds[f.key] || ''}
              onInput={e => setCreds({ ...creds, [f.key]: e.target.value })} />
            {f.hint && <small style="color:var(--text-muted);font-size:11px">{f.hint}</small>}
          </div>
        ))
      ) : (
        <div class="form-group">
          <label>{platform.fields[0]?.label || 'Bot Token'}</label>
          <input type="password" placeholder="Paste your bot token here"
            value={token}
            onInput={e => setToken(e.target.value)}
            onKeyDown={e => e.key === 'Enter' && handleConnect()} />
          {platform.fields[0]?.hint && (
            <small style="color:var(--text-muted);font-size:11px">{platform.fields[0].hint}</small>
          )}
        </div>
      )}

      {error && <div style="color:var(--error);font-size:13px;margin-bottom:12px">{error}</div>}

      <div style="display:flex;gap:8px;margin-top:16px">
        <button class="btn-primary" style="flex:1" onClick={handleConnect}
          disabled={loading || (isMultiField ? !Object.values(creds).some(v => v && v.trim()) : !token.trim())}>
          {loading ? 'Connecting...' : 'Connect'}
        </button>
        <button class="btn-secondary" onClick={() => { setSelected(null); setError(''); setToken(''); setCreds({}); }}>
          Back
        </button>
      </div>

      <div style="text-align:center;margin-top:12px">
        <button style="background:none;border:none;color:var(--text-muted);font-size:12px;cursor:pointer;text-decoration:underline"
          onClick={handleSkip}>
          Skip — use web chat only
        </button>
      </div>
    </div>
  );
}
