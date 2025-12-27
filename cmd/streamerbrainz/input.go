package main

import (
	"bytes"
	"encoding/binary"
	"io"
	"log/slog"
	"os"
)

// inputEvent represents a Linux input event structure
// struct input_event { struct timeval time; __u16 type; __u16 code; __s32 value; };
type inputEvent struct {
	Sec   int64
	Usec  int64
	Type  uint16
	Code  uint16
	Value int32
}

// readInputEvents reads Linux input events from a file descriptor and emits event directly.
// This runs in a dedicated goroutine and blocks on read operations.
//
// Design:
// - Keep the input module responsible for translating device events into event.
// - Keep reducer responsible for policy (e.g. RotaryTurn -> velocity-scaled volume changes).
func readInputEvents(f *os.File, event chan<- Event, readErr chan<- error, logger *slog.Logger) {
	evSize := binary.Size(inputEvent{})
	buf := make([]byte, evSize)
	reader := bytes.NewReader(buf) // Reusable reader, reset on each iteration

	for {
		if _, err := io.ReadFull(f, buf); err != nil {
			readErr <- err
			return
		}

		reader.Reset(buf) // Reset reader to reuse it
		var ev inputEvent
		if err := binary.Read(reader, binary.LittleEndian, &ev); err != nil {
			// Skip malformed events
			continue
		}

		emitEventFromInputEvent(ev, event, logger)
	}
}

// emitEventFromInputEvent converts a raw inputEvent into zero or more Events.
// It must not implement policy (velocity scaling etc.); only event->action mapping.
func emitEventFromInputEvent(ev inputEvent, events chan<- Event, logger *slog.Logger) {
	switch ev.Type {
	case EV_KEY:
		switch ev.Code {
		case KEY_VOLUMEUP:
			if ev.Value == evValuePress || ev.Value == evValueRepeat {
				events <- VolumeHeld{Direction: 1}
			} else if ev.Value == evValueRelease {
				events <- VolumeRelease{}
			}

		case KEY_VOLUMEDOWN:
			if ev.Value == evValuePress || ev.Value == evValueRepeat {
				events <- VolumeHeld{Direction: -1}
			} else if ev.Value == evValueRelease {
				events <- VolumeRelease{}
			}

		case KEY_MUTE:
			if ev.Value == evValuePress {
				events <- ToggleMute{}
			}

		// Media transport keys -> event (no-op in reducer/effects for now)
		case KEY_PLAYPAUSE:
			if ev.Value == evValuePress {
				events <- MediaPlayPause{}
			}
		case KEY_NEXTSONG:
			if ev.Value == evValuePress {
				events <- MediaNext{}
			}
		case KEY_PREVIOUSSONG:
			if ev.Value == evValuePress {
				events <- MediaPrevious{}
			}
		case KEY_PLAYCD:
			if ev.Value == evValuePress {
				events <- MediaPlay{}
			}
		case KEY_PAUSECD:
			if ev.Value == evValuePress {
				events <- MediaPause{}
			}
		case KEY_STOPCD:
			if ev.Value == evValuePress {
				events <- MediaStop{}
			}
		}

	case EV_REL:
		// Only handle rotary encoder relative axis codes
		if ev.Code != REL_DIAL && ev.Code != REL_WHEEL && ev.Code != REL_MISC {
			return
		}
		if ev.Value == 0 {
			return
		}

		// Emit raw rotary intent; reducer will apply velocity/step-size policy.
		events <- RotaryTurn{Steps: int(ev.Value)}
	}
}
