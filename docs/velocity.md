# Velocity Engine

This document describes StreamerBrainz’ velocity-based volume control engine: what it does, why it exists, how it works, and how to tune it.

It is written against the current implementation in `cmd/streamerbrainz/velocity.go` and the daemon loop in `cmd/streamerbrainz/daemon.go`.

It also documents the two supported “press-and-hold” modes and how tuning flags are interpreted in each mode.

---

## Goals

Traditional “press volume up/down” implementations often feel either:

- **Too step-y** (fixed increment per press), or
- **Too risky** near maximum volume (fast ramp to 0 dB), or
- **Jittery** (spamming the audio engine with tiny updates).

StreamerBrainz’ velocity engine aims to:

1. **Provide a natural press-and-hold feel** via acceleration and inertia-like decay.
2. **Be safe near maximum volume** by enforcing a “danger zone” ramp-up speed limit.
3. **Be stable across update rates** (e.g. 30 Hz or 80 Hz) with dt-based integration.
4. **Avoid races** by keeping all state mutation in the single-owner daemon loop.
5. **Play nicely with real input hardware** by tolerating missing release events.

---

## High-level behavior

The engine maintains:

- `targetDB`: where we want the volume to end up (in dB)
- `velocityDBPerS`: how fast `targetDB` is currently changing (in dB/s)

When you hold volume up/down:

- velocity accelerates in that direction until it hits a max allowed speed (`velMax`)
- `targetDB` integrates velocity over time (`targetDB += velocity * dt`)

When you release:

- velocity decays exponentially toward zero (smooth stopping)

Finally:

- `targetDB` is clamped to `[minDB, maxDB]`
- the daemon sends volume to CamillaDSP only if `|targetDB - currentVolume| > volumeUpdateThresholdDB`

---

## Data flow and ownership model

**Inputs** (IR, integrations) emit `Action`s.

The daemon loop in `runDaemon()` is the single owner of:

- velocity state
- CamillaDSP I/O

At each tick:

1. Compute `dt` from the ticker (`now.Sub(lastTick)`)
2. Call `velState.updateWithDt(dt, now)`
3. If `velState.shouldSendUpdate()` is true, call `applyVolume()` which calls CamillaDSP and then `velState.updateVolume(currentVol)`.

**Single-owner guarantee:** `velocityState` is intentionally *not* concurrency-safe. It is designed to be mutated and read only by the daemon goroutine. This keeps the engine simple and avoids lock contention/race complexity.

If you ever need to read or mutate velocity state from another goroutine, do it by sending an `Action` into the daemon and letting the daemon perform the read/mutation.

---

## Configuration model (VelocityConfig)

The velocity engine is configured via a single config struct:

- `type VelocityConfig struct { ... }`

It contains:

- mode selection (`Mode`)
- core dynamics / rates (`VelMaxDBPerS`, `AccelTime`, `DecayTau`)
- bounds (`MinDB`, `MaxDB`)
- robustness (`HoldTimeout`, `MaxDt`)
- danger-zone params (`DangerZoneDB`, `DangerVelMaxDBPerS`, `DangerVelMinNear0DBPerS`)

This replaces the older style of “construct + a handful of setters”.

---

## Velocity modes (`-vel-mode`)

StreamerBrainz supports two “press-and-hold” modes:

- `accelerating` (default): classic velocity engine with acceleration and release decay
- `constant`: constant-rate hold with an optional “turbo” after a delay

The mode is selected via:

- `-vel-mode accelerating|constant`

Constant mode turbo is configured via explicit flags (`-vel-turbo-mult`, `-vel-turbo-delay`) so that `-vel-accel-time` and `-vel-decay-tau` remain dedicated to accelerating mode.

---

## Key concepts and parameters

### Core tuning (mode-dependent meaning)

#### Shared (both modes)
- `-vel-max-db-per-sec` → `VelMaxDBPerS`

In accelerating mode, this is the maximum velocity.  
In constant mode, this is the base hold rate.

#### Accelerating mode (`-vel-mode accelerating`)
- `VelMaxDBPerS` (`-vel-max-db-per-sec`)  
  Maximum allowed speed outside the danger zone, in dB/s.

- `AccelTime` (`-vel-accel-time`)  
  Time to reach `VelMaxDBPerS`. Acceleration is derived as:  
  `accelDBPerS2 = VelMaxDBPerS / AccelTime` (if `AccelTime > 0`).

