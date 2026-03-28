import { h } from 'preact';
import { useState, useEffect } from 'preact/hooks';

const riskColors = {
  safe: 'badge-green',
  moderate: 'badge-yellow',
  sensitive: 'badge-red',
};

export default function Tools() {
  const [tools, setTools] = useState([]);
  const [search, setSearch] = useState('');
  const [expanded, setExpanded] = useState(null);
  const [filterCat, setFilterCat] = useState('all');
  const [filterRisk, setFilterRisk] = useState('all');
  const [viewMode, setViewMode] = useState('list'); // 'list' or 'groups'

  useEffect(() => {
    fetch('/api/tools', { credentials: 'include' }).then(r => r.json()).then(setTools).catch(() => {});
  }, []);

  const categories = ['all', ...new Set(tools.map(t => t.category).filter(Boolean))].sort();

  const filtered = tools.filter(t => {
    if (filterCat !== 'all' && t.category !== filterCat) return false;
    if (filterRisk !== 'all' && t.risk !== filterRisk) return false;
    if (search && !t.name.toLowerCase().includes(search.toLowerCase()) &&
        !t.description?.toLowerCase().includes(search.toLowerCase())) return false;
    return true;
  });

  // Group tools by category for group view.
  const grouped = {};
  filtered.forEach(t => {
    const cat = t.category || 'other';
    if (!grouped[cat]) grouped[cat] = [];
    grouped[cat].push(t);
  });

  const sortedGroups = Object.keys(grouped).sort();

  return (
    <div>
      <div style="display:flex;justify-content:space-between;align-items:center;margin-bottom:0.5rem">
        <span style="font-size:14px;font-weight:600">Tool Registry</span>
        <div style="display:flex;gap:4px">
          <button class={`btn-small ${viewMode === 'list' ? 'btn-primary' : 'btn-secondary'}`}
            onClick={() => setViewMode('list')}>List</button>
          <button class={`btn-small ${viewMode === 'groups' ? 'btn-primary' : 'btn-secondary'}`}
            onClick={() => setViewMode('groups')}>Groups</button>
        </div>
      </div>
      <p style="color:var(--text-muted);margin-bottom:1.5rem">{tools.length} tools registered</p>

      <div style="display:flex;gap:12px;margin-bottom:1.5rem;align-items:center">
        <input type="text" class="search-input" placeholder="Search tools..." value={search}
          onInput={e => setSearch(e.target.value)} style="flex:1;margin-bottom:0" />
        <select value={filterCat} onChange={e => setFilterCat(e.target.value)} style="width:160px;flex-shrink:0">
          {categories.map(c => <option value={c}>{c === 'all' ? 'All groups' : c}</option>)}
        </select>
        <select value={filterRisk} onChange={e => setFilterRisk(e.target.value)} style="width:140px;flex-shrink:0">
          <option value="all">All risks</option>
          <option value="safe">Safe</option>
          <option value="moderate">Moderate</option>
          <option value="sensitive">Sensitive</option>
        </select>
      </div>

      {viewMode === 'groups' ? (
        <div>
          {sortedGroups.map(group => (
            <div key={group} style="margin-bottom:1.5rem">
              <div style="display:flex;align-items:center;gap:8px;margin-bottom:8px">
                <h3 style="margin:0;text-transform:capitalize">{group}</h3>
                <span class="badge badge-gray">{grouped[group].length}</span>
                {grouped[group][0]?.risk && (
                  <span class={`badge ${riskColors[grouped[group][0].risk] || 'badge-gray'}`}>
                    {grouped[group][0].risk}
                  </span>
                )}
              </div>
              <div class="card-list">
                {grouped[group].map(t => (
                  <ToolCard key={t.name} tool={t} expanded={expanded} onToggle={setExpanded} />
                ))}
              </div>
            </div>
          ))}
        </div>
      ) : (
        <div class="card-list">
          {filtered.map(t => (
            <ToolCard key={t.name} tool={t} expanded={expanded} onToggle={setExpanded} />
          ))}
        </div>
      )}

      {filtered.length === 0 && (
        <p style="text-align:center;color:var(--text-muted);margin-top:2rem">No tools match your search. Try different keywords.</p>
      )}
    </div>
  );
}

function ToolCard({ tool: t, expanded, onToggle }) {
  return (
    <div class="card clickable" onClick={() => onToggle(expanded === t.name ? null : t.name)}
      style="padding:1rem;cursor:pointer">
      <div style="display:flex;justify-content:space-between;align-items:center">
        <div style="display:flex;align-items:center;gap:8px">
          <code style="font-size:1rem;color:var(--primary)">{t.name}</code>
          <span class="badge badge-gray">{t.category}</span>
          {t.risk && <span class={`badge ${riskColors[t.risk] || 'badge-gray'}`}>{t.risk}</span>}
          {t.source && t.source !== 'builtin' && (
            <span class="badge badge-blue" style="font-size:0.7rem">{t.source}</span>
          )}
        </div>
      </div>
      <div style="color:var(--text-muted);font-size:0.9rem;margin-top:0.5rem">{t.description}</div>

      {expanded === t.name && t.schema && (
        <div style="margin-top:1rem;background:var(--bg);padding:1rem;border-radius:6px">
          <div style="font-size:0.8rem;color:var(--text-muted);margin-bottom:0.5rem">Input Schema:</div>
          <pre style="white-space:pre-wrap;font-size:0.8rem;color:var(--text)">
            {JSON.stringify(t.schema, null, 2)}
          </pre>
        </div>
      )}
    </div>
  );
}
