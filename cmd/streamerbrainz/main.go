package main

import (
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"gopkg.in/yaml.v3"
)

const version = "1.0.0"

const defaultConfigPath = "~/.config/streamerbrainz/config.yaml"

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
	fmt.Println("  Daemon that bridges input/control intent to CamillaDSP volume control.")
	fmt.Println("  Configuration is primarily via a YAML config file.")
	fmt.Println()
	fmt.Println("OPTIONS:")
	fmt.Println("  -config string")
	fmt.Printf("        Path to YAML config file (default %q)\n", defaultConfigPath)
	fmt.Println()
	fmt.Println("  -print-default-config")
	fmt.Println("        Print a default YAML config to stdout and exit")
	fmt.Println()
	fmt.Println("  -log-level string")
	fmt.Println("        Override logging.level from config (error, warn, info, debug)")
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
	fmt.Println("  # Print a default config template")
	fmt.Println("  streamerbrainz -print-default-config > streamerbrainz.yaml")
	fmt.Println()
	fmt.Println("  # Run daemon with a config file")
	fmt.Println("  streamerbrainz -config ~/.config/streamerbrainz/config.yaml")
	fmt.Println()
	fmt.Println("NOTES:")
	fmt.Println("  - If you need to override one value ad-hoc, prefer editing the config file")
	fmt.Println("    or using a small supported override flag (like -log-level).")
	fmt.Println()
}

