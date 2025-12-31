package main

import (
	"context"
	"log/slog"
	"time"
)

// ============================================================================
// Daemon loop (orchestration)
// ============================================================================
//
// Responsibilities:
// - Receive Events from multiple producers (input, IPC, integrations)
// - Assign timestamps to ingress events via TimedEvent (reducer stays deterministic)
// - Emit Tick at a fixed cadence
// - Reduce events into (next state, Commands)
// - Execute Commands via the effects layer (runEffect) WITHOUT blocking the event loop,
//   and feed observations back as Events
//
// Key design rules:
// - Reduce() is pure: no I/O, no wall clock, no external mutation
// - This loop is the only place that sequences reduction and side effects
// - Side effects are isolated in `effects.go` (runEffect)
//
// Implementation notes:
// - Uses explicit event/command queues to avoid re-entrant execution
// - Commands are executed by a dedicated worker goroutine so CamillaDSP calls never block
//   the event loop
//
// ============================================================================

// runDaemon is the orchestrator:
// - Ingress: receives external Events and wraps them in TimedEvent{At: time.Now()}
// - Scheduling: produces Tick events at a fixed cadence
// - Reduction: calls Reduce() to compute next state + Commands
// - Effects: executes Commands via a worker and feeds resulting observation Events back into Reduce()
//
// Shutdown:
// - Returns when ctx is canceled
// - Returns cleanly when the events channel is closed
func runDaemon(
	ctx context.Context,
	events <-chan Event,
	stateBroadcasts chan<- StateBroadcast,
	client *CamillaDSPClient,
	cfg VelocityConfig,
	rotaryCfg RotaryConfig,
	updateHz int,
	logger *slog.Logger,
) {
	state := &DaemonState{}
	state.VolumeCtrl.TargetDB = safeDefaultDB
	state.VolumeCtrl.LastHeldAt = time.Now()

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
	// - cmdQueue holds commands awaiting execution (staged for dispatch to worker)
	var eventQueue []Event
	var cmdQueue []Command

	// Worker channels:
	// - cmdCh: daemon -> worker
	// - obsCh: worker -> daemon (observation events)
	cmdCh := make(chan Command, 64)
	obsCh := make(chan Event, 64)

	enqueueEvent := func(ev Event) {
		eventQueue = append(eventQueue, ev)
	}
	enqueueCommands := func(cmds []Command) {
		if len(cmds) == 0 {
			return
		}
		cmdQueue = append(cmdQueue, cmds...)
	}

	// Reduce all queued events, enqueuing any resulting commands and publishing any broadcasts.
	flushEvents := func() {
		for len(eventQueue) > 0 {
			ev := eventQueue[0]
			eventQueue = eventQueue[1:]

			rr := Reduce(state, ev, cfg, rotaryCfg)
			if rr.State != nil {
				state = rr.State
			}
			enqueueCommands(rr.Commands)

			// Publish reducer-emitted broadcasts to external consumers (e.g., WebSocket hub).
			// Never block the daemon loop; drop on backpressure, similar to obsCh behavior.
			if stateBroadcasts != nil && len(rr.Broadcasts) > 0 {
				for _, b := range rr.Broadcasts {
					select {
					case stateBroadcasts <- b:
					default:
						logger.Warn("state broadcast queue full, dropping broadcast")
					}
				}
			}
		}
	}

	// Dispatch all queued commands to the worker (non-blocking).
	// If the worker queue is full, keep remaining commands queued for the next flush.
	flushCommands := func() {
		for len(cmdQueue) > 0 {
			cmd := cmdQueue[0]

			select {
			case cmdCh <- cmd:
				cmdQueue = cmdQueue[1:]
			default:
				// Worker is backed up; try again later.
				return
			}
		}
	}

	// Drain any available observation events from the worker without blocking.
	// Observations are reduced promptly to keep state coherent and allow follow-up commands.
	drainObservations := func() {
		for {
			select {
			case obs := <-obsCh:
				enqueueEvent(obs)
			default:
				return
			}
		}
	}

	// Start effects worker.
	// All CamillaDSP I/O happens here; the daemon event loop remains responsive.
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case cmd := <-cmdCh:
				runEffect(client, cmd, logger, func(obs Event) {
					// Avoid blocking the worker indefinitely; if obsCh is full, drop and rely on future
					// polling/commands to converge. This prevents deadlock.
					select {
					case obsCh <- obs:
					default:
						logger.Warn("effects observation queue full, dropping event")
					}
				})
			}
		}
	}()

	// Bootstrap: ask reducer to emit initial CmdGet* commands.
	enqueueEvent(TimedEvent{Event: DaemonStarted{}, At: time.Now()})
	flushEvents()
	flushCommands()

	// Main loop
	for {
		select {
		case <-ctx.Done():
			logger.Info("daemon stopping (context canceled)")
			return

		case obs := <-obsCh:
			// Reduce observations as soon as they arrive.
			enqueueEvent(obs)
			flushEvents()
			flushCommands()

		case ev, ok := <-events:
			if !ok {
				logger.Info("daemon stopping (events channel closed)")
				return
			}
			enqueueEvent(TimedEvent{Event: ev, At: time.Now()})
			flushEvents()
			flushCommands()

		case now := <-ticker.C:
			// Periodic housekeeping:
			// - integrate hold/velocity controller (Tick)
			// - drain any pending observations
			// - dispatch any queued commands
			dt := now.Sub(lastTick).Seconds()
			lastTick = now

			enqueueEvent(Tick{Now: now, Dt: dt})
			drainObservations()
			flushEvents()
			flushCommands()
		}
	}
}
