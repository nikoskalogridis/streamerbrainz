# Multi-Device Input Configuration

StreamerBrainz supports reading key events from multiple input devices simultaneously. This allows you to control volume and media playback from various sources like IR remotes, keyboards, USB rotary encoders, and media keypads.

## Configuration

### New Configuration Format (v1.0.0+)

Use the `devices` array to specify multiple input devices:

```yaml
ir:
  devices:
    - /dev/input/event6  # IR remote receiver
    - /dev/input/event3  # Keyboard
    - /dev/input/event8  # USB rotary encoder
```

### Legacy Configuration Format (Deprecated)

The old single-device format is still supported for backward compatibility:

```yaml
ir:
  device: /dev/input/event6
```

**Note:** If both `device` and `devices` are specified, `devices` takes precedence.

## Finding Your Input Devices

### List All Input Devices

To see all available input devices on your system:

```bash
ls -la /dev/input/by-id/
```

Or view event mappings:

```bash
ls -la /dev/input/
```

### Identify Device Capabilities

To see which keys a device supports:

```bash
sudo evtest /dev/input/event3
```

Press keys on the device to see their event codes. Look for these media key codes:

- `KEY_VOLUMEUP` (115)
- `KEY_VOLUMEDOWN` (114)
- `KEY_MUTE` (113)
- `KEY_PLAYPAUSE` (164)
- `KEY_NEXTSONG` (163)
- `KEY_PREVIOUSSONG` (165)
- `KEY_PLAYCD` (200)
- `KEY_PAUSECD` (201)
- `KEY_STOPCD` (166)

### Find Device by Name

```bash
cat /proc/bus/input/devices
```

Look for the `Handlers` line to find the corresponding event device.

## Supported Key Events

StreamerBrainz currently supports the following key events:

### Volume Control (Fully Implemented)

- **KEY_VOLUMEUP** - Increases volume (with velocity control)
- **KEY_VOLUMEDOWN** - Decreases volume (with velocity control)
- **KEY_MUTE** - Toggles mute state

### Media Control (Logging Only)

The following media keys are detected and logged but do not currently trigger actions:

- **KEY_PLAYPAUSE** - Play/Pause toggle
- **KEY_NEXTSONG** - Next track
- **KEY_PREVIOUSSONG** - Previous track
- **KEY_PLAYCD** - Play
- **KEY_PAUSECD** - Pause
- **KEY_STOPCD** - Stop

> **Note:** Media control actions will be implemented in a future release. The daemon currently logs these events for debugging purposes.

## Example Configurations

### IR Remote + Keyboard

```yaml
ir:
  devices:
    - /dev/input/by-id/usb-FLIRC.tv_flirc-event-kbd
    - /dev/input/by-id/usb-Logitech_USB_Keyboard-event-kbd
```

### Multiple USB Controllers

```yaml
ir:
  devices:
    - /dev/input/event6  # IR receiver
    - /dev/input/event7  # USB volume knob
    - /dev/input/event8  # USB media keypad
```

### Symbolic Links (Recommended)

Using symbolic links makes configuration more portable and readable:

```yaml
ir:
  devices:
    - /dev/input/by-id/usb-flirc-ir-receiver
    - /dev/input/by-id/usb-encoder-volume-knob
    - /dev/input/by-path/platform-keyboard
```

## Permissions

StreamerBrainz needs read access to input devices. You have two options:

### Option 1: Run as Root (Not Recommended)

```bash
sudo streamerbrainz -config ~/.config/streamerbrainz/config.yaml
```

### Option 2: Add User to Input Group (Recommended)

```bash
sudo usermod -a -G input $USER
```

Then log out and log back in for the group change to take effect.

### Option 3: udev Rules (Most Secure)

Create a custom udev rule for specific devices:

```bash
sudo nano /etc/udev/rules.d/99-streamerbrainz.rules
```

Add rules like:

```
# FLIRC IR receiver
SUBSYSTEM=="input", ATTRS{idVendor}=="20a0", ATTRS{idProduct}=="0006", MODE="0660", GROUP="streamerbrainz"

# Generic USB rotary encoder
SUBSYSTEM=="input", ATTRS{idVendor}=="1234", ATTRS{idProduct}=="5678", MODE="0660", GROUP="streamerbrainz"
```

Create the group and add your user:

```bash
sudo groupadd streamerbrainz
sudo usermod -a -G streamerbrainz $USER
```

Reload udev rules:

```bash
sudo udevadm control --reload-rules
sudo udevadm trigger
```

## Troubleshooting

### Device Not Found

If StreamerBrainz can't open a device:

```
ERROR failed to open input device device=/dev/input/event6 error=open /dev/input/event6: no such file or directory
```

**Solution:** Verify the device exists:

```bash
ls -la /dev/input/event6
```

### Permission Denied

```
ERROR failed to open input device device=/dev/input/event6 error=open /dev/input/event6: permission denied
```

**Solution:** Check permissions and add user to `input` group (see Permissions section above).

### Events Not Registering

If keys are pressed but nothing happens:

1. **Enable debug logging:**

   ```yaml
   logging:
     level: debug
   ```

2. **Check device is sending events:**

   ```bash
   sudo evtest /dev/input/event6
   ```

3. **Verify key codes match:** Compare the event codes from `evtest` with the supported key codes listed above.

### One Device Failing Doesn't Stop Others

StreamerBrainz is designed to be resilient. If one device fails after startup, the daemon will log an error but continue processing events from other devices:

```
ERROR input reader error error=read /dev/input/event6: input/output error
```

The daemon will continue running with the remaining devices.

## Architecture Notes

### Concurrent Device Readers

StreamerBrainz spawns a separate goroutine for each input device. All goroutines write to a shared event channel, which is then processed by the main event loop.

```
┌─────────────┐
│ Device 1    │──┐
└─────────────┘  │
                 │
┌─────────────┐  │    ┌──────────────┐    ┌──────────────┐
│ Device 2    │──┼───▶│ Event Channel│───▶│  Main Loop   │
└─────────────┘  │    └──────────────┘    └──────────────┘
                 │
┌─────────────┐  │
│ Device 3    │──┘
└─────────────┘
```

### Event Deduplication

Currently, StreamerBrainz does **not** deduplicate events. If the same key is pressed on multiple devices simultaneously, multiple events will be processed. This is generally not an issue for volume control due to the velocity-based engine.

## Migration Guide

### From Single Device to Multiple Devices

**Old config:**

```yaml
ir:
  device: /dev/input/event6
```

**New config:**

```yaml
ir:
  devices:
    - /dev/input/event6
    - /dev/input/event3
```

### Using Command-Line Override

The `-ir-device` flag (if it exists) will set both the legacy `device` field and the new `devices` array:

```bash
streamerbrainz -config config.yaml -ir-device /dev/input/event6
```

This sets `devices: ["/dev/input/event6"]` internally.

## Future Enhancements

Planned improvements for multi-device input:

- [ ] Media control action implementation (play/pause/next/previous)
- [ ] Per-device key mapping configuration
- [ ] Device hot-plug support (add devices at runtime)
- [ ] Event filtering and deduplication options
- [ ] Device health monitoring and auto-recovery

## See Also

- [IR Integration Guide](ir.md) - Detailed IR remote setup
- [Architecture](ARCHITECTURE.md) - System design overview
- [Configuration Reference](../README.md#configuration) - Full config options