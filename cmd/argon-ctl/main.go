package main

import (
	"encoding/json"
	"fmt"
	"net"
	"os"
)

// ============================================================================
// argon-ctl - Command-line IPC Client
// ============================================================================
// This tool sends commands to the argon-camilladsp-remote daemon via IPC.
//
// Usage:
//   argon-ctl volume-up
//   argon-ctl volume-down
//   argon-ctl mute
//   argon-ctl set-volume -45.5
//   argon-ctl release
//
// Options:
//   -socket PATH    Unix domain socket path (default: /tmp/argon-camilladsp.sock)
// ============================================================================

// Action types (duplicated from main package for standalone binary)
type Action interface{}

type VolumeHeld struct {
	Direction int `json:"direction"`
}

type VolumeRelease struct{}

type ToggleMute struct{}

type SetVolumeAbsolute struct {
	Db     float64 `json:"db"`
	Origin string  `json:"origin"`
}

// ActionEnvelope wraps actions for JSON
type ActionEnvelope struct {
	Type string          `json:"type"`
	Data json.RawMessage `json:"data,omitempty"`
}

// IPCResponse represents the daemon's response
type IPCResponse struct {
	Status string `json:"status"`
	Error  string `json:"error,omitempty"`
}

func main() {
	socketPath := "/tmp/argon-camilladsp.sock"

	// Parse arguments
	args := os.Args[1:]
	if len(args) == 0 {
		printUsage()
		os.Exit(1)
	}

	// Check for -socket flag
	if args[0] == "-socket" || args[0] == "--socket" {
		if len(args) < 2 {
			fmt.Fprintf(os.Stderr, "error: -socket requires an argument\n")
			os.Exit(1)
		}
		socketPath = args[1]
		args = args[2:]
	}

	if len(args) == 0 {
		printUsage()
		os.Exit(1)
	}

	// Parse command
	var action Action

	switch args[0] {
	case "volume-up", "up":
		action = VolumeHeld{Direction: 1}

	case "volume-down", "down":
		action = VolumeHeld{Direction: -1}

	case "release":
		action = VolumeRelease{}

	case "mute", "toggle-mute":
		action = ToggleMute{}

	case "set-volume", "set":
		if len(args) < 2 {
			fmt.Fprintf(os.Stderr, "error: set-volume requires a dB value\n")
			os.Exit(1)
		}
		var db float64
		_, err := fmt.Sscanf(args[1], "%f", &db)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: invalid dB value: %v\n", err)
			os.Exit(1)
		}
		action = SetVolumeAbsolute{Db: db, Origin: "argon-ctl"}

	case "help", "-h", "--help":
		printUsage()
		os.Exit(0)

	default:
		fmt.Fprintf(os.Stderr, "error: unknown command: %s\n", args[0])
		printUsage()
		os.Exit(1)
	}

	// Send action
	if err := sendAction(socketPath, action); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}

	fmt.Println("ok")
}

func sendAction(socketPath string, action Action) error {
	// Connect to socket
	conn, err := net.Dial("unix", socketPath)
	if err != nil {
		return fmt.Errorf("connect to %s: %w", socketPath, err)
	}
	defer conn.Close()

	// Marshal action
	data, err := marshalAction(action)
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

func marshalAction(action Action) ([]byte, error) {
	var env ActionEnvelope

	switch a := action.(type) {
	case VolumeHeld:
		env.Type = "volume_held"
		data, err := json.Marshal(a)
		if err != nil {
			return nil, fmt.Errorf("marshal VolumeHeld: %w", err)
		}
		env.Data = data

	case VolumeRelease:
		env.Type = "volume_release"

	case ToggleMute:
		env.Type = "toggle_mute"

	case SetVolumeAbsolute:
		env.Type = "set_volume_absolute"
		data, err := json.Marshal(a)
		if err != nil {
			return nil, fmt.Errorf("marshal SetVolumeAbsolute: %w", err)
		}
		env.Data = data

	default:
		return nil, fmt.Errorf("unknown action type: %T", action)
	}

	return json.Marshal(env)
}

func printUsage() {
	fmt.Fprintf(os.Stderr, `argon-ctl - Control argon-camilladsp-remote daemon via IPC

Usage:
  argon-ctl [options] <command> [args]

Options:
  -socket PATH    Unix domain socket path (default: /tmp/argon-camilladsp.sock)

Commands:
  volume-up, up           Simulate volume up button press
  volume-down, down       Simulate volume down button press
  release                 Simulate button release
  mute, toggle-mute       Toggle mute state
  set-volume, set <dB>    Set absolute volume in dB (e.g., -45.5)
  help, -h, --help        Show this help message

Examples:
  argon-ctl mute
  argon-ctl set-volume -30.0
  argon-ctl -socket /var/run/argon.sock volume-up
`)
}
