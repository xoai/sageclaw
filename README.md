<h1 align="center">SageClaw</h1>
<p align="center">
  <img src="sageclaw-logo.svg" alt="SageClaw - Personal AI agents that compound experience." />
</p>
<p align="center"><strong>Personal AI agents that compound experience.</strong></p>

Build AI agents that remember what they learned, work together when the job is too big for one, and run entirely on your hardware. Single binary. Local-first. Yours to own.

---

- **Learn** — Mistakes become guardrails. Corrections turn into prevention rules, injected in every future session.
- **Orchestrate** — One agent or a full team. Task boards, delegation, handoff, evaluate loops.
- **Own** — Your machine, your data, your keys. Encrypted at rest, sandboxed at runtime, private by default.

## Why SageClaw

Most agent frameworks treat every conversation as a blank slate. SageClaw doesn't.

**Agents that learn and remember.** Persistent memory with full-text search, BM25 ranking, and a knowledge graph. When you correct your agent, the lesson becomes a prevention rule — injected automatically in every future session. Your agent doesn't just execute. It compounds experience.

**Quality through orchestration.** A coordinator breaks down complex problems, delegates to specialists, monitors a shared task board, and synthesizes the best result. Four patterns — delegation, teams, handoff, and evaluate loops — ensure work isn't just distributed, it's validated.

**Secure and private by design.** Your data stays on your machine — encrypted at rest, sandboxed at runtime. Channel pairing verifies who can talk to your agent. Five-layer prompt injection protection guards against external manipulation. No cloud accounts. No telemetry. No exceptions.

**Fast, light, and yours to own.** 14MB binary. Sub-second startup. 7 direct dependencies. Every package in `pkg/`, nothing hidden. You can read the entire agent loop in one sitting, trace any request from channel to response, and extend any layer without forking.

## Quick Start

```bash
# Build
go build -o bin/sageclaw ./cmd/sageclaw

# Interactive setup (API key, first agent)
./bin/sageclaw onboard

# Verify everything works
./bin/sageclaw doctor

# Start
./bin/sageclaw
```

Open `localhost:9090` for the web dashboard. Or just chat in the terminal.

## What You Get

**6 providers.** Anthropic, OpenAI, Gemini, OpenRouter, GitHub Copilot, Ollama. Tier-based routing with automatic fallback. Prompt caching for cost optimization.

**6 channels.** CLI, Telegram, Discord, Zalo, WhatsApp, MCP. Connect your agent to where your conversations already happen.

**27 tools.** File operations, shell commands, web search, memory, cron scheduling, team coordination, audit trail — all built in.

**19-page dashboard.** Embedded in the binary. Agent editor, memory explorer, knowledge graph, audit log, budget tracking, cost alerts — dark and light theme, responsive on mobile.

**4 team templates.** Productivity, Researcher, Developer, Content Creator. Pre-configured multi-agent teams ready to use: `sageclaw init --template developer`

## How Agents Work

Each agent is a folder of human-readable files:

```
agents/researcher/
  soul.md          — personality, voice, values
  behavior.md      — operating rules and decision frameworks
  identity.yaml    — name, model, status
  tools.yaml       — what this agent can do
  memory.yaml      — how it remembers
  bootstrap.md     — first-run introduction (auto-deleted)
```

Edit them by hand, through the dashboard, or let the agent refine its own behavior over time. Git-friendly. Portable. Yours.

## Architecture

![SageClaw Architecture](sageclaw_architecture.svg)

5-stage pipeline with middleware hooks at every point. Built for reliability — wall clock timeouts, activity tracking, budget enforcement, and prompt injection protection are on by default.

## Security by Default

- **Channel pairing** — one-time codes verify authorized users
- **Prompt injection protection** — 5-layer detection with Unicode defense
- **Workspace sandbox** — file operations confined to workspace root
- **Credential encryption** — AES-256-GCM at rest
- **Identity anchoring** — agents resist social engineering attempts
- **Budget enforcement** — hard stops when spending limits are reached

## Documentation

| | |
|---|---|
| [Getting Started](docs/getting-started.md) | Setup guide — first agent in 5 minutes |
| [Features](docs/features.md) | Channels, providers, tools, orchestration |
| [Agent Configuration](docs/agent-configuration.md) | Soul, behavior, tools, memory, teams |
| [How It Works](docs/how-it-works.md) | Pipeline, agent loop, memory, security |
| [API Reference](docs/api-reference.md) | Dashboard API — 60+ endpoints |
| [Architecture Decisions](docs/adr/) | ADRs and design rationale |

## License

MIT
