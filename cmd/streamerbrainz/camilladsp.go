package main

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/url"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

// CamillaDSPClientInterface defines the interface for CamillaDSP client operations
// This allows for mocking in tests
type CamillaDSPClientInterface interface {
	SetVolume(targetDB float64) (float64, error)
	GetVolume() (float64, error)

	// Mute control (default fader/control "Main")
	GetMute() (bool, error)
	SetMute(mute bool) error
	ToggleMute() (bool, error) // returns new mute state

	// State/config helpers for initial daemon sync
	GetConfigFilePath() (string, error)
	GetState() (string, error)

	Close() error
}

// CamillaDSPClient manages WebSocket communication with CamillaDSP
type CamillaDSPClient struct {
	mu          sync.Mutex
	conn        *websocket.Conn
	url         string
	logger      *slog.Logger
	readTimeout time.Duration
}

// NewCamillaDSPClient creates a new CamillaDSP client and establishes initial connection
func NewCamillaDSPClient(wsURL string, logger *slog.Logger, readTimeout int) (*CamillaDSPClient, error) {
	// Validate URL
	if _, err := url.Parse(wsURL); err != nil {
		return nil, fmt.Errorf("invalid websocket URL: %w", err)
	}

	client := &CamillaDSPClient{
		url:         wsURL,
		logger:      logger,
		readTimeout: time.Duration(readTimeout) * time.Millisecond,
	}

	// Establish initial connection with retry
	if err := client.connectWithRetry(); err != nil {
		return nil, err
	}

	return client, nil
}

// connect establishes a WebSocket connection to CamillaDSP
func (c *CamillaDSPClient) connect() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.conn != nil {
		c.conn.Close()
		c.conn = nil
	}

	u, err := url.Parse(c.url)
	if err != nil {
		return fmt.Errorf("invalid ws url: %w", err)
	}

	d := websocket.Dialer{
		HandshakeTimeout: 2 * time.Second,
	}

	conn, _, err := d.Dial(u.String(), nil)
	if err != nil {
		return err
	}

	c.conn = conn
	return nil
}

// connectWithRetry attempts to connect with exponential backoff
func (c *CamillaDSPClient) connectWithRetry() error {
	var lastErr error
	for attempt := 0; attempt < 10; attempt++ {
		err := c.connect()
		if err == nil {
			c.logger.Info("connected to CamillaDSP", "url", c.url)
			return nil
		}
		lastErr = err
		c.logger.Warn("connection failed; retrying...", "error", err, "attempt", attempt+1)
		time.Sleep(500 * time.Millisecond)
	}
	return fmt.Errorf("failed to connect after 10 attempts: %w", lastErr)
}

// ensureConnected checks connection and reconnects if necessary
func (c *CamillaDSPClient) ensureConnected() error {
	c.mu.Lock()
	if c.conn != nil {
		c.mu.Unlock()
		return nil
	}
	c.mu.Unlock()

	c.logger.Warn("connection lost; reconnecting...")
	return c.connectWithRetry()
}

