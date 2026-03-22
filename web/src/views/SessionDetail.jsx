import { h } from 'preact';
import { useState, useEffect } from 'preact/hooks';
import { route } from 'preact-router';
import { rpc } from '../api';

const formatTokens = (n) => {
  if (!n || n === 0) return '-';
  if (n > 1000000) return (n / 1000000).toFixed(1) + 'M';
  if (n > 1000) return (n / 1000).toFixed(1) + 'K';
  return n.toString();
};

export function SessionDetail({ id }) {
  const [messages, setMessages] = useState([]);
  const [session, setSession] = useState(null);
  const [loading, setLoading] = useState(true);
  const [showToolDetails, setShowToolDetails] = useState({});
  const [continuing, setContinuing] = useState(false);

  useEffect(() => {
    Promise.all([
      rpc('sessions.get', { id }),
      rpc('sessions.messages', { id, limit: 200 }),
    ])
      .then(([sess, msgs]) => {
        setSession(sess);
        setMessages(msgs || []);
      })
      .catch(() => {})
      .finally(() => setLoading(false));
  }, [id]);

  const toggleTool = (idx) => {
    setShowToolDetails(prev => ({ ...prev, [idx]: !prev[idx] }));
  };

  const continueHere = async () => {
    setContinuing(true);
    try {
      const result = await rpc('session.continue', {
        source_session_id: id,
        agent_id: session?.agent_id || '',
      });
      if (result?.session_id) {
        route(`/chat?session=${result.session_id}`);
      }
    } catch (err) {
      alert('Failed to continue: ' + err.message);
    }
    setContinuing(false);
  };

  if (loading) return <div class="empty">Loading...</div>;

  const formatTime = (ts) => {
    if (!ts) return '';
    return ts.slice(11, 19);
  };

  return (
    <div>
      <div style="margin-bottom:16px">
        <a href="/sessions" style="font-size:13px;color:var(--text-muted)">← Sessions</a>
      </div>

      <div style="display:flex;justify-content:space-between;align-items:center;margin-bottom:12px">
        <h1 style="margin-bottom:0">
          {session?.label || `Session ${id?.slice(0, 8)}`}
          <span style="font-size:13px;color:var(--text-muted);margin-left:12px">
            {session?.channel} · {session?.agent_id}
          </span>
        </h1>
        <div style="display:flex;gap:8px;align-items:center">
          {session?.channel !== 'web' && (
            <button class="btn-primary" onClick={continueHere} disabled={continuing}>
              {continuing ? 'Creating...' : 'Continue here'}
            </button>
          )}
        </div>
      </div>

      {/* Session metadata bar */}
      {session && (
        <div style="display:flex;gap:16px;margin-bottom:20px;font-size:12px;color:var(--text-muted);flex-wrap:wrap">
          <span>{messages.length} messages</span>
          {session.model && <span>Model: {session.model}</span>}
          {(session.input_tokens > 0 || session.output_tokens > 0) && (
            <span>Tokens: {formatTokens(session.input_tokens)} in / {formatTokens(session.output_tokens)} out</span>
          )}
          {session.compaction_count > 0 && (
            <span style="color:var(--warning)">Compacted {session.compaction_count}x</span>
          )}
          <span>Status: {session.status || 'active'}</span>
          {session.spawned_by && <span>Continued from: {session.spawned_by.slice(0, 8)}</span>}
        </div>
      )}

      {messages.length === 0 ? (
        <div class="empty">No messages in this session.</div>
      ) : (
        <div class="chat-messages" style="max-height:none;padding-bottom:24px">
          {messages.map((msg, i) => {
            const isUser = msg.role === 'user';
            const contents = msg.content || [];
            const textParts = contents.filter(c => c.type === 'text' && c.text);
            const toolCalls = contents.filter(c => c.type === 'tool_call' || c.type === 'tool_use');
            const toolResults = contents.filter(c => c.type === 'tool_result');

            return (
              <div key={i}>
                {textParts.length > 0 && (
                  <div class={`message ${msg.role}`}>
                    <div class="message-role">{msg.role}</div>
                    <div class="message-text">
                      {textParts.map((c, j) => <span key={j}>{c.text}</span>)}
                    </div>
                    {msg.created_at && (
                      <div style="font-size:10px;color:var(--text-muted);margin-top:6px;opacity:0.6">
                        {formatTime(msg.created_at)}
                      </div>
                    )}
                  </div>
                )}

                {toolCalls.length > 0 && (
                  <div style="margin:0 0 16px 0;max-width:80%">
                    {toolCalls.map((tc, j) => {
                      const toolName = tc.tool_call?.name || tc.name || 'tool';
                      const toolInput = tc.tool_call?.input || tc.input;
                      const key = `${i}-${j}`;
                      return (
                        <div key={j} class="card" style="padding:8px 12px;border-left:3px solid var(--primary);cursor:pointer"
                          onClick={() => toggleTool(key)}>
                          <div style="display:flex;align-items:center;gap:6px">
                            <span style="color:var(--primary);font-size:12px">⚡</span>
                            <span style="font-family:var(--mono);font-size:12px;color:var(--primary)">{toolName}</span>
                          </div>
                          {showToolDetails[key] && toolInput && (
                            <pre style="margin-top:6px;font-size:11px;color:var(--text-muted);white-space:pre-wrap;max-height:200px;overflow-y:auto">
                              {typeof toolInput === 'string' ? toolInput : JSON.stringify(toolInput, null, 2)}
                            </pre>
                          )}
                        </div>
                      );
                    })}
                  </div>
                )}

                {toolResults.length > 0 && (
                  <div style="margin:0 0 16px 0;max-width:80%">
                    {toolResults.map((tr, j) => {
                      const resultText = tr.tool_result?.content || tr.content || '';
                      const key = `r${i}-${j}`;
                      return (
                        <div key={j} class="card" style="padding:8px 12px;border-left:3px solid var(--success);cursor:pointer"
                          onClick={() => toggleTool(key)}>
                          <div style="font-size:11px;color:var(--success)">← result</div>
                          {showToolDetails[key] && (
                            <pre style="margin-top:4px;font-size:11px;color:var(--text-muted);white-space:pre-wrap;max-height:200px;overflow-y:auto">
                              {typeof resultText === 'string' ? resultText.slice(0, 500) : JSON.stringify(resultText, null, 2)}
                            </pre>
                          )}
                        </div>
                      );
                    })}
                  </div>
                )}
              </div>
            );
          })}
        </div>
      )}
    </div>
  );
}
