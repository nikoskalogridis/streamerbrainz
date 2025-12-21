package main

import (
	"flag"
	"log"
	"os"
	"os/signal"
	"syscall"
)

func main() {
	// Check for subcommand mode (librespot hook)
	if len(os.Args) > 1 && os.Args[1] == "librespot-hook" {
		runLibrespotSubcommand()
		return
	}

	// Parse command-line flags
	var (
		inputDev    = flag.String("input", "/dev/input/event6", "Linux input event device for IR (e.g. /dev/input/event6)")
		wsURL       = flag.String("ws", "ws://127.0.0.1:1234", "CamillaDSP websocket URL (CamillaDSP must be started with -pPORT)")
		socketPath  = flag.String("socket", "/tmp/argon-camilladsp.sock", "Unix domain socket path for IPC")
		minDB       = flag.Float64("min", -65.0, "Minimum volume clamp in dB")
		maxDB       = flag.Float64("max", 0.0, "Maximum volume clamp in dB")
		updateHz    = flag.Int("update-hz", defaultUpdateHz, "Update loop frequency in Hz")
		velMax      = flag.Float64("vel-max", defaultVelMaxDBPerS, "Maximum velocity in dB/s")
		accelTime   = flag.Float64("accel-time", defaultAccelTime, "Time to reach max velocity in seconds")
		decayTau    = flag.Float64("decay-tau", defaultDecayTau, "Velocity decay time constant in seconds")
		readTimeout = flag.Int("read-timeout-ms", defaultReadTimeoutMS, "Timeout in milliseconds for reading websocket responses")
		verbose     = flag.Bool("v", false, "Verbose logging")
	)
	flag.Parse()

	// Validate flags
	if *minDB > *maxDB {
		log.Fatalf("-min must be <= -max")
	}
	if *updateHz <= 0 || *updateHz > 1000 {
		log.Fatalf("-update-hz must be between 1 and 1000")
	}

	// Open input device
	f, err := os.Open(*inputDev)
	if err != nil {
		log.Fatalf("open input device %s: %v (tip: run as root or add user to 'input' group)", *inputDev, err)
	}
	defer f.Close()

	// Prepare websocket client
	ws := newWSClient(*wsURL)
	connectWithRetry(ws, *wsURL, *verbose)

	// Get initial volume from server
	initialVol, err := getCurrentVolume(ws, *readTimeout, *verbose)
	if err != nil {
		log.Printf("Warning: could not get initial volume: %v", err)
		log.Printf("Setting server volume to safe default: %.1f dB", safeDefaultDB)

		// Actively set the server to a safe default volume
		_, setErr := setVolumeCommand(ws, safeDefaultDB, *readTimeout, *verbose)
		if setErr != nil {
			log.Printf("Error: failed to set safe default volume: %v", setErr)
			log.Printf("Warning: cannot verify server volume - proceeding with caution")
		}
		initialVol = safeDefaultDB
	}

	// Initialize velocity state with known volume
	velState := newVelocityState(*velMax, *accelTime, *decayTau, *minDB, *maxDB)
	velState.updateVolume(initialVol)

	// Handle shutdown
	sigc := make(chan os.Signal, 1)
	signal.Notify(sigc, syscall.SIGINT, syscall.SIGTERM)

	// Create action channel - central command bus
	actions := make(chan Action, 64)

	// Start daemon brain in a goroutine
	go runDaemon(actions, ws, *wsURL, velState, *updateHz, *readTimeout, *verbose)

	// Start IPC server
	if err := runIPCServer(*socketPath, actions, *verbose); err != nil {
		log.Fatalf("start IPC server: %v", err)
	}

	// Read loop for input events
	events := make(chan inputEvent, 64)
	readErr := make(chan error, 1)
	go readInputEvents(f, events, readErr)

	log.Printf("listening on %s, IPC on %s, sending to %s (update rate: %d Hz)", *inputDev, *socketPath, *wsURL, *updateHz)

	// ============================================================================
	// Main Event Loop - Input Coordination Only
	// ============================================================================
	// This loop now only handles:
	//   - Shutdown signals
	//   - Input errors
	//   - Translating IR events into Actions
	//
	// The daemon brain (runDaemon) handles all state updates and CamillaDSP.
	// ============================================================================

	for {
		select {
		// --------------------------------------------------------------------
		// Shutdown handling
		// --------------------------------------------------------------------
		case <-sigc:
			log.Printf("shutting down")
			ws.close()
			f.Close()
			return

		// --------------------------------------------------------------------
		// Input error handling
		// --------------------------------------------------------------------
		case err := <-readErr:
			log.Printf("input reader stopped: %v", err)
			ws.close()
			f.Close()
			return

		// --------------------------------------------------------------------
		// IR input event handling (event translation layer)
		// --------------------------------------------------------------------
		// IR events are translated into Actions and sent to daemon brain
		case ev := <-events:
			// Filter non-key events
			if ev.Type != EV_KEY {
				continue
			}

			// Translate IR events into Actions
			switch ev.Code {
			case KEY_VOLUMEUP:
				if ev.Value == evValuePress || ev.Value == evValueRepeat {
					actions <- VolumeHeld{Direction: 1}
				} else if ev.Value == evValueRelease {
					actions <- VolumeRelease{}
				}

			case KEY_VOLUMEDOWN:
				if ev.Value == evValuePress || ev.Value == evValueRepeat {
					actions <- VolumeHeld{Direction: -1}
				} else if ev.Value == evValueRelease {
					actions <- VolumeRelease{}
				}

			case KEY_MUTE:
				if ev.Value == evValuePress {
					actions <- ToggleMute{}
				}
			}
		}
	}
}

// runLibrespotSubcommand handles librespot-hook subcommand mode
func runLibrespotSubcommand() {
	// Create a new flagset for librespot subcommand
	fs := flag.NewFlagSet("librespot-hook", flag.ExitOnError)
	socketPath := fs.String("socket", "/tmp/argon-camilladsp.sock", "Unix domain socket path for IPC")
	minDB := fs.Float64("min", -65.0, "Minimum volume clamp in dB")
	maxDB := fs.Float64("max", 0.0, "Maximum volume clamp in dB")
	verbose := fs.Bool("v", false, "Verbose logging")

	// Parse flags (skip "librespot-hook" subcommand name)
	fs.Parse(os.Args[2:])

	// Run hook handler (reads from environment variables)
	if err := runLibrespotHook(*socketPath, *minDB, *maxDB, *verbose); err != nil {
		log.Fatalf("librespot hook error: %v", err)
	}
}
