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

// Action is a marker interface for all daemon commands.
//
// NOTE: Actions also implement the reducer's Event marker so they can be reduced directly
// (option 2: use TimedEvent for timestamps, keep payload types clean).
type Action interface {
	eventMarker()
}

// VolumeHeld indicates a volume button is being held
type VolumeHeld struct {
	Direction int `json:"direction"` // -1 for down, 0 for none, +1 for up
}

func (VolumeHeld) eventMarker() {}

// VolumeRelease indicates all volume buttons have been released
type VolumeRelease struct{}

func (VolumeRelease) eventMarker() {}

// RotaryTurn represents a raw rotary encoder movement (detents/steps).
// The reducer owns policy for converting this into volume changes (including velocity scaling).
type RotaryTurn struct {
	Steps int `json:"steps"` // positive=up, negative=down
}

func (RotaryTurn) eventMarker() {}

// VolumeStep represents discrete volume adjustments from rotary encoders.
// NOTE: This is an internal "derived" action that may be produced by the reducer.
type VolumeStep struct {
	Steps     int     `json:"steps"`                 // Number of detents/steps (positive=up, negative=down)
	DbPerStep float64 `json:"db_per_step,omitempty"` // Optional: override default step size
}

func (VolumeStep) eventMarker() {}

// ToggleMute requests mute state to be toggled
type ToggleMute struct{}

func (ToggleMute) eventMarker() {}

// SetVolumeAbsolute requests volume to be set to a specific value
type SetVolumeAbsolute struct {
	Db     float64 `json:"db"`
	Origin string  `json:"origin"` // e.g., "ir", "librespot", "ipc", "ui"
}

func (SetVolumeAbsolute) eventMarker() {}

// ============================================================================
// Media Transport Actions (no-op for now; emitted by input devices / IPC / UI)
// ============================================================================

type MediaPlayPause struct{}
type MediaNext struct{}
type MediaPrevious struct{}
type MediaPlay struct{}
type MediaPause struct{}
type MediaStop struct{}

func (MediaPlayPause) eventMarker() {}
func (MediaNext) eventMarker()      {}
func (MediaPrevious) eventMarker()  {}
func (MediaPlay) eventMarker()      {}
func (MediaPause) eventMarker()     {}
func (MediaStop) eventMarker()      {}

// ============================================================================
// Librespot Event Actions
// ============================================================================

// LibrespotSessionConnected indicates a user connected to librespot
type LibrespotSessionConnected struct {
	UserName     string `json:"user_name"`
	ConnectionId string `json:"connection_id"`
}

func (LibrespotSessionConnected) eventMarker() {}

// LibrespotSessionDisconnected indicates a user disconnected from librespot
type LibrespotSessionDisconnected struct {
	UserName     string `json:"user_name"`
	ConnectionId string `json:"connection_id"`
}

func (LibrespotSessionDisconnected) eventMarker() {}

// LibrespotVolumeChanged indicates librespot volume changed
type LibrespotVolumeChanged struct {
	Volume uint16 `json:"volume"` // 0-65535
}

func (LibrespotVolumeChanged) eventMarker() {}

// LibrespotTrackChanged indicates track changed in librespot
type LibrespotTrackChanged struct {
	TrackId    string `json:"track_id"`
	Name       string `json:"name"`
	DurationMs string `json:"duration_ms"`
	Uri        string `json:"uri"`
}

func (LibrespotTrackChanged) eventMarker() {}

// LibrespotPlaybackState indicates playback state changed
type LibrespotPlaybackState struct {
	State      string `json:"state"` // "playing", "paused", "stopped"
	TrackId    string `json:"track_id"`
	PositionMs string `json:"position_ms"`
}

func (LibrespotPlaybackState) eventMarker() {}

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

func (PlexStateChanged) eventMarker() {}

// ============================================================================
// JSON Encoding/Decoding Support
// ============================================================================
// EventEnvelope wraps events for JSON serialization/deserialization.
// Since Go doesn't have union types, we use a type discriminator.
// ============================================================================

// EventEnvelope wraps an event with a type discriminator for JSON marshaling
type EventEnvelope struct {
	Type string          `json:"type"`
	Data json.RawMessage `json:"data,omitempty"`
}

