package main

import (
	"fmt"
	"log"
	"math"
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
		"auto_play_changed", "filter_explicit_content_changed":
		// These events exist but we don't handle them yet
		return nil, nil

	default:
		return nil, fmt.Errorf("unknown event type: %s", eventType)
	}
}

// mapSpotifyVolumeToDB maps Spotify volume (0-65535) to dB range
// Uses logarithmic mapping for better perceived volume control
func mapSpotifyVolumeToDB(spotifyVol uint16, minDB, maxDB float64) float64 {
	if spotifyVol == 0 {
		return minDB
	}
	if spotifyVol == 65535 {
		return maxDB
	}

	// Normalize to 0.0-1.0
	normalized := float64(spotifyVol) / spotifyVolumeMax

	// Apply logarithmic curve
	dbRange := maxDB - minDB
	logValue := math.Log10(1.0 + 9.0*normalized)
	db := minDB + dbRange*logValue

	return db
}

// runLibrespotHook handles librespot hook mode
func runLibrespotHook(socketPath string, minDB, maxDB float64, verbose bool) error {
	// Parse event from environment
	action, err := parseLibrespotEvent()
	if err != nil {
		return err
	}

	// Nil action means event doesn't translate yet
	if action == nil {
		if verbose {
			log.Printf("[LIBRESPOT] event '%s' ignored", os.Getenv("PLAYER_EVENT"))
		}
		return nil
	}

	// Convert LibrespotVolumeChanged to SetVolumeAbsolute for backward compatibility
	if volChange, ok := action.(LibrespotVolumeChanged); ok {
		db := mapSpotifyVolumeToDB(volChange.Volume, minDB, maxDB)
		action = SetVolumeAbsolute{
			Db:     db,
			Origin: "librespot",
		}
	}

	if verbose {
		log.Printf("[LIBRESPOT] event=%s action=%T", os.Getenv("PLAYER_EVENT"), action)
	}

	// Send action via IPC
	if err := SendIPCAction(socketPath, action); err != nil {
		return fmt.Errorf("send IPC action: %w", err)
	}

	if verbose {
		log.Printf("[LIBRESPOT] action sent successfully")
	}

	return nil
}
