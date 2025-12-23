#!/bin/bash
# build-binaries.sh - Build and extract binaries for multiple architectures
#
# This script uses Docker to cross-compile the StreamerBrainz daemon binary for different
# platforms and extracts it to the bin/ directory, organized by architecture.
#
# Usage:
#   ./build-binaries.sh                    # Build for all architectures
#   ./build-binaries.sh --amd64            # Build for amd64 only
#   ./build-binaries.sh --arm64            # Build for arm64 only (Raspberry Pi 4+)
#   ./build-binaries.sh --armv7            # Build for armv7 (Raspberry Pi 3)
#   ./build-binaries.sh --clean            # Clean all built binaries
#   ./build-binaries.sh --help             # Show help

set -e

# Configuration
OUTPUT_DIR="./bin"
DOCKER_FILE="Dockerfile.builder"
IMAGE_PREFIX="streamerbrainz-builder"
VERSION="${VERSION:-1.0.0}"

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

print_header() {
    echo ""
    echo -e "${BLUE}========================================${NC}"
    echo -e "${BLUE}$1${NC}"
    echo -e "${BLUE}========================================${NC}"
    echo ""
}

show_help() {
    cat <<EOF
StreamerBrainz Binary Builder

DESCRIPTION:
    Build standalone binaries for multiple architectures using Docker.
    Binaries are extracted to bin/<arch>/ and can be copied to target systems.

USAGE:
    $0 [OPTIONS]

OPTIONS:
    --all           Build for all supported architectures (default)
    --amd64         Build for amd64 (x86_64) only
    --arm64         Build for arm64 (Raspberry Pi 4+) only
    --armv7         Build for armv7 (Raspberry Pi 3) only
    --armv6         Build for armv6 (Raspberry Pi 1/Zero) only
    --no-upx        Build without UPX compression
    --verify        Verify binaries after building
    --clean         Clean all built binaries
    --help          Show this help message

OUTPUT:
    Binaries are organized in bin/ directory:

    bin/
    ├── amd64/
    │   └── streamerbrainz
    ├── arm64/
    │   └── streamerbrainz
    └── armv7/
        └── streamerbrainz

EXAMPLES:
    # Build for all architectures
    $0

    # Build only for Raspberry Pi 4
    $0 --arm64

    # Build for x86_64 without compression
    $0 --amd64 --no-upx

    # Build and verify all binaries
    $0 --all --verify

    # Clean built binaries
    $0 --clean

REQUIREMENTS:
    - Docker with buildx support
    - Sufficient disk space for build cache

DEPLOYING BINARIES:
    After building, copy binaries to target system:

    # For Raspberry Pi 4+
    scp bin/arm64/* pi@raspberrypi:/usr/local/bin/

    # For x86_64 server
    scp bin/amd64/* user@server:/usr/local/bin/

EOF
}

# Check if Docker is available
check_docker() {
    if ! command -v docker &> /dev/null; then
        print_error "Docker is not installed or not in PATH"
        exit 1
    fi
}

# Build binaries for a specific platform
build_platform() {
    local platform="$1"
    local arch="$2"
    local variant="${3:-}"

    local image_tag="${IMAGE_PREFIX}:${arch}"
    local output_path="${OUTPUT_DIR}/${arch}"

    print_header "Building for ${platform}"

    # Create output directory
    mkdir -p "${output_path}"

    print_info "Platform: ${platform}"
    print_info "Output: ${output_path}"

    # Build the Docker image with buildx
    print_info "Building Docker image..."
    docker buildx build \
        --platform "${platform}" \
        --target binaries \
        -f "${DOCKER_FILE}" \
        -t "${image_tag}" \
        --load \
        . || {
            print_error "Build failed for ${platform}"
            return 1
        }

    print_success "Docker build completed"

    # Extract binaries from the image
    print_info "Extracting binaries..."

    # Create a temporary container
    local container_id=$(docker create "${image_tag}")

    # Copy artifacts from container
    docker cp "${container_id}:/artifacts/." "${output_path}/" || {
        print_error "Failed to extract binaries"
        docker rm "${container_id}" > /dev/null 2>&1
        return 1
    }

    # Remove temporary container
    docker rm "${container_id}" > /dev/null 2>&1

    print_success "Binaries extracted to ${output_path}/"

    # Make binaries executable
    chmod +x "${output_path}"/*

    # Show file information
    echo ""
    print_info "Built binaries:"
    ls -lh "${output_path}/"

    # Show file type
    if command -v file &> /dev/null; then
        echo ""
        print_info "Binary information:"
        file "${output_path}"/*
    fi

    echo ""
    print_success "Build completed for ${arch}"
    echo ""
}

# Verify binaries using Docker
verify_binaries() {
    local arch="$1"
    local platform="$2"

    print_info "Verifying ${arch} binaries..."

    local image_tag="${IMAGE_PREFIX}:${arch}"

    # Run verification stage
    docker build \
        --platform "${platform}" \
        --target verify \
        -f "${DOCKER_FILE}" \
        -t "${image_tag}-verify" \
        . || {
            print_error "Verification failed for ${arch}"
            return 1
        }

    print_success "Verification passed for ${arch}"
}

# Clean built binaries
clean_binaries() {
    print_header "Cleaning Built Binaries"

    if [ -d "${OUTPUT_DIR}" ]; then
        print_info "Removing ${OUTPUT_DIR}/*"
        rm -rf "${OUTPUT_DIR}"/amd64 "${OUTPUT_DIR}"/arm64 "${OUTPUT_DIR}"/armv7 "${OUTPUT_DIR}"/armv6
        print_success "Binaries cleaned"
    else
        print_info "No binaries to clean"
    fi

    # Clean Docker images
    print_info "Cleaning Docker images..."
    docker images "${IMAGE_PREFIX}:*" -q | xargs -r docker rmi 2>/dev/null || true
    print_success "Docker images cleaned"
}

# Main script
main() {
    local build_all=false
    local build_amd64=false
    local build_arm64=false
    local build_armv7=false
    local build_armv6=false
    local do_verify=false
    local do_clean=false

    # Parse arguments
    if [ $# -eq 0 ]; then
        build_all=true
    fi

    while [[ $# -gt 0 ]]; do
        case $1 in
            --all)
                build_all=true
                shift
                ;;
            --amd64)
                build_amd64=true
                shift
                ;;
            --arm64)
                build_arm64=true
                shift
                ;;
            --armv7)
                build_armv7=true
                shift
                ;;
            --armv6)
                build_armv6=true
                shift
                ;;
            --verify)
                do_verify=true
                shift
                ;;
            --clean)
                do_clean=true
                shift
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

    # Handle clean
    if [ "$do_clean" = true ]; then
        clean_binaries
        exit 0
    fi

    # Check requirements
    check_docker

    # Set build targets
    if [ "$build_all" = true ]; then
        build_amd64=true
        build_arm64=true
        build_armv7=true
    fi

    print_header "StreamerBrainz Binary Builder"
    print_info "Version: ${VERSION}"
    print_info "Output directory: ${OUTPUT_DIR}"
    echo ""

    # Build for each requested platform
    local build_count=0

    if [ "$build_amd64" = true ]; then
        build_platform "linux/amd64" "amd64"
        ((build_count++))

        if [ "$do_verify" = true ]; then
            verify_binaries "amd64" "linux/amd64"
        fi
    fi

    if [ "$build_arm64" = true ]; then
        build_platform "linux/arm64" "arm64"
        ((build_count++))

        if [ "$do_verify" = true ]; then
            verify_binaries "arm64" "linux/arm64"
        fi
    fi

    if [ "$build_armv7" = true ]; then
        build_platform "linux/arm/v7" "armv7"
        ((build_count++))

        if [ "$do_verify" = true ]; then
            verify_binaries "armv7" "linux/arm/v7"
        fi
    fi

    if [ "$build_armv6" = true ]; then
        build_platform "linux/arm/v6" "armv6"
        ((build_count++))

        if [ "$do_verify" = true ]; then
            verify_binaries "armv6" "linux/arm/v6"
        fi
    fi

    # Summary
    print_header "Build Summary"
    print_success "Built binaries for ${build_count} platform(s)"
    echo ""
    print_info "Binary locations:"

    for arch_dir in "${OUTPUT_DIR}"/*; do
        if [ -d "${arch_dir}" ]; then
            arch=$(basename "${arch_dir}")
            echo ""
            echo -e "${GREEN}${arch}:${NC}"
            ls -lh "${arch_dir}/"
        fi
    done

    echo ""
    print_info "Next steps:"
    echo "  1. Test binaries locally (if compatible with your system)"
    echo "  2. Copy to target systems:"
    echo "     scp bin/arm64/* pi@raspberrypi:/usr/local/bin/"
    echo "     scp bin/amd64/* user@server:/usr/local/bin/"
    echo "  3. On target system, make binaries executable:"
    echo "     chmod +x /usr/local/bin/streamerbrainz"
    echo ""

    print_success "All builds completed successfully!"
}

# Run main function
main "$@"
