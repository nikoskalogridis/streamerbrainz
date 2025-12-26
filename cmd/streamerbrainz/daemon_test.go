package main

import (
	"log/slog"
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
	vel := newVelocityState(cfg)
	vel.updateVolume(-30.0)

	_ = slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))

	state := &DaemonState{}
	state.SetObservedVolume(-30.0, time.Now())

	// Reduce the action
	rr := Reduce(state, ActionEvent{Action: VolumeStep{Steps: 2, DbPerStep: 0.5}, At: time.Now()}, vel, cfg)

	// No side effects have run yet
	if len(client.setVolCalls) != 0 {
		t.Fatalf("expected 0 SetVolume calls before executing reducer commands, got %d", len(client.setVolCalls))
	}

	// Reducer should have emitted a CmdSetVolume once Tick is processed; here we follow the current reducer contract:
	// it records desired volume intent on the action, and emits commands on Tick.
	// So we drive a Tick to flush intents into commands.
	rr = Reduce(rr.State, Tick{Now: time.Now(), Dt: 0.01}, vel, cfg)

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
	rr = Reduce(rr.State, CamillaVolumeObserved{VolumeDB: currentVol, At: time.Now()}, vel, cfg)

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
	vel := newVelocityState(cfg)
	vel.updateVolume(-30.0)

	state := &DaemonState{}
	state.SetObservedVolume(-30.0, time.Now())

	rr := Reduce(state, ActionEvent{Action: VolumeStep{Steps: -3, DbPerStep: 0.5}, At: time.Now()}, vel, cfg)
	rr = Reduce(rr.State, Tick{Now: time.Now(), Dt: 0.01}, vel, cfg)

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
	rr = Reduce(rr.State, CamillaVolumeObserved{VolumeDB: currentVol, At: time.Now()}, vel, cfg)

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
	vel := newVelocityState(cfg)
	vel.updateVolume(-1.0)

	state := &DaemonState{}
	state.SetObservedVolume(-1.0, time.Now())

	rr := Reduce(state, ActionEvent{Action: VolumeStep{Steps: 10, DbPerStep: 0.5}, At: time.Now()}, vel, cfg)
	rr = Reduce(rr.State, Tick{Now: time.Now(), Dt: 0.01}, vel, cfg)

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
	rr = Reduce(rr.State, CamillaVolumeObserved{VolumeDB: currentVol, At: time.Now()}, vel, cfg)

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
	vel := newVelocityState(cfg)
	vel.updateVolume(-64.0)

	state := &DaemonState{}
	state.SetObservedVolume(-64.0, time.Now())

	// Reduce action then Tick to flush into commands
	rr := Reduce(state, ActionEvent{Action: VolumeStep{Steps: -10, DbPerStep: 0.5}, At: time.Now()}, vel, cfg)
	rr = Reduce(rr.State, Tick{Now: time.Now(), Dt: 0.01}, vel, cfg)

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
	rr = Reduce(rr.State, CamillaVolumeObserved{VolumeDB: currentVol, At: time.Now()}, vel, cfg)

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
	vel := newVelocityState(cfg)
	vel.updateVolume(-30.0)

	state := &DaemonState{}
	state.SetObservedVolume(-30.0, time.Now())

	// DbPerStep is 0 -> should use defaultRotaryDbPerStep
	rr := Reduce(state, ActionEvent{Action: VolumeStep{Steps: 2, DbPerStep: 0}, At: time.Now()}, vel, cfg)
	rr = Reduce(rr.State, Tick{Now: time.Now(), Dt: 0.01}, vel, cfg)

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
	rr = Reduce(rr.State, CamillaVolumeObserved{VolumeDB: currentVol, At: time.Now()}, vel, cfg)

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
	vel := newVelocityState(cfg)
	// Do not set observed volume in daemon state; this should fall back to vel.getTarget() (initially 0.0)

	state := &DaemonState{}

	rr := Reduce(state, ActionEvent{Action: VolumeStep{Steps: 2, DbPerStep: 0.5}, At: time.Now()}, vel, cfg)
	rr = Reduce(rr.State, Tick{Now: time.Now(), Dt: 0.01}, vel, cfg)

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
	rr = Reduce(rr.State, CamillaVolumeObserved{VolumeDB: currentVol, At: time.Now()}, vel, cfg)

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
	vel := newVelocityState(cfg)
	vel.updateVolume(-30.0)

	state := &DaemonState{}
	state.SetObservedVolume(-30.0, time.Now())

	rr := Reduce(state, ActionEvent{Action: VolumeStep{Steps: 2, DbPerStep: 0.5}, At: time.Now()}, vel, cfg)
	rr = Reduce(rr.State, Tick{Now: time.Now(), Dt: 0.01}, vel, cfg)
	cmd1 := rr.Commands[0].(CmdSetVolume)
	v1, _ := client.SetVolume(cmd1.TargetDB)
	rr = Reduce(rr.State, CamillaVolumeObserved{VolumeDB: v1, At: time.Now()}, vel, cfg)

	rr = Reduce(rr.State, ActionEvent{Action: VolumeStep{Steps: 2, DbPerStep: 0.5}, At: time.Now()}, vel, cfg)
	rr = Reduce(rr.State, Tick{Now: time.Now(), Dt: 0.01}, vel, cfg)
	cmd2 := rr.Commands[0].(CmdSetVolume)
	v2, _ := client.SetVolume(cmd2.TargetDB)
	rr = Reduce(rr.State, CamillaVolumeObserved{VolumeDB: v2, At: time.Now()}, vel, cfg)

	rr = Reduce(rr.State, ActionEvent{Action: VolumeStep{Steps: -1, DbPerStep: 0.5}, At: time.Now()}, vel, cfg)
	rr = Reduce(rr.State, Tick{Now: time.Now(), Dt: 0.01}, vel, cfg)
	cmd3 := rr.Commands[0].(CmdSetVolume)
	v3, _ := client.SetVolume(cmd3.TargetDB)
	rr = Reduce(rr.State, CamillaVolumeObserved{VolumeDB: v3, At: time.Now()}, vel, cfg)

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
	vel := newVelocityState(cfg)
	vel.updateVolume(-30.0)

	state := &DaemonState{}
	state.SetObservedVolume(-30.0, time.Now())

	rr := Reduce(state, ActionEvent{Action: VolumeStep{Steps: 3, DbPerStep: 1.0}, At: time.Now()}, vel, cfg)
	rr = Reduce(rr.State, Tick{Now: time.Now(), Dt: 0.01}, vel, cfg)

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
	rr = Reduce(rr.State, CamillaVolumeObserved{VolumeDB: currentVol, At: time.Now()}, vel, cfg)

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
	vel := newVelocityState(cfg)
	vel.updateVolume(-30.0)

	state := &DaemonState{}
	state.SetObservedVolume(-30.0, time.Now())

	// Hold, then step, then release. Rotary step should cancel hold movement.
	rr := Reduce(state, ActionEvent{Action: VolumeHeld{Direction: 1}, At: time.Now()}, vel, cfg)
	rr = Reduce(rr.State, ActionEvent{Action: VolumeStep{Steps: 2, DbPerStep: 0.5}, At: time.Now()}, vel, cfg)
	rr = Reduce(rr.State, ActionEvent{Action: VolumeRelease{}, At: time.Now()}, vel, cfg)

	// Flush to commands on Tick
	rr = Reduce(rr.State, Tick{Now: time.Now(), Dt: 0.01}, vel, cfg)

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
	rr = Reduce(rr.State, CamillaVolumeObserved{VolumeDB: currentVol, At: time.Now()}, vel, cfg)

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
	vel := newVelocityState(cfg)
	vel.updateVolume(-30.0)

	state := &DaemonState{}
	state.SetObservedMute(false, time.Now())

	// Reduce action: should set intent, no command until Tick
	rr := Reduce(state, ActionEvent{Action: ToggleMute{}, At: time.Now()}, vel, cfg)

	if client.toggleCalls != 0 {
		t.Errorf("expected 0 ToggleMute calls before executing reducer commands, got %d", client.toggleCalls)
	}

	// Drive a Tick to flush intents into commands
	rr = Reduce(rr.State, Tick{Now: time.Now(), Dt: 0.01}, vel, cfg)

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
	rr = Reduce(rr.State, CamillaMuteObserved{Muted: muted, At: time.Now()}, vel, cfg)

	if client.toggleCalls != 1 {
		t.Errorf("expected 1 ToggleMute call after executing command, got %d", client.toggleCalls)
	}
	if !rr.State.Camilla.MuteKnown || !rr.State.Camilla.Muted {
		t.Error("expected daemon state to have observed mute=true after observation")
	}

	// Second toggle
	rr = Reduce(rr.State, ActionEvent{Action: ToggleMute{}, At: time.Now()}, vel, cfg)
	rr = Reduce(rr.State, Tick{Now: time.Now(), Dt: 0.01}, vel, cfg)

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
	rr = Reduce(rr.State, CamillaMuteObserved{Muted: muted, At: time.Now()}, vel, cfg)

	if client.toggleCalls != 2 {
		t.Errorf("expected 2 ToggleMute calls after executing second command, got %d", client.toggleCalls)
	}
	if !rr.State.Camilla.MuteKnown || rr.State.Camilla.Muted {
		t.Error("expected daemon state to have observed mute=false after second observation")
	}
}
