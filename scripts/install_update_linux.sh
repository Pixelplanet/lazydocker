#!/bin/bash

# Installs the latest release of this fork, or updates an existing install.
#
#   curl -fsSL https://raw.githubusercontent.com/Pixelplanet/lazydocker/master/scripts/install_update_linux.sh | bash
#
# Set DIR to choose where the binary goes, and VERSION (e.g. v1.2.0) to
# install a specific release instead of the latest one.

set -euo pipefail

REPO="${REPO:-Pixelplanet/lazydocker}"

# Update the binary that is already on the PATH if there is one, so that we
# don't end up with two copies in different places.
on_path() {
    case ":$PATH:" in
        *":$1:"*) return 0 ;;
        *) return 1 ;;
    esac
}

# Otherwise pick a directory that is already on the PATH, so that the command
# works straight away without having to edit shell profiles: ~/.local/bin if
# the shell already looks there, else /usr/local/bin, which every shell does.
if [ -z "${DIR:-}" ]; then
    if EXISTING="$(command -v lazydocker)"; then
        DIR="$(dirname "$(readlink -f "$EXISTING")")"
    elif on_path "$HOME/.local/bin"; then
        DIR="$HOME/.local/bin"
    elif [ -w /usr/local/bin ] || command -v sudo >/dev/null; then
        DIR="/usr/local/bin"
    else
        DIR="$HOME/.local/bin"
    fi
fi

# map different architecture variations to the available binaries
case "$(uname -m)" in
    x86_64|amd64) ARCH=amd64 ;;
    aarch64|arm64) ARCH=arm64 ;;
    armv7*) ARCH=armv7 ;;
    *)
        echo "No release binary for architecture $(uname -m). Build from source instead, see README.md." >&2
        exit 1
        ;;
esac

# /releases/latest redirects to /releases/tag/<version>
if [ -z "${VERSION:-}" ]; then
    LATEST_URL="$(curl -fsSL -o /dev/null -w '%{url_effective}' "https://github.com/${REPO}/releases/latest")"
    case "$LATEST_URL" in
        */releases/tag/*) VERSION="${LATEST_URL##*/}" ;;
        *)
            echo "${REPO} has no published release yet. Build from source instead, see README.md." >&2
            exit 1
            ;;
    esac
fi

FILE="lazydocker_${VERSION#v}_linux_${ARCH}.tar.gz"
URL="https://github.com/${REPO}/releases/download/${VERSION}"

TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT

echo "Downloading lazydocker ${VERSION} (${ARCH})"
curl -fsSL -o "$TMP/$FILE" "$URL/$FILE"
curl -fsSL -o "$TMP/checksums.txt" "$URL/checksums.txt"

# checksums.txt lists the files as ./<name>
(cd "$TMP" && grep " ./${FILE}\$" checksums.txt | sha256sum -c --quiet -)

tar -xzf "$TMP/$FILE" -C "$TMP" lazydocker

if mkdir -p "$DIR" 2>/dev/null && [ -w "$DIR" ]; then
    install -m 755 "$TMP/lazydocker" "$DIR/lazydocker"
else
    echo "$DIR is not writable, using sudo"
    sudo install -D -m 755 "$TMP/lazydocker" "$DIR/lazydocker"
fi

echo "Installed $("$DIR/lazydocker" --version | head -n 1) to $DIR/lazydocker"

if on_path "$DIR"; then
    echo "Run it with: lazydocker"
else
    echo "$DIR is not on your PATH. Run it with: $DIR/lazydocker"
    echo "To be able to run it as 'lazydocker', add this line to ~/.profile and log in again:"
    echo "    export PATH=\"$DIR:\$PATH\""
fi
