import { h } from 'preact';
import { useState, useEffect, useRef, useCallback } from 'preact/hooks';
import { route } from 'preact-router';
import { subscribeEvents } from '../api';

// --- Helpers ---

const STATUS_ORDER = ['pending', 'in_progress', 'blocked', 'in_review', 'completed', 'failed', 'cancelled'];
const KANBAN_COLUMNS = [
  { key: 'pending',     label: 'Pending',     color: 'var(--text-muted)' },
  { key: 'in_progress', label: 'In Progress', color: 'var(--primary)' },
  { key: 'blocked',     label: 'Blocked',     color: 'var(--error)' },
  { key: 'in_review',   label: 'In Review',   color: 'var(--warning)' },
  { key: 'completed',   label: 'Done',        color: 'var(--success)' },
];

const PRIORITY_COLORS = { 3: 'var(--error)', 2: 'var(--warning)', 1: 'var(--primary)', 0: 'var(--text-muted)' };
const PRIORITY_LABELS = { 3: 'Urgent', 2: 'High', 1: 'Normal', 0: 'Low' };

function relativeTime(ts) {
  if (!ts) return '';
  const d = new Date(ts);
  const now = Date.now();
  const diff = Math.floor((now - d.getTime()) / 1000);
  if (diff < 60) return 'just now';
  if (diff < 3600) return `${Math.floor(diff / 60)}m ago`;
  if (diff < 86400) return `${Math.floor(diff / 3600)}h ago`;
  return `${Math.floor(diff / 86400)}d ago`;
}

function statusBadgeClass(status) {
  switch (status) {
    case 'completed': return 'badge-green';
    case 'in_progress': return 'badge-blue';
    case 'blocked': case 'failed': return 'badge-red';
    case 'in_review': return 'badge-yellow';
    case 'cancelled': return 'badge-gray';
    default: return 'badge-gray';
  }
}

// --- TaskCard ---

function PriorityDot({ priority }) {
  const color = PRIORITY_COLORS[priority] || PRIORITY_COLORS[0];
  return (
    <span title={PRIORITY_LABELS[priority] || 'Normal'}
      style={`display:inline-block;width:8px;height:8px;border-radius:50%;background:${color};flex-shrink:0`} />
  );
}

function ProgressBar({ percent }) {
  return (
    <div style="height:4px;background:var(--border);border-radius:2px;flex:1;max-width:80px;min-width:40px">
      <div style={`height:100%;border-radius:2px;background:var(--primary);width:${Math.min(100, percent)}%;transition:width 0.3s`} />
    </div>
  );
}

