package main

import (
	"log"
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
	ws *wsClient,
	wsURL string,
	velState *velocityState,
	updateHz int,
	readTimeout int,
	verbose bool,
) {
	updateInterval := time.Second / time.Duration(updateHz)
	ticker := time.NewTicker(updateInterval)
	defer ticker.Stop()

	for {
		select {
		case act := <-actions:
			handleAction(act, ws, wsURL, velState, readTimeout, verbose)

		case <-ticker.C:
			// Periodic velocity update and CamillaDSP synchronization
			velState.update()

			// Send update to CamillaDSP if needed
			if velState.shouldSendUpdate() {
				applyVolume(ws, wsURL, velState, readTimeout, verbose)
			}
		}
	}
}

// handleAction processes an Action and updates internal state
// This only mutates intent - it does NOT talk to CamillaDSP directly
func handleAction(act Action, ws *wsClient, wsURL string, velState *velocityState, readTimeout int, verbose bool) {
	switch a := act.(type) {
	case VolumeHeld:
		velState.setHeld(a.Direction)

	case VolumeRelease:
		velState.release()

	case ToggleMute:
		// Mute is immediate, not velocity-based
		sendWithRetry(ws, wsURL, verbose, func() error {
			return handleMuteCommand(ws, verbose)
		})

	case SetVolumeAbsolute:
		// Absolute volume request (e.g., from librespot)
		if verbose {
			log.Printf("[ACTION] SetVolumeAbsolute: %.2f dB from %s", a.Db, a.Origin)
		}
		sendWithRetry(ws, wsURL, verbose, func() error {
			currentVol, err := setVolumeCommand(ws, a.Db, readTimeout, verbose)
			if err == nil {
				velState.updateVolume(currentVol)
			}
			return err
		})

	case LibrespotSessionConnected:
		if verbose {
			log.Printf("[ACTION] LibrespotSessionConnected: user=%s", a.UserName)
		}
		// No-op for now

	case LibrespotSessionDisconnected:
		if verbose {
			log.Printf("[ACTION] LibrespotSessionDisconnected: user=%s", a.UserName)
		}
		// No-op for now

	case LibrespotVolumeChanged:
		if verbose {
			log.Printf("[ACTION] LibrespotVolumeChanged: volume=%d", a.Volume)
		}
		// No-op for now (converted to SetVolumeAbsolute in librespot.go)

	case LibrespotTrackChanged:
		if verbose {
			log.Printf("[ACTION] LibrespotTrackChanged: track=%s name=%s", a.TrackId, a.Name)
		}
		// No-op for now

	case LibrespotPlaybackState:
		if verbose {
			log.Printf("[ACTION] LibrespotPlaybackState: state=%s track=%s", a.State, a.TrackId)
		}
		// No-op for now

	default:
		log.Printf("[WARN] Unknown action type: %T", act)
	}
}

// applyVolume is the ONLY place that sends volume changes to CamillaDSP
// This centralization makes it easy to add policy (fade, mute, etc.)
func applyVolume(ws *wsClient, wsURL string, velState *velocityState, readTimeout int, verbose bool) {
	targetDB := velState.getTarget()
	sendWithRetry(ws, wsURL, verbose, func() error {
		currentVol, err := setVolumeCommand(ws, targetDB, readTimeout, verbose)
		if err == nil {
			velState.updateVolume(currentVol)
		}
		return err
	})
}
