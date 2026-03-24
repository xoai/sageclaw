import { h } from 'preact';
import { useState, useEffect } from 'preact/hooks';

export default function Delegation() {
  const [links, setLinks] = useState([]);
  const [history, setHistory] = useState([]);
  const [tab, setTab] = useState('links');
  const [showModal, setShowModal] = useState(false);
  const [form, setForm] = useState({ source: '', target: '', direction: 'sync', max_concurrent: 1 });

  const loadLinks = () => fetch('/api/delegation/links').then(r => r.json()).then(setLinks).catch(() => {});
  const loadHistory = () => fetch('/api/delegation/history').then(r => r.json()).then(setHistory).catch(() => {});

  useEffect(() => { loadLinks(); loadHistory(); }, []);

  const createLink = async () => {
    await fetch('/api/delegation/links', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(form),
    });
    setShowModal(false);
    setForm({ source: '', target: '', direction: 'sync', max_concurrent: 1 });
    loadLinks();
  };

  const deleteLink = async (id) => {
    if (!confirm('Delete this delegation link?')) return;
    await fetch(`/api/delegation/links/${id}`, { method: 'DELETE' });
    loadLinks();
  };

  const statusColor = (s) => {
    if (s === 'completed') return 'badge-green';
    if (s === 'running') return 'badge-blue';
    if (s === 'failed') return 'badge-red';
    return 'badge-gray';
  };

  return (
    <div>
      <h1>Delegation</h1>
      <div class="tab-bar">
        <button class={tab === 'links' ? 'tab-active' : ''} onClick={() => setTab('links')}>Links</button>
        <button class={tab === 'history' ? 'tab-active' : ''} onClick={() => setTab('history')}>History</button>
      </div>

      {tab === 'links' && (
        <div>
          <div style="display:flex;justify-content:flex-end;margin-bottom:1rem">
            <button class="btn-primary" onClick={() => setShowModal(true)}>+ Add Link</button>
          </div>
          {links.length === 0 ? (
            <p style="color:var(--text-muted);text-align:center">No delegation links yet. Link agents together to enable task handoff.</p>
          ) : (
            <div class="card-list">
              {links.map(l => (
                <div class="card" key={l.id}>
                  <div style="display:flex;justify-content:space-between;align-items:center">
                    <div>
                      <strong>{l.source_id}</strong>
                      <span style="margin:0 0.5rem;color:var(--text-muted)">{l.direction === 'async' ? '⇢' : '→'}</span>
                      <strong>{l.target_id}</strong>
                    </div>
                    <button class="btn-small btn-danger" onClick={() => deleteLink(l.id)}>Delete</button>
                  </div>
                  <div style="margin-top:0.5rem;font-size:0.85rem;color:var(--text-muted)">
                    {l.direction} | Max: {l.max_concurrent} | Active: {l.active_count}
                  </div>
                </div>
              ))}
            </div>
          )}
        </div>
      )}

      {tab === 'history' && (
        <table class="data-table">
          <thead>
            <tr><th scope="col">Source</th><th scope="col">Target</th><th scope="col">Status</th><th scope="col">Prompt</th><th scope="col">Started</th></tr>
          </thead>
          <tbody>
            {history.map(h => (
              <tr key={h.id}>
                <td>{h.source_id}</td>
                <td>{h.target_id}</td>
                <td><span class={`badge ${statusColor(h.status)}`}>{h.status}</span></td>
                <td>{h.prompt.length > 50 ? h.prompt.slice(0, 50) + '...' : h.prompt}</td>
                <td>{h.started_at}</td>
              </tr>
            ))}
            {history.length === 0 && <tr><td colspan="5" style="text-align:center;color:var(--text-muted)">No delegation history.</td></tr>}
          </tbody>
        </table>
      )}

      {showModal && (
        <div class="modal-overlay" onClick={() => setShowModal(false)} role="dialog" aria-modal="true" aria-labelledby="delegation-title">
          <div class="modal-content" onClick={e => e.stopPropagation()}>
            <h2 id="delegation-title">New Delegation Link</h2>
            <div class="form-group">
              <label>Source Agent ID</label>
              <input type="text" value={form.source} onInput={e => setForm({ ...form, source: e.target.value })} />
            </div>
            <div class="form-group">
              <label>Target Agent ID</label>
              <input type="text" value={form.target} onInput={e => setForm({ ...form, target: e.target.value })} />
            </div>
            <div class="form-group">
              <label>Direction</label>
              <select value={form.direction} onChange={e => setForm({ ...form, direction: e.target.value })}>
                <option value="sync">Sync</option>
                <option value="async">Async</option>
              </select>
            </div>
            <div class="form-group">
              <label>Max Concurrent</label>
              <input type="number" min="1" value={form.max_concurrent}
                onInput={e => setForm({ ...form, max_concurrent: parseInt(e.target.value) || 1 })} />
            </div>
            <div style="display:flex;gap:0.5rem;margin-top:1rem">
              <button class="btn-primary" onClick={createLink} disabled={!form.source || !form.target}>Create</button>
              <button class="btn-secondary" onClick={() => setShowModal(false)}>Cancel</button>
            </div>
          </div>
        </div>
      )}
    </div>
  );
}
