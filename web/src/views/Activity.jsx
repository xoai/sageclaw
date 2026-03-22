import { useState, useEffect, useRef } from 'preact/hooks';
import { subscribeEvents } from '../api';
import { EventCard } from '../components/EventCard';

export function Activity() {
  const [events, setEvents] = useState([]);
  const [paused, setPaused] = useState(false);
  const bottomRef = useRef(null);

  useEffect(() => {
    const unsub = subscribeEvents((event) => {
      setEvents((prev) => [...prev.slice(-199), event]);
    });
    return unsub;
  }, []);

  useEffect(() => {
    if (!paused && bottomRef.current) {
      bottomRef.current.scrollIntoView({ behavior: 'smooth' });
    }
  }, [events, paused]);

  return (
    <div>
      <div style="display: flex; justify-content: space-between; align-items: center; margin-bottom: 16px">
        <h1>Activity</h1>
        <button
          onClick={() => setPaused(!paused)}
          style={`padding: 6px 12px; background: var(--surface); border: 1px solid var(--border);
                  border-radius: 4px; color: var(--text); cursor: pointer; font-size: 12px`}
        >
          {paused ? '▶ Resume' : '⏸ Pause'}
        </button>
      </div>

      {events.length === 0 ? (
        <div class="empty">Waiting for agent activity...</div>
      ) : (
        <div>
          {events.map((event, i) => (
            <EventCard key={i} event={event} />
          ))}
          <div ref={bottomRef} />
        </div>
      )}
    </div>
  );
}
