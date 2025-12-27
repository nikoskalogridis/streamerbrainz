package main

import (
	"log/slog"
	"math"
	"os"
	"testing"
	"time"
)

// mockCamillaDSPClient is a test double for CamillaDSPClient
type mockCamillaDSPClient struct {
	volume      float64
	muted       bool
	setVolCalls []float64
	getVolCalls int
	toggleCalls int

	// Initial daemon-state sync helpers
	configFilePath string
	state          string
}

func newMockCamillaDSPClient(initialVolume float64) *mockCamillaDSPClient {
	return &mockCamillaDSPClient{
		volume:         initialVolume,
		muted:          false,
		setVolCalls:    make([]float64, 0),
		configFilePath: "/tmp/mock_camilladsp.yml",
		state:          "Running",
	}
}

func (m *mockCamillaDSPClient) SetVolume(db float64) (float64, error) {
	m.setVolCalls = append(m.setVolCalls, db)
	m.volume = db
	return db, nil
}

func (m *mockCamillaDSPClient) GetVolume() (float64, error) {
	m.getVolCalls++
	return m.volume, nil
}

func (m *mockCamillaDSPClient) GetMute() (bool, error) {
	return m.muted, nil
}

func (m *mockCamillaDSPClient) SetMute(mute bool) error {
	m.muted = mute
	return nil
}

func (m *mockCamillaDSPClient) ToggleMute() (bool, error) {
	m.toggleCalls++
	m.muted = !m.muted
	return m.muted, nil
}

func (m *mockCamillaDSPClient) GetConfigFilePath() (string, error) {
	return m.configFilePath, nil
}

func (m *mockCamillaDSPClient) GetState() (string, error) {
	return m.state, nil
}

func (m *mockCamillaDSPClient) Close() error {
	return nil
}

// TestReducer_VolumeStep_Basic tests basic VolumeStep handling via reducer.
func TestReducer_VolumeStep_Basic(t *testing.T) {
	client := newMockCamillaDSPClient(-30.0)
	cfg := VelocityConfig{
		MinDB: -65.0,
		MaxDB: 0.0,
	}
	_ = slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))

	state := &DaemonState{}
	state.SetObservedVolume(-30.0, time.Now())
	state.VolumeCtrl.TargetDB = -30.0

	// Reduce the action
	rr := Reduce(state, TimedEvent{Event: VolumeStep{Steps: 2, DbPerStep: 0.5}, At: time.Now()}, cfg, RotaryConfig{})

	// No side effects have run yet
	if len(client.setVolCalls) != 0 {
		t.Fatalf("expected 0 SetVolume calls before executing reducer commands, got %d", len(client.setVolCalls))
	}

	// Reducer should have emitted a CmdSetVolume once Tick is processed; here we follow the current reducer contract:
	// it records desired volume intent on the action, and emits commands on Tick.
	// So we drive a Tick to flush intents into commands.
	rr = Reduce(rr.State, Tick{Now: time.Now(), Dt: 0.01}, cfg, RotaryConfig{})

	if len(rr.Commands) != 1 {
		t.Fatalf("expected 1 command on Tick, got %d", len(rr.Commands))
	}

	cmd, ok := rr.Commands[0].(CmdSetVolume)
	if !ok {
		t.Fatalf("expected CmdSetVolume, got %T", rr.Commands[0])
	}

	expected := -29.0
	if cmd.TargetDB != expected {
		t.Errorf("expected CmdSetVolume target %f, got %f", expected, cmd.TargetDB)
	}

	// Execute the command (simulated)
	currentVol, err := client.SetVolume(cmd.TargetDB)
	if err != nil {
		t.Fatalf("SetVolume failed: %v", err)
	}

	// Feed observation back to reducer
	rr = Reduce(rr.State, CamillaVolumeObserved{VolumeDB: currentVol, At: time.Now()}, cfg, RotaryConfig{})

	if len(client.setVolCalls) != 1 {
		t.Fatalf("expected 1 SetVolume call after executing command, got %d", len(client.setVolCalls))
	}
	if client.setVolCalls[0] != expected {
		t.Errorf("expected volume %f, got %f", expected, client.setVolCalls[0])
	}
	if !rr.State.Camilla.VolumeKnown || rr.State.Camilla.VolumeDB != expected {
		t.Errorf("expected observed volume %f, got %f (known=%v)", expected, rr.State.Camilla.VolumeDB, rr.State.Camilla.VolumeKnown)
	}
}

