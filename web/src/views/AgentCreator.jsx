import { h } from 'preact';
import { useState, useEffect } from 'preact/hooks';
import { route } from 'preact-router';
import { rpc } from '../api';
import BodyDiagram from '../components/BodyDiagram';
import ConfigPanel from '../components/ConfigPanel';

const defaultConfig = {
  identity: {}, soul: {}, behavior: {}, skills: {},
  tools: {}, memory: {}, heartbeat: {}, channels: {}, bootstrap: {},
};

export default function AgentCreator({ id }) {
  const [config, setConfig] = useState({ ...defaultConfig });
  const [schemas, setSchemas] = useState([]);
  const [activeNode, setActiveNode] = useState(null);
  const [saving, setSaving] = useState(false);
  const [msg, setMsg] = useState('');
  const [isNew, setIsNew] = useState(!id || id === 'create');
  const [presets, setPresets] = useState([]);
  const [showPresets, setShowPresets] = useState(false);
  const [showMagic, setShowMagic] = useState(false);
  const [showAvatar, setShowAvatar] = useState(false);
  const [magicDesc, setMagicDesc] = useState('');
  const [generating, setGenerating] = useState(false);

  // Avatar state.
  const [avatarURI, setAvatarURI] = useState(null);
  const [avatarStyle, setAvatarStyle] = useState('minimalist');
  const [generatingAvatar, setGeneratingAvatar] = useState(false);

  // Load schemas + presets.
  useEffect(() => {
    fetch('/api/v2/agents/schemas').then(r => r.json()).then(data => setSchemas(data || [])).catch(() => {});
    fetch('/api/v2/agents/presets').then(r => r.json()).then(data => setPresets(data || [])).catch(() => {});
  }, []);

  // Load existing agent config if editing.
  useEffect(() => {
    if (id && id !== 'create') {
      setIsNew(false);
      fetch(`/api/v2/agents/${id}`)
        .then(r => r.json())
        .then(data => {
          if (data && !data.error) {
            setConfig(prev => ({ ...prev, ...data }));
          }
        })
        .catch(() => {});
    }
  }, [id]);

  const applyPreset = async (presetId) => {
    const res = await fetch(`/api/v2/agents/presets/${presetId}`, { method: 'POST' });
    const data = await res.json();
    if (data && !data.error) {
      setConfig({ ...defaultConfig, ...data });
      setShowPresets(false);
      setMsg(`Applied "${presetId}" preset. Customize any section.`);
      setTimeout(() => setMsg(''), 5000);
    }
  };

  const generateFromDescription = async () => {
    if (!magicDesc.trim()) return;
    setGenerating(true);
    try {
      const res = await fetch('/api/v2/agents/generate', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ description: magicDesc }),
      });
      const data = await res.json();
      if (data.error) {
        setMsg('Generation failed: ' + data.error);
      } else if (data.config) {
        setConfig({ ...defaultConfig, ...data.config });
        setShowMagic(false);
        setMagicDesc('');
        setMsg(`Agent generated! Review and customize below.`);
      }
    } catch (err) {
      setMsg('Generation failed: ' + err.message);
    }
    setGenerating(false);
    setTimeout(() => setMsg(''), 8000);
  };

  const updateSection = (type, values) => {
    setConfig(prev => ({ ...prev, [type]: { ...prev[type], ...values } }));
  };

  const getCompletionState = (type) => {
    const section = config[type];
    if (!section || Object.keys(section).length === 0) return 'empty';
    const schema = schemas.find(s => s.type === type);
    if (!schema) return 'empty';

    const requiredFields = [];
    schema.sections?.forEach(sec => {
      sec.fields?.forEach(f => {
        if (f.required) requiredFields.push(f.key);
      });
    });

    if (requiredFields.length === 0) {
      return Object.keys(section).length > 0 ? 'complete' : 'empty';
    }

    const filledRequired = requiredFields.filter(k => section[k] && section[k] !== '');
    if (filledRequired.length === requiredFields.length) return 'complete';
    if (filledRequired.length > 0) return 'partial';
    return Object.keys(section).some(k => section[k] && section[k] !== '') ? 'partial' : 'empty';
  };

  const generateAvatar = async (provider) => {
    const agentId = config.identity?.name?.toLowerCase().replace(/\s+/g, '-') || 'new-agent';
    setGeneratingAvatar(true);
    try {
      const res = await fetch('/api/v2/agents/avatar', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          agent_id: agentId,
          style: avatarStyle,
          provider,
          context: {
            identity: config.identity || {},
            soul: config.soul || {},
            behavior: config.behavior || {},
          },
        }),
      });
      const data = await res.json();
      if (data.data_uri) {
        setAvatarURI(data.data_uri);
      } else {
        setMsg('Error: ' + (data.error || 'Unknown error'));
      }
    } catch (err) {
      setMsg('Error: ' + err.message);
    }
    setGeneratingAvatar(false);
  };

  const handleSave = () => {
    // Show avatar modal before saving.
    setShowAvatar(true);
  };

  const saveAgent = async (skipAvatar) => {
    setShowAvatar(false);
    setSaving(true);
    try {
      const agentId = config.identity?.name?.toLowerCase().replace(/\s+/g, '-') || 'new-agent';
      const method = isNew ? 'POST' : 'PUT';
      const url = isNew ? '/api/v2/agents' : `/api/v2/agents/${id}`;

      const res = await fetch(url, {
        method,
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ id: agentId, config }),
      });
      const data = await res.json();
      if (data.error) {
        setMsg('Error: ' + data.error);
      } else {
        setMsg('Agent saved!');
        if (isNew) {
          setTimeout(() => route('/agents'), 1500);
        }
      }
    } catch (err) {
      setMsg('Error: ' + err.message);
    }
    setSaving(false);
    setTimeout(() => setMsg(''), 5000);
  };

  const activeSchema = schemas.find(s => s.type === activeNode);
  const isMobile = typeof window !== 'undefined' && window.innerWidth < 768;

  return (
    <div>
      <div style="display:flex;justify-content:space-between;align-items:center;margin-bottom:12px">
        <div>
          <h1 style="margin-bottom:2px">{isNew ? 'Create Agent' : `Edit: ${config.identity?.name || id}`}</h1>
          <p style="color:var(--text-muted);font-size:13px">
            {isNew ? 'Pick a preset, describe what you need, or build from scratch.' : 'Click any node to edit.'}
          </p>
        </div>
        <div style="display:flex;gap:8px;align-items:center">
          {msg && <span style={`font-size:13px;color:${msg.startsWith('Error') ? 'var(--error)' : 'var(--success)'}`}>{msg}</span>}
          <button class="btn-primary" onClick={handleSave} disabled={saving}>
            {saving ? 'Saving...' : (isNew ? 'Save Agent' : 'Save Changes')}
          </button>
        </div>
      </div>

      {/* Creation path buttons — only for new agents */}
      {isNew && (
        <div style="display:flex;gap:8px;margin-bottom:16px;flex-wrap:wrap">
          <button class="btn-secondary" onClick={() => setShowPresets(true)}>
            {'\u{1F3AE}'} Presets
          </button>
          <button class="btn-secondary" onClick={() => setShowMagic(true)}
            style="background:linear-gradient(135deg,rgba(88,166,255,0.1),rgba(210,153,34,0.1));border-color:var(--primary)">
            {'\u2728'} Magic — describe and generate
          </button>
        </div>
      )}

      {/* Desktop/Mobile layout */}
      {isMobile ? (
        <MobileCreator
          config={config}
          schemas={schemas}
          activeNode={activeNode}
          setActiveNode={setActiveNode}
          getCompletionState={getCompletionState}
          updateSection={updateSection}
        />
      ) : (
        <div style="display:flex;align-items:center;justify-content:center;min-height:600px;background:radial-gradient(ellipse at center,#253248 0%,#060a10 70%);border-radius:12px;padding:40px 48px;border:1px solid rgba(48,54,61,0.4)">
          <BodyDiagram
            activeNode={activeNode}
            onNodeClick={setActiveNode}
            getState={getCompletionState}
          />
        </div>
      )}

      {/* Config panel modal — opens when a node is clicked */}
      {activeNode && activeSchema && !isMobile && (
        <div class="modal-overlay" onClick={() => setActiveNode(null)}>
          <div class="modal-content" onClick={e => e.stopPropagation()} style="width:720px;max-height:85vh;display:flex;flex-direction:column;overflow:hidden">
            {/* Fixed header with close button — outside scroll */}
            <div style="display:flex;justify-content:space-between;align-items:center;padding-bottom:12px;border-bottom:1px solid var(--border);margin-bottom:12px;flex-shrink:0">
              <div>
                <h2 style="font-size:16px;margin-bottom:2px">{activeSchema.title}</h2>
                <p style="color:var(--text-muted);font-size:12px">{activeSchema.subtitle}</p>
              </div>
              <button class="btn-small" onClick={() => setActiveNode(null)}>{'\u2715'}</button>
            </div>
            {/* Scrollable content */}
            <div style="overflow-y:auto;flex:1">
              <ConfigPanel
                schema={activeSchema}
                values={config[activeNode] || {}}
                onChange={(values) => updateSection(activeNode, values)}
                onClose={() => setActiveNode(null)}
                inline={true}
              />
            </div>
          </div>
        </div>
      )}

      {/* === MODALS === */}

      {/* Preset selector modal */}
      {showPresets && (
        <div class="modal-overlay" onClick={() => setShowPresets(false)}>
          <div class="modal-content" onClick={e => e.stopPropagation()} style="width:640px">
            <h2>Choose a Preset</h2>
            <p style="color:var(--text-muted);font-size:13px;margin-bottom:16px">
              Start from a pre-configured agent template. You can customize everything after.
            </p>
            <div style="display:grid;grid-template-columns:repeat(auto-fill,minmax(180px,1fr));gap:12px">
              {presets.map(p => (
                <div key={p.id} class="card clickable" onClick={() => applyPreset(p.id)}
                  style="padding:16px;cursor:pointer;text-align:center">
                  <div style="font-size:28px;margin-bottom:8px">
                    {p.icon === 'search' ? '\u{1F50D}' : p.icon === 'code' ? '\u{1F4BB}' : p.icon === 'pen' ? '\u270D\uFE0F' : p.icon === 'tasks' ? '\u{1F3AF}' : p.icon === 'chart' ? '\u{1F4CA}' : '\u2B50'}
                  </div>
                  <div style="font-weight:600;font-size:14px">{p.name}</div>
                  <div style="font-size:12px;color:var(--text-muted);margin-top:4px">{p.description}</div>
                </div>
              ))}
            </div>
            <div style="margin-top:16px;text-align:right">
              <button class="btn-secondary" onClick={() => setShowPresets(false)}>Cancel</button>
            </div>
          </div>
        </div>
      )}

      {/* Magic generator modal */}
      {showMagic && (
        <div class="modal-overlay" onClick={() => setShowMagic(false)}>
          <div class="modal-content" onClick={e => e.stopPropagation()} style="width:560px">
            <h2>{'\u2728'} Describe Your Agent</h2>
            <p style="color:var(--text-muted);font-size:13px;margin-bottom:16px">
              Tell us what you need in a few sentences. We'll generate a complete agent configuration using AI.
            </p>
            <textarea
              value={magicDesc}
              onInput={(e) => setMagicDesc(e.target.value)}
              placeholder="e.g., I need an agent that helps me with daily research tasks. It should search the web, analyze findings, and build a knowledge graph. It should be thorough but concise, and always cite sources."
              rows={5}
              style="width:100%;margin-bottom:12px"
            />
            <div style="display:flex;justify-content:space-between;align-items:center">
              <span style="font-size:11px;color:var(--text-muted)">Estimated cost: ~$0.02 per generation</span>
              <div style="display:flex;gap:8px">
                <button class="btn-secondary" onClick={() => setShowMagic(false)}>Cancel</button>
                <button class="btn-primary" onClick={generateFromDescription} disabled={generating || !magicDesc.trim()}>
                  {generating ? 'Generating...' : 'Generate Agent'}
                </button>
              </div>
            </div>
          </div>
        </div>
      )}

      {/* Avatar modal — shown when Save is clicked */}
      {showAvatar && (
        <div class="modal-overlay" onClick={() => setShowAvatar(false)}>
          <div class="modal-content" onClick={e => e.stopPropagation()} style="width:480px;text-align:center">
            <h2>Agent Avatar</h2>
            <p style="color:var(--text-muted);font-size:13px;margin-bottom:20px">
              Generate an avatar for your agent, or skip to save without one.
            </p>

            {/* Avatar preview */}
            <div style="width:120px;height:120px;border-radius:16px;overflow:hidden;background:var(--bg);border:2px solid var(--border);margin:0 auto 16px;display:flex;align-items:center;justify-content:center">
              {avatarURI ? (
                <img src={avatarURI} alt="Avatar" style="width:100%;height:100%;object-fit:cover" />
              ) : (
                <span style="font-size:48px;opacity:0.2">{'\u{1F916}'}</span>
              )}
            </div>

            {/* Style picker */}
            <div style="margin-bottom:16px">
              <select value={avatarStyle} onChange={(e) => setAvatarStyle(e.target.value)} style="width:200px">
                <option value="minimalist">Minimalist</option>
                <option value="abstract">Abstract</option>
                <option value="pixel">Pixel Art</option>
                <option value="anime">Anime</option>
                <option value="realistic">Realistic</option>
              </select>
            </div>

            {/* Generate button */}
            <div style="display:flex;gap:8px;justify-content:center;flex-wrap:wrap;margin-bottom:20px">
              <button class="btn-secondary" onClick={() => generateAvatar('gemini')} disabled={generatingAvatar}>
                Generate with Gemini Imagen
              </button>
            </div>
            {generatingAvatar && <p style="font-size:12px;color:var(--text-muted);margin-bottom:12px">Generating avatar...</p>}

            {/* Action buttons */}
            <div style="display:flex;gap:8px;justify-content:center;border-top:1px solid var(--border);padding-top:16px">
              <button class="btn-secondary" onClick={() => setShowAvatar(false)}>Back</button>
              <button class="btn-secondary" onClick={() => saveAgent(true)}>Skip Avatar</button>
              <button class="btn-primary" onClick={() => saveAgent(false)} disabled={saving}>
                {saving ? 'Saving...' : 'Save Agent'}
              </button>
            </div>
          </div>
        </div>
      )}
    </div>
  );
}

