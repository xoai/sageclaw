/**
 * Chat engine — pure state machine for managing chat send/receive lifecycle.
 *
 * States: IDLE, SENDING, STREAMING, CONSENT, COMPLETING, ERROR
 *
 * No DOM dependencies. Testable with mock RPC + mock SSE events.
 */

const TIMEOUTS = {
  SENDING: 15000,
  STREAMING: 30000,
  CONSENT: 120000,
  COMPLETING: 10000,
  ERROR: 5000,
};

// Events that indicate the agent is actively working — trigger SENDING→STREAMING.
const LIFECYCLE_EVENTS = new Set([
  'chunk', 'tool.call', 'tool.result', 'tool.call.started', 'run.started',
]);

export function createChatEngine({
  onStateChange,
  onStreamChunk,
  onToolStep,
  onMessages,
  onConsent,
  onError,
  rpc,
  loadMessages,
}) {
  let state = 'IDLE';
  let activeSessionId = null;
  let timer = null;
  let sseDisconnected = false;
  let runCount = 0;

  function timeoutMs(base) {
    return sseDisconnected ? base * 2 : base;
  }

  function transition(newState) {
    clearTimer();
    state = newState;
    onStateChange?.(state);

    // Start timeout for the new state.
    const ms = TIMEOUTS[newState];
    if (ms) {
      timer = setTimeout(() => onTimeout(newState), timeoutMs(ms));
    }
  }

  function clearTimer() {
    if (timer) {
      clearTimeout(timer);
      timer = null;
    }
  }

  function resetTimeout() {
    if (state in TIMEOUTS) {
      clearTimer();
      timer = setTimeout(() => onTimeout(state), timeoutMs(TIMEOUTS[state]));
    }
  }

  function onTimeout(timedOutState) {
    if (state !== timedOutState) return;

    switch (timedOutState) {
      case 'SENDING':
      case 'STREAMING':
      case 'CONSENT':
        transition('ERROR');
        onError?.(`${timedOutState} timeout`);
        break;
      case 'COMPLETING':
        // No synthesis run — transition to IDLE (normal).
        doReload();
        break;
      case 'ERROR':
        // Auto-reset to IDLE.
        doReload();
        break;
    }
  }

  function doReload() {
    runCount = 0;
    transition('IDLE');
    // Reload from DB asynchronously — state is already IDLE.
    if (activeSessionId && loadMessages) {
      loadMessages(activeSessionId).then(
        (result) => onMessages?.(result.messages, result.dbToolSteps),
        (err) => onError?.(`DB reload failed: ${err.message}`),
      );
    }
  }

  function processEvent(event) {
    // Sync event: ring buffer expired — reload everything from DB.
    if (event.type === 'sync') {
      doReload();
      return;
    }

    // Session filtering.
    if (event.session_id && activeSessionId && event.session_id !== activeSessionId) {
      return;
    }

    // Dispatch callbacks regardless of state transition.
    if (event.type === 'chunk' && event.text) {
      onStreamChunk?.(event.text);
    }
    if (event.type === 'tool.call') {
      const tc = event.tool_call || {};
      let input = null;
      try {
        if (tc.input) input = typeof tc.input === 'string' ? JSON.parse(tc.input) : tc.input;
      } catch {}
      onToolStep?.({
        id: tc.id || 'tc_' + Date.now(),
        name: tc.name || 'unknown',
        input,
        status: 'running',
        startedAt: Date.now(),
        type: 'tool.call',
      });
    }
    if (event.type === 'tool.result') {
      const tr = event.tool_result || {};
      onToolStep?.({
        id: tr.tool_call_id,
        status: 'done',
        type: 'tool.result',
      });
    }
    if (event.type === 'consent.needed' && event.consent) {
      onConsent?.(event);
    }

    // State transitions.
    switch (state) {
      case 'SENDING':
        if (LIFECYCLE_EVENTS.has(event.type)) {
          transition('STREAMING');
        } else if (event.type === 'run.completed') {
          runCount++;
          transition('COMPLETING');
        }
        break;

      case 'STREAMING':
        if (event.type === 'run.completed') {
          runCount++;
          if (runCount >= 2) {
            // Second (or later) run done — finalize.
            doReload();
          } else {
            transition('COMPLETING');
          }
        } else if (event.type === 'consent.needed') {
          transition('CONSENT');
        } else {
          // Any event resets the streaming timeout.
          resetTimeout();
        }
        break;

      case 'CONSENT':
        if (event.type === 'consent.result') {
          transition('STREAMING');
        }
        break;

      case 'COMPLETING':
        if (LIFECYCLE_EVENTS.has(event.type)) {
          // Synthesis run started.
          transition('STREAMING');
        } else if (event.type === 'run.completed') {
          // Second run done.
          runCount++;
          doReload();
        }
        break;

      // IDLE and ERROR: events are noted (callbacks fired) but no state transition.
    }
  }

  async function send(text, agentId, chatId) {
    if (state !== 'IDLE') return;

    transition('SENDING');
    runCount = 0;

    try {
      const result = await rpc('chat.send', {
        text,
        agent_id: agentId || undefined,
        chat_id: chatId,
      });

      if (result.error) {
        transition('ERROR');
        onError?.(result.error);
        return;
      }

      // Store session_id from response.
      if (result.data?.session_id) {
        activeSessionId = result.data.session_id;
      }
    } catch (err) {
      transition('ERROR');
      onError?.(err.message);
    }
  }

  function setSession(sessionId) {
    activeSessionId = sessionId;
  }

  function setSseStatus(status) {
    sseDisconnected = status === 'reconnecting';
    // Re-arm current timeout with adjusted duration.
    if (state in TIMEOUTS) {
      resetTimeout();
    }
  }

  function getState() {
    return state;
  }

  function getSessionId() {
    return activeSessionId;
  }

  function destroy() {
    clearTimer();
    if (state !== 'IDLE') {
      state = 'IDLE';
      onStateChange?.('IDLE');
    }
    activeSessionId = null;
    runCount = 0;
  }

  return {
    send,
    setSession,
    setSseStatus,
    processEvent,
    getState,
    getSessionId,
    destroy,
  };
}
