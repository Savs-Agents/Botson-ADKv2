# AGENTS.md

This file orients AI coding agents (and human maintainers doing a deep dive) working in this codebase. For a short, human-facing project overview, see [README.md](./README.md).

Botson is a Go-based agent framework built on Google's **ADK v2**. It's one **core process** (the Gemini model, agent registry, session/artifact services) exposed over a single **HTTP API**, and HTTP is the *only* way anything talks to it. `botson core` never runs a TUI, tray, or any other in-process interface itself — this binary's own chat client (`botson chat`) is a separate, pure API consumer, no different in principle from a Discord bot or web console someone else builds against the same API. See "Architecture" below for why, and [docs/api.md](./docs/api.md) for the full consumer-facing API reference.

---

## Project structure

- **`/cmd`**: application entry points. `botson-core` is the only one.
  - **[`/botson-core`](./cmd/botson-core/)**: the primary application (ships as `botson-<os>-<arch>`) — a small Cobra CLI with two subcommands: `core` (the service itself, plus `start`/`stop`/`status`) and `chat` (the built-in terminal chat client). A bare `botson` with no subcommand is rewritten to `botson chat` in `main.go` before Cobra resolves it.
- **`/frontends`**: standalone consumers of the core's HTTP API, each with zero access to the agent runtime.
  - **[`/chat`](./frontends/chat/)**: a Bubble Tea terminal chat client (`client.go`/`model.go`/`update.go`/`view.go`/`render.go`). Talks to a running core purely over HTTP with a bearer token — the same interface any external consumer would use. This is the first thing that lives under `frontends/`; more (e.g. a Discord/Slack bridge) can land here later without ever touching `internal/`.
