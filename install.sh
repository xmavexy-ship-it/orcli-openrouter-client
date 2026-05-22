#!/usr/bin/env bash
# orcli installer for Arch Linux

set -e

echo ""
echo "  ┌─────────────────────────────┐"
echo "  │   orcli — OpenRouter CLI    │"
echo "  └─────────────────────────────┘"
echo ""

# Check Go
if ! command -v go &>/dev/null; then
    echo "  → Installing Go..."
    sudo pacman -S --noconfirm go
fi

echo "  → Go version: $(go version)"
echo "  → Downloading dependencies..."
go mod tidy

echo "  → Building..."
go build -ldflags="-s -w" -o orcli .

echo "  → Installing to /usr/local/bin/..."
sudo install -m755 orcli /usr/local/bin/orcli

echo ""
echo "  ✓ Done! Run: orcli config --key YOUR_OPENROUTER_KEY"
echo "  ✓ Then:      orcli"
echo ""
