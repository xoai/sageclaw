import { h } from 'preact';
import { useState, useEffect } from 'preact/hooks';
import { route } from 'preact-router';
import StepProvider from '../components/onboarding/StepProvider';
import StepAgent from '../components/onboarding/StepAgent';
import StepChannel from '../components/onboarding/StepChannel';
import StepComplete from '../components/onboarding/StepComplete';

const STEPS = [
  { key: 'provider', label: 'Connect AI' },
  { key: 'agent', label: 'Create Agent' },
  { key: 'channel', label: 'Connect Channel' },
  { key: 'complete', label: 'Live!' },
];

const STORAGE_KEY = 'sage_onboarding';

function loadProgress() {
  try {
    return JSON.parse(localStorage.getItem(STORAGE_KEY)) || {};
  } catch { return {}; }
}

function saveProgress(data) {
  localStorage.setItem(STORAGE_KEY, JSON.stringify(data));
}

function clearProgress() {
  localStorage.removeItem(STORAGE_KEY);
}

export default function Onboarding() {
  const [step, setStep] = useState(0);
  const [progress, setProgress] = useState({});
  const [checking, setChecking] = useState(true);

  // On mount, detect starting step based on existing setup + saved progress.
  useEffect(() => {
    const saved = loadProgress();
    Promise.all([
      fetch('/api/providers').then(r => r.json()).catch(() => []),
      fetch('/api/v2/agents').then(r => r.json()).catch(() => []),
      fetch('/api/v2/connections').then(r => r.json()).catch(() => []),
    ]).then(([providers, agents, connections]) => {
      const hasProvider = Array.isArray(providers) && providers.some(p => p.status === 'active');

      // Determine starting step — skip provider if already connected,
      // but always show agent step so user can choose existing or create new.
      let startStep = 0;
      const merged = { ...saved };
      if (hasProvider || saved.providerId) {
        startStep = 1; // Skip to agent step, never skip past it
        if (hasProvider && !merged.providerId) merged.providerId = 'existing';
      }

      setProgress(merged);
      setStep(startStep);
      setChecking(false);
    });
  }, []);

  const updateProgress = (data) => {
    const next = { ...progress, ...data };
    setProgress(next);
    saveProgress(next);
  };

  const goNext = () => setStep(s => Math.min(s + 1, STEPS.length - 1));
  const goBack = () => setStep(s => Math.max(s - 1, 0));

  const handleDone = () => {
    clearProgress();
    route('/', true);
  };

  if (checking) {
    return (
      <div style="display:flex;justify-content:center;align-items:center;height:60vh;color:var(--text-muted)">
        Checking setup status...
      </div>
    );
  }

  return (
    <div style="max-width:640px;margin:0 auto;padding-top:24px">
      {/* Header */}
      <div style="text-align:center;margin-bottom:32px">
        <h1 style="font-family:var(--mono);font-size:22px;margin-bottom:4px">Get Started</h1>
        <p style="color:var(--text-muted);font-size:13px">Set up your first agent in a few steps.</p>
      </div>

      {/* Stepper */}
      <div style="display:flex;align-items:center;justify-content:center;gap:0;margin-bottom:32px">
        {STEPS.map((s, i) => (
          <div key={s.key} style="display:flex;align-items:center">
            <div style={`display:flex;align-items:center;gap:6px;${i <= step ? 'opacity:1' : 'opacity:0.4'}`}>
              <span style={`
                display:inline-flex;align-items:center;justify-content:center;
                width:28px;height:28px;border-radius:50%;font-size:12px;font-weight:700;
                ${i < step ? 'background:var(--success);color:#fff' :
                  i === step ? 'background:var(--primary);color:#fff' :
                  'background:var(--surface);color:var(--text-muted);border:1px solid var(--border)'}
              `}>
                {i < step ? '\u2713' : i + 1}
              </span>
              <span style={`font-size:12px;font-weight:500;${i === step ? 'color:var(--text)' : 'color:var(--text-muted)'}`}>
                {s.label}
              </span>
            </div>
            {i < STEPS.length - 1 && (
              <div style={`width:32px;height:1px;margin:0 8px;${i < step ? 'background:var(--success)' : 'background:var(--border)'}`} />
            )}
          </div>
        ))}
      </div>

      {/* Step content */}
      {step === 0 && (
        <StepProvider
          progress={progress}
          onComplete={(data) => { updateProgress(data); goNext(); }}
        />
      )}
      {step === 1 && (
        <StepAgent
          progress={progress}
          onComplete={(data) => { updateProgress(data); goNext(); }}
          onBack={goBack}
        />
      )}
      {step === 2 && (
        <StepChannel
          progress={progress}
          onComplete={(data) => { updateProgress(data); goNext(); }}
          onBack={goBack}
        />
      )}
      {step === 3 && (
        <StepComplete
          progress={progress}
          onDone={handleDone}
        />
      )}
    </div>
  );
}
