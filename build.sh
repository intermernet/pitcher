#!/usr/bin/env sh
# Build script for Pitcher.
# Prerequisites: Go 1.25+, gcc/cc (for CGo), npm, and an internet connection
# for the first run (to download Go modules and npm packages).
set -e

die() { printf 'Error: %s\n' "$1" >&2; exit 1; }
info() { printf '==> %s\n' "$1"; }

# ── Go ──────────────────────────────────────────────────────────────────────
command -v go >/dev/null 2>&1 || die "Go is not installed. See https://go.dev/dl/"
GO_VER=$(go version | awk '{print $3}' | sed 's/go//')
GO_MAJOR=$(printf '%s' "$GO_VER" | cut -d. -f1)
GO_MINOR=$(printf '%s' "$GO_VER" | cut -d. -f2)
if [ "$GO_MAJOR" -lt 1 ] || { [ "$GO_MAJOR" -eq 1 ] && [ "$GO_MINOR" -lt 25 ]; }; then
    die "Go 1.25 or later required (found $GO_VER). See https://go.dev/dl/"
fi
info "Go $GO_VER"

# ── C compiler (required by Wails CGo on macOS and Linux; not needed on Windows) ─
OS=$(uname -s)
case "$OS" in
    Darwin)
        # Wails uses Objective-C/WebKit bindings on macOS
        if ! xcrun --find cc >/dev/null 2>&1; then
            die "Xcode Command Line Tools are required on macOS.
  Install with: xcode-select --install"
        fi
        info "Xcode CLT $(xcrun --find cc)"
        ;;
    Linux)
        # Wails uses CGo GTK bindings on Linux
        command -v cc >/dev/null 2>&1 || command -v gcc >/dev/null 2>&1 || \
            die "A C compiler is required on Linux for Wails' GTK bindings.
  Debian/Ubuntu: sudo apt install build-essential
  Fedora:        sudo dnf install gcc
  Arch:          sudo pacman -S base-devel"
        ;;
    # Windows (MSYS2/Git Bash): no CGo needed — Wails uses pure Go COM on Windows
esac

# ── npm ─────────────────────────────────────────────────────────────────────
command -v npm >/dev/null 2>&1 || die "npm is not installed. See https://nodejs.org/"
info "npm $(npm --version)"

# ── wails3 ──────────────────────────────────────────────────────────────────
if ! command -v wails3 >/dev/null 2>&1; then
    info "wails3 not found — installing via go install..."
    go install github.com/wailsapp/wails/v3/cmd/wails3@latest
    # Verify it's now reachable (go install writes to $GOPATH/bin or $HOME/go/bin)
    command -v wails3 >/dev/null 2>&1 || \
        die "wails3 installed but not in PATH. Add \$(go env GOPATH)/bin to your PATH and re-run."
fi
info "wails3 $(wails3 version 2>/dev/null | head -1 || echo '(version unavailable)')"

# ── Generate Wails JS/TS bindings ────────────────────────────────────────────
info "Generating Wails bindings..."
wails3 generate bindings

# ── Frontend ─────────────────────────────────────────────────────────────────
info "Installing frontend dependencies..."
( cd frontend && npm install )

info "Building frontend..."
( cd frontend && npm run build )

# ── Go binary ────────────────────────────────────────────────────────────────
info "Building pitcher..."
go build -ldflags="-s -w" -o pitcher .

info "Build complete — binary: ./pitcher"
info "Run with --help for usage, or --gui to open the graphical interface."
