package main

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net/url"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/gorilla/websocket"
)

// Linux input_event struct (from <linux/input.h>)
// struct input_event { struct timeval time; __u16 type; __u16 code; __s32 value; };
type inputEvent struct {
	Sec   int64
	Usec  int64
	Type  uint16
	Code  uint16
	Value int32
}

// EV_KEY and common key codes (Linux input-event-codes.h)
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

// CamillaDSP websocket protocol wants JSON *text* messages.
func sendJSONText(conn *websocket.Conn, v any) error {
	payload, err := json.Marshal(v)
	if err != nil {
		return err
	}
	return conn.WriteMessage(websocket.TextMessage, payload)
}

type wsClient struct {
	mu    sync.Mutex
	conn  *websocket.Conn
	wsURL string
}

func newWSClient(wsURL string) *wsClient {
	return &wsClient{wsURL: wsURL}
}

func (c *wsClient) connect() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.conn != nil {
		c.conn.Close()
		c.conn = nil
	}

	u, err := url.Parse(c.wsURL)
	if err != nil {
		return fmt.Errorf("invalid ws url: %w", err)
	}

	d := websocket.Dialer{
		HandshakeTimeout: 2 * time.Second,
	}

	conn, _, err := d.Dial(u.String(), nil)
	if err != nil {
		return err
	}

	// Optional: keep socket from stalling silently
	conn.SetWriteDeadline(time.Now().Add(2 * time.Second))
	conn.SetWriteDeadline(time.Time{})

	c.conn = conn
	return nil
}

func (c *wsClient) send(v any) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.conn == nil {
		return fmt.Errorf("no websocket connection")
	}
	return sendJSONText(c.conn, v)
}

func (c *wsClient) sendAndRead(v any, timeout time.Duration) ([]byte, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.conn == nil {
		return nil, fmt.Errorf("no websocket connection")
	}

	if err := sendJSONText(c.conn, v); err != nil {
		return nil, err
	}

	// Set read deadline
	c.conn.SetReadDeadline(time.Now().Add(timeout))
	defer c.conn.SetReadDeadline(time.Time{})

	_, message, err := c.conn.ReadMessage()
	if err != nil {
		return nil, err
	}

	return message, nil
}

func (c *wsClient) close() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.conn != nil {
		c.conn.Close()
		c.conn = nil
	}
}

func connectWithRetry(ws *wsClient, wsURL string, verbose bool) {
	for {
		err := ws.connect()
		if err == nil {
			if verbose {
				log.Printf("connected websocket: %s", wsURL)
			}
			return
		}
		log.Printf("ws connect failed: %v; retrying...", err)
		time.Sleep(500 * time.Millisecond)
	}
}

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

func (v *velocityState) setHeld(direction int) {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.heldDirection = direction
}

func (v *velocityState) release() {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.heldDirection = 0
}

func (v *velocityState) updateVolume(currentVol float64) {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.currentVolume = currentVol
	v.volumeKnown = true
	v.targetDB = currentVol // Sync target with actual
}

