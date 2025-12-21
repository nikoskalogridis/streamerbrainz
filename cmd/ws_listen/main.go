package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"math"
	"net/url"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/gorilla/websocket"
)

func main() {
	var (
		wsURL    = flag.String("ws", "ws://127.0.0.1:1234", "CamillaDSP websocket URL")
		poll     = flag.Bool("poll", false, "Enable polling mode - send GetFaders commands periodically")
		interval = flag.Int("interval", 500, "Polling interval in milliseconds (only used with -poll)")
		command  = flag.String("cmd", "", "Send a single command and exit (e.g., 'GetVersion' or 'GetVolume')")
		debug    = flag.Bool("debug", false, "Enable debug output - show raw JSON responses")
	)
	flag.Parse()

	// Parse websocket URL
	u, err := url.Parse(*wsURL)
	if err != nil {
		log.Fatalf("invalid websocket URL: %v", err)
	}

	// Handle shutdown
	sigc := make(chan os.Signal, 1)
	signal.Notify(sigc, syscall.SIGINT, syscall.SIGTERM)

	// Connect to websocket
	d := websocket.Dialer{
		HandshakeTimeout: 5 * time.Second,
	}

	log.Printf("connecting to %s...", u.String())
	conn, _, err := d.Dial(u.String(), nil)
	if err != nil {
		log.Fatalf("failed to connect: %v", err)
	}
	defer conn.Close()

	log.Printf("connected! (press Ctrl+C to exit)...\n")
	if *poll {
		log.Printf("Polling mode: will send GetFaders every %dms\n", *interval)
	} else {
		log.Printf("Passive mode: only listening (use -poll to enable polling)\n")
	}
	log.Printf("Use -cmd to send a single command and exit.\n\n")

	// Mutex to protect concurrent writes to websocket
	var writeMu sync.Mutex

	// Set up ping/pong handlers for connection health
	conn.SetReadDeadline(time.Now().Add(60 * time.Second))
	conn.SetPongHandler(func(string) error {
		conn.SetReadDeadline(time.Now().Add(60 * time.Second))
		return nil
	})

	// Start ping ticker to keep connection alive
	pingTicker := time.NewTicker(30 * time.Second)
	defer pingTicker.Stop()

	go func() {
		for {
			select {
			case <-pingTicker.C:
				writeMu.Lock()
				err := conn.WriteMessage(websocket.PingMessage, nil)
				writeMu.Unlock()
				if err != nil {
					log.Printf("ping failed: %v", err)
					return
				}
			}
		}
	}()

	// Handle single command mode
	if *command != "" {
		sendCommand(conn, &writeMu, *command, true) // verbose=true for single commands
		// Read response
		messageType, message, err := conn.ReadMessage()
		if err != nil {
			log.Fatalf("failed to read response: %v", err)
		}
		if messageType == websocket.TextMessage {
			fmt.Printf("Response: %s\n", string(message))
		}
		return
	}

	// Track last volume and mute state for change detection
	var (
		lastVolumeMu         sync.Mutex
		lastVolumeHundredths *int  // volume in hundredths of dB, nil means no previous volume
		lastMute             *bool // nil means no previous mute state
	)

	// Start polling if enabled
	if *poll {
		pollTicker := time.NewTicker(time.Duration(*interval) * time.Millisecond)
		defer pollTicker.Stop()

		go func() {
			for range pollTicker.C {
				sendCommand(conn, &writeMu, "GetFaders", false) // verbose=false for polling
			}
		}()
	}

	// Message reading loop
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			messageType, message, err := conn.ReadMessage()
			if err != nil {
				if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
					log.Printf("websocket error: %v", err)
				}
				return
			}

			switch messageType {
			case websocket.TextMessage:
				// Debug: log raw message if enabled
				if *debug {
					fmt.Printf("[RAW MESSAGE] %s\n", string(message))
				}

				// Try to parse and format JSON response
				var jsonData map[string]interface{}
				if err := json.Unmarshal(message, &jsonData); err != nil {
					if *debug {
						fmt.Printf("[DEBUG] JSON parse error: %v\n", err)
					}
					fmt.Printf("[TEXT] %s\n", string(message))
					continue
				}

				// Debug: log parsed JSON structure if enabled
				if *debug {
					prettyJSON, _ := json.MarshalIndent(jsonData, "", "  ")
					fmt.Printf("[DEBUG] Parsed JSON:\n%s\n", string(prettyJSON))
				}

				// Check if it's a GetFaders response (combined volume and mute for all faders)
				if fadersResp, ok := jsonData["GetFaders"].(map[string]interface{}); ok {
					if *debug {
						fmt.Printf("[DEBUG] Found GetFaders response\n")
					}
					if result, _ := fadersResp["result"].(string); result == "Ok" {
						if *debug {
							fmt.Printf("[DEBUG] GetFaders result: Ok\n")
						}
						if fadersArray, ok := fadersResp["value"].([]interface{}); ok && len(fadersArray) > 0 {
							if *debug {
								fmt.Printf("[DEBUG] Found %d faders in response\n", len(fadersArray))
							}
							// Main fader is index 0
							if mainFader, ok := fadersArray[0].(map[string]interface{}); ok {
								if *debug {
									fmt.Printf("[DEBUG] Main fader data: %v\n", mainFader)
								}
								// Extract volume and mute from main fader
								volVal, volOk := mainFader["volume"].(float64)
								muteVal, muteOk := mainFader["mute"].(bool)

								// Convert volume to integer hundredths of dB to completely avoid
								// floating-point comparison issues (e.g., -25.50 dB = -2550)
								var volHundredths int
								if volOk {
									volHundredths = int(math.Round(volVal * 100))
								}

								if *debug {
									if volOk {
										fmt.Printf("[DEBUG] Extracted volume: %.2f dB (as int: %d hundredths)\n", volVal, volHundredths)
									} else {
										fmt.Printf("[DEBUG] Warning: volume not found or wrong type in main fader\n")
									}
									if muteOk {
										fmt.Printf("[DEBUG] Extracted mute: %v\n", muteVal)
									} else {
										fmt.Printf("[DEBUG] Warning: mute not found or wrong type in main fader\n")
									}
								}

								// Only process if we successfully extracted both values
								if volOk && muteOk {
									// Check for changes and log
									lastVolumeMu.Lock()
									// Compare volumes as integers to avoid floating-point precision issues
									volChanged := lastVolumeHundredths == nil || *lastVolumeHundredths != volHundredths
									muteChanged := lastMute == nil || *lastMute != muteVal

									if *debug {
										fmt.Printf("[DEBUG] Volume changed: %v, Mute changed: %v\n", volChanged, muteChanged)
									}

									// Update values BEFORE printing to prevent race conditions
									// in rapid polling scenarios
									if volChanged {
										if lastVolumeHundredths == nil {
											v := volHundredths
											lastVolumeHundredths = &v
										} else {
											*lastVolumeHundredths = volHundredths
										}
									}

									if muteChanged {
										if lastMute == nil {
											m := muteVal
											lastMute = &m
										} else {
											*lastMute = muteVal
										}
									}

									lastVolumeMu.Unlock()

									// Print AFTER updating and unlocking to avoid holding lock during I/O
									if volChanged {
										volForPrint := float64(volHundredths) / 100.0
										fmt.Printf("[VOLUME] %.2f dB\n", volForPrint)
									}

									if muteChanged {
										muteStatus := "MUTED"
										if !muteVal {
											muteStatus = "UNMUTED"
										}
										fmt.Printf("[MUTE] %s\n", muteStatus)
									}
								} else {
									// If extraction failed, show full response for debugging
									if *debug {
										fmt.Printf("[DEBUG] Failed to extract volume or mute, showing full response\n")
									}
									prettyJSON, _ := json.MarshalIndent(jsonData, "", "  ")
									fmt.Printf("[RESPONSE]\n%s\n\n", string(prettyJSON))
								}
							} else {
								if *debug {
									fmt.Printf("[DEBUG] Error: main fader (index 0) is not a map\n")
								}
								prettyJSON, _ := json.MarshalIndent(jsonData, "", "  ")
								fmt.Printf("[RESPONSE]\n%s\n\n", string(prettyJSON))
							}
						} else {
							if *debug {
								fmt.Printf("[DEBUG] Error: value is not an array or array is empty\n")
							}
							prettyJSON, _ := json.MarshalIndent(jsonData, "", "  ")
							fmt.Printf("[RESPONSE]\n%s\n\n", string(prettyJSON))
						}
					} else {
						if *debug {
							fmt.Printf("[DEBUG] GetFaders result: %v (not Ok)\n", result)
						}
						prettyJSON, _ := json.MarshalIndent(jsonData, "", "  ")
						fmt.Printf("[RESPONSE]\n%s\n\n", string(prettyJSON))
					}
				} else {
					if *debug {
						fmt.Printf("[DEBUG] Response is not GetFaders, showing full response\n")
					}
					// Pretty print other responses (for single command mode, etc.)
					prettyJSON, _ := json.MarshalIndent(jsonData, "", "  ")
					fmt.Printf("[RESPONSE]\n%s\n\n", string(prettyJSON))
				}
			case websocket.BinaryMessage:
				fmt.Printf("[BINARY] %d bytes: %x\n", len(message), message)
			case websocket.PingMessage:
				fmt.Printf("[PING]\n")
			case websocket.PongMessage:
				fmt.Printf("[PONG]\n")
			case websocket.CloseMessage:
				fmt.Printf("[CLOSE]\n")
				return
			default:
				fmt.Printf("[UNKNOWN] type=%d, %d bytes\n", messageType, len(message))
			}
		}
	}()

	// Wait for shutdown signal or connection close
	select {
	case <-sigc:
		log.Printf("\nshutting down...")
		// Clean close
		writeMu.Lock()
		err := conn.WriteMessage(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""))
		writeMu.Unlock()
		if err != nil {
			log.Printf("error closing connection: %v", err)
		}
	case <-done:
		log.Printf("connection closed")
	}
}

// sendCommand sends a command to the websocket server (thread-safe)
// verbose controls whether to log the "[SENT]" message
func sendCommand(conn *websocket.Conn, writeMu *sync.Mutex, cmd string, verbose bool) {
	var payload []byte
	var err error

	// Commands without arguments are sent as JSON strings
	payload, err = json.Marshal(cmd)
	if err != nil {
		log.Printf("error marshaling command: %v", err)
		return
	}

	writeMu.Lock()
	err = conn.WriteMessage(websocket.TextMessage, payload)
	writeMu.Unlock()

	if err != nil {
		log.Printf("error sending command: %v", err)
		return
	}

	if verbose {
		fmt.Printf("[SENT] %s\n", cmd)
	}
}
