import { h } from 'preact';
import { useState, useEffect } from 'preact/hooks';
import { rpc } from '../api';

export default function Graph() {
  const [memories, setMemories] = useState([]);
  const [selectedNode, setSelectedNode] = useState(null);
  const [graphData, setGraphData] = useState({ nodes: [], edges: [] });
  const [linkForm, setLinkForm] = useState({ source_id: '', target_id: '', relation: '' });
  const [showLinkModal, setShowLinkModal] = useState(false);
  const [search, setSearch] = useState('');

  useEffect(() => {
    rpc('memory.list', { limit: 100 }).then(setMemories).catch(() => {});
  }, []);

  const loadGraph = async (id) => {
    setSelectedNode(id);
    try {
      const data = await fetch(`/api/graph/${id}?depth=2`).then(r => r.json());
      setGraphData(data);
    } catch {
      setGraphData({ nodes: [], edges: [] });
    }
  };

  const createLink = async () => {
    await fetch('/api/graph/link', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(linkForm),
    });
    setShowLinkModal(false);
    if (selectedNode) loadGraph(selectedNode);
  };

  const removeLink = async (sourceId, targetId, relation) => {
    await fetch('/api/graph/link', {
      method: 'DELETE',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ source_id: sourceId, target_id: targetId, relation }),
    });
    if (selectedNode) loadGraph(selectedNode);
  };

  const filtered = search
    ? memories.filter(m => m.title?.toLowerCase().includes(search.toLowerCase()) || m.content?.toLowerCase().includes(search.toLowerCase()))
    : memories;

  return (
    <div>
      <div style="display:flex;justify-content:space-between;align-items:center;margin-bottom:1.5rem">
        <h1>Knowledge Graph</h1>
        <button class="btn-primary" onClick={() => setShowLinkModal(true)}>+ Link Memories</button>
      </div>

      <div class="graph-layout" style="display:grid;grid-template-columns:280px 1fr;gap:1.5rem">
        {/* Node selector */}
        <div>
          <input type="text" placeholder="Search memories..." value={search}
            onInput={e => setSearch(e.target.value)} style="width:100%;margin-bottom:1rem" />
          <div style="max-height:70vh;overflow-y:auto">
            {filtered.slice(0, 50).map(m => (
              <div key={m.id}
                class={`card clickable ${selectedNode === m.id ? 'card-selected' : ''}`}
                onClick={() => loadGraph(m.id)}
                style="margin-bottom:0.5rem;padding:0.75rem;cursor:pointer">
                <div style="font-weight:600;font-size:0.9rem">{m.title || 'Untitled'}</div>
                <div style="font-size:0.8rem;color:var(--text-muted);margin-top:0.25rem">
                  {m.content?.slice(0, 60)}...
                </div>
              </div>
            ))}
          </div>
        </div>

        {/* Graph view */}
        <div>
          {!selectedNode ? (
            <div style="text-align:center;color:var(--text-muted);margin-top:3rem">
              Select a memory to explore its connections.
            </div>
          ) : (
            <div>
              <h3 style="margin-bottom:1rem">Connections</h3>

              {graphData.edges?.length === 0 ? (
                <p style="color:var(--text-muted)">No connections found. Use "Link Memories" to create relationships.</p>
              ) : (
                <div class="card-list">
                  {graphData.edges?.map((edge, i) => (
                    <div class="card" key={i} style="padding:0.75rem">
                      <div style="display:flex;justify-content:space-between;align-items:center">
                        <div>
                          <span style="color:var(--text-muted)">{edge.source_id?.slice(0, 8)}</span>
                          <span style="margin:0 0.5rem;color:var(--primary)">--{edge.relation}--&gt;</span>
                          <span style="color:var(--text-muted)">{edge.target_id?.slice(0, 8)}</span>
                        </div>
                        <button class="btn-small btn-danger"
                          onClick={() => removeLink(edge.source_id, edge.target_id, edge.relation)}>
                          Unlink
                        </button>
                      </div>
                    </div>
                  ))}
                </div>
              )}

              {graphData.nodes?.length > 0 && (
                <div style="margin-top:1.5rem">
                  <h3>Connected Memories ({graphData.nodes.length})</h3>
                  {graphData.nodes.map(n => (
                    <div key={n.id} class="card clickable" onClick={() => loadGraph(n.id)}
                      style="margin-top:0.5rem;padding:0.75rem;cursor:pointer">
                      <strong>{n.title || 'Untitled'}</strong>
                      <div style="font-size:0.85rem;color:var(--text-muted);margin-top:0.25rem">
                        {n.content?.slice(0, 100)}
                      </div>
                    </div>
                  ))}
                </div>
              )}
            </div>
          )}
        </div>
      </div>

      {showLinkModal && (
        <div class="modal-overlay" onClick={() => setShowLinkModal(false)} role="dialog" aria-modal="true" aria-labelledby="link-modal-title">
          <div class="modal-content" onClick={e => e.stopPropagation()}>
            <h2 id="link-modal-title">Link Memories</h2>
            <div class="form-group">
              <label>Source Memory ID</label>
              <input type="text" value={linkForm.source_id}
                onInput={e => setLinkForm({ ...linkForm, source_id: e.target.value })}
                placeholder={selectedNode || 'Memory ID'} />
            </div>
            <div class="form-group">
              <label>Target Memory ID</label>
              <input type="text" value={linkForm.target_id}
                onInput={e => setLinkForm({ ...linkForm, target_id: e.target.value })} />
            </div>
            <div class="form-group">
              <label>Relation</label>
              <input type="text" value={linkForm.relation}
                onInput={e => setLinkForm({ ...linkForm, relation: e.target.value })}
                placeholder="e.g. related_to, depends_on, part_of" />
            </div>
            <div style="display:flex;gap:0.5rem;margin-top:1rem">
              <button class="btn-primary" onClick={createLink}
                disabled={!linkForm.source_id || !linkForm.target_id || !linkForm.relation}>Create Link</button>
              <button class="btn-secondary" onClick={() => setShowLinkModal(false)}>Cancel</button>
            </div>
          </div>
        </div>
      )}
    </div>
  );
}
