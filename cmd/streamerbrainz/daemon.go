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

	// Configure the velocity engine's dt clamp relative to the daemon tick rate.
	velState.setUpdateHz(updateHz)

	// Drive the engine with an explicit dt for consistent behavior and testability.
	lastTick := time.Now()

	for {
		select {
		case act := <-actions:
			handleAction(act, client, velState, logger)

		case now := <-ticker.C:
			// Periodic velocity update and CamillaDSP synchronization
			dt := now.Sub(lastTick).Seconds()
			lastTick = now
			velState.updateWithDt(dt, now)

			// Send update to CamillaDSP if needed
			if velState.shouldSendUpdate() {
				applyVolume(client, velState, logger)
			}
		}
	}
}

// handleAction processes an Action and updates internal state
// This only mutates intent - it does NOT talk to CamillaDSP directly
func handleAction(act Action, client CamillaDSPClientInterface, velState *velocityState, logger *slog.Logger) {
	switch a := act.(type) {
	case VolumeStep:
		// Handle rotary encoder steps - bypasses velocity engine entirely
		dbPerStep := a.DbPerStep
		if dbPerStep == 0 {
			dbPerStep = defaultRotaryDbPerStep
		}

		deltaDB := float64(a.Steps) * dbPerStep

		// Get current volume (from velocity state or server)
		currentVol := velState.getTarget()
		if !velState.volumeKnown {
			// Need to query server first
			vol, err := client.GetVolume()
			if err != nil {
				logger.Error("get volume failed for rotary step", "error", err)
				return
			}
			currentVol = vol
		}

		newVol := currentVol + deltaDB

		// Clamp to limits (reuse velocity config bounds)
		if newVol < velState.cfg.MinDB {
			newVol = velState.cfg.MinDB
		}
		if newVol > velState.cfg.MaxDB {
			newVol = velState.cfg.MaxDB
		}

		// Apply immediately
		actualVol, err := client.SetVolume(newVol)
		if err == nil {
			velState.updateVolume(actualVol)
			logger.Debug("volume step applied",
				"steps", a.Steps,
				"delta_db", deltaDB,
				"new_volume", actualVol)
		} else {
			logger.Error("volume step failed", "error", err)
		}

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
func applyVolume(client CamillaDSPClientInterface, velState *velocityState, logger *slog.Logger) {
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
