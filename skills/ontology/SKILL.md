---
name: ontology
description: Model entity relationships using typed directed edges
version: "1.0.0"
tools: [memory_link, memory_graph]
---

# Ontology Skill

Build a knowledge graph by creating typed relationships between memories.

## When to Link Memories

- **Discovering a dependency:** "Service A depends on Service B" → link with `depends_on`
- **Documenting composition:** "Module X contains Component Y" → link with `contains`
- **Tracking authorship:** "Alice owns the billing service" → link with `owned_by`
- **Noting similarity:** "This pattern is similar to..." → link with `similar_to`

## Common Relation Types

| Relation | Meaning | Example |
|----------|---------|---------|
| `depends_on` | A requires B | Service → Database |
| `contains` | A includes B | Module → Component |
| `owned_by` | A is owned by B | Service → Team |
| `implements` | A implements B | Code → Interface |
| `similar_to` | A resembles B | Pattern → Pattern |
| `caused_by` | A was caused by B | Bug → Root cause |
| `follows` | A comes after B | Step → Step |

## How to Use Graph Traversal

- `memory_graph(start_id, direction="outbound", depth=2)` — what does this entity connect to?
- `memory_graph(start_id, direction="inbound", depth=1)` — what depends on this entity?
- `memory_graph(start_id, direction="both", depth=3)` — full neighborhood

## Tips

- Keep relations specific and consistent — use snake_case
- Add properties to edges for context (e.g., `{"version": "v2"}`)
- Depth > 3 rarely useful — keep traversals focused
