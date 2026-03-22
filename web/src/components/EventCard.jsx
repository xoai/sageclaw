export function EventCard({ event }) {
  const time = new Date().toLocaleTimeString('en-US', { hour12: false });

  const typeClass = {
    'run.started': 'run-start',
    'run.completed': 'run-done',
    'run.failed': 'error',
    'tool.call': 'tool-call',
    'tool.result': '',
    'chunk': '',
  }[event.type] || '';

  const icon = {
    'run.started': '▶',
    'run.completed': '✓',
    'run.failed': '✗',
    'tool.call': '⚡',
    'tool.result': '←',
    'chunk': '  ',
  }[event.type] || '•';

  let detail = event.session_id ? event.session_id.slice(0, 8) : '';
  if (event.text) {
    detail = event.text.length > 80 ? event.text.slice(0, 80) + '...' : event.text;
  }

  return (
    <div class={`event-card ${typeClass}`}>
      <span class="time">[{time}]</span>
      <span>{icon} {event.type}</span>
      {detail && <span style="margin-left: 8px; color: var(--text-muted)">{detail}</span>}
    </div>
  );
}
