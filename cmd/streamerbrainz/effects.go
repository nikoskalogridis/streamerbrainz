package main

import (
	"log/slog"
	"time"
)

// runEffect executes a single reducer-emitted Command (side effect) against external systems
// (currently CamillaDSP) and emits an observation Event via onEvent.
//
// Design rules:
// - This function is allowed to perform I/O.
// - It must never call Reduce() directly; it only emits Events to be reduced by the daemon loop.
// - The daemon loop is responsible for sequencing: Reduce -> Commands -> runEffect -> Events -> Reduce.
func runEffect(
	client *CamillaDSPClient,
	cmd Command,
	logger *slog.Logger,
	onEvent func(Event),
) {
	if onEvent == nil {
		// No place to report observations/errors; nothing sensible to do.
		return
	}

	if client == nil {
		onEvent(CamillaCommandFailed{
			Command: cmd,
			Err:     errNoClient{},
			At:      time.Now(),
		})
		return
	}

	now := time.Now()

	switch c := cmd.(type) {
	case CmdSetVolume:
		vol, err := client.SetVolume(c.TargetDB)
		if err != nil {
			logger.Error("camilladsp SetVolume failed", "error", err, "target_db", c.TargetDB)
			onEvent(CamillaCommandFailed{Command: cmd, Err: err, At: now})
			return
		}
		onEvent(CamillaVolumeObserved{VolumeDB: vol, At: now})

	case CmdGetVolume:
		vol, err := client.GetVolume()
		if err != nil {
			logger.Error("camilladsp GetVolume failed", "error", err)
			onEvent(CamillaCommandFailed{Command: cmd, Err: err, At: now})
			return
		}
		onEvent(CamillaVolumeObserved{VolumeDB: vol, At: now})

	case CmdToggleMute:
		muted, err := client.ToggleMute()
		if err != nil {
			logger.Error("camilladsp ToggleMute failed", "error", err)
			onEvent(CamillaCommandFailed{Command: cmd, Err: err, At: now})
			return
		}
		onEvent(CamillaMuteObserved{Muted: muted, At: now})

	case CmdSetMute:
		if err := client.SetMute(c.Muted); err != nil {
			logger.Error("camilladsp SetMute failed", "error", err, "muted", c.Muted)
			onEvent(CamillaCommandFailed{Command: cmd, Err: err, At: now})
			return
		}
		// CamillaDSP SetMute doesn't return the value; we know what we set.
		onEvent(CamillaMuteObserved{Muted: c.Muted, At: now})

	case CmdGetMute:
		muted, err := client.GetMute()
		if err != nil {
			logger.Error("camilladsp GetMute failed", "error", err)
			onEvent(CamillaCommandFailed{Command: cmd, Err: err, At: now})
			return
		}
		onEvent(CamillaMuteObserved{Muted: muted, At: now})

	case CmdGetConfigFilePath:
		path, err := client.GetConfigFilePath()
		if err != nil {
			logger.Error("camilladsp GetConfigFilePath failed", "error", err)
			onEvent(CamillaCommandFailed{Command: cmd, Err: err, At: now})
			return
		}
		onEvent(CamillaConfigFilePathObserved{Path: path, At: now})

	case CmdGetState:
		st, err := client.GetState()
		if err != nil {
			logger.Error("camilladsp GetState failed", "error", err)
			onEvent(CamillaCommandFailed{Command: cmd, Err: err, At: now})
			return
		}
		onEvent(CamillaProcessingStateObserved{State: st, At: now})

	case CmdPublishStateSnapshot:
		// Deliver reducer-produced snapshot to the requester.
		// This keeps the reducer pure by moving the channel send into the effects layer.
		if c.Reply == nil {
			logger.Warn("state snapshot requested with nil reply channel")
			return
		}

		// Never block the effects worker indefinitely.
		select {
		case c.Reply <- c.Snapshot:
			// delivered
		default:
			logger.Warn("state snapshot reply channel not ready; dropping snapshot")
		}

	default:
		// Unknown command: record failure so reducer can react (if desired).
		logger.Warn("unknown command type", "command", cmd.String())
		onEvent(CamillaCommandFailed{
			Command: cmd,
			Err:     errUnknownCommand{cmd: cmd},
			At:      now,
		})
	}
}

// errNoClient indicates the daemon was asked to execute a command without a CamillaDSP client.
type errNoClient struct{}

func (errNoClient) Error() string { return "no CamillaDSP client" }

type errUnknownCommand struct {
	cmd Command
}

func (e errUnknownCommand) Error() string { return "unknown command: " + e.cmd.String() }
