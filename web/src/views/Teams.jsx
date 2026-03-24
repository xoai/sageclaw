import { h } from 'preact';
import { useState, useEffect } from 'preact/hooks';
import { Label } from '../components/InfoTip';

export function Teams() {
  const [teams, setTeams] = useState([]);
  const [tasks, setTasks] = useState({});
  const [showModal, setShowModal] = useState(false);
  const [editing, setEditing] = useState(null);
  const [form, setForm] = useState({ name: '', lead_id: '', members: [] });
  const [agents, setAgents] = useState([]);
  const [memberSearch, setMemberSearch] = useState('');

  const load = () => {
    fetch('/api/teams').then(r => r.json()).then(data => {
      setTeams(data || []);
      (data || []).forEach(team => {
        fetch(`/api/teams/tasks/${team.id}`).then(r => r.json())
          .then(t => setTasks(prev => ({ ...prev, [team.id]: t || [] })))
          .catch(() => {});
      });
    }).catch(() => {});
  };

  const loadAgents = () => {
    fetch('/api/v2/agents').then(r => r.json()).then(data => setAgents(data || [])).catch(() =>
      fetch('/api/agents').then(r => r.json()).then(data => setAgents(data || [])).catch(() => {})
    );
  };

  useEffect(() => { load(); loadAgents(); }, []);

  const openCreate = () => {
    setEditing(null);
    setForm({ name: '', lead_id: '', members: [] });
    setMemberSearch('');
    setShowModal(true);
  };

  const openEdit = (team) => {
    setEditing(team.id);
    let members = [];
    try {
      const cfg = JSON.parse(team.config || '{}');
      members = cfg.members || [];
    } catch {}
    setForm({ name: team.name, lead_id: team.lead, members });
    setMemberSearch('');
    setShowModal(true);
  };

  const save = async () => {
    const body = { name: form.name, lead_id: form.lead_id, members: form.members };
    if (editing) {
      await fetch(`/api/teams/${editing}`, {
        method: 'PUT', headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(body),
      });
    } else {
      await fetch('/api/teams', {
        method: 'POST', headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(body),
      });
    }
    setShowModal(false);
    load();
  };

  const del = async (id) => {
    if (!confirm('Delete this team and all its tasks?')) return;
    await fetch(`/api/teams/${id}`, { method: 'DELETE' });
    load();
  };

  const addMember = (id) => {
    if (!form.members.includes(id)) {
      setForm({ ...form, members: [...form.members, id] });
    }
    setMemberSearch('');
  };

  const removeMember = (id) => {
    setForm({ ...form, members: form.members.filter(m => m !== id) });
  };

  // Filter agents for member search (exclude lead and already-added members).
  const availableAgents = agents.filter(a =>
    a.id !== form.lead_id &&
    !form.members.includes(a.id) &&
    (memberSearch === '' || a.name?.toLowerCase().includes(memberSearch.toLowerCase()) || a.id.includes(memberSearch.toLowerCase()))
  );

  const statusColor = {
    open: 'badge-blue', claimed: 'badge-gray',
    completed: 'badge-green', blocked: 'badge-red',
  };

  return (
    <div>
      <div style="display:flex;justify-content:space-between;align-items:center;margin-bottom:1.5rem">
        <h1>Teams</h1>
        <button class="btn-primary" onClick={openCreate}>+ Create Team</button>
      </div>

      {teams.length === 0 ? (
        <div class="empty">No teams yet. Create one to coordinate multiple agents on tasks.</div>
      ) : (
        teams.map(team => (
          <div key={team.id} style="margin-bottom:24px">
            <div class="card">
              <div style="display:flex;justify-content:space-between;align-items:center">
                <div>
                  <h3 style="font-size:16px;font-weight:600">{team.name}</h3>
                  <div style="font-size:12px;color:var(--text-muted);margin-top:4px">
                    Lead: <strong>{team.lead}</strong> · {team.members || 0} members
                  </div>
                </div>
                <div style="display:flex;gap:0.5rem">
                  <button class="btn-small" onClick={() => openEdit(team)}>Edit</button>
                  <button class="btn-small btn-danger" onClick={() => del(team.id)}>Delete</button>
                </div>
              </div>
            </div>

            {(tasks[team.id] || []).length > 0 && (
              <table class="data-table" style="margin-top:4px">
                <thead><tr><th scope="col">Status</th><th scope="col">Title</th><th scope="col">Assigned</th><th scope="col">Created By</th></tr></thead>
                <tbody>
                  {(tasks[team.id] || []).map(task => (
                    <tr key={task.id}>
                      <td><span class={`badge ${statusColor[task.status] || 'badge-gray'}`}>{task.status}</span></td>
                      <td>{task.title}</td>
                      <td style="color:var(--text-muted)">{task.assigned_to || '\u2014'}</td>
                      <td style="color:var(--text-muted)">{task.created_by}</td>
                    </tr>
                  ))}
                </tbody>
              </table>
            )}
          </div>
        ))
      )}

      {showModal && (
        <div class="modal-overlay" onClick={() => setShowModal(false)} role="dialog" aria-modal="true" aria-labelledby="create-team-title">
          <div class="modal-content" onClick={e => e.stopPropagation()}>
            <h2 id="create-team-title">{editing ? 'Edit Team' : 'Create Team'}</h2>

            <div class="form-group">
              <Label text="Team Name" tip="A descriptive name for this agent team." />
              <input type="text" placeholder="e.g. Research Team" value={form.name}
                onInput={e => setForm({ ...form, name: e.target.value })} />
            </div>

            <div class="form-group">
              <Label text="Lead Agent" tip="The orchestrator who creates and assigns tasks. The lead cannot use the team mailbox." />
              <select value={form.lead_id} onChange={e => setForm({ ...form, lead_id: e.target.value })}>
                <option value="">Select lead agent...</option>
                {agents.map(a => <option key={a.id} value={a.id}>{a.name || a.id} ({a.id})</option>)}
              </select>
            </div>

            <div class="form-group">
              <Label text="Member Agents" tip="Team members who execute tasks. They communicate via the team mailbox." />

              {/* Selected members as chips */}
              {form.members.length > 0 && (
                <div class="chip-list">
                  {form.members.map(id => {
                    const agent = agents.find(a => a.id === id);
                    return (
                      <span key={id} class="chip">
                        {agent?.name || id}
                        <span class="chip-remove" onClick={() => removeMember(id)}>&times;</span>
                      </span>
                    );
                  })}
                </div>
              )}

              {/* Search + dropdown */}
              <div style="position:relative">
                <input type="text" placeholder="Search agents to add..." value={memberSearch}
                  onInput={e => setMemberSearch(e.target.value)}
                  onFocus={() => setMemberSearch(memberSearch || '')} />

                {memberSearch !== '' && availableAgents.length > 0 && (
                  <div style="position:absolute;top:100%;left:0;right:0;background:var(--surface);border:1px solid var(--border);border-radius:0 0 6px 6px;max-height:200px;overflow-y:auto;z-index:10">
                    {availableAgents.slice(0, 10).map(a => (
                      <div key={a.id} style="padding:8px 12px;cursor:pointer;font-size:13px"
                        onMouseDown={() => addMember(a.id)}
                        onMouseEnter={e => e.target.style.background = 'var(--surface-hover)'}
                        onMouseLeave={e => e.target.style.background = ''}>
                        <strong>{a.name || a.id}</strong>
                        <span style="color:var(--text-muted);margin-left:8px">{a.id}</span>
                      </div>
                    ))}
                  </div>
                )}

                {memberSearch !== '' && availableAgents.length === 0 && (
                  <div style="position:absolute;top:100%;left:0;right:0;background:var(--surface);border:1px solid var(--border);border-radius:0 0 6px 6px;padding:8px 12px;font-size:12px;color:var(--text-muted);z-index:10">
                    No matching agents found.
                  </div>
                )}
              </div>
            </div>

            <div style="display:flex;gap:0.5rem;margin-top:1rem">
              <button class="btn-primary" onClick={save} disabled={!form.name || !form.lead_id}>
                {editing ? 'Save' : 'Create'}
              </button>
              <button class="btn-secondary" onClick={() => setShowModal(false)}>Cancel</button>
            </div>
          </div>
        </div>
      )}
    </div>
  );
}
