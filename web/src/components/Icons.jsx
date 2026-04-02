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

/** Paperclip / attach */
export const IconPaperclip = icon([
  <path d="M21.44 11.05l-9.19 9.19a6 6 0 0 1-8.49-8.49l9.19-9.19a4 4 0 0 1 5.66 5.66l-9.2 9.19a2 2 0 0 1-2.83-2.83l8.49-8.48" />,
]);

/** Arrow up / send */
export const IconArrowUp = icon([
  <line x1="12" y1="19" x2="12" y2="5" />,
  <polyline points="5 12 12 5 19 12" />,
]);

/** Store / marketplace */
export const IconStore = icon([
  <path d="M3 9l1-4h16l1 4" />,
  <path d="M3 9v10a1 1 0 0 0 1 1h16a1 1 0 0 0 1-1V9" />,
  <path d="M3 9h18" />,
  <path d="M9 21V12h6v9" />,
]);

/** Sparkle / magic */
export const IconSparkle = icon([
  <path d="M12 3l1.5 4.5L18 9l-4.5 1.5L12 15l-1.5-4.5L6 9l4.5-1.5L12 3z" />,
]);

/** Chevron left / back */
export const IconChevronLeft = icon([
  <polyline points="15 18 9 12 15 6" />,
]);

/** X / close */
export const IconX = icon([
  <line x1="18" y1="6" x2="6" y2="18" />,
  <line x1="6" y1="6" x2="18" y2="18" />,
]);

/** Loader / hourglass */
export const IconLoader = icon([
  <line x1="12" y1="2" x2="12" y2="6" />,
  <line x1="12" y1="18" x2="12" y2="22" />,
  <line x1="4.93" y1="4.93" x2="7.76" y2="7.76" />,
  <line x1="16.24" y1="16.24" x2="19.07" y2="19.07" />,
  <line x1="2" y1="12" x2="6" y2="12" />,
  <line x1="18" y1="12" x2="22" y2="12" />,
  <line x1="4.93" y1="19.07" x2="7.76" y2="16.24" />,
  <line x1="16.24" y1="7.76" x2="19.07" y2="4.93" />,
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
