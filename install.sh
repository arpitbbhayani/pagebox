#!/usr/bin/env sh
# Universal installer script for pagebox (https://pagebox.arpitbhayani.me)
#
# Usage:
#   curl -fsSL https://pagebox.arpitbhayani.me/install.sh | sh
#
# Environment variables:
#   VERSION       - target version to install (e.g. "0.1.0" or "latest", default: "latest")
#   INSTALL_DIR   - target directory for binary (default: /usr/local/bin or ~/.local/bin)
#   PAGEBOX_REPO  - GitHub repository (default: arpitbbhayani/pagebox)

set -eu

REPO="${PAGEBOX_REPO:-arpitbbhayani/pagebox}"
VERSION="${VERSION:-latest}"

# Color codes
BOLD="\033[1m"
GREEN="\033[38;5;71m"
AMBER="\033[38;5;208m"
RED="\033[38;5;167m"
RESET="\033[0m"

# Disable color if stdout is not a TTY
if [ ! -t 1 ]; then
  BOLD=""
  GREEN=""
  AMBER=""
  RED=""
  RESET=""
fi

log_info() {
  printf " ${GREEN}✓${RESET} %b\n" "$1"
}

log_step() {
  printf " ${AMBER}›${RESET} %b\n" "$1"
}

log_warn() {
  printf " ${AMBER}!${RESET} %b\n" "$1"
}

log_error() {
  printf " ${RED}✗${RESET} %b\n" "$1" >&2
}

# 1. Detect OS
OS="$(uname -s | tr '[:upper:]' '[:lower:]')"
case "$OS" in
  linux*)   OS="linux" ;;
  darwin*)  OS="darwin" ;;
  freebsd*) OS="freebsd" ;;
  openbsd*) OS="openbsd" ;;
  netbsd*)  OS="netbsd" ;;
  msys*|cygwin*|mingw*) OS="windows" ;;
  *)
    log_error "Unsupported operating system: $OS"
    exit 1
    ;;
esac

# 2. Detect Architecture
ARCH="$(uname -m)"
case "$ARCH" in
  x86_64|amd64)      ARCH="amd64" ;;
  aarch64|arm64)     ARCH="arm64" ;;
  armv7*|armv6*|arm) ARCH="arm" ;;
  i386|i686)         ARCH="386" ;;
  riscv64)           ARCH="riscv64" ;;
  *)
    log_error "Unsupported machine architecture: $ARCH"
    exit 1
    ;;
esac

# 3. HTTP Client detection
fetch() {
  url="$1"
  if command -v curl >/dev/null 2>&1; then
    curl -fsSL --connect-timeout 10 --max-time 30 --retry 2 "$url"
  elif command -v wget >/dev/null 2>&1; then
    wget -qO- -T 15 -t 3 "$url"
  else
    log_error "Neither curl nor wget found in PATH. Please install one of them."
    exit 1
  fi
}

fetch_file() {
  url="$1"
  out="$2"
  if command -v curl >/dev/null 2>&1; then
    curl -fsSL --connect-timeout 10 --max-time 300 --retry 2 -o "$out" "$url"
  elif command -v wget >/dev/null 2>&1; then
    wget -q -T 30 -t 3 -O "$out" "$url"
  fi
}

log_step "Detecting latest release for ${BOLD}${REPO}${RESET} (${OS}/${ARCH})..."

# 4. Resolve latest version if not explicitly provided
if [ "$VERSION" = "latest" ]; then
  RELEASE_JSON=$(fetch "https://api.github.com/repos/${REPO}/releases/latest" 2>/dev/null || true)
  if [ -n "$RELEASE_JSON" ]; then
    VERSION=$(printf '%s' "$RELEASE_JSON" | grep '"tag_name":' | head -n 1 | sed -E 's/.*"tag_name": *"([^"]+)".*/\1/' | sed 's/^v//')
  fi
  # Fallback to VERSION file in master if release API returned empty/rate-limited
  if [ -z "$VERSION" ] || [ "$VERSION" = "latest" ]; then
    VERSION=$(fetch "https://raw.githubusercontent.com/${REPO}/master/VERSION" 2>/dev/null | tr -d ' \r\n' || true)
  fi
