#!/bin/sh
# Builds the theme validator/renderer (Go) to WebAssembly for the editor.
set -eu
root="$(cd "$(dirname "$0")/../../.." && pwd)"
out="$(cd "$(dirname "$0")/.." && pwd)/public"
cd "$root"
GOOS=js GOARCH=wasm go build -trimpath -ldflags="-s -w" -o "$out/tors-theme.wasm" ./cmd/ui-wasm
cp -f "$(go env GOROOT)/lib/wasm/wasm_exec.js" "$out/wasm_exec.js"
chmod 644 "$out/wasm_exec.js"
