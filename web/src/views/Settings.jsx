import { h } from 'preact';
import { useState, useEffect, useRef } from 'preact/hooks';
import { Providers } from './Providers';
import { Channels } from './Channels';
import Cron from './Cron';
import Tunnel from './Tunnel';
import Tools from './Tools';
import { Breadcrumb } from '../components/Breadcrumb';

const TABS = [
  { id: 'general', label: 'General' },
  { id: 'ai-models', label: 'AI Models' },
  { id: 'channels', label: 'Channels' },
  { id: 'tools', label: 'Tools' },
  { id: 'scheduled-tasks', label: 'Scheduled Tasks' },
  { id: 'external-access', label: 'External Access' },
  { id: 'budget-alerts', label: 'Budget & Alerts' },
  { id: 'security', label: 'Security' },
  { id: 'import-export', label: 'Import / Export' },
];

export function Settings() {
  const params = new URLSearchParams(window.location.search);
  const [tab, setTab] = useState(params.get('tab') || 'general');

  const changeTab = (id) => {
    setTab(id);
    const url = id === 'general' ? '/settings' : `/settings?tab=${id}`;
    history.replaceState(null, '', url);
  };

  return (
    <div>
      <Breadcrumb items={[{ label: 'Settings' }]} />
      <h1>Settings</h1>
      <div class="settings-layout">
        <div class="settings-tabs" role="tablist" aria-orientation="vertical">
          {TABS.map((t, i) => (
            <button
              key={t.id}
              role="tab"
              aria-selected={tab === t.id}
              tabIndex={tab === t.id ? 0 : -1}
              class={`settings-tab ${tab === t.id ? 'active' : ''}`}
              onClick={() => changeTab(t.id)}
              onKeyDown={(e) => {
                let next = -1;
                if (e.key === 'ArrowDown') next = (i + 1) % TABS.length;
                else if (e.key === 'ArrowUp') next = (i - 1 + TABS.length) % TABS.length;
                else if (e.key === 'Home') next = 0;
                else if (e.key === 'End') next = TABS.length - 1;
                if (next >= 0) {
                  e.preventDefault();
                  changeTab(TABS[next].id);
                  e.target.parentElement.children[next]?.focus();
                }
              }}
            >
              {t.label}
            </button>
          ))}
        </div>
        <div class="settings-content">
          {tab === 'general' && <GeneralTab />}
          {tab === 'ai-models' && <div class="settings-embed"><Providers /></div>}
          {tab === 'channels' && <div class="settings-embed"><Channels /></div>}
          {tab === 'tools' && <div class="settings-embed"><Tools /></div>}
          {tab === 'scheduled-tasks' && <div class="settings-embed"><Cron /></div>}
          {tab === 'external-access' && <div class="settings-embed"><Tunnel /></div>}
          {tab === 'budget-alerts' && <BudgetAlertsTab />}
          {tab === 'security' && <SecurityTab />}
          {tab === 'import-export' && <ImportExportTab />}
        </div>
      </div>
    </div>
  );
}

