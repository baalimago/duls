#!/bin/sh

# Print the download URL of the latest release asset for the given OS and arch.
get_latest_release_url() {
  repo="baalimago/duls"
  os="$1"
  arch="$2"

  release_data=$(curl -fsSL "https://api.github.com/repos/$repo/releases/latest")

  printf '%s\n' "$release_data" |
    grep -o '"browser_download_url": "[^"]*-'"$os"'-'"$arch"'"' |
    cut -d '"' -f 4
}

# Detect the OS.
case "$(uname)" in
  Linux*)
    os="linux"
    ;;
  Darwin*)
    os="darwin"
    ;;
  *)
    echo "Unsupported OS: $(uname)"
    exit 1
    ;;
esac

# Detect the architecture. The release pipeline builds amd64, arm64 and 386.
case "$(uname -m)" in
  x86_64)
    arch="amd64"
    ;;
  aarch64 | arm64)
    arch="arm64"
    ;;
  i?86)
    arch="386"
    ;;
  *)
    echo "Unsupported architecture: $(uname -m)"
    exit 1
    ;;
esac

printf "detected os: '%s', arch: '%s'\n" "$os" "$arch"

printf "finding asset url..."
download_url=$(get_latest_release_url "$os" "$arch")
printf "OK!\n"

if [ -z "$download_url" ]; then
  echo "No release asset for $os-$arch. See https://github.com/baalimago/duls/releases."
  exit 1
fi

tmp_file=$(mktemp)

printf "downloading binary..."
if ! curl -fsSL -o "$tmp_file" "$download_url"; then
  echo
  echo "Failed to download $download_url."
  exit 1
fi
printf "OK!\n"

printf "setting file executable file permissions..."
if ! chmod +x "$tmp_file"; then
  echo
  echo "Failed to make the binary executable. Try running the script with sudo."
  exit 1
fi
printf "OK!\n"

# Install into the standard XDG location, or systemwide when run as root.
INSTALL_DIR=$HOME/.local/bin
if [ "$(/usr/bin/id -u)" -eq 0 ]; then
  INSTALL_DIR=/usr/local/bin
fi

if ! mkdir -p "$INSTALL_DIR"; then
  echo "Failed to create $INSTALL_DIR."
  exit 1
fi

if ! mv "$tmp_file" "$INSTALL_DIR/duls"; then
  echo "Failed to move the binary to $INSTALL_DIR/duls, see error above. Try making sure you have write permission there, or run 'mv $tmp_file <desired-position>'."
  exit 1
fi

echo "duls installed successfully in $INSTALL_DIR, try it out with 'duls -h'"
