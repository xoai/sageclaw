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
      window.location.reload(); // Token expired — redirect to login.
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
  const [messages, setMessages] = useState([]);
  const [input, setInput] = useState('');
  const [sending, setSending] = useState(false);
  const [streaming, setStreaming] = useState('');
  const [status, setStatus] = useState('loading');
  const [sessions, setSessions] = useState([]);
  const [agents, setAgents] = useState([]);
  const [selectedSession, setSelectedSession] = useState(null);
  const [selectedAgent, setSelectedAgent] = useState('');
  const [consentPrompt, setConsentPrompt] = useState(null);
  const bottomRef = useRef(null);
  const timerRef = useRef(null);
  const sseRef = useRef(null);

  const findWebSession = useCallback(async () => {
    const { data: sessions } = await callRPC('sessions.list', { limit: 50 });
    if (!sessions) return null;
    const ws = sessions.find(s => s.channel === 'web' && s.chat_id === 'web-client');
    return ws ? ws.id : null;
  }, []);

  const loadMessages = useCallback(async (sessionId) => {
    const { data: msgs } = await callRPC('sessions.messages', { id: sessionId, limit: 100 });
    if (!msgs || !Array.isArray(msgs)) return [];
    return msgs.map(m => ({ role: m.role, text: extractText(m.content) })).filter(m => m.text);
  }, []);

  // Init.
  useEffect(() => {
    (async () => {
      try {
        const res = await fetch('/api/health');
        const h = await res.json();
        if (h.providers) {
          const hasProvider = Object.values(h.providers).some(s => s === 'connected');
          if (!hasProvider) setStatus('no-provider');
        }
      } catch {}

      // Load agents for picker.
      try {
        const agentRes = await fetch('/api/agents', { credentials: 'include' });
        const agentData = await agentRes.json();
        if (Array.isArray(agentData) && agentData.length > 0) {
          setAgents(agentData);
          // Default to the first agent.
          if (!selectedAgent) setSelectedAgent(agentData[0].id);
        }
      } catch {}

      // Load sessions for picker.
      const { data: allSess } = await callRPC('sessions.list', { limit: 20 });
      if (allSess) setSessions(allSess);

      // Load current web chat.
      const sid = await findWebSession();
      if (sid) {
        setSelectedSession(sid);
        const msgs = await loadMessages(sid);
        if (msgs.length > 0) setMessages(msgs);
      }
      if (status === 'loading') setStatus('ready');
    })();

    return () => {
      if (timerRef.current) clearTimeout(timerRef.current);
      if (sseRef.current) sseRef.current.close();
    };
  }, []);

  // Auto-scroll on messages or streaming update.
  useEffect(() => {
    bottomRef.current?.scrollIntoView({ behavior: 'smooth' });
  }, [messages, streaming]);

  const send = async () => {
    const text = input.trim();
    if (!text || sending) return;

    setInput('');
    setMessages(prev => [...prev, { role: 'user', text }]);
    setSending(true);
    setStreaming('');

    // Connect SSE for streaming BEFORE sending, so we catch the first chunk.
    let streamText = '';
    let gotChunks = false;
    let completed = false;

    // Close previous SSE if any.
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

        // Tool activity means the agent is working — keep SSE alive.
        if (event.type === 'tool.call' || event.type === 'tool.result') {
          gotChunks = true; // Prevents SSE timeout from firing.
          if (event.type === 'tool.call') {
            setStreaming('Using tools...');
          }
        }

        // Consent prompt — agent needs permission to use a tool.
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
            // Finalize: replace streaming with completed message.
            setStreaming('');
            setMessages(prev => [...prev, { role: 'assistant', text: streamText }]);
            setSending(false);
          } else {
            // No chunks received — fallback to DB poll.
            startPoll();
          }
        }
      } catch {}
    };

    es.onerror = () => {
      // SSE failed — rely on DB polling.
      es.close();
      sseRef.current = null;
      if (!completed && !gotChunks) {
        startPoll();
      }
    };

    // Send the message with selected agent.
    const { error } = await callRPC('chat.send', { text, agent_id: selectedAgent || undefined });
    if (error) {
      es.close();
      sseRef.current = null;
      setMessages(prev => [...prev, { role: 'assistant', text: `Error: ${error}` }]);
      setSending(false);
      setStreaming('');
      return;
    }

    // Also start a delayed DB poll as backup in case SSE doesn't deliver.
    const sseTimeout = setTimeout(() => {
      if (!gotChunks && !completed) {
        es.close();
        sseRef.current = null;
        startPoll();
      }
    }, 120000); // 120s SSE grace period (tool calls need multiple LLM round-trips)

    // Cleanup timeout if SSE works.
    const origClose = es.close.bind(es);
    es.close = () => { clearTimeout(sseTimeout); origClose(); };

    function startPoll() {
      let attempts = 0;
      const poll = async () => {
        attempts++;
        if (attempts > 40) {
          setMessages(prev => [...prev, {
            role: 'assistant',
            text: 'Timed out. The agent may still be processing — check Sessions.'
          }]);
          setSending(false);
          setStreaming('');
          return;
        }

        const sid = await findWebSession();
        if (!sid) {
          timerRef.current = setTimeout(poll, 1500);
          return;
        }

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

  const refresh = async () => {
    const sid = await findWebSession();
    if (sid) {
      const msgs = await loadMessages(sid);
      if (msgs.length > 0) {
        setMessages(msgs);
        setSending(false);
        setStreaming('');
      }
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

  const switchSession = async (sid) => {
    setSelectedSession(sid);
    if (sid) {
      const msgs = await loadMessages(sid);
      setMessages(msgs.length > 0 ? msgs : []);
    } else {
      setMessages([]);
    }
    setSending(false);
    setStreaming('');
  };

  return (
    <div class="chat-container" style="height:calc(100vh - 48px)">
      <div style="display:flex;justify-content:space-between;align-items:center;margin-bottom:12px;gap:12px">
        <h1 style="margin-bottom:0;flex-shrink:0">Chat</h1>
        <div style="display:flex;gap:8px;align-items:center;flex:1;justify-content:flex-end">
          <select value={selectedAgent} onChange={e => setSelectedAgent(e.target.value)}
            style="width:160px;flex-shrink:0">
            {agents.map(a => (
              <option key={a.id} value={a.id}>{a.name || a.id}</option>
            ))}
            {agents.length === 0 && <option value="">No agents</option>}
          </select>
          <select value={selectedSession || ''} onChange={e => switchSession(e.target.value || null)}
            style="width:280px;flex-shrink:0">
            <option value="">New conversation</option>
            {sessions.map(s => (
              <option key={s.id} value={s.id}>
                {s.agent_id} — {s.id?.slice(0, 8)} ({s.updated_at?.slice(0, 10)})
              </option>
            ))}
          </select>
          <button class="btn-small" onClick={refresh} style="flex-shrink:0">Refresh</button>
        </div>
      </div>

      {status === 'no-provider' && (
        <div class="card" style="padding:12px;margin-bottom:12px;border-color:var(--warning)">
          <strong style="color:var(--warning)">No providers connected.</strong>
          <span style="color:var(--text-muted);margin-left:8px">
            Add a provider in <a href="/providers">Providers</a> and restart.
          </span>
        </div>
      )}

      <div class="chat-messages">
        {status === 'loading' && <div class="empty">Loading...</div>}
        {status !== 'loading' && messages.length === 0 && !sending && (
          <div class="empty">Send a message to start chatting with SageClaw.</div>
        )}
        {messages.map((msg, i) => (
          <div key={i} class={`message ${msg.role}`}>
            {msg.role !== 'user' && <div class="message-role">{msg.role}</div>}
            <div class="message-text">{msg.text}</div>
          </div>
        ))}

        {/* Streaming response — shows tokens as they arrive */}
        {streaming && (
          <div class="message assistant">
            <div class="message-role">assistant</div>
            <div class="message-text">{streaming}<span class="cursor-blink">|</span></div>
          </div>
        )}

        {/* Waiting indicator — only if no streaming has started */}
        {sending && !streaming && (
          <div class="message assistant">
            <div class="message-role">assistant</div>
            <div class="message-text" style="opacity:0.5">
              <span class="thinking-dots">Thinking</span>
            </div>
          </div>
        )}

        <div ref={bottomRef} />
      </div>

      {/* Consent modal */}
      {consentPrompt && (
        <div style="position:fixed;inset:0;background:rgba(0,0,0,0.5);display:flex;align-items:center;justify-content:center;z-index:100">
          <div class="card" style="padding:24px;max-width:420px;width:100%">
            <h3 style="margin-top:0">Permission Required</h3>
            <p>The agent wants to use a tool that requires your approval:</p>
            <div style="background:var(--bg);padding:12px;border-radius:6px;margin:12px 0">
              <div><strong>{consentPrompt.tool_name}</strong></div>
              <div style="color:var(--text-muted);font-size:0.9rem;margin-top:4px">
                Group: <span style="text-transform:capitalize">{consentPrompt.group}</span>
                {' '}&middot;{' '}
                Risk: <span class={`badge ${consentPrompt.risk_level === 'sensitive' ? 'badge-red' : 'badge-yellow'}`}>
                  {consentPrompt.risk_level}
                </span>
              </div>
              {consentPrompt.explanation && (
                <div style="color:var(--text-muted);font-size:0.85rem;margin-top:8px">{consentPrompt.explanation}</div>
              )}
            </div>
            <div style="display:flex;gap:8px;justify-content:flex-end;margin-top:16px">
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