function GeneralTab() {
  const [theme, setTheme] = useState(localStorage.getItem('sageclaw-theme') || 'dark');
  const [utilityModel, setUtilityModel] = useState('auto');
  const [mechModels, setMechModels] = useState({ snip: 'auto', compact: 'auto', review: 'auto' });
  const [showAdvanced, setShowAdvanced] = useState(false);
  const [models, setModels] = useState([]);
  const [saving, setSaving] = useState(false);
  const [toast, setToast] = useState(null);

  const changeTheme = (next) => {
    setTheme(next);
    localStorage.setItem('sageclaw-theme', next);
    document.documentElement.setAttribute('data-theme', next);
  };

  useEffect(() => {
    fetch('/api/settings/utility-model', { credentials: 'include' })
      .then(r => r.json()).then(d => { if (d.model) setUtilityModel(d.model); }).catch(() => {});
    fetch('/api/settings/mechanism-models', { credentials: 'include' })
      .then(r => r.json()).then(d => { if (d) setMechModels(prev => ({ ...prev, ...d })); }).catch(() => {});
    fetch('/api/providers/models', { credentials: 'include' })
      .then(r => r.json()).then(d => {
        const list = Array.isArray(d) ? d : (d && Array.isArray(d.models) ? d.models : []);
        if (list.length > 0) {
          const available = list.filter(m => m.available);
          setModels(available.length > 0 ? available : list);
        }
      }).catch(() => {});
  }, []);

  const saveModels = async () => {
    setSaving(true);
    try {
      const [r1, r2] = await Promise.all([
        fetch('/api/settings/utility-model', {
          method: 'PUT', credentials: 'include',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ model: utilityModel }),
        }),
        fetch('/api/settings/mechanism-models', {
          method: 'PUT', credentials: 'include',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify(mechModels),
        }),
      ]);
      if (r1.ok && r2.ok) {
        setToast({ msg: 'Saved. Restart server to apply.', type: 'success' });
      } else {
        setToast({ msg: 'Failed to save', type: 'error' });
      }
    } catch { setToast({ msg: 'Failed to save', type: 'error' }); }
    setSaving(false);
    setTimeout(() => setToast(null), 3000);
  };

  return (
    <div>
      {toast && <div class={`toast toast-${toast.type}`}>{toast.msg}</div>}

      <h3 style="margin-bottom:16px">Appearance</h3>
      <div class="form-group">
        <label>Theme</label>
        <div style="display:flex;gap:8px">
          <button class={theme === 'dark' ? 'btn-primary' : 'btn-secondary'} onClick={() => changeTheme('dark')}>Dark</button>
          <button class={theme === 'light' ? 'btn-primary' : 'btn-secondary'} onClick={() => changeTheme('light')}>Light</button>
        </div>
      </div>

      <h3 style="margin-top:24px;margin-bottom:16px">Default Background Model</h3>
      <p style="font-size:12px;color:var(--text-muted);margin-bottom:12px">
        Used for context summaries, compaction, and background tasks. Leave on Auto to use the cheapest available model.
      </p>
      <div style="display:flex;gap:8px;align-items:center">
        <select value={utilityModel} onChange={e => setUtilityModel(e.target.value)}
          style="flex:1;max-width:320px">
          <option value="auto">Auto (recommended)</option>
          {models.map(m => (
            <option key={m.model_id || m.id} value={m.model_id || m.id}>
              {m.name || m.model_id || m.id} — {m.tier || 'unknown'}
            </option>
          ))}
        </select>
        <button class="btn-primary" style="padding:6px 16px;font-size:13px" onClick={saveModels} disabled={saving}>
          {saving ? 'Saving...' : 'Save'}
        </button>
      </div>

      <div style="margin-top:16px">
        <button class="btn-secondary" style="padding:4px 12px;font-size:12px"
          onClick={() => setShowAdvanced(!showAdvanced)}>
          {showAdvanced ? 'Hide' : 'Show'} Advanced Model Settings
        </button>
      </div>
      {showAdvanced && (
        <div style="margin-top:12px;padding:12px;border:1px solid var(--border);border-radius:8px">
          <p style="font-size:12px;color:var(--text-muted);margin-bottom:12px">
            Override the background model for specific mechanisms. "Auto" inherits the default above.
          </p>
          {[
            { key: 'snip', label: 'Snip Summary', desc: 'Generates one-line summaries when snipping old tool results' },
            { key: 'compact', label: 'Compaction', desc: 'Summarizes conversation history during context compaction' },
            { key: 'review', label: 'Background Review', desc: 'Extracts learnings and procedures from conversation' },
          ].map(({ key, label, desc }) => (
            <div key={key} style="margin-bottom:12px">
              <label style="font-size:13px;font-weight:500">{label}</label>
              <p style="font-size:11px;color:var(--text-muted);margin:2px 0 6px">{desc}</p>
              <select value={mechModels[key] || 'auto'}
                onChange={e => setMechModels(prev => ({ ...prev, [key]: e.target.value }))}
                style="max-width:320px;width:100%">
                <option value="auto">Auto (use default)</option>
                {models.map(m => (
                  <option key={m.model_id || m.id} value={m.model_id || m.id}>
                    {m.name || m.model_id || m.id} — {m.tier || 'unknown'}
                  </option>
                ))}
              </select>
            </div>
          ))}
        </div>
      )}

      <h3 style="margin-top:24px;margin-bottom:16px">About</h3>
      <div style="font-size:13px;color:var(--text-muted)">
        <p>SageClaw — AI Agent Framework</p>
        <p style="margin-top:4px">Dashboard v0.4.0</p>
      </div>
    </div>
  );
}

