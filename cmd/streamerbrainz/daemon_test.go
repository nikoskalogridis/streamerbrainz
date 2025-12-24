package main

import (
	"log/slog"
	"os"
	"testing"
)

// mockCamillaDSPClient is a test double for CamillaDSPClient
type mockCamillaDSPClient struct {
	volume      float64
	muted       bool
	setVolCalls []float64
	getVolCalls int
	toggleCalls int
}

func newMockCamillaDSPClient(initialVolume float64) *mockCamillaDSPClient {
	return &mockCamillaDSPClient{
		volume:      initialVolume,
		muted:       false,
		setVolCalls: make([]float64, 0),
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

func (m *mockCamillaDSPClient) ToggleMute() error {
	m.toggleCalls++
	m.muted = !m.muted
	return nil
}

func (m *mockCamillaDSPClient) Close() error {
	return nil
}

// TestHandleAction_VolumeStep_Basic tests basic VolumeStep handling
func TestHandleAction_VolumeStep_Basic(t *testing.T) {
	client := newMockCamillaDSPClient(-30.0)
	cfg := VelocityConfig{
		MinDB: -65.0,
		MaxDB: 0.0,
	}
	velState := newVelocityState(cfg)
	velState.updateVolume(-30.0)

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))

	// Step up by 2 steps at 0.5 dB/step = +1.0 dB
	action := VolumeStep{
		Steps:     2,
		DbPerStep: 0.5,
	}

	handleAction(action, client, velState, logger)

	// Should set volume to -30.0 + 1.0 = -29.0
	if len(client.setVolCalls) != 1 {
		t.Fatalf("expected 1 SetVolume call, got %d", len(client.setVolCalls))
	}

	expected := -29.0
	if client.setVolCalls[0] != expected {
		t.Errorf("expected volume %f, got %f", expected, client.setVolCalls[0])
	}

	// Verify velocity state was updated
	if velState.getTarget() != expected {
		t.Errorf("expected velocity state target %f, got %f", expected, velState.getTarget())
	}
}

// TestHandleAction_VolumeStep_Negative tests negative steps (volume down)
func TestHandleAction_VolumeStep_Negative(t *testing.T) {
	client := newMockCamillaDSPClient(-30.0)
	cfg := VelocityConfig{
		MinDB: -65.0,
		MaxDB: 0.0,
	}
	velState := newVelocityState(cfg)
	velState.updateVolume(-30.0)

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))

	// Step down by 3 steps at 0.5 dB/step = -1.5 dB
	action := VolumeStep{
		Steps:     -3,
		DbPerStep: 0.5,
	}

	handleAction(action, client, velState, logger)

	expected := -31.5
	if client.setVolCalls[0] != expected {
		t.Errorf("expected volume %f, got %f", expected, client.setVolCalls[0])
	}
}

// TestHandleAction_VolumeStep_ClampMax tests clamping to maximum volume
func TestHandleAction_VolumeStep_ClampMax(t *testing.T) {
	client := newMockCamillaDSPClient(-1.0)
	cfg := VelocityConfig{
		MinDB: -65.0,
		MaxDB: 0.0,
	}
	velState := newVelocityState(cfg)
	velState.updateVolume(-1.0)

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))

	// Try to step up by 5 dB (would exceed max)
	action := VolumeStep{
		Steps:     10,
		DbPerStep: 0.5,
	}

	handleAction(action, client, velState, logger)

	// Should clamp to MaxDB (0.0)
	expected := 0.0
	if client.setVolCalls[0] != expected {
		t.Errorf("expected volume clamped to %f, got %f", expected, client.setVolCalls[0])
	}
}

// TestHandleAction_VolumeStep_ClampMin tests clamping to minimum volume
func TestHandleAction_VolumeStep_ClampMin(t *testing.T) {
	client := newMockCamillaDSPClient(-64.0)
	cfg := VelocityConfig{
		MinDB: -65.0,
		MaxDB: 0.0,
	}
	velState := newVelocityState(cfg)
	velState.updateVolume(-64.0)

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))

	// Try to step down by 5 dB (would go below min)
	action := VolumeStep{
		Steps:     -10,
		DbPerStep: 0.5,
	}

	handleAction(action, client, velState, logger)

	// Should clamp to MinDB (-65.0)
	expected := -65.0
	if client.setVolCalls[0] != expected {
		t.Errorf("expected volume clamped to %f, got %f", expected, client.setVolCalls[0])
	}
}

// TestHandleAction_VolumeStep_DefaultStepSize tests default step size when DbPerStep is 0
func TestHandleAction_VolumeStep_DefaultStepSize(t *testing.T) {
	client := newMockCamillaDSPClient(-30.0)
	cfg := VelocityConfig{
		MinDB: -65.0,
		MaxDB: 0.0,
	}
	velState := newVelocityState(cfg)
	velState.updateVolume(-30.0)

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))

	// DbPerStep is 0, should use default (0.5)
	action := VolumeStep{
		Steps:     2,
		DbPerStep: 0,
	}

	handleAction(action, client, velState, logger)

	// Should use defaultRotaryDbPerStep (0.5)
	// 2 steps * 0.5 = 1.0 dB
	expected := -29.0
	if client.setVolCalls[0] != expected {
		t.Errorf("expected volume %f (using default step size), got %f", expected, client.setVolCalls[0])
	}
}

