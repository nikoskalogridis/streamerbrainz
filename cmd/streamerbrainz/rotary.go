package main

import (
	"sync"
	"time"
)

// rotaryState tracks recent encoder activity for velocity detection.
// This allows us to detect "fast spinning" and scale the step size accordingly.
//
// Thread-safe: multiple input device goroutines may call addStep() concurrently.
type rotaryState struct {
	recentSteps []rotaryStep
	mu          sync.Mutex
}

// rotaryStep records a single encoder detent/step
type rotaryStep struct {
	timestamp time.Time
	direction int // +1 for up, -1 for down
}

// newRotaryState creates a new rotary state tracker
func newRotaryState() *rotaryState {
	return &rotaryState{
		recentSteps: make([]rotaryStep, 0, 16), // Pre-allocate small capacity
	}
}

// addStep records a new encoder step and returns the count of recent steps
// in the same direction within the velocity window.
//
// This count can be used to determine if the user is "fast spinning" the encoder.
//
// Parameters:
//   - direction: +1 for clockwise/up, -1 for counter-clockwise/down
//   - windowMS: time window in milliseconds for velocity detection
//
// Returns:
//   - count of steps in the same direction within the window
func (r *rotaryState) addStep(direction int, windowMS int) int {
	r.mu.Lock()
	defer r.mu.Unlock()

	now := time.Now()
	cutoff := now.Add(-time.Duration(windowMS) * time.Millisecond)

	// Remove old steps outside the velocity window
	filtered := r.recentSteps[:0] // reuse underlying array
	for _, s := range r.recentSteps {
		if s.timestamp.After(cutoff) {
			filtered = append(filtered, s)
		}
	}

	// Add new step
	filtered = append(filtered, rotaryStep{
		timestamp: now,
		direction: direction,
	})
	r.recentSteps = filtered

	// Count steps in same direction within window
	sameDir := 0
	for _, s := range filtered {
		if s.direction == direction {
			sameDir++
		}
	}

	return sameDir
}
