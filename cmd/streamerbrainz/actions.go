package main

import (
	"encoding/json"
	"fmt"
)

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
	Direction int `json:"direction"` // -1 for down, 0 for none, +1 for up
}

// VolumeRelease indicates all volume buttons have been released
type VolumeRelease struct{}

// VolumeStep represents discrete volume adjustments from rotary encoders
// This bypasses the velocity/hold system for clean step-based control
type VolumeStep struct {
	Steps     int     `json:"steps"`                 // Number of detents/steps (positive=up, negative=down)
	DbPerStep float64 `json:"db_per_step,omitempty"` // Optional: override default step size
}

// ToggleMute requests mute state to be toggled
type ToggleMute struct{}

// SetVolumeAbsolute requests volume to be set to a specific value
type SetVolumeAbsolute struct {
	Db     float64 `json:"db"`
	Origin string  `json:"origin"` // e.g., "ir", "librespot", "ipc", "ui"
}

// ============================================================================
// Librespot Event Actions
// ============================================================================

// LibrespotSessionConnected indicates a user connected to librespot
type LibrespotSessionConnected struct {
	UserName     string `json:"user_name"`
	ConnectionId string `json:"connection_id"`
}

// LibrespotSessionDisconnected indicates a user disconnected from librespot
type LibrespotSessionDisconnected struct {
	UserName     string `json:"user_name"`
	ConnectionId string `json:"connection_id"`
}

// LibrespotVolumeChanged indicates librespot volume changed
type LibrespotVolumeChanged struct {
	Volume uint16 `json:"volume"` // 0-65535
}

// LibrespotTrackChanged indicates track changed in librespot
type LibrespotTrackChanged struct {
	TrackId    string `json:"track_id"`
	Name       string `json:"name"`
	DurationMs string `json:"duration_ms"`
	Uri        string `json:"uri"`
}

// LibrespotPlaybackState indicates playback state changed
type LibrespotPlaybackState struct {
	State      string `json:"state"` // "playing", "paused", "stopped"
	TrackId    string `json:"track_id"`
	PositionMs string `json:"position_ms"`
}

// ============================================================================
// Plexamp Event Actions
// ============================================================================

// PlexStateChanged indicates Plexamp/Plex playback state changed
type PlexStateChanged struct {
	State         string `json:"state"`          // "playing", "paused", "stopped"
	Title         string `json:"title"`          // Track title
	Artist        string `json:"artist"`         // Artist name
	Album         string `json:"album"`          // Album name
	DurationMs    int64  `json:"duration_ms"`    // Track duration in milliseconds
	PositionMs    int64  `json:"position_ms"`    // Current position in milliseconds
	SessionKey    string `json:"session_key"`    // Plex session key
	RatingKey     string `json:"rating_key"`     // Plex rating key
	PlayerTitle   string `json:"player_title"`   // Player name
	PlayerProduct string `json:"player_product"` // Player product (e.g., "Plexamp")
}

// ============================================================================
// JSON Encoding/Decoding Support
// ============================================================================
// ActionEnvelope wraps actions for JSON serialization/deserialization.
// Since Go doesn't have union types, we use a type discriminator.
// ============================================================================

// ActionEnvelope wraps an action with a type discriminator for JSON marshaling
type ActionEnvelope struct {
	Type string          `json:"type"`
	Data json.RawMessage `json:"data,omitempty"`
}