function TaskCard({ task, expanded, onToggle, onAction, agents }) {
  const agentName = agents[task.assigned_to] || task.assigned_to || '\u2014';
  const subtasks = task.subtask_count || 0;
  const subtasksDone = task.subtasks_done || 0;

  return (
    <div class="card clickable" onClick={() => onToggle(task.id)}
      style={`cursor:pointer;margin-bottom:8px;padding:10px 12px;border-left:3px solid ${
        KANBAN_COLUMNS.find(c => c.key === task.status)?.color || 'var(--border)'
      }`}>
      {/* Collapsed view */}
      <div style="display:flex;align-items:center;gap:8px">
        <PriorityDot priority={task.priority} />
        <span style="flex:1;font-size:13px;font-weight:500;white-space:nowrap;overflow:hidden;text-overflow:ellipsis">
          {task.title}
        </span>
        {task.progress_percent > 0 && task.status !== 'completed' && (
          <ProgressBar percent={task.progress_percent} />
        )}
        {subtasks > 0 && (
          <span style="font-size:11px;color:var(--text-muted);white-space:nowrap">{subtasksDone}/{subtasks}</span>
        )}
        <span class="badge" style="font-size:10px;padding:1px 6px;white-space:nowrap;color:var(--text-muted)">
          {task.identifier || ''}
        </span>
        <span style="font-size:11px;color:var(--text-muted);white-space:nowrap;max-width:80px;overflow:hidden;text-overflow:ellipsis">
          {agentName}
        </span>
        <span style="font-size:11px;color:var(--text-muted);white-space:nowrap">{relativeTime(task.updated_at || task.created_at)}</span>
      </div>

      {/* Expanded view */}
      {expanded && (
        <div style="margin-top:12px;border-top:1px solid var(--border);padding-top:10px">
          <div style="display:flex;gap:16px;margin-bottom:8px;flex-wrap:wrap">
            <div style="font-size:12px;color:var(--text-muted)">
              Status: <span class={`badge ${statusBadgeClass(task.status)}`}>{task.status.replace('_', ' ')}</span>
            </div>
            <div style="font-size:12px;color:var(--text-muted)">
              Priority: {PRIORITY_LABELS[task.priority] || 'Normal'}
            </div>
            <div style="font-size:12px;color:var(--text-muted)">
              Assigned: {agentName}
            </div>
            {task.batch_id && (
              <div style="font-size:12px;color:var(--text-muted)">Batch: {task.batch_id.slice(0, 8)}</div>
            )}
          </div>

          {task.description && (
            <div style="font-size:13px;color:var(--text);margin-bottom:8px;white-space:pre-wrap;max-height:200px;overflow-y:auto">
              {task.description}
            </div>
          )}

          {task.blocked_by && (
            <div style="font-size:12px;color:var(--error);margin-bottom:8px">
              Blocked by: {task.blocked_by}
            </div>
          )}

          {task.error_message && (
            <div style="font-size:12px;color:var(--error);margin-bottom:8px;font-family:var(--mono)">
              Error: {task.error_message}
            </div>
          )}

          {task.result && (
            <div style="margin-bottom:8px">
              <div style="font-size:11px;color:var(--text-muted);margin-bottom:4px">Result</div>
              <div style="font-size:12px;background:var(--bg);border-radius:4px;padding:8px;max-height:200px;overflow-y:auto;white-space:pre-wrap;font-family:var(--mono)">
                {task.result}
              </div>
            </div>
          )}

          {/* Cross-links */}
          {task.session_id && (
            <div style="margin-bottom:8px">
              <a href={`/chat?session=${task.session_id}`}
                style="font-size:12px;color:var(--primary);text-decoration:underline"
                onClick={e => e.stopPropagation()}>
                View in chat
              </a>
            </div>
          )}

          {/* Intervention controls */}
          <div style="display:flex;gap:6px;margin-top:8px;flex-wrap:wrap">
            {(task.status === 'pending' || task.status === 'in_progress' || task.status === 'blocked') && (
              <button class="btn-small btn-danger" onClick={(e) => { e.stopPropagation(); onAction(task.id, 'cancel'); }}>
                Cancel
              </button>
            )}
            {task.status === 'in_review' && (
              <>
                <button class="btn-small" style="color:var(--success);border-color:var(--success)"
                  onClick={(e) => { e.stopPropagation(); onAction(task.id, 'approve'); }}>
                  Approve
                </button>
                <button class="btn-small btn-danger"
                  onClick={(e) => { e.stopPropagation(); onAction(task.id, 'reject'); }}>
                  Reject
                </button>
              </>
            )}
            {task.status === 'failed' && (
              <button class="btn-small" style="color:var(--warning);border-color:var(--warning)"
                onClick={(e) => { e.stopPropagation(); onAction(task.id, 'retry'); }}>
                Retry
              </button>
            )}
          </div>
        </div>
      )}
    </div>
  );
}

// --- Kanban View ---

const DONE_LIMIT = 10;