function BudgetAlertsTab() {
  const [config, setConfig] = useState({ daily_limit_usd: 0, monthly_limit_usd: 0, alert_at_percent: 80, hard_stop: false });
  const [editForm, setEditForm] = useState({});
  const [toast, setToast] = useState(null);

  useEffect(() => {
    fetch('/api/budget/config').then(r => r.json()).then(c => { setConfig(c); setEditForm(c); }).catch(() => {});
  }, []);

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
      fetch('/api/budget/config').then(r => r.json()).then(c => { setConfig(c); setEditForm(c); }).catch(() => {});
    } else {
      showToast('Failed to save', 'error');
    }
  };

  return (
    <div>
      {toast && <div class={`toast toast-${toast.type}`}>{toast.msg}</div>}

      <h3 style="margin-bottom:16px">Spending Limits</h3>
      <div style="display:grid;grid-template-columns:1fr 1fr;gap:16px;margin-bottom:20px">
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
      </div>

      <h3 style="margin-bottom:16px">Alert Settings</h3>
      <div style="display:grid;grid-template-columns:1fr 1fr;gap:16px;margin-bottom:20px">
        <div class="form-group">
          <label>Alert at (%)</label>
          <input type="number" step="1" min="1" max="100" placeholder="80"
            value={editForm.alert_at_percent || 80}
            onInput={e => setEditForm({ ...editForm, alert_at_percent: parseInt(e.target.value) || 80 })} />
        </div>
        <div class="form-group" style="padding-top:20px">
          <label style="display:flex;align-items:center;gap:8px;margin:0;cursor:pointer">
            <input type="checkbox" checked={editForm.hard_stop}
              onChange={e => setEditForm({ ...editForm, hard_stop: e.target.checked })} />
            Hard stop (block requests when budget exceeded)
          </label>
        </div>
      </div>

      <button class="btn-primary" onClick={saveConfig}>Save Settings</button>

      <div style="margin-top:24px;padding-top:16px;border-top:1px solid var(--border)">
        <p style="color:var(--text-muted);font-size:13px">
          View spending trends and history on the <a href="/?tab=budget">Dashboard Budget tab</a>.
        </p>
      </div>
    </div>
  );
}

function SecurityTab() {
  return (
    <div>
      <PasswordSection />
      <TwoFactorSection />
      <ConsentGrantsSection />
      <CredentialsSection />
    </div>
  );
}

