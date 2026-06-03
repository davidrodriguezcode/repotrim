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

REPO_OWNER="davidrodriguezcode"
REPO_NAME="repotrim"
VERSION="latest"

# Map variables to corresponding release asset name and download URL
ARCHIVE_EXT="tar.gz"
if [ "$OS" = "windows" ]; then
    ARCHIVE_EXT="zip"
fi

ASSET_NAME="repotrim_${OS}_${ARCH}.${ARCHIVE_EXT}"
# Note: For latest release, GitHub maps to redirect URL.
DOWNLOAD_URL="https://github.com/${REPO_OWNER}/${REPO_NAME}/releases/download/${VERSION}/${ASSET_NAME}"

# Temporary directory setup
TEMP_DIR=$(mktemp -d)
trap 'rm -rf "$TEMP_DIR"' EXIT

echo "======================================================"
echo "🚀 RepoTrim Universal Installer"
echo "======================================================"
echo "🎯 Host OS:        $OS ($OS_NAME)"
echo "🎯 Architecture:   $ARCH ($ARCH_NAME)"
echo "📦 Release Asset:  $ASSET_NAME"
echo "🌐 Download URL:   $DOWNLOAD_URL"
echo "======================================================"
echo "⬇️ Downloading RepoTrim..."

# Determine download tool
if command -v curl >/dev/null 2>&1; then
    curl -L -f -o "$TEMP_DIR/$ASSET_NAME" "$DOWNLOAD_URL"
elif command -v wget >/dev/null 2>&1; then
    wget -q -O "$TEMP_DIR/$ASSET_NAME" "$DOWNLOAD_URL"
else
    echo "Error: Neither curl nor wget was found. Please install one of them." >&2
    exit 1
fi

echo "📦 Extracting archive..."
if [ "$ARCHIVE_EXT" = "tar.gz" ]; then
    tar -xzf "$TEMP_DIR/$ASSET_NAME" -C "$TEMP_DIR"
else
    unzip -q "$TEMP_DIR/$ASSET_NAME" -d "$TEMP_DIR"
fi

# Ensure binary is executable
chmod +x "$TEMP_DIR/repotrim"

# Determine installation directory
INSTALL_DIR="/usr/local/bin"
if [ ! -d "$INSTALL_DIR" ]; then
    # Try to create it, or fall back to home directory
    if mkdir -p "$INSTALL_DIR" 2>/dev/null; then
        echo "Created install directory: $INSTALL_DIR"
    else
        INSTALL_DIR="$HOME/.local/bin"
        mkdir -p "$INSTALL_DIR"
    fi
fi

echo "🚚 Installing repotrim to $INSTALL_DIR/repotrim..."
if [ -w "$INSTALL_DIR" ]; then
    cp "$TEMP_DIR/repotrim" "$INSTALL_DIR/repotrim"
else
    echo "Permission denied for $INSTALL_DIR. Requesting sudo access..."
    sudo cp "$TEMP_DIR/repotrim" "$INSTALL_DIR/repotrim"
fi

echo "======================================================"
echo "🎉 RepoTrim installed successfully!"
echo "📍 Location: $INSTALL_DIR/repotrim"
echo "======================================================"
echo "Test the installation by running:"
echo "  repotrim -dir ."
echo "======================================================"