// TestReducer_VolumeStep_Negative tests negative steps (volume down) via reducer.
func TestReducer_VolumeStep_Negative(t *testing.T) {
	client := newMockCamillaDSPClient(-30.0)
	cfg := VelocityConfig{
		MinDB: -65.0,
		MaxDB: 0.0,
	}
	state := &DaemonState{}
	state.SetObservedVolume(-30.0, time.Now())
	state.VolumeCtrl.TargetDB = -30.0

	rr := Reduce(state, TimedEvent{Event: VolumeStep{Steps: -3, DbPerStep: 0.5}, At: time.Now()}, cfg, RotaryConfig{})
	rr = Reduce(rr.State, Tick{Now: time.Now(), Dt: 0.01}, cfg, RotaryConfig{})

	if len(rr.Commands) != 1 {
		t.Fatalf("expected 1 command on Tick, got %d", len(rr.Commands))
	}
	cmd, ok := rr.Commands[0].(CmdSetVolume)
	if !ok {
		t.Fatalf("expected CmdSetVolume, got %T", rr.Commands[0])
	}

	expected := -31.5
	if cmd.TargetDB != expected {
		t.Errorf("expected CmdSetVolume target %f, got %f", expected, cmd.TargetDB)
	}

	currentVol, err := client.SetVolume(cmd.TargetDB)
	if err != nil {
		t.Fatalf("SetVolume failed: %v", err)
	}
	rr = Reduce(rr.State, CamillaVolumeObserved{VolumeDB: currentVol, At: time.Now()}, cfg, RotaryConfig{})

	if len(client.setVolCalls) != 1 {
		t.Fatalf("expected 1 SetVolume call after executing command, got %d", len(client.setVolCalls))
	}
	if client.setVolCalls[0] != expected {
		t.Errorf("expected volume %f, got %f", expected, client.setVolCalls[0])
	}
	if !rr.State.Camilla.VolumeKnown || rr.State.Camilla.VolumeDB != expected {
		t.Errorf("expected observed volume %f, got %f (known=%v)", expected, rr.State.Camilla.VolumeDB, rr.State.Camilla.VolumeKnown)
	}
}

// TestReducer_VolumeStep_ClampMax tests clamping to maximum volume via reducer.
func TestReducer_VolumeStep_ClampMax(t *testing.T) {
	client := newMockCamillaDSPClient(-1.0)
	cfg := VelocityConfig{
		MinDB: -65.0,
		MaxDB: 0.0,
	}
	state := &DaemonState{}
	state.SetObservedVolume(-1.0, time.Now())
	state.VolumeCtrl.TargetDB = -1.0

	rr := Reduce(state, TimedEvent{Event: VolumeStep{Steps: 10, DbPerStep: 0.5}, At: time.Now()}, cfg, RotaryConfig{})
	rr = Reduce(rr.State, Tick{Now: time.Now(), Dt: 0.01}, cfg, RotaryConfig{})

	if len(rr.Commands) != 1 {
		t.Fatalf("expected 1 command on Tick, got %d", len(rr.Commands))
	}
	cmd, ok := rr.Commands[0].(CmdSetVolume)
	if !ok {
		t.Fatalf("expected CmdSetVolume, got %T", rr.Commands[0])
	}

	expected := 0.0
	if cmd.TargetDB != expected {
		t.Errorf("expected CmdSetVolume target %f, got %f", expected, cmd.TargetDB)
	}

	currentVol, err := client.SetVolume(cmd.TargetDB)
	if err != nil {
		t.Fatalf("SetVolume failed: %v", err)
	}
	rr = Reduce(rr.State, CamillaVolumeObserved{VolumeDB: currentVol, At: time.Now()}, cfg, RotaryConfig{})

	if len(client.setVolCalls) != 1 {
		t.Fatalf("expected 1 SetVolume call after executing command, got %d", len(client.setVolCalls))
	}
	if client.setVolCalls[0] != expected {
		t.Errorf("expected volume %f, got %f", expected, client.setVolCalls[0])
	}
}

