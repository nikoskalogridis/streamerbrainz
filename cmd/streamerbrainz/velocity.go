package main

import (
	"math"
	"time"
)

// VelocityMode selects how "press-and-hold" behaves.
//
// The goal is to allow a simpler constant-rate mode (with optional turbo)
// while reusing the existing knobs:
// - vel-max-db-per-sec
// - vel-accel-time
// - vel-decay-tau
//
// Interpretation:
//
// Accelerating mode (default):
// - vel-max-db-per-sec: maximum velocity
// - vel-accel-time: time to reach max velocity
// - vel-decay-tau: exponential decay time constant when released
//
// Constant mode:
// - vel-max-db-per-sec: base (normal) hold rate in dB/s
// - vel-accel-time: turbo multiplier (if > 1). turboRate = baseRate * vel-accel-time
// - vel-decay-tau: turbo activation delay in seconds (if > 0). after holding for this long, turbo engages
type VelocityMode string

const (
	VelocityModeAccelerating VelocityMode = "accelerating"
	VelocityModeConstant     VelocityMode = "constant"
)

// VelocityConfig contains all tunable parameters for the velocity engine.
type VelocityConfig struct {
	// Mode
	Mode VelocityMode

	// Core dynamics / rates
	VelMaxDBPerS float64 // Accelerating: max velocity; Constant: base hold rate (dB/s)
	AccelTime    float64 // Accelerating: time to reach max (s); Constant: turbo multiplier (unitless, >1 enables turbo)
	DecayTau     float64 // Accelerating: velocity decay tau (s); Constant: turbo delay (s), 0 disables turbo

	// Volume bounds
	MinDB float64
	MaxDB float64

	// Robustness
	HoldTimeout time.Duration // Auto-release if no hold events arrive in this duration
	MaxDt       float64       // Max dt allowed per update step (seconds). 0 disables clamping.

	// Danger zone (near max volume), ramp-up only
	DangerZoneDB            float64 // Size of danger zone below MaxDB (dB)
	DangerVelMaxDBPerS      float64 // Hard cap for ramp-up velocity inside danger zone (dB/s)
	DangerVelMinNear0DBPerS float64 // Minimum ramp-up velocity near MaxDB (dB/s), avoids "sticky" top
}

// velocityState manages smooth velocity-based volume control.
type velocityState struct {
	// Dynamics
	targetDB       float64 // Target volume in dB
	velocityDBPerS float64 // Current velocity in dB/s (signed). Used in accelerating mode.

	// Hold tracking
	heldDirection int // -1 for down, 0 for none, 1 for up
	lastHeldAt    time.Time

	// Turbo tracking (constant mode)
	holdBeganAt time.Time // first time we observed a hold for the current hold gesture

	// Server state
	currentVolume float64 // Last known actual volume from server
	volumeKnown   bool    // Whether we have a valid volume reading

	// Configuration
	cfg          VelocityConfig
	accelDBPerS2 float64 // Acceleration in dB/s² (accelerating mode only)
}

// newVelocityState creates a new velocity state with the given configuration.
func newVelocityState(cfg VelocityConfig) *velocityState {
	// Fill defaults.
	if cfg.Mode == "" {
		cfg.Mode = VelocityModeAccelerating
	}

	// Fill robustness defaults if caller leaves them empty.
	if cfg.HoldTimeout == 0 {
		// Many remotes repeat at ~100-200ms; 600ms avoids premature release while still being safe.
		cfg.HoldTimeout = 600 * time.Millisecond
	}
	if cfg.MaxDt == 0 {
		cfg.MaxDt = 0.5
	}
	if cfg.DangerZoneDB == 0 {
		cfg.DangerZoneDB = dangerZoneDB
	}
	if cfg.DangerVelMaxDBPerS == 0 {
		cfg.DangerVelMaxDBPerS = dangerVelMaxDBPerS
	}
	if cfg.DangerVelMinNear0DBPerS == 0 {
		cfg.DangerVelMinNear0DBPerS = dangerVelMinNear0DBPerS
	}

	// Precompute acceleration from VelMax / AccelTime (accelerating mode only).
	accelDBPerS2 := 0.0
	if cfg.Mode == VelocityModeAccelerating && cfg.AccelTime > 0 {
		accelDBPerS2 = cfg.VelMaxDBPerS / cfg.AccelTime
	}

	return &velocityState{
		cfg:          cfg,
		accelDBPerS2: accelDBPerS2,
		lastHeldAt:   time.Now(),
	}
}

