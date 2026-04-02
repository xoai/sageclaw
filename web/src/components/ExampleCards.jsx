import { h } from 'preact';

/**
 * ExampleCards displays clickable example prompt cards.
 * Clicking a card fills the chat input with the prompt text.
 *
 * @param {Object} props
 * @param {string[]} props.examples - Array of example prompt strings.
 * @param {(text: string) => void} props.onSelect - Called when user clicks an example.
 */
export function ExampleCards({ examples, onSelect }) {
  if (!examples || examples.length === 0) return null;

  return (
    <div class="example-cards-container">
      <div class="example-cards-header">
        <span style="font-size:var(--text-sm);color:var(--text-muted);font-weight:500">Try asking</span>
      </div>
      <div class="example-cards-grid">
        {examples.map((text, i) => (
          <button
            key={i}
            class="example-card"
            onClick={() => onSelect(text)}
            title={text}
          >
            <span class="example-card-text">{text}</span>
          </button>
        ))}
      </div>
    </div>
  );
}
