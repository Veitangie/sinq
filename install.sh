#!/bin/sh
set -e

REPO="Veitangie/sinq"
INSTALL_DIR="/usr/local/bin"
TARGET_VERSION=$1
EXE_EXT=""

OS=$(uname -s | tr '[:upper:]' '[:lower:]')
ARCH=$(uname -m)

case "$ARCH" in
    x86_64) ARCH="amd64" ;;
    aarch64|arm64) ARCH="arm64" ;;
    *) echo "Unsupported architecture: $ARCH"; exit 1 ;;
esac

case "$OS" in
    linux*)     OS="linux" ;;
    darwin*)    OS="darwin" ;;
    msys*|cygwin*|mingw*|nt|win*) OS="windows"; EXE_EXT=".exe" ;;
    *)          echo "Unsupported OS: $OS"; exit 1 ;;
esac

BINARY_NAME="sinq${EXE_EXT}"
echo "Detected OS: $OS, Architecture: $ARCH"

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

DOWNLOAD_URL="https://github.com/$REPO/releases/download/$TARGET_VERSION/sinq-${OS}-${ARCH}${EXE_EXT}"
TMP_FILE="/tmp/sinq_download${EXE_EXT}"

echo "Downloading $DOWNLOAD_URL..."
if ! curl -sL "$DOWNLOAD_URL" -o "$TMP_FILE"; then
    echo "Error: Download failed."
    exit 1
fi

CHECKSUM_URL="${DOWNLOAD_URL}.sha256"
CHECKSUM_FILE="${TMP_FILE}.sha256"

echo "Downloading checksum file..."
if ! curl -sL "$CHECKSUM_URL" -o "$CHECKSUM_FILE"; then
    echo "Error: Failed to download checksum file."
    rm -f "$TMP_FILE"
    exit 1
fi

echo "Verifying checksum..."
EXPECTED_CHECKSUM=$(awk '{print $1}' "$CHECKSUM_FILE")

if command -v sha256sum >/dev/null 2>&1; then
    ACTUAL_CHECKSUM=$(sha256sum "$TMP_FILE" | awk '{print $1}')
elif command -v shasum >/dev/null 2>&1; then
    ACTUAL_CHECKSUM=$(shasum -a 256 "$TMP_FILE" | awk '{print $1}')
else
    echo "Error: Neither sha256sum nor shasum utility found. Cannot verify binary integrity."
    rm -f "$TMP_FILE" "$CHECKSUM_FILE"
    exit 1
fi

if [ "$ACTUAL_CHECKSUM" != "$EXPECTED_CHECKSUM" ]; then
    echo "Error: Checksum verification failed! The binary may be corrupted or compromised."
    echo "Expected: $EXPECTED_CHECKSUM"
    echo "Actual:   $ACTUAL_CHECKSUM"
    rm -f "$TMP_FILE" "$CHECKSUM_FILE"
    exit 1
fi

echo "Checksum verified successfully."
rm -f "$CHECKSUM_FILE"
# --- End Checksum Verification ---

chmod +x "$TMP_FILE"

echo "Installing $BINARY_NAME to $INSTALL_DIR..."
if [ -w "$INSTALL_DIR" ]; then
    mv "$TMP_FILE" "$INSTALL_DIR/$BINARY_NAME"
else
    echo "Elevated permissions required to write to $INSTALL_DIR. Prompting for sudo..."
    sudo mv "$TMP_FILE" "$INSTALL_DIR/$BINARY_NAME"
fi

echo "Successfully installed $BINARY_NAME $TARGET_VERSION!"
echo "Run 'sinq -v' to verify."
