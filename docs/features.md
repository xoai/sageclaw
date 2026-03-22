# SageClaw Features

## Channels (6)

| Channel | Transport | Config |
|---------|-----------|--------|
| CLI | Interactive terminal (default) | Always available |
| Telegram | Long polling | `TELEGRAM_BOT_TOKEN` |
| Discord | REST API | `DISCORD_BOT_TOKEN` |
| Zalo OA | Webhook | `ZALO_OA_ID` + `ZALO_SECRET_KEY` + `ZALO_ACCESS_TOKEN` |
| WhatsApp | Cloud API webhook | `WHATSAPP_PHONE_NUMBER_ID` + `WHATSAPP_ACCESS_TOKEN` |
| MCP | stdio / SSE / HTTP | `--mcp` flag |

## Providers (6) + Model Router

| Provider | Models | Config |
|----------|--------|--------|
| Anthropic | Claude Opus, Sonnet, Haiku | `ANTHROPIC_API_KEY` |
| OpenAI | GPT-4o, GPT-4o-mini, o1 | `OPENAI_API_KEY` |
| Gemini | Gemini 2.5 Pro, Flash | `GEMINI_API_KEY` |
| OpenRouter | Any model via OpenRouter | `OPENROUTER_API_KEY` |
| GitHub Copilot | GPT-4o, Claude via Copilot | `GITHUB_TOKEN` |
| Ollama | Any local model | Auto-detected at `localhost:11434` |

The model router maps tiers to providers:

```yaml
# Combos (presets or custom)
router:
  tiers:
    local:
      provider: ollama
      model: "llama3.2:3b"
    fast:
      provider: anthropic
      model: "claude-haiku-4-5-20251001"
    strong:
      provider: anthropic
      model: "claude-sonnet-4-20250514"
  fallback: strong
```

## Multi-Agent Orchestration (4 patterns)

**Delegation** — Agent A dispatches subtasks to Agent B (sync or async).
Concurrency-controlled with semaphores.

**Teams** — Task board + mailbox. A lead agent creates tasks, members
claim and complete them. TEAM.md injected for role-aware context.
Auto-completion of blocked dependencies.

**Handoff** — Transfer a conversation from one agent to another
mid-session with full context transfer.

**Evaluate Loop** — Generator + evaluator iterate until quality threshold
is met or max rounds exceeded.

## Memory

- FTS5 full-text search with BM25 ranking
- Tag-based filtering (hard AND) and boosting (soft relevance)
- Recency decay (14-day half-life)
- Knowledge graph (typed directed edges between memories)
- Self-learning: mistakes become prevention rules
- Discriminative term filtering (>20% frequency terms excluded)

## Agent Configuration (File-Based)

Each agent is a folder with structured config files:

```
agents/
  sageclaw/
    identity.yaml     # Name, role, model, status
    soul.md           # Personality, voice, values
    behavior.md       # Operating rules, decision frameworks
    bootstrap.md      # First-run ritual (auto-deleted)
    tools.yaml        # Enabled tools
    memory.yaml       # Memory scope, retention
    heartbeat.yaml    # Cron schedule for proactive behaviors
    channels.yaml     # Which channels this agent serves
```

Dashboard provides a multi-tab editor for all config files.

## Tools (27+)

| Category | Tools |
|----------|-------|
| File system | `read_file`, `write_file`, `list_directory` |
| Shell | `execute_command` |
| Web | `web_search`, `web_fetch` |
| Memory | `memory_search`, `memory_store`, `memory_delete` |
| Graph | `memory_link`, `memory_graph` |
| Cron | `cron_create`, `cron_list`, `cron_delete` |
| Delegation | `delegate`, `delegation_status` |
| Teams | `team_create_task`, `team_assign_task`, `team_claim_task`, `team_complete_task`, `team_list_tasks`, `team_send`, `team_inbox` |
| Orchestration | `handoff`, `evaluate`, `spawn` |
| Audit | `audit_search`, `audit_stats` |
| Credentials | `credential_store`, `credential_get` |

## Security

- **Channel pairing** — one-time codes verify authorized users
- **Prompt injection protection** — 5-layer detection (15 patterns, Unicode defense)
- **Workspace sandbox** — file operations confined to workspace root
- **Shell deny patterns** — dangerous commands blocked
- **Credential encryption** — AES-256-GCM at rest
- **Secret scrubbing** — API keys removed from tool outputs
- **Identity anchoring** — agents resist social engineering to override personality

## Dashboard (19 pages)

Web UI embedded in the binary at `localhost:9090`:

**Core:** Overview, Chat (agent selector, session picker), Agents (multi-tab editor)
**Conversations:** Sessions (bulk delete/archive), Activity (real-time events)
**Data:** Memory (search/edit/delete), Knowledge Graph, Audit Log (filterable)
**Connectivity:** Providers (test connection, combos), Channels (configure), Tunnel (Cloudflare)
**Capabilities:** Skills (install/uninstall/reload), Tools (registry browser), Cron, Teams, Delegation
**System:** Budget (cost tracking, alerts), Health, Settings (credentials, templates, import/export)

Responsive design with dark/light theme toggle.

## Templates

Pre-configured agent teams:

| Template | Agents | Use Case |
|----------|--------|----------|
| Productivity | Coordinator, Researcher, Coder | General tasks |
| Researcher | Analyst, Explorer | Deep research |
| Developer | Architect, Coder, Reviewer | Software development |
| Content Creator | Editor, Writer, SEO | Blog posts, newsletters |

Apply via CLI (`sageclaw init --template productivity`) or dashboard.

## Prompt Caching

Automatic cache-control headers for Anthropic (system prompt + conversation
history). Cost tracking shows cache hit rates and savings in the dashboard.

## Budget Management

- Per-session daily limits and per-agent monthly limits
- Hard stop or warn-only modes
- Cost tracking per model with real pricing data
- Dashboard alerts with acknowledgment workflow
- Top model usage breakdown

## Cloudflare Tunnel

Built-in integration for exposing webhook channels (WhatsApp, Zalo):

```bash
sageclaw tunnel    # CLI management
```

Or configure via the dashboard Tunnel page.
