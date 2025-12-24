package main

import (
	"testing"
	"time"
)

// TestRotaryState_AddStep_Basic tests basic step tracking
func TestRotaryState_AddStep_Basic(t *testing.T) {
	r := newRotaryState()

	// First step
	count := r.addStep(1, 200)
	if count != 1 {
		t.Errorf("expected count=1, got %d", count)
	}

	// Second step in same direction
	count = r.addStep(1, 200)
	if count != 2 {
		t.Errorf("expected count=2, got %d", count)
	}

	// Third step in same direction
	count = r.addStep(1, 200)
	if count != 3 {
		t.Errorf("expected count=3, got %d", count)
	}
}

// TestRotaryState_AddStep_DirectionChange tests that direction changes
// don't count toward the velocity threshold
func TestRotaryState_AddStep_DirectionChange(t *testing.T) {
	r := newRotaryState()

	// Three steps up
	r.addStep(1, 200)
	r.addStep(1, 200)
	count := r.addStep(1, 200)
	if count != 3 {
		t.Errorf("expected 3 up steps, got %d", count)
	}

	// Change direction - should reset count for this direction
	count = r.addStep(-1, 200)
	if count != 1 {
		t.Errorf("expected count=1 for new direction, got %d", count)
	}

	// More steps down
	count = r.addStep(-1, 200)
	if count != 2 {
		t.Errorf("expected count=2 for down direction, got %d", count)
	}

	// Back up again
	count = r.addStep(1, 200)
	if count != 4 {
		t.Errorf("expected count=4 (3 old + 1 new up steps still in window), got %d", count)
	}
}

// TestRotaryState_AddStep_WindowExpiry tests that old steps are pruned
func TestRotaryState_AddStep_WindowExpiry(t *testing.T) {
	r := newRotaryState()

	// Add some steps
	r.addStep(1, 100) // 100ms window
	r.addStep(1, 100)
	count := r.addStep(1, 100)
	if count != 3 {
		t.Errorf("expected count=3, got %d", count)
	}

	// Wait for window to expire
	time.Sleep(150 * time.Millisecond)

	// New step should only count itself
	count = r.addStep(1, 100)
	if count != 1 {
		t.Errorf("expected count=1 after window expiry, got %d", count)
	}
}

// TestRotaryState_AddStep_PartialExpiry tests that some steps expire while others remain
func TestRotaryState_AddStep_PartialExpiry(t *testing.T) {
	r := newRotaryState()

	windowMS := 100

	// Add first step
	r.addStep(1, windowMS)

	// Wait half the window
	time.Sleep(60 * time.Millisecond)

	// Add two more steps
	r.addStep(1, windowMS)
	r.addStep(1, windowMS)

	// Wait another 60ms (total 120ms from first step, 60ms from last two)
	time.Sleep(60 * time.Millisecond)

	// First step should be expired, last two should remain
	count := r.addStep(1, windowMS)
	if count != 3 {
		t.Errorf("expected count=3 (2 recent + 1 new), got %d", count)
	}
}

// TestRotaryState_AddStep_ZeroWindow tests behavior with zero window
func TestRotaryState_AddStep_ZeroWindow(t *testing.T) {
	r := newRotaryState()

	// With zero window, only current step should count
	count := r.addStep(1, 0)
	if count != 1 {
		t.Errorf("expected count=1 with zero window, got %d", count)
	}

	count = r.addStep(1, 0)
	if count != 1 {
		t.Errorf("expected count=1 with zero window (previous expired), got %d", count)
	}
}

// TestRotaryState_AddStep_MixedDirections tests counting with mixed directions
func TestRotaryState_AddStep_MixedDirections(t *testing.T) {
	r := newRotaryState()

	windowMS := 200

	// Alternating directions
	r.addStep(1, windowMS)          // up
	r.addStep(-1, windowMS)         // down
	r.addStep(1, windowMS)          // up
	r.addStep(-1, windowMS)         // down
	count := r.addStep(1, windowMS) // up

	// Should count 3 up steps (indices 0, 2, 4)
	if count != 3 {
		t.Errorf("expected count=3 (3 up steps), got %d", count)
	}
}

// TestRotaryState_AddStep_LargeWindow tests with large window (no expiry)
func TestRotaryState_AddStep_LargeWindow(t *testing.T) {
	r := newRotaryState()

	// Use very large window so nothing expires
	windowMS := 10000 // 10 seconds

	for i := 1; i <= 10; i++ {
		count := r.addStep(1, windowMS)
		if count != i {
			t.Errorf("step %d: expected count=%d, got %d", i, i, count)
		}
	}
}

// TestRotaryState_Concurrent tests thread safety
func TestRotaryState_Concurrent(t *testing.T) {
	r := newRotaryState()

	done := make(chan bool)

	// Spawn multiple goroutines adding steps concurrently
	for i := 0; i < 10; i++ {
		go func(dir int) {
			for j := 0; j < 100; j++ {
				r.addStep(dir, 1000)
			}
			done <- true
		}(i%2*2 - 1) // alternate between 1 and -1
	}

	// Wait for all goroutines
	for i := 0; i < 10; i++ {
		<-done
	}

	// Should have tracked all steps without panicking
	// (exact count depends on timing, just verify no crash)
	count := r.addStep(1, 1000)
	if count < 1 {
		t.Errorf("expected at least 1 step, got %d", count)
	}
}

// TestRotaryState_NegativeDirection tests negative direction values
func TestRotaryState_NegativeDirection(t *testing.T) {
	r := newRotaryState()

	// Add steps in negative direction
	count := r.addStep(-1, 200)
	if count != 1 {
		t.Errorf("expected count=1 for negative direction, got %d", count)
	}

	count = r.addStep(-1, 200)
	if count != 2 {
		t.Errorf("expected count=2 for negative direction, got %d", count)
	}

	// Switch to positive
	count = r.addStep(1, 200)
	if count != 1 {
		t.Errorf("expected count=1 for new positive direction, got %d", count)
	}
}
