#!/usr/bin/env bash
#
# Pack a DevDojo Framework site folder into the compile protocol, render it,
# and unpack the output into <site>/_out/. Requires jq.
#
#   ./render.sh          # render this example
#   ./render.sh <dir>    # render any site folder, Blade or the HTML syntax
#                        # (./render.sh ../example-studio)
#
# The binary is found on PATH, or set DEVDOJO=/path/to/devdojo.
set -euo pipefail

site="$(cd "${1:-$(dirname "$0")}" && pwd)"
bin="${DEVDOJO:-devdojo}"

if ! command -v "$bin" >/dev/null; then
    echo "devdojo binary not found — build it first:" >&2
    echo "  go build -o /usr/local/bin/devdojo ./cmd/devdojo" >&2
    exit 1
fi

cd "$site"

# Bundle src/ (or resources/, for an older site) and public/ into the protocol's {"files": {...}} document.
dirs=()
for d in src resources public; do [ -d "$d" ] && dirs+=("$d"); done
input=$(find "${dirs[@]}" -type f | while read -r f; do
    jq -n --arg k "$f" --rawfile v "$f" '{($k): $v}'
done | jq -s '{files: (add // {})}')

output=$(printf '%s' "$input" | "$bin" compile)

# Show diagnostics (file:line:col [kind] message), if any.
printf '%s' "$output" | jq -r '.diagnostics[] | "\(.file):\(.line):\(.col) [\(.kind)] \(.message)"' >&2

# Unpack rendered pages into _out/.
mkdir -p _out
printf '%s' "$output" | jq -r '.files | keys[]' | while read -r k; do
    mkdir -p "_out/$(dirname "$k")"
    printf '%s' "$output" | jq -j --arg k "$k" '.files[$k]' > "_out/$k"
    echo "rendered _out/$k"
done
