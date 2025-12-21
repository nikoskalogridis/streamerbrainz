package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/url"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

// wsClient manages a WebSocket connection to CamillaDSP
type wsClient struct {
	mu    sync.Mutex
	conn  *websocket.Conn
	wsURL string
}

// newWSClient creates a new WebSocket client
func newWSClient(wsURL string) *wsClient {
	return &wsClient{wsURL: wsURL}
}

// connect establishes a WebSocket connection to CamillaDSP
func (c *wsClient) connect() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.conn != nil {
		c.conn.Close()
		c.conn = nil
	}

	u, err := url.Parse(c.wsURL)
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

	// Optional: keep socket from stalling silently
	conn.SetWriteDeadline(time.Now().Add(2 * time.Second))
	conn.SetWriteDeadline(time.Time{})

	c.conn = conn
	return nil
}

// send sends a message to CamillaDSP (one-way)
func (c *wsClient) send(v any) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.conn == nil {
		return fmt.Errorf("no websocket connection")
	}
	return sendJSONText(c.conn, v)
}

// sendAndRead sends a message and waits for a response
func (c *wsClient) sendAndRead(v any, timeout time.Duration) ([]byte, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.conn == nil {
		return nil, fmt.Errorf("no websocket connection")
	}

	if err := sendJSONText(c.conn, v); err != nil {
		return nil, err
	}

	// Set read deadline
	c.conn.SetReadDeadline(time.Now().Add(timeout))
	defer c.conn.SetReadDeadline(time.Time{})

	_, message, err := c.conn.ReadMessage()
	if err != nil {
		return nil, err
	}

	return message, nil
}

// close closes the WebSocket connection
func (c *wsClient) close() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.conn != nil {
		c.conn.Close()
		c.conn = nil
	}
}

// connectWithRetry connects to the WebSocket server with retry logic
func connectWithRetry(ws *wsClient, wsURL string, verbose bool) {
	for {
		err := ws.connect()
		if err == nil {
			if verbose {
				log.Printf("connected websocket: %s", wsURL)
			}
			return
		}
		log.Printf("ws connect failed: %v; retrying...", err)
		time.Sleep(500 * time.Millisecond)
	}
}

// sendJSONText sends a JSON message as a text WebSocket frame
func sendJSONText(conn *websocket.Conn, v any) error {
	payload, err := json.Marshal(v)
	if err != nil {
		return err
	}
	return conn.WriteMessage(websocket.TextMessage, payload)
}

// sendWithRetry executes a command function and reconnects on error
func sendWithRetry(ws *wsClient, wsURL string, verbose bool, cmd func() error) {
	if err := cmd(); err != nil {
		log.Printf("ws send failed: %v; reconnecting...", err)
		connectWithRetry(ws, wsURL, verbose)
	}
}
