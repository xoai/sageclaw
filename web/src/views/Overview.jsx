import { h } from 'preact';
import { useState, useEffect } from 'preact/hooks';
import { rpc, subscribeEvents } from '../api';

export function Overview() {
  const [status, setStatus] = useState(null);
  const [health, setHealth] = useState(null);
  const [recentSessions, setRecentSessions] = useState([]);
  const [recentEvents, setRecentEvents] = useState([]);

  const [dataReady, setDataReady] = useState(false);
  const [showSetup, setShowSetup] = useState(false);

  useEffect(() => {
    // Load all data in parallel, mark ready only when all complete.
    // Compute setup status BEFORE rendering to prevent flash.
    Promise.all([
      fetch('/api/status').then(r => r.json()).catch(() => null),
      fetch('/api/health').then(r => r.json()).catch(() => null),
      fetch('/api/providers').then(r => r.json()).catch(() => []),
      rpc('sessions.list', { limit: 5 }).catch(() => []),
    ]).then(([statusData, healthData, dbProviders, sessions]) => {
      if (healthData) {
        // Merge DB providers into health map.
        if (Array.isArray(dbProviders) && dbProviders.length > 0) {
          const merged = { ...healthData.providers };
          dbProviders.forEach(p => {
            if (p.status === 'active') merged[p.type] = 'connected';
          });
          healthData.providers = merged;
        }
      }

      // Compute setup status before any render.
      const providerConnected = healthData?.providers && Object.values(healthData.providers).some(s => s === 'connected');
      const agentExists = (statusData?.agents ?? 0) > 0;
      const needsSetup = !providerConnected || !agentExists;

      if (statusData) setStatus(statusData);
      if (healthData) setHealth(healthData);
      setRecentSessions(sessions || []);
      setShowSetup(needsSetup);
      setDataReady(true);
    });

    const unsub = subscribeEvents(event => {
      setRecentEvents(prev => [event, ...prev.slice(0, 9)]);
    });
    return unsub;
  }, []);

  const formatUptime = (secs) => {
    if (!secs) return '-';
    const h = Math.floor(secs / 3600);
    const m = Math.floor((secs % 3600) / 60);
    const s = Math.floor(secs % 60);
    if (h > 0) return `${h}h ${m}m`;
    if (m > 0) return `${m}m ${s}s`;
    return `${s}s`;
  };

  const hasProvider = health?.providers && Object.values(health.providers).some(s => s === 'connected');
  const hasAgent = (status?.agents ?? 0) > 0;
  const hasSession = (status?.sessions ?? 0) > 0;
  const setupComplete = hasProvider && hasAgent;
  const connectedProviders = health?.providers ? Object.entries(health.providers).filter(([, s]) => s === 'connected').length : 0;
  const totalProviders = health?.providers ? Object.keys(health.providers).length : 0;
  const cache = health?.cache;

  // Active channels.
  const activeChannels = [];
  if (health?.providers) {
    activeChannels.push('web', 'cli');
  }

  return (
    <div>
      <div style="display:flex;justify-content:space-between;align-items:center;margin-bottom:20px">
        <div>
          <h1 style="margin-bottom:2px">Dashboard</h1>
          <p style="color:var(--text-muted);font-size:13px">Overview and system status</p>
        </div>
        {hasProvider && (
          <span class="badge badge-green" style="font-size:12px;padding:4px 12px">Connected</span>
        )}
      </div>

      {/* Setup wizard — redirect to guided onboarding */}
      {dataReady && showSetup && (
        <div class="card" style="padding:24px;margin-bottom:24px;border-color:var(--primary);border-width:2px;text-align:center">
          <h2 style="font-size:16px;margin-bottom:4px">Welcome to SageClaw</h2>
          <p style="font-size:13px;color:var(--text-muted);margin-bottom:16px">
            Set up your first agent in a few guided steps.
          </p>
          <a href="/onboarding" class="btn-primary" style="text-decoration:none;display:inline-block;padding:10px 24px;font-size:14px">
            Get Started
          </a>
        </div>
      )}

      {/* Top stats — GoClaw style */}
      <div class="overview-stats" style="display:grid;grid-template-columns:repeat(5,1fr);gap:12px;margin-bottom:24px">
        <StatCard label="Requests" value={cache?.total_requests ?? 0} sub="total" />
        <StatCard label="Tokens" value={formatTokens(cache?.total_input, cache?.total_output)} sub={`${formatNum(cache?.total_input || 0)} in / ${formatNum(cache?.total_output || 0)} out`} />
        <StatCard label="Est. Cost" value={`$${(cache?.est_cost_usd || 0).toFixed(2)}`} sub={cache?.est_saved_usd > 0 ? `$${cache.est_saved_usd.toFixed(2)} saved` : 'no cache savings yet'} color={cache?.est_cost_usd > 0 ? 'var(--warning)' : undefined} />
        <StatCard label="Agents" value={`${status?.agents ?? 0}`} sub="configured" />
        <StatCard label="Channels" value={`${connectedProviders}/${totalProviders}`} sub="providers active" />
      </div>

      {/* System Health */}
      <div class="card" style="padding:16px;margin-bottom:24px">
        <h2 style="font-size:15px;margin-bottom:12px">System Health</h2>
        <div class="overview-cols" style="display:grid;grid-template-columns:repeat(3,1fr);gap:12px;margin-bottom:16px">
          <HealthItem label="Uptime" value={formatUptime(health?.uptime_seconds)} ok={true} />
          <HealthItem label="Database" value="Connected" ok={true} />
          <HealthItem label="Providers" value={`${connectedProviders} active`} ok={connectedProviders > 0} />
        </div>
        <div class="overview-cols" style="display:grid;grid-template-columns:repeat(3,1fr);gap:12px;margin-bottom:16px">
          <HealthItem label="Tools" value={`27+`} ok={true} />
          <HealthItem label="Sessions" value={`${status?.sessions ?? 0}`} ok={true} />
          <HealthItem label="Memories" value={`${status?.memories ?? 0}`} ok={true} />
        </div>

        {/* Channels status */}
        {health?.providers && (
          <div style="margin-top:8px">
            <div style="font-size:11px;text-transform:uppercase;letter-spacing:0.5px;color:var(--text-muted);margin-bottom:8px">Providers</div>
            <div style="display:flex;gap:12px;flex-wrap:wrap">
              {Object.entries(health.providers).map(([name, pStatus]) => (
                <span key={name} style="display:flex;align-items:center;gap:4px;font-size:13px">
                  <span style={`width:8px;height:8px;border-radius:50%;background:${pStatus === 'connected' ? 'var(--success)' : 'var(--text-muted)'}`} />
                  <span style="text-transform:capitalize">{name}</span>
                </span>
              ))}
            </div>
          </div>
        )}

        {/* Cache stats */}
        {cache && cache.total_requests > 0 && (
          <div style="margin-top:12px">
            <div style="font-size:11px;text-transform:uppercase;letter-spacing:0.5px;color:var(--text-muted);margin-bottom:8px">Prompt Cache</div>
            <div style="display:flex;gap:24px;font-size:13px">
              <span>Hit rate: <strong>{(cache.hit_rate || 0).toFixed(1)}%</strong></span>
              <span>Savings: <strong>{(cache.cost_savings_pct || 0).toFixed(1)}%</strong></span>
              <span>Cached reads: <strong>{formatNum(cache.cache_read || 0)}</strong> tokens</span>
            </div>
          </div>
        )}
      </div>

      {/* Two-column: Sessions + Activity */}
      <div class="overview-cols" style="display:grid;grid-template-columns:1fr 1fr;gap:24px;margin-bottom:24px">
        {/* Recent Sessions */}
        <div>
          <div style="display:flex;justify-content:space-between;align-items:center;margin-bottom:12px">
            <h2 style="font-size:15px">Recent Sessions</h2>
            <a href="/sessions" style="font-size:12px">View all</a>
          </div>
          {recentSessions.length === 0 ? (
            <div class="card" style="padding:16px;text-align:center;color:var(--text-muted);font-size:13px">No sessions yet.</div>
          ) : (
            recentSessions.map(s => (
              <a key={s.id} href={`/sessions/${s.id}`} style="text-decoration:none">
                <div class="card clickable" style="padding:10px 14px">
                  <div style="display:flex;justify-content:space-between;align-items:center">
                    <span style="font-size:13px;color:var(--text);font-weight:500">{s.label || s.id?.slice(0, 8)}</span>
                    <div style="display:flex;gap:6px;align-items:center">
                      <span class="badge badge-gray">{s.channel}</span>
                      {s.message_count > 0 && <span style="font-size:11px;color:var(--text-muted)">{s.message_count} msgs</span>}
                    </div>
                  </div>
                  <div style="font-size:12px;color:var(--text-muted);margin-top:2px">
                    {s.agent_name || s.agent_id} · {s.updated_at?.slice(11, 19)}
                  </div>
                </div>
              </a>
            ))
          )}
        </div>

        {/* Live Activity */}
        <div>
          <div style="display:flex;justify-content:space-between;align-items:center;margin-bottom:12px">
            <h2 style="font-size:15px">Live Activity</h2>
            <a href="/activity" style="font-size:12px">Full view</a>
          </div>
          {recentEvents.length === 0 ? (
            <div class="card" style="padding:16px;text-align:center;color:var(--text-muted);font-size:13px">
              Waiting for agent activity...
            </div>
          ) : (
            <div style="max-height:280px;overflow-y:auto">
              {recentEvents.map((event, i) => (
                <div key={i} class="event-card" style="padding:6px 12px;font-size:12px;font-family:var(--mono)">
                  <span style="color:var(--text-muted);margin-right:8px">{event.type}</span>
                  {event.agent_id && <span style="margin-right:8px">{event.agent_id}</span>}
                  {event.text && <span style="color:var(--text-muted)">{event.text?.slice(0, 60)}</span>}
                </div>
              ))}
            </div>
          )}
        </div>
      </div>

      {/* Quick Actions */}
      <div>
        <h2 style="font-size:15px;margin-bottom:12px">Quick Actions</h2>
        <div style="display:flex;gap:12px;flex-wrap:wrap">
          <a href="/chat" class="btn-primary" style="text-decoration:none">Open Chat</a>
          <a href="/onboarding" class="btn-secondary" style="text-decoration:none">Quick Setup</a>
          <a href="/agents" class="btn-secondary" style="text-decoration:none">Manage Agents</a>
          <a href="/providers" class="btn-secondary" style="text-decoration:none">Providers</a>
          <a href="/memory" class="btn-secondary" style="text-decoration:none">Memory</a>
          <a href="/channels" class="btn-secondary" style="text-decoration:none">Channels</a>
        </div>
      </div>
    </div>
  );
}

