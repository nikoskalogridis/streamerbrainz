package main

// ============================================================================
// Action Types - Command-based Architecture
// ============================================================================
// Actions represent intent from various sources (IR, IPC, librespot, UI).
// The central daemon loop consumes these actions and applies policy.
// ============================================================================

// Action is a marker interface for all daemon commands
type Action interface{}

// VolumeHeld indicates a volume button is being held
type VolumeHeld struct {
	Direction int // -1 for down, 0 for none, +1 for up
}

// VolumeRelease indicates all volume buttons have been released
type VolumeRelease struct{}

// ToggleMute requests mute state to be toggled
type ToggleMute struct{}

// SetVolumeAbsolute requests volume to be set to a specific value
type SetVolumeAbsolute struct {
	Db     float64
	Origin string // e.g., "ir", "librespot", "ipc", "ui"
}
