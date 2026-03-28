import { h } from 'preact';
import { useState, useEffect } from 'preact/hooks';
import { route } from 'preact-router';
import { Label } from '../components/InfoTip';
import ConfigPanel from '../components/ConfigPanel';

export default function AgentEditor({ id }) {
  const [agent, setAgent] = useState(null);
  const [loading, setLoading] = useState(true);
  const [tab, setTab] = useState('identity');
  const [saving, setSaving] = useState(false);
  const [msg, setMsg] = useState('');
  const [availableTools, setAvailableTools] = useState([]);
  const [modelData, setModelData] = useState({ models: [], connected: {} });
  const [schemas, setSchemas] = useState([]);
  const isNew = !id || id === 'new';

  useEffect(() => {
    // Reset state when navigating between agents.
    setAgent(null);
    setLoading(true);
    setTab('identity');
    setMsg('');

    if (!isNew) {
      fetch(`/api/v2/agents/${id}`)
        .then(r => r.json())
        .then(data => {
          if (data.error) throw new Error(data.error);
          setAgent(data);
        })
        .catch(() => setAgent(null))
        .finally(() => setLoading(false));
    } else {
      // Auto-generate a default name and ID.
      const num = Math.floor(Math.random() * 900) + 100;
      const defaultName = `Agent ${num}`;
      const defaultId = `agent-${num}`;
      setAgent({
        id: defaultId,
        identity: { name: defaultName, role: 'AI assistant', model: 'strong', max_tokens: 8192, max_iterations: 25, avatar: '', tags: [] },
        soul: '',
        behavior: '',
        bootstrap: '',
        tools: { enabled: [], config: {} },
        memory: { scope: 'project', auto_store: true, retention_days: 0, search_limit: 10, tags_boost: [] },
        heartbeat: { schedules: [] },
        channels: { serve: [], overrides: {} },
      });
      setLoading(false);
    }

    // Load available tools, models, and schemas.
    fetch('/api/tools').then(r => r.json()).then(setAvailableTools).catch(() => {});
    fetch('/api/providers/models').then(r => r.json()).then(setModelData).catch(() => {});
    fetch('/api/v2/agents/schemas').then(r => r.json()).then(data => setSchemas(data || [])).catch(() => {});
  }, [id]);

  const save = async () => {
    setSaving(true);
    setMsg('');
    try {
      const method = isNew ? 'POST' : 'PUT';
      const url = isNew ? '/api/v2/agents' : `/api/v2/agents/${id}`;
      const body = isNew ? { ...agent } : { config: agent };
      const res = await fetch(url, {
        method,
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(body),
      });
      const data = await res.json();
      if (data.error) { setMsg('Error: ' + data.error); return; }
      setMsg('Saved!');
      if (isNew && agent.id) {
        setTimeout(() => route(`/agents/${agent.id}`), 500);
      }
    } catch (e) {
      setMsg('Save failed');
    }
    setSaving(false);
    setTimeout(() => setMsg(''), 3000);
  };

  if (loading) return <div class="empty">Loading...</div>;
  if (!agent && !isNew) return <div class="empty">Agent not found.</div>;

  const update = (path, value) => {
    setAgent(prev => {
      const next = JSON.parse(JSON.stringify(prev));
      const keys = path.split('.');
      let obj = next;
      for (let i = 0; i < keys.length - 1; i++) obj = obj[keys[i]];
      obj[keys[keys.length - 1]] = value;
      return next;
    });
  };

  const tabs = [
    { id: 'identity', label: 'Identity' },
    { id: 'soul', label: 'Soul' },
    { id: 'behavior', label: 'Behavior' },
    { id: 'bootstrap', label: 'Bootstrap' },
    { id: 'tools', label: 'Tools' },
    { id: 'memory', label: 'Memory' },
    { id: 'heartbeat', label: 'Heartbeat' },
    { id: 'channels', label: 'Channels' },
    { id: 'skills', label: 'Skills' },
    { id: 'voice', label: 'Voice' },
  ];

  return (
    <div>
      <div style="margin-bottom:16px">
        <a href="/agents" style="font-size:13px;color:var(--text-muted)">← Agents</a>
      </div>

      <div style="display:flex;justify-content:space-between;align-items:center;margin-bottom:16px">
        <h1 style="margin-bottom:0">{isNew ? 'New Agent' : `Edit: ${agent.identity?.name || id}`}</h1>
        <div style="display:flex;gap:8px;align-items:center">
          {msg && <span style={`font-size:13px;color:${msg.startsWith('Error') ? 'var(--error)' : 'var(--success)'}`}>{msg}</span>}
          <button class="btn-primary" onClick={save} disabled={saving}>
            {saving ? 'Saving...' : 'Save'}
          </button>
        </div>
      </div>

      {/* Tab bar */}
      <div class="tab-bar">
        {tabs.map(t => (
          <button key={t.id} class={tab === t.id ? 'tab-active' : ''} onClick={() => setTab(t.id)}>
            {t.label}
          </button>
        ))}
      </div>

      {/* Tab content */}
      {tab === 'identity' && <IdentityTab agent={agent} update={update} isNew={isNew} modelData={modelData} />}
      {tab === 'soul' && <SectionTab section="soul" value={agent.soul} onChange={v => update('soul', v)} schemas={schemas} label="Soul" placeholder="Define who this agent is — personality, voice, values..." />}
      {tab === 'behavior' && <SectionTab section="behavior" value={agent.behavior} onChange={v => update('behavior', v)} schemas={schemas} label="Behavior" placeholder="Define how this agent works — rules, constraints, decision frameworks..." />}
      {tab === 'bootstrap' && <SectionTab section="bootstrap" value={agent.bootstrap} onChange={v => update('bootstrap', v)} schemas={schemas} label="Bootstrap" placeholder="First-run instructions for the agent's initial conversation..." />}
      {tab === 'tools' && <ToolsTab agent={agent} update={update} available={availableTools} />}
      {tab === 'memory' && <MemoryTab agent={agent} update={update} />}
      {tab === 'heartbeat' && <HeartbeatTab agent={agent} update={update} />}
      {tab === 'channels' && <ChannelsTab agent={agent} update={update} />}
      {tab === 'skills' && <SkillsTab agent={agent} update={update} />}
      {tab === 'voice' && <VoiceTab agent={agent} update={update} />}
    </div>
  );
}

