import { h } from 'preact';
import { useState, useEffect } from 'preact/hooks';

const MODEL_TIERS = [
  { value: 'strong', label: 'Strong', desc: 'Best quality, higher cost' },
  { value: 'fast', label: 'Fast', desc: 'Lower latency, cheaper' },
  { value: 'local', label: 'Local', desc: 'Ollama, free' },
];

export default function StepAgent({ progress, onComplete, onBack }) {
  const [mode, setMode] = useState(null); // 'existing' | 'preset' | 'generate' | 'blank'
  const [presets, setPresets] = useState([]);
  const [existingAgents, setExistingAgents] = useState([]);

  // Form state
  const [name, setName] = useState('');
  const [agentId, setAgentId] = useState('');
  const [role, setRole] = useState('AI assistant');
  const [model, setModel] = useState('strong');
  const [idEdited, setIdEdited] = useState(false);

  // Generate state
  const [genPrompt, setGenPrompt] = useState('');
  const [genResult, setGenResult] = useState(null);
  const [genLoading, setGenLoading] = useState(false);

  const [loading, setLoading] = useState(false);
  const [error, setError] = useState('');

  useEffect(() => {
    fetch('/api/v2/agents/presets', { credentials: 'include' }).then(r => r.json()).then(setPresets).catch(() => {});
    fetch('/api/v2/agents', { credentials: 'include' }).then(r => r.json()).then(agents => {
      if (Array.isArray(agents)) setExistingAgents(agents);
    }).catch(() => {});
  }, []);

  // Suppress local tier if provider isn't ollama.
  const tiers = progress.providerType === 'ollama'
    ? MODEL_TIERS
    : MODEL_TIERS.filter(t => t.value !== 'local');

  const autoId = (n) => n.toLowerCase().replace(/[^a-z0-9]+/g, '-').replace(/^-|-$/g, '') || 'agent';

  const handleNameChange = (val) => {
    setName(val);
    if (!idEdited) setAgentId(autoId(val));
  };

  const createAgent = async (overrides = {}) => {
    const finalName = overrides.name || name;
    const finalId = overrides.id || agentId;
    const finalRole = overrides.role || role;
    const finalModel = overrides.model || model;

    if (!finalName.trim() || !finalId.trim()) {
      setError('Name is required');
      return;
    }

    setLoading(true);
    setError('');

    try {
      const body = {
        id: finalId,
        identity: {
          name: finalName,
          role: finalRole,
          model: finalModel,
          max_tokens: 8192,
          max_iterations: 25,
          status: 'active',
        },
      };
      const res = await fetch('/api/v2/agents', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        credentials: 'include',
        body: JSON.stringify(body),
      });
      const data = await res.json();
      if (data.error) {
        setError(data.error);
        setLoading(false);
        return;
      }

      onComplete({ agentId: finalId, agentName: finalName });
    } catch (e) {
      setError('Failed: ' + e.message);
    }
    setLoading(false);
  };

  const applyPreset = async (preset) => {
    setLoading(true);
    setError('');
    try {
      // Step 1: Fetch the preset config template.
      const configRes = await fetch(`/api/v2/agents/presets/${preset.id}`, { method: 'POST', credentials: 'include' });
      const config = await configRes.json();
      if (config.error) { setError(config.error); setLoading(false); return; }

      // Step 2: Create the agent using the preset config.
      const agentId = preset.id;
      const agentName = config.identity?.name || preset.name || preset.id;
      const body = {
        id: agentId,
        identity: {
          name: agentName,
          role: config.identity?.role || '',
          model: config.identity?.model || 'strong',
          max_tokens: parseInt(config.identity?.max_tokens) || 8192,
          max_iterations: 25,
          status: 'active',
        },
      };
      const createRes = await fetch('/api/v2/agents', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        credentials: 'include',
        body: JSON.stringify(body),
      });
      const createData = await createRes.json();
      if (createData.error) { setError(createData.error); setLoading(false); return; }

      onComplete({ agentId, agentName });
    } catch (e) {
      setError('Failed: ' + e.message);
    }
    setLoading(false);
  };

  const quickCreatePreset = async (preset) => {
    setLoading(true);
    setError('');
    try {
      const res = await fetch('/api/v2/agents/quick-create', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        credentials: 'include',
        body: JSON.stringify({
          name: preset.name,
          role: preset.role,
          avatar: preset.avatar,
          model: 'strong',
          tool_profile: preset.profile || 'full',
        }),
      });
      const data = await res.json();
      if (data.error) { setError(data.error); setLoading(false); return; }
      onComplete({ agentId: data.id, agentName: preset.name });
    } catch (e) {
      setError('Failed: ' + e.message);
    }
    setLoading(false);
  };

  const handleGenerate = async () => {
    if (!genPrompt.trim()) return;
    setGenLoading(true);
    setError('');
    try {
      const res = await fetch('/api/v2/agents/generate', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        credentials: 'include',
        body: JSON.stringify({ description: genPrompt }),
      });
      const data = await res.json();
      if (data.error && !data.config) { setError(data.error); setGenLoading(false); return; }
      setGenResult(data.config || data);
    } catch (e) {
      setError('Generation failed: ' + e.message);
    }
    setGenLoading(false);
  };

  const useGenerated = async () => {
    if (!genResult) return;
    setLoading(true);
    setError('');
    try {
      const res = await fetch('/api/v2/agents/quick-create', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        credentials: 'include',
        body: JSON.stringify(genResult),
      });
      const data = await res.json();
      if (data.error) { setError(data.error); setLoading(false); return; }
      onComplete({ agentId: data.id, agentName: genResult.name || 'Agent' });
    } catch (e) {
      setError('Failed: ' + e.message);
    }
    setLoading(false);
  };

  // Select existing agent.
  const selectExisting = (agent) => {
    onComplete({ agentId: agent.id, agentName: agent.name || agent.id });
  };

  // Mode selection screen.
  if (!mode) {
    return (
      <div class="card" style="padding:24px">
        <h2 style="font-size:18px;font-weight:700;margin-bottom:4px">
          {existingAgents.length > 0 ? 'Choose an Agent' : 'Create Your Agent'}
        </h2>
        <p style="color:var(--text-muted);font-size:13px;margin-bottom:20px">
          {existingAgents.length > 0
            ? 'Pick an existing agent or create a new one.'
            : 'Define who your agent is. Choose how to get started.'}
        </p>

        <div style="display:flex;flex-direction:column;gap:8px;margin-bottom:20px">
          {/* Existing agents */}
          {existingAgents.length > 0 && (
            <div>
              <div style="font-size:11px;text-transform:uppercase;letter-spacing:0.5px;color:var(--text-muted);margin-bottom:6px">
                Your Agents
              </div>
              {existingAgents.map(a => (
                <div key={a.id} class="card clickable" style="padding:12px;margin-bottom:6px;display:flex;align-items:center;gap:10px"
                  onClick={() => selectExisting(a)}>
                  {a.avatar && <span style="font-size:20px">{a.avatar}</span>}
                  <div style="flex:1">
                    <div style="font-weight:600;font-size:13px">{a.name || a.id}</div>
                    <div style="font-size:12px;color:var(--text-muted)">{a.role || 'No role defined'}</div>
                  </div>
                  <span class="badge badge-blue" style="font-size:11px">{a.model || 'strong'}</span>
                </div>
              ))}
            </div>
          )}

          {/* Quick presets — one-click creation */}
          <div style="font-size:11px;text-transform:uppercase;letter-spacing:0.5px;color:var(--text-muted);margin-top:8px;margin-bottom:2px">
            {existingAgents.length > 0 ? 'Quick Create' : 'Quick Start'}
          </div>
          <div style="display:grid;grid-template-columns:1fr 1fr;gap:8px">
            {[
              { name: 'Research', avatar: '\uD83D\uDD0D', role: 'Research and analysis assistant', profile: 'full' },
              { name: 'Writing', avatar: '\u270D\uFE0F', role: 'Content creation assistant', profile: 'full' },
              { name: 'Coding', avatar: '\uD83D\uDCBB', role: 'Software development assistant', profile: 'coding' },
              { name: 'General', avatar: '\u2B50', role: 'General-purpose assistant', profile: 'full' },
            ].map(p => (
              <div key={p.name} class="card clickable" style="padding:12px;margin-bottom:0;text-align:center;cursor:pointer"
                onClick={() => quickCreatePreset(p)}>
                <div style="font-size:24px;margin-bottom:4px">{p.avatar}</div>
                <div style="font-weight:600;font-size:13px">{p.name}</div>
              </div>
            ))}
          </div>

          {/* Other creation modes */}
          <div style="font-size:11px;text-transform:uppercase;letter-spacing:0.5px;color:var(--text-muted);margin-top:8px;margin-bottom:2px">
            Or Customize
          </div>
          <div class="card clickable" style="padding:14px;margin-bottom:0" onClick={() => setMode('generate')}>
            <div style="font-weight:600;font-size:14px">Create with AI</div>
            <div style="font-size:12px;color:var(--text-muted)">Describe your agent and let AI create it</div>
          </div>
          <div class="card clickable" style="padding:14px;margin-bottom:0" onClick={() => setMode('blank')}>
            <div style="font-weight:600;font-size:14px">Start Blank</div>
            <div style="font-size:12px;color:var(--text-muted)">Set up name, role, and model manually</div>
          </div>
        </div>

        <button class="btn-secondary" style="width:100%" onClick={onBack}>Back</button>
      </div>
    );
  }

  // Preset selection.
  if (mode === 'preset') {
    return (
      <div class="card" style="padding:24px">
        <h2 style="font-size:18px;font-weight:700;margin-bottom:4px">Choose a Preset</h2>
        <p style="color:var(--text-muted);font-size:13px;margin-bottom:16px">
          Pick a template. You can customize everything later.
        </p>

        <div style="display:flex;flex-direction:column;gap:8px;margin-bottom:16px">
          {presets.map(p => (
            <div key={p.id} class="card clickable" style="padding:12px;margin-bottom:0"
              onClick={() => applyPreset(p)}>
              <div style="font-weight:600;font-size:13px">{p.name || p.id}</div>
              {p.description && <div style="font-size:12px;color:var(--text-muted)">{p.description}</div>}
            </div>
          ))}
        </div>

        {error && <div style="color:var(--error);font-size:13px;margin-bottom:12px">{error}</div>}
        {loading && <div style="color:var(--text-muted);font-size:13px;margin-bottom:12px">Creating agent...</div>}

        <button class="btn-secondary" style="width:100%" onClick={() => setMode(null)}>Back</button>
      </div>
    );
  }

  // Generate mode.
  if (mode === 'generate') {
    return (
      <div class="card" style="padding:24px">
        <h2 style="font-size:18px;font-weight:700;margin-bottom:4px">Describe Your Agent</h2>
        <p style="color:var(--text-muted);font-size:13px;margin-bottom:16px">
          Tell us about your agent and we'll generate the configuration.
        </p>

        {!genResult ? (
          <div>
            <div class="form-group">
              <label>Description</label>
              <textarea
                style="width:100%;min-height:80px;background:var(--bg)"
                placeholder="e.g. A friendly coding assistant that helps with Python and JavaScript..."
                value={genPrompt}
                onInput={e => setGenPrompt(e.target.value)}
              />
            </div>
            {error && <div style="color:var(--error);font-size:13px;margin-bottom:12px">{error}</div>}
            <div style="display:flex;gap:8px">
              <button class="btn-primary" style="flex:1" onClick={handleGenerate}
                disabled={genLoading || !genPrompt.trim()}>
                {genLoading ? 'Generating...' : 'Generate'}
              </button>
              <button class="btn-secondary" onClick={() => setMode(null)}>Back</button>
            </div>
          </div>
        ) : (
          <div>
            <div class="card" style="padding:12px;margin-bottom:16px;border-color:var(--primary)">
              <div style="display:flex;align-items:center;gap:10px;margin-bottom:8px">
                <span style="font-size:24px">{genResult.avatar || '\u2B50'}</span>
                <div>
                  <div style="font-weight:600;font-size:14px">{genResult.name || 'Agent'}</div>
                  <div style="font-size:12px;color:var(--text-muted)">{genResult.role || ''}</div>
                </div>
              </div>
              <div style="display:flex;gap:6px">
                <span class="badge badge-blue">{genResult.model || 'strong'}</span>
                <span class="badge badge-neutral">{genResult.tool_profile || 'full'}</span>
              </div>
            </div>
            {error && <div style="color:var(--error);font-size:13px;margin-bottom:12px">{error}</div>}
            {loading && <div style="color:var(--text-muted);font-size:13px;margin-bottom:12px">Creating agent...</div>}
            <div style="display:flex;gap:8px">
              <button class="btn-primary" style="flex:1" onClick={useGenerated} disabled={loading}>Use This</button>
              <button class="btn-secondary" onClick={() => { setGenResult(null); setError(''); }}>Regenerate</button>
            </div>
          </div>
        )}
      </div>
    );
  }

  // Blank / manual mode.
  return (
    <div class="card" style="padding:24px">
      <h2 style="font-size:18px;font-weight:700;margin-bottom:4px">Create Your Agent</h2>
      <p style="color:var(--text-muted);font-size:13px;margin-bottom:20px">
        Set up the basics. You can customize personality, tools, and more later.
      </p>

      <div class="form-group">
        <label>Agent Name</label>
        <input type="text" placeholder="e.g. Research Assistant" value={name}
          onInput={e => handleNameChange(e.target.value)} />
      </div>

      <div class="form-group">
        <label>Agent ID <span style="color:var(--text-muted);font-weight:400">(folder name)</span></label>
        <input type="text" placeholder="auto-generated" value={agentId}
          style="font-family:var(--mono);font-size:12px"
          onInput={e => { setAgentId(e.target.value.toLowerCase().replace(/[^a-z0-9_-]/g, '')); setIdEdited(true); }} />
      </div>

      <div class="form-group">
        <label>Role</label>
        <input type="text" placeholder="e.g. personal research assistant" value={role}
          onInput={e => setRole(e.target.value)} />
      </div>

      <div class="form-group">
        <label>Model Tier</label>
        <div style="display:flex;gap:8px">
          {tiers.map(t => (
            <div
              key={t.value}
              class={`card clickable ${model === t.value ? 'card-selected' : ''}`}
              style={`flex:1;padding:12px;text-align:center;cursor:pointer;margin-bottom:0`}
              onClick={() => setModel(t.value)}
            >
              <div style={`font-size:13px;font-weight:${model === t.value ? '700' : '600'}`}>{t.label}</div>
              <div style="font-size:11px;color:var(--text-muted)">{t.desc}</div>
            </div>
          ))}
        </div>
      </div>

      {error && <div style="color:var(--error);font-size:13px;margin-bottom:12px">{error}</div>}

      <div style="display:flex;gap:8px;margin-top:16px">
        <button class="btn-primary" style="flex:1" onClick={() => createAgent()}
          disabled={loading || !name.trim()}>
          {loading ? 'Creating...' : 'Create Agent'}
        </button>
        <button class="btn-secondary" onClick={() => setMode(null)}>Back</button>
      </div>
    </div>
  );
}
