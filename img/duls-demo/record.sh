#!/usr/bin/env bash
# Records img/duls-demo.gif. Run from the repository root:
#   bash img/duls-demo/record.sh
# VHS needs Chrome; where the kernel refuses its sandbox, pass
# VHS_NO_SANDBOX=true to this script.
set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$root"

make build
vhs img/duls-demo.tape