function IdentityTab({ agent, update, isNew, modelData }) {
  const models = modelData?.models || [];
  const combos = modelData?.combos || [];
  const connected = modelData?.connected || {};

  // Group models by provider.
  const grouped = {};
  models.forEach(m => {
    if (!grouped[m.provider]) grouped[m.provider] = [];
    grouped[m.provider].push(m);
  });

  const providerLabels = {
    anthropic: 'Anthropic', openai: 'OpenAI', gemini: 'Google Gemini',
    openrouter: 'OpenRouter', github: 'GitHub Copilot', ollama: 'Ollama (Local)',
  };

  const formatCost = (m) => {
    if (m.input_cost === 0 && m.output_cost === 0) return 'Free';
    return `$${m.input_cost}/$${m.output_cost} per 1M tokens`;
  };

  return (
    <div style="max-width:600px">
      <div class="form-group">
        <Label text="Display Name" tip="The name shown in the dashboard and conversations." />
        <input type="text" value={agent.identity?.name} placeholder="e.g. Research Agent"
          onInput={e => {
            const name = e.target.value;
            update('identity.name', name);
            if (isNew) {
              const autoId = name.toLowerCase().replace(/[^a-z0-9]+/g, '-').replace(/^-|-$/g, '');
              update('id', autoId || 'agent');
            }
          }} />
      </div>
      {isNew && (
        <div class="form-group">
          <Label text="Agent ID" tip="Folder name on disk. Must be lowercase, no spaces." />
          <input type="text" value={agent.id} placeholder="auto-generated from name"
            onInput={e => update('id', e.target.value.toLowerCase().replace(/[^a-z0-9_-]/g, ''))} />
        </div>
      )}
      <div class="form-group">
        <Label text="Role" tip="A short description included in the system prompt. Defines what the agent does." />
        <input type="text" value={agent.identity?.role} placeholder="e.g. personal research assistant"
          onInput={e => update('identity.role', e.target.value)} />
      </div>
      <div class="form-group">
        <Label text="Model" tip="Which LLM powers this agent. Tiers auto-route to the best available provider. Or pick a specific model." />
        <select value={agent.identity?.model} onChange={e => update('identity.model', e.target.value)}>
          <optgroup label="Auto-route (recommended)">
            <option value="strong">strong — Best quality (auto-selects)</option>
            <option value="fast">fast — Lower latency (auto-selects)</option>
            <option value="local">local — Ollama, free</option>
          </optgroup>
          {combos.length > 0 && (
            <optgroup label="Combos (custom fallback chains)">
              {combos.map(c => (
                <option key={c.id} value={`combo:${c.id}`}>
                  {c.name} — {c.strategy} ({(c.models || []).length} models)
                </option>
              ))}
            </optgroup>
          )}
          {Object.entries(grouped).map(([prov, provModels]) => (
            <optgroup key={prov} label={`${providerLabels[prov] || prov} ${connected[prov] ? '(connected)' : '(not connected)'}`}>
              {provModels.map(m => (
                <option key={m.id} value={m.model_id} disabled={!m.available}>
                  {m.id} — {m.name} ({formatCost(m)})
                </option>
              ))}
            </optgroup>
          ))}
        </select>
      </div>
      <div style="display:grid;grid-template-columns:1fr 1fr;gap:12px">
        <div class="form-group">
          <label>Max Output Tokens</label>
          <input type="number" value={agent.identity?.max_tokens}
            onInput={e => update('identity.max_tokens', parseInt(e.target.value) || 8192)} />
          <div style="font-size:11px;color:var(--text-muted);margin-top:4px">Max tokens per response</div>
        </div>
        <div class="form-group">
          <label>Max Iterations</label>
          <input type="number" value={agent.identity?.max_iterations}
            onInput={e => update('identity.max_iterations', parseInt(e.target.value) || 25)} />
          <div style="font-size:11px;color:var(--text-muted);margin-top:4px">Max tool-use cycles per turn</div>
        </div>
      </div>
      <div class="form-group">
        <label>Avatar (emoji or URL)</label>
        <input type="text" value={agent.identity?.avatar} placeholder="e.g. 🤖 or https://..."
          onInput={e => update('identity.avatar', e.target.value)} />
      </div>
      <div class="form-group">
        <label>Status</label>
        <select value={agent.identity?.status || 'active'} onChange={e => update('identity.status', e.target.value)}>
          <option value="active">Active — accepting conversations</option>
          <option value="inactive">Inactive — disabled</option>
        </select>
      </div>
      <div class="form-group">
        <label>Tags (comma-separated)</label>
        <input type="text" value={(agent.identity?.tags || []).join(', ')}
          onInput={e => update('identity.tags', e.target.value.split(',').map(s => s.trim()).filter(Boolean))}
          placeholder="e.g. default, general, research" />
      </div>
    </div>
  );
}

