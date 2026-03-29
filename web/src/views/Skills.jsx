import { h } from 'preact';
import { useState, useEffect, useRef } from 'preact/hooks';
import { TabBar } from '../components/TabBar';
import Tools from './Tools';
import MCPServers from './MCPServers';

const TOP_TABS = [
  { id: 'skills', label: 'Skills' },
  { id: 'tools', label: 'Tools' },
  { id: 'plugins', label: 'Plugins' },
];

export function Skills() {
  const params = new URLSearchParams(window.location.search);
  const [topTab, setTopTab] = useState(params.get('tab') || 'skills');

  const changeTopTab = (id) => {
    setTopTab(id);
    const url = id === 'skills' ? '/skills' : `/skills?tab=${id}`;
    history.replaceState(null, '', url);
  };

  return (
    <div>
      <h1>Skills</h1>
      <TabBar tabs={TOP_TABS} active={topTab} onChange={changeTopTab} />
      <div class="tab-content-enter" key={topTab}>
        {topTab === 'skills' && <SkillsContent />}
        {topTab === 'tools' && <Tools />}
        {topTab === 'plugins' && <MCPServers />}
      </div>
    </div>
  );
}

export function SkillsContent() {
  const [tab, setTab] = useState('installed'); // 'installed' | 'browse'
  const [installed, setInstalled] = useState([]);
  const [searchResults, setSearchResults] = useState([]);
  const [searchQuery, setSearchQuery] = useState('');
  const [searching, setSearching] = useState(false);
  const [agents, setAgents] = useState([]);
  const [msg, setMsg] = useState('');
  const [msgType, setMsgType] = useState('info'); // 'info' | 'error' | 'success'

  // Preview / detail modal state.
  const [preview, setPreview] = useState(null);
  const [previewLoading, setPreviewLoading] = useState(false);
  const [previewTab, setPreviewTab] = useState('readme'); // 'readme' | 'files' | 'scripts' | 'agents'
  const [approved, setApproved] = useState(false);
  const [installing, setInstalling] = useState(false);
  const [selectedAgents, setSelectedAgents] = useState({});

  // Assign modal state.
  const [assignSkill, setAssignSkill] = useState(null);
  const [assignAgents, setAssignAgents] = useState({});

  // Updates state.
  const [updates, setUpdates] = useState({});
  const [checkingUpdates, setCheckingUpdates] = useState(false);

  // Built-in skills (on disk but not from marketplace).
  const [builtinSkills, setBuiltinSkills] = useState([]);

  const searchTimer = useRef(null);

  const showMsg = (text, type = 'info') => {
    setMsg(text);
    setMsgType(type);
    setTimeout(() => setMsg(''), 4000);
  };

  // Load installed skills (marketplace).
  const loadInstalled = async () => {
    try {
      const res = await fetch('/api/skills/marketplace/installed', { credentials: 'include' });
      const data = await res.json();
      setInstalled(Array.isArray(data) ? data : []);
    } catch { setInstalled([]); }
  };

  // Load all on-disk skills, then filter to find built-ins (not in marketplace lock file).
  const loadBuiltinSkills = async () => {
    try {
      const [allRes, installedRes] = await Promise.all([
        fetch('/api/skills', { credentials: 'include' }),
        fetch('/api/skills/marketplace/installed', { credentials: 'include' }),
      ]);
      const allSkills = await allRes.json();
      const marketplaceSkills = await installedRes.json();
      const marketplaceNames = new Set((Array.isArray(marketplaceSkills) ? marketplaceSkills : []).map(s => s.name));
      const builtins = (Array.isArray(allSkills) ? allSkills : [])
        .filter(s => !marketplaceNames.has(s.name))
        .map(s => ({ ...s, builtin: true }));
      setBuiltinSkills(builtins);
    } catch { setBuiltinSkills([]); }
  };

  // Load agents.
  const loadAgents = async () => {
    try {
      const res = await fetch('/api/v2/agents', { credentials: 'include' });
      const data = await res.json();
      setAgents(Array.isArray(data) ? data : []);
    } catch {}
  };

  useEffect(() => { loadInstalled(); loadBuiltinSkills(); loadAgents(); }, []);

  // Search with debounce.
  const doSearch = async (q) => {
    if (!q.trim()) { setSearchResults([]); return; }
    setSearching(true);
    try {
      const res = await fetch(`/api/skills/marketplace/search?q=${encodeURIComponent(q)}&limit=30`, { credentials: 'include' });
      const data = await res.json();
      setSearchResults(data.results || []);
    } catch { setSearchResults([]); }
    setSearching(false);
  };

  const onSearchInput = (q) => {
    setSearchQuery(q);
    if (searchTimer.current) clearTimeout(searchTimer.current);
    searchTimer.current = setTimeout(() => doSearch(q), 400);
  };

  // Preview a skill before install.
  const openPreview = async (source, name) => {
    setPreviewLoading(true);
    setPreviewTab('readme');
    setApproved(false);
    setSelectedAgents({});
    setPreview({ name, source, loading: true });
    try {
      const res = await fetch(`/api/skills/marketplace/preview?source=${encodeURIComponent(source)}`, { credentials: 'include' });
      const data = await res.json();
      if (data.error) {
        showMsg('Preview failed: ' + data.error, 'error');
        setPreview(null);
      } else {
        setPreview({ ...data, source });
      }
    } catch {
      showMsg('Failed to load preview', 'error');
      setPreview(null);
    }
    setPreviewLoading(false);
  };

  // Install skill.
  const installSkill = async () => {
    if (!preview || !approved) return;
    setInstalling(true);
    try {
      const agentIds = Object.entries(selectedAgents).filter(([, v]) => v).map(([k]) => k);
      const res = await fetch('/api/skills/marketplace/install', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        credentials: 'include',
        body: JSON.stringify({ source: preview.source, approved: true, agents: agentIds }),
      });
      const data = await res.json();
      if (data.error) {
        showMsg('Install failed: ' + data.error, 'error');
      } else {
        showMsg(`${data.name} installed successfully!`, 'success');
        setPreview(null);
        loadInstalled();
      }
    } catch {
      showMsg('Install failed', 'error');
    }
    setInstalling(false);
  };

  // Uninstall skill.
  const uninstallSkill = async (name) => {
    if (!confirm(`Uninstall "${name}"? This will remove it from all agents.`)) return;
    try {
      const res = await fetch(`/api/skills/marketplace/${name}`, { method: 'DELETE', credentials: 'include' });
      const data = await res.json();
      if (data.error) showMsg(data.error, 'error');
      else { showMsg(`${name} uninstalled.`, 'success'); loadInstalled(); }
    } catch { showMsg('Uninstall failed', 'error'); }
  };

  // Update skill.
  const updateSkill = async (name) => {
    try {
      const res = await fetch(`/api/skills/marketplace/update/${name}`, { method: 'POST', credentials: 'include' });
      const data = await res.json();
      if (data.error) showMsg(data.error, 'error');
      else {
        showMsg(`${name} updated!`, 'success');
        const next = { ...updates };
        delete next[name];
        setUpdates(next);
        loadInstalled();
      }
    } catch { showMsg('Update failed', 'error'); }
  };

  // Check for updates.
  const checkUpdates = async () => {
    setCheckingUpdates(true);
    try {
      const res = await fetch('/api/skills/marketplace/updates', { credentials: 'include' });
      const data = await res.json();
      const map = {};
      (data.updates || []).forEach(u => { map[u.name] = u; });
      setUpdates(map);
      if (data.count === 0) showMsg('All skills are up to date.', 'info');
      else showMsg(`${data.count} update(s) available.`, 'info');
    } catch { showMsg('Failed to check updates', 'error'); }
    setCheckingUpdates(false);
  };

  // Open assign modal.
  const openAssign = (skill) => {
    const map = {};
    (skill.agents || []).forEach(a => { map[a] = true; });
    setAssignAgents(map);
    setAssignSkill(skill);
  };

  // Save assignment.
  const saveAssign = async () => {
    if (!assignSkill) return;
    const agentIds = Object.entries(assignAgents).filter(([, v]) => v).map(([k]) => k);
    try {
      await fetch(`/api/skills/marketplace/assign/${assignSkill.name}`, {
        method: 'PUT',
        headers: { 'Content-Type': 'application/json' },
        credentials: 'include',
        body: JSON.stringify({ agents: agentIds }),
      });
      showMsg('Agent assignments updated.', 'success');
      setAssignSkill(null);
      loadInstalled();
    } catch { showMsg('Failed to update assignments', 'error'); }
  };

  // Check if a skill is already installed — match on source+name to avoid
  // false positives when different repos have skills with the same name.
  const isInstalled = (source, name) => installed.some(s => s.name === name && s.source === source);

  // --- UPLOAD ---

  const fileInputRef = useRef(null);
  const [uploadReview, setUploadReview] = useState(null); // pending upload review data
  const [uploading, setUploading] = useState(false);

  const doUpload = async (file) => {
    setUploading(true);
    try {
      const form = new FormData();
      form.append('file', file);
      const res = await fetch('/api/skills/upload', {
        method: 'POST',
        credentials: 'include',
        body: form,
      });
      const data = await res.json();
      if (data.error) {
        showMsg(data.error, 'error');
      } else if (data.status === 'review') {
        // Has scripts — show review modal.
        setUploadReview(data);
        setPreviewTab('scripts');
      } else if (data.status === 'installed') {
        showMsg(`Skill "${data.name}" installed!`, 'success');
        loadInstalled();
        loadBuiltinSkills();
      }
    } catch (e) {
      showMsg(e.message || 'Upload failed', 'error');
    }
    setUploading(false);
    if (fileInputRef.current) fileInputRef.current.value = '';
  };

  const approveUpload = async () => {
    if (!uploadReview) return;
    setInstalling(true);
    try {
      const agentIds = Object.entries(selectedAgents).filter(([, v]) => v).map(([k]) => k);
      const res = await fetch('/api/skills/upload/approve', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        credentials: 'include',
        body: JSON.stringify({ upload_id: uploadReview.upload_id, agents: agentIds }),
      });
      const data = await res.json();
      if (data.error) {
        showMsg(data.error, 'error');
      } else {
        showMsg(`Skill "${data.name}" installed!`, 'success');
        loadInstalled();
        loadBuiltinSkills();
      }
    } catch { showMsg('Approval failed', 'error'); }
    setUploadReview(null);
    setInstalling(false);
    setSelectedAgents({});
  };

  // --- RENDER ---

  return (
    <div>
      <div style="margin-bottom:16px">
        <p style="color:var(--text-muted);font-size:13px">Extend your agents with community skills.</p>
      </div>

      {/* Tabs */}
      <TabBar
        tabs={[
          { id: 'installed', label: `Installed (${installed.length})` },
          { id: 'browse', label: 'Browse Marketplace' },
        ]}
        active={tab}
        onChange={setTab}
      />

      {msg && (
        <div class="card" style={`padding:10px 14px;margin-bottom:16px;font-size:13px;border-color:${msgType === 'error' ? 'var(--error)' : msgType === 'success' ? 'var(--success)' : 'var(--border)'};color:${msgType === 'error' ? 'var(--error)' : msgType === 'success' ? 'var(--success)' : 'var(--text)'}`}>
          {msg}
        </div>
      )}

      {/* ==================== INSTALLED TAB ==================== */}
      {tab === 'installed' && (
        <div>
          <div style="display:flex;gap:8px;margin-bottom:16px">
            <button class="btn-primary" onClick={() => fileInputRef.current?.click()} disabled={uploading}>
              {uploading ? 'Uploading...' : 'Upload Skill'}
            </button>
            <input ref={fileInputRef} type="file" accept=".md,.zip" style="display:none"
              onChange={e => e.target.files[0] && doUpload(e.target.files[0])} />
            <button class="btn-secondary" onClick={checkUpdates} disabled={checkingUpdates}>
              {checkingUpdates ? 'Checking...' : 'Check for Updates'}
            </button>
            <button class="btn-secondary" onClick={() => fetch('/api/skills/reload', { method: 'POST', credentials: 'include' }).then(() => showMsg('Reload requested.', 'info'))}>
              Reload Skills
            </button>
          </div>

          {/* Built-in skills */}
          {builtinSkills.length > 0 && (
            <div style="margin-bottom:16px">
              <div style="font-size:12px;font-weight:600;color:var(--text-muted);text-transform:uppercase;letter-spacing:0.05em;margin-bottom:8px">Built-in Skills</div>
              <div style="display:flex;flex-direction:column;gap:8px">
                {builtinSkills.map(sk => (
                  <div key={sk.name} class="card" style="padding:14px">
                    <div style="display:flex;justify-content:space-between;align-items:center">
                      <div style="flex:1;min-width:0">
                        <div style="display:flex;align-items:center;gap:8px">
                          <span style="font-weight:600;font-size:14px">{sk.name}</span>
                          <span class="badge badge-gray" style="font-size:10px">built-in</span>
                          {sk.tools > 0 && <span style="font-size:11px;color:var(--text-muted)">{sk.tools} tools</span>}
                        </div>
                      </div>
                    </div>
                  </div>
                ))}
              </div>
            </div>
          )}

          {/* Marketplace-installed skills */}
          {installed.length === 0 && builtinSkills.length === 0 ? (
            <div class="card" style="padding:32px;text-align:center">
              <p style="color:var(--text-muted);font-size:14px;margin-bottom:12px">
                No skills installed yet.
              </p>
              <button class="btn-primary" onClick={() => setTab('browse')}>Browse Marketplace</button>
            </div>
          ) : installed.length > 0 && (
            <div>
              {builtinSkills.length > 0 && (
                <div style="font-size:12px;font-weight:600;color:var(--text-muted);text-transform:uppercase;letter-spacing:0.05em;margin-bottom:8px">Marketplace Skills</div>
              )}
              <div style="display:flex;flex-direction:column;gap:8px">
              {installed.map(sk => (
                <div key={sk.name} class="card" style="padding:14px">
                  <div style="display:flex;justify-content:space-between;align-items:flex-start">
                    <div style="flex:1;min-width:0">
                      <div style="display:flex;align-items:center;gap:8px;margin-bottom:4px">
                        <span style="font-weight:600;font-size:14px">{sk.name}</span>
                        {sk.hasScripts && <span class="badge badge-yellow" style="font-size:10px">scripts</span>}
                        {updates[sk.name] && <span class="badge badge-blue" style="font-size:10px">update available</span>}
                      </div>
                      {sk.description && <div style="font-size:12px;color:var(--text-muted);margin-bottom:4px">{sk.description}</div>}
                      <div style="font-size:11px;color:var(--text-muted)">
                        {sk.source && <span>Source: {sk.source}</span>}
                        {sk.installedAt && <span style="margin-left:12px">Installed: {sk.installedAt.slice(0, 10)}</span>}
                      </div>
                      {sk.agents && sk.agents.length > 0 && (
                        <div style="margin-top:6px;display:flex;gap:4px;flex-wrap:wrap">
                          {sk.agents.map(a => <span key={a} class="badge badge-gray" style="font-size:10px">{a}</span>)}
                        </div>
                      )}
                    </div>
                    <div style="display:flex;gap:6px;flex-shrink:0;margin-left:12px">
                      {updates[sk.name] && (
                        <button class="btn-small" style="color:var(--primary);border-color:var(--primary)" onClick={() => updateSkill(sk.name)}>Update</button>
                      )}
                      <button class="btn-small" onClick={() => openAssign(sk)}>Agents</button>
                      <button class="btn-small btn-danger" onClick={() => uninstallSkill(sk.name)}>Uninstall</button>
                    </div>
                  </div>
                </div>
              ))}
              </div>
            </div>
          )}
        </div>
      )}

      {/* ==================== BROWSE TAB ==================== */}
      {tab === 'browse' && (
        <div>
          <div style="margin-bottom:16px">
            <input
              type="text"
              placeholder="Search skills... (e.g. react, typescript, memory)"
              value={searchQuery}
              onInput={e => onSearchInput(e.target.value)}
              style="width:100%;padding:10px 14px;font-size:14px"
            />
          </div>

          {searching && <div style="text-align:center;color:var(--text-muted);padding:24px;font-size:13px">Searching...</div>}

          {!searching && searchQuery && searchResults.length === 0 && (
            <div style="text-align:center;color:var(--text-muted);padding:24px;font-size:13px">
              No skills found for "{searchQuery}".
            </div>
          )}

          {!searching && !searchQuery && (
            <div class="card" style="padding:32px;text-align:center">
              <p style="color:var(--text-muted);font-size:14px">
                Search the skills.sh marketplace to find community skills for your agents.
              </p>
            </div>
          )}

          {!searching && searchResults.length > 0 && (
            <div style="display:flex;flex-direction:column;gap:8px">
              {searchResults.map(sk => {
                const alreadyInstalled = isInstalled(sk.source, sk.skillId || sk.name);
                return (
                  <div key={sk.id} class="card clickable" style="padding:14px;cursor:pointer"
                    onClick={() => !alreadyInstalled && openPreview(sk.source + '@' + (sk.skillId || sk.name), sk.skillId || sk.name)}>
                    <div style="display:flex;justify-content:space-between;align-items:center">
                      <div style="flex:1;min-width:0">
                        <div style="display:flex;align-items:center;gap:8px;margin-bottom:2px">
                          <span style="font-weight:600;font-size:14px">{sk.name || sk.skillId}</span>
                          {alreadyInstalled && <span class="badge badge-green" style="font-size:10px">installed</span>}
                        </div>
                        {sk.description && <div style="font-size:12px;color:var(--text-muted)">{sk.description}</div>}
                        <div style="font-size:11px;color:var(--text-muted);margin-top:2px">
                          {sk.source} {sk.installs > 0 && <span style="margin-left:8px">{sk.installs.toLocaleString()} installs</span>}
                        </div>
                      </div>
                      {!alreadyInstalled && (
                        <button class="btn-small" style="flex-shrink:0;margin-left:12px"
                          onClick={(e) => { e.stopPropagation(); openPreview(sk.source + '@' + (sk.skillId || sk.name), sk.skillId || sk.name); }}>
                          Install
                        </button>
                      )}
                    </div>
                  </div>
                );
              })}
            </div>
          )}
        </div>
      )}

      {/* ==================== PREVIEW / INSTALL MODAL ==================== */}
      {preview && (
        <div class="modal-overlay" onClick={() => !installing && setPreview(null)} role="dialog" aria-modal="true">
          <div class="modal-content" style="max-width:680px;max-height:80vh;overflow-y:auto" onClick={e => e.stopPropagation()}>
            {previewLoading || preview.loading ? (
              <div style="padding:32px;text-align:center;color:var(--text-muted)">Loading preview...</div>
            ) : (
              <div>
                <div style="display:flex;justify-content:space-between;align-items:flex-start;margin-bottom:16px">
                  <div>
                    <h2 style="font-size:18px;margin-bottom:4px">{preview.name}</h2>
                    {preview.description && <p style="font-size:13px;color:var(--text-muted);margin:0">{preview.description}</p>}
                    <div style="font-size:11px;color:var(--text-muted);margin-top:4px">Source: {preview.source}</div>
                  </div>
                  <button class="btn-secondary" onClick={() => setPreview(null)} style="padding:4px 10px;font-size:18px;line-height:1">&times;</button>
                </div>

                {/* Script warning */}
                {preview.hasScripts && (
                  <div class="card" style="padding:12px;margin-bottom:16px;border-color:var(--warning);background:var(--surface)">
                    <div style="font-weight:600;font-size:13px;color:var(--warning);margin-bottom:4px">
                      This skill contains executable scripts
                    </div>
                    <div style="font-size:12px;color:var(--text-muted)">
                      Review the scripts in the "Scripts" tab before installing. Scripts run on your server with agent permissions.
                    </div>
                  </div>
                )}

                {/* Tabs */}
                <TabBar
                  tabs={[
                    { id: 'readme', label: 'README' },
                    { id: 'files', label: `Files (${preview.files?.length || 0})` },
                    ...(preview.hasScripts ? [{ id: 'scripts', label: `Scripts (${preview.scripts?.length || 0})` }] : []),
                    { id: 'agents', label: 'Assign to Agents' },
                  ]}
                  active={previewTab}
                  onChange={setPreviewTab}
                />

                {/* Tab content */}
                <div style="min-height:200px;max-height:350px;overflow-y:auto;margin-bottom:16px">
                  {previewTab === 'readme' && (
                    <div style="font-size:13px;line-height:1.6;white-space:pre-wrap;font-family:var(--mono);padding:12px;background:var(--bg);border-radius:6px">
                      {preview.skillMd || 'No SKILL.md content available.'}
                    </div>
                  )}

                  {previewTab === 'files' && (
                    <div style="font-size:12px;font-family:var(--mono)">
                      {(preview.files || []).map((f, i) => (
                        <div key={i} style="padding:6px 12px;border-bottom:1px solid var(--border);display:flex;justify-content:space-between">
                          <span>{f.path}</span>
                          <span style="color:var(--text-muted)">{f.size > 1024 ? (f.size / 1024).toFixed(1) + ' KB' : f.size + ' B'}</span>
                        </div>
                      ))}
                    </div>
                  )}

                  {previewTab === 'scripts' && (
                    <div>
                      {(preview.scripts || []).length === 0 ? (
                        <div style="color:var(--text-muted);padding:12px;font-size:13px">No scripts detected.</div>
                      ) : (
                        preview.scripts.map((sc, i) => (
                          <div key={i} style="margin-bottom:12px">
                            <div style="font-weight:600;font-size:12px;margin-bottom:4px;color:var(--warning)">{sc.name}</div>
                            <pre style="font-size:11px;background:var(--bg);padding:12px;border-radius:6px;overflow-x:auto;margin:0;border:1px solid var(--border)">{sc.content}</pre>
                          </div>
                        ))
                      )}
                    </div>
                  )}

                  {previewTab === 'agents' && (
                    <div style="padding:4px">
                      <p style="font-size:12px;color:var(--text-muted);margin-bottom:12px">
                        Select which agents should have access to this skill after installation.
                      </p>
                      {agents.length === 0 ? (
                        <div style="color:var(--text-muted);font-size:13px">No agents configured.</div>
                      ) : (
                        agents.map(a => (
                          <label key={a.id} style="display:flex;align-items:center;gap:8px;padding:8px;cursor:pointer">
                            <input type="checkbox" checked={selectedAgents[a.id] || false}
                              onChange={() => setSelectedAgents(prev => ({ ...prev, [a.id]: !prev[a.id] }))} />
                            <span style="font-size:13px;font-weight:500">{a.name || a.id}</span>
                            {a.role && <span style="font-size:11px;color:var(--text-muted)">{a.role}</span>}
                          </label>
                        ))
                      )}
                    </div>
                  )}
                </div>

                {/* Consent + Install */}
                <div style="border-top:1px solid var(--border);padding-top:12px">
                  <label style="display:flex;align-items:center;gap:8px;margin-bottom:12px;cursor:pointer">
                    <input type="checkbox" checked={approved} onChange={() => setApproved(!approved)} />
                    <span style="font-size:13px">
                      {preview.hasScripts
                        ? 'I have reviewed the scripts and approve installation'
                        : 'I approve installing this skill'}
                    </span>
                  </label>
                  <div style="display:flex;gap:8px">
                    <button class="btn-primary" style="flex:1" onClick={installSkill}
                      disabled={!approved || installing}>
                      {installing ? 'Installing...' : 'Install Skill'}
                    </button>
                    <button class="btn-secondary" onClick={() => setPreview(null)} disabled={installing}>Cancel</button>
                  </div>
                </div>
              </div>
            )}
          </div>
        </div>
      )}

      {/* ==================== ASSIGN MODAL ==================== */}
      {/* ==================== UPLOAD REVIEW MODAL ==================== */}
      {uploadReview && (
        <div class="modal-overlay" onClick={() => setUploadReview(null)}>
          <div class="modal-content" style="max-width:640px" onClick={e => e.stopPropagation()}>
            <div style="display:flex;justify-content:space-between;align-items:flex-start;margin-bottom:12px">
              <div>
                <h2 style="margin:0 0 4px 0">{uploadReview.name}</h2>
                <p style="font-size:13px;color:var(--text-muted);margin:0">This skill contains executable scripts. Review before installing.</p>
              </div>
              <button class="btn-secondary" onClick={() => setUploadReview(null)} style="padding:4px 10px;font-size:18px;line-height:1">&times;</button>
            </div>

            {/* Warning banner */}
            <div class="card" style="padding:10px 14px;margin-bottom:12px;border-color:var(--warning)">
              <div style="font-size:12px;color:var(--warning);font-weight:600">This skill contains scripts that will execute on your system.</div>
              <div style="font-size:11px;color:var(--text-muted);margin-top:4px">Review the scripts below before approving.</div>
            </div>

            {/* Tabs */}
            <TabBar
              tabs={[
                { id: 'scripts', label: `Scripts (${uploadReview.scripts?.length || 0})` },
                { id: 'readme', label: 'README' },
                { id: 'files', label: `Files (${uploadReview.files?.length || 0})` },
                { id: 'agents', label: 'Agents' },
              ]}
              active={previewTab}
              onChange={setPreviewTab}
            />

            <div style="margin-top:12px;max-height:300px;overflow-y:auto">
              {previewTab === 'scripts' && uploadReview.scripts?.map(sc => (
                <div key={sc.path} style="margin-bottom:12px">
                  <div style="font-size:12px;font-weight:600;margin-bottom:4px;color:var(--warning)">{sc.path}</div>
                  <pre style="background:var(--bg);border:1px solid var(--border);border-radius:6px;padding:10px;font-size:11px;overflow-x:auto;max-height:200px;margin:0">{sc.content}</pre>
                </div>
              ))}
              {previewTab === 'readme' && (
                <pre style="white-space:pre-wrap;font-size:12px;color:var(--text-muted)">{uploadReview.readme || 'No README'}</pre>
              )}
              {previewTab === 'files' && (
                <div style="font-size:12px">{uploadReview.files?.map(f => <div key={f} style="padding:4px 0;color:var(--text-muted)">{f}</div>)}</div>
              )}
              {previewTab === 'agents' && (
                <div>
                  {agents.map(a => (
                    <label key={a.id} style="display:flex;align-items:center;gap:8px;padding:6px 0;cursor:pointer">
                      <input type="checkbox" checked={selectedAgents[a.id] || false}
                        onChange={() => setSelectedAgents(prev => ({ ...prev, [a.id]: !prev[a.id] }))} />
                      <span style="font-size:13px">{a.name || a.id}</span>
                    </label>
                  ))}
                </div>
              )}
            </div>

            <div style="border-top:1px solid var(--border);padding-top:12px;margin-top:12px;display:flex;gap:8px">
              <button class="btn-primary" style="flex:1" onClick={approveUpload} disabled={installing}>
                {installing ? 'Installing...' : 'Approve & Install'}
              </button>
              <button class="btn-secondary" onClick={() => setUploadReview(null)}>Cancel</button>
            </div>
          </div>
        </div>
      )}

      {assignSkill && (
        <div class="modal-overlay" onClick={() => setAssignSkill(null)} role="dialog" aria-modal="true">
          <div class="modal-content" style="max-width:420px" onClick={e => e.stopPropagation()}>
            <h2 style="font-size:16px;margin-bottom:4px">Manage Agents</h2>
            <p style="font-size:12px;color:var(--text-muted);margin-bottom:16px">
              Select which agents can use <strong>{assignSkill.name}</strong>.
            </p>
            {agents.length === 0 ? (
              <div style="color:var(--text-muted);font-size:13px;margin-bottom:16px">No agents configured.</div>
            ) : (
              <div style="margin-bottom:16px">
                {agents.map(a => (
                  <label key={a.id} style="display:flex;align-items:center;gap:8px;padding:8px;cursor:pointer">
                    <input type="checkbox" checked={assignAgents[a.id] || false}
                      onChange={() => setAssignAgents(prev => ({ ...prev, [a.id]: !prev[a.id] }))} />
                    <span style="font-size:13px;font-weight:500">{a.name || a.id}</span>
                  </label>
                ))}
              </div>
            )}
            <div style="display:flex;gap:8px">
              <button class="btn-primary" style="flex:1" onClick={saveAssign}>Save</button>
              <button class="btn-secondary" onClick={() => setAssignSkill(null)}>Cancel</button>
            </div>
          </div>
        </div>
      )}
    </div>
  );
}
