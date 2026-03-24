import { h } from 'preact';
import { useState, useEffect } from 'preact/hooks';

export default function MCPServers() {
  const [servers, setServers] = useState([]);
  const [loading, setLoading] = useState(true);
  const [showAdd, setShowAdd] = useState(false);
  const [msg, setMsg] = useState('');
  const [msgType, setMsgType] = useState('');

  // Add form state.
  const [addName, setAddName] = useState('');
  const [addTransport, setAddTransport] = useState('stdio');
  const [addCommand, setAddCommand] = useState('');
  const [addArgs, setAddArgs] = useState('');
  const [addURL, setAddURL] = useState('');
  const [addTrust, setAddTrust] = useState('untrusted');
  const [addPrefix, setAddPrefix] = useState('');

  const loadServers = async () => {
    try {
      const res = await fetch('/api/mcp/servers', { credentials: 'include' });
      if (res.ok) setServers(await res.json());
    } catch {}
    setLoading(false);
  };

  useEffect(() => { loadServers(); }, []);

  const flash = (text, type) => {
    setMsg(text);
    setMsgType(type);
    setTimeout(() => setMsg(''), 4000);
  };

  const addServer = async () => {
    if (!addName) return flash('Name is required', 'error');

    const config = {
      transport: addTransport,
      trust: addTrust,
    };

    if (addTransport === 'stdio') {
      if (!addCommand) return flash('Command is required for stdio', 'error');
      config.command = addCommand;
      config.args = addArgs ? addArgs.split(/\s+/) : [];
    } else {
      if (!addURL) return flash('URL is required for ' + addTransport, 'error');
      config.url = addURL;
    }

    if (addPrefix) config.tool_prefix = addPrefix;

    try {
      const res = await fetch('/api/mcp/servers', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        credentials: 'include',
        body: JSON.stringify({ name: addName, config }),
      });
      if (res.ok) {
        flash('Server connected', 'success');
        setShowAdd(false);
        resetForm();
        loadServers();
      } else {
        const text = await res.text();
        flash(text || 'Failed to add server', 'error');
      }
    } catch (e) {
      flash(e.message, 'error');
    }
  };

  const removeServer = async (name) => {
    if (!confirm(`Remove MCP server "${name}"?`)) return;
    try {
      const res = await fetch('/api/mcp/servers/' + encodeURIComponent(name), {
        method: 'DELETE',
        credentials: 'include',
      });
      if (res.ok) {
        flash('Server removed', 'success');
        loadServers();
      }
    } catch {}
  };

  const resetForm = () => {
    setAddName('');
    setAddTransport('stdio');
    setAddCommand('');
    setAddArgs('');
    setAddURL('');
    setAddTrust('untrusted');
    setAddPrefix('');
  };

  if (loading) return <div class="empty">Loading...</div>;

  return (
    <div>
      <div style="display:flex;justify-content:space-between;align-items:center;margin-bottom:1.5rem">
        <div>
          <h1 style="margin-bottom:0">MCP Servers</h1>
          <p style="color:var(--text-muted);margin-top:4px">{servers.length} server{servers.length !== 1 ? 's' : ''} connected</p>
        </div>
        <button class="btn-primary" onClick={() => setShowAdd(true)}>Add Server</button>
      </div>

      {msg && (
        <div class="card" style={`padding:10px;margin-bottom:12px;border-color:var(--${msgType === 'error' ? 'error' : 'success'})`}>
          <span style={`color:var(--${msgType === 'error' ? 'error' : 'success'})`}>{msg}</span>
        </div>
      )}

      {servers.length === 0 && (
        <div class="empty">
          <p>No MCP servers connected.</p>
          <p style="color:var(--text-muted)">Add servers via the dashboard or in agent tools.yaml config.</p>
        </div>
      )}

      <div class="card-list">
        {servers.map(s => (
          <div key={s.name} class="card" style="padding:1rem">
            <div style="display:flex;justify-content:space-between;align-items:center">
              <div style="display:flex;align-items:center;gap:8px">
                <span style={`width:8px;height:8px;border-radius:50%;background:var(--${s.healthy ? 'success' : 'error'})`} />
                <code style="font-size:1rem;color:var(--primary)">{s.name}</code>
                <span class="badge badge-gray">{s.transport || 'stdio'}</span>
                <span class={`badge ${s.trust === 'trusted' ? 'badge-green' : 'badge-yellow'}`}>
                  {s.trust || 'untrusted'}
                </span>
              </div>
              <div style="display:flex;align-items:center;gap:12px">
                <span style="color:var(--text-muted);font-size:0.85rem">{s.tool_count} tools</span>
                <button class="btn-small btn-danger" onClick={() => removeServer(s.name)}>Remove</button>
              </div>
            </div>
          </div>
        ))}
      </div>

      {showAdd && (
        <div class="modal-overlay" onClick={() => setShowAdd(false)}>
          <div class="modal-content" onClick={e => e.stopPropagation()} style="max-width:500px">
            <h2 style="margin-top:0">Add MCP Server</h2>

            <label style="display:block;margin-bottom:12px">
              <span style="color:var(--text-muted);font-size:0.85rem">Name</span>
              <input type="text" value={addName} onInput={e => setAddName(e.target.value)}
                placeholder="e.g. filesystem" style="width:100%;margin-top:4px" />
            </label>

            <label style="display:block;margin-bottom:12px">
              <span style="color:var(--text-muted);font-size:0.85rem">Transport</span>
              <select value={addTransport} onChange={e => setAddTransport(e.target.value)} style="width:100%;margin-top:4px">
                <option value="stdio">stdio (local process)</option>
                <option value="sse">SSE (Server-Sent Events)</option>
                <option value="streamable-http">Streamable HTTP</option>
              </select>
            </label>

            {addTransport === 'stdio' && (
              <div>
                <label style="display:block;margin-bottom:12px">
                  <span style="color:var(--text-muted);font-size:0.85rem">Command</span>
                  <input type="text" value={addCommand} onInput={e => setAddCommand(e.target.value)}
                    placeholder="e.g. npx" style="width:100%;margin-top:4px" />
                </label>
                <label style="display:block;margin-bottom:12px">
                  <span style="color:var(--text-muted);font-size:0.85rem">Arguments (space-separated)</span>
                  <input type="text" value={addArgs} onInput={e => setAddArgs(e.target.value)}
                    placeholder="e.g. -y @modelcontextprotocol/server-filesystem /path" style="width:100%;margin-top:4px" />
                </label>
              </div>
            )}

            {addTransport !== 'stdio' && (
              <label style="display:block;margin-bottom:12px">
                <span style="color:var(--text-muted);font-size:0.85rem">URL</span>
                <input type="text" value={addURL} onInput={e => setAddURL(e.target.value)}
                  placeholder="e.g. http://localhost:3000/sse" style="width:100%;margin-top:4px" />
              </label>
            )}

            <label style="display:block;margin-bottom:12px">
              <span style="color:var(--text-muted);font-size:0.85rem">Tool Prefix (optional)</span>
              <input type="text" value={addPrefix} onInput={e => setAddPrefix(e.target.value)}
                placeholder="Leave empty for name_" style="width:100%;margin-top:4px" />
            </label>

            <label style="display:block;margin-bottom:16px">
              <span style="color:var(--text-muted);font-size:0.85rem">Trust Level</span>
              <select value={addTrust} onChange={e => setAddTrust(e.target.value)} style="width:100%;margin-top:4px">
                <option value="untrusted">Untrusted (results wrapped, consent required)</option>
                <option value="trusted">Trusted (direct pass-through)</option>
              </select>
            </label>

            <div style="display:flex;gap:8px;justify-content:flex-end">
              <button class="btn-secondary" onClick={() => { setShowAdd(false); resetForm(); }}>Cancel</button>
              <button class="btn-primary" onClick={addServer}>Connect</button>
            </div>
          </div>
        </div>
      )}
    </div>
  );
}
