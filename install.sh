#!/usr/bin/env bash
# orcli universal installer
# Supports: Arch, Debian/Ubuntu, Fedora/RHEL, macOS, any Linux with Go tarball fallback

set -e

INSTALL_DIR="/usr/local/bin"
BINARY="orcli"
GO_VERSION="1.22.4"
GO_TARBALL="go${GO_VERSION}.linux-amd64.tar.gz"
GO_URL="https://go.dev/dl/${GO_TARBALL}"

# ── Colors ────────────────────────────────────────────────────────────────────
RED='\033[0;31m'; GREEN='\033[0;32m'; YELLOW='\033[1;33m'
CYAN='\033[0;36m'; BOLD='\033[1m'; DIM='\033[2m'; RESET='\033[0m'

info()    { echo -e "${CYAN}  →${RESET} $*"; }
ok()      { echo -e "${GREEN}  ✓${RESET} $*"; }
warn()    { echo -e "${YELLOW}  ⚠${RESET}  $*"; }
err()     { echo -e "${RED}  ✗${RESET} $*"; exit 1; }
header()  { echo -e "\n${BOLD}${CYAN}  $*${RESET}"; }
dim()     { echo -e "${DIM}  $*${RESET}"; }

# ── Banner ────────────────────────────────────────────────────────────────────
echo ""
echo -e "${CYAN}${BOLD}  ╔══════════════════════════════════════╗"
echo -e "  ║   ⬡  orcli — OpenRouter Agent CLI   ║"
echo -e "  ╚══════════════════════════════════════╝${RESET}"
echo ""

# ── Detect OS ─────────────────────────────────────────────────────────────────
header "Detecting system..."

OS="$(uname -s)"
ARCH="$(uname -m)"

case "$ARCH" in
  x86_64)  GOARCH="amd64" ;;
  aarch64|arm64) GOARCH="arm64" ;;
  armv7l)  GOARCH="armv6l" ;;
  *)       err "Unsupported architecture: $ARCH" ;;
esac

case "$OS" in
  Linux)
    GOOS="linux"
    GO_TARBALL="go${GO_VERSION}.linux-${GOARCH}.tar.gz"
    GO_URL="https://go.dev/dl/${GO_TARBALL}"

    if [ -f /etc/os-release ]; then
      . /etc/os-release
      DISTRO="${ID}"
    else
      DISTRO="unknown"
    fi
    ;;
  Darwin)
    GOOS="darwin"
    GO_TARBALL="go${GO_VERSION}.darwin-${GOARCH}.pkg"
    GO_URL="https://go.dev/dl/${GO_TARBALL}"
    DISTRO="macos"
    ;;
  *)
    err "Unsupported OS: $OS"
    ;;
esac

ok "OS: ${OS} (${DISTRO}), Arch: ${ARCH}"

# ── Install Go ────────────────────────────────────────────────────────────────
install_go() {
  header "Installing Go ${GO_VERSION}..."

  case "$DISTRO" in
    arch|manjaro|endeavouros|garuda)
      info "Using pacman..."
      sudo pacman -S --noconfirm go
      ;;

    ubuntu|debian|linuxmint|pop|kali|raspbian)
      info "Using apt..."
      sudo apt-get update -qq
      sudo apt-get install -y golang-go
      # Check if version is too old (apt often has ancient Go)
      if ! go version 2>/dev/null | grep -qE 'go1\.(1[89]|2[0-9])'; then
        warn "apt Go is too old, installing from tarball..."
        install_go_tarball
      fi
      ;;

    fedora)
      info "Using dnf..."
      sudo dnf install -y golang
      ;;

    centos|rhel|rocky|almalinux)
      info "Using dnf/yum..."
      sudo dnf install -y golang 2>/dev/null || sudo yum install -y golang
      ;;

    opensuse*|sles)
      info "Using zypper..."
      sudo zypper install -y go
      ;;

    alpine)
      info "Using apk..."
      sudo apk add --no-cache go
      ;;

    macos)
      if command -v brew &>/dev/null; then
        info "Using Homebrew..."
        brew install go
      else
        warn "Homebrew not found, installing from pkg..."
        install_go_pkg_macos
      fi
      ;;

    *)
      warn "Unknown distro '${DISTRO}', installing Go from tarball..."
      install_go_tarball
      ;;
  esac
}

