import { h } from 'preact';
import { useEffect, useRef } from 'preact/hooks';

/**
 * Reusable tab bar component.
 * @param {{ tabs: Array<{id: string, label: string, count?: number}>, active: string, onChange: (id: string) => void }} props
 */
export function TabBar({ tabs, active, onChange }) {
  const barRef = useRef(null);

  const handleKeyDown = (e, idx) => {
    let next = -1;
    if (e.key === 'ArrowRight') next = (idx + 1) % tabs.length;
    else if (e.key === 'ArrowLeft') next = (idx - 1 + tabs.length) % tabs.length;
    else if (e.key === 'Home') next = 0;
    else if (e.key === 'End') next = tabs.length - 1;

    if (next >= 0) {
      e.preventDefault();
      onChange(tabs[next].id);
    }
  };

  // Focus the active tab button when active changes via keyboard.
  useEffect(() => {
    if (!barRef.current) return;
    const activeBtn = barRef.current.querySelector('[aria-selected="true"]');
    if (activeBtn && barRef.current.contains(document.activeElement)) {
      activeBtn.focus();
    }
  }, [active]);

  return (
    <div class="tab-bar" role="tablist" ref={barRef}>
      {tabs.map((tab, i) => (
        <button
          key={tab.id}
          role="tab"
          aria-selected={active === tab.id}
          tabIndex={active === tab.id ? 0 : -1}
          class={active === tab.id ? 'tab-active' : ''}
          onClick={() => onChange(tab.id)}
          onKeyDown={(e) => handleKeyDown(e, i)}
        >
          {tab.label}
          {tab.count != null && (
            <span class="tab-count">{tab.count}</span>
          )}
        </button>
      ))}
    </div>
  );
}
