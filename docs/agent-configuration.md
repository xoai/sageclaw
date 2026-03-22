# Agent Configuration Guide

How to create, configure, and manage agents in SageClaw.

## Overview

An agent in SageClaw combines:
- **Identity** — who the agent is (name, role, model)
- **Soul** — personality, voice, values
- **Behavior** — rules, constraints, decision frameworks
- **Tools** — what the agent can do
- **Memory** — how the agent remembers
- **Heartbeat** — proactive scheduled actions
- **Channels** — where the agent operates

Each agent is a folder on disk. The dashboard is a friendly editor for these files.

## File Structure

```
agents/
  my-agent/
    identity.yaml      # Required — who the agent is
    soul.md            # Optional — personality (markdown)
    behavior.md        # Optional — rules (markdown)
    bootstrap.md       # Optional — first-run ritual (auto-deleted)
    tools.yaml         # Optional — enabled tools
    memory.yaml        # Optional — memory settings
    heartbeat.yaml     # Optional — cron schedules
    channels.yaml      # Optional — channel routing
```

Only `identity.yaml` is required. Everything else has sensible defaults.

## identity.yaml

```yaml
name: Research Assistant
role: personal research assistant specializing in technology
model: strong          # Tier: strong, fast, local — or specific model ID
max_tokens: 8192       # Max tokens per response
max_iterations: 25     # Max tool-use cycles per turn
avatar: "🔬"
status: active         # active or inactive
tags: [research, tech]
```

### Model Selection

| Value | Routes to |
|-------|-----------|
| `strong` | Best available (Claude Sonnet → GPT-4o → Gemini Flash) |
| `fast` | Fastest available (Claude Haiku → GPT-4o Mini → Gemini Flash Lite) |
| `local` | Ollama (runs on your machine, no API key needed) |
| `claude-sonnet-4-20250514` | Specific Anthropic model |
| `gpt-4o` | Specific OpenAI model |
| `gemini-2.0-flash` | Specific Google model |

## soul.md

Defines WHO the agent is. Written in markdown.

```markdown
# Soul

You are a thoughtful research assistant who helps users explore ideas deeply.

## Voice
- Clear and precise, never jargon-heavy
- Curious — ask follow-up questions when the topic is rich
- Honest about uncertainty — say "I'm not sure" rather than guessing

## Values
- Accuracy over speed — verify claims before presenting them
- Privacy — never share user data or conversation content
- Depth — prefer thorough analysis over surface-level summaries
```

### Tips
- Write naturally, as if describing a person
- Be specific about voice and tone — vague instructions produce vague results
- Include what the agent should NOT do (constraints are often more useful than instructions)

## behavior.md

Defines HOW the agent works. Written in markdown.

```markdown
# Behavior

## Decision Framework
1. Understand the request fully before acting
2. Search memory first — don't re-research what's already known
3. Use tools proactively for tasks that benefit from them
4. Save important findings to memory for future sessions

## Constraints
- Never execute destructive commands without explicit confirmation
- Keep responses under 500 words unless the user asks for detail
- Always cite sources when presenting research findings

## Error Handling
- If a tool fails, try an alternative approach
- If stuck after 3 attempts, explain what you tried and ask for guidance
- Never apologize excessively — acknowledge and move forward
```

## bootstrap.md

First-run instructions that execute once, then the file is automatically deleted.

```markdown
# Bootstrap

This is your first conversation with a new user.

## First Run Tasks
1. Introduce yourself warmly — share your name and what you do
2. Ask what the user is working on
3. Learn their preferences (communication style, technical level)
4. Store what you learn in memory for future sessions

## After Bootstrap
Once complete, operate normally using your soul and behavior guidelines.
```

### When to use bootstrap
- Setting up a personal assistant that needs to learn about its owner
- Creating a specialized agent that needs initial context
- Onboarding flows where the agent should ask questions first

## tools.yaml

Controls which tools the agent can use.

```yaml
# Empty enabled list = all tools available (default)
enabled:
  - fs_read
  - fs_write
  - exec
  - web_search
  - web_fetch
  - memory_search
  - memory_store
  - memory_link

# Per-tool configuration
config:
  exec:
    timeout: 30s
    sandbox: standard
  web_fetch:
    max_size: 1MB
```

### Available Tools

