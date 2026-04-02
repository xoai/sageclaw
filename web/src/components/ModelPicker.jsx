import { h } from 'preact';
import { useState, useRef, useEffect } from 'preact/hooks';

const providerLabels = {
  anthropic: 'Anthropic', openai: 'OpenAI', gemini: 'Google Gemini',
  openrouter: 'OpenRouter', github: 'GitHub Copilot', ollama: 'Ollama (Local)',
};

const tierDescriptions = {
  strong: 'Best quality — auto-selects from connected providers',
  fast: 'Lower latency — auto-selects cheapest fast model',
  local: 'Ollama only — free, private',
};

const formatCost = (m) => {
  if (!m.input_cost && !m.output_cost) return 'Free';
  return `$${m.input_cost}/$${m.output_cost} per 1M`;
};

const formatContext = (w) => {
  if (!w) return '';
  if (w >= 1000000) return `${(w / 1000000).toFixed(0)}M ctx`;
  if (w >= 1000) return `${(w / 1000).toFixed(0)}K ctx`;
  return `${w} ctx`;
};

export default function ModelPicker({ value, onChange, models, combos, connected, stale, onRefresh }) {
  const [query, setQuery] = useState('');
  const [open, setOpen] = useState(false);
  const [refreshing, setRefreshing] = useState(false);
  const ref = useRef(null);

  // Close dropdown on outside click.
  useEffect(() => {
    const handler = (e) => {
      if (ref.current && !ref.current.contains(e.target)) setOpen(false);
    };
    document.addEventListener('mousedown', handler);
    return () => document.removeEventListener('mousedown', handler);
  }, []);

  const q = query.toLowerCase();
  const filteredModels = q
    ? (models || []).filter(m =>
        (m.name || '').toLowerCase().includes(q) ||
        (m.model_id || '').toLowerCase().includes(q) ||
        (m.provider || '').toLowerCase().includes(q) ||
        (m.tier || '').toLowerCase().includes(q))
    : (models || []);

  // Group by provider.
  const grouped = {};
  filteredModels.forEach(m => {
    if (!grouped[m.provider]) grouped[m.provider] = [];
    grouped[m.provider].push(m);
  });

  // Display label for the current value.
  const displayLabel = () => {
    if (!value) return '';
    if (tierDescriptions[value]) return `${value} — ${tierDescriptions[value]}`;
    if (value.startsWith('combo:')) {
      const c = (combos || []).find(c => c.id === value.slice(6));
      return c ? `combo:${c.name}` : value;
    }
    const m = (models || []).find(m => m.model_id === value || m.id === value || (m.provider + '/' + m.model_id) === value);
    return m ? `${m.provider}/${m.model_id}` : value;
  };

  const select = (val) => {
    onChange(val);
    setQuery('');
    setOpen(false);
  };

  const handleRefresh = async () => {
    setRefreshing(true);
    try { await onRefresh?.(); } finally { setRefreshing(false); }
  };

  return (
    <div ref={ref} class="model-picker" style="position:relative">
      <div style="display:flex;gap:6px;align-items:center">
        <input
          type="text"
          value={open ? query : displayLabel()}
          placeholder="Search models..."
          onFocus={() => { setOpen(true); setQuery(''); }}
          onInput={e => { setQuery(e.target.value); setOpen(true); }}
          style="flex:1"
        />
        {stale && (
          <button type="button" class="btn-sm" onClick={handleRefresh}
            disabled={refreshing} title="Models may be outdated — click to refresh"
            style="font-size:12px;padding:4px 8px;white-space:nowrap">
            {refreshing ? '...' : '↻'}
          </button>
        )}
      </div>

      {open && (
        <div class="model-picker-dropdown">
          {/* Tiers */}
          <div class="model-picker-group">
            <div class="model-picker-group-label">Auto-route (recommended)</div>
            {['strong', 'fast', 'local'].map(tier => (
              <div key={tier} class={`model-picker-item ${value === tier ? 'selected' : ''}`}
                onClick={() => select(tier)}>
                <span class="model-picker-name">{tier}</span>
                <span class="model-picker-meta">{tierDescriptions[tier]}</span>
              </div>
            ))}
          </div>

          {/* Combos */}
          {(combos || []).length > 0 && (
            <div class="model-picker-group">
              <div class="model-picker-group-label">Combos (custom fallback chains)</div>
              {combos.map(c => (
                <div key={c.id} class={`model-picker-item ${value === 'combo:' + c.id ? 'selected' : ''}`}
                  onClick={() => select('combo:' + c.id)}>
                  <span class="model-picker-name">{c.name}</span>
                  <span class="model-picker-meta">{c.strategy} · {(Array.isArray(c.models) ? c.models : (typeof c.models === 'string' ? (() => { try { return JSON.parse(c.models); } catch { return []; } })() : [])).length} models</span>
                </div>
              ))}
            </div>
          )}

          {/* Per-provider models */}
          {Object.entries(grouped).map(([prov, provModels]) => (
            <div key={prov} class="model-picker-group">
              <div class="model-picker-group-label">
                {providerLabels[prov] || prov}
                {connected?.[prov] ? <span class="model-picker-dot connected" /> : <span class="model-picker-dot" />}
              </div>
              {provModels.map(m => (
                <div key={m.id}
                  class={`model-picker-item ${(value === m.model_id || value === m.provider + '/' + m.model_id) ? 'selected' : ''} ${!m.available ? 'unavailable' : ''}`}
                  onClick={() => select(m.provider + '/' + m.model_id)}>
                  <span class="model-picker-name">{m.provider}/{m.model_id}</span>
                  <span class="model-picker-meta">
                    {m.tier && <span class="model-picker-tier">{m.tier}</span>}
                    {formatContext(m.context_window)}
                    {m.input_cost > 0 && ` · ${formatCost(m)}`}
                    {m.input_cost === 0 && m.output_cost === 0 && m.provider === 'ollama' && ' · Free'}
                  </span>
                </div>
              ))}
            </div>
          ))}

          {filteredModels.length === 0 && query && (
            <div class="model-picker-item" style="color:var(--text-secondary);font-style:italic">
              No matches — value will be used as-is: "{query}"
              <button type="button" class="btn-sm" style="margin-left:8px;font-size:11px"
                onClick={() => select(query)}>Use custom ID</button>
            </div>
          )}
        </div>
      )}
    </div>
  );
}