// UnmarshalAction deserializes a JSON action envelope into a concrete Action
func UnmarshalAction(data []byte) (Action, error) {
	var env ActionEnvelope
	if err := json.Unmarshal(data, &env); err != nil {
		return nil, fmt.Errorf("unmarshal envelope: %w", err)
	}

	switch env.Type {
	case "volume_held":
		var a VolumeHeld
		if err := json.Unmarshal(env.Data, &a); err != nil {
			return nil, fmt.Errorf("unmarshal VolumeHeld: %w", err)
		}
		return a, nil

	case "volume_release":
		return VolumeRelease{}, nil

	case "volume_step":
		var a VolumeStep
		if err := json.Unmarshal(env.Data, &a); err != nil {
			return nil, fmt.Errorf("unmarshal VolumeStep: %w", err)
		}
		return a, nil

	case "toggle_mute":
		return ToggleMute{}, nil

	case "set_volume_absolute":
		var a SetVolumeAbsolute
		if err := json.Unmarshal(env.Data, &a); err != nil {
			return nil, fmt.Errorf("unmarshal SetVolumeAbsolute: %w", err)
		}
		return a, nil

	case "librespot_session_connected":
		var a LibrespotSessionConnected
		if err := json.Unmarshal(env.Data, &a); err != nil {
			return nil, fmt.Errorf("unmarshal LibrespotSessionConnected: %w", err)
		}
		return a, nil

	case "librespot_session_disconnected":
		var a LibrespotSessionDisconnected
		if err := json.Unmarshal(env.Data, &a); err != nil {
			return nil, fmt.Errorf("unmarshal LibrespotSessionDisconnected: %w", err)
		}
		return a, nil

	case "librespot_volume_changed":
		var a LibrespotVolumeChanged
		if err := json.Unmarshal(env.Data, &a); err != nil {
			return nil, fmt.Errorf("unmarshal LibrespotVolumeChanged: %w", err)
		}
		return a, nil

	case "librespot_track_changed":
		var a LibrespotTrackChanged
		if err := json.Unmarshal(env.Data, &a); err != nil {
			return nil, fmt.Errorf("unmarshal LibrespotTrackChanged: %w", err)
		}
		return a, nil

	case "librespot_playback_state":
		var a LibrespotPlaybackState
		if err := json.Unmarshal(env.Data, &a); err != nil {
			return nil, fmt.Errorf("unmarshal LibrespotPlaybackState: %w", err)
		}
		return a, nil

	case "plex_state_changed":
		var a PlexStateChanged
		if err := json.Unmarshal(env.Data, &a); err != nil {
			return nil, fmt.Errorf("unmarshal PlexStateChanged: %w", err)
		}
		return a, nil

	default:
		return nil, fmt.Errorf("unknown action type: %s", env.Type)
	}
}

// MarshalAction serializes an Action into a JSON action envelope
func MarshalAction(action Action) ([]byte, error) {
	var env ActionEnvelope

	switch a := action.(type) {
	case VolumeHeld:
		env.Type = "volume_held"
		data, err := json.Marshal(a)
		if err != nil {
			return nil, fmt.Errorf("marshal VolumeHeld: %w", err)
		}
		env.Data = data

	case VolumeRelease:
		env.Type = "volume_release"

	case VolumeStep:
		env.Type = "volume_step"
		data, err := json.Marshal(a)
		if err != nil {
			return nil, fmt.Errorf("marshal VolumeStep: %w", err)
		}
		env.Data = data

	case ToggleMute:
		env.Type = "toggle_mute"

	case SetVolumeAbsolute:
		env.Type = "set_volume_absolute"
		data, err := json.Marshal(a)
		if err != nil {
			return nil, fmt.Errorf("marshal SetVolumeAbsolute: %w", err)
		}
		env.Data = data

	case LibrespotSessionConnected:
		env.Type = "librespot_session_connected"
		data, err := json.Marshal(a)
		if err != nil {
			return nil, fmt.Errorf("marshal LibrespotSessionConnected: %w", err)
		}
		env.Data = data

	case LibrespotSessionDisconnected:
		env.Type = "librespot_session_disconnected"
		data, err := json.Marshal(a)
		if err != nil {
			return nil, fmt.Errorf("marshal LibrespotSessionDisconnected: %w", err)
		}
		env.Data = data

	case LibrespotVolumeChanged:
		env.Type = "librespot_volume_changed"
		data, err := json.Marshal(a)
		if err != nil {
			return nil, fmt.Errorf("marshal LibrespotVolumeChanged: %w", err)
		}
		env.Data = data

	case LibrespotTrackChanged:
		env.Type = "librespot_track_changed"
		data, err := json.Marshal(a)
		if err != nil {
			return nil, fmt.Errorf("marshal LibrespotTrackChanged: %w", err)
		}
		env.Data = data

	case LibrespotPlaybackState:
		env.Type = "librespot_playback_state"
		data, err := json.Marshal(a)
		if err != nil {
			return nil, fmt.Errorf("marshal LibrespotPlaybackState: %w", err)
		}
		env.Data = data

	case PlexStateChanged:
		env.Type = "plex_state_changed"
		data, err := json.Marshal(a)
		if err != nil {
			return nil, fmt.Errorf("marshal PlexStateChanged: %w", err)
		}
		env.Data = data

	default:
		return nil, fmt.Errorf("unknown action type: %T", action)
	}

	return json.Marshal(env)
}
