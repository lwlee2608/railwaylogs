# railwaylog — Architecture

A drop-in replacement for `railway logs` that streams deployment logs directly from
Railway's GraphQL API, auto-reconnects when the stream drops, and emits NDJSON so
output pipes cleanly into log formatters like `humanlog`.

- **Language:** Go 1.25
- **Transport:** GraphQL over HTTPS (queries) + graphql-transport-ws over WSS (subscription)
- **Output:** newline-delimited JSON (NDJSON) on stdout
- **Size:** ~1,000 LOC, 9 Go source files

---

## 1. System Context

The binary runs on the user's workstation, reads config + credentials locally, and
talks to a single upstream (Railway's GraphQL endpoint). Output is a Unix pipe.

```
            ┌─────────────────────────────┐
            │         Developer           │
            │ (terminal, Ctrl+C, pipes)   │
            └──────────────┬──────────────┘
                           │ stdin: flags
                           │ stdout: NDJSON
                           ▼
┌──────────────────────────────────────────────┐
│               railwaylog (CLI)               │        ┌────────────────────┐
│                                              │ HTTPS  │  backboard         │
│  ┌────┐  ┌────────┐  ┌──────┐  ┌──────────┐  │ ─────► │  .railway.com      │
│  │cmd │─►│ config │─►│ api  │─►│  output  │  │        │  /graphql/v2       │
│  └────┘  └────────┘  └──────┘  └──────────┘  │ ◄───── │  (HTTP + WSS)      │
│              ▲           ▲          │        │  WSS   └────────────────────┘
│              │           │          ▼        │
│   ~/.config/railwaylog  ~/.railway  stdout   │
│   config.yaml           config.json (pipe)   │
│                         env vars             │
└──────────────────────────────────────────────┘
                           │
                           ▼
                     ┌──────────┐
                     │ humanlog │ (optional, downstream)
                     └──────────┘
```

---

## 2. Package Map

```
railwaylog/
├── cmd/railwaylog/          entry point — flag parsing, orchestration, signals
│   ├── main.go              run(): wires config → api → output
│   └── logger.go            slog setup (file-based diagnostic log)
│
├── internal/
│   ├── config/              YAML config with embedded defaults + backfill
│   │   ├── config.go        Config structs, Load(), backfillDefaults()
│   │   └── default_config.yaml   //go:embed baseline
│   │
│   ├── railway/             Railway CLI interop + auth resolution
│   │   └── link.go          reads ~/.railway/config.json, env vars
│   │
│   ├── api/                 GraphQL client (HTTP + WS subscription)
│   │   ├── client.go        Client, Query(), auth headers
│   │   ├── deployment.go    LatestDeployment query
│   │   ├── logs.go          StreamDeployLogs — reconnect loop + WS frames
│   │   └── retry.go         exponential backoff
│   │
│   └── output/
│       └── ndjson.go        NDJSON Writer, attribute JSON-unwrap
│
└── pkg/                     (reserved; currently empty)
```

Boundaries are strict: `cmd` is the only package that imports everything; `api`
depends on `railway` (auth) and `output` (sink); `config` and `output` depend on
nothing internal.

---

## 3. Component Diagram

```
                        ┌────────────────────────┐
                        │  cmd/railwaylog.run()  │
                        │  main.go:31            │
                        └──┬──────────┬───────┬──┘
           resolveLinked() │          │       │ signal.NotifyContext
                           ▼          ▼       ▼
             ┌──────────────┐  ┌─────────────┐  ┌──────────────┐
             │ config.Load  │  │ railway     │  │  ctx         │
             │ +backfill    │  │ .Load /     │  │  (SIGINT/    │
             │              │  │  AuthFromEnv│  │   SIGTERM)   │
             └──────┬───────┘  └──────┬──────┘  └──────┬───────┘
                    │                 │                │
                    └────────┬────────┘                │
                             ▼                         │
                   ┌──────────────────┐                │
                   │   api.Client     │                │
                   │   http+ws+auth   │                │
                   └──┬────────────┬──┘                │
      LatestDeployment│            │ StreamDeployLogs  │
                      ▼            ▼                   │
           ┌──────────────┐  ┌──────────────────┐      │
           │ HTTP POST    │  │  WSS subscription│◄─────┘
           │ (one-shot)   │  │  + retry loop    │
           └──────────────┘  └────────┬─────────┘
                                      │ LogLine
                                      ▼
                            ┌──────────────────┐
                            │ output.Writer    │
                            │ (NDJSON, mutex)  │
                            └────────┬─────────┘
                                     │
                                     ▼
                                  stdout
```

---

## 4. Startup Sequence

Happy path from `railwaylog --service svc --environment env` to first log line.

```
User         cmd.run         config        railway       api.Client     WS server
 │              │              │              │              │              │
 │─ flags ─────►│              │              │              │              │
 │              │─ Load ──────►│              │              │              │
 │              │◄─ Config ────│              │              │              │
 │              │──────── Load / AuthFromEnv ►│              │              │
 │              │◄─── Auth + LinkedProject ───│              │              │
 │              │  resolveLinked(flags>cfg>env>link)         │              │
 │              │─────────── NewClient ─────────────────────►│              │
 │              │─────────── LatestDeployment ──────────────►│              │
 │              │                                            │─ HTTP POST ─►│
 │              │                                            │◄─ {id,status}│
 │              │◄────────── Deployment ─────────────────────│              │
 │              │─────────── StreamDeployLogs ──────────────►│              │
 │              │                                            │─ WSS Dial ──►│
 │              │                                            │◄─ open ──────│
 │              │                                            │─ connect_init►
 │              │                                            │◄─ ack ───────│
 │              │                                            │─ subscribe ─►│
 │              │                                            │◄─ next ──────│
 │              │                                            │── LogLine ──►│ output.Write
 │◄─── NDJSON on stdout ──────────────────────────────────────────────────  │
```

Key file references:

- `cmd/railwaylog/main.go:31` — `run()` orchestration
- `cmd/railwaylog/main.go:103` — `resolveLinked()` priority layering
- `internal/api/logs.go:62` — `StreamDeployLogs` reconnect loop
- `internal/api/logs.go:96` — `runStream` single connection
- `internal/api/logs.go:110` — 16 MiB WS read limit

---

## 5. Configuration Resolution

Four sources, resolved per-field (not per-source). First hit wins.

```
                                              priority
┌────────────────────────────┐                    ▲
│ CLI flag (--service, etc.) │  highest           │
└────────────────────────────┘                    │
┌────────────────────────────┐                    │
│ config.yaml                │                    │
│ ~/.config/railwaylog/      │                    │
└────────────────────────────┘                    │
┌────────────────────────────┐                    │
│ RAILWAY_*_ID env vars      │                    │
└────────────────────────────┘                    │
┌────────────────────────────┐                    │
│ ~/.railway/config.json     │  lowest            │
│ (official CLI's link)      │                    ▼
└────────────────────────────┘
```

### Config schema

```yaml
log:
  level: info # info | warn | debug | error | OFF
  path: "" # default: $XDG_STATE_HOME/railwaylog/railwaylog.log

railway:
  project_id: ""
  environment_id: ""
  service_id: ""
  http_endpoint: "https://backboard.railway.com/graphql/v2"
  ws_endpoint: "wss://backboard.railway.com/graphql/v2"

reconnect:
  max_attempts: 12
  initial_delay_ms: 1000
  max_delay_ms: 8000
```

### Backfill defaults

On load, any missing key is merged in from the embedded `default_config.yaml`
(`//go:embed`). This lets new releases add config fields without breaking
existing user files. Backfill write failures are logged but non-fatal — the
in-memory config always reflects the merged result.

- `internal/config/config.go:42` — `Load()`
- `internal/config/config.go:99` — `backfillDefaults()`

### Auth kinds

| Kind           | Env var             | Header sent                     |
| -------------- | ------------------- | ------------------------------- |
| Bearer         | `RAILWAY_API_TOKEN` | `Authorization: Bearer <token>` |
| Project-access | `RAILWAY_TOKEN`     | `project-access-token: <token>` |

`RAILWAY_TOKEN` wins if both are set. Falls back to `user.token` /
`user.accessToken` in `~/.railway/config.json`.

---

## 6. Streaming: Reconnect State Machine

`StreamDeployLogs` wraps `runStream` in a retry loop. Two pieces of state
persist across reconnects:

- `state.lastTimestamp` — the latest log timestamp observed, used to dedupe
  replays after a reconnect (Railway re-sends from the start).
- `backoff` — exponential delay (1.5×), reset on any successful `next` frame.

```
            ┌─────────────┐
   start ──►│  Dial WSS   │◄───────────────────┐
            └──────┬──────┘                    │
                   ▼                           │
            ┌──────────────┐                   │
            │ connect_init │                   │
            │ + ack        │                   │
            └──────┬───────┘                   │
                   ▼                           │
            ┌──────────────┐                   │
            │  subscribe   │                   │
            └──────┬───────┘                   │
                   ▼                           │
           ┌───────────────┐  ping  ┌───────┐  │
           │  read frame   │──────► │ pong  │──┘
           │               │        └───────┘
           │               │  next      → dedupe → output.Write → reset backoff
           │               │  error     → reconnect
           │               │  complete  → exit(0)  (clean server shutdown)
           │               │  ctx done  → exit     (SIGINT/SIGTERM)
           └──────┬────────┘
                  │ error (not complete)
                  ▼
            ┌──────────────┐  attempts left? ─ no ─► fail
            │ backoff.Next │       │
            └──────┬───────┘       yes
                   ▼                │
              sleep(delay) ─────────┘
                   │
                   └────────── reconnect ──────────┐
                                                   │
                                                   └─► Dial WSS
```

### Dedup

```go
// internal/api/logs.go:152
if state.lastTimestamp != "" && line.Timestamp <= state.lastTimestamp {
    continue // replay after reconnect — already delivered
}
```

Relies on Railway emitting strictly-monotonic RFC3339 nanosecond timestamps on
the subscription. If two distinct logs share a timestamp the later one is
dropped; acceptable given ns precision.

### Backoff shape

`delay[n] = min(initial × 1.5ⁿ, max)`, no jitter. With defaults:

```
attempt:   1     2     3     4     5     6     7…12
delay ms:  1000  1500  2250  3375  5063  7594  8000 (capped)
```

Total worst-case wait before giving up: ~75 seconds across 12 attempts.

### Framing notes

- WS read limit pinned to **16 MiB** (`logs.go:110`) to cap per-frame memory.
- `awaitConnectionAck` (`logs.go:192`) tolerates stray `ping` frames during
  init so pongs still get sent before the `ack`.
- Server `complete` returns a sentinel (`errStreamComplete`) so the outer loop
  distinguishes "done" from "dropped" and exits 0 without retrying.

---

## 7. Output Format

Each log line becomes one JSON object, newline-terminated:

```json
{
  "timestamp": "2026-04-21T12:30:00.123456789Z",
  "message": "server started",
  "level": "info",
  "addr": ":8080"
}
```

Railway emits attribute values as already-JSON-encoded strings (e.g. the
literal `"\"info\""`). `decodeAttrValue` unwraps one layer of JSON so the
published NDJSON has native types:

- `"\"info\""` → `"info"`
- `"123"` → `123`
- `"true"` → `true`
- unparseable → raw string (lossless fallback)

Writes go through a `sync.Mutex` even though there is currently a single
producer — cheap defense if a future caller adds a second writer.

- `internal/output/ndjson.go:22` — `Writer.Write`
- `internal/output/ndjson.go:56` — `decodeAttrValue`

---

## 8. Concurrency & Lifecycle

- **No goroutines are spawned by app code.** The whole pipeline is one
  blocking loop: dial → read frame → write NDJSON → repeat.
- `signal.NotifyContext` in `main` installs a context that cancels on
  SIGINT/SIGTERM. It propagates into:
  - `http.NewRequestWithContext` for the HTTP query
  - `websocket.Dial` and every `websocket.Read`
- On cancellation the WS read returns, `runStream` returns, the retry loop
  sees `ctx.Err()` and exits cleanly.
- The diagnostic `slog` log goes to a file, never stdout — stdout is reserved
  for NDJSON so piping stays clean.

---

## 9. External Interfaces

### 9.1 GraphQL HTTP (query)

```
POST https://backboard.railway.com/graphql/v2
Headers: <auth header>, Content-Type: application/json
Body:    { "query": "...", "variables": {...} }
```

Used once per run, for `LatestDeployment` when `--deployment` is not supplied.

### 9.2 GraphQL WebSocket (subscription)

- URL: `wss://backboard.railway.com/graphql/v2`
- Subprotocol: **graphql-transport-ws**
- Subscription:

  ```graphql
  subscription DeploymentLogs(
    $deploymentId: String!
    $filter: String
    $limit: Int
  ) {
    deploymentLogs(
      deploymentId: $deploymentId
      filter: $filter
      limit: $limit
    ) {
      timestamp
      message
      attributes {
        key
        value
      }
    }
  }
  ```

Frame types exchanged:

| Direction | Type            | Purpose                         |
| --------- | --------------- | ------------------------------- |
| C → S     | connection_init | start handshake (empty payload) |
| S → C     | connection_ack  | handshake complete              |
| C → S     | subscribe       | open subscription with id="1"   |
| S → C     | next            | log batch                       |
| S → C     | ping            | liveness                        |
| C → S     | pong            | reply to ping                   |
| S → C     | error           | subscription failed → reconnect |
| S → C     | complete        | subscription done → clean exit  |

---

## 10. Dependencies

| Module                       | Version | Purpose                   |
| ---------------------------- | ------- | ------------------------- |
| `github.com/coder/websocket` | v1.8.14 | RFC 6455 WebSocket client |
| `github.com/lwlee2608/adder` | v0.3.2  | Viper-style config loader |
| `gopkg.in/yaml.v3`           | v3.0.1  | YAML parse                |

Everything else — HTTP, JSON, logging, signals — comes from the standard library.

---

## 11. Design Decisions

1. **NDJSON on stdout, diagnostic log on disk.** The whole reason for the tool
   is pipe-friendliness; mixing progress/debug noise into stdout would defeat
   it. `slog` writes to `$XDG_STATE_HOME/railwaylog/railwaylog.log`.

2. **Dedup by timestamp, not id.** Railway's subscription replays from the
   start on every reconnect. Tracking `lastTimestamp` avoids duplicate output
   without needing server-side cursors.

3. **Single-threaded.** A logs CLI is IO-bound on one socket; goroutines would
   add complexity (ordering, shutdown) for no throughput gain.

4. **Config backfill, not migrations.** New config keys land as merges into
   the user's file on next run. Simpler than versioning; keys are additive.

5. **16 MiB WS read cap.** Not unlimited — a malformed upstream frame
   shouldn't be able to OOM the process. 16 MiB is well above any legitimate
   log batch.

6. **Distinguish `complete` from `error`.** Server-initiated `complete` exits
   cleanly (code 0); only transport errors trigger the reconnect loop. This
   keeps `railwaylog | grep …` from looping forever when the server ends the
   subscription deliberately.

7. **Two auth schemes, picked by env.** Project-access tokens (CI) and user
   bearer tokens (local dev) use different headers on Railway's side; the
   client picks the header from `Auth.Kind` rather than forcing the caller to
   know.

---

## 12. File Index

| File                                  | Role                                               |
| ------------------------------------- | -------------------------------------------------- |
| `cmd/railwaylog/main.go`              | `run()`, flag parsing, `resolveLinked()`           |
| `cmd/railwaylog/logger.go`            | slog file handler                                  |
| `internal/config/config.go`           | `Config`, `Load()`, `backfillDefaults()`           |
| `internal/config/default_config.yaml` | embedded defaults                                  |
| `internal/railway/link.go`            | `Auth`, `LinkedProject`, env + config.json sources |
| `internal/api/client.go`              | `Client`, `Query()`, auth header selection         |
| `internal/api/deployment.go`          | `LatestDeployment` query                           |
| `internal/api/logs.go`                | reconnect loop, WS handshake, dedupe               |
| `internal/api/retry.go`               | exponential `Backoff`                              |
| `internal/output/ndjson.go`           | `Writer`, `decodeAttrValue`                        |
