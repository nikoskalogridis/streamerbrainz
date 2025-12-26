package main

import (
	"math"
	"time"
)

// VelocityMode selects how "press-and-hold" behaves.
//
// Accelerating mode (default):
//   - VelMaxDBPerS: maximum velocity (dB/s)
//   - AccelTime: time to reach max velocity (s)
//   - DecayTau: exponential decay time constant when released (s)
//
// Constant mode:
//   - VelMaxDBPerS: base hold rate (dB/s)
//   - AccelTime: turbo multiplier (unitless, >1 enables turbo)
//   - DecayTau: turbo activation delay (s), 0 disables delay
type VelocityMode string

const (
	VelocityModeAccelerating VelocityMode = "accelerating"
	VelocityModeConstant     VelocityMode = "constant"
)

// VelocityConfig contains all tunable parameters for the volume controller.
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
	// HoldTimeout auto-releases if no hold events arrive in this duration. 0 disables timeout.
	HoldTimeout time.Duration
	// MaxDt clamps very large dt steps (seconds). 0 disables clamping.
	MaxDt float64

	// Danger zone (near max volume), ramp-up only
	DangerZoneDB            float64 // Size of danger zone below MaxDB (dB)
	DangerVelMaxDBPerS      float64 // Hard cap for ramp-up velocity inside danger zone (dB/s)
	DangerVelMinNear0DBPerS float64 // Minimum ramp-up velocity near MaxDB (dB/s)
}

// StepVolumeController advances the reducer-owned volume controller state by one tick.
//
// IMPORTANT (Option A):
// - This function is pure w.r.t. ownership: it does not mutate any external state.
// - Observed volume/mute/config are NOT responsibilities of the controller. Those live in DaemonState.Camilla.
// - Whether to send volume updates (thresholding, rate limiting) is a reducer/policy responsibility.
//
// Parameters:
//   - ctrl: current controller state (held direction, velocity, gesture timing, target)
//   - baselineTarget: the target position to integrate from for this tick (typically daemon desired if present,
//     else the last observed CamillaDSP volume, else ctrl.TargetDB)
//   - dt/now: timing inputs for integration
//   - cfg: velocity configuration (bounds, dynamics)
//
// StepVolumeController advances the reducer-owned volume controller state by one tick.
//
// IMPORTANT (Option A):
// - This function is pure w.r.t. ownership: it does not mutate any external state.
// - Observed volume/mute/config are NOT responsibilities of the controller. Those live in DaemonState.Camilla.
// - Whether to send volume updates (thresholding, rate limiting) is a reducer/policy responsibility.
func StepVolumeController(ctrl VolumeControllerState, baselineTarget float64, dt float64, now time.Time, cfg VelocityConfig) VolumeControllerState {
	// Defensive dt handling
	if dt <= 0 {
		return ctrl
	}
	if cfg.MaxDt > 0 && dt > cfg.MaxDt {
		dt = cfg.MaxDt
	}

	// Hold-timeout behavior: if we haven't observed a hold event recently, treat as released.
	// This is a robustness fallback for inputs that emit repeats but may miss releases.
	if ctrl.HeldDirection != 0 && cfg.HoldTimeout > 0 && !ctrl.LastHeldAt.IsZero() {
		if now.Sub(ctrl.LastHeldAt) > cfg.HoldTimeout {
			ctrl.HeldDirection = 0
			ctrl.HoldBeganAt = time.Time{}
		}
	}

	// Integrate from daemon-provided baseline.
	ctrl.TargetDB = baselineTarget

	// Compute per-tick velMax with danger-zone behavior for ramp-up (UP only).
	velMax := cfg.VelMaxDBPerS
	if ctrl.HeldDirection == 1 && cfg.DangerZoneDB > 0 {
		estVol := ctrl.TargetDB
		dangerThreshold := cfg.MaxDB - cfg.DangerZoneDB
		if estVol > dangerThreshold {
			den := cfg.MaxDB - dangerThreshold
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
			extra := 1.0 - (x * x * x)
			if extra < 0 {
				extra = 0
			}
			if extra > 1 {
				extra = 1
			}
			// velMax transitions from DangerVelMaxDBPerS at zone entry down to DangerVelMinNear0DBPerS near MaxDB.
			velMax = cfg.DangerVelMinNear0DBPerS + (cfg.DangerVelMaxDBPerS-cfg.DangerVelMinNear0DBPerS)*extra
		}
	}

	switch cfg.Mode {
	case VelocityModeConstant:
		// Constant-rate hold with optional turbo.
		rate := 0.0
		if ctrl.HeldDirection == 1 {
			rate = cfg.VelMaxDBPerS
		} else if ctrl.HeldDirection == -1 {
			rate = -cfg.VelMaxDBPerS
		}

		// Turbo only applies while held.
		if ctrl.HeldDirection != 0 {
			mult := cfg.AccelTime
			if mult < 1 {
				mult = 1
			}
			delay := cfg.DecayTau
			if delay < 0 {
				delay = 0
			}

			if mult > 1 && delay > 0 && !ctrl.HoldBeganAt.IsZero() && now.Sub(ctrl.HoldBeganAt) >= time.Duration(delay*float64(time.Second)) {
				rate *= mult
			} else if mult > 1 && delay == 0 {
				rate *= mult
			}
		}

		// Apply per-tick velMax cap (danger zone affects UP only by how velMax was computed).
		if rate > 0 && rate > velMax {
			rate = velMax
		}
		if rate < 0 && -rate > velMax {
			rate = -velMax
		}

		ctrl.TargetDB += rate * dt

	default:
		// Accelerating mode.
		// accel = VelMax / AccelTime
		accel := 0.0
		if cfg.AccelTime > 0 {
			accel = cfg.VelMaxDBPerS / cfg.AccelTime
		}

		// Snappier direction changes: if the held direction reverses, reset velocity so it responds immediately.
		if (ctrl.HeldDirection == 1 && ctrl.VelocityDBPerS < 0) || (ctrl.HeldDirection == -1 && ctrl.VelocityDBPerS > 0) {
			ctrl.VelocityDBPerS = 0
		}

		// Update velocity based on held state
		if ctrl.HeldDirection == 1 {
			ctrl.VelocityDBPerS += accel * dt
			if ctrl.VelocityDBPerS > velMax {
				ctrl.VelocityDBPerS = velMax
			}
		} else if ctrl.HeldDirection == -1 {
			ctrl.VelocityDBPerS -= accel * dt
			if ctrl.VelocityDBPerS < -velMax {
				ctrl.VelocityDBPerS = -velMax
			}
		} else {
			// Not held: apply exponential decay for tick-rate-independent behavior.
			if cfg.DecayTau <= 0 {
				ctrl.VelocityDBPerS = 0
			} else {
				decay := math.Exp(-dt / cfg.DecayTau)
				ctrl.VelocityDBPerS *= decay
			}
		}

		// Update target position
		ctrl.TargetDB += ctrl.VelocityDBPerS * dt
	}

	// Clamp target to limits
	if ctrl.TargetDB < cfg.MinDB {
		ctrl.TargetDB = cfg.MinDB
		ctrl.VelocityDBPerS = 0
	}
	if ctrl.TargetDB > cfg.MaxDB {
		ctrl.TargetDB = cfg.MaxDB
		ctrl.VelocityDBPerS = 0
	}

	return ctrl
}
