import { h } from 'preact';
import { useState } from 'preact/hooks';

const PROVIDERS = [
  { type: 'anthropic', name: 'Anthropic', desc: 'Claude models', placeholder: 'sk-ant-...', color: '#d4a574' },
  { type: 'openai', name: 'OpenAI', desc: 'GPT models', placeholder: 'sk-...', color: '#74aa9c' },
  { type: 'gemini', name: 'Google Gemini', desc: 'Gemini models', placeholder: 'AIza...', color: '#4285f4' },
  { type: 'openrouter', name: 'OpenRouter', desc: '200+ models', placeholder: 'sk-or-...', color: '#f59e0b' },
  { type: 'github', name: 'GitHub Copilot', desc: 'Copilot models', placeholder: 'ghp_...', color: '#6e40c9' },
  { type: 'ollama', name: 'Ollama', desc: 'Local, free', placeholder: '', color: '#888', noKey: true },
];

const DEFAULT_URLS = {
  anthropic: 'https://api.anthropic.com', openai: 'https://api.openai.com',
  gemini: 'https://generativelanguage.googleapis.com', openrouter: 'https://openrouter.ai/api/v1',
  github: 'https://api.githubcopilot.com', ollama: 'http://localhost:11434',
};

export default function StepProvider({ progress, onComplete }) {
  const [selected, setSelected] = useState('anthropic');
  const [apiKey, setApiKey] = useState('');
  const [showAdvanced, setShowAdvanced] = useState(false);
  const [baseUrl, setBaseUrl] = useState('');
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState('');
  const [success, setSuccess] = useState('');

  const provider = PROVIDERS.find(p => p.type === selected);

  const handleConnect = async () => {
    if (!provider.noKey && !apiKey.trim()) {
      setError('API key is required');
      return;
    }
    setLoading(true);
    setError('');
    setSuccess('');

    try {
      const body = {
        type: selected,
        name: provider.name,
        api_key: apiKey || undefined,
        base_url: baseUrl || DEFAULT_URLS[selected] || undefined,
      };
      const res = await fetch('/api/providers', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(body),
      });
      const data = await res.json();
      if (data.error) {
        setError(data.error);
        setLoading(false);
        return;
      }

      setSuccess(`${provider.name} connected!`);
      setTimeout(() => {
        onComplete({
          providerId: data.id || selected,
          providerName: provider.name,
          providerType: selected,
        });
      }, 600);
    } catch (e) {
      setError('Connection failed: ' + e.message);
    }
    setLoading(false);
  };

  return (
    <div class="card" style="padding:24px">
      <h2 style="font-size:16px;margin-bottom:4px">Connect an AI Provider</h2>
      <p style="color:var(--text-muted);font-size:13px;margin-bottom:20px">
        Give your agent a brain. Pick a provider and paste your API key.
      </p>

      {/* Provider grid */}
      <div style="display:grid;grid-template-columns:repeat(3,1fr);gap:8px;margin-bottom:20px">
        {PROVIDERS.map(p => (
          <div
            key={p.type}
            class={`card clickable ${selected === p.type ? 'card-selected' : ''}`}
            style="padding:12px;text-align:center;cursor:pointer;margin-bottom:0"
            onClick={() => { setSelected(p.type); setError(''); setSuccess(''); setApiKey(''); }}
          >
            <span style={`display:inline-block;width:10px;height:10px;border-radius:50%;background:${p.color};margin-bottom:4px`} />
            <div style="font-size:13px;font-weight:600">{p.name}</div>
            <div style="font-size:11px;color:var(--text-muted)">{p.desc}</div>
          </div>
        ))}
      </div>

      {/* API Key */}
      {!provider.noKey ? (
        <div class="form-group">
          <label>API Key</label>
          <input
            type="password"
            placeholder={provider.placeholder}
            value={apiKey}
            onInput={e => setApiKey(e.target.value)}
            onKeyDown={e => e.key === 'Enter' && handleConnect()}
          />
          <small style="color:var(--text-muted);font-size:11px">Stored encrypted. Never displayed after saving.</small>
        </div>
      ) : (
        <div class="card" style="padding:12px;margin-bottom:12px;border-color:var(--success);background:rgba(63,185,80,0.05)">
          <span style="font-size:13px;color:var(--success)">Ollama runs locally — no API key needed. Make sure it's running on your machine.</span>
        </div>
      )}

      {/* Advanced */}
      <div style="margin-bottom:16px">
        <button
          style="background:none;border:none;color:var(--text-muted);font-size:12px;cursor:pointer;padding:0"
          onClick={() => setShowAdvanced(!showAdvanced)}
        >
          {showAdvanced ? '\u25BC' : '\u25B6'} Advanced settings
        </button>
        {showAdvanced && (
          <div class="form-group" style="margin-top:8px">
            <label>Base URL</label>
            <input
              type="text"
              placeholder={DEFAULT_URLS[selected] || 'https://...'}
              value={baseUrl}
              onInput={e => setBaseUrl(e.target.value)}
            />
            <small style="color:var(--text-muted);font-size:11px">Only change if using a proxy or self-hosted instance.</small>
          </div>
        )}
      </div>

      {/* Messages */}
      {error && <div style="color:var(--error);font-size:13px;margin-bottom:12px">{error}</div>}
      {success && <div style="color:var(--success);font-size:13px;margin-bottom:12px">{success}</div>}

      {/* Action */}
      <button class="btn-primary" style="width:100%" onClick={handleConnect} disabled={loading || (!provider.noKey && !apiKey.trim())}>
        {loading ? 'Connecting...' : `Connect ${provider.name}`}
      </button>
    </div>
  );
}
