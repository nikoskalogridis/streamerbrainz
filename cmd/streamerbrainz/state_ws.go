package main

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

// ============================================================================
// State WebSocket: hub + per-client pumps + broadcaster
// ============================================================================
//
// This file implements:
//   - A Hub that tracks connected WebSocket clients
//   - Per-client write pumps so one slow client doesn't block others
//   - A broadcaster loop that reads reducer-emitted state broadcasts and fans out
//
// Design constraints (project architecture):
//   - DaemonState remains daemon-owned; never expose *DaemonState to other goroutines.
//   - Initial state snapshot on connect must go through the reducer/event loop.
//   - WS broadcasts originate from reducer-emitted broadcasts (ReduceResult.Broadcasts).
//   - Slow clients must be disconnected if they can't keep up.
//
// Notes:
//   - Slow clients are disconnected when their send buffer fills.
//   - Messages are JSON text frames with an envelope: {type, ts, data}.
//   - The initial message on connect is "state_init" with StateSnapshot in data.
//
// ============================================================================

// wsMessageSnapshot is the JSON `data` payload for the WS "state_init" event.
// Keep this decoupled from internal state; expand over time (players, etc).
type wsMessageSnapshot struct {
	VolumeDB    float64   `json:"volume_db"`
	VolumeKnown bool      `json:"volume_known"`
	VolumeAt    time.Time `json:"volume_at"`

	Muted     bool      `json:"muted"`
	MuteKnown bool      `json:"mute_known"`
	MuteAt    time.Time `json:"mute_at"`
}

// wsVolumeChangedData is the JSON `data` payload for "volume_changed".
type wsVolumeChangedData struct {
	VolumeDB float64 `json:"volume_db"`
}

// wsMuteChangedData is the JSON `data` payload for "mute_changed".
type wsMuteChangedData struct {
	Muted bool `json:"muted"`
}

// wsOutboundEvent is a pre-typed, externally-consumable state event.
type wsOutboundEvent struct {
	Type string
	Data any
	At   time.Time // optional timestamp; zero means "omit" or use now
}

// envelope is the wire format envelope for WS messages.
type envelope struct {
	Type string      `json:"type"`
	Ts   *time.Time  `json:"ts,omitempty"`
	Data interface{} `json:"data,omitempty"`
}

// ============================================================================
// Hub
// ============================================================================

type Hub struct {
	logger *slog.Logger

	// Buffered broadcast channel for already-serialized JSON frames.
	broadcast  chan []byte
	register   chan *Client
	unregister chan *Client

	mu      sync.Mutex
	clients map[*Client]struct{}

	// Configuration
	sendBuf int
}

type HubConfig struct {
	// SendBuf is the per-client outbound queue size.
	// If zero, a conservative default is used.
	SendBuf int

	// BroadcastBuf is the hub inbound broadcast queue size.
	// If zero, a conservative default is used.
	BroadcastBuf int
}

// NewHub constructs a hub. Call Run(ctx) to start it.
func NewHub(logger *slog.Logger, cfg HubConfig) *Hub {
	sendBuf := cfg.SendBuf
	if sendBuf <= 0 {
		sendBuf = 32
	}
	bcastBuf := cfg.BroadcastBuf
	if bcastBuf <= 0 {
		bcastBuf = 128
	}

	return &Hub{
		logger:     logger,
		broadcast:  make(chan []byte, bcastBuf),
		register:   make(chan *Client, 64),
		unregister: make(chan *Client, 64),
		clients:    make(map[*Client]struct{}),
		sendBuf:    sendBuf,
	}
}

// Run processes hub events until ctx is canceled.
// It disconnects all clients on shutdown.
func (h *Hub) Run(ctx context.Context) {
	h.logger.Info("ws hub starting")

	for {
		select {
		case <-ctx.Done():
			h.logger.Info("ws hub stopping (context canceled)")
			h.closeAllClients()
			return

		case c := <-h.register:
			h.mu.Lock()
			h.clients[c] = struct{}{}
			n := len(h.clients)
			h.mu.Unlock()
			h.logger.Info("ws client registered", "remote_addr", c.remoteAddr, "clients", n)

		case c := <-h.unregister:
			h.removeClient(c, "unregister")

		case msg := <-h.broadcast:
			// Avoid mutating the clients map while ranging over it.
			// Collect slow clients first, then remove them after we unlock.
			var slow []*Client

			h.mu.Lock()
			for c := range h.clients {
				select {
				case c.send <- msg:
				default:
					slow = append(slow, c)
				}
			}
			h.mu.Unlock()

			for _, c := range slow {
				h.removeClient(c, "slow_client")
			}
		}
	}
}

