import { h } from 'preact';
import { useState, useRef } from 'preact/hooks';

export default function ConfigPanel({ schema, values, onChange, onClose, inline }) {
  const [showAdvanced, setShowAdvanced] = useState(false);

  const setValue = (key, val) => {
    onChange({ [key]: val });
  };

  const containerStyle = inline ? {} : {
    background: 'var(--surface)',
    border: '1px solid var(--border)',
    borderRadius: '8px',
    padding: '20px',
    maxHeight: '600px',
    overflowY: 'auto',
  };

  return (
    <div style={containerStyle}>
      {!inline && (
        <div style="display:flex;justify-content:space-between;align-items:center;margin-bottom:16px">
          <div>
            <h2 style="font-size:16px;margin-bottom:2px">{schema.title}</h2>
            <p style="color:var(--text-muted);font-size:12px">{schema.subtitle}</p>
          </div>
          <button class="btn-small" onClick={onClose} aria-label="Close panel">{'\u2715'}</button>
        </div>
      )}

      {schema.sections?.map((section, si) => (
        <div key={si} style="margin-bottom:20px">
          <h3 style="font-size:13px;font-weight:600;margin-bottom:4px;color:var(--text)">{section.title}</h3>
          {section.description && (
            <p style="font-size:12px;color:var(--text-muted);margin-bottom:12px">{section.description}</p>
          )}

          {section.fields?.map(field => (
            <SchemaField
              key={field.key}
              field={field}
              value={values[field.key]}
              onChange={(val) => setValue(field.key, val)}
            />
          ))}
        </div>
      ))}

      {/* Advanced: raw JSON editor */}
      <div style="border-top:1px solid var(--border);padding-top:12px;margin-top:8px">
        <button class="btn-small" onClick={() => setShowAdvanced(!showAdvanced)}
          style="font-size:11px;color:var(--text-muted)">
          {showAdvanced ? 'Hide' : 'Show'} Advanced Editor
        </button>
        {showAdvanced && (
          <textarea
            style="width:100%;margin-top:8px;font-family:var(--mono);font-size:11px;min-height:150px;background:var(--bg);color:var(--text);border:1px solid var(--border);border-radius:4px;padding:8px"
            value={JSON.stringify(values, null, 2)}
            onInput={(e) => {
              try {
                const parsed = JSON.parse(e.target.value);
                onChange(parsed);
              } catch {}
            }}
          />
        )}
      </div>
    </div>
  );
}

let _keyCounter = 0;

