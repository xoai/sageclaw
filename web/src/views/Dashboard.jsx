import { h } from 'preact';
import { useState, useEffect, useRef } from 'preact/hooks';
import { rpc, subscribeEvents } from '../api';
import { EventCard } from '../components/EventCard';
import { TabBar } from '../components/TabBar';
import { Breadcrumb } from '../components/Breadcrumb';

const TABS = [
  { id: 'overview', label: 'Overview' },
  { id: 'activity', label: 'Activity' },
  { id: 'budget', label: 'Budget' },
];

export default function Dashboard() {
  const params = new URLSearchParams(window.location.search);
  const [tab, setTab] = useState(params.get('tab') || 'overview');

  const changeTab = (id) => {
    setTab(id);
    const url = id === 'overview' ? '/' : `/?tab=${id}`;
    history.replaceState(null, '', url);
  };

  return (
    <div>
      <Breadcrumb items={[{ label: 'Dashboard' }]} />
      <h1 style="margin-bottom:2px">Dashboard</h1>
      <p class="page-subtitle" style="margin-bottom:16px">Health, activity, and agent metrics</p>
      <TabBar tabs={TABS} active={tab} onChange={changeTab} />
      <div class="tab-content-enter" key={tab}>
        {tab === 'overview' && <OverviewTab />}
        {tab === 'activity' && <ActivityTab />}
        {tab === 'budget' && <BudgetTab />}
      </div>
    </div>
  );
}

// ── Overview Tab (from Overview.jsx) ──

