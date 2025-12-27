package main

import (
	"encoding/xml"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"strings"
)

// ============================================================================
// Plexamp Integration
// ============================================================================
// This module receives webhook events from Plex Media Server and converts
// them to events. When a webhook is received, it queries the Plex API
// /status/sessions endpoint to get detailed track information, filtering
// by machineIdentifier to find the correct player.
//
// Usage: streamerbrainz plexamp-webhook [OPTIONS]
// ============================================================================

// PlexMediaContainer represents the root XML response from Plex API
type PlexMediaContainer struct {
	XMLName xml.Name    `xml:"MediaContainer"`
	Size    int         `xml:"size,attr"`
	Tracks  []PlexTrack `xml:"Track"`
}

// PlexTrack represents a track in the Plex sessions response
type PlexTrack struct {
	Title            string      `xml:"title,attr"`
	GrandparentTitle string      `xml:"grandparentTitle,attr"` // Artist
	ParentTitle      string      `xml:"parentTitle,attr"`      // Album
	Duration         int64       `xml:"duration,attr"`         // milliseconds
	ViewOffset       int64       `xml:"viewOffset,attr"`       // current position in ms
	SessionKey       string      `xml:"sessionKey,attr"`
	RatingKey        string      `xml:"ratingKey,attr"`
	Type             string      `xml:"type,attr"`
	Player           PlexPlayer  `xml:"Player"`
	Media            []PlexMedia `xml:"Media"`
}

// PlexPlayer represents the player information in a Plex session
type PlexPlayer struct {
	MachineIdentifier string `xml:"machineIdentifier,attr"`
	State             string `xml:"state,attr"` // "playing", "paused", "stopped"
	Title             string `xml:"title,attr"` // Player name
	Product           string `xml:"product,attr"`
	Platform          string `xml:"platform,attr"`
}

// PlexMedia represents media information in a track
type PlexMedia struct {
	AudioChannels int    `xml:"audioChannels,attr"`
	AudioCodec    string `xml:"audioCodec,attr"`
	Bitrate       int    `xml:"bitrate,attr"`
	Duration      int64  `xml:"duration,attr"`
	Container     string `xml:"container,attr"`
}

// PlexampConfig holds configuration for the Plexamp webhook server
type PlexampConfig struct {
	ServerUrl         string // Plex server URL (e.g., "http://plex.home.arpa:32400")
	Token             string // Plex authentication token
	MachineIdentifier string // Machine identifier to filter sessions by
}

// fetchPlexSessions queries the Plex API for current sessions
func fetchPlexSessions(config PlexampConfig, logger *slog.Logger) (*PlexMediaContainer, error) {
	// Build URL with token
	baseURL := fmt.Sprintf("%s/status/sessions", config.ServerUrl)
	u, err := url.Parse(baseURL)
	if err != nil {
		return nil, fmt.Errorf("parse base URL: %w", err)
	}

	q := u.Query()
	q.Set("X-Plex-Token", config.Token)
	u.RawQuery = q.Encode()

	logger.Debug("fetching Plex sessions", "url", u.String())

	// Make HTTP request
	resp, err := http.Get(u.String())
	if err != nil {
		return nil, fmt.Errorf("HTTP request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(body))
	}

	// Parse XML response
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response body: %w", err)
	}

	logger.Debug("received Plex response", "body_length", len(body))

	var container PlexMediaContainer
	if err := xml.Unmarshal(body, &container); err != nil {
		return nil, fmt.Errorf("parse XML: %w", err)
	}

	return &container, nil
}

// findTrackByMachineIdentifier searches for a track with matching player
func findTrackByMachineIdentifier(container *PlexMediaContainer, machineID string) *PlexTrack {
	for i := range container.Tracks {
		if container.Tracks[i].Player.MachineIdentifier == machineID {
			return &container.Tracks[i]
		}
	}
	return nil
}

// handlePlexWebhook processes incoming Plex webhook events
func handlePlexWebhook(config PlexampConfig, events chan<- Event, logger *slog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		logger.Debug("received Plex webhook", "method", r.Method, "path", r.URL.Path)

		// Respond immediately: the webhook delivery has succeeded once we accept it.
		w.WriteHeader(http.StatusOK)

		// Process session lookup asynchronously.
		go func() {
			// Fetch current sessions from Plex
			container, err := fetchPlexSessions(config, logger)
			if err != nil {
				logger.Error("failed to fetch Plex sessions", "error", err)
				return
			}

			logger.Debug("fetched Plex sessions", "count", container.Size)

			// Find track for our machine identifier
			track := findTrackByMachineIdentifier(container, config.MachineIdentifier)
			if track == nil {
				logger.Debug("no track found for machine identifier", "machine_id", config.MachineIdentifier)
				// Not an error: the webhook might be for a different player.
				return
			}

			logger.Info("Plex session found",
				"title", track.Title,
				"artist", track.GrandparentTitle,
				"album", track.ParentTitle,
				"state", track.Player.State,
				"position_ms", track.ViewOffset,
				"duration_ms", track.Duration)

			// Create event from track info
			event := PlexStateChanged{
				State:         track.Player.State,
				Title:         track.Title,
				Artist:        track.GrandparentTitle,
				Album:         track.ParentTitle,
				DurationMs:    track.Duration,
				PositionMs:    track.ViewOffset,
				SessionKey:    track.SessionKey,
				RatingKey:     track.RatingKey,
				PlayerTitle:   track.Player.Title,
				PlayerProduct: track.Player.Product,
			}

			// Send action to daemon
			select {
			case events <- event:
				logger.Debug("Plex action sent", "state", track.Player.State)
			default:
				logger.Warn("action queue full, dropping Plex event")
			}
		}()
	}
}

// setupPlexWebhook registers the Plex webhook endpoint
func setupPlexWebhook(serverUrl, tokenFile, machineID string, events chan<- Event, logger *slog.Logger) error {
	// Load token from file
	tokenBytes, err := os.ReadFile(tokenFile)
	if err != nil {
		return fmt.Errorf("failed to read plex token file: %w", err)
	}
	token := strings.TrimSpace(string(tokenBytes))
	if token == "" {
		return fmt.Errorf("plex token file is empty")
	}

	plexConfig := PlexampConfig{
		ServerUrl:         serverUrl,
		Token:             token,
		MachineIdentifier: machineID,
	}

	http.HandleFunc("/webhooks/plex", handlePlexWebhook(plexConfig, events, logger))
	logger.Info("Plex webhook enabled", "server", serverUrl, "machine_id", machineID, "endpoint", "/webhooks/plex")

	return nil
}
