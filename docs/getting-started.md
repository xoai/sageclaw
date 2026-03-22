# Getting Started with SageClaw

Get your personal AI agent running in under 5 minutes.

## Install

Download the latest binary from [Releases](https://github.com/xoai/sageclaw/releases):

- **Windows:** `sageclaw.exe`
- **Linux/macOS:** `sageclaw`

Or build from source:

```bash
git clone https://github.com/xoai/sageclaw.git
cd sageclaw
go build -o sageclaw ./cmd/sageclaw
cd web && npm install && npm run build && cd ..
```

## Quick Start

### 1. Run SageClaw

```bash
./sageclaw
```

SageClaw starts and opens the dashboard at `http://localhost:9090`.

On first visit, you'll set a dashboard password.

### 2. Add a Provider

Go to **Providers** → **+ Add Provider**.

Choose your provider:

| Provider | What you need |
|----------|--------------|
| Anthropic | API key from [console.anthropic.com](https://console.anthropic.com) |
| OpenAI | API key from [platform.openai.com](https://platform.openai.com) |
| Google Gemini | API key from [aistudio.google.com](https://aistudio.google.com) |
| OpenRouter | API key from [openrouter.ai](https://openrouter.ai) (200+ models) |
| Ollama | Install [Ollama](https://ollama.com), run locally (free, no key needed) |

Enter your API key and click Save. Restart SageClaw to activate.

### 3. Chat

Go to **Chat** and send a message. Your agent responds using the provider you configured.

That's it — you have a personal AI agent.

## Next Steps

### Configure Your Agent

Go to **Agents** → click your agent → edit across 8 tabs:

| Tab | What it does |
|-----|-------------|
| **Identity** | Name, role, model, avatar |
| **Soul** | Personality, voice, values (markdown) |
| **Behavior** | Rules, constraints, decision frameworks (markdown) |
| **Bootstrap** | First-run instructions (auto-deleted after first conversation) |
| **Tools** | Which tools the agent can use |
| **Memory** | Memory scope, auto-store, retention |
| **Heartbeat** | Proactive cron schedules |
| **Channels** | Which channels this agent serves |

### Connect Telegram

1. Create a bot with [@BotFather](https://t.me/BotFather) on Telegram
2. Go to **Channels** → configure Telegram with your bot token
3. Go to **Channels** → click **Pair** → send the code to your bot
4. Chat with your bot on Telegram

### Connect Other Channels

| Channel | Setup |
|---------|-------|
| Discord | Create a bot in [Discord Developer Portal](https://discord.com/developers), add bot token |
| WhatsApp | Set up [WhatsApp Business API](https://developers.facebook.com/docs/whatsapp), requires webhook (use `sageclaw tunnel`) |
| Zalo | Register a [Zalo OA](https://oa.zalo.me), requires webhook |

### Expose Webhooks (WhatsApp/Zalo)

WhatsApp and Zalo require a public URL for webhooks. Use Cloudflare Tunnel:

```bash
# Install cloudflared first (free, no account needed)
sageclaw tunnel
```

Or use the dashboard: **System** → **Tunnel** → **Start Tunnel**.

## Agent Configuration Files

SageClaw stores agent configs as files on disk:

```
agents/
  default/
    identity.yaml      # Name, role, model
    soul.md            # Personality (markdown)
    behavior.md        # Rules (markdown)
    bootstrap.md       # First-run ritual (auto-deleted)
    tools.yaml         # Enabled tools
    memory.yaml        # Memory settings
    heartbeat.yaml     # Cron schedules
    channels.yaml      # Channel routing
```

Edit via the dashboard or directly on disk. Changes are detected automatically.

## CLI Commands

```bash
sageclaw                    # Start SageClaw (dashboard + all channels)
sageclaw --cli              # Force CLI interactive mode
sageclaw --tui              # Launch TUI dashboard
sageclaw --mcp              # Run as MCP server (stdio)
sageclaw tunnel             # Start Cloudflare Tunnel
sageclaw tunnel status      # Check if cloudflared is installed
sageclaw init --template=productivity  # Initialize from template
sageclaw skill install <url> # Install a skill from git
sageclaw --version          # Show version
sageclaw --help             # Show all options
```

## Environment Variables

| Variable | Purpose |
|----------|---------|
| `ANTHROPIC_API_KEY` | Anthropic API key |
| `OPENAI_API_KEY` | OpenAI API key |
| `GEMINI_API_KEY` | Google Gemini API key |
| `OPENROUTER_API_KEY` | OpenRouter API key |
| `GITHUB_COPILOT_TOKEN` | GitHub Copilot token |
| `TELEGRAM_BOT_TOKEN` | Telegram bot token |
| `DISCORD_BOT_TOKEN` | Discord bot token |
| `SAGECLAW_DB_PATH` | Database file path (default: `~/.sageclaw/sageclaw.db`) |
| `SAGECLAW_WORKSPACE` | Workspace root directory |
| `SAGECLAW_RPC_ADDR` | Dashboard address (default: `:9090`) |
| `SAGECLAW_PAIRING` | Set to `off` to disable channel pairing |
| `SAGECLAW_STORE` | `sqlite` (default) or `postgres` |
| `SAGECLAW_PG_URL` | PostgreSQL connection URL |

## Architecture Overview

```
User ──→ Channel (Telegram/Discord/CLI/Web) ──→ Pipeline
                                                    │
                                              ┌─────┼─────┐
                                              │  Debounce  │
                                              │  Intent    │
                                              │  Schedule  │
                                              └─────┼─────┘
                                                    │
                                              Agent Loop
                                              ┌─────┼─────┐
                                              │  Provider  │ (Anthropic/OpenAI/Gemini/...)
                                              │  Tools     │ (27+ tools)
                                              │  Memory    │ (FTS5 + knowledge graph)
                                              │  Middleware │ (pre-context, post-tool, pre-response)
                                              └────────────┘
```

## Security

- **Channel pairing** — one-time codes verify device ownership
- **Prompt injection protection** — 5-layer defense against malicious external content
- **Sandbox** — file operations restricted to workspace
- **Secret scrubbing** — API keys auto-redacted from output
- **Dashboard auth** — password + JWT cookies
- **Credential encryption** — AES-256-GCM for stored secrets

## Team Collaboration

Create agent teams for complex tasks:

```
Lead Agent ──→ creates tasks ──→ Member A (research)
                              ──→ Member B (writing)
                              ──→ Member C (review)
```

- Lead orchestrates via task board
- Members work independently and report results
- Blocked tasks auto-unblock when dependencies complete
- Members communicate via team mailbox

## MCP Integration

SageClaw works as both MCP **server** and **client**:

**As server** — expose SageClaw's tools to other AI tools:
- stdio: `sageclaw --mcp`
- SSE: `GET /mcp/sse` + `POST /mcp/messages`
- HTTP: `POST /mcp`

**As client** — connect to external MCP servers and import their tools into your agent's toolkit.

## Troubleshooting

| Problem | Solution |
|---------|----------|
| Dashboard won't load | Check `localhost:9090` is accessible. Try `--rpc :8080` for different port |
| No providers available | Add an API key via Providers page or environment variable, then restart |
| Telegram bot doesn't respond | Check pairing — send the pairing code to your bot first |
| Chat shows "Thinking..." forever | Check Sessions page — if the response is there, the polling may be slow. Click Refresh |
| Agent ignores personality | Check soul.md and behavior.md aren't empty. Identity anchoring prevents override |
