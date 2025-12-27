package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"golang.org/x/sync/errgroup"
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
	fmt.Println("        Options: -config, -log-level")
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
		configPath         = flag.String("config", "", "Path to YAML config file")
		printDefaultConfig = flag.Bool("print-default-config", false, "Print default YAML config and exit")
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
	for i := range cfg.Inputs {
		cfg.Inputs[i].Path = ExpandPath(cfg.Inputs[i].Path)
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

	// Open all input devices
	type openDevice struct {
		file *os.File
		typ  InputDeviceType
		path string
	}
	var openDevices []openDevice

	for _, inputDev := range cfg.Inputs {
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

	// Initialize daemon state and populate it from CamillaDSP (authoritative source of truth).
	daemonState := &DaemonState{}
	now := time.Now()

	// Volume
	initialVol, err := client.GetVolume()
	if err != nil {
		logger.Warn("failed to query initial volume, using safe default", "error", err, "safe_default_db", safeDefaultDB)
		initialVol = safeDefaultDB
	}
	daemonState.SetObservedVolume(initialVol, now)

	// Mute
	if muted, err := client.GetMute(); err != nil {
		logger.Warn("failed to query initial mute state", "error", err)
	} else {
		daemonState.SetObservedMute(muted, now)
	}

	// Config file path (best-effort)
	if cfgPath, err := client.GetConfigFilePath(); err != nil {
		logger.Warn("failed to query initial config file path", "error", err)
	} else {
		daemonState.SetObservedConfigFilePath(cfgPath, now)
	}

	// Processing state (best-effort)
	if dspState, err := client.GetState(); err != nil {
		logger.Warn("failed to query initial processing state", "error", err)
	} else {
		daemonState.SetObservedProcessingState(dspState, now)
	}

	// Initialize reducer-owned volume controller state using daemon state's observed volume as baseline.
	velCfg := cfg.ToVelocityConfig()
	if daemonState.Camilla.VolumeKnown {
		daemonState.VolumeCtrl.TargetDB = daemonState.Camilla.VolumeDB
	} else {
		daemonState.VolumeCtrl.TargetDB = safeDefaultDB
	}
	// Ensure controller timing starts in a sane state.
	daemonState.VolumeCtrl.LastHeldAt = time.Now()

	// Coordinated shutdown using context + errgroup.
	// - ctx is canceled on SIGINT/SIGTERM
	// - goroutines should return on ctx.Done()
	// - main waits for all components to stop before exiting
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	g, ctx := errgroup.WithContext(ctx)

	// Create action channel - central command bus
	events := make(chan Event, 64)

	// Start daemon brain
	g.Go(func() error {
		runDaemon(ctx, events, client, velCfg, cfg.Rotary, daemonState, cfg.CamillaDSP.UpdateHz, logger)
		return nil
	})

	// Start IPC server (context-aware; blocks until ctx is canceled)
	g.Go(func() error {
		return runIPCServer(ctx, cfg.IPC.SocketPath, events, logger)
	})

	// Enable Plex integration (webhooks + session polling) if configured.
	// NOTE: setupPlexWebhook currently isn't context-aware; it likely starts background
	// work internally. We keep the existing behavior. If it fails, we cancel the program
	// and let the coordinated shutdown path handle teardown.
	if cfg.Plex.Enabled {
		if err := setupPlexWebhook(cfg.Plex.ServerURL, cfg.Plex.TokenFile, cfg.Plex.MachineID, events, logger); err != nil {
			logger.Error("failed to setup Plex webhook", "error", err)
			stop()
		}
	}

	// Start webhooks HTTP server (context-aware; blocks until ctx is canceled)
	g.Go(func() error {
		return runWebhooksServer(ctx, cfg.Webhooks.Port, logger)
	})

	readErr := make(chan error, len(openDevices))

	// Start a reader goroutine for each input device and track them for shutdown.
	// Input readers now emit events directly into the central `events` channel.
	var inputWG sync.WaitGroup
	inputWG.Add(len(openDevices))

	for _, od := range openDevices {
		go func(file *os.File, name string, devType InputDeviceType) {
			defer inputWG.Done()
			logger.Debug("starting input reader", "device", name, "type", devType)
			readInputEvents(file, events, readErr, logger)
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
		"plex_enabled", cfg.Plex.Enabled)

	listenInfo := []any{
		"input_devices", devicePaths,
		"ipc", cfg.IPC.SocketPath,
		"camilladsp_ws", cfg.CamillaDSP.WsURL,
		"update_rate_hz", cfg.CamillaDSP.UpdateHz,
		"webhooks_port", cfg.Webhooks.Port,
	}
	logger.Info("daemon started", listenInfo...)
	if cfg.Plex.Enabled {
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
		case <-ctx.Done():
			logger.Info("shutting down")

			// Stop producers by closing input devices. This unblocks readInputEvents.
			for _, od := range openDevices {
				_ = od.file.Close()
			}

			// Ensure input reader goroutines have exited before we close the actions channel.
			// This reduces the risk of panics from sends to a closed channel during teardown.
			inputWG.Wait()

			// Close the actions channel to signal downstream consumers (daemon) to stop.
			// Safe to close once here because main is the coordinator.
			close(events)

			// Close CamillaDSP client connection.
			_ = client.Close()

			// Wait for background components (daemon, IPC, webhooks) to exit.
			if err := g.Wait(); err != nil {
				logger.Error("shutdown error", "error", err)
			}
			return

		// --------------------------------------------------------------------
		// Input error handling
		// --------------------------------------------------------------------
		case err := <-readErr:
			logger.Error("input reader error", "error", err)
			// Note: We continue running even if one device fails
			// This allows other devices to keep working

			// Input events are translated into Actions inside the input reader goroutines.
			// Main no longer receives raw input events here.

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
	fmt.Println("  daemon via Unix socket configured in the YAML config file.")
	fmt.Println("  Reads PLAYER_EVENT environment variable to handle playback events")
	fmt.Println("  (start|stop|playing|paused|changed).")
	fmt.Println()
	fmt.Println("OPTIONS:")
	fmt.Println("  -config string")
	fmt.Printf("        Path to YAML config file (default %q)\n", defaultConfigPath)
	fmt.Println()
	fmt.Println("  -log-level string")
	fmt.Println("        Override logging.level from config (error, warn, info, debug)")
	fmt.Println()
	fmt.Println("ENVIRONMENT VARIABLES:")
	fmt.Println("  PLAYER_EVENT - Event type from librespot (start|stop|playing|paused|changed)")
	fmt.Println()
	fmt.Println("EXAMPLE:")
	fmt.Println("  Add to librespot configuration:")
	fmt.Println("  onevent = /usr/local/bin/streamerbrainz librespot-hook -config ~/.config/streamerbrainz/config.yaml")
	fmt.Println()
}

// runLibrespotSubcommand handles librespot-hook subcommand mode
func runLibrespotSubcommand() {
	// Create a new flagset for librespot subcommand
	fs := flag.NewFlagSet("librespot-hook", flag.ExitOnError)
	configPath := fs.String("config", "", "Path to YAML config file")
	logLevelOverride := fs.String("log-level", "", "Override logging.level from config (error, warn, info, debug)")
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

	// Expand user paths (only what librespot-hook uses)
	cfg.IPC.SocketPath = ExpandPath(cfg.IPC.SocketPath)

	// Validate fully materialized config
	if err := cfg.Validate(); err != nil {
		fmt.Fprintln(os.Stderr, "error: invalid config:", err)
		os.Exit(1)
	}

	// Parse and validate log level
	logLevel, err := parseLogLevel(cfg.Logging.Level)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}

	// Setup logger
	logger := setupLogger(logLevel)

	// Run hook handler (reads from environment variables)
	if err := runLibrespotHook(cfg.IPC.SocketPath, logger); err != nil {
		logger.Error("librespot hook error", "error", err)
		os.Exit(1)
	}
}