// TestReducer_VolumeStep_ClampMin tests clamping to minimum volume via reducer.
func TestReducer_VolumeStep_ClampMin(t *testing.T) {
	client := newMockCamillaDSPClient(-64.0)
	cfg := VelocityConfig{
		MinDB: -65.0,
		MaxDB: 0.0,
	}
	state := &DaemonState{}
	state.SetObservedVolume(-64.0, time.Now())
	state.VolumeCtrl.TargetDB = -64.0

	// Reduce action then Tick to flush into commands
	rr := Reduce(state, TimedEvent{Event: VolumeStep{Steps: -10, DbPerStep: 0.5}, At: time.Now()}, cfg, RotaryConfig{})
	rr = Reduce(rr.State, Tick{Now: time.Now(), Dt: 0.01}, cfg, RotaryConfig{})

	if len(rr.Commands) != 1 {
		t.Fatalf("expected 1 command on Tick, got %d", len(rr.Commands))
	}
	cmd, ok := rr.Commands[0].(CmdSetVolume)
	if !ok {
		t.Fatalf("expected CmdSetVolume, got %T", rr.Commands[0])
	}

	expected := -65.0
	if cmd.TargetDB != expected {
		t.Errorf("expected CmdSetVolume target %f, got %f", expected, cmd.TargetDB)
	}

	// Execute command and feed observation back
	currentVol, err := client.SetVolume(cmd.TargetDB)
	if err != nil {
		t.Fatalf("SetVolume failed: %v", err)
	}
	rr = Reduce(rr.State, CamillaVolumeObserved{VolumeDB: currentVol, At: time.Now()}, cfg, RotaryConfig{})

	if len(client.setVolCalls) != 1 {
		t.Fatalf("expected 1 SetVolume call after executing command, got %d", len(client.setVolCalls))
	}
	if client.setVolCalls[0] != expected {
		t.Errorf("expected volume %f, got %f", expected, client.setVolCalls[0])
	}
	if !rr.State.Camilla.VolumeKnown || rr.State.Camilla.VolumeDB != expected {
		t.Errorf("expected observed volume %f, got %f (known=%v)", expected, rr.State.Camilla.VolumeDB, rr.State.Camilla.VolumeKnown)
	}
}

// TestReducer_VolumeStep_DefaultStepSize tests default step size when DbPerStep is 0 via reducer.
func TestReducer_VolumeStep_DefaultStepSize(t *testing.T) {
	client := newMockCamillaDSPClient(-30.0)
	cfg := VelocityConfig{
		MinDB: -65.0,
		MaxDB: 0.0,
	}
	state := &DaemonState{}
	state.SetObservedVolume(-30.0, time.Now())
	state.VolumeCtrl.TargetDB = -30.0

	// DbPerStep is 0 -> should use defaultRotaryDbPerStep
	rr := Reduce(state, TimedEvent{Event: VolumeStep{Steps: 2, DbPerStep: 0}, At: time.Now()}, cfg, RotaryConfig{})
	rr = Reduce(rr.State, Tick{Now: time.Now(), Dt: 0.01}, cfg, RotaryConfig{})

	if len(rr.Commands) != 1 {
		t.Fatalf("expected 1 command on Tick, got %d", len(rr.Commands))
	}
	cmd, ok := rr.Commands[0].(CmdSetVolume)
	if !ok {
		t.Fatalf("expected CmdSetVolume, got %T", rr.Commands[0])
	}

	// 2 steps * defaultRotaryDbPerStep(0.5) = +1.0 dB => -29.0
	expected := -29.0
	if cmd.TargetDB != expected {
		t.Errorf("expected CmdSetVolume target %f, got %f", expected, cmd.TargetDB)
	}

	currentVol, err := client.SetVolume(cmd.TargetDB)
	if err != nil {
		t.Fatalf("SetVolume failed: %v", err)
	}
	rr = Reduce(rr.State, CamillaVolumeObserved{VolumeDB: currentVol, At: time.Now()}, cfg, RotaryConfig{})

	if len(client.setVolCalls) != 1 {
		t.Fatalf("expected 1 SetVolume call after executing command, got %d", len(client.setVolCalls))
	}
	if client.setVolCalls[0] != expected {
		t.Errorf("expected volume %f, got %f", expected, client.setVolCalls[0])
	}
}