// TestHandleAction_VolumeStep_VolumeUnknown tests behavior when volume is unknown
func TestHandleAction_VolumeStep_VolumeUnknown(t *testing.T) {
	client := newMockCamillaDSPClient(-30.0)
	cfg := VelocityConfig{
		MinDB: -65.0,
		MaxDB: 0.0,
	}
	velState := newVelocityState(cfg)
	// Don't call updateVolume - leave volume unknown

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))

	action := VolumeStep{
		Steps:     2,
		DbPerStep: 0.5,
	}

	handleAction(action, client, velState, logger)

	// Should query volume first
	if client.getVolCalls != 1 {
		t.Errorf("expected 1 GetVolume call to query current volume, got %d", client.getVolCalls)
	}

	// Then apply step: -30.0 + (2 * 0.5) = -29.0
	expected := -29.0
	if len(client.setVolCalls) != 1 {
		t.Fatalf("expected 1 SetVolume call, got %d", len(client.setVolCalls))
	}
	if client.setVolCalls[0] != expected {
		t.Errorf("expected volume %f, got %f", expected, client.setVolCalls[0])
	}
}

// TestHandleAction_VolumeStep_MultipleSteps tests multiple sequential steps
func TestHandleAction_VolumeStep_MultipleSteps(t *testing.T) {
	client := newMockCamillaDSPClient(-30.0)
	cfg := VelocityConfig{
		MinDB: -65.0,
		MaxDB: 0.0,
	}
	velState := newVelocityState(cfg)
	velState.updateVolume(-30.0)

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))

	// First step: +1 dB
	handleAction(VolumeStep{Steps: 2, DbPerStep: 0.5}, client, velState, logger)

	// Second step: +1 dB
	handleAction(VolumeStep{Steps: 2, DbPerStep: 0.5}, client, velState, logger)

	// Third step: -0.5 dB
	handleAction(VolumeStep{Steps: -1, DbPerStep: 0.5}, client, velState, logger)

	// Verify final volume: -30.0 + 1.0 + 1.0 - 0.5 = -28.5
	if len(client.setVolCalls) != 3 {
		t.Fatalf("expected 3 SetVolume calls, got %d", len(client.setVolCalls))
	}

	expected := -28.5
	if client.setVolCalls[2] != expected {
		t.Errorf("expected final volume %f, got %f", expected, client.setVolCalls[2])
	}

	if velState.getTarget() != expected {
		t.Errorf("expected velocity state target %f, got %f", expected, velState.getTarget())
	}
}

// TestHandleAction_VolumeStep_LargeStepSize tests large step sizes
func TestHandleAction_VolumeStep_LargeStepSize(t *testing.T) {
	client := newMockCamillaDSPClient(-30.0)
	cfg := VelocityConfig{
		MinDB: -65.0,
		MaxDB: 0.0,
	}
	velState := newVelocityState(cfg)
	velState.updateVolume(-30.0)

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))

	// Large step size (velocity mode)
	action := VolumeStep{
		Steps:     3,
		DbPerStep: 1.0, // 2x default (velocity multiplier applied)
	}

	handleAction(action, client, velState, logger)

	// 3 steps * 1.0 = +3.0 dB
	expected := -27.0
	if client.setVolCalls[0] != expected {
		t.Errorf("expected volume %f, got %f", expected, client.setVolCalls[0])
	}
}

// TestHandleAction_VolumeHeld_Integration tests that VolumeHeld doesn't interfere with VolumeStep
func TestHandleAction_VolumeHeld_Integration(t *testing.T) {
	client := newMockCamillaDSPClient(-30.0)
	cfg := VelocityConfig{
		MinDB:        -65.0,
		MaxDB:        0.0,
		VelMaxDBPerS: 15.0,
		AccelTime:    2.0,
		DecayTau:     0.2,
	}
	velState := newVelocityState(cfg)
	velState.updateVolume(-30.0)

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))

	// Mix VolumeHeld and VolumeStep actions
	handleAction(VolumeHeld{Direction: 1}, client, velState, logger)
	handleAction(VolumeStep{Steps: 2, DbPerStep: 0.5}, client, velState, logger)
	handleAction(VolumeRelease{}, client, velState, logger)

	// VolumeStep should apply immediately regardless of held state
	// Only one SetVolume call from VolumeStep (VolumeHeld updates come from tick)
	if len(client.setVolCalls) != 1 {
		t.Fatalf("expected 1 SetVolume call from VolumeStep, got %d", len(client.setVolCalls))
	}

	expected := -29.0
	if client.setVolCalls[0] != expected {
		t.Errorf("expected volume %f from VolumeStep, got %f", expected, client.setVolCalls[0])
	}
}

// TestHandleAction_ToggleMute tests mute action
func TestHandleAction_ToggleMute(t *testing.T) {
	client := newMockCamillaDSPClient(-30.0)
	cfg := VelocityConfig{
		MinDB: -65.0,
		MaxDB: 0.0,
	}
	velState := newVelocityState(cfg)
	velState.updateVolume(-30.0)

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))

	// Toggle mute on
	handleAction(ToggleMute{}, client, velState, logger)
	if client.toggleCalls != 1 {
		t.Errorf("expected 1 ToggleMute call, got %d", client.toggleCalls)
	}
	if !client.muted {
		t.Error("expected mute to be on")
	}

	// Toggle mute off
	handleAction(ToggleMute{}, client, velState, logger)
	if client.toggleCalls != 2 {
		t.Errorf("expected 2 ToggleMute calls, got %d", client.toggleCalls)
	}
	if client.muted {
		t.Error("expected mute to be off")
	}
}
