# Multi-Device Input Quick Start Guide

This guide will help you quickly set up StreamerBrainz to read from multiple input devices (keyboards, IR remotes, USB rotary encoders, media keypads, etc.).

## What Changed?

**StreamerBrainz now supports reading from multiple input devices simultaneously!**

Previously, you could only specify one device. Now you can monitor multiple devices at the same time, allowing you to control volume from:

- IR remote controls
- Keyboard media keys
- USB rotary encoders
- USB media keypads
- Any Linux input device that sends volume/media key events

## Quick Setup (3 Steps)

### Step 1: Find Your Input Devices

List all available input devices:

```bash
ls -la /dev/input/by-id/
```

Or use event numbers:

```bash
ls -la /dev/input/
```

### Step 2: Test Each Device

Identify which keys each device sends:

```bash
sudo evtest /dev/input/event3
```

Press keys on your device and look for these event codes:
- `KEY_VOLUMEUP` (115)
- `KEY_VOLUMEDOWN` (114)
- `KEY_MUTE` (113)
- `KEY_PLAYPAUSE` (164)
- `KEY_NEXTSONG` (163)
- `KEY_PREVIOUSSONG` (165)

### Step 3: Update Your Config

Edit `~/.config/streamerbrainz/config.yaml`:

```yaml
ir:
  devices:
    - /dev/input/event6  # IR remote
    - /dev/input/event3  # Keyboard
    - /dev/input/event8  # USB encoder
```

That's it! Restart StreamerBrainz and all devices will work.

## Configuration Examples

### Using Stable Device IDs (Recommended)

```yaml
ir:
  devices:
    - /dev/input/by-id/usb-FLIRC.tv_flirc-event-kbd
    - /dev/input/by-id/usb-Logitech_USB_Keyboard-event-kbd
    - /dev/input/by-id/usb-Custom_Rotary_Encoder-event-if00
```

**Why?** Event numbers (event0, event1, etc.) can change on reboot. Using `/dev/input/by-id/` paths is more stable.

### Using Event Numbers (Simpler, Less Stable)

```yaml
ir:
  devices:
    - /dev/input/event6
    - /dev/input/event3
    - /dev/input/event7
```

### Legacy Single Device (Still Supported)

```yaml
ir:
  device: /dev/input/event6
```

This still works but is deprecated. Use `devices` instead.

## Permissions

StreamerBrainz needs read access to input devices.

### Quick Fix: Add User to Input Group

```bash
sudo usermod -a -G input $USER
```

**Important:** Log out and log back in for this to take effect!

### Verify Permissions

```bash
groups $USER
```

You should see `input` in the list.

### Test Access

```bash
cat /dev/input/event6 > /dev/null
```

If this gives "Permission denied", you need to fix permissions first.

## Supported Keys

### ✅ Fully Working (Volume Control)

- **Volume Up** - Increases volume with velocity control
- **Volume Down** - Decreases volume with velocity control
- **Mute** - Toggles mute on/off

### 🚧 Detected but Not Yet Implemented (Media Control)

These keys are detected and logged, but don't trigger actions yet:

- Play/Pause
- Next Track
- Previous Track
- Play
- Pause
- Stop

**Note:** Media control actions will be added in a future release. For now, you'll see debug logs when these keys are pressed.

## Troubleshooting

### "Device not found" Error

```
ERROR failed to open input device device=/dev/input/event6 error=no such file or directory
```

**Fix:** Check if device exists:

```bash
ls -la /dev/input/event6
```

If it doesn't exist, the device may have a different event number or isn't connected.

### "Permission denied" Error

```
ERROR failed to open input device device=/dev/input/event6 error=permission denied
```

**Fix:** Add user to input group (see Permissions section above).

### Keys Not Working

1. **Enable debug logging** in `config.yaml`:

   ```yaml
   logging:
     level: debug
   ```

2. **Restart StreamerBrainz** and press keys

3. **Look for event logs** like:

   ```
   DEBUG media key key=play/pause
   ```

4. **If you don't see logs**, the device may not be sending the right key codes. Test with `evtest`.

### One Device Fails, Others Still Work

StreamerBrainz is resilient! If one device fails, you'll see an error but the other devices continue working:

```
ERROR input reader error error=read /dev/input/event6: input/output error
```

The daemon keeps running with the remaining devices.

## How It Works

StreamerBrainz spawns a separate thread for each input device. All threads send events to a shared channel, which is processed by the main event loop.

```
┌─────────────────┐
│  IR Remote      │─────┐
│  (Device 1)     │     │
└─────────────────┘     │
                        ▼
┌─────────────────┐   ┌──────────────┐   ┌──────────────┐
│  Keyboard       │──▶│ Event Queue  │──▶│  Main Loop   │──▶ CamillaDSP
│  (Device 2)     │   └──────────────┘   └──────────────┘
└─────────────────┘     ▲
                        │
┌─────────────────┐     │
│  USB Encoder    │─────┘
│  (Device 3)     │
└─────────────────┘
```

## Migration from Old Config

If you have an old single-device config:

**Before:**
```yaml
ir:
  device: /dev/input/event6
```

**After:**
```yaml
ir:
  devices:
    - /dev/input/event6
    - /dev/input/event3  # Add more devices!
```

The old format still works, but you should migrate to the new format.

## Example Use Cases

### Home Theater Setup

```yaml
ir:
  devices:
    - /dev/input/by-id/usb-FLIRC.tv_flirc-event-kbd  # IR remote for couch
    - /dev/input/by-id/usb-Logitech_Keyboard-event   # Keyboard near equipment
```

### Desk Audio Setup

```yaml
ir:
  devices:
    - /dev/input/by-id/usb-Custom_Volume_Knob-event-if00  # Rotary encoder
    - /dev/input/by-path/platform-i8042-serio-0-event-kbd # Built-in keyboard
```

### Minimal Setup

```yaml
ir:
  devices:
    - /dev/input/event6  # Whatever input device you have
```

## Next Steps

- **Full documentation:** See [docs/multi-device-input.md](multi-device-input.md)
- **Example configs:** See [examples/config-multi-device.yaml](../examples/config-multi-device.yaml)
- **IR setup guide:** See [docs/ir.md](ir.md)

## Getting Help

If you run into issues:

1. Enable debug logging (`level: debug`)
2. Check device permissions (`ls -la /dev/input/eventX`)
3. Test devices individually with `evtest`
4. Review logs for specific error messages

For media key support (play/pause/next/previous), watch for future updates!