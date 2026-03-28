import { h } from 'preact';
import { useState, useEffect, useRef, useCallback } from 'preact/hooks';
import { SessionPanel } from '../components/SessionPanel';

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

function extractText(content) {
  if (!content) return '';
  if (typeof content === 'string') return content;
  if (Array.isArray(content)) {
    return content.map(c => (typeof c === 'string' ? c : (c && c.text) || '')).join('');
  }
  return content.text || String(content);
}

function extractAudio(content) {
  if (!content || !Array.isArray(content)) return null;
  for (const c of content) {
    if (c && c.type === 'audio' && c.audio) {
      return c.audio;
    }
  }
  return null;
}

export function Chat() {
  // Panel state: what the right panel shows.
  // 'empty' | 'pick-agent' | 'chat'
  const [rightPanel, setRightPanel] = useState('empty');

  // Mobile: which panel is visible.
  const [mobileView, setMobileView] = useState('list'); // 'list' | 'conversation'

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
    return msgs.map(m => {
      const text = extractText(m.content);
      const audio = extractAudio(m.content);
      return { role: m.role, text, audio };
    }).filter(m => m.text || m.audio);
  }, []);

  const findWebSession = useCallback(async (agentId) => {
    const { data: sessions } = await callRPC('sessions.list', { limit: 50 });
    if (!sessions) return null;
    const match = agentId
      ? sessions.find(s => s.channel === 'web' && s.chat_id === 'web-client' && s.agent_id === agentId)
      : sessions.find(s => s.channel === 'web' && s.chat_id === 'web-client');
    return match ? match.id : null;
  }, []);

  // Init: load sessions, check providers, handle ?session= param.
  useEffect(() => {
    loadSessions();
    fetch('/api/health').then(r => r.json()).then(h => {
      if (h.providers) {
        const hasProvider = Object.values(h.providers).some(s => s === 'connected');
        if (!hasProvider) setNoProvider(true);
      }
    }).catch(() => {});

    // Check for ?session= query param (from redirect).
    const params = new URLSearchParams(window.location.search);
    const sessionParam = params.get('session');
    if (sessionParam) {
      callRPC('sessions.get', { id: sessionParam }).then(async ({ data: sess }) => {
        if (sess) {
          openSessionById(sessionParam, sess.agent_id, sess.agent_name || sess.agent_id);
        }
      }).catch(() => {});
    }

    return () => {
      if (timerRef.current) clearTimeout(timerRef.current);
      if (sseRef.current) sseRef.current.close();
    };
  }, []);

  useEffect(() => {
    if (rightPanel === 'chat') {
      bottomRef.current?.scrollIntoView({ behavior: 'smooth' });
    }
  }, [messages, streaming, rightPanel]);

  // --- Session Actions ---

  const openSessionById = async (id, agentId, agentName) => {
    setSelectedSession(id);
    setSelectedAgent(agentId);
    setSelectedAgentName(agentName);
    const msgs = await loadMessages(id);
    setMessages(msgs);
    setRightPanel('chat');
    setMobileView('conversation');
  };

  const openSession = async (session) => {
    await openSessionById(session.id, session.agent_id, session.agent_name || session.agent_id);
  };

  const showAgentPicker = async () => {
    setAgentsLoading(true);
    setRightPanel('pick-agent');
    setMobileView('conversation');
    try {
      const res = await fetch('/api/v2/agents', { credentials: 'include' });
      const data = await res.json();
      if (Array.isArray(data)) {
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
    setRightPanel('chat');
    setMobileView('conversation');
  };

  const backToList = () => {
    setMobileView('list');
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
          // Refresh session list to show updated timestamp.
          loadSessions();
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
            text: 'Timed out. The agent may still be processing.'
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
          loadSessions();
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

  const respondConsent = async (granted, tier = 'once') => {
    if (!consentPrompt) return;
    const payload = consentPrompt.nonce
      ? { nonce: consentPrompt.nonce, granted, tier }
      : { group: consentPrompt.group, granted }; // Legacy fallback.
    await fetch('/api/consent', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      credentials: 'include',
      body: JSON.stringify(payload),
    });
    setConsentPrompt(null);
    const msg = !granted ? 'Permission denied.' : tier === 'always' ? 'Always allowed, continuing...' : 'Permission granted, continuing...';
    setStreaming(msg);
  };

  // ==================== RENDER ====================

  return (
    <div class="chat-split">
      {/* Left: Session Panel */}
      <div class={`chat-split-left ${mobileView === 'list' ? 'mobile-show' : 'mobile-hide'}`}>
        <SessionPanel
          sessions={webSessions}
          loading={listLoading}
          activeId={selectedSession}
          onSelect={openSession}
          onNewChat={showAgentPicker}
        />
      </div>

      {/* Right: Conversation or Agent Picker or Empty */}
      <div class={`chat-split-right ${mobileView === 'conversation' ? 'mobile-show' : 'mobile-hide'}`}>

        {/* Empty state */}
        {rightPanel === 'empty' && (
          <div class="chat-empty">
            <div style="max-width:320px">
              <div style="font-size:32px;margin-bottom:16px;opacity:0.3">{'\u2759'}</div>
              <h2 style="font-size:18px;font-weight:600;margin-bottom:8px">Chat with your agents</h2>
              <p style="color:var(--text-muted);font-size:13px;line-height:1.6;margin-bottom:24px">
                Ask questions, run tasks, or explore ideas. Pick an agent
                and start typing — your conversation history stays here.
              </p>
              <button class="btn-primary" onClick={showAgentPicker}>+ New Chat</button>
              {webSessions.length > 0 && (
                <p style="color:var(--text-muted);font-size:12px;margin-top:12px">
                  or select a recent session from the left
                </p>
              )}
            </div>
          </div>
        )}

        {/* Agent Picker */}
        {rightPanel === 'pick-agent' && (
          <div style="padding:4px" onKeyDown={(e) => { if (e.key === 'Escape') backToList(); }}>
            <div style="display:flex;align-items:center;gap:12px;margin-bottom:16px">
              <button class="btn-secondary chat-back-btn" onClick={backToList} style="padding:6px 12px">
                {'\u2190'}
              </button>
              <div>
                <h2 style="font-size:18px;font-weight:600;margin-bottom:2px">New Chat</h2>
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
        )}

        {/* Chat View */}
        {rightPanel === 'chat' && (
          <div class="chat-container">
            <div class="chat-header">
              <button class="btn-secondary chat-back-btn" onClick={backToList} style="padding:6px 12px;flex-shrink:0" aria-label="Back to chat list">
                {'\u2190'}
              </button>
              <div style="flex:1;min-width:0">
                <span style="font-weight:600;font-size:15px">{selectedAgentName}</span>
              </div>
            </div>

            {noProvider && (
              <div class="card" style="padding:12px;margin-bottom:12px;border-color:var(--warning)">
                <strong style="color:var(--warning)">No providers connected.</strong>
                <span style="color:var(--text-muted);margin-left:8px">
                  Add a provider in <a href="/settings?tab=ai-models">AI Models</a> settings.
                </span>
              </div>
            )}

            <div class="chat-messages" aria-live="polite" aria-relevant="additions">
              {messages.length === 0 && !sending && (
                <div class="empty" style="padding:48px 24px">
                  <div style="font-size:15px;margin-bottom:4px">Chatting with <strong>{selectedAgentName}</strong></div>
                  <div style="font-size:13px;color:var(--text-muted)">Type a message below to get started.</div>
                </div>
              )}
              {messages.map((msg, i) => (
                <div key={i} class={`message ${msg.role}`}>
                  {msg.role !== 'user' && <div class="message-role">{msg.role}</div>}
                  {msg.audio && (
                    <div style="margin-bottom:4px">
                      <audio controls preload="none" style="max-width:100%;height:36px"
                        src={`/api/audio/${msg.audio.file_path.split('/').map(encodeURIComponent).join('/')}`}>
                      </audio>
                      {msg.audio.duration_ms > 0 && (
                        <span style="font-size:11px;color:var(--text-muted);margin-left:8px">
                          {Math.round(msg.audio.duration_ms / 1000)}s
                        </span>
                      )}
                    </div>
                  )}
                  {msg.text && <div class="message-text">{msg.text}</div>}
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
              <div style="position:fixed;inset:0;background:rgba(0,0,0,0.5);display:flex;align-items:center;justify-content:center;z-index:var(--z-modal)"
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
                    <button class="btn-secondary" onClick={() => respondConsent(false, 'deny')}>Deny</button>
                    <button class="btn-primary" onClick={() => respondConsent(true, 'once')}>Allow once</button>
                    <button class="btn-primary" style="background:var(--success)" onClick={() => respondConsent(true, 'always')}>Always allow</button>
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
        )}
      </div>
    </div>
  );
}
