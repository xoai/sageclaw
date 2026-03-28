import { h } from 'preact';
import { useState, useEffect } from 'preact/hooks';
import { route } from 'preact-router';
import { Label } from '../components/InfoTip';
import { TabBar } from '../components/TabBar';
import { Teams } from './Teams';
import Delegation from './Delegation';

const TABS = [
  { id: 'agents', label: 'Agents' },
  { id: 'teams', label: 'Teams' },
];

export function Agents() {
  const params = new URLSearchParams(window.location.search);
  const [topTab, setTopTab] = useState(params.get('tab') || 'agents');

  const changeTopTab = (id) => {
    setTopTab(id);
    const url = id === 'agents' ? '/agents' : `/agents?tab=${id}`;
    history.replaceState(null, '', url);
  };

  return (
    <div>
      <h1>Agents</h1>
      <TabBar tabs={TABS} active={topTab} onChange={changeTopTab} />
      <div class="tab-content-enter" key={topTab}>
        {topTab === 'agents' && <AgentsList />}
        {topTab === 'teams' && <TeamsAndDelegation />}
      </div>
    </div>
  );
}

function TeamsAndDelegation() {
  const [section, setSection] = useState('teams');
  return (
    <div>
      <div style="display:flex;gap:8px;margin-bottom:16px">
        <button class={section === 'teams' ? 'btn-primary' : 'btn-secondary'} onClick={() => setSection('teams')}>Teams</button>
        <button class={section === 'delegation' ? 'btn-primary' : 'btn-secondary'} onClick={() => setSection('delegation')}>Delegation</button>
      </div>
      {section === 'teams' && <Teams embedded />}
      {section === 'delegation' && <Delegation />}
    </div>
  );
}

