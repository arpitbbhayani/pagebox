#!/bin/sh
set -e

# Pagebox Installer
# Usage:
#   curl -fsSL https://raw.githubusercontent.com/arpitbbhayani/pagebox/master/install.sh | sh

REPO_OWNER="arpitbbhayani"
REPO_NAME="pagebox"
REPO_URL="https://github.com/${REPO_OWNER}/${REPO_NAME}"
BINARY_NAME="pagebox"

# Text formatting
BOLD="\033[1m"
GREEN="\033[32m"
YELLOW="\033[33m"
RED="\033[31m"
BLUE="\033[34m"
RESET="\033[0m"

# Disable color if stdout is not a TTY
if [ ! -t 1 ]; then
    BOLD=""
    GREEN=""
    YELLOW=""
    RED=""
    BLUE=""
    RESET=""
fi

log_info() {
    printf "${BLUE}›${RESET} %s\n" "$1"
}

log_ok() {
    printf "${GREEN}✓${RESET} %s\n" "$1"
}

log_warn() {
    printf "${YELLOW}!${RESET} %s\n" "$1"
}

log_err() {
    printf "${RED}✗${RESET} %s\n" "$1" >&2
}

echo
printf "${BOLD}Pagebox Installer${RESET}\n"
echo

# Detect OS and architecture
OS="$(uname -s | tr '[:upper:]' '[:lower:]')"
ARCH="$(uname -m)"

case "$ARCH" in
    x86_64|amd64)
        ARCH="amd64"
        ;;
    arm64|aarch64)
        ARCH="arm64"
        ;;
    *)
        log_err "Unsupported architecture: $ARCH"
        exit 1
        ;;
esac

case "$OS" in
    linux|darwin)
        ;;
    *)
        log_err "Unsupported operating system: $OS"
        exit 1
        ;;
esac

TMP_DIR="$(mktemp -d 2>/dev/null || mktemp -d -t 'pagebox-install')"
cleanup() {
    rm -rf "$TMP_DIR"
}
trap cleanup EXIT INT TERM

# Determine installation directory
if [ -n "$PAGEBOX_INSTALL_DIR" ]; then
    INSTALL_DIR="$PAGEBOX_INSTALL_DIR"
elif [ -w "/usr/local/bin" ]; then
    INSTALL_DIR="/usr/local/bin"
else
    INSTALL_DIR="$HOME/.local/bin"
fi

mkdir -p "$INSTALL_DIR"

BINARY_PATH="$TMP_DIR/$BINARY_NAME"

# Check if run inside existing repository with cmd/pagebox/main.go
if [ -f "./cmd/pagebox/main.go" ] && command -v go >/dev/null 2>&1; then
    log_info "Building from local repository source..."
    go build -o "$BINARY_PATH" ./cmd/pagebox
else
    # Try downloading from GitHub Releases first
    RELEASE_URL="https://github.com/${REPO_OWNER}/${REPO_NAME}/releases/latest/download/pagebox-${OS}-${ARCH}"
    REL_TMP="$TMP_DIR/pagebox-release"
    HTTP_CODE=$(curl -sL -w "%{http_code}" -o "$REL_TMP" "$RELEASE_URL" 2>/dev/null || echo "000")

    if [ "$HTTP_CODE" = "200" ] && [ -s "$REL_TMP" ]; then
        mv "$REL_TMP" "$BINARY_PATH"
        log_ok "Downloaded prebuilt binary from GitHub Releases"
    else
        rm -f "$REL_TMP"
        # If release binary is not available, check for Go to build from source
        if command -v go >/dev/null 2>&1; then
            log_info "Building from master branch source..."
            ARCHIVE_URL="https://github.com/${REPO_OWNER}/${REPO_NAME}/archive/refs/heads/master.tar.gz"
            log_info "Fetching latest source from $ARCHIVE_URL..."
            curl -fsSL "$ARCHIVE_URL" | tar -xz -C "$TMP_DIR"
            (
                cd "$TMP_DIR/${REPO_NAME}-master"
                go build -o "$BINARY_PATH" ./cmd/pagebox
            )
        else
            log_err "Pre-built binary is not available yet, and Go compiler was not found."
            log_err "Please install Go (https://go.dev/dl/) and re-run this script, or clone the repository and run 'make'."
            exit 1
        fi
    fi
fi

chmod +x "$BINARY_PATH"

# Install binary
DEST_FILE="$INSTALL_DIR/$BINARY_NAME"
log_info "Installing $BINARY_NAME to $INSTALL_DIR..."

if [ -w "$INSTALL_DIR" ]; then
    mv "$BINARY_PATH" "$DEST_FILE"
else
    if command -v sudo >/dev/null 2>&1; then
        log_info "Elevated permissions required to write to $INSTALL_DIR"
        sudo mv "$BINARY_PATH" "$DEST_FILE"
    else
        log_err "Cannot write to $INSTALL_DIR and sudo is not available."
        exit 1
    fi
fi

log_ok "Pagebox installed successfully to $DEST_FILE"

# PATH check
case ":$PATH:" in
    *":$INSTALL_DIR:"*)
        ;;
    *)
        echo
        log_warn "$INSTALL_DIR is not in your PATH."
        log_info "Add it to your PATH by adding this line to your shell profile (~/.bashrc or ~/.zshrc):"
        printf "\n  ${BOLD}export PATH=\"%s:\$PATH\"${RESET}\n\n" "$INSTALL_DIR"
        ;;
esac

echo
log_info "Get started by running:"
printf "  ${BOLD}pagebox init${RESET}\n"
printf "  ${BOLD}pagebox daemon install${RESET}\n"
echo
