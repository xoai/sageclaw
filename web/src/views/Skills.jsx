import { h } from 'preact';
import { useState, useEffect } from 'preact/hooks';

export function Skills() {
  const [skills, setSkills] = useState([]);
  const [showInstall, setShowInstall] = useState(false);
  const [url, setUrl] = useState('');
  const [installing, setInstalling] = useState(false);
  const [msg, setMsg] = useState('');

  const load = () => fetch('/api/skills').then(r => r.json()).then(data => setSkills(data || [])).catch(() => {});
  useEffect(load, []);

  const install = async () => {
    setInstalling(true);
    setMsg('');
    try {
      const res = await fetch('/api/skills/install', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ url }),
      });
      const data = await res.json();
      if (res.ok) {
        setMsg('Installed successfully. Click Reload to activate.');
        setUrl('');
        setShowInstall(false);
        load();
      } else {
        setMsg(data.error || 'Install failed');
      }
    } catch {
      setMsg('Install failed');
    }
    setInstalling(false);
  };

  const uninstall = async (name) => {
    if (!confirm(`Uninstall skill "${name}"?`)) return;
    await fetch(`/api/skills/${name}`, { method: 'DELETE' });
    load();
  };

  const reload = async () => {
    await fetch('/api/skills/reload', { method: 'POST' });
    setMsg('Reload requested. Skills will be refreshed.');
    setTimeout(() => setMsg(''), 3000);
    load();
  };

  return (
    <div>
      <div style="display:flex;justify-content:space-between;align-items:center;margin-bottom:1.5rem">
        <h1>Skills</h1>
        <div style="display:flex;gap:0.5rem">
          <button class="btn-secondary" onClick={reload}>Reload</button>
          <button class="btn-primary" onClick={() => setShowInstall(true)}>+ Install Skill</button>
        </div>
      </div>

      {msg && <div class="card" style="padding:0.75rem;margin-bottom:1rem;color:var(--success)">{msg}</div>}

      {skills.length === 0 ? (
        <p style="color:var(--text-muted);text-align:center;margin-top:3rem">
          No skills installed yet. Skills extend what your agents can do — install one from a git repository.
        </p>
      ) : (
        <div class="card-list">
          {skills.map(s => (
            <div class="card" key={s.name} style="padding:1rem">
              <div style="display:flex;justify-content:space-between;align-items:center">
                <div>
                  <strong style="font-size:1.1rem">{s.name}</strong>
                  <span class="badge badge-gray" style="margin-left:0.75rem">{s.tools} tools</span>
                  {s.has_skillmd && <span class="badge badge-green" style="margin-left:0.5rem">SKILL.md</span>}
                </div>
                <button class="btn-small btn-danger" onClick={() => uninstall(s.name)}>Uninstall</button>
              </div>
            </div>
          ))}
        </div>
      )}

      {showInstall && (
        <div class="modal-overlay" onClick={() => setShowInstall(false)} role="dialog" aria-modal="true" aria-labelledby="install-skill-title">
          <div class="modal-content" onClick={e => e.stopPropagation()}>
            <h2 id="install-skill-title">Install Skill</h2>
            <div class="form-group">
              <label>Git Repository URL</label>
              <input type="text" placeholder="https://github.com/user/skill-name"
                value={url} onInput={e => setUrl(e.target.value)} />
            </div>
            <div style="display:flex;gap:0.5rem;margin-top:1rem">
              <button class="btn-primary" onClick={install} disabled={!url || installing}>
                {installing ? 'Installing...' : 'Install'}
              </button>
              <button class="btn-secondary" onClick={() => setShowInstall(false)}>Cancel</button>
            </div>
          </div>
        </div>
      )}
    </div>
  );
}
