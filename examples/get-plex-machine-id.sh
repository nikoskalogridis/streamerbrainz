#!/bin/bash
# Helper script to discover Plex player machine identifiers
# This queries the Plex API /status/sessions endpoint and displays
# all active players with their machine identifiers.

set -e

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
CYAN='\033[0;36m'
NC='\033[0m' # No Color

log_info() {
    echo -e "${GREEN}[INFO]${NC} $1"
}

log_warn() {
    echo -e "${YELLOW}[WARN]${NC} $1"
}

log_error() {
    echo -e "${RED}[ERROR]${NC} $1"
}

# Show usage
if [ "$1" = "-h" ] || [ "$1" = "--help" ]; then
    echo "Usage: $0 <plex-host:port> <plex-token>"
    echo ""
    echo "Discover Plex player machine identifiers from active sessions"
    echo ""
    echo "Arguments:"
    echo "  plex-host:port    Plex server host and port (e.g., plex.home.arpa:32400)"
    echo "  plex-token        Your Plex authentication token"
    echo ""
    echo "Examples:"
    echo "  $0 plex.home.arpa:32400 YOUR_TOKEN_HERE"
    echo "  $0 192.168.1.100:32400 abcd1234efgh5678"
    echo ""
    echo "To find your Plex token:"
    echo "  https://support.plex.tv/articles/204059436-finding-an-authentication-token-x-plex-token/"
    echo ""
    exit 0
fi

# Validate arguments
if [ $# -ne 2 ]; then
    log_error "Missing arguments"
    echo "Usage: $0 <plex-host:port> <plex-token>"
    echo "Run '$0 -h' for help"
    exit 1
fi

PLEX_HOST="$1"
PLEX_TOKEN="$2"

# Build URL
URL="http://${PLEX_HOST}/status/sessions?X-Plex-Token=${PLEX_TOKEN}"

echo "============================================"
echo "  Plex Machine Identifier Discovery"
echo "============================================"
echo ""
log_info "Querying Plex server: ${PLEX_HOST}"
echo ""

# Fetch sessions
response=$(curl -s "$URL")

if [ $? -ne 0 ]; then
    log_error "Failed to connect to Plex server"
    log_error "Check that the host and port are correct"
    exit 1
fi

# Check for authentication error
if echo "$response" | grep -q "unauthorized"; then
    log_error "Authentication failed - invalid Plex token"
    exit 1
fi

# Parse XML and extract player information
# This is a simple grep-based parser - works for basic cases
echo "$response" | grep -q "<MediaContainer" || {
    log_error "Unexpected response from Plex server"
    exit 1
}

# Get session count
size=$(echo "$response" | grep -oP 'size="\K[^"]+' | head -1)

if [ "$size" = "0" ] || [ -z "$size" ]; then
    log_warn "No active sessions found"
    echo ""
    echo "Start playing something in Plexamp/Plex and run this script again."
    echo ""
    exit 0
fi

log_info "Found ${size} active session(s)"
echo ""
echo "─────────────────────────────────────────────"

# Extract and display player information
count=0
while IFS= read -r line; do
    if echo "$line" | grep -q "<Player "; then
        count=$((count + 1))

        # Extract player attributes
        machine_id=$(echo "$line" | grep -oP 'machineIdentifier="\K[^"]+')
        title=$(echo "$line" | grep -oP 'title="\K[^"]+')
        product=$(echo "$line" | grep -oP 'product="\K[^"]+')
        platform=$(echo "$line" | grep -oP 'platform="\K[^"]+')
        state=$(echo "$line" | grep -oP 'state="\K[^"]+')

        echo -e "${CYAN}Player #${count}${NC}"
        echo "  Title:              ${title}"
        echo "  Product:            ${product}"
        echo "  Platform:           ${platform}"
        echo "  State:              ${state}"
        echo -e "  ${BLUE}Machine ID:         ${machine_id}${NC}"

        # Get track info if available
        track_title=""
        artist=""

        # Look for Track element before this Player
        track_line=$(echo "$response" | grep -B 20 "machineIdentifier=\"${machine_id}\"" | grep "<Track " | tail -1)
        if [ -n "$track_line" ]; then
            track_title=$(echo "$track_line" | grep -oP 'title="\K[^"]+' || echo "")
            artist=$(echo "$track_line" | grep -oP 'grandparentTitle="\K[^"]+' || echo "")

            if [ -n "$track_title" ]; then
                echo "  Now Playing:        ${artist} - ${track_title}"
            fi
        fi

        echo "─────────────────────────────────────────────"
    fi
done <<< "$response"

echo ""
log_info "To use with StreamerBrainz:"
echo ""
echo -e "${YELLOW}streamerbrainz \\"
echo "  -plex-server-url http://${PLEX_HOST} \\"
echo "  -plex-token-file /path/to/plex-token \\"
echo -e "  -plex-machine-id YOUR_MACHINE_ID${NC}"
echo ""
