# IR integration (Linux evdev)

StreamerBrainz can read volume/mute key events from a Linux input device (evdev), typically an IR receiver that shows up as `/dev/input/eventX`.

This page is user-facing: how to configure StreamerBrainz to use the right device, required permissions, and common troubleshooting.

## Requirements

- Linux with evdev (`/dev/input/event*`)
- An IR receiver / remote that produces key events (e.g. `KEY_VOLUMEUP`, `KEY_VOLUMEDOWN`, `KEY_MUTE`)
- StreamerBrainz has read access to the chosen `/dev/input/eventX`

## What StreamerBrainz listens for

StreamerBrainz translates these key codes into internal actions:

- `KEY_VOLUMEUP`
- `KEY_VOLUMEDOWN`
- `KEY_MUTE`

Volume up/down are treated as “held/repeat + release” to drive velocity-based ramping.

## Configuration

StreamerBrainz is configured via YAML at `~/.config/streamerbrainz/config.yaml`.

### IR section

```yaml
ir:
  # Linux evdev input device for IR remote (must be readable by the daemon user)
  device: /dev/input/event6
```

**device**: Path to the Linux input event device (default: `/dev/input/event6`)

Example config:

```yaml
ir:
  device: /dev/input/event6
```

## Finding the correct `/dev/input/eventX`

### Option A: inspect device names
List devices and their human-readable names:

- `cat /proc/bus/input/devices`

Look for a block that matches your IR receiver and note its `eventX` handler.

### Option B: watch events while pressing buttons
You can use standard Linux tools like `evtest` to confirm which event device receives your remote presses. The exact command depends on your distro, but the general workflow is:

1. Run an event tester (e.g. `evtest`)
2. Select candidate `/dev/input/eventX`
3. Press volume up/down/mute on the remote
4. Confirm you see `KEY_VOLUMEUP`, `KEY_VOLUMEDOWN`, `KEY_MUTE`

## Permissions

Reading from `/dev/input/eventX` typically requires either:
- running StreamerBrainz with elevated privileges, or
- granting your user access to the input device(s)

Common approaches:

- Add your user to the `input` group (if your distro uses it), then re-login.
- Use a udev rule to set group/permissions for the IR receiver device.

The goal is: the user running StreamerBrainz must be able to open the device file for reading.

## Troubleshooting

### "failed to open input device"
This means StreamerBrainz could not open the device specified in `ir.device`.

Checklist:
1. Verify the device exists:
   - `ls -l /dev/input/event*`
2. Verify `ir.device` in your config points to the correct device.
3. Check permissions:
   - `ls -l /dev/input/eventX`
4. If running under systemd, confirm the service user matches the permissions you set.

### Remote presses do nothing (no volume change)
Checklist:
1. Confirm StreamerBrainz is running and connected to CamillaDSP (see `docs/camilladsp.md`).
2. Confirm your IR receiver produces key events:
   - use an event tester and press volume/mute
3. Ensure the receiver maps buttons to the expected keys:
   - StreamerBrainz currently listens for `KEY_VOLUMEUP`, `KEY_VOLUMEDOWN`, `KEY_MUTE`.

If your remote produces different key codes, you’ll need to remap them at the OS/input layer (or extend StreamerBrainz to recognize additional keys).

#### Verify key events with `evtest`
If you’re unsure whether your IR receiver is producing the expected key codes, use `evtest` to inspect the device directly:

1. Install `evtest` (package name is typically `evtest`).
2. Run it against the candidate input device:
   ```bash
   evtest /dev/input/eventX
   ```
3. Press volume up/down/mute on your remote and verify you see events like:
   - `KEY_VOLUMEUP`
   - `KEY_VOLUMEDOWN`
   - `KEY_MUTE`

If you see different `KEY_*` codes, StreamerBrainz won’t react unless those keys are remapped to the expected ones (or StreamerBrainz is extended to recognize them).

### Volume ramps but feels "jumpy" or "slow"
This usually isn't an IR issue. It's typically velocity tuning / update rate tuning in StreamerBrainz. Check these config keys:
- `camilladsp.update_hz`
- `velocity.max_db_per_sec`
- `velocity.accel_time_sec`
- `velocity.decay_tau_sec`

## Notes

- StreamerBrainz reads from exactly one `ir.device` path.
- For a fully-documented configuration example, see: `examples/config.yaml`
- For configuration reference, run: `streamerbrainz -help`
