import { h } from 'preact';
import { useState, useEffect } from 'preact/hooks';

export function Channels() {
  const [channels, setChannels] = useState([]);
  const [configuring, setConfiguring] = useState(null);
  const [vars, setVars] = useState({});
  const [msg, setMsg] = useState('');
  const [pairingCode, setPairingCode] = useState(null);
  const [pairingChannel, setPairingChannel] = useState(null);
  const [pairedDevices, setPairedDevices] = useState([]);

  const load = () => fetch('/api/channels').then(r => r.json()).then(data => setChannels(data || [])).catch(() => {});
  const loadPaired = () => fetch('/api/pairing').then(r => r.json()).then(data => setPairedDevices(data || [])).catch(() => {});
  useEffect(() => { load(); loadPaired(); }, []);

  const statusDot = (status) => {
    const color = status === 'active' ? 'var(--success)' :
                  status === 'available' ? 'var(--warning)' : 'var(--text-muted)';
    return <span style={`display:inline-block;width:8px;height:8px;border-radius:50%;background:${color};margin-right:8px`} />;
  };

  const startConfigure = (ch) => {
    setConfiguring(ch.name);
    const v = {};
    (ch.fields || []).forEach(f => { v[f.key] = ''; });
    setVars(v);
  };

  const saveConfigure = async () => {
    const res = await fetch('/api/channels/configure', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ channel: configuring, vars }),
    });
    const data = await res.json();
    setConfiguring(null);
    if (data.started) {
      setMsg(`${configuring} configured and started. Generate a pairing code to authorize users.`);
    } else {
      setMsg(`${configuring} configured — ${data.stored} credentials stored.`);
    }
    setTimeout(() => setMsg(''), 8000);
    load();
  };

  const generatePairingCode = async (channelName) => {
    try {
      const res = await fetch('/api/pairing/generate', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ channel: channelName }),
      });
      const data = await res.json();
      if (data.code) {
        setPairingCode(data.code);
        setPairingChannel(channelName);
      } else {
        setMsg(data.error || 'Failed to generate pairing code');
        setTimeout(() => setMsg(''), 5000);
      }
    } catch {
      setMsg('Failed to generate pairing code');
      setTimeout(() => setMsg(''), 5000);
    }
  };

  const deletePaired = async (id) => {
    await fetch(`/api/pairing/${id}`, { method: 'DELETE' });
    loadPaired();
  };

  return (
    <div>
      <h1>Channels</h1>
      <p style="color:var(--text-muted);font-size:13px;margin-bottom:16px">
        Configure channels to connect SageClaw to messaging platforms.
      </p>

      {msg && <div class="card" style="padding:0.75rem;margin-bottom:1rem;color:var(--success)">{msg}</div>}

      {channels.map(ch => (
        <div key={ch.name} class="card" style="margin-bottom:12px">
          <div style="display:flex;justify-content:space-between;align-items:center">
            <div>
              <h3 style="font-size:14px;font-weight:600">{statusDot(ch.status)}{ch.name}</h3>
              <p style="font-size:13px;color:var(--text-muted);margin-top:4px">{ch.description}</p>
            </div>
            <div style="display:flex;gap:0.5rem;align-items:center">
              <span class={`badge ${ch.status === 'active' ? 'badge-green' : ch.status === 'available' ? 'badge-blue' : 'badge-gray'}`}>
                {ch.status}
              </span>
              {ch.status === 'active' && ch.configurable && (
                <button class="btn-small" onClick={() => generatePairingCode(ch.name)}>
                  Pair
                </button>
              )}
              {ch.configurable && (
                <button class="btn-small" onClick={() => startConfigure(ch)}>
                  {ch.status === 'active' ? 'Reconfigure' : 'Configure'}
                </button>
              )}
            </div>
          </div>

          {ch.fields && (
            <div style="margin-top:8px;display:flex;gap:8px;flex-wrap:wrap">
              {ch.fields.map(f => (
                <span key={f.key} class={`badge ${f.configured ? 'badge-green' : 'badge-gray'}`}>
                  {f.label}: {f.configured ? 'Set' : 'Missing'}
                </span>
              ))}
            </div>
          )}
        </div>
      ))}

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

      {/* Configure modal */}
      {configuring && (
        <div class="modal-overlay" onClick={() => setConfiguring(null)}>
          <div class="modal-content" onClick={e => e.stopPropagation()}>
            <h2>Configure {configuring}</h2>
            <p style="color:var(--text-muted);font-size:13px;margin-bottom:16px">
              Credentials are encrypted and stored in the database. The channel will start immediately after saving.
            </p>
            {Object.keys(vars).map(key => {
              const ch = channels.find(c => c.name === configuring);
              const field = ch?.fields?.find(f => f.key === key);
              return (
                <div class="form-group" key={key}>
                  <label>{field?.label || key}</label>
                  <input type="password" placeholder={key}
                    value={vars[key]} onInput={e => setVars({ ...vars, [key]: e.target.value })} />
                </div>
              );
            })}
            <div style="display:flex;gap:0.5rem;margin-top:1rem">
              <button class="btn-primary" onClick={saveConfigure}>Save</button>
              <button class="btn-secondary" onClick={() => setConfiguring(null)}>Cancel</button>
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
              This code expires in 5 minutes. Open {pairingChannel} and send this code as a message to the bot.
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
