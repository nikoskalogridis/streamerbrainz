package main

import (
	"math"
	"time"
)

// This file implements the reducer-style architecture building blocks:
//
//   - Events: inputs to the reducer (user actions, time ticks, CamillaDSP observations, command failures)
//   - Commands: side effects requested by the reducer (CamillaDSP websocket commands)
//   - Reduce(): computes next state + commands, without performing I/O
//
// IMPORTANT (Option A):
// The reducer must be pure. It must NOT mutate external controller objects.
// All controller state is embedded in DaemonState (DaemonState.VolumeCtrl), and the velocity
// integration is performed via the pure StepVolumeController function.
//
// The daemon loop is responsible for executing Commands and feeding observations back as Events.

// ==============================
// Events
// ==============================

// Event is the input to the reducer.
type Event interface {
	eventMarker()
}

// TimedEvent wraps any Event with a timestamp assigned by the daemon loop.
// This keeps payload types clean and makes reducer time-handling deterministic.
type TimedEvent struct {
	Event Event
	At    time.Time
}

func (TimedEvent) eventMarker() {}

// Tick is emitted by the daemon loop at a fixed cadence.
// Dt is wall-clock delta in seconds between ticks.
type Tick struct {
	Now time.Time
	Dt  float64
}

func (Tick) eventMarker() {}

// NOTE: We intentionally do not have a separate ActionEvent type.
// User intents are Events directly (e.g. VolumeHeld, RotaryTurn, ToggleMute, ...),
// and timing is carried by TimedEvent.

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
// Reducer input/output
// ==============================

// ReduceResult is the output of Reduce(): next state plus a set of Commands to execute.
//
// Commands are expected to be coalesced by the reducer where appropriate.
// For example, multiple desired-volume updates in one tick should typically result
// in a single CmdSetVolume with the latest desired value.
type ReduceResult struct {
	State    *DaemonState
	Commands []Command
}

// Reduce is the pure reducer:
//
// Rules:
// - Must not perform I/O
// - Must not block
// - Must not mutate anything outside the returned state (including no mutation of external controllers)
//
// The daemon loop must:
// - execute Commands
// - translate responses into Events
// - feed those Events back into Reduce()
func Reduce(s *DaemonState, e Event, cfg VelocityConfig, rotaryCfg RotaryConfig) ReduceResult {
	if s == nil {
		s = &DaemonState{}
	}

	// Unwrap timing if present. The reducer must never consult wall clock.
	var at time.Time
	if te, ok := e.(TimedEvent); ok {
		at = te.At
		e = te.Event
	}

	var cmds []Command

	switch ev := e.(type) {
	case Tick:
		// Tick drives hold integration and flushes intents into Commands.

		// Establish baseline for integration (authoritative desired for this cycle):
		//  - desired volume intent if present
		//  - else observed volume if known (CamillaDSP authoritative observed)
		//  - else controller target (fallback)
		baseline := s.VolumeCtrl.TargetDB
		if s.Camilla.VolumeKnown {
			baseline = s.Camilla.VolumeDB
		}
		if s.Intent.DesiredVolumeDB != nil {
			baseline = *s.Intent.DesiredVolumeDB
		}

		// Always advance controller so hold-timeout and decay behavior run consistently.
		nextCtrl := StepVolumeController(s.VolumeCtrl, baseline, ev.Dt, ev.Now, cfg)
		s.VolumeCtrl = nextCtrl
		if nextCtrl.HeldDirection != 0 {
			s.SetDesiredVolume(nextCtrl.TargetDB)
		}

		// Flush intents into commands (coalesced latest-wins).
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
			// Observed state is authoritative (CamillaDSP), so we threshold against it when known.
			//
			// If volume is not known, emit the command so we converge quickly.
			if !s.Camilla.VolumeKnown || math.Abs(v-s.Camilla.VolumeDB) >= volumeUpdateThresholdDB {
				cmds = append(cmds, CmdSetVolume{TargetDB: v})
			}
		}

	case RotaryTurn:
		// Rotary cancels holds and any ongoing controller motion.
		s.VolumeCtrl.HeldDirection = 0
		s.VolumeCtrl.VelocityDBPerS = 0
		s.VolumeCtrl.HoldBeganAt = time.Time{}

		steps := ev.Steps
		if steps == 0 {
			break
		}

		// Track recent rotary steps in reducer-owned state for velocity detection.
		// Reducer is deterministic: timestamps must be provided by the daemon via TimedEvent.
		now := at
		if now.IsZero() {
			// No timestamp => cannot do windowed velocity detection deterministically.
			break
		}

		direction := 1
		if steps < 0 {
			direction = -1
		}

		// Prune old samples outside the velocity window.
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

		// Determine effective step size with optional velocity multiplier.
		dbPerStep := rotaryCfg.DbPerStep
		if dbPerStep == 0 {
			dbPerStep = defaultRotaryDbPerStep
		}
		if recentCount >= rotaryCfg.VelocityThreshold {
			dbPerStep *= rotaryCfg.VelocityMultiplier
		}

		// Apply step to baseline (desired > observed > controller target).
		current := s.VolumeCtrl.TargetDB
		if s.Camilla.VolumeKnown {
			current = s.Camilla.VolumeDB
		}
		if s.Intent.DesiredVolumeDB != nil {
			current = *s.Intent.DesiredVolumeDB
		}

		deltaDB := float64(steps) * dbPerStep
		next := current + deltaDB
		if next < cfg.MinDB {
			next = cfg.MinDB
		}
		if next > cfg.MaxDB {
			next = cfg.MaxDB
		}

		s.SetDesiredVolume(next)
		s.VolumeCtrl.TargetDB = next

	case VolumeStep:
		// Backward compatibility / IPC: treat VolumeStep as an explicit step-based volume delta.
		// This bypasses reducer-side velocity detection (callers can set DbPerStep explicitly).
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

		next := current + deltaDB
		if next < cfg.MinDB {
			next = cfg.MinDB
		}
		if next > cfg.MaxDB {
			next = cfg.MaxDB
		}

		s.SetDesiredVolume(next)
		s.VolumeCtrl.TargetDB = next

	case VolumeHeld:
		// Start or update hold gesture.
		// Reducer is deterministic: timestamps must be provided by the daemon via TimedEvent.
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

		next := ev.Db
		if next < cfg.MinDB {
			next = cfg.MinDB
		}
		if next > cfg.MaxDB {
			next = cfg.MaxDB
		}

		s.SetDesiredVolume(next)
		s.VolumeCtrl.TargetDB = next

	default:
		// no-op

	case CamillaVolumeObserved:
		s.SetObservedVolume(ev.VolumeDB, ev.At)

		// Keep controller position aligned with observed volume ONLY if we are not currently holding.
		// If a hold is active, we keep controller dynamics as-is and baseline integration uses desired/observed
		// depending on which is present.
		//
		// IMPORTANT: Do NOT kill inertia by zeroing velocity immediately on observations after release.
		// Only snap/zero when the controller is already nearly stopped.
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
		s.SetObservedMute(ev.Muted, ev.At)

	case CamillaConfigFilePathObserved:
		s.SetObservedConfigFilePath(ev.Path, ev.At)

	case CamillaProcessingStateObserved:
		s.SetObservedProcessingState(ev.State, ev.At)

	case CamillaCommandFailed:
		// Keep state as-is. Future work could add backoff/retry/disconnected state.
		_ = ev
	}

	return ReduceResult{
		State:    s,
		Commands: cmds,
	}
}
