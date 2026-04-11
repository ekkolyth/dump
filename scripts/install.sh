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

# Create Dump.app on macOS
if [ "$OS" = "darwin" ]; then
  APP="$HOME/Desktop/Dump.app"
  rm -rf "$APP"
  mkdir -p "$APP/Contents/MacOS" "$APP/Contents/Resources"

  # Copy icon if included in the archive
  if [ -f "$TMP/icon.icns" ]; then
    cp "$TMP/icon.icns" "$APP/Contents/Resources/icon.icns"
  fi

  # Launcher script
  cat > "$APP/Contents/MacOS/Dump" << 'LAUNCHER'
#!/bin/bash
open -a Terminal "$HOME/.local/bin/dump"
LAUNCHER
  chmod +x "$APP/Contents/MacOS/Dump"

  # Info.plist
  cat > "$APP/Contents/Info.plist" << PLIST
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>CFBundleName</key>
  <string>Dump</string>
  <key>CFBundleExecutable</key>
  <string>Dump</string>
  <key>CFBundleIconFile</key>
  <string>icon</string>
  <key>CFBundleIdentifier</key>
  <string>com.ekkolyth.dump</string>
  <key>CFBundleVersion</key>
  <string>${TAG}</string>
  <key>CFBundleShortVersionString</key>
  <string>${TAG}</string>
  <key>CFBundlePackageType</key>
  <string>APPL</string>
</dict>
</plist>
PLIST

  echo "Dump.app created on Desktop"
fi

# Check PATH
if ! echo "$PATH" | tr ':' '\n' | grep -q "$INSTALL_DIR"; then
  echo ""
  echo "Add this to your shell profile:"
  echo "  export PATH=\"\$HOME/.local/bin:\$PATH\""
fi
