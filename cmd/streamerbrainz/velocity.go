package main

import (
	"sync"
	"time"
)

// velocityState manages smooth velocity-based volume control
type velocityState struct {
	mu             sync.Mutex
	targetDB       float64   // Target volume in dB
	velocityDBPerS float64   // Current velocity in dB/s (signed)
	heldDirection  int       // -1 for down, 0 for none, 1 for up
	lastUpdate     time.Time // Last update timestamp
	currentVolume  float64   // Last known actual volume from server
	volumeKnown    bool      // Whether we have a valid volume reading

	// Configuration
	velMaxDBPerS float64 // Maximum velocity
	accelDBPerS2 float64 // Acceleration in dB/s²
	decayTau     float64 // Decay time constant in seconds
	minDB        float64 // Minimum volume limit
	maxDB        float64 // Maximum volume limit
}

// newVelocityState creates a new velocity state with the given parameters
func newVelocityState(velMax, accelTime, decayTau, minDB, maxDB float64) *velocityState {
	return &velocityState{
		velMaxDBPerS: velMax,
		accelDBPerS2: velMax / accelTime, // Reach velMax in accelTime seconds
		decayTau:     decayTau,
		minDB:        minDB,
		maxDB:        maxDB,
		lastUpdate:   time.Now(),
	}
}

// setHeld sets the direction the volume button is being held
func (v *velocityState) setHeld(direction int) {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.heldDirection = direction
}

// release releases all volume buttons
func (v *velocityState) release() {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.heldDirection = 0
}

// updateVolume synchronizes the internal state with the server's actual volume
func (v *velocityState) updateVolume(currentVol float64) {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.currentVolume = currentVol
	v.volumeKnown = true
	v.targetDB = currentVol // Sync target with actual
}

// update advances the velocity and target based on elapsed time
func (v *velocityState) update() {
	v.mu.Lock()
	defer v.mu.Unlock()

	now := time.Now()
	dt := now.Sub(v.lastUpdate).Seconds()
	v.lastUpdate = now

	if dt <= 0 || dt > 0.5 { // Skip if too long (startup or stall)
		return
	}

	// Determine velocity limits based on safety zone
	velMax := v.velMaxDBPerS
	if v.volumeKnown && v.currentVolume > -safetyZoneDB {
		velMax = safetyVelMaxDBPerS // Slow down near 0dB
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
	} else { // Not held - apply decay
		decayFactor := 1.0 - (dt / v.decayTau)
		if decayFactor < 0 {
			decayFactor = 0
		}
		v.velocityDBPerS *= decayFactor
	}

	// Update target position
	v.targetDB += v.velocityDBPerS * dt

	// Clamp target to limits
	if v.targetDB < v.minDB {
		v.targetDB = v.minDB
		v.velocityDBPerS = 0
	}
	if v.targetDB > v.maxDB {
		v.targetDB = v.maxDB
		v.velocityDBPerS = 0
	}
}

// getTarget returns the current target volume
func (v *velocityState) getTarget() float64 {
	v.mu.Lock()
	defer v.mu.Unlock()
	return v.targetDB
}

// shouldSendUpdate returns true if we should send an update to CamillaDSP
func (v *velocityState) shouldSendUpdate() bool {
	v.mu.Lock()
	defer v.mu.Unlock()

	if !v.volumeKnown {
		return false
	}

	// Send if target differs from current by more than threshold
	diff := v.targetDB - v.currentVolume
	return diff > volumeUpdateThresholdDB || diff < -volumeUpdateThresholdDB
}
