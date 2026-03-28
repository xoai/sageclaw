import { h } from 'preact';
import { useState, useEffect, useRef } from 'preact/hooks';
import { Providers } from './Providers';
import { Channels } from './Channels';
import Cron from './Cron';
import Tunnel from './Tunnel';

const TABS = [
  { id: 'general', label: 'General' },
  { id: 'ai-models', label: 'AI Models' },
  { id: 'channels', label: 'Channels' },
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

  const changeTheme = (next) => {
    setTheme(next);
    localStorage.setItem('sageclaw-theme', next);
    document.documentElement.setAttribute('data-theme', next);
  };

  return (
    <div>
      <h3 style="margin-bottom:16px">Appearance</h3>
      <div class="form-group">
        <label>Theme</label>
        <div style="display:flex;gap:8px">
          <button class={theme === 'dark' ? 'btn-primary' : 'btn-secondary'} onClick={() => changeTheme('dark')}>Dark</button>
          <button class={theme === 'light' ? 'btn-primary' : 'btn-secondary'} onClick={() => changeTheme('light')}>Light</button>
        </div>
      </div>

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
      <CredentialsSection />
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
