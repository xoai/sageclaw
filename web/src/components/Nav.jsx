import { h } from 'preact';
import { useState, useEffect } from 'preact/hooks';
import { getCurrentUrl, route } from 'preact-router';
import { IconChat, IconBot, IconSkills, IconKnowledge, IconDashboard, IconSettings, StatusDot } from './Icons';

export function Nav({ open, onNavigate }) {
  const [url, setUrl] = useState(getCurrentUrl());
  const [theme, setTheme] = useState(localStorage.getItem('sageclaw-theme') || 'dark');
  const [healthy, setHealthy] = useState(true);
  const [attentionCount, setAttentionCount] = useState(0);

  useEffect(() => {
    const update = () => setUrl(getCurrentUrl());
    window.addEventListener('popstate', update);

    const orig = history.pushState;
    history.pushState = function () {
      orig.apply(this, arguments);
      update();
    };

    document.documentElement.setAttribute('data-theme', theme);

    // Check provider health for status dot.
    const checkHealth = () => {
      fetch('/api/health').then(r => r.json()).then(data => {
        const connected = data?.providers && Object.values(data.providers).some(s => s === 'connected');
        setHealthy(!!connected);
      }).catch(() => setHealthy(false));
    };
    checkHealth();
    const healthInterval = setInterval(checkHealth, 30000);

    // Poll for tasks needing attention (in_review, failed, blocked).
    const checkAttention = () => {
      fetch('/api/teams/attention', { credentials: 'include' }).then(r => r.json()).then(data => {
        setAttentionCount(data?.count || 0);
      }).catch(() => {});
    };
    checkAttention();
    const attentionInterval = setInterval(checkAttention, 15000);

    return () => {
      window.removeEventListener('popstate', update);
      history.pushState = orig;
      clearInterval(healthInterval);
      clearInterval(attentionInterval);
    };
  }, []);

  const toggleTheme = () => {
    const next = theme === 'dark' ? 'light' : 'dark';
    setTheme(next);
    localStorage.setItem('sageclaw-theme', next);
    document.documentElement.setAttribute('data-theme', next);
  };

  const isActive = (href) => {
    const path = url.split('?')[0];
    if (href === '/') return path === '/';
    return path === href || path.startsWith(href + '/');
  };

  const link = (href, label, Icon, badge) => (
    <a
      href={href}
      class={`nav-item ${isActive(href) ? 'active' : ''}`}
      onClick={(e) => {
        e.preventDefault();
        route(href);
        setUrl(href);
        if (onNavigate) onNavigate();
      }}
    >
      <Icon />
      <span>{label}</span>
      {badge > 0 && (
        <span style="margin-left:auto;background:var(--error);color:#fff;font-size:10px;font-weight:700;padding:1px 6px;border-radius:10px;min-width:18px;text-align:center">
          {badge}
        </span>
      )}
    </a>
  );

  const logout = async () => {
    await fetch('/api/auth/logout', { method: 'POST', credentials: 'include' });
    window.location.reload();
  };

  return (
    <nav class={`sidebar ${open ? 'open' : ''}`}>
      <div class="sidebar-header">
        <div class="sidebar-logo">
          <StatusDot ok={healthy} />
          <span>SageClaw</span>
        </div>
        <button class="theme-toggle" onClick={toggleTheme} title={`Switch to ${theme === 'dark' ? 'light' : 'dark'} theme`}>
          {theme === 'dark' ? '\u2600' : '\u263D'}
        </button>
      </div>

      <div class="nav-items">
        {link('/chat', 'Chat', IconChat)}
        {link('/agents', 'Agents', IconBot, attentionCount)}
        {link('/marketplace', 'Marketplace', IconSkills)}
        {link('/knowledge', 'Knowledge', IconKnowledge)}
        {link('/', 'Dashboard', IconDashboard)}
      </div>

      <div style="flex:1" />

      <div class="nav-items nav-bottom">
        {link('/settings', 'Settings', IconSettings)}
        <a href="#" onClick={(e) => { e.preventDefault(); logout(); }} class="nav-item nav-logout">Logout</a>
      </div>
    </nav>
  );
}