// UnmarshalEvent deserializes a JSON event envelope into a concrete Event
func UnmarshalEvent(data []byte) (Event, error) {
	var env EventEnvelope
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

	case "rotary_turn":
		var a RotaryTurn
		if err := json.Unmarshal(env.Data, &a); err != nil {
			return nil, fmt.Errorf("unmarshal RotaryTurn: %w", err)
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

	case "media_play_pause":
		return MediaPlayPause{}, nil
	case "media_next":
		return MediaNext{}, nil
	case "media_previous":
		return MediaPrevious{}, nil
	case "media_play":
		return MediaPlay{}, nil
	case "media_pause":
		return MediaPause{}, nil
	case "media_stop":
		return MediaStop{}, nil

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
		return nil, fmt.Errorf("unknown event type: %q", env.Type)
	}
}

// MarshalEvent serializes an Event into a JSON envelope with type discriminator
func MarshalEvent(e Event) ([]byte, error) {
	var env EventEnvelope

	switch e := e.(type) {
	case VolumeHeld:
		env.Type = "volume_held"
		data, err := json.Marshal(e)
		if err != nil {
			return nil, fmt.Errorf("marshal VolumeHeld: %w", err)
		}
		env.Data = data

	case VolumeRelease:
		env.Type = "volume_release"

	case RotaryTurn:
		env.Type = "rotary_turn"
		data, err := json.Marshal(e)
		if err != nil {
			return nil, fmt.Errorf("marshal RotaryTurn: %w", err)
		}
		env.Data = data

	case VolumeStep:
		env.Type = "volume_step"
		data, err := json.Marshal(e)
		if err != nil {
			return nil, fmt.Errorf("marshal VolumeStep: %w", err)
		}
		env.Data = data

	case ToggleMute:
		env.Type = "toggle_mute"

	case SetVolumeAbsolute:
		env.Type = "set_volume_absolute"
		data, err := json.Marshal(e)
		if err != nil {
			return nil, fmt.Errorf("marshal SetVolumeAbsolute: %w", err)
		}
		env.Data = data

	case MediaPlayPause:
		env.Type = "media_play_pause"
	case MediaNext:
		env.Type = "media_next"
	case MediaPrevious:
		env.Type = "media_previous"
	case MediaPlay:
		env.Type = "media_play"
	case MediaPause:
		env.Type = "media_pause"
	case MediaStop:
		env.Type = "media_stop"

	case LibrespotSessionConnected:
		env.Type = "librespot_session_connected"
		data, err := json.Marshal(e)
		if err != nil {
			return nil, fmt.Errorf("marshal LibrespotSessionConnected: %w", err)
		}
		env.Data = data

	case LibrespotSessionDisconnected:
		env.Type = "librespot_session_disconnected"
		data, err := json.Marshal(e)
		if err != nil {
			return nil, fmt.Errorf("marshal LibrespotSessionDisconnected: %w", err)
		}
		env.Data = data

	case LibrespotVolumeChanged:
		env.Type = "librespot_volume_changed"
		data, err := json.Marshal(e)
		if err != nil {
			return nil, fmt.Errorf("marshal LibrespotVolumeChanged: %w", err)
		}
		env.Data = data

	case LibrespotTrackChanged:
		env.Type = "librespot_track_changed"
		data, err := json.Marshal(e)
		if err != nil {
			return nil, fmt.Errorf("marshal LibrespotTrackChanged: %w", err)
		}
		env.Data = data

	case LibrespotPlaybackState:
		env.Type = "librespot_playback_state"
		data, err := json.Marshal(e)
		if err != nil {
			return nil, fmt.Errorf("marshal LibrespotPlaybackState: %w", err)
		}
		env.Data = data

	case PlexStateChanged:
		env.Type = "plex_state_changed"
		data, err := json.Marshal(e)
		if err != nil {
			return nil, fmt.Errorf("marshal PlexStateChanged: %w", err)
		}
		env.Data = data

	default:
		return nil, fmt.Errorf("unsupported event type: %T", e)
	}

	return json.Marshal(env)
}
