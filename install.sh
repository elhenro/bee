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
# set to 1 once a downloaded binary's checksum matches the published SHA256SUMS.
# gates the macOS quarantine strip so an unverified download can't silently
# bypass Gatekeeper.
VERIFIED=0

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

# verify_checksum confirms the downloaded binary matches the SHA256SUMS
# published with the release. Returns 0 on match, 1 on MISMATCH (caller must
# abort), 2 when no SHA256SUMS is published or no sha256 tool is available
# (caller warns: unverified).
verify_checksum() {
    dest=$1
    os=$2
    arch=$3
    sums_url="https://github.com/${REPO}/releases/latest/download/SHA256SUMS"
    sums=""
    if has_cmd curl; then
        sums=$(curl -fsSL "$sums_url" 2>/dev/null) || sums=""
    elif has_cmd wget; then
        sums=$(wget -qO- "$sums_url" 2>/dev/null) || sums=""
    fi
    [ -n "$sums" ] || return 2
    want=$(printf "%s\n" "$sums" | grep "bee-${os}-${arch}" | awk '{print $1}' | head -n1)
    [ -n "$want" ] || return 2
    if has_cmd sha256sum; then
        got=$(sha256sum "$dest" | awk '{print $1}')
    elif has_cmd shasum; then
        got=$(shasum -a 256 "$dest" | awk '{print $1}')
    else
        return 2
    fi
    [ "$got" = "$want" ] && return 0
    return 1
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
    # verify integrity before we ever run or install the binary
    if verify_checksum "$dest" "$os" "$arch"; then
        log "checksum verified against SHA256SUMS"
        VERIFIED=1
    else
        rc=$?
        if [ "$rc" -eq 1 ]; then
            err "checksum mismatch for $url — refusing to install (possible tampering)"
        fi
        log "WARNING: release has no SHA256SUMS (or no sha256 tool); installing UNVERIFIED binary"
        VERIFIED=0
    fi
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
    macos_fixup "$target" "$sudo_cmd"
}

# macos_fixup re-signs and de-quarantines the installed binary. macOS kills a
# binary with SIGKILL "Code Signature Invalid" when its cdhash doesn't match
# the AMFI-cached one (happens after an in-place overwrite) or when a freshly
# downloaded binary carries a quarantine xattr. ad-hoc re-sign (`-s -`) clears
# both. no-op on non-darwin or when codesign is absent.
macos_fixup() {
    target=$1
    sudo_cmd=$2
    [ "$(detect_os)" = darwin ] || return 0
    has_cmd codesign || return 0
    # only clear the download quarantine when the binary's checksum was verified;
    # stripping it on an UNVERIFIED download would silently defeat Gatekeeper.
    if [ "${VERIFIED:-0}" = "1" ]; then
        $sudo_cmd xattr -d com.apple.quarantine "$target" 2>/dev/null || true
    fi
    # ad-hoc re-sign fixes the cdhash mismatch after an in-place overwrite.
    $sudo_cmd codesign -f -s - "$target" >/dev/null 2>&1 || true
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