- **`/internal`**: main application packages.
  - **[`/config`](./internal/config/)**: `AppConfig` struct, load/save/update, and data-dir lookups (`~/.botson/`). `Load` caches a single shared instance per process and `Update` mutates it in place (see "Self-configuration" below) — this is the one package every settings-reading/writing code path ultimately goes through, so it can't import `internal/management`, `internal/engine/agent`, or `internal/engine/tools` without creating a cycle.
  - **[`/daemon`](./internal/daemon/)**: generic detach/control lifecycle (start/stop/status, PID files, the loopback control channel) for `core start`/`stop`/`status`.
  - **[`/networking/api`](./internal/networking/api/)**: Botson's single unified HTTP server (`server.go`) — one `http.Server`, bound to a configurable host:port, serving ADK's own REST/A2A surface (`adkbackend.go` runs a real ADK REST server internally on a loopback port; `adkreverseproxy.go` fronts it with a plain reverse proxy) and Botson's own settings/agents/sessions/dashboard routes (`routes.go`) side by side, both behind one bearer-token auth middleware (`auth.go`). See "Architecture" below.
  - **[`/networking/adkwire`](./internal/networking/adkwire/)**: the shared ADK REST wire types (`RunAgentRequest`, `Event`) used by both `internal/automode` and `frontends/chat` — one definition instead of two independently hand-rolled copies.
  - **[`/management`](./internal/management/)**: shared, interface-agnostic business logic (agents, sessions, config, dashboard) — the functions `internal/networking/api`'s handlers call. `ListSessions`/`GetSession`/`DeleteSession` (`sessions.go`) only need a `session.Service`, not the full Gemini/agent-loader bootstrap.
  - **[`/storage/session`](./internal/storage/session/)**: GORM & SQLite implementation for persisting conversation state. `InitPersistentSessionService` silences GORM's default logger (it writes to stdout, not stderr) at construction, since a consumer reading structured output off stdout would otherwise get corrupted output — don't reintroduce a per-consumer workaround for this.
  - **[`/storage/artifact`](./internal/storage/artifact/)**: local filesystem service for persistent artifacts (`local.go`). `appName`/`userID`/`sessionID`/`fileName` are validated (`sanitizeSegment`) before they ever become path segments — no path separators, Windows-reserved characters, control characters, or `.`/`..` — plus a `confineToBase` containment check as defense in depth. Fix path-safety bugs here, not per-caller; see `local_test.go` for the full rejection matrix.
  - **[`/engine/agent`](./internal/engine/agent/)**: custom recursive agent loader, default definitions, and tool registry.
  - **[`/engine/tools`](./internal/engine/tools/)**: secure tools (`listFiles`, `readFile`, `writeFile`, `editFile`, `loadArtifacts`, `saveArtifact`, `updateSettings`, `runCommand`). `readFile`/`writeFile`/`editFile` share path validation via `resolveWorkspacePath` (`workspace.go`) — the one place that confines a tool to the workspace root and blocks `.env` access, so fix path-safety bugs there rather than per-tool.
  - **[`/engine/tools/procutil`](./internal/engine/tools/procutil/)**: `Run(ctx, name, args, opts)` — runs a subprocess with a timeout that actually works (kills the whole process group, not just the direct child) and truncates captured output. Leaf package (only depends on the stdlib), used by `runCommand` so this exec-safety logic exists in exactly one place.
  - **[`/engine/toolorder`](./internal/engine/toolorder/)**: ADK plugin serializing a turn's parallel-dispatched tool calls into the order the model emitted them, across HITL confirmation pauses/resumes — including deferring non-gated calls (via synthetic, auto-approvable confirmations) behind gated ones. See the package doc and "HITL confirmation wire protocol" below.
  - **[`/automode`](./internal/automode/)**: background worker (started alongside the HTTP API server in `cmd/botson-core`'s `runCoreServer`) that keeps a session moving after every client disconnects. Polls the shared session service for sessions flagged `management.AutoModeStateKey`, finds any confirmation left pending, and answers it itself by calling `POST /api/run` on the core's own loopback address — one more HTTP client of the standard API, not a fork of it. See the package doc and "HITL confirmation wire protocol" below.
  - **[`/providers`](./internal/providers/)**: builds the `model.LLM` at boot from config (`"gemini"` or `"openrouter"`).

## Architecture / how it works

1. **Registry loading**: default agents (bundled) and custom user agents (from `~/.botson/agents/`) are parsed and built recursively, supporting tool configuration and sub-agent delegation.
2. **Core hosting**: `botson core` runs one `http.Server` (`internal/networking/api`) bound to a configurable `host:port`. Internally, it also starts a real ADK REST+A2A server (`google.golang.org/adk/v2/cmd/launcher/prod`) on a loopback-only port and reverse-proxies `/api/*`, `/a2a/*`, and the agent-card path into it — so that surface always matches upstream ADK's own behavior exactly, never reimplemented by hand. Botson's own routes (`/botson/*`) are mounted on the same router. Both are behind one bearer-token auth middleware. This is the one process that holds the agent registry, session service, and artifact service in memory.
3. **Every consumer is an HTTP client.** There is no in-process interface of any kind in this repo — including this binary's own `botson chat`, which only ever talks to the core over the same API an external consumer would use.

### Why this shape (history worth knowing)

Historically, `botson tui`, `botson web`, and `botson discord` were three fully independent OS processes, each running its own copy of `setupApp()`'s bootstrap (Gemini model, agent registry, session service) with no in-memory sharing — the actual bug, not "running a TUI in this binary" per se, was that every daemon booted its own private copy of the whole agent runtime. Two redesigns followed: first, one core process holding the state and exposing it over an embedded NATS server; later (2026-07, this one) dropping NATS as a transport entirely in favor of exposing the same functionality over plain HTTP, since nothing in this project actually needed a message bus — a single bearer-token-authenticated HTTP server does the same job with less machinery. `botson chat` was then added back as a genuinely safe TUI, specifically *because* it's built as a pure HTTP client of the core's own public API rather than a second copy of the runtime — the constraint that matters is "never boot a second agent runtime," not "never ship a TUI in this binary."

- **The core is `botson core`** (`cmd/botson-core/cmd_core.go`). `runCore` registers daemon state (`daemon.WriteState`, the loopback control channel) and calls `runCoreServer`, which starts `api.Server` (the HTTP API) and `automode.Run` (the background HITL auto-answer worker) concurrently via `errgroup` until `ctx` is cancelled. `runCore` always registers, regardless of how the process was launched — directly (`botson core`), detached (`core start`), or under an external supervisor like systemd.
- **The internal ADK backend's port is never advertised** and only reachable via the reverse proxy in normal operation, but it is not itself network-isolated by ADK (the underlying launcher always binds to all interfaces, not loopback-only) — anything that can already reach the host machine could reach that port directly, unauthenticated, if it guessed it. The bearer-token auth middleware in front of the reverse proxy is what actually gates real traffic; this is an accepted residual risk, not a solved one.
- **Streaming (`run_sse`/`run_live`) is proxied through but unexercised.** Since the reverse proxy forwards to a real ADK server (not a hand-rolled subset), these routes exist and Go's `httputil.ReverseProxy` does flush `text/event-stream` responses immediately rather than buffering — but no consumer in this repo has driven them yet. `frontends/chat` and `internal/automode` both use `/api/run` and wait for the full turn.
- **Known limitation, not solved by this**: switching an *already-running* core's workspace directory. It's pinned for that process's lifetime — restart it from a new directory to change it. True per-session/per-tool-call workspace switching would require threading a workspace argument through `agent.Context` and every tool built on `os.Getwd()`, a materially bigger change than this.

## Self-configuration

`internal/config.Load()` returns a single cached `*AppConfig` per process (not a fresh read each call), and `internal/config.Update(mutate func(*AppConfig))` edits that cached instance's fields **in place** before persisting to disk, rather than building a new struct and swapping the pointer. That means every long-lived holder of the config pointer within the core process (`cmd/botson-core`'s `appBoot.Config`) sees an `Update` immediately, with no restart needed. `PATCH /botson/settings` (`internal/networking/api`) goes through `config.Update` for this reason — see `internal/config/config_test.go` for the regression test guarding this specifically (it would be easy to "simplify" `Update` back into load-then-replace and silently break this).

