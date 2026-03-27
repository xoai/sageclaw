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
  if (kind === 'dm') return 'badge-blue';
  if (kind === 'group') return 'badge-green';
  if (kind === 'subagent') return 'badge-purple';
  if (kind === 'cron') return 'badge-gray';
  return '';
};

const kindLabel = (kind) => {
  if (kind === 'dm') return 'DM';
  if (kind === 'group') return 'Group';
  return kind;
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
  const [filterKind, setFilterKind] = useState('');

  const load = () => {
    const params = { limit: 100 };
    if (filterAgent) params.agent_id = filterAgent;
    if (filterChannel) params.channel = filterChannel;
    if (filterStatus) params.status = filterStatus;
    if (filterKind) params.kind = filterKind;
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
  useEffect(load, [filterAgent, filterChannel, filterStatus, filterKind]);

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
    if (!confirm(`Delete ${selected.size} session(s)? This removes all conversation history and cannot be undone.`)) return;
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

  if (loading) return <div class="empty" role="status">Loading sessions...</div>;

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
        <select value={filterKind} onChange={e => setFilterKind(e.target.value)} style="width:110px">
          <option value="">All kinds</option>
          <option value="dm">DM</option>
          <option value="group">Group</option>
          <option value="subagent">Subagent</option>
          <option value="cron">Cron</option>
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
              <th scope="col" style="width:30px">
                <input type="checkbox" checked={selected.size === sessions.length && sessions.length > 0}
                  onChange={toggleAll} aria-label="Select all sessions" />
              </th>
              <th scope="col">Session</th>
              <th scope="col">Agent</th>
              <th scope="col">Channel</th>
              <th scope="col">Messages</th>
              <th scope="col">Tokens</th>
              <th scope="col">Status</th>
              <th scope="col">Updated</th>
              <th scope="col"><span class="sr-only">Actions</span></th>
            </tr>
          </thead>
          <tbody>
            {sessions.map(s => (
              <tr key={s.id} onClick={() => route(`/sessions/${s.id}`)} class="clickable" tabIndex={0}
                onKeyDown={(e) => { if (e.key === 'Enter') route(`/sessions/${s.id}`); }}>
                <td onClick={e => e.stopPropagation()}>
                  <input type="checkbox" checked={selected.has(s.id)}
                    onChange={e => toggle(s.id, e)} aria-label={`Select session ${s.label || s.id?.slice(0, 8)}`} />
                </td>
                <td>
                  <div style="font-weight:600;font-size:13px">{s.title || s.label || s.id?.slice(0, 8)}</div>
                  <div style="font-family:var(--mono);font-size:11px;color:var(--text-muted)" title={s.id}>
                    {s.agent_id} on {s.channel} &middot; {s.id?.slice(0, 8)}
                  </div>
                </td>
                <td>{s.agent_name || s.agent_id}</td>
                <td>
                  <span style="margin-right:4px">{channelIcons[s.channel] || ''}</span>
                  {s.channel}
                  {s.kind && (
                    <span class={`badge ${kindBadge(s.kind)}`} style="margin-left:6px">{kindLabel(s.kind)}</span>
                  )}
                  {s.thread_id && (
                    <span class="badge badge-gray" style="margin-left:4px" title={`Thread ${s.thread_id}`}>Thread</span>
                  )}
                  {s.spawned_by && !s.thread_id && s.kind !== 'subagent' && s.kind !== 'cron' && (
                    <span style="font-size:10px;color:var(--text-muted);margin-left:4px" title={`Parent: ${s.spawned_by}`}>&#8627;</span>
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
                    if (!confirm('Delete this session? All messages and history will be removed.')) return;
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