function TwoFactorSection() {
  const [enabled, setEnabled] = useState(null);
  const [setupData, setSetupData] = useState(null);
  const [password, setPassword] = useState('');
  const [error, setError] = useState('');
  const [loading, setLoading] = useState(false);

  useEffect(() => {
    fetch('/api/auth/check', { credentials: 'include' })
      .then(r => r.json())
      .then(data => setEnabled(data.totp_enabled || false));
  }, []);

  const setup2FA = async () => {
    setLoading(true);
    setError('');
    try {
      const res = await fetch('/api/auth/totp/setup', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ password }),
        credentials: 'include',
      });
      const data = await res.json();
      if (data.error) { setError(data.error); return; }
      setSetupData(data);
      setEnabled(true);
      setPassword('');
    } catch { setError('Connection failed'); }
    finally { setLoading(false); }
  };

  const disable2FA = async () => {
    setLoading(true);
    setError('');
    try {
      const res = await fetch('/api/auth/totp/disable', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ password }),
        credentials: 'include',
      });
      const data = await res.json();
      if (data.error) { setError(data.error); return; }
      setEnabled(false);
      setSetupData(null);
      setPassword('');
    } catch { setError('Connection failed'); }
    finally { setLoading(false); }
  };

  if (enabled === null) return null;

  return (
    <div class="card" style="padding:16px;margin-bottom:16px">
      <div style="display:flex;justify-content:space-between;align-items:center;margin-bottom:12px">
        <h3>Two-Factor Authentication</h3>
        <span class={`badge ${enabled ? 'badge-green' : 'badge-gray'}`}>
          {enabled ? 'Enabled' : 'Disabled'}
        </span>
      </div>

      {setupData && (
        <div style="margin-bottom:16px;padding:12px;background:var(--bg);border-radius:8px">
          <p style="font-size:13px;color:var(--text-muted);margin-bottom:8px">
            Add this to your authenticator app (Google Authenticator, Authy, etc):
          </p>
          <div class="form-group">
            <label>Secret</label>
            <div style="display:flex;gap:8px;align-items:center">
              <input type="text" value={setupData.secret} readOnly
                style="flex:1;font-family:var(--mono);font-size:12px" />
              <button class="btn-small" onClick={() => navigator.clipboard?.writeText(setupData.secret)}>
                Copy
              </button>
            </div>
          </div>
          <div class="form-group">
            <label>URI (for manual entry)</label>
            <input type="text" value={setupData.uri} readOnly
              style="font-family:var(--mono);font-size:11px;width:100%" />
          </div>
          <p style="font-size:12px;color:var(--warning);margin-top:8px">
            Save this secret — you'll need it if you lose access to your authenticator.
          </p>
        </div>
      )}

      <div>
        <label style="font-size:12px;color:var(--text-muted)">Password required</label>
        <input type="password" class="search-input" placeholder="Enter password"
          value={password} onInput={e => setPassword(e.target.value)}
          style="margin-top:4px;margin-bottom:12px" />
        {enabled ? (
          <button class="btn-danger btn-small" onClick={disable2FA} disabled={loading || !password}>
            {loading ? 'Disabling...' : 'Disable 2FA'}
          </button>
        ) : (
          <button class="btn-primary btn-small" onClick={setup2FA} disabled={loading || !password}>
            {loading ? 'Setting up...' : 'Enable 2FA'}
          </button>
        )}
      </div>

      {error && <div style="color:var(--error);font-size:13px;margin-top:8px">{error}</div>}
    </div>
  );
}