function KanbanColumn({ col, tasks, expandedId, onToggle, onAction, agents }) {
  let colTasks = tasks.filter(t => {
    if (col.key === 'completed') return t.status === 'completed' || t.status === 'failed' || t.status === 'cancelled';
    return t.status === col.key;
  });
  colTasks.sort((a, b) => (b.priority || 0) - (a.priority || 0));

  const isDone = col.key === 'completed';
  const [showAll, setShowAll] = useState(false);
  const displayTasks = isDone && !showAll ? colTasks.slice(0, DONE_LIMIT) : colTasks;

  return (
    <div key={col.key} class="kanban-column">
      <div class="kanban-column-header">
        <span style={`display:inline-block;width:8px;height:8px;border-radius:50%;background:${col.color}`} />
        <span style="font-size:13px;font-weight:600">{col.label}</span>
        <span style="font-size:12px;color:var(--text-muted);margin-left:auto">{colTasks.length}</span>
      </div>
      <div class="kanban-column-body">
        {displayTasks.length === 0 && (
          <div style="padding:16px;text-align:center;color:var(--text-muted);font-size:12px">No tasks</div>
        )}
        {displayTasks.map(task => (
          <TaskCard key={task.id} task={task} expanded={expandedId === task.id}
            onToggle={onToggle} onAction={onAction} agents={agents} />
        ))}
        {isDone && colTasks.length > DONE_LIMIT && !showAll && (
          <button class="btn-small" style="width:100%;text-align:center;margin-top:4px"
            onClick={() => setShowAll(true)}>
            Show all ({colTasks.length})
          </button>
        )}
      </div>
    </div>
  );
}

function KanbanView({ tasks, expandedId, onToggle, onAction, agents }) {
  return (
    <div class="kanban-board">
      {KANBAN_COLUMNS.map(col => (
        <KanbanColumn key={col.key} col={col} tasks={tasks} expandedId={expandedId}
          onToggle={onToggle} onAction={onAction} agents={agents} />
      ))}
    </div>
  );
}

// --- Agent Lane View ---

function AgentLaneView({ tasks, team, expandedId, onToggle, onAction, agents }) {
  // Build member list: lead first, then members.
  const members = [];
  if (team) {
    if (team.lead) members.push({ id: team.lead, role: 'lead' });
    try {
      const cfg = JSON.parse(team.config || '{}');
      (cfg.members || []).forEach(id => {
        if (id !== team.lead) members.push({ id, role: 'member' });
      });
    } catch {}
  }

  // Group tasks by assigned_to.
  const tasksByAgent = {};
  tasks.forEach(t => {
    const key = t.assigned_to || 'unassigned';
    if (!tasksByAgent[key]) tasksByAgent[key] = [];
    tasksByAgent[key].push(t);
  });

  // Ensure all members appear even with no tasks.
  members.forEach(m => {
    if (!tasksByAgent[m.id]) tasksByAgent[m.id] = [];
  });

  // Add unassigned row if needed.
  const allRows = [...members.map(m => m.id)];
  if (tasksByAgent['unassigned']?.length > 0) allRows.push('unassigned');

  return (
    <div style="display:flex;flex-direction:column;gap:12px">
      {allRows.map(agentId => {
        const agentTasks = tasksByAgent[agentId] || [];
        const isLead = members.find(m => m.id === agentId)?.role === 'lead';
        const name = agents[agentId] || agentId;
        const current = agentTasks.find(t => t.status === 'in_progress');
        const queued = agentTasks.filter(t => t.status === 'pending' || t.status === 'blocked');
        const done = agentTasks.filter(t => t.status === 'completed' || t.status === 'cancelled');
        const review = agentTasks.filter(t => t.status === 'in_review');
        const failed = agentTasks.filter(t => t.status === 'failed');

        // Status dot color.
        const dotColor = current ? 'var(--success)' : queued.length > 0 ? 'var(--primary)' : 'var(--text-muted)';

        return (
          <div key={agentId} class="card" style="padding:12px 16px">
            <div style="display:flex;align-items:center;gap:10px;margin-bottom:8px">
              <span style={`display:inline-block;width:8px;height:8px;border-radius:50%;background:${dotColor}`} />
              <span style="font-size:14px;font-weight:600">{name}</span>
              {isLead && <span class="badge badge-purple" style="font-size:10px">Lead</span>}
              <span style="font-size:12px;color:var(--text-muted);margin-left:auto">
                {agentTasks.length} task{agentTasks.length !== 1 ? 's' : ''}
              </span>
            </div>

            {/* Current task */}
            {current && (
              <div style="margin-bottom:6px">
                <div style="font-size:11px;color:var(--text-muted);margin-bottom:4px;text-transform:uppercase;letter-spacing:0.5px">Current</div>
                <TaskCard task={current} expanded={expandedId === current.id}
                  onToggle={onToggle} onAction={onAction} agents={agents} />
              </div>
            )}

            {/* In review */}
            {review.length > 0 && (
              <div style="margin-bottom:6px">
                <div style="font-size:11px;color:var(--text-muted);margin-bottom:4px;text-transform:uppercase;letter-spacing:0.5px">In Review ({review.length})</div>
                {review.map(t => (
                  <TaskCard key={t.id} task={t} expanded={expandedId === t.id}
                    onToggle={onToggle} onAction={onAction} agents={agents} />
                ))}
              </div>
            )}

            {/* Queued tasks */}
            {queued.length > 0 && (
              <div style="margin-bottom:6px">
                <div style="font-size:11px;color:var(--text-muted);margin-bottom:4px;text-transform:uppercase;letter-spacing:0.5px">Queued ({queued.length})</div>
                {queued.map(t => (
                  <TaskCard key={t.id} task={t} expanded={expandedId === t.id}
                    onToggle={onToggle} onAction={onAction} agents={agents} />
                ))}
              </div>
            )}

            {/* Failed */}
            {failed.length > 0 && (
              <div style="margin-bottom:6px">
                <div style="font-size:11px;color:var(--text-muted);margin-bottom:4px;text-transform:uppercase;letter-spacing:0.5px">Failed ({failed.length})</div>
                {failed.map(t => (
                  <TaskCard key={t.id} task={t} expanded={expandedId === t.id}
                    onToggle={onToggle} onAction={onAction} agents={agents} />
                ))}
              </div>
            )}

            {/* Completed (muted) */}
            {done.length > 0 && (
              <div style="opacity:0.6">
                <div style="font-size:11px;color:var(--text-muted);margin-bottom:4px;text-transform:uppercase;letter-spacing:0.5px">Done ({done.length})</div>
                {done.slice(0, 3).map(t => (
                  <TaskCard key={t.id} task={t} expanded={expandedId === t.id}
                    onToggle={onToggle} onAction={onAction} agents={agents} />
                ))}
                {done.length > 3 && (
                  <div style="font-size:12px;color:var(--text-muted);padding:4px 12px">+{done.length - 3} more</div>
                )}
              </div>
            )}

            {agentTasks.length === 0 && (
              <div style="font-size:12px;color:var(--text-muted);padding:4px 0">No tasks assigned</div>
            )}
          </div>
        );
      })}
    </div>
  );
}

