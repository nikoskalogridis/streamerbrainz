package main

import (
	"fmt"
	"log/slog"
	"os"
	"strconv"
)

// ============================================================================
// Librespot Integration
// ============================================================================
// This module parses librespot onevent hooks and converts them to actions.
// librespot passes events via environment variables.
// Usage: argon-camilladsp-remote librespot-hook
// ============================================================================

const (
	spotifyVolumeMax = 65535.0 // Spotify volume range: 0-65535
)

// parseLibrespotEvent reads librespot event from environment variables
func parseLibrespotEvent() (Action, error) {
	eventType := os.Getenv("PLAYER_EVENT")
	if eventType == "" {
		return nil, fmt.Errorf("PLAYER_EVENT not set")
	}

	switch eventType {
	case "session_connected":
		return LibrespotSessionConnected{
			UserName:     os.Getenv("USER_NAME"),
			ConnectionId: os.Getenv("CONNECTION_ID"),
		}, nil

	case "session_disconnected":
		return LibrespotSessionDisconnected{
			UserName:     os.Getenv("USER_NAME"),
			ConnectionId: os.Getenv("CONNECTION_ID"),
		}, nil

	case "volume_changed":
		volStr := os.Getenv("VOLUME")
		vol, err := strconv.ParseUint(volStr, 10, 16)
		if err != nil {
			return nil, fmt.Errorf("parse volume: %w", err)
		}
		return LibrespotVolumeChanged{
			Volume: uint16(vol),
		}, nil

	case "track_changed":
		return LibrespotTrackChanged{
			TrackId:    os.Getenv("TRACK_ID"),
			Name:       os.Getenv("NAME"),
			DurationMs: os.Getenv("DURATION_MS"),
			Uri:        os.Getenv("URI"),
		}, nil

	case "playing", "paused", "stopped", "seeked", "position_correction":
		return LibrespotPlaybackState{
			State:      eventType,
			TrackId:    os.Getenv("TRACK_ID"),
			PositionMs: os.Getenv("POSITION_MS"),
		}, nil

	case "started", "end_of_track", "loading", "preloading", "unavailable":
		// These events exist but we don't handle them yet
		return nil, nil

	case "session_client_changed", "shuffle_changed", "repeat_changed",
		"auto_play_changed", "filter_explicit_content_changed", "play_request_id_changed":
		// These events exist but we don't handle them yet
		return nil, nil

	default:
		return nil, fmt.Errorf("unknown event type: %s", eventType)
	}
}

// runLibrespotHook handles librespot hook mode
func runLibrespotHook(socketPath string, logger *slog.Logger) error {
	// Parse event from environment
	action, err := parseLibrespotEvent()
	if err != nil {
		return err
	}

	// Nil action means event doesn't translate yet
	if action == nil {
		logger.Debug("librespot event ignored", "event", os.Getenv("PLAYER_EVENT"))
		return nil
	}

	logger.Debug("librespot event", "event", os.Getenv("PLAYER_EVENT"), "action", fmt.Sprintf("%T", action))

	// Send action via IPC
	if err := SendIPCAction(socketPath, action); err != nil {
		return fmt.Errorf("send IPC action: %w", err)
	}

	logger.Debug("librespot action sent successfully")

	return nil
}
