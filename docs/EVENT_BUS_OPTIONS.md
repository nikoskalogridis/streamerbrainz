# Event bus options for StreamerBrainz daemon events

## Goal

Expose the daemon’s internal stream of `Action`/state events on a *public bus* so external clients can:

- subscribe to specific event types (or all events and filter client-side),
- receive events in (near) real time,
- optionally reconnect and catch up (depending on transport).

This is **outbound telemetry/events**, distinct from the existing **inbound control plane** (the Unix domain socket IPC that accepts actions).

## What the program looks like today (relevant bits)

- There is a **central daemon loop** (`runDaemon`) that serializes policy/state updates and is the natural place to emit events.
- There is already an **IPC server** (Unix domain socket, line-delimited JSON) for clients to send actions *to* the daemon (control).
- There are multiple action sources (IR/input readers, librespot hook, Plex integration), all funneled into a single `actions` channel.

Implication: implementing an event bus is mainly about adding an **event sink** next to the daemon loop (a “fan-out”) without disturbing the single-writer state model.

## Recommended event model (independent of transport)

Define a small set of outbound event types. Keep them stable and versioned.

Minimum useful categories:

- **Input / intent events**: e.g., `VolumeHeld`, `VolumeStep`, `ToggleMute`, `PlexStateChanged`, librespot session/playback/volume changes.
- **State snapshots** (optional): e.g., `VolumeTargetChanged`, `VolumeApplied` (ack from CamillaDSP), `MuteStateChanged`.
- **Health**: errors from CamillaDSP operations, reconnects, dropped events, queue saturation.

General advice:

- Prefer **one JSON envelope** with metadata:
  - `ts`, `type`, `payload`, maybe `seq` (monotonic sequence) and `source`.
- Make delivery semantics explicit:
  - “best-effort live stream” vs “durable with replay”.

## Option A — Built-in WebSocket server (daemon hosts it)

### What it is
The daemon exposes a WebSocket endpoint (e.g. `/events`). Clients connect and receive a JSON stream.

### Pros
- **No external dependency**: single binary; easiest deployment on embedded boxes.
- **Great for UIs**: browsers and local dashboards connect naturally.
- **Low latency** and simple mental model (“tail -f over a socket”).
- You can implement **server-side filtering** cheaply (subscribe message: `{types:[...]}`).

### Cons
- **No durability by default**: reconnect means you miss events unless you add buffering/replay.
- **Fan-out load is on the daemon**: many clients = more memory and write pressure.
- Requires careful handling of:
  - slow clients (backpressure, drop policy),
  - per-client queues,
  - authentication/authorization if exposed beyond localhost.

### Fit for StreamerBrainz
Very good if the primary consumers are **local apps / dashboards** and you’re OK with **best-effort streaming**.

## Option B — MQTT (publish to a broker)

### What it is
Daemon publishes events to an MQTT broker under topics like:

- `streamerbrainz/events/#`
- `streamerbrainz/events/volume/applied`
- `streamerbrainz/events/plex/state`

Clients subscribe via standard MQTT tooling.

### Pros
- **Decouples producers and consumers**: broker handles fan-out.
- Supports **topic-based filtering natively** (no custom protocol required).
- Can get **stronger delivery semantics** (QoS 1/2), retained messages, persistent sessions.
- Good ecosystem: Home Assistant, Node-RED, Grafana/Telegraf, etc.

### Cons
- **Requires running a broker** (Mosquitto, EMQX, etc): more operational moving parts.
- More configuration/security surface area (TLS, credentials, ACLs).
- You still must decide event schema/versioning; MQTT only moves bytes.
- Not browser-native without bridges (WebSocket MQTT exists but adds complexity).

### Fit for StreamerBrainz
Best when you expect **multiple heterogeneous clients**, want **topic filtering**, and already run (or are willing to run) an MQTT broker on the network.

## Option C — Extend existing IPC (add “subscribe” mode)

### What it is
Reuse the existing Unix domain socket server, but allow clients to connect and request a streaming mode (server pushes events back). This stays **local-machine only** unless you add TCP.

### Pros
- **Minimal new surface area**: same transport, same framing (line-delimited JSON).
- Very low overhead and simple for local integrations/scripts.
- No extra dependencies.

### Cons
- Unix socket is **not “public”** in the network sense; remote clients need SSH tunneling or an additional bridge.
- You must implement fan-out/backpressure similarly to WebSockets.
- Harder to integrate with browser UIs.

### Fit for StreamerBrainz
Great for **local-only** consumers and CLI tooling. If “public bus” means “network-accessible”, this likely needs a bridge anyway.

## Option D — Server-Sent Events (SSE) over HTTP

### What it is
Daemon serves an HTTP endpoint that streams events via SSE.

### Pros
- Simpler than WebSockets (one-way push).
- Browser-friendly and easy to consume.
- Works well for “event feed” semantics.

### Cons
- Still no durability by default.
- Not bidirectional; for subscriptions/filters you either use query params or separate endpoints.
- Similar per-client backpressure concerns.

### Fit for StreamerBrainz
A nice middle ground if you only need **daemon → client** streaming and want easy browser integration.

## Option E — NATS / Redis PubSub / ZeroMQ (brief)

These are viable, but generally less aligned with “keep it simple” unless you already run them.

- **NATS**: excellent pub/sub, simple clients, optional JetStream for persistence; requires server.
- **Redis PubSub/Streams**: good if Redis already exists; Streams give replay; extra dependency.
- **ZeroMQ**: no broker, very fast; but you’re designing more of the system yourself (discovery, durability).

## Recommendation (short)

### If you want simplest “works out of the box”
Implement a **built-in WebSocket (or SSE) event stream** in the daemon.

- Use it for **live telemetry** and local UI/debugging.
- Add a small bounded per-client buffer and a drop policy (e.g., drop oldest, and emit a `DroppedEvents` notification).

### If you want a true “public bus” with many clients and topic subscriptions
Publish to **MQTT**.

- Treat MQTT as the distribution layer.
- Keep the daemon implementation minimal: publish events, don’t manage many client connections.
- Use topics for server-side filtering and QoS if you need reliability.

### Hybrid (often best)
Do both:

- **WebSocket/SSE** for local UI and debugging.
- **MQTT publisher** as an optional module enabled by config for network-wide integrations.

## Implementation notes (to keep it safe)

- Keep the daemon loop single-writer: emit events by sending to a non-blocking internal `events` channel.
- Never block `runDaemon` on a slow subscriber; add buffering and/or drop.
- Make schema explicit and versioned (`schema_version` field).
- Default bind addresses conservatively (localhost) unless explicitly configured.
- If exposing over the network, require auth (token) and support TLS (or recommend reverse proxy).