// TestReducer_VolumeStep_VolumeUnknown tests behavior when observed volume is unknown via reducer.
func TestReducer_VolumeStep_VolumeUnknown(t *testing.T) {
	client := newMockCamillaDSPClient(-30.0)
	cfg := VelocityConfig{
		MinDB: -65.0,
		MaxDB: 0.0,
	}
	// Do not set observed volume in daemon state; this should fall back to controller TargetDB (initially 0.0).
	state := &DaemonState{}

	rr := Reduce(state, TimedEvent{Event: VolumeStep{Steps: 2, DbPerStep: 0.5}, At: time.Now()}, cfg, RotaryConfig{})
	rr = Reduce(rr.State, Tick{Now: time.Now(), Dt: 0.01}, cfg, RotaryConfig{})

	if len(rr.Commands) != 1 {
		t.Fatalf("expected 1 command on Tick, got %d", len(rr.Commands))
	}
	cmd, ok := rr.Commands[0].(CmdSetVolume)
	if !ok {
		t.Fatalf("expected CmdSetVolume, got %T", rr.Commands[0])
	}

	// baseline 0.0 + 1.0 => 1.0, clamped to MaxDB => 0.0
	expected := 0.0
	if cmd.TargetDB != expected {
		t.Errorf("expected CmdSetVolume target %f, got %f", expected, cmd.TargetDB)
	}

	currentVol, err := client.SetVolume(cmd.TargetDB)
	if err != nil {
		t.Fatalf("SetVolume failed: %v", err)
	}
	rr = Reduce(rr.State, CamillaVolumeObserved{VolumeDB: currentVol, At: time.Now()}, cfg, RotaryConfig{})

	if len(client.setVolCalls) != 1 {
		t.Fatalf("expected 1 SetVolume call after executing command, got %d", len(client.setVolCalls))
	}
	if client.setVolCalls[0] != expected {
		t.Errorf("expected volume %f, got %f", expected, client.setVolCalls[0])
	}
}

