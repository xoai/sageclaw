import { h } from 'preact';

// Mirror of the Go ToolDisplayMap — maps tool names to display info.
const TOOL_DISPLAY = {
  web_search:      { emoji: '\u{1F50D}', verb: 'Searching',         key: 'query' },
  web_fetch:       { emoji: '\u{1F310}', verb: 'Fetching',          key: 'url' },
  browser:         { emoji: '\u{1F310}', verb: 'Browsing',          key: 'url' },
  handoff:         { emoji: '\u{1F504}', verb: 'Handing off',       key: 'target_agent' },
  spawn:           { emoji: '\u{1F680}', verb: 'Starting sub-agent' },
  delegate:        { emoji: '\u{1F4CB}', verb: 'Delegating',        key: 'agent' },
  memory_search:   { emoji: '\u{1F9E0}', verb: 'Searching memory',  key: 'query' },
  memory_get:      { emoji: '\u{1F9E0}', verb: 'Reading memory' },
  read_file:       { emoji: '\u{1F4D6}', verb: 'Reading',           key: 'path' },
  write_file:      { emoji: '\u{270F}\uFE0F', verb: 'Writing',      key: 'path' },
  edit:            { emoji: '\u{270F}\uFE0F', verb: 'Editing',      key: 'path' },
  execute_command: { emoji: '\u26A1', verb: 'Running command',       key: 'command' },
  create_image:    { emoji: '\u{1F3A8}', verb: 'Creating image',    key: 'prompt' },
  read_image:      { emoji: '\u{1F441}', verb: 'Processing image' },
  read_document:   { emoji: '\u{1F4C4}', verb: 'Reading document' },
};

function resolveDisplay(name, input) {
  // Sub-action disambiguation.
  let key = name;
  if (input && input.action) {
    const composite = name + ':' + input.action;
    if (TOOL_DISPLAY[composite]) key = composite;
  }

  const entry = TOOL_DISPLAY[key];
  if (!entry) {
    // MCP tools.
    if (name.startsWith('mcp_')) {
      return { emoji: '\u{1F50C}', text: 'Using extension' };
    }
    return { emoji: '\u{1F527}', text: 'Running ' + name };
  }

  let detail = '';
  if (entry.key && input && input[entry.key]) {
    detail = String(input[entry.key]);
    if (detail.length > 50) detail = detail.slice(0, 47) + '...';
  }

  return {
    emoji: entry.emoji,
    text: detail ? entry.verb + ': ' + detail : entry.verb,
  };
}

// Team delegation sub-action display.
TOOL_DISPLAY['team_tasks:create'] = { emoji: '\u{1F4CB}', verb: 'Delegating', key: 'assignee' };
TOOL_DISPLAY['team_tasks:list']   = { emoji: '\u{1F4CB}', verb: 'Checking tasks' };
TOOL_DISPLAY['team_tasks:search'] = { emoji: '\u{1F4CB}', verb: 'Searching tasks', key: 'query' };

/**
 * ToolTimeline renders a list of tool call steps during agent processing.
 *
 * Props:
 *   steps: Array of { id, name, input, status, startedAt }
 *     - status: 'running' | 'done' | 'error'
 *   collapsed: boolean (default false)
 *   onToggle: function to toggle collapsed state
 */
export function ToolTimeline({ steps, collapsed, onToggle }) {
  if (!steps || steps.length === 0) return null;

  const doneCount = steps.filter(s => s.status !== 'running').length;
  const total = steps.length;
  const allDone = doneCount === total;

  return (
    <div class="tool-timeline">
      <div class="tool-timeline-header" onClick={onToggle} role="button" tabIndex={0}
        onKeyDown={e => { if (e.key === 'Enter' || e.key === ' ') { e.preventDefault(); onToggle(); } }}>
        <span class="tool-timeline-toggle">{collapsed ? '\u25B6' : '\u25BC'}</span>
        <span class="tool-timeline-summary">
          {allDone
            ? `${total} tool${total > 1 ? 's' : ''} used`
            : `${doneCount}/${total} tools completed`}
        </span>
      </div>

      {!collapsed && (
        <div class="tool-timeline-steps">
          {steps.map(step => {
            const display = resolveDisplay(step.name, step.input);
            const icon = step.status === 'running'
              ? '\u23F3' // hourglass
              : step.status === 'error'
                ? '\u274C'
                : '\u2705';
            const stale = step.status === 'running' && step.startedAt
              && (Date.now() - step.startedAt > 10000);

            return (
              <div key={step.id} class={`tool-step tool-step-${step.status}`}>
                <span class="tool-step-icon">{icon}</span>
                <span class="tool-step-emoji">{display.emoji}</span>
                <span class="tool-step-text">
                  {display.text}
                  {stale && <span class="tool-step-stale"> (still working...)</span>}
                </span>
              </div>
            );
          })}
        </div>
      )}
    </div>
  );
}
