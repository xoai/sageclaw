import { h } from 'preact';

/**
 * Breadcrumb navigation. Items: [{label, href?}].
 * Last item renders as current page (no link).
 */
export function Breadcrumb({ items }) {
  if (!items || items.length === 0) return null;

  const navigate = (e, href) => {
    e.preventDefault();
    history.pushState(null, '', href);
    window.dispatchEvent(new PopStateEvent('popstate'));
  };

  return (
    <nav class="breadcrumb" aria-label="Breadcrumb">
      <a href="/" onClick={e => navigate(e, '/')}>Home</a>
      {items.map((item, i) => (
        <span key={i}>
          <span class="breadcrumb-sep">/</span>
          {item.href && i < items.length - 1
            ? <a href={item.href} onClick={e => navigate(e, item.href)}>{item.label}</a>
            : <span class="breadcrumb-current">{item.label}</span>
          }
        </span>
      ))}
    </nav>
  );
}
