import { h } from 'preact';
import { useState, useEffect } from 'preact/hooks';
import { route } from 'preact-router';
import { Label } from '../components/InfoTip';

export default function AgentEditor({ id }) {
  const [agent, setAgent] = useState(null);
  const [loading, setLoading] = useState(true);
  const [tab, setTab] = useState('identity');
  const [saving, setSaving] = useState(false);
  const [msg, setMsg] = useState('');
  const [availableTools, setAvailableTools] = useState([]);
  const [modelData, setModelData] = useState({ models: [], connected: {} });
  const isNew = id === 'new';

  useEffect(() => {
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

    // Load available tools and models.
    fetch('/api/tools').then(r => r.json()).then(setAvailableTools).catch(() => {});
    fetch('/api/providers/models').then(r => r.json()).then(setModelData).catch(() => {});
  }, [id]);

  const save = async () => {
    setSaving(true);
    setMsg('');
    try {
      const method = isNew ? 'POST' : 'PUT';
      const url = isNew ? '/api/v2/agents' : `/api/v2/agents/${id}`;
      const body = isNew ? { ...agent } : agent;
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
      {tab === 'soul' && <MarkdownTab value={agent.soul} onChange={v => update('soul', v)} label="Soul" placeholder="Define who this agent is — personality, voice, values..." />}
      {tab === 'behavior' && <MarkdownTab value={agent.behavior} onChange={v => update('behavior', v)} label="Behavior" placeholder="Define how this agent works — rules, constraints, decision frameworks..." />}
      {tab === 'bootstrap' && <BootstrapTab agent={agent} update={update} />}
      {tab === 'tools' && <ToolsTab agent={agent} update={update} available={availableTools} />}
      {tab === 'memory' && <MemoryTab agent={agent} update={update} />}
      {tab === 'heartbeat' && <HeartbeatTab agent={agent} update={update} />}
      {tab === 'channels' && <ChannelsTab agent={agent} update={update} />}
    </div>
  );
}

function IdentityTab({ agent, update, isNew, modelData }) {
  const models = modelData?.models || [];
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

function MarkdownTab({ value, onChange, label, placeholder }) {
  const [preview, setPreview] = useState(false);
  return (
    <div>
      <div style="display:flex;justify-content:space-between;align-items:center;margin-bottom:12px">
        <span style="font-size:13px;color:var(--text-muted)">{label} — Markdown</span>
        <button class="btn-small" onClick={() => setPreview(!preview)}>
          {preview ? 'Edit' : 'Preview'}
        </button>
      </div>
      {preview ? (
        <div class="card" style="padding:16px;min-height:300px;white-space:pre-wrap;font-size:13px;line-height:1.8">
          {value || <span style="color:var(--text-muted)">No content yet.</span>}
        </div>
      ) : (
        <textarea
          value={value || ''}
          onInput={e => onChange(e.target.value)}
          placeholder={placeholder}
          style="width:100%;min-height:400px;font-family:var(--mono);font-size:13px;line-height:1.6;resize:vertical;background:var(--bg)"
        />
      )}
    </div>
  );
}

function BootstrapTab({ agent, update }) {
  const hasBootstrap = agent.bootstrap && agent.bootstrap.trim().length > 0;
  return (
    <div>
      <p style="color:var(--text-muted);font-size:13px;margin-bottom:16px">
        Bootstrap instructions run once on the agent's first conversation, then the file is automatically deleted.
        Use this for first-run rituals — introducing the agent, learning about the user, or initial setup tasks.
      </p>

      {!hasBootstrap && (
        <div class="card" style="padding:16px;margin-bottom:16px;border-style:dashed;text-align:center">
          <p style="color:var(--text-muted);margin-bottom:12px">No bootstrap configured. The agent will start with its normal personality.</p>
          <button class="btn-secondary" onClick={() => update('bootstrap',
            '# Bootstrap\n\nThis is your first conversation with a new user.\n\n' +
            '## First Run Tasks\n1. Introduce yourself warmly\n2. Ask what the user needs help with\n3. Learn their preferences\n\n' +
            '## After Bootstrap\nOnce complete, operate normally using your soul and behavior guidelines.'
          )}>Add Bootstrap Template</button>
        </div>
      )}

      {hasBootstrap && (
        <div>
          <div style="display:flex;justify-content:space-between;align-items:center;margin-bottom:8px">
            <span class="badge badge-blue">One-time — deleted after first conversation</span>
            <button class="btn-small btn-danger" onClick={() => update('bootstrap', '')}>Remove Bootstrap</button>
          </div>
          <textarea
            value={agent.bootstrap}
            onInput={e => update('bootstrap', e.target.value)}
            placeholder="First-run instructions..."
            style="width:100%;min-height:300px;font-family:var(--mono);font-size:13px;line-height:1.6;resize:vertical;background:var(--bg)"
          />
        </div>
      )}
    </div>
  );
}

function ToolsTab({ agent, update, available }) {
  const enabled = new Set(agent.tools?.enabled || []);

  const toggle = (name) => {
    const next = new Set(enabled);
    if (next.has(name)) next.delete(name); else next.add(name);
    update('tools.enabled', Array.from(next));
  };

  const allEnabled = enabled.size === 0; // Empty = all tools

  return (
    <div>
      <div style="margin-bottom:16px">
        <label style="display:flex;align-items:center;gap:8px;cursor:pointer">
          <input type="checkbox" checked={allEnabled}
            onChange={() => update('tools.enabled', allEnabled ? available.map(t => t.name) : [])} />
          <span style="font-size:13px">All tools enabled (leave empty for full access)</span>
        </label>
      </div>

      {!allEnabled && (
        <div style="display:grid;grid-template-columns:repeat(auto-fill,minmax(280px,1fr));gap:8px">
          {available.map(t => (
            <label key={t.name} class="card" style="padding:8px 12px;display:flex;gap:8px;align-items:flex-start;cursor:pointer">
              <input type="checkbox" checked={enabled.has(t.name)} onChange={() => toggle(t.name)}
                style="margin-top:2px" />
              <div>
                <div style="font-family:var(--mono);font-size:12px;color:var(--primary)">{t.name}</div>
                <div style="font-size:11px;color:var(--text-muted);margin-top:2px">{t.description}</div>
              </div>
            </label>
          ))}
        </div>
      )}

      <div style="font-size:12px;color:var(--text-muted);margin-top:16px">
        {allEnabled ? `${available.length} tools available` : `${enabled.size} of ${available.length} tools enabled`}
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
