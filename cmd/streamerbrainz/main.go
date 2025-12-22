package main

import (
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"
)

const version = "1.0.0"

func printVersion() {
	fmt.Printf("StreamerBrainz v%s\n", version)
	fmt.Println("IR remote control daemon for CamillaDSP volume control")
}

func printUsage() {
	printVersion()
	fmt.Println()
	fmt.Println("USAGE:")
	fmt.Println("  streamerbrainz [OPTIONS]")
	fmt.Println("  streamerbrainz librespot-hook [OPTIONS]")
	fmt.Println()
	fmt.Println("DESCRIPTION:")
	fmt.Println("  Daemon that bridges IR remote control events (via Linux input devices)")
	fmt.Println("  to CamillaDSP volume control over WebSocket. Features velocity-based")
	fmt.Println("  volume ramping for smooth control and librespot integration.")
	fmt.Println()
	fmt.Println("OPTIONS:")
	fmt.Println("  -ir-device string")
	fmt.Printf("        Linux input event device for IR remote (default \"/dev/input/event6\")\n")
	fmt.Println()
	fmt.Println("  -camilladsp-ws-url string")
	fmt.Printf("        CamillaDSP websocket URL (default \"ws://127.0.0.1:1234\")\n")
	fmt.Println("        Note: CamillaDSP must be started with -pPORT option")
	fmt.Println()
	fmt.Println("  -camilladsp-ws-timeout-ms int")
	fmt.Printf("        Timeout for websocket responses in ms (default %d)\n", defaultReadTimeoutMS)
	fmt.Println()
	fmt.Println("  -camilladsp-min-db float")
	fmt.Printf("        Minimum volume clamp in dB (default -65.0)\n")
	fmt.Println()
	fmt.Println("  -camilladsp-max-db float")
	fmt.Printf("        Maximum volume clamp in dB (default 0.0)\n")
	fmt.Println()
	fmt.Println("  -camilladsp-update-hz int")
	fmt.Printf("        Update loop frequency in Hz (default %d)\n", defaultUpdateHz)
	fmt.Println()
	fmt.Println("  -vel-max-db-per-sec float")
	fmt.Printf("        Maximum velocity in dB/s (default %.1f)\n", defaultVelMaxDBPerS)
	fmt.Println()
	fmt.Println("  -vel-accel-time float")
	fmt.Printf("        Time to reach max velocity in seconds (default %.1f)\n", defaultAccelTime)
	fmt.Println()
	fmt.Println("  -vel-decay-tau float")
	fmt.Printf("        Velocity decay time constant in seconds (default %.1f)\n", defaultDecayTau)
	fmt.Println()
	fmt.Println("  -ipc-socket string")
	fmt.Printf("        Unix domain socket path for IPC (default \"/tmp/streamerbrainz.sock\")\n")
	fmt.Println()
	fmt.Println("  -webhooks-port int")
	fmt.Println("        Webhooks HTTP listener port (default 3001)")
	fmt.Println()
	fmt.Println("  -plex-server-url string")
	fmt.Println("        Plex server URL (e.g., \"http://plex.home.arpa:32400\") - enables Plex integration when set")
	fmt.Println()
	fmt.Println("  -plex-token-file string")
	fmt.Println("        Path to file containing Plex authentication token (required for Plex integration)")
	fmt.Println()
	fmt.Println("  -plex-machine-id string")
	fmt.Println("        Plex player machine identifier to filter sessions (required for Plex integration)")
	fmt.Println()
	fmt.Println("  -log-level string")
	fmt.Println("        Log level: error, warn, info, debug (default \"info\")")
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
	fmt.Println("        Options: -ipc-socket, -log-level")
	fmt.Println()
	fmt.Println("EXAMPLES:")
	fmt.Println("  # Start daemon with default settings")
	fmt.Println("  streamerbrainz")
	fmt.Println()
	fmt.Println("  # Custom input device and volume range")
	fmt.Println("  streamerbrainz -ir-device /dev/input/event4 -camilladsp-min-db -80 -camilladsp-max-db -10")
	fmt.Println()
	fmt.Println("  # Connect to remote CamillaDSP instance")
	fmt.Println("  streamerbrainz -camilladsp-ws-url ws://192.168.1.100:1234")
	fmt.Println()
	fmt.Println("  # Enable Plexamp webhook integration")
	fmt.Println("  streamerbrainz -plex-server-url http://plex.home.arpa:32400 -plex-token-file /path/to/token -plex-machine-id YOUR_ID")
	fmt.Println()
	fmt.Println("  # Use as librespot hook (add to librespot config)")
	fmt.Println("  onevent = streamerbrainz librespot-hook")
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
		irDevice            = flag.String("ir-device", "/dev/input/event6", "Linux input event device for IR (e.g. /dev/input/event6)")
		camillaDspWsUrl     = flag.String("camilladsp-ws-url", "ws://127.0.0.1:1234", "CamillaDSP websocket URL (CamillaDSP must be started with -pPORT)")
		camillaDspWsTimeout = flag.Int("camilladsp-ws-timeout-ms", defaultReadTimeoutMS, "Timeout in milliseconds for reading websocket responses")
		camillaDspMinDb     = flag.Float64("camilladsp-min-db", -65.0, "Minimum volume clamp in dB")
		camillaDspMaxDb     = flag.Float64("camilladsp-max-db", 0.0, "Maximum volume clamp in dB")
		camillaDspUpdateHz  = flag.Int("camilladsp-update-hz", defaultUpdateHz, "Update loop frequency in Hz")
		velMaxDbPerSec      = flag.Float64("vel-max-db-per-sec", defaultVelMaxDBPerS, "Maximum velocity in dB/s")
		velAccelTime        = flag.Float64("vel-accel-time", defaultAccelTime, "Time to reach max velocity in seconds")
		velDecayTau         = flag.Float64("vel-decay-tau", defaultDecayTau, "Velocity decay time constant in seconds")
		ipcSocketPath       = flag.String("ipc-socket", "/tmp/streamerbrainz.sock", "Unix domain socket path for IPC")
		webhooksPort        = flag.Int("webhooks-port", 3001, "Webhooks HTTP listener port")
		plexServerUrl       = flag.String("plex-server-url", "", "Plex server URL (e.g., http://plex.home.arpa:32400)")
		plexTokenFile       = flag.String("plex-token-file", "", "Path to file containing Plex authentication token")
		plexMachineID       = flag.String("plex-machine-id", "", "Plex player machine identifier to filter sessions")
		logLevelStr         = flag.String("log-level", "info", "Log level: error, warn, info, debug")
		showVersion         = flag.Bool("version", false, "Print version and exit")
		showHelp            = flag.Bool("help", false, "Print help message")
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

	// Parse and validate log level
	logLevel, err := parseLogLevel(*logLevelStr)
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}

	// Validate flags
	if *camillaDspMinDb > *camillaDspMaxDb {
		fmt.Fprintln(os.Stderr, "error: -camilladsp-min-db must be <= -camilladsp-max-db")
		os.Exit(1)
	}
	if *camillaDspUpdateHz <= 0 || *camillaDspUpdateHz > 1000 {
		fmt.Fprintln(os.Stderr, "error: -camilladsp-update-hz must be between 1 and 1000")
		os.Exit(1)
	}

	// Check if Plex integration should be enabled
	plexEnabled := *plexServerUrl != "" || *plexTokenFile != "" || *plexMachineID != ""

	if plexEnabled {
		if *plexServerUrl == "" {
			fmt.Fprintln(os.Stderr, "error: -plex-server-url is required for Plex integration")
			os.Exit(1)
		}
		if *plexTokenFile == "" {
			fmt.Fprintln(os.Stderr, "error: -plex-token-file is required for Plex integration")
			os.Exit(1)
		}
		if *plexMachineID == "" {
			fmt.Fprintln(os.Stderr, "error: -plex-machine-id is required for Plex integration")
			os.Exit(1)
		}
	}

	// Setup logger
	logger := setupLogger(logLevel)

	// Open input device
	f, err := os.Open(*irDevice)
	if err != nil {
		logger.Error("failed to open input device", "device", *irDevice, "error", err, "tip", "run as root or add user to 'input' group")
		os.Exit(1)
	}
	defer f.Close()

	// Initialize CamillaDSP client
	client, err := NewCamillaDSPClient(*camillaDspWsUrl, logger, *camillaDspWsTimeout)
	if err != nil {
		logger.Error("failed to connect to CamillaDSP", "error", err)
		os.Exit(1)
	}
	defer client.Close()

	// Get initial volume from server
	initialVol, err := client.GetVolume()
	if err != nil {
		logger.Warn("could not get initial volume", "error", err)
		logger.Info("setting server volume to safe default", "db", safeDefaultDB)

		// Actively set the server to a safe default volume
		_, setErr := client.SetVolume(safeDefaultDB)
		if setErr != nil {
			logger.Error("failed to set safe default volume", "error", setErr)
			logger.Warn("cannot verify server volume - proceeding with caution")
		}
		initialVol = safeDefaultDB
	}

	// Initialize velocity state with known volume
	velState := newVelocityState(*velMaxDbPerSec, *velAccelTime, *velDecayTau, *camillaDspMinDb, *camillaDspMaxDb)
	velState.updateVolume(initialVol)

	// Handle shutdown
	sigc := make(chan os.Signal, 1)
	signal.Notify(sigc, syscall.SIGINT, syscall.SIGTERM)

	// Create action channel - central command bus
	actions := make(chan Action, 64)

	// Start daemon brain in a goroutine
	go runDaemon(actions, client, velState, *camillaDspUpdateHz, logger)

	// Start IPC server
	if err := runIPCServer(*ipcSocketPath, actions, logger); err != nil {
		logger.Error("failed to start IPC server", "error", err)
		os.Exit(1)
	}

	// Setup webhook integrations
	if plexEnabled {
		if err := setupPlexWebhook(*plexServerUrl, *plexTokenFile, *plexMachineID, actions, logger); err != nil {
			logger.Error("failed to setup Plex webhook", "error", err)
			os.Exit(1)
		}
	}

	// Start webhooks HTTP server
	go func() {
		if err := runWebhooksServer(*webhooksPort, logger); err != nil {
			logger.Error("webhooks server error", "error", err)
		}
	}()

	// Read loop for input events
	events := make(chan inputEvent, 64)
	readErr := make(chan error, 1)
	go readInputEvents(f, events, readErr)

	logger.Debug("starting streamerbrainz", "version", version)
	logger.Debug("configuration",
		"ir_device", *irDevice,
		"camilladsp_ws_url", *camillaDspWsUrl,
		"camilladsp_ws_timeout_ms", *camillaDspWsTimeout,
		"ipc_socket", *ipcSocketPath,
		"camilladsp_min_db", *camillaDspMinDb,
		"camilladsp_max_db", *camillaDspMaxDb,
		"camilladsp_update_hz", *camillaDspUpdateHz,
		"vel_max_db_per_sec", *velMaxDbPerSec,
		"vel_accel_time", *velAccelTime,
		"vel_decay_tau", *velDecayTau,
		"webhooks_port", *webhooksPort,
		"plex_enabled", plexEnabled)
	listenInfo := []any{"ir_device", *irDevice, "ipc", *ipcSocketPath, "camilladsp_ws", *camillaDspWsUrl, "update_rate_hz", *camillaDspUpdateHz, "webhooks_port", *webhooksPort}
	if plexEnabled {
		listenInfo = append(listenInfo, "plex_server", *plexServerUrl)
	}
	logger.Info("listening", listenInfo...)

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
			logger.Info("shutting down")
			client.Close()
			f.Close()
			return

		// --------------------------------------------------------------------
		// Input error handling
		// --------------------------------------------------------------------
		case err := <-readErr:
			logger.Error("input reader stopped", "error", err)
			client.Close()
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
	fmt.Printf("StreamerBrainz librespot-hook v%s\n", version)
	fmt.Println()
	fmt.Println("USAGE:")
	fmt.Println("  streamerbrainz librespot-hook [OPTIONS]")
	fmt.Println()
	fmt.Println("DESCRIPTION:")
	fmt.Println("  Librespot event hook that communicates with the StreamerBrainz")
	fmt.Println("  daemon via Unix socket. Reads PLAYER_EVENT environment variable to")
	fmt.Println("  handle playback events (start/stop/playing/paused/changed).")
	fmt.Println()
	fmt.Println("OPTIONS:")
	fmt.Println("  -ipc-socket string")
	fmt.Println("        Unix domain socket path for IPC (default \"/tmp/streamerbrainz.sock\")")
	fmt.Println()
	fmt.Println("  -log-level string")
	fmt.Println("        Log level: error, warn, info, debug (default \"info\")")
	fmt.Println()
	fmt.Println("ENVIRONMENT VARIABLES:")
	fmt.Println("  PLAYER_EVENT - Event type from librespot (start|stop|playing|paused|changed)")
	fmt.Println()
	fmt.Println("EXAMPLE:")
	fmt.Println("  Add to librespot configuration:")
	fmt.Println("  onevent = /usr/local/bin/streamerbrainz librespot-hook")
	fmt.Println()
}

// runLibrespotSubcommand handles librespot-hook subcommand mode
func runLibrespotSubcommand() {
	// Create a new flagset for librespot subcommand
	fs := flag.NewFlagSet("librespot-hook", flag.ExitOnError)
	ipcSocketPath := fs.String("ipc-socket", "/tmp/streamerbrainz.sock", "Unix domain socket path for IPC")
	logLevelStr := fs.String("log-level", "info", "Log level: error, warn, info, debug")
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

	// Parse and validate log level
	logLevel, err := parseLogLevel(*logLevelStr)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}

	// Setup logger
	logger := setupLogger(logLevel)

	// Run hook handler (reads from environment variables)
	if err := runLibrespotHook(*ipcSocketPath, logger); err != nil {
		logger.Error("librespot hook error", "error", err)
		os.Exit(1)
	}
}
