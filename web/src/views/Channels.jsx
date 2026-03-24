import { h } from 'preact';
import { useState, useEffect } from 'preact/hooks';

const PLATFORMS = [
  { id: 'telegram', label: 'Telegram', icon: '✈', fields: [{ key: 'token', label: 'Bot Token', hint: 'Get from @BotFather on Telegram' }] },
  { id: 'discord', label: 'Discord', icon: '🎮', fields: [{ key: 'token', label: 'Bot Token', hint: 'Get from Discord Developer Portal' }] },
  { id: 'zalo', label: 'Zalo', icon: '💬', fields: [
    { key: 'oa_id', label: 'OA ID' },
    { key: 'secret_key', label: 'Secret Key' },
    { key: 'access_token', label: 'Access Token' },
  ]},
  { id: 'whatsapp', label: 'WhatsApp', icon: '📱', fields: [
    { key: 'phone_number_id', label: 'Phone Number ID' },
    { key: 'access_token', label: 'Access Token' },
    { key: 'verify_token', label: 'Verify Token' },
    { key: 'app_secret', label: 'App Secret' },
  ]},
];

export function Channels() {
  const [connections, setConnections] = useState([]);
  const [agents, setAgents] = useState([]);
  const [loading, setLoading] = useState(true);
  const [msg, setMsg] = useState('');
  const [msgType, setMsgType] = useState('success');

  // Filters
  const [filterPlatform, setFilterPlatform] = useState('');
  const [filterStatus, setFilterStatus] = useState('');

  // Add modal
  const [showAdd, setShowAdd] = useState(false);
  const [addPlatform, setAddPlatform] = useState('telegram');
  const [addToken, setAddToken] = useState('');
  const [addCreds, setAddCreds] = useState({});
  const [addLoading, setAddLoading] = useState(false);
  const [addPreview, setAddPreview] = useState(null);

  // Bind modal
  const [bindConn, setBindConn] = useState(null);
  const [bindAgent, setBindAgent] = useState('');

  // Pairing
  const [pairingCode, setPairingCode] = useState(null);
  const [pairingChannel, setPairingChannel] = useState(null);
  const [pairedDevices, setPairedDevices] = useState([]);

  const flash = (text, type = 'success') => {
    setMsg(text);
    setMsgType(type);
    setTimeout(() => setMsg(''), 6000);
  };

  const loadConnections = async () => {
    try {
      let url = '/api/v2/connections?';
      if (filterPlatform) url += `platform=${filterPlatform}&`;
      if (filterStatus) url += `status=${filterStatus}&`;
      const res = await fetch(url);
      if (res.ok) setConnections(await res.json());
    } catch {}
    setLoading(false);
  };

  const loadAgents = async () => {
    try {
      const res = await fetch('/api/v2/agents');
      if (res.ok) setAgents(await res.json());
    } catch {}
  };

  const loadPaired = () => fetch('/api/pairing').then(r => r.json()).then(data => setPairedDevices(data || [])).catch(() => {});

  useEffect(() => { loadConnections(); loadAgents(); loadPaired(); }, [filterPlatform, filterStatus]);

  // --- Add Connection ---
  const currentPlatform = PLATFORMS.find(p => p.id === addPlatform);
  const isMultiField = currentPlatform && currentPlatform.fields.length > 1;

  const handleAdd = async () => {
    let body;
    if (isMultiField) {
      // Multi-field credentials (Zalo, WhatsApp).
      const hasValues = Object.values(addCreds).some(v => v && v.trim());
      if (!hasValues) return;
      body = { platform: addPlatform, credentials: addCreds };
    } else {
      // Single token (Telegram, Discord).
      if (!addToken.trim()) return;
      body = { platform: addPlatform, token: addToken };
    }
    setAddLoading(true);
    try {
      const res = await fetch('/api/v2/connections', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(body),
      });
      const data = await res.json();
      if (res.ok) {
        flash(`Connection created: ${data.label || data.id}`);
        setShowAdd(false);
        setAddToken('');
        setAddCreds({});
        setAddPreview(null);
        loadConnections();
      } else {
        flash(data.error || 'Failed to create connection', 'error');
      }
    } catch (e) {
      flash('Failed: ' + e.message, 'error');
    }
    setAddLoading(false);
  };

  // --- Toggle DM/Group policy ---
  const togglePolicy = async (connId, field, currentValue) => {
    await fetch(`/api/v2/connections/${connId}`, {
      method: 'PUT',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ [field]: !currentValue }),
    });
    loadConnections();
  };

  // --- Delete ---
  const handleDelete = async (conn) => {
    if (!confirm(`Delete connection "${conn.label || conn.id}"? The adapter will be stopped and the credential removed.`)) return;
    try {
      const res = await fetch(`/api/v2/connections/${conn.id}`, { method: 'DELETE' });
      if (res.ok) {
        flash('Connection deleted');
        loadConnections();
      }
    } catch {}
  };

  // --- Stop / Start ---
  const toggleStatus = async (conn) => {
    const newStatus = conn.status === 'active' ? 'stopped' : 'active';
    await fetch(`/api/v2/connections/${conn.id}`, {
      method: 'PUT',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ status: newStatus }),
    });
    loadConnections();
  };

  // --- Bind Agent ---
  const handleBind = async () => {
    await fetch(`/api/v2/connections/${bindConn.id}`, {
      method: 'PUT',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ agent_id: bindAgent || null }),
    });
    setBindConn(null);
    setBindAgent('');
    loadConnections();
  };

  // --- Pairing ---
  const generatePairingCode = async (connId) => {
    try {
      const res = await fetch('/api/pairing/generate', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ channel: connId }),
      });
      const data = await res.json();
      if (data.code) {
        setPairingCode(data.code);
        setPairingChannel(connId);
      } else {
        flash(data.error || 'Failed to generate pairing code', 'error');
      }
    } catch {
      flash('Failed to generate pairing code', 'error');
    }
  };

  const deletePaired = async (id) => {
    await fetch(`/api/pairing/${id}`, { method: 'DELETE' });
    loadPaired();
  };

  const platformInfo = (id) => PLATFORMS.find(p => p.id === id) || { label: id, icon: '?' };

  const parseMetadata = (conn) => {
    try {
      return typeof conn.metadata === 'string' ? JSON.parse(conn.metadata) : (conn.metadata || {});
    } catch { return {}; }
  };

  return (
    <div>
      <div style="display:flex;justify-content:space-between;align-items:center;margin-bottom:16px">
        <div>
          <h1>Channels</h1>
          <p style="color:var(--text-muted);font-size:13px;margin-top:4px">
            Manage bot connections across messaging platforms.
          </p>
        </div>
        <button class="btn-primary" onClick={() => setShowAdd(true)}>Add Connection</button>
      </div>

      {msg && (
        <div class="card" style={`padding:0.75rem;margin-bottom:1rem;color:var(--${msgType === 'error' ? 'error' : 'success'})`}>
          {msg}
        </div>
      )}

      {/* Filters */}
      <div style="display:flex;gap:8px;margin-bottom:16px">
        <select value={filterPlatform} onChange={e => setFilterPlatform(e.target.value)}
          style="min-width:120px">
          <option value="">All Platforms</option>
          {PLATFORMS.map(p => <option key={p.id} value={p.id}>{p.label}</option>)}
        </select>
        <select value={filterStatus} onChange={e => setFilterStatus(e.target.value)}
          style="min-width:120px">
          <option value="">All Status</option>
          <option value="active">Active</option>
          <option value="stopped">Stopped</option>
          <option value="error">Error</option>
        </select>
      </div>

      {/* Connections list */}
      {loading ? (
        <p style="color:var(--text-muted)">Loading...</p>
      ) : connections.length === 0 ? (
        <div class="card" style="padding:2rem;text-align:center;color:var(--text-muted)">
          <p style="font-size:15px;margin-bottom:8px">No connections yet</p>
          <p style="font-size:13px">Click "Add Connection" to connect a bot from Telegram, Discord, or other platforms.</p>
        </div>
      ) : (
        <div style="display:flex;flex-direction:column;gap:8px">
          {connections.map(conn => {
            const plat = platformInfo(conn.platform);
            const meta = parseMetadata(conn);
            const isActive = conn.status === 'active';
            const isRunning = conn.running;
            const agentName = conn.agent_name || conn.agent_id;

            return (
              <div key={conn.id} class="card" style="padding:12px 16px">
                <div style="display:flex;justify-content:space-between;align-items:center">
                  <div style="display:flex;align-items:center;gap:12px">
                    <span style="font-size:20px" title={plat.label}>{plat.icon}</span>
                    <div>
                      <div style="display:flex;align-items:center;gap:8px">
                        <span style="font-weight:600;font-size:14px">{conn.label || conn.id}</span>
                        <span class={`badge ${isActive ? (isRunning ? 'badge-green' : 'badge-blue') : conn.status === 'error' ? 'badge-red' : 'badge-gray'}`}>
                          {isRunning ? 'running' : conn.status}
                        </span>
                        {!conn.agent_id && (
                          <span class="badge badge-yellow" title="No agent assigned — messages will be ignored">
                            unbound
                          </span>
                        )}
                      </div>
                      <div style="font-size:12px;color:var(--text-muted);margin-top:2px;display:flex;gap:12px;align-items:center">
                        <span>{plat.label}</span>
                        {meta.username && <span>@{meta.username}</span>}
                        {agentName && <span>Agent: {agentName}</span>}
                        <span style="font-family:var(--mono)">{conn.id}</span>
                        <span style="display:flex;gap:6px;margin-left:4px">
                          <label style="display:flex;align-items:center;gap:3px;cursor:pointer" title="Allow DM messages">
                            <input type="checkbox" checked={conn.dm_enabled !== false}
                              onChange={() => togglePolicy(conn.id, 'dm_enabled', conn.dm_enabled !== false)} />
                            <span style="font-size:11px">DM</span>
                          </label>
                          <label style="display:flex;align-items:center;gap:3px;cursor:pointer" title="Allow group messages (requires @mention)">
                            <input type="checkbox" checked={conn.group_enabled !== false}
                              onChange={() => togglePolicy(conn.id, 'group_enabled', conn.group_enabled !== false)} />
                            <span style="font-size:11px">Group</span>
                          </label>
                        </span>
                      </div>
                    </div>
                  </div>

                  <div style="display:flex;gap:6px;align-items:center">
                    <button class="btn-small" onClick={() => generatePairingCode(conn.id)}>Pair</button>
                    <button class="btn-small" onClick={() => { setBindConn(conn); setBindAgent(conn.agent_id || ''); }}>
                      {conn.agent_id ? 'Rebind' : 'Bind'}
                    </button>
                    <button class="btn-small" onClick={() => toggleStatus(conn)}>
                      {isActive ? 'Stop' : 'Start'}
                    </button>
                    <button class="btn-small btn-danger" onClick={() => handleDelete(conn)}>Delete</button>
                  </div>
                </div>
              </div>
            );
          })}
        </div>
      )}

      {/* Paired devices */}
      {pairedDevices.length > 0 && (
        <div style="margin-top:24px">
          <h2 style="font-size:16px;margin-bottom:12px">Paired Devices</h2>
          <table class="data-table">
            <thead>
              <tr><th>Channel</th><th>Chat ID</th><th>Paired At</th><th></th></tr>
            </thead>
            <tbody>
              {pairedDevices.map(d => (
                <tr key={d.id}>
                  <td>{d.channel}</td>
                  <td style="font-family:var(--mono);font-size:12px">{d.chat_id}</td>
                  <td style="color:var(--text-muted)">{d.paired_at?.slice(0, 19)}</td>
                  <td><button class="btn-small btn-danger" onClick={() => deletePaired(d.id)}>Unpair</button></td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}

      {/* Add Connection modal */}
      {showAdd && (
        <div class="modal-overlay" onClick={() => setShowAdd(false)}>
          <div class="modal-content" onClick={e => e.stopPropagation()}>
            <h2>Add Connection</h2>
            <p style="color:var(--text-muted);font-size:13px;margin-bottom:16px">
              Connect a bot to SageClaw. The platform will be queried to fetch bot info.
            </p>
            <div class="form-group">
              <label>Platform</label>
              <select value={addPlatform} onChange={e => { setAddPlatform(e.target.value); setAddToken(''); setAddCreds({}); }}>
                {PLATFORMS.map(p => <option key={p.id} value={p.id}>{p.icon} {p.label}</option>)}
              </select>
            </div>
            {isMultiField ? (
              currentPlatform.fields.map(f => (
                <div class="form-group" key={f.key}>
                  <label>{f.label}</label>
                  <input type="password" placeholder={f.label}
                    value={addCreds[f.key] || ''} onInput={e => setAddCreds({ ...addCreds, [f.key]: e.target.value })} />
                  {f.hint && <small style="color:var(--text-muted)">{f.hint}</small>}
                </div>
              ))
            ) : (
              <div class="form-group">
                <label>{currentPlatform?.fields[0]?.label || 'Bot Token'}</label>
                <input type="password" placeholder="Paste your bot token here"
                  value={addToken} onInput={e => setAddToken(e.target.value)} />
                {currentPlatform?.fields[0]?.hint && (
                  <small style="color:var(--text-muted)">{currentPlatform.fields[0].hint}</small>
                )}
              </div>
            )}

            {addPreview && (
              <div class="card" style="padding:12px;margin-bottom:12px">
                <p style="font-size:13px;color:var(--success)">Bot verified: {addPreview.label}</p>
              </div>
            )}

            <div style="display:flex;gap:0.5rem;margin-top:1rem">
              <button class="btn-primary" onClick={handleAdd} disabled={addLoading || (isMultiField ? !Object.values(addCreds).some(v => v && v.trim()) : !addToken.trim())}>
                {addLoading ? 'Connecting...' : 'Connect'}
              </button>
              <button class="btn-secondary" onClick={() => { setShowAdd(false); setAddToken(''); setAddCreds({}); setAddPreview(null); }}>
                Cancel
              </button>
            </div>
          </div>
        </div>
      )}

      {/* Bind Agent modal */}
      {bindConn && (
        <div class="modal-overlay" onClick={() => setBindConn(null)}>
          <div class="modal-content" onClick={e => e.stopPropagation()}>
            <h2>Bind Agent to {bindConn.label || bindConn.id}</h2>
            <p style="color:var(--text-muted);font-size:13px;margin-bottom:16px">
              Messages from this connection will be routed to the selected agent.
            </p>
            <div class="form-group">
              <label>Agent</label>
              <select value={bindAgent} onChange={e => setBindAgent(e.target.value)}>
                <option value="">-- No Agent (unbound) --</option>
                {agents.map(a => (
                  <option key={a.id} value={a.id}>{a.name || a.id}</option>
                ))}
              </select>
            </div>
            <div style="display:flex;gap:0.5rem;margin-top:1rem">
              <button class="btn-primary" onClick={handleBind}>Save</button>
              <button class="btn-secondary" onClick={() => setBindConn(null)}>Cancel</button>
            </div>
          </div>
        </div>
      )}

      {/* Pairing code modal */}
      {pairingCode && (
        <div class="modal-overlay" onClick={() => { setPairingCode(null); setPairingChannel(null); }}>
          <div class="modal-content" onClick={e => e.stopPropagation()} style="text-align:center">
            <h2>Pairing Code</h2>
            <p style="color:var(--text-muted);font-size:13px;margin-bottom:16px">
              Send this code to your bot on {pairingChannel} to pair your device.
            </p>
            <div style="font-family:var(--mono);font-size:32px;font-weight:700;letter-spacing:4px;color:var(--primary);padding:24px;background:var(--bg);border-radius:8px;margin-bottom:16px">
              {pairingCode}
            </div>
            <p style="color:var(--text-muted);font-size:12px">
              This code expires in 5 minutes.
            </p>
            <button class="btn-secondary" style="margin-top:16px"
              onClick={() => { setPairingCode(null); setPairingChannel(null); loadPaired(); }}>
              Done
            </button>
          </div>
        </div>
      )}
    </div>
  );
}
