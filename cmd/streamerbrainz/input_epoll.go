//go:build linux

package main

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"os"
	"syscall"

	"golang.org/x/sys/unix"
)

// readInputEventsEpoll reads from multiple input devices using epoll
// This is more efficient than spawning a goroutine per device
//
// Instead of:
//   - N goroutines, each blocking on read()
//   - N OS threads potentially
//
// We use:
//   - 1 goroutine with epoll
//   - Kernel wakes us only when events are available
//   - More scalable for many devices
func readInputEventsEpoll(files []*os.File, events chan<- inputEvent, readErr chan<- error) {
	if len(files) == 0 {
		readErr <- fmt.Errorf("no input devices provided")
		return
	}

	// Create epoll instance
	epfd, err := unix.EpollCreate1(0)
	if err != nil {
		readErr <- fmt.Errorf("epoll_create1: %w", err)
		return
	}
	defer unix.Close(epfd)

	// Map file descriptors to files for later identification
	fdToFile := make(map[int]*os.File)

	// Register all input devices with epoll
	for _, f := range files {
		fd := int(f.Fd())
		fdToFile[fd] = f

		// Register this fd for read events
		event := unix.EpollEvent{
			Events: unix.EPOLLIN, // Notify when readable
			Fd:     int32(fd),
		}

		if err := unix.EpollCtl(epfd, unix.EPOLL_CTL_ADD, fd, &event); err != nil {
			readErr <- fmt.Errorf("epoll_ctl_add fd=%d: %w", fd, err)
			return
		}
	}

	// Reusable buffers
	const maxEvents = 32 // Process up to 32 events per epoll_wait call
	epollEvents := make([]unix.EpollEvent, maxEvents)
	evSize := binary.Size(inputEvent{})
	buf := make([]byte, evSize)
	reader := bytes.NewReader(buf)

	// Main epoll loop
	for {
		// Wait for events (blocks until at least one device has data)
		// -1 = wait indefinitely
		n, err := unix.EpollWait(epfd, epollEvents, -1)
		if err != nil {
			// Handle interrupted system call (e.g., SIGINT)
			if err == syscall.EINTR {
				continue
			}
			readErr <- fmt.Errorf("epoll_wait: %w", err)
			return
		}

		// Process all ready file descriptors
		for i := 0; i < n; i++ {
			fd := int(epollEvents[i].Fd)
			f := fdToFile[fd]

			// Check for errors or hangup
			if epollEvents[i].Events&(unix.EPOLLERR|unix.EPOLLHUP) != 0 {
				readErr <- fmt.Errorf("device error/hangup: %s (fd=%d)", f.Name(), fd)
				// Note: We could remove this fd from epoll and continue with others
				// For now, we treat any device error as fatal
				return
			}

			// Read the input event
			if _, err := f.Read(buf); err != nil {
				readErr <- fmt.Errorf("read from %s: %w", f.Name(), err)
				return
			}

			// Parse binary event
			reader.Reset(buf)
			var ev inputEvent
			if err := binary.Read(reader, binary.LittleEndian, &ev); err != nil {
				// Skip malformed events
				continue
			}

			// Send to event channel
			events <- ev
		}
	}
}

// readInputEventsSelect is an alternative implementation using select()
// Works on more platforms than epoll (macOS, BSD, etc.)
func readInputEventsSelect(files []*os.File, events chan<- inputEvent, readErr chan<- error) {
	if len(files) == 0 {
		readErr <- fmt.Errorf("no input devices provided")
		return
	}

	// Build FD set
	var maxFd int
	fds := make(map[int]*os.File)
	for _, f := range files {
		fd := int(f.Fd())
		fds[fd] = f
		if fd > maxFd {
			maxFd = fd
		}
	}

	// Reusable buffers
	evSize := binary.Size(inputEvent{})
	buf := make([]byte, evSize)
	reader := bytes.NewReader(buf)

	// Main select loop
	for {
		// Prepare read FD set
		var readFds unix.FdSet
		for fd := range fds {
			readFds.Set(fd)
		}

		// Wait for any fd to become readable
		// Note: select modifies readFds, so we rebuild it each iteration
		n, err := unix.Select(maxFd+1, &readFds, nil, nil, nil)
		if err != nil {
			if err == syscall.EINTR {
				continue
			}
			readErr <- fmt.Errorf("select: %w", err)
			return
		}

		if n == 0 {
			continue // No fds ready (shouldn't happen with nil timeout)
		}

		// Check which fds are ready
		for fd, f := range fds {
			if !readFds.IsSet(fd) {
				continue
			}

			// Read the input event
			if _, err := f.Read(buf); err != nil {
				readErr <- fmt.Errorf("read from %s: %w", f.Name(), err)
				return
			}

			// Parse binary event
			reader.Reset(buf)
			var ev inputEvent
			if err := binary.Read(reader, binary.LittleEndian, &ev); err != nil {
				// Skip malformed events
				continue
			}

			// Send to event channel
			events <- ev
		}
	}
}

// Notes on using these implementations:
//
// To use epoll version (Linux-only, more efficient):
//   go readInputEventsEpoll(deviceFiles, events, readErr)
//
// To use select version (portable, works everywhere):
//   go readInputEventsSelect(deviceFiles, events, readErr)
//
// Advantages of epoll/select approach:
//   - Single goroutine instead of N goroutines
//   - Kernel efficiently wakes us only when data is ready
//   - Lower memory overhead
//   - Better scalability (10+ devices)
//
// Advantages of current multi-goroutine approach:
//   - Simpler code
//   - Easier to debug (each device has its own goroutine)
//   - Device errors are isolated
//   - Works fine for small number of devices (2-5)
