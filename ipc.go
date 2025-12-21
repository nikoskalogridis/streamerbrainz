package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"os"
	"strings"
)

// ============================================================================
// IPC Server - Unix Domain Socket Interface
// ============================================================================
// The IPC server allows external clients to send JSON actions to the daemon
// via a Unix domain socket. This enables:
//   - Remote control via command-line tools
//   - Integration with librespot and other audio sources
//   - UI/Web interface control
//   - Scripting and automation
//
// Protocol: Line-delimited JSON
//   - Client sends: {"type": "action_name", "data": {...}}
//   - Server responds: {"status": "ok"} or {"status": "error", "error": "msg"}
// ============================================================================

// IPCResponse represents the response sent back to IPC clients
type IPCResponse struct {
	Status string `json:"status"`          // "ok" or "error"
	Error  string `json:"error,omitempty"` // error message if status == "error"
}

// runIPCServer starts the Unix domain socket server
func runIPCServer(socketPath string, actions chan<- Action, verbose bool) error {
	// Remove existing socket file if it exists
	if err := os.RemoveAll(socketPath); err != nil {
		return fmt.Errorf("remove existing socket: %w", err)
	}

	// Create Unix domain socket listener
	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		return fmt.Errorf("listen on %s: %w", socketPath, err)
	}

	// Make socket accessible (consider security implications in production)
	if err := os.Chmod(socketPath, 0666); err != nil {
		listener.Close()
		return fmt.Errorf("chmod socket: %w", err)
	}

	if verbose {
		log.Printf("[IPC] listening on %s", socketPath)
	}

	// Accept connections in a loop
	go func() {
		defer listener.Close()
		defer os.Remove(socketPath)

		for {
			conn, err := listener.Accept()
			if err != nil {
				// Check if listener was closed (e.g., during shutdown)
				if strings.Contains(err.Error(), "use of closed network connection") {
					if verbose {
						log.Printf("[IPC] listener closed")
					}
					return
				}
				log.Printf("[IPC] accept error: %v", err)
				continue
			}

			// Handle connection in a separate goroutine
			go handleIPCConnection(conn, actions, verbose)
		}
	}()

	return nil
}

// handleIPCConnection processes a single IPC client connection
func handleIPCConnection(conn net.Conn, actions chan<- Action, verbose bool) {
	defer conn.Close()

	if verbose {
		log.Printf("[IPC] connection from %s", conn.RemoteAddr())
	}

	scanner := bufio.NewScanner(conn)
	encoder := json.NewEncoder(conn)

	for scanner.Scan() {
		line := scanner.Text()
		if verbose {
			log.Printf("[IPC] received: %s", line)
		}

		// Parse action from JSON
		action, err := UnmarshalAction([]byte(line))
		if err != nil {
			response := IPCResponse{
				Status: "error",
				Error:  fmt.Sprintf("parse action: %v", err),
			}
			if encErr := encoder.Encode(response); encErr != nil {
				log.Printf("[IPC] failed to send error response: %v", encErr)
			}
			continue
		}

		// Send action to daemon
		select {
		case actions <- action:
			// Action queued successfully
			response := IPCResponse{Status: "ok"}
			if encErr := encoder.Encode(response); encErr != nil {
				log.Printf("[IPC] failed to send success response: %v", encErr)
			}
		default:
			// Action channel is full (should rarely happen with buffer)
			response := IPCResponse{
				Status: "error",
				Error:  "action queue full",
			}
			if encErr := encoder.Encode(response); encErr != nil {
				log.Printf("[IPC] failed to send error response: %v", encErr)
			}
		}
	}

	if err := scanner.Err(); err != nil {
		log.Printf("[IPC] scanner error: %v", err)
	}

	if verbose {
		log.Printf("[IPC] connection closed")
	}
}

// ============================================================================
// IPC Client Utility Functions
// ============================================================================
// These functions can be used to send actions to the daemon from external
// programs or for testing.
// ============================================================================

// SendIPCAction sends an action to the daemon via IPC and returns the response
func SendIPCAction(socketPath string, action Action) error {
	// Connect to socket
	conn, err := net.Dial("unix", socketPath)
	if err != nil {
		return fmt.Errorf("connect to %s: %w", socketPath, err)
	}
	defer conn.Close()

	// Marshal action
	data, err := MarshalAction(action)
	if err != nil {
		return fmt.Errorf("marshal action: %w", err)
	}

	// Send action (line-delimited JSON)
	if _, err := fmt.Fprintf(conn, "%s\n", data); err != nil {
		return fmt.Errorf("send action: %w", err)
	}

	// Read response
	var response IPCResponse
	decoder := json.NewDecoder(conn)
	if err := decoder.Decode(&response); err != nil {
		return fmt.Errorf("decode response: %w", err)
	}

	// Check response status
	if response.Status == "error" {
		return fmt.Errorf("daemon error: %s", response.Error)
	}

	return nil
}
