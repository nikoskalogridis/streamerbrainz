package main

import "fmt"

// ==============================
// Commands (side effects)
// ==============================

// Command represents an external side effect to be executed by the daemon loop.
// In this codebase, those are primarily CamillaDSP websocket commands.
type Command interface {
	commandMarker()
	String() string
}

// CmdSetVolume requests setting volume in CamillaDSP (Main fader volume).
type CmdSetVolume struct {
	TargetDB float64
}

func (CmdSetVolume) commandMarker() {}
func (c CmdSetVolume) String() string {
	return fmt.Sprintf("CmdSetVolume(target_db=%.3f)", c.TargetDB)
}

// CmdToggleMute toggles mute in CamillaDSP (Main).
type CmdToggleMute struct{}

func (CmdToggleMute) commandMarker() {}
func (CmdToggleMute) String() string { return "CmdToggleMute()" }

// CmdSetMute sets mute explicitly in CamillaDSP (Main).
type CmdSetMute struct {
	Muted bool
}

func (CmdSetMute) commandMarker()   {}
func (c CmdSetMute) String() string { return fmt.Sprintf("CmdSetMute(muted=%v)", c.Muted) }

// CmdGetVolume requests current volume from CamillaDSP.
type CmdGetVolume struct{}

func (CmdGetVolume) commandMarker() {}
func (CmdGetVolume) String() string { return "CmdGetVolume()" }

// CmdGetMute requests current mute from CamillaDSP.
type CmdGetMute struct{}

func (CmdGetMute) commandMarker() {}
func (CmdGetMute) String() string { return "CmdGetMute()" }

// CmdGetConfigFilePath requests current config file path from CamillaDSP.
type CmdGetConfigFilePath struct{}

func (CmdGetConfigFilePath) commandMarker() {}
func (CmdGetConfigFilePath) String() string { return "CmdGetConfigFilePath()" }

// CmdGetState requests current processing state from CamillaDSP.
type CmdGetState struct{}

func (CmdGetState) commandMarker() {}
func (CmdGetState) String() string { return "CmdGetState()" }
