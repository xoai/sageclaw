import { h } from 'preact';

export function InfoTip({ text }) {
  if (!text) return null;
  return (
    <span class="info-tip">
      <span class="info-tip-icon">?</span>
      <span class="info-tip-text">{text}</span>
    </span>
  );
}

export function Label({ text, tip, htmlFor }) {
  return (
    <label htmlFor={htmlFor}>
      {text}
      {tip && <InfoTip text={tip} />}
    </label>
  );
}