This is what makes the `updateSettings` agent tool (`internal/engine/tools/update_settings.go`) meaningful: the running agent can change its own model/root-agent mid-conversation and have it actually take effect for the rest of that process's life, not just on next launch. It deliberately excludes secrets (the Gemini API key) — that stays human-controlled via `PATCH /botson/settings` (or hand-editing `config.json`), so a confused or compromised agent can't rotate or wipe its own credentials. `RequireConfirmation: true` is set on its registry entry (`internal/engine/agent/registry.go`), same as `saveArtifact`, so it still pauses for a HITL approval before taking effect.

## Coding/exec tools

`writeFile` and `runCommand` (`internal/engine/tools/write_file.go`, `internal/engine/tools/run_command.go`) give the agent real editing and shell-execution capability in the project workspace, on top of the earlier read-only `readFile`/`listFiles`. Both default to `RequireConfirmation: true` in the registry, same posture as `saveArtifact`/`updateSettings` — this was a deliberate choice (2026-07) since it's the biggest capability jump in the tool registry so far, not because the code backing them is untrusted.

`runCommand` runs the given string through the platform's own shell (`/bin/sh -c` / `cmd /C`) in the workspace root, via `internal/engine/tools/procutil.Run` (timeout default 120s, output capped at ~200KB to protect the agent's own context). `procutil.Run` is the one place that handles two easy-to-get-wrong things correctly: killing the *whole process group* on timeout rather than just the direct child (`exec.CommandContext` alone would leave a forked-not-exec'd grandchild running, e.g. `sh -c "sleep 5"` on this box, holding the captured stdout/stderr pipe open past the shell's own death and silently defeating the timeout — see `internal/engine/tools/procutil/procutil_test.go`'s timeout case), and correctly classifying "killed by our own timeout" separately from "process ran and exited non-zero" (a SIGKILL'd process surfaces as the same `*exec.ExitError` type as a normal non-zero exit, so the timeout check has to run first).

