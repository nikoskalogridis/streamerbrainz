package main

import "time"

// DaemonState is the top-level, daemon-owned state container.
//
// Goals:
//   - Keep all reducer-owned state in one place (pure reducer, no external mutation).
//   - Provide a single place to store observed state (what CamillaDSP reported)
//     and intent (what we want to apply).
//   - Make it easy to publish a coherent snapshot to other clients (IPC/UI/etc).
//
// NOTE: This file only introduces the state model. Wiring it into the daemon loop
// (initial sync, applying effects, publishing snapshots) is handled elsewhere.
type DaemonState struct {
	// CamillaDSP is the authoritative backend for volume/mute/config.
	// We cache what we last observed from CamillaDSP so we can expose it to other clients.
	Camilla CamillaDSPState

	// VolumeCtrl is the reducer-owned volume controller state (velocity/hold dynamics).
	// This embeds what used to be mutated inside velocityState so the reducer can be pure.
	VolumeCtrl VolumeControllerState

	// Rotary is reducer-owned state used for rotary velocity detection and step scaling policy.
	// Input handlers should emit raw rotary actions; the reducer should apply velocity policy.
	Rotary RotaryReducerState

	// Intent contains desired changes that should be applied by the daemon's
	// centralized effects stage (the only place that should talk to CamillaDSP).
	Intent DaemonIntent
}

// VolumeControllerState is the reducer-owned state for the velocity/hold volume controller.
//
// This is NOT CamillaDSP state. It represents "how the controller should behave over time"
// (held direction, velocity, gesture timing). The reducer updates this state, and the daemon
// loop applies the resulting desired volume to CamillaDSP via commands.
//
// Keeping this inside DaemonState (Option A) allows the reducer to remain pure:
// it returns a new DaemonState without mutating external controller objects.
type VolumeControllerState struct {
	// TargetDB is the controller's current desired target position (in dB).
	// This is a controller state variable used for integration; it is not the authoritative
	// observed volume (which lives in CamillaDSPState.VolumeDB).
	TargetDB float64

	// VelocityDBPerS is the current velocity in dB/s (signed).
	// Used in accelerating mode; in constant mode it may remain 0.
	VelocityDBPerS float64

	// HeldDirection: -1 for down, 0 for none, 1 for up
	HeldDirection int

	// Timing for hold gestures and safety timeouts
	LastHeldAt  time.Time
	HoldBeganAt time.Time
}

// RotaryReducerState tracks recent rotary turns for reducer-side velocity detection.
// The reducer can use this to implement step scaling (e.g. "fast spin" multiplier)
// without depending on any external mutable state.
type RotaryReducerState struct {
	RecentSteps []RotaryReducerStep
}

// RotaryReducerStep is one observed rotary detent/step at a given time.
// Direction is -1 or +1.
type RotaryReducerStep struct {
	At        time.Time
	Direction int
}

// CamillaDSPState is the daemon's cached view of CamillaDSP.
//
// This is "observed" state: it should be updated when we successfully query CamillaDSP
// or when a CamillaDSP command returns a value confirming the new state.
type CamillaDSPState struct {
	// VolumeDB is the last observed actual volume from CamillaDSP.
	VolumeDB    float64
	VolumeKnown bool
	VolumeAt    time.Time // when VolumeDB was last refreshed

	// Muted is the last observed mute status from CamillaDSP.
	Muted     bool
	MuteKnown bool
	MuteAt    time.Time // when Muted was last refreshed

	// Config is the last observed/known active config identifier.
	// Prefer storing file path/title/hash instead of full YAML to keep snapshots small.
	Config CamillaDSPConfigState

	// State is the processing state reported by CamillaDSP (e.g. Running/Paused/etc),
	// if you choose to cache it.
	Processing CamillaDSPProcessingState
}

type CamillaDSPConfigState struct {
	// FilePath is from GetConfigFilePath (if used). Empty if unknown/unset.
	FilePath string
	Known    bool
	At       time.Time
}

type CamillaDSPProcessingState struct {
	// State is from GetState (if used). Empty if unknown/unset.
	State string
	Known bool
	At    time.Time
}