function MobileCreator({ config, schemas, activeNode, setActiveNode, getCompletionState, updateSection }) {
  const nodeOrder = ['identity', 'soul', 'behavior', 'skills', 'tools', 'memory', 'heartbeat', 'channels', 'bootstrap'];
  const stateColors = { complete: 'var(--success)', partial: 'var(--warning)', empty: 'var(--text-muted)' };
  const stateLabels = { complete: 'Configured', partial: 'Partial', empty: 'Not set' };

  return (
    <div>
      {nodeOrder.map(type => {
        const schema = schemas.find(s => s.type === type);
        if (!schema) return null;
        const state = getCompletionState(type);
        const isOpen = activeNode === type;

        return (
          <div key={type} class="card" style="margin-bottom:8px">
            <div
              style="display:flex;justify-content:space-between;align-items:center;cursor:pointer;padding:4px 0"
              onClick={() => setActiveNode(isOpen ? null : type)}
            >
              <div>
                <strong>{schema.title}</strong>
                <span style="color:var(--text-muted);font-size:12px;margin-left:8px">{schema.subtitle}</span>
              </div>
              <span style={`width:10px;height:10px;border-radius:50%;background:${stateColors[state]}`}
                title={stateLabels[state]} />
            </div>
            {isOpen && schema && (
              <div style="margin-top:12px;border-top:1px solid var(--border);padding-top:12px">
                <ConfigPanel
                  schema={schema}
                  values={config[type] || {}}
                  onChange={(values) => updateSection(type, values)}
                  onClose={() => setActiveNode(null)}
                  inline={true}
                />
              </div>
            )}
          </div>
        );
      })}
    </div>
  );
}
