#!/bin/sh
set -euo pipefail

REPO="Veitangie/sinq"
INSTALL_DIR="/usr/local/bin"
TARGET_VERSION=$1

OS=$(uname -s)
ARCH=$(uname -m)

case "$OS" in
    Linux*)     OS_NAME="linux"; EXT=".tar.gz" ;;
    Darwin*)    OS_NAME="macOS"; EXT=".tar.gz" ;;
    CYGWIN*|MINGW*|MSYS*) OS_NAME="windows"; EXT=".zip"; EXE_EXT=".exe" ;;
    *)          echo "Unsupported OS: $OS"; exit 1 ;;
esac

case "$ARCH" in
    x86_64|amd64) ARCH_NAME="x86_64" ;;
    aarch64|arm64) ARCH_NAME="arm64" ;;
    *) echo "Unsupported architecture: $ARCH"; exit 1 ;;
esac

BINARY_NAME="sinq${EXE_EXT:-}"
echo "Detected OS: $OS_NAME, Architecture: $ARCH_NAME"

if [ "$EXT" = ".tar.gz" ] && ! command -v tar >/dev/null 2>&1; then
    echo "Error: 'tar' is required to extract the archive, but it is not installed."
    exit 1
fi
if [ "$EXT" = ".zip" ] && ! command -v unzip >/dev/null 2>&1; then
    echo "Error: 'unzip' is required to extract the archive, but it is not installed."
    exit 1
fi
if [ -z "$TARGET_VERSION" ]; then
    echo "Fetching latest release version..."
    TARGET_VERSION=$(curl -s "https://api.github.com/repos/$REPO/releases/latest" | grep '"tag_name":' | sed -E 's/.*"([^"]+)".*/\1/')

    if [ -z "$TARGET_VERSION" ]; then
        echo "Error: Failed to fetch the latest release tag from GitHub."
        exit 1
    fi
    echo "Latest version is $TARGET_VERSION"
else
    echo "Target version specified: $TARGET_VERSION"
fi

RAW_VERSION=$(echo "$TARGET_VERSION" | sed 's/^v//')
ARCHIVE_NAME="sinq-${RAW_VERSION}-${OS_NAME}-${ARCH_NAME}${EXT}"
DOWNLOAD_URL="https://github.com/$REPO/releases/download/$TARGET_VERSION/$ARCHIVE_NAME"
CHECKSUMS_URL="https://github.com/$REPO/releases/download/$TARGET_VERSION/sinq_${RAW_VERSION}_checksums.txt"

TMP_DIR=$(mktemp -d)
trap 'rm -rf "$TMP_DIR"' EXIT

TMP_ARCHIVE="$TMP_DIR/$ARCHIVE_NAME"
TMP_CHECKSUMS="$TMP_DIR/checksums.txt"

echo "Downloading $DOWNLOAD_URL..."
if ! curl -sL "$DOWNLOAD_URL" -o "$TMP_ARCHIVE"; then
    echo "Error: Failed to download archive."
    exit 1
fi

echo "Downloading checksums..."
if ! curl -sL "$CHECKSUMS_URL" -o "$TMP_CHECKSUMS"; then
    echo "Error: Failed to download checksums file."
    exit 1
fi

echo "Verifying checksum..."
EXPECTED_CHECKSUM=$(grep "$ARCHIVE_NAME" "$TMP_CHECKSUMS" | awk '{print $1}' || true)
if [ -z "$EXPECTED_CHECKSUM" ]; then
    echo "Error: Checksum for $ARCHIVE_NAME not found in checksums.txt"
    exit 1
fi

if command -v sha256sum >/dev/null 2>&1; then
    ACTUAL_CHECKSUM=$(sha256sum "$TMP_ARCHIVE" | awk '{print $1}')
elif command -v shasum >/dev/null 2>&1; then
    ACTUAL_CHECKSUM=$(shasum -a 256 "$TMP_ARCHIVE" | awk '{print $1}')
else
    echo "Error: Neither sha256sum nor shasum utility found. Cannot verify integrity."
    exit 1
fi

if [ "$ACTUAL_CHECKSUM" != "$EXPECTED_CHECKSUM" ]; then
    echo "Error: Checksum verification failed!"
    echo "Expected: $EXPECTED_CHECKSUM"
    echo "Actual:   $ACTUAL_CHECKSUM"
    exit 1
fi
echo "Checksum verified successfully."

if command -v gh >/dev/null 2>&1; then
    echo "Verifying artifact attestation with GitHub CLI..."
    if ! gh attestation verify "$TMP_ARCHIVE" --repo "$REPO"; then
        echo "Error: Artifact attestation verification failed!"
        exit 1
    fi
    echo "Artifact attestation verified successfully."
else
    echo "Notice: GitHub CLI (gh) not found. Skipping artifact attestation verification."
fi

echo "Extracting archive..."
if [ "$EXT" = ".tar.gz" ]; then
    tar -xzf "$TMP_ARCHIVE" -C "$TMP_DIR" "$BINARY_NAME"
else
    unzip -q "$TMP_ARCHIVE" "$BINARY_NAME" -d "$TMP_DIR"
fi

echo "Installing $BINARY_NAME to $INSTALL_DIR..."
chmod +x "$TMP_DIR/$BINARY_NAME"
if [ -w "$INSTALL_DIR" ]; then
    mv "$TMP_DIR/$BINARY_NAME" "$INSTALL_DIR/$BINARY_NAME"
else
    if [ "$(id -u)" -eq 0 ]; then
        echo "Error: Cannot write to $INSTALL_DIR even as root. Check mount permissions."
        exit 1
    fi
    echo "Elevated permissions required to write to $INSTALL_DIR. Prompting for sudo..."
    if [ -t 0 ]; then
        sudo mv "$TMP_DIR/$BINARY_NAME" "$INSTALL_DIR/$BINARY_NAME"
    else
        echo "Error: Cannot prompt for sudo in a non-interactive shell. Please run this script as root (e.g. via sudo)."
        exit 1
    fi
fi

echo "Successfully installed $BINARY_NAME $TARGET_VERSION!"
echo "Run 'sinq -v' to verify."
