package main

// Linux input event types and codes (from <linux/input.h>)
const (
	EV_KEY = 0x01
	EV_REL = 0x02

	KEY_MUTE         = 113
	KEY_VOLUMEDOWN   = 114
	KEY_VOLUMEUP     = 115
	KEY_PLAYPAUSE    = 164
	KEY_STOPCD       = 166
	KEY_PREVIOUSSONG = 165
	KEY_NEXTSONG     = 163
	KEY_PLAYCD       = 200
	KEY_PAUSECD      = 201

	// Rotary encoder relative axis codes
	REL_DIAL  = 0x07
	REL_WHEEL = 0x08
	REL_MISC  = 0x09
)

// Input event value constants
const (
	evValueRelease = 0
	evValuePress   = 1
	evValueRepeat  = 2
)

// Velocity-based volume control configuration
const (
	defaultUpdateHz      = 30   // Update loop frequency (Hz)
	defaultVelMaxDBPerS  = 15.0 // Maximum velocity in dB/s
	defaultAccelTime     = 2.0  // Time to reach max velocity (seconds)
	defaultDecayTau      = 0.2  // Decay time constant (seconds)
	defaultReadTimeoutMS = 500  // Default timeout for reading websocket responses (ms)

	// Danger zone (near max volume):
	//
	// The last `dangerZoneDB` dB below max volume is treated as a "danger zone" for ramp-up.
	// Note: the threshold is computed relative to max volume: (maxDB - dangerZoneDB).
	dangerZoneDB = 12.0 // Size of the danger zone below max volume (dB)

	// Ramp-up speed limits inside the danger zone:
	// - dangerVelMaxDBPerS: immediate hard cap when entering the danger zone
	// - dangerVelMinNear0DBPerS: minimum ramp-up velocity near 0 dB (prevents "sticky" behavior)
	dangerVelMaxDBPerS      = 3.0 // Max velocity in danger zone (dB/s)
	dangerVelMinNear0DBPerS = 0.3 // Min velocity near 0 dB within danger zone (dB/s)

	safeDefaultDB = -45.0 // Safe default volume when query fails (dB)

	// Volume update threshold
	volumeUpdateThresholdDB = 0.02 // Minimum volume difference to send update (dB)

	// Rotary encoder configuration defaults
	defaultRotaryDbPerStep          = 0.5 // Default dB change per encoder step
	defaultRotaryVelocityWindowMS   = 200 // Time window for velocity detection (ms)
	defaultRotaryVelocityMultiplier = 2.0 // Multiplier for "fast spinning"
	defaultRotaryVelocityThreshold  = 3   // Steps in window to trigger velocity mode
)
