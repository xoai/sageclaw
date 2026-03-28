import { h } from 'preact';
import { useState, useEffect, useRef } from 'preact/hooks';
import { rpc } from '../api';

export function Memory({ embedded } = {}) {
  const [query, setQuery] = useState('');
  const [memories, setMemories] = useState([]);
  const [loading, setLoading] = useState(false);
  const [expanded, setExpanded] = useState(null);
  const [editing, setEditing] = useState(null);
  const [editForm, setEditForm] = useState({ title: '', content: '', tags: [] });
  const [tagInput, setTagInput] = useState('');
  const timerRef = useRef(null);

  const load = () => {
    rpc('memory.list', { limit: 50 })
      .then(data => setMemories(data || []))
      .catch(() => {});
  };

  useEffect(load, []);

  // Debounced search.
  useEffect(() => {
    if (timerRef.current) clearTimeout(timerRef.current);
    if (!query.trim()) { load(); return; }
    timerRef.current = setTimeout(() => {
      setLoading(true);
      rpc('memory.search', { query, limit: 20 })
        .then(data => setMemories(data || []))
        .catch(() => {})
        .finally(() => setLoading(false));
    }, 300);
  }, [query]);

  const startEdit = (mem) => {
    setEditing(mem.id);
    setEditForm({ title: mem.title || '', content: mem.content || '', tags: mem.tags || [] });
    setTagInput('');
  };

  const saveEdit = async (id) => {
    await fetch(`/api/memory/${id}`, {
      method: 'PUT',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(editForm),
    });
    setEditing(null);
    load();
  };

  const deleteMem = async (id) => {
    if (!confirm('Delete this memory?')) return;
    await fetch(`/api/memory/${id}`, { method: 'DELETE' });
    load();
  };

  const addTag = () => {
    if (tagInput.trim() && !editForm.tags.includes(tagInput.trim())) {
      setEditForm({ ...editForm, tags: [...editForm.tags, tagInput.trim()] });
      setTagInput('');
    }
  };

  const removeTag = (tag) => {
    setEditForm({ ...editForm, tags: editForm.tags.filter(t => t !== tag) });
  };

  return (
    <div>
      {!embedded && <h1>Memory</h1>}

      <input type="text" class="search-input" placeholder="Search memories..."
        value={query} onInput={e => setQuery(e.target.value)} />

      {loading && <div class="empty">Searching...</div>}
      {!loading && memories.length === 0 && (
        <div class="empty" style="padding:48px 24px">
          <div style="font-size:15px;margin-bottom:8px;color:var(--text)">No memories stored</div>
          <div style="font-size:13px;max-width:360px;margin:0 auto;line-height:1.6">
            Memories are facts, preferences, and learnings your agents collect during conversations.
            They build up automatically as you chat — or you can add them manually via the API.
          </div>
        </div>
      )}

      {memories.map(mem => (
        <div key={mem.id} class="memory-card">
          {editing === mem.id ? (
            /* Edit mode */
            <div>
              <div class="form-group">
                <label>Title</label>
                <input type="text" value={editForm.title}
                  onInput={e => setEditForm({ ...editForm, title: e.target.value })} />
              </div>
              <div class="form-group">
                <label>Content</label>
                <textarea rows="6" value={editForm.content}
                  onInput={e => setEditForm({ ...editForm, content: e.target.value })} />
              </div>
              <div class="form-group">
                <label>Tags</label>
                <div style="display:flex;flex-wrap:wrap;gap:0.25rem;margin-bottom:0.5rem">
                  {editForm.tags.map(tag => (
                    <span key={tag} class="tag" onClick={() => removeTag(tag)} style="cursor:pointer">
                      {tag} x
                    </span>
                  ))}
                </div>
                <div style="display:flex;gap:0.5rem">
                  <input type="text" placeholder="Add tag..." value={tagInput}
                    onInput={e => setTagInput(e.target.value)}
                    onKeyDown={e => { if (e.key === 'Enter') { e.preventDefault(); addTag(); } }} />
                  <button class="btn-small" onClick={addTag}>Add</button>
                </div>
              </div>
              <div style="display:flex;gap:0.5rem;margin-top:0.75rem">
                <button class="btn-primary" onClick={() => saveEdit(mem.id)}>Save</button>
                <button class="btn-secondary" onClick={() => setEditing(null)}>Cancel</button>
              </div>
            </div>
          ) : (
            /* View mode */
            <div>
              <div style="display:flex;justify-content:space-between;align-items:center">
                <h3>{mem.title || mem.id?.slice(0, 12)}</h3>
                <div style="display:flex;gap:0.5rem;align-items:center">
                  {mem.score > 0 && <span class="score">score: {mem.score.toFixed(2)}</span>}
                  <button class="btn-small" onClick={() => setExpanded(expanded === mem.id ? null : mem.id)}>
                    {expanded === mem.id ? 'Collapse' : 'Expand'}
                  </button>
                  <button class="btn-small" onClick={() => startEdit(mem)}>Edit</button>
                  <button class="btn-small btn-danger" onClick={() => deleteMem(mem.id)}>Delete</button>
                </div>
              </div>
              <div style="margin:4px 0">
                {(mem.tags || []).map(tag => (
                  <span key={tag} class="tag" onClick={() => setQuery(tag)}>{tag}</span>
                ))}
              </div>
              <p>{expanded === mem.id ? mem.content : (mem.content?.length > 200 ? mem.content.slice(0, 200) + '...' : mem.content)}</p>
              {expanded === mem.id && (
                <div style="font-size:0.8rem;color:var(--text-muted);margin-top:0.5rem">
                  ID: {mem.id}
                </div>
              )}
            </div>
          )}
        </div>
      ))}
    </div>
  );
}