// setHeld sets the direction the volume button is being held.
//
// This is intended to be called only by the daemon goroutine (single-owner).
func (v *velocityState) setHeld(direction int) {
	now := time.Now()

	// Track the start of a "hold gesture" for constant+turbo mode.
	// We define a new gesture as:
	// - transitioning from not-held to held, or
	// - changing direction while held
	if v.heldDirection == 0 || (direction != 0 && direction != v.heldDirection) {
		v.holdBeganAt = now
	}

	v.heldDirection = direction
	v.lastHeldAt = now
}

// release releases all volume buttons.
//
// This is intended to be called only by the daemon goroutine (single-owner).
func (v *velocityState) release() {
	v.heldDirection = 0
	// Reset hold gesture so the next hold starts fresh.
	v.holdBeganAt = time.Time{}
}

// updateVolume synchronizes the internal state with the server's actual volume.
//
// This is intended to be called only by the daemon goroutine (single-owner).
func (v *velocityState) updateVolume(currentVol float64) {
	v.currentVolume = currentVol
	v.volumeKnown = true
	v.targetDB = currentVol // Sync target with actual
}

// updateWithDt advances the velocity and target based on elapsed time.
//
// This is intended to be called only by the daemon goroutine (single-owner).
func (v *velocityState) updateWithDt(dt float64, now time.Time) {
	// Defensive dt handling:
	// - ignore non-positive dt
	// - clamp very large dt (startup or stalls) to avoid huge jumps while still making progress
	if dt <= 0 {
		return
	}
	if v.cfg.MaxDt > 0 && dt > v.cfg.MaxDt {
		dt = v.cfg.MaxDt
	}

	// Hold-timeout: if we haven't observed a hold event recently, treat as released.
	// This protects against missing release events or integrations that only emit repeats sporadically.
	if v.heldDirection != 0 && v.cfg.HoldTimeout > 0 && !v.lastHeldAt.IsZero() {
		if now.Sub(v.lastHeldAt) > v.cfg.HoldTimeout {
			v.heldDirection = 0
			v.holdBeganAt = time.Time{}
		}
	}

	// Base "rate limit" (velMax) for this tick.
	//
	// Goals:
	// - Ramping UP should slow down near max volume (danger zone).
	// - Ramping DOWN should behave like the rest of the band (no danger-zone slowdown).
	velMax := v.cfg.VelMaxDBPerS

	if v.heldDirection == 1 {
		// For safety, engage the danger-zone based on where we're headed, not only what the server last reported.
		// Use a simple, conservative estimate:
		//   estVol = max(currentVolume, targetDB) when volumeKnown
		//   estVol = targetDB otherwise
		estVol := v.targetDB
		if v.volumeKnown && v.currentVolume > estVol {
			estVol = v.currentVolume
		}

		// Danger zone threshold is defined relative to max volume (MaxDB).
		// Enter danger zone when estVol > (MaxDB - DangerZoneDB).
		dangerThreshold := v.cfg.MaxDB - v.cfg.DangerZoneDB

		// Map estVol from [dangerThreshold..MaxDB] into x in [0..1]
		if estVol > dangerThreshold {
			den := (v.cfg.MaxDB - dangerThreshold)
			x := 1.0
			if den > 0 {
				x = (estVol - dangerThreshold) / den
			}
			if x < 0 {
				x = 0
			}
			if x > 1 {
				x = 1
			}

			// Hard cap immediately upon entering the danger zone, then apply extra caution near max:
			// extraReduction(x) = 1 - x^3  (1 at zone entry, 0 at MaxDB)
			extra := 1.0 - (x * x * x)
			if extra < 0 {
				extra = 0
			}
			if extra > 1 {
				extra = 1
			}

			// velMax transitions from DangerVelMaxDBPerS at zone entry down to
			// DangerVelMinNear0DBPerS near MaxDB.
			velMax = v.cfg.DangerVelMinNear0DBPerS + (v.cfg.DangerVelMaxDBPerS-v.cfg.DangerVelMinNear0DBPerS)*extra
		}
	}

	switch v.cfg.Mode {
	case VelocityModeConstant:
		// Constant-rate hold:
		// - base rate: VelMaxDBPerS
		// - optional turbo:
		//    turbo multiplier: AccelTime (if > 1)
		//    turbo delay: DecayTau (seconds, if > 0)
		rate := 0.0
		if v.heldDirection == 1 {
			rate = v.cfg.VelMaxDBPerS
		} else if v.heldDirection == -1 {
			rate = -v.cfg.VelMaxDBPerS
		} else {
			rate = 0
		}

		// Turbo only applies while held.
		if v.heldDirection != 0 {
			mult := v.cfg.AccelTime
			if mult < 1 {
				mult = 1
			}

			delay := v.cfg.DecayTau
			if delay < 0 {
				delay = 0
			}

			if mult > 1 && delay > 0 && !v.holdBeganAt.IsZero() && now.Sub(v.holdBeganAt) >= time.Duration(delay*float64(time.Second)) {
				rate *= mult
			} else if mult > 1 && delay == 0 {
				// If delay is 0, turbo is immediate.
				rate *= mult
			}
		}

		// Apply the per-tick velMax cap (danger zone applies to UP only by how velMax was computed above).
		if rate > 0 && rate > velMax {
			rate = velMax
		}
		if rate < 0 && -rate > velMax {
			rate = -velMax
		}

		v.targetDB += rate * dt

	default:
		// Accelerating mode (existing behavior).
		// Snappier direction changes: if the held direction reverses, reset velocity so it responds immediately.
		if (v.heldDirection == 1 && v.velocityDBPerS < 0) || (v.heldDirection == -1 && v.velocityDBPerS > 0) {
			v.velocityDBPerS = 0
		}

		// Update velocity based on held state
		if v.heldDirection == 1 { // UP held
			v.velocityDBPerS += v.accelDBPerS2 * dt
			if v.velocityDBPerS > velMax {
				v.velocityDBPerS = velMax
			}
		} else if v.heldDirection == -1 { // DOWN held
			v.velocityDBPerS -= v.accelDBPerS2 * dt
			if v.velocityDBPerS < -velMax {
				v.velocityDBPerS = -velMax
			}
		} else { // Not held - apply exponential decay for tick-rate-independent behavior
			if v.cfg.DecayTau <= 0 {
				v.velocityDBPerS = 0
			} else {
				decay := math.Exp(-dt / v.cfg.DecayTau)
				v.velocityDBPerS *= decay
			}
		}

		// Update target position
		v.targetDB += v.velocityDBPerS * dt
	}

	// Clamp target to limits
	if v.targetDB < v.cfg.MinDB {
		v.targetDB = v.cfg.MinDB
		v.velocityDBPerS = 0
	}
	if v.targetDB > v.cfg.MaxDB {
		v.targetDB = v.cfg.MaxDB
		v.velocityDBPerS = 0
	}
}

// setUpdateHz configures the engine's max dt clamp relative to the daemon update rate.
// This should be called once during initialization.
//
// This is intended to be called only by the daemon goroutine (single-owner).
func (v *velocityState) setUpdateHz(updateHz int) {
	if updateHz <= 0 {
		return
	}
	// Allow up to ~2 ticks worth of time to be integrated in one step.
	v.cfg.MaxDt = 2.0 / float64(updateHz)
}

// getTarget returns the current target volume.
//
// This is intended to be called only by the daemon goroutine (single-owner).
func (v *velocityState) getTarget() float64 {
	return v.targetDB
}

// shouldSendUpdate returns true if we should send an update to CamillaDSP.
//
// This is intended to be called only by the daemon goroutine (single-owner).
func (v *velocityState) shouldSendUpdate() bool {
	if !v.volumeKnown {
		return false
	}

	// Send if target differs from current by more than threshold
	diff := v.targetDB - v.currentVolume
	return diff > volumeUpdateThresholdDB || diff < -volumeUpdateThresholdDB
}