function ConsentGrantsSection() {
  const [grants, setGrants] = useState([]);
  const [loading, setLoading] = useState(true);

  const loadGrants = async () => {
    try {
      const res = await fetch('/api/consent/grants', { credentials: 'include' });
      const data = await res.json();
      setGrants(Array.isArray(data) ? data : []);
    } catch {}
    setLoading(false);
  };

  useEffect(() => { loadGrants(); }, []);

  const revoke = async (id) => {
    try {
      await fetch(`/api/consent/grants/${id}`, {
        method: 'DELETE',
        credentials: 'include',
      });
      setGrants(grants.filter(g => g.ID !== id));
    } catch {}
  };

  return (
    <div class="memory-card" style="margin-bottom:16px">
      <h3 style="margin-bottom:4px">Persistent Consent Grants</h3>
      <p style="font-size:12px;color:var(--text-muted);margin-bottom:12px">
        Tools you've permanently allowed. Revoke any time.
      </p>
      {loading && <div style="color:var(--text-muted);font-size:13px">Loading...</div>}
      {!loading && grants.length === 0 && (
        <div style="color:var(--text-muted);font-size:13px">No persistent grants. When you click "Always allow" on a tool permission prompt, it will appear here.</div>
      )}
      {grants.length > 0 && (
        <table style="width:100%;border-collapse:collapse;font-size:13px">
          <thead>
            <tr style="text-align:left;border-bottom:1px solid var(--border)">
              <th style="padding:6px 8px">Platform</th>
              <th style="padding:6px 8px">Tool Group</th>
              <th style="padding:6px 8px">Granted</th>
              <th style="padding:6px 8px"></th>
            </tr>
          </thead>
          <tbody>
            {grants.map(g => (
              <tr key={g.ID} style="border-bottom:1px solid var(--border)">
                <td style="padding:6px 8px;text-transform:capitalize">{g.Platform}</td>
                <td style="padding:6px 8px;font-family:var(--mono)">{g.ToolGroup.startsWith('mcp:') ? `MCP: ${g.ToolGroup.slice(4)}` : g.ToolGroup}</td>
                <td style="padding:6px 8px;color:var(--text-muted);font-size:12px">{g.GrantedAt ? new Date(g.GrantedAt).toLocaleDateString() : '—'}</td>
                <td style="padding:6px 8px;text-align:right">
                  <button class="btn-secondary" style="padding:3px 10px;font-size:12px;color:var(--error)"
                    onClick={() => revoke(g.ID)}>Revoke</button>
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      )}
    </div>
  );
}

function ImportExportTab() {
  return (
    <div>
      <ConfigSection />
      <TemplatesSection />
    </div>
  );
}

// ── Original Settings sub-components (preserved) ──

function PasswordSection() {
  const [oldPw, setOldPw] = useState('');
  const [newPw, setNewPw] = useState('');
  const [msg, setMsg] = useState('');
  const [error, setError] = useState('');

  const change = async () => {
    setMsg(''); setError('');
    try {
      const res = await fetch('/api/settings/password', {
        method: 'PUT',
        headers: { 'Content-Type': 'application/json' },
        credentials: 'include',
        body: JSON.stringify({ old_password: oldPw, new_password: newPw }),
      });
      const data = await res.json();
      if (data.error) { setError(data.error); return; }
      setMsg('Password changed successfully');
      setOldPw(''); setNewPw('');
    } catch (_) {
      setError('Failed to change password');
    }
  };

  return (
    <div class="memory-card" style="margin-bottom:16px">
      <h3 style="margin-bottom:12px">Change Password</h3>
      <input type="password" class="search-input" placeholder="Current password"
        value={oldPw} onInput={e => setOldPw(e.target.value)} style="margin-bottom:8px" />
      <input type="password" class="search-input" placeholder="New password (min 8 chars)"
        value={newPw} onInput={e => setNewPw(e.target.value)} style="margin-bottom:12px" />
      {error && <div style="color:var(--error);font-size:13px;margin-bottom:8px">{error}</div>}
      {msg && <div style="color:var(--success);font-size:13px;margin-bottom:8px">{msg}</div>}
      <button class="btn-primary" onClick={change}>Change Password</button>
    </div>
  );
}

function CredentialsSection() {
  const [creds, setCreds] = useState([]);
  const [showAdd, setShowAdd] = useState(false);
  const [form, setForm] = useState({ name: '', value: '' });
  const [msg, setMsg] = useState('');

  const load = () => fetch('/api/credentials').then(r => r.json()).then(setCreds).catch(() => {});
  useEffect(load, []);

  const store = async () => {
    await fetch('/api/credentials', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(form),
    });
    setForm({ name: '', value: '' });
    setShowAdd(false);
    setMsg('Credential stored');
    setTimeout(() => setMsg(''), 3000);
    load();
  };

  const del = async (name) => {
    if (!confirm(`Delete credential "${name}"?`)) return;
    await fetch(`/api/credentials/${name}`, { method: 'DELETE' });
    load();
  };

  return (
    <div class="memory-card" style="margin-bottom:16px">
      <div style="display:flex;justify-content:space-between;align-items:center;margin-bottom:12px">
        <h3>Credentials</h3>
        <button class="btn-small" onClick={() => setShowAdd(!showAdd)}>+ Add</button>
      </div>
      <p style="color:var(--text-muted);font-size:13px;margin-bottom:12px">
        Encrypted credentials stored in the database. Values are never displayed.
      </p>

      {msg && <div style="color:var(--success);font-size:13px;margin-bottom:8px">{msg}</div>}

      {showAdd && (
        <div style="margin-bottom:12px;padding:12px;background:var(--bg);border-radius:6px">
          <input type="text" class="search-input" placeholder="Credential name (e.g. GITHUB_TOKEN)"
            value={form.name} onInput={e => setForm({ ...form, name: e.target.value })} style="margin-bottom:8px" />
          <input type="password" class="search-input" placeholder="Value"
            value={form.value} onInput={e => setForm({ ...form, value: e.target.value })} style="margin-bottom:8px" />
          <div style="display:flex;gap:8px">
            <button class="btn-primary" onClick={store} disabled={!form.name || !form.value}>Store</button>
            <button class="btn-secondary" onClick={() => setShowAdd(false)}>Cancel</button>
          </div>
        </div>
      )}

      {creds.length === 0 ? (
        <p style="color:var(--text-muted);font-size:13px">No credentials stored.</p>
      ) : (
        <table class="table" style="margin-top:8px">
          <thead><tr><th scope="col">Name</th><th scope="col">Value</th><th scope="col">Updated</th><th scope="col"><span class="sr-only">Actions</span></th></tr></thead>
          <tbody>
            {creds.map(c => (
              <tr key={c.name}>
                <td><code>{c.name}</code></td>
                <td style="color:var(--text-muted)">{'*'.repeat(12)}</td>
                <td style="font-size:12px;color:var(--text-muted)">{c.updated_at?.slice(0, 19)}</td>
                <td><button class="btn-small btn-danger" onClick={() => del(c.name)}>Delete</button></td>
              </tr>
            ))}
          </tbody>
        </table>
      )}
    </div>
  );
}

function ConfigSection() {
  const [importMsg, setImportMsg] = useState('');
  const [importError, setImportError] = useState('');
  const fileRef = useRef(null);

  const exportConfig = () => window.open('/api/settings/export', '_blank');

  const handleFileSelected = async (e) => {
    const file = e.target.files?.[0];
    if (!file) return;
    setImportMsg(''); setImportError('');
    try {
      const text = await file.text();
      const json = JSON.parse(text);
      const res = await fetch('/api/settings/import', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        credentials: 'include',
        body: JSON.stringify(json),
      });
      const data = await res.json();
      if (data.error) { setImportError(data.error); return; }
      setImportMsg(`Imported: ${data.agents || 0} agents`);
    } catch {
      setImportError('Invalid JSON file');
    }
    fileRef.current.value = '';
  };

  return (
    <div class="memory-card" style="margin-bottom:16px">
      <h3 style="margin-bottom:12px">Configuration</h3>
      <p style="color:var(--text-muted);font-size:13px;margin-bottom:12px">
        Export or import your agent configuration as JSON.
      </p>
      <input type="file" accept=".json" ref={fileRef} onChange={handleFileSelected} style="display:none" />
      <div style="display:flex;gap:8px">
        <button class="btn-primary" onClick={exportConfig}>Export Config</button>
        <button class="btn-secondary" onClick={() => fileRef.current.click()}>Import Config</button>
      </div>
      {importError && <div style="color:var(--error);font-size:13px;margin-top:8px">{importError}</div>}
      {importMsg && <div style="color:var(--success);font-size:13px;margin-top:8px">{importMsg}</div>}
    </div>
  );
}

function TemplatesSection() {
  const [templates, setTemplates] = useState([]);
  const [msg, setMsg] = useState('');

  useEffect(() => {
    fetch('/api/templates').then(r => r.json()).then(setTemplates).catch(() => {});
  }, []);

  const apply = async (name) => {
    const res = await fetch('/api/templates/apply', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ template: name }),
    });
    const data = await res.json();
    setMsg(`Applied "${name}": ${data.files_copied || 0} config files created.`);
    setTimeout(() => setMsg(''), 5000);
  };

  return (
    <div class="memory-card">
      <h3 style="margin-bottom:12px">Templates</h3>
      <p style="color:var(--text-muted);font-size:13px;margin-bottom:12px">
        Apply a template to create pre-configured agent setups.
      </p>

      {msg && <div style="color:var(--success);font-size:13px;margin-bottom:8px">{msg}</div>}

      {templates.length === 0 ? (
        <p style="color:var(--text-muted);font-size:13px">No templates available.</p>
      ) : (
        <div style="display:grid;grid-template-columns:repeat(auto-fill,minmax(250px,1fr));gap:12px">
          {templates.map(t => (
            <div key={t.name} style="padding:12px;background:var(--bg);border-radius:6px;border:1px solid var(--border)">
              <strong>{t.name}</strong>
              {t.description && <p style="color:var(--text-muted);font-size:13px;margin:4px 0">{t.description}</p>}
              <button class="btn-small" onClick={() => apply(t.name)} style="margin-top:8px">Apply</button>
            </div>
          ))}
        </div>
      )}
    </div>
  );
}
