import { h } from 'preact';

/**
 * Left panel showing session list for Chat split-panel layout.
 * Props:
 *   sessions: Array of session objects
 *   loading: boolean
 *   activeId: string | null — currently selected session ID
 *   onSelect: (session) => void
 *   onNewChat: () => void
 */
export function SessionPanel({ sessions, loading, activeId, onSelect, onNewChat }) {
  const timeAgo = (dateStr) => {
    if (!dateStr) return '';
    // SQLite timestamps are UTC but lack the Z suffix — append it so JS parses correctly.
    const utcStr = dateStr.endsWith('Z') ? dateStr : dateStr + 'Z';
    const diff = Date.now() - new Date(utcStr).getTime();
    const mins = Math.floor(diff / 60000);
    if (mins < 1) return 'now';
    if (mins < 60) return `${mins}m`;
    const hrs = Math.floor(mins / 60);
    if (hrs < 24) return `${hrs}h`;
    const days = Math.floor(hrs / 24);
    return `${days}d`;
  };

  return (
    <div class="session-panel">
      <div class="session-panel-header">
        <button class="btn-secondary" style="width:100%;padding:8px;font-weight:500" onClick={onNewChat}>
          + New Chat
        </button>
      </div>

      <div class="session-panel-list">
        {loading && (
          <div style="padding:16px;text-align:center;color:var(--text-muted);font-size:13px">Loading...</div>
        )}

        {!loading && sessions.length === 0 && (
          <div style="padding:24px 16px;color:var(--text-muted);font-size:13px;line-height:1.6">
            <div style="font-size:14px;color:var(--text);font-weight:500;margin-bottom:4px">No conversations yet</div>
            <div>Start a new chat to talk with your agents. Sessions are saved automatically.</div>
          </div>
        )}

        {sessions.map(s => (
          <div
            key={s.id}
            class={`session-item ${activeId === s.id ? 'active' : ''}`}
            onClick={() => onSelect(s)}
            role="button"
            tabIndex={0}
            onKeyDown={(e) => { if (e.key === 'Enter' || e.key === ' ') { e.preventDefault(); onSelect(s); } }}
          >
            <div style="display:flex;justify-content:space-between;align-items:center;gap:8px">
              <span class="session-item-name" style={s.metadata?.has_unread === 'true' ? 'font-weight:700' : ''}>
                {s.metadata?.has_unread === 'true' && <span style="display:inline-block;width:7px;height:7px;border-radius:50%;background:var(--primary);margin-right:6px;vertical-align:middle" />}
                {s.agent_name || s.agent_id}
              </span>
              <span class="session-item-time">{timeAgo(s.updated_at)}</span>
            </div>
            <div class="session-item-preview">
              {s.title || s.label || `${s.message_count || 0} messages`}
            </div>
          </div>
        ))}
      </div>
    </div>
  );
}
