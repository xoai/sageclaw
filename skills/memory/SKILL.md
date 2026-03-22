---
name: memory
description: Knowledge capture and retrieval using persistent memory
version: "1.0.0"
tools: [memory_search, memory_get]
---

# Memory Skill

Use memory to persist knowledge across conversations.

## When to Search Memory

- **Before starting any task** — search for relevant context, past decisions, or warnings
- **When the user references something from a previous conversation**
- **Before making architectural or design decisions** — check for existing decisions

## When to Store Memory

- **After learning something significant** about the codebase, architecture, or domain
- **After making a decision** with rationale worth preserving
- **After debugging** — store the root cause and fix
- **After the user corrects you** — store a self-learning rule

## How to Use Tags

- Use domain-specific tags: `go`, `database`, `auth`, `frontend`, etc.
- Use `self-learning` tag for mistake prevention rules
- Use `architecture` tag for design decisions
- Tags enable filtering — choose them carefully

## Memory Search Tips

- Use natural language queries, not keywords
- Use `filter_tags` for hard filtering (AND logic)
- Use `tags` for soft boosting (higher ranking, not exclusion)
- Recent memories rank higher (14-day half-life decay)
