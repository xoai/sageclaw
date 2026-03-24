import { h } from 'preact';
import { useState, useEffect } from 'preact/hooks';

export default function Audit() {
  const [entries, setEntries] = useState([]);
  const [stats, setStats] = useState(null);
  const [filters, setFilters] = useState({ agent_id: '', tool: '', from: '', to: '' });
  const [expanded, setExpanded] = useState(null);

  const loadStats = () => fetch('/api/audit/stats').then(r => r.json()).then(setStats).catch(() => {});
  const loadEntries = () => {
    const params = new URLSearchParams();
    if (filters.agent_id) params.set('agent_id', filters.agent_id);
    if (filters.tool) params.set('tool', filters.tool);
    if (filters.from) params.set('from', filters.from);
    if (filters.to) params.set('to', filters.to);
    fetch(`/api/audit?${params}`).then(r => r.json()).then(setEntries).catch(() => {});
  };

  useEffect(() => { loadStats(); loadEntries(); }, []);

  const search = () => loadEntries();

  return (
    <div>
      <h1>Audit Log</h1>

      {/* Stats cards */}
      {stats && (
        <div style="display:grid;grid-template-columns:repeat(4,1fr);gap:1rem;margin-bottom:1.5rem">
          <div class="card" style="text-align:center;padding:1rem">
            <div style="font-size:2rem;font-weight:700;color:var(--primary)">{stats.total}</div>
            <div style="color:var(--text-muted);font-size:0.85rem">Total Events</div>
          </div>
          <div class="card" style="text-align:center;padding:1rem">
            <div style="font-size:2rem;font-weight:700;color:var(--primary)">{stats.unique_tools}</div>
            <div style="color:var(--text-muted);font-size:0.85rem">Unique Tools</div>
          </div>
          <div class="card" style="text-align:center;padding:1rem">
            <div style={`font-size:2rem;font-weight:700;color:${stats.error_rate > 5 ? 'var(--error)' : 'var(--success)'}`}>{stats.error_rate?.toFixed(1)}%</div>
            <div style="color:var(--text-muted);font-size:0.85rem">Error Rate</div>
          </div>
          <div class="card" style="text-align:center;padding:1rem">
            <div style="font-size:1.1rem;font-weight:700;color:var(--primary);word-break:break-all">{stats.most_used || '-'}</div>
            <div style="color:var(--text-muted);font-size:0.85rem">Most Used</div>
          </div>
        </div>
      )}

      {/* Filters */}
      <div class="card" style="padding:16px;margin-bottom:1.5rem">
        <div class="audit-filters" style="display:grid;grid-template-columns:repeat(4,1fr) auto;gap:12px;align-items:end">
          <div class="form-group">
            <label>Agent ID</label>
            <input type="text" placeholder="Filter by agent..." value={filters.agent_id}
              onInput={e => setFilters({ ...filters, agent_id: e.target.value })} />
          </div>
          <div class="form-group">
            <label>Tool</label>
            <input type="text" placeholder="Filter by tool..." value={filters.tool}
              onInput={e => setFilters({ ...filters, tool: e.target.value })} />
          </div>
          <div class="form-group">
            <label>From</label>
            <input type="datetime-local" value={filters.from}
              onInput={e => setFilters({ ...filters, from: e.target.value })} />
          </div>
          <div class="form-group">
            <label>To</label>
            <input type="datetime-local" value={filters.to}
              onInput={e => setFilters({ ...filters, to: e.target.value })} />
          </div>
          <button class="btn-primary" onClick={search} style="height:38px;margin-bottom:12px">Search</button>
        </div>
      </div>

      {/* Results table */}
      <table class="data-table">
        <thead>
          <tr><th scope="col">Time</th><th scope="col">Agent</th><th scope="col">Event</th><th scope="col">Details</th></tr>
        </thead>
        <tbody>
          {entries.map(e => (
            <tr key={e.id} class="clickable" onClick={() => setExpanded(expanded === e.id ? null : e.id)}>
              <td style="white-space:nowrap">{e.created_at}</td>
              <td>{e.agent_id}</td>
              <td><code>{e.event_type}</code></td>
              <td>
                {expanded === e.id
                  ? <pre style="white-space:pre-wrap;max-width:500px;font-size:0.8rem">{e.payload}</pre>
                  : (e.payload?.length > 80 ? e.payload.slice(0, 80) + '...' : e.payload)
                }
              </td>
            </tr>
          ))}
          {entries.length === 0 && (
            <tr><td colspan="4" style="text-align:center;color:var(--text-muted)">No audit events yet. Agent activity will appear here automatically.</td></tr>
          )}
        </tbody>
      </table>
    </div>
  );
}