- `DecayTau` (`-vel-decay-tau`)  
  Exponential decay time constant (seconds). When not held:  
  `velocity *= exp(-dt / DecayTau)`.

#### Constant mode (`-vel-mode constant`) + turbo
In constant mode, the engine does **not** accelerate/decelerate a velocity state. Instead it applies a constant rate while held.

- Base rate:
  - `VelMaxDBPerS` (`-vel-max-db-per-sec`)  
    Base (normal) hold rate in dB/s.

- Turbo configuration (explicit flags):
  - `-vel-turbo-mult`  
    Turbo multiplier (unitless). If `> 1`, turbo increases the hold rate to:

    `turboRate = baseRate * vel-turbo-mult`

    If `<= 1`, turbo is effectively disabled.

  - `-vel-turbo-delay`  
    Turbo activation delay in seconds.

    - If `> 0`: turbo engages after holding continuously for `vel-turbo-delay` seconds
    - If `== 0`: turbo is immediate (always turbo while held)

Turbo resets when the hold is released, and also when direction changes.

The daemon logs print the chosen `vel_mode` and the raw values so you can verify configuration at startup.

### Update cadence
- The daemon tick rate (`-camilladsp-update-hz`) controls how often the engine updates.
- The engine is dt-driven; behavior should not fundamentally depend on Hz.
- A defensive clamp is applied:
  - the engine clamps `dt` to `MaxDt` (seconds)
  - by default, the daemon sets this relative to update rate: `MaxDt = 2.0 / updateHz`

### Volume bounds
- `MinDB`, `MaxDB` are hard clamps for `targetDB`.

### Sending threshold
- `volumeUpdateThresholdDB` in `cmd/streamerbrainz/constants.go` prevents sending updates for tiny differences.

---

## Danger zone (near max volume)

The danger zone is designed to prevent rapid increases near max volume (e.g. near 0 dB), where fast ramping can damage speakers.

### Definition

Danger zone is defined **relative to max volume**:

- `dangerThreshold = MaxDB - DangerZoneDB`

When ramping **up**, if the estimated volume is above `dangerThreshold`, the danger zone logic engages.

This avoids hardcoding “-12 dB” and correctly follows non-zero `MaxDB` configs.

### Estimated volume (simplified)

The engine intentionally uses a conservative estimate so the danger zone engages early enough:

- If volume is known: `estVol = max(currentVolume, targetDB)`
- Else: `estVol = targetDB`

This is simpler than a weighted blend and is safer for ramp-up (it considers both actual volume and where you’re trying to go).

**Constant mode note:** danger-zone limiting applies to the (possibly turbo) hold rate as well. In practice: even if turbo is active, ramp-up is still capped in the danger zone.

### Hard cap + extra caution near max

Within the danger zone, ramp-up max speed is computed in two layers:

1) **Immediate hard cap** upon entering the zone  
   `velMax` is limited to at most `DangerVelMaxDBPerS` (at zone entry).

2) **Extra caution near max volume**  
   As you approach `MaxDB`, `velMax` is progressively reduced toward a minimum (to avoid a “sticky top” while still being very safe).

Implementation details:

- Compute normalized zone progress:

  `x = (estVol - dangerThreshold) / (MaxDB - dangerThreshold)` clamped to `[0..1]`

- Compute reduction factor:

  `extra = 1 - x^3`

  - At zone entry: `x=0` => `extra=1`
  - At max: `x=1` => `extra=0`

- Convert this into a velocity bound that never goes below a small minimum:

  `velMax = DangerVelMinNear0DBPerS + (DangerVelMaxDBPerS - DangerVelMinNear0DBPerS) * extra`

So:

- At zone entry: `velMax ≈ DangerVelMaxDBPerS`
- Near max: `velMax → DangerVelMinNear0DBPerS`

### Directionality

Danger zone affects **ramp-up only** (`heldDirection == 1`). Ramp-down behaves like the rest of the band.

---

## Hold tracking and auto-release

Real input devices can be messy:
- release events may be dropped
- repeats might be irregular
- integrations might emit “held” intents without clean releases

To avoid getting “stuck accelerating forever”, the engine includes:

