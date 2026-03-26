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
  useEffect(() => {
    const checkPending = async () => {
      try {
        const res = await fetch('/api/consent/pending', { credentials: 'include' });
        const data = await res.json();
        if (Array.isArray(data) && data.length > 0 && !consent) {
          const c = data[0]; // Show the first pending consent.
          setConsent({
            tool_name: c.tool_name,
            group: c.group,
            risk_level: c.risk_level,
            explanation: c.explanation,
            agentId: c.agent_id,
          });
        }
      } catch {}
    };

    checkPending();
    pollRef.current = setInterval(checkPending, 2000);
    return () => clearInterval(pollRef.current);
  }, []);

  const respondConsent = async (granted) => {
    if (!consent) return;
    try {
      await fetch('/api/consent', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        credentials: 'include',
        body: JSON.stringify({ group: consent.group, granted }),
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
                    {consent.agentId && <span>Agent <strong>{consent.agentId}</strong> wants to use </span>}
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
                  <div style="display:flex;gap:8px">
                    <button class="btn-primary" style="padding:6px 16px;font-size:13px" onClick={() => respondConsent(true)}>Allow</button>
                    <button class="btn-secondary" style="padding:6px 16px;font-size:13px" onClick={() => respondConsent(false)}>Deny</button>
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
