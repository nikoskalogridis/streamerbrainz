# Daemon Architecture Review (`cmd/streamerbrainz/daemon.go`)

This document reviews the daemon loop architecture and action handling in `cmd/streamerbrainz/daemon.go`, highlights strengths, and suggests optimizations / alternative approaches. It does **not** propose code changes directly; it’s a set of findings and recommendations.

---

## 1) What the current architecture gets right

### 1.1 Single-writer principle for core state
Your core intent is solid: **only one goroutine owns `velocityState` and communicates with CamillaDSP** (at least conceptually). This is the most important architectural decision because it prevents the “many inputs, many writers” race you’ll otherwise hit as input sources grow.

### 1.2 Clear separation between:
- **Intent updates** (held direction, target updates)
- **Periodic integration** (velocity update with `dt`)
- **Side effects** (sending `SetVolume` to CamillaDSP)

This is very close to a “state machine + effect sink” model, which scales well.

### 1.3 Explicit `dt` update
Driving the velocity engine with an explicit `dt` (`updateWithDt(dt, now)`) is a great choice:
- Testable
- More predictable than implicit time
- Easier to clamp / bound behavior

### 1.4 Tick-based cadence for DSP synchronization
Using a ticker loop and `shouldSendUpdate()` is an efficient way to:
- Reduce network chatter
- Avoid hammering CamillaDSP while still maintaining responsiveness

---

## 2) Key architectural inconsistencies / risks

### 2.1 The “only apply side effects in one place” rule is violated
The comments state:
- `handleAction` “only mutates intent - it does NOT talk to CamillaDSP directly”
- `applyVolume` “is the ONLY place that sends volume changes to CamillaDSP”

But `handleAction` currently **does** call CamillaDSP for:
- `VolumeStep` (calls `GetVolume` and `SetVolume`)
- `ToggleMute` (calls `ToggleMute`)
- `SetVolumeAbsolute` (calls `SetVolume`)

This creates a *split-brain side effect policy*:
- Some updates are immediate and bypass the velocity engine
- Some are scheduled via ticks and `applyVolume`
- Both mutate `velState`

**Impact:** it becomes harder to:
- Add consistent policy (rate limiting, smoothing, fades, safety bounds)
- Reason about invariants (e.g., “current volume is accurate”)
- Guarantee consistent ordering when actions and ticks interleave

**Recommendation:** adopt a single “effects sink” rule:
- `handleAction` mutates state/intents only
- Tick loop applies side effects in a consistent place
If you truly need immediate application for some actions (e.g., mute), you can still route them through a dedicated effect queue processed by the same goroutine, rather than calling DSP in the action handler.

### 2.2 Fairness and backlog behavior under load
The `select` has two cases: actions and ticker. In Go, selection is pseudo-random among ready cases. Under heavy action load, the ticker case may get delayed; under heavy tick scheduling, action processing might lag.

**Recommendation options:**
1. **Drain actions per tick**: on every tick, drain all pending actions (with a bounded loop) then update. This improves responsiveness and ensures intent updates are applied before integration.
2. **Priority handling**: if actions are available, drain them first, then do a single update. This reduces “tick thrash” when you already have a backlog.
3. **Backpressure strategy**: define what happens when `actions` channel is overwhelmed (drop/merge/coalesce).

### 2.3 Blocking I/O inside the loop harms timing guarantees
Any call to CamillaDSP (`GetVolume`, `SetVolume`, `ToggleMute`) is network I/O. If it blocks for 100ms+, your `dt` grows, integration clamps may kick in, and responsiveness changes.

**Recommendation:** Keep the main loop “mostly non-blocking”:
- Consider separating the DSP client I/O into a dedicated worker goroutine and communicate via channels, or
- Add timeouts / contexts for DSP operations so the daemon loop doesn’t stall indefinitely, or
- Implement a “command queue” that serializes DSP calls and allows measuring latency and retries.

If you keep I/O in the same goroutine (which is valid for strict serialization), you should at least treat the daemon loop as the DSP I/O scheduler and accept that `updateHz` becomes “best effort”.

### 2.4 Channel close / shutdown semantics are missing
`runDaemon` loops forever. If `actions` is closed, `case act := <-actions:` will produce the zero value repeatedly (and `act` will be `nil` for interfaces), leading to an infinite stream of “unknown action type”.

**Recommendation:** support graceful termination:
- Accept a `context.Context` or `done <-chan struct{}`
- Handle `act, ok := <-actions` and exit if `!ok`
- Stop ticker and return cleanly

This becomes important as soon as you embed this daemon in a system service with reload/stop behavior and tests.

---

## 3) Action design and policy suggestions

### 3.1 Strongly typed action envelope
Your `Action` is an interface with multiple concrete types. That’s fine, but consider a consistent envelope:
- Source metadata (device id, subsystem: rotary/librespot/plex, timestamp)
- Priority / category (immediate vs deferrable)
- Coalescing key (e.g., “volume target” can replace older “volume target”)

This makes it easier to:
- debug event flow
- implement policies like “drop old volume target updates”
- implement per-source behavior (e.g., rotary overrides librespot temporarily)

### 3.2 Coalescing and de-duplication
For volume-like signals, you can often collapse multiple actions:
- Many `VolumeHeld` repeats are redundant
- Multiple absolute volume requests: keep only latest
- Rotary steps: sum steps within a short window

**Recommendation:** implement a coalescer stage:
- Either in producers (preferred if possible)
- Or in the daemon (drain loop gathers and merges)

This reduces jitter and DSP chatter.

