package main

import (
	"fmt"
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
// It can be a user Action, a Tick, or a response/error from CamillaDSP.
type Event interface {
	eventMarker()
}

// Tick is emitted by the daemon loop at a fixed cadence.
// Dt is wall-clock delta in seconds between ticks.
type Tick struct {
	Now time.Time
	Dt  float64
}

func (Tick) eventMarker() {}

// ActionEvent wraps an existing Action so it can be used as an Event.
type ActionEvent struct {
	Action Action
	At     time.Time
}

func (ActionEvent) eventMarker() {}

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
// Commands (side effects)
// ==============================

// Command represents an external side effect to be executed by the daemon loop.
// In this codebase, those are primarily CamillaDSP websocket commands.
type Command interface {
	commandMarker()
	String() string
}

// CmdSetVolume requests setting volume in CamillaDSP (Main fader volume).
type CmdSetVolume struct {
	TargetDB float64
}

func (CmdSetVolume) commandMarker() {}
func (c CmdSetVolume) String() string {
	return fmt.Sprintf("CmdSetVolume(target_db=%.3f)", c.TargetDB)
}

// CmdToggleMute toggles mute in CamillaDSP (Main).
type CmdToggleMute struct{}

func (CmdToggleMute) commandMarker() {}
func (CmdToggleMute) String() string { return "CmdToggleMute()" }

// CmdSetMute sets mute explicitly in CamillaDSP (Main).
type CmdSetMute struct {
	Muted bool
}

func (CmdSetMute) commandMarker()   {}
func (c CmdSetMute) String() string { return fmt.Sprintf("CmdSetMute(muted=%v)", c.Muted) }

// CmdGetVolume requests current volume from CamillaDSP.
type CmdGetVolume struct{}

func (CmdGetVolume) commandMarker() {}
func (CmdGetVolume) String() string { return "CmdGetVolume()" }

// CmdGetMute requests current mute from CamillaDSP.
type CmdGetMute struct{}

func (CmdGetMute) commandMarker() {}
func (CmdGetMute) String() string { return "CmdGetMute()" }

// CmdGetConfigFilePath requests current config file path from CamillaDSP.
type CmdGetConfigFilePath struct{}

func (CmdGetConfigFilePath) commandMarker() {}
func (CmdGetConfigFilePath) String() string { return "CmdGetConfigFilePath()" }

// CmdGetState requests current processing state from CamillaDSP.
type CmdGetState struct{}

func (CmdGetState) commandMarker() {}
func (CmdGetState) String() string { return "CmdGetState()" }

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
func Reduce(s *DaemonState, e Event, cfg VelocityConfig) ReduceResult {
	if s == nil {
		s = &DaemonState{}
	}

	var cmds []Command

	switch ev := e.(type) {
	case Tick:
		// Tick drives hold integration and flushes intents into Commands.

		// Establish baseline for integration:
		//  - desired volume intent if present (authoritative desired for this cycle)
		//  - else observed volume if known (CamillaDSP authoritative observed)
		//  - else controller target (fallback)
		baseline := s.VolumeCtrl.TargetDB
		if s.Camilla.VolumeKnown {
			baseline = s.Camilla.VolumeDB
		}
		if s.Intent.DesiredVolumeDB != nil {
			baseline = *s.Intent.DesiredVolumeDB
		}

		// Integrate controller if held; if not held, still allow timeout logic to run
		// (StepVolumeController includes hold-timeout handling).
		if s.VolumeCtrl.HeldDirection != 0 {
			nextCtrl, nextTarget := StepVolumeController(s.VolumeCtrl, baseline, ev.Dt, ev.Now, cfg)
			s.VolumeCtrl = nextCtrl
			s.SetDesiredVolume(nextTarget)
		} else {
			// Still step to apply hold-timeout logic and decay behavior while not held.
			// This keeps VelocityDBPerS decaying in accelerating mode if desired.
			nextCtrl, _ := StepVolumeController(s.VolumeCtrl, baseline, ev.Dt, ev.Now, cfg)
			s.VolumeCtrl = nextCtrl
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
			cmds = append(cmds, CmdSetVolume{TargetDB: v})
		}

	case ActionEvent:
		switch a := ev.Action.(type) {
		case VolumeStep:
			// Rotary cancels holds and any ongoing controller motion.
			s.VolumeCtrl.HeldDirection = 0
			s.VolumeCtrl.VelocityDBPerS = 0
			s.VolumeCtrl.HoldBeganAt = time.Time{}

			// Determine step size.
			dbPerStep := a.DbPerStep
			if dbPerStep == 0 {
				dbPerStep = defaultRotaryDbPerStep
			}
			deltaDB := float64(a.Steps) * dbPerStep

			// Baseline:
			//  - desired if present
			//  - else observed volume if known
			//  - else controller target
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
			// Keep controller position aligned with latest desired target for subsequent ticks.
			s.VolumeCtrl.TargetDB = next

		case VolumeHeld:
			// Start or update hold gesture.
			now := ev.At
			if now.IsZero() {
				now = time.Now()
			}

			// New gesture if transitioning from not-held to held, or reversing direction.
			if s.VolumeCtrl.HeldDirection == 0 || (a.Direction != 0 && a.Direction != s.VolumeCtrl.HeldDirection) {
				s.VolumeCtrl.HoldBeganAt = now
				// Reset velocity on direction change to keep response snappy.
				if a.Direction != s.VolumeCtrl.HeldDirection {
					s.VolumeCtrl.VelocityDBPerS = 0
				}
			}

			s.VolumeCtrl.HeldDirection = a.Direction
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

			next := a.Db
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
		}

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

	default:
		// Unknown event type: no-op.
	}

	return ReduceResult{
		State:    s,
		Commands: cmds,
	}
}
