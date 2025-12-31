package main

import (
	"math"
	"time"
)

// This file contains the pure reducer:
// - it consumes Events
// - it updates DaemonState
// - it emits Commands (side effects) for the daemon loop to execute
//
// Design rules:
// - No I/O, no blocking, no wall-clock access
// - No mutation outside the returned state
//
// Notes on time:
// - Payload events (e.g. VolumeHeld, RotaryTurn) are timestamped by the daemon via TimedEvent.
// - Tick carries its own timing (Now/Dt).
// - Observation events carry their own At timestamp (set in the effects layer).

// ==============================
// Reducer inputs (Events)
// ==============================

// Event is the input to the reducer.
type Event interface {
	eventMarker()
}

// TimedEvent decorates an Event with a timestamp assigned by the daemon loop.
// Use this for payload events where timing matters (holds, rotary velocity windowing, etc.).
type TimedEvent struct {
	Event Event
	At    time.Time
}

func (TimedEvent) eventMarker() {}

// DaemonStarted is emitted once by the daemon loop at startup.
// The reducer can use this to request initial observations (CmdGetVolume, CmdGetMute, etc.).
type DaemonStarted struct{}

func (DaemonStarted) eventMarker() {}

// Tick is emitted by the daemon loop at a fixed cadence.
// Dt is the wall-clock delta in seconds between ticks.
type Tick struct {
	Now time.Time
	Dt  float64
}

func (Tick) eventMarker() {}

// CamillaVolumeObserved is emitted after a successful GetVolume/SetVolume (or any API returning volume).
type CamillaVolumeObserved struct {
	VolumeDB float64
	At       time.Time
}

func (CamillaVolumeObserved) eventMarker() {}

// CamillaMuteObserved is emitted after a successful GetMute/SetMute/ToggleMute.
type CamillaMuteObserved struct {
	Muted bool
	At    time.Time
}

func (CamillaMuteObserved) eventMarker() {}

// CamillaConfigFilePathObserved is emitted after a successful GetConfigFilePath.
type CamillaConfigFilePathObserved struct {
	Path string
	At   time.Time
}

func (CamillaConfigFilePathObserved) eventMarker() {}

// CamillaProcessingStateObserved is emitted after a successful GetState.
type CamillaProcessingStateObserved struct {
	State string
	At    time.Time
}

func (CamillaProcessingStateObserved) eventMarker() {}

// CamillaCommandFailed is emitted when executing a Command fails.
type CamillaCommandFailed struct {
	Command Command
	Err     error
	At      time.Time
}

func (CamillaCommandFailed) eventMarker() {}

// ==============================
// Reducer helpers
// ==============================

func clampVolumeDB(v float64, cfg VelocityConfig) float64 {
	if v < cfg.MinDB {
		return cfg.MinDB
	}
	if v > cfg.MaxDB {
		return cfg.MaxDB
	}
	return v
}

// ==============================
// Reducer output
// ==============================

// StateSnapshot is the externally-consumable snapshot of daemon state.
// It is intentionally a copy/DTO (never a pointer to DaemonState) so external
// consumers can't mutate daemon-owned state.
//
// NOTE: We'll extend this over time (e.g., players status) without exposing the
// full internal DaemonState structure.
type StateSnapshot struct {
	// CamillaDSP (authoritative backend for volume/mute)
	VolumeDB    float64   `json:"volume_db"`
	VolumeKnown bool      `json:"volume_known"`
	VolumeAt    time.Time `json:"volume_at"`

	Muted     bool      `json:"muted"`
	MuteKnown bool      `json:"mute_known"`
	MuteAt    time.Time `json:"mute_at"`
}

// StateBroadcast is a reducer-emitted broadcast event intended for external consumers
// (e.g. WebSocket clients). This is separate from reducer input Events.
type StateBroadcast interface {
	stateBroadcastMarker()
}

// BroadcastVolumeChanged is emitted when observed volume changes.
type BroadcastVolumeChanged struct {
	VolumeDB float64   `json:"volume_db"`
	At       time.Time `json:"at"`
}

