import { h } from 'preact';
import { useState, useEffect } from 'preact/hooks';
import { Label } from '../components/InfoTip';

export function Providers() {
  const [providers, setProviders] = useState([]);
  const [combos, setCombos] = useState([]);
  const [tab, setTab] = useState('providers');
  const [showAddProvider, setShowAddProvider] = useState(false);
  const [showAddCombo, setShowAddCombo] = useState(false);
  const [expandedCombo, setExpandedCombo] = useState(null);
  const [providerForm, setProviderForm] = useState({ type: 'anthropic', name: '', api_key: '', base_url: '' });
  const [comboForm, setComboForm] = useState({ name: '', description: '', strategy: 'priority', models: [] });
  const [editingComboId, setEditingComboId] = useState(null);
  const [allModels, setAllModels] = useState([]);
  const [comboSearch, setComboSearch] = useState('');
  const [dragIdx, setDragIdx] = useState(null);
  const [msg, setMsg] = useState('');
  const [toast, setToast] = useState(null); // { text, type: 'success'|'error'|'warning' }
  const [testing, setTesting] = useState(null);

  const loadProviders = () => fetch('/api/providers').then(r => r.json()).then(setProviders).catch(() => {});
  const loadCombos = () => fetch('/api/combos').then(r => r.json()).then(setCombos).catch(() => {});

  useEffect(() => {
    loadProviders();
    loadCombos();
    // Load hardcoded models, then merge with live-discovered models.
    fetch('/api/providers/models').then(r => r.json()).then(d => {
      const known = d.models || [];
      // Also fetch live models from connected providers.
      fetch('/api/providers/models/live').then(r => r.json()).then(live => {
        const liveModels = live.models || [];
        // Merge: live models that aren't already in the known list.
        const knownIds = new Set(known.map(m => m.model_id));
        const merged = [...known];
        for (const m of liveModels) {
          if (!knownIds.has(m.model_id)) {
            merged.push(m);
          }
        }
        setAllModels(merged);
      }).catch(() => setAllModels(known));
    }).catch(() => {});
  }, []);

  const saveProvider = async () => {
    const body = { ...providerForm };
    if (!body.name) body.name = body.type.charAt(0).toUpperCase() + body.type.slice(1);
    const res = await fetch('/api/providers', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(body),
    });
    const data = await res.json();
    if (data.error) { setMsg(data.error); return; }
    setShowAddProvider(false);
    setProviderForm({ type: 'anthropic', name: '', api_key: '', base_url: '' });
    setMsg('Provider added. Restart the SageClaw server to activate.');
    setTimeout(() => setMsg(''), 5000);
    loadProviders();
  };

  const deleteProvider = async (id) => {
    if (!confirm('Delete this provider?')) return;
    await fetch(`/api/providers/${id}`, { method: 'DELETE' });
    loadProviders();
  };

  const showToast = (text, type = 'info') => {
    setToast({ text, type });
    setTimeout(() => setToast(null), 4000);
  };

  const testConnection = async (provider) => {
    setTesting(provider.id);
    try {
      const res = await fetch('/api/health');
      const health = await res.json();
      const status = health.providers?.[provider.type];
      if (status === 'connected') {
        showToast(`${provider.name}: Connected successfully!`, 'success');
      } else {
        showToast(`${provider.name}: ${status || 'Not reachable'}. Restart SageClaw after adding the key.`, 'warning');
      }
    } catch {
      showToast(`${provider.name}: Connection test failed.`, 'error');
    }
    setTesting(null);
  };

  const saveCombo = async () => {
    const res = await fetch('/api/combos', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(comboForm),
    });
    const data = await res.json();
    if (data.error) { setMsg(data.error); return; }
    setShowAddCombo(false);
    setComboForm({ name: '', description: '', strategy: 'priority', models: [] });
    loadCombos();
  };

  const deleteCombo = async (id) => {
    if (!confirm('Delete this combo?')) return;
    try {
      const res = await fetch(`/api/combos/${id}`, { method: 'DELETE', credentials: 'include' });
      const data = await res.json();
      if (data.warning) {
        alert(data.warning);
      }
    } catch {}
    loadCombos();
  };

  const typeColors = {
    anthropic: 'var(--brand-anthropic)', openai: 'var(--brand-openai)',
    gemini: 'var(--brand-gemini)', openrouter: 'var(--brand-openrouter)',
    github: 'var(--brand-github)', ollama: 'var(--brand-ollama)',
  };
  const typeIcon = (type) => (
    <span style={`display:inline-block;width:10px;height:10px;border-radius:50%;background:${typeColors[type] || '#888'};margin-right:8px`} />
  );

  const defaultURLs = {
    anthropic: 'https://api.anthropic.com', openai: 'https://api.openai.com',
    gemini: 'https://generativelanguage.googleapis.com', openrouter: 'https://openrouter.ai/api/v1',
    github: 'https://api.githubcopilot.com', ollama: 'http://localhost:11434',
  };

  const keyPlaceholders = {
    anthropic: 'sk-ant-...', openai: 'sk-...', gemini: 'AIza...',
    openrouter: 'sk-or-...', github: 'ghp_...',
  };

  const needsKey = { anthropic: true, openai: true, gemini: true, openrouter: true, github: true, ollama: false };

  // Check how many providers are connected for combo validation.
  const connectedCount = providers.filter(p => p.status === 'active').length;

  return (
    <div>
      {toast && <div class={`toast toast-${toast.type}`}>{toast.text}</div>}
      <h1>Providers</h1>

      <div class="tab-bar">
        <button class={tab === 'providers' ? 'tab-active' : ''} onClick={() => setTab('providers')}>
          Providers ({providers.length})
        </button>
        <button class={tab === 'combos' ? 'tab-active' : ''} onClick={() => setTab('combos')}>
          Combos ({combos.length})
        </button>
      </div>

      {msg && <div class="card" style={`padding:0.75rem;margin-bottom:1rem;color:${msg.includes('success') || msg.includes('Connected') ? 'var(--success)' : 'var(--warning)'}`}>{msg}</div>}

      {tab === 'providers' && (
        <div>
          <div style="display:flex;justify-content:flex-end;margin-bottom:1rem">
            <button class="btn-primary" onClick={() => setShowAddProvider(true)}>+ Add Provider</button>
          </div>

          {providers.length === 0 ? (
            <div class="empty">No providers configured. Add one to start using SageClaw.</div>
          ) : (
            <div class="card-list">
              {providers.map(p => (
                <div class="card" key={p.id} style="padding:1rem">
                  <div style="display:flex;justify-content:space-between;align-items:center">
                    <div>
                      <strong style="font-size:1.1rem">{typeIcon(p.type)}{p.name}</strong>
                      <span class="badge badge-gray" style="margin-left:0.75rem">{p.type}</span>
                      <span class={`badge ${p.status === 'active' ? 'badge-green' : 'badge-gray'}`} style="margin-left:0.5rem">{p.status}</span>
                    </div>
                    <div style="display:flex;gap:0.5rem">
                      <button class="btn-small" onClick={() => testConnection(p)} disabled={testing === p.id}>
                        {testing === p.id ? 'Testing...' : 'Test'}
                      </button>
                      <button class="btn-small btn-danger" onClick={() => deleteProvider(p.id)}>Delete</button>
                    </div>
                  </div>
                  <div style="font-size:12px;color:var(--text-muted);margin-top:8px">
                    {p.base_url && <span>Base URL: {p.base_url} · </span>}
                    API Key: {p.has_key ? 'Configured' : 'Missing'}
                  </div>
                </div>
              ))}
            </div>
          )}
        </div>
      )}

      {tab === 'combos' && (
        <div>
          <div style="display:flex;justify-content:space-between;align-items:center;margin-bottom:1rem">
            <p style="color:var(--text-muted);font-size:13px">
              Combos route requests to models in priority order. Assign a combo to an agent instead of a specific model.
            </p>
            <button class="btn-primary" onClick={() => setShowAddCombo(true)} disabled={connectedCount === 0}
              title={connectedCount === 0 ? 'Add and connect at least one provider first' : ''}>
              + Create Combo
            </button>
          </div>

          {connectedCount === 0 && (
            <div class="card" style="padding:12px;margin-bottom:12px;border-color:var(--warning)">
              <span style="color:var(--warning)">Add and connect at least one provider before creating combos.</span>
            </div>
          )}

          <div class="card-list">
            {combos.map(c => {
              const comboModels = Array.isArray(c.models) ? c.models : [];
              return (
                <div class="card" key={c.id} style="padding:1rem;cursor:pointer"
                  onClick={() => setExpandedCombo(expandedCombo === c.id ? null : c.id)}>
                  <div style="display:flex;justify-content:space-between;align-items:center">
                    <div>
                      <strong style="font-size:1.1rem">{c.name}</strong>
                      {c.is_preset && <span class="badge badge-blue" style="margin-left:0.75rem">preset</span>}
                      <span class="badge badge-gray" style="margin-left:0.5rem">{c.strategy}</span>
                      <span style="color:var(--text-muted);font-size:12px;margin-left:0.75rem">{comboModels.length} models</span>
                    </div>
                    <div style="display:flex;gap:6px;align-items:center">
                      <span style="font-size:11px;color:var(--text-muted);font-family:var(--mono)">combo:{c.id}</span>
                      {!c.is_preset && (
                        <div style="display:flex;gap:4px">
                          <button class="btn-small" onClick={(e) => {
                            e.stopPropagation();
                            setComboForm({ name: c.name, description: c.description || '', strategy: c.strategy, models: comboModels });
                            setEditingComboId(c.id);
                            setShowAddCombo(true);
                          }}>Edit</button>
                          <button class="btn-small btn-danger" onClick={(e) => { e.stopPropagation(); deleteCombo(c.id); }}>Delete</button>
                        </div>
                      )}
                    </div>
                  </div>
                  {c.description && <div style="color:var(--text-muted);font-size:13px;margin-top:4px">{c.description}</div>}
                  {expandedCombo === c.id && comboModels.length > 0 && (
                    <div style="margin-top:10px;border-top:1px solid var(--border);padding-top:10px">
                      <div style="font-size:11px;color:var(--text-muted);margin-bottom:6px;text-transform:uppercase;letter-spacing:0.5px">Fallback Chain</div>
                      {comboModels.map((m, i) => (
                        <div key={i} style="display:flex;align-items:center;gap:8px;padding:4px 0;font-size:13px">
                          <span style="color:var(--text-muted);font-size:11px;width:18px;text-align:right">{i + 1}.</span>
                          <span class="badge badge-gray" style="font-size:10px">{m.provider_type || m.provider || '?'}</span>
                          <span style="font-family:var(--mono)">{m.model || m.model_id || (typeof m === 'string' ? m : '?')}</span>
                        </div>
                      ))}
                    </div>
                  )}
                </div>
              );
            })}
          </div>
        </div>
      )}

      {/* Add Provider Modal */}
      {showAddProvider && (
        <div class="modal-overlay" onClick={() => setShowAddProvider(false)} role="dialog" aria-modal="true" aria-labelledby="add-provider-title">
          <div class="modal-content" onClick={e => e.stopPropagation()}>
            <h2 id="add-provider-title">Add Provider</h2>
            <div class="form-group">
              <Label text="Provider Type" tip="The AI service to connect. Each provider offers different models and pricing." />
              <select value={providerForm.type} onChange={e => {
                const t = e.target.value;
                setProviderForm({ ...providerForm, type: t, name: t.charAt(0).toUpperCase() + t.slice(1), base_url: defaultURLs[t] || '' });
              }}>
                <option value="anthropic">Anthropic (Claude)</option>
                <option value="openai">OpenAI (GPT)</option>
                <option value="gemini">Google Gemini</option>
                <option value="openrouter">OpenRouter (200+ models)</option>
                <option value="github">GitHub Copilot</option>
                <option value="ollama">Ollama (Local, no key needed)</option>
              </select>
            </div>
            <div class="form-group">
              <Label text="Display Name" tip="A friendly name for this provider in the dashboard." />
              <input type="text" placeholder="e.g. My Anthropic Account" value={providerForm.name}
                onInput={e => setProviderForm({ ...providerForm, name: e.target.value })} />
            </div>
            <div class="form-group">
              <Label text="Base URL" tip="The API endpoint. Only change this if you're using a custom proxy or self-hosted instance." />
              <input type="text" placeholder={defaultURLs[providerForm.type] || 'https://...'} value={providerForm.base_url}
                onInput={e => setProviderForm({ ...providerForm, base_url: e.target.value })} />
            </div>
            {needsKey[providerForm.type] !== false && (
              <div class="form-group">
                <Label text="API Key" tip="Your secret API key. Stored encrypted in the database. Never displayed after saving." />
                <input type="password" placeholder={keyPlaceholders[providerForm.type] || 'Enter API key'}
                  value={providerForm.api_key}
                  onInput={e => setProviderForm({ ...providerForm, api_key: e.target.value })} />
              </div>
            )}
            <div style="display:flex;gap:0.5rem;margin-top:1rem">
              <button class="btn-primary" onClick={saveProvider} disabled={!providerForm.type}>Save</button>
              <button class="btn-secondary" onClick={() => setShowAddProvider(false)}>Cancel</button>
            </div>
          </div>
        </div>
      )}

      {/* Add Combo Modal */}
      {showAddCombo && (() => {
        const selectedIds = new Set((comboForm.models || []).map(m => m.model));
        const q = comboSearch.trim().toLowerCase();
        const searchResults = allModels.filter(m =>
          !selectedIds.has(m.model_id) &&
          (q === '' ||
           (m.name || '').toLowerCase().includes(q) ||
           (m.id || '').toLowerCase().includes(q) ||
           (m.model_id || '').toLowerCase().includes(q) ||
           (m.provider || '').toLowerCase().includes(q))
        );

        const addModel = (m) => {
          const priority = (comboForm.models || []).length + 1;
          setComboForm({
            ...comboForm,
            models: [...(comboForm.models || []), {
              provider_type: m.provider,
              model: m.model_id,
              display: `${m.id} — ${m.name}`,
              priority,
            }],
          });
          setComboSearch('');
        };

        const removeModel = (idx) => {
          const next = [...comboForm.models];
          next.splice(idx, 1);
          next.forEach((m, i) => m.priority = i + 1);
          setComboForm({ ...comboForm, models: next });
        };

        const moveModel = (from, to) => {
          if (to < 0 || to >= comboForm.models.length) return;
          const next = [...comboForm.models];
          const [item] = next.splice(from, 1);
          next.splice(to, 0, item);
          next.forEach((m, i) => m.priority = i + 1);
          setComboForm({ ...comboForm, models: next });
        };

        return (
          <div class="modal-overlay" onClick={() => { setShowAddCombo(false); setEditingComboId(null); }} role="dialog" aria-modal="true" aria-labelledby="add-combo-title">
            <div class="modal-content" style="width:560px;max-height:85vh;overflow-y:auto" onClick={e => e.stopPropagation()}>
              <h2 id="add-combo-title">{editingComboId ? 'Edit Combo' : 'Create Combo'}</h2>
              <p style="color:var(--text-muted);font-size:12px;margin-bottom:16px">
                Define a fallback chain. Requests to this combo name try each model in order.
              </p>
              <div class="form-group">
                <Label text="Name" tip="A short name for this routing combo." />
                <input type="text" placeholder="e.g. Budget Friendly"
                  value={comboForm.name} onInput={e => setComboForm({ ...comboForm, name: e.target.value })} />
              </div>
              <div class="form-group">
                <Label text="Description" tip="What this combo is optimized for." />
                <input type="text" placeholder="e.g. Cheap models first, strong fallback"
                  value={comboForm.description} onInput={e => setComboForm({ ...comboForm, description: e.target.value })} />
              </div>
              <div class="form-group">
                <Label text="Strategy" tip="Priority: try in order. Round Robin: distribute. Cost: cheapest first." />
                <select value={comboForm.strategy} onChange={e => setComboForm({ ...comboForm, strategy: e.target.value })}>
                  <option value="priority">Priority (try in order)</option>
                  <option value="round-robin">Round Robin</option>
                  <option value="cost">Cost (cheapest first)</option>
                </select>
              </div>

              <div class="form-group">
                <Label text="Models" tip="Add models in priority order. Drag to reorder. Only connected providers shown." />

                {/* Selected models — reorderable */}
                {(comboForm.models || []).length > 0 && (
                  <div style="margin-bottom:12px">
                    {comboForm.models.map((m, idx) => (
                      <div key={idx} style="display:flex;align-items:center;gap:8px;padding:8px 10px;background:var(--bg);border:1px solid var(--border);border-radius:4px;margin-bottom:4px">
                        <span style="color:var(--text-muted);font-size:12px;font-weight:700;width:20px">#{m.priority}</span>
                        <div style="flex:1;font-size:12px">
                          <span style="color:var(--primary);font-family:var(--mono)">{m.model}</span>
                          <span style="color:var(--text-muted);margin-left:6px">({m.provider_type})</span>
                        </div>
                        <button class="btn-small" onClick={() => moveModel(idx, idx - 1)} disabled={idx === 0}
                          style="padding:2px 6px;font-size:11px" title="Move up">{'\u2191'}</button>
                        <button class="btn-small" onClick={() => moveModel(idx, idx + 1)} disabled={idx === comboForm.models.length - 1}
                          style="padding:2px 6px;font-size:11px" title="Move down">{'\u2193'}</button>
                        <button class="btn-small btn-danger" onClick={() => removeModel(idx)}
                          style="padding:2px 6px;font-size:11px">{'\u00D7'}</button>
                      </div>
                    ))}
                  </div>
                )}

                {/* Search to add — dropdown opens upward to avoid modal overflow */}
                <div style="position:relative">
                  {comboSearch && searchResults.length > 0 && (
                    <div style="position:absolute;bottom:100%;left:0;right:0;background:var(--surface);border:1px solid var(--border);border-radius:6px 6px 0 0;max-height:200px;overflow-y:auto;z-index:100;box-shadow:0 -4px 24px rgba(0,0,0,0.4)">
                      {searchResults.slice(0, 15).map(m => (
                        <div key={m.id} style="padding:8px 12px;cursor:pointer;font-size:12px;border-bottom:1px solid var(--border)"
                          onMouseDown={() => addModel(m)}
                          onMouseEnter={e => e.currentTarget.style.background = 'var(--surface-hover)'}
                          onMouseLeave={e => e.currentTarget.style.background = ''}>
                          <div>
                            <span style="font-family:var(--mono);color:var(--primary)">{m.id}</span>
                            {m.tier && <span class={`badge badge-${m.tier === 'strong' ? 'blue' : m.tier === 'fast' ? 'green' : 'gray'}`}
                              style="margin-left:6px">{m.tier}</span>}
                          </div>
                          <div style="color:var(--text-muted);margin-top:2px">
                            {!m.input_cost && !m.output_cost ? '' : m.input_cost === 0 ? 'Free' : `$${m.input_cost} in / $${m.output_cost} out per 1M tokens`}
                            {m.context_window > 0 && ` · ${(m.context_window / 1000).toFixed(0)}K context`}
                          </div>
                        </div>
                      ))}
                    </div>
                  )}
                  <input type="text" placeholder="+ Add model (type to search)..." value={comboSearch}
                    onInput={e => setComboSearch(e.target.value)} />
                </div>
              </div>

              <div style="display:flex;gap:0.5rem;margin-top:1rem">
                <button class="btn-primary" onClick={() => {
                  const models = (comboForm.models || []).map(m => ({
                    provider_type: m.provider_type || m.provider, model: m.model || m.model_id, priority: m.priority,
                  }));
                  saveCombo({ ...comboForm, models: JSON.stringify(models) });
                  setEditingComboId(null);
                }} disabled={!comboForm.name || !(comboForm.models || []).length}>
                  {editingComboId ? 'Save Changes' : 'Create'}
                </button>
                <button class="btn-secondary" onClick={() => { setShowAddCombo(false); setEditingComboId(null); }}>Cancel</button>
              </div>
            </div>
          </div>
        );
      })()}
    </div>
  );
}