// TestReducer_VolumeStep_MultipleSteps tests multiple sequential steps via reducer.
func TestReducer_VolumeStep_MultipleSteps(t *testing.T) {
	client := newMockCamillaDSPClient(-30.0)
	cfg := VelocityConfig{
		MinDB: -65.0,
		MaxDB: 0.0,
	}
	state := &DaemonState{}
	state.SetObservedVolume(-30.0, time.Now())
	state.VolumeCtrl.TargetDB = -30.0

	rr := Reduce(state, TimedEvent{Event: VolumeStep{Steps: 2, DbPerStep: 0.5}, At: time.Now()}, cfg, RotaryConfig{})
	rr = Reduce(rr.State, Tick{Now: time.Now(), Dt: 0.01}, cfg, RotaryConfig{})
	cmd1 := rr.Commands[0].(CmdSetVolume)
	v1, _ := client.SetVolume(cmd1.TargetDB)
	rr = Reduce(rr.State, CamillaVolumeObserved{VolumeDB: v1, At: time.Now()}, cfg, RotaryConfig{})

	rr = Reduce(rr.State, TimedEvent{Event: VolumeStep{Steps: 2, DbPerStep: 0.5}, At: time.Now()}, cfg, RotaryConfig{})
	rr = Reduce(rr.State, Tick{Now: time.Now(), Dt: 0.01}, cfg, RotaryConfig{})
	cmd2 := rr.Commands[0].(CmdSetVolume)
	v2, _ := client.SetVolume(cmd2.TargetDB)
	rr = Reduce(rr.State, CamillaVolumeObserved{VolumeDB: v2, At: time.Now()}, cfg, RotaryConfig{})

	rr = Reduce(rr.State, TimedEvent{Event: VolumeStep{Steps: -1, DbPerStep: 0.5}, At: time.Now()}, cfg, RotaryConfig{})
	rr = Reduce(rr.State, Tick{Now: time.Now(), Dt: 0.01}, cfg, RotaryConfig{})
	cmd3 := rr.Commands[0].(CmdSetVolume)
	v3, _ := client.SetVolume(cmd3.TargetDB)
	rr = Reduce(rr.State, CamillaVolumeObserved{VolumeDB: v3, At: time.Now()}, cfg, RotaryConfig{})

	if len(client.setVolCalls) != 3 {
		t.Fatalf("expected 3 SetVolume calls, got %d", len(client.setVolCalls))
	}

	expected := -28.5
	if client.setVolCalls[2] != expected {
		t.Errorf("expected final volume %f, got %f", expected, client.setVolCalls[2])
	}
	if !rr.State.Camilla.VolumeKnown || rr.State.Camilla.VolumeDB != expected {
		t.Errorf("expected daemon observed volume %f, got %f (known=%v)", expected, rr.State.Camilla.VolumeDB, rr.State.Camilla.VolumeKnown)
	}
}

// TestReducer_VolumeStep_LargeStepSize tests large step sizes via reducer.
func TestReducer_VolumeStep_LargeStepSize(t *testing.T) {
	client := newMockCamillaDSPClient(-30.0)
	cfg := VelocityConfig{
		MinDB: -65.0,
		MaxDB: 0.0,
	}
	state := &DaemonState{}
	state.SetObservedVolume(-30.0, time.Now())
	state.VolumeCtrl.TargetDB = -30.0

	rr := Reduce(state, TimedEvent{Event: VolumeStep{Steps: 3, DbPerStep: 1.0}, At: time.Now()}, cfg, RotaryConfig{})
	rr = Reduce(rr.State, Tick{Now: time.Now(), Dt: 0.01}, cfg, RotaryConfig{})

	if len(rr.Commands) != 1 {
		t.Fatalf("expected 1 command on Tick, got %d", len(rr.Commands))
	}
	cmd, ok := rr.Commands[0].(CmdSetVolume)
	if !ok {
		t.Fatalf("expected CmdSetVolume, got %T", rr.Commands[0])
	}

	expected := -27.0
	if cmd.TargetDB != expected {
		t.Errorf("expected CmdSetVolume target %f, got %f", expected, cmd.TargetDB)
	}

	currentVol, err := client.SetVolume(cmd.TargetDB)
	if err != nil {
		t.Fatalf("SetVolume failed: %v", err)
	}
	rr = Reduce(rr.State, CamillaVolumeObserved{VolumeDB: currentVol, At: time.Now()}, cfg, RotaryConfig{})

	if len(client.setVolCalls) != 1 {
		t.Fatalf("expected 1 SetVolume call after executing command, got %d", len(client.setVolCalls))
	}
	if client.setVolCalls[0] != expected {
		t.Errorf("expected volume %f, got %f", expected, client.setVolCalls[0])
	}
}