### 3.3 Centralize safety limits and invariants
Clamping to `MinDB`/`MaxDB` is done for rotary steps in `handleAction`, while velocity updates likely clamp elsewhere (not shown here). Make sure there is a single authority:
- One function that enforces bounds for *all* pathways
- Prefer clamping at the “intent set” boundary, not scattered

---

## 4) Timing model improvements

### 4.1 Ticker + `time.Now()` nuances
You’re using `now := <-ticker.C` and then `dt := now.Sub(lastTick).Seconds()`. Good. However:
- If the loop stalls (I/O), `ticker.C` will not “queue up” unlimited ticks. You’ll get fewer ticks than expected.
- `dt` becomes large and may cause a jump in the integrator.

**Recommendation:** define how you want large `dt` handled:
- Clamp dt in the velocity engine (you already mention a dt clamp)
- Optionally run multiple fixed-step updates when dt is large (semi-fixed timestep), but cap steps to avoid spiral of death:
  - `for dt > fixed { step(fixed); dt -= fixed } step(dt)`
This yields smoother, deterministic integration.

### 4.2 `updateHz` validation
If `updateHz` is 0 or negative, `time.Second / time.Duration(updateHz)` panics or divides by zero. Even if it’s always configured correctly, it’s worth guarding in the daemon entrypoint or config parsing layer.

---

## 5) Observability and diagnostics

### 5.1 Structured logging already in place
`slog` usage is good. Improvements that pay off quickly:
- Log loop timing when dt clamp triggers or when I/O latency exceeds thresholds
- Log coalescing decisions (“merged 12 rotary steps into +3.0dB”)
- Include correlation fields: `"source"`, `"action_type"`, `"device"`

### 5.2 Metrics (optional but valuable)
A minimal set:
- action queue length (or approximate backlog)
- tick drift (`actual_dt - expected_dt`)
- DSP call latency and error rate
- number of volume updates sent per minute (helps tune `shouldSendUpdate`)

---

## 6) Specific notes on current `handleAction` cases

### 6.1 `VolumeStep` bypasses velocity engine
This is a product/design decision as much as architecture. It can be valid (rotary feels immediate), but the cost is policy fragmentation.

**Better approach:** treat `VolumeStep` as an intent update:
- Convert steps into a delta on the target
- Let the tick loop apply it immediately on the next cycle (which at 60–200Hz will feel immediate)
- If you truly need immediate, send a “high priority volume apply” effect that’s still executed by the same goroutine and same policy function.

Also note: `VolumeStep` performs a `GetVolume` if `velState.volumeKnown` is false. That implies the daemon may “discover” state lazily. That’s fine, but be aware it introduces a blocking read on first use.

### 6.2 Mute as immediate side-effect
Mute toggles are typically UX-critical and can bypass smoothing. Still, you likely want:
- a single place where DSP is called
- consistent error handling/backoff

If you later add “mute state” into `velState`, you can also ensure volume updates don’t fight mute toggles (e.g., a tick volume update might unmute or interfere, depending on DSP behavior).

### 6.3 “No-op for now” actions
Good to have placeholders, but consider:
- Are they expected to be frequent? If yes, no-op actions can still load the daemon.
- If they’re used only for logging, consider routing them to a separate logger sink rather than the main loop if they become high-volume.

---

## 7) Suggested alternative architecture patterns (choose based on future scope)

### Option A: Pure “Reducer + Effects”
- **Reducer**: `newState = reduce(oldState, action, now)`
- **Effects**: reducer returns a list of effect commands (e.g., `SetVolume(db)` / `ToggleMute`) that are executed by the same goroutine after reduction.
- Tick produces a synthetic action (e.g., `Tick(now)`).

Pros: single mental model; easy to test with deterministic inputs.  
Cons: requires discipline and a bit more structure.

### Option B: Event bus + per-domain workers (if it grows)
Split into domain actors:
- Volume actor (velocity + target management)
- Playback actor (librespot, plex)
- DSP actor (CamillaDSP I/O)

Each actor owns its state and communicates via channels.  
Pros: scales to more domains.  
Cons: more complexity, cross-actor coordination needed.

### Option C: Keep current loop but formalize “immediate vs scheduled”
If you want a pragmatic middle ground:
- Define two queues: `immediateEffects` and `scheduledStateUpdates`
- `handleAction` may enqueue an effect rather than calling DSP directly
- Tick loop performs velocity integration then executes effects in order

Pros: minimal churn; retains single goroutine authority.  
Cons: still needs consistent invariants and ordering rules.

---

## 8) Concrete optimization checklist (no code, just actions)

1. **Unify the side-effect boundary**: make a single pathway to call CamillaDSP and enforce policy.
2. **Add shutdown handling**: context/done channel, handle closed action channel.
3. **Address blocking I/O**: add timeouts and/or separate DSP I/O worker.
4. **Improve fairness**: drain/coalesce actions around ticks.
5. **Define coalescing rules**: especially for volume-type actions.
6. **Strengthen invariants**: central clamp, consistent “volumeKnown” acquisition strategy.
7. **Add observability**: dt clamp events, DSP call latency, action throughput.

---

## 9) Final assessment

The core idea—**central daemon loop as a single writer that drives a velocity engine and synchronizes with DSP**—is a strong foundation. The main architectural improvement is to **fully enforce the separation between intent/state updates and side effects**. Once that boundary is consistent, the system becomes easier to evolve: adding sources (rotary, IR, librespot, web UI), adding policies (fades, safety limits, priority), and improving reliability (timeouts/retries) all become straightforward rather than invasive.