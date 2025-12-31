package main

import (
	"context"
	"log/slog"
	"testing"
	"time"
)

// NOTE: These tests focus on hub behavior (fanout + slow-client disconnection)
// without standing up a real websocket server.
//
// We intentionally avoid relying on network I/O. We construct Clients with a nil
// websocket.Conn and ensure our test paths never require actual writes.
// For slow-client eviction, the hub calls conn.Close(); nil is safe (hub guards against nil).

// newTestHub returns a hub with small buffers for deterministic tests.
func newTestHub(t *testing.T, sendBuf int, broadcastBuf int) *Hub {
	t.Helper()
	return NewHub(slog.Default(), HubConfig{
		SendBuf:      sendBuf,
		BroadcastBuf: broadcastBuf,
	})
}

func TestHub_BroadcastDeliveredToAllClients(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	hub := newTestHub(t, 4, 8)

	// Run the hub loop.
	done := make(chan struct{})
	go func() {
		defer close(done)
		hub.Run(ctx)
	}()

	// Create two clients with buffered send channels and nil conns (not used in this test).
	c1 := &Client{
		hub:        hub,
		conn:       nil,
		send:       make(chan []byte, 4),
		remoteAddr: "c1",
		logger:     slog.Default(),
	}
	c2 := &Client{
		hub:        hub,
		conn:       nil,
		send:       make(chan []byte, 4),
		remoteAddr: "c2",
		logger:     slog.Default(),
	}

	// Ensure registrations have been processed by the hub goroutine before broadcasting.
	hub.register <- c1
	waitUntil(t, 500*time.Millisecond, func() bool {
		hub.mu.Lock()
		defer hub.mu.Unlock()
		_, ok := hub.clients[c1]
		return ok
	}, "client1 not registered in time")

	hub.register <- c2
	waitUntil(t, 500*time.Millisecond, func() bool {
		hub.mu.Lock()
		defer hub.mu.Unlock()
		_, ok := hub.clients[c2]
		return ok
	}, "client2 not registered in time")

	msg := []byte(`{"type":"volume_changed","data":{"volume_db":-12.0}}`)

	// Avoid BroadcastBytes() here because it is intentionally non-blocking and may
	// drop if the hub broadcast queue is temporarily full during scheduling.
	hub.broadcast <- msg

	// Both clients should receive the message.
	select {
	case got := <-c1.send:
		if string(got) != string(msg) {
			t.Fatalf("client1 got %q, want %q", string(got), string(msg))
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatalf("timeout waiting for client1 to receive broadcast")
	}

	select {
	case got := <-c2.send:
		if string(got) != string(msg) {
			t.Fatalf("client2 got %q, want %q", string(got), string(msg))
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatalf("timeout waiting for client2 to receive broadcast")
	}

	// Shutdown hub.
	cancel()
	select {
	case <-done:
	case <-time.After(1 * time.Second):
		t.Fatalf("timeout waiting for hub to stop")
	}
}

func TestHub_SlowClientDisconnectedOnFullSendBuffer(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// sendBuf=1 so we can fill it easily; broadcastBuf ample.
	hub := newTestHub(t, 1, 8)

	done := make(chan struct{})
	go func() {
		defer close(done)
		hub.Run(ctx)
	}()

	// Slow client: send buffer will fill and we never drain it.
	slow := &Client{
		hub:        hub,
		conn:       nil,
		send:       make(chan []byte, 1),
		remoteAddr: "slow",
		logger:     slog.Default(),
	}

	// Fast client: we will drain its channel.
	fast := &Client{
		hub:        hub,
		conn:       nil,
		send:       make(chan []byte, 8),
		remoteAddr: "fast",
		logger:     slog.Default(),
	}

	// Ensure registrations have been processed by the hub goroutine before broadcasting.
	hub.register <- slow
	waitUntil(t, 500*time.Millisecond, func() bool {
		hub.mu.Lock()
		defer hub.mu.Unlock()
		_, ok := hub.clients[slow]
		return ok
	}, "slow client not registered in time")

	hub.register <- fast
	waitUntil(t, 500*time.Millisecond, func() bool {
		hub.mu.Lock()
		defer hub.mu.Unlock()
		_, ok := hub.clients[fast]
		return ok
	}, "fast client not registered in time")

	// Pre-fill slow client buffer to simulate it being stuck.
	slow.send <- []byte(`"already queued"`)

	// Broadcast should attempt to enqueue to slow, hit default, and disconnect it,
	// while still delivering to fast.
	msg := []byte(`{"type":"mute_changed","data":{"muted":true}}`)

	// Avoid BroadcastBytes() here for the same reason as above; we want deterministic delivery
	// into the hub's select loop.
	hub.broadcast <- msg

	select {
	case got := <-fast.send:
		if string(got) != string(msg) {
			t.Fatalf("fast client got %q, want %q", string(got), string(msg))
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatalf("timeout waiting for fast client to receive broadcast")
	}

	// The slow client should be disconnected and its send channel should be closed.
	// (There may still be the pre-filled message in the buffer; drain it first.)
	select {
	case <-slow.send:
	default:
	}

	waitUntil(t, 750*time.Millisecond, func() bool {
		select {
		case _, ok := <-slow.send:
			return !ok
		default:
			return false
		}
	}, "expected slow send channel to be closed")
}

func waitUntil(t *testing.T, timeout time.Duration, cond func() bool, msg string) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timeout: %s", msg)
}
