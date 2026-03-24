import { h } from 'preact';
import { useState, useEffect } from 'preact/hooks';
import Router from 'preact-router';
import { Layout } from './components/Layout';
import { Activity } from './views/Activity';
import { Sessions } from './views/Sessions';
import { SessionDetail } from './views/SessionDetail';
import { Memory } from './views/Memory';
import { Chat } from './views/Chat';
import { Providers } from './views/Providers';
import { Agents } from './views/Agents';
import { Channels } from './views/Channels';
import { Teams } from './views/Teams';
import { Skills } from './views/Skills';
import { Settings } from './views/Settings';
import { Setup } from './views/Setup';
import { Login } from './views/Login';
import Graph from './views/Graph';
import Audit from './views/Audit';
import Tools from './views/Tools';
import Cron from './views/Cron';
import Delegation from './views/Delegation';
import Health from './views/Health';
import { Overview } from './views/Overview';
import Tunnel from './views/Tunnel';
import AgentEditor from './views/AgentEditor';
import AgentCreator from './views/AgentCreator';
import Budget from './views/Budget';
import MCPServers from './views/MCPServers';

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
        <Overview path="/" />
        <Activity path="/activity" />
        <Sessions path="/sessions" />
        <SessionDetail path="/sessions/:id" />
        <Memory path="/memory" />
        <Graph path="/graph" />
        <Chat path="/chat" />
        <Audit path="/audit" />
        <Providers path="/providers" />
        <Agents path="/agents" />
        <AgentCreator path="/agents/create" />
        <AgentCreator path="/agents/:id/edit" />
        <AgentEditor path="/agents/:id" />
        <Channels path="/channels" />
        <Teams path="/teams" />
        <Delegation path="/delegation" />
        <Skills path="/skills" />
        <Tools path="/tools" />
        <MCPServers path="/mcp" />
        <Cron path="/cron" />
        <Budget path="/budget" />
        <Tunnel path="/tunnel" />
        <Health path="/health" />
        <Settings path="/settings" />
      </Router>
    </Layout>
  );
}
