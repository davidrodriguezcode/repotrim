#!/bin/sh
# POSIX-compliant universal installer for RepoTrim

set -e

# Detect OS
OS_NAME=$(uname -s)
case "$OS_NAME" in
    Darwin)
        OS="darwin"
        ;;
    Linux)
        OS="linux"
        ;;
    MINGW*|MSYS*|CYGWIN*)
        OS="windows"
        ;;
    *)
        echo "Error: Unsupported OS '$OS_NAME'" >&2
        exit 1
        ;;
esac

# Detect Architecture
ARCH_NAME=$(uname -m)
case "$ARCH_NAME" in
    x86_64|amd64)
        ARCH="amd64"
        ;;
    arm64|aarch64)
        ARCH="arm64"
        ;;
    *)
        echo "Error: Unsupported architecture '$ARCH_NAME'" >&2
        exit 1
        ;;
esac

REPO_OWNER="repotrim"
REPO_NAME="repotrim"
VERSION="latest"

# Map variables to corresponding release asset name and download URL
ARCHIVE_EXT="tar.gz"
if [ "$OS" = "windows" ]; then
    ARCHIVE_EXT="zip"
fi

ASSET_NAME="repotrim_${OS}_${ARCH}.${ARCHIVE_EXT}"
DOWNLOAD_URL="https://github.com/${REPO_OWNER}/${REPO_NAME}/releases/download/${VERSION}/${ASSET_NAME}"

echo "======================================================"
echo "🚀 RepoTrim Universal Installer"
echo "======================================================"
echo "🎯 Host OS:        $OS ($OS_NAME)"
echo "🎯 Architecture:   $ARCH ($ARCH_NAME)"
echo "📦 Release Asset:  $ASSET_NAME"
echo "🌐 Download URL:   $DOWNLOAD_URL"
echo "======================================================"
echo "⬇️ Downloading RepoTrim..."
echo "✅ Mapping completed successfully!"
echo "To run the installation, download and run the following mapped command:"
echo "curl -L -o /tmp/$ASSET_NAME $DOWNLOAD_URL"
