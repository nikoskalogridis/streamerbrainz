# Development

This page is for contributors working on StreamerBrainz (the daemon).

## Repository layout

- `cmd/streamerbrainz/` — main daemon binary (also contains the `librespot-hook` subcommand)
- `docs/` — documentation
- `examples/` — runnable examples (e.g. systemd user service)
- `bin/` — build output (created by `make`)

## Building

Use the Makefile if available in your environment:

- `make` — build binaries into `bin/`
- `make clean` — remove build output

Or build directly with Go (paths are important because code lives under `cmd/`):

- `go build -o bin/streamerbrainz ./cmd/streamerbrainz`

## Testing

Run unit tests:

- `go test ./...`

## Design notes (what to preserve)

- **Action-based design:** inputs (IR, librespot hook, Plex webhook, etc.) are translated into typed `Action` values.
- **Single “daemon brain”:** only the daemon loop goroutine owns the mutable velocity state and talks to CamillaDSP. This is intentional to avoid races as new inputs are added.
- **IPC is an internal implementation detail:** the Unix socket IPC is used by the librespot integration (the event hook) to forward events into the daemon. It is not a public API.

## Adding a new Action type

Action encoding/decoding and the daemon loop live under `cmd/streamerbrainz/`.

### 1) Define the action struct

Add a struct in `cmd/streamerbrainz/actions.go`:

```go
type MyAction struct {
    Value string `json:"value"`
}
```

### 2) Register it in JSON encoding/decoding

In `cmd/streamerbrainz/actions.go`:

- Add a case to `UnmarshalAction()` for your `"my_action"` discriminator
- Add a case to `MarshalAction()` for `MyAction`

### 3) Handle it in the daemon loop

In `cmd/streamerbrainz/daemon.go`, add handling in `handleAction(...)`:

```go
case MyAction:
    // Handle action (update state and/or call CamillaDSP client as appropriate)
```

### Notes

- Prefer keeping **all CamillaDSP mutations** centralized in the daemon brain path. If your action must call CamillaDSP immediately (like mute toggling currently does), document why and consider whether it should be unified with the “apply” path later.

## Logging / troubleshooting during development

- The daemon supports a `-log-level` flag (e.g. `debug`) which is useful to confirm flag parsing and runtime configuration.
- For systemd `--user` units, see `examples/streamerbrainz.service` for the recommended log command.