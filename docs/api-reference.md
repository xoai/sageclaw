# API Reference

SageClaw exposes a REST API on `localhost:9090` (configurable via `SAGECLAW_RPC_ADDR`).

All authenticated endpoints require the `sage-auth` cookie (set after login).

## Authentication

| Method | Endpoint | Description |
|--------|----------|-------------|
| `GET` | `/api/auth/check` | Check auth state (`setup`, `login`, `ready`) |
| `POST` | `/api/auth/setup` | First-time password setup (`{password, confirm}`) |
| `POST` | `/api/auth/login` | Login (`{password}`) → sets HttpOnly cookie |
| `POST` | `/api/auth/logout` | Logout → clears cookie |

## Status & Health

| Method | Endpoint | Auth | Description |
|--------|----------|------|-------------|
| `GET` | `/api/status` | Yes | Session/memory/agent counts |
| `GET` | `/api/health` | No | System health, uptime, provider status, cache stats |

## Agents (v2 — file-based)

| Method | Endpoint | Description |
|--------|----------|-------------|
| `GET` | `/api/v2/agents` | List all agents |
| `POST` | `/api/v2/agents` | Create new agent (full config JSON) |
| `GET` | `/api/v2/agents/{id}` | Get full agent config |
| `PUT` | `/api/v2/agents/{id}` | Update agent config |
| `DELETE` | `/api/v2/agents/{id}` | Delete agent (removes folder) |
| `GET` | `/api/v2/agents/{id}/{file}` | Get single file content (soul, behavior, etc.) |
| `PUT` | `/api/v2/agents/{id}/{file}` | Update single file (`{content}`) |

File names: `soul`, `behavior`, `bootstrap`, `tools`, `memory`, `heartbeat`, `channels`, `identity`

## Providers

| Method | Endpoint | Description |
|--------|----------|-------------|
| `GET` | `/api/providers` | List configured providers |
| `POST` | `/api/providers` | Add provider (`{type, name, api_key, base_url}`) |
| `DELETE` | `/api/providers/{id}` | Delete provider |
| `GET` | `/api/providers/models` | Available models + combos |

## Combos (Model Routing)

| Method | Endpoint | Description |
|--------|----------|-------------|
| `GET` | `/api/combos` | List combos (presets + custom) |
| `POST` | `/api/combos` | Create combo (`{name, strategy, models}`) |
| `DELETE` | `/api/combos/{id}` | Delete combo |

## Channels

| Method | Endpoint | Description |
|--------|----------|-------------|
| `GET` | `/api/channels` | List channels with status |
| `POST` | `/api/channels/configure` | Configure channel (`{channel, vars}`) |

## Channel Pairing

| Method | Endpoint | Description |
|--------|----------|-------------|
| `GET` | `/api/pairing/status` | Check if pairing is enabled |
| `POST` | `/api/pairing/generate` | Generate pairing code (`{channel}`) |
| `GET` | `/api/pairing` | List paired devices (`?channel=telegram`) |
| `DELETE` | `/api/pairing/{channel}/{chatID}` | Unpair a device |

## Teams

| Method | Endpoint | Description |
|--------|----------|-------------|
| `GET` | `/api/teams` | List teams |
| `POST` | `/api/teams` | Create team (`{name, lead_id, members}`) |
| `PUT` | `/api/teams/{id}` | Update team |
| `DELETE` | `/api/teams/{id}` | Delete team |
| `GET` | `/api/teams/tasks/{id}` | List tasks for a team |

## Delegation

| Method | Endpoint | Description |
|--------|----------|-------------|
| `GET` | `/api/delegation/links` | List delegation links |
| `POST` | `/api/delegation/links` | Create link (`{source, target, direction}`) |
| `DELETE` | `/api/delegation/links/{id}` | Delete link |
| `GET` | `/api/delegation/history` | Delegation history (`?agent_id=`) |

## Sessions