func main() {
	// Check for subcommand mode (librespot hook) first
	if len(os.Args) > 1 && os.Args[1] == "librespot-hook" {
		runLibrespotSubcommand()
		return
	}

	// Check for version/help flags early (for main command)
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

	// Parse command-line flags (config-first, minimal overrides)
	var (
		configPath         = flag.String("config", "", "Path to JSON config file")
		printDefaultConfig = flag.Bool("print-default-config", false, "Print default JSON config and exit")
		logLevelOverride   = flag.String("log-level", "", "Override logging.level from config (error, warn, info, debug)")
		showVersion        = flag.Bool("version", false, "Print version and exit")
		showHelp           = flag.Bool("help", false, "Print help message")
	)

	flag.Usage = printUsage
	flag.Parse()

	if *showHelp {
		printUsage()
		return
	}
	if *showVersion {
		printVersion()
		return
	}
	if *printDefaultConfig {
		cfg := DefaultConfig()
		b, err := yaml.Marshal(cfg)
		if err != nil {
			fmt.Fprintln(os.Stderr, "error: marshal default config:", err)
			os.Exit(1)
		}
		fmt.Println(string(b))
		return
	}
	if *configPath == "" {
		*configPath = defaultConfigPath
	}

	cfg, err := LoadConfigFile(*configPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}

	// Apply small overrides
	if *logLevelOverride != "" {
		cfg.Logging.Level = *logLevelOverride
	}

	// Expand user paths
	cfg.IPC.SocketPath = ExpandPath(cfg.IPC.SocketPath)
	for i := range cfg.IR.InputDevices {
		cfg.IR.InputDevices[i].Path = ExpandPath(cfg.IR.InputDevices[i].Path)
	}
	cfg.Plex.TokenFile = ExpandPath(cfg.Plex.TokenFile)

	// Validate fully materialized config
	if err := cfg.Validate(); err != nil {
		fmt.Fprintln(os.Stderr, "error: invalid config:", err)
		os.Exit(1)
	}

	// Setup logger
	logLevel, err := parseLogLevel(cfg.Logging.Level)
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
	logger := setupLogger(logLevel)

	// Plex enablement semantics moved to config
	plexEnabled := cfg.Plex.Enabled

	// Open all input devices
	type openDevice struct {
		file *os.File
		typ  InputDeviceType
		path string
	}
	var openDevices []openDevice

	for _, inputDev := range cfg.IR.InputDevices {
		f, err := os.Open(inputDev.Path)
		if err != nil {
			logger.Error("failed to open input device", "device", inputDev.Path, "error", err, "tip", "run as root or add user to 'input' group")
			// Close already opened devices
			for _, od := range openDevices {
				od.file.Close()
			}
			os.Exit(1)
		}
		openDevices = append(openDevices, openDevice{
			file: f,
			typ:  inputDev.Type,
			path: inputDev.Path,
		})
		logger.Debug("opened input device", "device", inputDev.Path, "type", inputDev.Type)
	}
	defer func() {
		for _, od := range openDevices {
			od.file.Close()
		}
	}()

	// Setup CamillaDSP client
	client, err := NewCamillaDSPClient(cfg.CamillaDSP.WsURL, logger, cfg.CamillaDSP.TimeoutMS)
	if err != nil {
		logger.Error("failed to connect to CamillaDSP", "error", err)
		os.Exit(1)
	}
	defer client.Close()

	// Query initial volume from CamillaDSP
	initialVol, err := client.GetVolume()
	if err != nil {
		logger.Warn("failed to query initial volume, using safe default", "error", err, "safe_default_db", safeDefaultDB)
		initialVol = safeDefaultDB
	}

	// Initialize velocity engine
	velState := newVelocityState(cfg.ToVelocityConfig())
	velState.updateVolume(initialVol)

	// Initialize rotary encoder state
	rotary := newRotaryState()

	// Handle shutdown
	sigc := make(chan os.Signal, 1)
	signal.Notify(sigc, syscall.SIGINT, syscall.SIGTERM)

	// Create action channel - central command bus
	actions := make(chan Action, 64)

	// Start daemon brain in a goroutine
	go runDaemon(actions, client, velState, cfg.CamillaDSP.UpdateHz, logger)

	// Start IPC server
	if err := runIPCServer(cfg.IPC.SocketPath, actions, logger); err != nil {
		logger.Error("failed to start IPC server", "error", err)
		os.Exit(1)
	}

	// Enable Plex integration (webhooks + session polling) if configured.
	if plexEnabled {
		if err := setupPlexWebhook(cfg.Plex.ServerURL, cfg.Plex.TokenFile, cfg.Plex.MachineID, actions, logger); err != nil {
			logger.Error("failed to setup Plex webhook", "error", err)
			os.Exit(1)
		}
	}

	// Start webhooks HTTP server
	go func() {
		if err := runWebhooksServer(cfg.Webhooks.Port, logger); err != nil {
			logger.Error("webhooks server error", "error", err)
			os.Exit(1)
		}
	}()

	events := make(chan inputEvent, 64)
	readErr := make(chan error, len(openDevices))

	// Start a reader goroutine for each input device
	for _, od := range openDevices {
		go func(file *os.File, name string, devType InputDeviceType) {
			logger.Debug("starting input reader", "device", name, "type", devType)
			readInputEvents(file, events, readErr)
			logger.Warn("input reader stopped", "device", name)
		}(od.file, od.path, od.typ)
	}

	logger.Debug("starting streamerbrainz", "version", version)

	// Build device list for logging
	var devicePaths []string
	for _, od := range openDevices {
		devicePaths = append(devicePaths, od.path)
	}

	logger.Debug("configuration",
		"config_path", *configPath,
		"input_devices", devicePaths,
		"camilladsp_ws_url", cfg.CamillaDSP.WsURL,
		"camilladsp_ws_timeout_ms", cfg.CamillaDSP.TimeoutMS,
		"ipc_socket", cfg.IPC.SocketPath,
		"camilladsp_min_db", cfg.CamillaDSP.MinDB,
		"camilladsp_max_db", cfg.CamillaDSP.MaxDB,
		"camilladsp_update_hz", cfg.CamillaDSP.UpdateHz,
		"vel_mode", cfg.Velocity.Mode,
		"vel_max_db_per_sec", cfg.Velocity.MaxDBPerSec,
		"rotary_db_per_step", cfg.Rotary.DbPerStep,
		"rotary_velocity_window_ms", cfg.Rotary.VelocityWindowMS,
		"vel_accel_time_sec", cfg.Velocity.AccelTimeSec,
		"vel_decay_tau_sec", cfg.Velocity.DecayTauSec,
		"vel_turbo_mult", cfg.Velocity.TurboMult,
		"vel_turbo_delay_sec", cfg.Velocity.TurboDelay,
		"vel_danger_zone_db", cfg.Velocity.DangerZoneDB,
		"vel_danger_vel_max_db_per_sec", cfg.Velocity.DangerVelMaxDBPerSec,
		"vel_danger_vel_min_near0_db_per_sec", cfg.Velocity.DangerVelMinNear0DBPerS,
		"vel_hold_timeout_ms", cfg.Velocity.HoldTimeoutMS,
		"webhooks_port", cfg.Webhooks.Port,
		"plex_enabled", plexEnabled)

	listenInfo := []any{
		"input_devices", devicePaths,
		"ipc", cfg.IPC.SocketPath,
		"camilladsp_ws", cfg.CamillaDSP.WsURL,
		"update_rate_hz", cfg.CamillaDSP.UpdateHz,
		"webhooks_port", cfg.Webhooks.Port,
	}
	logger.Info("daemon started", listenInfo...)
	if plexEnabled {
		listenInfo = append(listenInfo, "plex_server", cfg.Plex.ServerURL)
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
			for _, od := range openDevices {
				od.file.Close()
			}
			return

		// --------------------------------------------------------------------
		// Input error handling
		// --------------------------------------------------------------------
		case err := <-readErr:
			logger.Error("input reader error", "error", err)
			// Note: We continue running even if one device fails
			// This allows other devices to keep working

		// --------------------------------------------------------------------
		// Input event handling (event translation layer)
		// --------------------------------------------------------------------
		// Input events are translated into Actions and sent to daemon brain
		case ev := <-events:
			// Route events based on type
			switch ev.Type {
			case EV_KEY:
				handleKeyEvent(ev, actions, logger)

			case EV_REL:
				handleRelEvent(ev, actions, rotary, &cfg, logger)

			default:
				// Ignore other event types
				continue
			}

		}
	}
}

