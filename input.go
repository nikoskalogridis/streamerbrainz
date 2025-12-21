package main

import (
	"bytes"
	"encoding/binary"
	"io"
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

// readInputEvents reads input events from a file descriptor and sends them to a channel
// This runs in a dedicated goroutine and blocks on read operations
func readInputEvents(f *os.File, events chan<- inputEvent, readErr chan<- error) {
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

		events <- ev
	}
}