**`editFile`** (`internal/engine/tools/edit_file.go`, added 2026-07 mirroring how Claude Code's own Edit tool works) makes a precise find-and-replace edit — `oldString` must match the file's current content exactly, and exactly once unless `replaceAll` is set — rather than requiring `writeFile` to regenerate an entire file from memory just to change a few lines. `readFile` (`internal/engine/tools/read_file.go`) was rewritten alongside it to return `cat -n`-style line-numbered, paginated output (`offset`/`limit`, default 2000-line limit) instead of the whole file as one string, so a line number it reports can be quoted directly in a following `editFile` call.

**Read-before-write guard** (`internal/engine/tools/read_tracking.go`): `writeFile` and `editFile` both refuse to touch a file that hasn't been read via `readFile` earlier in the *same session* — except a brand-new file, which is exempt (nothing to have read). Tracking uses `agent.Context.State()`, verified to be a durable, session-scoped key/value store (traced through `agent/common_context.go` → the ADK runner's `StateDelta` → `session/database/service.go` → `internal/storage/session/persistent.go`'s GORM/SQLite backing). **Key-shape matters here**: one flat state key per absolute path (`"botson:tools:read:" + fullPath` → `bool`), not one key holding a `map[string]bool` — session state is JSON round-tripped on reload, so a map value comes back as `map[string]interface{}` on a later turn while a bool round-trips losslessly as a bool either way. The guard fails open (silently skipped) if `ctx`/`ctx.State()` is nil, which only happens in hand-written unit tests, never in production tool invocation — see `internal/engine/tools/fake_context_test.go`'s `fakeContext` (embeds `agent.ContextMock`, overrides `State()`) for the test double that lets tests actually exercise the guard instead of bypassing it.

Verified live end-to-end (not just unit-tested): drove a real conversation through the core asking the agent to edit a file. It chose `listFiles` → `readFile` (confirmed the state delta really contained `"botson:tools:read:<path>": true`) → `editFile` with a whitespace-exact `oldString`/`newString` pulled straight from the numbered read → paused for HITL confirmation (since `editFile` is `RequireConfirmation: true`) → after approval, the file was changed correctly with nothing else disturbed, and the agent even re-read the file on its own to confirm.

## HITL confirmation wire protocol

ADK's `RequireConfirmation: true` (used by `saveArtifact`, `updateSettings`, `writeFile`, `editFile`, `runCommand`) does **not** simply pause and resume the original tool call — the full event sequence, the "two `functionResponse`s for one call id" trap, the ordering-only `internal/engine/toolorder` deferrals, and auto mode are all documented in **[docs/api.md §5](./docs/api.md#5-human-in-the-loop-hitl-confirmations)**, which is the canonical reference (it's consumer-facing, so it has to be exactly right). The short version: a gated call's real result doesn't come back directly — a synthetic `adk_request_confirmation` call shows up instead, a consumer answers it with `{"confirmed": true|false}` on a following `/api/run`, and only then does the real tool run. `frontends/chat/render.go`'s `renderEvents` is a working reference implementation of the client-side detection logic.

## Platform-specific files

Windows-only functionality (`internal/daemon`'s detach mechanics, `internal/engine/tools/procutil`'s process-group kill) is split via Go build tags into `_windows.go` / `_unix.go` files (`detach_windows.go`/`detach_unix.go`, `procutil_windows.go`/`procutil_unix.go`). When adding a platform-specific feature, follow this pattern rather than runtime `if runtime.GOOS` branching inside shared files.

No Windows machine is available in this environment — changes touching either package are verified with `GOOS=windows GOARCH=amd64 go build ./...` cross-compilation only. There's no CI gate for this yet.

## CLI reference

Build platform binaries into `/bin`:
```bash
go run scripts/build_windows.go   # Windows
go run scripts/build_linux.go     # Linux
```

### First-run setup

There's no dedicated install/config command. `config.Load()` (`internal/config/config.go`) bootstraps `~/.botson/config.json` on first read regardless of how it's triggered — e.g. by just running `botson core` — filling in a generated `workspace_root` and `api_auth_token`, a default `model_name`/`root_agent`, but a blank `gemini_api_key`. Fill that in by hand-editing the file, or via `PATCH /botson/settings` on an already-running core (see "Configuration reference" below), then (re)start the core.

### Running the core

```bash
botson core --port=4222         # foreground: the HTTP API server plus the automode worker
botson core start --port=4222   # detached background process with a PID-file-backed lifecycle
botson core status               # reads the state file + probes the control channel
botson core stop [--force]      # graceful stop via control channel, or force-kill
```
Logs: `~/.botson/logs/core.log`. State: `~/.botson/core.pid`. Since Windows has no signal-based graceful shutdown for an arbitrary detached process, `stop` talks to a small loopback control channel the background process opens instead — this works identically on Linux.

### Chatting

```bash
botson                    # opens the built-in terminal chat client (same as `botson chat`)
botson chat --agent=NAME  # chat with a specific agent instead of root_agent
```
Requires a `botson core` already running and reachable — chat never starts, manages, or falls back to running its own core; on an unreachable core it prints an error pointing at `botson core start` and exits.

### Everything else is an HTTP route, not a CLI command

Settings, custom-agent CRUD, and session/dashboard management are all `/botson/*` routes (`internal/networking/api/routes.go`) — see that file for the full route table. Creating/running/inspecting a session mid-conversation, artifacts, and A2A go through `/api/*`/`/a2a/*` (proxied into a real ADK server). There is no CLI equivalent for any of this — a raw HTTP client (`curl`, or a script in any language) is the only way to exercise it outside of building a full consumer project. **See [docs/api.md](./docs/api.md) for the full consumer-facing reference** — every route, request/reply shape, and a worked example.