function SectionTab({ section, value, onChange, schemas, label, placeholder }) {
  const [mode, setMode] = useState('structured'); // 'structured' or 'advanced'
  const schema = schemas.find(s => s.type === section);

  // Normalize value: could be string (legacy markdown) or object (schema-based).
  const isObject = value && typeof value === 'object';
  const objValue = isObject ? value : {};
  const strValue = isObject ? '' : (value || '');

  // When switching to advanced mode with object data, serialize to YAML-like string.
  const getAdvancedText = () => {
    if (!isObject) return strValue;
    // Convert object to readable key: value lines.
    return Object.entries(value)
      .filter(([, v]) => v !== '' && v !== null && v !== undefined)
      .map(([k, v]) => {
        if (Array.isArray(v)) return `${k}: ${v.join(', ')}`;
        return `${k}: ${v}`;
      }).join('\n');
  };

  if (mode === 'advanced' || !schema) {
    return (
      <div>
        <div style="display:flex;justify-content:space-between;align-items:center;margin-bottom:12px">
          <span style="font-size:13px;color:var(--text-muted)">{label} — Markdown</span>
          {schema && (
            <button class="btn-small" onClick={() => setMode('structured')}>
              Structured Editor
            </button>
          )}
        </div>
        <textarea
          value={mode === 'advanced' && isObject ? getAdvancedText() : strValue}
          onInput={e => onChange(e.target.value)}
          placeholder={placeholder}
          style="width:100%;min-height:400px;font-family:var(--mono);font-size:13px;line-height:1.6;resize:vertical;background:var(--bg)"
        />
      </div>
    );
  }

  return (
    <div>
      <div style="display:flex;justify-content:space-between;align-items:center;margin-bottom:12px">
        <span style="font-size:13px;color:var(--text-muted)">{label} — Structured</span>
        <button class="btn-small" onClick={() => setMode('advanced')}>
          Advanced (Markdown)
        </button>
      </div>
      <ConfigPanel
        schema={schema}
        values={objValue}
        onChange={(vals) => onChange({ ...objValue, ...vals })}
        onClose={() => {}}
        inline={true}
      />
    </div>
  );
}