// DaemonIntent captures pending user/system intents.
// These are applied by the daemon's centralized side-effect stage (the only code
// that should talk to CamillaDSP).
type DaemonIntent struct {
	// MuteTogglePending indicates a ToggleMute was requested and not yet applied.
	// If multiple toggles are requested before being applied, they collapse into one
	// (toggle is its own inverse).
	MuteTogglePending bool

	// DesiredMute, if non-nil, represents an intent to set mute to a specific state.
	// This is more expressive than toggle and is useful for UI synchronization.
	DesiredMute *bool

	// DesiredVolumeDB, if non-nil, represents an intent to set volume to a specific value.
	// This is intentionally separate from any velocity engine/controller state.
	DesiredVolumeDB *float64
}

// RequestToggleMute records a mute toggle intent.
// This is intended to be called only by the daemon goroutine (single-owner).
func (s *DaemonState) RequestToggleMute() {
	s.Intent.MuteTogglePending = true
	// If there is an explicit DesiredMute pending, a toggle becomes ambiguous.
	// Keep DesiredMute as-is and let the effects layer decide precedence.
}

// ConsumeToggleMute consumes a pending toggle intent.
// Returns true if there was a toggle pending.
// This is intended to be called only by the daemon goroutine (single-owner).
func (s *DaemonState) ConsumeToggleMute() bool {
	if !s.Intent.MuteTogglePending {
		return false
	}
	s.Intent.MuteTogglePending = false
	return true
}

// SetDesiredVolume records an explicit desired volume intent.
// This is intended to be called only by the daemon goroutine (single-owner).
func (s *DaemonState) SetDesiredVolume(db float64) {
	s.Intent.DesiredVolumeDB = &db
}

// ClearDesiredVolume clears any pending desired volume intent.
// This is intended to be called only by the daemon goroutine (single-owner).
func (s *DaemonState) ClearDesiredVolume() {
	s.Intent.DesiredVolumeDB = nil
}

// GetDesiredVolume returns (value, true) if a desired volume intent is present.
// This is intended to be called only by the daemon goroutine (single-owner).
func (s *DaemonState) GetDesiredVolume() (float64, bool) {
	if s.Intent.DesiredVolumeDB == nil {
		return 0, false
	}
	return *s.Intent.DesiredVolumeDB, true
}

// ConsumeDesiredVolume consumes the desired volume intent, if present.
// Returns (value, true) if there was an intent.
// This is intended to be called only by the daemon goroutine (single-owner).
func (s *DaemonState) ConsumeDesiredVolume() (float64, bool) {
	if s.Intent.DesiredVolumeDB == nil {
		return 0, false
	}
	v := *s.Intent.DesiredVolumeDB
	s.Intent.DesiredVolumeDB = nil
	return v, true
}

// SetObservedMute updates the cached mute state from CamillaDSP.
// This is intended to be called only by the daemon goroutine (single-owner),
// after successful GetMute/ToggleMute/SetMute results.
func (s *DaemonState) SetObservedMute(muted bool, now time.Time) {
	s.Camilla.Muted = muted
	s.Camilla.MuteKnown = true
	s.Camilla.MuteAt = now
}

// SetObservedVolume updates the cached volume state from CamillaDSP.
// This is intended to be called only by the daemon goroutine (single-owner),
// after successful GetVolume/SetVolume results.
func (s *DaemonState) SetObservedVolume(volumeDB float64, now time.Time) {
	s.Camilla.VolumeDB = volumeDB
	s.Camilla.VolumeKnown = true
	s.Camilla.VolumeAt = now
}

// SetObservedConfigFilePath updates the cached config file path from CamillaDSP.
// This is intended to be called only by the daemon goroutine (single-owner),
// after successful GetConfigFilePath results.
func (s *DaemonState) SetObservedConfigFilePath(path string, now time.Time) {
	s.Camilla.Config.FilePath = path
	s.Camilla.Config.Known = true
	s.Camilla.Config.At = now
}

// SetObservedProcessingState updates the cached processing state from CamillaDSP.
// This is intended to be called only by the daemon goroutine (single-owner),
// after successful GetState results.
func (s *DaemonState) SetObservedProcessingState(state string, now time.Time) {
	s.Camilla.Processing.State = state
	s.Camilla.Processing.Known = true
	s.Camilla.Processing.At = now
}
