import { h } from 'preact';
import { useState } from 'preact/hooks';
import { Nav } from './Nav';

export function Layout({ children }) {
  const [sidebarOpen, setSidebarOpen] = useState(false);

  const toggle = () => setSidebarOpen(!sidebarOpen);
  const close = () => setSidebarOpen(false);

  return (
    <div class="layout">
      <button class="hamburger" onClick={toggle} aria-label="Menu">
        {sidebarOpen ? '\u2715' : '\u2630'}
      </button>
      <div class={`sidebar-overlay ${sidebarOpen ? 'open' : ''}`} onClick={close} />
      <Nav open={sidebarOpen} onNavigate={close} />
      <main class="content">
        {children}
      </main>
    </div>
  );
}
