import { h } from 'preact';
import { useState } from 'preact/hooks';

/**
 * MagicCreate — AI-powered agent creation flow.
 *
 * States: idle → describing → generating → preview → saving
 *
 * @param {Object} props
 * @param {(agent: {id, name}) => void} props.onCreated - Called after agent is saved.
 * @param {() => void} props.onCancel - Called when user cancels.
 */
export function MagicCreate({ onCreated, onCancel }) {
  const [step, setStep] = useState('describe'); // describe | generating | preview
  const [description, setDescription] = useState('');
  const [config, setConfig] = useState(null);
  const [error, setError] = useState('');
  const [saving, setSaving] = useState(false);

  const generate = async () => {
    if (!description.trim()) return;
    setStep('generating');
    setError('');

    try {
      const res = await fetch('/api/v2/agents/generate', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        credentials: 'include',
        body: JSON.stringify({ description: description.trim() }),
      });
      const data = await res.json();
      if (data.error && !data.config) {
        setError(data.error);
        setStep('describe');
        return;
      }
      setConfig(data.config);
      setStep('preview');
    } catch (err) {
      setError('Failed to generate. Try again.');
      setStep('describe');
    }
  };

  const quickPreset = async (presetName) => {
    setStep('generating');
    setError('');

    // Use the generate endpoint with a preset description for simplicity.
    const presetDescriptions = {
      'Research': 'A research assistant that helps find information, analyze data, and build knowledge',
      'Writing': 'A writing assistant that helps create content, blog posts, articles, and documentation',
      'Coding': 'A coding assistant that helps write, review, and debug code across languages',
      'General': 'A general-purpose AI assistant for everyday tasks and questions',
    };

    const desc = presetDescriptions[presetName] || presetDescriptions['General'];

    // Quick-create directly from preset config without LLM.
    const presetConfigs = {
      'Research': { name: 'Researcher', role: 'Research and analysis assistant', avatar: '\uD83D\uDD0D', model: 'strong', tool_profile: 'full' },
      'Writing': { name: 'Writer', role: 'Content creation and editing assistant', avatar: '\u270D\uFE0F', model: 'strong', tool_profile: 'full' },
      'Coding': { name: 'Developer', role: 'Software development assistant', avatar: '\uD83D\uDCBB', model: 'strong', tool_profile: 'coding' },
      'General': { name: 'Assistant', role: 'General-purpose AI assistant', avatar: '\u2B50', model: 'strong', tool_profile: 'full' },
    };

    const cfg = presetConfigs[presetName] || presetConfigs['General'];
    setConfig(cfg);
    setStep('preview');
  };

  const save = async () => {
    if (!config) return;
    setSaving(true);
    setError('');

    try {
      const res = await fetch('/api/v2/agents/quick-create', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        credentials: 'include',
        body: JSON.stringify(config),
      });
      const data = await res.json();
      if (data.error) {
        setError(data.error);
        setSaving(false);
        return;
      }
      onCreated({ id: data.id, name: config.name });
    } catch (err) {
      setError('Failed to save agent.');
      setSaving(false);
    }
  };

  const handleKeyDown = (e) => {
    if (e.key === 'Enter' && !e.shiftKey) {
      e.preventDefault();
      generate();
    }
  };

  // --- Describe step ---
  if (step === 'describe') {
    return (
      <div class="magic-create">
        <div class="magic-create-header">
          <span style="font-size:var(--text-lg);font-weight:600">Create an agent</span>
          <button class="btn-secondary" onClick={onCancel} style="padding:4px 10px;font-size:var(--text-xs)">Cancel</button>
        </div>
        <p style="color:var(--text-muted);font-size:var(--text-sm);margin-bottom:16px">
          Describe what you need help with, or start from a preset.
        </p>

        <div style="display:flex;gap:8px;margin-bottom:16px;flex-wrap:wrap">
          {['Research', 'Writing', 'Coding', 'General'].map(p => (
            <button key={p} class="btn-secondary" onClick={() => quickPreset(p)}
              style="padding:6px 14px;font-size:13px">
              {p}
            </button>
          ))}
        </div>

        <div style="display:flex;gap:8px">
          <input
            type="text"
            class="chat-input"
            placeholder="A research assistant for academic papers..."
            value={description}
            onInput={e => setDescription(e.target.value)}
            onKeyDown={handleKeyDown}
            autofocus
          />
          <button class="btn-primary" onClick={generate} disabled={!description.trim()}>
            Generate
          </button>
        </div>

        {error && <p style="color:var(--error);font-size:13px;margin-top:8px">{error}</p>}
      </div>
    );
  }

  // --- Generating step ---
  if (step === 'generating') {
    return (
      <div class="magic-create">
        <div class="magic-create-header">
          <span style="font-size:var(--text-lg);font-weight:600">Setting things up...</span>
        </div>
        <div class="empty" style="padding:32px">
          <span class="thinking-dots">Creating your agent</span>
        </div>
      </div>
    );
  }

  // --- Preview step ---
  if (step === 'preview' && config) {
    return (
      <div class="magic-create">
        <div class="magic-create-header">
          <span style="font-size:var(--text-lg);font-weight:600">Here's what we made</span>
          <button class="btn-secondary" onClick={() => setStep('describe')} style="padding:4px 10px;font-size:var(--text-xs)">Back</button>
        </div>

        <div class="card" style="padding:16px;margin-bottom:16px">
          <div style="display:flex;align-items:center;gap:12px;margin-bottom:12px">
            <span style="font-size:28px">{config.avatar || '\u2B50'}</span>
            <div>
              <input
                type="text"
                value={config.name || ''}
                onInput={e => setConfig({ ...config, name: e.target.value })}
                style="font-weight:600;font-size:16px;background:transparent;border:none;color:var(--text);width:100%;padding:0"
              />
              <input
                type="text"
                value={config.role || ''}
                onInput={e => setConfig({ ...config, role: e.target.value })}
                style="font-size:13px;color:var(--text-muted);background:transparent;border:none;width:100%;padding:0;margin-top:2px"
              />
            </div>
          </div>

          <div style="display:flex;gap:8px;margin-bottom:12px;flex-wrap:wrap">
            <span class="badge badge-blue">{config.model || 'strong'}</span>
            <span class="badge badge-neutral">{config.tool_profile || 'full'}</span>
          </div>

          {config.examples && config.examples.length > 0 && (
            <div>
              <div style="font-size:var(--text-xs);color:var(--text-muted);margin-bottom:6px">Suggested prompts</div>
              <div style="display:flex;flex-direction:column;gap:4px">
                {config.examples.map((ex, i) => (
                  <div key={i} style="font-size:12px;color:var(--text-muted);padding:4px 8px;background:var(--surface);border-radius:4px">
                    {ex}
                  </div>
                ))}
              </div>
            </div>
          )}
        </div>

        {error && <p style="color:var(--error);font-size:13px;margin-bottom:8px">{error}</p>}

        <div style="display:flex;gap:8px;justify-content:flex-end">
          <button class="btn-secondary" onClick={() => setStep('describe')}>Start over</button>
          <button class="btn-primary" onClick={save} disabled={saving || !config.name}>
            {saving ? 'Creating...' : 'Looks good, create it'}
          </button>
        </div>
      </div>
    );
  }

  return null;
}
