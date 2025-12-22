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
		interval = flag.Int("interval", 500, "Polling interval in milliseconds")
		command  = flag.String("cmd", "", "Send a single command and exit (e.g., 'GetVersion' or 'GetVolume')")
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

	log.Printf("connected! (press Ctrl+C to exit)")
	log.Printf("polling GetFaders every %dms\n", *interval)

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
		for range pingTicker.C {
			writeMu.Lock()
			err := conn.WriteMessage(websocket.PingMessage, nil)
			writeMu.Unlock()
			if err != nil {
				log.Printf("ping failed: %v", err)
				return
			}
		}
	}()

	// Handle single command mode
	if *command != "" {
		sendCommand(conn, &writeMu, *command)
		// Read response
		messageType, message, err := conn.ReadMessage()
		if err != nil {
			log.Fatalf("failed to read response: %v", err)
		}
		if messageType == websocket.TextMessage {
			var jsonData map[string]any
			if err := json.Unmarshal(message, &jsonData); err == nil {
				prettyJSON, _ := json.MarshalIndent(jsonData, "", "  ")
				fmt.Printf("%s\n", string(prettyJSON))
			} else {
				fmt.Printf("%s\n", string(message))
			}
		}
		return
	}

	// Track last volume and mute state for change detection
	var (
		lastVolumeMu sync.Mutex
		lastVolume   *float64 // nil means no previous volume
		lastMute     *bool    // nil means no previous mute state
	)

	// Start polling
	pollTicker := time.NewTicker(time.Duration(*interval) * time.Millisecond)
	defer pollTicker.Stop()

	go func() {
		for range pollTicker.C {
			sendCommand(conn, &writeMu, "GetFaders")
		}
	}()

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
				handleTextMessage(message, &lastVolumeMu, &lastVolume, &lastMute)
			case websocket.BinaryMessage:
				fmt.Printf("[BINARY] %d bytes\n", len(message))
			case websocket.CloseMessage:
				fmt.Printf("[CLOSE]\n")
				return
			}
		}
	}()

	// Wait for shutdown signal or connection close
	select {
	case <-sigc:
		log.Printf("shutting down...")
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

// handleTextMessage processes incoming text messages
func handleTextMessage(message []byte, lastVolumeMu *sync.Mutex, lastVolume **float64, lastMute **bool) {
	var jsonData map[string]any
	if err := json.Unmarshal(message, &jsonData); err != nil {
		fmt.Printf("[TEXT] %s\n", string(message))
		return
	}

	// Check if it's a GetFaders response
	if fadersResp, ok := jsonData["GetFaders"].(map[string]any); ok {
		handleGetFadersResponse(fadersResp, lastVolumeMu, lastVolume, lastMute)
		return
	}

	// Pretty print other responses
	prettyJSON, _ := json.MarshalIndent(jsonData, "", "  ")
	fmt.Printf("[RESPONSE]\n%s\n\n", string(prettyJSON))
}

// handleGetFadersResponse processes GetFaders responses and tracks volume/mute changes
func handleGetFadersResponse(fadersResp map[string]any, lastVolumeMu *sync.Mutex, lastVolume **float64, lastMute **bool) {
	result, _ := fadersResp["result"].(string)
	if result != "Ok" {
		prettyJSON, _ := json.MarshalIndent(fadersResp, "", "  ")
		fmt.Printf("[RESPONSE]\n%s\n\n", string(prettyJSON))
		return
	}

	fadersArray, ok := fadersResp["value"].([]any)
	if !ok || len(fadersArray) == 0 {
		return
	}

	// Main fader is index 0
	mainFader, ok := fadersArray[0].(map[string]any)
	if !ok {
		return
	}

	// Extract volume and mute from main fader
	volVal, volOk := mainFader["volume"].(float64)
	muteVal, muteOk := mainFader["mute"].(bool)

	if !volOk || !muteOk {
		return
	}

	// Round volume to 2 decimal places to avoid spurious changes
	volVal = math.Round(volVal*100) / 100

	// Check for changes and log
	lastVolumeMu.Lock()
	volChanged := *lastVolume == nil || math.Abs(**lastVolume-volVal) >= 0.01
	muteChanged := *lastMute == nil || **lastMute != muteVal

	// Update values
	if volChanged {
		if *lastVolume == nil {
			v := volVal
			*lastVolume = &v
		} else {
			**lastVolume = volVal
		}
	}

	if muteChanged {
		if *lastMute == nil {
			m := muteVal
			*lastMute = &m
		} else {
			**lastMute = muteVal
		}
	}

	lastVolumeMu.Unlock()

	// Print changes
	if volChanged {
		fmt.Printf("[VOLUME] %.2f dB\n", volVal)
	}

	if muteChanged {
		muteStatus := "MUTED"
		if !muteVal {
			muteStatus = "UNMUTED"
		}
		fmt.Printf("[MUTE] %s\n", muteStatus)
	}
}

// sendCommand sends a command to the websocket server (thread-safe)
func sendCommand(conn *websocket.Conn, writeMu *sync.Mutex, cmd string) {
	payload, err := json.Marshal(cmd)
	if err != nil {
		log.Printf("error marshaling command: %v", err)
		return
	}

	writeMu.Lock()
	err = conn.WriteMessage(websocket.TextMessage, payload)
	writeMu.Unlock()

	if err != nil {
		log.Printf("error sending command: %v", err)
	}
}
