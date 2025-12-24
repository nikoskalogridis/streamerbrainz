# Alternative Input Handling Approaches

StreamerBrainz currently supports reading from multiple input devices using either:
1. **Multiple goroutines** (one per device) - Current default implementation
2. **epoll/select** - Single goroutine, more efficient for many devices

However, there are other approaches available in Linux that might be worth considering:

## 1. Multiple Goroutines (Current Implementation)

**How it works:**
- Opens each device (`/dev/input/eventX`) as a separate file
- Spawns one goroutine per device
- Each goroutine blocks on `read()` and sends events to a shared channel

**Pros:**
- Simple, easy to understand
- Device errors are isolated (one device failing doesn't affect others)
- Works well for 2-5 devices
- Straightforward debugging

**Cons:**
- N goroutines = N OS threads potentially
- More memory overhead for many devices
- Each goroutine blocks on a syscall

**Code example:**
```go
for _, file := range deviceFiles {
    go func(f *os.File) {
        readInputEvents(f, events, readErr)
    }(file)
}
```

---

## 2. epoll (Linux-Specific, Efficient)

**How it works:**
- Uses Linux `epoll` system call to monitor multiple file descriptors
- Single goroutine waits for any device to have data ready
- Kernel wakes the goroutine only when events are available

**Pros:**
- Single goroutine for all devices
- Very efficient (kernel does the work)
- Scales well to 10+ devices
- Lower memory overhead

**Cons:**
- Linux-only (doesn't work on macOS/BSD)
- Slightly more complex code
- All devices share error handling

**Code example:**
```go
epfd, _ := unix.EpollCreate1(0)
for _, f := range files {
    unix.EpollCtl(epfd, unix.EPOLL_CTL_ADD, fd, &event)
}
for {
    n, _ := unix.EpollWait(epfd, events, -1)
    // Process ready file descriptors
}
```

**Implementation:** See `input_epoll.go`

---

## 3. select/poll (Portable Alternative)

**How it works:**
- Uses POSIX `select()` or `poll()` to monitor multiple fds
- Similar to epoll but works on all Unix-like systems
- Single goroutine, kernel notifies when devices are ready

**Pros:**
- Works on Linux, macOS, BSD, etc.
- Single goroutine
- Good scalability (better than multiple goroutines)

**Cons:**
- `select()` has FD_SETSIZE limit (usually 1024)
- Less efficient than epoll on Linux
- More complex than goroutine-per-device

**Code example:**
```go
var readFds unix.FdSet
for fd := range fds {
    readFds.Set(fd)
}
unix.Select(maxFd+1, &readFds, nil, nil, nil)
```

**Implementation:** See `input_epoll.go` (select version)

---

## 4. libinput (NOT an Aggregated Device)

**What it is:**
- High-level input device library used by Wayland, systemd-logind
- Provides device discovery, event normalization, gesture recognition
- NOT a single aggregated device file

**How it works:**
- Library (not a kernel feature)
- Manages multiple `/dev/input/eventX` devices internally
- Provides unified event stream through API
- Handles device hotplug automatically

**Pros:**
- Automatic device discovery and hotplug
- Normalizes events across different hardware
- Gesture support, palm rejection, etc.
- Used by production systems (Wayland compositors)

**Cons:**
- C library (would need CGO bindings)
- Much heavier than raw evdev
- Designed for display servers, not simple daemons
- Requires running as separate session or integrating with systemd-logind

**Example (pseudocode):**
```c
li = libinput_udev_create_context(...);
libinput_udev_assign_seat(li, "seat0");
while (1) {
    libinput_dispatch(li);
    while ((event = libinput_get_event(li))) {
        // Process event from any device
    }
}
```

**Note:** This is probably overkill for StreamerBrainz's use case.

---

## 5. No Built-In Aggregated Device in Linux

**The reality:**
There is **NO** `/dev/input/keyboard-all` or similar device that aggregates all keyboards/media keys.

**What DOES exist:**
- `/dev/input/mice` - Aggregates all mice (pointer movements/clicks)
- `/dev/input/mouse0`, `/dev/input/mouse1` - Individual mice

**What DOESN'T exist:**
- ❌ `/dev/input/keyboards` - No such thing
- ❌ `/dev/input/event-all` - No such thing
- ❌ Kernel-level keyboard aggregation

**Why?**
- Different devices have different capabilities
- Security concerns (keyboard input is sensitive)
- Applications usually want specific devices (e.g., ignore virtual keyboards)

---

## 6. uinput (Create Virtual Aggregated Device)

**What it is:**
- Kernel module for creating virtual input devices from userspace
- You could read from multiple devices and write to a virtual device

**How it would work:**
```
[Device 1] ─┐
[Device 2] ─┼─> [Userspace Daemon] ─> [uinput] ─> /dev/input/eventX (virtual)
[Device 3] ─┘                                      ↓
                                            StreamerBrainz reads this
```

**Pros:**
- Creates a real device file
- Other applications can use it too
- Clean separation

**Cons:**
- Complex to implement
- Requires managing device lifecycle
- Introduces extra latency
- Overkill for this use case

**Example (pseudocode):**
```c
fd = open("/dev/uinput", O_WRONLY);
ioctl(fd, UI_SET_EVBIT, EV_KEY);
ioctl(fd, UI_SET_KEYBIT, KEY_VOLUMEUP);
write(fd, &setup, sizeof(setup));
ioctl(fd, UI_DEV_CREATE);
// Now you have /dev/input/eventX that you created
```

---

## 7. inotify + Dynamic Device Discovery

**How it works:**
- Monitor `/dev/input` directory with `inotify`
- Automatically detect when devices are added/removed
- Add new devices to monitoring set on hotplug

**Pros:**
- Handles USB devices being plugged/unplugged
- No need to restart daemon
- Automatic device discovery

**Cons:**
- More complex
- Need to filter which devices to monitor
- Security concerns (monitoring all input devices)

**Code example:**
```go
watcher, _ := fsnotify.NewWatcher()
watcher.Add("/dev/input")
for {
    event := <-watcher.Events
    if event.Op&fsnotify.Create != 0 {
        // New device appeared, maybe add it
    }
}
```

---

## Recommendation for StreamerBrainz

**For most users (2-5 devices):**
- ✅ **Current multi-goroutine approach is fine**
- Simple, reliable, easy to debug
- Performance difference is negligible

**For power users (5+ devices):**
- ✅ **Consider epoll-based approach**
- More efficient
- Better scalability
- See `input_epoll.go` for implementation

**NOT recommended:**
- ❌ libinput - Too heavy, designed for display servers
- ❌ uinput aggregation - Overly complex
- ❌ Searching for non-existent aggregated device

---

## Comparison Table

| Approach           | Goroutines | Scalability | Portability | Complexity | Recommended |
|--------------------|-----------|-------------|-------------|------------|-------------|
| Multiple goroutines| N         | Good (2-5)  | All platforms| Low       | ✅ Default  |
| epoll              | 1         | Excellent   | Linux-only  | Medium     | ✅ Power users |
| select/poll        | 1         | Good        | All Unix    | Medium     | 🟡 Alternative |
| libinput           | 1         | Excellent   | Linux       | High       | ❌ Overkill |
| uinput aggregation | N+1       | Good        | Linux       | Very High  | ❌ Too complex |
| inotify hotplug    | Variable  | Good        | Linux       | High       | 🟡 Future enhancement |

---

## How to Switch to epoll

If you want to use the more efficient epoll-based approach:

1. **Add dependency** to `go.mod`:
   ```bash
   go get golang.org/x/sys/unix
   ```

2. **Replace in main.go:**
   ```go
   // OLD: Multiple goroutines
   for i, f := range deviceFiles {
       deviceName := cfg.IR.InputDevices[i].Path
       go func(file *os.File, name string) {
           readInputEvents(file, events, readErr)
       }(f, deviceName)
   }

   // NEW: Single goroutine with epoll
   go readInputEventsEpoll(deviceFiles, events, readErr)
   ```

3. **Build and test:**
   ```bash
   go build ./cmd/streamerbrainz
   ```

The rest of the code remains unchanged - it still receives events on the same channel.

---

## Conclusion

**Bottom line:** There is no magical aggregated input device in Linux. You must either:
1. Open multiple device files (what StreamerBrainz does)
2. Use a library like libinput that manages multiple devices for you
3. Create your own virtual device with uinput

The current implementation (multiple goroutines) is actually a perfectly fine approach for the typical use case (2-5 input devices). If you need better scalability, the epoll-based approach is recommended.