fi

if [ -z "$VERSION" ]; then
  VERSION="0.1.3"
fi

BINARY_EXT=""
[ "$OS" = "windows" ] && BINARY_EXT=".exe"

# 5. Determine installation target directory (cascading ladder; sudo is last resort)
USE_SUDO=0
TARGET_DIR=""

is_writable() {
  d="$1"
  [ -z "$d" ] && return 1
  mkdir -p "$d" 2>/dev/null || return 1
  test_file="$d/.pagebox_test_$$"
  if ( : > "$test_file" ) 2>/dev/null; then
    rm -f "$test_file" 2>/dev/null
    return 0
  fi
  return 1
}

# Priority 1: Explicit user overrides
if [ -n "${PAGEBOX_INSTALL_DIR:-}" ] && is_writable "$PAGEBOX_INSTALL_DIR"; then
  TARGET_DIR="$PAGEBOX_INSTALL_DIR"
elif [ -n "${INSTALL_DIR:-}" ] && is_writable "$INSTALL_DIR"; then
  TARGET_DIR="$INSTALL_DIR"
fi

# Priority 2: Standard installation directory (default: ~/.local/bin)
if [ -z "$TARGET_DIR" ]; then
  if [ -n "${HOME:-}" ] && is_writable "${HOME}/.local/bin"; then
    TARGET_DIR="${HOME}/.local/bin"
  elif is_writable "/usr/local/bin"; then
    TARGET_DIR="/usr/local/bin"
  elif [ -n "${HOME:-}" ] && is_writable "${HOME}/bin"; then
    TARGET_DIR="${HOME}/bin"
  fi
fi

# Priority 3: Fallback requiring elevated permissions
if [ -z "$TARGET_DIR" ]; then
  if command -v sudo >/dev/null 2>&1 && [ -d "/usr/local/bin" ]; then
    USE_SUDO=1
    TARGET_DIR="/usr/local/bin"
  else
    TARGET_DIR="${TMPDIR:-/tmp}/pagebox/bin"
    mkdir -p "$TARGET_DIR" 2>/dev/null || true
  fi
fi

TARGET_BIN="${TARGET_DIR}/pagebox${BINARY_EXT}"

BINARY_NAME="pagebox-${VERSION}-${OS}-${ARCH}${BINARY_EXT}"
DOWNLOAD_URL="https://github.com/${REPO}/releases/download/v${VERSION}/${BINARY_NAME}"
FALLBACK_URL="https://github.com/${REPO}/releases/download/${VERSION}/${BINARY_NAME}"

TMP_DIR="$(mktemp -d 2>/dev/null || mktemp -d -t 'pagebox-install')"
trap 'rm -rf "$TMP_DIR"' EXIT

TMP_FILE="${TMP_DIR}/pagebox"

# 6. Download or build fresh binary
DOWNLOADED=0
log_step "Downloading fresh binary ${BINARY_NAME}..."

if fetch_file "$DOWNLOAD_URL" "$TMP_FILE" 2>/dev/null && [ -s "$TMP_FILE" ]; then
  DOWNLOADED=1
elif fetch_file "$FALLBACK_URL" "$TMP_FILE" 2>/dev/null && [ -s "$TMP_FILE" ]; then
  DOWNLOADED=1
fi

if [ "$DOWNLOADED" = "0" ]; then
  # Fallback to source compilation if binary release is not published yet
  if command -v go >/dev/null 2>&1; then
    log_warn "Pre-built binary release not yet available. Compiling from source..."
    if [ -f "./cmd/pagebox/main.go" ]; then
      go build -o "$TMP_FILE" ./cmd/pagebox
      DOWNLOADED=1
    else
      ARCHIVE_URL="https://github.com/${REPO}/archive/refs/heads/master.tar.gz"
      log_step "Fetching source from ${ARCHIVE_URL}..."
      fetch_file "$ARCHIVE_URL" "${TMP_DIR}/source.tar.gz" 2>/dev/null || true
      if [ -f "${TMP_DIR}/source.tar.gz" ] && [ -s "${TMP_DIR}/source.tar.gz" ]; then
        tar -xz -C "$TMP_DIR" -f "${TMP_DIR}/source.tar.gz"
        SOURCE_DIR=$(find "$TMP_DIR" -maxdepth 1 -type d -name "pagebox*" | head -n 1)
        if [ -n "$SOURCE_DIR" ] && [ -d "$SOURCE_DIR/cmd/pagebox" ]; then
          (
            cd "$SOURCE_DIR"
            go build -o "$TMP_FILE" ./cmd/pagebox
          )
          DOWNLOADED=1
        fi
      fi
    fi
  fi