function ToolsTab({ agent, update, available }) {
  const profile = agent.tools?.profile || 'full';
  const deny = agent.tools?.deny || [];
  const shellDeny = new Set(agent.tools?.shell_deny_groups || []);
  const mcpServers = agent.tools?.mcp_servers || {};
  const headless = agent.tools?.headless || false;
  const preAuthorize = agent.tools?.pre_authorize || [];
  const [showAdvanced, setShowAdvanced] = useState(deny.length > 0 || shellDeny.size > 0);
  const [showAddMCP, setShowAddMCP] = useState(false);
  const [newMCP, setNewMCP] = useState({ name: '', transport: 'stdio', command: '', url: '', trust: 'untrusted' });
  const [denyInput, setDenyInput] = useState('');

  const addDeny = () => {
    const v = denyInput.trim();
    if (v && !deny.includes(v)) {
      update('tools.deny', [...deny, v]);
    }
    setDenyInput('');
  };

  const removeDeny = (item) => {
    update('tools.deny', deny.filter(d => d !== item));
  };

  const toggleShellGroup = (group) => {
    const next = new Set(shellDeny);
    if (next.has(group)) next.delete(group); else next.add(group);
    update('tools.shell_deny_groups', Array.from(next));
  };

  const togglePreAuth = (group) => {
    if (preAuthorize.includes(group)) {
      update('tools.pre_authorize', preAuthorize.filter(g => g !== group));
    } else {
      update('tools.pre_authorize', [...preAuthorize, group]);
    }
  };

  const addMCPServer = () => {
    if (!newMCP.name) return;
    const cfg = { transport: newMCP.transport, trust: newMCP.trust };
    if (newMCP.transport === 'stdio') {
      cfg.command = newMCP.command;
    } else {
      cfg.url = newMCP.url;
    }
    update('tools.mcp_servers', { ...mcpServers, [newMCP.name]: cfg });
    setNewMCP({ name: '', transport: 'stdio', command: '', url: '', trust: 'untrusted' });
    setShowAddMCP(false);
  };

  const removeMCPServer = (name) => {
    const next = { ...mcpServers };
    delete next[name];
    update('tools.mcp_servers', next);
  };

  const profileDescriptions = {
    full: 'All tool groups. Shell, MCP, and delegation still require consent.',
    coding: 'Files, shell, web, memory, knowledge, delegation, audit.',
    messaging: 'Web, memory, and team tools.',
    readonly: 'Files (read), web, memory, and audit. No writing or execution.',
    minimal: 'No tools by default. Use deny list exceptions or consent prompts.',
  };

  const shellGroups = ['filesystem', 'network', 'process', 'system', 'package'];
  const alwaysConsentGroups = ['runtime', 'mcp', 'orchestration'];

  return (
    <div>
      {/* Tool Profile */}
      <div style="margin-bottom:20px">
        <h3 style="font-size:14px;margin-bottom:8px">Tool Profile</h3>
        <select value={profile} onChange={e => update('tools.profile', e.target.value)} style="width:280px">
          <option value="full">Full — all tool groups</option>
          <option value="coding">Coding — files, shell, web, memory, delegation</option>
          <option value="messaging">Messaging — web, memory, team</option>
          <option value="readonly">Read Only — files, web, memory, audit</option>
          <option value="minimal">Minimal — no tools by default</option>
        </select>
        <div style="font-size:11px;color:var(--text-muted);margin-top:4px">
          {profileDescriptions[profile] || 'Select a profile to define which tools this agent can use.'}
        </div>
      </div>

      {/* Always-Consent Notice */}
      <div class="card" style="padding:12px;margin-bottom:20px;border-color:var(--warning);background:var(--warning-bg, rgba(255,193,7,0.05))">
        <div style="font-size:12px;font-weight:600;margin-bottom:4px">Always requires consent</div>
        <div style="font-size:11px;color:var(--text-muted)">
          Shell commands, MCP servers, and delegation always prompt for permission, regardless of profile.
        </div>
        <div style="display:flex;gap:8px;margin-top:8px">
          {alwaysConsentGroups.map(g => (
            <span key={g} class="badge badge-yellow" style="font-size:10px">{g}</span>
          ))}
        </div>
      </div>

      {/* Headless Mode */}
      <div style="margin-bottom:20px">
        <label style="display:flex;align-items:center;gap:8px;cursor:pointer">
          <input type="checkbox" checked={headless}
            onChange={e => update('tools.headless', e.target.checked)} />
          <div>
            <span style="font-size:13px;font-weight:500">Headless mode</span>
            <div style="font-size:11px;color:var(--text-muted)">
              No consent prompts. For cron jobs, API agents, and webhooks.
            </div>
          </div>
        </label>

        {headless && (
          <div style="margin-top:12px;padding:12px;border-radius:8px;background:var(--bg-secondary)">
            <div style="font-size:12px;font-weight:500;margin-bottom:8px">Pre-authorize (required for always-consent tools)</div>
            <div style="display:flex;flex-wrap:wrap;gap:8px">
              {['runtime', 'orchestration'].map(g => (
                <label key={g} style="display:flex;align-items:center;gap:6px;font-size:12px;cursor:pointer">
                  <input type="checkbox" checked={preAuthorize.includes(g)} onChange={() => togglePreAuth(g)} />
                  <span style="text-transform:capitalize">{g}</span>
                </label>
              ))}
              {Object.keys(mcpServers).map(name => {
                const key = `mcp:${name}`;
                return (
                  <label key={key} style="display:flex;align-items:center;gap:6px;font-size:12px;cursor:pointer">
                    <input type="checkbox" checked={preAuthorize.includes(key)} onChange={() => togglePreAuth(key)} />
                    <span style="font-family:var(--mono)">mcp:{name}</span>
                  </label>
                );
              })}
            </div>
            {preAuthorize.length === 0 && (
              <div style="font-size:11px;color:var(--warning);margin-top:6px">
                No pre-authorized groups. Shell, MCP, and delegation will be blocked.
              </div>
            )}
          </div>
        )}
      </div>

      {/* Advanced Section (collapsible) */}
      <div style="margin-bottom:20px">
        <span onClick={() => setShowAdvanced(!showAdvanced)}
          style="font-size:13px;color:var(--text-muted);cursor:pointer;user-select:none">
          {showAdvanced ? '\u25BC' : '\u25B6'} Advanced settings
          {deny.length > 0 && ` (${deny.length} denied)`}
        </span>

        {showAdvanced && (
          <div style="margin-top:12px">
            {/* Deny List */}
            <div style="margin-bottom:16px">
              <h4 style="font-size:13px;margin-bottom:6px">Deny List</h4>
              <p style="font-size:11px;color:var(--text-muted);margin-bottom:8px">
                Block specific tools or groups. Use "group:runtime" for groups or tool names directly.
              </p>
              <div style="display:flex;gap:6px;margin-bottom:8px">
                <input type="text" value={denyInput} placeholder="e.g. write_file or group:runtime"
                  onInput={e => setDenyInput(e.target.value)}
                  onKeyDown={e => e.key === 'Enter' && addDeny()}
                  style="flex:1;font-size:12px" />
                <button class="btn-small" onClick={addDeny} disabled={!denyInput.trim()}>Add</button>
              </div>
              {deny.length > 0 && (
                <div style="display:flex;flex-wrap:wrap;gap:6px">
                  {deny.map(d => (
                    <span key={d} class="badge" style="display:flex;align-items:center;gap:4px;font-size:11px;padding:4px 8px;background:var(--bg-secondary)">
                      <span style="font-family:var(--mono)">{d}</span>
                      <button onClick={() => removeDeny(d)} style="background:none;border:none;cursor:pointer;font-size:14px;color:var(--text-muted);padding:0">&times;</button>
                    </span>
                  ))}
                </div>
              )}
            </div>

            {/* Shell Deny Groups */}
            <div>
              <h4 style="font-size:13px;margin-bottom:6px">Shell Command Restrictions</h4>
              <p style="font-size:11px;color:var(--text-muted);margin-bottom:8px">
                Block categories of shell commands.
              </p>
              <div style="display:flex;flex-wrap:wrap;gap:8px">
                {shellGroups.map(g => (
                  <label key={g} class="card" style="padding:6px 12px;display:flex;align-items:center;gap:6px;cursor:pointer">
                    <input type="checkbox" checked={shellDeny.has(g)} onChange={() => toggleShellGroup(g)} />
                    <span style="text-transform:capitalize;font-size:12px">{g}</span>
                  </label>
                ))}
              </div>
            </div>
          </div>
        )}
      </div>

      {/* MCP Servers */}
      <div>
        <div style="display:flex;justify-content:space-between;align-items:center;margin-bottom:8px">
          <h3 style="font-size:14px;margin:0">MCP Servers</h3>
          <button class="btn-small" onClick={() => setShowAddMCP(true)}>+ Add</button>
        </div>
        <p style="font-size:12px;color:var(--text-muted);margin-bottom:12px">
          External tool servers. Each server requires consent on first use.
        </p>

        {Object.keys(mcpServers).length === 0 ? (
          <div style="font-size:12px;color:var(--text-muted)">No MCP servers configured for this agent.</div>
        ) : (
          <div class="card-list">
            {Object.entries(mcpServers).map(([name, cfg]) => (
              <div key={name} class="card" style="padding:12px;display:flex;justify-content:space-between;align-items:center">
                <div>
                  <div style="font-family:var(--mono);font-size:13px;color:var(--primary)">{name}</div>
                  <div style="font-size:11px;color:var(--text-muted);margin-top:2px">
                    {cfg.transport || 'stdio'} &middot; {cfg.trust || 'untrusted'}
                    {cfg.command && ` \u00b7 ${cfg.command}`}
                    {cfg.url && ` \u00b7 ${cfg.url}`}
                  </div>
                </div>
                <button class="btn-small btn-danger" onClick={() => removeMCPServer(name)}>Remove</button>
              </div>
            ))}
          </div>
        )}

        {showAddMCP && (
          <div class="card" style="padding:16px;margin-top:12px;border-color:var(--primary)">
            <div style="display:grid;grid-template-columns:1fr 1fr;gap:12px">
              <div class="form-group">
                <label>Server Name</label>
                <input type="text" value={newMCP.name} placeholder="e.g. brave-search"
                  onInput={e => setNewMCP({ ...newMCP, name: e.target.value })} />
              </div>
              <div class="form-group">
                <label>Transport</label>
                <select value={newMCP.transport} onChange={e => setNewMCP({ ...newMCP, transport: e.target.value })}>
                  <option value="stdio">stdio (local process)</option>
                  <option value="sse">SSE (remote)</option>
                  <option value="streamable-http">Streamable HTTP (remote)</option>
                </select>
              </div>
            </div>
            {newMCP.transport === 'stdio' ? (
              <div class="form-group">
                <label>Command</label>
                <input type="text" value={newMCP.command} placeholder="e.g. npx -y @anthropic/mcp-server-brave"
                  onInput={e => setNewMCP({ ...newMCP, command: e.target.value })} />
              </div>
            ) : (
              <div class="form-group">
                <label>URL</label>
                <input type="text" value={newMCP.url} placeholder="e.g. http://localhost:8080/mcp"
                  onInput={e => setNewMCP({ ...newMCP, url: e.target.value })} />
              </div>
            )}
            <div class="form-group">
              <label>Trust Level</label>
              <select value={newMCP.trust} onChange={e => setNewMCP({ ...newMCP, trust: e.target.value })}>
                <option value="untrusted">Untrusted (results wrapped + scrubbed)</option>
                <option value="trusted">Trusted (raw results passed through)</option>
              </select>
            </div>
            <div style="display:flex;gap:8px;justify-content:flex-end">
              <button class="btn-secondary" onClick={() => setShowAddMCP(false)}>Cancel</button>
              <button class="btn-primary" onClick={addMCPServer} disabled={!newMCP.name}>Add Server</button>
            </div>
          </div>
        )}
      </div>
    </div>
  );
}

