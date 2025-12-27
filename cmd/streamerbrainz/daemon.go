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
//   - Receives Events from multiple sources
//   - Emits Tick events on a fixed cadence
//   - Reduces events into (state, commands)
//   - Executes commands against CamillaDSP and feeds observations back into the reducer
//
// Shutdown semantics:
//   - Exits when ctx is canceled
//   - Exits cleanly when the actions channel is closed
func runDaemon(
	ctx context.Context,
	events <-chan Event,
	client *CamillaDSPClient,
	cfg VelocityConfig,
	rotaryCfg RotaryConfig,
	state *DaemonState,
	updateHz int,
	logger *slog.Logger,
) {
	// Guard: reducer-driven daemon expects a state container.
	if state == nil {
		logger.Error("daemon state is nil")
		return
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

			rr := Reduce(state, ev, cfg, rotaryCfg)
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

			runEffect(client, cmd, logger, func(obs Event) {
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

		case act, ok := <-events:
			if !ok {
				logger.Info("daemon stopping (events channel closed)")
				return
			}
			enqueueEvent(TimedEvent{Event: act, At: time.Now()})
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

// NOTE:
// Command execution is implemented in `effects.go` as `runEffect(...)`.
// This file is only responsible for orchestrating event/command queues and reducer invocation.