function StatCard({ label, value, sub, color }) {
  return (
    <div class="card" style="text-align:center;padding:14px 10px">
      <div style={`font-size:1.5rem;font-weight:700;color:${color || 'var(--primary)'}`}>{value}</div>
      <div style="color:var(--text-muted);font-size:11px;margin-top:2px">{label}</div>
      {sub && <div style="color:var(--text-muted);font-size:10px;margin-top:2px;opacity:0.7">{sub}</div>}
    </div>
  );
}

function HealthItem({ label, value, ok }) {
  return (
    <div class="card" style="padding:10px 14px;display:flex;align-items:center;gap:8px">
      <span style={`width:8px;height:8px;border-radius:50%;background:${ok ? 'var(--success)' : 'var(--error)'};flex-shrink:0`} />
      <div>
        <div style="font-size:11px;color:var(--text-muted)">{label}</div>
        <div style="font-size:13px;font-weight:500">{value}</div>
      </div>
    </div>
  );
}

function formatNum(n) {
  if (n >= 1000000) return (n / 1000000).toFixed(1) + 'M';
  if (n >= 1000) return (n / 1000).toFixed(1) + 'K';
  return String(n);
}

function formatTokens(inp, out) {
  const total = (inp || 0) + (out || 0);
  return formatNum(total);
}