install_go_tarball() {
  local tarball="go${GO_VERSION}.linux-${GOARCH}.tar.gz"
  local url="https://go.dev/dl/${tarball}"

  info "Downloading ${tarball}..."
  if command -v curl &>/dev/null; then
    curl -L -o "/tmp/${tarball}" "$url"
  elif command -v wget &>/dev/null; then
    wget -O "/tmp/${tarball}" "$url"
  else
    err "Neither curl nor wget found. Install one and retry."
  fi

  info "Extracting to /usr/local/go..."
  sudo rm -rf /usr/local/go
  sudo tar -C /usr/local -xzf "/tmp/${tarball}"
  rm -f "/tmp/${tarball}"

  # Add to PATH for this session
  export PATH="/usr/local/go/bin:$PATH"

  # Persist to shell configs
  for rc in ~/.bashrc ~/.zshrc ~/.profile; do
    if [ -f "$rc" ] && ! grep -q '/usr/local/go/bin' "$rc"; then
      echo 'export PATH="/usr/local/go/bin:$PATH"' >> "$rc"
      ok "Added Go to PATH in $rc"
    fi
  done
}

install_go_pkg_macos() {
  local pkg="go${GO_VERSION}.darwin-${GOARCH}.pkg"
  local url="https://go.dev/dl/${pkg}"
  info "Downloading ${pkg}..."
  curl -L -o "/tmp/${pkg}" "$url"
  sudo installer -pkg "/tmp/${pkg}" -target /
  rm -f "/tmp/${pkg}"
}

# ── Check / install Go ────────────────────────────────────────────────────────
header "Checking Go..."

if command -v go &>/dev/null; then
  GOVER=$(go version | awk '{print $3}' | sed 's/go//')
  ok "Go ${GOVER} already installed at $(which go)"
else
  warn "Go not found"
  install_go

  # Re-check
  if ! command -v go &>/dev/null; then
    # Try common paths
    for p in /usr/local/go/bin /usr/lib/go/bin /usr/local/bin; do
      if [ -x "$p/go" ]; then
        export PATH="$p:$PATH"
        break
      fi
    done
  fi

  command -v go &>/dev/null || err "Go installation failed. Install manually: https://go.dev/dl/"
  ok "Go $(go version | awk '{print $3}') installed"
fi

# ── Check source files ────────────────────────────────────────────────────────
header "Checking source files..."

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

[ -f "$SCRIPT_DIR/main.go" ] || err "main.go not found in $SCRIPT_DIR"
[ -f "$SCRIPT_DIR/go.mod" ]  || err "go.mod not found in $SCRIPT_DIR"

ok "Found main.go and go.mod in $SCRIPT_DIR"

# ── Download dependencies ─────────────────────────────────────────────────────
header "Downloading dependencies..."
cd "$SCRIPT_DIR"
go mod tidy
ok "Dependencies ready"

# ── Build ─────────────────────────────────────────────────────────────────────
header "Building orcli..."
go build -ldflags="-s -w" -o "$BINARY" .
ok "Built: $SCRIPT_DIR/$BINARY ($(du -sh $BINARY | cut -f1))"

# ── Install binary ────────────────────────────────────────────────────────────
header "Installing to $INSTALL_DIR..."

if [ -w "$INSTALL_DIR" ]; then
  cp "$BINARY" "$INSTALL_DIR/$BINARY"
else
  sudo install -m755 "$BINARY" "$INSTALL_DIR/$BINARY"
fi

ok "Installed: $INSTALL_DIR/$BINARY"

# ── Verify ────────────────────────────────────────────────────────────────────
command -v orcli &>/dev/null || export PATH="$INSTALL_DIR:$PATH"

echo ""
echo -e "${GREEN}${BOLD}  ┌─────────────────────────────────────┐"
echo -e "  │   orcli installed successfully!     │"
echo -e "  └─────────────────────────────────────┘${RESET}"
echo ""
echo -e "  ${BOLD}Next steps:${RESET}"
echo ""
dim "  1. Get your API key at https://openrouter.ai/keys"
echo ""
echo -e "  ${CYAN}2.${RESET} orcli config --key sk-or-v1-XXXXXXXXXXXXXXXX"
echo -e "     ${DIM}or: export OPENROUTER_API_KEY=sk-or-v1-XXXX  (add to ~/.bashrc)${RESET}"
echo ""
echo -e "  ${CYAN}3.${RESET} orcli"
echo ""
dim "  Optional: change default model"
dim "  orcli config --model anthropic/claude-sonnet-4-5"
echo ""
dim "  Run 'orcli models' to see all available models"
echo ""
