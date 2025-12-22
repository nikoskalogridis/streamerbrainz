package main

// Linux input event types and codes (from <linux/input.h>)
const (
	EV_KEY = 0x01

	KEY_MUTE       = 113
	KEY_VOLUMEDOWN = 114
	KEY_VOLUMEUP   = 115
)

// Input event value constants
const (
	evValueRelease = 0
	evValuePress   = 1
	evValueRepeat  = 2
)

// Velocity-based volume control configuration
const (
	defaultUpdateHz      = 30    // Update loop frequency (Hz)
	defaultVelMaxDBPerS  = 15.0  // Maximum velocity in dB/s
	defaultAccelTime     = 2.0   // Time to reach max velocity (seconds)
	defaultDecayTau      = 0.2   // Decay time constant (seconds)
	defaultReadTimeoutMS = 500   // Default timeout for reading websocket responses (ms)
	safetyZoneDB         = 12.0  // Slow down above -12dB
	safetyVelMaxDBPerS   = 3.0   // Max velocity in safety zone (dB/s)
	safeDefaultDB        = -45.0 // Safe default volume when query fails (dB)
)
