/**
 * Persistent SSE connection wrapper.
 * Manages a single EventSource with auto-reconnect and status callbacks.
 */
export function createSSEConnection(url, { onEvent, onStatusChange }) {
  let connected = false;
  const es = new EventSource(url, { withCredentials: true });

  es.onopen = () => {
    connected = true;
    onStatusChange?.('connected');
  };

  es.onmessage = (e) => {
    try {
      onEvent(JSON.parse(e.data));
    } catch (err) {
      console.error('SSE parse error:', err);
    }
  };

  es.onerror = () => {
    connected = false;
    onStatusChange?.('reconnecting');
    // EventSource auto-reconnects. Browser handles retry + Last-Event-ID.
  };

  return {
    close: () => { es.close(); connected = false; },
    isConnected: () => connected,
  };
}
