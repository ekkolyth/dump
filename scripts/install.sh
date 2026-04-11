#!/bin/bash
set -euo pipefail

REPO="ekkolyth/dump"
INSTALL_DIR="$HOME/.local/bin"

# Detect OS
OS="$(uname -s)"
case "$OS" in
  Darwin) OS="darwin" ;;
  Linux)  OS="linux" ;;
  *)
    echo "Unsupported OS: $OS (dump supports macOS and Linux)"
    exit 1
    ;;
esac

# Detect arch
ARCH="$(uname -m)"
case "$ARCH" in
  x86_64)  ARCH="amd64" ;;
  aarch64) ARCH="arm64" ;;
  arm64)   ARCH="arm64" ;;
  *)
    echo "Unsupported architecture: $ARCH"
    exit 1
    ;;
esac

# Fetch latest version
echo "Fetching latest version..."
TAG=$(curl -fsSL "https://api.github.com/repos/$REPO/releases/latest" | grep '"tag_name"' | sed -E 's/.*"v?([^"]+)".*/\1/')

if [ -z "$TAG" ]; then
  echo "Failed to fetch latest version"
  exit 1
fi

# Use macOS 11 build for older macOS versions
SUFFIX=""
if [ "$OS" = "darwin" ]; then
  MACOS_VERSION=$(sw_vers -productVersion | cut -d. -f1)
  if [ "$MACOS_VERSION" -lt 12 ] 2>/dev/null; then
    SUFFIX="_macos11"
  fi
fi

ASSET="dump_${TAG}_${OS}_${ARCH}${SUFFIX}.tar.gz"
URL="https://github.com/$REPO/releases/download/v${TAG}/$ASSET"

# Download and extract
echo "Downloading dump v${TAG} for ${OS}/${ARCH}..."
TMP=$(mktemp -d)
trap 'rm -rf "$TMP"' EXIT

curl -fsSL "$URL" -o "$TMP/$ASSET"
tar -xzf "$TMP/$ASSET" -C "$TMP"

# Install
mkdir -p "$INSTALL_DIR"
mv "$TMP/dump" "$INSTALL_DIR/dump"
chmod +x "$INSTALL_DIR/dump"

echo ""
echo "dump v${TAG} installed to $INSTALL_DIR/dump"

# Create desktop shortcut on macOS
if [ "$OS" = "darwin" ]; then
  SHORTCUT="$HOME/Desktop/Dump.command"
  printf '#!/bin/bash\n%s/dump\n' "$INSTALL_DIR" > "$SHORTCUT"
  chmod +x "$SHORTCUT"
  echo "Desktop shortcut created at $SHORTCUT"
fi

# Check PATH
if ! echo "$PATH" | tr ':' '\n' | grep -q "$INSTALL_DIR"; then
  echo ""
  echo "Add this to your shell profile:"
  echo "  export PATH=\"\$HOME/.local/bin:\$PATH\""
fi