func (BroadcastVolumeChanged) stateBroadcastMarker() {}

// BroadcastMuteChanged is emitted when observed mute changes.
type BroadcastMuteChanged struct {
	Muted bool      `json:"muted"`
	At    time.Time `json:"at"`
}

func (BroadcastMuteChanged) stateBroadcastMarker() {}

// RequestStateSnapshot asks the reducer to produce a snapshot for an external consumer.
// The reply channel is carried through a Command so delivery happens in the effects layer
// (no side effects in the reducer).
type RequestStateSnapshot struct {
	Reply chan<- StateSnapshot
}

func (RequestStateSnapshot) eventMarker() {}

// ReduceResult is the output of Reduce(): the next state plus Commands to execute,
// plus broadcast messages to publish externally.
//
// Commands are expected to be coalesced where appropriate (latest-wins).
type ReduceResult struct {
	State      *DaemonState
	Commands   []Command
	Broadcasts []StateBroadcast
}

// Reduce is the pure reducer.
// It computes the next state and a list of Commands for the daemon loop to execute.
func Reduce(s *DaemonState, e Event, cfg VelocityConfig, rotaryCfg RotaryConfig) ReduceResult {
	if s == nil {
		s = &DaemonState{}
	}

	// Unwrap timing if present. The reducer never consults wall clock.
	var at time.Time
	if te, ok := e.(TimedEvent); ok {
		at = te.At
		e = te.Event
	}

	var cmds []Command
	var broadcasts []StateBroadcast

	switch ev := e.(type) {
	case DaemonStarted:
		// Bootstrap: request initial observed state from CamillaDSP.
		// These will come back as Camilla*Observed events from the effects layer.
		cmds = append(cmds,
			CmdGetVolume{},
			CmdGetMute{},
			CmdGetConfigFilePath{},
			CmdGetState{},
		)

	case Tick:
		// Tick advances the hold/velocity controller and flushes intents into Commands.

		// Baseline for integration (highest priority wins):
		//  1) current desired intent (if any)
		//  2) observed CamillaDSP volume (if known)
		//  3) controller target (fallback)
		baseline := s.VolumeCtrl.TargetDB
		if s.Camilla.VolumeKnown {
			baseline = s.Camilla.VolumeDB
		}
		if s.Intent.DesiredVolumeDB != nil {
			baseline = *s.Intent.DesiredVolumeDB
		}

		// Always advance controller so hold-timeout and decay run consistently.
		nextCtrl := StepVolumeController(s.VolumeCtrl, baseline, ev.Dt, ev.Now, cfg)
		s.VolumeCtrl = nextCtrl
		if nextCtrl.HeldDirection != 0 {
			s.SetDesiredVolume(nextCtrl.TargetDB)
		}

		// Flush intents into Commands (coalesced latest-wins).
		if s.Intent.MuteTogglePending {
			s.Intent.MuteTogglePending = false
			cmds = append(cmds, CmdToggleMute{})
		}
		if s.Intent.DesiredMute != nil {
			m := *s.Intent.DesiredMute
			s.Intent.DesiredMute = nil
			cmds = append(cmds, CmdSetMute{Muted: m})
		}
		if s.Intent.DesiredVolumeDB != nil {
			v := *s.Intent.DesiredVolumeDB
			s.Intent.DesiredVolumeDB = nil

			// Policy: avoid unnecessary SetVolume commands when we're already close to observed state.
			// Observed state is authoritative (CamillaDSP), so threshold against it when known.
			// If volume is unknown, emit the command so we converge quickly.
			if !s.Camilla.VolumeKnown || math.Abs(v-s.Camilla.VolumeDB) >= volumeUpdateThresholdDB {
				cmds = append(cmds, CmdSetVolume{TargetDB: v})
			}
		}

	case RotaryTurn:
		// Rotary input cancels holds and any ongoing controller motion.
		s.VolumeCtrl.HeldDirection = 0
		s.VolumeCtrl.VelocityDBPerS = 0
		s.VolumeCtrl.HoldBeganAt = time.Time{}

		steps := ev.Steps
		if steps == 0 {
			break
		}

		// Track recent rotary steps in reducer-owned state for velocity detection.
		// Requires a timestamp from TimedEvent (assigned by the daemon).
		now := at
		if now.IsZero() {
			// Without a timestamp we can't do windowed velocity detection deterministically.
			break
		}

		direction := 1
		if steps < 0 {
			direction = -1
		}

		// Prune samples outside the velocity window.
		windowMS := rotaryCfg.VelocityWindowMS
		cutoff := now.Add(-time.Duration(windowMS) * time.Millisecond)
		kept := s.Rotary.RecentSteps[:0]
		for _, st := range s.Rotary.RecentSteps {
			if st.At.After(cutoff) {
				kept = append(kept, st)
			}
		}
		s.Rotary.RecentSteps = kept

		// Add each detent as a separate sample so velocity detection is consistent.
		stepsAbs := steps
		if stepsAbs < 0 {
			stepsAbs = -stepsAbs
		}
		for i := 0; i < stepsAbs; i++ {
			s.Rotary.RecentSteps = append(s.Rotary.RecentSteps, RotaryReducerStep{
				At:        now,
				Direction: direction,
			})
		}

		// Count recent steps in the same direction (including the new ones we just appended).
		recentCount := 0
		for i := len(s.Rotary.RecentSteps) - 1; i >= 0; i-- {
			if s.Rotary.RecentSteps[i].Direction != direction {
				continue
			}
			if !s.Rotary.RecentSteps[i].At.After(cutoff) {
				continue
			}
			recentCount++
		}

		// Determine effective step size, with optional velocity multiplier.
		dbPerStep := rotaryCfg.DbPerStep
		if dbPerStep == 0 {
			dbPerStep = defaultRotaryDbPerStep
		}
		if recentCount >= rotaryCfg.VelocityThreshold {
			dbPerStep *= rotaryCfg.VelocityMultiplier
		}

		// Apply step against baseline (desired > observed > controller target).
		current := s.VolumeCtrl.TargetDB
		if s.Camilla.VolumeKnown {
			current = s.Camilla.VolumeDB
		}
		if s.Intent.DesiredVolumeDB != nil {
			current = *s.Intent.DesiredVolumeDB
		}

		deltaDB := float64(steps) * dbPerStep
		next := clampVolumeDB(current+deltaDB, cfg)

		s.SetDesiredVolume(next)
		s.VolumeCtrl.TargetDB = next

	case VolumeStep:
		// Explicit step delta (bypasses reducer-side velocity detection).
		// Primarily for IPC/back-compat; callers may set DbPerStep explicitly.
		s.VolumeCtrl.HeldDirection = 0
		s.VolumeCtrl.VelocityDBPerS = 0
		s.VolumeCtrl.HoldBeganAt = time.Time{}

		dbPerStep := ev.DbPerStep
		if dbPerStep == 0 {
			dbPerStep = defaultRotaryDbPerStep
		}
		deltaDB := float64(ev.Steps) * dbPerStep

		current := s.VolumeCtrl.TargetDB
		if s.Camilla.VolumeKnown {
			current = s.Camilla.VolumeDB
		}
		if s.Intent.DesiredVolumeDB != nil {
			current = *s.Intent.DesiredVolumeDB
		}

		next := clampVolumeDB(current+deltaDB, cfg)

		s.SetDesiredVolume(next)
		s.VolumeCtrl.TargetDB = next

	case VolumeHeld:
		// Start or update a hold gesture.
		// Requires a timestamp from TimedEvent (assigned by the daemon).
		now := at
		if now.IsZero() {
			break
		}

		// New gesture if transitioning from not-held to held, or reversing direction.
		if s.VolumeCtrl.HeldDirection == 0 || (ev.Direction != 0 && ev.Direction != s.VolumeCtrl.HeldDirection) {
			s.VolumeCtrl.HoldBeganAt = now
			// Reset velocity on direction change to keep response snappy.
			if ev.Direction != s.VolumeCtrl.HeldDirection {
				s.VolumeCtrl.VelocityDBPerS = 0
			}
		}

		s.VolumeCtrl.HeldDirection = ev.Direction
		s.VolumeCtrl.LastHeldAt = now

	case VolumeRelease:
		s.VolumeCtrl.HeldDirection = 0
		s.VolumeCtrl.HoldBeganAt = time.Time{}

	case ToggleMute:
		s.RequestToggleMute()

	case SetVolumeAbsolute:
		// Absolute set cancels holds/motion.
		s.VolumeCtrl.HeldDirection = 0
		s.VolumeCtrl.VelocityDBPerS = 0
		s.VolumeCtrl.HoldBeganAt = time.Time{}

		next := clampVolumeDB(ev.Db, cfg)

		s.SetDesiredVolume(next)
		s.VolumeCtrl.TargetDB = next

	case RequestStateSnapshot:
		// Build a DTO snapshot from daemon-owned state (safe copy; no pointers exposed).
		// Delivery to the requester happens via a Command (effects layer), keeping the reducer pure.
		snap := StateSnapshot{
			VolumeDB:    s.Camilla.VolumeDB,
			VolumeKnown: s.Camilla.VolumeKnown,
			VolumeAt:    s.Camilla.VolumeAt,
			Muted:       s.Camilla.Muted,
			MuteKnown:   s.Camilla.MuteKnown,
			MuteAt:      s.Camilla.MuteAt,
		}
		cmds = append(cmds, CmdPublishStateSnapshot{
			Snapshot: snap,
			Reply:    ev.Reply,
		})

	default:
		// No-op for unhandled event types (e.g. media controls not wired yet).

	case CamillaVolumeObserved:
		prevKnown := s.Camilla.VolumeKnown
		prevVolRounded := math.Round(s.Camilla.VolumeDB*10.0) / 10.0

		// Store observed volume at full precision (daemon-owned truth).
		// Round only for external broadcast emission to avoid noisy float jitter and reduce spam.
		volRounded := math.Round(ev.VolumeDB*10.0) / 10.0

		s.SetObservedVolume(ev.VolumeDB, ev.At)

		// Broadcast only when the rounded observed value changes (or becomes known).
		// NOTE: Payload uses the rounded value (0.1 dB), while internal state remains full precision.
		if !prevKnown || prevVolRounded != volRounded {
			broadcasts = append(broadcasts, BroadcastVolumeChanged{
				VolumeDB: volRounded,
				At:       ev.At,
			})
		}

		// Keep controller position aligned with observed volume only if we are not currently holding.
		// If a hold is active, preserve controller dynamics (inertia/decay) and let Tick integration
		// choose baseline from desired/observed as appropriate.
		if s.VolumeCtrl.HeldDirection == 0 {
			s.VolumeCtrl.TargetDB = ev.VolumeDB

			// If we're effectively stopped, snap velocity to 0 to avoid tiny drift.
			// Otherwise preserve VelocityDBPerS so accelerating-mode decay produces inertia naturally.
			const velEps = 0.01 // dB/s
			if s.VolumeCtrl.VelocityDBPerS < velEps && s.VolumeCtrl.VelocityDBPerS > -velEps {
				s.VolumeCtrl.VelocityDBPerS = 0
			}
		}

	case CamillaMuteObserved:
		prevKnown := s.Camilla.MuteKnown
		prevMuted := s.Camilla.Muted

		s.SetObservedMute(ev.Muted, ev.At)

		// Broadcast only on meaningful observed change.
		if !prevKnown || prevMuted != ev.Muted {
			broadcasts = append(broadcasts, BroadcastMuteChanged{
				Muted: ev.Muted,
				At:    ev.At,
			})
		}

	case CamillaConfigFilePathObserved:
		s.SetObservedConfigFilePath(ev.Path, ev.At)

	case CamillaProcessingStateObserved:
		s.SetObservedProcessingState(ev.State, ev.At)

	case CamillaCommandFailed:
		// Keep state as-is. Future work could add backoff/retry/disconnected state.
		_ = ev
	}

	return ReduceResult{
		State:      s,
		Commands:   cmds,
		Broadcasts: broadcasts,
	}
}
