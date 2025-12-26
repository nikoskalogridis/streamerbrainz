# Planned features

This document lists **intended** (not yet implemented) features for StreamerBrainz. It is deliberately concise and may change.

## Plex integration

### Player state + metadata (beyond logging)
- Make the selected player’s playback state and metadata available to daemon features beyond log output
- Track at least:
  - playback state (playing/paused/stopped)
  - track metadata (title/artist/album)
  - timing (duration/position)
  - player identity (machineIdentifier/title/product)

### Player transport control (“media keys”)
Using Plex Media Server’s remote control endpoints to target a specific player (`machineIdentifier`):

- Play/Pause (toggle)
- Next track
- Previous track
- Stop

Notes:
- Control will be scoped to a single selected player (same selection model as state tracking)
- Endpoint details, auth, and error handling will be documented when implemented

## Core daemon

### Policy/actions based on playback state
- Trigger volume fades or ramps on playback transitions (e.g., pause/stop)
- Optional source priority / source arbitration when multiple integrations emit events

### Configuration / extensibility
- ~~Config file support (in addition to flags), if/when needed~~
- Clean public API (separate from internal integration plumbing)

## Librespot integration

- Broaden event handling beyond the current “hook forwards events into daemon” approach (as needed)
- Map player volume/state events into consistent internal actions for policy features
