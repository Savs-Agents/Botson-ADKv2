# Botson

Botson is an AI agent service, built in Go on top of Google's **ADK v2** (Agent Development Kit) and the Gemini API. It's a single core process — never more than one — that holds the agent registry, session state, and Gemini model, and exposes all of it over a plain, bearer-token-authenticated HTTP API. Every consumer (this repo's own terminal chat, a Discord bot, a web console) talks to the one running core purely over that API, so there's never a case of "the Discord bot has one agent instance and the web UI started a second one."

It can read and manage files, hold persistent conversations, and ask for approval before doing anything sensitive.

## Features

- **One core, one HTTP API** — `botson core` is the only process that ever holds the agent runtime; every consumer, including this binary's own chat client, talks to it purely over HTTP, never in-process
- **Official ADK REST/A2A surface** — list agents, create/run/inspect sessions, artifacts, and A2A, reverse-proxied into a real ADK server so behavior always matches upstream ADK exactly
- **Botson-specific state, same API** — settings, custom-agent CRUD, and dashboard-shaped session listing live alongside the ADK surface under `/botson/*` — nothing requires touching `~/.botson/` files directly
- **Built-in chat client** — `botson` (or `botson chat`) opens an interactive terminal chat against an already-running core, as a pure API client with no special access of its own
- **Human-in-the-loop approvals** — sensitive tool calls pause for a yes/no confirmation
- **Custom agents** — define your own agents and tool sets, saved under `~/.botson/agents/`
- **Background core** — runs detached, with `start`/`stop`/`status`, or under a real service supervisor (systemd, etc.)

## Getting started

You'll need a [Gemini API key](https://aistudio.google.com/apikey) and Go 1.26+ to build from source.

**1. Build**
```bash
go run scripts/build_linux.go     # or build_windows.go on Windows
```
This produces `bin/botson-<os>-<arch>`.

**2. Configure**

Running the core once bootstraps `~/.botson/config.json` with a generated workspace directory and API auth token, but an empty API key:

```bash
botson core start   # creates ~/.botson/config.json with a blank API key
```

Edit that file (or `PATCH /botson/settings` once a core is running — see below) to add `gemini_api_key` (and `root_agent`, if you don't want the default `"Agent Botson"`), then restart.

**3. Run the core, then chat**
```bash
botson core start   # or `botson core` to run in the foreground
botson               # opens the built-in chat client (same as `botson chat`)
```
From here, `botson chat` is one consumer among many — anything that can make an authenticated HTTP request can talk to the same core. See [docs/api.md](./docs/api.md) for the full REST API reference (every route, request/reply shape, and a worked example) if you want to build your own.

Every connection needs the API auth token generated into `~/.botson/config.json`'s `api_auth_token` field on first bootstrap. A consumer on the same machine (like this repo's own chat client) can read that file directly and pair with zero configuration; a remote consumer needs the token copied over separately.

## Configuration

Settings live in `~/.botson/config.json` — your Gemini API key, chosen model, root agent, workspace directory, host/port, and API auth token. Change it by hand-editing the file (restart the core after), via `PATCH /botson/settings` (including the API keys — restart still required for a key/model/provider change to take effect on an already-running core), or the agent's own `updateSettings` tool. The file/command tools default to `workspace_root` (`~/.botson/workspace` unless changed); a session can point them at a different, unsandboxed absolute path instead via `stateDelta` on `/api/run` — see [docs/api.md](./docs/api.md#7-session-state-conventions).

By default Botson talks to Gemini. To use a model served through [OpenRouter](https://openrouter.ai) instead, set `provider` to `"openrouter"` and `openrouter_api_key` to your key — `model_name` then needs to be the full OpenRouter model slug, not a bare Gemini model name. A `provider` change takes effect on the next `botson core` restart.

## Learn more

- **[AGENTS.md](./AGENTS.md)** — architecture, project layout, and the full CLI reference. Start here if you're contributing or maintaining the code (human or AI).
- **[docs/api.md](./docs/api.md)** — the full REST API reference for building your own consumer (a website, a TUI, a Discord bot, anything): every route, request/reply shape, the HITL confirmation protocol, and a worked example.