func (h *Hub) closeAllClients() {
	h.mu.Lock()
	defer h.mu.Unlock()
	for c := range h.clients {
		if c.conn != nil {
			_ = c.conn.Close()
		}
		close(c.send)
		delete(h.clients, c)
	}
}

func (h *Hub) removeClient(c *Client, reason string) {
	h.mu.Lock()
	_, ok := h.clients[c]
	if ok {
		delete(h.clients, c)
	}
	n := len(h.clients)
	h.mu.Unlock()

	if ok {
		if c.conn != nil {
			_ = c.conn.Close()
		}
		// Closing send signals writePump to exit.
		// Guard against double-close by recovering (best-effort).
		safeCloseChan(c.send)

		h.logger.Info("ws client disconnected", "remote_addr", c.remoteAddr, "reason", reason, "clients", n)
	}
}

func safeCloseChan(ch chan []byte) {
	defer func() {
		_ = recover() // ignore "close of closed channel"
	}()
	close(ch)
}

// BroadcastBytes enqueues a pre-serialized JSON WS frame for broadcast.
// It never blocks; if the hub queue is full it drops the message.
func (h *Hub) BroadcastBytes(msg []byte) {
	select {
	case h.broadcast <- msg:
	default:
		h.logger.Warn("ws hub broadcast queue full, dropping message", "bytes", len(msg))
	}
}

// ============================================================================
// Client
// ============================================================================

type Client struct {
	hub *Hub

	conn *websocket.Conn
	send chan []byte

	remoteAddr string
	logger     *slog.Logger
}

// NewClient creates a client with a buffered send channel.
func NewClient(hub *Hub, conn *websocket.Conn, remoteAddr string, logger *slog.Logger) *Client {
	sendBuf := 32
	if hub != nil && hub.sendBuf > 0 {
		sendBuf = hub.sendBuf
	}
	return &Client{
		hub:        hub,
		conn:       conn,
		send:       make(chan []byte, sendBuf),
		remoteAddr: remoteAddr,
		logger:     logger,
	}
}

const (
	writeWait = 5 * time.Second

	// Keepalive defaults: conservative. If you already have a global WS keepalive policy,
	// tune these accordingly.
	pongWait   = 30 * time.Second
	pingPeriod = 20 * time.Second
)

// wsVolumeCoalesceWindow is the maximum time window during which bursty volume updates
// are coalesced (latest-wins) before broadcasting to clients.
const wsVolumeCoalesceWindow = 50 * time.Millisecond

// closeStatus extracts a human-readable websocket close code / text when possible.
func closeStatus(err error) (code int, text string, ok bool) {
	var ce *websocket.CloseError
	if errors.As(err, &ce) {
		return ce.Code, ce.Text, true
	}
	return 0, "", false
}

// writePump writes messages from the send queue to the websocket.
// It exits on write error or when send is closed.
func (c *Client) writePump(ctx context.Context) {
	ticker := time.NewTicker(pingPeriod)
	defer ticker.Stop()

	c.conn.SetWriteDeadline(time.Now().Add(writeWait))

	for {
		select {
		case <-ctx.Done():
			return

		case msg, ok := <-c.send:
			c.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if !ok {
				// Channel closed: hub is disconnecting us.
				_ = c.conn.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}
			if err := c.conn.WriteMessage(websocket.TextMessage, msg); err != nil {
				if !errors.Is(err, websocket.ErrCloseSent) {
					if code, text, ok := closeStatus(err); ok {
						c.logger.Info("ws writePump exiting (close)", "remote_addr", c.remoteAddr, "code", code, "reason", text)
					} else {
						c.logger.Info("ws writePump exiting (write error)", "remote_addr", c.remoteAddr, "error", err)
					}
				}
				return
			}

		case <-ticker.C:
			c.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if err := c.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				if !errors.Is(err, websocket.ErrCloseSent) {
					if code, text, ok := closeStatus(err); ok {
						c.logger.Info("ws writePump exiting (close)", "remote_addr", c.remoteAddr, "code", code, "reason", text)
					} else {
						c.logger.Info("ws writePump exiting (ping error)", "remote_addr", c.remoteAddr, "error", err)
					}
				}
				return
			}
		}
	}
}