function SchemaField({ field, value, onChange }) {
  const val = value !== undefined ? value : field.default;
  const fieldId = `field-${field.key}`;
  // Stable keys for repeating list items (avoids index-as-key issues on delete)
  const keysRef = useRef([]);
  const ensureKeys = (count) => {
    while (keysRef.current.length < count) keysRef.current.push(++_keyCounter);
    if (keysRef.current.length > count) keysRef.current.length = count;
    return keysRef.current;
  };

  switch (field.type) {
    case 'text':
      return (
        <div class="form-group">
          <label htmlFor={fieldId}>{field.label} {field.required && <span style="color:var(--error)">*</span>}</label>
          <input id={fieldId} type="text" value={val || ''} placeholder={field.placeholder || ''}
            onInput={(e) => onChange(e.target.value)} />
          {field.help && <div style="font-size:11px;color:var(--text-muted);margin-top:2px">{field.help}</div>}
        </div>
      );

    case 'textarea':
      return (
        <div class="form-group">
          <label htmlFor={fieldId}>{field.label}</label>
          <textarea id={fieldId} rows={field.rows || 4} value={val || ''} placeholder={field.placeholder || ''}
            onInput={(e) => onChange(e.target.value)} />
          {field.help && <div style="font-size:11px;color:var(--text-muted);margin-top:2px">{field.help}</div>}
        </div>
      );

    case 'dropdown':
      return (
        <div class="form-group">
          <label htmlFor={fieldId}>{field.label}</label>
          <select id={fieldId} value={val || field.default || ''} onChange={(e) => onChange(e.target.value)}>
            {(field.options || []).map(opt => (
              <option key={opt} value={opt}>{opt}</option>
            ))}
          </select>
          {field.help && <div style="font-size:11px;color:var(--text-muted);margin-top:2px">{field.help}</div>}
        </div>
      );

    case 'toggle':
      return (
        <div class="form-group" style="display:flex;align-items:flex-start;gap:8px;margin-bottom:10px">
          <input id={fieldId} type="checkbox" checked={val !== undefined ? val : field.default}
            onChange={(e) => onChange(e.target.checked)}
            style="margin-top:2px;flex-shrink:0" />
          <div style="flex:1">
            <label htmlFor={fieldId} style="margin-bottom:0;font-size:13px;color:var(--text);display:inline">{field.label}</label>
            {field.help && (
              <span class="info-tip">
                <span class="info-tip-icon">?</span>
                <span class="info-tip-text">{field.help}</span>
              </span>
            )}
          </div>
        </div>
      );

    case 'checklist':
      const selected = Array.isArray(val) ? val : (field.default || []);
      return (
        <div class="form-group">
          <label id={`${fieldId}-label`}>{field.label}</label>
          <div style="display:flex;flex-wrap:wrap;gap:6px;margin-top:4px">
            {(field.options || []).map(opt => {
              const isSelected = selected.includes(opt);
              return (
                <button key={opt}
                  type="button"
                  style={`padding:4px 10px;border-radius:12px;font-size:12px;border:1px solid ${isSelected ? 'var(--primary)' : 'var(--border)'};background:${isSelected ? 'rgba(88,166,255,0.15)' : 'var(--surface)'};color:${isSelected ? 'var(--primary)' : 'var(--text-muted)'};cursor:pointer`}
                  onClick={() => {
                    if (isSelected) onChange(selected.filter(s => s !== opt));
                    else onChange([...selected, opt]);
                  }}>
                  {opt}
                </button>
              );
            })}
          </div>
          {field.help && <div style="font-size:11px;color:var(--text-muted);margin-top:4px">{field.help}</div>}
        </div>
      );

    case 'tool-selector':
      const enabledTools = Array.isArray(val) ? val : (field.default || []);
      return (
        <div class="form-group">
          <label id={`${fieldId}-label`}>{field.label}</label>
          {field.categories && Object.entries(field.categories).map(([cat, tools]) => {
            const allSelected = tools.every(t => enabledTools.includes(t));
            const someSelected = tools.some(t => enabledTools.includes(t));
            return (
              <div key={cat} style="margin-top:8px">
                <div style="display:flex;align-items:center;gap:6px;margin-bottom:4px">
                  <input type="checkbox" checked={allSelected}
                    style="opacity:0.7"
                    onChange={() => {
                      if (allSelected) onChange(enabledTools.filter(t => !tools.includes(t)));
                      else onChange([...new Set([...enabledTools, ...tools])]);
                    }} />
                  <span style="font-size:12px;font-weight:600;color:var(--text)">{cat}</span>
                  <span style="font-size:11px;color:var(--text-muted)">({tools.filter(t => enabledTools.includes(t)).length}/{tools.length})</span>
                </div>
                <div style="display:flex;flex-wrap:wrap;gap:4px;margin-left:22px">
                  {tools.map(tool => {
                    const isOn = enabledTools.includes(tool);
                    return (
                      <button key={tool} type="button"
                        style={`padding:2px 8px;border-radius:4px;font-size:11px;font-family:var(--mono);border:1px solid ${isOn ? 'var(--primary)' : 'var(--border)'};background:${isOn ? 'rgba(88,166,255,0.1)' : 'transparent'};color:${isOn ? 'var(--primary)' : 'var(--text-muted)'};cursor:pointer`}
                        onClick={() => {
                          if (isOn) onChange(enabledTools.filter(t => t !== tool));
                          else onChange([...enabledTools, tool]);
                        }}>
                        {tool}
                      </button>
                    );
                  })}
                </div>
              </div>
            );
          })}
          {field.help && <div style="font-size:11px;color:var(--text-muted);margin-top:6px">{field.help}</div>}
        </div>
      );

    case 'repeating':
    case 'repeating-text':
      const items = Array.isArray(val) ? val : (field.default || []);
      const itemKeys = ensureKeys(items.length);
      return (
        <div class="form-group">
          <label id={`${fieldId}-label`}>{field.label}</label>
          {items.map((item, i) => (
            <div key={itemKeys[i]} style="display:flex;gap:6px;margin-bottom:6px;align-items:center">
              {field.type === 'repeating-text' ? (
                <input type="text" value={item || ''} placeholder={field.placeholder}
                  style="flex:1"
                  onInput={(e) => {
                    const newItems = [...items];
                    newItems[i] = e.target.value;
                    onChange(newItems);
                  }} />
              ) : (
                <div style="flex:1;display:flex;gap:4px;flex-wrap:wrap">
                  {(field.item_fields || []).map(subField => (
                    <input key={subField.key} type="text"
                      value={item?.[subField.key] || ''}
                      placeholder={subField.placeholder || subField.label}
                      style="flex:1;min-width:120px"
                      onInput={(e) => {
                        const newItems = [...items];
                        newItems[i] = { ...newItems[i], [subField.key]: e.target.value };
                        onChange(newItems);
                      }} />
                  ))}
                </div>
              )}
              <button class="btn-small btn-danger" type="button" aria-label={`Remove item ${i + 1}`}
                onClick={() => {
                  keysRef.current.splice(i, 1);
                  onChange(items.filter((_, j) => j !== i));
                }}>
                {'\u2715'}
              </button>
            </div>
          ))}
          <button class="btn-small" type="button"
            onClick={() => onChange([...items, field.type === 'repeating-text' ? '' : {}])}>
            + Add
          </button>
          {field.help && <div style="font-size:11px;color:var(--text-muted);margin-top:4px">{field.help}</div>}
        </div>
      );

    default:
      return (
        <div class="form-group">
          <label htmlFor={fieldId}>{field.label}</label>
          <input id={fieldId} type="text" value={val || ''} onInput={(e) => onChange(e.target.value)} />
        </div>
      );
  }
}