// handleKeyEvent processes EV_KEY events (keyboards, IR remotes)
func handleKeyEvent(ev inputEvent, actions chan<- Action, logger *slog.Logger) {
	// Translate key events into Actions
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

	// Media control keys
	case KEY_PLAYPAUSE:
		if ev.Value == evValuePress {
			logger.Debug("media key", "key", "play/pause")
			// TODO: Add PlayPause action if needed
		}

	case KEY_NEXTSONG:
		if ev.Value == evValuePress {
			logger.Debug("media key", "key", "next")
			// TODO: Add Next action if needed
		}

	case KEY_PREVIOUSSONG:
		if ev.Value == evValuePress {
			logger.Debug("media key", "key", "previous")
			// TODO: Add Previous action if needed
		}

	case KEY_PLAYCD:
		if ev.Value == evValuePress {
			logger.Debug("media key", "key", "play")
			// TODO: Add Play action if needed
		}

	case KEY_PAUSECD:
		if ev.Value == evValuePress {
			logger.Debug("media key", "key", "pause")
			// TODO: Add Pause action if needed
		}

	case KEY_STOPCD:
		if ev.Value == evValuePress {
			logger.Debug("media key", "key", "stop")
			// TODO: Add Stop action if needed
		}
	}
}

// handleRelEvent processes EV_REL events (rotary encoders)
func handleRelEvent(ev inputEvent, actions chan<- Action, rotary *rotaryState, cfg *Config, logger *slog.Logger) {
	// Only handle rotary encoder codes
	if ev.Code != REL_DIAL && ev.Code != REL_WHEEL && ev.Code != REL_MISC {
		return
	}

	if ev.Value == 0 {
		return // No movement
	}

	// Determine direction
	direction := 1
	if ev.Value < 0 {
		direction = -1
	}

	// Track velocity: count recent steps in same direction
	recentCount := rotary.addStep(direction, cfg.Rotary.VelocityWindowMS)

	// Calculate step size with optional velocity multiplier
	dbPerStep := cfg.Rotary.DbPerStep

	if recentCount >= cfg.Rotary.VelocityThreshold {
		dbPerStep *= cfg.Rotary.VelocityMultiplier
		logger.Debug("rotary velocity mode",
			"steps_in_window", recentCount,
			"multiplier", cfg.Rotary.VelocityMultiplier,
			"db_per_step", dbPerStep)
	}

	// Send VolumeStep action (preserves sign from ev.Value)
	actions <- VolumeStep{
		Steps:     int(ev.Value),
		DbPerStep: dbPerStep,
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
