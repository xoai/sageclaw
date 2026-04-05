import { h } from 'preact';
import { useState, useEffect, useRef } from 'preact/hooks';

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
  const [viewMode, setViewMode] = useState('list');

  const loadTools = () => {
    fetch('/api/tools', { credentials: 'include' }).then(r => r.json()).then(setTools).catch(() => {});
  };

  useEffect(() => { loadTools(); }, []);

  const categories = ['all', ...new Set(tools.map(t => t.category).filter(Boolean))].sort();

  const filtered = tools.filter(t => {
    if (filterCat !== 'all' && t.category !== filterCat) return false;
    if (filterRisk !== 'all' && t.risk !== filterRisk) return false;
    if (search && !t.name.toLowerCase().includes(search.toLowerCase()) &&
        !t.description?.toLowerCase().includes(search.toLowerCase())) return false;
    return true;
  });

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
                  <ToolCard key={t.name} tool={t} expanded={expanded} onToggle={setExpanded} onSave={loadTools} />
                ))}
              </div>
            </div>
          ))}
        </div>
      ) : (
        <div class="card-list">
          {filtered.map(t => (
            <ToolCard key={t.name} tool={t} expanded={expanded} onToggle={setExpanded} onSave={loadTools} />
          ))}
        </div>
      )}

      {filtered.length === 0 && (
        <p style="text-align:center;color:var(--text-muted);margin-top:2rem">No tools match your search. Try different keywords.</p>
      )}
    </div>
  );
}

function ToolCard({ tool: t, expanded, onToggle, onSave }) {
  const isExpanded = expanded === t.name;
  const [editing, setEditing] = useState(false);
  const [desc, setDesc] = useState(t.description || '');
  const [schema, setSchema] = useState('');
  const [saving, setSaving] = useState(false);
  const [schemaError, setSchemaError] = useState('');
  const [showConfig, setShowConfig] = useState(false);
  const descRef = useRef(null);

  const startEdit = (e) => {
    e.stopPropagation();
    setDesc(t.description || '');
    setSchema(JSON.stringify(t.schema, null, 2));
    setSchemaError('');
    setEditing(true);
    setShowConfig(false);
    if (!isExpanded) onToggle(t.name);
  };

  const cancelEdit = (e) => {
    e.stopPropagation();
    setEditing(false);
    setSchemaError('');
  };

  const save = async (e) => {
    e.stopPropagation();
    let parsedSchema;
    try {
      parsedSchema = JSON.parse(schema);
    } catch {
      setSchemaError('Invalid JSON');
      return;
    }
    setSaving(true);
    try {
      const res = await fetch(`/api/tools/${encodeURIComponent(t.name)}`, {
        method: 'PUT',
        headers: { 'Content-Type': 'application/json' },
        credentials: 'include',
        body: JSON.stringify({ description: desc, schema: parsedSchema }),
      });
      if (res.ok) {
        setEditing(false);
        onSave();
      }
    } catch {}
    setSaving(false);
  };

  return (
    <div class="card clickable" onClick={() => { if (!editing && !showConfig) onToggle(isExpanded ? null : t.name); }}
      style="padding:1rem;cursor:pointer">
      <div style="display:flex;justify-content:space-between;align-items:center">
        <div style="display:flex;align-items:center;gap:8px">
          <code style="font-size:1rem;color:var(--primary)">{t.name}</code>
          <span class="badge badge-gray">{t.category}</span>
          {t.risk && <span class={`badge ${riskColors[t.risk] || 'badge-gray'}`}>{t.risk}</span>}
          {t.source && t.source !== 'builtin' && (
            <span class="badge badge-blue" style="font-size:0.7rem">{t.source}</span>
          )}
          {t.has_config && <span class="badge badge-purple" style="font-size:0.65rem">configurable</span>}
        </div>
        {isExpanded && !editing && !showConfig && (
          <div style="display:flex;gap:6px">
            {t.has_config && (
              <button class="btn-small btn-primary" onClick={(e) => { e.stopPropagation(); setShowConfig(true); }} style="font-size:0.75rem">Configure</button>
            )}
            <button class="btn-small btn-secondary" onClick={startEdit} style="font-size:0.75rem">Edit</button>
          </div>
        )}
      </div>

      {!editing && !showConfig && (
        <div style="color:var(--text-muted);font-size:0.9rem;margin-top:0.5rem">{t.description}</div>
      )}

      {isExpanded && !editing && !showConfig && t.schema && (
        <div style="margin-top:1rem;background:var(--bg);padding:1rem;border-radius:6px">
          <div style="font-size:0.8rem;color:var(--text-muted);margin-bottom:0.5rem">Input Schema:</div>
          <pre style="white-space:pre-wrap;font-size:0.8rem;color:var(--text)">
            {JSON.stringify(t.schema, null, 2)}
          </pre>
        </div>
      )}

      {showConfig && (
        <div style="margin-top:0.75rem" onClick={e => e.stopPropagation()}>
          <ToolConfigForm toolName={t.name} onClose={() => setShowConfig(false)} />
        </div>
      )}

      {editing && (
        <div style="margin-top:0.75rem" onClick={e => e.stopPropagation()}>
          <div class="form-group" style="margin-bottom:0.75rem">
            <label style="font-size:0.8rem;color:var(--text-muted);margin-bottom:4px;display:block">Description (visible to LLM)</label>
            <textarea ref={descRef} value={desc} onInput={e => setDesc(e.target.value)}
              rows={2} style="width:100%;resize:vertical;font-size:0.85rem" />
          </div>
          <div class="form-group" style="margin-bottom:0.75rem">
            <label style="font-size:0.8rem;color:var(--text-muted);margin-bottom:4px;display:block">Input Schema (JSON)</label>
            <textarea value={schema} onInput={e => { setSchema(e.target.value); setSchemaError(''); }}
              rows={12} style="width:100%;resize:vertical;font-family:var(--mono);font-size:0.8rem" />
            {schemaError && <div style="color:var(--danger);font-size:0.75rem;margin-top:4px">{schemaError}</div>}
          </div>
          <div style="display:flex;gap:8px;justify-content:flex-end">
            <button class="btn-small btn-secondary" onClick={cancelEdit}>Cancel</button>
            <button class="btn-small btn-primary" onClick={save} disabled={saving}>
              {saving ? 'Saving...' : 'Save'}
            </button>
          </div>
        </div>
      )}
    </div>
  );
}

