const RPC_URL = '/rpc';
const EVENTS_URL = '/events';

let rpcId = 0;

export async function rpc(method, params = {}) {
  const res = await fetch(RPC_URL, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    credentials: 'include',
    body: JSON.stringify({ jsonrpc: '2.0', id: ++rpcId, method, params }),
  });
  if (res.status === 401) {
    window.location.reload(); // Token expired — redirect to login.
    throw new Error('Session expired');
  }
  const data = await res.json();
  if (data.error) throw new Error(data.error);
  return data.result;
}

export function subscribeEvents(onEvent) {
  const source = new EventSource(EVENTS_URL, { withCredentials: true });
  source.onmessage = (e) => {
    try {
      onEvent(JSON.parse(e.data));
    } catch (_) {}
  };
  source.onerror = () => {
    // Reconnect handled by EventSource automatically.
  };
  return () => source.close();
}
