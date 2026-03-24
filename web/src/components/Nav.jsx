import { h } from 'preact';
import { useState, useEffect } from 'preact/hooks';
import { getCurrentUrl, route } from 'preact-router';

export function Nav({ open, onNavigate }) {
  const [url, setUrl] = useState(getCurrentUrl());
  const [theme, setTheme] = useState(localStorage.getItem('sageclaw-theme') || 'dark');

  useEffect(() => {
    const update = () => setUrl(getCurrentUrl());
    window.addEventListener('popstate', update);

    const orig = history.pushState;
    history.pushState = function () {
      orig.apply(this, arguments);
      update();
    };

    document.documentElement.setAttribute('data-theme', theme);

    return () => {
      window.removeEventListener('popstate', update);
      history.pushState = orig;
    };
  }, []);

  const toggleTheme = () => {
    const next = theme === 'dark' ? 'light' : 'dark';
    setTheme(next);
    localStorage.setItem('sageclaw-theme', next);
    document.documentElement.setAttribute('data-theme', next);
  };

  const link = (href, label) => (
    <a href={href}
      class={url === href || (href !== '/' && url.startsWith(href)) ? 'active' : ''}
      onClick={(e) => {
        e.preventDefault();
        route(href);
        setUrl(href);
        if (onNavigate) onNavigate();
      }}>
      {label}
    </a>
  );

  const logout = async () => {
    await fetch('/api/auth/logout', { method: 'POST', credentials: 'include' });
    window.location.reload();
  };

  return (
    <nav class={`sidebar ${open ? 'open' : ''}`}>
      <div style="display:flex;justify-content:space-between;align-items:center;padding:0 16px 16px;border-bottom:1px solid var(--border);margin-bottom:8px">
        <div class="sidebar-logo" style="padding:0;border:none;margin:0">SageClaw</div>
        <button class="theme-toggle" onClick={toggleTheme} title={`Switch to ${theme === 'dark' ? 'light' : 'dark'} theme`}>
          {theme === 'dark' ? '\u2600' : '\u263D'}
        </button>
      </div>

      <div class="nav-section-label">Core</div>
      {link('/', 'Overview')}
      {link('/chat', 'Chat')}
      {link('/agents', 'Agents')}


      <div class="nav-section-label">Conversations</div>
      {link('/sessions', 'Sessions')}
      {link('/activity', 'Activity')}


      <div class="nav-section-label">Data</div>
      {link('/memory', 'Memory')}
      {link('/graph', 'Knowledge Graph')}
      {link('/audit', 'Audit')}


      <div class="nav-section-label">Connectivity</div>
      {link('/providers', 'Providers')}
      {link('/channels', 'Channels')}
      {link('/tunnel', 'Tunnel')}


      <div class="nav-section-label">Capabilities</div>
      {link('/skills', 'Skills')}
      {link('/tools', 'Tools')}
      {link('/mcp', 'MCP Servers')}
      {link('/cron', 'Cron')}
      {link('/teams', 'Teams')}
      {link('/delegation', 'Delegation')}


      <div class="nav-section-label">System</div>
      {link('/budget', 'Budget')}
      {link('/health', 'Health')}
      {link('/settings', 'Settings')}

      <div style="flex:1" />
      <a href="#" onClick={logout} style="color:var(--error);font-size:12px;padding:8px 16px">Logout</a>
    </nav>
  );
}
