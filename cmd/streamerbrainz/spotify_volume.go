package main

import "math"

// mapSpotifyVolumeToDB maps Spotify volume (0-65535) to dB range.
// Uses logarithmic mapping for better perceived volume control.
//
// Notes:
// - spotifyVol==0 maps to minDB
// - spotifyVol==65535 maps to maxDB
func mapSpotifyVolumeToDB(spotifyVol uint16, minDB, maxDB float64) float64 {
	if spotifyVol == 0 {
		return minDB
	}
	if spotifyVol == 65535 {
		return maxDB
	}

	// Normalize to 0.0-1.0
	normalized := float64(spotifyVol) / spotifyVolumeMax

	// Apply logarithmic curve
	dbRange := maxDB - minDB
	logValue := math.Log10(1.0 + 9.0*normalized)
	db := minDB + dbRange*logValue

	return db
}
