# How SageClaw Works

## Pipeline Architecture

SageClaw processes every message through a 5-stage pipeline:

```
Channel → Ingestion → Debounce → Intent Classify → Lane Schedule → Agent Loop
```

### Stage 1: Ingestion

Channels (CLI, Telegram, Discord, etc.) convert platform-specific
messages into canonical format and publish to the message bus.

### Stage 2: Debounce

Rapid messages within 1000ms are merged before processing. Media
messages bypass the debounce window.

### Stage 3: Intent Classify

Two-tier classification:
- Fast path: keyword match for messages under 60 characters
- LLM path: full intent classification for complex messages

### Stage 4: Lane Schedule

Four typed lanes with per-lane semaphores:
- **main:** user conversations (DM=1, group=3 concurrent)
- **subagent:** spawned subtasks (capacity 2)
- **delegate:** delegation calls (capacity 3)
- **cron:** scheduled jobs (capacity 1)

Adaptive throttle at 60% context window. Generation counter for stale
detection.

### Stage 5: Agent Loop

Think-act-observe cycle:

1. **PreContext middleware** — inject context (memory, skills, team context)
2. **Build request** — assemble system prompt + history + tools
3. **Call LLM** — via provider (with router for tier-based selection)
4. **Process response** — extract text, tool calls
5. **Execute tools** — run tool calls, collect results
6. **PostTool middleware** — scrub secrets, audit, validate
7. **Loop** — continue until end_turn, max_iterations, or timeout (300s)
8. **PreResponse middleware** — final processing before delivery

## Middleware Hooks

Middleware runs WITHIN Stage 5, not as separate stages:

| Hook | When | Use Case |
|------|------|----------|
| PreContext | Before each LLM call | Inject memory, skill instructions, team context |
| PostTool | After each tool execution | Scrub secrets, log audit, validate results |
| PreResponse | After agent loop completes | Compaction check, response formatting |

## Agent Configuration

Each agent is defined by markdown and YAML files:

- **identity.yaml** — name, role, model, max tokens, status
- **soul.md** — personality, voice, values (injected as system prompt)
- **behavior.md** — operating rules, decision frameworks
- **tools.yaml** — which tools this agent can use
- **memory.yaml** — scope, retention, auto-save settings
- **heartbeat.yaml** — cron schedule for proactive behaviors
- **channels.yaml** — which channels this agent serves
- **bootstrap.md** — first-run ritual (auto-deleted after completion)

System prompt is assembled from: identity anchoring preamble + soul.md +
behavior.md + team context (if in a team) + skill instructions.

Context truncation: files exceeding limits are trimmed (70% start, 20% end).

## Memory System

FTS5 full-text search with BM25 ranking:

1. Tokenize query, filter high-frequency terms (>20% of docs)
2. FTS5 keyword search with BM25 scoring
3. Tag soft-boost (up to 15%), recency decay (14-day half-life)
4. Results deduplicated and ranked

Knowledge graph: typed directed edges between memories. Supports
traversal queries ("show me everything connected to concept X").

Self-learning: when users correct agents, the correction is stored
with `self-learning` tag and automatically injected as context in
future sessions.

## Security Model

### Prompt Injection Protection (5 layers)

1. **Pattern matching** — 15 regex patterns for common injection attempts
2. **Unicode normalization** — detect homoglyph and invisible character attacks
3. **Trust boundaries** — external content (web_fetch, web_search) wrapped in markers
4. **Scoring** — each layer contributes to a composite score (warn at 0.4, block at 0.8)
5. **Audit logging** — all detections logged for review

### Channel Pairing

Channels require a one-time pairing code to verify authorized users.
Without pairing, anyone who discovers your bot token can interact
with your agent.

### Workspace Sandbox

File operations are confined to the workspace root. Path traversal
attempts are blocked. Dangerous shell commands are denied.

### Credential Management

API keys and tokens are encrypted with AES-256-GCM before storage.
Secret patterns are scrubbed from tool outputs before the LLM sees them.

## Context Bridge

When the model router switches providers mid-conversation (e.g., Claude
rate-limited, falls back to GPT-4o):

1. Convert history from Provider A format → canonical
2. Truncate if new provider has smaller context window
3. Convert canonical → Provider B format
4. Log the switch for observability

## Activity Tracking

Every agent loop execution creates an Activity:

```
pending → thinking → acting → completed | failed | cancelled | timeout
```

Activities track: token usage, cost, iterations, tool calls, parent
activity (for delegation trees), and duration. The dashboard renders
Activities as the primary observability unit.

## Team Orchestration

When agents work in teams:

- **TEAM.md** injected into system prompt with role-aware content
- **Lead** orchestrates via `team_tasks` (create, assign, list)
- **Members** execute via `team_tasks` (claim, complete) and communicate via `team_message`
- **Auto-unblock:** when a task completes, blocked dependents auto-transition to pending
- **Lead tool filtering:** leads cannot use `team_message` (separation of concerns)

## Budget Enforcement

Every LLM call records cost via BudgetEngine:

- Per-session daily limits
- Per-agent monthly limits
- Hard stop or warn-only modes
- Real pricing data for 30+ models across 6 providers
- Dashboard alerts when thresholds reached
