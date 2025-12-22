# Docker Deployment Guide

This guide covers building and deploying StreamerBrainz using Docker containers, with support for both amd64 and ARM64 (Raspberry Pi 4+) architectures.

---

## Table of Contents

- [Prerequisites](#prerequisites)
- [Quick Start](#quick-start)
- [Building Images](#building-images)
- [Running Containers](#running-containers)
- [Docker Compose](#docker-compose)
- [Multi-Architecture Builds](#multi-architecture-builds)
- [Configuration](#configuration)
- [Troubleshooting](#troubleshooting)

---

## Prerequisites

### Required
- Docker 20.10+ with buildx support
- Linux host with input device access (`/dev/input/eventX`)
- CamillaDSP running and accessible via WebSocket

### For Multi-Architecture Builds
```bash
# Create and use a multi-platform builder
docker buildx create --name multiarch --use
docker buildx inspect --bootstrap
```

### Verify Docker Buildx
```bash
docker buildx version
# Should output: github.com/docker/buildx vX.X.X
```

---

## Quick Start

### Pull and Run (if image is published)
```bash
docker pull ghcr.io/username/streamerbrainz:latest

docker run -d \
  --name streamerbrainz \
  --restart unless-stopped \
  --device /dev/input/event6:/dev/input/event6 \
  --network host \
  -v /tmp:/tmp \
  ghcr.io/username/streamerbrainz:latest \
  streamerbrainz -ir-device /dev/input/event6 -log-level info
```

### Build and Run Locally
```bash
# Build for current architecture
make docker-build

# Run the container
docker run -d \
  --name streamerbrainz \
  --device /dev/input/event6:/dev/input/event6 \
  --network host \
  streamerbrainz:latest \
  streamerbrainz -ir-device /dev/input/event6
```

---

## Building Images

### Using Makefile (Recommended)

```bash
# Build for current architecture
make docker-build

# Build for all architectures (amd64 + arm64)
make docker-build-all

# Build for amd64 only
make docker-build-amd64

# Build for Raspberry Pi 4+ (arm64) only
make docker-build-arm64

# Build with custom tag
make docker-build DOCKER_TAG=v1.0.0

# Build and push to registry
make docker-push DOCKER_REGISTRY=ghcr.io/username DOCKER_TAG=latest
```

### Using Build Script

The `docker-build.sh` script provides more control:

```bash
# Show help
./docker-build.sh --help

# Build for current platform and load into Docker
./docker-build.sh --load

# Build for all architectures
./docker-build.sh --all

# Build for Raspberry Pi 4 only
./docker-build.sh --arm64 --tag rpi4

# Build and push to registry
./docker-build.sh --all --push --registry ghcr.io/username

# Build specific version
VERSION=1.0.0 ./docker-build.sh --all --push
```

### Using Docker Directly

```bash
# Single architecture build
docker build -t streamerbrainz:latest .

# Multi-architecture build
docker buildx build \
  --platform linux/amd64,linux/arm64 \
  -t streamerbrainz:latest \
  --push \
  .

# Build for Raspberry Pi 4 only
docker buildx build \
  --platform linux/arm64 \
  -t streamerbrainz:rpi4 \
  --load \
  .
```

---

## Running Containers

### Basic Usage

```bash
docker run -d \
  --name streamerbrainz \
  --restart unless-stopped \
  --device /dev/input/event6:/dev/input/event6 \
  --network host \
  -v /tmp:/tmp \
  streamerbrainz:latest \
  streamerbrainz \
    -ir-device /dev/input/event6 \
    -camilladsp-ws-url ws://127.0.0.1:1234 \
    -ipc-socket /tmp/streamerbrainz.sock \
    -log-level info
```

### With Plex Integration

```bash
# Create Plex token file
echo "YOUR_PLEX_TOKEN" > /path/to/plex-token

# Run with Plex integration
docker run -d \
  --name streamerbrainz \
  --restart unless-stopped \
  --device /dev/input/event6:/dev/input/event6 \
  --network host \
  -v /tmp:/tmp \
  -v /path/to/plex-token:/run/secrets/plex-token:ro \
  streamerbrainz:latest \
  streamerbrainz \
    -ir-device /dev/input/event6 \
    -camilladsp-ws-url ws://127.0.0.1:1234 \
    -plex-server-url http://plex.home.arpa:32400 \
    -plex-token-file /run/secrets/plex-token \
    -plex-machine-id YOUR_MACHINE_ID \
    -webhooks-port 3001 \
    -log-level info
```

### Interactive Mode (Testing)

```bash
# Run interactively
docker run -it --rm \
  --device /dev/input/event6:/dev/input/event6 \
  --network host \
  streamerbrainz:latest \
  streamerbrainz -ir-device /dev/input/event6 -log-level debug

# Run shell for debugging
docker run -it --rm \
  streamerbrainz:latest \
  /bin/sh
```

### Using sbctl from Container

```bash
# Control daemon running in container
docker exec streamerbrainz sbctl mute
docker exec streamerbrainz sbctl set-volume -30.0
docker exec streamerbrainz sbctl volume-up

# Or access via IPC socket on host
docker run --rm \
  -v /tmp:/tmp \
  streamerbrainz:latest \
  sbctl -ipc-socket /tmp/streamerbrainz.sock mute
```

---

## Docker Compose

### Basic Configuration

Edit `docker-compose.yml` to customize settings:

```yaml
services:
  streamerbrainz:
    image: streamerbrainz:latest
    container_name: streamerbrainz
    restart: unless-stopped
    user: root
    network_mode: host
    devices:
      - /dev/input/event6:/dev/input/event6
    volumes:
      - /tmp:/tmp
    command:
      - streamerbrainz
      - -ir-device
      - /dev/input/event6
      - -camilladsp-ws-url
      - ws://127.0.0.1:1234
      - -log-level
      - info
```

### Common Commands

```bash
# Start services
docker-compose up -d

# View logs
docker-compose logs -f streamerbrainz

# View logs (last 100 lines)
docker-compose logs --tail=100 streamerbrainz

# Stop services
docker-compose down

# Restart services
docker-compose restart

# Update and restart
docker-compose pull
docker-compose up -d

# Run with development profile
docker-compose --profile dev up -d

# Execute commands in running container
docker-compose exec streamerbrainz sbctl mute
docker-compose exec streamerbrainz streamerbrainz -version
```

### With CamillaDSP (Testing)

```bash
# Start both StreamerBrainz and CamillaDSP
docker-compose --profile dev up -d

# Check both are running
docker-compose ps

# View logs from both
docker-compose logs -f
```

---

## Multi-Architecture Builds

### Setup Multi-Platform Builder

```bash
# Create builder instance
docker buildx create --name multiarch --driver docker-container --use

# Inspect and bootstrap
docker buildx inspect --bootstrap

# Verify supported platforms
docker buildx inspect multiarch | grep Platforms
# Should show: linux/amd64, linux/arm64, linux/arm/v7, etc.
```

### Build for Multiple Platforms

```bash
# Build and push to registry (required for multi-platform)
docker buildx build \
  --platform linux/amd64,linux/arm64 \
  -t ghcr.io/username/streamerbrainz:latest \
  --push \
  .

# Build for specific platforms
docker buildx build \
  --platform linux/arm64 \
  -t streamerbrainz:rpi4 \
  --load \
  .
```

### Using the Build Script

```bash
# Build for all platforms
./docker-build.sh --all

# Build and push
./docker-build.sh --all --push --registry ghcr.io/username

# Build for Raspberry Pi 4 only
./docker-build.sh --arm64 --load
```

### Platform-Specific Tags

```bash
# Build with architecture-specific tags
docker buildx build \
  --platform linux/amd64 \
  -t streamerbrainz:amd64 \
  --load \
  .

docker buildx build \
  --platform linux/arm64 \
  -t streamerbrainz:arm64 \
  --load \
  .
```

---

## Configuration

### Environment Variables

```bash
docker run -d \
  --name streamerbrainz \
  --device /dev/input/event6:/dev/input/event6 \
  --network host \
  -e TZ=America/New_York \
  -e LOG_LEVEL=debug \
  streamerbrainz:latest \
  streamerbrainz -ir-device /dev/input/event6
```

### Volume Mounts

```bash
# IPC socket (for sbctl access from host)
-v /tmp:/tmp

# Plex token file
-v /path/to/plex-token:/run/secrets/plex-token:ro

# Custom configuration (future)
-v /path/to/config:/config:ro
```

### Network Modes

```bash
# Host network (recommended for CamillaDSP access)
docker run --network host ...

# Bridge network (if CamillaDSP is also in container)
docker run --network streamerbrainz-net ...

# With port forwarding (for webhooks only)
docker run -p 3001:3001 ...
```

### Device Access

```bash
# IR input device (required)
--device /dev/input/event6:/dev/input/event6

# Multiple input devices
--device /dev/input/event6:/dev/input/event6 \
--device /dev/input/event7:/dev/input/event7

# All input devices (not recommended for security)
--device /dev/input:/dev/input
```

### User and Permissions

```bash
# Run as root (required for input device access)
docker run --user root ...

# Or add user to input group on host
sudo usermod -a -G input $(whoami)

# Then run as non-root (may still have issues)
docker run --user 1000:1000 \
  --group-add $(getent group input | cut -d: -f3) \
  ...
```

---

## Troubleshooting

### Docker Build Issues

#### Problem: `buildx: command not found`
```bash
# Solution: Update Docker to 19.03+ or install buildx
docker version
# Update Docker or install buildx plugin
```

#### Problem: Multi-platform build fails
```bash
# Solution: Create and use multiarch builder
docker buildx create --name multiarch --use
docker buildx inspect --bootstrap
```

#### Problem: Build is slow
```bash
# Check .dockerignore is present
cat .dockerignore

# Use build cache
docker buildx build --cache-from type=local,src=/tmp/buildcache ...

# Build single platform
./docker-build.sh --amd64 --load
```

### Container Runtime Issues

#### Problem: Cannot access IR input device
```bash
# Check device exists on host
ls -l /dev/input/event6

# Check permissions
sudo chmod 666 /dev/input/event6

# Run container as root
docker run --user root ...

# Verify device is mounted
docker exec streamerbrainz ls -l /dev/input/event6
```

#### Problem: Cannot connect to CamillaDSP
```bash
# Check CamillaDSP is running and accessible
curl http://127.0.0.1:1234/api/v1/version

# Use host network mode
docker run --network host ...

# Check WebSocket URL
docker run ... streamerbrainz -camilladsp-ws-url ws://127.0.0.1:1234 -log-level debug
```

#### Problem: IPC socket not accessible from host
```bash
# Mount /tmp volume
docker run -v /tmp:/tmp ...

# Check socket exists
docker exec streamerbrainz ls -l /tmp/streamerbrainz.sock

# Test from host
echo '{"type":"toggle_mute"}' | nc -U /tmp/streamerbrainz.sock
```

#### Problem: Container exits immediately
```bash
# Check logs
docker logs streamerbrainz

# Run interactively to see errors
docker run -it --rm \
  --device /dev/input/event6:/dev/input/event6 \
  streamerbrainz:latest \
  streamerbrainz -ir-device /dev/input/event6 -log-level debug

# Verify command is correct
docker run --rm streamerbrainz:latest streamerbrainz -help
```

### Docker Compose Issues

#### Problem: Compose file validation error
```bash
# Validate compose file
docker-compose config

# Check version compatibility
docker-compose version
```

#### Problem: Services won't start
```bash
# Check service status
docker-compose ps

# View logs
docker-compose logs streamerbrainz

# Start in foreground to see errors
docker-compose up
```

### Performance Issues

#### Problem: High CPU usage
```bash
# Check update frequency
docker exec streamerbrainz streamerbrainz -help | grep update-hz

# Reduce update rate
docker run ... streamerbrainz -camilladsp-update-hz 10 ...
```

#### Problem: High memory usage
```bash
# Check container stats
docker stats streamerbrainz

# Set memory limits
docker run --memory=128m --memory-swap=256m ...
```

---

## Best Practices

### Production Deployment

1. **Use specific version tags**
   ```bash
   docker pull streamerbrainz:v1.0.0  # Not :latest
   ```

2. **Set resource limits**
   ```bash
   docker run --cpus=0.5 --memory=128m ...
   ```

3. **Configure restart policy**
   ```bash
   docker run --restart=unless-stopped ...
   ```

4. **Use read-only filesystems where possible**
   ```bash
   docker run --read-only -v /tmp:/tmp ...
   ```

5. **Store secrets securely**
   ```bash
   docker secret create plex_token /path/to/token
   docker run --secret plex_token ...
   ```

6. **Monitor logs**
   ```bash
   docker-compose logs -f --tail=100
   ```

### Security

1. **Run with minimal privileges**
   ```bash
   # Only add required capabilities
   --cap-drop=ALL --cap-add=DAC_READ_SEARCH
   ```

2. **Use specific device mounts**
   ```bash
   --device /dev/input/event6  # Not /dev/input
   ```

3. **Don't expose unnecessary ports**
   ```bash
   # Use host network only if needed
   # Otherwise use bridge with specific ports
   ```

4. **Keep images updated**
   ```bash
   docker pull streamerbrainz:latest
   docker-compose up -d
   ```

---

## Advanced Usage

### Multi-Stage Development

```bash
# Development build with debug symbols
docker build --target builder -t streamerbrainz:debug .

# Run tests in container
docker run --rm streamerbrainz:debug go test ./...
```

### Custom Build Arguments

```bash
# Build with custom Go version
docker build --build-arg GO_VERSION=1.23 .

# Build with custom base image
docker build --build-arg BASE_IMAGE=debian:bookworm-slim .
```

### Debugging

```bash
# Run with shell for debugging
docker run -it --rm \
  --device /dev/input/event6:/dev/input/event6 \
  --network host \
  --entrypoint /bin/sh \
  streamerbrainz:latest

# Then inside container
/usr/local/bin/streamerbrainz -version
ls -l /dev/input/
```

### Health Checks

```bash
# Custom health check
docker run -d \
  --health-cmd='sbctl -ipc-socket /tmp/streamerbrainz.sock set-volume -30.0' \
  --health-interval=30s \
  --health-timeout=5s \
  --health-retries=3 \
  streamerbrainz:latest \
  streamerbrainz -ir-device /dev/input/event6
```

---

## See Also

- [Main README](../README.md) - General documentation
- [Architecture Documentation](ARCHITECTURE.md) - System design
- [Dockerfile](../Dockerfile) - Image definition
- [docker-compose.yml](../docker-compose.yml) - Compose configuration
- [docker-build.sh](../docker-build.sh) - Build script