## Configuration reference

`~/.botson/config.json`:
```json
{
  "model_name": "gemini-3.1-flash-lite",
  "gemini_api_key": "your_api_key_here",
  "provider": "gemini",
  "openrouter_api_key": "",
  "root_agent": "Agent Botson",
  "workspace_root": "/home/you/.botson/workspace",
  "host": "127.0.0.1",
  "port": 4222,
  "api_auth_token": "a generated hex token"
}
```
`provider` selects which `internal/providers` backend builds the model at
boot: `"gemini"` (default) or `"openrouter"`. `model_name` is interpreted
accordingly -- a bare Gemini model name, or a full OpenRouter model slug
(e.g. `"anthropic/claude-3.5-sonnet"`) when `provider` is `"openrouter"`,
in which case `openrouter_api_key` is required instead of (or alongside)
`gemini_api_key`. Like `model_name`/`root_agent`, changing `provider`
takes effect on the next core restart, not live.

`host`/`port` are the bind address of Botson's own HTTP API server
(`internal/networking/api`); a CLI flag on `botson core`/`core start` can
override either for a single run without touching these persisted
defaults.

`workspace_root` and `api_auth_token` are generated automatically the
first time the config is loaded if either is missing (see
`fillDefaults` in `internal/config/config.go`) -- there's nothing
to set by hand on a fresh install. `api_auth_token` gates every
connection to the HTTP API server and
is deliberately never exposed through `GET /botson/settings`/`Mask()` —
it's the credential gating that very API. `workspace_root` is the default
directory the file/command tools operate in (`internal/engine/tools/workspace.go`);
a session can override it per-session via `stateDelta` on `/api/run` (see
[docs/api.md §7](./docs/api.md#7-session-state-conventions)),
to any absolute path — not sandboxed, unlike `workspace_root` itself.

Prefer `PATCH /botson/settings` or the `updateSettings` tool over hand-editing this file while a `botson core` process is running, so the in-memory copy that process is holding doesn't drift from disk — see "Self-configuration" above. Hand-editing is fine when no core is running (e.g. the very first edit, to add `gemini_api_key`).

## Dependencies

Prefer the standard library where it can reasonably do the job; the project leans on these specific third-party packages rather than pulling in new ones casually:

- `google.golang.org/adk/v2` — core Agent Development Kit
- `google.golang.org/genai` — Gemini API client
- `github.com/a2aproject/a2a-go/v2` — A2A protocol types, needed to mount the proxied `/a2a/*`/agent-card routes at the exact paths the internal ADK backend uses
- `github.com/gorilla/mux` — HTTP routing for `internal/networking/api`'s composed router (ADK reverse proxy + Botson's own routes)
- `github.com/spf13/cobra` — CLI command/flag framework powering `botson`'s subcommands
- `github.com/charmbracelet/bubbletea` + `bubbles` + `lipgloss` — the terminal chat client's TUI stack (`frontends/chat`)
- `golang.org/x/sync` (`errgroup`) — runs the HTTP API server and the automode worker concurrently under one cancellable context in `runCoreServer`
- `gorm.io/gorm` (+ `glebarez/sqlite`) — ORM/SQLite backing session persistence

## Conventions

- Commit messages follow Conventional Commits style: `feat:`, `fix:`, `refactor:`, etc., imperative mood, no trailing period.
- Prefer adding a flag with a sensible default over introducing a new prompt, when a feature needs to be scriptable.
- Cobra commands that only manage a background process's lifecycle, or act as a pure API client, set `PersistentPreRunE: noBootstrap` to skip the expensive config/Gemini/agent/session bootstrap — see `newCoreStartCmd`, `newCoreStopCmd`, `newCoreStatusCmd`, `newChatCmd`.
- Import direction: `cmd/botson-core` → `internal/networking/api` → `internal/management` → `internal/engine/agent` → `internal/engine/tools` → `internal/config`. `internal/engine/tools` must never import `internal/management` or `internal/engine/agent` (it would cycle back through `internal/engine/agent`'s import of `internal/engine/tools`) — shared logic those layers both need (e.g. `Mask`) belongs in `internal/config` instead, not `internal/management`.