function MemoryTab({ agent, update }) {
  const mem = agent.memory || {};
  return (
    <div style="max-width:500px">
      <div class="form-group">
        <label>Scope</label>
        <select value={mem.scope || 'project'} onChange={e => update('memory.scope', e.target.value)}>
          <option value="project">Project (per-workspace)</option>
          <option value="global">Global (shared across all workspaces)</option>
        </select>
      </div>
      <div class="form-group">
        <label style="display:flex;align-items:center;gap:8px;cursor:pointer">
          <input type="checkbox" checked={mem.auto_store !== false}
            onChange={e => update('memory.auto_store', e.target.checked)} />
          Auto-store learnings and important findings
        </label>
      </div>
      <div class="form-group">
        <label>Retention (days, 0 = forever)</label>
        <input type="number" value={mem.retention_days || 0}
          onInput={e => update('memory.retention_days', parseInt(e.target.value) || 0)} />
      </div>
      <div class="form-group">
        <label>Default Search Limit</label>
        <input type="number" value={mem.search_limit || 10}
          onInput={e => update('memory.search_limit', parseInt(e.target.value) || 10)} />
      </div>
      <div class="form-group">
        <label>Priority Tags (comma-separated)</label>
        <input type="text" value={(mem.tags_boost || []).join(', ')}
          onInput={e => update('memory.tags_boost', e.target.value.split(',').map(s => s.trim()).filter(Boolean))}
          placeholder="e.g. important, decision, learning" />
        <div style="font-size:11px;color:var(--text-muted);margin-top:4px">
          Memories with these tags rank higher in search results.
        </div>
      </div>
    </div>
  );
}