fi

if [ "$DOWNLOADED" = "0" ] || [ ! -s "$TMP_FILE" ]; then
  log_error "Failed to obtain binary for pagebox v${VERSION}."
  log_warn "If this version is newly tagged, the GitHub release artifacts may still be compiling."
  exit 1
fi

chmod +x "$TMP_FILE"

# 7. Install fresh binary
log_step "Installing to ${BOLD}${TARGET_BIN}${RESET}..."

if [ "${USE_SUDO:-0}" = "1" ]; then
  sudo rm -f "$TARGET_BIN"
  sudo mv "$TMP_FILE" "$TARGET_BIN"
  sudo chmod 755 "$TARGET_BIN"
else
  mkdir -p "$TARGET_DIR"
  rm -f "$TARGET_BIN"
  mv "$TMP_FILE" "$TARGET_BIN"
  chmod 755 "$TARGET_BIN"
fi

log_info "pagebox v${VERSION} installed successfully!"

# 9. Check if TARGET_DIR is in PATH
PATH_ALREADY_CONFIGURED=0
case ":$PATH:" in
  *":$TARGET_DIR:"*) PATH_ALREADY_CONFIGURED=1 ;;
esac

detect_shell_rc() {
  case "$(basename "${SHELL:-}")" in
    zsh)
      echo "${HOME}/.zshrc"
      ;;
    bash)
      if [ -f "${HOME}/.bashrc" ]; then
        echo "${HOME}/.bashrc"
      elif [ -f "${HOME}/.bash_profile" ]; then
        echo "${HOME}/.bash_profile"
      else
        echo "${HOME}/.profile"
      fi
      ;;
    fish)
      echo "${HOME}/.config/fish/config.fish"
      ;;
    *)
      if [ -f "${HOME}/.zshrc" ]; then
        echo "${HOME}/.zshrc"
      elif [ -f "${HOME}/.bashrc" ]; then
        echo "${HOME}/.bashrc"
      elif [ -f "${HOME}/.bash_profile" ]; then
        echo "${HOME}/.bash_profile"
      elif [ -f "${HOME}/.profile" ]; then
        echo "${HOME}/.profile"
      else
        echo ""
      fi
      ;;
  esac
}

SHELL_RC="$(detect_shell_rc)"

has_path_configured() {
  rc="$1"
  dir="$2"
  [ -f "$rc" ] && grep -Fq "$dir" "$rc"
}

if [ "$PATH_ALREADY_CONFIGURED" = "0" ]; then
  log_warn "${TARGET_DIR} is not in your current PATH."
  if [ -n "$SHELL_RC" ]; then
    if has_path_configured "$SHELL_RC" "$TARGET_DIR"; then
      log_info "PATH addition already present in ${SHELL_RC}."
    else
      printf "\n# Added by pagebox installer\nexport PATH=\"%s:\$PATH\"\n" "$TARGET_DIR" >> "$SHELL_RC"
      log_info "Added ${TARGET_DIR} to PATH in ${SHELL_RC}"
    fi
  else
    printf "   Add it by running:\n"
    printf "     export PATH=\"%s:\$PATH\"\n\n" "$TARGET_DIR"
  fi
fi

# 10. Next Steps
echo
printf "Get started with %b:\n\n" "${BOLD}pagebox${RESET}"
printf "  ${AMBER}pagebox init${RESET}            # configure docs folder & S3 storage\n"
printf "  ${AMBER}pagebox serve${RESET}           # run foreground server & sync loop\n"
printf "  ${AMBER}pagebox daemon install${RESET}  # run background sync daemon as OS service\n"
echo
