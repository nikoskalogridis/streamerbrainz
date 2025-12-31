package main

import (
	"testing"
	"time"
)

func TestReduce_CamillaVolumeObserved_EmitsBroadcastOnlyOnRoundedChange(t *testing.T) {
	cfg := VelocityConfig{
		MinDB: -80,
		MaxDB: 0,
	}
	rotaryCfg := RotaryConfig{
		DbPerStep:          1.0,
		VelocityWindowMS:   250,
		VelocityThreshold:  3,
		VelocityMultiplier: 2,
	}

	t0 := time.Unix(1000, 0).UTC()

	// Start with unknown volume; first observation should broadcast.
	// Internal state keeps full precision; broadcast payload is rounded to 0.1 dB.
	s := &DaemonState{}
	rr := Reduce(s, CamillaVolumeObserved{VolumeDB: -12.04, At: t0}, cfg, rotaryCfg)

	if rr.State == nil {
		t.Fatalf("expected non-nil state")
	}
	if !rr.State.Camilla.VolumeKnown {
		t.Fatalf("expected volume to become known")
	}
	// Internal observed volume is full precision (no rounding applied to DaemonState).
	if rr.State.Camilla.VolumeDB != -12.04 {
		t.Fatalf("expected internal volume_db=-12.04, got %v", rr.State.Camilla.VolumeDB)
	}

	if got := len(rr.Broadcasts); got != 1 {
		t.Fatalf("expected 1 broadcast on first observed volume, got %d", got)
	}
	bc1, ok := rr.Broadcasts[0].(BroadcastVolumeChanged)
	if !ok {
		t.Fatalf("expected BroadcastVolumeChanged, got %T", rr.Broadcasts[0])
	}
	// Broadcast payload is rounded to 0.1 dB.
	if bc1.VolumeDB != -12.0 {
		t.Fatalf("expected broadcast volume_db=-12.0, got %v", bc1.VolumeDB)
	}

	// Second observation differs slightly but rounds to the same 0.1 dB -> should NOT broadcast.
	t1 := t0.Add(1 * time.Second)
	rr2 := Reduce(rr.State, CamillaVolumeObserved{VolumeDB: -12.01, At: t1}, cfg, rotaryCfg)

	if got := len(rr2.Broadcasts); got != 0 {
		t.Fatalf("expected 0 broadcasts when rounded volume unchanged, got %d (%T)", got, rr2.Broadcasts[0])
	}

	// Third observation crosses rounding boundary -> SHOULD broadcast.
	t2 := t1.Add(1 * time.Second)
	rr3 := Reduce(rr2.State, CamillaVolumeObserved{VolumeDB: -11.94, At: t2}, cfg, rotaryCfg)

	if got := len(rr3.Broadcasts); got != 1 {
		t.Fatalf("expected 1 broadcast when rounded volume changes, got %d", got)
	}
	bc3, ok := rr3.Broadcasts[0].(BroadcastVolumeChanged)
	if !ok {
		t.Fatalf("expected BroadcastVolumeChanged, got %T", rr3.Broadcasts[0])
	}
	// -11.94 rounds to -11.9 at 0.1 dB precision.
	if bc3.VolumeDB != -11.9 {
		t.Fatalf("expected broadcast volume_db=-11.9, got %v", bc3.VolumeDB)
	}
	if !bc3.At.Equal(t2) {
		t.Fatalf("expected broadcast timestamp %v, got %v", t2, bc3.At)
	}
}

func TestReduce_CamillaVolumeObserved_EmitsBroadcastWhenPrevKnownButRoundedDifferent(t *testing.T) {
	cfg := VelocityConfig{
		MinDB: -80,
		MaxDB: 0,
	}
	rotaryCfg := RotaryConfig{}

	t0 := time.Unix(2000, 0).UTC()

	// Seed state with known volume (full precision in internal state).
	s := &DaemonState{}
	s.Camilla.VolumeKnown = true
	s.Camilla.VolumeDB = -20.02
	s.Camilla.VolumeAt = t0.Add(-10 * time.Second)

	// Rounded values: -20.02 -> -20.0, -19.96 -> -20.0 => no broadcast.
	rr := Reduce(s, CamillaVolumeObserved{VolumeDB: -19.96, At: t0}, cfg, rotaryCfg)
	if got := len(rr.Broadcasts); got != 0 {
		t.Fatalf("expected 0 broadcasts when rounded volume unchanged, got %d", got)
	}

	// Rounded values: -19.94 -> -19.9 => broadcast.
	t1 := t0.Add(1 * time.Second)
	rr2 := Reduce(rr.State, CamillaVolumeObserved{VolumeDB: -19.94, At: t1}, cfg, rotaryCfg)
	if got := len(rr2.Broadcasts); got != 1 {
		t.Fatalf("expected 1 broadcast when rounded volume changes, got %d", got)
	}
	bc, ok := rr2.Broadcasts[0].(BroadcastVolumeChanged)
	if !ok {
		t.Fatalf("expected BroadcastVolumeChanged, got %T", rr2.Broadcasts[0])
	}
	if bc.VolumeDB != -19.9 {
		t.Fatalf("expected broadcast volume_db=-19.9, got %v", bc.VolumeDB)
	}
	if !bc.At.Equal(t1) {
		t.Fatalf("expected broadcast timestamp %v, got %v", t1, bc.At)
	}
}
