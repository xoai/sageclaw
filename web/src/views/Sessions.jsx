import { h } from 'preact';
import { useState, useEffect } from 'preact/hooks';
import { route } from 'preact-router';
import { rpc } from '../api';

export function Sessions() {
  const [sessions, setSessions] = useState([]);
  const [loading, setLoading] = useState(true);
  const [selected, setSelected] = useState(new Set());

  const load = () => {
    rpc('sessions.list', { limit: 50 })
      .then(data => setSessions(data || []))
      .catch(() => {})
      .finally(() => setLoading(false));
  };

  useEffect(load, []);

  const toggle = (id, e) => {
    e.stopPropagation();
    const next = new Set(selected);
    if (next.has(id)) next.delete(id); else next.add(id);
    setSelected(next);
  };

  const toggleAll = () => {
    if (selected.size === sessions.length) {
      setSelected(new Set());
    } else {
      setSelected(new Set(sessions.map(s => s.id)));
    }
  };

  const deleteSelected = async () => {
    if (!confirm(`Delete ${selected.size} session(s)?`)) return;
    for (const id of selected) {
      await fetch(`/api/sessions/${id}`, { method: 'DELETE' });
    }
    setSelected(new Set());
    load();
  };

  const archiveSelected = async () => {
    for (const id of selected) {
      await fetch(`/api/sessions/${id}/archive`, { method: 'POST' });
    }
    setSelected(new Set());
    load();
  };

  if (loading) return <div class="empty">Loading sessions...</div>;

  return (
    <div>
      <div style="display:flex;justify-content:space-between;align-items:center;margin-bottom:1.5rem">
        <h1>Sessions</h1>
        {selected.size > 0 && (
          <div style="display:flex;gap:0.5rem">
            <button class="btn-small" onClick={archiveSelected}>Archive ({selected.size})</button>
            <button class="btn-small btn-danger" onClick={deleteSelected}>Delete ({selected.size})</button>
          </div>
        )}
      </div>

      {sessions.length === 0 ? (
        <div class="empty">No sessions yet. Start a conversation to see it here.</div>
      ) : (
        <table class="table">
          <thead>
            <tr>
              <th style="width:30px">
                <input type="checkbox" checked={selected.size === sessions.length}
                  onChange={toggleAll} />
              </th>
              <th>ID</th>
              <th>Channel</th>
              <th>Chat</th>
              <th>Agent</th>
              <th>Updated</th>
              <th>Actions</th>
            </tr>
          </thead>
          <tbody>
            {sessions.map(s => (
              <tr key={s.id} onClick={() => route(`/sessions/${s.id}`)} style="cursor:pointer">
                <td onClick={e => e.stopPropagation()}>
                  <input type="checkbox" checked={selected.has(s.id)}
                    onChange={e => toggle(s.id, e)} />
                </td>
                <td style="font-family:var(--mono);font-size:12px">{s.id?.slice(0, 8)}</td>
                <td>{s.channel}</td>
                <td>{s.chat_id?.slice(0, 12)}</td>
                <td>{s.agent_id}</td>
                <td style="color:var(--text-muted)">{s.updated_at?.slice(0, 19)}</td>
                <td onClick={e => e.stopPropagation()}>
                  <button class="btn-small btn-danger" onClick={async () => {
                    if (!confirm('Delete this session?')) return;
                    await fetch(`/api/sessions/${s.id}`, { method: 'DELETE' });
                    load();
                  }}>Delete</button>
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      )}
    </div>
  );
}