function ToolConfigForm({ toolName, onClose }) {
  const [configData, setConfigData] = useState(null);
  const [values, setValues] = useState({});
  const [saving, setSaving] = useState(false);
  const [saved, setSaved] = useState(false);
  const [error, setError] = useState('');

  useEffect(() => {
    fetch(`/api/tools/${encodeURIComponent(toolName)}/config`, { credentials: 'include' })
      .then(r => r.json())
      .then(data => {
        setConfigData(data);
        setValues(data.values || {});
      })
      .catch(() => setError('Failed to load config'));
  }, [toolName]);

  const handleSave = async () => {
    setSaving(true);
    setError('');
    setSaved(false);
    try {
      const res = await fetch(`/api/tools/${encodeURIComponent(toolName)}/config`, {
        method: 'PUT',
        headers: { 'Content-Type': 'application/json' },
        credentials: 'include',
        body: JSON.stringify(values),
      });
      if (res.ok) {
        setSaved(true);
        setTimeout(() => setSaved(false), 2000);
      } else {
        const err = await res.json();
        setError(err.error || 'Save failed');
      }
    } catch {
      setError('Save failed');
    }
    setSaving(false);
  };

  if (!configData) {
    return <div style="padding:0.5rem;color:var(--text-muted);font-size:0.85rem">{error || 'Loading...'}</div>;
  }

  const schema = configData.schema || {};
  const fieldNames = Object.keys(schema).sort((a, b) => {
    // Required fields first.
    if (schema[a].required !== schema[b].required) return schema[a].required ? -1 : 1;
    return a.localeCompare(b);
  });

  return (
    <div style="background:var(--bg);border-radius:6px;padding:1rem">
      <div style="display:flex;justify-content:space-between;align-items:center;margin-bottom:1rem">
        <span style="font-size:0.9rem;font-weight:600">Configuration</span>
        <button class="btn-small btn-secondary" onClick={onClose} style="font-size:0.75rem">Close</button>
      </div>

      {fieldNames.map(fieldName => {
        const field = schema[fieldName];
        return (
          <div key={fieldName} style="margin-bottom:1rem">
            <label style="font-size:0.85rem;display:flex;align-items:center;gap:6px;margin-bottom:4px">
              {fieldName}
              {field.required && <span style="color:var(--danger)">*</span>}
              {field.link && (
                <a href={field.link} target="_blank" rel="noopener" style="font-size:0.75rem;color:var(--primary)">Get key &#8594;</a>
              )}
            </label>
            {field.description && (
              <div style="font-size:0.75rem;color:var(--text-muted);margin-bottom:4px">{field.description}</div>
            )}
            <ConfigField
              field={field}
              value={values[fieldName] || ''}
              onChange={v => setValues(prev => ({ ...prev, [fieldName]: v }))}
            />
          </div>
        );
      })}

      <div style="display:flex;gap:8px;align-items:center;margin-top:0.5rem">
        <button class="btn-small btn-primary" onClick={handleSave} disabled={saving}>
          {saving ? 'Saving...' : 'Save'}
        </button>
        {saved && <span style="font-size:0.8rem;color:var(--success)">Saved</span>}
        {error && <span style="font-size:0.8rem;color:var(--danger)">{error}</span>}
      </div>
    </div>
  );
}

function ConfigField({ field, value, onChange }) {
  const inputStyle = "width:100%;font-size:0.85rem";

  switch (field.type) {
    case 'password':
      return (
        <input type="password" value={value} onInput={e => onChange(e.target.value)}
          placeholder={field.default ? `Default: ${field.default}` : 'Enter value...'}
          style={inputStyle} />
      );
    case 'number':
      return (
        <input type="number" value={value} onInput={e => onChange(e.target.value)}
          placeholder={field.default != null ? `Default: ${field.default}` : ''}
          style={inputStyle} />
      );
    case 'boolean':
      return (
        <label style="display:flex;align-items:center;gap:8px;font-size:0.85rem;cursor:pointer">
          <input type="checkbox" checked={value === 'true' || value === true}
            onChange={e => onChange(e.target.checked ? 'true' : 'false')} />
          {value === 'true' || value === true ? 'Enabled' : 'Disabled'}
        </label>
      );
    case 'select':
      return (
        <select value={value || field.default || ''} onChange={e => onChange(e.target.value)}
          style={inputStyle}>
          {!value && !field.default && <option value="">Select...</option>}
          {(field.options || []).map(opt => (
            <option key={opt} value={opt}>{opt}</option>
          ))}
        </select>
      );
    default: // string
      return (
        <input type="text" value={value} onInput={e => onChange(e.target.value)}
          placeholder={field.default ? `Default: ${field.default}` : 'Enter value...'}
          style={inputStyle} />
      );
  }
}