function AgentsList() {
  const [agents, setAgents] = useState([]);
  const [loading, setLoading] = useState(true);
  const [showCreate, setShowCreate] = useState(false);
  const [createForm, setCreateForm] = useState({ id: '', name: '', role: 'AI assistant', model: 'strong' });
  const [createError, setCreateError] = useState('');
  const [modelData, setModelData] = useState({ models: [], connected: {} });

  const load = async () => {
    try {
      const res = await fetch('/api/v2/agents');
      if (res.ok) setAgents(await res.json());
    } catch {}
    setLoading(false);
  };

  const deleteAgent = async (id) => {
    if (!confirm(`Delete agent "${id}"? This removes the agent folder and all its files.`)) return;
    await fetch(`/api/v2/agents/${id}`, { method: 'DELETE' });
    load();
  };

  const createAgent = async () => {
    setCreateError('');
    const body = {
      id: createForm.id,
      identity: {
        name: createForm.name,
        role: createForm.role,
        model: createForm.model,
        max_tokens: 8192,
        max_iterations: 25,
        status: 'active',
      },
    };
    const res = await fetch('/api/v2/agents', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(body),
    });
    const data = await res.json();
    if (data.error) { setCreateError(data.error); return; }
    setShowCreate(false);
    // Navigate to the full editor for detailed configuration.
    route(`/agents/${createForm.id}`);
  };

  const openCreate = () => {
    route('/agents/create');
  };

  useEffect(() => {
    load();
    fetch('/api/providers/models').then(r => r.json()).then(setModelData).catch(() => {});
  }, []);

  if (loading) return <div class="empty">Loading agents...</div>;

  return (
    <div>
      <div style="display:flex;justify-content:flex-end;margin-bottom:16px">
        <button class="btn-primary" onClick={openCreate}>+ New Agent</button>
      </div>

      {agents.length === 0 ? (
        <div class="empty">
          <p>No agents configured.</p>
          <p style="margin-top:8px">
            <button class="btn-primary" onClick={openCreate}>Create Your First Agent</button>
          </p>
        </div>
      ) : (
        <div class="card-list">
          {agents.map(a => (
            <div key={a.id} class="card clickable" style="padding:16px;cursor:pointer"
              onClick={() => route(`/agents/${a.id}`)}>
              <div style="display:flex;justify-content:space-between;align-items:center">
                <div style="display:flex;align-items:center;gap:10px">
                  {a.avatar && <span style="font-size:24px">{a.avatar}</span>}
                  <div>
                    <h3 style="font-size:15px;font-weight:600;margin-bottom:2px">{a.name || a.id}</h3>
                    <div style="font-size:12px;color:var(--text-muted)">{a.role || 'No role defined'}</div>
                  </div>
                </div>
                <div style="display:flex;gap:8px;align-items:center" onClick={e => e.stopPropagation()}>
                  {a.status === 'inactive' && <span class="badge badge-red">inactive</span>}
                  <span class="badge badge-blue">{a.model || 'strong'}</span>
                  {a.has_soul && <span class="badge badge-green" title="Has soul.md">soul</span>}
                  {a.has_behavior && <span class="badge badge-green" title="Has behavior.md">behavior</span>}
                  {a.has_bootstrap && <span class="badge badge-blue" title="Has bootstrap.md">bootstrap</span>}
                  {a.tools_count > 0 && <span class="badge badge-gray">{a.tools_count} tools</span>}
                  <button class="btn-small" onClick={(e) => { e.stopPropagation(); route(`/agents/${a.id}/edit`); }}>Edit</button>
                  <button class="btn-small btn-danger" onClick={(e) => { e.stopPropagation(); deleteAgent(a.id); }}>Delete</button>
                </div>
              </div>
            </div>
          ))}
        </div>
      )}

      {/* Quick Create Modal */}
      {showCreate && (
        <div class="modal-overlay" onClick={() => setShowCreate(false)} role="dialog" aria-modal="true" aria-labelledby="create-agent-title">
          <div class="modal-content" onClick={e => e.stopPropagation()}>
            <h2 id="create-agent-title">New Agent</h2>
            <p style="color:var(--text-muted);font-size:13px;margin-bottom:16px">
              Set up the basics here. You'll configure personality, tools, and more in the full editor.
            </p>

            <div class="form-group">
              <Label text="Display Name" tip="The name shown in the dashboard and used in conversations." />
              <input type="text" placeholder="e.g. Research Assistant" value={createForm.name}
                onInput={e => {
                  const name = e.target.value;
                  const autoId = name.toLowerCase().replace(/[^a-z0-9]+/g, '-').replace(/^-|-$/g, '') || 'agent';
                  setCreateForm({ ...createForm, name, id: autoId });
                }} />
            </div>

            <div class="form-group">
              <Label text="Agent ID" tip="Folder name on disk. Auto-generated from name. Must be lowercase, no spaces." />
              <input type="text" placeholder="auto-generated" value={createForm.id}
                onInput={e => setCreateForm({ ...createForm, id: e.target.value.toLowerCase().replace(/[^a-z0-9_-]/g, '') })} />
            </div>

            <div class="form-group">
              <Label text="Role" tip="A short description of what this agent does. Included in the system prompt." />
              <input type="text" placeholder="e.g. personal research assistant" value={createForm.role}
                onInput={e => setCreateForm({ ...createForm, role: e.target.value })} />
            </div>

            <div class="form-group">
              <Label text="Model" tip="Auto-route picks the best available. Or choose a specific model from a connected provider." />
              <select value={createForm.model} onChange={e => setCreateForm({ ...createForm, model: e.target.value })}>
                <optgroup label="Auto-route (recommended)">
                  <option value="strong">strong — Best quality (auto-selects)</option>
                  <option value="fast">fast — Lower latency (auto-selects)</option>
                  <option value="local">local — Ollama, free</option>
                </optgroup>
                {(() => {
                  const grouped = {};
                  const provLabels = { anthropic: 'Anthropic', openai: 'OpenAI', gemini: 'Gemini', openrouter: 'OpenRouter', github: 'GitHub', ollama: 'Ollama' };
                  (modelData.models || []).forEach(m => {
                    if (!grouped[m.provider]) grouped[m.provider] = [];
                    grouped[m.provider].push(m);
                  });
                  return Object.entries(grouped).map(([prov, models]) => (
                    <optgroup key={prov} label={`${provLabels[prov] || prov} ${modelData.connected?.[prov] ? '' : '(not connected)'}`}>
                      {models.map(m => (
                        <option key={m.id} value={m.model_id} disabled={!m.available}>
                          {m.id} — {m.name}
                        </option>
                      ))}
                    </optgroup>
                  ));
                })()}
              </select>
            </div>

            {createError && <div style="color:var(--error);font-size:13px;margin-bottom:8px">{createError}</div>}

            <div style="display:flex;gap:8px;margin-top:16px">
              <button class="btn-primary" onClick={createAgent} disabled={!createForm.id || !createForm.name}>
                Create & Configure
              </button>
              <button class="btn-secondary" onClick={() => setShowCreate(false)}>Cancel</button>
            </div>
          </div>
        </div>
      )}
    </div>
  );
}