// readPump reads and discards incoming messages to detect disconnects and handle control frames.
// It exits on read error, then unregisters the client.
func (c *Client) readPump(ctx context.Context) {
	_ = c.conn.SetReadDeadline(time.Now().Add(pongWait))
	c.conn.SetPongHandler(func(string) error {
		_ = c.conn.SetReadDeadline(time.Now().Add(pongWait))
		return nil
	})

	for {
		select {
		case <-ctx.Done():
			return
		default:
			// Continue to read.
		}

		_, _, err := c.conn.ReadMessage()
		if err != nil {
			// Normal close is expected on client disconnect.
			if !errors.Is(err, websocket.ErrCloseSent) {
				if code, text, ok := closeStatus(err); ok {
					c.logger.Info("ws readPump exiting (close)", "remote_addr", c.remoteAddr, "code", code, "reason", text)
				} else {
					c.logger.Info("ws readPump exiting (read error)", "remote_addr", c.remoteAddr, "error", err)
				}
			}

			if c.hub != nil {
				c.hub.unregister <- c
			}
			return
		}
	}
}

// ============================================================================
// HTTP Handler + server wiring helpers
// ============================================================================

type Server struct {
	logger *slog.Logger

	hub *Hub

	// Required for initial snapshot request on connect (through reducer/event loop).
	events chan<- Event
}

type ServerConfig struct {
	Hub HubConfig
}

// NewServer constructs the WS state server components. Call Register on a mux,
// start hub.Run(ctx), and start broadcaster loop.
func NewServer(logger *slog.Logger, events chan<- Event, cfg ServerConfig) *Server {
	hub := NewHub(logger, cfg.Hub)
	return &Server{
		logger: logger,
		hub:    hub,
		events: events,
	}
}

func (s *Server) Hub() *Hub { return s.hub }

// Register registers the WS handler on the provided mux.
func (s *Server) Register(mux *http.ServeMux, path string) {
	if mux == nil {
		return
	}
	mux.HandleFunc(path, s.handleStateWS)
}

var upgrader = websocket.Upgrader{
	// NOTE: If you need stricter origin checks, implement them at integration time.
	CheckOrigin: func(r *http.Request) bool { return true },
}

// handleStateWS upgrades and registers a client, then sends state_init.
func (s *Server) handleStateWS(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		s.logger.Warn("ws upgrade failed", "error", err)
		return
	}

	client := NewClient(s.hub, conn, r.RemoteAddr, s.logger)

	// Register client first so broadcasts can reach it.
	s.hub.register <- client

	// Start pumps.
	//
	// IMPORTANT:
	// Do not tie the pumps to the HTTP request context (r.Context()).
	// net/http cancels the request context when the handler returns, which would
	// prematurely stop the pumps and cause abnormal WS closures (e.g. code 1006).
	// The connection lifetime is instead managed by the hub (close/unregister) and
	// by the websocket read/write errors.
	go client.writePump(context.Background())
	go client.readPump(context.Background())

	// Request snapshot for initial state_init message (through reducer/event loop).
	// Use the HTTP request context here so it cancels if the client disconnects
	// during the snapshot round-trip.
	if s.events != nil {
		reply := make(chan StateSnapshot, 1)

		select {
		case <-r.Context().Done():
			return
		case s.events <- RequestStateSnapshot{Reply: reply}:
		}

		waitCtx := r.Context()
		if _, has := r.Context().Deadline(); !has {
			var cancel context.CancelFunc
			waitCtx, cancel = context.WithTimeout(r.Context(), 1*time.Second)
			defer cancel()
		}

		select {
		case <-waitCtx.Done():
			if !errors.Is(waitCtx.Err(), context.Canceled) {
				s.logger.Warn("ws snapshot request failed", "error", waitCtx.Err())
			}
			return

		case snap := <-reply:
			payload := wsMessageSnapshot{
				VolumeDB:    snap.VolumeDB,
				VolumeKnown: snap.VolumeKnown,
				VolumeAt:    snap.VolumeAt,
				Muted:       snap.Muted,
				MuteKnown:   snap.MuteKnown,
				MuteAt:      snap.MuteAt,
			}

			now := time.Now().UTC()
			initMsg, mErr := json.Marshal(envelope{
				Type: "state_init",
				Ts:   &now,
				Data: payload,
			})
			if mErr == nil {
				// Enqueue init message; if client is already slow, disconnect.
				select {
				case client.send <- initMsg:
				default:
					s.hub.unregister <- client
					return
				}
			}
		}
	}
}

