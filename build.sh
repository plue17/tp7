#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "$0")" && pwd)"

for app in "$ROOT"/cmd/*/; do
    name="$(basename "$app")"
    echo "building $name..."
    go build -o "$ROOT/$name" "$app"
done

echo "done."
