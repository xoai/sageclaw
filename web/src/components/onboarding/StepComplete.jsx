import { h } from 'preact';
import { useState, useEffect } from 'preact/hooks';

export default function StepComplete({ progress, onDone }) {
  const [pairingCode, setPairingCode] = useState(null);
  const [pairingLoading, setPairingLoading] = useState(false);

  // Auto-generate pairing code if a platform channel was connected.
  useEffect(() => {
    if (progress.connectionId && !progress.skippedChannel) {
      setPairingLoading(true);
      fetch('/api/pairing/generate', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ channel: progress.connectionId }),
      })
        .then(r => r.json())
        .then(data => {
          if (data.code) setPairingCode(data.code);
        })
        .catch(() => {})
        .finally(() => setPairingLoading(false));
    }
  }, []);

  const platformLabel = {
    telegram: 'Telegram', discord: 'Discord', zalo_bot: 'Zalo Bot',
    zalo: 'Zalo OA', whatsapp: 'WhatsApp', web: 'Web Chat',
  };

  return (
    <div class="card" style="padding:32px 24px;text-align:center">
      {/* Success icon */}
      <div style="width:48px;height:48px;border-radius:50%;background:color-mix(in srgb, var(--success) 15%, transparent);display:inline-flex;align-items:center;justify-content:center;margin-bottom:16px">
        <span style="color:var(--success);font-size:24px">{'\u2713'}</span>
      </div>
      <h2 style="font-size:20px;font-weight:700;margin-bottom:4px">Setup Complete</h2>
      <p style="color:var(--text-muted);font-size:13px;margin-bottom:28px">
        Your agent is configured and ready to go.
      </p>

      {/* Setup summary */}
      <div style="text-align:left;margin-bottom:28px">
        <div style="padding:0 4px">
          <div class="health-row" style="padding:10px 0">
            <span style="font-size:12px;color:var(--text-muted);min-width:72px">Provider</span>
            <span style="font-size:13px;font-weight:600">{progress.providerName || progress.providerType || 'Configured'}</span>
          </div>
          <div class="health-row" style="padding:10px 0">
            <span style="font-size:12px;color:var(--text-muted);min-width:72px">Agent</span>
            <span style="font-size:13px;font-weight:600">{progress.agentName || progress.agentId}</span>
          </div>
          <div class="health-row" style="padding:10px 0">
            <span style="font-size:12px;color:var(--text-muted);min-width:72px">Channel</span>
            <span style="font-size:13px;font-weight:600">
              {progress.skippedChannel ? 'Web Chat' : `${platformLabel[progress.connectionPlatform] || progress.connectionPlatform}`}
            </span>
          </div>
        </div>
      </div>

      {/* Pairing code */}
      {pairingCode && (
        <div style="margin-bottom:24px;text-align:left">
          <div class="card" style="padding:16px;border-color:var(--primary)">
            <h3 style="font-size:14px;margin-bottom:8px">Pair Your Device</h3>
            <p style="font-size:12px;color:var(--text-muted);margin-bottom:12px">
              Send this code to your bot on {platformLabel[progress.connectionPlatform] || progress.connectionPlatform} to pair your device.
            </p>
            <div style="font-family:var(--mono);font-size:28px;font-weight:700;letter-spacing:4px;color:var(--primary);text-align:center;padding:16px;background:var(--bg);border-radius:8px">
              {pairingCode}
            </div>
            <p style="font-size:11px;color:var(--text-muted);text-align:center;margin-top:8px">
              Expires in 5 minutes
            </p>
          </div>
        </div>
      )}
      {pairingLoading && (
        <div style="color:var(--text-muted);font-size:12px;margin-bottom:16px">Generating pairing code...</div>
      )}

      {/* Action buttons */}
      <div style="display:flex;flex-direction:column;gap:8px">
        <a href={`/?agent=${encodeURIComponent(progress.agentId || '')}`} class="btn-primary" style="text-decoration:none;text-align:center;display:block;padding:14px;font-size:14px"
          onClick={(e) => { e.preventDefault(); onDone(); }}>
          Start Chatting
        </a>
        <div style="display:flex;gap:8px">
          <a href={`/agents/${progress.agentId}`} class="btn-secondary" style="flex:1;text-decoration:none;text-align:center">
            Customize Agent
          </a>
          <a href="/channels" class="btn-secondary" style="flex:1;text-decoration:none;text-align:center">
            Add More Channels
          </a>
        </div>
        <button class="btn-secondary" style="width:100%;margin-top:4px" onClick={onDone}>
          Go to Dashboard
        </button>
      </div>
    </div>
  );
}