// TestReducer_VolumeHeld_Integration tests that VolumeHeld doesn't interfere with VolumeStep via reducer.
func TestReducer_VolumeHeld_Integration(t *testing.T) {
	client := newMockCamillaDSPClient(-30.0)
	cfg := VelocityConfig{
		MinDB:        -65.0,
		MaxDB:        0.0,
		VelMaxDBPerS: 15.0,
		AccelTime:    2.0,
		DecayTau:     0.2,
	}
	state := &DaemonState{}
	state.SetObservedVolume(-30.0, time.Now())
	state.VolumeCtrl.TargetDB = -30.0

	// Hold, then step, then release. Rotary step should cancel hold movement.
	rr := Reduce(state, TimedEvent{Event: VolumeHeld{Direction: 1}, At: time.Now()}, cfg, RotaryConfig{})
	rr = Reduce(rr.State, TimedEvent{Event: VolumeStep{Steps: 2, DbPerStep: 0.5}, At: time.Now()}, cfg, RotaryConfig{})
	rr = Reduce(rr.State, TimedEvent{Event: VolumeRelease{}, At: time.Now()}, cfg, RotaryConfig{})

	// Flush to commands on Tick
	rr = Reduce(rr.State, Tick{Now: time.Now(), Dt: 0.01}, cfg, RotaryConfig{})

	if len(rr.Commands) != 1 {
		t.Fatalf("expected 1 command on Tick, got %d", len(rr.Commands))
	}
	cmd, ok := rr.Commands[0].(CmdSetVolume)
	if !ok {
		t.Fatalf("expected CmdSetVolume, got %T", rr.Commands[0])
	}

	expected := -29.0
	if cmd.TargetDB != expected {
		t.Errorf("expected CmdSetVolume target %f, got %f", expected, cmd.TargetDB)
	}

	currentVol, err := client.SetVolume(cmd.TargetDB)
	if err != nil {
		t.Fatalf("SetVolume failed: %v", err)
	}
	rr = Reduce(rr.State, CamillaVolumeObserved{VolumeDB: currentVol, At: time.Now()}, cfg, RotaryConfig{})

	if len(client.setVolCalls) != 1 {
		t.Fatalf("expected 1 SetVolume call after executing command, got %d", len(client.setVolCalls))
	}
	if client.setVolCalls[0] != expected {
		t.Errorf("expected volume %f, got %f", expected, client.setVolCalls[0])
	}
}

// TestReducer_ToggleMute tests that ToggleMute results in a CmdToggleMute on Tick and updates observed state on reply.
func TestReducer_ToggleMute(t *testing.T) {
	client := newMockCamillaDSPClient(-30.0)
	cfg := VelocityConfig{
		MinDB: -65.0,
		MaxDB: 0.0,
	}
	state := &DaemonState{}
	state.SetObservedMute(false, time.Now())

	// Reduce action: should set intent, no command until Tick
	rr := Reduce(state, TimedEvent{Event: ToggleMute{}, At: time.Now()}, cfg, RotaryConfig{})

	if client.toggleCalls != 0 {
		t.Errorf("expected 0 ToggleMute calls before executing reducer commands, got %d", client.toggleCalls)
	}

	// Drive a Tick to flush intents into commands
	rr = Reduce(rr.State, Tick{Now: time.Now(), Dt: 0.01}, cfg, RotaryConfig{})

	if len(rr.Commands) != 1 {
		t.Fatalf("expected 1 command on Tick, got %d", len(rr.Commands))
	}
	if _, ok := rr.Commands[0].(CmdToggleMute); !ok {
		t.Fatalf("expected CmdToggleMute, got %T", rr.Commands[0])
	}

	// Execute command and feed observation
	muted, err := client.ToggleMute()
	if err != nil {
		t.Fatalf("ToggleMute failed: %v", err)
	}
	rr = Reduce(rr.State, CamillaMuteObserved{Muted: muted, At: time.Now()}, cfg, RotaryConfig{})

	if client.toggleCalls != 1 {
		t.Errorf("expected 1 ToggleMute call after executing command, got %d", client.toggleCalls)
	}
	if !rr.State.Camilla.MuteKnown || !rr.State.Camilla.Muted {
		t.Error("expected daemon state to have observed mute=true after observation")
	}

	// Second toggle
	rr = Reduce(rr.State, TimedEvent{Event: ToggleMute{}, At: time.Now()}, cfg, RotaryConfig{})
	rr = Reduce(rr.State, Tick{Now: time.Now(), Dt: 0.01}, cfg, RotaryConfig{})

	if len(rr.Commands) != 1 {
		t.Fatalf("expected 1 command on second Tick, got %d", len(rr.Commands))
	}
	if _, ok := rr.Commands[0].(CmdToggleMute); !ok {
		t.Fatalf("expected CmdToggleMute on second toggle, got %T", rr.Commands[0])
	}

	muted, err = client.ToggleMute()
	if err != nil {
		t.Fatalf("ToggleMute failed: %v", err)
	}
	rr = Reduce(rr.State, CamillaMuteObserved{Muted: muted, At: time.Now()}, cfg, RotaryConfig{})

	if client.toggleCalls != 2 {
		t.Errorf("expected 2 ToggleMute calls after executing second command, got %d", client.toggleCalls)
	}
	if !rr.State.Camilla.MuteKnown || rr.State.Camilla.Muted {
		t.Error("expected daemon state to have observed mute=false after second observation")
	}
}