// update advances the velocity and target based on elapsed time
func (v *velocityState) update(verbose bool) {
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

	if verbose && (v.heldDirection != 0 || v.velocityDBPerS != 0) {
		log.Printf("[VEL] held=%d vel=%.2f dB/s target=%.2f dB (current=%.2f dB)",
			v.heldDirection, v.velocityDBPerS, v.targetDB, v.currentVolume)
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

	// Send if target differs from current by more than 0.1dB
	diff := v.targetDB - v.currentVolume
	return diff > 0.1 || diff < -0.1
}

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

func setVolumeCommand(ws *wsClient, targetDB float64, readTimeout int, verbose bool) (float64, error) {
	cmd := map[string]any{"SetVolume": targetDB}

	// Send command and read response
	response, err := ws.sendAndRead(cmd, time.Duration(readTimeout)*time.Millisecond)
	if err != nil {
		return 0, err
	}

	// Parse response - SetVolume returns just the result, we need to query current volume
	// For now, assume success and return the target as current
	var setResp struct {
		SetVolume struct {
			Result string `json:"result"`
		} `json:"SetVolume"`
	}

	if err := json.Unmarshal(response, &setResp); err != nil {
		if verbose {
			log.Printf("[ERROR] failed to parse SetVolume response: %v", err)
		}
		return targetDB, nil // Assume success
	}

	if verbose {
		log.Printf("[VOLUME] Set target=%.2f dB, result=%s", targetDB, setResp.SetVolume.Result)
	}

	return targetDB, nil
}

func getCurrentVolume(ws *wsClient, readTimeout int, verbose bool) (float64, error) {
	cmd := "GetVolume"

	response, err := ws.sendAndRead(cmd, time.Duration(readTimeout)*time.Millisecond)
	if err != nil {
		return 0, err
	}

	var volResp struct {
		GetVolume struct {
			Result string  `json:"result"`
			Value  float64 `json:"value"`
		} `json:"GetVolume"`
	}

	if err := json.Unmarshal(response, &volResp); err != nil {
		if verbose {
			log.Printf("[ERROR] failed to parse GetVolume response: %v", err)
		}
		return 0, err
	}

	if verbose {
		log.Printf("[VOLUME] Current volume: %.2f dB", volResp.GetVolume.Value)
	}

	return volResp.GetVolume.Value, nil
}

func handleMuteCommand(ws *wsClient, verbose bool) error {
	if verbose {
		log.Printf("KEY_MUTE -> ToggleMute")
	}
	return ws.send("ToggleMute")
}

// sendWithRetry executes a command function and reconnects on error
func sendWithRetry(ws *wsClient, wsURL string, verbose bool, cmd func() error) {
	if err := cmd(); err != nil {
		log.Printf("ws send failed: %v; reconnecting...", err)
		connectWithRetry(ws, wsURL, verbose)
	}
}

func main() {
	var (
		inputDev    = flag.String("input", "/dev/input/event6", "Linux input event device for IR (e.g. /dev/input/event6)")
		wsURL       = flag.String("ws", "ws://127.0.0.1:1234", "CamillaDSP websocket URL (CamillaDSP must be started with -pPORT)")
		minDB       = flag.Float64("min", -65.0, "Minimum volume clamp in dB")
		maxDB       = flag.Float64("max", 0.0, "Maximum volume clamp in dB")
		updateHz    = flag.Int("update-hz", defaultUpdateHz, "Update loop frequency in Hz")
		velMax      = flag.Float64("vel-max", defaultVelMaxDBPerS, "Maximum velocity in dB/s")
		accelTime   = flag.Float64("accel-time", defaultAccelTime, "Time to reach max velocity in seconds")
		decayTau    = flag.Float64("decay-tau", defaultDecayTau, "Velocity decay time constant in seconds")
		readTimeout = flag.Int("read-timeout-ms", defaultReadTimeoutMS, "Timeout in milliseconds for reading websocket responses")
		verbose     = flag.Bool("v", false, "Verbose logging")
	)
	flag.Parse()

	if *minDB > *maxDB {
		log.Fatalf("-min must be <= -max")
	}
	if *updateHz <= 0 || *updateHz > 1000 {
		log.Fatalf("-update-hz must be between 1 and 1000")
	}

	// Open input device
	f, err := os.Open(*inputDev)
	if err != nil {
		log.Fatalf("open input device %s: %v (tip: run as root or add user to 'input' group)", *inputDev, err)
	}
	defer f.Close()

	// Prepare websocket client
	ws := newWSClient(*wsURL)
	connectWithRetry(ws, *wsURL, *verbose)

	// Get initial volume from server
	initialVol, err := getCurrentVolume(ws, *readTimeout, *verbose)
	if err != nil {
		log.Printf("Warning: could not get initial volume: %v", err)
		log.Printf("Setting server volume to safe default: %.1f dB", safeDefaultDB)

		// Actively set the server to a safe default volume
		_, setErr := setVolumeCommand(ws, safeDefaultDB, *readTimeout, *verbose)
		if setErr != nil {
			log.Printf("Error: failed to set safe default volume: %v", setErr)
			log.Printf("Warning: cannot verify server volume - proceeding with caution")
		}
		initialVol = safeDefaultDB
	}

	// Initialize velocity state with known volume
	velState := newVelocityState(*velMax, *accelTime, *decayTau, *minDB, *maxDB)
	velState.updateVolume(initialVol)

	// Handle shutdown
	sigc := make(chan os.Signal, 1)
	signal.Notify(sigc, syscall.SIGINT, syscall.SIGTERM)

	// Read loop for input events
	events := make(chan inputEvent, 64)
	readErr := make(chan error, 1)
	go readInputEvents(f, events, readErr)

	// Update loop ticker
	updateInterval := time.Second / time.Duration(*updateHz)
	updateTicker := time.NewTicker(updateInterval)
	defer updateTicker.Stop()

	log.Printf("listening on %s, sending to %s (update rate: %d Hz)", *inputDev, *wsURL, *updateHz)

	for {
		select {
		case <-sigc:
			log.Printf("shutting down")
			ws.close()
			f.Close()
			return

		case err := <-readErr:
			log.Printf("input reader stopped: %v", err)
			ws.close()
			f.Close()
			return

		case <-updateTicker.C:
			// Update velocity and target
			velState.update(*verbose)

			// Send update to CamillaDSP if needed
			if velState.shouldSendUpdate() {
				targetDB := velState.getTarget()
				sendWithRetry(ws, *wsURL, *verbose, func() error {
					currentVol, err := setVolumeCommand(ws, targetDB, *readTimeout, *verbose)
					if err == nil {
						velState.updateVolume(currentVol)
					}
					return err
				})
			}

		case ev := <-events:
			// Filter non-key events
			if ev.Type != EV_KEY {
				continue
			}

			// Handle key codes
			switch ev.Code {
			case KEY_VOLUMEUP:
				if ev.Value == evValuePress || ev.Value == evValueRepeat {
					velState.setHeld(1)
				} else if ev.Value == evValueRelease {
					velState.release()
				}

			case KEY_VOLUMEDOWN:
				if ev.Value == evValuePress || ev.Value == evValueRepeat {
					velState.setHeld(-1)
				} else if ev.Value == evValueRelease {
					velState.release()
				}

			case KEY_MUTE:
				if ev.Value == evValuePress {
					sendWithRetry(ws, *wsURL, *verbose, func() error {
						return handleMuteCommand(ws, *verbose)
					})
				}
			}
		}
	}
}
