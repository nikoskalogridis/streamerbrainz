package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
)

const version = "1.0.0"

func printVersion() {
	fmt.Printf("argon-camilladsp-remote v%s\n", version)
	fmt.Println("IR remote control daemon for CamillaDSP volume control")
}

func printUsage() {
	printVersion()
	fmt.Println()
	fmt.Println("USAGE:")
	fmt.Println("  argon-camilladsp-remote [OPTIONS]")
	fmt.Println("  argon-camilladsp-remote librespot-hook [OPTIONS]")
	fmt.Println()
	fmt.Println("DESCRIPTION:")
	fmt.Println("  Daemon that bridges IR remote control events (via Linux input devices)")
	fmt.Println("  to CamillaDSP volume control over WebSocket. Features velocity-based")
	fmt.Println("  volume ramping for smooth control and librespot integration.")
	fmt.Println()
	fmt.Println("OPTIONS:")
	fmt.Println("  -input string")
	fmt.Printf("        Linux input event device for IR remote (default \"/dev/input/event6\")\n")
	fmt.Println()
	fmt.Println("  -ws string")
	fmt.Printf("        CamillaDSP websocket URL (default \"ws://127.0.0.1:1234\")\n")
	fmt.Println("        Note: CamillaDSP must be started with -pPORT option")
	fmt.Println()
	fmt.Println("  -socket string")
	fmt.Printf("        Unix domain socket path for IPC (default \"/tmp/argon-camilladsp.sock\")\n")
	fmt.Println()
	fmt.Println("  -min float")
	fmt.Printf("        Minimum volume clamp in dB (default -65.0)\n")
	fmt.Println()
	fmt.Println("  -max float")
	fmt.Printf("        Maximum volume clamp in dB (default 0.0)\n")
	fmt.Println()
	fmt.Println("  -update-hz int")
	fmt.Printf("        Update loop frequency in Hz (default %d)\n", defaultUpdateHz)
	fmt.Println()
	fmt.Println("  -vel-max float")
	fmt.Printf("        Maximum velocity in dB/s (default %.1f)\n", defaultVelMaxDBPerS)
	fmt.Println()
	fmt.Println("  -accel-time float")
	fmt.Printf("        Time to reach max velocity in seconds (default %.1f)\n", defaultAccelTime)
	fmt.Println()
	fmt.Println("  -decay-tau float")
	fmt.Printf("        Velocity decay time constant in seconds (default %.1f)\n", defaultDecayTau)
	fmt.Println()
	fmt.Println("  -read-timeout-ms int")
	fmt.Printf("        Timeout for websocket responses in ms (default %d)\n", defaultReadTimeoutMS)
	fmt.Println()
	fmt.Println("  -v    Enable verbose logging")
	fmt.Println()
	fmt.Println("  -version")
	fmt.Println("        Print version and exit")
	fmt.Println()
	fmt.Println("  -help")
	fmt.Println("        Print this help message")
	fmt.Println()
	fmt.Println("SUBCOMMANDS:")
	fmt.Println("  librespot-hook")
	fmt.Println("        Run as librespot event hook (reads PLAYER_EVENT from environment)")
	fmt.Println("        Options: -socket, -min, -max, -v")
	fmt.Println()
	fmt.Println("EXAMPLES:")
	fmt.Println("  # Start daemon with default settings")
	fmt.Println("  argon-camilladsp-remote")
	fmt.Println()
	fmt.Println("  # Custom input device and volume range")
	fmt.Println("  argon-camilladsp-remote -input /dev/input/event4 -min -80 -max -10")
	fmt.Println()
	fmt.Println("  # Connect to remote CamillaDSP instance")
	fmt.Println("  argon-camilladsp-remote -ws ws://192.168.1.100:1234")
	fmt.Println()
	fmt.Println("  # Use as librespot hook (add to librespot config)")
	fmt.Println("  onevent = argon-camilladsp-remote librespot-hook")
	fmt.Println()
	fmt.Println("NOTES:")
	fmt.Println("  - Requires read access to input device (run as root or add user to 'input' group)")
	fmt.Println("  - CamillaDSP must be running with websocket enabled (-pPORT)")
	fmt.Println("  - Volume changes use velocity-based ramping for smooth control")
	fmt.Println("  - Safety zone active above -12dB (slower ramping)")
	fmt.Println()
}

