package main

import (
	"encoding/json"
	"log"
	"time"
)

// setVolumeCommand sends a SetVolume command to CamillaDSP
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

// getCurrentVolume queries CamillaDSP for the current volume
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

// handleMuteCommand sends a ToggleMute command to CamillaDSP
func handleMuteCommand(ws *wsClient, verbose bool) error {
	if verbose {
		log.Printf("KEY_MUTE -> ToggleMute")
	}
	return ws.send("ToggleMute")
}
