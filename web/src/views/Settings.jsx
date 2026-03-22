import { h } from 'preact';
import { useState, useEffect, useRef } from 'preact/hooks';

export function Settings() {
  return (
    <div>
      <h1>Settings</h1>
      <PasswordSection />
      <ConfigSection />
      <CredentialsSection />
      <TemplatesSection />
    </div>
  );
}

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
      <button class="chat-send" onClick={change}>Change Password</button>
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
        <button class="chat-send" onClick={exportConfig}>Export Config</button>
        <button class="chat-send" onClick={() => fileRef.current.click()}
          style="background:var(--surface);border:1px solid var(--border);color:var(--text)">Import Config</button>
      </div>
      {importError && <div style="color:var(--error);font-size:13px;margin-top:8px">{importError}</div>}
      {importMsg && <div style="color:var(--success);font-size:13px;margin-top:8px">{importMsg}</div>}
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
          <thead><tr><th>Name</th><th>Value</th><th>Updated</th><th></th></tr></thead>
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
