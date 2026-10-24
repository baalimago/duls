#!/usr/bin/env bash
set -euo pipefail

# Prints one caption line, then erases the shell line that invoked it, so the
# recording shows the caption alone. Keep the whole invocation, path included,
# inside one terminal row: the erase moves up two rows and would leave a
# wrapped fragment behind.
printf '\033[1;36m▸\033[0m \033[1m%s\033[0m\n' "$*"
printf '\033[2A\r\033[2K\033[2B'
