import { h } from 'preact';
import { useState, useEffect } from 'preact/hooks';

export default function Tools() {
  const [tools, setTools] = useState([]);
  const [search, setSearch] = useState('');
  const [expanded, setExpanded] = useState(null);
  const [filterCat, setFilterCat] = useState('all');

  useEffect(() => {
    fetch('/api/tools').then(r => r.json()).then(setTools).catch(() => {});
  }, []);

  const categories = ['all', ...new Set(tools.map(t => t.category))];

  const filtered = tools.filter(t => {
    if (filterCat !== 'all' && t.category !== filterCat) return false;
    if (search && !t.name.toLowerCase().includes(search.toLowerCase()) &&
        !t.description?.toLowerCase().includes(search.toLowerCase())) return false;
    return true;
  });

  return (
    <div>
      <h1>Tool Registry</h1>
      <p style="color:var(--text-muted);margin-bottom:1.5rem">{tools.length} tools registered</p>

      <div style="display:flex;gap:12px;margin-bottom:1.5rem;align-items:center">
        <input type="text" class="search-input" placeholder="Search tools..." value={search}
          onInput={e => setSearch(e.target.value)} style="flex:1;margin-bottom:0" />
        <select value={filterCat} onChange={e => setFilterCat(e.target.value)} style="width:180px;flex-shrink:0">
          {categories.map(c => <option value={c}>{c === 'all' ? 'All categories' : c}</option>)}
        </select>
      </div>

      <div class="card-list">
        {filtered.map(t => (
          <div key={t.name} class="card clickable" onClick={() => setExpanded(expanded === t.name ? null : t.name)}
            style="padding:1rem;cursor:pointer">
            <div style="display:flex;justify-content:space-between;align-items:center">
              <div>
                <code style="font-size:1rem;color:var(--primary)">{t.name}</code>
                <span class="badge badge-gray" style="margin-left:0.75rem">{t.category}</span>
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
        ))}
      </div>

      {filtered.length === 0 && (
        <p style="text-align:center;color:var(--text-muted);margin-top:2rem">No tools match your search.</p>
      )}
    </div>
  );
}
