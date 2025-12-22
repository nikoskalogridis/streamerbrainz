#!/bin/bash
# docker-build.sh - Multi-architecture Docker build helper script
#
# This script simplifies building StreamerBrainz Docker images for multiple architectures.
# It uses Docker Buildx for cross-platform compilation.
#
# Usage:
#   ./docker-build.sh                    # Build for current architecture
#   ./docker-build.sh --all              # Build for all architectures
#   ./docker-build.sh --amd64            # Build for amd64 only
#   ./docker-build.sh --arm64            # Build for arm64 only (Raspberry Pi 4+)
#   ./docker-build.sh --push             # Build and push to registry
#   ./docker-build.sh --help             # Show help

set -e

# Configuration
IMAGE_NAME="${IMAGE_NAME:-streamerbrainz}"
VERSION="${VERSION:-latest}"
REGISTRY="${REGISTRY:-}"  # Set to your registry, e.g., "ghcr.io/username"

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# Helper functions
print_info() {
    echo -e "${BLUE}[INFO]${NC} $1"
}

print_success() {
    echo -e "${GREEN}[SUCCESS]${NC} $1"
}

print_warn() {
    echo -e "${YELLOW}[WARN]${NC} $1"
}

print_error() {
    echo -e "${RED}[ERROR]${NC} $1"
}

show_help() {
    cat <<EOF
StreamerBrainz Docker Build Script

USAGE:
    $0 [OPTIONS]

OPTIONS:
    --all           Build for all supported architectures (amd64, arm64)
    --amd64         Build for amd64 (x86_64) only
    --arm64         Build for arm64 (Raspberry Pi 4+) only
    --push          Push images to registry after building
    --load          Load image into local Docker (single platform only)
    --tag TAG       Use custom tag (default: latest)
    --registry REG  Use custom registry (e.g., ghcr.io/username)
    --help          Show this help message

EXAMPLES:
    # Build for current architecture and load into Docker
    $0 --load

    # Build for Raspberry Pi 4
    $0 --arm64 --tag rpi4

    # Build for all architectures and push to registry
    $0 --all --push --registry ghcr.io/myuser

    # Build specific version
    VERSION=1.0.0 $0 --all --push

ENVIRONMENT VARIABLES:
    IMAGE_NAME      Docker image name (default: streamerbrainz)
    VERSION         Image version tag (default: latest)
    REGISTRY        Docker registry URL

REQUIREMENTS:
    - Docker with buildx support
    - For multi-arch builds: docker buildx builder with multi-platform support

SETUP BUILDX (if needed):
    docker buildx create --name multiarch --use
    docker buildx inspect --bootstrap

EOF
}

# Check if Docker is available
check_docker() {
    if ! command -v docker &> /dev/null; then
        print_error "Docker is not installed or not in PATH"
        exit 1
    fi
}

# Check if buildx is available
check_buildx() {
    if ! docker buildx version &> /dev/null; then
        print_error "Docker buildx is not available"
        print_info "Please update Docker to a version that supports buildx"
        exit 1
    fi
}

# Setup buildx builder for multi-arch
setup_buildx() {
    print_info "Checking buildx builder..."

    if ! docker buildx inspect multiarch &> /dev/null; then
        print_info "Creating buildx builder 'multiarch'..."
        docker buildx create --name multiarch --use
        docker buildx inspect --bootstrap
        print_success "Buildx builder created"
    else
        print_info "Using existing buildx builder 'multiarch'"
        docker buildx use multiarch
    fi
}

# Build Docker image
build_image() {
    local platforms="$1"
    local push_flag="$2"
    local load_flag="$3"

    local full_image_name="${IMAGE_NAME}:${VERSION}"
    if [ -n "$REGISTRY" ]; then
        full_image_name="${REGISTRY}/${full_image_name}"
    fi

    print_info "Building Docker image: ${full_image_name}"
    print_info "Platforms: ${platforms}"

    local build_args=(
        "buildx" "build"
        "--platform" "${platforms}"
        "-t" "${full_image_name}"
    )

    # Add push or load flag
    if [ "$push_flag" = "true" ]; then
        build_args+=("--push")
        print_info "Images will be pushed to registry"
    elif [ "$load_flag" = "true" ]; then
        build_args+=("--load")
        print_info "Image will be loaded into local Docker"
    else
        print_warn "Image will be built but not pushed or loaded"
        print_info "Use --push to push to registry or --load to load locally"
    fi

    # Add context
    build_args+=(".")

    print_info "Running: docker ${build_args[*]}"
    docker "${build_args[@]}"

    print_success "Build completed successfully!"
    print_info "Image: ${full_image_name}"
}

# Main script
main() {
    local platforms=""
    local push_flag="false"
    local load_flag="false"
    local custom_tag=""

    # Parse arguments
    while [[ $# -gt 0 ]]; do
        case $1 in
            --all)
                platforms="linux/amd64,linux/arm64"
                shift
                ;;
            --amd64)
                platforms="linux/amd64"
                shift
                ;;
            --arm64)
                platforms="linux/arm64"
                shift
                ;;
            --push)
                push_flag="true"
                shift
                ;;
            --load)
                load_flag="true"
                shift
                ;;
            --tag)
                custom_tag="$2"
                shift 2
                ;;
            --registry)
                REGISTRY="$2"
                shift 2
                ;;
            --help|-h)
                show_help
                exit 0
                ;;
            *)
                print_error "Unknown option: $1"
                echo ""
                show_help
                exit 1
                ;;
        esac
    done

    # Override version if custom tag provided
    if [ -n "$custom_tag" ]; then
        VERSION="$custom_tag"
    fi

    # Validate options
    if [ "$push_flag" = "true" ] && [ "$load_flag" = "true" ]; then
        print_error "Cannot use --push and --load together"
        exit 1
    fi

    if [ "$load_flag" = "true" ] && [[ "$platforms" == *","* ]]; then
        print_error "Cannot use --load with multiple platforms"
        print_info "Use --load with a single platform (--amd64 or --arm64)"
        exit 1
    fi

    # Default to current architecture if none specified
    if [ -z "$platforms" ]; then
        platforms="linux/amd64"
        print_info "No platform specified, defaulting to linux/amd64"
    fi

    # Check requirements
    check_docker
    check_buildx

    # Setup buildx for multi-arch builds
    if [[ "$platforms" == *","* ]]; then
        setup_buildx
    fi

    # Build the image
    build_image "$platforms" "$push_flag" "$load_flag"

    # Show usage instructions
    echo ""
    print_success "Build complete!"
    echo ""
    print_info "Next steps:"

    if [ "$push_flag" = "true" ]; then
        echo "  Pull on target system: docker pull ${REGISTRY}${REGISTRY:+/}${IMAGE_NAME}:${VERSION}"
    elif [ "$load_flag" = "true" ]; then
        echo "  Run locally: docker run --rm ${IMAGE_NAME}:${VERSION} streamerbrainz -version"
    else
        echo "  Rebuild with --load to load into Docker"
        echo "  Rebuild with --push to push to registry"
    fi

    echo ""
    print_info "Run container with device access:"
    echo "  docker run --rm \\"
    echo "    --device /dev/input/event6:/dev/input/event6 \\"
    echo "    --network host \\"
    echo "    ${REGISTRY}${REGISTRY:+/}${IMAGE_NAME}:${VERSION} \\"
    echo "    streamerbrainz -ir-device /dev/input/event6"
}

# Run main function
main "$@"
