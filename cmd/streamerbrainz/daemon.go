package main

import (
	"log/slog"
	"math"
	"time"
)

// ============================================================================
// Central Daemon Loop - The "Daemon Brain"
// ============================================================================
// runDaemon is the central orchestrator that:
//   - Consumes Actions from multiple sources
//   - Updates velocity state
//   - Applies policy (safety, limits, etc.)
//   - Communicates with CamillaDSP
//
// Only this goroutine modifies velocityState and talks to CamillaDSP.
// This prevents race conditions when multiple input sources are added.
// ============================================================================

// runDaemon is the main daemon loop that processes actions and updates state
func runDaemon(
	actions <-chan Action,
	client *CamillaDSPClient,
	velState *velocityState,
	updateHz int,
	logger *slog.Logger,
) {
	updateInterval := time.Second / time.Duration(updateHz)
	ticker := time.NewTicker(updateInterval)
	defer ticker.Stop()

	for {
		select {
		case act := <-actions:
			handleAction(act, client, velState, logger)

		case <-ticker.C:
			// Periodic velocity update and CamillaDSP synchronization
			velState.update()

			// Send update to CamillaDSP if needed
			if velState.shouldSendUpdate() {
				applyVolume(client, velState, logger)
			}
		}
	}
}

// handleAction processes an Action and updates internal state
// This only mutates intent - it does NOT talk to CamillaDSP directly
func handleAction(act Action, client *CamillaDSPClient, velState *velocityState, logger *slog.Logger) {
	switch a := act.(type) {
	case VolumeHeld:
		velState.setHeld(a.Direction)

	case VolumeRelease:
		velState.release()

	case ToggleMute:
		// Mute is immediate, not velocity-based
		if err := client.ToggleMute(); err != nil {
			logger.Error("toggle mute failed", "error", err)
		}

	case SetVolumeAbsolute:
		// Absolute volume request (e.g., from librespot)
		currentVol, err := client.SetVolume(a.Db)
		if err == nil {
			velState.updateVolume(currentVol)
		} else {
			logger.Error("set volume failed", "error", err)
		}

	case LibrespotSessionConnected:
		// No-op for now

	case LibrespotSessionDisconnected:
		// No-op for now

	case LibrespotVolumeChanged:
		// No-op for now

	case LibrespotTrackChanged:
		// No-op for now

	case LibrespotPlaybackState:
		// No-op for now

	case PlexStateChanged:
		logger.Info("Plex state changed",
			"state", a.State,
			"title", a.Title,
			"artist", a.Artist,
			"album", a.Album)
		// No-op for now - just log the event

	default:
		logger.Warn("unknown action type", "type", act)
	}
}

// applyVolume is the ONLY place that sends volume changes to CamillaDSP
// This centralization makes it easy to add policy (fade, mute, etc.)
func applyVolume(client *CamillaDSPClient, velState *velocityState, logger *slog.Logger) {
	targetDB := velState.getTarget()
	currentVol, err := client.SetVolume(targetDB)
	if err == nil {
		velState.updateVolume(currentVol)
	} else {
		logger.Error("apply volume failed", "error", err)
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