// ============================================================================
// Broadcaster
// ============================================================================

// RunBroadcaster reads reducer-emitted StateBroadcast events, marshals them, and broadcasts
// them to all hub clients. Intended to run as a single goroutine.
func RunBroadcaster(ctx context.Context, hub *Hub, src <-chan StateBroadcast, logger *slog.Logger) {
	if hub == nil || src == nil {
		return
	}

	// Rate-limit bursty volume updates: flush latest pending volume at most once every
	// wsVolumeCoalesceWindow, even if updates keep arriving (no debounce-on-silence).
	var pendingVol *wsOutboundEvent
	var volTimer *time.Timer
	var volTimerCh <-chan time.Time

	flushPendingVol := func() {
		if pendingVol == nil {
			return
		}

		ts := pendingVol.At
		if ts.IsZero() {
			ts = time.Now().UTC()
		}

		msg, err := json.Marshal(envelope{
			Type: pendingVol.Type,
			Ts:   &ts,
			Data: pendingVol.Data,
		})
		if err != nil {
			logger.Warn("ws broadcaster marshal failed", "error", err, "type", pendingVol.Type)
			// Drop the pending item so we don't retry-marshal forever.
			pendingVol = nil
			return
		}

		hub.BroadcastBytes(msg)
		pendingVol = nil
	}

	stopVolTimer := func() {
		if volTimer == nil {
			volTimerCh = nil
			return
		}
		if !volTimer.Stop() {
			// Drain if needed.
			select {
			case <-volTimer.C:
			default:
			}
		}
		volTimerCh = nil
		volTimer = nil
	}

	startVolTimerIfNeeded := func() {
		if volTimer != nil {
			return
		}
		volTimer = time.NewTimer(wsVolumeCoalesceWindow)
		volTimerCh = volTimer.C
	}

	resetVolTimer := func() {
		// Timer must already exist.
		if volTimer == nil {
			return
		}
		if !volTimer.Stop() {
			select {
			case <-volTimer.C:
			default:
			}
		}
		volTimer.Reset(wsVolumeCoalesceWindow)
		volTimerCh = volTimer.C
	}

	for {
		select {
		case <-ctx.Done():
			// Best-effort: flush pending volume update before exit.
			flushPendingVol()
			stopVolTimer()
			return

		case <-volTimerCh:
			// Timer tick: flush latest pending volume if present.
			flushPendingVol()
			// Keep ticking only if more volume updates are pending; otherwise stop.
			if pendingVol == nil {
				stopVolTimer()
			} else {
				resetVolTimer()
			}

		case b, ok := <-src:
			if !ok {
				// If the source ends, flush any pending coalesced volume update then stop.
				flushPendingVol()
				stopVolTimer()
				logger.Info("ws broadcaster stopping (source ended)")
				return
			}

			ev, ok := convertBroadcast(b)
			if !ok {
				// Unknown broadcasts are dropped.
				continue
			}

			// Rate-limit only volume_changed; do NOT reset the timer on each update.
			// Latest-wins: replace pending event and ensure the periodic timer is running.
			if ev.Type == "volume_changed" {
				copyEv := ev
				pendingVol = &copyEv
				startVolTimerIfNeeded()
				continue
			}

			// Non-volume event: flush pending volume first, then emit this event immediately.
			flushPendingVol()
			stopVolTimer()

			ts := ev.At
			if ts.IsZero() {
				ts = time.Now().UTC()
			}

			msg, err := json.Marshal(envelope{
				Type: ev.Type,
				Ts:   &ts,
				Data: ev.Data,
			})
			if err != nil {
				logger.Warn("ws broadcaster marshal failed", "error", err, "type", ev.Type)
				continue
			}

			hub.BroadcastBytes(msg)
		}
	}
}

func convertBroadcast(b StateBroadcast) (wsOutboundEvent, bool) {
	switch ev := b.(type) {
	case BroadcastVolumeChanged:
		return wsOutboundEvent{
			Type: "volume_changed",
			Data: wsVolumeChangedData{VolumeDB: ev.VolumeDB},
			At:   ev.At,
		}, true

	case BroadcastMuteChanged:
		return wsOutboundEvent{
			Type: "mute_changed",
			Data: wsMuteChangedData{Muted: ev.Muted},
			At:   ev.At,
		}, true

	default:
		return wsOutboundEvent{}, false
	}
}
