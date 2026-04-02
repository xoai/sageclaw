import { h } from 'preact';
import { useState, useEffect } from 'preact/hooks';
import { route } from 'preact-router';
import { Breadcrumb } from '../components/Breadcrumb';

/**
 * AgentCreator — simplified creation page.
 * Two paths: (1) pick a preset, (2) describe with AI.
 * Both create the agent via quick-create then redirect to the editor.
 */
export default function AgentCreator() {
  const [mode, setMode] = useState('pick'); // 'pick' | 'describe' | 'preview'
  const [presets, setPresets] = useState([]);
  const [description, setDescription] = useState('');
  const [generating, setGenerating] = useState(false);
  const [preview, setPreview] = useState(null);
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState('');

  useEffect(() => {
    fetch('/api/v2/agents/presets', { credentials: 'include' })
      .then(r => r.json())
      .then(data => setPresets(data || []))
      .catch(() => {});
  }, []);

  const presetIcons = {
    search: '\uD83D\uDD0D', code: '\uD83D\uDCBB', pen: '\u270D\uFE0F',
    tasks: '\uD83C\uDFAF', chart: '\uD83D\uDCCA', star: '\u2B50',
  };

  // Create agent from preset directly via quick-create.
  const createFromPreset = async (preset) => {
    setSaving(true);
    setError('');
    try {
      const res = await fetch('/api/v2/agents/quick-create', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        credentials: 'include',
        body: JSON.stringify({
          name: preset.name,
          role: preset.description,
          model: 'strong',
          tool_profile: 'full',
        }),
      });
      const data = await res.json();
      if (data.error) { setError(data.error); setSaving(false); return; }
      route(`/agents/${data.id}`);
    } catch (e) {
      setError('Failed: ' + e.message);
      setSaving(false);
    }
  };

  // Generate agent config from description via LLM.
  const generate = async () => {
    if (!description.trim()) return;
    setGenerating(true);
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
        setGenerating(false);
        return;
      }
      setPreview(data.config);
      setMode('preview');
    } catch (e) {
      setError('Generation failed: ' + e.message);
    }
    setGenerating(false);
  };

  // Save the previewed config via quick-create, then redirect to editor.
  const savePreview = async () => {
    if (!preview) return;
    setSaving(true);
    setError('');
    try {
      const res = await fetch('/api/v2/agents/quick-create', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        credentials: 'include',
        body: JSON.stringify(preview),
      });
      const data = await res.json();
      if (data.error) { setError(data.error); setSaving(false); return; }
      route(`/agents/${data.id}`);
    } catch (e) {
      setError('Failed: ' + e.message);
      setSaving(false);
    }
  };

  return (
    <div>
      <Breadcrumb items={[{ label: 'Agents', href: '/agents' }, { label: 'Create' }]} />
      <h1 style="margin-bottom:4px">Create an agent</h1>
      <p style="color:var(--text-muted);font-size:var(--text-sm);margin-bottom:24px">
        Pick a preset or describe what you need.
      </p>

      {error && (
        <div style="color:var(--error);font-size:var(--text-sm);margin-bottom:12px;padding:8px 12px;background:color-mix(in srgb, var(--error) 10%, transparent);border-radius:8px">
          {error}
        </div>
      )}

      {/* === Pick mode: presets + describe option === */}
      {mode === 'pick' && (
        <div class="panel-enter">
          {/* Presets grid */}
          <div style="font-size:var(--text-xs);text-transform:uppercase;letter-spacing:0.06em;color:var(--text-muted);margin-bottom:10px;font-weight:600">
            Presets
          </div>
          <div style="display:grid;grid-template-columns:repeat(auto-fill,minmax(200px,1fr));gap:10px;margin-bottom:28px">
            {presets.map(p => (
              <button key={p.id} class="card clickable" onClick={() => createFromPreset(p)} disabled={saving}
                style="padding:20px;cursor:pointer;text-align:center;border:1px solid var(--border);background:var(--surface);width:100%">
                <div style="font-size:28px;margin-bottom:8px">
                  {presetIcons[p.icon] || '\u2B50'}
                </div>
                <div style="font-weight:600;font-size:var(--text-base)">{p.name}</div>
                <div style="font-size:var(--text-xs);color:var(--text-muted);margin-top:4px;line-height:1.4">{p.description}</div>
              </button>
            ))}
          </div>

          {saving && <div style="color:var(--text-muted);font-size:var(--text-sm);margin-bottom:12px">Creating agent...</div>}

          {/* Describe with AI */}
          <div style="font-size:var(--text-xs);text-transform:uppercase;letter-spacing:0.06em;color:var(--text-muted);margin-bottom:10px;font-weight:600">
            Or describe what you need
          </div>
          <div class="chat-composer" style="max-width:560px">
            <textarea
              placeholder="A research assistant that helps me find and summarize academic papers..."
              value={description}
              onInput={e => setDescription(e.target.value)}
              onKeyDown={e => { if (e.key === 'Enter' && !e.shiftKey && description.trim()) { e.preventDefault(); generate(); } }}
              rows={3}
            />
            <div style="display:flex;justify-content:flex-end;padding-top:6px">
              <button class="btn-primary" onClick={generate} disabled={generating || !description.trim()}>
                {generating ? 'Generating...' : 'Create with AI'}
              </button>
            </div>
          </div>
        </div>
      )}

      {/* === Preview mode: show generated config before saving === */}
      {mode === 'preview' && preview && (
        <div class="panel-enter" style="max-width:480px">
          <div class="card" style="padding:20px;margin-bottom:16px">
            <div style="display:flex;align-items:center;gap:12px;margin-bottom:14px">
              <span style="font-size:32px">{preview.avatar || '\u2B50'}</span>
              <div style="flex:1">
                <input
                  type="text"
                  value={preview.name || ''}
                  onInput={e => setPreview({ ...preview, name: e.target.value })}
                  style="font-weight:600;font-size:var(--text-lg);background:transparent;border:none;color:var(--text);width:100%;padding:0"
                  placeholder="Agent name"
                />
                <input
                  type="text"
                  value={preview.role || ''}
                  onInput={e => setPreview({ ...preview, role: e.target.value })}
                  style="font-size:var(--text-sm);color:var(--text-muted);background:transparent;border:none;width:100%;padding:0;margin-top:2px"
                  placeholder="What does this agent do?"
                />
              </div>
            </div>

            <div style="display:flex;gap:6px;margin-bottom:14px;flex-wrap:wrap">
              <span class="badge badge-blue">{preview.model || 'strong'}</span>
              <span class="badge" style="background:var(--surface-hover);color:var(--text-muted)">{preview.tool_profile || 'full'}</span>
            </div>

            {preview.examples && preview.examples.length > 0 && (
              <div>
                <div style="font-size:var(--text-xs);color:var(--text-muted);margin-bottom:6px">Suggested prompts</div>
                <div style="display:flex;flex-direction:column;gap:4px">
                  {preview.examples.map((ex, i) => (
                    <div key={i} style="font-size:var(--text-xs);color:var(--text-muted);padding:4px 8px;background:var(--surface-hover);border-radius:4px">
                      {ex}
                    </div>
                  ))}
                </div>
              </div>
            )}
          </div>

          <div style="display:flex;gap:8px">
            <button class="btn-secondary" onClick={() => { setMode('pick'); setPreview(null); }}>
              Start over
            </button>
            <button class="btn-primary" onClick={savePreview} disabled={saving || !preview.name}>
              {saving ? 'Creating...' : 'Looks good, create it'}
            </button>
          </div>
        </div>
      )}
    </div>
  );
}
