import { h } from 'preact';
import { useState, useEffect } from 'preact/hooks';
import { route } from 'preact-router';
import { rpc } from '../api';

const channelIcons = {
  web: '\u{1F310}', telegram: '\u{2708}', discord: '\u{1F3AE}',
  zalo: '\u{1F4AC}', whatsapp: '\u{1F4F1}', cli: '\u{1F4BB}',
  subagent: '\u{1F916}', cron: '\u{23F0}', mcp: '\u{1F50C}',
};

const kindBadge = (kind) => {
  if (kind === 'subagent') return 'badge-blue';
  if (kind === 'cron') return 'badge-gray';
  return '';
};

const statusBadge = (status) => {
  if (status === 'active') return 'badge-green';
  if (status === 'archived') return 'badge-gray';
  if (status === 'compacted') return 'badge-blue';
  return 'badge-gray';
};

const formatTokens = (n) => {
  if (!n || n === 0) return '-';
  if (n > 1000000) return (n / 1000000).toFixed(1) + 'M';
  if (n > 1000) return (n / 1000).toFixed(1) + 'K';
  return n.toString();
};

export function Sessions() {
  const [sessions, setSessions] = useState([]);
  const [loading, setLoading] = useState(true);
  const [selected, setSelected] = useState(new Set());
  const [agents, setAgents] = useState([]);

  // Filters.
  const [filterAgent, setFilterAgent] = useState('');
  const [filterChannel, setFilterChannel] = useState('');
  const [filterStatus, setFilterStatus] = useState('');

  const load = () => {
    const params = { limit: 100 };
    if (filterAgent) params.agent_id = filterAgent;
    if (filterChannel) params.channel = filterChannel;
    if (filterStatus) params.status = filterStatus;
    rpc('sessions.list', params)
      .then(data => setSessions(data || []))
      .catch(() => {})
      .finally(() => setLoading(false));
  };

  useEffect(() => {
    load();
    // Load agents for filter dropdown.
    fetch('/api/agents').then(r => r.json()).then(data => setAgents(data || [])).catch(() => {});
  }, []);

  // Re-load when filters change.
  useEffect(load, [filterAgent, filterChannel, filterStatus]);

  const toggle = (id, e) => {
    e.stopPropagation();
    const next = new Set(selected);
    if (next.has(id)) next.delete(id); else next.add(id);
    setSelected(next);
  };

  const toggleAll = () => {
    if (selected.size === sessions.length) setSelected(new Set());
    else setSelected(new Set(sessions.map(s => s.id)));
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

  // Unique channels from sessions for filter.
  const channels = [...new Set(sessions.map(s => s.channel))].sort();

  if (loading) return <div class="empty">Loading sessions...</div>;

  return (
    <div>
      <div style="display:flex;justify-content:space-between;align-items:center;margin-bottom:1rem">
        <h1>Sessions</h1>
        {selected.size > 0 && (
          <div style="display:flex;gap:0.5rem">
            <button class="btn-small" onClick={archiveSelected}>Archive ({selected.size})</button>
            <button class="btn-small btn-danger" onClick={deleteSelected}>Delete ({selected.size})</button>
          </div>
        )}
      </div>

      {/* Filter bar */}
      <div style="display:flex;gap:12px;margin-bottom:1rem;align-items:center;flex-wrap:wrap">
        <select value={filterAgent} onChange={e => setFilterAgent(e.target.value)} style="width:160px">
          <option value="">All agents</option>
          {agents.map(a => <option key={a.id} value={a.id}>{a.name || a.id}</option>)}
        </select>
        <select value={filterChannel} onChange={e => setFilterChannel(e.target.value)} style="width:140px">
          <option value="">All channels</option>
          {channels.map(c => <option key={c} value={c}>{c}</option>)}
        </select>
        <select value={filterStatus} onChange={e => setFilterStatus(e.target.value)} style="width:130px">
          <option value="">All status</option>
          <option value="active">Active</option>
          <option value="archived">Archived</option>
          <option value="compacted">Compacted</option>
        </select>
        <span style="color:var(--text-muted);font-size:12px">{sessions.length} sessions</span>
      </div>

      {sessions.length === 0 ? (
        <div class="empty">No sessions match your filters.</div>
      ) : (
        <table class="data-table">
          <thead>
            <tr>
              <th style="width:30px">
                <input type="checkbox" checked={selected.size === sessions.length && sessions.length > 0}
                  onChange={toggleAll} />
              </th>
              <th>Session</th>
              <th>Agent</th>
              <th>Channel</th>
              <th>Messages</th>
              <th>Tokens</th>
              <th>Status</th>
              <th>Updated</th>
              <th></th>
            </tr>
          </thead>
          <tbody>
            {sessions.map(s => (
              <tr key={s.id} onClick={() => route(`/sessions/${s.id}`)} class="clickable">
                <td onClick={e => e.stopPropagation()}>
                  <input type="checkbox" checked={selected.has(s.id)}
                    onChange={e => toggle(s.id, e)} />
                </td>
                <td>
                  <div style="font-weight:600;font-size:13px">{s.label || s.id?.slice(0, 8)}</div>
                  <div style="font-family:var(--mono);font-size:11px;color:var(--text-muted)">{s.id?.slice(0, 8)}</div>
                </td>
                <td>{s.agent_id}</td>
                <td>
                  <span style="margin-right:4px">{channelIcons[s.channel] || ''}</span>
                  {s.channel}
                  {s.kind && s.kind !== 'direct' && (
                    <span class={`badge ${kindBadge(s.kind)}`} style="margin-left:6px">{s.kind}</span>
                  )}
                </td>
                <td style="font-family:var(--mono);font-size:12px">{s.message_count || 0}</td>
                <td style="font-family:var(--mono);font-size:12px;color:var(--text-muted)">
                  {formatTokens((s.input_tokens || 0) + (s.output_tokens || 0))}
                  {s.compaction_count > 0 && (
                    <span style="margin-left:4px;color:var(--warning)" title={`Compacted ${s.compaction_count} time(s)`}>
                      C{s.compaction_count}
                    </span>
                  )}
                </td>
                <td><span class={`badge ${statusBadge(s.status)}`}>{s.status || 'active'}</span></td>
                <td style="color:var(--text-muted);font-size:12px;white-space:nowrap">{s.updated_at?.slice(0, 16)?.replace('T', ' ')}</td>
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