// send sends a message to CamillaDSP (one-way, no response expected)
func (c *CamillaDSPClient) send(v any) error {
	if err := c.ensureConnected(); err != nil {
		return err
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	if c.conn == nil {
		return fmt.Errorf("no websocket connection")
	}

	payload, err := json.Marshal(v)
	if err != nil {
		return fmt.Errorf("marshal command: %w", err)
	}

	if err := c.conn.WriteMessage(websocket.TextMessage, payload); err != nil {
		c.conn = nil // Mark connection as broken
		return err
	}

	return nil
}

// sendAndRead sends a message and waits for a response
func (c *CamillaDSPClient) sendAndRead(v any, timeout time.Duration) ([]byte, error) {
	if err := c.ensureConnected(); err != nil {
		return nil, err
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	if c.conn == nil {
		return nil, fmt.Errorf("no websocket connection")
	}

	payload, err := json.Marshal(v)
	if err != nil {
		return nil, fmt.Errorf("marshal command: %w", err)
	}

	if err := c.conn.WriteMessage(websocket.TextMessage, payload); err != nil {
		c.conn = nil // Mark connection as broken
		return nil, err
	}

	// Set read deadline
	c.conn.SetReadDeadline(time.Now().Add(timeout))
	defer c.conn.SetReadDeadline(time.Time{})

	_, message, err := c.conn.ReadMessage()
	if err != nil {
		c.conn = nil // Mark connection as broken
		return nil, err
	}

	return message, nil
}

// Close closes the WebSocket connection
func (c *CamillaDSPClient) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.conn != nil {
		c.conn.Close()
		c.conn = nil
	}
	return nil
}

// SetVolume sends a SetVolume command to CamillaDSP and returns the target volume
func (c *CamillaDSPClient) SetVolume(targetDB float64) (float64, error) {
	cmd := map[string]any{"SetVolume": targetDB}

	response, err := c.sendAndRead(cmd, c.readTimeout)
	if err != nil {
		return 0, fmt.Errorf("set volume: %w", err)
	}

	var setResp struct {
		SetVolume struct {
			Result string `json:"result"`
		} `json:"SetVolume"`
	}

	if err := json.Unmarshal(response, &setResp); err != nil {
		c.logger.Warn("failed to parse SetVolume response", "error", err)
		return targetDB, nil // Assume success
	}

	c.logger.Debug("SetVolume", "target_db", targetDB, "result", setResp.SetVolume.Result)

	return targetDB, nil
}

// GetVolume queries CamillaDSP for the current volume
func (c *CamillaDSPClient) GetVolume() (float64, error) {
	cmd := "GetVolume"

	response, err := c.sendAndRead(cmd, c.readTimeout)
	if err != nil {
		return 0, fmt.Errorf("get volume: %w", err)
	}

	var volResp struct {
		GetVolume struct {
			Result string  `json:"result"`
			Value  float64 `json:"value"`
		} `json:"GetVolume"`
	}

	if err := json.Unmarshal(response, &volResp); err != nil {
		c.logger.Warn("failed to parse GetVolume response", "error", err)
		return 0, err
	}

	c.logger.Debug("GetVolume", "volume_db", volResp.GetVolume.Value)

	return volResp.GetVolume.Value, nil
}

// GetMute queries CamillaDSP for the current mute state.
func (c *CamillaDSPClient) GetMute() (bool, error) {
	cmd := "GetMute"

	response, err := c.sendAndRead(cmd, c.readTimeout)
	if err != nil {
		return false, fmt.Errorf("get mute: %w", err)
	}

	var muteResp struct {
		GetMute struct {
			Result string `json:"result"`
			Value  bool   `json:"value"`
		} `json:"GetMute"`
	}

	if err := json.Unmarshal(response, &muteResp); err != nil {
		c.logger.Warn("failed to parse GetMute response", "error", err)
		return false, err
	}

	c.logger.Debug("GetMute", "mute", muteResp.GetMute.Value)

	return muteResp.GetMute.Value, nil
}

// SetMute sets the mute state in CamillaDSP.
func (c *CamillaDSPClient) SetMute(mute bool) error {
	cmd := map[string]any{"SetMute": mute}

	response, err := c.sendAndRead(cmd, c.readTimeout)
	if err != nil {
		return fmt.Errorf("set mute: %w", err)
	}

	var setResp struct {
		SetMute struct {
			Result string `json:"result"`
		} `json:"SetMute"`
	}

	if err := json.Unmarshal(response, &setResp); err != nil {
		c.logger.Warn("failed to parse SetMute response", "error", err)
		return nil // Assume success if we can't parse the response
	}

	c.logger.Debug("SetMute", "mute", mute, "result", setResp.SetMute.Result)

	return nil
}

// ToggleMute sends a ToggleMute command to CamillaDSP and returns the new mute state.
func (c *CamillaDSPClient) ToggleMute() (bool, error) {
	cmd := "ToggleMute"

	response, err := c.sendAndRead(cmd, c.readTimeout)
	if err != nil {
		return false, fmt.Errorf("toggle mute: %w", err)
	}

	var toggleResp struct {
		ToggleMute struct {
			Result string `json:"result"`
			Value  bool   `json:"value"`
		} `json:"ToggleMute"`
	}

	if err := json.Unmarshal(response, &toggleResp); err != nil {
		c.logger.Warn("failed to parse ToggleMute response", "error", err)
		return false, err
	}

	c.logger.Debug("ToggleMute", "mute", toggleResp.ToggleMute.Value, "result", toggleResp.ToggleMute.Result)

	return toggleResp.ToggleMute.Value, nil
}

// GetConfigFilePath queries CamillaDSP for the currently active config file path.
func (c *CamillaDSPClient) GetConfigFilePath() (string, error) {
	cmd := "GetConfigFilePath"

	response, err := c.sendAndRead(cmd, c.readTimeout)
	if err != nil {
		return "", fmt.Errorf("get config file path: %w", err)
	}

	var resp struct {
		GetConfigFilePath struct {
			Result string `json:"result"`
			Value  string `json:"value"`
		} `json:"GetConfigFilePath"`
	}

	if err := json.Unmarshal(response, &resp); err != nil {
		c.logger.Warn("failed to parse GetConfigFilePath response", "error", err)
		return "", err
	}

	c.logger.Debug("GetConfigFilePath", "path", resp.GetConfigFilePath.Value, "result", resp.GetConfigFilePath.Result)

	return resp.GetConfigFilePath.Value, nil
}

// GetState queries CamillaDSP for the current processing state ("Running", "Paused", etc.).
func (c *CamillaDSPClient) GetState() (string, error) {
	cmd := "GetState"

	response, err := c.sendAndRead(cmd, c.readTimeout)
	if err != nil {
		return "", fmt.Errorf("get state: %w", err)
	}

	var resp struct {
		GetState struct {
			Result string `json:"result"`
			Value  string `json:"value"`
		} `json:"GetState"`
	}

	if err := json.Unmarshal(response, &resp); err != nil {
		c.logger.Warn("failed to parse GetState response", "error", err)
		return "", err
	}

	c.logger.Debug("GetState", "state", resp.GetState.Value, "result", resp.GetState.Result)

	return resp.GetState.Value, nil
}
