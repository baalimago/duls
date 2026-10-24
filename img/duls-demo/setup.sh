#!/usr/bin/env bash
# Builds the recording's HOME: a copy of the fixture notebook in
# img/duls-demo/notes and the caption helper. It must not run duls, because the
# first run has to look like a first run: an empty line-count store.
set -euo pipefail

demo_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
root="${1:-$HOME/.cache/duls-demo}"
home="$root/home"

rm -rf "$root"
mkdir -p "$home"
cp -R "$demo_dir/notes" "$home/notes"
install -m 755 "$demo_dir/note.sh" /tmp/duls-note.sh
