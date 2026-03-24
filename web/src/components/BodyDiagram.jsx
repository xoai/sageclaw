import { h } from 'preact';

// Each node: position (x%, y%) = circle center, anchor (ax%, ay%) = body connection point.
const nodes = [
  { id: 'identity',  label: 'Identity',  subtitle: 'Who',       x: 82,  y: 3,   ax: 50, ay: 5   },
  { id: 'memory',    label: 'Memory',    subtitle: 'Recalls',   x: 14,  y: 5,   ax: 47, ay: 8   },
  { id: 'behavior',  label: 'Behavior',  subtitle: 'Rules',     x: 86,  y: 42,  ax: 50, ay: 45  },
  { id: 'heartbeat', label: 'Heartbeat', subtitle: 'Schedule',  x: 86,  y: 25,  ax: 55, ay: 28  },
  { id: 'soul',      label: 'Soul',      subtitle: 'Voice',     x: 10,  y: 35,  ax: 47, ay: 30  },
  { id: 'skills',    label: 'Skills',    subtitle: 'Knows',     x: 4,   y: 54,  ax: 28, ay: 48  },
  { id: 'tools',     label: 'Tools',     subtitle: 'Does',      x: 96,  y: 54,  ax: 72, ay: 48  },
  { id: 'bootstrap', label: 'Bootstrap', subtitle: 'First run', x: 14,  y: 91,  ax: 42, ay: 72  },
  { id: 'channels',  label: 'Channels',  subtitle: 'Talks',     x: 10,  y: 20,  ax: 50, ay: 14  },
];

const stateColors = {
  complete: 'var(--success)',
  partial: 'var(--warning)',
  empty: 'var(--border)',
};

export default function BodyDiagram({ activeNode, onNodeClick, getState }) {
  const sizePct = 14;

  return (
    <div style="position:relative;width:100%;max-width:520px;aspect-ratio:3/4;overflow:visible">
      {/* Body image — centered */}
      <img src="/body-small.png" alt="Agent body diagram"
        style="position:absolute;top:0;left:0;width:100%;height:100%;object-fit:contain;pointer-events:none;opacity:0.55" />

      {/* SVG — lines from circles to body anchor points */}
      <svg viewBox="0 0 300 400" preserveAspectRatio="none"
        style="position:absolute;top:0;left:0;width:100%;height:100%;pointer-events:none;overflow:visible">
        {nodes.map((node, i) => (
          <line key={i}
            x1={node.x * 3} y1={node.y * 4}
            x2={node.ax * 3} y2={node.ay * 4}
            stroke="rgba(88,166,255,0.25)" strokeWidth="1" strokeDasharray="4,3"
          />
        ))}
      </svg>

      {/* HTML circle nodes — flexbox-centered text */}
      <div style="position:absolute;top:0;left:0;width:100%;height:100%;overflow:visible">
        {nodes.map(node => {
          const state = getState(node.id);
          const isActive = activeNode === node.id;
          const color = stateColors[state];

          const glow = isActive ? '0 0 24px rgba(88,166,255,0.7)'
            : state === 'complete' ? '0 0 14px rgba(63,185,80,0.5)'
            : state === 'partial' ? '0 0 12px rgba(210,153,34,0.4)'
            : 'none';

          const hasRing = isActive || state !== 'empty';
          const ringColor = isActive ? 'rgba(88,166,255,0.8)'
            : state === 'complete' ? 'rgba(63,185,80,0.35)'
            : state === 'partial' ? 'rgba(210,153,34,0.35)'
            : 'transparent';

          return (
            <div key={node.id} role="button" tabIndex={0} aria-label={`${node.label}: ${node.subtitle}`}
              onClick={() => onNodeClick(node.id)}
              onKeyDown={(e) => { if (e.key === 'Enter' || e.key === ' ') { e.preventDefault(); onNodeClick(node.id); } }}
              style={[
                'position:absolute',
                `left:${node.x}%;top:${node.y}%`,
                'transform:translate(-50%,-50%)',
                `width:${sizePct}%`,
                'aspect-ratio:1',
                'border-radius:50%',
                `background:${isActive ? 'var(--primary)' : 'rgba(13,17,23,0.85)'}`,
                `border:${isActive ? 3 : 2}px solid ${isActive ? 'var(--primary)' : color}`,
                `box-shadow:${glow}`,
                hasRing ? `outline:2px ${isActive ? 'solid' : 'dashed'} ${ringColor};outline-offset:6px` : '',
                'display:flex;flex-direction:column;align-items:center;justify-content:center',
                'cursor:pointer;user-select:none',
              ].filter(Boolean).join(';')}>
              <span style={`color:${isActive ? 'var(--text-on-primary)' : 'var(--text)'};font-size:13px;font-weight:600;font-family:var(--sans);line-height:1.2;text-align:center`}>
                {node.label}
              </span>
              <span style={`color:var(--text-muted);font-size:10px;font-family:var(--sans);line-height:1.2;text-align:center${isActive ? ';opacity:0.7' : ''}`}>
                {node.subtitle}
              </span>

            </div>
          );
        })}
      </div>
    </div>
  );
}