| Method | Endpoint | Description |
|--------|----------|-------------|
| `DELETE` | `/api/sessions/{id}` | Delete session + messages |
| `POST` | `/api/sessions/{id}/archive` | Archive session |

## Memory

| Method | Endpoint | Description |
|--------|----------|-------------|
| `GET` | `/api/memory/{id}` | Get memory by ID |
| `PUT` | `/api/memory/{id}` | Update memory (`{title, content, tags}`) |
| `DELETE` | `/api/memory/{id}` | Delete memory |

## Knowledge Graph

| Method | Endpoint | Description |
|--------|----------|-------------|
| `GET` | `/api/graph/{id}` | Get graph from node (`?direction=both&depth=2`) |
| `POST` | `/api/graph/link` | Create link (`{source_id, target_id, relation}`) |
| `DELETE` | `/api/graph/link` | Remove link (`{source_id, target_id, relation}`) |

## Cron Jobs

| Method | Endpoint | Description |
|--------|----------|-------------|
| `GET` | `/api/cron` | List cron jobs |
| `POST` | `/api/cron` | Create job (`{agent_id, schedule, prompt}`) |
| `DELETE` | `/api/cron/{id}` | Delete job |
| `POST` | `/api/cron/{id}/trigger` | Trigger job immediately |

## Audit

| Method | Endpoint | Description |
|--------|----------|-------------|
| `GET` | `/api/audit` | Query audit log (`?agent_id=&tool=&from=&to=&limit=`) |
| `GET` | `/api/audit/stats` | Audit statistics |

## Tools

| Method | Endpoint | Description |
|--------|----------|-------------|
| `GET` | `/api/tools` | List all registered tools with schemas |

## Credentials

| Method | Endpoint | Description |
|--------|----------|-------------|
| `GET` | `/api/credentials` | List stored credentials (names only, never values) |
| `POST` | `/api/credentials` | Store credential (`{name, value}`) |
| `DELETE` | `/api/credentials/{name}` | Delete credential |

## Skills

| Method | Endpoint | Description |
|--------|----------|-------------|
| `GET` | `/api/skills` | List installed skills |
| `POST` | `/api/skills/install` | Install from git (`{url}`) |
| `POST` | `/api/skills/reload` | Reload skills (SIGHUP) |
| `DELETE` | `/api/skills/{name}` | Uninstall skill |

## Templates

| Method | Endpoint | Description |
|--------|----------|-------------|
| `GET` | `/api/templates` | List available templates |
| `POST` | `/api/templates/apply` | Apply template (`{template, dir}`) |

## Tunnel

| Method | Endpoint | Description |
|--------|----------|-------------|
| `GET` | `/api/tunnel/status` | Tunnel status + cloudflared info |
| `POST` | `/api/tunnel/start` | Start tunnel (`{mode: "quick"}`) |
| `POST` | `/api/tunnel/stop` | Stop tunnel |

## Settings

| Method | Endpoint | Description |
|--------|----------|-------------|
| `PUT` | `/api/settings/password` | Change password (`{old_password, new_password}`) |
| `GET` | `/api/settings/export` | Export config as JSON |
| `POST` | `/api/settings/import` | Import config JSON |

## JSON-RPC (Legacy)

| Method | Endpoint | Description |
|--------|----------|-------------|
| `POST` | `/rpc` | JSON-RPC endpoint for sessions, memory, chat |

Methods: `sessions.list`, `sessions.get`, `sessions.messages`, `memory.search`, `memory.list`, `chat.send`

## Server-Sent Events

| Method | Endpoint | Description |
|--------|----------|-------------|
| `GET` | `/events` | SSE stream of agent events (chunk, run.started, run.completed) |

## MCP Transports

| Method | Endpoint | Description |
|--------|----------|-------------|
| `GET` | `/mcp/sse` | MCP SSE transport (event stream) |
| `POST` | `/mcp/messages` | MCP SSE transport (send request, `?sessionId=`) |
| `POST` | `/mcp` | MCP HTTP transport (stateless request/response) |