function HeartbeatTab({ agent, update }) {
  const schedules = agent.heartbeat?.schedules || [];

  const addSchedule = () => {
    update('heartbeat.schedules', [...schedules, { name: '', cron: '', prompt: '', channel: 'web' }]);
  };

  const removeSchedule = (idx) => {
    update('heartbeat.schedules', schedules.filter((_, i) => i !== idx));
  };

  const updateSchedule = (idx, field, value) => {
    const next = [...schedules];
    next[idx] = { ...next[idx], [field]: value };
    update('heartbeat.schedules', next);
  };

  const presets = [
    { label: 'Every hour', value: '0 * * * *' },
    { label: 'Daily 9am', value: '0 9 * * *' },
    { label: 'Weekday 9am', value: '0 9 * * 1-5' },
    { label: 'Weekly Friday 5pm', value: '0 17 * * 5' },
  ];

  return (
    <div>
      <p style="color:var(--text-muted);font-size:13px;margin-bottom:16px">
        Heartbeat schedules let the agent run proactively — checking things, summarizing, or performing routine tasks on a schedule.
      </p>

      {schedules.map((s, i) => (
        <div key={i} class="card" style="padding:16px;margin-bottom:12px">
          <div style="display:flex;justify-content:space-between;align-items:center;margin-bottom:12px">
            <strong>Schedule {i + 1}</strong>
            <button class="btn-small btn-danger" onClick={() => removeSchedule(i)}>Remove</button>
          </div>
          <div style="display:grid;grid-template-columns:1fr 1fr;gap:12px">
            <div class="form-group">
              <label>Name</label>
              <input type="text" value={s.name} placeholder="e.g. morning-briefing"
                onInput={e => updateSchedule(i, 'name', e.target.value)} />
            </div>
            <div class="form-group">
              <label>Cron Schedule</label>
              <select value={s.cron} onChange={e => { if (e.target.value) updateSchedule(i, 'cron', e.target.value); }}>
                <option value="">Custom...</option>
                {presets.map(p => <option key={p.value} value={p.value}>{p.label}</option>)}
              </select>
              <input type="text" value={s.cron} placeholder="0 9 * * *"
                onInput={e => updateSchedule(i, 'cron', e.target.value)} style="margin-top:4px" />
            </div>
          </div>
          <div class="form-group">
            <label>Prompt</label>
            <textarea rows="3" value={s.prompt} placeholder="What should the agent do?"
              onInput={e => updateSchedule(i, 'prompt', e.target.value)} style="width:100%" />
          </div>
          <div class="form-group">
            <label>Channel</label>
            <select value={s.channel || 'web'} onChange={e => updateSchedule(i, 'channel', e.target.value)}>
              <option value="web">Web Dashboard</option>
              <option value="telegram">Telegram</option>
              <option value="discord">Discord</option>
              <option value="cli">CLI</option>
            </select>
          </div>
        </div>
      ))}

      <button class="btn-secondary" onClick={addSchedule}>+ Add Schedule</button>
    </div>
  );
}

function SkillsTab({ agent, update }) {
  const assigned = agent.skills?.skills || [];
  const [installed, setInstalled] = useState([]);
  const [loading, setLoading] = useState(true);

  useEffect(() => {
    fetch('/api/skills/marketplace/installed', { credentials: 'include' })
      .then(r => r.json())
      .then(data => { setInstalled(Array.isArray(data) ? data : []); setLoading(false); })
      .catch(() => setLoading(false));
  }, []);

  const isAssigned = (name) => assigned.includes(name);

  const toggle = (name) => {
    const next = isAssigned(name)
      ? assigned.filter(s => s !== name)
      : [...assigned, name];
    update('skills.skills', next);
  };

  return (
    <div style="max-width:600px">
      <p style="font-size:13px;color:var(--text-muted);margin-bottom:16px">
        Select which marketplace skills this agent can use. Install skills from the <a href="/skills">Skills page</a>.
      </p>

      {loading ? (
        <div style="color:var(--text-muted);font-size:13px">Loading installed skills...</div>
      ) : installed.length === 0 ? (
        <div class="card" style="padding:20px;text-align:center">
          <p style="color:var(--text-muted);font-size:13px;margin-bottom:8px">No marketplace skills installed yet.</p>
          <a href="/skills" class="btn-secondary" style="text-decoration:none;display:inline-block">Browse Marketplace</a>
        </div>
      ) : (
        <div>
          <div style="margin-bottom:12px;font-size:12px;color:var(--text-muted)">
            {assigned.length} of {installed.length} skills assigned
          </div>
          {installed.map(sk => (
            <label key={sk.name} style="display:flex;align-items:flex-start;gap:10px;padding:10px;cursor:pointer;border-bottom:1px solid var(--border)">
              <input type="checkbox" checked={isAssigned(sk.name)} onChange={() => toggle(sk.name)}
                style="margin-top:2px" />
              <div style="flex:1">
                <div style="font-weight:600;font-size:13px">{sk.name}</div>
                {sk.description && <div style="font-size:11px;color:var(--text-muted)">{sk.description}</div>}
                <div style="font-size:11px;color:var(--text-muted);margin-top:2px">
                  {sk.source}
                  {sk.hasScripts && <span class="badge badge-yellow" style="font-size:9px;margin-left:6px">scripts</span>}
                </div>
              </div>
            </label>
          ))}
        </div>
      )}
    </div>
  );
}

