package main

import (
	"fmt"
	"time"
)

// This file introduces a reducer-style architecture building block for the daemon:
//
//   - Events: inputs to the reducer (user actions, time ticks, and results from CamillaDSP)
//   - Commands: side effects the reducer wants executed (CamillaDSP calls, etc.)
//   - Reducer: computes next state + commands, without performing I/O
//
// NOTE: This file is intentionally focused on types and the reducer contract.
// Wiring this into the existing daemon loop is a follow-up step.

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

// Reducer is a pure-ish function:
// - It must not perform I/O
// - It must not block
// - It must only compute next state + commands
//
// The daemon loop is responsible for:
// - executing Commands
// - translating responses into Events
// - feeding those Events back into the reducer
type Reducer func(s *DaemonState, e Event) ReduceResult

// ==============================
// Default reducer (initial implementation)
// ==============================

// Reduce is a starter reducer implementing your current rules:
// - rotary VolumeStep cancels holds (stopMoving)
// - holds integrate velocity engine to produce desired volume on Tick
// - mute/volume are applied via emitted Commands, not by direct side effects
//
// This version does NOT yet manage renderer state; it focuses on volume/mute/config state.
func Reduce(s *DaemonState, e Event, vel *velocityState, cfg VelocityConfig) ReduceResult {
	if s == nil {
		s = &DaemonState{}
	}
	if vel == nil {
		// We keep the reducer total-order deterministic; if vel is nil,
		// we cannot compute hold dynamics. Still handle mute intents and observed state.
		vel = newVelocityState(cfg)
	}

	var cmds []Command

	switch ev := e.(type) {
	case Tick:
		// Tick drives hold integration and also allows emitting coalesced commands.

		// Establish baseline for integration:
		//  - desired volume if present
		//  - else observed volume if known
		//  - else fall back to velocity internal target
		baseline := vel.getTarget()
		if s.Camilla.VolumeKnown {
			baseline = s.Camilla.VolumeDB
		}
		if s.Intent.DesiredVolumeDB != nil {
			baseline = *s.Intent.DesiredVolumeDB
		}

		// Integrate while held; if not held, still let velocity update timeouts.
		if vel.heldDirection != 0 {
			next := vel.computeNextTargetWithDt(baseline, ev.Dt, ev.Now)

			// Clamp using cfg bounds (velocity config remains the canonical bounds for now).
			if next < cfg.MinDB {
				next = cfg.MinDB
			}
			if next > cfg.MaxDB {
				next = cfg.MaxDB
			}

			// Update desired intent (latest-wins).
			s.SetDesiredVolume(next)
		} else {
			vel.updateWithDt(ev.Dt, ev.Now)
		}

		// Convert intents into commands (and clear intents by construction: the reducer coalesces).
		//
		// Note: This is the key architectural shift: "consume" semantics become:
		// - reducer emits a command
		// - reducer clears the intent so it won't re-emit unless re-requested
		if s.Intent.MuteTogglePending {
			// Collapse multiple toggles into one.
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

			// Optional micro-optimization: if observed volume is known and already close, don't emit.
			// (Your project already has a volumeUpdateThresholdDB constant; we don't import it here
			// to keep reducer.go isolated. The daemon wiring can add that policy if desired.)
			cmds = append(cmds, CmdSetVolume{TargetDB: v})
		}

	case ActionEvent:
		// Translate existing Action types into state transitions + intents.
		switch a := ev.Action.(type) {
		case VolumeStep:
			// Rotary cancels holds.
			vel.stopMoving()

			dbPerStep := a.DbPerStep
			if dbPerStep == 0 {
				dbPerStep = defaultRotaryDbPerStep
			}
			deltaDB := float64(a.Steps) * dbPerStep

			// Baseline:
			//  - desired (if present)
			//  - else observed CamillaDSP volume
			//  - else velocity internal target
			current := vel.getTarget()
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

		case VolumeHeld:
			vel.setHeld(a.Direction)

		case VolumeRelease:
			vel.release()

		case ToggleMute:
			s.RequestToggleMute()

		case SetVolumeAbsolute:
			// Absolute set cancels holds.
			vel.stopMoving()

			next := a.Db
			if next < cfg.MinDB {
				next = cfg.MinDB
			}
			if next > cfg.MaxDB {
				next = cfg.MaxDB
			}
			s.SetDesiredVolume(next)

		// For now, non-volume actions don't affect this reducer.
		default:
			// no-op
		}

	case CamillaVolumeObserved:
		s.SetObservedVolume(ev.VolumeDB, ev.At)
		// Keep velocity engine baseline aligned with observed server volume.
		vel.updateVolume(ev.VolumeDB)

	case CamillaMuteObserved:
		s.SetObservedMute(ev.Muted, ev.At)

	case CamillaConfigFilePathObserved:
		s.SetObservedConfigFilePath(ev.Path, ev.At)

	case CamillaProcessingStateObserved:
		s.SetObservedProcessingState(ev.State, ev.At)

	case CamillaCommandFailed:
		// For now, just keep state as-is. In follow-ups you might:
		// - mark Camilla as disconnected
		// - re-queue commands
		// - apply backoff
		_ = ev

	default:
		// Unknown event type: no-op.
	}

	return ReduceResult{
		State:    s,
		Commands: cmds,
	}
}
