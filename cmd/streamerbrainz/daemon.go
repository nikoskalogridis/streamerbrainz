package main

import (
	"context"
	"log/slog"
	"time"
)

// ============================================================================
// Central Daemon Loop - Reducer-driven "Daemon Brain"
// ============================================================================
//
// This version wires the reducer into the daemon loop.
//
// Design rules enforced here:
//   - The reducer performs no I/O and computes: next state + commands.
//   - The daemon loop is the only place that executes side effects (CamillaDSP calls).
//   - CamillaDSP responses are turned into Events and fed back into the reducer.
//   - No direct "consume intent" logic or imperative action handling remains here.
//
// Refinements in this version:
//   - Use an explicit event queue and command queue (no nested/re-entrant execution).
//   - Reducer is pure and owns controller state via DaemonState.VolumeCtrl (Option A).
//
// ============================================================================

// runDaemon is the main daemon loop that:
//   - Receives Actions from multiple sources
//   - Emits Tick events on a fixed cadence
//   - Reduces events into (state, commands)
//   - Executes commands against CamillaDSP and feeds observations back into the reducer
//
// Shutdown semantics:
//   - Exits when ctx is canceled
//   - Exits cleanly when the actions channel is closed
func runDaemon(
	ctx context.Context,
	actions <-chan Action,
	client *CamillaDSPClient,
	cfg VelocityConfig,
	state *DaemonState,
	updateHz int,
	logger *slog.Logger,
) {
	// Guard: reducer-driven daemon expects a state container.
	if state == nil {
		state = &DaemonState{}
	}

	// Configure tick cadence.
	updateInterval := time.Second / time.Duration(updateHz)
	ticker := time.NewTicker(updateInterval)
	defer ticker.Stop()

	// Keep dt clamping consistent with the old velocity engine behavior.
	// Allow up to ~2 ticks worth of time to be integrated in one step.
	if updateHz > 0 {
		cfg.MaxDt = 2.0 / float64(updateHz)
	}

	lastTick := time.Now()

	// Explicit queues:
	// - eventQueue holds events awaiting reduction
	// - cmdQueue holds commands awaiting execution
	var eventQueue []Event
	var cmdQueue []Command

	enqueueEvent := func(ev Event) {
		eventQueue = append(eventQueue, ev)
	}
	enqueueCommands := func(cmds []Command) {
		if len(cmds) == 0 {
			return
		}
		cmdQueue = append(cmdQueue, cmds...)
	}

	// Reduce all queued events, enqueuing any resulting commands.
	flushEvents := func() {
		for len(eventQueue) > 0 {
			ev := eventQueue[0]
			eventQueue = eventQueue[1:]

			rr := Reduce(state, ev, cfg)
			if rr.State != nil {
				state = rr.State
			}
			enqueueCommands(rr.Commands)
		}
	}

	// Execute all queued commands, enqueuing observation events.
	flushCommands := func() {
		for len(cmdQueue) > 0 {
			cmd := cmdQueue[0]
			cmdQueue = cmdQueue[1:]

			executeCommand(client, cmd, logger, func(obs Event) {
				enqueueEvent(obs)
			})

			// Observations should be reduced promptly to keep state coherent and
			// allow the reducer to emit follow-up commands (if any).
			flushEvents()
		}
	}

	// Main loop
	for {
		select {
		case <-ctx.Done():
			logger.Info("daemon stopping (context canceled)")
			return

		case act, ok := <-actions:
			if !ok {
				logger.Info("daemon stopping (actions channel closed)")
				return
			}
			enqueueEvent(ActionEvent{Action: act, At: time.Now()})
			flushEvents()
			flushCommands()

		case now := <-ticker.C:
			dt := now.Sub(lastTick).Seconds()
			lastTick = now
			enqueueEvent(Tick{Now: now, Dt: dt})
			flushEvents()
			flushCommands()
		}
	}
}

// executeCommand runs a single reducer-emitted Command against CamillaDSP and emits an observation Event.
// The observation is fed back into the reducer by the caller via the onEvent callback.
func executeCommand(
	client *CamillaDSPClient,
	cmd Command,
	logger *slog.Logger,
	onEvent func(Event),
) {
	if client == nil {
		onEvent(CamillaCommandFailed{
			Command: cmd,
			Err:     errNoClient{},
			At:      time.Now(),
		})
		return
	}

	now := time.Now()

	switch c := cmd.(type) {
	case CmdSetVolume:
		vol, err := client.SetVolume(c.TargetDB)
		if err != nil {
			logger.Error("camilladsp SetVolume failed", "error", err, "target_db", c.TargetDB)
			onEvent(CamillaCommandFailed{Command: cmd, Err: err, At: now})
			return
		}
		onEvent(CamillaVolumeObserved{VolumeDB: vol, At: now})

	case CmdGetVolume:
		vol, err := client.GetVolume()
		if err != nil {
			logger.Error("camilladsp GetVolume failed", "error", err)
			onEvent(CamillaCommandFailed{Command: cmd, Err: err, At: now})
			return
		}
		onEvent(CamillaVolumeObserved{VolumeDB: vol, At: now})

	case CmdToggleMute:
		muted, err := client.ToggleMute()
		if err != nil {
			logger.Error("camilladsp ToggleMute failed", "error", err)
			onEvent(CamillaCommandFailed{Command: cmd, Err: err, At: now})
			return
		}
		onEvent(CamillaMuteObserved{Muted: muted, At: now})

	case CmdSetMute:
		if err := client.SetMute(c.Muted); err != nil {
			logger.Error("camilladsp SetMute failed", "error", err, "muted", c.Muted)
			onEvent(CamillaCommandFailed{Command: cmd, Err: err, At: now})
			return
		}
		// CamillaDSP SetMute doesn't return the value; we know what we set.
		onEvent(CamillaMuteObserved{Muted: c.Muted, At: now})

	case CmdGetMute:
		muted, err := client.GetMute()
		if err != nil {
			logger.Error("camilladsp GetMute failed", "error", err)
			onEvent(CamillaCommandFailed{Command: cmd, Err: err, At: now})
			return
		}
		onEvent(CamillaMuteObserved{Muted: muted, At: now})

	case CmdGetConfigFilePath:
		path, err := client.GetConfigFilePath()
		if err != nil {
			logger.Error("camilladsp GetConfigFilePath failed", "error", err)
			onEvent(CamillaCommandFailed{Command: cmd, Err: err, At: now})
			return
		}
		onEvent(CamillaConfigFilePathObserved{Path: path, At: now})

	case CmdGetState:
		st, err := client.GetState()
		if err != nil {
			logger.Error("camilladsp GetState failed", "error", err)
			onEvent(CamillaCommandFailed{Command: cmd, Err: err, At: now})
			return
		}
		onEvent(CamillaProcessingStateObserved{State: st, At: now})

	default:
		// Unknown command: record failure so reducer can react (if desired).
		logger.Warn("unknown command type", "command", cmd.String())
		onEvent(CamillaCommandFailed{
			Command: cmd,
			Err:     errUnknownCommand{cmd: cmd},
			At:      now,
		})
	}
}

// errNoClient indicates the daemon was asked to execute a command without a CamillaDSP client.
type errNoClient struct{}

func (errNoClient) Error() string { return "no CamillaDSP client" }

type errUnknownCommand struct {
	cmd Command
}

func (e errUnknownCommand) Error() string { return "unknown command: " + e.cmd.String() }