function ChannelsTab({ agent, update }) {
  const serve = new Set(agent.channels?.serve || []);
  const overrides = agent.channels?.overrides || {};
  const allChannels = ['web', 'cli', 'telegram', 'discord', 'zalo', 'whatsapp', 'mcp'];
  const allServed = serve.size === 0;

  const toggleChannel = (ch) => {
    const next = new Set(serve);
    if (next.has(ch)) next.delete(ch); else next.add(ch);
    update('channels.serve', Array.from(next));
  };

  const updateOverride = (ch, field, value) => {
    const next = { ...overrides };
    if (!next[ch]) next[ch] = {};
    next[ch][field] = value;
    update('channels.overrides', next);
  };

  return (
    <div style="max-width:600px">
      <div style="margin-bottom:16px">
        <label style="display:flex;align-items:center;gap:8px;cursor:pointer">
          <input type="checkbox" checked={allServed}
            onChange={() => update('channels.serve', allServed ? allChannels : [])} />
          <span style="font-size:13px">Serve all channels (leave empty for universal)</span>
        </label>
      </div>

      {!allServed && (
        <div style="margin-bottom:16px">
          {allChannels.map(ch => (
            <label key={ch} style="display:flex;align-items:center;gap:8px;padding:6px 0;cursor:pointer">
              <input type="checkbox" checked={serve.has(ch)} onChange={() => toggleChannel(ch)} />
              <span style="text-transform:capitalize">{ch}</span>
            </label>
          ))}
        </div>
      )}

      <h3 style="font-size:14px;margin:16px 0 12px">Per-Channel Overrides</h3>
      <p style="font-size:12px;color:var(--text-muted);margin-bottom:12px">
        Override max_tokens per channel (e.g. shorter responses on mobile).
      </p>

      {allChannels.filter(ch => allServed || serve.has(ch)).map(ch => (
        <div key={ch} style="display:flex;align-items:center;gap:12px;margin-bottom:8px">
          <span style="width:100px;text-transform:capitalize;font-size:13px">{ch}</span>
          <input type="number" placeholder="Default" style="width:120px"
            value={overrides[ch]?.max_tokens || ''}
            onInput={e => updateOverride(ch, 'max_tokens', parseInt(e.target.value) || 0)} />
          <span style="font-size:11px;color:var(--text-muted)">max tokens</span>
        </div>
      ))}
    </div>
  );
}

