import { h } from 'preact';
import { useState, useEffect } from 'preact/hooks';

export default function Tunnel() {
  const [status, setStatus] = useState(null);
  const [loading, setLoading] = useState(true);
  const [starting, setStarting] = useState(false);
  const [stopping, setStopping] = useState(false);

  const load = () => {
    fetch('/api/tunnel/status')
      .then(r => r.json())
      .then(setStatus)
      .catch(() => {})
      .finally(() => setLoading(false));
  };

  useEffect(() => {
    load();
    // Poll status every 3s while page is open.
    const id = setInterval(load, 3000);
    return () => clearInterval(id);
  }, []);

  const start = async () => {
    setStarting(true);
    try {
      const res = await fetch('/api/tunnel/start', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ mode: 'quick' }),
      });
      const data = await res.json();
      if (data.error) alert(data.error);
    } catch {}
    setStarting(false);
    // URL arrives async, polling will pick it up.
    setTimeout(load, 2000);
  };

  const stop = async () => {
    setStopping(true);
    await fetch('/api/tunnel/stop', { method: 'POST' });
    setStopping(false);
    load();
  };

  if (loading) return <div class="empty">Loading tunnel status...</div>;

  return (
    <div>
      <h1>Tunnel</h1>
      <p style="color:var(--text-muted);margin-bottom:24px">
        Expose your local SageClaw to the internet for webhook channels (WhatsApp, Zalo).
        Telegram and Discord don't need a tunnel.
      </p>

      {/* Cloudflared status */}
      <div class="card" style="padding:16px;margin-bottom:16px">
        <div style="display:flex;justify-content:space-between;align-items:center">
          <h3>Cloudflared</h3>
          <span class={`badge ${status?.installed ? 'badge-green' : 'badge-red'}`}>
            {status?.installed ? 'Installed' : 'Not Found'}
          </span>
        </div>
        {status?.installed ? (
          <div style="margin-top:8px;font-size:13px;color:var(--text-muted)">
            Version: {status.version} &middot; Path: {status.path}
          </div>
        ) : (
          <div style="margin-top:12px">
            <p style="color:var(--text-muted);font-size:13px;margin-bottom:8px">
              Install cloudflared to enable tunneling:
            </p>
            <pre style="background:var(--bg);padding:10px 14px;border-radius:6px;font-size:12px;font-family:var(--mono);overflow-x:auto">
              {status?.install_hint}
            </pre>
          </div>
        )}
      </div>

      {/* Tunnel control */}
      {status?.installed && (
        <div class="card" style="padding:16px;margin-bottom:16px">
          <div style="display:flex;justify-content:space-between;align-items:center;margin-bottom:12px">
            <h3>Quick Tunnel</h3>
            <span class={`badge ${status?.running ? 'badge-green' : 'badge-gray'}`}>
              {status?.running ? 'Running' : 'Stopped'}
            </span>
          </div>

          {status?.running ? (
            <div>
              {status.url ? (
                <div>
                  <div class="form-group">
                    <label>Public URL</label>
                    <div style="display:flex;gap:8px;align-items:center">
                      <input type="text" value={status.url} readOnly
                        style="flex:1;font-family:var(--mono);font-size:12px" />
                      <button class="btn-small" onClick={() => navigator.clipboard?.writeText(status.url)}>
                        Copy
                      </button>
                    </div>
                  </div>

                  {status.webhooks && Object.entries(status.webhooks).map(([ch, url]) => (
                    <div key={ch} class="form-group">
                      <label>{ch} webhook</label>
                      <div style="display:flex;gap:8px;align-items:center">
                        <input type="text" value={url} readOnly
                          style="flex:1;font-family:var(--mono);font-size:12px" />
                        <button class="btn-small" onClick={() => navigator.clipboard?.writeText(url)}>
                          Copy
                        </button>
                      </div>
                    </div>
                  ))}

                  <div style="margin-top:8px;font-size:12px;color:var(--text-muted)">
                    Started: {status.started_at}
                  </div>
                </div>
              ) : (
                <div style="color:var(--warning);font-size:13px">
                  Tunnel is starting... URL will appear shortly.
                </div>
              )}

              <button class="btn-danger btn-small" style="margin-top:12px"
                onClick={stop} disabled={stopping}>
                {stopping ? 'Stopping...' : 'Stop Tunnel'}
              </button>
            </div>
          ) : (
            <div>
              <p style="color:var(--text-muted);font-size:13px;margin-bottom:12px">
                Start a free quick tunnel using trycloudflare.com. No Cloudflare account needed.
                The URL changes each time you restart.
              </p>
              <button class="btn-primary" onClick={start} disabled={starting}>
                {starting ? 'Starting...' : 'Start Tunnel'}
              </button>
            </div>
          )}

          {status?.error && (
            <div style="margin-top:12px;color:var(--error);font-size:13px">
              Error: {status.error}
            </div>
          )}
        </div>
      )}

      {/* Info card */}
      <div class="card" style="padding:16px">
        <h3 style="margin-bottom:8px">How it works</h3>
        <div style="font-size:13px;color:var(--text-muted);line-height:1.8">
          <p><strong>Quick Tunnel</strong> — creates a temporary public URL via Cloudflare's free trycloudflare.com service. No account required. URL changes on each restart.</p>
          <p style="margin-top:8px"><strong>Named Tunnel</strong> — for a permanent URL, use the CLI: <code style="color:var(--primary)">cloudflared tunnel create sageclaw</code>, then configure DNS in your Cloudflare dashboard.</p>
          <p style="margin-top:8px"><strong>CLI alternative</strong> — run <code style="color:var(--primary)">sageclaw tunnel</code> from terminal for the same functionality.</p>
        </div>
      </div>
    </div>
  );
}
