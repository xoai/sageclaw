import { h } from 'preact';
import { useState, useEffect } from 'preact/hooks';
import Router from 'preact-router';
import { route } from 'preact-router';
import { Layout } from './components/Layout';
import { Chat } from './views/Chat';
import { Agents } from './views/Agents';
import { Marketplace } from './views/Marketplace';
import Knowledge from './views/Knowledge';
import Dashboard from './views/Dashboard';
import { Settings } from './views/Settings';
import { Setup } from './views/Setup';
import { Login } from './views/Login';
import AgentEditor from './views/AgentEditor';
import AgentCreator from './views/AgentCreator';
import Onboarding from './views/Onboarding';

/** Client-side redirect: navigates on mount. */
function Redir({ to, id }) {
  useEffect(() => {
    let target = to;
    // Handle /sessions/:id → /chat?session=:id
    if (id && !to) {
      target = `/chat?session=${id}`;
    }
    route(target, true);
  }, [to, id]);
  return null;
}

export function App() {
  const [authState, setAuthState] = useState('loading');

  const checkAuth = async () => {
    try {
      const res = await fetch('/api/auth/check', { credentials: 'include' });
      const data = await res.json();
      setAuthState(data.state || 'ready');
    } catch (_) {
      setAuthState('ready');
    }
  };

  useEffect(() => { checkAuth(); }, []);

  if (authState === 'loading') {
    return (
      <div style="display:flex;justify-content:center;align-items:center;height:100vh;background:var(--bg);color:var(--text-muted)">
        Loading...
      </div>
    );
  }

  if (authState === 'setup') {
    return <Setup onComplete={() => setAuthState('ready')} />;
  }

  if (authState === 'login') {
    return <Login onComplete={() => setAuthState('ready')} />;
  }

  return (
    <Layout>
      <Router>
        {/* ── Primary routes ── */}
        <Dashboard path="/" />
        <Chat path="/chat" />
        <Agents path="/agents" />
        <AgentCreator path="/agents/create" />
        <AgentCreator path="/agents/:id/edit" />
        <AgentEditor path="/agents/:id" />
        <Marketplace path="/marketplace" />
        <Knowledge path="/knowledge" />
        <Settings path="/settings" />
        <Onboarding path="/onboarding" />

        {/* ── All redirects ── */}
        {/* M5: Sessions absorbed into Chat */}
        <Redir path="/sessions" to="/chat" />
        <Redir path="/sessions/:id" />

        {/* M2: Page merges */}
        <Redir path="/memory" to="/knowledge" />
        <Redir path="/graph" to="/knowledge?tab=graph" />
        <Redir path="/skills" to="/marketplace" />
        <Redir path="/tools" to="/settings?tab=tools" />
        <Redir path="/mcp" to="/marketplace?section=mcp" />
        <Redir path="/teams" to="/agents?tab=teams" />
        <Redir path="/delegation" to="/agents?tab=teams" />

        {/* M3: Settings hub */}
        <Redir path="/providers" to="/settings?tab=ai-models" />
        <Redir path="/channels" to="/settings?tab=channels" />
        <Redir path="/cron" to="/settings?tab=scheduled-tasks" />
        <Redir path="/tunnel" to="/settings?tab=external-access" />

        {/* M4: Dashboard consolidation */}
        <Redir path="/activity" to="/?tab=activity" />
        <Redir path="/audit" to="/?tab=activity" />
        <Redir path="/budget" to="/?tab=budget" />
        <Redir path="/health" to="/" />
      </Router>
    </Layout>
  );
}