func TestReducer_InertiaDecayAfterRelease(t *testing.T) {
	cfg := VelocityConfig{
		Mode:         VelocityModeAccelerating,
		MinDB:        -65.0,
		MaxDB:        0.0,
		VelMaxDBPerS: 15.0,
		AccelTime:    2.0,
		DecayTau:     0.2,
		// Disable auto-release behavior so this test focuses purely on inertia/decay.
		HoldTimeout: 0,
		// Disable danger-zone effects for this unit test.
		DangerZoneDB:            0,
		DangerVelMaxDBPerS:      0,
		DangerVelMinNear0DBPerS: 0,
	}

	// Start from a stable observed volume.
	state := &DaemonState{}
	now := time.Now()
	state.SetObservedVolume(-30.0, now)
	state.VolumeCtrl.TargetDB = -30.0

	// Press/hold volume up for a few ticks to build up velocity (acceleration).
	state = Reduce(state, TimedEvent{Event: VolumeHeld{Direction: 1}, At: now}, cfg, RotaryConfig{}).State
	for i := 0; i < 10; i++ {
		now = now.Add(33 * time.Millisecond)
		state = Reduce(state, Tick{Now: now, Dt: 0.033}, cfg, RotaryConfig{}).State

		// In the real program, after each Tick the daemon executes CmdSetVolume and then
		// feeds CamillaVolumeObserved back into the reducer. If we don't do that here,
		// the reducer's baseline selection may snap back to the observed volume and
		// fight the controller's inertial motion.
		state = Reduce(state, CamillaVolumeObserved{VolumeDB: state.VolumeCtrl.TargetDB, At: now}, cfg, RotaryConfig{}).State
	}

	// The controller should have built some positive velocity.
	if state.VolumeCtrl.VelocityDBPerS <= 0 {
		t.Fatalf("expected positive velocity after holding up, got %f", state.VolumeCtrl.VelocityDBPerS)
	}

	// Release should NOT zero velocity immediately in accelerating mode; it should decay over time.
	state = Reduce(state, TimedEvent{Event: VolumeRelease{}, At: now}, cfg, RotaryConfig{}).State
	if state.VolumeCtrl.HeldDirection != 0 {
		t.Fatalf("expected heldDirection=0 after release, got %d", state.VolumeCtrl.HeldDirection)
	}

	vel0 := state.VolumeCtrl.VelocityDBPerS
	target0 := state.VolumeCtrl.TargetDB

	// After release, the target should continue moving for at least one tick (inertia),
	// and velocity should start decaying.
	now = now.Add(33 * time.Millisecond)
	state = Reduce(state, Tick{Now: now, Dt: 0.033}, cfg, RotaryConfig{}).State
	state = Reduce(state, CamillaVolumeObserved{VolumeDB: state.VolumeCtrl.TargetDB, At: now}, cfg, RotaryConfig{}).State

	target1 := state.VolumeCtrl.TargetDB
	vel1 := state.VolumeCtrl.VelocityDBPerS

	// Use a small epsilon because float math and rounding can make tiny reversals.
	if target1 <= target0+1e-9 {
		t.Fatalf("expected target to continue increasing after release due to inertia, target0=%f target1=%f", target0, target1)
	}
	if !(vel1 > 0 && vel1 < vel0) {
		t.Fatalf("expected velocity to decay but remain positive after one tick, vel0=%f vel1=%f", vel0, vel1)
	}

	// After several ticks, velocity should continue decaying toward 0 (monotonic-ish).
	// We don't require strict monotonicity due to float rounding, but it should not jump up.
	prevVel := vel1
	prevTarget := target1
	for i := 0; i < 10; i++ {
		now = now.Add(33 * time.Millisecond)
		state = Reduce(state, Tick{Now: now, Dt: 0.033}, cfg, RotaryConfig{}).State
		state = Reduce(state, CamillaVolumeObserved{VolumeDB: state.VolumeCtrl.TargetDB, At: now}, cfg, RotaryConfig{}).State

		v := state.VolumeCtrl.VelocityDBPerS
		tgt := state.VolumeCtrl.TargetDB

		if v < 0 {
			t.Fatalf("expected velocity to remain non-negative during decay after release, got %f", v)
		}
		if v > prevVel+1e-6 {
			t.Fatalf("expected velocity not to increase during decay, prev=%f now=%f", prevVel, v)
		}
		if tgt < prevTarget-1e-9 {
			t.Fatalf("expected target not to decrease while velocity is positive, prev=%f now=%f", prevTarget, tgt)
		}

		prevVel = v
		prevTarget = tgt
	}

	// Eventually, velocity should get close to 0.
	if math.Abs(state.VolumeCtrl.VelocityDBPerS) > 0.5 {
		t.Fatalf("expected velocity to decay near 0 after several ticks, got %f", state.VolumeCtrl.VelocityDBPerS)
	}
}

