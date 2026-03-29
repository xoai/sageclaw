import { h } from 'preact';
import { useState, useEffect, useRef } from 'preact/hooks';
import { SkillsContent } from './Skills';
import MCPServers from './MCPServers';

export function Marketplace() {
  const params = new URLSearchParams(window.location.search);
  const [section, setSection] = useState(params.get('section') || 'mcp');
  const [category, setCategory] = useState(params.get('category') || '');
  const [categories, setCategories] = useState([]);
  const [entries, setEntries] = useState([]);
  const [searchQuery, setSearchQuery] = useState('');
  const [agents, setAgents] = useState([]);
  const [detail, setDetail] = useState(null);
  const [installError, setInstallError] = useState('');
  const [configValues, setConfigValues] = useState({});
  const [msg, setMsg] = useState('');
  const [msgType, setMsgType] = useState('');
  const pollingRef = useRef(null);

  // Skills installed count for sidebar.
  const [skillsInstalledCount, setSkillsInstalledCount] = useState(0);

  const flash = (text, type = 'info') => {
    setMsg(text); setMsgType(type);
    setTimeout(() => setMsg(''), 4000);
  };

  // --- DATA LOADING ---

  const loadCategories = async () => {
    try {
      const r = await fetch('/api/mcp/marketplace/categories', { credentials: 'include' });
      setCategories(await r.json());
    } catch {}
  };

  const loadEntries = async (cat, q) => {
    const params = new URLSearchParams();
    if (cat) params.set('category', cat);
    if (q) params.set('q', q);
    try {
      const r = await fetch('/api/mcp/marketplace/list?' + params, { credentials: 'include' });
      const data = await r.json();
      setEntries(data);
      // Update detail modal if open (status may have changed).
      // Use functional update to avoid stale closure — only update if prev is non-null.
      setDetail(prev => {
        if (!prev) return null;
        const updated = data.find(e => e.id === prev.id);
        if (updated) return { ...prev, status: updated.status, status_error: updated.status_error, installed: updated.installed, enabled: updated.enabled };
        return prev;
      });
      return data;
    } catch { setEntries([]); return []; }
  };

  const loadAgents = async () => {
    try {
      const r = await fetch('/api/v2/agents', { credentials: 'include' });
      setAgents(await r.json());
    } catch {}
  };

  const loadSkillsInstalledCount = async () => {
    try {
      const r = await fetch('/api/skills/marketplace/installed', { credentials: 'include' });
      const d = await r.json();
      setSkillsInstalledCount(Array.isArray(d) ? d.length : 0);
    } catch {}
  };

  useEffect(() => {
    loadCategories();
    loadAgents();
    loadSkillsInstalledCount();
  }, []);

  useEffect(() => {
    if (section === 'mcp' || section.startsWith('mcp-')) {
      loadEntries(category, searchQuery);
    }
  }, [section, category, searchQuery]);

  // --- POLLING: auto-refresh while any entry is "installing" ---
  useEffect(() => {
    const hasInstalling = entries.some(e => e.status === 'installing');
    if (hasInstalling && !pollingRef.current) {
      pollingRef.current = setInterval(() => {
        loadEntries(category, searchQuery);
        loadCategories();
      }, 3000);
    } else if (!hasInstalling && pollingRef.current) {
      clearInterval(pollingRef.current);
      pollingRef.current = null;
    }
    return () => { if (pollingRef.current) { clearInterval(pollingRef.current); pollingRef.current = null; } };
  }, [entries, category, searchQuery]);

  const navigate = (sec, cat = '') => {
    setSection(sec);
    setCategory(cat);
    setSearchQuery('');
    const url = cat ? `/marketplace?section=${sec}&category=${cat}` :
      sec === 'mcp' ? '/marketplace' : `/marketplace?section=${sec}`;
    history.replaceState(null, '', url);
  };

  // --- INSTALLED COUNTS (from categories, not filtered entries) ---
  const installedMCPCount = categories.reduce((s, c) => s + c.installed, 0);
  const totalMCPCount = categories.reduce((s, c) => s + c.total, 0);

  // --- MCP ACTIONS ---

  const openDetail = async (id) => {
    try {
      const r = await fetch(`/api/mcp/marketplace/detail/${id}`, { credentials: 'include' });
      const d = await r.json();
      setDetail(d);
      setConfigValues({});
      setInstallError('');
    } catch { flash('Failed to load details', 'error'); }
  };

  const doInstall = async () => {
    if (!detail) return;
    setInstallError('');
    try {
      const r = await fetch('/api/mcp/marketplace/install', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        credentials: 'include',
        body: JSON.stringify({ id: detail.id, config: configValues }),
      });
      const d = await r.json();
      if (d.error) {
        setInstallError(d.error);
      } else {
        // Instant — update detail to show installing state.
        setDetail({ ...detail, status: 'installing', installed: false, enabled: false });
        loadEntries(category, searchQuery);
        loadCategories();
      }
    } catch (e) {
      setInstallError(e.message || 'Install failed.');
    }
  };

  const doRetry = async () => {
    if (!detail) return;
    setInstallError('');
    try {
      const r = await fetch('/api/mcp/marketplace/retry', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        credentials: 'include',
        body: JSON.stringify({ id: detail.id }),
      });
      const d = await r.json();
      if (d.error) {
        setInstallError(d.error);
      } else {
        setDetail({ ...detail, status: 'installing', status_error: '' });
        loadEntries(category, searchQuery);
      }
    } catch (e) {
      setInstallError(e.message || 'Retry failed.');
    }
  };

  const doAction = async (action, id, name) => {
    try {
      const r = await fetch(`/api/mcp/marketplace/${action}`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        credentials: 'include',
        body: JSON.stringify({ id }),
      });
      if (r.ok) {
        flash(`${name} ${action}d.`, 'success');
        loadEntries(category, searchQuery);
        loadCategories();
        if (detail?.id === id) {
          if (action === 'remove') setDetail(null);
          else if (action === 'enable') setDetail({ ...detail, status: 'installing' });
          else if (action === 'disable') setDetail({ ...detail, status: 'disabled', enabled: false });
        }
      } else {
        const d = await r.json();
        flash(d.error || `${action} failed`, 'error');
      }
    } catch { flash(`${action} failed`, 'error'); }
  };

  const doAssign = async (id, agentIds) => {
    try {
      await fetch('/api/mcp/marketplace/assign', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        credentials: 'include',
        body: JSON.stringify({ id, agent_ids: agentIds }),
      });
      flash('Agent assignment updated.', 'success');
    } catch { flash('Assignment failed', 'error'); }
  };


  // --- RENDER ---

  const isSensitive = (name) => {
    const u = name.toUpperCase();
    return u.includes('KEY') || u.includes('SECRET') || u.includes('TOKEN') || u.includes('PASSWORD');
  };

  const parseSchema = (s) => {
    if (!s || s === '{}' || s === 'null') return null;
    if (typeof s === 'object' && s !== null) {
      return Object.keys(s).length > 0 ? s : null;
    }
    try {
      const parsed = JSON.parse(s);
      if (!parsed || typeof parsed !== 'object' || Object.keys(parsed).length === 0) return null;
      return parsed;
    } catch { return null; }
  };

  const filteredEntries = entries;
  const showMCP = section === 'mcp' || section === 'mcp-installed';
  const st = detail?.status || 'available';

  return (
    <div>
      {/* Breadcrumb */}
      <div style="display:flex;align-items:center;gap:6px;margin-bottom:8px;font-size:12px;color:var(--text-muted)">
        <a href="/" style="color:var(--text-muted);text-decoration:none" onClick={e => { e.preventDefault(); history.pushState(null, '', '/'); window.dispatchEvent(new PopStateEvent('popstate')); }}>Home</a>
        <span>/</span>
        <span style="color:var(--text)">Marketplace</span>
        {category && categories.length > 0 && (
          <>
            <span>/</span>
            <span style="color:var(--text)">{categories.find(c => c.id === category)?.name || category}</span>
          </>
        )}
      </div>

      <h1 style="margin-bottom:16px">Marketplace</h1>

      {msg && (
        <div class="card" style={`padding:10px 14px;margin-bottom:12px;font-size:13px;border-color:${msgType === 'error' ? 'var(--error)' : 'var(--success)'}`}>
          <span style={`color:${msgType === 'error' ? 'var(--error)' : 'var(--success)'}`}>{msg}</span>
        </div>
      )}

      <div class="marketplace-layout">
        {/* ==================== SIDEBAR ==================== */}
        <div class="marketplace-sidebar">
          <div class="marketplace-sidebar-section">Skills</div>
          <div class={`marketplace-sidebar-item ${section === 'skills' ? 'active' : ''}`}
            onClick={() => navigate('skills')}>
            <span>Skills</span>
            <span class="marketplace-sidebar-count">{skillsInstalledCount || ''}</span>
          </div>

          <div class="marketplace-sidebar-section" style="margin-top:16px">MCP</div>
          <div class={`marketplace-sidebar-item ${section === 'mcp' && !category ? 'active' : ''}`}
            onClick={() => navigate('mcp')}>
            <span>All</span>
            <span class="marketplace-sidebar-count">{totalMCPCount}</span>
          </div>
          {categories.map(c => (
            <div key={c.id}
              class={`marketplace-sidebar-item ${category === c.id ? 'active' : ''}`}
              onClick={() => navigate('mcp', c.id)}>
              <span>{c.icon} {c.name}</span>
              <span class="marketplace-sidebar-count">{c.total}</span>
            </div>
          ))}
          <div class={`marketplace-sidebar-item ${section === 'mcp-installed' ? 'active' : ''}`}
            onClick={() => navigate('mcp-installed')}>
            <span>Installed</span>
            <span class="marketplace-sidebar-count">{installedMCPCount || '—'}</span>
          </div>

          <div style="margin-top:16px;padding:0 12px">
            <button class="btn-secondary" style="width:100%;font-size:12px" onClick={() => navigate('mcp-custom')}>
              + Add Custom
            </button>
          </div>
        </div>

        {/* ==================== CONTENT ==================== */}
        <div class="marketplace-content">

          {/* Search bar (MCP sections) */}
          {showMCP && (
            <div style="margin-bottom:16px">
              <input type="text" placeholder="Search MCP servers..."
                value={searchQuery} onInput={e => setSearchQuery(e.target.value)}
                style="width:100%;padding:10px 14px;font-size:14px" />
            </div>
          )}

          {/* MCP ALL / CATEGORY */}
          {section === 'mcp' && (
            <div class="marketplace-grid">
              {filteredEntries.map(e => (
                <MCPCard key={e.id} entry={e} onClick={() => openDetail(e.id)} />
              ))}
              {filteredEntries.length === 0 && (
                <div class="empty" style="grid-column:1/-1">No MCP servers found.</div>
              )}
            </div>
          )}

          {/* MCP INSTALLED */}
          {section === 'mcp-installed' && (
            <div class="marketplace-grid">
              {filteredEntries.filter(e => e.status !== 'available').map(e => (
                <MCPCard key={e.id} entry={e} onClick={() => openDetail(e.id)} />
              ))}
              {filteredEntries.filter(e => e.status !== 'available').length === 0 && (
                <div class="empty" style="grid-column:1/-1">
                  No MCP servers installed yet. Browse categories to get started.
                </div>
              )}
            </div>
          )}

          {/* MCP CUSTOM (manual add) */}
          {section === 'mcp-custom' && <MCPServers />}

          {/* SKILLS */}
          {section === 'skills' && <SkillsContent />}
        </div>
      </div>

      {/* ==================== DETAIL MODAL ==================== */}
      {detail && (
        <div class="modal-overlay" onClick={() => setDetail(null)} role="dialog" aria-modal="true">
          <div class="modal-content" style="max-width:580px" onClick={e => e.stopPropagation()}>
            <div style="display:flex;justify-content:space-between;align-items:flex-start;margin-bottom:12px">
              <div>
                <h2 style="margin:0 0 4px 0">{detail.name}</h2>
                <p style="font-size:13px;color:var(--text-muted);margin:0">{detail.description}</p>
              </div>
              <button class="btn-secondary" onClick={() => setDetail(null)}
                style="padding:4px 10px;font-size:18px;line-height:1">&times;</button>
            </div>

            {/* Meta */}
            <div style="font-size:12px;color:var(--text-muted);margin-bottom:16px;display:flex;gap:12px;flex-wrap:wrap">
              {detail.github_url && <a href={detail.github_url} target="_blank" style="color:var(--primary)">GitHub ★ {detail.stars}</a>}
              {detail.tags?.length > 0 && detail.tags.map(t => <span key={t} class="badge badge-gray">{t}</span>)}
            </div>

            {/* Installing indicator */}
            {st === 'installing' && (
              <div class="card" style="padding:10px 14px;margin-bottom:12px;border-color:var(--warning)">
                <div style="font-size:13px;color:var(--warning);font-weight:600;animation:pulse 1.5s infinite">
                  Installing... connecting in background
                </div>
              </div>
            )}

            {/* Config form (only for available/failed) */}
            {(() => {
              const schema = parseSchema(detail.config_schema);
              if ((st === 'available' || st === 'failed') && schema) {
                return (
                  <div style="margin-bottom:16px">
                    <div style="font-size:13px;font-weight:600;margin-bottom:8px">Configuration</div>
                    {Object.entries(schema).map(([name, field]) => (
                      <label key={name} style="display:block;margin-bottom:10px">
                        <span style="font-size:12px;color:var(--text-muted)">
                          {name} {field.required && <span style="color:var(--error)">*</span>}
                        </span>
                        {field.description && <div style="font-size:11px;color:var(--text-muted);margin-bottom:2px">{field.description}</div>}
                        <input
                          type={isSensitive(name) ? 'password' : 'text'}
                          value={configValues[name] || ''}
                          onInput={e => setConfigValues(prev => ({ ...prev, [name]: e.target.value }))}
                          style="width:100%;margin-top:4px"
                          placeholder={isSensitive(name) ? '••••••••' : ''}
                        />
                      </label>
                    ))}
                  </div>
                );
              }
              return null;
            })()}

            {/* Failed error */}
            {st === 'failed' && detail.status_error && (
              <div class="card" style="padding:10px 14px;margin-bottom:12px;border-color:var(--error)">
                <div style="font-size:12px;font-weight:600;color:var(--error);margin-bottom:4px">Connection failed</div>
                <div style="font-size:12px;color:var(--text-muted)">{detail.status_error}</div>
              </div>
            )}

            {/* Install error (validation error, not connection error) */}
            {installError && (
              <div class="card" style="padding:10px 14px;margin-bottom:12px;border-color:var(--error)">
                <div style="font-size:12px;color:var(--error)">{installError}</div>
              </div>
            )}

            {/* Status + Actions */}
            <div style="border-top:1px solid var(--border);padding-top:12px;display:flex;gap:8px;flex-wrap:wrap">
              {st === 'available' && (
                <button class="btn-primary" style="flex:1" onClick={doInstall}>
                  Install
                </button>
              )}
              {st === 'failed' && (
                <button class="btn-primary" style="flex:1" onClick={doRetry}>
                  Retry
                </button>
              )}
              {st === 'connected' && (
                <button class="btn-secondary" onClick={() => doAction('disable', detail.id, detail.name)}>Disable</button>
              )}
              {st === 'disabled' && (
                <button class="btn-primary" onClick={() => doAction('enable', detail.id, detail.name)}>Enable</button>
              )}
              {(st === 'connected' || st === 'disabled' || st === 'failed') && (
                <button class="btn-small btn-danger" onClick={() => {
                  if (confirm(`Remove "${detail.name}"? This will delete stored credentials.`)) {
                    doAction('remove', detail.id, detail.name);
                  }
                }}>Remove</button>
              )}
            </div>

            {/* Agent assignment (if connected) */}
            {st === 'connected' && agents.length > 0 && (
              <div style="margin-top:16px;border-top:1px solid var(--border);padding-top:12px">
                <div style="font-size:13px;font-weight:600;margin-bottom:8px">Agent Assignment</div>
                <p style="font-size:11px;color:var(--text-muted);margin-bottom:8px">
                  Empty = available to all agents.
                </p>
                <AgentCheckboxes agents={agents} selected={detail.agent_ids || []}
                  onChange={(ids) => { doAssign(detail.id, ids); setDetail({ ...detail, agent_ids: ids }); }} />
              </div>
            )}
          </div>
        </div>
      )}
    </div>
  );
}