- `lastHeldAt` updated on every `setHeld(direction)`
- `HoldTimeout` (e.g. 600ms)

On each update:

- If a hold is currently active and `now - lastHeldAt > HoldTimeout`, it forces `heldDirection = 0`.

This protects the system and keeps behavior predictable.

Tuning:
- If you see premature releases, increase `vel-hold-timeout-ms`.
- If you want faster safety stop on missing releases, decrease it.

---

## Direction reversal behavior

### Accelerating mode
When changing from UP to DOWN or vice versa, the engine resets velocity to zero if the sign disagrees with the new direction.

Without this, a quick reversal would first need to “brake” through zero velocity, which feels sluggish.

### Constant mode
Direction reversal is immediate: the signed hold rate simply flips sign (and turbo “hold duration” is reset, so turbo does not carry across direction changes).

---

## When does it actually send to CamillaDSP?

The engine updates its internal `targetDB` every tick, but CamillaDSP updates are only sent when:

- `volumeKnown == true`, and
- `|targetDB - currentVolume| > volumeUpdateThresholdDB`

After a successful `SetVolume(targetDB)` call, the daemon calls:

- `velState.updateVolume(currentVol)`

which also sets:

- `targetDB = currentVol` (synchronizes target with the authoritative actual volume)

This reduces drift and stabilizes the send loop.

---

## Logging / debugging

The engine intentionally contains no internal debug logging state and is single-owner (daemon-driven). If you need to debug danger-zone behavior, prefer:
- logging in the daemon around calls to `updateWithDt()` and `applyVolume()`, or
- temporarily adding targeted logs in the engine while tuning (and removing them once done).

A useful set of values to log while tuning:
- `heldDirection`, `dt`
- `targetDB`, `currentVolume`
- danger-zone threshold (`MaxDB - DangerZoneDB`)
- `velMax` chosen for the tick

---

## Is the engine overcomplicated? What can be simplified?

The engine is featureful because it is trying to satisfy multiple “real world” constraints:
- safe near max volume
- responsive direction changes
- stable across update rates
- robust to missing release events

The most meaningful simplifications (without losing safety) are already reflected in the current design:
- a single `VelocityConfig` (instead of setters)
- a conservative `estVol = max(currentVolume, targetDB)` (instead of a heuristic blend)
- moving debug logging out of the engine core

### Single-owner note (mutex removed)

The engine is already implemented as a single-owner state machine (daemon-driven) and does not use a mutex. This is a deliberate design choice to:

- keep the hot-path small and predictable
- make tuning/behavior easier to reason about
- avoid introducing incidental concurrency complexity

The trade-off is that you must preserve the invariant: only the daemon goroutine calls into `velocityState`. If you need cross-goroutine interaction, communicate via `Action`s and let the daemon perform the work.


### Further possible simplification: Make danger-zone velocity a step function (no “extra caution” curve)
If you only need the hard cap and don’t care about additional taper near max:

- in danger zone: `velMax = DangerVelMaxDBPerS`

Pros:
- very simple, very safe
- easier to tune

Cons:
- may feel abrupt approaching max (still safe, just less “analog” feel)

---

## Tuning guidelines (practical)

1) Pick a normal max ramp speed:
- `vel-max-db-per-sec`: typical range might be 10–20 dB/s depending on preference.

2) Choose acceleration time:
- shorter accel time = quicker “gets going”
- longer accel time = gentler feel

3) Set danger-zone size:
- `vel-danger-zone-db`: how many dB below max should be protected (e.g. 12).

4) Set danger-zone hard cap:
- `vel-danger-vel-max-db-per-sec`: your “never go faster than this near max” value (e.g. 3).

5) Set minimum near max:
- `vel-danger-vel-min-near0-db-per-sec`: small, but nonzero (e.g. 0.3) to avoid stickiness.

6) Adjust send threshold:
- `volumeUpdateThresholdDB`: smaller => smoother tracking but more updates; larger => fewer updates but potentially more “stair stepping”.

---

## Related files

- `cmd/streamerbrainz/velocity.go` — engine implementation
- `cmd/streamerbrainz/daemon.go` — tick loop, update/send policy
- `cmd/streamerbrainz/constants.go` — defaults and thresholds
- `docs/camilladsp.md` — connection and volume bounds
- `docs/ARCHITECTURE.md` — broader system architecture