func TestReducer_AccelerationBuildsVelocityWhileHeld(t *testing.T) {
	cfg := VelocityConfig{
		Mode:         VelocityModeAccelerating,
		MinDB:        -65.0,
		MaxDB:        0.0,
		VelMaxDBPerS: 15.0,
		AccelTime:    2.0, // acceleration = 7.5 dB/s^2
		DecayTau:     0.2, // irrelevant while held
		HoldTimeout:  0,   // disable auto-release to keep the test deterministic
		DangerZoneDB: 0,   // keep dynamics simple
	}
	state := &DaemonState{}
	now := time.Now()
	state.SetObservedVolume(-30.0, now)
	state.VolumeCtrl.TargetDB = -30.0

	// Start holding volume up.
	state = Reduce(state, TimedEvent{Event: VolumeHeld{Direction: 1}, At: now}, cfg, RotaryConfig{}).State

	// Step a few ticks and verify velocity increases over time until capped.
	var prevVel float64
	for i := 0; i < 8; i++ {
		now = now.Add(33 * time.Millisecond)
		state = Reduce(state, Tick{Now: now, Dt: 0.033}, cfg, RotaryConfig{}).State
		// Simulate that CamillaDSP applies the desired volume (keeps baseline aligned).
		state = Reduce(state, CamillaVolumeObserved{VolumeDB: state.VolumeCtrl.TargetDB, At: now}, cfg, RotaryConfig{}).State

		v := state.VolumeCtrl.VelocityDBPerS
		if v < 0 {
			t.Fatalf("expected non-negative velocity while holding up, got %f", v)
		}
		if i > 0 && v < prevVel-1e-6 {
			t.Fatalf("expected velocity to be non-decreasing while held (accelerating), prev=%f now=%f", prevVel, v)
		}
		if v > cfg.VelMaxDBPerS+1e-6 {
			t.Fatalf("expected velocity to be capped at vel_max_db_per_sec=%f, got %f", cfg.VelMaxDBPerS, v)
		}
		prevVel = v
	}

	// Sanity check: after a few ticks, velocity should be meaningfully > 0 (i.e., acceleration happened).
	if state.VolumeCtrl.VelocityDBPerS < 0.5 {
		t.Fatalf("expected velocity to build up due to acceleration, got %f", state.VolumeCtrl.VelocityDBPerS)
	}
}