// --- Main Taskboard ---

export function Taskboard({ id: teamId }) {
  const [tasks, setTasks] = useState([]);
  const [team, setTeam] = useState(null);
  const [agents, setAgents] = useState({});  // id → name map
  const [view, setView] = useState('kanban'); // 'kanban' | 'agents'
  const [expandedId, setExpandedId] = useState(null);
  const [loading, setLoading] = useState(true);
  const seqMap = useRef({});
  const [actionLoading, setActionLoading] = useState(null);
  const [rejectTaskId, setRejectTaskId] = useState(null);
  const [rejectFeedback, setRejectFeedback] = useState('');

  // Auto-expand task from query param (?task=TSK-N).
  useEffect(() => {
    const params = new URLSearchParams(window.location.search);
    const taskParam = params.get('task');
    if (taskParam && tasks.length > 0) {
      const match = tasks.find(t => t.identifier === taskParam);
      if (match) setExpandedId(match.id);
    }
  }, [tasks]);

  // Load team info + agents.
  useEffect(() => {
    fetch('/api/teams').then(r => r.json()).then(data => {
      const t = (data || []).find(t => t.id === teamId);
      setTeam(t || null);
    }).catch(() => {});

    fetch('/api/v2/agents').then(r => r.json()).then(data => {
      const map = {};
      (data || []).forEach(a => { map[a.id] = a.name || a.id; });
      setAgents(map);
    }).catch(() =>
      fetch('/api/agents').then(r => r.json()).then(data => {
        const map = {};
        (data || []).forEach(a => { map[a.id] = a.name || a.id; });
        setAgents(map);
      }).catch(() => {})
    );
  }, [teamId]);

  // Load full board state.
  const loadBoard = useCallback(() => {
    fetch(`/api/teams/tasks/${teamId}`).then(r => r.json()).then(data => {
      setTasks(data || []);
      // Seed seq map from loaded data.
      seqMap.current = {};
      (data || []).forEach(t => {
        if (t.updated_at) seqMap.current[t.id] = new Date(t.updated_at).getTime();
      });
      setLoading(false);
    }).catch(() => { setTasks([]); setLoading(false); });
  }, [teamId]);

  useEffect(() => { loadBoard(); }, [loadBoard]);

  // SSE subscription with reconnection resync (4.8).
  useEffect(() => {
    const unsub = subscribeEvents((event) => {
      if (typeof event.type === 'string' && event.type.startsWith('team.task.')) {
        const { task_id, seq, task } = event;
        if (!task_id || !task) return;
        // Check if this event is for our team.
        if (task.team_id && task.team_id !== teamId) return;

        setTasks(prev => {
          const prevSeq = seqMap.current[task_id] || 0;
          if (seq && seq <= prevSeq) return prev;
          if (seq) seqMap.current[task_id] = seq;

          const idx = prev.findIndex(t => t.id === task_id);
          if (idx >= 0) {
            const updated = [...prev];
            updated[idx] = { ...prev[idx], ...task };
            return updated;
          }
          // New task.
          return [task, ...prev];
        });
      }

      // SSE reconnection: EventSource auto-reconnects; on reconnect we refetch.
      // The subscribeEvents wrapper handles this via onerror → auto-reconnect.
      // We detect reconnection by checking for a special "reconnected" event type,
      // or we just refetch periodically as fallback.
    });

    // Fallback: refetch every 30s to catch missed events (4.8).
    const interval = setInterval(loadBoard, 30000);

    return () => { unsub(); clearInterval(interval); };
  }, [teamId, loadBoard]);

  const toggleExpanded = useCallback((id) => {
    setExpandedId(prev => prev === id ? null : id);
  }, []);

  // Task actions (intervention controls).
  const handleAction = useCallback(async (taskId, action) => {
    if (action === 'reject') {
      setRejectTaskId(taskId);
      setRejectFeedback('');
      return;
    }

    setActionLoading(taskId);
    try {
      await fetch(`/api/teams/tasks/${teamId}/action`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        credentials: 'include',
        body: JSON.stringify({ task_id: taskId, action }),
      });
      // Optimistic update.
      setTasks(prev => prev.map(t => {
        if (t.id !== taskId) return t;
        if (action === 'cancel') return { ...t, status: 'cancelled' };
        if (action === 'approve') return { ...t, status: 'completed' };
        if (action === 'retry') return { ...t, status: 'pending', error_message: '' };
        return t;
      }));
    } catch (err) {
      // Revert: refetch board state.
      loadBoard();
    }
    setActionLoading(null);
  }, [teamId, loadBoard]);

  const submitReject = useCallback(async () => {
    if (!rejectTaskId) return;
    setActionLoading(rejectTaskId);
    try {
      await fetch(`/api/teams/tasks/${teamId}/action`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        credentials: 'include',
        body: JSON.stringify({ task_id: rejectTaskId, action: 'reject', feedback: rejectFeedback }),
      });
      setTasks(prev => prev.map(t =>
        t.id === rejectTaskId ? { ...t, status: 'pending' } : t
      ));
    } catch { loadBoard(); }
    setActionLoading(null);
    setRejectTaskId(null);
    setRejectFeedback('');
  }, [rejectTaskId, rejectFeedback, teamId, loadBoard]);

  // Empty states (4.7).
  if (loading) {
    return (
      <div>
        <div style="display:flex;align-items:center;gap:8px;margin-bottom:16px">
          <button class="btn-small" onClick={() => route('/agents?tab=teams')}>&larr; Teams</button>
        </div>
        <div style="text-align:center;padding:48px;color:var(--text-muted)">Loading board...</div>
      </div>
    );
  }

  if (!team) {
    return (
      <div>
        <div style="display:flex;align-items:center;gap:8px;margin-bottom:16px">
          <button class="btn-small" onClick={() => route('/agents?tab=teams')}>&larr; Teams</button>
        </div>
        <div style="text-align:center;padding:48px">
          <div style="font-size:24px;margin-bottom:8px">Team not found</div>
          <div style="color:var(--text-muted)">This team may have been deleted.</div>
        </div>
      </div>
    );
  }

  const allDone = tasks.length > 0 && tasks.every(t => t.status === 'completed' || t.status === 'cancelled');

  return (
    <div>
      {/* Header */}
      <div style="display:flex;align-items:center;gap:12px;margin-bottom:16px;flex-wrap:wrap">
        <button class="btn-small" onClick={() => route('/agents?tab=teams')}>&larr; Teams</button>
        <h1 style="margin:0;font-size:20px">{team.name}</h1>
        {team.description && (
          <span style="font-size:13px;color:var(--text-muted)">{team.description}</span>
        )}
        <div style="margin-left:auto;display:flex;gap:6px">
          <button class={view === 'kanban' ? 'btn-primary' : 'btn-secondary'}
            style="padding:5px 12px;font-size:12px"
            onClick={() => setView('kanban')}>Kanban</button>
          <button class={view === 'agents' ? 'btn-primary' : 'btn-secondary'}
            style="padding:5px 12px;font-size:12px"
            onClick={() => setView('agents')}>Agents</button>
        </div>
      </div>

      {/* Summary stats */}
      <div style="display:flex;gap:16px;margin-bottom:16px;flex-wrap:wrap">
        {KANBAN_COLUMNS.slice(0, 4).map(col => {
          const count = tasks.filter(t => t.status === col.key).length;
          return (
            <div key={col.key} style="font-size:12px;color:var(--text-muted)">
              <span style={`color:${col.color};font-weight:600;font-family:var(--mono)`}>{count}</span> {col.label}
            </div>
          );
        })}
        <div style="font-size:12px;color:var(--text-muted)">
          <span style={`color:var(--success);font-weight:600;font-family:var(--mono)`}>
            {tasks.filter(t => t.status === 'completed').length}
          </span> Done
        </div>
      </div>

      {/* Empty state: no tasks */}
      {tasks.length === 0 && (
        <div class="card" style="text-align:center;padding:48px">
          <div style="font-size:18px;margin-bottom:8px">No tasks yet</div>
          <div style="color:var(--text-muted);font-size:13px">
            Tasks will appear here when the team lead delegates work. Send a message to the lead agent to get started.
          </div>
        </div>
      )}

      {/* All done state */}
      {allDone && tasks.length > 0 && (
        <div class="card" style="text-align:center;padding:24px;border-color:var(--success);margin-bottom:16px">
          <div style="font-size:16px;font-weight:600;color:var(--success);margin-bottom:4px">All tasks complete</div>
          <div style="color:var(--text-muted);font-size:13px">
            {tasks.filter(t => t.status === 'completed').length} completed, {tasks.filter(t => t.status === 'cancelled').length} cancelled
          </div>
        </div>
      )}

      {/* Board views */}
      {tasks.length > 0 && view === 'kanban' && (
        <KanbanView tasks={tasks} expandedId={expandedId}
          onToggle={toggleExpanded} onAction={handleAction} agents={agents} />
      )}

      {tasks.length > 0 && view === 'agents' && (
        <AgentLaneView tasks={tasks} team={team} expandedId={expandedId}
          onToggle={toggleExpanded} onAction={handleAction} agents={agents} />
      )}

      {/* Reject feedback modal */}
      {rejectTaskId && (
        <div class="modal-overlay" onClick={() => setRejectTaskId(null)} role="dialog" aria-modal="true">
          <div class="modal-content" onClick={e => e.stopPropagation()} style="max-width:400px">
            <h2 style="font-size:16px;margin-bottom:12px">Reject Task</h2>
            <div class="form-group">
              <label style="font-size:13px;color:var(--text-muted);margin-bottom:4px;display:block">
                Feedback (optional)
              </label>
              <textarea rows="3" style="width:100%" value={rejectFeedback}
                onInput={e => setRejectFeedback(e.target.value)}
                placeholder="What needs to change?" />
            </div>
            <div style="display:flex;gap:8px;margin-top:12px">
              <button class="btn-primary" style="background:var(--error)" onClick={submitReject}>
                Reject
              </button>
              <button class="btn-secondary" onClick={() => setRejectTaskId(null)}>Cancel</button>
            </div>
          </div>
        </div>
      )}
    </div>
  );
}