// --- MCP Card Component ---

function MCPCard({ entry, onClick }) {
  const connType = entry.connection_type || 'stdio';
  const typeBadge = connType === 'stdio' ? 'badge-gray' : 'badge-blue';
  const st = entry.status || 'available';

  const statusBadge = {
    installing: { cls: 'badge-yellow', label: 'installing...', pulse: true },
    connected: { cls: 'badge-green', label: 'connected' },
    disabled: { cls: 'badge-gray', label: 'disabled' },
    failed: { cls: 'badge-yellow', label: 'failed', color: 'var(--error)' },
  }[st];

  return (
    <div class="card clickable mcp-card" onClick={onClick}>
      <div style="display:flex;align-items:center;gap:8px;margin-bottom:6px">
        <span style="font-weight:600;font-size:14px;flex:1;min-width:0;overflow:hidden;text-overflow:ellipsis;white-space:nowrap">
          {entry.name}
        </span>
        {statusBadge && (
          <span class={`badge ${statusBadge.cls}`}
            style={`font-size:10px;flex-shrink:0;${statusBadge.pulse ? 'animation:pulse 1.5s infinite;' : ''}${statusBadge.color ? 'color:' + statusBadge.color + ';border-color:' + statusBadge.color + ';' : ''}`}>
            {statusBadge.label}
          </span>
        )}
      </div>
      <div style="font-size:12px;color:var(--text-muted);min-height:32px;display:-webkit-box;-webkit-line-clamp:2;-webkit-box-orient:vertical;overflow:hidden">
        {entry.description}
      </div>
      <div style="display:flex;align-items:center;gap:8px;margin-top:8px;font-size:11px;color:var(--text-muted)">
        <span class={`badge ${typeBadge}`} style="font-size:10px">{connType}</span>
        {entry.github_url ? (
          <a href={entry.github_url} target="_blank" rel="noopener" onClick={e => e.stopPropagation()}
            style="color:var(--text-muted);text-decoration:none">
            ★ {(entry.stars || 0).toLocaleString()}
          </a>
        ) : entry.stars > 0 && <span>★ {entry.stars.toLocaleString()}</span>}
        {entry.needs_config && <span>🔑</span>}
      </div>
    </div>
  );
}

// --- Agent Checkboxes ---

function AgentCheckboxes({ agents, selected, onChange }) {
  const [sel, setSel] = useState(new Set(selected));

  useEffect(() => { setSel(new Set(selected)); }, [selected.join(',')]);

  const toggle = (id) => {
    const next = new Set(sel);
    if (next.has(id)) next.delete(id); else next.add(id);
    setSel(next);
    onChange([...next]);
  };

  return (
    <div>
      {agents.map(a => (
        <label key={a.id} style="display:flex;align-items:center;gap:8px;padding:4px 0;cursor:pointer">
          <input type="checkbox" checked={sel.has(a.id)} onChange={() => toggle(a.id)} />
          <span style="font-size:13px">{a.name || a.id}</span>
        </label>
      ))}
    </div>
  );
}
