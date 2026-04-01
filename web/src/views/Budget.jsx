import { h } from 'preact';
import { useState, useEffect } from 'preact/hooks';

export default function Budget() {
  const [summary, setSummary] = useState(null);
  const [config, setConfig] = useState({ daily_limit_usd: 0, monthly_limit_usd: 0, alert_at_percent: 80, hard_stop: false });
  const [history, setHistory] = useState([]);
  const [alerts, setAlerts] = useState([]);
  const [topModels, setTopModels] = useState([]);
  const [editing, setEditing] = useState(false);
  const [editForm, setEditForm] = useState({});
  const [toast, setToast] = useState(null);
  const [tab, setTab] = useState('overview');
  const [pricing, setPricing] = useState([]);
  const [pricingEdit, setPricingEdit] = useState(null);

  const load = () => {
    fetch('/api/budget/summary').then(r => r.json()).then(setSummary).catch(() => {});
    fetch('/api/budget/config').then(r => r.json()).then(c => { setConfig(c); setEditForm(c); }).catch(() => {});
    fetch('/api/budget/history?days=30').then(r => r.json()).then(d => setHistory(d || [])).catch(() => {});
    fetch('/api/budget/alerts').then(r => r.json()).then(d => setAlerts(d || [])).catch(() => {});
    fetch('/api/budget/top-models').then(r => r.json()).then(d => setTopModels(d || [])).catch(() => {});
    fetch('/api/budget/pricing').then(r => r.json()).then(d => setPricing(d || [])).catch(() => {});
  };

  useEffect(load, []);

  const showToast = (msg, type = 'success') => {
    setToast({ msg, type });
    setTimeout(() => setToast(null), 3000);
  };

  const saveConfig = async () => {
    const res = await fetch('/api/budget/config', {
      method: 'PUT',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(editForm),
    });
    if (res.ok) {
      showToast('Budget settings saved');
      setEditing(false);
      load();
    } else {
      showToast('Failed to save', 'error');
    }
  };

  const ackAlert = async (id) => {
    await fetch(`/api/budget/alerts/${id}`, { method: 'POST' });
    load();
  };

  const pctBar = (pct, color) => (
    <div style="background:var(--border);border-radius:4px;height:8px;overflow:hidden;margin-top:6px">
      <div style={{
        width: `${Math.min(pct, 100)}%`,
        height: '100%',
        background: pct >= 100 ? 'var(--error)' : pct >= 80 ? 'var(--warning)' : (color || 'var(--primary)'),
        borderRadius: 4,
        transition: 'width 0.3s',
      }} />
    </div>
  );

  const maxCost = Math.max(...history.map(h => h.cost_usd), 0.01);

  return (
    <div>
      <div style="display:flex;justify-content:space-between;align-items:center;margin-bottom:1.5rem">
        <h1>Budget & Costs</h1>
        <button class="btn-secondary" onClick={() => { setEditing(!editing); setEditForm(config); }}>
          {editing ? 'Cancel' : 'Settings'}
        </button>
      </div>

      {toast && <div class={`toast toast-${toast.type}`}>{toast.msg}</div>}

      {/* Budget settings form */}
      {editing && (
        <div class="card" style="padding:20px;margin-bottom:1.5rem">
          <h3 style="margin-bottom:16px">Budget Settings</h3>
          <div style="display:grid;grid-template-columns:1fr 1fr;gap:16px">
            <div class="form-group">
              <label>Daily Limit (USD)</label>
              <input type="number" step="0.01" min="0" placeholder="0 = no limit"
                value={editForm.daily_limit_usd || ''}
                onInput={e => setEditForm({ ...editForm, daily_limit_usd: parseFloat(e.target.value) || 0 })} />
            </div>
            <div class="form-group">
              <label>Monthly Limit (USD)</label>
              <input type="number" step="0.01" min="0" placeholder="0 = no limit"
                value={editForm.monthly_limit_usd || ''}
                onInput={e => setEditForm({ ...editForm, monthly_limit_usd: parseFloat(e.target.value) || 0 })} />
            </div>
            <div class="form-group">
              <label>Alert at (%)</label>
              <input type="number" step="1" min="1" max="100" placeholder="80"
                value={editForm.alert_at_percent || 80}
                onInput={e => setEditForm({ ...editForm, alert_at_percent: parseInt(e.target.value) || 80 })} />
            </div>
            <div class="form-group" style="display:flex;align-items:center;gap:8px;padding-top:20px">
              <input type="checkbox" checked={editForm.hard_stop}
                onChange={e => setEditForm({ ...editForm, hard_stop: e.target.checked })} />
              <label style="margin:0;cursor:pointer">Hard stop (block requests when budget exceeded)</label>
            </div>
          </div>
          <button class="btn-primary" onClick={saveConfig} style="margin-top:12px">Save Settings</button>
        </div>
      )}

      {/* Tab bar */}
      <div class="tab-bar">
        <button class={tab === 'overview' ? 'tab-active' : ''} onClick={() => setTab('overview')}>Overview</button>
        <button class={tab === 'history' ? 'tab-active' : ''} onClick={() => setTab('history')}>Daily History</button>
        <button class={tab === 'alerts' ? 'tab-active' : ''} onClick={() => setTab('alerts')}>
          Alerts {alerts.filter(a => !a.acknowledged).length > 0 && (
            <span class="badge badge-red" style="margin-left:4px">{alerts.filter(a => !a.acknowledged).length}</span>
          )}
        </button>
        <button class={tab === 'pricing' ? 'tab-active' : ''} onClick={() => setTab('pricing')}>Pricing</button>
      </div>

      {tab === 'overview' && summary && (
        <div>
          {/* Spending cards */}
          <div style="display:grid;grid-template-columns:1fr 1fr;gap:16px;margin-bottom:1.5rem">
            <div class="card" style="padding:20px">
              <div style="color:var(--text-muted);font-size:12px;text-transform:uppercase;letter-spacing:0.5px">Today</div>
              <div style="font-size:2rem;font-weight:700;color:var(--primary);margin:8px 0">${summary.today_usd?.toFixed(2)}</div>
              <div style="font-size:12px;color:var(--text-muted)">{summary.today_requests} requests · ${summary.today_saved_usd?.toFixed(2)} saved</div>
              {config.daily_limit_usd > 0 && (
                <div>
                  {pctBar(summary.daily_percent)}
                  <div style="font-size:11px;color:var(--text-muted);margin-top:4px">
                    ${summary.daily_remaining?.toFixed(2)} remaining of ${config.daily_limit_usd.toFixed(2)} limit
                  </div>
                </div>
              )}
            </div>
            <div class="card" style="padding:20px">
              <div style="color:var(--text-muted);font-size:12px;text-transform:uppercase;letter-spacing:0.5px">This Month</div>
              <div style="font-size:2rem;font-weight:700;color:var(--primary);margin:8px 0">${summary.month_usd?.toFixed(2)}</div>
              <div style="font-size:12px;color:var(--text-muted)">{summary.month_requests} requests · ${summary.month_saved_usd?.toFixed(2)} saved</div>
              {config.monthly_limit_usd > 0 && (
                <div>
                  {pctBar(summary.monthly_percent)}
                  <div style="font-size:11px;color:var(--text-muted);margin-top:4px">
                    ${summary.monthly_remaining?.toFixed(2)} remaining of ${config.monthly_limit_usd.toFixed(2)} limit
                  </div>
                </div>
              )}
            </div>
          </div>

          {/* Top models */}
          {topModels.length > 0 && (() => {
            const hasThinking = topModels.some(m => m.thinking_tokens > 0);
            return (
              <div class="card" style="padding:20px;margin-bottom:1.5rem">
                <h3 style="margin-bottom:12px">Top Models (This Month)</h3>
                <table class="data-table">
                  <thead>
                    <tr>
                      <th scope="col">Model</th><th scope="col">Provider</th><th scope="col">Cost</th><th scope="col">Requests</th>
                      <th scope="col">Input Tokens</th><th scope="col">Output Tokens</th>
                      {hasThinking && <th scope="col">Thinking</th>}
                    </tr>
                  </thead>
                  <tbody>
                    {topModels.map((m, i) => (
                      <tr key={i}>
                        <td><code>{m.model}</code></td>
                        <td>{m.provider}</td>
                        <td style="font-weight:600">${m.cost_usd?.toFixed(4)}</td>
                        <td>{m.requests}</td>
                        <td>{(m.input_tokens || 0).toLocaleString()}</td>
                        <td>{(m.output_tokens || 0).toLocaleString()}</td>
                        {hasThinking && <td>{(m.thinking_tokens || 0).toLocaleString()}</td>}
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            );
          })()}

          {/* Quick stats */}
          <div style="display:grid;grid-template-columns:repeat(3,1fr);gap:12px">
            <div class="card" style="text-align:center;padding:16px">
              <div style="font-size:1.5rem;font-weight:700;color:var(--success)">${(summary.today_saved_usd + summary.month_saved_usd || 0).toFixed(2)}</div>
              <div style="color:var(--text-muted);font-size:12px;margin-top:4px">Total Saved (Caching)</div>
            </div>
            <div class="card" style="text-align:center;padding:16px">
              <div style="font-size:1.5rem;font-weight:700;color:var(--primary)">{summary.month_requests || 0}</div>
              <div style="color:var(--text-muted);font-size:12px;margin-top:4px">Requests This Month</div>
            </div>
            <div class="card" style="text-align:center;padding:16px">
              <div style="font-size:1.5rem;font-weight:700;color:var(--primary)">
                {summary.month_requests > 0 ? `$${(summary.month_usd / summary.month_requests).toFixed(4)}` : '-'}
              </div>
              <div style="color:var(--text-muted);font-size:12px;margin-top:4px">Avg Cost per Request</div>
            </div>
          </div>
        </div>
      )}

      {tab === 'history' && (
        <div>
          {/* Simple bar chart */}
          {history.length > 0 ? (
            <div class="card" style="padding:20px">
              <h3 style="margin-bottom:16px">Daily Spending (Last 30 Days)</h3>
              <div style="display:flex;align-items:end;gap:4px;height:200px;border-bottom:1px solid var(--border);padding-bottom:8px">
                {history.map((d, i) => (
                  <div key={i} style="flex:1;display:flex;flex-direction:column;align-items:center;justify-content:flex-end;height:100%"
                    title={`${d.date}: $${d.cost_usd?.toFixed(4)} (${d.requests} requests)`}>
                    <div style={{
                      width: '100%',
                      maxWidth: 24,
                      height: `${Math.max((d.cost_usd / maxCost) * 100, 2)}%`,
                      background: 'var(--primary)',
                      borderRadius: '3px 3px 0 0',
                      minHeight: 2,
                    }} />
                  </div>
                ))}
              </div>
              <div style="display:flex;justify-content:space-between;margin-top:4px;font-size:11px;color:var(--text-muted)">
                <span>{history[0]?.date}</span>
                <span>{history[history.length - 1]?.date}</span>
              </div>

              {/* Table below chart */}
              <table class="data-table" style="margin-top:16px">
                <thead>
                  <tr><th scope="col">Date</th><th scope="col">Cost</th><th scope="col">Saved</th><th scope="col">Requests</th></tr>
                </thead>
                <tbody>
                  {[...history].reverse().map((d, i) => (
                    <tr key={i}>
                      <td>{d.date}</td>
                      <td style="font-weight:600">${d.cost_usd?.toFixed(4)}</td>
                      <td style="color:var(--success)">${d.saved_usd?.toFixed(4)}</td>
                      <td>{d.requests}</td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          ) : (
            <p style="color:var(--text-muted);text-align:center;margin-top:3rem">No cost data yet. Start chatting to see spending.</p>
          )}
        </div>
      )}

      {tab === 'alerts' && (
        <div>
          {alerts.length === 0 ? (
            <p style="color:var(--text-muted);text-align:center;margin-top:3rem">No budget alerts. Set daily or monthly limits to enable alerts.</p>
          ) : (
            <div class="card-list">
              {alerts.map(a => (
                <div key={a.id} class="card" style="padding:16px">
                  <div style="display:flex;justify-content:space-between;align-items:center">
                    <div>
                      <span class={`badge ${a.alert_type === 'limit_reached' ? 'badge-red' : 'badge-yellow'}`} style="margin-right:8px">
                        {a.alert_type === 'limit_reached' ? 'LIMIT REACHED' : 'WARNING'}
                      </span>
                      <strong style="text-transform:capitalize">{a.period}</strong> budget
                    </div>
                    <div style="display:flex;align-items:center;gap:8px">
                      <span style="font-size:12px;color:var(--text-muted)">{a.created_at}</span>
                      {!a.acknowledged && (
                        <button class="btn-small" onClick={() => ackAlert(a.id)}>Dismiss</button>
                      )}
                    </div>
                  </div>
                  <div style="margin-top:8px;font-size:13px;color:var(--text-muted)">
                    Spent ${a.spent_usd?.toFixed(2)} of ${a.limit_usd?.toFixed(2)} limit ({a.percent?.toFixed(0)}%)
                  </div>
                  {pctBar(a.percent)}
                </div>
              ))}
            </div>
          )}
        </div>
      )}

      {tab === 'pricing' && (
        <div>
          {/* Pricing editor modal */}
          {pricingEdit && (
            <div style="position:fixed;inset:0;background:rgba(0,0,0,0.5);display:flex;align-items:center;justify-content:center;z-index:100"
              onClick={e => { if (e.target === e.currentTarget) setPricingEdit(null); }}>
              <div class="card" style="padding:24px;min-width:400px;max-width:500px" onClick={e => e.stopPropagation()}>
                <h3 style="margin-bottom:16px">Edit Pricing: {pricingEdit.model_id}</h3>
                <div style="display:grid;grid-template-columns:1fr 1fr;gap:12px">
                  {['input_cost', 'output_cost', 'cache_cost', 'thinking_cost', 'cache_creation_cost'].map(field => (
                    <div class="form-group" key={field}>
                      <label style="font-size:12px;text-transform:capitalize">{field.replace(/_/g, ' ')} ($/1M)</label>
                      <input type="number" step="0.01" min="0"
                        value={pricingEdit[field] ?? 0}
                        onInput={e => setPricingEdit({ ...pricingEdit, [field]: parseFloat(e.target.value) || 0 })} />
                    </div>
                  ))}
                </div>
                <div style="display:flex;gap:8px;margin-top:16px;justify-content:flex-end">
                  <button class="btn-secondary" onClick={() => setPricingEdit(null)}>Cancel</button>
                  <button class="btn-primary" onClick={async () => {
                    const res = await fetch('/api/budget/pricing', {
                      method: 'PUT',
                      headers: { 'Content-Type': 'application/json' },
                      body: JSON.stringify(pricingEdit),
                    });
                    if (res.ok) {
                      showToast(`Pricing override saved for ${pricingEdit.model_id}`);
                      setPricingEdit(null);
                      load();
                    } else {
                      showToast('Failed to save override', 'error');
                    }
                  }}>Save Override</button>
                </div>
              </div>
            </div>
          )}

          <div class="card" style="padding:20px">
            <div style="display:flex;justify-content:space-between;align-items:center;margin-bottom:16px">
              <h3>Model Pricing</h3>
              <span style="font-size:12px;color:var(--text-muted)">{pricing.length} models</span>
            </div>
            {pricing.length === 0 ? (
              <p style="color:var(--text-muted);text-align:center;padding:2rem 0">No models discovered yet. Add a provider to see pricing.</p>
            ) : (
              <table class="data-table">
                <thead>
                  <tr>
                    <th scope="col">Model</th>
                    <th scope="col">Provider</th>
                    <th scope="col">Source</th>
                    <th scope="col">Input</th>
                    <th scope="col">Output</th>
                    <th scope="col">Cache</th>
                    <th scope="col" style="width:1px"></th>
                  </tr>
                </thead>
                <tbody>
                  {pricing.map((m, i) => (
                    <tr key={i}>
                      <td><code style="font-size:12px">{m.model_id}</code></td>
                      <td>{m.provider}</td>
                      <td>
                        <span class={`badge ${
                          m.pricing_source === 'user' ? 'badge-blue' :
                          m.pricing_source === 'openrouter' ? 'badge-green' :
                          m.pricing_source === 'known' ? 'badge-gray' : 'badge-yellow'
                        }`} style="font-size:10px;text-transform:uppercase">
                          {m.pricing_source === 'user' ? 'Custom' :
                           m.pricing_source === 'openrouter' ? 'Auto' :
                           m.pricing_source === 'known' ? 'Built-in' : 'Unknown'}
                        </span>
                      </td>
                      <td style="font-size:12px">${m.input_cost?.toFixed(2)}</td>
                      <td style="font-size:12px">${m.output_cost?.toFixed(2)}</td>
                      <td style="font-size:12px">${m.cache_cost?.toFixed(2)}</td>
                      <td style="white-space:nowrap">
                        <button class="btn-small" onClick={() => setPricingEdit({
                          model_id: m.model_id, provider: m.provider,
                          input_cost: m.input_cost, output_cost: m.output_cost,
                          cache_cost: m.cache_cost, thinking_cost: m.thinking_cost,
                          cache_creation_cost: m.cache_creation_cost,
                        })}>Edit</button>
                        {m.has_override && (
                          <button class="btn-small" style="margin-left:4px;color:var(--error)" onClick={async () => {
                            await fetch(`/api/budget/pricing/${encodeURIComponent(m.model_id)}`, { method: 'DELETE' });
                            showToast(`Override removed for ${m.model_id}`);
                            load();
                          }}>Reset</button>
                        )}
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            )}
          </div>
        </div>
      )}
    </div>
  );
}