func main() {
	// Check for subcommand mode (librespot hook) first
	if len(os.Args) > 1 && os.Args[1] == "librespot-hook" {
		runLibrespotSubcommand()
		return
	}

	// Check for version flag early (for main command)
	for _, arg := range os.Args[1:] {
		if arg == "-version" || arg == "--version" {
			printVersion()
			return
		}
		if arg == "-help" || arg == "--help" || arg == "-h" {
			printUsage()
			return
		}
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
		showVersion = flag.Bool("version", false, "Print version and exit")
		showHelp    = flag.Bool("help", false, "Print help message")
	)

	// Custom usage function
	flag.Usage = printUsage
	flag.Parse()

	// Handle help and version flags
	if *showHelp {
		printUsage()
		return
	}
	if *showVersion {
		printVersion()
		return
	}

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

	if *verbose {
		log.Printf("argon-camilladsp-remote v%s starting", version)
		log.Printf("Configuration:")
		log.Printf("  Input device: %s", *inputDev)
		log.Printf("  WebSocket URL: %s", *wsURL)
		log.Printf("  IPC socket: %s", *socketPath)
		log.Printf("  Volume range: %.1f dB to %.1f dB", *minDB, *maxDB)
		log.Printf("  Update rate: %d Hz", *updateHz)
		log.Printf("  Max velocity: %.1f dB/s", *velMax)
		log.Printf("  Accel time: %.1f s", *accelTime)
		log.Printf("  Decay tau: %.1f s", *decayTau)
		log.Printf("  Read timeout: %d ms", *readTimeout)
	}
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

func printLibrespotHookUsage() {
	fmt.Printf("argon-camilladsp-remote librespot-hook v%s\n", version)
	fmt.Println()
	fmt.Println("USAGE:")
	fmt.Println("  argon-camilladsp-remote librespot-hook [OPTIONS]")
	fmt.Println()
	fmt.Println("DESCRIPTION:")
	fmt.Println("  Librespot event hook that communicates with the argon-camilladsp-remote")
	fmt.Println("  daemon via Unix socket. Reads PLAYER_EVENT environment variable to")
	fmt.Println("  handle playback events (start/stop/playing/paused/changed).")
	fmt.Println()
	fmt.Println("OPTIONS:")
	fmt.Println("  -socket string")
	fmt.Println("        Unix domain socket path for IPC (default \"/tmp/argon-camilladsp.sock\")")
	fmt.Println()
	fmt.Println("  -min float")
	fmt.Println("        Minimum volume clamp in dB (default -65.0)")
	fmt.Println()
	fmt.Println("  -max float")
	fmt.Println("        Maximum volume clamp in dB (default 0.0)")
	fmt.Println()
	fmt.Println("  -v    Enable verbose logging")
	fmt.Println()
	fmt.Println("ENVIRONMENT VARIABLES:")
	fmt.Println("  PLAYER_EVENT - Event type from librespot (start|stop|playing|paused|changed)")
	fmt.Println()
	fmt.Println("EXAMPLE:")
	fmt.Println("  Add to librespot configuration:")
	fmt.Println("  onevent = /usr/local/bin/argon-camilladsp-remote librespot-hook")
	fmt.Println()
}

// runLibrespotSubcommand handles librespot-hook subcommand mode
func runLibrespotSubcommand() {
	// Create a new flagset for librespot subcommand
	fs := flag.NewFlagSet("librespot-hook", flag.ExitOnError)
	socketPath := fs.String("socket", "/tmp/argon-camilladsp.sock", "Unix domain socket path for IPC")
	minDB := fs.Float64("min", -65.0, "Minimum volume clamp in dB")
	maxDB := fs.Float64("max", 0.0, "Maximum volume clamp in dB")
	verbose := fs.Bool("v", false, "Verbose logging")
	showHelp := fs.Bool("help", false, "Print help message")

	// Custom usage for librespot subcommand
	fs.Usage = printLibrespotHookUsage

	// Parse flags (skip "librespot-hook" subcommand name)
	fs.Parse(os.Args[2:])

	// Handle help flag
	if *showHelp {
		printLibrespotHookUsage()
		return
	}

	// Run hook handler (reads from environment variables)
	if err := runLibrespotHook(*socketPath, *minDB, *maxDB, *verbose); err != nil {
		log.Fatalf("librespot hook error: %v", err)
	}
}
