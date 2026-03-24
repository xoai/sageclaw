import { h } from 'preact';
import { useState, useEffect, useRef, useCallback } from 'preact/hooks';

// Direct fetch wrapper.
async function callRPC(method, params) {
  try {
    const res = await fetch('/rpc', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      credentials: 'include',
      body: JSON.stringify({ jsonrpc: '2.0', id: Date.now(), method, params: params || {} }),
    });
    if (res.status === 401) {
      window.location.reload();
      return { error: 'Session expired' };
    }
    const data = await res.json();
    if (data.error) return { error: data.error };
    return { data: data.result };
  } catch (e) {
    return { error: e.message };
  }
}

// Extract text from canonical content blocks.
function extractText(content) {
  if (!content) return '';
  if (typeof content === 'string') return content;
  if (Array.isArray(content)) {
    return content.map(c => (typeof c === 'string' ? c : (c && c.text) || '')).join('');
  }
  return content.text || String(content);
}

export function Chat() {
  // View state: 'list' | 'pick-agent' | 'chat'
  const [view, setView] = useState('list');

  // Session list state
  const [webSessions, setWebSessions] = useState([]);
  const [listLoading, setListLoading] = useState(true);

  // Agent picker state
  const [agents, setAgents] = useState([]);
  const [agentsLoading, setAgentsLoading] = useState(false);

  // Chat state
  const [messages, setMessages] = useState([]);
  const [input, setInput] = useState('');
  const [sending, setSending] = useState(false);
  const [streaming, setStreaming] = useState('');
  const [selectedSession, setSelectedSession] = useState(null);
  const [selectedAgent, setSelectedAgent] = useState('');
  const [selectedAgentName, setSelectedAgentName] = useState('');
  const [consentPrompt, setConsentPrompt] = useState(null);
  const [noProvider, setNoProvider] = useState(false);
  const bottomRef = useRef(null);
  const timerRef = useRef(null);
  const sseRef = useRef(null);

  // Load web sessions for the list view.
  const loadSessions = useCallback(async () => {
    setListLoading(true);
    const { data: allSess } = await callRPC('sessions.list', { limit: 50 });
    if (allSess) {
      const web = allSess
        .filter(s => s.channel === 'web')
        .sort((a, b) => (b.updated_at || '').localeCompare(a.updated_at || ''));
      setWebSessions(web);
    }
    setListLoading(false);
  }, []);

  const loadMessages = useCallback(async (sessionId) => {
    const { data: msgs } = await callRPC('sessions.messages', { id: sessionId, limit: 100 });
    if (!msgs || !Array.isArray(msgs)) return [];
    return msgs.map(m => ({ role: m.role, text: extractText(m.content) })).filter(m => m.text);
  }, []);

  const findWebSession = useCallback(async (agentId) => {
    const { data: sessions } = await callRPC('sessions.list', { limit: 50 });
    if (!sessions) return null;
    // Find a web session for this specific agent, or any web session.
    const match = agentId
      ? sessions.find(s => s.channel === 'web' && s.chat_id === 'web-client' && s.agent_id === agentId)
      : sessions.find(s => s.channel === 'web' && s.chat_id === 'web-client');
    return match ? match.id : null;
  }, []);

  // Init: load sessions + check provider status.
  useEffect(() => {
    loadSessions();
    fetch('/api/health').then(r => r.json()).then(h => {
      if (h.providers) {
        const hasProvider = Object.values(h.providers).some(s => s === 'connected');
        if (!hasProvider) setNoProvider(true);
      }
    }).catch(() => {});

    return () => {
      if (timerRef.current) clearTimeout(timerRef.current);
      if (sseRef.current) sseRef.current.close();
    };
  }, []);

  // Auto-scroll on messages or streaming update.
  useEffect(() => {
    if (view === 'chat') {
      bottomRef.current?.scrollIntoView({ behavior: 'smooth' });
    }
  }, [messages, streaming, view]);

  // --- Session List Actions ---

  const openSession = async (session) => {
    setSelectedSession(session.id);
    setSelectedAgent(session.agent_id);
    setSelectedAgentName(session.agent_name || session.agent_id);
    const msgs = await loadMessages(session.id);
    setMessages(msgs);
    setView('chat');
  };

  const showAgentPicker = async () => {
    setAgentsLoading(true);
    setView('pick-agent');
    try {
      const res = await fetch('/api/v2/agents', { credentials: 'include' });
      const data = await res.json();
      if (Array.isArray(data)) {
        // Only show agents that serve the web channel.
        // Empty serve list = serves all channels (including web).
        const webAgents = data.filter(a => {
          const serve = a.channels_serve;
          return !serve || serve.length === 0 || serve.includes('web');
        });
        setAgents(webAgents);
      }
    } catch {}
    setAgentsLoading(false);
  };

  const startNewChat = (agent) => {
    setSelectedSession(null);
    setSelectedAgent(agent.id);
    setSelectedAgentName(agent.name || agent.id);
    setMessages([]);
    setInput('');
    setSending(false);
    setStreaming('');
    setView('chat');
  };

  const backToList = () => {
    if (sseRef.current) { sseRef.current.close(); sseRef.current = null; }
    if (timerRef.current) { clearTimeout(timerRef.current); timerRef.current = null; }
    setSending(false);
    setStreaming('');
    setConsentPrompt(null);
    setView('list');
    loadSessions();
  };

  // --- Chat Actions ---

  const send = async () => {
    const text = input.trim();
    if (!text || sending) return;

    setInput('');
    setMessages(prev => [...prev, { role: 'user', text }]);
    setSending(true);
    setStreaming('');

    let streamText = '';
    let gotChunks = false;
    let completed = false;

    if (sseRef.current) sseRef.current.close();

    const es = new EventSource('/events', { withCredentials: true });
    sseRef.current = es;

    es.onmessage = (e) => {
      try {
        const event = JSON.parse(e.data);

        if (event.type === 'chunk' && event.text) {
          gotChunks = true;
          streamText += event.text;
          setStreaming(streamText);
        }

        if (event.type === 'tool.call' || event.type === 'tool.result') {
          gotChunks = true;
          if (event.type === 'tool.call') setStreaming('Using tools...');
        }

        if (event.type === 'consent.needed' && event.consent) {
          gotChunks = true;
          setConsentPrompt(event.consent);
          setStreaming('Waiting for permission...');
        }

        if (event.type === 'run.completed') {
          completed = true;
          es.close();
          sseRef.current = null;

          if (gotChunks && streamText) {
            setStreaming('');
            setMessages(prev => [...prev, { role: 'assistant', text: streamText }]);
            setSending(false);
          } else {
            startPoll();
          }
        }
      } catch {}
    };

    es.onerror = () => {
      es.close();
      sseRef.current = null;
      if (!completed && !gotChunks) startPoll();
    };

    const { error } = await callRPC('chat.send', { text, agent_id: selectedAgent || undefined });
    if (error) {
      es.close();
      sseRef.current = null;
      setMessages(prev => [...prev, { role: 'assistant', text: `Error: ${error}` }]);
      setSending(false);
      setStreaming('');
      return;
    }

    const sseTimeout = setTimeout(() => {
      if (!gotChunks && !completed) {
        es.close();
        sseRef.current = null;
        startPoll();
      }
    }, 120000);

    const origClose = es.close.bind(es);
    es.close = () => { clearTimeout(sseTimeout); origClose(); };

    function startPoll() {
      let attempts = 0;
      const poll = async () => {
        attempts++;
        if (attempts > 40) {
          setMessages(prev => [...prev, {
            role: 'assistant',
            text: 'Timed out. The agent may still be processing \u2014 check Sessions.'
          }]);
          setSending(false);
          setStreaming('');
          return;
        }

        const sid = await findWebSession(selectedAgent);
        if (!sid) {
          timerRef.current = setTimeout(poll, 1500);
          return;
        }

        if (!selectedSession) setSelectedSession(sid);

        const msgs = await loadMessages(sid);
        if (msgs.length > 0 && msgs[msgs.length - 1].role === 'assistant') {
          setMessages(msgs);
          setSending(false);
          setStreaming('');
          return;
        }

        timerRef.current = setTimeout(poll, 1500);
      };

      timerRef.current = setTimeout(poll, 2000);
    }
  };

  const handleKeyDown = (e) => {
    if (e.key === 'Enter' && !e.shiftKey) {
      e.preventDefault();
      send();
    }
  };

  const respondConsent = async (granted) => {
    if (!consentPrompt) return;
    await fetch('/api/consent', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      credentials: 'include',
      body: JSON.stringify({ group: consentPrompt.group, granted }),
    });
    setConsentPrompt(null);
    setStreaming(granted ? 'Permission granted, continuing...' : 'Permission denied.');
  };

  // ==================== RENDER ====================

  // --- Session List View ---
  if (view === 'list') {
    return (
      <div>
        <div style="display:flex;justify-content:space-between;align-items:center;margin-bottom:16px">
          <div>
            <h1 style="margin-bottom:2px">Chat</h1>
            <p style="color:var(--text-muted);font-size:13px">Talk to your agents in real time.</p>
          </div>
          <button class="btn-primary" onClick={showAgentPicker}>+ New Chat</button>
        </div>

        {noProvider && (
          <div class="card" style="padding:12px;margin-bottom:12px;border-color:var(--warning)">
            <strong style="color:var(--warning)">No providers connected.</strong>
            <span style="color:var(--text-muted);margin-left:8px">
              Add a provider in <a href="/providers">Providers</a> and restart.
            </span>
          </div>
        )}

        {listLoading ? (
          <div class="empty" role="status">Loading sessions...</div>
        ) : webSessions.length === 0 ? (
          <div class="card" style="padding:32px;text-align:center">
            <p style="color:var(--text-muted);font-size:15px;margin-bottom:12px">No chat sessions yet.</p>
            <button class="btn-primary" onClick={showAgentPicker}>Start Your First Chat</button>
          </div>
        ) : (
          <div style="display:flex;flex-direction:column;gap:8px">
            {webSessions.map(s => (
              <div key={s.id} class="card clickable" style="padding:14px;cursor:pointer" role="button" tabIndex={0}
                onClick={() => openSession(s)} onKeyDown={(e) => { if (e.key === 'Enter' || e.key === ' ') { e.preventDefault(); openSession(s); } }}>
                <div style="display:flex;justify-content:space-between;align-items:center">
                  <div style="display:flex;align-items:center;gap:10px;flex:1;min-width:0">
                    <div style="flex:1;min-width:0">
                      <div style="display:flex;align-items:center;gap:8px;margin-bottom:2px">
                        <span style="font-weight:600;font-size:14px">{s.agent_name || s.agent_id}</span>
                        <span class="badge badge-blue" style="font-size:10px">{s.kind || 'dm'}</span>
                      </div>
                      <div style="font-size:12px;color:var(--text-muted);white-space:nowrap;overflow:hidden;text-overflow:ellipsis">
                        {s.label || s.id?.slice(0, 8)}
                      </div>
                    </div>
                  </div>
                  <div style="text-align:right;flex-shrink:0;margin-left:16px">
                    <div style="font-size:12px;color:var(--text-muted)">
                      {s.updated_at?.slice(0, 10)}
                    </div>
                    <div style="font-size:11px;color:var(--text-muted)">
                      {s.message_count || 0} msgs
                    </div>
                  </div>
                </div>
              </div>
            ))}
          </div>
        )}
      </div>
    );
  }

  // --- Agent Picker View ---
  if (view === 'pick-agent') {
    return (
      <div>
        <div style="display:flex;align-items:center;gap:12px;margin-bottom:16px">
          <button class="btn-secondary" onClick={() => setView('list')} style="padding:6px 12px">
            {'\u2190'} Back
          </button>
          <div>
            <h1 style="margin-bottom:2px">New Chat</h1>
            <p style="color:var(--text-muted);font-size:13px">Choose an agent to chat with.</p>
          </div>
        </div>

        {agentsLoading ? (
          <div class="empty" role="status">Loading agents...</div>
        ) : agents.length === 0 ? (
          <div class="card" style="padding:24px;text-align:center">
            <p style="color:var(--text-muted);margin-bottom:12px">No agents configured.</p>
            <a href="/agents/create" class="btn-primary" style="text-decoration:none">Create an Agent</a>
          </div>
        ) : (
          <div style="display:flex;flex-direction:column;gap:8px">
            {agents.map(a => (
              <div key={a.id} class="card clickable" style="padding:14px;cursor:pointer;display:flex;align-items:center;gap:12px"
                role="button" tabIndex={0}
                onClick={() => startNewChat(a)} onKeyDown={(e) => { if (e.key === 'Enter' || e.key === ' ') { e.preventDefault(); startNewChat(a); } }}>
                {a.avatar ? (
                  <span style="font-size:24px">{a.avatar}</span>
                ) : (
                  <span style="display:inline-flex;align-items:center;justify-content:center;width:32px;height:32px;border-radius:50%;background:color-mix(in srgb, var(--primary) 15%, transparent);color:var(--primary);font-weight:700;font-size:14px;font-family:var(--mono);flex-shrink:0">
                    {(a.name || a.id || '?').charAt(0).toUpperCase()}
                  </span>
                )}
                <div style="flex:1">
                  <div style="font-weight:600;font-size:14px">{a.name || a.id}</div>
                  <div style="font-size:12px;color:var(--text-muted)">{a.role || 'No role defined'}</div>
                </div>
                <span class="badge badge-blue">{a.model || 'strong'}</span>
              </div>
            ))}
          </div>
        )}
      </div>
    );
  }

  // --- Chat View ---
  return (
    <div class="chat-container" style="height:calc(100vh - 48px)">
      <div class="chat-header">
        <button class="btn-secondary" onClick={backToList} style="padding:6px 12px;flex-shrink:0" aria-label="Back to chat list">
          {'\u2190'}
        </button>
        <div style="flex:1;min-width:0">
          <h1 style="margin-bottom:0;font-size:18px">{selectedAgentName}</h1>
        </div>
      </div>

      {noProvider && (
        <div class="card" style="padding:12px;margin-bottom:12px;border-color:var(--warning)">
          <strong style="color:var(--warning)">No providers connected.</strong>
          <span style="color:var(--text-muted);margin-left:8px">
            Add a provider in <a href="/providers">Providers</a> and restart.
          </span>
        </div>
      )}

      <div class="chat-messages" aria-live="polite" aria-relevant="additions">
        {messages.length === 0 && !sending && (
          <div class="empty">Send a message to start chatting with {selectedAgentName}.</div>
        )}
        {messages.map((msg, i) => (
          <div key={i} class={`message ${msg.role}`}>
            {msg.role !== 'user' && <div class="message-role">{msg.role}</div>}
            <div class="message-text">{msg.text}</div>
          </div>
        ))}

        {streaming && (
          <div class="message assistant">
            <div class="message-role">assistant</div>
            <div class="message-text">{streaming}<span class="cursor-blink">|</span></div>
          </div>
        )}

        {sending && !streaming && (
          <div class="message assistant">
            <div class="message-role">assistant</div>
            <div class="message-text" style="color:var(--text-muted)">
              <span class="thinking-dots">Thinking</span>
            </div>
          </div>
        )}

        <div ref={bottomRef} />
      </div>

      {/* Consent modal */}
      {consentPrompt && (
        <div style="position:fixed;inset:0;background:rgba(0,0,0,0.5);display:flex;align-items:center;justify-content:center;z-index:100"
          role="dialog" aria-modal="true" aria-labelledby="consent-title">
          <div class="card" style="padding:24px;max-width:420px;width:100%">
            <h3 id="consent-title" style="margin-top:0;font-size:16px;font-weight:700">Permission Required</h3>
            <p style="font-size:13px;color:var(--text-muted);margin-bottom:12px">The agent wants to use a tool that requires your approval.</p>
            <div style="background:var(--bg);padding:14px;border-radius:6px;margin-bottom:16px">
              <div style="font-weight:700;font-family:var(--mono);font-size:13px;margin-bottom:6px">{consentPrompt.tool_name}</div>
              <div style="display:flex;gap:8px;align-items:center;font-size:12px;color:var(--text-muted)">
                <span style="text-transform:capitalize">{consentPrompt.group}</span>
                <span>&middot;</span>
                <span class={`badge ${consentPrompt.risk_level === 'sensitive' ? 'badge-red' : 'badge-yellow'}`}>
                  {consentPrompt.risk_level}
                </span>
              </div>
              {consentPrompt.explanation && (
                <div style="color:var(--text-muted);font-size:12px;margin-top:8px;line-height:1.5">{consentPrompt.explanation}</div>
              )}
            </div>
            <div style="display:flex;gap:8px;justify-content:flex-end">
              <button class="btn-secondary" onClick={() => respondConsent(false)}>Deny</button>
              <button class="btn-primary" onClick={() => respondConsent(true)}>Allow</button>
            </div>
          </div>
        </div>
      )}

      <div class="chat-input-row">
        <input
          type="text"
          class="chat-input"
          placeholder="Type a message..."
          value={input}
          onInput={e => setInput(e.target.value)}
          onKeyDown={handleKeyDown}
          disabled={sending}
        />
        <button class="btn-primary" onClick={send} disabled={sending}>
          {sending ? '...' : 'Send'}
        </button>
      </div>
    </div>
  );
}
