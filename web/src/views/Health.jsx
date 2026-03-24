import { h } from 'preact';
import { useState, useEffect } from 'preact/hooks';

export default function Health() {
  const [health, setHealth] = useState(null);

  const load = () => fetch('/api/health').then(r => r.json()).then(setHealth).catch(() => {});

  useEffect(() => { load(); const id = setInterval(load, 10000); return () => clearInterval(id); }, []);

  const formatUptime = (secs) => {
    if (!secs) return '-';
    const h = Math.floor(secs / 3600);
    const m = Math.floor((secs % 3600) / 60);
    const s = Math.floor(secs % 60);
    if (h > 0) return `${h}h ${m}m ${s}s`;
    if (m > 0) return `${m}m ${s}s`;
    return `${s}s`;
  };

  const providerColor = (status) => {
    if (status === 'connected') return 'var(--success)';
    if (status === 'not configured') return 'var(--text-muted)';
    return 'var(--error)';
  };

  if (!health) return <div><h1>Health</h1><p style="color:var(--text-muted)">Loading...</p></div>;

  return (
    <div>
      <h1>System Health</h1>

      {/* Status cards */}
      <div class="health-row-3" style="display:grid;grid-template-columns:repeat(3,1fr);gap:1rem;margin-bottom:2rem">
        <div class="card" style="text-align:center;padding:1.5rem">
          <div style="font-size:1.5rem;font-weight:700;color:var(--success)">{health.pipeline}</div>
          <div style="color:var(--text-muted);margin-top:0.5rem">Pipeline</div>
        </div>
        <div class="card" style="text-align:center;padding:1.5rem">
          <div style="font-size:1.5rem;font-weight:700;color:var(--primary)">{formatUptime(health.uptime_seconds)}</div>
          <div style="color:var(--text-muted);margin-top:0.5rem">Uptime</div>
        </div>
        <div class="card" style="text-align:center;padding:1.5rem">
          <div style="font-size:1.5rem;font-weight:700;color:var(--primary)">{health.sessions_active}</div>
          <div style="color:var(--text-muted);margin-top:0.5rem">Sessions</div>
        </div>
      </div>

      <div class="health-row-2" style="display:grid;grid-template-columns:repeat(2,1fr);gap:1rem;margin-bottom:2rem">
        <div class="card" style="text-align:center;padding:1.5rem">
          <div style="font-size:1.5rem;font-weight:700;color:var(--primary)">{health.memories_count}</div>
          <div style="color:var(--text-muted);margin-top:0.5rem">Memories</div>
        </div>
        <div class="card" style="text-align:center;padding:1.5rem">
          <div style="font-size:1.5rem;font-weight:700;color:var(--primary)">{health.cron_jobs}</div>
          <div style="color:var(--text-muted);margin-top:0.5rem">Active Cron Jobs</div>
        </div>
      </div>

      {/* Providers */}
      <h2 style="margin-bottom:1rem">Providers</h2>
      <div class="health-providers" style="display:grid;grid-template-columns:repeat(3,1fr);gap:1rem">
        {health.providers && Object.entries(health.providers).map(([name, status]) => (
          <div class="card" key={name} style="padding:1rem">
            <div style="display:flex;justify-content:space-between;align-items:center">
              <strong style="text-transform:capitalize">{name}</strong>
              <span style={{
                display: 'inline-block',
                width: 10, height: 10, borderRadius: '50%',
                backgroundColor: providerColor(status),
              }} />
            </div>
            <div style="color:var(--text-muted);font-size:0.85rem;margin-top:0.5rem">{status}</div>
          </div>
        ))}
      </div>
    </div>
  );
}
