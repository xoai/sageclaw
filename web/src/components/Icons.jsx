import { h } from 'preact';

// Inline SVG icons — Lucide-style, 18x18, stroke-based, currentColor.
// No external icon library dependency.

const defaults = { width: 18, height: 18, viewBox: '0 0 24 24', fill: 'none', stroke: 'currentColor', 'stroke-width': 2, 'stroke-linecap': 'round', 'stroke-linejoin': 'round' };

const icon = (paths) => (props) => (
  <svg {...defaults} {...props}>{paths}</svg>
);

/** Chat / message bubble */
export const IconChat = icon([
  <path d="M21 15a2 2 0 0 1-2 2H7l-4 4V5a2 2 0 0 1 2-2h14a2 2 0 0 1 2 2z" />
]);

/** Bot / agent */
export const IconBot = icon([
  <path d="M12 8V4H8" />,
  <rect x="4" y="8" width="16" height="12" rx="2" />,
  <path d="M2 14h2" />,
  <path d="M20 14h2" />,
  <circle cx="9" cy="13" r="1" fill="currentColor" stroke="none" />,
  <circle cx="15" cy="13" r="1" fill="currentColor" stroke="none" />,
]);

/** Sparkles / skills */
export const IconSkills = icon([
  <path d="M12 3l1.5 4.5L18 9l-4.5 1.5L12 15l-1.5-4.5L6 9l4.5-1.5L12 3z" />,
  <path d="M18 14l1 3 3 1-3 1-1 3-1-3-3-1 3-1 1-3z" />,
]);

/** Brain / knowledge */
export const IconKnowledge = icon([
  <path d="M12 2a7 7 0 0 0-7 7c0 2.4 1.2 4.5 3 5.7V17a2 2 0 0 0 2 2h4a2 2 0 0 0 2-2v-2.3c1.8-1.2 3-3.3 3-5.7a7 7 0 0 0-7-7z" />,
  <path d="M10 21h4" />,
  <path d="M9 13h6" />,
]);

/** Layout grid / dashboard */
export const IconDashboard = icon([
  <rect x="3" y="3" width="7" height="7" rx="1" />,
  <rect x="14" y="3" width="7" height="7" rx="1" />,
  <rect x="3" y="14" width="7" height="7" rx="1" />,
  <rect x="14" y="14" width="7" height="7" rx="1" />,
]);

/** Gear / settings */
export const IconSettings = icon([
  <circle cx="12" cy="12" r="3" />,
  <path d="M19.4 15a1.65 1.65 0 0 0 .33 1.82l.06.06a2 2 0 1 1-2.83 2.83l-.06-.06a1.65 1.65 0 0 0-1.82-.33 1.65 1.65 0 0 0-1 1.51V21a2 2 0 0 1-4 0v-.09a1.65 1.65 0 0 0-1.08-1.51 1.65 1.65 0 0 0-1.82.33l-.06.06a2 2 0 1 1-2.83-2.83l.06-.06a1.65 1.65 0 0 0 .33-1.82 1.65 1.65 0 0 0-1.51-1H3a2 2 0 0 1 0-4h.09a1.65 1.65 0 0 0 1.51-1.08 1.65 1.65 0 0 0-.33-1.82l-.06-.06a2 2 0 1 1 2.83-2.83l.06.06a1.65 1.65 0 0 0 1.82.33H9a1.65 1.65 0 0 0 1-1.51V3a2 2 0 0 1 4 0v.09a1.65 1.65 0 0 0 1.08 1.51 1.65 1.65 0 0 0 1.82-.33l.06-.06a2 2 0 1 1 2.83 2.83l-.06.06a1.65 1.65 0 0 0-.33 1.82V9c.26.6.77 1.02 1.51 1.08H21a2 2 0 0 1 0 4h-.09a1.65 1.65 0 0 0-1.51 1.08z" />,
]);

/** Status dot — small circle indicator */
export function StatusDot({ ok, style }) {
  return (
    <span
      class={`sidebar-status-dot ${ok ? 'ok' : 'err'}`}
      style={style}
      aria-label={ok ? 'System healthy' : 'System issue'}
    />
  );
}
