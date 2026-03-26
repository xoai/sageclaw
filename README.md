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

**Compound Intelligence.** Persistent memory with FTS5 search, BM25 ranking, and a knowledge graph. When you correct your agent, the lesson becomes a prevention rule — injected automatically in future sessions. When one agent isn't enough, a coordinator breaks down the problem, delegates to specialists, monitors a shared task board, and synthesizes the best result. Your agents don't just execute — they learn, coordinate, and get better.

**Quality by Design.** Four orchestration patterns — delegation, teams, handoff, and evaluate loops — ensure work isn't just distributed, it's validated. History pipeline manages context windows automatically. Auto-compaction preserves knowledge when conversations grow long. Every tool call is audited. Every decision is traceable.

**Secure by Default.** Channel pairing verifies who can talk to your agent. Five-layer prompt injection protection guards against external manipulation. Credentials encrypted with AES-256-GCM. File operations sandboxed to your workspace. Identity anchoring resists social engineering. Budget enforcement stops runaway costs. Security isn't a feature — it's the foundation.

**Built to Perform.** Single binary. Sub-second startup. SQLite with FTS5 — no external databases. Prompt caching cuts API costs up to 90%. Every package in `pkg/`, nothing hidden. No cloud accounts. No telemetry. Your machine, your data, your agents.

## Quick Start

```bash
# Build
go build -o bin/sageclaw ./cmd/sageclaw

# Start
./bin/sageclaw
```

Open `localhost:9090` — the onboarding wizard walks you through connecting a provider, creating your first agent, and choosing a channel in under 2 minutes.

## What You Get

**6 providers.** Anthropic, OpenAI, Gemini, OpenRouter, GitHub Copilot, Ollama. Tier-based routing with automatic fallback. Prompt caching for cost optimization.

**7 channels.** Web, CLI, Telegram, Discord, Zalo, WhatsApp, MCP. Connect your agent to where your conversations already happen.

**Voice messaging.** Send a voice note on Telegram, get a voice note back. Powered by Gemini Live API with native audio — no transcription pipeline, no external codecs. Pure Go, zero dependencies. Your agent hears tone, emotion, and nuance that text pipelines lose.

**Skills marketplace.** Browse and install community skills from [skills.sh](https://skills.sh). One-click install with consent review — scripts are shown before you approve. Assign skills per agent so each one only carries what it needs. Check for updates, manage assignments, uninstall — all from the dashboard.

**27+ tools.** File operations, shell commands, web search, memory, cron scheduling, team coordination, audit trail — all built in. Extend with marketplace skills or your own.

**Web dashboard.** Embedded in the binary. Onboarding wizard, agent editor, chat with session history, skills marketplace, memory explorer, knowledge graph, audit log, budget tracking, consent management — dark theme, responsive on mobile.

**4 team templates.** Productivity, Researcher, Developer, Content Creator. Pre-configured multi-agent teams ready to use: `sageclaw init --template developer`

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
- **Skill consent** — scripts reviewed and approved before installation

## Documentation

| | |
|---|---|
| [Getting Started](docs/getting-started.md) | Setup guide — first agent in 2 minutes |
| [Features](docs/features.md) | Channels, providers, tools, orchestration |
| [Agent Configuration](docs/agent-configuration.md) | Soul, behavior, tools, memory, teams |
| [How It Works](docs/how-it-works.md) | Pipeline, agent loop, memory, security |
| [API Reference](docs/api-reference.md) | Dashboard API — 70+ endpoints |
| [Architecture Decisions](docs/adr/) | ADRs and design rationale |

## License

MIT
