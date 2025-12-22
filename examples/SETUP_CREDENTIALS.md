# Setting Up Systemd Encrypted Credentials for Plex Token

This guide shows how to securely store your Plex authentication token using systemd's encrypted credentials feature.

---

## Why Use Encrypted Credentials?

- ✅ Token is encrypted at rest
- ✅ Only readable by your user service
- ✅ Automatic decryption at service startup
- ✅ No plaintext tokens in service files or environment
- ✅ Better than storing tokens in config files

---

## Prerequisites

- systemd v250 or newer (check with `systemd --version`)
- A Plex authentication token

---

## Quick Setup

### 1. Get Your Plex Token

See the [Plexamp Quick Start](PLEXAMP_QUICKSTART.md#1-get-your-plex-token) guide.

### 2. Create Credential Directory

```bash
mkdir -p ~/.config/streamerbrainz
```

### 3. Encrypt Your Token

```bash
# Interactive method (recommended)
systemd-creds encrypt --name=plex-token - - > ~/.config/streamerbrainz/plex-token.cred
```

When prompted, paste your Plex token and press `Ctrl+D`.

**Alternative: From command line**

```bash
echo -n "YOUR_PLEX_TOKEN_HERE" | \
  systemd-creds encrypt --name=plex-token - - > ~/.config/streamerbrainz/plex-token.cred
```

### 4. Verify the Credential

```bash
# Check the file was created
ls -lh ~/.config/streamerbrainz/plex-token.cred

# Test decryption (requires systemd v252+)
systemd-creds decrypt ~/.config/streamerbrainz/plex-token.cred
```

### 5. Secure the Credential File

```bash
# Set restrictive permissions
chmod 600 ~/.config/streamerbrainz/plex-token.cred
```

---

## Using with the Service

The example service file is already configured:

```ini
# Optional but a good idea: hides the runtime creds dir from other services
PrivateMounts=yes

# Load encrypted credential from your home directory
LoadCredentialEncrypted=plex-token:%h/.config/streamerbrainz/plex-token.cred

# Pass the credential directory path (%d) to the program
ExecStart=%h/.local/bin/streamerbrainz \
    -plex-token-file=%d/plex-token \
    ...
```

**Key points:**
- `%h` expands to your home directory
- `%d` expands to the service's credential directory at runtime
- The token is automatically decrypted when the service starts

---

## Install and Start the Service

```bash
# Copy the service file
mkdir -p ~/.config/systemd/user
cp examples/streamerbrainz-plex.service ~/.config/systemd/user/

# Edit if needed (change machine ID, ports, etc.)
nano ~/.config/systemd/user/streamerbrainz-plex.service

# Reload systemd
systemctl --user daemon-reload

# Enable and start
systemctl --user enable --now streamerbrainz-plex

# Check status
systemctl --user status streamerbrainz-plex

# View logs
journalctl --user -u streamerbrainz-plex -f
```

---

## Alternative: Plain Text File (Less Secure)

If you're on an older systemd version without encrypted credentials support:

```bash
# Create token file
echo -n "YOUR_PLEX_TOKEN_HERE" > ~/.config/streamerbrainz/plex-token

# Secure it
chmod 600 ~/.config/streamerbrainz/plex-token
```

**Update service file:**

```ini
# Remove LoadCredentialEncrypted
# LoadCredentialEncrypted=plex-token:%h/.config/streamerbrainz/plex-token.cred

# Use LoadCredential instead
LoadCredential=plex-token:%h/.config/streamerbrainz/plex-token

# The rest stays the same
ExecStart=%h/.local/bin/streamerbrainz \
    -plex-token-file=%d/plex-token \
    ...
```

---

## Troubleshooting

### "Failed to decrypt credential"

- **Check systemd version:** `systemd --version` (need v250+)
- **Verify file exists:** `ls -l ~/.config/streamerbrainz/plex-token.cred`
- **Test decryption manually:** `systemd-creds decrypt ~/.config/streamerbrainz/plex-token.cred`

### "Failed to read plex token file"

- **Check service logs:** `journalctl --user -u streamerbrainz-plex -n 50`
- **Verify LoadCredentialEncrypted is correct** in service file
- **Check %d expansion:** Add `Environment="DEBUG=1"` to service and check if path is correct

### "Plex token file is empty"

- The credential file might have extra whitespace
- Re-create with `echo -n` (note the `-n` flag)

### Permission Denied

```bash
# Fix credential file permissions
chmod 600 ~/.config/streamerbrainz/plex-token.cred
chown $USER:$USER ~/.config/streamerbrainz/plex-token.cred
```

---

## Security Best Practices

### ✅ DO:
- Use `LoadCredentialEncrypted` for sensitive tokens
- Set `PrivateMounts=yes` to isolate credentials
- Use `chmod 600` on credential files
- Store credentials in `~/.config/` or similar
- Use `-plex-token-file` instead of `-plex-token` in production

### ❌ DON'T:
- Commit `.cred` files to version control
- Share credential files between users
- Store plaintext tokens in service files
- Use world-readable permissions

---

## Rotating Tokens

If your Plex token changes:

```bash
# Stop the service
systemctl --user stop streamerbrainz-plex

# Re-encrypt new token
echo -n "NEW_PLEX_TOKEN" | \
  systemd-creds encrypt --name=plex-token - - > ~/.config/streamerbrainz/plex-token.cred

# Restart service
systemctl --user restart streamerbrainz-plex
```

---

## Complete Example

```bash
#!/bin/bash
# Complete setup script

set -e

# Get token from user
read -sp "Enter your Plex token: " PLEX_TOKEN
echo

# Create directory
mkdir -p ~/.config/streamerbrainz

# Encrypt and save
echo -n "$PLEX_TOKEN" | \
  systemd-creds encrypt --name=plex-token - - > \
  ~/.config/streamerbrainz/plex-token.cred

# Secure it
chmod 600 ~/.config/streamerbrainz/plex-token.cred

echo "✓ Credential encrypted and saved"
echo "  Location: ~/.config/streamerbrainz/plex-token.cred"

# Test decryption
echo -n "Testing decryption... "
systemd-creds decrypt ~/.config/streamerbrainz/plex-token.cred > /dev/null
echo "✓ OK"

echo ""
echo "Now install the service:"
echo "  cp examples/streamerbrainz-plex.service ~/.config/systemd/user/"
echo "  systemctl --user daemon-reload"
echo "  systemctl --user enable --now streamerbrainz-plex"
```

---

## References

- [systemd-creds manual](https://www.freedesktop.org/software/systemd/man/systemd-creds.html)
- [systemd.exec - LoadCredential](https://www.freedesktop.org/software/systemd/man/systemd.exec.html#LoadCredential=)
- [Encrypted Service Credentials (systemd blog)](https://systemd.io/CREDENTIALS/)