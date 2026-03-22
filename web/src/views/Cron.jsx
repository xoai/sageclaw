import { h } from 'preact';
import { useState, useEffect } from 'preact/hooks';
import { rpc } from '../api';

export default function Cron() {
  const [jobs, setJobs] = useState([]);
  const [showModal, setShowModal] = useState(false);
  const [form, setForm] = useState({ agent_id: 'default', schedule: '', prompt: '' });

  const load = () => {
    fetch('/api/cron').then(r => r.json()).then(setJobs).catch(() => {});
  };

  useEffect(load, []);

  const create = async () => {
    await fetch('/api/cron', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(form),
    });
    setShowModal(false);
    setForm({ agent_id: 'default', schedule: '', prompt: '' });
    load();
  };

  const del = async (id) => {
    if (!confirm('Delete this cron job?')) return;
    await fetch(`/api/cron/${id}`, { method: 'DELETE' });
    load();
  };

  const trigger = async (id) => {
    await fetch(`/api/cron/${id}/trigger`, { method: 'POST' });
    load();
  };

  const presets = [
    { label: 'Every hour', value: '0 * * * *' },
    { label: 'Every day 9am', value: '0 9 * * *' },
    { label: 'Every week Mon', value: '0 9 * * 1' },
    { label: 'Every 30 min', value: '*/30 * * * *' },
  ];

  return (
    <div>
      <div style="display:flex;justify-content:space-between;align-items:center;margin-bottom:1.5rem">
        <h1>Cron Jobs</h1>
        <button class="btn-primary" onClick={() => setShowModal(true)}>+ Add Job</button>
      </div>

      {jobs.length === 0 ? (
        <p style="color:#8899a6;text-align:center;margin-top:3rem">No cron jobs scheduled.</p>
      ) : (
        <table class="data-table">
          <thead>
            <tr>
              <th>Schedule</th>
              <th>Prompt</th>
              <th>Agent</th>
              <th>Last Run</th>
              <th>Status</th>
              <th>Actions</th>
            </tr>
          </thead>
          <tbody>
            {jobs.map(j => (
              <tr key={j.id}>
                <td><code>{j.schedule}</code></td>
                <td>{j.prompt.length > 60 ? j.prompt.slice(0, 60) + '...' : j.prompt}</td>
                <td>{j.agent_id}</td>
                <td>{j.last_run || 'Never'}</td>
                <td><span class={`badge ${j.enabled ? 'badge-green' : 'badge-gray'}`}>{j.enabled ? 'Active' : 'Disabled'}</span></td>
                <td>
                  <button class="btn-small" onClick={() => trigger(j.id)}>Trigger</button>{' '}
                  <button class="btn-small btn-danger" onClick={() => del(j.id)}>Delete</button>
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      )}

      {showModal && (
        <div class="modal-overlay" onClick={() => setShowModal(false)}>
          <div class="modal-content" onClick={e => e.stopPropagation()}>
            <h2>New Cron Job</h2>
            <div class="form-group">
              <label>Schedule</label>
              <select onChange={e => { if (e.target.value) setForm({ ...form, schedule: e.target.value }); }}>
                <option value="">Select preset...</option>
                {presets.map(p => <option value={p.value}>{p.label} ({p.value})</option>)}
              </select>
              <input type="text" placeholder="Or enter cron expression..."
                value={form.schedule} onInput={e => setForm({ ...form, schedule: e.target.value })} />
            </div>
            <div class="form-group">
              <label>Prompt</label>
              <textarea rows="4" placeholder="What should the agent do?"
                value={form.prompt} onInput={e => setForm({ ...form, prompt: e.target.value })} />
            </div>
            <div class="form-group">
              <label>Agent ID</label>
              <input type="text" value={form.agent_id}
                onInput={e => setForm({ ...form, agent_id: e.target.value })} />
            </div>
            <div style="display:flex;gap:0.5rem;margin-top:1rem">
              <button class="btn-primary" onClick={create} disabled={!form.schedule || !form.prompt}>Create</button>
              <button class="btn-secondary" onClick={() => setShowModal(false)}>Cancel</button>
            </div>
          </div>
        </div>
      )}
    </div>
  );
}
