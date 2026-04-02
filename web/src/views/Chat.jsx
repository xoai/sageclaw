import { h } from 'preact';
import { useState, useEffect, useLayoutEffect, useRef, useCallback } from 'preact/hooks';
import { SessionPanel } from '../components/SessionPanel';
import { ToolTimeline } from '../components/ToolTimeline';
import { ExampleCards } from '../components/ExampleCards';
import { MagicCreate } from '../components/MagicCreate';
import { Breadcrumb } from '../components/Breadcrumb';
import { IconPaperclip, IconArrowUp, IconStore, IconSparkle, IconChevronLeft, IconX, IconLoader } from '../components/Icons';
import snarkdown from 'snarkdown';
import DOMPurify from 'dompurify';

function renderMarkdown(text) {
  if (!text) return '';
  return DOMPurify.sanitize(snarkdown(text));
}

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
  // 'empty' | 'pick-agent' | 'chat' | 'magic-create'
  const [rightPanel, setRightPanel] = useState('empty');

  // Mobile: which panel is visible.
  const [mobileView, setMobileView] = useState('list'); // 'list' | 'conversation'

  // Session list state
  const [webSessions, setWebSessions] = useState([]);
  const [listLoading, setListLoading] = useState(true);

  // Agent picker state
  const [agents, setAgents] = useState([]);
  const [agentsLoading, setAgentsLoading] = useState(false);
  const [homepageAgent, setHomepageAgent] = useState(null); // Selected agent on homepage

  // Chat state
  const [messages, setMessages] = useState([]);
  const [input, setInput] = useState('');
  const [sending, setSending] = useState(false);
  const [streaming, setStreaming] = useState('');
  const [selectedSession, setSelectedSession] = useState(null);
  const [selectedAgent, setSelectedAgent] = useState('');
  const [selectedAgentName, setSelectedAgentName] = useState('');
  // Consent is handled globally by Layout.jsx (works on all pages).
  // Chat.jsx only tracks streaming state for "Waiting for permission..." text.
  const [noProvider, setNoProvider] = useState(false);
  const [toolSteps, setToolSteps] = useState([]);
  const [toolCollapsed, setToolCollapsed] = useState(false);
  const [attachedFiles, setAttachedFiles] = useState([]);  // [{file, name, size, preview?}]
  const bottomRef = useRef(null);
  const messagesContainerRef = useRef(null);
  const timerRef = useRef(null);
  const sseRef = useRef(null);
  const initialScrollDone = useRef(false);
  const pendingSend = useRef(false); // Auto-send after homepage → chat transition
  const chatTextareaRef = useRef(null);

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
    const chatId = chatIdRef.current;
    const match = agentId
      ? sessions.find(s => s.channel === 'web' && s.chat_id === chatId && s.agent_id === agentId)
      : sessions.find(s => s.channel === 'web' && s.chat_id === chatId);
    return match ? match.id : null;
  }, []);

  // Load agents eagerly so examples are available in homepage mode.
  const loadAgents = useCallback(async () => {
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
  }, []);

  // Init: load sessions, agents, check providers, handle ?session= param.
  useEffect(() => {
    loadSessions();
    loadAgents();
    fetch('/api/health').then(r => r.json()).then(h => {
      if (h.providers) {
        const hasProvider = Object.values(h.providers).some(s => s === 'connected');
        if (!hasProvider) setNoProvider(true);
      }
    }).catch(() => {});

    // Check for ?session= or ?agent= query param (from redirect).
    const params = new URLSearchParams(window.location.search);
    const sessionParam = params.get('session');
    const agentParam = params.get('agent');
    if (sessionParam) {
      callRPC('sessions.get', { id: sessionParam }).then(async ({ data: sess }) => {
        if (sess) {
          openSessionById(sessionParam, sess.agent_id, sess.agent_name || sess.agent_id);
        }
      }).catch(() => {});
    } else if (agentParam) {
      // Auto-start chat with specified agent (from onboarding redirect).
      fetch('/api/v2/agents', { credentials: 'include' }).then(r => r.json()).then(data => {
        if (Array.isArray(data)) {
          const webAgents = data.filter(a => {
            const serve = a.channels_serve;
            return !serve || serve.length === 0 || serve.includes('web');
          });
          setAgents(webAgents);
          const match = webAgents.find(a => a.id === agentParam);
          startNewChat(match || { id: agentParam, name: agentParam });
        }
      }).catch(() => {});
    }

    // Listen for navigation to "/" (e.g. clicking "Home" in breadcrumb) — reset to empty state.
    const handleNavHome = () => {
      const path = window.location.pathname;
      const search = window.location.search;
      if (path === '/' && !search) {
        goHome();
      }
    };
    window.addEventListener('popstate', handleNavHome);

    // Global keyboard shortcuts.
    const handleGlobalKeys = (e) => {
      // Escape: dismiss overlays, go back to empty/list
      if (e.key === 'Escape') {
        if (rightPanel === 'magic-create') { setRightPanel('empty'); setMobileView('list'); }
        else if (rightPanel === 'pick-agent') { setRightPanel('empty'); setMobileView('list'); }
      }
      // Cmd/Ctrl+Shift+O: new chat (agent picker)
      if ((e.metaKey || e.ctrlKey) && e.shiftKey && e.key === 'O') {
        e.preventDefault();
        showAgentPicker();
      }
    };
    window.addEventListener('keydown', handleGlobalKeys);

    return () => {
      if (timerRef.current) clearTimeout(timerRef.current);
      if (sseRef.current) sseRef.current.close();
      window.removeEventListener('popstate', handleNavHome);
      window.removeEventListener('keydown', handleGlobalKeys);
      // Clean up file preview object URLs.
      attachedFiles.forEach(af => { if (af.preview) URL.revokeObjectURL(af.preview); });
    };
  }, [rightPanel]);

  // Auto-send when transitioning from homepage to chat with pending input.
  useEffect(() => {
    if (rightPanel === 'chat' && pendingSend.current && input.trim()) {
      pendingSend.current = false;
      send();
    }
  }, [rightPanel]);

  // Initial scroll: jump to bottom before paint (no visible jump).
  useLayoutEffect(() => {
    if (rightPanel === 'chat' && messages.length > 0 && !initialScrollDone.current) {
      const container = messagesContainerRef.current;
      if (container) {
        container.scrollTop = container.scrollHeight;
        initialScrollDone.current = true;
      }
    }
  }, [rightPanel, messages.length]);

  // Subsequent messages: auto-scroll if user is near bottom.
  // Use instant scroll during streaming (smooth can't keep up with rapid updates).
  useEffect(() => {
    if (rightPanel !== 'chat' || !initialScrollDone.current) return;
    const container = messagesContainerRef.current;
    if (!container) return;
    const nearBottom = container.scrollHeight - container.scrollTop - container.clientHeight < 300;
    if (nearBottom) {
      const behavior = streaming ? 'instant' : 'smooth';
      bottomRef.current?.scrollIntoView({ behavior });
    }
  }, [messages, streaming, toolSteps]);

  // --- Session Actions ---

  const openSessionById = async (id, agentId, agentName) => {
    initialScrollDone.current = false; // Reset for new session.
    setSelectedSession(id);
    setSelectedAgent(agentId);
    setSelectedAgentName(agentName);
    // Restore chatID from session so subsequent messages go to the right session.
    const { data: sess } = await callRPC('sessions.get', { id });
    if (sess && sess.chat_id) {
      chatIdRef.current = sess.chat_id;
    }
    const msgs = await loadMessages(id);
    setMessages(msgs);
    setRightPanel('chat');
    setMobileView('conversation');
  };

  const openSession = async (session) => {
    await openSessionById(session.id, session.agent_id, session.agent_name || session.agent_id);
  };

  const showAgentPicker = async () => {
    setRightPanel('pick-agent');
    setMobileView('conversation');
    if (agents.length === 0) {
      setAgentsLoading(true);
      await loadAgents();
      setAgentsLoading(false);
    }
  };

  const chatIdRef = useRef('web-client');

  const startNewChat = (agent) => {
    // Generate unique chatID so each conversation gets its own session.
    chatIdRef.current = 'web-' + Date.now().toString(36) + Math.random().toString(36).slice(2, 6);
    setSelectedSession(null);
    setSelectedAgent(agent.id);
    setSelectedAgentName(agent.name || agent.id);
    setMessages([]);
    if (!pendingSend.current) setInput(''); // Preserve input for auto-send from homepage
    setSending(false); sendingRef.current = false;
    setStreaming('');
    setRightPanel('chat');
    setMobileView('conversation');
  };

  const backToList = () => {
    setMobileView('list');
  };

  const goHome = () => {
    setRightPanel('empty');
    setSelectedSession(null);
    setMobileView('list');
  };

  // --- File Upload ---

  const fileInputRef = useRef(null);

  const handleFileSelect = (e) => {
    const files = Array.from(e.target.files || []);
    addFiles(files);
    if (fileInputRef.current) fileInputRef.current.value = '';
  };

  const addFiles = (files) => {
    const maxSize = 30 * 1024 * 1024;
    const allowed = ['.txt','.md','.csv','.json','.yaml','.yml','.xml','.toml',
      '.html','.css','.js','.ts','.jsx','.tsx','.go','.py','.rs','.java','.sh',
      '.pdf','.png','.jpg','.jpeg','.gif','.webp'];
    const newFiles = files.filter(f => {
      const ext = '.' + f.name.split('.').pop().toLowerCase();
      return f.size <= maxSize && allowed.includes(ext);
    }).map(f => ({
      file: f,
      name: f.name,
      size: f.size,
      preview: f.type.startsWith('image/') ? URL.createObjectURL(f) : null,
    }));
    setAttachedFiles(prev => [...prev, ...newFiles]);
  };

  const removeFile = (idx) => {
    setAttachedFiles(prev => {
      const next = [...prev];
      if (next[idx]?.preview) URL.revokeObjectURL(next[idx].preview);
      next.splice(idx, 1);
      return next;
    });
  };

  const uploadFiles = async (sessionId) => {
    const uploaded = [];
    for (const af of attachedFiles) {
      const form = new FormData();
      form.append('file', af.file);
      form.append('session_id', sessionId || 'unsorted');
      try {
        const resp = await fetch('/api/upload', { method: 'POST', credentials: 'include', body: form });
        if (resp.ok) {
          const data = await resp.json();
          uploaded.push(data);
        }
      } catch {
        // Upload failed — silently skip this file.
      }
    }
    return uploaded;
  };

  // --- Chat Actions ---

  const sendingRef = useRef(false); // Guard against async double-submit

  const send = async () => {
    const text = input.trim();
    if (!text && attachedFiles.length === 0) return;
    if (sending || sendingRef.current) return;
    sendingRef.current = true;

    // Upload files first if any attached.
    let fileRefs = [];
    if (attachedFiles.length > 0) {
      fileRefs = await uploadFiles(selectedSession || chatIdRef.current);
      setAttachedFiles([]);
    }

    // Build message text with file references.
    let fullText = text;
    if (fileRefs.length > 0) {
      const refs = fileRefs.map(f => `[Attached file: ${f.name} (${f.size} bytes) at ${f.path}]`).join('\n');
      fullText = fullText ? fullText + '\n\n' + refs : refs;
    }

    setInput('');
    if (chatTextareaRef.current) chatTextareaRef.current.style.height = 'auto'; // Reset textarea height
    setMessages(prev => [...prev, { role: 'user', text: fullText }]);
    setSending(true);
    setStreaming('');
    setToolSteps([]);
    setToolCollapsed(false);

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

        if (event.type === 'tool.call') {
          gotChunks = true;
          const toolCall = event.tool_call || {};
          let input = null;
          try { if (toolCall.input) input = JSON.parse(toolCall.input); } catch {}
          setToolSteps(prev => [...prev, {
            id: toolCall.id || 'tc_' + Date.now(),
            name: toolCall.name || 'unknown',
            input,
            status: 'running',
            startedAt: Date.now(),
          }]);
        }

        if (event.type === 'tool.result') {
          gotChunks = true;
          const toolResult = event.tool_result || {};
          setToolSteps(prev => prev.map(s =>
            s.id === toolResult.tool_call_id
              ? { ...s, status: toolResult.is_error ? 'error' : 'done' }
              : s
          ));
        }

        if (event.type === 'consent.needed' && event.consent) {
          gotChunks = true;
          // Consent modal handled by Layout.jsx globally.
          setStreaming('Needs your approval to continue...');
        }

        if (event.type === 'consent.result') {
          setStreaming('');
        }

        if (event.type === 'run.completed') {
          completed = true;
          es.close();
          sseRef.current = null;

          // Mark any remaining running tools as done.
          setToolSteps(prev => prev.map(s =>
            s.status === 'running' ? { ...s, status: 'done' } : s
          ));
          setToolCollapsed(true);

          if (gotChunks && streamText) {
            setStreaming('');
            setMessages(prev => [...prev, { role: 'assistant', text: streamText }]);
            setSending(false); sendingRef.current = false;
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

    const { error } = await callRPC('chat.send', { text: fullText, agent_id: selectedAgent || undefined, chat_id: chatIdRef.current });
    if (error) {
      es.close();
      sseRef.current = null;
      setMessages(prev => [...prev, { role: 'assistant', text: `Something went wrong: ${error}. Try sending your message again.` }]);
      setSending(false); sendingRef.current = false;
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
            text: 'This is taking longer than expected. The agent may still be working \u2014 try refreshing in a moment.'
          }]);
          setSending(false); sendingRef.current = false;
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
          setSending(false); sendingRef.current = false;
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

  // Consent response handled by Layout.jsx globally.

  // Time-based greeting for homepage delight.
  const getGreeting = () => {
    const h = new Date().getHours();
    if (h < 12) return 'Good morning';
    if (h < 17) return 'Good afternoon';
    return 'Good evening';
  };

  // ==================== RENDER ====================

  return (
    <div class="chat-page">
      <Breadcrumb items={[{ label: 'Chat' }]} />
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

        {/* Homepage — hero layout with composer as centerpiece */}
        {rightPanel === 'empty' && (
          <div class="chat-home">
            <div class="chat-home-spacer" />

            <div class="chat-home-content">
              {/* Greeting */}
              {agents.length > 0 && (() => {
                const defaultAgent = agents.find(a => a.id === 'default') || agents[0];
                const currentHomepageAgent = homepageAgent || defaultAgent;
                const examples = currentHomepageAgent.examples || [];
                return (
                  <div class="panel-enter">
                    <div class="chat-home-greeting">
                      <span class="chat-home-avatar" style="font-size:28px">{currentHomepageAgent.avatar || '\u2B50'}</span>
                      <h1 class="chat-home-title">{getGreeting()}</h1>
                    </div>

                    {/* Hero composer */}
                    <div class="chat-composer chat-composer-hero" role="form" aria-label="New message">
                      <textarea
                        placeholder={`Ask ${currentHomepageAgent.name || 'anything'}...`}
                        aria-label={`Message ${currentHomepageAgent.name || currentHomepageAgent.id}`}
                        value={input}
                        onInput={e => { setInput(e.target.value); e.target.style.height = 'auto'; e.target.style.height = Math.min(e.target.scrollHeight, 160) + 'px'; }}
                        onKeyDown={(e) => {
                          if (e.key === 'Enter' && !e.shiftKey && input.trim()) {
                            e.preventDefault();
                            pendingSend.current = true;
                            startNewChat(currentHomepageAgent);
                          }
                        }}
                        rows={2}
                      />
                      <div class="chat-composer-toolbar">
                        <div class="chat-composer-actions">
                          <select
                            class="chat-agent-select"
                            value={currentHomepageAgent.id}
                            aria-label="Select agent"
                            onChange={(e) => {
                              const a = agents.find(ag => ag.id === e.target.value);
                              if (a) setHomepageAgent(a);
                            }}
                          >
                            {agents.map(a => (
                              <option key={a.id} value={a.id}>{a.avatar ? a.avatar + ' ' : ''}{a.name || a.id}</option>
                            ))}
                          </select>
                        </div>
                        <button class="chat-send-btn" onClick={() => {
                          if (input.trim()) {
                            pendingSend.current = true;
                            startNewChat(currentHomepageAgent);
                          }
                        }} disabled={!input.trim()} title="Send" aria-label="Send message">
                          <IconArrowUp width={18} height={18} />
                        </button>
                      </div>
                    </div>
                    <div class="kbd-hint">
                      <kbd>Enter</kbd> to send · <kbd>Shift+Enter</kbd> new line
                    </div>

                    {/* Example cards with generous top spacing */}
                    {examples.length > 0 && (
                      <div style="margin-top:24px">
                        <ExampleCards
                          examples={examples}
                          onSelect={(text) => {
                            startNewChat(currentHomepageAgent);
                            setInput(text);
                          }}
                        />
                      </div>
                    )}
                  </div>
                );
              })()}

              {/* Fallback when no agents loaded yet */}
              {agents.length === 0 && !agentsLoading && (
                <div style="text-align:center">
                  <h1 class="chat-home-title">Welcome to SageClaw</h1>
                  <p class="chat-home-subtitle" style="margin-bottom:24px">Create your first agent to start chatting.</p>
                  <div style="display:flex;gap:8px;justify-content:center">
                    <button class="btn-primary" onClick={() => { setRightPanel('magic-create'); setMobileView('conversation'); }}>
                      Create an agent
                    </button>
                  </div>
                </div>
              )}
              {agentsLoading && <div class="empty" style="padding:24px">Loading agents...</div>}
            </div>

            <div class="chat-home-spacer" />
          </div>
        )}

        {/* Agent Picker */}
        {rightPanel === 'pick-agent' && (
          <div class="panel-enter" style="padding:4px;flex:1;overflow-y:auto;min-height:0" onKeyDown={(e) => { if (e.key === 'Escape') backToList(); }}>
            <div style="display:flex;align-items:center;gap:12px;margin-bottom:16px">
              <button class="btn-secondary chat-back-btn" onClick={backToList} style="padding:6px 12px">
                <IconChevronLeft width={16} height={16} />
              </button>
              <div>
                <h2 style="font-size:var(--text-lg);font-weight:600;margin-bottom:2px">New conversation</h2>
                <p style="color:var(--text-muted);font-size:var(--text-sm)">Pick who you'd like to talk to.</p>
              </div>
            </div>

            {agentsLoading ? (
              <div class="empty" role="status">Loading agents...</div>
            ) : agents.length === 0 ? (
              <div class="card" style="padding:24px;text-align:center">
                <p style="color:var(--text-muted);margin-bottom:12px">No agents yet. Create one to get started.</p>
                <a href="/agents/create" class="btn-primary" style="text-decoration:none">Create your first agent</a>
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
                    <div style="flex:1;min-width:0">
                      <div style="font-weight:600;font-size:var(--text-base);overflow:hidden;text-overflow:ellipsis;white-space:nowrap">{a.name || a.id}</div>
                      <div style="font-size:var(--text-xs);color:var(--text-muted);overflow:hidden;text-overflow:ellipsis;white-space:nowrap">{a.role || 'General assistant'}</div>
                    </div>
                    <span class="badge badge-blue">{a.model || 'strong'}</span>
                  </div>
                ))}
              </div>
            )}
          </div>
        )}

        {/* Magic Create */}
        {rightPanel === 'magic-create' && (
          <div class="chat-empty">
            <MagicCreate
              onCreated={(agent) => {
                // Reload agents to include the new one, then start chat.
                loadAgents().then(() => {
                  startNewChat(agent);
                });
              }}
              onCancel={() => { setRightPanel('empty'); setMobileView('list'); }}
            />
          </div>
        )}

        {/* Chat View */}
        {rightPanel === 'chat' && (
          <div class="chat-container panel-enter">
            <div class="chat-header">
              <button class="btn-secondary" onClick={goHome} style="padding:6px 12px;flex-shrink:0" aria-label="Back to home">
                <IconChevronLeft width={16} height={16} />
              </button>
              <div style="flex:1;min-width:0;overflow:hidden;text-overflow:ellipsis;white-space:nowrap">
                <span style="font-weight:600;font-size:var(--text-base)">{selectedAgentName}</span>
              </div>
            </div>

            {noProvider && (
              <div class="card" style="padding:12px;margin-bottom:12px;border-color:var(--warning)">
                <strong style="color:var(--warning)">No AI provider connected.</strong>
                <span style="color:var(--text-muted);margin-left:8px">
                  <a href="/settings?tab=ai-models">Add your API key</a> to start chatting.
                </span>
              </div>
            )}

            <div class="chat-messages" ref={messagesContainerRef} aria-live="polite" aria-relevant="additions"
              onDragOver={e => { e.preventDefault(); e.stopPropagation(); }}
              onDrop={e => { e.preventDefault(); e.stopPropagation(); addFiles(Array.from(e.dataTransfer.files)); }}>
              {messages.length === 0 && !sending && (() => {
                const currentAgent = agents.find(a => a.id === selectedAgent);
                const examples = currentAgent?.examples || [];
                return (
                  <div class="chat-conv-empty">
                    <div class="chat-conv-empty-inner">
                      {currentAgent?.avatar && <span class="chat-home-avatar" style="font-size:32px;display:block;margin-bottom:8px">{currentAgent.avatar}</span>}
                      <div style="font-size:var(--text-lg);font-weight:600;margin-bottom:4px">{selectedAgentName}</div>
                      <div style="font-size:var(--text-sm);color:var(--text-muted);max-width:36ch;margin:0 auto">{currentAgent?.role || 'Ready when you are.'}</div>
                      {examples.length > 0 && (
                        <div style="max-width:520px;width:100%;margin-top:24px">
                          <ExampleCards examples={examples} onSelect={(text) => setInput(text)} />
                        </div>
                      )}
                    </div>
                  </div>
                );
              })()}
              {messages.map((msg, i) => {
                // Show tool timeline just before the final assistant message — same position whether collapsed or expanded.
                const showTimelineBefore = toolSteps.length > 0
                  && i === messages.length - 1 && msg.role === 'assistant';
                return (
                  <div key={i}>
                    {showTimelineBefore && (
                      <div class="message assistant">
                        <ToolTimeline
                          steps={toolSteps}
                          collapsed={toolCollapsed}
                          onToggle={() => setToolCollapsed(c => !c)}
                        />
                      </div>
                    )}
                    <div class={`message ${msg.role}`}>
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
                      {msg.text && msg.role === 'assistant'
                        ? <div class="message-text markdown-body" dangerouslySetInnerHTML={{ __html: renderMarkdown(msg.text) }} />
                        : msg.text && <div class="message-text">{msg.text}</div>
                      }
                    </div>
                  </div>
                );
              })}

              {/* Tool timeline — shown at bottom only during active streaming (no final message yet) */}
              {toolSteps.length > 0 && (messages.length === 0 || messages[messages.length - 1].role !== 'assistant') && (
                <div class="message assistant">
                  <ToolTimeline
                    steps={toolSteps}
                    collapsed={toolCollapsed}
                    onToggle={() => setToolCollapsed(c => !c)}
                  />
                </div>
              )}

              {streaming && (
                <div class="message assistant">
                  <div class="message-text markdown-body" dangerouslySetInnerHTML={{ __html: renderMarkdown(streaming) + '<span class="cursor-blink">|</span>' }} />
                </div>
              )}

              {sending && !streaming && toolSteps.length === 0 && (
                <div class="message assistant">
                  <div class="message-text" style="color:var(--text-muted)">
                    <span class="thinking-dots">Thinking</span>
                  </div>
                </div>
              )}

              <div ref={bottomRef} />
            </div>

            {/* Consent handled by Layout.jsx global banner */}

            {/* Chat composer */}
            <div class="chat-composer" role="form" aria-label="Send message">
              <input
                ref={fileInputRef}
                type="file"
                multiple
                accept=".txt,.md,.csv,.json,.yaml,.yml,.xml,.toml,.html,.css,.js,.ts,.jsx,.tsx,.go,.py,.rs,.java,.sh,.pdf,.png,.jpg,.jpeg,.gif,.webp"
                style="display:none"
                onChange={handleFileSelect}
              />

              {/* Attached file chips */}
              {attachedFiles.length > 0 && (
                <div style="display:flex;gap:6px;padding-bottom:8px;flex-wrap:wrap">
                  {attachedFiles.map((af, i) => (
                    <span key={i} style="display:inline-flex;align-items:center;gap:4px;background:var(--bg);border:1px solid var(--border);border-radius:8px;padding:3px 10px;font-size:12px;color:var(--text-muted)">
                      {af.preview && <img src={af.preview} alt="" style="width:16px;height:16px;border-radius:2px;object-fit:cover" />}
                      {af.name} ({(af.size / 1024).toFixed(0)}KB)
                      <button onClick={() => removeFile(i)} style="background:none;border:none;color:var(--error);cursor:pointer;padding:0;display:flex" aria-label="Remove file"><IconX width={14} height={14} /></button>
                    </span>
                  ))}
                </div>
              )}

              <textarea
                ref={chatTextareaRef}
                placeholder={`Message ${selectedAgentName}...`}
                value={input}
                onInput={e => { setInput(e.target.value); e.target.style.height = 'auto'; e.target.style.height = Math.min(e.target.scrollHeight, 160) + 'px'; }}
                onKeyDown={handleKeyDown}
                disabled={sending}
                rows={1}
                aria-label={`Message ${selectedAgentName}`}
              />

              <div class="chat-composer-toolbar">
                <div class="chat-composer-actions">
                  <button class="chat-icon-btn" title="Attach file" onClick={() => fileInputRef.current?.click()} disabled={sending}>
                    <IconPaperclip width={16} height={16} />
                  </button>
                  <a href="/marketplace" class="chat-icon-btn" title="Marketplace" style="text-decoration:none">
                    <IconStore width={16} height={16} />
                  </a>
                </div>
                <button class="chat-send-btn" onClick={send} disabled={sending || (!input.trim() && attachedFiles.length === 0)} title="Send" aria-label="Send message">
                  {sending ? <IconLoader width={16} height={16} class="spin" /> : <IconArrowUp width={18} height={18} />}
                </button>
              </div>
            </div>
          </div>
        )}
      </div>
    </div>
    </div>
  );
}