function OverviewTab() {
  const [status, setStatus] = useState(null);
  const [health, setHealth] = useState(null);
  const [recentSessions, setRecentSessions] = useState([]);
  const [recentEvents, setRecentEvents] = useState([]);
  const [dataReady, setDataReady] = useState(false);
  const [showSetup, setShowSetup] = useState(false);

  useEffect(() => {
    Promise.all([
      fetch('/api/status').then(r => r.json()).catch(() => null),
      fetch('/api/health').then(r => r.json()).catch(() => null),
      fetch('/api/providers').then(r => r.json()).catch(() => []),
      rpc('sessions.list', { limit: 5 }).catch(() => []),
    ]).then(([statusData, healthData, dbProviders, sessions]) => {
      if (healthData) {
        if (Array.isArray(dbProviders) && dbProviders.length > 0) {
          const merged = { ...healthData.providers };
          dbProviders.forEach(p => {
            if (p.status === 'active') merged[p.type] = 'connected';
          });
          healthData.providers = merged;
        }
      }
      const providerConnected = healthData?.providers && Object.values(healthData.providers).some(s => s === 'connected');
      const agentExists = (statusData?.agents ?? 0) > 0;
      if (statusData) setStatus(statusData);
      if (healthData) setHealth(healthData);
      setRecentSessions(sessions || []);
      setShowSetup(!providerConnected || !agentExists);
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
  const connectedProviders = health?.providers ? Object.entries(health.providers).filter(([, s]) => s === 'connected').length : 0;
  const totalProviders = health?.providers ? Object.keys(health.providers).length : 0;
  const cache = health?.cache;

  return (
    <div>
      {/* Setup wizard */}
      {dataReady && showSetup && (
        <div style="padding:24px 0 32px;text-align:center;border-bottom:1px solid var(--border);margin-bottom:48px">
          <h2 style="font-size:16px;margin-bottom:4px">Welcome to SageClaw</h2>
          <p style="font-size:13px;color:var(--text-muted);margin-bottom:16px">
            Set up your first agent in a few guided steps.
          </p>
          <a href="/onboarding" class="btn-primary" style="text-decoration:none;display:inline-block;padding:10px 24px;font-size:14px">
            Get Started
          </a>
        </div>
      )}

      {/* Stats — asymmetric: cost prominent, others inline */}
      <div class="dash-stats">
        <div class="dash-stat-primary">
          <div class="dash-stat-label">Est. Cost</div>
          <div class="dash-stat-value" style={cache?.est_cost_usd > 0 ? 'color:var(--warning)' : ''}>
            ${(cache?.est_cost_usd || 0).toFixed(2)}
          </div>
          <div class="dash-stat-sub">
            {cache?.est_saved_usd > 0 ? `$${cache.est_saved_usd.toFixed(2)} saved via cache` : 'no cache savings yet'}
          </div>
        </div>
        <div class="dash-stat-secondary">
          <InlineStat label="Requests" value={cache?.total_requests ?? 0} />
          <InlineStat label="Tokens" value={formatTokens(cache?.total_input, cache?.total_output)} sub={`${formatNum(cache?.total_input || 0)} in / ${formatNum(cache?.total_output || 0)} out`} />
          <InlineStat label="Agents" value={status?.agents ?? 0} />
          <InlineStat label="Providers" value={`${connectedProviders}/${totalProviders}`} />
        </div>
      </div>

      {/* System Health */}
      <div style="margin-bottom:48px">
        <div class="section-label" style="margin-bottom:16px">System Health</div>
        <div class="overview-cols" style="display:grid;grid-template-columns:repeat(3,1fr);gap:0 24px">
          <HealthItem label="Uptime" value={formatUptime(health?.uptime_seconds)} ok={true} />
          <HealthItem label="Database" value="Connected" ok={true} />
          <HealthItem label="Providers" value={`${connectedProviders} active`} ok={connectedProviders > 0} />
          <HealthItem label="Sessions" value={`${status?.sessions ?? 0}`} ok={true} />
          <HealthItem label="Memories" value={`${status?.memories ?? 0}`} ok={true} />
        </div>

        {health?.providers && (
          <div style="margin-top:16px;padding-top:12px;border-top:1px solid var(--border)">
            <div style="display:flex;gap:16px;flex-wrap:wrap">
              {Object.entries(health.providers).map(([name, pStatus]) => (
                <span key={name} style="display:flex;align-items:center;gap:6px;font-size:13px">
                  <span class={`health-dot ${pStatus === 'connected' ? 'ok' : 'err'}`} />
                  <span style="text-transform:capitalize">{name}</span>
                </span>
              ))}
            </div>
          </div>
        )}

      </div>

      {/* Recent Sessions + Activity — generous gap from health */}
      <div class="overview-cols" style="display:grid;grid-template-columns:1fr 1fr;gap:32px">
        <div>
          <div style="display:flex;justify-content:space-between;align-items:baseline;margin-bottom:12px">
            <div class="section-label">Recent Sessions</div>
            <a href="/chat" style="font-size:12px">View all</a>
          </div>
          {recentSessions.length === 0 ? (
            <div style="padding:16px 0;color:var(--text-muted);font-size:13px;line-height:1.6">
              Sessions appear here as you chat. <a href="/chat">Start a conversation</a> to see activity.
            </div>
          ) : (
            <div class="dash-session-list">
              {recentSessions.map(s => (
                <a key={s.id} href={`/chat?session=${s.id}`} class="dash-session-item">
                  <div style="display:flex;justify-content:space-between;align-items:center">
                    <span style="font-size:13px;color:var(--text);font-weight:500">{s.title || s.label || s.id?.slice(0, 8)}</span>
                    <span style="font-size:11px;color:var(--text-muted)">{s.message_count || 0} msgs</span>
                  </div>
                  <div style="font-size:12px;color:var(--text-muted);margin-top:2px">
                    {s.agent_name || s.agent_id} · {s.updated_at?.slice(11, 19)}
                  </div>
                </a>
              ))}
            </div>
          )}
        </div>

        <div>
          <div class="section-label" style="margin-bottom:12px">Live Activity</div>
          {recentEvents.length === 0 ? (
            <div style="padding:16px 0;color:var(--text-muted);font-size:13px;line-height:1.6">
              Agent events stream here in real time as conversations happen.
            </div>
          ) : (
            <div style="max-height:280px;overflow-y:auto">
              {recentEvents.map((event, i) => (
                <div key={i} class="event-card" style="padding:6px 0;font-size:12px;font-family:var(--mono);border-bottom-color:var(--border)">
                  <span style="color:var(--text-muted);margin-right:8px">{event.type}</span>
                  {event.agent_id && <span style="margin-right:8px">{event.agent_id}</span>}
                  {event.text && <span style="color:var(--text-muted)">{event.text?.slice(0, 60)}</span>}
                </div>
              ))}
            </div>
          )}
        </div>
      </div>
    </div>
  );
}

// ── Activity Tab (merged Activity + Audit) ──

function ActivityTab() {
  const [mode, setMode] = useState('live'); // 'live' | 'history'

  return (
    <div>
      <div style="display:flex;gap:8px;margin-bottom:16px">
        <button class={mode === 'live' ? 'btn-primary' : 'btn-secondary'} onClick={() => setMode('live')}>Live</button>
        <button class={mode === 'history' ? 'btn-primary' : 'btn-secondary'} onClick={() => setMode('history')}>History</button>
      </div>
      {mode === 'live' && <LiveActivity />}
      {mode === 'history' && <AuditHistory />}
    </div>
  );
}

function LiveActivity() {
  const [events, setEvents] = useState([]);
  const [paused, setPaused] = useState(false);
  const bottomRef = useRef(null);

  useEffect(() => {
    const unsub = subscribeEvents((event) => {
      setEvents((prev) => [...prev.slice(-199), event]);
    });
    return unsub;
  }, []);

  useEffect(() => {
    if (!paused && bottomRef.current) {
      bottomRef.current.scrollIntoView({ behavior: 'smooth' });
    }
  }, [events, paused]);

  return (
    <div>
      <div style="display:flex;justify-content:flex-end;margin-bottom:12px">
        <button
          onClick={() => setPaused(!paused)}
          style="padding:6px 12px;background:var(--surface);border:1px solid var(--border);border-radius:4px;color:var(--text);cursor:pointer;font-size:12px"
        >
          {paused ? '\u25B6 Resume' : '\u23F8 Pause'}
        </button>
      </div>
      {events.length === 0 ? (
        <div class="empty" style="padding:48px 24px">
          <div style="font-size:15px;margin-bottom:4px;color:var(--text)">Listening for events</div>
          <div style="font-size:13px;max-width:320px;margin:0 auto;line-height:1.6">
            Agent events stream here in real time. Start a <a href="/chat">conversation</a> to see tool calls, messages, and system events.
          </div>
        </div>
      ) : (
        <div>
          {events.map((event, i) => (
            <EventCard key={i} event={event} />
          ))}
          <div ref={bottomRef} />
        </div>
      )}
    </div>
  );
}

function AuditHistory() {
  const [entries, setEntries] = useState([]);
  const [stats, setStats] = useState(null);
  const [filters, setFilters] = useState({ agent_id: '', tool: '', from: '', to: '' });
  const [expanded, setExpanded] = useState(null);

  const loadStats = () => fetch('/api/audit/stats').then(r => r.json()).then(setStats).catch(() => {});
  const loadEntries = () => {
    const params = new URLSearchParams();
    if (filters.agent_id) params.set('agent_id', filters.agent_id);
    if (filters.tool) params.set('tool', filters.tool);
    if (filters.from) params.set('from', filters.from);
    if (filters.to) params.set('to', filters.to);
    fetch(`/api/audit?${params}`).then(r => r.json()).then(setEntries).catch(() => {});
  };

  useEffect(() => { loadStats(); loadEntries(); }, []);

  return (
    <div>
      {/* Stats */}
      {stats && (
        <div style="display:grid;grid-template-columns:repeat(4,1fr);gap:16px;margin-bottom:24px">
          <div class="card" style="text-align:center;padding:16px">
            <div style="font-size:28px;font-weight:700;color:var(--primary);font-family:var(--mono)">{stats.total}</div>
            <div style="color:var(--text-muted);font-size:12px">Total Events</div>
          </div>
          <div class="card" style="text-align:center;padding:16px">
            <div style="font-size:28px;font-weight:700;color:var(--primary);font-family:var(--mono)">{stats.unique_tools}</div>
            <div style="color:var(--text-muted);font-size:12px">Unique Tools</div>
          </div>
          <div class="card" style="text-align:center;padding:16px">
            <div style={`font-size:28px;font-weight:700;font-family:var(--mono);color:${stats.error_rate > 5 ? 'var(--error)' : 'var(--success)'}`}>{stats.error_rate?.toFixed(1)}%</div>
            <div style="color:var(--text-muted);font-size:12px">Error Rate</div>
          </div>
          <div class="card" style="text-align:center;padding:16px">
            <div style="font-size:16px;font-weight:700;color:var(--primary);font-family:var(--mono);word-break:break-all">{stats.most_used || '-'}</div>
            <div style="color:var(--text-muted);font-size:12px">Most Used</div>
          </div>
        </div>
      )}

      {/* Filters */}
      <div class="card" style="padding:16px;margin-bottom:24px">
        <div class="audit-filters" style="display:grid;grid-template-columns:repeat(4,1fr) auto;gap:12px;align-items:end">
          <div class="form-group">
            <label>Agent ID</label>
            <input type="text" placeholder="Filter by agent..." value={filters.agent_id}
              onInput={e => setFilters({ ...filters, agent_id: e.target.value })} />
          </div>
          <div class="form-group">
            <label>Tool</label>
            <input type="text" placeholder="Filter by tool..." value={filters.tool}
              onInput={e => setFilters({ ...filters, tool: e.target.value })} />
          </div>
          <div class="form-group">
            <label>From</label>
            <input type="datetime-local" value={filters.from}
              onInput={e => setFilters({ ...filters, from: e.target.value })} />
          </div>
          <div class="form-group">
            <label>To</label>
            <input type="datetime-local" value={filters.to}
              onInput={e => setFilters({ ...filters, to: e.target.value })} />
          </div>
          <button class="btn-primary" onClick={loadEntries} style="height:38px;margin-bottom:12px">Search</button>
        </div>
      </div>

      {/* Table */}
      <table class="data-table">
        <thead>
          <tr><th scope="col">Time</th><th scope="col">Agent</th><th scope="col">Event</th><th scope="col">Details</th></tr>
        </thead>
        <tbody>
          {entries.map(e => (
            <tr key={e.id} class="clickable" onClick={() => setExpanded(expanded === e.id ? null : e.id)}>
              <td style="white-space:nowrap">{e.created_at}</td>
              <td>{e.agent_id}</td>
              <td><code>{e.event_type}</code></td>
              <td>
                {expanded === e.id
                  ? <pre style="white-space:pre-wrap;max-width:500px;font-size:0.8rem">{e.payload}</pre>
                  : (e.payload?.length > 80 ? e.payload.slice(0, 80) + '...' : e.payload)
                }
              </td>
            </tr>
          ))}
          {entries.length === 0 && (
            <tr><td colspan="4" style="text-align:center;color:var(--text-muted)">Tool calls and system events will appear here as agents work.</td></tr>
          )}
        </tbody>
      </table>
    </div>
  );
}

// ── Budget Tab (monitoring only, config in Settings) ──

function BudgetTab() {
  const [summary, setSummary] = useState(null);
  const [config, setConfig] = useState({ daily_limit_usd: 0, monthly_limit_usd: 0 });
  const [history, setHistory] = useState([]);
  const [alerts, setAlerts] = useState([]);
  const [topModels, setTopModels] = useState([]);
  const [subTab, setSubTab] = useState('overview');

  useEffect(() => {
    fetch('/api/budget/summary').then(r => r.json()).then(setSummary).catch(() => {});
    fetch('/api/budget/config').then(r => r.json()).then(setConfig).catch(() => {});
    fetch('/api/budget/history?days=30').then(r => r.json()).then(d => setHistory(d || [])).catch(() => {});
    fetch('/api/budget/alerts').then(r => r.json()).then(d => setAlerts(d || [])).catch(() => {});
    fetch('/api/budget/top-models').then(r => r.json()).then(d => setTopModels(d || [])).catch(() => {});
  }, []);

  const ackAlert = async (id) => {
    await fetch(`/api/budget/alerts/${id}`, { method: 'POST' });
    fetch('/api/budget/alerts').then(r => r.json()).then(d => setAlerts(d || [])).catch(() => {});
  };

  const pctBar = (pct) => (
    <div style="background:var(--border);border-radius:4px;height:8px;overflow:hidden;margin-top:6px">
      <div style={{
        width: `${Math.min(pct, 100)}%`,
        height: '100%',
        background: pct >= 100 ? 'var(--error)' : pct >= 80 ? 'var(--warning)' : 'var(--primary)',
        borderRadius: 4,
        transition: 'width 0.3s',
      }} />
    </div>
  );

  const maxCost = Math.max(...history.map(h => h.cost_usd), 0.01);
  const unackAlerts = alerts.filter(a => !a.acknowledged).length;

  return (
    <div>
      <div style="display:flex;justify-content:space-between;align-items:center;margin-bottom:16px">
        <div style="display:flex;gap:8px">
          <button class={subTab === 'overview' ? 'btn-primary' : 'btn-secondary'} onClick={() => setSubTab('overview')}>Overview</button>
          <button class={subTab === 'history' ? 'btn-primary' : 'btn-secondary'} onClick={() => setSubTab('history')}>Daily History</button>
          <button class={subTab === 'alerts' ? 'btn-primary' : 'btn-secondary'} onClick={() => setSubTab('alerts')}>
            Alerts {unackAlerts > 0 && <span class="badge badge-red" style="margin-left:4px">{unackAlerts}</span>}
          </button>
        </div>
        <a href="/settings?tab=budget-alerts" style="font-size:12px">Configure limits</a>
      </div>

      {subTab === 'overview' && summary && (
        <div>
          <div style="display:grid;grid-template-columns:1fr 1fr;gap:16px;margin-bottom:24px">
            <div class="card" style="padding:20px">
              <div style="color:var(--text-muted);font-size:12px;text-transform:uppercase;letter-spacing:0.5px">Today</div>
              <div style="font-size:2rem;font-weight:700;color:var(--primary);font-family:var(--mono);margin:8px 0">${summary.today_usd?.toFixed(2)}</div>
              <div style="font-size:12px;color:var(--text-muted)">{summary.today_requests} requests · ${summary.today_saved_usd?.toFixed(2)} saved</div>
              {config.daily_limit_usd > 0 && (
                <div>
                  {pctBar(summary.daily_percent)}
                  <div style="font-size:11px;color:var(--text-muted);margin-top:4px">
                    ${summary.daily_remaining?.toFixed(2)} remaining of ${config.daily_limit_usd.toFixed(2)} limit
                  </div>
                </div>
              )}
            </div>
            <div class="card" style="padding:20px">
              <div style="color:var(--text-muted);font-size:12px;text-transform:uppercase;letter-spacing:0.5px">This Month</div>
              <div style="font-size:2rem;font-weight:700;color:var(--primary);font-family:var(--mono);margin:8px 0">${summary.month_usd?.toFixed(2)}</div>
              <div style="font-size:12px;color:var(--text-muted)">{summary.month_requests} requests · ${summary.month_saved_usd?.toFixed(2)} saved</div>
              {config.monthly_limit_usd > 0 && (
                <div>
                  {pctBar(summary.monthly_percent)}
                  <div style="font-size:11px;color:var(--text-muted);margin-top:4px">
                    ${summary.monthly_remaining?.toFixed(2)} remaining of ${config.monthly_limit_usd.toFixed(2)} limit
                  </div>
                </div>
              )}
            </div>
          </div>

          {topModels.length > 0 && (
            <div class="card" style="padding:20px;margin-bottom:24px">
              <h3 style="margin-bottom:12px">Top Models (This Month)</h3>
              <table class="data-table">
                <thead>
                  <tr><th scope="col">Model</th><th scope="col">Provider</th><th scope="col">Cost</th><th scope="col">Requests</th><th scope="col">Input</th><th scope="col">Output</th></tr>
                </thead>
                <tbody>
                  {topModels.map((m, i) => (
                    <tr key={i}>
                      <td><code>{m.model}</code></td>
                      <td>{m.provider}</td>
                      <td style="font-weight:600">${m.cost_usd?.toFixed(4)}</td>
                      <td>{m.requests}</td>
                      <td>{(m.input_tokens || 0).toLocaleString()}</td>
                      <td>{(m.output_tokens || 0).toLocaleString()}</td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          )}

          <div style="display:grid;grid-template-columns:repeat(3,1fr);gap:12px">
            <div class="card" style="text-align:center;padding:16px">
              <div style="font-size:24px;font-weight:700;color:var(--success)">${((summary.today_saved_usd || 0) + (summary.month_saved_usd || 0)).toFixed(2)}</div>
              <div style="color:var(--text-muted);font-size:12px;margin-top:4px">Total Saved (Caching)</div>
            </div>
            <div class="card" style="text-align:center;padding:16px">
              <div style="font-size:24px;font-weight:700;color:var(--primary)">{summary.month_requests || 0}</div>
              <div style="color:var(--text-muted);font-size:12px;margin-top:4px">Requests This Month</div>
            </div>
            <div class="card" style="text-align:center;padding:16px">
              <div style="font-size:24px;font-weight:700;color:var(--primary)">
                {summary.month_requests > 0 ? `$${(summary.month_usd / summary.month_requests).toFixed(4)}` : '-'}
              </div>
              <div style="color:var(--text-muted);font-size:12px;margin-top:4px">Avg Cost per Request</div>
            </div>
          </div>
        </div>
      )}

      {subTab === 'history' && (
        <div>
          {history.length > 0 ? (
            <div class="card" style="padding:20px">
              <h3 style="margin-bottom:16px">Daily Spending (Last 30 Days)</h3>
              <div style="display:flex;align-items:end;gap:4px;height:200px;border-bottom:1px solid var(--border);padding-bottom:8px">
                {history.map((d, i) => (
                  <div key={i} style="flex:1;display:flex;flex-direction:column;align-items:center;justify-content:flex-end;height:100%"
                    title={`${d.date}: $${d.cost_usd?.toFixed(4)} (${d.requests} requests)`}>
                    <div style={{
                      width: '100%',
                      maxWidth: 24,
                      height: `${Math.max((d.cost_usd / maxCost) * 100, 2)}%`,
                      background: 'var(--primary)',
                      borderRadius: '3px 3px 0 0',
                      minHeight: 2,
                    }} />
                  </div>
                ))}
              </div>
              <div style="display:flex;justify-content:space-between;margin-top:4px;font-size:11px;color:var(--text-muted)">
                <span>{history[0]?.date}</span>
                <span>{history[history.length - 1]?.date}</span>
              </div>
              <table class="data-table" style="margin-top:16px">
                <thead>
                  <tr><th scope="col">Date</th><th scope="col">Cost</th><th scope="col">Saved</th><th scope="col">Requests</th></tr>
                </thead>
                <tbody>
                  {[...history].reverse().map((d, i) => (
                    <tr key={i}>
                      <td>{d.date}</td>
                      <td style="font-weight:600">${d.cost_usd?.toFixed(4)}</td>
                      <td style="color:var(--success)">${d.saved_usd?.toFixed(4)}</td>
                      <td>{d.requests}</td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          ) : (
            <p style="color:var(--text-muted);text-align:center;margin-top:3rem">No cost data yet.</p>
          )}
        </div>
      )}

      {subTab === 'alerts' && (
        <div>
          {alerts.length === 0 ? (
            <p style="color:var(--text-muted);text-align:center;margin-top:3rem">No budget alerts. <a href="/settings?tab=budget-alerts">Set limits</a> to enable alerts.</p>
          ) : (
            <div class="card-list">
              {alerts.map(a => (
                <div key={a.id} class="card" style="padding:16px">
                  <div style="display:flex;justify-content:space-between;align-items:center">
                    <div>
                      <span class={`badge ${a.alert_type === 'limit_reached' ? 'badge-red' : 'badge-yellow'}`} style="margin-right:8px">
                        {a.alert_type === 'limit_reached' ? 'LIMIT REACHED' : 'WARNING'}
                      </span>
                      <strong style="text-transform:capitalize">{a.period}</strong> budget
                    </div>
                    <div style="display:flex;align-items:center;gap:8px">
                      <span style="font-size:12px;color:var(--text-muted)">{a.created_at}</span>
                      {!a.acknowledged && (
                        <button class="btn-small" onClick={() => ackAlert(a.id)}>Dismiss</button>
                      )}
                    </div>
                  </div>
                  <div style="margin-top:8px;font-size:13px;color:var(--text-muted)">
                    Spent ${a.spent_usd?.toFixed(2)} of ${a.limit_usd?.toFixed(2)} limit ({a.percent?.toFixed(0)}%)
                  </div>
                  {pctBar(a.percent)}
                </div>
              ))}
            </div>
          )}
        </div>
      )}
    </div>
  );
}

// ── Shared helpers ──

function InlineStat({ label, value, sub }) {
  return (
    <div class="dash-inline-stat">
      <span class="dash-inline-label">{label}</span>
      <span class="dash-inline-value">{value}</span>
      {sub && <span class="dash-inline-sub">{sub}</span>}
    </div>
  );
}

function HealthItem({ label, value, ok }) {
  return (
    <div class="health-row">
      <span class={`health-dot ${ok ? 'ok' : 'err'}`} />
      <span style="font-size:12px;color:var(--text-muted);min-width:64px">{label}</span>
      <span style="font-size:13px;font-weight:500">{value}</span>
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
