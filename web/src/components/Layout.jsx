import { h } from 'preact';
import { useState, useEffect, useRef } from 'preact/hooks';
import { Nav } from './Nav';

export function Layout({ children }) {
  const [sidebarOpen, setSidebarOpen] = useState(false);
  const [consent, setConsent] = useState(null);
  const pollRef = useRef(null);

  const toggle = () => setSidebarOpen(!sidebarOpen);
  const close = () => setSidebarOpen(false);

  // Poll for pending consent requests every 2 seconds.
  // More reliable than SSE for one-shot events that can be missed.
  // Works on ALL pages — users may chat via Telegram and consent from web.
  useEffect(() => {
    const checkPending = async () => {
      try {
        const res = await fetch('/api/consent/pending', { credentials: 'include' });
        const data = await res.json();
        const pending = Array.isArray(data) ? data : [];

        setConsent(prev => {
          // Auto-dismiss: if currently showing a consent that's no longer pending
          // (answered from another client/tab), clear it.
          if (prev && !pending.some(p => p.nonce === prev.nonce)) {
            return null;
          }
          // Show first pending consent if nothing currently displayed.
          if (!prev && pending.length > 0) {
            const c = pending[0];
            return {
              tool_name: c.tool_name,
              group: c.group,
              risk_level: c.risk_level,
              explanation: c.explanation,
              tool_input: c.tool_input,
              agentName: c.agent_name || c.agent_id,
              nonce: c.nonce,
            };
          }
          return prev;
        });
      } catch {}
    };

    checkPending();
    pollRef.current = setInterval(checkPending, 2000);
    return () => clearInterval(pollRef.current);
  }, []);

  const respondConsent = async (granted, tier = 'once') => {
    if (!consent) return;
    try {
      const payload = consent.nonce
        ? { nonce: consent.nonce, granted, tier }
        : { group: consent.group, granted }; // Legacy fallback.
      await fetch('/api/consent', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        credentials: 'include',
        body: JSON.stringify(payload),
      });
    } catch {}
    setConsent(null);
  };

  return (
    <div class="layout">
      <button class="hamburger" onClick={toggle} aria-label="Menu">
        {sidebarOpen ? '\u2715' : '\u2630'}
      </button>
      <div class={`sidebar-overlay ${sidebarOpen ? 'open' : ''}`} onClick={close} />
      <Nav open={sidebarOpen} onNavigate={close} />
      <main class="content">
        {/* Global consent banner */}
        {consent && (
          <div style="position:fixed;top:0;left:0;right:0;z-index:200;display:flex;justify-content:center;padding:12px;pointer-events:none">
            <div class="card" style="padding:16px 20px;max-width:480px;width:100%;pointer-events:auto;border-color:var(--warning);box-shadow:0 4px 24px rgba(0,0,0,0.4)"
              role="alert">
              <div style="display:flex;align-items:flex-start;gap:12px">
                <span style="font-size:20px;flex-shrink:0">&#9888;</span>
                <div style="flex:1">
                  <div style="font-weight:700;font-size:14px;margin-bottom:4px">Permission Required</div>
                  <div style="font-size:13px;color:var(--text-muted);margin-bottom:2px">
                    {consent.agentName && <span>Agent <strong>{consent.agentName}</strong> wants to use </span>}
                    <strong style="font-family:var(--mono)">{consent.tool_name}</strong>
                  </div>
                  <div style="display:flex;gap:6px;align-items:center;font-size:12px;color:var(--text-muted);margin-bottom:8px">
                    <span style="text-transform:capitalize">{consent.group}</span>
                    <span>&middot;</span>
                    <span class={`badge ${consent.risk_level === 'sensitive' ? 'badge-red' : 'badge-yellow'}`} style="font-size:10px">
                      {consent.risk_level}
                    </span>
                  </div>
                  {consent.explanation && (
                    <div style="font-size:12px;color:var(--text-muted);margin-bottom:8px">{consent.explanation}</div>
                  )}
                  {consent.tool_input && (() => {
                    try {
                      const parsed = JSON.parse(consent.tool_input);
                      return (
                        <pre style="margin-bottom:8px;padding:8px;background:var(--bg-darker,#1a1a2e);border-radius:4px;font-size:10px;font-family:var(--mono);color:var(--text);overflow-x:auto;max-height:120px;white-space:pre-wrap;word-break:break-all">
                          {JSON.stringify(parsed, null, 2)}
                        </pre>
                      );
                    } catch {
                      return (
                        <pre style="margin-bottom:8px;padding:8px;background:var(--bg-darker,#1a1a2e);border-radius:4px;font-size:10px;font-family:var(--mono);color:var(--text);overflow-x:auto;max-height:120px;white-space:pre-wrap;word-break:break-all">
                          {consent.tool_input}
                        </pre>
                      );
                    }
                  })()}
                  <div style="display:flex;gap:8px">
                    <button class="btn-primary" style="padding:6px 16px;font-size:13px" onClick={() => respondConsent(true, 'once')}>Allow once</button>
                    <button class="btn-primary" style="padding:6px 16px;font-size:13px;background:var(--success)" onClick={() => respondConsent(true, 'always')}>Always allow</button>
                    <button class="btn-secondary" style="padding:6px 16px;font-size:13px" onClick={() => respondConsent(false, 'deny')}>Deny</button>
                  </div>
                </div>
              </div>
            </div>
          </div>
        )}
        {children}
      </main>
    </div>
  );
}