| Tool | Description |
|------|------------|
| `fs_read` | Read files |
| `fs_write` | Write/create files |
| `fs_list` | List directory contents |
| `exec` | Execute shell commands |
| `web_search` | Search the web |
| `web_fetch` | Fetch web page content |
| `memory_search` | Search stored memories |
| `memory_store` | Store new memories |
| `memory_link` | Link memories in knowledge graph |
| `memory_graph` | Explore knowledge graph |
| `cron_create` | Create scheduled jobs |
| `cron_list` | List scheduled jobs |
| `delegate` | Delegate work to another agent |
| `team_create_task` | Create a team task |
| `team_complete_task` | Complete a team task |
| `team_send` | Send team message (members only) |
| `team_inbox` | Check team inbox (members only) |
| `handoff` | Transfer conversation to another agent |
| `evaluate` | Run generator+evaluator loop |
| `spawn` | Create a new agent |
| `audit_search` | Query audit logs |

## memory.yaml

```yaml
scope: project           # project (per-workspace) or global
auto_store: true         # Automatically store important findings
retention_days: 0        # 0 = keep forever
search_limit: 10         # Default search results
tags_boost:              # Tags that rank higher in search
  - important
  - decision
  - learning
```

## heartbeat.yaml

Proactive schedules — the agent runs these automatically.

```yaml
schedules:
  - name: morning-briefing
    cron: "0 9 * * *"         # Every day at 9am
    prompt: "Check my tasks, emails, and calendar. Give me a morning briefing."
    channel: telegram

  - name: weekly-review
    cron: "0 17 * * 5"        # Friday at 5pm
    prompt: "Review what I worked on this week. Summarize progress and blockers."
    channel: web

  - name: memory-cleanup
    cron: "0 2 * * 0"         # Sunday at 2am
    prompt: "Review stored memories. Remove outdated entries and consolidate duplicates."
    channel: cli
```

### Cron Syntax

```
┌───────── minute (0-59)
│ ┌─────── hour (0-23)
│ │ ┌───── day of month (1-31)
│ │ │ ┌─── month (1-12)
│ │ │ │ ┌─ day of week (0-6, 0=Sunday)
│ │ │ │ │
* * * * *
```

Common patterns:
- `0 9 * * *` — daily at 9am
- `0 9 * * 1-5` — weekdays at 9am
- `*/30 * * * *` — every 30 minutes
- `0 17 * * 5` — Fridays at 5pm

## channels.yaml

```yaml
# Which channels this agent serves. Empty = all channels.
serve:
  - web
  - telegram
  - cli

# Per-channel overrides
overrides:
  telegram:
    max_tokens: 4096       # Shorter responses on mobile
  cli:
    max_tokens: 16384      # Longer responses in terminal
```

## Creating an Agent

### Via Dashboard

1. Go to **Agents** → **+ New Agent**
2. Fill in Identity (name auto-generates an ID)
3. Write soul.md and behavior.md
4. Configure tools, memory, heartbeat as needed
5. Click **Save**

### Via Files

```bash
mkdir -p agents/my-agent
cat > agents/my-agent/identity.yaml << 'EOF'
name: My Agent
role: personal assistant
model: strong
max_tokens: 8192
EOF

cat > agents/my-agent/soul.md << 'EOF'
# Soul
You are a helpful, concise personal assistant.
EOF
```

SageClaw detects the new folder automatically (file watcher).

### Via CLI Template

```bash
sageclaw init --template=productivity
```

## Agent Status

| Status | Meaning |
|--------|---------|
| `active` | Agent is running and accepting conversations |
| `inactive` | Agent is disabled — conversations are rejected |

Set status in `identity.yaml` or via the dashboard Identity tab.

## Identity Anchoring

SageClaw protects your agent's identity. If a user or external content tries to convince the agent to ignore its soul.md, act as a different agent, or override its personality — the agent politely declines.

This is automatic and always active. Your agent's core identity is non-negotiable.

## Tips

- **Start minimal** — identity.yaml + soul.md is enough. Add complexity as needed.
- **Be specific in soul.md** — "warm and concise" is better than "be helpful"
- **Use behavior.md for guardrails** — what NOT to do is often more useful than what to do
- **Bootstrap for first impressions** — a good first conversation sets the tone
- **Test via Chat** — iterate on soul/behavior by chatting and observing
- **Git-friendly** — agent configs are plain files, version them alongside your code
