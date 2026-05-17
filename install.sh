#!/bin/sh
# bee installer — POSIX sh, no bashisms.
#
# 1. detect os/arch
# 2. download pre-built binary if a release exists, else build from source
#    (requires `go` on PATH)
# 3. install to /usr/local/bin/bee (prompts for sudo when needed)
# 4. first `bee` / `bee <skill>` invocation seeds ~/.bee/skills with the
#    bundled defaults — no shim sync or PATH mutation needed
# 5. idempotent: re-running is safe
set -eu

REPO="elhenro/bee"
INSTALL_DIR="${BEE_INSTALL_DIR:-/usr/local/bin}"
BIN_NAME="bee"

log() { printf "bee-install: %s\n" "$*"; }
err() { printf "bee-install: error: %s\n" "$*" >&2; exit 1; }

detect_os() {
    uname_s=$(uname -s 2>/dev/null || echo unknown)
    case "$uname_s" in
        Darwin) echo darwin ;;
        Linux)  echo linux ;;
        *)      echo "$uname_s" | tr '[:upper:]' '[:lower:]' ;;
    esac
}

detect_arch() {
    uname_m=$(uname -m 2>/dev/null || echo unknown)
    case "$uname_m" in
        x86_64|amd64) echo amd64 ;;
        arm64|aarch64) echo arm64 ;;
        *) echo "$uname_m" ;;
    esac
}

has_cmd() { command -v "$1" >/dev/null 2>&1; }

need_sudo() {
    # write test the install dir without actually writing
    if [ -w "$INSTALL_DIR" ]; then
        echo ""
    elif has_cmd sudo; then
        echo "sudo"
    else
        err "no write access to $INSTALL_DIR and sudo not found"
    fi
}

download_binary() {
    os=$1
    arch=$2
    dest=$3
    url="https://github.com/${REPO}/releases/latest/download/bee-${os}-${arch}"
    log "downloading $url"
    if has_cmd curl; then
        if ! curl -fsSL "$url" -o "$dest"; then
            return 1
        fi
    elif has_cmd wget; then
        if ! wget -q "$url" -O "$dest"; then
            return 1
        fi
    else
        err "need curl or wget to download release"
    fi
    chmod +x "$dest"
    return 0
}

build_from_source() {
    dest=$1
    has_cmd go || err "no pre-built release and 'go' not on PATH"
    log "building from source with $(go version)"
    tmpdir=$(mktemp -d)
    trap 'rm -rf "$tmpdir"' EXIT
    # use the current checkout if we're inside one, else clone
    if [ -f ./go.mod ] && grep -q "module github.com/elhenro/bee" ./go.mod 2>/dev/null; then
        src=$(pwd)
    else
        has_cmd git || err "no checkout and 'git' not on PATH"
        log "cloning $REPO into $tmpdir"
        git clone --depth=1 "https://github.com/${REPO}.git" "$tmpdir/src" >/dev/null
        src="$tmpdir/src"
    fi
    (cd "$src" && go build -o "$dest" ./cmd/bee) || err "go build failed"
}

install_binary() {
    src=$1
    sudo_cmd=$(need_sudo)
    target="$INSTALL_DIR/$BIN_NAME"
    if [ -n "$sudo_cmd" ]; then
        log "installing to $target (sudo)"
    else
        log "installing to $target"
    fi
    $sudo_cmd mv "$src" "$target"
    $sudo_cmd chmod 0755 "$target"
}

main() {
    os=$(detect_os)
    arch=$(detect_arch)
    log "platform: ${os}/${arch}"

    case "$os" in
        darwin|linux) ;;
        *) err "unsupported os: $os (darwin/linux only for now)" ;;
    esac
    case "$arch" in
        amd64|arm64) ;;
        *) err "unsupported arch: $arch (amd64/arm64 only for now)" ;;
    esac

    tmpbin=$(mktemp)
    if download_binary "$os" "$arch" "$tmpbin" 2>/dev/null; then
        log "downloaded pre-built binary"
    else
        log "no pre-built release available, falling back to source build"
        rm -f "$tmpbin"
        tmpbin=$(mktemp)
        build_from_source "$tmpbin"
    fi
    install_binary "$tmpbin"

    log "done."
    log "next: export OPENROUTER_API_KEY=... and run 'bee' (or 'bee run \"task\"')"
    log "skills dir: ~/.bee/skills  (each skill is invokable as 'bee <name>')"
}

main "$@"