function VoiceTab({ agent, update }) {
  const voice = agent.voice || {};
  const enabled = voice.enabled || false;
  const model = voice.model || '';
  const voiceName = voice.voice_name || 'Sadaltager';
  const languageCode = voice.language_code || '';

  // All 30 Gemini Live voice presets from official docs, grouped by character.
  const voicePresets = [
    { name: 'Sadaltager', desc: 'Knowledgeable' },
    { name: 'Kore', desc: 'Bright' },
    { name: 'Charon', desc: 'Informative' },
    { name: 'Zephyr', desc: 'Bright' },
    { name: 'Puck', desc: 'Upbeat' },
    { name: 'Fenrir', desc: 'Excitable' },
    { name: 'Orus', desc: 'Firm' },
    { name: 'Leda', desc: 'Youthful' },
    { name: 'Aoede', desc: 'Breezy' },
    { name: 'Callirrhoe', desc: 'Easy-going' },
    { name: 'Autonoe', desc: 'Bright' },
    { name: 'Iapetus', desc: 'Clear' },
    { name: 'Enceladus', desc: 'Breathy' },
    { name: 'Umbriel', desc: 'Easy-going' },
    { name: 'Despina', desc: 'Smooth' },
    { name: 'Algieba', desc: 'Smooth' },
    { name: 'Algzenib', desc: 'Gravelly' },
    { name: 'Rasalgethi', desc: 'Informative' },
    { name: 'Erinome', desc: 'Clear' },
    { name: 'Alnilam', desc: 'Firm' },
    { name: 'Laomedeia', desc: 'Upbeat' },
    { name: 'Achernar', desc: 'Soft' },
    { name: 'Pulcherrima', desc: 'Forward' },
    { name: 'Schedar', desc: 'Even' },
    { name: 'Gacrux', desc: 'Mature' },
    { name: 'Vindemiatrix', desc: 'Gentle' },
    { name: 'Achird', desc: 'Friendly' },
    { name: 'Zubenelgenubi', desc: 'Casual' },
    { name: 'Sulafat', desc: 'Warm' },
    { name: 'Sadachbia', desc: 'Lively' },
  ];

  // Supported languages from Gemini Live docs.
  const languages = [
    { code: '', label: 'Auto-detect (default)' },
    { code: 'en-US', label: 'English (US)' },
    { code: 'en-IN', label: 'English (India)' },
    { code: 'vi-VN', label: 'Vietnamese' },
    { code: 'ja-JP', label: 'Japanese' },
    { code: 'ko-KR', label: 'Korean' },
    { code: 'zh-CN', label: 'Chinese (Mandarin)' },
    { code: 'fr-FR', label: 'French' },
    { code: 'de-DE', label: 'German' },
    { code: 'es-US', label: 'Spanish (US)' },
    { code: 'pt-BR', label: 'Portuguese (Brazil)' },
    { code: 'hi-IN', label: 'Hindi' },
    { code: 'id-ID', label: 'Indonesian' },
    { code: 'it-IT', label: 'Italian' },
    { code: 'nl-NL', label: 'Dutch' },
    { code: 'pl-PL', label: 'Polish' },
    { code: 'ro-RO', label: 'Romanian' },
    { code: 'ru-RU', label: 'Russian' },
    { code: 'th-TH', label: 'Thai' },
    { code: 'tr-TR', label: 'Turkish' },
    { code: 'uk-UA', label: 'Ukrainian' },
    { code: 'ar-EG', label: 'Arabic (Egyptian)' },
    { code: 'bn-BD', label: 'Bengali' },
    { code: 'mr-IN', label: 'Marathi' },
    { code: 'ta-IN', label: 'Tamil' },
    { code: 'te-IN', label: 'Telugu' },
  ];

  return (
    <div style="max-width:600px">
      <div style="margin-bottom:20px">
        <label style="display:flex;align-items:center;gap:8px;cursor:pointer">
          <input type="checkbox" checked={enabled}
            onChange={() => {
              update('voice.enabled', !enabled);
              if (!enabled && !voice.voice_name) {
                update('voice.voice_name', 'Sadaltager');
              }
            }} />
          <span style="font-weight:500">Enable voice messaging</span>
        </label>
        <p style="font-size:12px;color:var(--text-muted);margin:6px 0 0 26px">
          When enabled, this agent can receive and respond with voice messages
          using Gemini Live native audio.
        </p>

        <div style="margin:8px 0 0 26px;padding:8px 12px;background:var(--warning-bg, #fef3c7);border-radius:6px;border:1px solid var(--warning-border, #f59e0b)">
          <p style="font-size:12px;color:var(--warning-text, #92400e);margin:0">
            Requires a <strong>Gemini API key</strong> configured in <a href="/providers">Providers</a>.
            Voice will not work without it.
          </p>
        </div>
      </div>

      {enabled && (
        <div>
          <div style="margin-bottom:16px">
            <label style="display:block;font-size:13px;font-weight:500;margin-bottom:6px">Audio Model</label>
            <input type="text" style="width:100%"
              placeholder="gemini-2.5-flash-native-audio-preview-12-2025"
              value={model}
              onInput={e => update('voice.model', e.target.value)} />
            <p style="font-size:11px;color:var(--text-muted);margin-top:4px">
              Leave empty for default. Preview models may change.
            </p>
          </div>

          <div style="margin-bottom:16px">
            <label style="display:block;font-size:13px;font-weight:500;margin-bottom:6px">Language</label>
            <select style="width:100%;padding:6px 8px"
              value={languageCode}
              onChange={e => update('voice.language_code', e.target.value)}>
              {languages.map(l => (
                <option key={l.code} value={l.code}>{l.label}</option>
              ))}
            </select>
            <p style="font-size:11px;color:var(--text-muted);margin-top:4px">
              Native audio models can auto-detect language, but setting one explicitly improves quality.
            </p>
          </div>

          <div style="margin-bottom:16px">
            <label style="display:block;font-size:13px;font-weight:500;margin-bottom:6px">
              Voice Preset
              <span style="font-weight:400;color:var(--text-muted);margin-left:8px">
                ({voiceName || 'none'})
              </span>
            </label>
            <div style="display:flex;flex-wrap:wrap;gap:6px">
              {voicePresets.map(v => (
                <button key={v.name}
                  class={voiceName === v.name ? 'btn-primary' : 'btn-outline'}
                  style="padding:3px 10px;font-size:11px"
                  title={v.desc}
                  onClick={() => update('voice.voice_name', v.name)}>
                  {v.name}
                </button>
              ))}
            </div>
            <p style="font-size:11px;color:var(--text-muted);margin-top:4px">
              Default: Sadaltager (Knowledgeable). Hover for character description.
            </p>
          </div>

          <div style="padding:12px;background:var(--bg-elevated);border-radius:8px;border:1px solid var(--border)">
            <p style="font-size:12px;color:var(--text-muted);margin:0;line-height:1.5">
              <strong>Cost:</strong> Audio processing costs ~$1.35/min for full-duplex conversation
              ($3/M input tokens, $12/M output tokens). Audio accumulates at ~25 tokens/sec.
              Costs are tracked in the Budget page.
            </p>
          </div>
        </div>
      )}
    </div>
  );
}
