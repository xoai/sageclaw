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
    const id = setInterval(load, 3000);
    return () => clearInterval(id);
  }, []);

  const start = async () => {
    setStarting(true);
    try {
      const res = await fetch('/api/tunnel/start', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ mode: 'managed' }),
      });
      const data = await res.json();
      if (data.error) alert(data.error);
    } catch {}
    setStarting(false);
    setTimeout(load, 2000);
  };

  const stop = async () => {
    setStopping(true);
    await fetch('/api/tunnel/stop', { method: 'POST' });
    setStopping(false);
    load();
  };

  if (loading) return <div class="empty">Loading tunnel status...</div>;

  const webhookPlatforms = ['whatsapp', 'zalo'];

  return (
    <div>
      <h1>External Access</h1>
      <p style="color:var(--text-muted);margin-bottom:24px">
        Expose your local SageClaw to the internet for webhook channels (WhatsApp, Zalo).
        Telegram and Discord don't need a tunnel.
      </p>

      {/* Security banner when tunnel active + no 2FA */}
      {status?.running && !status?.two_fa && (
        <div class="card" style="padding:12px 16px;margin-bottom:16px;border-color:var(--warning);background:var(--warning-bg, rgba(255,180,0,0.08))">
          <div style="display:flex;align-items:center;gap:8px">
            <span style="font-size:18px">⚠</span>
            <div>
              <strong style="color:var(--warning)">Dashboard accessible from internet</strong>
              <div style="font-size:12px;color:var(--text-muted);margin-top:2px">
                Enable 2FA in Settings → Security for stronger protection.
              </div>
            </div>
          </div>
        </div>
      )}

      {/* Tunnel control */}
      <div class="card" style="padding:16px;margin-bottom:16px">
        <div style="display:flex;justify-content:space-between;align-items:center;margin-bottom:12px">
          <h3>Native Tunnel</h3>
          <span class={`badge ${status?.running ? 'badge-green' : 'badge-gray'}`}>
            {status?.running ? 'Connected' : 'Stopped'}
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

                {webhookPlatforms.map(platform => (
                  <div key={platform} class="form-group">
                    <label>{platform} webhook</label>
                    <div style="display:flex;gap:8px;align-items:center">
                      <input type="text" value={`${status.url}/webhook/${platform}`} readOnly
                        style="flex:1;font-family:var(--mono);font-size:12px" />
                      <button class="btn-small" onClick={() => navigator.clipboard?.writeText(`${status.url}/webhook/${platform}`)}>
                        Copy
                      </button>
                    </div>
                  </div>
                ))}

                <div style="margin-top:8px;display:flex;gap:16px;font-size:12px;color:var(--text-muted)">
                  <span>Started: {status.started_at}</span>
                  {status.latency_ms > 0 && <span>Latency: {status.latency_ms}ms</span>}
                  <span>Mode: {status.mode}</span>
                </div>
              </div>
            ) : (
              <div style="color:var(--warning);font-size:13px">
                Connecting to relay... URL will appear shortly.
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
              Start a native tunnel to get a stable public URL. No external tools required.
              The URL persists across restarts (tied to your instance ID).
            </p>
            <button class="btn-primary" onClick={start} disabled={starting}>
              {starting ? 'Starting...' : 'Start Tunnel'}
            </button>
          </div>
        )}

        {status?.error && (
          <div style="margin-top:12px;color:var(--error);font-size:13px">
            {status.error}
          </div>
        )}
      </div>

      {/* Info card */}
      <div class="card" style="padding:16px">
        <h3 style="margin-bottom:8px">How it works</h3>
        <div style="font-size:13px;color:var(--text-muted);line-height:1.8">
          <p><strong>Managed Tunnel</strong> (recommended) — connects to the SageClaw relay server. Zero configuration. Stable subdomain based on your instance ID.</p>
          <p style="margin-top:8px"><strong>Self-Hosted</strong> — deploy your own relay server for full control. Configure in your config YAML.</p>
          <p style="margin-top:8px"><strong>CLI alternative</strong> — run <code style="color:var(--primary)">sageclaw tunnel</code> from terminal for the same functionality.</p>
        </div>
      </div>
    </div>
  );
}
