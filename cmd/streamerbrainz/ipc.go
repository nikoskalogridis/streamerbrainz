package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"os"
	"strings"
)

// ============================================================================
// IPC Server - Unix Domain Socket Interface
// ============================================================================
// The IPC server allows external clients to send JSON events to the daemon
// via a Unix domain socket. This enables:
//   - Remote control via command-line tools
//   - Integration with librespot and other audio sources
//   - UI/Web interface control
//   - Scripting and automation
//
// Protocol: Line-delimited JSON
//   - Client sends: {"type": "event_name", "data": {...}}
//   - Server responds: {"status": "ok"} or {"status": "error", "error": "msg"}
// ============================================================================

// IPCResponse represents the response sent back to IPC clients
type IPCResponse struct {
	Status string `json:"status"`          // "ok" or "error"
	Error  string `json:"error,omitempty"` // error message if status == "error"
}

// runIPCServer starts the Unix domain socket server.
// It runs until ctx is canceled, at which point it closes the listener and exits.
//
// This function is context-aware so the main program can implement proper shutdown semantics.
func runIPCServer(ctx context.Context, socketPath string, events chan<- Event, logger *slog.Logger) error {
	// Remove existing socket file if it exists
	if err := os.RemoveAll(socketPath); err != nil {
		return fmt.Errorf("remove existing socket: %w", err)
	}

	// Create Unix domain socket listener
	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		return fmt.Errorf("listen on %s: %w", socketPath, err)
	}
	defer listener.Close()
	defer os.Remove(socketPath)

	// Make socket accessible (consider security implications in production)
	if err := os.Chmod(socketPath, 0666); err != nil {
		return fmt.Errorf("chmod socket: %w", err)
	}

	logger.Info("IPC listening", "socket", socketPath)

	// Close the listener on shutdown. This unblocks Accept().
	go func() {
		<-ctx.Done()
		_ = listener.Close()
	}()

	for {
		conn, err := listener.Accept()
		if err != nil {
			// Exit cleanly on shutdown/close.
			if ctx.Err() != nil {
				logger.Debug("IPC listener closed (shutdown)")
				return nil
			}

			// Some platforms return net.ErrClosed; keep this defensive.
			if errors.Is(err, net.ErrClosed) || strings.Contains(err.Error(), "use of closed network connection") {
				logger.Debug("IPC listener closed")
				return nil
			}

			logger.Error("IPC accept error", "error", err)
			continue
		}

		// Handle connection in a separate goroutine.
		go handleIPCConnection(conn, events, logger)
	}
}

// handleIPCConnection processes a single IPC client connection
// handleIPCConnection handles a single IPC connection
func handleIPCConnection(conn net.Conn, events chan<- Event, logger *slog.Logger) {
	defer conn.Close()

	logger.Debug("IPC connection", "remote_addr", conn.RemoteAddr())

	scanner := bufio.NewScanner(conn)
	encoder := json.NewEncoder(conn)

	for scanner.Scan() {
		line := scanner.Text()
		logger.Debug("IPC received", "line", line)

		// Parse event from JSON (payload events only; daemon assigns timestamps via TimedEvent)
		ev, err := UnmarshalEvent([]byte(line))
		if err != nil {
			response := IPCResponse{
				Status: "error",
				Error:  fmt.Sprintf("parse event: %v", err),
			}
			if encErr := encoder.Encode(response); encErr != nil {
				logger.Error("IPC failed to send error response", "error", encErr)
			}
			continue
		}

		// Send event to daemon
		select {
		case events <- ev:
			// Event queued successfully
			response := IPCResponse{Status: "ok"}
			if encErr := encoder.Encode(response); encErr != nil {
				logger.Error("IPC failed to send success response", "error", encErr)
			}
		default:
			// Event channel is full (should rarely happen with buffer)
			response := IPCResponse{
				Status: "error",
				Error:  "event queue full",
			}
			if encErr := encoder.Encode(response); encErr != nil {
				logger.Error("IPC failed to send error response", "error", encErr)
			}
		}
	}

	logger.Debug("IPC connection closed")
}

// ============================================================================
// IPC Client Utility Functions
// ============================================================================
// These functions can be used to send events to the daemon from external
// programs or for testing.
// ============================================================================

// SendIPCEvent sends an event to the daemon via IPC and returns the response
func SendIPCEvent(socketPath string, ev Event) error {
	// Connect to socket
	conn, err := net.Dial("unix", socketPath)
	if err != nil {
		return fmt.Errorf("connect to %s: %w", socketPath, err)
	}
	defer conn.Close()

	// Marshal event
	data, err := MarshalEvent(ev)
	if err != nil {
		return fmt.Errorf("marshal event: %w", err)
	}

	// Send event
	if _, err := fmt.Fprintf(conn, "%s\n", strings.TrimSpace(string(data))); err != nil {
		return fmt.Errorf("send event: %w", err)
	}

	// Read response
	decoder := json.NewDecoder(conn)
	var resp IPCResponse
	if err := decoder.Decode(&resp); err != nil {
		return fmt.Errorf("decode response: %w", err)
	}

	if resp.Status != "ok" {
		return fmt.Errorf("ipc error: %s", resp.Error)
	}

	return nil
}
